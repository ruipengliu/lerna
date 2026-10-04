//go:build integration

package component_test

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	fixture "github.com/ruipengliu/lerna/conformance/internal/contentfixture"
	v "github.com/ruipengliu/lerna/contract/v1_2"
	"github.com/ruipengliu/lerna/domain/content"
)

func TestContentOriginalPolicyAndHolderPagesResumeWithoutErasingOtherSavingBasis(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	w := fixture.New(t, ctx)
	secondary := fixture.New(t, ctx)
	wide := time.Now().Add(time.Hour).UTC().Truncate(time.Microsecond)
	m := trustedContentManager(t, w, 2)
	policy := func(ref v.ContentRef, subject v.SubjectBinding) content.FixturePolicy {
		return content.FixturePolicy{Ref: ref, Subject: subject, Purpose: "verification", Revision: 1, ValidUntil: wide, RetainUntil: wide, Read: true, Process: true, Save: true, Sync: true, Disclose: true}
	}
	rootPolicy := policy(alphaRef, contentPrincipal)
	if _, err := m.InstallPolicy(ctx, &contentPrincipal, rootPolicy, 0); err != nil {
		t.Fatal(err)
	}
	if err := w.Store().InstallFixtureCommandReader(ctx, contentOwner, contentPrincipal, wide); err != nil {
		t.Fatal(err)
	}
	other := v.SubjectBinding{TenantID: contentOwner.TenantID, SubjectID: "pages-other-writer", DelegationChain: []v.DelegatedSubject{{TenantID: contentOwner.TenantID, SubjectID: "pages-other-delegator"}}}
	otherManager, err := content.NewManager(content.ManagementConfig{Owner: contentOwner, Store: w.Store(), TrustedSubject: other, TrustedUntil: wide, PageSize: 2, WorkBudget: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = otherManager.InstallPolicy(ctx, &other, policy(alphaRef, other), 0); err != nil {
		t.Fatal(err)
	}
	if err = w.Store().InstallFixtureCommandReader(ctx, contentOwner, other, wide); err != nil {
		t.Fatal(err)
	}
	service := contentService(t, w)
	if _, ok := putContentRequest(t, ctx, service, contentPut(t, alphaRef, "pages-root", "YWxwaGEK")).AsAccepted(); !ok {
		t.Fatal("normal root refused")
	}
	if _, err = service.Step(ctx); err != nil {
		t.Fatal(err)
	}
	refs := []v.ContentRef{alphaRef}
	for i := 0; i < 3; i++ {
		ref := alphaRef
		ref.ContentID = v.ID(fmt.Sprintf("pages-descendant-%d", i))
		refs = append(refs, ref)
		if _, err = m.InstallPolicy(ctx, &contentPrincipal, policy(ref, contentPrincipal), 0); err != nil {
			t.Fatal(err)
		}
		request := contentPut(t, ref, string(ref.ContentID), "YWxwaGEK")
		request.Payload.Sources = []v.ContentRef{alphaRef}
		if _, ok := putContentRequest(t, ctx, service, request).AsAccepted(); !ok {
			t.Fatal("normal descendant refused", ref)
		}
		if _, err = service.Step(ctx); err != nil {
			t.Fatal(err)
		}
		assertContentBody(t, ctx, service, ref, nil, "alpha\n")
	}
	otherRef := alphaRef
	otherRef.ContentID = "pages-other-basis"
	if _, err = otherManager.InstallPolicy(ctx, &other, policy(otherRef, other), 0); err != nil {
		t.Fatal(err)
	}
	otherRequest := contentPut(t, otherRef, "pages-other-basis", "YWxwaGEK")
	otherRequest.Payload.Sources = []v.ContentRef{alphaRef}
	raw, err := v.Encode(otherRequest)
	if err != nil {
		t.Fatal(err)
	}
	out, err := service.Put(ctx, raw, &other)
	received, ok := out.AsReceived()
	if err != nil || !ok {
		t.Fatal("normal other saving basis transport failed", out, err)
	}
	if _, ok := received.Receipt.AsAccepted(); !ok {
		t.Fatal("normal other saving basis refused", received)
	}
	if _, err = service.Step(ctx); err != nil {
		t.Fatal(err)
	}
	assertOther := func() {
		t.Helper()
		view, err := service.Get(ctx, contentGetWire(t, otherRef, nil), &other)
		published, ok := view.AsPublished()
		if err != nil || !ok {
			t.Fatal("independent current saving basis lost body", view, err)
		}
		bytes, err := base64.StdEncoding.DecodeString(published.BytesBase64)
		if err != nil || string(bytes) != "alpha\n" || published.ContentRef != otherRef {
			t.Fatal("other saving basis original bytes changed", err)
		}
	}
	assertOther()
	assertOtherRetained := func() {
		t.Helper()
		view, err := service.Get(ctx, contentGetWire(t, otherRef, nil), &other)
		denied, ok := view.AsRejected()
		if err != nil || !ok || denied.Reason != "forbidden" {
			t.Fatal("sealed ancestor did not close other subject's new body use", view, err)
		}
		_, key, err := content.VersionIdentity(otherRef)
		if err != nil {
			t.Fatal(err)
		}
		bytes, err := os.ReadFile(filepath.Join(w.Directory, key))
		if err != nil || string(bytes) != "alpha\n" {
			t.Fatal("source actor erased another subject's saved body", err)
		}
	}
	version2 := alphaRef
	version2.Version = "2"
	version2.Hash = "sha256:f2c82decdd7181cf98945929a62598db7e6b477e11f6e0eb0ae97020eff151ad"
	version2.ByteLength = "5"
	if _, err = m.InstallPolicy(ctx, &contentPrincipal, policy(version2, contentPrincipal), 0); err != nil {
		t.Fatal(err)
	}
	if _, ok := putContentRequest(t, ctx, service, contentPut(t, version2, "pages-version2", "YmV0YQo=")).AsAccepted(); !ok {
		t.Fatal("normal independent Version2 refused")
	}
	if _, err = service.Step(ctx); err != nil {
		t.Fatal(err)
	}
	assertContentBody(t, ctx, service, version2, nil, "beta\n")
	config := content.LifecycleConfig{ManagementConfig: content.ManagementConfig{Owner: contentOwner, Store: w.Store(), TrustedSubject: contentPrincipal, TrustedUntil: wide, PageSize: 2, WorkBudget: time.Minute}, PrimaryHolderID: "primary", Objects: w.Objects, SecondaryHolderID: "secondary", SecondaryObjects: secondary.Objects, Worker: "pages-cleanup"}
	l, err := content.NewLifecycle(config)
	if err != nil {
		t.Fatal(err)
	}
	copyRequest := content.CopyRequest{Ref: refs[1], Purpose: "verification", ID: "pages-original-copy", Deadline: time.Now().Add(time.Minute).UTC().Truncate(time.Microsecond)}
	copy, err := l.CopyToSecondary(ctx, &contentPrincipal, copyRequest)
	if err != nil || !copy.Confirmed {
		t.Fatal("normal independent copy refused", copy, err)
	}
	_, copyKey, err := content.VersionIdentity(refs[1])
	if err != nil {
		t.Fatal(err)
	}
	primaryInfo, err := os.Stat(filepath.Join(w.Directory, copyKey))
	if err != nil {
		t.Fatal(err)
	}
	secondaryInfo, err := os.Stat(filepath.Join(secondary.Directory, copyKey))
	if err != nil || os.SameFile(primaryInfo, secondaryInfo) {
		t.Fatal("copy is not independent physical bytes", err)
	}
	copyBytes, err := os.ReadFile(filepath.Join(secondary.Directory, copyKey))
	if err != nil || string(copyBytes) != "alpha\n" {
		t.Fatal("independent copy bytes missing", err)
	}
	if err = secondary.Objects.Close(); err != nil {
		t.Fatal(err)
	}
	config.SecondaryObjects = nil
	rootPolicy.Revision = 2
	rootPolicy.Save = false
	change, err := m.InstallPolicy(ctx, &contentPrincipal, rootPolicy, 1)
	if err != nil {
		t.Fatal(err)
	}
	var first content.PropagationObservation
	for i := 0; i < 8; i++ {
		worked, err := m.Step(ctx, &contentPrincipal)
		if err != nil {
			t.Fatal(err)
		}
		first, err = m.ObserveChange(ctx, &contentPrincipal, change.Key, "", 2)
		if err != nil {
			t.Fatal(err)
		}
		if first.Change.Cursor != "" {
			break
		}
		if !worked || first.Change.State != "pending" {
			t.Fatal("original change did not reach its actual bounded partial page", worked, first)
		}
	}
	if first.Change.Cursor == "" || first.NextCursor == "" || len(first.Responsibilities) != 2 || first.Change.Watermark != change.Watermark || !first.Change.Deadline.Equal(change.Deadline) {
		t.Fatal("first partial propagation page lost original identity/window", first)
	}
	firstTail, err := m.ObserveChange(ctx, &contentPrincipal, change.Key, first.NextCursor, 2)
	if err != nil || len(first.Responsibilities)+len(firstTail.Responsibilities) <= 2 {
		t.Fatal("original partial cursor had no actual responsibilities beyond one page", firstTail, err)
	}
	w.Reopen(ctx)
	m = trustedContentManager(t, w, 2)
	for i := 0; i < 4; i++ {
		obs, err := m.ObserveChange(ctx, &contentPrincipal, change.Key, "", 2)
		if err != nil {
			t.Fatal(err)
		}
		if obs.Change.State != "pending" {
			break
		}
		if _, err = m.Step(ctx, &contentPrincipal); err != nil {
			t.Fatal(err)
		}
	}
	seen := map[v.ContentRef]bool{}
	cursor := ""
	for i := 0; i < 4; i++ {
		obs, err := m.ObserveChange(ctx, &contentPrincipal, change.Key, cursor, 2)
		if err != nil || obs.Change.State == "pending" || obs.Change.Watermark != change.Watermark || !obs.Change.ExpiryDeadline.Equal(change.ExpiryDeadline) {
			t.Fatal("remaining propagation pages changed original facts", obs, err)
		}
		for _, responsibility := range obs.Responsibilities {
			if seen[responsibility.Ref] || !responsibility.Deadline.Equal(change.Deadline) {
				t.Fatal("duplicate/refreshed original responsibility", responsibility)
			}
			seen[responsibility.Ref] = true
			if responsibility.Ref == otherRef {
				if responsibility.BodyCleanup != "not_required" {
					t.Fatal("another saving subject was selected for erase", responsibility)
				}
			} else if responsibility.BodyCleanup != "pending" || !responsibility.ObjectHolder {
				t.Fatal("original holder responsibility lost", responsibility)
			}
		}
		cursor = obs.NextCursor
		if cursor == "" {
			break
		}
	}
	if len(seen) != len(refs)+1 || !seen[otherRef] {
		t.Fatal("not every actual original descendant was registered", seen)
	}
	for _, ref := range refs {
		if !seen[ref] {
			t.Fatal("missing original propagated ref", ref)
		}
	}
	service = contentService(t, w)
	assertOther() // A's boolean revoke does not revoke B's complete saving basis.
	config.Store = w.Store()
	config.Objects = w.Objects
	l, err = content.NewLifecycle(config)
	if err != nil {
		t.Fatal(err)
	}
	cursor = ""
	for i := 0; i < 4; i++ {
		obs, err := l.ConsumePolicyCleanup(ctx, &contentPrincipal, change.Key, cursor)
		if err != nil {
			t.Fatal(err)
		}
		cursor = obs.NextCursor
		if cursor == "" {
			break
		}
	}
	for _, ref := range refs {
		obs, err := l.Observe(ctx, &contentPrincipal, ref, "")
		if err != nil || obs.CleanupComplete || obs.Seal.Ref != ref || obs.Seal.PolicyChangeKey != change.Key || !obs.Seal.Deadline.Equal(change.Deadline) {
			t.Fatal("all-page consumer lost an original seal/duty", ref, obs, err)
		}
	}
	service = contentService(t, w)
	assertOtherRetained()
	assertContentBody(t, ctx, service, version2, nil, "beta\n")
	offlineFailure := false
	for i := 0; i < 12; i++ {
		worked, err := l.Step(ctx, &contentPrincipal)
		if errors.Is(err, content.ErrUnavailable) {
			offlineFailure = true
			break
		}
		if err != nil || !worked {
			t.Fatal("normal original cleanup before offline copy failed", worked, err)
		}
	}
	if !offlineFailure {
		t.Fatal("actually closed copy did not remain unconfirmed")
	}
	page, err := l.Observe(ctx, &contentPrincipal, refs[1], "")
	if err != nil || page.CleanupComplete || len(page.Holders) != 2 || page.NextCursor == "" {
		t.Fatal("first holder page falsely completed all three holders", page, err)
	}
	ackKinds := map[string]bool{}
	for _, holder := range page.Holders {
		if holder.State != "erased" || holder.Identity.Ref != refs[1] || holder.Identity.SealID != page.Seal.ID || !holder.Deadline.Equal(change.Deadline) {
			t.Fatal("original primary/staging did not ACK before offline copy", holder)
		}
		switch holder.Kind {
		case "pg-staging":
			if holder.Identity.HolderID != "postgres-staging" {
				t.Fatal("wrong original staging holder", holder)
			}
		case "primary":
			if holder.Identity.HolderID != "primary" || holder.Identity.Binding != w.Objects.Binding() || holder.Identity.ObjectKey != copyKey {
				t.Fatal("wrong original physical primary holder", holder)
			}
		default:
			t.Fatal("first actual page was not primary/staging", holder)
		}
		ackKinds[holder.Kind] = true
	}
	if !ackKinds["primary"] || !ackKinds["pg-staging"] {
		t.Fatal("first holder page did not contain both original ACKs", page)
	}
	last, err := l.Observe(ctx, &contentPrincipal, refs[1], page.NextCursor)
	if err != nil || last.CleanupComplete || last.NextCursor != "" || len(last.Holders) != 1 {
		t.Fatal("last holder page lost independent offline duty", last, err)
	}
	holder := last.Holders[0]
	if holder.Kind != "secondary" || holder.State != "residual" || holder.Responsible != "secondary" || holder.CopyID != copyRequest.ID || holder.Identity.Ref != refs[1] || holder.Identity.HolderID != "secondary" || holder.Identity.SealID != page.Seal.ID || holder.Identity.ObjectKey != copyKey || holder.Identity.Binding != copy.Binding || !holder.EffectDeadline.Equal(copyRequest.Deadline) || !holder.Deadline.Equal(change.Deadline) {
		t.Fatal("offline page changed original physical copy responsibility", holder)
	}
	if _, err = os.ReadFile(filepath.Join(w.Directory, copyKey)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("primary not really erased at offline frontier", err)
	}
	copyBytes, err = os.ReadFile(filepath.Join(secondary.Directory, copyKey))
	if err != nil || string(copyBytes) != "alpha\n" {
		t.Fatal("offline copy was falsely erased", err)
	}
	deferredAt := time.Now()
	w.Reopen(ctx)
	secondary.Reopen(ctx)
	m = trustedContentManager(t, w, 2)
	config.Store = w.Store()
	config.Objects = w.Objects
	config.SecondaryObjects = secondary.Objects
	l, err = content.NewLifecycle(config)
	if err != nil {
		t.Fatal(err)
	}
	waitUntil(t, ctx, deferredAt.Add(120*time.Millisecond))
	for i := 0; i < 12; i++ {
		worked, err := l.Step(ctx, &contentPrincipal)
		if err != nil {
			t.Fatal(err)
		}
		if !worked {
			break
		}
	}
	for _, ref := range refs {
		obs, err := l.Observe(ctx, &contentPrincipal, ref, "")
		if err != nil || !obs.CleanupComplete || !obs.Seal.Deadline.Equal(change.Deadline) {
			t.Fatal("remaining original holder pages did not recover", ref, obs, err)
		}
		_, key, err := content.VersionIdentity(ref)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = os.ReadFile(filepath.Join(w.Directory, key)); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("all-page ACK left original primary bytes", ref, err)
		}
	}
	if _, err = os.ReadFile(filepath.Join(secondary.Directory, copyKey)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("all-page ACK left original independent copy", err)
	}
	cursor = ""
	acked := 0
	for i := 0; i < 4; i++ {
		obs, err := m.ObserveChange(ctx, &contentPrincipal, change.Key, cursor, 2)
		if err != nil {
			t.Fatal(err)
		}
		for _, responsibility := range obs.Responsibilities {
			if responsibility.Ref != otherRef {
				if responsibility.BodyCleanup != "erased" || !responsibility.Deadline.Equal(change.Deadline) {
					t.Fatal("all-page physical ACK lost original policy responsibility", responsibility)
				}
				acked++
			} else if responsibility.BodyCleanup != "not_required" {
				t.Fatal("other saving basis responsibility changed", responsibility)
			}
		}
		cursor = obs.NextCursor
		if cursor == "" {
			break
		}
	}
	if acked != len(refs) {
		t.Fatal("not every original policy responsibility received ACK", acked)
	}
	service = contentService(t, w)
	assertOtherRetained()
	assertContentBody(t, ctx, service, version2, nil, "beta\n")
}
