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
	"sync"
	"testing"
	"time"
)

func TestContentAuthoritativeErasureStopsNewPrimaryHolderClaimsAndKeepsOldHistory(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	w := fixture.New(t, ctx)
	installContentPolicy(t, ctx, w, alphaRef)
	service := contentService(t, w)
	request := contentPut(t, alphaRef, "holder-fact-original", "YWxwaGEK")
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
	old, err := m.ObserveChange(ctx, &contentPrincipal, change.Key, "", 2)
	if err != nil || len(old.Responsibilities) != 1 || !old.Responsibilities[0].ObjectHolder {
		t.Fatal("normal original published holder history missing", old, err)
	}
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
	observed, err := l.Observe(ctx, &contentPrincipal, alphaRef, "")
	if err != nil || !observed.CleanupComplete {
		t.Fatal("actual holder cleanup incomplete", observed, err)
	}
	_, key, err := content.VersionIdentity(alphaRef)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = os.ReadFile(filepath.Join(w.Directory, key)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("independent primary body still present", err)
	}
	staging, err := l.ObserveStaging(ctx, &contentPrincipal, alphaRef)
	if err != nil || staging.Present {
		t.Fatal("independent staging body still present", staging, err)
	}
	metadata := content.MetadataPolicy{Ref: alphaRef, Subject: contentPrincipal, Purpose: "verification", Revision: 1, ValidUntil: time.Now().Add(time.Hour).UTC().Truncate(time.Microsecond)}
	if err = l.InstallMetadataPolicy(ctx, &contentPrincipal, metadata, 0); err != nil {
		t.Fatal(err)
	}
	view, err := service.Get(ctx, contentGetWire(t, alphaRef, nil), &contentPrincipal)
	gone, ok := view.AsGone()
	if err != nil || !ok || gone.ContentRef != alphaRef || gone.EvidenceAvailable {
		t.Fatal("authorized gone did not match real body absence", view, err)
	}
	w.Reopen(ctx)
	m = trustedContentManager(t, w, 2)
	policy.Revision = 3
	policy.Save = true
	current, err := m.InstallPolicy(ctx, &contentPrincipal, policy, 2)
	if err != nil {
		t.Fatal(err)
	}
	newFact, err := m.ObserveChange(ctx, &contentPrincipal, current.Key, "", 2)
	if err != nil || len(newFact.Responsibilities) != 1 || newFact.Responsibilities[0].ObjectHolder {
		t.Fatal("new policy responsibility claimed an erased primary still holds body", newFact, err)
	}
	old, err = m.ObserveChange(ctx, &contentPrincipal, change.Key, "", 2)
	if err != nil || len(old.Responsibilities) != 1 || !old.Responsibilities[0].ObjectHolder || old.Responsibilities[0].BodyCleanup != "erased" {
		t.Fatal("current holder correction rewrote old physical history", old, err)
	}
	command, err := contentService(t, w).GetCommand(ctx, contentCommandGetWire(t, request.CommandID), &contentPrincipal)
	found, ok := command.AsFound()
	progress, hasProgress := found.Progress.AsContent()
	if err != nil || !ok || !hasProgress || progress.Publication != "published" || progress.ContentRef != alphaRef {
		t.Fatal("holder correction replaced published history", command, err)
	}
	view, err = contentService(t, w).Get(ctx, contentGetWire(t, alphaRef, nil), &contentPrincipal)
	gone, ok = view.AsGone()
	if err != nil || !ok || gone.EvidenceAvailable {
		t.Fatal("save restoration revived erased body", view, err)
	}
}

