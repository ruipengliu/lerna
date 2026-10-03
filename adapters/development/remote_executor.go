package development

import (
	"context"
	"errors"
	"net/url"
	"path/filepath"
	"sync"
	"time"

	target "github.com/ruipengliu/lerna/adapters/execution"
	"github.com/ruipengliu/lerna/adapters/executor"
	"github.com/ruipengliu/lerna/adapters/platform"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/runtime"
)

// RemoteExecutorConfig 固定原设备 owner、SQLite 身份、实例、TLS 和签名公钥。
// 它只登记受信候选；每项行动仍须配置准确 ActionBinding/Grant。
type RemoteExecutorConfig struct {
	OwnerID       string             `json:"owner_id"`
	DatabaseID    string             `json:"database_id"`
	InstanceID    string             `json:"instance_id"`
	Endpoint      string             `json:"endpoint"`
	TLSCAFile     string             `json:"tls_ca_file"`
	PeerTokenFile string             `json:"peer_token_file"`
	PublicKeyID   string             `json:"public_key_id"`
	PublicX       string             `json:"public_x"`
	PublicY       string             `json:"public_y"`
	Bindings      []executor.Binding `json:"bindings"`
}

type remoteExecutor struct {
	Config RemoteExecutorConfig
	Digest string
	Keys   *platform.Keyring
	Client *executor.Client
	mu     sync.Mutex
	dial   *remoteDial
	closed bool
}

type remoteDial struct {
	done   chan struct{}
	cancel context.CancelFunc
	err    error
}

type remoteExecutors struct {
	app       *App
	routes    map[string]*remoteExecutor
	closeOnce sync.Once
	closeErr  error
}

// 构造只验证静态配对，不拨号、发 RPC 或打开目标。
func (a *App) configureRemoteExecutors() error {
	if len(a.Config.RemoteExecutors) > 4 {
		return api.E("invalid_request", "remote_executor_limit")
	}
	r := &remoteExecutors{app: a, routes: map[string]*remoteExecutor{}}
	for _, configured := range a.Config.RemoteExecutors {
		var c RemoteExecutorConfig
		if err := api.Decode(api.Raw(configured), &c); err != nil {
			return err
		}
		u, err := url.Parse(c.Endpoint)
		if err != nil || u.Scheme != "grpcs" || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || len(c.Endpoint) > 2048 || !api.ValidID(c.OwnerID) || c.OwnerID == a.Scope.OwnerID || !api.ValidID(c.DatabaseID) || !api.ValidID(c.InstanceID) || !filepath.IsAbs(c.TLSCAFile) || !filepath.IsAbs(c.PeerTokenFile) || len(c.PublicKeyID) == 0 || len(c.PublicKeyID) > 128 || len(c.Bindings) == 0 || len(c.Bindings) > maxActionBindings {
			return api.E("invalid_request", "remote_executor_configuration_invalid")
		}
		if r.routes[c.OwnerID] != nil {
			return api.E("invalid_request", "duplicate_remote_executor")
		}
		public, err := platform.ParsePublicKey(c.PublicX, c.PublicY)
		if err != nil {
			return api.E("forbidden", "device_signing_key_not_paired")
		}
		seen := map[string]bool{}
		for _, b := range c.Bindings {
			read := api.Equal(b.CapabilityRef, target.FileReadCapability().Ref)
			write := api.Equal(b.CapabilityRef, target.FileWriteCapability().Ref)
			if (!read && !write) || runtime.CheckRef(a.Scope, b.BindingRef) != nil || b.BindingRef.OwnerID != c.OwnerID || api.ValidateRecord("ComponentRef", b.InstallLockRef) != nil || len(b.Resources) != 1 || b.Resources[0] != "managed-files" || len(b.Actions) != 1 || (read && b.Actions[0] != "file.read") || (write && b.Actions[0] != "file.write") || seen[b.BindingRef.ObjectID] {
				return api.E("forbidden", "device_binding_not_qualified")
			}
			seen[b.BindingRef.ObjectID] = true
		}
		digest, err := api.Digest(c)
		if err != nil {
			return err
		}
		r.routes[c.OwnerID] = &remoteExecutor{Config: c, Digest: digest, Keys: &platform.Keyring{Keys: map[string]platform.RegisteredKey{c.PublicKeyID: {TenantID: a.Scope.TenantID, Issuer: c.OwnerID, Public: public, Purposes: []string{"executor_usage", "executor_content"}}}}}
	}
	a.remoteExecutors = r
	return nil
}

