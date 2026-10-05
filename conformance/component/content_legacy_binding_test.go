//go:build integration

package component_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	local "github.com/ruipengliu/lerna/adapters/objectstore/local"
	fixture "github.com/ruipengliu/lerna/conformance/internal/contentfixture"
	v "github.com/ruipengliu/lerna/contract/v1_2"
	"github.com/ruipengliu/lerna/domain/content"
	"github.com/ruipengliu/lerna/runtime"
)

func TestContentStoppedOriginalLegacyScopeBindsOnlyAfterPositiveWriterClose(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	heldRefused := false
	w, old, qualified := fixture.NewStoppedLegacy(t, ctx, func(w *fixture.World, observation fixture.StoppedLegacyObservation) {
		// Only open the current reader holder here. No current Put, fence or
		// erasure competes with the old non-fencing writer still at its gate.
		objects, err := local.Open(w.Directory)
		if err != nil {
			t.Fatal(err)
		}
		l, err := content.NewLifecycle(content.LifecycleConfig{ManagementConfig: content.ManagementConfig{Owner: contentOwner, Store: w.Store(), TrustedSubject: contentPrincipal, TrustedUntil: time.Now().Add(time.Hour), PageSize: 2, WorkBudget: time.Minute}, PrimaryHolderID: "primary", Objects: objects, Worker: "content-body-cleanup"})
		if err != nil {
			t.Fatal(errors.Join(err, objects.Close()))
		}
		_, bindErr := l.BindLegacyPrimary(ctx, &contentPrincipal, content.LegacyPrimaryRequest{Ref: observation.Requests[0].Payload.ContentRef, Purpose: "verification"})
		closeErr := objects.Close()
		if !errors.Is(bindErr, content.ErrUnavailable) || closeErr != nil {
			t.Fatal("held old writer was adopted or current reader did not close", bindErr, closeErr)
		}
		heldRefused = true
	})
	if !heldRefused {
		t.Fatal("actual held old writer refusal was not observed")
	}
	service := contentService(t, w)
	for i, request := range old.Requests {
		ref := request.Payload.ContentRef
		_, key, err := content.VersionIdentity(ref)
		if err != nil {
			t.Fatal(err)
		}
		body, err := os.ReadFile(filepath.Join(w.Directory, key))
		want := "alpha\n"
		if i == 1 {
			want = "beta\n"
		}
		if err != nil || string(body) != want {
			t.Fatal("actual original bytes changed during upgrade", string(body), err)
		}
		view, err := service.Get(ctx, contentGetWire(t, ref, nil), &contentPrincipal)
		unavailable, ok := view.AsUnavailable()
		if err != nil || !ok || unavailable.ContentRef != ref || unavailable.Reason != "dependency_unavailable" {
			t.Fatal("unbound legacy body was adopted by current reader", view, err)
		}
		requestWire, err := v.Encode(request)
		if err != nil {
			t.Fatal(err)
		}
		replay, err := service.Put(ctx, requestWire, &contentPrincipal)
		received, ok := replay.AsReceived()
		if err != nil || !ok {
			t.Fatal("upgrade lost original fixed receipt", replay, err)
		}
		before, err := v.Encode(old.Receipts[i])
		if err != nil {
			t.Fatal(err)
		}
		after, err := v.Encode(received.Receipt)
		if err != nil {
			t.Fatal(err)
		}
		if string(before) != string(after) {
			t.Fatal("upgrade rewrote original fixed receipt")
		}
		command, err := service.GetCommand(ctx, contentCommandGetWire(t, request.CommandID), &contentPrincipal)
		found, ok := command.AsFound()
		history, hasHistory := found.Progress.AsContent()
		if err != nil || !ok || !hasHistory || history.Publication != "published" {
			t.Fatal("upgrade rewrote original published history", command, err)
		}
	}
	request := content.SealRequest{Ref: old.Requests[0].Payload.ContentRef, Purpose: "verification", SealID: "legacy-unbound-refusal", Deadline: time.Now().UTC().Add(time.Minute).Truncate(time.Microsecond)}
	if _, err := bodyLifecycle(t, w).Seal(ctx, &contentPrincipal, request); !errors.Is(err, content.ErrHolderBinding) {
		t.Fatal("unbound original holder received a seal", err)
	}
	l, err := content.NewLifecycle(content.LifecycleConfig{ManagementConfig: content.ManagementConfig{Owner: contentOwner, Store: w.Store(), TrustedSubject: contentPrincipal, TrustedUntil: time.Now().Add(time.Hour), PageSize: 2, WorkBudget: time.Minute}, PrimaryHolderID: "primary", Objects: w.Objects, Worker: "content-body-cleanup", LegacyPrimary: &qualified})
	if err != nil {
		t.Fatal(err)
	}
	bound, err := l.BindLegacyPrimary(ctx, &contentPrincipal, content.LegacyPrimaryRequest{Ref: request.Ref, Purpose: "verification"})
	if err != nil || bound.Ref != request.Ref || bound.Binding != qualified.Binding || bound.QualificationID != qualified.ID || bound.EvidenceDigest != qualified.EvidenceDigest || bound.Publication != "published" {
		t.Fatal("positive original writer Close/Wait scope cannot bind its original holder", bound, err)
	}
	// The host configuration is an immutable snapshot. A caller retaining its
	// original DTO cannot replace the medium after constructing this consumer.
	savedBinding := qualified.Binding
	qualified.Binding = "caller-mutated-binding"
	replayed, err := l.BindLegacyPrimary(ctx, &contentPrincipal, content.LegacyPrimaryRequest{Ref: request.Ref, Purpose: "verification"})
	qualified.Binding = savedBinding
	if err != nil || replayed != bound {
		t.Fatal("same original qualification replay changed after host DTO mutation", replayed, err)
	}
	second := old.Requests[1].Payload.ContentRef
	if _, err = l.BindLegacyPrimary(ctx, &contentPrincipal, content.LegacyPrimaryRequest{Ref: second, Purpose: "verification"}); err != nil {
		t.Fatal("independent original Version2 binding failed", err)
	}
	assertContentBody(t, ctx, service, request.Ref, nil, "alpha\n")
	assertContentBody(t, ctx, service, second, nil, "beta\n")
	newLifecycle := func(objects content.ErasingObjects, q content.LegacyPrimaryQualification) *content.Lifecycle {
		t.Helper()
		consumer, err := content.NewLifecycle(content.LifecycleConfig{ManagementConfig: content.ManagementConfig{Owner: contentOwner, Store: w.Store(), TrustedSubject: contentPrincipal, TrustedUntil: time.Now().Add(time.Hour), PageSize: 2, WorkBudget: time.Minute}, PrimaryHolderID: "primary", Objects: objects, Worker: "content-body-cleanup", LegacyPrimary: &q})
		if err != nil {
			t.Fatal(err)
		}
		return consumer
	}
	wrong := fixture.New(t, ctx)
	if _, err = newLifecycle(wrong.Objects, qualified).BindLegacyPrimary(ctx, &contentPrincipal, content.LegacyPrimaryRequest{Ref: request.Ref, Purpose: "verification"}); !errors.Is(err, content.ErrHolderBinding) {
		t.Fatal("empty configured root adopted original legacy responsibility", err)
	}
	_, key, err := content.VersionIdentity(request.Ref)
	if err != nil {
		t.Fatal(err)
	}
	wrong.WriteIndependentObject(key, []byte("alpha\n"))
	if _, err = newLifecycle(wrong.Objects, qualified).BindLegacyPrimary(ctx, &contentPrincipal, content.LegacyPrimaryRequest{Ref: request.Ref, Purpose: "verification"}); !errors.Is(err, content.ErrHolderBinding) {
		t.Fatal("copied correct bytes were accepted as the original holder", err)
	}
	wrongNamespace := qualified
	wrongNamespace.Namespace = wrong.Config.Schema
	if _, err = newLifecycle(w.Objects, wrongNamespace).BindLegacyPrimary(ctx, &contentPrincipal, content.LegacyPrimaryRequest{Ref: request.Ref, Purpose: "verification"}); !errors.Is(err, runtime.ErrScope) {
		t.Fatal("wrong database scope bypassed the exact qualified namespace", err)
	}
	w.Reopen(ctx)
	service = contentService(t, w)
	l = newLifecycle(w.Objects, qualified)
	replayed, err = l.BindLegacyPrimary(ctx, &contentPrincipal, content.LegacyPrimaryRequest{Ref: request.Ref, Purpose: "verification"})
	if err != nil || replayed != bound {
		t.Fatal("actual reopen lost idempotent original binding provenance", replayed, err)
	}
	assertContentBody(t, ctx, service, request.Ref, nil, "alpha\n")
	assertContentBody(t, ctx, service, second, nil, "beta\n")
	seal, err := l.Seal(ctx, &contentPrincipal, request)
	if err != nil || seal.Seal.PrimaryHolderBinding != qualified.Binding || seal.CleanupComplete {
		t.Fatal("bound original body did not enter the new erasure protocol", seal, err)
	}
	for i := 0; i < 4; i++ {
		if worked, err := l.Step(ctx, &contentPrincipal); err != nil || !worked {
			t.Fatal("original registered holder cleanup failed", worked, err)
		}
		seal, err = l.Observe(ctx, &contentPrincipal, request.Ref, "")
		if err != nil {
			t.Fatal(err)
		}
		if seal.CleanupComplete {
			break
		}
	}
	if !seal.CleanupComplete || seal.Seal.ID != request.SealID || !seal.Seal.Deadline.Equal(request.Deadline) {
		t.Fatal("original holder duty was not completely ACKed", seal)
	}
	if _, err = os.Lstat(filepath.Join(w.Directory, key)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("original physical body still exists after ACK", err)
	}
	for _, effect := range old.Attempts[key] {
		if _, err = os.Lstat(filepath.Join(w.Directory, effect.Attempt)); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("original actual legacy attempt was not exactly absent", effect, err)
		}
	}
	if err = l.InstallMetadataPolicy(ctx, &contentPrincipal, content.MetadataPolicy{Ref: request.Ref, Subject: contentPrincipal, Purpose: "verification", Revision: 1, ValidUntil: time.Now().Add(time.Hour)}, 0); err != nil {
		t.Fatal(err)
	}
	w.Reopen(ctx)
	service = contentService(t, w)
	view, err := service.Get(ctx, contentGetWire(t, request.Ref, nil), &contentPrincipal)
	gone, ok := view.AsGone()
	if err != nil || !ok || gone.ContentRef != request.Ref || gone.EvidenceAvailable {
		t.Fatal("reopened original erased version did not return exact metadata gone", view, err)
	}
	assertContentBody(t, ctx, service, second, nil, "beta\n")
	for i, original := range old.Requests {
		receipt := putContentRequest(t, ctx, service, original)
		before, err := v.Encode(old.Receipts[i])
		if err != nil {
			t.Fatal(err)
		}
		after, err := v.Encode(receipt)
		if err != nil {
			t.Fatal(err)
		}
		if string(before) != string(after) {
			t.Fatal("real legacy binding/erasure changed original fixed receipt")
		}
		command, err := service.GetCommand(ctx, contentCommandGetWire(t, original.CommandID), &contentPrincipal)
		found, ok := command.AsFound()
		history, hasHistory := found.Progress.AsContent()
		if err != nil || !ok || !hasHistory || history.Publication != "published" {
			t.Fatal("real legacy binding/erasure rewrote original publication history", command, err)
		}
	}
}
