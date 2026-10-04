//go:build integration

package component_test

import (
	"context"
	fixture "github.com/ruipengliu/lerna/conformance/internal/contentfixture"
	v "github.com/ruipengliu/lerna/contract/v1_2"
	"github.com/ruipengliu/lerna/domain/content"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func bodyLifecycle(t *testing.T, w *fixture.World) *content.Lifecycle {
	t.Helper()
	l, err := content.NewLifecycle(content.LifecycleConfig{ManagementConfig: content.ManagementConfig{Owner: contentOwner, Store: w.Store(), TrustedSubject: contentPrincipal, TrustedUntil: time.Now().Add(time.Hour), PageSize: 2, WorkBudget: time.Minute}, PrimaryHolderID: "primary", Objects: w.Objects, Worker: "content-body-cleanup"})
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func TestContentTrustedSealSurvivesReopenAndPreservesOriginalReceipt(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	w := fixture.New(t, ctx)
	installContentPolicy(t, ctx, w, alphaRef)
	service := contentService(t, w)
	req := contentPut(t, alphaRef, "seal-original-command", "YWxwaGEK")
	receipt := putContentRequest(t, ctx, service, req)
	if _, ok := receipt.AsAccepted(); !ok {
		t.Fatal("normal original admission refused")
	}
	if _, err := service.Step(ctx); err != nil {
		t.Fatal(err)
	}
	assertContentBody(t, ctx, service, alphaRef, nil, "alpha\n")
	original, _ := v.Encode(receipt)
	close := content.SealRequest{Ref: alphaRef, Purpose: "verification", SealID: "seal-original", Deadline: time.Now().Add(time.Minute).UTC().Truncate(time.Microsecond)}
	observation, err := bodyLifecycle(t, w).Seal(ctx, &contentPrincipal, close)
	if err != nil || observation.Seal.Ref != alphaRef || observation.Seal.ID != close.SealID || observation.CleanupComplete {
		t.Fatalf("trusted original seal: %+v %v", observation, err)
	}
	_, key, err := content.VersionIdentity(alphaRef)
	if err != nil {
		t.Fatal(err)
	}
	bytes, err := os.ReadFile(filepath.Join(w.Directory, key))
	if err != nil || string(bytes) != "alpha\n" {
		t.Fatalf("seal was incorrectly treated as erased: %q %v", bytes, err)
	}
	w.Reopen(ctx)
	service = contentService(t, w)
	observation, err = bodyLifecycle(t, w).Observe(ctx, &contentPrincipal, alphaRef, "")
	if err != nil || observation.Seal.ID != close.SealID || !observation.Seal.Deadline.Equal(close.Deadline) || len(observation.Holders) != 2 || observation.CleanupComplete {
		t.Fatalf("reopened original holder responsibilities: %+v %v", observation, err)
	}
	view, err := service.Get(ctx, contentGetWire(t, alphaRef, nil), &contentPrincipal)
	denied, ok := view.AsRejected()
	if err != nil || !ok || denied.Reason != "forbidden" {
		t.Fatalf("sealed body used: %+v %v", view, err)
	}
	replayed, _ := v.Encode(putContentRequest(t, ctx, service, req))
	if string(replayed) != string(original) {
		t.Fatal("seal changed fixed original receipt")
	}
	alias := req
	alias.CommandID = "sealed-new-association"
	requireRejection(t, putContentRequest(t, ctx, service, alias), "forbidden")
	command, err := service.GetCommand(ctx, contentCommandGetWire(t, req.CommandID), &contentPrincipal)
	found, ok := command.AsFound()
	progress, hasProgress := found.Progress.AsContent()
	if err != nil || !ok || !hasProgress || progress.Publication != "published" {
		t.Fatalf("seal rewrote publication history: %+v %v", command, err)
	}
}
