package development

import (
	"context"
	"errors"
	"net"
	"net/http"
	"time"

	rpcadapter "github.com/ruipengliu/lerna/adapters/grpc"
	"github.com/ruipengliu/lerna/adapters/wss"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/runtime"
)

// Run binds only the configured address; development's cleartext exception is loopback.
func (a *App) Run(ctx context.Context, serve, work bool) error {
	if !serve && !work {
		return api.E("invalid_request", "process_role_required")
	}
	if work && !a.OwnsTargets {
		return api.E("unsupported", "worker_target_ownership_required")
	}
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	failures := make(chan error, 2)
	count := 0
	var server *http.Server
	var gateway *wss.Server
	if serve {
		if a.Config.Development {
			host, _, err := net.SplitHostPort(a.Config.HTTPAddr)
			if err != nil || net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback() {
				return api.E("forbidden", "development_requires_loopback")
			}
		}
		var err error
		gateway, err = a.Gateway()
		if err != nil {
			return err
		}
		listener, err := net.Listen("tcp", a.Config.HTTPAddr)
		if err != nil {
			return errors.Join(err, gateway.Close())
		}
		server = &http.Server{Handler: gateway.Handler(), BaseContext: func(net.Listener) context.Context { return runCtx }, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16384}
		count++
		go func() {
			err := server.Serve(listener)
			if errors.Is(err, http.ErrServerClosed) {
				err = nil
			}
			failures <- err
		}()
	}
	if work {
		worker := runtime.Worker{Store: a.Store, Registry: a.Registry, Scopes: []runtime.Scope{a.Scope}, Kinds: a.Registry.JobKinds(), Concurrency: 8, Lease: 30 * time.Second, Poll: 25 * time.Millisecond}
		count++
		go func() { failures <- worker.Run(runCtx) }()
	}
	var first error
	select {
	case <-ctx.Done():
	case first = <-failures:
		count--
	}
	cancel()
	if server != nil {
		stopCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
		err := server.Shutdown(stopCtx)
		stop()
		if err != nil {
			err = errors.Join(err, server.Close())
		}
		first = errors.Join(first, err, gateway.Close())
	}
	for ; count > 0; count-- {
		first = errors.Join(first, <-failures)
	}
	return first
}

// RunRPC exposes the same current authenticated contracts to the application
// process. EndpointChannel remains closed until a real endpoint authority exists.
func (a *App) RunRPC(ctx context.Context) error {
	server, err := rpcadapter.New(rpcadapter.Config{OwnerID: a.Config.OwnerID, Identity: a.Identity, Processor: wss.LocalProcessor{Dispatcher: a.Dispatcher}, AllowInsecureLoopback: a.Config.Development})
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp", a.Config.GRPCAddr)
	if err != nil {
		return err
	}
	return server.Serve(ctx, listener, nil)
}
