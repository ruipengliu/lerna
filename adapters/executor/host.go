package executor

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	target "github.com/ruipengliu/lerna/adapters/execution"
	rpc "github.com/ruipengliu/lerna/adapters/grpc"
	"github.com/ruipengliu/lerna/adapters/objectstore"
	"github.com/ruipengliu/lerna/adapters/platform"
	"github.com/ruipengliu/lerna/adapters/sqlite"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/execution"
	"github.com/ruipengliu/lerna/internal/governance"
	"github.com/ruipengliu/lerna/runtime"
)

type TrustedAuthority struct {
	KeyID   string `json:"key_id"`
	OwnerID string `json:"owner_id"`
	PublicX string `json:"public_x"`
	PublicY string `json:"public_y"`
}
type Config struct {
	Development        bool             `json:"development"`
	TenantID           string           `json:"tenant_id"`
	OwnerID            string           `json:"owner_id"`
	InstanceID         string           `json:"instance_id"`
	DatabaseID         string           `json:"database_id"`
	DatabasePath       string           `json:"database_path"`
	DataRoot           string           `json:"data_root"`
	SigningKeyFile     string           `json:"signing_key_file"`
	PeerTokenFile      string           `json:"peer_token_file"`
	Authority          TrustedAuthority `json:"authority"`
	Bindings           []Binding        `json:"bindings"`
	GRPCAddr           string           `json:"grpc_addr"`
	TLSCertificateFile string           `json:"tls_certificate_file"`
	TLSKeyFile         string           `json:"tls_key_file"`
}
type Host struct {
	Config     Config
	Store      runtime.Store
	Scope      runtime.Scope
	Registry   *runtime.Registry
	Dispatcher *runtime.Dispatcher
	Identity   *platform.DevIdentity
	Keys       *platform.Keyring
	Proof      Proof
	Execution  *execution.Service
	Governance *governance.Service
	Objects    *objectstore.Local
	Files      *target.ManagedFiles
}

