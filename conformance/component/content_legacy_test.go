//go:build integration

package component_test

import (
	"context"
	"fmt"
	fixture "github.com/ruipengliu/lerna/conformance/internal/contentfixture"
	v "github.com/ruipengliu/lerna/contract/v1_2"
	"github.com/ruipengliu/lerna/domain/content"
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
