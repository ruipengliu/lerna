//go:build integration

package component_test

import (
	"context"
	"fmt"
	fixture "github.com/ruipengliu/lerna/conformance/internal/contentfixture"
	v "github.com/ruipengliu/lerna/contract/v1_2"
	"github.com/ruipengliu/lerna/domain/content"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestContentLegacyBackfillRestoresOriginalExpiryAndFailedHolderResponsibilities(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	w, old := fixture.NewLegacy(t, ctx)
	manager := trustedContentManager(t, w, 1)
	if done, err := manager.RebuildSources(ctx, &contentPrincipal); err != nil || done {
		t.Fatal("finite first page", done, err)
	}
	w.Reopen(ctx)
	manager = trustedContentManager(t, w, 1)
	for i := 0; i < 4; i++ {
		done, err := manager.RebuildSources(ctx, &contentPrincipal)
		if err != nil {
			t.Fatal(err)
		}
		if done {
			break
		}
	}
	until := time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC)
	for i, request := range old.Requests {
		policy := content.FixturePolicy{Ref: request.Payload.ContentRef, Subject: contentPrincipal, Purpose: "verification", Revision: 1, ValidUntil: until, RetainUntil: until, Read: true, Process: true, Save: true, Disclose: true}
		if i == 2 {
			policy.Revision = 2
			policy.Save = false
		}
		observation, err := manager.ObservePolicyChange(ctx, &contentPrincipal, policy)
		if err != nil {
			t.Fatal("legacy original policy maintenance unregistered", err)
		}
		if !observation.Change.ExpiryDue.Equal(until) || !observation.Change.ExpiryDeadline.Equal(until.Add(time.Minute)) {
			t.Fatal("original absolute expiry refreshed", observation.Change)
		}
		if i == 2 && (len(observation.Responsibilities) != 1 || observation.Responsibilities[0].BodyCleanup != "pending" || !observation.Responsibilities[0].StagingHolder || observation.Responsibilities[0].Publication != "failed") {
			t.Fatal("failed legacy staging responsibility lost", observation)
		}
		if i > 0 {
			obligations, next, err := manager.ObserveAdmissionChanges(ctx, &contentPrincipal, request.Payload.ContentRef, "", 64)
			if err != nil || next != "" || len(obligations) != i {
				t.Fatal("legacy inherited obligations incomplete", err, obligations, next)
			}
			for _, obligation := range obligations {
				if obligation.Phase != "natural_expiry" || !obligation.Due.Equal(until) || !obligation.Deadline.Equal(until.Add(time.Minute)) {
					t.Fatal("legacy original inherited deadline/phase refreshed", obligation)
				}
			}
		}
		service := contentService(t, w)
		view, err := service.GetCommand(ctx, contentCommandGetWire(t, request.CommandID), &contentPrincipal)
		found, ok := view.AsFound()
		before, _ := v.Encode(old.Receipts[i])
		after, _ := v.Encode(found.Receipt)
		if err != nil || !ok || string(before) != string(after) {
			t.Fatal("legacy receipt mutated", err, view)
		}
		progress, ok := found.Progress.AsContent()
		if !ok || progress.Publication != old.States[i] {
			t.Fatal("legacy publication history replaced", view)
		}
		if i < 2 {
			assertContentBody(t, ctx, service, request.Payload.ContentRef, nil, "alpha\n")
		}
	}
}

func TestContentLegacyPolicyPagesDoNotTruncateAt64OrResetOnReopen(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	w, old := fixture.NewLegacyPolicies(t, ctx)
	manager := trustedContentManager(t, w, 2)
	for i := 0; i < 3; i++ {
		done, err := manager.RebuildSources(ctx, &contentPrincipal)
		if err != nil || done {
			t.Fatal("legacy policies prematurely marked ready", done, err)
		}
	}
	if worked, err := manager.Step(ctx, &contentPrincipal); err != nil || worked {
		t.Fatal("propagation ran before complete legacy index/policy watermark", worked, err)
	}
	w.Reopen(ctx)
	manager = trustedContentManager(t, w, 2)
	complete := false
	for i := 0; i < 40; i++ {
		done, err := manager.RebuildSources(ctx, &contentPrincipal)
		if err != nil {
			t.Fatal(err)
		}
		if done {
			complete = true
			break
		}
	}
	if !complete {
		t.Fatal("original finite policy cursor did not finish")
	}
	if worked, err := manager.Step(ctx, &contentPrincipal); err != nil || !worked {
		t.Fatal("complete legacy watermark did not resume original work", worked, err)
	}
	until := time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 65; i++ {
		subject := contentPrincipal
		subject.SubjectID = v.ID(fmt.Sprintf("legacy-guest-%02d", i))
		p := content.FixturePolicy{Ref: old.Requests[0].Payload.ContentRef, Subject: subject, Purpose: "verification", Revision: 1, ValidUntil: until, RetainUntil: until, Read: true, Process: true, Save: true, Sync: true, Disclose: true}
		observation, err := manager.ObservePolicyChange(ctx, &contentPrincipal, p)
		if err != nil || observation.Change.State != "scheduled" || !observation.Change.ExpiryDue.Equal(until) || !observation.Change.Deadline.Equal(until.Add(time.Minute)) {
			t.Fatal("policy beyond first page lost or deadline refreshed", i, err, observation)
		}
	}
	service := contentService(t, w)
	assertContentBody(t, ctx, service, old.Requests[0].Payload.ContentRef, nil, "alpha\n")
	guest := contentPrincipal
	guest.SubjectID = "legacy-guest-64"
	view, err := service.Get(ctx, contentGetWire(t, old.Requests[0].Payload.ContentRef, nil), &guest)
	published, ok := view.AsPublished()
	if err != nil || !ok || published.BytesBase64 != "YWxwaGEK" {
		t.Fatal("last real authorized policy not usable", err, view)
	}
}