func TestContentLateSuccessfulPutCannotRestoreErasedPrimaryHolderFact(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	w := fixture.New(t, ctx)
	installContentPolicy(t, ctx, w, alphaRef)
	request := contentPut(t, alphaRef, "late-holder-fact-original", "YWxwaGEK")
	receipt := putContentRequest(t, ctx, contentService(t, w), request)
	if _, ok := receipt.AsAccepted(); !ok {
		t.Fatal("normal original admission refused")
	}
	fixed, err := v.Encode(receipt)
	if err != nil {
		t.Fatal(err)
	}
	objects := &heldContentWrite{Objects: w.Objects, entered: make(chan struct{}), release: make(chan struct{})}
	publisher := withPublication(t, w, w.Store(), objects, time.Minute, 5*time.Second)
	var once sync.Once
	release := func() error { once.Do(func() { close(objects.release) }); return nil }
	done := make(chan error, 1)
	finished := make(chan struct{})
	go func() { defer close(finished); _, err := publisher.Step(ctx); done <- err }()
	joinContentRead(t, w, release, finished)
	select {
	case <-objects.entered:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	_, key, err := content.VersionIdentity(alphaRef)
	if err != nil {
		t.Fatal(err)
	}
	bytes, err := os.ReadFile(filepath.Join(w.Directory, key))
	if err != nil || string(bytes) != "alpha\n" {
		t.Fatal("original successful native Put not independently observed", err)
	}
	l := bodyLifecycle(t, w)
	seal := content.SealRequest{Ref: alphaRef, Purpose: "verification", SealID: "late-holder-fact-seal", Deadline: time.Now().Add(time.Minute).UTC().Truncate(time.Microsecond)}
	if _, err = l.SealOrphan(ctx, &contentPrincipal, seal); err != nil {
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
	observed, err := l.Observe(ctx, &contentPrincipal, alphaRef, "")
	if err != nil || !observed.CleanupComplete {
		t.Fatal("actual pre-Finish cleanup incomplete", observed, err)
	}
	staging, err := l.ObserveStaging(ctx, &contentPrincipal, alphaRef)
	if err != nil || staging.Present {
		t.Fatal("pre-Finish staging still holds bytes", staging, err)
	}
	if _, err = os.ReadFile(filepath.Join(w.Directory, key)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("pre-Finish real primary body remains", err)
	}
	if err = release(); err != nil {
		t.Fatal(err)
	}
	select {
	case err = <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	// Explicitly join the actual worker; a cancelled caller is not a join ACK.
	select {
	case <-finished:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	w.Reopen(ctx)
	wide := time.Now().Add(time.Hour)
	policy := content.FixturePolicy{Ref: alphaRef, Subject: contentPrincipal, Purpose: "verification", Revision: 2, ValidUntil: wide, RetainUntil: wide, Read: true, Process: true, Save: false, Sync: true, Disclose: true}
	m := trustedContentManager(t, w, 2)
	change, err := m.InstallPolicy(ctx, &contentPrincipal, policy, 1)
	if err != nil {
		t.Fatal(err)
	}
	actual, err := m.ObserveChange(ctx, &contentPrincipal, change.Key, "", 2)
	if err != nil || len(actual.Responsibilities) != 1 || actual.Responsibilities[0].ObjectHolder {
		t.Fatal("late successful I/O receipt resurrected the erased primary holder fact", actual, err)
	}
	replay, err := v.Encode(putContentRequest(t, ctx, contentService(t, w), request))
	if err != nil || string(replay) != string(fixed) {
		t.Fatal("late holder correction changed original receipt", err)
	}
	command, err := contentService(t, w).GetCommand(ctx, contentCommandGetWire(t, request.CommandID), &contentPrincipal)
	found, ok := command.AsFound()
	progress, hasProgress := found.Progress.AsContent()
	if err != nil || !ok || !hasProgress || progress.Publication != "failed" || progress.ContentRef != alphaRef {
		t.Fatal("late Finish escaped original seal", command, err)
	}
	observed, err = bodyLifecycle(t, w).Observe(ctx, &contentPrincipal, alphaRef, "")
	if err != nil || !observed.CleanupComplete || observed.Seal.ID != seal.SealID || !observed.Seal.Deadline.Equal(seal.Deadline) {
		t.Fatal("late Finish lost original physical cleanup history", observed, err)
	}
	if _, err = os.ReadFile(filepath.Join(w.Directory, key)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("late Finish rebuilt erased primary body", err)
	}
}
