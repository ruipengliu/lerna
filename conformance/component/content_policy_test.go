//go:build integration

package component_test

import (
	"context"
	"errors"
	fixture "github.com/ruipengliu/lerna/conformance/internal/contentfixture"
	v "github.com/ruipengliu/lerna/contract/v1_2"
	"github.com/ruipengliu/lerna/domain/content"
	"testing"
	"time"
)

func TestContentExplicitPolicyAndTrustedPrincipalFailClosed(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	w := fixture.New(t, ctx)
	service := contentService(t, w)
	request := contentPut(t, alphaRef, "before-policy", "YWxwaGEK")
	raw, err := v.Encode(request)
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.Put(ctx, raw, &contentPrincipal)
	var refusal *v.ContractError
	if !errors.As(err, &refusal) || refusal.Code != "forbidden" {
		t.Fatal("missing explicit reader/policy granted admission", err)
	}
	installContentPolicy(t, ctx, w, alphaRef)
	request.CommandID = "normal"
	if _, ok := putContentRequest(t, ctx, service, request).AsAccepted(); !ok {
		t.Fatal("explicit normal refused")
	}
	if _, err = service.Step(ctx); err != nil {
		t.Fatal(err)
	}
	assertContentBody(t, ctx, service, alphaRef, nil, "alpha\n")
	for _, principal := range []*v.SubjectBinding{nil, func() *v.SubjectBinding { p := contentPrincipal; p.TenantID = "different-tenant"; return &p }(), func() *v.SubjectBinding { p := contentPrincipal; p.SubjectID = "different-subject"; return &p }()} {
		view, err := service.Get(ctx, contentGetWire(t, alphaRef, nil), principal)
		if err != nil {
			t.Fatal(err)
		}
		r, ok := view.AsRejected()
		if !ok || r.Reason != "forbidden" {
			t.Fatal("untrusted/unpermitted principal observed bytes")
		}
		command, err := service.GetCommand(ctx, contentCommandGetWire(t, request.CommandID), principal)
		if err != nil {
			t.Fatal(err)
		}
		cr, ok := command.AsRejected()
		if !ok || cr.Reason != "forbidden" {
			t.Fatal("untrusted principal observed Command")
		}
	}
	foreign := alphaRef
	foreign.Owner.OwnerID = "different-owner"
	view, err := service.Get(ctx, contentGetWire(t, foreign, nil), &contentPrincipal)
	if err != nil {
		t.Fatal(err)
	}
	r, ok := view.AsRejected()
	if !ok || r.Reason != "forbidden" {
		t.Fatal("owner route silently substituted local owner")
	}
	wide := time.Now().Add(time.Hour)
	for i, action := range []string{"disclose", "read"} {
		p := content.FixturePolicy{Ref: alphaRef, Subject: contentPrincipal, Purpose: "verification", Revision: int64(i + 2), ValidUntil: wide, RetainUntil: wide, Read: true, Process: true, Save: true, Disclose: true}
		if action == "disclose" {
			p.Disclose = false
		} else {
			p.Read = false
		}
		if err = w.Store().InstallFixturePolicy(ctx, p, int64(i+1)); err != nil {
			t.Fatal(err)
		}
		view, err = service.Get(ctx, contentGetWire(t, alphaRef, nil), &contentPrincipal)
		if err != nil {
			t.Fatal(err)
		}
		r, ok = view.AsRejected()
		if !ok || r.Reason != "forbidden" {
			t.Fatalf("%s independently denied but bytes disclosed", action)
		}
	}
	if _, err = content.New(content.Config{Owner: contentOwner, Store: w.Store(), Objects: w.Objects}); err == nil {
		t.Fatal("missing finite config silently defaulted")
	}
	config := content.Config{Owner: contentOwner, Store: w.Store(), Objects: w.Objects, Limits: content.Limits{MaxPreparingVersions: 1, MaxStagingBytes: 6, Lease: time.Second, WorkTimeout: time.Second}, PublishBudget: time.Minute, MaxPublicationAttempts: 1, Worker: "finite"}
	config.Objects = nil
	if _, err = content.New(config); err == nil {
		t.Fatal("missing object dependency silently allowed")
	}
}
func TestContentDirectSourcesRequireSeparateReadProcessSaveAndRetention(t *testing.T) {
	for _, action := range []string{"read", "process", "save"} {
		t.Run(action, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			w := fixture.New(t, ctx)
			installContentPolicy(t, ctx, w, alphaRef)
			service := contentService(t, w)
			if _, ok := putContentRequest(t, ctx, service, contentPut(t, alphaRef, "source", "YWxwaGEK")).AsAccepted(); !ok {
				t.Fatal("normal source refused")
			}
			if _, err := service.Step(ctx); err != nil {
				t.Fatal(err)
			}
			assertContentBody(t, ctx, service, alphaRef, nil, "alpha\n")
			cap := time.Now().UTC().Add(10 * time.Minute).Truncate(time.Microsecond)
			wide := time.Now().Add(time.Hour)
			p := content.FixturePolicy{Ref: alphaRef, Subject: contentPrincipal, Purpose: "verification", Revision: 2, ValidUntil: wide, RetainUntil: cap, Read: true, Process: true, Save: true, Disclose: true}
			if err := w.Store().InstallFixturePolicy(ctx, p, 1); err != nil {
				t.Fatal(err)
			}
			derived := alphaRef
			derived.ContentID = "derived-control"
			installContentPolicy(t, ctx, w, derived)
			request := contentPut(t, derived, "derive-normal", "YWxwaGEK")
			request.Payload.Sources = []v.ContentRef{alphaRef}
			receipt := putContentRequest(t, ctx, service, request)
			accepted, ok := receipt.AsAccepted()
			if !ok || accepted.RetainUntil != v.Time(cap.Format("2006-01-02T15:04:05.000000Z")) {
				t.Fatal("source cap not fixed in derived accepted")
			}
			if _, err := service.Step(ctx); err != nil {
				t.Fatal(err)
			}
			assertContentBody(t, ctx, service, derived, nil, "alpha\n")
			pending := alphaRef
			pending.ContentID = "derived-preparing"
			installContentPolicy(t, ctx, w, pending)
			pendingRequest := contentPut(t, pending, "pending-before-revoke", "YWxwaGEK")
			pendingRequest.Payload.Sources = []v.ContentRef{alphaRef}
			if _, ok := putContentRequest(t, ctx, service, pendingRequest).AsAccepted(); !ok {
				t.Fatal("normal pending source admission refused")
			}
			p.Revision = 3
			switch action {
			case "read":
				p.Read = false
			case "process":
				p.Process = false
			case "save":
				p.Save = false
			}
			if err := w.Store().InstallFixturePolicy(ctx, p, 2); err != nil {
				t.Fatal(err)
			}
			denied := alphaRef
			denied.ContentID = "derived-denied"
			installContentPolicy(t, ctx, w, denied)
			request = contentPut(t, denied, "derive-denied", "YWxwaGEK")
			request.Payload.Sources = []v.ContentRef{alphaRef}
			requireRejection(t, putContentRequest(t, ctx, service, request), "forbidden")
			if processed, err := service.Step(ctx); err != nil || !processed {
				t.Fatal("original source responsibility not closed", err)
			}
			view, err := service.Get(ctx, contentGetWire(t, pending, nil), &contentPrincipal)
			if err != nil {
				t.Fatal(err)
			}
			if action == "read" {
				denied, ok := view.AsRejected()
				if !ok || denied.Reason != "forbidden" {
					t.Fatal("current source read denial disclosed failed target observation")
				}
			} else {
				failed, ok := view.AsFailed()
				if !ok || failed.Reason != "forbidden" {
					t.Fatal("current direct source permission did not stop original publication")
				}
			}
			command, err := service.GetCommand(ctx, contentCommandGetWire(t, pendingRequest.CommandID), &contentPrincipal)
			if err != nil {
				t.Fatal(err)
			}
			found, ok := command.AsFound()
			if !ok {
				t.Fatal("original publication history disappeared")
			}
			progress, ok := found.Progress.AsContent()
			if !ok || progress.Publication != "failed" {
				t.Fatal("original source responsibility did not fail")
			}
			assertExactContentObjects(t, w.Directory, map[v.ContentRef]string{alphaRef: "alpha\n", derived: "alpha\n"}, pending, denied)
		})
	}
}
