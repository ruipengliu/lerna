//go:build integration

package component_test

import (
	"context"
	"errors"
	fixture "github.com/ruipengliu/lerna/conformance/internal/contentfixture"
	"github.com/ruipengliu/lerna/domain/content"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestContentIndependentSecondaryOfflineRetainsOriginalCleanupResponsibility(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	w := fixture.New(t, ctx)
	secondary := fixture.New(t, ctx)
	installContentPolicy(t, ctx, w, alphaRef)
	until := time.Now().Add(time.Hour).UTC().Truncate(time.Microsecond)
	if err := w.Store().InstallFixturePolicy(ctx, content.FixturePolicy{Ref: alphaRef, Subject: contentPrincipal, Purpose: "verification", Revision: 2, ValidUntil: until, RetainUntil: until, Read: true, Process: true, Save: true, Sync: true, Disclose: true}, 1); err != nil {
		t.Fatal(err)
	}
	service := contentService(t, w)
	if _, ok := putContentRequest(t, ctx, service, contentPut(t, alphaRef, "copy-original", "YWxwaGEK")).AsAccepted(); !ok {
		t.Fatal("normal admission refused")
	}
	if _, err := service.Step(ctx); err != nil {
		t.Fatal(err)
	}
	assertContentBody(t, ctx, service, alphaRef, nil, "alpha\n")
	config := content.LifecycleConfig{ManagementConfig: content.ManagementConfig{Owner: contentOwner, Store: w.Store(), TrustedSubject: contentPrincipal, TrustedUntil: until, PageSize: 2, WorkBudget: time.Minute}, PrimaryHolderID: "primary", Objects: w.Objects, SecondaryHolderID: "secondary", SecondaryObjects: secondary.Objects, Worker: "secondary-cleanup"}
	l, err := content.NewLifecycle(config)
	if err != nil {
		t.Fatal(err)
	}
	copyRequest := content.CopyRequest{Ref: alphaRef, Purpose: "verification", ID: "original-copy", Deadline: time.Now().Add(time.Minute).UTC().Truncate(time.Microsecond)}
	copied, err := l.CopyToSecondary(ctx, &contentPrincipal, copyRequest)
	if err != nil || !copied.Confirmed || copied.HolderID != "secondary" || copied.Ref != alphaRef {
		t.Fatalf("normal independent secondary copy: %+v %v", copied, err)
	}
	_, key, err := content.VersionIdentity(alphaRef)
	if err != nil {
		t.Fatal(err)
	}
	originalInfo, err := os.Stat(filepath.Join(w.Directory, key))
	if err != nil {
		t.Fatal(err)
	}
	copyInfo, err := os.Stat(filepath.Join(secondary.Directory, key))
	if err != nil || os.SameFile(originalInfo, copyInfo) {
		t.Fatal("secondary is not an independent physical body", err)
	}
	bytes, err := os.ReadFile(filepath.Join(secondary.Directory, key))
	if err != nil || string(bytes) != "alpha\n" {
		t.Fatal("independent secondary bytes", err)
	}
	// Actually close its media holder. Offline config has no active object port.
	if err = secondary.Objects.Close(); err != nil {
		t.Fatal(err)
	}
	config.SecondaryObjects = nil
	offline, err := content.NewLifecycle(config)
	if err != nil {
		t.Fatal(err)
	}
	sealRequest := content.SealRequest{Ref: alphaRef, Purpose: "verification", SealID: "original-secondary-seal", Deadline: time.Now().Add(time.Minute).UTC().Truncate(time.Microsecond)}
	if _, err = offline.Seal(ctx, &contentPrincipal, sealRequest); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 4; i++ {
		worked, e := offline.Step(ctx, &contentPrincipal)
		if e != nil {
			break
		}
		if !worked {
			break
		}
	}
	primaryErased := false
	pending := false
	cursor := ""
	for {
		observed, e := offline.Observe(ctx, &contentPrincipal, alphaRef, cursor)
		if e != nil || observed.CleanupComplete {
			t.Fatalf("offline copy falsely complete: %+v %v", observed, e)
		}
		for _, holder := range observed.Holders {
			if holder.Identity.HolderID == "primary" && holder.State == "erased" {
				primaryErased = true
			}
			if holder.Identity.HolderID == "secondary" {
				pending = holder.State != "erased" && holder.Responsible == "secondary" && holder.Deadline.Equal(sealRequest.Deadline)
			}
		}
		if observed.NextCursor == "" {
			break
		}
		cursor = observed.NextCursor
	}
	if !primaryErased || !pending {
		t.Fatal("offline independent holder responsibility lost", primaryErased, pending)
	}
	bytes, err = os.ReadFile(filepath.Join(secondary.Directory, key))
	if err != nil || string(bytes) != "alpha\n" {
		t.Fatal("offline actual copy disappeared", err)
	}
	if _, err = os.ReadFile(filepath.Join(w.Directory, key)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("primary body retained", err)
	}
	if err = offline.InstallMetadataPolicy(ctx, &contentPrincipal, content.MetadataPolicy{Ref: alphaRef, Subject: contentPrincipal, Purpose: "verification", Revision: 1, ValidUntil: until}, 0); err != nil {
		t.Fatal(err)
	}
	view, err := service.Get(ctx, contentGetWire(t, alphaRef, nil), &contentPrincipal)
	gone, ok := view.AsGone()
	if err != nil || !ok || gone.EvidenceAvailable {
		t.Fatalf("primary gone while secondary pending: %+v %v", view, err)
	}
	secondary.Reopen(ctx)
	w.Reopen(ctx)
	config.Store = w.Store()
	config.Objects = w.Objects
	config.SecondaryObjects = secondary.Objects
	resumed, err := content.NewLifecycle(config)
	if err != nil {
		t.Fatal(err)
	}
	// Reopening advances only the same original duty and cannot restart its budget.
	for i := 0; i < 30; i++ {
		_, e := resumed.Step(ctx, &contentPrincipal)
		if e != nil {
			t.Fatal(e)
		}
		progress, e := resumed.Observe(ctx, &contentPrincipal, alphaRef, "")
		if e != nil {
			t.Fatal(e)
		}
		if progress.CleanupComplete {
			break
		}
		timer := time.NewTimer(10 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			t.Fatal(ctx.Err())
		case <-timer.C:
		}
	}
	observed, err := resumed.Observe(ctx, &contentPrincipal, alphaRef, "")
	if err != nil || !observed.CleanupComplete || !observed.Seal.Deadline.Equal(sealRequest.Deadline) {
		t.Fatalf("original cleanup did not recover: %+v %v", observed, err)
	}
	if _, err = os.ReadFile(filepath.Join(secondary.Directory, key)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("secondary body retained after real recovery", err)
	}
}
