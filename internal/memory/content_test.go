package memory_test

import (
	"context"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/adapters/objectstore"
	"github.com/ruipengliu/lerna/adapters/sqlite"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/memory"
	"github.com/ruipengliu/lerna/runtime"
)

type fixture struct {
	service    *memory.Service
	scope      runtime.Scope
	auth       runtime.Auth
	policy     memory.Policy
	dispatcher runtime.Dispatcher
	ctx        context.Context
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	ctx := context.Background()
	store, err := sqlite.Open(t.TempDir() + "/memory.sqlite")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err = store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	return newFixtureWithStore(t, store)
}

func newFixtureWithStore(t *testing.T, store runtime.Store) fixture {
	t.Helper()
	ctx := context.Background()
	objects, err := objectstore.OpenLocal(t.TempDir(), memory.MaxContentBytes)
	if err != nil {
		t.Fatal(err)
	}
	s := memory.New(store, objects)
	scope := runtime.Scope{TenantID: api.NewID("tenant"), OwnerID: api.NewID("owner"), DatabaseID: store.ID()}
	auth := runtime.Auth{TenantID: scope.TenantID, SubjectID: api.NewID("subject"), CredentialGeneration: 1, Roles: []string{"content_admin", "memory_admin"}}
	values := memory.PolicyValues{Subjects: []string{auth.SubjectID}, Purposes: []string{"content.write", "task.goal", "memory.save", "memory.read", "memory.query", "memory.extract", "memory.sync"}, Locations: []string{"local"}, RetainUntil: api.Time(time.Now().Add(time.Hour)), Continuous: true}
	digest, err := api.Digest(values)
	if err != nil {
		t.Fatal(err)
	}
	policy, err := memory.NewPolicy(api.ComponentRef{ComponentID: api.NewID("policy"), Version: "1", Digest: digest}, values)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.InstallPolicy(ctx, scope, auth, policy); err != nil {
		t.Fatal(err)
	}
	registry := runtime.NewRegistry()
	s.Register(registry)
	return fixture{s, scope, auth, policy, runtime.Dispatcher{Store: store, OwnerID: scope.OwnerID, Registry: registry}, ctx}
}

func (f fixture) upload(t *testing.T, body string, sources ...api.ContentRef) api.ContentRef {
	t.Helper()
	ref := api.ContentRef{TenantID: f.scope.TenantID, OwnerID: f.scope.OwnerID, ContentID: api.NewID("content"), Version: 1, Hash: api.Hash([]byte(body)), ByteLength: uint64(len([]byte(body))), MediaType: "text/plain"}
	policyDeadline, _ := api.ParseTime(f.policy.Values.RetainUntil)
	_, err := f.service.Upload(f.ctx, f.scope, f.auth, memory.PublicationRequest{ContentRef: ref, TransferID: api.NewID("upload"), ReserveCommandID: api.NewID("command"), PutCommandID: api.NewID("command"), PolicyRef: f.policy.PolicyRef, ProcessedSources: append([]api.ContentRef{}, sources...), DisclosedSources: []api.ContentRef{}, RetentionUntil: api.Time(policyDeadline.Add(-20 * time.Minute)), TransferDeadline: api.Time(time.Now().Add(10 * time.Minute))}, []byte(body))
	if err != nil {
		t.Fatal(err)
	}
	return ref
}

func (f fixture) command(t *testing.T, name, target string, expected *uint64, in any) api.Receipt {
	t.Helper()
	c := api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: f.scope.OwnerID, CommandID: api.NewID("command"), Method: name, TargetID: target, ExpectedRevision: expected, ExpiresAt: api.Time(time.Now().Add(time.Minute)), Payload: api.Raw(in)}
	r, err := f.dispatcher.Command(f.ctx, f.auth, api.Raw(c))
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestPublishedContentHasAccurateBytesAndSourceCloseBlocksUndisclosedInput(t *testing.T) {
	f := newFixture(t)
	source := f.upload(t, "敏感输入")
	derived := f.upload(t, "未引用输入的摘要", source)
	body, err := f.service.Read(f.ctx, f.scope, f.auth, derived, "task.goal")
	if err != nil || string(body) != "未引用输入的摘要" {
		t.Fatalf("derived read: %q %v", body, err)
	}
	one := uint64(1)
	r := f.command(t, "content.close", source.ContentID, &one, memory.CloseInput{ContentRef: source, Reason: "用户主动关闭"})
	if r.Stage != "applied" {
		t.Fatalf("close: %+v", r)
	}
	if _, err = f.service.Read(f.ctx, f.scope, f.auth, derived, "task.goal"); !api.IsCode(err, "forbidden") {
		t.Fatalf("undisclosed processed source remained readable: %v", err)
	}
}
