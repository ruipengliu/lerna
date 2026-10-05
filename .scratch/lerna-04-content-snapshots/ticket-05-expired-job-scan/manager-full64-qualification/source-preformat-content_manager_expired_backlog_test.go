//go:build integration

package component_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	fixture "github.com/ruipengliu/lerna/conformance/internal/contentfixture"
	v "github.com/ruipengliu/lerna/contract/v1_2"
	"github.com/ruipengliu/lerna/domain/content"
)

type managerExpiredDuty struct {
	ref         v.ContentRef
	request     v.ContentPutRequest
	receipt     []byte
	observation content.BodyCleanupObservation
}

// This separate public Manager tracer leaves the accepted original 60f817
// publication / Lifecycle business case and its fixed input untouched.
func TestContentManagerPolicyPassesFullExpiredCleanupPageWithoutChangingOriginalDuties(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	w := fixture.New(t, ctx)
	service := contentService(t, w)
	duties := prepareManagerExpiredDuties(t, ctx, w, service)
	source := alphaRef
	source.ContentID = "manager-later-source"
	child := alphaRef
	child.ContentID = "manager-later-child"
	for i, ref := range []v.ContentRef{source, child} {
		installContentPolicy(t, ctx, w, ref)
		request := contentPut(t, ref, fmt.Sprintf("manager-later-publication-%d", i), "YWxwaGEK")
		if ref == child {
			request.Payload.Sources = []v.ContentRef{source}
		}
		if _, ok := putContentRequest(t, ctx, service, request).AsAccepted(); !ok {
			t.Fatal("Manager normal counterpart publication admission refused")
		}
		if worked, err := service.Step(ctx); err != nil || !worked {
			t.Fatal("Manager normal counterpart publication failed", worked, err)
		}
		assertContentBody(t, ctx, service, ref, nil, "alpha\n")
		assertBacklogPhysicalBody(t, w, ref, "alpha\n")
		if err := service.AuthorizeUse(ctx, &contentPrincipal, ref, "verification", "save"); err != nil {
			t.Fatal("Manager normal counterpart original saving basis refused", err)
		}
	}
	sealAndReopenManagerExpiredDuties(t, ctx, w, duties)
	service = contentService(t, w)
	manager := trustedContentManager(t, w, 2)
	wide := time.Now().Add(time.Hour).UTC().Truncate(time.Microsecond)
	policy := content.FixturePolicy{Ref: source, Subject: contentPrincipal, Purpose: "verification", Revision: 2, ValidUntil: wide, RetainUntil: wide, Read: true, Process: true, Save: false, Sync: true, Disclose: true}
	change, err := manager.InstallPolicy(ctx, &contentPrincipal, policy, 1)
	if err != nil || change.Key == "" || change.State != "pending" || change.Phase != "policy_change" || change.Watermark < 1 || !time.Now().Before(change.Deadline) {
		t.Fatal("later original policy change was not accepted with its fixed window", err)
	}
	for _, ref := range []v.ContentRef{source, child} {
		var rejected *v.ContractError
		if err = service.AuthorizeUse(ctx, &contentPrincipal, ref, "verification", "save"); !errors.As(err, &rejected) || rejected.Code != "forbidden" {
			t.Fatal("current original withdrawal did not close saving use", err)
		}
	}
	before, err := manager.ObserveChange(ctx, &contentPrincipal, change.Key, "", 2)
	if err != nil || before.Change.State != "pending" || len(before.Responsibilities) != 1 || before.Responsibilities[0].Ref != source {
		t.Fatal("later accepted change lacks its original source responsibility", err)
	}
	originalSourceDuty := before.Responsibilities[0]
	var propagated content.PropagationObservation
	for step := 0; step < 4; step++ {
		if _, err = manager.Step(ctx, &contentPrincipal); err != nil {
			t.Fatal("later actual Manager propagation step failed", err)
		}
		propagated, err = manager.ObserveChange(ctx, &contentPrincipal, change.Key, "", 2)
		if err != nil {
			t.Fatal(err)
		}
		if propagated.Change.State != "pending" {
			break
		}
	}
	// Propagation's normal terminal transition schedules the already fixed
// natural expiry. Original cleanup responsibilities keep the policy deadline.
	expectedChange := change
	expectedChange.State = "scheduled"
	expectedChange.Phase = "natural_expiry"
	expectedChange.Cursor = ""
	expectedChange.Due = change.ExpiryDue
	expectedChange.Deadline = change.ExpiryDeadline
	if !sameManagerPublicValue(t, propagated.Change, expectedChange) || len(propagated.Responsibilities) != 2 || propagated.NextCursor != "" {
		t.Fatalf("later Manager policy blocked by 64 expired original cleanup jobs: state=%q phase=%q responsibilities=%d exact_key=%v exact_watermark=%v", propagated.Change.State, propagated.Change.Phase, len(propagated.Responsibilities), propagated.Change.Key == change.Key, propagated.Change.Watermark == change.Watermark)
	}
	seen := map[v.ContentRef]bool{}
	for _, duty := range propagated.Responsibilities {
		if seen[duty.Ref] || (duty.Ref != source && duty.Ref != child) || duty.ChangeKey != change.Key || duty.BodyCleanup != "pending" || duty.Residual != "holder_unconfirmed" || !duty.ObjectHolder || duty.StagingHolder || duty.Publication != "published" || duty.Purpose != "verification" || !duty.Deadline.Equal(change.Deadline) || !sameManagerPublicValue(t, duty.Subject, contentPrincipal) {
			t.Fatal("actual propagation changed or falsely erased an original finite responsibility")
		}
		if duty.Ref == source && !sameManagerPublicValue(t, duty, originalSourceDuty) {
			t.Fatal("actual propagation replaced the fixed original source responsibility")
		}
		if !sameManagerPublicValue(t, duty.Actions, []string{"save"}) {
			t.Fatal("actual save-only revocation invented another action")
		}
		seen[duty.Ref] = true
		assertBacklogPhysicalBody(t, w, duty.Ref, "alpha\n")
	}
	if !seen[source] || !seen[child] {
		t.Fatal("actual original source and derived child responsibilities are incomplete")
	}
	w.Reopen(ctx)
	service = contentService(t, w)
	manager = trustedContentManager(t, w, 2)
	after, err := manager.ObserveChange(ctx, &contentPrincipal, change.Key, "", 2)
	if err != nil || !sameManagerPublicValue(t, after, propagated) {
		t.Fatal("real reopen changed original policy identity, watermark, deadlines or responsibility history", err)
	}
	assertManagerExpiredDuties(t, ctx, w, service, duties)
	for i, ref := range []v.ContentRef{source, child} {
		assertContentBody(t, ctx, service, ref, nil, "alpha\n")
		assertBacklogPhysicalBody(t, w, ref, "alpha\n")
		var rejected *v.ContractError
		if err = service.AuthorizeUse(ctx, &contentPrincipal, ref, "verification", "save"); !errors.As(err, &rejected) || rejected.Code != "forbidden" {
			t.Fatal("real reopen reopened revoked saving use", err)
		}
		command, err := service.GetCommand(ctx, contentCommandGetWire(t, v.ID(fmt.Sprintf("manager-later-publication-%d", i))), &contentPrincipal)
		found, ok := command.AsFound()
		progress, hasProgress := found.Progress.AsContent()
		if err != nil || !ok || !hasProgress || progress.ContentRef != ref || progress.Publication != "published" {
			t.Fatal("Manager propagation erased original later publication history", err)
		}
	}
}

