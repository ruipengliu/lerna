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

func TestContentExpiredOriginalAcceptedCapStartsCleanupDespiteCurrentWideRenewal(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	w := fixture.New(t, ctx)
	m := trustedContentManager(t, w, 2)
	wide := time.Now().Add(time.Hour)
	policy := content.FixturePolicy{Ref: alphaRef, Subject: contentPrincipal, Purpose: "verification", Revision: 1, ValidUntil: wide, RetainUntil: wide, Read: true, Process: true, Save: true, Sync: true, Disclose: true}
	change, err := m.InstallPolicy(ctx, &contentPrincipal, policy, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err = w.Store().InstallFixtureCommandReader(ctx, contentOwner, contentPrincipal, wide); err != nil {
		t.Fatal(err)
	}
	cap := time.Now().Add(2 * time.Second).UTC().Truncate(time.Microsecond)
	request := contentPut(t, alphaRef, "accepted-cap-original", "YWxwaGEK")
	request.Payload.RetainUntil = v.Time(cap.Format("2006-01-02T15:04:05.000000Z"))
	service := contentService(t, w)
	receipt := putContentRequest(t, ctx, service, request)
	if _, ok := receipt.AsAccepted(); !ok {
		t.Fatal("normal original short cap refused")
	}
	if _, err = service.Step(ctx); err != nil {
		t.Fatal(err)
	}
	assertContentBody(t, ctx, service, alphaRef, nil, "alpha\n")
	scheduled, err := m.ObserveChange(ctx, &contentPrincipal, change.Key, "", 2)
	if err != nil || scheduled.Change.State != "scheduled" || !scheduled.Change.Due.Equal(cap) || !scheduled.Change.ExpiryDue.Equal(cap) {
		t.Fatal("accepted cap did not fix original finite maintenance", scheduled, err)
	}
	// A current wide save renewal cannot raise the already accepted original
	// cap. It still supplies current policy and a normal independent Version2.
	policy.Revision = 2
	if _, err = m.InstallPolicy(ctx, &contentPrincipal, policy, 1); err != nil {
		t.Fatal(err)
	}
	if _, err = service.Step(ctx); err != nil {
		t.Fatal(err)
	}
	version2 := alphaRef
	version2.Version = "2"
	version2.Hash = "sha256:f2c82decdd7181cf98945929a62598db7e6b477e11f6e0eb0ae97020eff151ad"
	version2.ByteLength = "5"
	installContentPolicy(t, ctx, w, version2)
	if _, ok := putContentRequest(t, ctx, service, contentPut(t, version2, "accepted-cap-version2", "YmV0YQo=")).AsAccepted(); !ok {
		t.Fatal("normal independent Version2 refused")
	}
	if _, err = service.Step(ctx); err != nil {
		t.Fatal(err)
	}
	assertContentBody(t, ctx, service, version2, nil, "beta\n")
	before, err := v.Encode(receipt)
	if err != nil {
		t.Fatal(err)
	}
	w.Reopen(ctx)
	waitUntil(t, ctx, cap.Add(20*time.Millisecond))
	m = trustedContentManager(t, w, 2)
	if worked, err := m.Step(ctx, &contentPrincipal); err != nil || !worked {
		t.Fatal("real original accepted cap due did not execute", worked, err)
	}
	original, err := m.ObserveChange(ctx, &contentPrincipal, change.Key, "", 2)
	if err != nil || len(original.Responsibilities) != 1 || original.Responsibilities[0].Ref != alphaRef || original.Responsibilities[0].BodyCleanup != "pending" || original.Responsibilities[0].Reason != "accepted_retention_expired" || !original.Responsibilities[0].ObjectHolder || !original.Change.ExpiryDeadline.Equal(scheduled.Change.ExpiryDeadline) {
		t.Fatal("actual expired cap lost original responsibility", original, err)
	}
	if !time.Now().Before(original.Responsibilities[0].Deadline) {
		t.Fatal("original cap cleanup window expired before consumer qualification")
	}
	service = contentService(t, w)
	view, err := service.Get(ctx, contentGetWire(t, alphaRef, nil), &contentPrincipal)
	denied, ok := view.AsRejected()
	if err != nil || !ok || denied.Reason != "expired" {
		t.Fatal("wide renewal revived original accepted cap", view, err)
	}
	assertContentBody(t, ctx, service, version2, nil, "beta\n")
	_, key, err := content.VersionIdentity(alphaRef)
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(w.Directory, key))
	if err != nil || string(body) != "alpha\n" {
		t.Fatal("pending cap was mistaken for physical erase", err)
	}
	l := bodyLifecycle(t, w)
	if _, err = l.ConsumePolicyCleanup(ctx, &contentPrincipal, change.Key, ""); err != nil {
		t.Fatal(err)
	}
	sealed, err := l.Observe(ctx, &contentPrincipal, alphaRef, "")
	if err != nil || sealed.Seal.PolicyChangeKey != change.Key || sealed.CleanupComplete || !sealed.Seal.Deadline.Equal(original.Responsibilities[0].Deadline) {
		t.Fatal("definite expired accepted cap did not start original cleanup", sealed, err)
	}
	for i := 0; i < 3; i++ {
		if _, err = l.Step(ctx, &contentPrincipal); err != nil {
			t.Fatal(err)
		}
	}
	w.Reopen(ctx)
	ack, err := trustedContentManager(t, w, 2).ObserveChange(ctx, &contentPrincipal, change.Key, "", 2)
	if err != nil || len(ack.Responsibilities) != 1 || ack.Responsibilities[0].BodyCleanup != "erased" || ack.Responsibilities[0].Reason != "accepted_retention_expired" || !ack.Responsibilities[0].Deadline.Equal(original.Responsibilities[0].Deadline) {
		t.Fatal("actual ACK changed original cap cause/window", ack, err)
	}
	if _, err = os.ReadFile(filepath.Join(w.Directory, key)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("ACK did not correspond to independent original physical absence", err)
	}
	complete, err := bodyLifecycle(t, w).Observe(ctx, &contentPrincipal, alphaRef, "")
	if err != nil || !complete.CleanupComplete || complete.Seal.ID != sealed.Seal.ID || !complete.Seal.Deadline.Equal(sealed.Seal.Deadline) {
		t.Fatal("reopen lost original cap seal/all-holder ACK", complete, err)
	}
	service = contentService(t, w)
	assertContentBody(t, ctx, service, version2, nil, "beta\n")
	after, err := v.Encode(putContentRequest(t, ctx, service, request))
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("expired original cap changed fixed receipt")
	}
}