func (r *remoteExecutors) binding(ref api.ObjectRef, cap, lock api.ComponentRef) (*remoteExecutor, error) {
	if r == nil || r.routes[ref.OwnerID] == nil {
		return nil, api.E("unsupported", "original_remote_executor_not_configured")
	}
	route := r.routes[ref.OwnerID]
	for _, b := range route.Config.Bindings {
		if api.Equal(b.BindingRef, ref) && api.Equal(b.CapabilityRef, cap) && api.Equal(b.InstallLockRef, lock) {
			return route, nil
		}
	}
	return nil, api.E("forbidden", "original_device_binding_mismatch")
}

func (r *remoteExecutors) client(ctx context.Context, owner string) (*remoteExecutor, error) {
	if r == nil {
		return nil, api.E("unsupported", "original_remote_executor_not_configured")
	}
	route := r.routes[owner]
	if route == nil {
		return nil, api.E("unsupported", "original_remote_executor_not_configured")
	}
	route.mu.Lock()
	if route.closed {
		route.mu.Unlock()
		return nil, api.E("dependency_unavailable", "remote_executor_host_closed")
	}
	if route.Client != nil {
		route.mu.Unlock()
		return route, nil
	}
	if pending := route.dial; pending != nil {
		route.mu.Unlock()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-pending.done:
			if pending.err != nil {
				return nil, pending.err
			}
			return r.client(ctx, owner)
		}
	}
	dialCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	pending := &remoteDial{done: make(chan struct{}), cancel: cancel}
	route.dial = pending
	route.mu.Unlock()
	// 文件、TLS 和 journal 的建立都在锁外；纯 VerifyTx 不等待这些 I/O。
	c := route.Config
	client, err := executor.Dial(dialCtx, executor.RemoteConfig{DatabaseID: c.DatabaseID, Endpoint: c.Endpoint, OwnerID: c.OwnerID, InstanceID: c.InstanceID, AuthorityID: r.app.Scope.OwnerID, TenantID: r.app.Scope.TenantID, TLSCAFile: c.TLSCAFile, PeerTokenFile: c.PeerTokenFile, JournalRoot: r.journalRoot(c.OwnerID)})
	cancel()
	route.mu.Lock()
	closed := route.closed
	if err == nil && !closed {
		route.Client = client
	}
	route.mu.Unlock()
	if closed {
		if client != nil {
			err = errors.Join(api.E("dependency_unavailable", "remote_executor_host_closed"), client.Close())
		} else if err == nil {
			err = api.E("dependency_unavailable", "remote_executor_host_closed")
		}
	}
	route.mu.Lock()
	pending.err = err
	route.dial = nil
	close(pending.done)
	route.mu.Unlock()
	if err != nil {
		return nil, err
	}
	return route, nil
}

func (r *remoteExecutors) journalRoot(owner string) string {
	instance := r.app.Role
	if r.app.Role == "worker" && r.app.Config.WorkerPool != nil {
		instance += "/" + r.app.Config.WorkerPool.PoolID
	}
	if r.app.Role == "application" && r.app.Config.EndpointChannels != nil {
		instance += "/" + r.app.Config.EndpointChannels.ApplicationInstanceID
	}
	return filepath.Join(r.app.Config.DataRoot, "remote-executor-journals", owner, instance)
}

func (r *remoteExecutors) close() error {
	if r == nil {
		return nil
	}
	r.closeOnce.Do(func() {
		pending := []*remoteDial{}
		clients := []*executor.Client{}
		for _, route := range r.routes {
			route.mu.Lock()
			route.closed = true
			if route.dial != nil {
				pending = append(pending, route.dial)
			}
			if route.Client != nil {
				clients = append(clients, route.Client)
			}
			route.mu.Unlock()
		}
		for _, dial := range pending {
			dial.cancel()
		}
		for _, dial := range pending {
			<-dial.done
		}
		for _, client := range clients {
			r.closeErr = errors.Join(r.closeErr, client.Close())
		}
	})
	return r.closeErr
}
