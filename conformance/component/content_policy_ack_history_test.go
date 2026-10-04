//go:build integration

package component_test

import (
	"context"
	fixture "github.com/ruipengliu/lerna/conformance/internal/contentfixture"
	"github.com/ruipengliu/lerna/domain/content"
	"reflect"
	"testing"
	"time"
)

func TestContentNaturalPolicyPhaseCannotUndoOriginalErasureAcknowledgement(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	w := fixture.New(t, ctx)
	installContentPolicy(t, ctx, w, alphaRef)
	service := contentService(t, w)
	if _, ok := putContentRequest(t, ctx, service, contentPut(t, alphaRef, "natural-ack-original", "YWxwaGEK")).AsAccepted(); !ok {
		t.Fatal("normal original admission refused")
	}
	if _, err := service.Step(ctx); err != nil {
		t.Fatal(err)
	}
	assertContentBody(t, ctx, service, alphaRef, nil, "alpha\n")
	m := trustedContentManager(t, w, 2)
	due := time.Now().UTC().Add(2 * time.Second).Truncate(time.Microsecond)
	wide := time.Now().Add(time.Hour)
	policy := content.FixturePolicy{Ref: alphaRef, Subject: contentPrincipal, Purpose: "verification", Revision: 2, ValidUntil: due, RetainUntil: wide, Read: true, Process: true, Save: false, Sync: true, Disclose: true}
	change, err := m.InstallPolicy(ctx, &contentPrincipal, policy, 1)
	if err != nil {
		t.Fatal(err)
	}
	if worked, err := service.Step(ctx); err != nil || !worked {
		t.Fatal("real policy propagation did not run", worked, err)
	}
	before, err := m.ObserveChange(ctx, &contentPrincipal, change.Key, "", 2)
	if err != nil || len(before.Responsibilities) != 1 || before.Change.Phase != "natural_expiry" || !before.Change.Due.Equal(due) || !before.Responsibilities[0].ObjectHolder {
		t.Fatal("original real natural phase/holder history missing", before, err)
	}
	original := before.Responsibilities[0]
	l := bodyLifecycle(t, w)
	if _, err = l.ConsumePolicyCleanup(ctx, &contentPrincipal, change.Key, ""); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 4; i++ {
		worked, err := l.Step(ctx, &contentPrincipal)
		if err != nil {
			t.Fatal(err)
		}
		if !worked {
			break
		}
	}
	ack, err := m.ObserveChange(ctx, &contentPrincipal, change.Key, "", 2)
	if err != nil || len(ack.Responsibilities) != 1 || ack.Responsibilities[0].BodyCleanup != "erased" || !ack.Responsibilities[0].ObjectHolder {
		t.Fatal("normal original physical ACK/history missing", ack, err)
	}
	w.Reopen(ctx)
	// Wait for the original installed policy's real absolute expiry. No SQL
	// deadline rewrite, clock substitution or extra execution budget is used.
	waitUntil(t, ctx, due.Add(20*time.Millisecond))
	m = trustedContentManager(t, w, 2)
	if worked, err := m.Step(ctx, &contentPrincipal); err != nil || !worked {
		t.Fatal("actual original natural phase did not advance", worked, err)
	}
	after, err := m.ObserveChange(ctx, &contentPrincipal, change.Key, "", 2)
	if err != nil || len(after.Responsibilities) != 1 {
		t.Fatal("natural phase lost original responsibility", after, err)
	}
	actual := after.Responsibilities[0]
	if actual.BodyCleanup != "erased" || actual.Residual != "" || actual.ChangeKey != original.ChangeKey || actual.Ref != original.Ref || !reflect.DeepEqual(actual.Subject, original.Subject) || actual.Purpose != original.Purpose || !actual.Deadline.Equal(original.Deadline) || actual.ObjectHolder != original.ObjectHolder || actual.StagingHolder != original.StagingHolder || actual.AttemptKey != original.AttemptKey || actual.Publication != original.Publication || actual.Reason != original.Reason {
		t.Fatal("ordinary natural-phase registration undid original physical ACK/history", actual, original)
	}
	if !reflect.DeepEqual(actual.Actions, []string{"read", "process", "save", "sync", "disclose"}) || after.Change.Watermark != before.Change.Watermark || !after.Change.Due.Equal(due) || !after.Change.ExpiryDeadline.Equal(before.Change.ExpiryDeadline) {
		t.Fatal("natural phase discarded action union or refreshed original schedule", after)
	}
	observed, err := bodyLifecycle(t, w).Observe(ctx, &contentPrincipal, alphaRef, "")
	if err != nil || !observed.CleanupComplete || !observed.Seal.Deadline.Equal(original.Deadline) {
		t.Fatal("natural phase changed original actual holder ACK", observed, err)
	}
	w.Reopen(ctx)
	after, err = trustedContentManager(t, w, 2).ObserveChange(ctx, &contentPrincipal, change.Key, "", 2)
	if err != nil || len(after.Responsibilities) != 1 || after.Responsibilities[0].BodyCleanup != "erased" || !after.Responsibilities[0].ObjectHolder || !after.Responsibilities[0].Deadline.Equal(original.Deadline) {
		t.Fatal("reopen lost original ACK/history", after, err)
	}
}
