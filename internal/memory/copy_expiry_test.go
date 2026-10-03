package memory_test

import (
	"testing"
	"time"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/memory"
)

func TestCopyDeadlineStopsUseWithoutClosingOrErasingTheOriginal(t *testing.T) {
	f := newFixture(t)
	ref := f.upload(t, "原文保留期独立于单个副本期限")
	copyID := api.NewID("copy")
	r := f.command(t, "content.register_copy", ref.ContentID, nil, memory.RegisterCopyInput{CopyID: copyID, ContentRef: ref, HolderRef: f.auth.Ref(f.scope.OwnerID), Purpose: "memory.read", Location: "local", RetainUntil: api.Time(time.Now().Add(time.Minute)), ReferenceIntentRef: f.scope.Ref(api.NewID("intent"), 1)})
	if r.Stage != "applied" {
		t.Fatalf("register: %+v", r)
	}
	now := time.Now().Add(2 * time.Minute)
	f.service.Store = clockStore{Store: f.service.Store, now: &now}
	q := api.Query{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: f.scope.OwnerID, QueryID: api.NewID("query"), Method: "content.get", TargetID: ref.ContentID, Payload: api.Raw(memory.GetContentInput{ContentRef: ref, Mode: "control", CopyID: copyID})}
	raw, err := f.dispatcher.Query(f.ctx, f.auth, api.Raw(q))
	if err != nil {
		t.Fatal(err)
	}
	var out memory.GetContentOutput
	if err = api.Decode(raw, &out); err != nil {
		t.Fatal(err)
	}
	if out.UseState != "closing" || out.CleanupState != "pending" {
		t.Fatalf("copy expiry falsely allowed use or completed physical cleanup: %+v", out)
	}
	if body, err := f.service.Read(f.ctx, f.scope, f.auth, ref, "memory.read"); err != nil || string(body) != "原文保留期独立于单个副本期限" {
		t.Fatalf("copy expiry closed or erased original: %q %v", body, err)
	}
}
