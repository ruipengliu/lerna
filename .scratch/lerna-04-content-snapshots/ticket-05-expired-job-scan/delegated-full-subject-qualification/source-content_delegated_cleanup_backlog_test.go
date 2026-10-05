//go:build integration

package component_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	fixture "github.com/ruipengliu/lerna/conformance/internal/contentfixture"
	v "github.com/ruipengliu/lerna/contract/v1_2"
	"github.com/ruipengliu/lerna/domain/content"
)

type delegatedCleanupDuty struct {
	ref         v.ContentRef
	request     v.ContentPutRequest
	receipt     []byte
	observation content.BodyCleanupObservation
}

func TestContentDelegatedLifecyclePassesOtherSavingSubjectFullPageWithoutChangingOriginalDuties(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	w := fixture.New(t, ctx)
	service := contentService(t, w)
	first := v.SubjectBinding{TenantID: contentOwner.TenantID, SubjectID: "delegated-cleanup-writer", DelegationChain: []v.DelegatedSubject{{TenantID: contentOwner.TenantID, SubjectID: "first-cleanup-delegator"}}}
	second := v.SubjectBinding{TenantID: first.TenantID, SubjectID: first.SubjectID, DelegationChain: []v.DelegatedSubject{{TenantID: contentOwner.TenantID, SubjectID: "second-cleanup-delegator"}}}
	trustedUntil := time.Now().Add(time.Hour).UTC().Truncate(time.Microsecond)
	newLifecycle := func(subject v.SubjectBinding) *content.Lifecycle {
		l, err := content.NewLifecycle(content.LifecycleConfig{ManagementConfig: content.ManagementConfig{Owner: contentOwner, Store: w.Store(), TrustedSubject: subject, TrustedUntil: trustedUntil, PageSize: 2, WorkBudget: time.Minute}, PrimaryHolderID: "primary", Objects: w.Objects, Worker: "delegated-cleanup"})
		if err != nil {
			t.Fatal("legitimate complete saving Subject capability refused", err)
		}
		return l
	}
	for _, subject := range []v.SubjectBinding{first, second} {
		if err := w.Store().InstallFixtureCommandReader(ctx, contentOwner, subject, trustedUntil); err != nil {
			t.Fatal(err)
		}
	}
	duties := make([]delegatedCleanupDuty, 64)
	for i := range duties {
		ref := alphaRef
		ref.ContentID = v.ID(fmt.Sprintf("delegated-first-%02d", i))
		duties[i] = publishDelegatedCleanupDuty(t, ctx, w, service, first, trustedUntil, ref, fmt.Sprintf("delegated-first-publication-%02d", i))
	}
	ownRef := alphaRef
	ownRef.ContentID = "delegated-second-live"
	own := publishDelegatedCleanupDuty(t, ctx, w, service, second, trustedUntil, ownRef, "delegated-second-publication")
	firstLifecycle := newLifecycle(first)
	secondLifecycle := newLifecycle(second)
	// These are live original responsibilities, not the expired-body control.
	// Fix one initial finite live window before the first seal; never renew it.
	deadline := time.Now().Add(45 * time.Second).UTC().Truncate(time.Microsecond)
	seal := func(lifecycle *content.Lifecycle, subject v.SubjectBinding, duty *delegatedCleanupDuty, id string) {
		request := content.SealRequest{Ref: duty.ref, Purpose: "verification", SealID: id, Deadline: deadline}
		observed, err := lifecycle.Seal(ctx, &subject, request)
		if err != nil || observed.Seal.Ref != request.Ref || observed.Seal.ID != request.SealID || observed.Seal.Purpose != request.Purpose || !observed.Seal.Deadline.Equal(deadline) || !sameManagerPublicValue(t, observed.Seal.Subject, subject) || observed.Seal.PrimaryHolderID != "primary" || observed.Seal.PrimaryHolderBinding != w.Objects.Binding() || observed.CleanupComplete || len(observed.Holders) != 2 || observed.NextCursor != "" {
			t.Fatal("legitimate original delegated Seal / complete holders refused", err)
		}
		duty.observation = observed
	}
	for i := range duties {
		seal(firstLifecycle, first, &duties[i], fmt.Sprintf("delegated-first-seal-%02d", i))
	}
	// Created after the full live other-subject page, same primary and binding.
	seal(secondLifecycle, second, &own, "delegated-second-seal")
	w.Reopen(ctx)
	service = contentService(t, w)
	firstLifecycle = newLifecycle(first)
	secondLifecycle = newLifecycle(second)
	assertDelegatedCleanupDuties(t, ctx, w, service, firstLifecycle, first, duties)
	assertDelegatedCleanupDuties(t, ctx, w, service, secondLifecycle, second, []delegatedCleanupDuty{own})
	if !time.Now().Before(deadline) {
		t.Fatal("delegated preparation exceeded the original live Seal window")
	}
	for step := 0; step < 4; step++ {
		observed, err := secondLifecycle.Observe(ctx, &second, ownRef, "")
		if err != nil {
			t.Fatal(err)
		}
		if observed.CleanupComplete {
			break
		}
		if _, err = secondLifecycle.Step(ctx, &second); err != nil {
			t.Fatal("second legitimate full Subject actual cleanup failed", err)
		}
	}
	completed, err := secondLifecycle.Observe(ctx, &second, ownRef, "")
	if err != nil || !completed.CleanupComplete || len(completed.Holders) != 2 || completed.NextCursor != "" || !sameBacklogPublicObservation(t, content.BodyCleanupObservation{Seal: completed.Seal}, content.BodyCleanupObservation{Seal: own.observation.Seal}) {
		t.Fatal("later legitimate delegated cleanup blocked by 64 live other-Subject duties", err)
	}
	matchedOriginals := make([]bool, len(own.observation.Holders))
	for _, holder := range completed.Holders {
		matched := -1
		for i, original := range own.observation.Holders {
			if sameManagerPublicValue(t, holder.Identity, original.Identity) && holder.Kind == original.Kind && holder.Deadline.Equal(original.Deadline) {
				matched = i
				break
			}
		}
		if matched < 0 || holder.State != "erased" {
			t.Fatal("later full Subject cleanup lacks an original exact holder ACK")
		}
		if matchedOriginals[matched] {
			t.Fatal("later full Subject cleanup duplicated an original holder ACK")
		}
		matchedOriginals[matched] = true
	}
	for _, matched := range matchedOriginals {
		if !matched {
			t.Fatal("later full Subject cleanup omitted an original holder ACK")
		}
	}
	_, ownKey, err := content.VersionIdentity(ownRef)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = os.Lstat(filepath.Join(w.Directory, ownKey)); !os.IsNotExist(err) {
		t.Fatal("later legitimate delegated body was not physically erased", err)
	}
	metadata := content.MetadataPolicy{Ref: ownRef, Subject: second, Purpose: "verification", Revision: 1, ValidUntil: trustedUntil}
	if err = secondLifecycle.InstallMetadataPolicy(ctx, &second, metadata, 0); err != nil {
		t.Fatal(err)
	}
	w.Reopen(ctx)
	service = contentService(t, w)
	firstLifecycle = newLifecycle(first)
	secondLifecycle = newLifecycle(second)
	assertDelegatedCleanupDuties(t, ctx, w, service, firstLifecycle, first, duties)
	if !time.Now().Before(deadline) {
		t.Fatal("old other-Subject responsibilities were no longer live at final proof")
	}
	after, err := secondLifecycle.Observe(ctx, &second, ownRef, "")
	if err != nil || !sameBacklogPublicObservation(t, after, completed) {
		t.Fatal("real reopen changed the second complete Subject's original holder ACK / Seal", err)
	}
	if _, err = os.Lstat(filepath.Join(w.Directory, ownKey)); !os.IsNotExist(err) {
		t.Fatal("real reopen revived the second Subject's physically erased body", err)
	}
	command, err := service.GetCommand(ctx, contentCommandGetWire(t, own.request.CommandID), &second)
	found, ok := command.AsFound()
	progress, hasProgress := found.Progress.AsContent()
	if err != nil || !ok || !hasProgress || progress.ContentRef != ownRef || progress.Publication != "published" {
		t.Fatal("delegated cleanup changed original second Subject publication history", err)
	}
	receipt, err := v.Encode(found.Receipt)
	if err != nil || string(receipt) != string(own.receipt) {
		t.Fatal("delegated cleanup changed the second Subject's fixed receipt", err)
	}
	view, err := service.Get(ctx, contentGetWire(t, ownRef, nil), &second)
	gone, ok := view.AsGone()
	if err != nil || !ok || gone.ContentRef != ownRef || gone.EvidenceAvailable {
		t.Fatal("current authorized second Subject metadata does not match physical absence", err)
	}
}

