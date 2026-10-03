package bootstrap

import (
	"context"
	"errors"
	"net"
	"net/http"
	"time"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/runtime"
)

// Run binds only the configured address; development's cleartext exception is loopback.
func (a *App) Run(ctx context.Context, serve, work bool) error {
	if !serve && !work {
		return api.E("invalid_request", "process_role_required")
	}
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	failures := make(chan error, 2)
	count := 0
	var server *http.Server
	if serve {
		if a.Config.Development {
			host, _, err := net.SplitHostPort(a.Config.HTTPAddr)
			if err != nil || net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback() {
				return api.E("forbidden", "development_requires_loopback")
			}
		}
		gateway, err := a.Gateway()
		if err != nil {
			return err
		}
		listener, err := net.Listen("tcp", a.Config.HTTPAddr)
		if err != nil {
			return err
		}
		server = &http.Server{Handler: gateway.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16384}
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
		if first == nil {
			first = err
		}
	}
	for ; count > 0; count-- {
		if err := <-failures; first == nil {
			first = err
		}
	}
	return first
}
