package extractioncheck

import (
	"context"
	"crypto/sha256"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"lerna/adapters/contentpolicy"
	"lerna/adapters/extractionauth"
	"lerna/adapters/localextractionsource"
	"lerna/answers"
	"lerna/artifacts"
	"lerna/extraction"
	wire "lerna/gen/harness/v1"
	"lerna/memory"
)

func TestTriggerPollingRecoversOriginalTaskAndKeepsRoundBudget(t *testing.T) {
	ctx := context.Background()
	h, err := fresh(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer h.destroy()
	enableScan(t, ctx, h)
	auth, err := extractionauth.New(h.auth, extractionauth.Scope{Namespace: "local", Resource: "root", Purpose: "task", Location: "local"})
	if err != nil {
		t.Fatal(err)
	}
	b := memory.Binding{Token: h.token, Namespace: "local", Subject: "operator", Location: "local", Recipient: "local"}
	registry, err := extraction.NewTriggerRegistry(h.candidates, auth, h.clock, b, "task")
	if err != nil {
		t.Fatal(err)
	}
	scope := extraction.SourceScope{Kind: "note", Key: "one"}
	spec := extraction.TriggerSpec{ID: "poll-source", Condition: "source.changed", Sources: []extraction.SourceScope{scope}, MaxRounds: 2, MaxSteps: 3, ExpiresUnix: h.now().Add(10 * time.Minute).Unix()}
	if err = registry.Register(ctx, spec); err != nil {
		t.Fatal(err)
	}
	entry := localextractionsource.Entry{Ref: &wire.ContentSource{Kind: "note", Key: "one", Revision: 1}, Path: filepath.Join(h.root, "source.txt"), SHA256: fmt.Sprintf("%x", sha256.Sum256([]byte("回答时，我偏好简洁的说明。"))), Speaker: "operator", Method: "authenticated-note", Fragment: "paragraph:1", Restrictions: extraction.Restrictions{Storage: []string{"local"}, Processing: []string{"local"}, Recipients: []string{"local"}, Purposes: []string{"task"}, RetainUntil: h.now().Add(time.Hour).Unix()}}
	source, err := localextractionsource.New([]localextractionsource.Entry{entry}, h.clock)
	if err != nil {
		t.Fatal(err)
	}
	newPoller := func() *extraction.TriggerPoller {
		d, e := extraction.NewTriggerDispatcher(h.candidates, auth, h.clock, b, h.core, triggerInputs{h}, h.operation)
		if e != nil {
			t.Fatal(e)
		}
		p, e := extraction.NewTriggerPoller(d, source)
		if e != nil {
			t.Fatal(e)
		}
		return p
	}
	poller := newPoller()
	first, err := poller.Poll(ctx, spec.ID, scope)
	if err != nil {
		t.Fatal(err)
	}
	// Reconstruct the host object; operation identity/budget live in SQLite.
	poller = newPoller()
	replay, err := poller.Poll(ctx, spec.ID, scope)
	if err != nil || replay.Ref != first.Ref || replay.Version != first.Version {
		t.Fatalf("poll replay: %+v %v", replay, err)
	}
	entry.Ref.Revision = 2
	if _, err = source.Change(ctx, h.candidates, extraction.SourceInvalidation{Namespace: "local", Kind: "note", Key: "one", ThroughRevision: 1}, &entry); err != nil {
		t.Fatal(err)
	}
	if err = h.policy.Replace([]contentpolicy.Rule{{Kind: "note", Key: "one", Revision: 2, Actions: []string{"store", "process", "retain", "discover", "disclose", "delete"}, Purposes: []string{"task"}, Locations: []string{"local"}, RetainUntil: h.now().Add(time.Hour).Unix()}}); err != nil {
		t.Fatal(err)
	}
	second, err := poller.Poll(ctx, spec.ID, scope)
	if err != nil || second.Ref == first.Ref {
		t.Fatalf("new revision did not create distinct work: %+v %v", second, err)
	}
	inputRef, err := answers.ParseReference(second.InputRefs[0])
	if err != nil {
		t.Fatal(err)
	}
	meta, err := h.content.Call(ctx, artifacts.Binding{Token: h.token, Namespace: "local", Location: "local", Recipient: "local"}, &wire.ContentRequest{Method: "GET", Ref: inputRef, Purpose: "task"})
	if err != nil || len(meta.GetRecord().GetSpec().GetSources()) != 1 || meta.Record.Spec.Sources[0].Revision != 2 || meta.Record.Spec.RetainUntil > second.Constraints.DeadlineUnix {
		t.Fatalf("input metadata lost actual event/deadline: %v %v", meta, err)
	}
	entry.Ref.Revision = 3
	if _, err = source.Change(ctx, h.candidates, extraction.SourceInvalidation{Namespace: "local", Kind: "note", Key: "one", ThroughRevision: 2}, &entry); err != nil {
		t.Fatal(err)
	}
	if err = h.policy.Replace([]contentpolicy.Rule{{Kind: "note", Key: "one", Revision: 3, Actions: []string{"store", "process", "retain", "discover", "disclose", "delete"}, Purposes: []string{"task"}, Locations: []string{"local"}, RetainUntil: h.now().Add(time.Hour).Unix()}}); err != nil {
		t.Fatal(err)
	}
	if _, err = newPoller().Poll(ctx, spec.ID, scope); err != memory.Capacity {
		t.Fatalf("poll reset durable budget: %v", err)
	}
	if _, err = h.candidates.GetTriggerRound(ctx, "local", "operator", spec.ID, entry.Ref); err != memory.Missing {
		t.Fatalf("exhausted round persisted: %v", err)
	}
	if err = h.candidates.CancelTrigger(ctx, "local", "operator", spec.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = poller.Poll(ctx, spec.ID, scope); err != memory.Denied {
		t.Fatalf("cancelled poll: %v", err)
	}
}

func TestPollingRechecksScanAfterMetadataRead(t *testing.T) {
	ctx := context.Background()
	h, err := fresh(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer h.destroy()
	enableScan(t, ctx, h)
	auth, err := extractionauth.New(h.auth, extractionauth.Scope{Namespace: "local", Resource: "root", Purpose: "task", Location: "local"})
	if err != nil {
		t.Fatal(err)
	}
	b := memory.Binding{Token: h.token, Namespace: "local", Subject: "operator", Location: "local", Recipient: "local"}
	registry, err := extraction.NewTriggerRegistry(h.candidates, auth, h.clock, b, "task")
	if err != nil {
		t.Fatal(err)
	}
	scope := extraction.SourceScope{Kind: "note", Key: "one"}
	if err = registry.Register(ctx, extraction.TriggerSpec{ID: "revoke-poll", Condition: "source.changed", Sources: []extraction.SourceScope{scope}, MaxRounds: 1, MaxSteps: 3, ExpiresUnix: h.now().Add(10 * time.Minute).Unix()}); err != nil {
		t.Fatal(err)
	}
	poller, err := extraction.NewTriggerPoller(recoveryDispatcher(t, h, h.core), revokeAfterMetadata{h.currentSources, h})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = poller.Poll(ctx, "revoke-poll", scope); err != memory.Denied {
		t.Fatalf("scan revoked during metadata read: %v", err)
	}
	assertOrdinaryExtractionAllowed(t, ctx, h)
	if _, err = h.candidates.GetTriggerRound(ctx, "local", "operator", "revoke-poll", &wire.ContentSource{Kind: "note", Key: "one", Revision: 1}); err != memory.Missing {
		t.Fatalf("revoked poll created task submission: %v", err)
	}
}

// Inject a real policy mutation after the actual manifest read, before dispatch.
type revokeAfterMetadata struct {
	extraction.CurrentSources
	h *harness
}

func (s revokeAfterMetadata) Current(ctx context.Context, scope extraction.SourceScope, location, purpose string) (*wire.ContentSource, error) {
	ref, err := s.CurrentSources.Current(ctx, scope, location, purpose)
	if err != nil {
		return nil, err
	}
	if err = revokeSaveAction(ctx, s.h, "memory.scan"); err != nil {
		return nil, err
	}
	return ref, nil
}
