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

func TestContentOrphanSealWinsBeforeLatePublicationAndPreservesLiveVersion(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	w := fixture.New(t, ctx)
	live := alphaRef
	live.Version = "2"
	installContentPolicy(t, ctx, w, live)
	service := contentService(t, w)
	if _, ok := putContentRequest(t, ctx, service, contentPut(t, live, "live-version-two", "YWxwaGEK")).AsAccepted(); !ok {
		t.Fatal("normal independent new version refused")
	}
	if _, err := service.Step(ctx); err != nil {
		t.Fatal(err)
	}
	assertContentBody(t, ctx, service, live, nil, "alpha\n")
	installContentPolicy(t, ctx, w, alphaRef)
	request := contentPut(t, alphaRef, "orphan-original", "YWxwaGEK")
	receipt := putContentRequest(t, ctx, service, request)
	if _, ok := receipt.AsAccepted(); !ok {
		t.Fatal("normal orphan candidate admission refused")
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
	actual, err := os.ReadFile(filepath.Join(w.Directory, key))
	if err != nil || string(actual) != "alpha\n" {
		t.Fatal("original real attempt did not write body", err)
	}
	l := bodyLifecycle(t, w)
	seal := content.SealRequest{Ref: alphaRef, Purpose: "verification", SealID: "orphan-original-seal", Deadline: time.Now().Add(time.Minute).UTC().Truncate(time.Microsecond)}
	observed, err := l.SealOrphan(ctx, &contentPrincipal, seal)
	if err != nil || observed.Seal.Ref != alphaRef || observed.CleanupComplete {
		t.Fatalf("registered unpublished orphan could not close: %+v %v", observed, err)
	}
	for i := 0; i < 4; i++ {
		worked, e := l.Step(ctx, &contentPrincipal)
		if e != nil {
			t.Fatal(e)
		}
		if !worked {
			break
		}
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
	if _, err = os.ReadFile(filepath.Join(w.Directory, key)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("late publication rebuilt orphan body", err)
	}
	_, liveKey, err := content.VersionIdentity(live)
	if err != nil {
		t.Fatal(err)
	}
	liveBytes, err := os.ReadFile(filepath.Join(w.Directory, liveKey))
	if err != nil || string(liveBytes) != "alpha\n" {
		t.Fatal("orphan deleted independent new version", err)
	}
	assertContentBody(t, ctx, contentService(t, w), live, nil, "alpha\n")
	w.Reopen(ctx)
	replay, err := v.Encode(putContentRequest(t, ctx, contentService(t, w), request))
	if err != nil || string(replay) != string(fixed) {
		t.Fatal("orphan revived or changed original receipt", err)
	}
	command, err := contentService(t, w).GetCommand(ctx, contentCommandGetWire(t, request.CommandID), &contentPrincipal)
	found, ok := command.AsFound()
	progress, hasProgress := found.Progress.AsContent()
	if err != nil || !ok || !hasProgress || progress.Publication != "failed" || progress.ContentRef != alphaRef {
		t.Fatalf("late publication escaped the original seal: %+v %v", command, err)
	}
	observed, err = bodyLifecycle(t, w).Observe(ctx, &contentPrincipal, alphaRef, "")
	if err != nil || !observed.CleanupComplete || !observed.Seal.Deadline.Equal(seal.Deadline) {
		t.Fatalf("orphan original responsibility lost on reopen: %+v %v", observed, err)
	}
	assertContentBody(t, ctx, contentService(t, w), live, nil, "alpha\n")
}

func TestContentPublishedReferenceWinsOrphanSelectionWithoutCreatingCleanup(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	w := fixture.New(t, ctx)
	service := contentService(t, w)
	live := alphaRef
	live.Version = "2"
	for _, ref := range []v.ContentRef{alphaRef, live} {
		installContentPolicy(t, ctx, w, ref)
		command := "published-orphan-version-" + string(ref.Version)
		if _, ok := putContentRequest(t, ctx, service, contentPut(t, ref, command, "YWxwaGEK")).AsAccepted(); !ok {
			t.Fatal("normal published reference refused")
		}
		if _, err := service.Step(ctx); err != nil {
			t.Fatal(err)
		}
		assertContentBody(t, ctx, service, ref, nil, "alpha\n")
	}
	l := bodyLifecycle(t, w)
	request := content.SealRequest{Ref: alphaRef, Purpose: "verification", SealID: "must-not-create-orphan-seal", Deadline: time.Now().Add(time.Minute).UTC().Truncate(time.Microsecond)}
	if _, err := l.SealOrphan(ctx, &contentPrincipal, request); !errors.Is(err, content.ErrOrphanReferenced) {
		t.Fatalf("published live reference did not win original lock: %v", err)
	}
	if _, err := l.Observe(ctx, &contentPrincipal, alphaRef, ""); !errors.Is(err, content.ErrUnavailable) {
		t.Fatal("refused orphan created seal responsibility", err)
	}
	if worked, err := l.Step(ctx, &contentPrincipal); err != nil || worked {
		t.Fatal("refused orphan created active cleanup work", worked, err)
	}
	for _, ref := range []v.ContentRef{alphaRef, live} {
		_, key, err := content.VersionIdentity(ref)
		if err != nil {
			t.Fatal(err)
		}
		actual, err := os.ReadFile(filepath.Join(w.Directory, key))
		if err != nil || string(actual) != "alpha\n" {
			t.Fatal("orphan selection touched a live version body", err)
		}
		assertContentBody(t, ctx, service, ref, nil, "alpha\n")
	}
	w.Reopen(ctx)
	service = contentService(t, w)
	for _, ref := range []v.ContentRef{alphaRef, live} {
		assertContentBody(t, ctx, service, ref, nil, "alpha\n")
	}
	if _, err := bodyLifecycle(t, w).Observe(ctx, &contentPrincipal, alphaRef, ""); !errors.Is(err, content.ErrUnavailable) {
		t.Fatal("orphan refusal became a seal on reopen", err)
	}
	command, err := service.GetCommand(ctx, contentCommandGetWire(t, "published-orphan-version-1"), &contentPrincipal)
	found, ok := command.AsFound()
	progress, hasProgress := found.Progress.AsContent()
	if err != nil || !ok || !hasProgress || progress.Publication != "published" || progress.ContentRef != alphaRef {
		t.Fatalf("orphan refusal changed original published history: %+v %v", command, err)
	}
}
