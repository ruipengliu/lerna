package memory_test

import (
	"context"
	"errors"
	"io"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/adapters/sqlite"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/memory"
	"github.com/ruipengliu/lerna/runtime"
)

type writeProbe struct {
	memory.ObjectStore
	writes atomic.Int32
}

func (p *writeProbe) Write(ctx context.Context, ref api.ContentRef, body io.Reader) (memory.ObjectLocation, error) {
	p.writes.Add(1)
	return p.ObjectStore.Write(ctx, ref, body)
}

func TestReadyCommitUnknownRecoversOriginalTicketWithoutPublishingOrRewritingBytes(t *testing.T) {
	path := t.TempDir() + "/content.sqlite"
	var remaining atomic.Int32
	store, err := sqlite.Open(path, sqlite.WithCommitFault(func(phase sqlite.CommitPhase) error {
		if phase == sqlite.AfterCommit && remaining.Load() > 0 && remaining.Add(-1) == 0 {
			return errors.New("injected lost commit reply")
		}
		return nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err = store.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	f := newFixtureWithStore(t, store)
	probe := &writeProbe{ObjectStore: f.service.Objects}
	f.service.Objects = probe
	body := []byte("介质已耐久但ready回复丢失")
	ref := api.ContentRef{TenantID: f.scope.TenantID, OwnerID: f.scope.OwnerID, ContentID: api.NewID("content"), Version: 1, Hash: api.Hash(body), ByteLength: uint64(len(body)), MediaType: "text/plain"}
	transfer := api.NewID("upload")
	retention := api.Time(time.Now().Add(10 * time.Minute))
	r := f.command(t, "content.upload_reserve", ref.ContentID, nil, memory.ReserveInput{TransferID: transfer, ContentRef: ref, PolicyRef: f.policy.PolicyRef, ProcessedSources: []api.ContentRef{}, RetentionUntil: retention, TransferDeadline: api.Time(time.Now().Add(time.Minute))})
	if r.Stage != "applied" {
		t.Fatalf("reserve: %+v", r)
	}
	remaining.Store(3) // Lookup/writing 提交成功，ready 的实际 COMMIT 后丢回复。
	if _, err = f.service.ReceiveTransferBytes(f.ctx, f.scope, f.auth, transfer, body); !errors.Is(err, runtime.ErrCommitUnknown) {
		t.Fatalf("ready lost reply: %v", err)
	}
	if _, err = f.service.Read(f.ctx, f.scope, f.auth, ref, "task.goal"); !api.IsCode(err, "not_found") {
		t.Fatalf("unknown ready result published metadata: %v", err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := sqlite.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	f.service = memory.New(reopened, probe)
	f.scope.DatabaseID = reopened.ID()
	registry := runtime.NewRegistry()
	f.service.Register(registry)
	f.dispatcher = runtime.Dispatcher{Store: reopened, OwnerID: f.scope.OwnerID, Registry: registry}
	status, err := f.service.LookupTransfer(f.ctx, f.scope, f.auth, transfer)
	if err != nil || status.Phase != "ready" || status.Durability != "local_fsync" {
		t.Fatalf("original ready not recoverable: %+v %v", status, err)
	}
	if _, err = f.service.ReceiveTransferBytes(f.ctx, f.scope, f.auth, transfer, body); err != nil {
		t.Fatal(err)
	}
	if probe.writes.Load() != 1 {
		t.Fatalf("recovery rewrote original bytes: %d", probe.writes.Load())
	}
	r = f.command(t, "content.put", ref.ContentID, nil, memory.PutInput{ContentRef: ref, TransferID: transfer, PolicyRef: f.policy.PolicyRef, ProcessedSources: []api.ContentRef{}, DisclosedSources: []api.ContentRef{}, RetentionUntil: retention})
	if r.Stage != "applied" {
		t.Fatalf("publish original ticket: %+v", r)
	}
	got, err := f.service.Read(f.ctx, f.scope, f.auth, ref, "task.goal")
	if err != nil || string(got) != string(body) {
		t.Fatalf("accurate original body after restart: %q %v", got, err)
	}
}

type deleteReplyLost struct {
	memory.ObjectStore
	lost bool
}

func (s *deleteReplyLost) Delete(ctx context.Context, loc memory.ObjectLocation) error {
	if err := s.ObjectStore.Delete(ctx, loc); err != nil {
		return err
	}
	if !s.lost {
		s.lost = true
		return errors.New("injected reply loss after actual unlink and fsync")
	}
	return nil
}

func TestCleanupRecoversLostMediaDeleteReplyWithoutReopeningContent(t *testing.T) {
	f := newFixture(t)
	ref := f.upload(t, "原介质等待清理")
	f.service.Objects = &deleteReplyLost{ObjectStore: f.service.Objects}
	one := uint64(1)
	r := f.command(t, "content.close", ref.ContentID, &one, memory.CloseInput{ContentRef: ref, Reason: "主动关闭"})
	if r.Stage != "applied" {
		t.Fatalf("close: %+v", r)
	}
	works, status, err := f.service.Store.Claim(f.ctx, f.scope, api.NewID("boot"), []string{"content.cleanup"}, 1, 30*time.Second)
	if err != nil || status != runtime.Committed || len(works) != 1 {
		t.Fatalf("claim: %v %v %+v", err, status, works)
	}
	h, _ := f.dispatcher.Registry.Job("content.cleanup")
	if err = h(f.ctx, f.service.Store, f.scope, works[0]); err == nil {
		t.Fatal("lost media result must leave cleanup recoverable")
	}
	if _, err = f.service.Read(f.ctx, f.scope, f.auth, ref, "task.goal"); !api.IsCode(err, "forbidden") {
		t.Fatalf("media failure reopened source gate: %v", err)
	}
	if err = h(f.ctx, f.service.Store, f.scope, works[0]); err != nil {
		t.Fatalf("retry original cleanup responsibility: %v", err)
	}
	if _, err = f.service.Objects.Locate(f.ctx, ref); !api.IsCode(err, "gone") {
		t.Fatalf("original source bytes remained after cleanup: %v", err)
	}
	two := uint64(2)
	r = f.command(t, "content.close", ref.ContentID, &two, memory.CloseInput{ContentRef: ref, Reason: "重试已关闭内容"})
	if r.Stage != "applied" {
		t.Fatalf("physical deletion changed source control CAS identity: %+v", r)
	}
}
