//go:build integration

package component_test

import (
	"context"
	"errors"
	fixture "github.com/ruipengliu/lerna/conformance/internal/contentfixture"
	v "github.com/ruipengliu/lerna/contract/v1_2"
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

func TestContentCurrentSaveWithdrawalStartsOriginalSealAndRealCleanupConsumer(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	w := fixture.New(t, ctx)
	installContentPolicy(t, ctx, w, alphaRef)
	service := contentService(t, w)
	request := contentPut(t, alphaRef, "policy-seal-original", "YWxwaGEK")
	receipt := putContentRequest(t, ctx, service, request)
	if _, ok := receipt.AsAccepted(); !ok {
		t.Fatal("normal original admission refused")
	}
	fixed, err := v.Encode(receipt)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.Step(ctx); err != nil {
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
	// The ordinary Content worker really consumes policy_propagation; it does
	// not treat save-only pending as a body seal or physical erasure ACK.
	if worked, err := service.Step(ctx); err != nil || !worked {
		t.Fatal("Content policy consumer did not advance original work", worked, err)
	}
	before, err := m.ObserveChange(ctx, &contentPrincipal, change.Key, "", 2)
	if err != nil || len(before.Responsibilities) != 1 || before.Responsibilities[0].BodyCleanup != "pending" {
		t.Fatal("real original policy responsibility missing", before, err)
	}
	original := before.Responsibilities[0]
	assertContentBody(t, ctx, service, alphaRef, nil, "alpha\n")
	w.Reopen(ctx)
	l := bodyLifecycle(t, w)
	if _, err = l.ConsumePolicyCleanup(ctx, &contentPrincipal, change.Key, ""); err != nil {
		t.Fatal(err)
	}
	sealed, err := l.Observe(ctx, &contentPrincipal, alphaRef, "")
	if err != nil || sealed.Seal.Ref != alphaRef || sealed.Seal.ID == "" || !sealed.Seal.Deadline.Equal(original.Deadline) || sealed.CleanupComplete {
		t.Fatalf("current original save withdrawal did not commit its bounded seal: %+v %v", sealed, err)
	}
	service = contentService(t, w)
	view, err := service.Get(ctx, contentGetWire(t, alphaRef, nil), &contentPrincipal)
	denied, ok := view.AsRejected()
	if err != nil || !ok || denied.Reason != "forbidden" {
		t.Fatal("actual seal did not close body usage", view, err)
	}
	_, key, err := content.VersionIdentity(alphaRef)
	if err != nil {
		t.Fatal(err)
	}
	bytes, err := os.ReadFile(filepath.Join(w.Directory, key))
	if err != nil || string(bytes) != "alpha\n" {
		t.Fatal("seal falsely claimed actual erasure", err)
	}
	if worked, err := service.Step(ctx); err != nil || worked {
		t.Fatal("ordinary Content consumer claimed Lifecycle body cleanup", worked, err)
	}
	w.Reopen(ctx)
	l = bodyLifecycle(t, w)
	for i := 0; i < 4; i++ {
		worked, err := l.Step(ctx, &contentPrincipal)
		if err != nil {
			t.Fatal(err)
		}
		if !worked {
			break
		}
	}
	observed, err := l.Observe(ctx, &contentPrincipal, alphaRef, "")
	if err != nil || !observed.CleanupComplete || observed.Seal.ID != sealed.Seal.ID || !observed.Seal.Deadline.Equal(original.Deadline) {
		t.Fatal("real Lifecycle consumer lost original cleanup duty", observed, err)
	}
	staging, err := l.ObserveStaging(ctx, &contentPrincipal, alphaRef)
	if err != nil || staging.Present {
		t.Fatal("independent staging observer still sees body", staging, err)
	}
	if _, err = os.ReadFile(filepath.Join(w.Directory, key)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("independent original primary body remains", err)
	}
	metadata := content.MetadataPolicy{Ref: alphaRef, Subject: contentPrincipal, Purpose: "verification", Revision: 1, ValidUntil: time.Now().Add(time.Hour).UTC().Truncate(time.Microsecond)}
	if err = l.InstallMetadataPolicy(ctx, &contentPrincipal, metadata, 0); err != nil {
		t.Fatal(err)
	}
	view, err = contentService(t, w).Get(ctx, contentGetWire(t, alphaRef, nil), &contentPrincipal)
	gone, ok := view.AsGone()
	if err != nil || !ok || gone.ContentRef != alphaRef || gone.EvidenceAvailable {
		t.Fatal("authorized metadata does not reflect actual erasure", view, err)
	}
	w.Reopen(ctx)
	service = contentService(t, w)
	replayed, err := v.Encode(putContentRequest(t, ctx, service, request))
	if err != nil || string(replayed) != string(fixed) {
		t.Fatal("policy cleanup changed or revived original receipt", err)
	}
	command, err := service.GetCommand(ctx, contentCommandGetWire(t, request.CommandID), &contentPrincipal)
	found, ok := command.AsFound()
	progress, hasProgress := found.Progress.AsContent()
	if err != nil || !ok || !hasProgress || progress.Publication != "published" || progress.ContentRef != alphaRef {
		t.Fatal("policy cleanup erased published history", command, err)
	}
	observed, err = bodyLifecycle(t, w).Observe(ctx, &contentPrincipal, alphaRef, "")
	if err != nil || !observed.CleanupComplete || observed.Seal.ID != sealed.Seal.ID || !observed.Seal.Deadline.Equal(original.Deadline) {
		t.Fatal("reopen reset original policy cleanup", observed, err)
	}
	after, err := trustedContentManager(t, w, 2).ObserveChange(ctx, &contentPrincipal, change.Key, "", 2)
	if err != nil || len(after.Responsibilities) != 1 {
		t.Fatal("original policy consumer lost final responsibility", after, err)
	}
	ack := after.Responsibilities[0]
	if ack.BodyCleanup != "erased" || ack.Residual != "" || ack.Ref != original.Ref || !ack.Deadline.Equal(original.Deadline) || !reflect.DeepEqual(ack.Actions, original.Actions) || !reflect.DeepEqual(ack.Subject, original.Subject) || ack.Purpose != original.Purpose || ack.Reason != original.Reason || ack.ObjectHolder != original.ObjectHolder || ack.StagingHolder != original.StagingHolder || ack.AttemptKey != original.AttemptKey || ack.Publication != original.Publication {
		t.Fatal("actual all-holder ACK failed to qualify original policy duty/history", ack, original)
	}
}
