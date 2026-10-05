//go:build integration

package component_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	fixture "github.com/ruipengliu/lerna/conformance/internal/contentfixture"
	v "github.com/ruipengliu/lerna/contract/v1_2"
	"github.com/ruipengliu/lerna/domain/content"
)

// The one-job control and full original scan page have separate owned scopes.
// Both use only public Content / trusted Lifecycle and independent exact bytes.
func TestContentLaterWorkPassesOneExpiredCleanupResponsibility(t *testing.T) {
	testContentExpiredCleanupBacklog(t, 1)
}

func TestContentLaterWorkPassesFullExpiredCleanupPageWithoutChangingOriginalDuties(t *testing.T) {
	testContentExpiredCleanupBacklog(t, 64)
}

func testContentExpiredCleanupBacklog(t *testing.T, count int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	w := fixture.New(t, ctx)
	service := contentService(t, w)
	refs := make([]v.ContentRef, count)
	requests := make([]v.ContentPutRequest, count)
	receipts := make([][]byte, count)
	for i := range refs {
		refs[i] = alphaRef
		refs[i].ContentID = v.ID(fmt.Sprintf("expired-backlog-%02d", i))
		installContentPolicy(t, ctx, w, refs[i])
		requests[i] = contentPut(t, refs[i], fmt.Sprintf("backlog-original-%02d", i), "YWxwaGEK")
		receipt := putContentRequest(t, ctx, service, requests[i])
		if _, ok := receipt.AsAccepted(); !ok {
			t.Fatal("backlog preparation: original publication admission refused", i)
		}
		var err error
		receipts[i], err = v.Encode(receipt)
		if err != nil {
			t.Fatal(err)
		}
		if worked, err := service.Step(ctx); err != nil || !worked {
			t.Fatal("backlog preparation: original publication did not progress", i, worked, err)
		}
		assertContentBody(t, ctx, service, refs[i], nil, "alpha\n")
		assertBacklogPhysicalBody(t, w, refs[i], "alpha\n")
	}
	// Publish the future live-cleanup control before any backlog is sealed.
	live := alphaRef
	live.ContentID = "backlog-live-cleanup"
	installContentPolicy(t, ctx, w, live)
	if _, ok := putContentRequest(t, ctx, service, contentPut(t, live, "backlog-live-original", "YWxwaGEK")).AsAccepted(); !ok {
		t.Fatal("backlog preparation: live control refused")
	}
	if worked, err := service.Step(ctx); err != nil || !worked {
		t.Fatal("backlog preparation: live control publication failed", worked, err)
	}
	assertContentBody(t, ctx, service, live, nil, "alpha\n")
	lifecycle := bodyLifecycle(t, w)
	// Fixed once, before the first seal; never renewed after preparation/failure.
	originalDeadline := time.Now().Add(15 * time.Second).UTC().Truncate(time.Microsecond)
	original := make([]content.BodyCleanupObservation, count)
	for i, ref := range refs {
		var err error
		request := content.SealRequest{Ref: ref, Purpose: "verification", SealID: fmt.Sprintf("backlog-seal-%02d", i), Deadline: originalDeadline}
		original[i], err = lifecycle.Seal(ctx, &contentPrincipal, request)
		if err != nil || original[i].Seal.Ref != ref || original[i].Seal.ID != request.SealID || original[i].Seal.Purpose != request.Purpose || !original[i].Seal.Deadline.Equal(originalDeadline) || original[i].CleanupComplete || len(original[i].Holders) != 2 || original[i].NextCursor != "" {
			t.Fatal("backlog preparation: real original seal/holders incomplete", i, err)
		}
	}
	waitUntil(t, ctx, originalDeadline.Add(20*time.Millisecond))
	w.Reopen(ctx)
	service = contentService(t, w)
	lifecycle = bodyLifecycle(t, w)
	for i, ref := range refs {
		observed, err := lifecycle.Observe(ctx, &contentPrincipal, ref, "")
		if err != nil || !reflect.DeepEqual(observed, original[i]) {
			t.Fatal("backlog preparation: reopen changed original unconfirmed duty", i, err)
		}
		assertBacklogPhysicalBody(t, w, ref, "alpha\n")
	}
	version2 := refs[0]
	version2.Version = "2"
	version2.Hash = "sha256:f2c82decdd7181cf98945929a62598db7e6b477e11f6e0eb0ae97020eff151ad"
	version2.ByteLength = "5"
	installContentPolicy(t, ctx, w, version2)
	if _, ok := putContentRequest(t, ctx, service, contentPut(t, version2, "backlog-new-version", "YmV0YQo=")).AsAccepted(); !ok {
		t.Fatal("new exact version was not accepted after old deadlines")
	}
	_, version2Key, err := content.VersionIdentity(version2)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = os.Lstat(filepath.Join(w.Directory, version2Key)); !os.IsNotExist(err) {
		t.Fatal("new version had a body before original publication work", err)
	}
	for i := 0; i < 4; i++ {
		if _, err := service.Step(ctx); err != nil {
			t.Fatal("new exact version publication step failed", err)
		}
	}
	command, err := service.GetCommand(ctx, contentCommandGetWire(t, "backlog-new-version"), &contentPrincipal)
	found, ok := command.AsFound()
	progress, hasProgress := found.Progress.AsContent()
	if err != nil || !ok || !hasProgress || progress.ContentRef != version2 || progress.Publication != "published" {
		_, physicalErr := os.Lstat(filepath.Join(w.Directory, version2Key))
		if !os.IsNotExist(physicalErr) {
			t.Fatal("unexpected independent new-body result while publication was not complete", physicalErr)
		}
		t.Fatalf("later accepted version blocked by %d expired original cleanup jobs: publication=%q exact_ref=%v independent_body_missing=true command_error=%v", count, progress.Publication, progress.ContentRef == version2, err)
	}
	assertContentBody(t, ctx, service, version2, nil, "beta\n")
	assertBacklogPhysicalBody(t, w, version2, "beta\n")
	liveDeadline := time.Now().Add(45 * time.Second).UTC().Truncate(time.Microsecond)
	if _, err := lifecycle.Seal(ctx, &contentPrincipal, content.SealRequest{Ref: live, Purpose: "verification", SealID: "backlog-later-live-seal", Deadline: liveDeadline}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 4; i++ {
		if _, err := lifecycle.Step(ctx, &contentPrincipal); err != nil {
			t.Fatal(err)
		}
	}
	liveObservation, err := lifecycle.Observe(ctx, &contentPrincipal, live, "")
	if err != nil || !liveObservation.CleanupComplete || !liveObservation.Seal.Deadline.Equal(liveDeadline) {
		t.Fatal("later live original cleanup did not reach all independent holder ACKs", liveObservation, err)
	}
	for _, holder := range liveObservation.Holders {
		if holder.State != "erased" {
			t.Fatal("live cleanup lacks original holder ACK", holder)
		}
	}
	_, liveKey, err := content.VersionIdentity(live)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = os.Lstat(filepath.Join(w.Directory, liveKey)); !os.IsNotExist(err) {
		t.Fatal("live original body still exists", err)
	}
	w.Reopen(ctx)
	service = contentService(t, w)
	lifecycle = bodyLifecycle(t, w)
	for i, ref := range refs {
		observed, err := lifecycle.Observe(ctx, &contentPrincipal, ref, "")
		if err != nil || !reflect.DeepEqual(observed, original[i]) {
			t.Fatal("later work changed an old expired duty, deadline or holder history", i, err)
		}
		assertBacklogPhysicalBody(t, w, ref, "alpha\n")
		replay, err := v.Encode(putContentRequest(t, ctx, service, requests[i]))
		if err != nil || string(replay) != string(receipts[i]) {
			t.Fatal("later work changed original fixed receipt", i, err)
		}
		oldCommand, err := service.GetCommand(ctx, contentCommandGetWire(t, requests[i].CommandID), &contentPrincipal)
		oldFound, exists := oldCommand.AsFound()
		oldProgress, hasProgress := oldFound.Progress.AsContent()
		if err != nil || !exists || !hasProgress || oldProgress.ContentRef != ref || oldProgress.Publication != "published" {
			t.Fatal("later work changed original publication history", i, err)
		}
	}
	liveObservation, err = lifecycle.Observe(ctx, &contentPrincipal, live, "")
	if err != nil || !liveObservation.CleanupComplete {
		t.Fatal("reopen lost later cleanup ACKs", err)
	}
	assertContentBody(t, ctx, service, version2, nil, "beta\n")
}

func assertBacklogPhysicalBody(t *testing.T, w *fixture.World, ref v.ContentRef, want string) {
	t.Helper()
	_, key, err := content.VersionIdentity(ref)
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(w.Directory, key))
	if err != nil || string(body) != want {
		t.Fatal("independent exact original body differs", ref, err)
	}
}
