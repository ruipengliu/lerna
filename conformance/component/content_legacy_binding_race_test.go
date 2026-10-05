//go:build integration

package component_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	fixture "github.com/ruipengliu/lerna/conformance/internal/contentfixture"
	v "github.com/ruipengliu/lerna/contract/v1_2"
	"github.com/ruipengliu/lerna/domain/content"
	"github.com/ruipengliu/lerna/runtime"
)

// Pause only after the original native read (including its FD Close) succeeds.
// This mechanical return gate never replaces bytes, binding or physical facts.
type heldLegacyOriginalRead struct {
	content.ErasingObjects
	entered, release chan struct{}
}

func (o *heldLegacyOriginalRead) Read(ctx context.Context, key, hash string, length int64) ([]byte, error) {
	body, err := o.ErasingObjects.Read(ctx, key, hash, length)
	if err != nil {
		return nil, err
	}
	close(o.entered)
	finite, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	select {
	case <-o.release:
		return body, nil
	case <-finite.Done():
		return nil, finite.Err()
	}
}

func TestContentLegacyWholeRecordRaceRefusesStaleBindingAndReplaysOriginalQualification(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	w, original, qualified := fixture.NewStoppedLegacy(t, ctx, nil)
	first := original.Requests[0].Payload.ContentRef
	second := original.Requests[1].Payload.ContentRef
	request := content.LegacyPrimaryRequest{Ref: first, Purpose: "verification"}
	consumer := func(objects content.ErasingObjects) *content.Lifecycle {
		t.Helper()
		l, err := content.NewLifecycle(content.LifecycleConfig{ManagementConfig: content.ManagementConfig{Owner: contentOwner, Store: w.Store(), TrustedSubject: contentPrincipal, TrustedUntil: time.Now().Add(time.Hour), PageSize: 2, WorkBudget: time.Minute}, Objects: objects, PrimaryHolderID: "primary", Worker: "legacy-original-race", LegacyPrimary: &qualified})
		if err != nil {
			t.Fatal(err)
		}
		return l
	}
	assertOriginal := func() {
		t.Helper()
		service := contentService(t, w)
		for i, old := range original.Requests {
			_, key, err := content.VersionIdentity(old.Payload.ContentRef)
			if err != nil {
				t.Fatal(err)
			}
			body, err := os.ReadFile(filepath.Join(w.Directory, key))
			want := "alpha\n"
			if i == 1 {
				want = "beta\n"
			}
			if err != nil || string(body) != want {
				t.Fatal("original stopped-scope bytes changed", err)
			}
			before, err := v.Encode(original.Receipts[i])
			if err != nil {
				t.Fatal(err)
			}
			after, err := v.Encode(putContentRequest(t, ctx, service, old))
			if err != nil || string(before) != string(after) {
				t.Fatal("original fixed receipt changed", err)
			}
			observation, err := service.GetCommand(ctx, contentCommandGetWire(t, old.CommandID), &contentPrincipal)
			found, ok := observation.AsFound()
			progress, published := found.Progress.AsContent()
			if err != nil || !ok || !published || progress.Publication != "published" {
				t.Fatal("original publication history changed", observation, err)
			}
		}
	}
	assertOriginal()
	service := contentService(t, w)
	view, err := service.Get(ctx, contentGetWire(t, first, nil), &contentPrincipal)
	unavailable, ok := view.AsUnavailable()
	if err != nil || !ok || unavailable.ContentRef != first {
		t.Fatal("original empty binding unexpectedly available", view, err)
	}
	held := &heldLegacyOriginalRead{ErasingObjects: w.Objects, entered: make(chan struct{}), release: make(chan struct{})}
	var once sync.Once
	release := func() error { once.Do(func() { close(held.release) }); return nil }
	stale := consumer(held)
	result := make(chan error, 1)
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		_, err := stale.BindLegacyPrimary(ctx, &contentPrincipal, request)
		result <- err
	}()
	joinContentRead(t, w, release, finished)
	select {
	case <-held.entered:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	// This is a legitimate management action on the same original full Record,
	// after binder Tx1 committed and before its real read return/Tx2.
	retain := time.Now().UTC().Add(30 * time.Minute).Truncate(time.Microsecond)
	policy := content.FixturePolicy{Ref: first, Subject: contentPrincipal, Purpose: "verification", Revision: 2, ValidUntil: time.Now().Add(time.Hour), RetainUntil: retain, Read: true, Process: true, Save: true, Sync: true, Disclose: true}
	manager := trustedContentManager(t, w, 2)
	change, err := manager.InstallPolicy(ctx, &contentPrincipal, policy, 1)
	if err != nil || change.Policy.RetainUntil != retain || change.Policy.Ref != first {
		t.Fatal("actual lawful narrower saving policy did not commit", change, err)
	}
	if err = release(); err != nil {
		t.Fatal(err)
	}
	select {
	case err = <-result:
		if !errors.Is(err, runtime.ErrClaim) {
			t.Fatal("stale whole original Record was rebound", err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	select {
	case <-finished:
	case <-ctx.Done():
		t.Fatal("legacy race invocation did not finish", ctx.Err())
	}
	assertOriginal()
	view, err = service.Get(ctx, contentGetWire(t, first, nil), &contentPrincipal)
	unavailable, ok = view.AsUnavailable()
	if err != nil || !ok || unavailable.ContentRef != first {
		t.Fatal("stale binding survived race refusal", view, err)
	}
	seal := content.SealRequest{Ref: first, Purpose: "verification", SealID: "legacy-race-unbound", Deadline: time.Now().UTC().Add(time.Minute).Truncate(time.Microsecond)}
	if _, err = bodyLifecycle(t, w).Seal(ctx, &contentPrincipal, seal); !errors.Is(err, content.ErrHolderBinding) {
		t.Fatal("race refusal changed original holder authority", err)
	}
	w.Reopen(ctx)
	fresh := consumer(w.Objects)
	bound, err := fresh.BindLegacyPrimary(ctx, &contentPrincipal, request)
	if err != nil || bound.Ref != first || bound.Binding != qualified.Binding || bound.QualificationID != qualified.ID || bound.EvidenceDigest != qualified.EvidenceDigest || bound.Publication != "published" {
		t.Fatal("same original qualification could not replay current lawful Record", bound, err)
	}
	if _, err = fresh.BindLegacyPrimary(ctx, &contentPrincipal, content.LegacyPrimaryRequest{Ref: second, Purpose: "verification"}); err != nil {
		t.Fatal("independent original Version2 could not bind normally", err)
	}
	assertContentBody(t, ctx, contentService(t, w), first, nil, "alpha\n")
	assertContentBody(t, ctx, contentService(t, w), second, nil, "beta\n")
	assertOriginal()
	w.Reopen(ctx)
	replay, err := consumer(w.Objects).BindLegacyPrimary(ctx, &contentPrincipal, request)
	if err != nil || replay != bound {
		t.Fatal("reopen lost same original binding/provenance", replay, err)
	}
	assertContentBody(t, ctx, contentService(t, w), first, nil, "alpha\n")
	assertContentBody(t, ctx, contentService(t, w), second, nil, "beta\n")
	assertOriginal()
}