func publishDelegatedCleanupDuty(t *testing.T, ctx context.Context, w *fixture.World, service *content.Service, subject v.SubjectBinding, trustedUntil time.Time, ref v.ContentRef, command string) delegatedCleanupDuty {
	t.Helper()
	manager, err := content.NewManager(content.ManagementConfig{Owner: contentOwner, Store: w.Store(), TrustedSubject: subject, TrustedUntil: trustedUntil, PageSize: 2, WorkBudget: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	policy := content.FixturePolicy{Ref: ref, Subject: subject, Purpose: "verification", Revision: 1, ValidUntil: trustedUntil, RetainUntil: trustedUntil, Read: true, Process: true, Save: true, Sync: true, Disclose: true}
	if _, err = manager.InstallPolicy(ctx, &subject, policy, 0); err != nil {
		t.Fatal("actual delegated saving policy refused", err)
	}
	request := contentPut(t, ref, command, "YWxwaGEK")
	raw, err := v.Encode(request)
	if err != nil {
		t.Fatal(err)
	}
	out, err := service.Put(ctx, raw, &subject)
	received, ok := out.AsReceived()
	if err != nil || !ok {
		t.Fatal("actual delegated publication transport failed", err)
	}
	if _, ok = received.Receipt.AsAccepted(); !ok {
		t.Fatal("actual delegated publication admission refused")
	}
	fixed, err := v.Encode(received.Receipt)
	if err != nil {
		t.Fatal(err)
	}
	if worked, err := service.Step(ctx); err != nil || !worked {
		t.Fatal("actual delegated publication did not progress", worked, err)
	}
	view, err := service.Get(ctx, contentGetWire(t, ref, nil), &subject)
	published, ok := view.AsPublished()
	if err != nil || !ok || published.ContentRef != ref || published.BytesBase64 != "YWxwaGEK" {
		t.Fatal("actual delegated normal saving basis lacks known original bytes", err)
	}
	assertBacklogPhysicalBody(t, w, ref, "alpha\n")
	if err = service.AuthorizeUse(ctx, &subject, ref, "verification", "save"); err != nil {
		t.Fatal("actual delegated normal saving use refused", err)
	}
	return delegatedCleanupDuty{ref: ref, request: request, receipt: fixed}
}

func assertDelegatedCleanupDuties(t *testing.T, ctx context.Context, w *fixture.World, service *content.Service, lifecycle *content.Lifecycle, subject v.SubjectBinding, duties []delegatedCleanupDuty) {
	t.Helper()
	for i, duty := range duties {
		observed, err := lifecycle.Observe(ctx, &subject, duty.ref, "")
		if err != nil || !sameBacklogPublicObservation(t, observed, duty.observation) {
			t.Fatal("other legitimate full Subject original responsibility/holders/deadline changed", i, err)
		}
		assertBacklogPhysicalBody(t, w, duty.ref, "alpha\n")
		raw, err := v.Encode(duty.request)
		if err != nil {
			t.Fatal(err)
		}
		out, err := service.Put(ctx, raw, &subject)
		received, ok := out.AsReceived()
		if err != nil || !ok {
			t.Fatal("actual original delegated receipt replay transport failed", i, err)
		}
		receipt, err := v.Encode(received.Receipt)
		if err != nil || string(receipt) != string(duty.receipt) {
			t.Fatal("actual original delegated fixed receipt changed", i, err)
		}
		command, err := service.GetCommand(ctx, contentCommandGetWire(t, duty.request.CommandID), &subject)
		found, ok := command.AsFound()
		progress, hasProgress := found.Progress.AsContent()
		if err != nil || !ok || !hasProgress || progress.ContentRef != duty.ref || progress.Publication != "published" {
			t.Fatal("actual original delegated publication history changed", i, err)
		}
	}
}
