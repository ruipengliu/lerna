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

func TestContentWrongPhysicalRootCannotAcknowledgeOriginalHolderErasure(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	w := fixture.New(t, ctx)
	installContentPolicy(t, ctx, w, alphaRef)
	service := contentService(t, w)
	if _, ok := putContentRequest(t, ctx, service, contentPut(t, alphaRef, "binding-original", "YWxwaGEK")).AsAccepted(); !ok {
		t.Fatal("normal admission refused")
	}
	if _, err := service.Step(ctx); err != nil {
		t.Fatal(err)
	}
	assertContentBody(t, ctx, service, alphaRef, nil, "alpha\n")
	original := bodyLifecycle(t, w)
	request := content.SealRequest{Ref: alphaRef, Purpose: "verification", SealID: "binding-original", Deadline: time.Now().Add(time.Minute).UTC().Truncate(time.Microsecond)}
	if _, err := original.Seal(ctx, &contentPrincipal, request); err != nil {
		t.Fatal(err)
	}
	wrong := fixture.New(t, ctx) // independently recorded, empty, different real root
	l, err := content.NewLifecycle(content.LifecycleConfig{ManagementConfig: content.ManagementConfig{Owner: contentOwner, Store: w.Store(), TrustedSubject: contentPrincipal, TrustedUntil: time.Now().Add(time.Hour), PageSize: 2, WorkBudget: time.Minute}, PrimaryHolderID: "primary", Objects: wrong.Objects, Worker: "wrong-root-cleanup"})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		_, err = l.Step(ctx, &contentPrincipal)
		if err != nil {
			break
		}
	}
	if !errors.Is(err, content.ErrHolderBinding) {
		t.Fatalf("same holder ID allowed a different physical root to ACK: %v", err)
	}
	observation, err := original.Observe(ctx, &contentPrincipal, alphaRef, "")
	if err != nil || observation.CleanupComplete {
		t.Fatalf("wrong root completed original duties: %+v %v", observation, err)
	}
	_, key, err := content.VersionIdentity(alphaRef)
	if err != nil {
		t.Fatal(err)
	}
	bytes, err := os.ReadFile(filepath.Join(w.Directory, key))
	if err != nil || string(bytes) != "alpha\n" {
		t.Fatal("wrong-root attempt touched the original body", err)
	}
	for i := 0; i < 4; i++ {
		worked, err := original.Step(ctx, &contentPrincipal)
		if err != nil {
			t.Fatal(err)
		}
		if !worked {
			break
		}
	}
	observation, err = original.Observe(ctx, &contentPrincipal, alphaRef, "")
	if err != nil || !observation.CleanupComplete || observation.Seal.ID != request.SealID || !observation.Seal.Deadline.Equal(request.Deadline) {
		t.Fatalf("original root could not recover same seal: %+v %v", observation, err)
	}
	if _, err = os.ReadFile(filepath.Join(w.Directory, key)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("correct recovery retained original bytes", err)
	}
}
