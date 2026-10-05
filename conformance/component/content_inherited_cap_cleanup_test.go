//go:build integration

package component_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	fixture "github.com/ruipengliu/lerna/conformance/internal/contentfixture"
	v "github.com/ruipengliu/lerna/contract/v1_2"
	"github.com/ruipengliu/lerna/domain/content"
)

func TestContentInheritedExpiredCapConsumesItsExactAdmissionTargetWithoutDeletingLiveAncestor(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	w := fixture.New(t, ctx)
	manager := trustedContentManager(t, w, 2)
	service := contentService(t, w)
	wide := time.Now().Add(time.Hour)
	savingPolicy := func(ref v.ContentRef) content.FixturePolicy {
		return content.FixturePolicy{Ref: ref, Subject: contentPrincipal, Purpose: "verification", Revision: 1, ValidUntil: wide, RetainUntil: wide, Read: true, Process: true, Save: true, Sync: true, Disclose: true}
	}
	policyA := savingPolicy(alphaRef)
	if _, err := manager.InstallPolicy(ctx, &contentPrincipal, policyA, 0); err != nil {
		t.Fatal(err)
	}
	if err := w.Store().InstallFixtureCommandReader(ctx, contentOwner, contentPrincipal, wide); err != nil {
		t.Fatal(err)
	}
	cap := time.Now().UTC().Add(2 * time.Second).Truncate(time.Microsecond)
	rootRequest := contentPut(t, alphaRef, "inherited-cap-original-root", "YWxwaGEK")
	rootRequest.Payload.RetainUntil = v.Time(cap.Format("2006-01-02T15:04:05.000000Z"))
	rootReceipt := putContentRequest(t, ctx, service, rootRequest)
	if _, ok := rootReceipt.AsAccepted(); !ok {
		t.Fatal("normal short-cap source refused")
	}
	if _, err := service.Step(ctx); err != nil {
		t.Fatal(err)
	}
	assertContentBody(t, ctx, service, alphaRef, nil, "alpha\n")
	policyA.Revision = 2
	renewal, err := manager.InstallPolicy(ctx, &contentPrincipal, policyA, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.Step(ctx); err != nil {
		t.Fatal(err)
	}
	live := alphaRef
	live.ContentID = "live-cap-ancestor"
	if _, err = manager.InstallPolicy(ctx, &contentPrincipal, savingPolicy(live), 0); err != nil {
		t.Fatal(err)
	}
	liveRequest := contentPut(t, live, "inherited-cap-live-ancestor", "YWxwaGEK")
	liveReceipt := putContentRequest(t, ctx, service, liveRequest)
	if _, ok := liveReceipt.AsAccepted(); !ok {
		t.Fatal("normal independent live ancestor refused")
	}
	if _, err = service.Step(ctx); err != nil {
		t.Fatal(err)
	}
	assertContentBody(t, ctx, service, live, nil, "alpha\n")
	target := alphaRef
	target.ContentID = "inherited-cap-derived"
	if _, err = manager.InstallPolicy(ctx, &contentPrincipal, savingPolicy(target), 0); err != nil {
		t.Fatal(err)
	}
	request := contentPut(t, target, "inherited-cap-original-target", "YWxwaGEK")
	request.Payload.Sources = []v.ContentRef{alphaRef, live}
	receipt := putContentRequest(t, ctx, service, request)
	accepted, ok := receipt.AsAccepted()
	if !ok || accepted.RetainUntil != rootRequest.Payload.RetainUntil {
		t.Fatal("actual target admission did not inherit original accepted cap", receipt)
	}
	if _, err = service.Step(ctx); err != nil {
		t.Fatal(err)
	}
	assertContentBody(t, ctx, service, target, nil, "alpha\n")
	if err = service.AuthorizeUse(ctx, &contentPrincipal, target, "verification", "save"); err != nil {
		t.Fatal("normal complete two-ancestor saving scope refused", err)
	}
	originalRequests := []v.ContentPutRequest{rootRequest, liveRequest, request}
	originalReceipts := []v.CommandReceipt{rootReceipt, liveReceipt, receipt}
	fixedReceipts := make([][]byte, len(originalReceipts))
	for i, original := range originalReceipts {
		fixedReceipts[i], err = v.Encode(original)
		if err != nil {
			t.Fatal(err)
		}
	}
	version2 := alphaRef
	version2.Version = "2"
	version2.Hash = "sha256:f2c82decdd7181cf98945929a62598db7e6b477e11f6e0eb0ae97020eff151ad"
	version2.ByteLength = "5"
	installContentPolicy(t, ctx, w, version2)
	if _, ok := putContentRequest(t, ctx, service, contentPut(t, version2, "inherited-cap-independent-version", "YmV0YQo=")).AsAccepted(); !ok {
		t.Fatal("normal independent Version2 refused")
	}
	if _, err = service.Step(ctx); err != nil {
		t.Fatal(err)
	}
	assertContentBody(t, ctx, service, version2, nil, "beta\n")
	var admission *content.PolicyChange
	cursor := ""
	for i := 0; i < 4; i++ {
		changes, next, err := manager.ObserveAdmissionChanges(ctx, &contentPrincipal, target, cursor, 2)
		if err != nil {
			t.Fatal(err)
		}
		for _, change := range changes {
			if change.Policy.Ref != alphaRef || !change.ExpiryDue.Equal(cap) {
				continue
			}
			if admission != nil {
				t.Fatal("duplicate exact original inherited-cap duty", changes)
			}
			if change.AdmissionTarget == nil || *change.AdmissionTarget != target || change.Policy.Revision != renewal.Policy.Revision || !reflect.DeepEqual(change.Policy.Subject, policyA.Subject) || change.Policy.Purpose != policyA.Purpose || !change.ExpiryDeadline.Equal(cap.Add(time.Minute)) {
				t.Fatal("original admission target/source basis/deadline not fixed", change)
			}
			copy := change
			admission = &copy
		}
		cursor = next
		if cursor == "" {
			break
		}
	}
	if admission == nil || cursor != "" {
		t.Fatal("all original admission pages did not reveal exact inherited cause", admission, cursor)
	}
	stable, err := manager.ObserveChange(ctx, &contentPrincipal, admission.Key, "", 2)
	if err != nil || stable.Change.AdmissionTarget == nil || *stable.Change.AdmissionTarget != target || stable.Change.Watermark != admission.Watermark || !stable.Change.ExpiryDeadline.Equal(admission.ExpiryDeadline) {
		t.Fatal("original admission identity unavailable", stable, err)
	}
	if !time.Now().Before(cap) {
		t.Fatal("original cap already elapsed before early-consume control")
	}
	l := bodyLifecycle(t, w)
	early, err := l.ConsumePolicyCleanup(ctx, &contentPrincipal, admission.Key, "")
	if err != nil || !reflect.DeepEqual(early, stable) {
		t.Fatal("before-cap consume changed the exact original admission", early, err)
	}
	if _, err = l.Observe(ctx, &contentPrincipal, target, ""); !errors.Is(err, content.ErrUnavailable) {
		t.Fatal("before-cap original admission created a target seal", err)
	}
	for _, ref := range []v.ContentRef{alphaRef, live, target} {
		assertContentBody(t, ctx, service, ref, nil, "alpha\n")
	}
	assertContentBody(t, ctx, service, version2, nil, "beta\n")
	w.Reopen(ctx)
	manager = trustedContentManager(t, w, 2)
	service = contentService(t, w)
	waitUntil(t, ctx, cap.Add(20*time.Millisecond))
	var pending content.PropagationObservation
	found := false
	for i := 0; i < 12; i++ {
		if _, err = manager.Step(ctx, &contentPrincipal); err != nil {
			t.Fatal(err)
		}
		pending, err = manager.ObserveChange(ctx, &contentPrincipal, admission.Key, "", 2)
		if err != nil {
			t.Fatal(err)
		}
		if len(pending.Responsibilities) == 1 && pending.Responsibilities[0].BodyCleanup == "pending" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("actual original inherited due did not register target responsibility", pending)
	}
	duty := pending.Responsibilities[0]
	if duty.Ref != target || !reflect.DeepEqual(duty.Subject, contentPrincipal) || duty.Purpose != "verification" || duty.BodyCleanup != "pending" || duty.Residual != "holder_unconfirmed" || duty.Reason != "accepted_retention_expired" || !duty.ObjectHolder || duty.Publication != "published" || !duty.Deadline.Equal(admission.ExpiryDeadline) || !time.Now().Before(duty.Deadline) {
		t.Fatal("exact original inherited target duty malformed", duty)
	}
	// The source's current wide renewal and live second ancestor do not raise
	// either the original source cap or the actually accepted descendant cap.
	for _, ref := range []v.ContentRef{alphaRef, target} {
		view, err := service.Get(ctx, contentGetWire(t, ref, nil), &contentPrincipal)
		denied, ok := view.AsRejected()
		if err != nil || !ok || denied.Reason != "expired" {
			t.Fatal("wide renewal revived inherited accepted cap", ref, view, err)
		}
	}
	assertContentBody(t, ctx, service, live, nil, "alpha\n")
	assertContentBody(t, ctx, service, version2, nil, "beta\n")
	for _, ref := range []v.ContentRef{alphaRef, live, target} {
		_, key, err := content.VersionIdentity(ref)
		if err != nil {
			t.Fatal(err)
		}
		body, err := os.ReadFile(filepath.Join(w.Directory, key))
		if err != nil || string(body) != "alpha\n" {
			t.Fatal("pending inherited duty was mistaken for erase", ref, err)
		}
	}
	l = bodyLifecycle(t, w)
	other := contentPrincipal
	other.SubjectID = "inherited-cap-other-subject"
	_, err = l.ConsumePolicyCleanup(ctx, &other, admission.Key, "")
	var forbidden *v.ContractError
	if !errors.As(err, &forbidden) || forbidden.Code != "forbidden" {
		t.Fatal("another real subject consumed the original target duty", err)
	}
	unchanged, err := manager.ObserveChange(ctx, &contentPrincipal, admission.Key, "", 2)
	if err != nil || !reflect.DeepEqual(unchanged, pending) {
		t.Fatal("forbidden consumption changed the original pending duty", unchanged, err)
	}
	if _, err = l.ConsumePolicyCleanup(ctx, &contentPrincipal, admission.Key, ""); err != nil {
		t.Fatal("exact original admission target cannot consume definite expired inherited cap", err)
	}
	sealed, err := l.Observe(ctx, &contentPrincipal, target, "")
	if err != nil || sealed.Seal.PolicyChangeKey != admission.Key || !sealed.Seal.Deadline.Equal(duty.Deadline) {
		t.Fatal("inherited cleanup changed original cause or deadline", sealed, err)
	}
	for i := 0; i < 3; i++ {
		if _, err = l.Step(ctx, &contentPrincipal); err != nil {
			t.Fatal(err)
		}
	}
	w.Reopen(ctx)
	l = bodyLifecycle(t, w)
	all, err := l.Observe(ctx, &contentPrincipal, target, "")
	if err != nil || !all.CleanupComplete || all.Seal.ID != sealed.Seal.ID || all.Seal.PolicyChangeKey != admission.Key || !all.Seal.Deadline.Equal(duty.Deadline) {
		t.Fatal("reopen lost original inherited allACK", all, err)
	}
	ack, err := trustedContentManager(t, w, 2).ObserveChange(ctx, &contentPrincipal, admission.Key, "", 2)
	if err != nil || len(ack.Responsibilities) != 1 || ack.Responsibilities[0].BodyCleanup != "erased" || ack.Responsibilities[0].Reason != duty.Reason || !ack.Responsibilities[0].Deadline.Equal(duty.Deadline) || ack.Change.Watermark != admission.Watermark {
		t.Fatal("inherited exact original duty ACK changed identity/history", ack, err)
	}
	_, key, err := content.VersionIdentity(target)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = os.ReadFile(filepath.Join(w.Directory, key)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("target allACK lacks independent physical absence", err)
	}
	for _, ref := range []v.ContentRef{alphaRef, live} {
		_, ancestorKey, err := content.VersionIdentity(ref)
		if err != nil {
			t.Fatal(err)
		}
		body, err := os.ReadFile(filepath.Join(w.Directory, ancestorKey))
		if err != nil || string(body) != "alpha\n" {
			t.Fatal("target cleanup erased a distinct ancestor", ref, err)
		}
		if _, err = l.Observe(ctx, &contentPrincipal, ref, ""); !errors.Is(err, content.ErrUnavailable) {
			t.Fatal("target duty created ancestor seal", ref, err)
		}
	}
	service = contentService(t, w)
	assertContentBody(t, ctx, service, live, nil, "alpha\n")
	assertContentBody(t, ctx, service, version2, nil, "beta\n")
	for i, request := range originalRequests {
		after, err := v.Encode(putContentRequest(t, ctx, service, request))
		if err != nil || string(after) != string(fixedReceipts[i]) {
			t.Fatal("inherited cleanup changed original receipt", err)
		}
		command, err := service.GetCommand(ctx, contentCommandGetWire(t, request.CommandID), &contentPrincipal)
		original, ok := command.AsFound()
		progress, published := original.Progress.AsContent()
		if err != nil || !ok || !published || progress.Publication != "published" {
			t.Fatal("inherited cleanup changed original publication history", command, err)
		}
	}
}
