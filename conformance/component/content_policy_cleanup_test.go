//go:build integration

package component_test

import (
	"context"
	"errors"
	fixture "github.com/ruipengliu/lerna/conformance/internal/contentfixture"
	"github.com/ruipengliu/lerna/domain/content"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestContentRestoredSaveBasisMakesOriginalCleanupNotRequiredWithoutSealing(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	w := fixture.New(t, ctx)
	installContentPolicy(t, ctx, w, alphaRef)
	service := contentService(t, w)
	request := contentPut(t, alphaRef, "policy-cleanup-original", "YWxwaGEK")
	if _, ok := putContentRequest(t, ctx, service, request).AsAccepted(); !ok {
		t.Fatal("normal original admission refused")
	}
	if _, err := service.Step(ctx); err != nil {
		t.Fatal(err)
	}
	assertContentBody(t, ctx, service, alphaRef, nil, "alpha\n")
	m := trustedContentManager(t, w, 2)
	wide := time.Now().Add(time.Hour)
	policy := content.FixturePolicy{Ref: alphaRef, Subject: contentPrincipal, Purpose: "verification", Revision: 2, ValidUntil: wide, RetainUntil: wide, Read: true, Process: true, Save: false, Sync: true, Disclose: true}
	change, err := m.InstallPolicy(ctx, &contentPrincipal, policy, 1)
	if err != nil {
		t.Fatal(err)
	}
	if worked, err := m.Step(ctx, &contentPrincipal); err != nil || !worked {
		t.Fatal("real original policy propagation did not run", worked, err)
	}
	before, err := m.ObserveChange(ctx, &contentPrincipal, change.Key, "", 2)
	if err != nil || len(before.Responsibilities) != 1 {
		t.Fatal("original policy responsibility unavailable", before, err)
	}
	original := before.Responsibilities[0]
	if original.BodyCleanup != "pending" || original.Ref != alphaRef || !original.ObjectHolder || original.Residual != "holder_unconfirmed" {
		t.Fatal("save withdrawal lost original physical responsibility", original)
	}
	assertContentBody(t, ctx, service, alphaRef, nil, "alpha\n")
	policy.Revision = 3
	policy.Save = true
	if _, err = m.InstallPolicy(ctx, &contentPrincipal, policy, 2); err != nil {
		t.Fatal(err)
	}
	if err = service.AuthorizeUse(ctx, &contentPrincipal, alphaRef, "verification", "save"); err != nil {
		t.Fatal("normal restored save basis refused", err)
	}
	w.Reopen(ctx)
	l := bodyLifecycle(t, w)
	consumed, err := l.ConsumePolicyCleanup(ctx, &contentPrincipal, change.Key, "")
	if err != nil || len(consumed.Responsibilities) != 1 || consumed.NextCursor != "" {
		t.Fatalf("restored original saving basis could not qualify old cleanup: %+v %v", consumed, err)
	}
	actual := consumed.Responsibilities[0]
	if actual.BodyCleanup != "not_required" || actual.ChangeKey != original.ChangeKey || actual.Ref != original.Ref || !reflect.DeepEqual(actual.Subject, original.Subject) || actual.Purpose != original.Purpose || !actual.Deadline.Equal(original.Deadline) || !reflect.DeepEqual(actual.Actions, original.Actions) || actual.Reason != original.Reason {
		t.Fatal("current qualification replaced original responsibility/history", actual, original)
	}
	if _, err = l.Observe(ctx, &contentPrincipal, alphaRef, ""); !errors.Is(err, content.ErrUnavailable) {
		t.Fatal("old transient withdrawal created irreversible seal", err)
	}
	if worked, err := l.Step(ctx, &contentPrincipal); err != nil || worked {
		t.Fatal("not-required policy cleanup created body erasure work", worked, err)
	}
	_, key, err := content.VersionIdentity(alphaRef)
	if err != nil {
		t.Fatal(err)
	}
	bytes, err := os.ReadFile(filepath.Join(w.Directory, key))
	if err != nil || string(bytes) != "alpha\n" {
		t.Fatal("old policy event erased restored original body", err)
	}
	assertContentBody(t, ctx, contentService(t, w), alphaRef, nil, "alpha\n")
	w.Reopen(ctx)
	after, err := trustedContentManager(t, w, 2).ObserveChange(ctx, &contentPrincipal, change.Key, "", 2)
	if err != nil || len(after.Responsibilities) != 1 || after.Responsibilities[0].BodyCleanup != "not_required" || !after.Responsibilities[0].Deadline.Equal(original.Deadline) || after.Change.Policy.Revision != 2 || after.Change.Policy.Save || after.Change.Watermark != before.Change.Watermark {
		t.Fatal("reopen lost qualified responsibility or original policy history", after, err)
	}
	assertContentBody(t, ctx, contentService(t, w), alphaRef, nil, "alpha\n")
}
