package development_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/adapters/development"
	rpc "github.com/ruipengliu/lerna/adapters/grpc"
	"github.com/ruipengliu/lerna/adapters/providers"
	"github.com/ruipengliu/lerna/adapters/sqlite"
	"github.com/ruipengliu/lerna/api"
)

// 配置、真正的独立数据库与公开合同是接缝；构造不预造 CopyHolder 或许可证明。
func TestConfiguredForeignConsumerOpensOriginalSourceContracts(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	path := filepath.Join(root, "config.json")
	cfg, err := development.InitializeConfig(ctx, path, root, "sqlite")
	if err != nil {
		t.Fatal(err)
	}
	cfg.TenantID, cfg.OwnerID, cfg.SubjectID = api.NewID("tenant"), api.NewID("owner"), api.NewID("subject")
	consumer, err := sqlite.Open(filepath.Join(t.TempDir(), "independent-consumer.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer consumer.Close()
	if err = consumer.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	owner, subject, peer := api.NewID("owner"), api.NewID("subject"), api.NewID("subject")
	t.Setenv("HARNESS_NATIVE_SOURCE_TEST_PEER", "explicit-native-content-peer-credential")
	var object map[string]any
	if err = json.Unmarshal(api.Raw(cfg), &object); err != nil {
		t.Fatal(err)
	}
	object["foreign_consumers"] = []any{map[string]any{
		"tenant_id": cfg.TenantID, "owner_id": owner, "database_id": consumer.ID(),
		"peer_subject_ref":      map[string]any{"tenant_id": cfg.TenantID, "owner_id": cfg.OwnerID, "object_id": peer, "revision": 1},
		"inbound_token_env_ref": "HARNESS_NATIVE_SOURCE_TEST_PEER",
		"holders":               []any{map[string]any{"subject_ref": map[string]any{"tenant_id": cfg.TenantID, "owner_id": owner, "object_id": subject, "revision": 1}, "roles": []string{}}},
		"purposes":              []string{"read", "memory.save", "memory.read"}, "locations": []string{"local"},
	}}
	if err = os.WriteFile(path, api.Raw(object), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err = development.LoadConfig(path)
	if err != nil {
		t.Fatalf("explicit original consumer configuration was rejected: %v", err)
	}
	app, err := development.OpenApp(ctx, cfg, true)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	for _, expected := range providers.ForeignSourceContracts() {
		actual, ok := app.Registry.Method(expected.Name)
		if !ok || actual.Contract.SchemaDigest != expected.SchemaDigest {
			t.Fatalf("original Source contract %s missing or changed", expected.Name)
		}
	}
	app.Config.HTTPAddr = "127.0.0.1:0"
	serveContext, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	err = app.Run(serveContext, true, false)
	cancel()
	if !api.IsCode(err, "unsupported") {
		t.Fatalf("configured Source served without its required HTTPS authority: %v", err)
	}
	if err = app.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := development.OpenApp(ctx, cfg, false)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if reopened.Scope != app.Scope {
		t.Fatal("source construction replaced its original database identity")
	}
	if err = reopened.Close(); err != nil {
		t.Fatal(err)
	}
	changed := cfg
	changed.ForeignConsumers = append([]development.ForeignConsumerConfig{}, cfg.ForeignConsumers...)
	changed.ForeignConsumers[0].DatabaseID = api.NewID("database")
	if replacement, err := development.OpenApp(ctx, changed, false); !api.IsCode(err, "idempotency_conflict") {
		if replacement != nil {
			_ = replacement.Close()
		}
		t.Fatalf("another consumer database replaced the original paired responsibility: %v", err)
	}
	disabled := cfg
	disabled.ForeignConsumers = nil
	closed, err := development.OpenApp(ctx, disabled, false)
	if err != nil {
		t.Fatal(err)
	}
	defer closed.Close()
	if _, open := closed.Registry.Method("content.foreign.current"); open {
		t.Fatal("missing explicit consumer configuration opened Source transport")
	}
	// 同一 gateway 只能有一个准确 TLS 配对：同引用可复用，另一组不能并行。
	combined := cfg
	combined.Driver = "postgres"
	files := &development.ForeignSourceTLSConfig{CertificateFile: filepath.Join(root, "paired-cert.pem"), KeyFile: filepath.Join(root, "paired-key.pem")}
	combined.ForeignSourceTLS = files
	combined.EndpointChannels = &development.EndpointChannelConfig{GatewayInstanceID: api.NewID("instance"), ApplicationInstanceID: api.NewID("instance"), ApplicationAddresses: []string{"grpcs://127.0.0.1:24433"}, GatewayIdentities: []string{"spiffe://harness.test/approved-gateway"}, Registrations: []rpc.EndpointRegistration{{TenantID: cfg.TenantID, SubjectID: cfg.SubjectID, CredentialGeneration: 1, EndpointID: api.NewID("endpoint"), InstanceID: api.NewID("instance"), Generation: 1, RecipientServiceID: cfg.OwnerID}}, GatewayTLS: development.EndpointTLSFiles{CertificateFile: files.CertificateFile, KeyFile: files.KeyFile}}
	combinedPath := filepath.Join(root, "combined-source-endpoint.json")
	if err = development.SaveConfig(combinedPath, combined); err != nil {
		t.Fatal(err)
	}
	if _, err = development.LoadConfig(combinedPath); err != nil {
		t.Fatalf("same gateway TLS references were not reusable: %v", err)
	}
	combined.EndpointChannels.GatewayTLS.KeyFile = filepath.Join(root, "another-key.pem")
	if err = development.SaveConfig(combinedPath, combined); err != nil {
		t.Fatal(err)
	}
	if _, err = development.LoadConfig(combinedPath); !api.IsCode(err, "forbidden") {
		t.Fatalf("different endpoint/Source TLS references became parallel authority: %v", err)
	}
}
