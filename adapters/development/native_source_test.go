package development_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/ruipengliu/lerna/adapters/development"
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
}
