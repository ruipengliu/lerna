//go:build integration

package component_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	fixture "github.com/ruipengliu/lerna/conformance/internal/contentfixture"
	v "github.com/ruipengliu/lerna/contract/v1_2"
	"github.com/ruipengliu/lerna/domain/content"
)

func TestContentDerivedGoneRequiresCurrentExactMetadataForReaderAndEveryAncestor(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	w := fixture.New(t, ctx)
	service := contentService(t, w)
	derived := alphaRef
	derived.ContentID = "metadata-derived"
	middle := alphaRef
	middle.ContentID = "metadata-middle"
	for _, ref := range []v.ContentRef{alphaRef, middle, derived} {
		installContentPolicy(t, ctx, w, ref)
	}
	if _, ok := putContentRequest(t, ctx, service, contentPut(t, alphaRef, "metadata-source", "YWxwaGEK")).AsAccepted(); !ok {
		t.Fatal("normal source refused")
	}
	if _, err := service.Step(ctx); err != nil {
		t.Fatal(err)
	}
	middleRequest := contentPut(t, middle, "metadata-middle", "YWxwaGEK")
	middleRequest.Payload.Sources = []v.ContentRef{alphaRef}
	if _, ok := putContentRequest(t, ctx, service, middleRequest).AsAccepted(); !ok {
		t.Fatal("normal middle body refused")
	}
	if _, err := service.Step(ctx); err != nil {
		t.Fatal(err)
	}
	request := contentPut(t, derived, "metadata-derived", "YWxwaGEK")
	request.Payload.Sources = []v.ContentRef{middle}
	if _, ok := putContentRequest(t, ctx, service, request).AsAccepted(); !ok {
		t.Fatal("normal derived body refused")
	}
	if _, err := service.Step(ctx); err != nil {
		t.Fatal(err)
	}
	assertContentBody(t, ctx, service, derived, nil, "alpha\n")
	l := bodyLifecycle(t, w)
	seal := content.SealRequest{Ref: derived, Purpose: "verification", SealID: "metadata-derived", Deadline: time.Now().Add(time.Minute).UTC().Truncate(time.Microsecond)}
	if _, err := l.Seal(ctx, &contentPrincipal, seal); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if _, err := l.Step(ctx, &contentPrincipal); err != nil {
			t.Fatal(err)
		}
	}
	w.Reopen(ctx)
	l = bodyLifecycle(t, w)
	service = contentService(t, w)
	original, err := l.Observe(ctx, &contentPrincipal, derived, "")
	if err != nil || !original.CleanupComplete {
		t.Fatal("real derived erase not complete", original, err)
	}
	_, key, err := content.VersionIdentity(derived)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = os.ReadFile(filepath.Join(w.Directory, key)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("derived original body not physically erased", err)
	}
	assertContentBody(t, ctx, service, alphaRef, nil, "alpha\n")
	assertContentBody(t, ctx, service, middle, nil, "alpha\n")
	reader := v.SubjectBinding{TenantID: contentOwner.TenantID, SubjectID: "metadata-reader", DelegationChain: []v.DelegatedSubject{{TenantID: contentOwner.TenantID, SubjectID: "metadata-delegator"}}}
	wide := time.Now().Add(time.Hour)
	permit := content.MetadataPolicy{Ref: derived, Subject: reader, Purpose: "verification", Revision: 1, ValidUntil: wide}
	if err = l.InstallMetadataPolicy(ctx, &contentPrincipal, permit, 0); err != nil {
		t.Fatal(err)
	}
	expectRejected := func(ref v.ContentRef, subject v.SubjectBinding, purpose v.Purpose, reason v.ErrorCode) {
		t.Helper()
		query, err := v.DecodeGet(contentGetWire(t, ref, nil))
		if err != nil {
			t.Fatal(err)
		}
		query.Payload.Purpose = purpose
		raw, err := v.Encode(query)
		if err != nil {
			t.Fatal(err)
		}
		view, err := service.Get(ctx, raw, &subject)
		denied, ok := view.AsRejected()
		if err != nil || !ok || denied.Reason != reason {
			t.Fatal("metadata qualification returned wrong rejection", reason, view, err)
		}
	}
	reject := func(ref v.ContentRef, subject v.SubjectBinding, purpose v.Purpose) {
		t.Helper()
		expectRejected(ref, subject, purpose, "forbidden")
	}
	reject(derived, reader, "verification") // Target-only permit cannot cover source.
	ancestor := permit
	ancestor.Ref = middle
	if err = l.InstallMetadataPolicy(ctx, &contentPrincipal, ancestor, 0); err != nil {
		t.Fatal(err)
	}
	reject(derived, reader, "verification") // Direct source permit cannot cover its ancestor.
	ancestor.Ref = alphaRef
	if err = l.InstallMetadataPolicy(ctx, &contentPrincipal, ancestor, 0); err != nil {
		t.Fatal(err)
	}
	view, err := service.Get(ctx, contentGetWire(t, derived, nil), &reader)
	gone, ok := view.AsGone()
	if err != nil || !ok || gone.ContentRef != derived || gone.EvidenceAvailable {
		t.Fatal("current exact ancestor permits did not allow minimal gone", view, err)
	}
	encoded, err := v.EncodeContentResponse(view, v.ContentGetPayload{ContentRef: derived, Purpose: "verification"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = v.DecodeContentResponse(encoded, v.ContentGetPayload{ContentRef: derived, Purpose: "verification"}); err != nil {
		t.Fatal(err)
	}
	other := reader
	other.DelegationChain = []v.DelegatedSubject{{TenantID: contentOwner.TenantID, SubjectID: "different-delegator"}}
	reject(derived, other, "verification")
	reject(derived, reader, "different-purpose")
	wrong := derived
	wrong.Hash = "sha256:f2c82decdd7181cf98945929a62598db7e6b477e11f6e0eb0ae97020eff151ad"
	expectRejected(wrong, reader, "verification", "integrity")
	wrongPermit := permit
	wrongPermit.Subject.SubjectID = "metadata-wrong-declaration"
	wrongPermit.Ref = wrong
	if err = l.InstallMetadataPolicy(ctx, &contentPrincipal, wrongPermit, 0); err != nil {
		t.Fatal(err)
	}
	reject(derived, wrongPermit.Subject, "verification")
	absent := derived
	absent.ContentID = "unpermitted-missing"
	reject(absent, reader, "verification")
	// The actual original ancestor permit expires; no stored clock or work due
	// is rewritten. The target permit remains current and cannot bypass it.
	ancestor.Revision = 2
	ancestor.ValidUntil = time.Now().Add(700 * time.Millisecond)
	if err = l.InstallMetadataPolicy(ctx, &contentPrincipal, ancestor, 1); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, ctx, ancestor.ValidUntil.Add(20*time.Millisecond))
	reject(derived, reader, "verification")
	unchanged, err := l.Observe(ctx, &contentPrincipal, derived, "")
	if err != nil || !unchanged.CleanupComplete || unchanged.Seal.ID != original.Seal.ID || !unchanged.Seal.Deadline.Equal(original.Seal.Deadline) {
		t.Fatal("queries changed original cleanup responsibility", unchanged, err)
	}
	if worked, err := l.Step(ctx, &contentPrincipal); err != nil || worked {
		t.Fatal("metadata queries created cleanup work", worked, err)
	}
	assertContentBody(t, ctx, service, alphaRef, nil, "alpha\n")
	assertContentBody(t, ctx, service, middle, nil, "alpha\n")
}
