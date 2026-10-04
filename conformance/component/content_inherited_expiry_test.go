//go:build integration

package component_test

import (
	"context"
	"fmt"
	fixture "github.com/ruipengliu/lerna/conformance/internal/contentfixture"
	v "github.com/ruipengliu/lerna/contract/v1_2"
	"github.com/ruipengliu/lerna/domain/content"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestContentLaterAdmissionInheritsOriginalAncestorExpiry(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	w := fixture.New(t, ctx)
	m := trustedContentManager(t, w, 2)
	service := contentService(t, w)
	wide := time.Now().Add(time.Hour)
	due := time.Now().UTC().Add(2 * time.Second).Truncate(time.Microsecond)
	p := content.FixturePolicy{Ref: alphaRef, Subject: contentPrincipal, Purpose: "verification", Revision: 1, ValidUntil: due, RetainUntil: wide, Read: true, Process: true, Save: true, Sync: true, Disclose: true}
	original, err := m.InstallPolicy(ctx, &contentPrincipal, p, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err = w.Store().InstallFixtureCommandReader(ctx, contentOwner, contentPrincipal, wide); err != nil {
		t.Fatal(err)
	}
	if _, ok := putContentRequest(t, ctx, service, contentPut(t, alphaRef, "expiry-source", "YWxwaGEK")).AsAccepted(); !ok {
		t.Fatal("normal source refused")
	}
	if _, err = service.Step(ctx); err != nil {
		t.Fatal(err)
	}
	before, err := m.ObserveChange(ctx, &contentPrincipal, original.Key, "", 64)
	if err != nil {
		t.Fatal(err)
	}
	target := alphaRef
	target.ContentID = "later-expiry-derived"
	installContentPolicy(t, ctx, w, target)
	req := contentPut(t, target, "later-expiry-derived", "YWxwaGEK")
	req.Payload.Sources = []v.ContentRef{alphaRef}
	receipt := putContentRequest(t, ctx, service, req)
	if _, ok := receipt.AsAccepted(); !ok {
		t.Fatal("normal later derived refused")
	}
	if _, err = service.Step(ctx); err != nil {
		t.Fatal(err)
	}
	assertContentBody(t, ctx, service, target, nil, "alpha\n")
	if err = service.AuthorizeUse(ctx, &contentPrincipal, target, "verification", "save"); err != nil {
		t.Fatal("normal use refused", err)
	}
	initial, _, err := m.ObserveAdmissionChanges(ctx, &contentPrincipal, target, "", 64)
	if err != nil || len(initial) != 1 {
		t.Fatal("admission did not fix maintenance identity", err, initial)
	}
	alias := req
	alias.CommandID = "normal-expiry-alias"
	if _, ok := putContentRequest(t, ctx, service, alias).AsAccepted(); !ok {
		t.Fatal("normal alias refused")
	}
	replayBefore, _ := v.Encode(receipt)
	replayAfter, _ := v.Encode(putContentRequest(t, ctx, service, req))
	if string(replayBefore) != string(replayAfter) {
		t.Fatal("original replay changed receipt")
	}
	stable, _, err := m.ObserveAdmissionChanges(ctx, &contentPrincipal, target, "", 64)
	if err != nil || len(stable) != 1 || stable[0].Key != initial[0].Key || !stable[0].Deadline.Equal(initial[0].Deadline) {
		t.Fatal("normal alias/replay added or refreshed obligation", err, stable)
	}
	// No read creates maintenance work: the worker runs at the original cutoff.
	waitUntil(t, ctx, due.Add(20*time.Millisecond))
	for i := 0; i < 5; i++ {
		if _, err = m.Step(ctx, &contentPrincipal); err != nil {
			t.Fatal(err)
		}
	}
	if err = service.AuthorizeUse(ctx, &contentPrincipal, target, "verification", "save"); err == nil {
		t.Fatal("expired ancestor authorized new use")
	}
	obligations, next, err := m.ObserveAdmissionChanges(ctx, &contentPrincipal, target, "", 2)
	if err != nil || next != "" || len(obligations) != 1 {
		t.Fatal("post-watermark descendant has no original ancestor maintenance obligation", err, obligations, next)
	}
	obligation := obligations[0]
	if obligation.Policy.Ref != alphaRef || obligation.AdmissionTarget == nil || *obligation.AdmissionTarget != target || !obligation.Due.Equal(due) || !obligation.Deadline.Equal(before.Change.ExpiryDeadline) {
		t.Fatal("ancestor original obligation identity/cutoff changed", obligation)
	}
	view, err := m.ObserveChange(ctx, &contentPrincipal, obligation.Key, "", 64)
	if err != nil || len(view.Responsibilities) != 1 || view.Responsibilities[0].Ref != target || view.Responsibilities[0].BodyCleanup != "pending" || !view.Responsibilities[0].ObjectHolder {
		t.Fatal("expired inherited saving basis lost target holder responsibility", err, view)
	}
	originalAfter, err := m.ObserveChange(ctx, &contentPrincipal, original.Key, "", 64)
	if err != nil || originalAfter.Change.Watermark != before.Change.Watermark {
		t.Fatal("original propagation watermark widened", err, originalAfter)
	}
	entries, err := os.ReadDir(w.Directory)
	if err != nil || len(entries) != 2 {
		t.Fatal("holder bytes disappeared", err)
	}
	for _, entry := range entries {
		bytes, e := os.ReadFile(filepath.Join(w.Directory, entry.Name()))
		if e != nil || string(bytes) != "alpha\n" {
			t.Fatal("independent bytes altered", e)
		}
	}
	expiredReplay, _ := v.Encode(putContentRequest(t, ctx, service, req))
	if string(expiredReplay) != string(replayBefore) {
		t.Fatal("expired ancestor replaced fixed original receipt")
	}
	command, e := service.GetCommand(ctx, contentCommandGetWire(t, req.CommandID), &contentPrincipal)
	found, ok := command.AsFound()
	progress, published := found.Progress.AsContent()
	if e != nil || !ok || !published || progress.Publication != "published" {
		t.Fatal("maintenance changed original publication history", e, command)
	}
	w.Reopen(ctx)
	m = trustedContentManager(t, w, 2)
	again, _, err := m.ObserveAdmissionChanges(ctx, &contentPrincipal, target, "", 2)
	if err != nil || len(again) != 1 || again[0].Key != obligation.Key || !again[0].Deadline.Equal(obligation.Deadline) {
		t.Fatal("reopen refreshed fixed obligation", err, again)
	}
}

func TestContentNaturalSourceAndTargetChecksCurrentRenewalWithoutErasingRevoke(t *testing.T) {
	for _, historicalRevoke := range []bool{false, true} {
		t.Run(fmt.Sprintf("historical-revoke=%t", historicalRevoke), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			w := fixture.New(t, ctx)
			service := contentService(t, w)
			m := trustedContentManager(t, w, 2)
			due := time.Now().UTC().Add(2 * time.Second).Truncate(time.Microsecond)
			wide := time.Now().Add(time.Hour)
			p := content.FixturePolicy{Ref: alphaRef, Subject: contentPrincipal, Purpose: "verification", Revision: 1, ValidUntil: due, RetainUntil: wide, Read: true, Process: true, Save: true, Sync: true, Disclose: true}
			first, err := m.InstallPolicy(ctx, &contentPrincipal, p, 0)
			if err != nil {
				t.Fatal(err)
			}
			if err = w.Store().InstallFixtureCommandReader(ctx, contentOwner, contentPrincipal, wide); err != nil {
				t.Fatal(err)
			}
			if _, ok := putContentRequest(t, ctx, service, contentPut(t, alphaRef, "renew-source", "YWxwaGEK")).AsAccepted(); !ok {
				t.Fatal("source normal")
			}
			if _, err = service.Step(ctx); err != nil {
				t.Fatal(err)
			}
			target := alphaRef
			target.ContentID = "renew-derived"
			installContentPolicy(t, ctx, w, target)
			req := contentPut(t, target, "renew-derived", "YWxwaGEK")
			req.Payload.Sources = []v.ContentRef{alphaRef}
			receipt := putContentRequest(t, ctx, service, req)
			if _, ok := receipt.AsAccepted(); !ok {
				t.Fatal("derived normal")
			}
			if _, err = service.Step(ctx); err != nil {
				t.Fatal(err)
			}
			obligations, _, err := m.ObserveAdmissionChanges(ctx, &contentPrincipal, target, "", 64)
			if err != nil || len(obligations) != 1 {
				t.Fatal(err, obligations)
			}
			revision := int64(1)
			var revoke content.PolicyChange
			if historicalRevoke {
				p.Revision = 2
				p.Save = false
				revoke, err = m.InstallPolicy(ctx, &contentPrincipal, p, revision)
				if err != nil {
					t.Fatal(err)
				}
				revision = 2
			}
			p.Revision = revision + 1
			p.Save = true
			p.ValidUntil = wide
			if _, err = m.InstallPolicy(ctx, &contentPrincipal, p, revision); err != nil {
				t.Fatal(err)
			}
			// Exercise deferred historic revoke after renewal, then its natural stage.
			for i := 0; i < 4; i++ {
				if _, err = m.Step(ctx, &contentPrincipal); err != nil {
					t.Fatal(err)
				}
			}
			waitUntil(t, ctx, due.Add(20*time.Millisecond))
			for i := 0; i < 5; i++ {
				if _, err = m.Step(ctx, &contentPrincipal); err != nil {
					t.Fatal(err)
				}
			}
			for _, key := range []string{first.Key, obligations[0].Key} {
				observation, e := m.ObserveChange(ctx, &contentPrincipal, key, "", 64)
				if e != nil || len(observation.Responsibilities) != 1 || observation.Responsibilities[0].BodyCleanup != "not_required" {
					t.Fatal("legitimate renewal classified old natural cutoff as current save invalidity", e, observation)
				}
			}
			if historicalRevoke {
				observation, e := m.ObserveChange(ctx, &contentPrincipal, revoke.Key, "", 64)
				if e != nil || len(observation.Responsibilities) != 2 {
					t.Fatal("historic revoke lost", e, observation)
				}
				for _, r := range observation.Responsibilities {
					if r.BodyCleanup != "pending" || !r.ObjectHolder {
						t.Fatal("natural renewal washed original revoke holder", r)
					}
				}
			}
			w.Reopen(ctx)
			service = contentService(t, w)
			assertContentBody(t, ctx, service, target, nil, "alpha\n")
			original, e := service.GetCommand(ctx, contentCommandGetWire(t, req.CommandID), &contentPrincipal)
			found, ok := original.AsFound()
			before, _ := v.Encode(receipt)
			after, _ := v.Encode(found.Receipt)
			if e != nil || !ok || string(before) != string(after) {
				t.Fatal("renewal changed fixed receipt", e)
			}
		})
	}
}

func TestContentRenewalResidualDoesNotPretendFutureMaintenanceCoverage(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	w := fixture.New(t, ctx)
	service := contentService(t, w)
	m := trustedContentManager(t, w, 2)
	due := time.Now().UTC().Add(2 * time.Second).Truncate(time.Microsecond)
	wide := time.Now().Add(time.Hour)
	p := content.FixturePolicy{Ref: alphaRef, Subject: contentPrincipal, Purpose: "verification", Revision: 1, ValidUntil: due, RetainUntil: wide, Read: true, Process: true, Save: true, Sync: true, Disclose: true}
	original, err := m.InstallPolicy(ctx, &contentPrincipal, p, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err = w.Store().InstallFixtureCommandReader(ctx, contentOwner, contentPrincipal, wide); err != nil {
		t.Fatal(err)
	}
	if _, ok := putContentRequest(t, ctx, service, contentPut(t, alphaRef, "residual-renew-source", "YWxwaGEK")).AsAccepted(); !ok {
		t.Fatal("normal source")
	}
	if _, err = service.Step(ctx); err != nil {
		t.Fatal(err)
	}
	target := alphaRef
	target.ContentID = "residual-renew-derived"
	installContentPolicy(t, ctx, w, target)
	req := contentPut(t, target, "residual-renew-derived", "YWxwaGEK")
	req.Payload.Sources = []v.ContentRef{alphaRef}
	if _, ok := putContentRequest(t, ctx, service, req).AsAccepted(); !ok {
		t.Fatal("normal derived")
	}
	if _, err = service.Step(ctx); err != nil {
		t.Fatal(err)
	}
	obligations, _, err := m.ObserveAdmissionChanges(ctx, &contentPrincipal, target, "", 64)
	if err != nil || len(obligations) != 1 {
		t.Fatal(err, obligations)
	}
	limited, err := content.NewManager(content.ManagementConfig{Owner: contentOwner, Store: w.Store(), TrustedSubject: contentPrincipal, TrustedUntil: wide, PageSize: 2, WorkBudget: 5 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	p.Revision = 2
	p.ValidUntil = wide
	renewal, err := limited.InstallPolicy(ctx, &contentPrincipal, p, 1)
	if err != nil {
		t.Fatal(err)
	}
	waitUntil(t, ctx, renewal.Deadline.Add(20*time.Millisecond))
	if _, err = m.Step(ctx, &contentPrincipal); err != nil {
		t.Fatal(err)
	}
	rv, err := m.ObserveChange(ctx, &contentPrincipal, renewal.Key, "", 64)
	if err != nil || rv.Change.State != "residual" {
		t.Fatal("normal residual control", err, rv)
	}
	waitUntil(t, ctx, due.Add(20*time.Millisecond))
	for i := 0; i < 4; i++ {
		if _, err = m.Step(ctx, &contentPrincipal); err != nil {
			t.Fatal(err)
		}
	}
	for _, key := range []string{original.Key, obligations[0].Key} {
		view, e := m.ObserveChange(ctx, &contentPrincipal, key, "", 64)
		if e != nil || len(view.Responsibilities) != 1 || view.Responsibilities[0].BodyCleanup != "pending" || view.Responsibilities[0].Reason != "maintenance_coverage_unknown" || view.Responsibilities[0].Residual != "current_basis_unconfirmed" {
			t.Fatal("residual work with future expiry fields falsely qualified maintenance coverage", e, view)
		}
	}
	assertContentBody(t, ctx, service, target, nil, "alpha\n") // Current use and holder confirmation are separate.
}

func TestContentSameSubjectIDAliasRetainsOriginalFullSavingBinding(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	w := fixture.New(t, ctx)
	service := contentService(t, w)
	m := trustedContentManager(t, w, 2)
	installContentPolicy(t, ctx, w, alphaRef)
	req := contentPut(t, alphaRef, "binding-original", "YWxwaGEK")
	original := putContentRequest(t, ctx, service, req)
	if _, ok := original.AsAccepted(); !ok {
		t.Fatal("normal original")
	}
	if _, err := service.Step(ctx); err != nil {
		t.Fatal(err)
	}
	other := contentPrincipal
	other.DelegationChain = []v.DelegatedSubject{{TenantID: contentOwner.TenantID, SubjectID: "delegating-actor"}}
	wide := time.Now().Add(time.Hour)
	narrow := time.Now().UTC().Add(2 * time.Second).Truncate(time.Microsecond)
	p := content.FixturePolicy{Ref: alphaRef, Subject: other, Purpose: "verification", Revision: 1, ValidUntil: wide, RetainUntil: wide, Read: true, Process: true, Save: true, Sync: true, Disclose: true}
	if err := w.Store().InstallFixturePolicy(ctx, p, 0); err != nil {
		t.Fatal(err)
	}
	if err := w.Store().InstallFixtureCommandReader(ctx, contentOwner, other, wide); err != nil {
		t.Fatal(err)
	}
	alias := req
	alias.CommandID = "same-id-other-binding-normal"
	putOther := func(request v.ContentPutRequest) v.CommandReceipt {
		raw, e := v.Encode(request)
		if e != nil {
			t.Fatal(e)
		}
		out, e := service.Put(ctx, raw, &other)
		received, ok := out.AsReceived()
		if e != nil || !ok {
			t.Fatal(e, out)
		}
		return received.Receipt
	}
	if _, ok := putOther(alias).AsAccepted(); !ok {
		t.Fatal("normal other-binding alias")
	}
	p.Revision = 2
	p.RetainUntil = narrow
	if err := w.Store().InstallFixturePolicy(ctx, p, 1); err != nil {
		t.Fatal(err)
	}
	alias.CommandID = "same-id-other-binding-narrow"
	if _, ok := putOther(alias).AsAccepted(); !ok {
		t.Fatal("current legal narrowed alias")
	}
	obligations, _, err := m.ObserveAdmissionChanges(ctx, &contentPrincipal, alphaRef, "", 64)
	if err != nil || len(obligations) != 1 {
		t.Fatal("narrower alias obligation missing", err, obligations)
	}
	a, _ := v.Encode(obligations[0].Policy.Subject)
	b, _ := v.Encode(contentPrincipal)
	if string(a) != string(b) || !obligations[0].Due.Equal(narrow) {
		t.Fatal("same-ID alias used another full subject's maintenance basis", obligations)
	}
	waitUntil(t, ctx, narrow.Add(20*time.Millisecond))
	for i := 0; i < 5; i++ {
		if _, err = m.Step(ctx, &contentPrincipal); err != nil {
			t.Fatal(err)
		}
	}
	observation, err := m.ObserveChange(ctx, &contentPrincipal, obligations[0].Key, "", 64)
	if err != nil || len(observation.Responsibilities) != 1 || observation.Responsibilities[0].BodyCleanup != "pending" || !observation.Responsibilities[0].ObjectHolder {
		t.Fatal("original saving body has no current cap holder responsibility", err, observation)
	}
	replay, _ := v.Encode(putContentRequest(t, ctx, service, req))
	fixed, _ := v.Encode(original)
	if string(replay) != string(fixed) {
		t.Fatal("alias rewrote original fixed receipt")
	}
}

func TestContentMultipleAncestorsKeepIndependentEarlierAdmissionDue(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	w := fixture.New(t, ctx)
	service := contentService(t, w)
	m := trustedContentManager(t, w, 2)
	wide := time.Now().Add(time.Hour)
	due := time.Now().UTC().Add(2 * time.Second).Truncate(time.Microsecond)
	refs := []v.ContentRef{alphaRef, alphaRef}
	refs[1].ContentID = "later-valid-ancestor"
	for i, ref := range refs {
		valid := wide
		if i == 0 {
			valid = due
		}
		p := content.FixturePolicy{Ref: ref, Subject: contentPrincipal, Purpose: "verification", Revision: 1, ValidUntil: valid, RetainUntil: wide, Read: true, Process: true, Save: true, Sync: true, Disclose: true}
		if _, err := m.InstallPolicy(ctx, &contentPrincipal, p, 0); err != nil {
			t.Fatal(err)
		}
		if err := w.Store().InstallFixtureCommandReader(ctx, contentOwner, contentPrincipal, wide); err != nil {
			t.Fatal(err)
		}
		if _, ok := putContentRequest(t, ctx, service, contentPut(t, ref, string(ref.ContentID), "YWxwaGEK")).AsAccepted(); !ok {
			t.Fatal("normal ancestor")
		}
		if _, err := service.Step(ctx); err != nil {
			t.Fatal(err)
		}
	}
	target := alphaRef
	target.ContentID = "two-ancestor-derived"
	installContentPolicy(t, ctx, w, target)
	req := contentPut(t, target, "two-ancestor-derived", "YWxwaGEK")
	req.Payload.Sources = refs
	if _, ok := putContentRequest(t, ctx, service, req).AsAccepted(); !ok {
		t.Fatal("normal target")
	}
	if _, err := service.Step(ctx); err != nil {
		t.Fatal(err)
	}
	assertContentBody(t, ctx, service, target, nil, "alpha\n")
	obligations, _, err := m.ObserveAdmissionChanges(ctx, &contentPrincipal, target, "", 64)
	if err != nil || len(obligations) != 2 {
		t.Fatal("distinct ancestor obligations missing", err, obligations)
	}
	early := ""
	for _, o := range obligations {
		if o.Policy.Ref == alphaRef {
			early = o.Key
			if !o.Due.Equal(due) {
				t.Fatal("later ancestor delayed earlier cutoff", o)
			}
		}
	}
	if early == "" {
		t.Fatal("early obligation absent")
	}
	waitUntil(t, ctx, due.Add(20*time.Millisecond))
	for i := 0; i < 5; i++ {
		if _, err = m.Step(ctx, &contentPrincipal); err != nil {
			t.Fatal(err)
		}
	}
	view, err := m.ObserveChange(ctx, &contentPrincipal, early, "", 64)
	if err != nil || len(view.Responsibilities) != 1 || view.Responsibilities[0].BodyCleanup != "pending" {
		t.Fatal("early ancestor lost target holder responsibility", err, view)
	}
	assertContentBody(t, ctx, service, refs[1], nil, "alpha\n")
}