func TestContentActualExpiredOldWriterArchiveRetainsAncestorResponsibilities(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	w, old := fixture.NewLegacyExpired(t, ctx)
	m := trustedContentManager(t, w, 2)
	if old.SourceValidUntil.IsZero() {
		t.Fatal("original producer finite cutoff missing")
	}
	complete := false
	for i := 0; i < 40; i++ {
		done, err := m.RebuildSources(ctx, &contentPrincipal)
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			w.Reopen(ctx)
			m = trustedContentManager(t, w, 2)
		}
		if done {
			complete = true
			break
		}
	}
	if !complete {
		t.Fatal("expired original archive backfill incomplete")
	}
	service := contentService(t, w)
	for i, req := range old.Requests {
		command, err := service.GetCommand(ctx, contentCommandGetWire(t, req.CommandID), &contentPrincipal)
		found, ok := command.AsFound()
		progress, published := found.Progress.AsContent()
		before, _ := v.Encode(old.Receipts[i])
		after, _ := v.Encode(found.Receipt)
		if err != nil || !ok || !published || string(before) != string(after) || progress.Publication != old.States[i] {
			t.Fatal("expired upgrade changed original receipt/history", err, command)
		}
		view, err := service.Get(ctx, contentGetWire(t, req.Payload.ContentRef, nil), &contentPrincipal)
		if _, ok := view.AsRejected(); err != nil || !ok {
			t.Fatal("expired original ancestor allowed body", err, view)
		}
		if i > 0 {
			obligations, next, err := m.ObserveAdmissionChanges(ctx, &contentPrincipal, req.Payload.ContentRef, "", 64)
			if err != nil || next != "" || len(obligations) != i {
				t.Fatal("expired legacy inherited obligations missing", err, obligations)
			}
			seen := false
			for _, obligation := range obligations {
				if obligation.Policy.Ref == old.Requests[0].Payload.ContentRef {
					seen = true
					if !obligation.Due.Equal(old.SourceValidUntil) || !obligation.Deadline.Equal(old.SourceValidUntil.Add(time.Minute)) || obligation.State != "residual" || obligation.Phase != "natural_expiry" {
						t.Fatal("expired original cutoff refreshed or claimed complete", obligation)
					}
					observation, e := m.ObserveChange(ctx, &contentPrincipal, obligation.Key, "", 64)
					if e != nil || len(observation.Responsibilities) != 1 || observation.Responsibilities[0].BodyCleanup != "pending" {
						t.Fatal("expired inherited holder lost", e, observation)
					}
					r := observation.Responsibilities[0]
					if i == 1 && !r.ObjectHolder || i == 2 && !r.StagingHolder {
						t.Fatal("original published/failed holder lost", r)
					}
				}
			}
			if !seen {
				t.Fatal("exact expired ancestor not registered")
			}
		}
	}
	entries, err := os.ReadDir(w.Directory)
	if err != nil || len(entries) != 2 {
		t.Fatal("original native objects changed", err)
	}
	for _, entry := range entries {
		bytes, e := os.ReadFile(filepath.Join(w.Directory, entry.Name()))
		if e != nil || string(bytes) != "alpha\n" {
			t.Fatal("independent original object bytes changed", e)
		}
	}
	// A separate legitimate current chain controls the expired-history refusal.
	source := alphaRef
	source.ContentID = "fresh-after-expired-upgrade-source"
	installContentPolicy(t, ctx, w, source)
	if _, ok := putContentRequest(t, ctx, service, contentPut(t, source, "fresh-after-upgrade-source", "YWxwaGEK")).AsAccepted(); !ok {
		t.Fatal("new normal source refused")
	}
	if _, err = service.Step(ctx); err != nil {
		t.Fatal(err)
	}
	target := alphaRef
	target.ContentID = "fresh-after-expired-upgrade-derived"
	installContentPolicy(t, ctx, w, target)
	req := contentPut(t, target, "fresh-after-upgrade-derived", "YWxwaGEK")
	req.Payload.Sources = []v.ContentRef{source}
	if _, ok := putContentRequest(t, ctx, service, req).AsAccepted(); !ok {
		t.Fatal("new normal derived refused")
	}
	if _, err = service.Step(ctx); err != nil {
		t.Fatal(err)
	}
	assertContentBody(t, ctx, service, target, nil, "alpha\n")
	w.Reopen(ctx)
	m = trustedContentManager(t, w, 2)
	again, _, err := m.ObserveAdmissionChanges(ctx, &contentPrincipal, old.Requests[1].Payload.ContentRef, "", 64)
	if err != nil || len(again) != 1 || !again[0].Due.Equal(old.SourceValidUntil) || !again[0].Deadline.Equal(old.SourceValidUntil.Add(time.Minute)) {
		t.Fatal("expired legacy reopen reset maintenance deadline", err, again)
	}
}
