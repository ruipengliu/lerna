package memory_test

import (
	"testing"
	"time"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/memory"
	"github.com/ruipengliu/lerna/runtime"
)

func (f fixture) uploadUntil(t *testing.T, body string, retain, deadline time.Time, sources ...api.ContentRef) api.ContentRef {
	t.Helper()
	ref := api.ContentRef{TenantID: f.scope.TenantID, OwnerID: f.scope.OwnerID, ContentID: api.NewID("content"), Version: 1, Hash: api.Hash([]byte(body)), ByteLength: uint64(len([]byte(body))), MediaType: "text/plain"}
	_, err := f.service.Upload(f.ctx, f.scope, f.auth, memory.PublicationRequest{ContentRef: ref, TransferID: api.NewID("upload"), ReserveCommandID: api.NewID("command"), PutCommandID: api.NewID("command"), PolicyRef: f.policy.PolicyRef, ProcessedSources: append([]api.ContentRef{}, sources...), DisclosedSources: []api.ContentRef{}, RetentionUntil: api.Time(retain), TransferDeadline: api.Time(deadline)}, []byte(body))
	if err != nil {
		t.Fatal(err)
	}
	return ref
}

func TestIndependentDerivedRetentionSurvivesExpiryButExplicitCloseStillRevokes(t *testing.T) {
	f := newFixture(t)
	values := f.policy.Values
	values.IndependentDerived = true
	digest, _ := api.Digest(values)
	policy, err := memory.NewPolicy(api.ComponentRef{ComponentID: api.NewID("policy"), Version: "1", Digest: digest}, values)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.service.InstallPolicy(f.ctx, f.scope, f.auth, policy); err != nil {
		t.Fatal(err)
	}
	f.policy = policy
	expires := time.Now().Add(5 * time.Minute)
	source := f.uploadUntil(t, "可按独立许可保存派生的输入", expires, expires)
	derived := f.uploadUntil(t, "保留十分钟的独立派生", time.Now().Add(10*time.Minute), time.Now().Add(time.Minute), source)
	// 只替换已批准的可信时间端口，并显式投递到期唤醒；数据库和介质仍真实执行。
	store := f.service.Store
	now := expires.Add(time.Second)
	f.service.Store = clockStore{Store: store, now: &now}
	wake := func(kind string) {
		t.Helper()
		status, err := store.Within(f.ctx, f.scope, memory.Participants, func(tx runtime.Tx) error {
			due, err := tx.Now(f.ctx)
			if err != nil {
				return err
			}
			job, err := tx.Raise(f.ctx, kind, source.ContentID+":1", f.scope.Ref(source.ContentID, 1), due)
			if err != nil {
				return err
			}
			return tx.Hint(f.ctx, job.JobID, due)
		})
		if err != nil || status != runtime.Committed {
			t.Fatalf("timer fixture wake: %v %v", status, err)
		}
	}
	wake("content.expire")
	if err = runtime.Drain(f.ctx, f.service.Store, f.scope, f.dispatcher.Registry, 50); err != nil {
		t.Fatal(err)
	}
	wake("content.cleanup")
	if err = runtime.Drain(f.ctx, f.service.Store, f.scope, f.dispatcher.Registry, 50); err != nil {
		t.Fatal(err)
	}
	if _, err = f.service.Read(f.ctx, f.scope, f.auth, source, "task.goal"); !api.IsCode(err, "forbidden") {
		t.Fatalf("original source expiry left readable bytes: %v", err)
	}
	got, err := f.service.Read(f.ctx, f.scope, f.auth, derived, "task.goal")
	if err != nil || string(got) != "保留十分钟的独立派生" {
		t.Fatalf("independent permission lost on ordinary source expiry: %q %v", got, err)
	}
	two := uint64(2)
	r := f.command(t, "content.close", source.ContentID, &two, memory.CloseInput{ContentRef: source, Reason: "保存期到期后仍主动撤销来源"})
	if r.Stage != "applied" {
		t.Fatalf("active close after expiry: %+v", r)
	}
	if _, err = f.service.Read(f.ctx, f.scope, f.auth, derived, "task.goal"); !api.IsCode(err, "forbidden") {
		t.Fatalf("retention close suppressed later explicit revocation: %v", err)
	}
}