func Open(ctx context.Context, c Config, initialize bool) (host *Host, err error) {
	if !c.Development || !api.ValidID(c.TenantID) || !api.ValidID(c.OwnerID) || !api.ValidID(c.InstanceID) || !api.ValidID(c.Authority.OwnerID) || c.Authority.OwnerID == c.OwnerID || c.Authority.KeyID == "" || !filepath.IsAbs(c.DatabasePath) || !filepath.IsAbs(c.DataRoot) || !filepath.IsAbs(c.SigningKeyFile) || !filepath.IsAbs(c.PeerTokenFile) || len(c.Bindings) == 0 || len(c.Bindings) > 32 {
		return nil, api.E("unsupported", "independent_executor_unconfigured")
	}
	public, e := platform.ParsePublicKey(c.Authority.PublicX, c.Authority.PublicY)
	if e != nil {
		return nil, api.E("forbidden", "authority_key_not_paired")
	}
	if !initialize && c.DatabaseID == "" {
		return nil, api.E("forbidden", "original_device_database_required")
	}
	if !initialize {
		if _, e = os.Stat(c.SigningKeyFile); e != nil {
			return nil, api.E("forbidden", "original_device_key_required")
		}
	}
	st, e := sqlite.Open(c.DatabasePath, sqlite.WithExpectedDatabaseID(c.DatabaseID))
	if e != nil {
		return nil, e
	}
	h := &Host{Config: c, Store: st, Registry: runtime.NewRegistry()}
	ok := false
	defer func() {
		if !ok {
			err = errors.Join(err, h.Close())
		}
	}()
	if initialize {
		if e = st.Migrate(ctx); e != nil {
			return nil, e
		}
	}
	h.Config.DatabaseID = st.ID()
	h.Scope = runtime.Scope{TenantID: c.TenantID, OwnerID: c.OwnerID, DatabaseID: st.ID()}
	h.Keys, e = platform.OpenDevelopmentKey(c.SigningKeyFile, c.TenantID, c.OwnerID, []string{"grant_use", "executor_usage", "executor_content"})
	if e != nil {
		return nil, e
	}
	deviceKey := h.Keys.Keys["development-es256"]
	delete(h.Keys.Keys, "development-es256")
	h.Keys.Keys["device-es256"] = deviceKey
	if c.Authority.KeyID == "device-es256" {
		return nil, api.E("forbidden", "authority_key_id_collision")
	}
	h.Keys.Keys[c.Authority.KeyID] = platform.RegisteredKey{TenantID: c.TenantID, Issuer: c.Authority.OwnerID, Public: public, Purposes: []string{"control", "grant_lease", "executor_admission", "executor_revocation"}}
	h.Proof = Proof{Keys: h.Keys, SigningKeyID: "device-es256"}
	token, e := privateToken(c.PeerTokenFile)
	if e != nil {
		return nil, e
	}
	peer := runtime.Auth{TenantID: c.TenantID, SubjectID: c.Authority.OwnerID, CredentialGeneration: 1, Roles: []string{"executor_peer"}}
	h.Identity = &platform.DevIdentity{Store: st, OwnerID: c.OwnerID, Principals: []platform.Principal{{Auth: peer, TokenHash: api.Hash([]byte(token))}}, SessionTTL: time.Hour}
	if initialize {
		if e = h.Identity.Initialize(ctx); e != nil {
			return nil, e
		}
	} else if e = h.Identity.CheckCurrent(ctx, peer); e != nil {
		return nil, e
	}
	h.Objects, e = objectstore.OpenLocal(filepath.Join(c.DataRoot, "objects"), MaxContentBytes)
	if e != nil {
		return nil, e
	}
	if initialize {
		if e = os.MkdirAll(filepath.Join(c.DataRoot, "files"), 0700); e != nil {
			return nil, e
		}
	}
	h.Files, e = target.NewManagedFiles(filepath.Join(c.DataRoot, "files"))
	if e != nil {
		return nil, e
	}
	h.Governance = governance.New(st, governance.Options{Proof: h.Proof, OfflineGate: deviceAuthority{h}, EndpointID: c.OwnerID, InstanceID: c.InstanceID, Participants: []string{Namespace, "execution"}})
	drivers := []execution.Driver{&target.FileDriver{Files: h.Files, Content: deviceContent{h}, Location: "device"}, &target.FileDriver{Files: h.Files, Content: deviceContent{h}, Location: "device", ReadOnly: true}}
	for _, binding := range c.Bindings {
		found := false
		for _, driver := range drivers {
			if api.Equal(driver.Capability().Ref, binding.CapabilityRef) {
				found = true
			}
		}
		if !found || runtime.CheckRef(h.Scope, binding.BindingRef) != nil || api.ValidateRecord("ComponentRef", binding.InstallLockRef) != nil || len(binding.Resources) == 0 || len(binding.Actions) == 0 {
			return nil, api.E("unsupported", "device_binding_not_installed")
		}
	}
	h.Execution, e = execution.New(execution.Config{OwnerID: c.OwnerID, Content: deviceContent{h}, Authority: deviceAuthority{h}, AuthorityParticipants: []string{Namespace, "governance"}, Drivers: drivers, Location: "device"})
	if e != nil {
		return nil, e
	}
	if e = h.Execution.Register(h.Registry); e != nil {
		return nil, e
	}
	if e = h.register(); e != nil {
		return nil, e
	}
	h.Dispatcher = &runtime.Dispatcher{Store: st, OwnerID: c.OwnerID, Registry: h.Registry}
	ok = true
	return h, nil
}
func privateToken(path string) (string, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return "", api.E("forbidden", "peer_token_file_permissions")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	token := strings.TrimSpace(string(b))
	if len(token) < 32 || len(token) > 4089 || strings.ContainsAny(token, "\r\n") {
		return "", api.E("forbidden", "invalid_peer_token")
	}
	return token, nil
}
func (h *Host) Close() error {
	var err error
	if h.Files != nil {
		err = errors.Join(err, h.Files.Close())
		h.Files = nil
	}
	if h.Store != nil {
		err = errors.Join(err, h.Store.Close())
		h.Store = nil
	}
	return err
}

// Run 持有独立设备的真实 listener 和 worker，取消后等待实际退出再关闭 SQLite。
func (h *Host) Run(ctx context.Context) error {
	cert, err := tls.LoadX509KeyPair(h.Config.TLSCertificateFile, h.Config.TLSKeyFile)
	if err != nil {
		return api.E("forbidden", "device_tls_unconfigured")
	}
	listener, err := net.Listen("tcp", h.Config.GRPCAddr)
	if err != nil {
		return err
	}
	server, err := rpc.New(rpc.Config{OwnerID: h.Scope.OwnerID, Identity: h.Identity, Processor: h})
	if err != nil {
		listener.Close()
		return err
	}
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	failures := make(chan error, 2)
	worker := runtime.Worker{Store: h.Store, Registry: h.Registry, Scopes: []runtime.Scope{h.Scope}, Kinds: h.Registry.JobKinds(), Concurrency: 2, Lease: 30 * time.Second, Poll: 10 * time.Millisecond}
	go func() {
		failures <- server.Serve(runCtx, listener, &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{cert}})
	}()
	go func() { failures <- worker.Run(runCtx) }()
	count := 2
	var first error
	select {
	case <-ctx.Done():
	case first = <-failures:
		count--
	}
	cancel()
	for ; count > 0; count-- {
		first = errors.Join(first, <-failures)
	}
	return first
}
