package development

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/ruipengliu/lerna/adapters/collaboration"
	"github.com/ruipengliu/lerna/adapters/platform"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/runtime"
)

func TestConfiguredRemoteAgentConstructsWithoutOutbound(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	cfg, err := InitializeConfig(ctx, filepath.Join(root, "config.json"), root, "sqlite")
	if err != nil {
		t.Fatal(err)
	}
	cfg.TenantID, cfg.OwnerID, cfg.SubjectID = api.NewID("tenant"), api.NewID("owner"), api.NewID("subject")
	peer, err := InitializeConfig(ctx, filepath.Join(t.TempDir(), "config.json"), t.TempDir(), "sqlite")
	if err != nil {
		t.Fatal(err)
	}
	peer.OwnerID = api.NewID("owner")
	keys, err := platform.NewDevelopmentKey(cfg.TenantID, peer.OwnerID, []string{"agent_allocation", "agent_state", "foreign_content"})
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKIXPublicKey(keys.Keys["development-es256"].Public)
	if err != nil {
		t.Fatal(err)
	}
	publicFile := filepath.Join(root, "peer-public.pem")
	if err = os.WriteFile(publicFile, pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}), 0600); err != nil {
		t.Fatal(err)
	}
	var outbound atomic.Int64
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { outbound.Add(1); w.WriteHeader(503) }))
	defer server.Close()
	ca := filepath.Join(root, "peer-ca.pem")
	if err = os.WriteFile(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HARNESS_AGENT_TEST_INBOUND", "opaque-reference-inbound-credential")
	t.Setenv("HARNESS_AGENT_TEST_OUTBOUND", "opaque-reference-outbound-credential")
	profile, err := collaboration.NewRemoteAgentProfile(api.NewID("profile"), "1", collaboration.RemoteAgentValues{ParentOwnerID: cfg.OwnerID, ReceiverID: peer.OwnerID, AgentBindingRef: api.ObjectRef{TenantID: cfg.TenantID, OwnerID: cfg.OwnerID, ObjectID: api.NewID("binding"), Revision: 1}, PolicyRef: component("task-policy"), InstallLockRef: component("builtin-install-lock"), SubjectRefs: []api.ObjectRef{{TenantID: cfg.TenantID, OwnerID: cfg.OwnerID, ObjectID: cfg.SubjectID, Revision: 1}}, PermissionRefs: []api.ObjectRef{}, CapabilityRefs: []api.ComponentRef{}, BindingRefs: []api.ObjectRef{}, ResourceRefs: []api.ComponentRef{}, BudgetLimits: []api.Amount{{Unit: "USD", Value: "3"}}, MaxDepth: 4, MaxInputs: 4, Location: "cloud", MaterialPurposes: []string{"task.goal", "content.write"}})
	if err != nil {
		t.Fatal(err)
	}
	cfg.RemoteAgent = &RemoteAgentConfig{Profiles: []collaboration.RemoteAgentProfile{profile}, Peers: []RemoteAgentPeerConfig{{TenantID: cfg.TenantID, OwnerID: peer.OwnerID, DatabaseID: peer.DatabaseID, Endpoint: server.URL, CAFile: ca, SigningKeyID: "development-es256", SigningPublicKeyFile: publicFile, SigningPublicKeyDigest: api.Hash(der), OutboundTokenEnvRef: "HARNESS_AGENT_TEST_OUTBOUND", InboundTokenEnvRef: "HARNESS_AGENT_TEST_INBOUND", InboundSubjectRef: runtime.Scope{TenantID: cfg.TenantID, OwnerID: cfg.OwnerID}.Ref(peer.OwnerID, 1)}}}
	app, err := OpenApp(ctx, cfg, true)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	if app.RemoteAgent == nil || outbound.Load() != 0 {
		t.Fatalf("explicit remote construction: configured=%v outbound=%d", app.RemoteAgent != nil, outbound.Load())
	}
	var registered bool
	for _, method := range app.Registry.Contracts() {
		registered = registered || method.Name == "collaboration.delegate"
	}
	if !registered {
		t.Fatal("public remote delegation method not registered")
	}
	if err = app.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenApp(ctx, cfg, false)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if reopened.RemoteAgent == nil || outbound.Load() != 0 {
		t.Fatalf("original configured identity reopen: configured=%v outbound=%d", reopened.RemoteAgent != nil, outbound.Load())
	}
}