func prepareManagerExpiredDuties(t *testing.T, ctx context.Context, w *fixture.World, service *content.Service) []managerExpiredDuty {
	t.Helper()
	duties := make([]managerExpiredDuty, 64)
	for i := range duties {
		ref := alphaRef
		ref.ContentID = v.ID(fmt.Sprintf("manager-expired-%02d", i))
		installContentPolicy(t, ctx, w, ref)
		request := contentPut(t, ref, fmt.Sprintf("manager-expired-publication-%02d", i), "YWxwaGEK")
		receipt := putContentRequest(t, ctx, service, request)
		if _, ok := receipt.AsAccepted(); !ok {
			t.Fatal("64-original preparation admission refused", i)
		}
		fixed, err := v.Encode(receipt)
		if err != nil {
			t.Fatal(err)
		}
		if worked, err := service.Step(ctx); err != nil || !worked {
			t.Fatal("64-original preparation publication failed", i, worked, err)
		}
		assertContentBody(t, ctx, service, ref, nil, "alpha\n")
		assertBacklogPhysicalBody(t, w, ref, "alpha\n")
		duties[i] = managerExpiredDuty{ref: ref, request: request, receipt: fixed}
	}
	return duties
}

func sealAndReopenManagerExpiredDuties(t *testing.T, ctx context.Context, w *fixture.World, duties []managerExpiredDuty) {
	t.Helper()
	lifecycle := bodyLifecycle(t, w)
	// Fixed once before the first original Seal, never renewed or raised.
	deadline := time.Now().Add(15 * time.Second).UTC().Truncate(time.Microsecond)
	for i := range duties {
		request := content.SealRequest{Ref: duties[i].ref, Purpose: "verification", SealID: fmt.Sprintf("manager-expired-seal-%02d", i), Deadline: deadline}
		observed, err := lifecycle.Seal(ctx, &contentPrincipal, request)
		if err != nil || observed.Seal.Ref != request.Ref || observed.Seal.ID != request.SealID || observed.Seal.Purpose != request.Purpose || !observed.Seal.Deadline.Equal(deadline) || observed.CleanupComplete || len(observed.Holders) != 2 || observed.NextCursor != "" {
			t.Fatal("64-original preparation seal/complete holders failed", i, err)
		}
		duties[i].observation = observed
	}
	waitUntil(t, ctx, deadline.Add(20*time.Millisecond))
	w.Reopen(ctx)
	assertManagerExpiredDuties(t, ctx, w, contentService(t, w), duties)
}

func assertManagerExpiredDuties(t *testing.T, ctx context.Context, w *fixture.World, service *content.Service, duties []managerExpiredDuty) {
	t.Helper()
	lifecycle := bodyLifecycle(t, w)
	for i, duty := range duties {
		observed, err := lifecycle.Observe(ctx, &contentPrincipal, duty.ref, "")
		if err != nil || !sameBacklogPublicObservation(t, observed, duty.observation) {
			t.Fatal("expired original full responsibility/holder/deadline changed", i, err)
		}
		assertBacklogPhysicalBody(t, w, duty.ref, "alpha\n")
		receipt, err := v.Encode(putContentRequest(t, ctx, service, duty.request))
		if err != nil || string(receipt) != string(duty.receipt) {
			t.Fatal("expired original immutable receipt changed", i, err)
		}
		command, err := service.GetCommand(ctx, contentCommandGetWire(t, duty.request.CommandID), &contentPrincipal)
		found, ok := command.AsFound()
		progress, hasProgress := found.Progress.AsContent()
		if err != nil || !ok || !hasProgress || progress.ContentRef != duty.ref || progress.Publication != "published" {
			t.Fatal("expired original publication history changed", i, err)
		}
	}
}

func sameManagerPublicValue(t *testing.T, actual, expected any) bool {
	t.Helper()
	a, err := json.Marshal(actual)
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(expected)
	if err != nil {
		t.Fatal(err)
	}
	return string(a) == string(b)
}
