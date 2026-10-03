package brain_test

import (
	"context"
	"github.com/ruipengliu/lerna/adapters/sqlite"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/brain"
	"github.com/ruipengliu/lerna/runtime"
	"path/filepath"
	"testing"
	"time"
)

// 原模型结果失联后恢复不得增加物理请求；真实 SQLite 保留 send_started。
func TestUnknownModelCallIsNeverSentTwice(t *testing.T) {
	ctx := context.Background()
	s, e := sqlite.Open(filepath.Join(t.TempDir(), "brain.sqlite"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	if e = s.Migrate(ctx); e != nil {
		t.Fatal(e)
	}
	scope := runtime.Scope{TenantID: api.NewID("tenant"), OwnerID: api.NewID("owner"), DatabaseID: s.ID()}
	auth := runtime.Auth{TenantID: scope.TenantID, SubjectID: scope.OwnerID, CredentialGeneration: 1, Roles: []string{"service"}}
	profile := brain.Profile{Ref: api.ComponentRef{ComponentID: api.NewID("profile"), Version: "1", Digest: api.Hash([]byte("fixture"))}, ContextLimit: 10000, MaxInputTokens: 9000, MaxOutputTokens: 1000, SafetyMargin: 10, MaxInputBytes: 65536, RequestTimeout: time.Second}
	content := &contents{items: map[string][]byte{}}
	goal := content.add(scope, "goal", api.Raw(brain.GoalSpec{Kind: "answer", Body: "accurate answer"}))
	snap := api.Snapshot{SnapshotID: api.NewID("snapshot"), Revision: 1, TaskRef: scope.Ref(api.NewID("task"), 1), GoalRevision: 1, ControlRevision: 1, GoalRef: goal, Requirements: []api.Requirement{}, RequirementsDigest: api.Hash([]byte("requirements")), RequirementsState: "collecting", Purpose: "interpret_requirements", FactRefs: []api.ObjectRef{}, UnresolvedCollections: []api.CollectionSummary{}, PolicyRef: profile.Ref, InstallLockRef: profile.Ref, ModelProfileRef: profile.Ref, CapabilityRefs: []api.ComponentRef{}, BindingRefs: []api.ObjectRef{}, MaterialRefs: []api.ContentRef{}, SelectionReportRef: goal, ProcessedSources: []api.ContentRef{goal}, InputTokens: 100, ReservedOutputTokens: 100, SafetyMarginTokens: 10, CountMode: "upper_bound", TokenizerRef: profile.Ref, EncodedDigest: api.Hash([]byte("sealed"))}
	snapshot := content.add(scope, "snapshot", api.Raw(snap))
	engine := &unknownEngine{}
	service, e := brain.New(brain.Config{Profiles: []brain.Profile{profile}, Content: content, Engine: engine, Gate: gate{}})
	if e != nil {
		t.Fatal(e)
	}
	registry := runtime.NewRegistry()
	if e = service.Register(registry); e != nil {
		t.Fatal(e)
	}
	dispatch := runtime.Dispatcher{Store: s, OwnerID: scope.OwnerID, Registry: registry}
	id := api.NewID("decision")
	command := api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: scope.OwnerID, CommandID: api.NewID("command"), TargetID: id, Method: "brain.decide", ExpiresAt: api.Time(time.Now().Add(time.Minute)), Payload: api.Raw(brain.DecideInput{DecisionID: id, TaskRef: snap.TaskRef, SnapshotRef: snapshot, SnapshotRevision: 1, ModelProfileRef: profile.Ref, UseRefs: []api.ObjectRef{}, Limits: []api.Amount{{Unit: "USD", Value: "1"}}, Deadline: api.Time(time.Now().Add(time.Minute))})}
	r, e := dispatch.Command(ctx, auth, api.Raw(command))
	if e != nil || r.Stage != "accepted" {
		t.Fatalf("accept %+v %v", r, e)
	}
	for i := 0; i < 5; i++ {
		if e = runtime.Drain(ctx, s, scope, registry, 10); e != nil {
			t.Fatal(e)
		}
	}
	got, e := service.Get(ctx, s, scope, auth, id)
	if e != nil {
		t.Fatal(e)
	}
	if engine.sent != 1 || got.Decision.PhysicalRequestCount != 1 || !got.Decision.SendStarted || got.Decision.Status != "provider_result_unknown" || got.Decision.UsageFinal {
		t.Fatalf("lost-result facts sent=%d view=%+v", engine.sent, got)
	}
	duplicate, e := dispatch.Command(ctx, auth, api.Raw(command))
	if e != nil || duplicate.Stage != "accepted" {
		t.Fatalf("original accepted lookup: %+v %v", duplicate, e)
	}
}

type contents struct{ items map[string][]byte }

func (c *contents) add(s runtime.Scope, id string, b []byte) api.ContentRef {
	ref := api.ContentRef{TenantID: s.TenantID, OwnerID: s.OwnerID, ContentID: api.NewID(id), Version: 1, Hash: api.Hash(b), MediaType: "application/json", ByteLength: uint64(len(b))}
	c.items[ref.ContentID] = b
	return ref
}
func (c *contents) Read(_ context.Context, _ runtime.Scope, _ runtime.Auth, r api.ContentRef, _ string) ([]byte, error) {
	b, ok := c.items[r.ContentID]
	if !ok {
		return nil, runtime.ErrNotFound
	}
	return b, nil
}
func (c *contents) Publish(_ context.Context, s runtime.Scope, _ runtime.Auth, p brain.Publication, b []byte) (api.ContentRef, error) {
	r := api.ContentRef{TenantID: s.TenantID, OwnerID: s.OwnerID, ContentID: p.ContentID, Version: 1, Hash: api.Hash(b), MediaType: p.MediaType, ByteLength: uint64(len(b))}
	c.items[r.ContentID] = b
	return r, nil
}

type gate struct{}

func (gate) CheckTx(context.Context, runtime.Tx, runtime.Auth, brain.DecideInput, *brain.Encoding) error {
	return nil
}

type unknownEngine struct{ sent int }

func (*unknownEngine) Physical() bool { return true }
func (*unknownEngine) Encode(_ context.Context, s api.Snapshot, _ []byte, p brain.Profile) (brain.Encoding, error) {
	return brain.Encoding{Body: api.Raw(s), Digest: api.Hash(api.Raw(s)), Receiver: "test-provider", Location: "cloud", InputTokens: 100, CountMode: "upper_bound", ProcessedSources: s.ProcessedSources}, nil
}
func (e *unknownEngine) Request(context.Context, string, brain.Encoding) (brain.Generated, error) {
	e.sent++
	return brain.Generated{}, api.E("effect_unknown", "provider_result_unknown")
}
func (*unknownEngine) Lookup(context.Context, string, brain.Encoding) (brain.Generated, error) {
	return brain.Generated{}, api.E("effect_unknown", "provider_result_unknown")
}
