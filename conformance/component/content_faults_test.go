//go:build integration

package component_test

import (
	"context"
	"encoding/json"
	fixture "github.com/ruipengliu/lerna/conformance/internal/contentfixture"
	old "github.com/ruipengliu/lerna/contract"
	middle "github.com/ruipengliu/lerna/contract/v1_1"
	v "github.com/ruipengliu/lerna/contract/v1_2"
	"github.com/ruipengliu/lerna/domain/content"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func requireRejection(t *testing.T, receipt v.CommandReceipt, reason v.ErrorCode) {
	t.Helper()
	r, ok := receipt.AsRejected()
	if !ok || r.Reason != reason {
		t.Fatalf("expected fixed %s rejection", reason)
	}
}
func TestContentDeclaredBytesAndVersionCannotChange(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	w := fixture.New(t, ctx)
	installContentPolicy(t, ctx, w, alphaRef)
	service := contentService(t, w)
	for _, bad := range []struct {
		id, body string
		ref      v.ContentRef
	}{{"bad-body", "YmV0YQo=", alphaRef}, {"bad-length", "YWxwaGEK", func() v.ContentRef { r := alphaRef; r.ByteLength = "5"; return r }()}} {
		requireRejection(t, putContentRequest(t, ctx, service, contentPut(t, bad.ref, bad.id, bad.body)), "integrity")
	}
	original := contentPut(t, alphaRef, "normal", "YWxwaGEK")
	if _, ok := putContentRequest(t, ctx, service, original).AsAccepted(); !ok {
		t.Fatal("normal original refused")
	}
	changed := original
	changed.CommandID = "changed-source"
	changed.Payload.Sources = []v.ContentRef{alphaRef}
	requireRejection(t, putContentRequest(t, ctx, service, changed), "version_conflict")
	changed = original
	changed.CommandID = "changed-bytes"
	changed.Payload.ContentRef.Hash = "sha256:f2c82decdd7181cf98945929a62598db7e6b477e11f6e0eb0ae97020eff151ad"
	changed.Payload.ContentRef.ByteLength = "5"
	changed.Payload.BytesBase64 = "YmV0YQo="
	requireRejection(t, putContentRequest(t, ctx, service, changed), "version_conflict")
	if _, err := service.Step(ctx); err != nil {
		t.Fatal(err)
	}
	assertContentBody(t, ctx, service, alphaRef, nil, "alpha\n")
	altered := alphaRef
	altered.MediaType = "application/octet-stream"
	view, err := service.Get(ctx, contentGetWire(t, altered, nil), &contentPrincipal)
	if err != nil {
		t.Fatal(err)
	}
	r, ok := view.AsRejected()
	if !ok || r.Reason != "integrity" {
		t.Fatal("exact version metadata silently substituted")
	}
}
func TestContentPublishedDamageAndMissingNeverRepairOnQuery(t *testing.T) {
	for _, fault := range []string{"damage", "missing"} {
		t.Run(fault, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			w := fixture.New(t, ctx)
			installContentPolicy(t, ctx, w, alphaRef)
			service := contentService(t, w)
			request := contentPut(t, alphaRef, "original", "YWxwaGEK")
			if _, ok := putContentRequest(t, ctx, service, request).AsAccepted(); !ok {
				t.Fatal("normal refused")
			}
			if _, err := service.Step(ctx); err != nil {
				t.Fatal(err)
			}
			assertContentBody(t, ctx, service, alphaRef, nil, "alpha\n")
			assertExactContentObjects(t, w.Directory, map[v.ContentRef]string{alphaRef: "alpha\n"})
			_, key, err := content.VersionIdentity(alphaRef)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(w.Directory, key)
			reason := "dependency_unavailable"
			if fault == "damage" {
				reason = "integrity"
				w.WriteIndependentObject(key, []byte("wrong\n"))
			} else {
				err = os.Remove(path)
			}
			if err != nil {
				t.Fatal(err)
			}
			w.Reopen(ctx)
			service = contentService(t, w)
			for i := 0; i < 2; i++ {
				view, err := service.Get(ctx, contentGetWire(t, alphaRef, &v.ContentRange{Offset: "0", Length: "1"}), &contentPrincipal)
				if err != nil {
					t.Fatal(err)
				}
				unavailable, ok := view.AsUnavailable()
				if !ok || unavailable.Reason != reason {
					t.Fatal("whole object verification did not detect exact damaged/missing bytes")
				}
			}
			command, err := service.GetCommand(ctx, contentCommandGetWire(t, request.CommandID), &contentPrincipal)
			if err != nil {
				t.Fatal(err)
			}
			found, ok := command.AsFound()
			if !ok {
				t.Fatal("original command lost")
			}
			progress, ok := found.Progress.AsContent()
			if !ok || progress.Publication != "published" {
				t.Fatal("byte fault rewrote publication history")
			}
			if fault == "damage" {
				data, err := os.ReadFile(path)
				if err != nil || string(data) != "wrong\n" {
					t.Fatal("query repaired damaged object", err)
				}
			} else if _, err = os.Lstat(path); !os.IsNotExist(err) {
				t.Fatal("query recreated missing original", err)
			}
			control := alphaRef
			control.ContentID = "unaffected"
			installContentPolicy(t, ctx, w, control)
			if _, ok := putContentRequest(t, ctx, service, contentPut(t, control, "control", "YWxwaGEK")).AsAccepted(); !ok {
				t.Fatal("control refused")
			}
			if _, err = service.Step(ctx); err != nil {
				t.Fatal(err)
			}
			assertContentBody(t, ctx, service, control, nil, "alpha\n")
		})
	}
}
func TestContentOldReadersPreserveLosslessFactsAndUnavailable(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	w := fixture.New(t, ctx)
	installContentPolicy(t, ctx, w, alphaRef)
	service := contentService(t, w)
	requireRejection(t, putContentRequest(t, ctx, service, contentPut(t, alphaRef, "new-reason", "YmV0YQo=")), "integrity")
	expired := contentPut(t, alphaRef, "rejected", "YWxwaGEK")
	expired.AcceptBefore = "2000-01-01T00:00:00.000000Z"
	requireRejection(t, putContentRequest(t, ctx, service, expired), "expired")
	if _, ok := putContentRequest(t, ctx, service, contentPut(t, alphaRef, "accepted", "YWxwaGEK")).AsAccepted(); !ok {
		t.Fatal("normal refused")
	}
	if _, err := service.Step(ctx); err != nil {
		t.Fatal(err)
	}
	subjectWire, _ := v.Encode(contentPrincipal)
	subject10, err := old.Decode[old.SubjectBinding](subjectWire)
	if err != nil {
		t.Fatal(err)
	}
	subject11, err := middle.Decode[middle.SubjectBinding](subjectWire)
	if err != nil {
		t.Fatal(err)
	}
	for _, version := range []string{"1.0.0", "1.1.0"} {
		for _, id := range []v.ID{"accepted", "rejected", "new-reason", "absent"} {
			var request map[string]json.RawMessage
			if err = json.Unmarshal(contentCommandGetWire(t, id), &request); err != nil {
				t.Fatal(err)
			}
			request["contract_version"] = json.RawMessage(`"` + version + `"`)
			wire, _ := json.Marshal(request)
			var result []byte
			if version == "1.0.0" {
				view, err := service.GetCommand10(ctx, wire, &subject10)
				if err != nil {
					t.Fatal(err)
				}
				result, err = old.EncodeCommandResponse(view, old.CommandRef{Owner: old.OwnerRef{TenantID: "content-tenant", OwnerID: "content-owner"}, CommandID: old.ID(id)})
				if err != nil {
					t.Fatal(err)
				}
			} else {
				view, err := service.GetCommand11(ctx, wire, &subject11)
				if err != nil {
					t.Fatal(err)
				}
				result, err = middle.EncodeCommandResponse(view, middle.CommandRef{Owner: middle.OwnerRef{TenantID: "content-tenant", OwnerID: "content-owner"}, CommandID: middle.ID(id)})
				if err != nil {
					t.Fatal(err)
				}
			}
			var status struct{ Status string }
			if err = json.Unmarshal(result, &status); err != nil {
				t.Fatal(err)
			}
			want := map[v.ID]string{"accepted": "unavailable", "rejected": "found", "absent": "not_found", "new-reason": "unavailable"}[id]
			if status.Status != want {
				t.Fatalf("old %s reader changed lossless/unavailable boundary: %s", version, status.Status)
			}
		}
	}
	assertContentBody(t, ctx, service, alphaRef, nil, "alpha\n")
}
func TestContentFailedStagingStillConsumesExplicitByteLimit(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	w := fixture.New(t, ctx)
	installContentPolicy(t, ctx, w, alphaRef)
	service, err := content.New(content.Config{Owner: contentOwner, Store: w.Store(), Objects: w.Objects, Limits: content.Limits{MaxPreparingVersions: 1, MaxStagingBytes: 6, Lease: time.Minute, WorkTimeout: time.Second}, PublishBudget: time.Minute, MaxPublicationAttempts: 3, Worker: "bounded"})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := putContentRequest(t, ctx, service, contentPut(t, alphaRef, "normal", "YWxwaGEK")).AsAccepted(); !ok {
		t.Fatal("bounded normal refused")
	}
	other := alphaRef
	other.ContentID = "second"
	installContentPolicy(t, ctx, w, other)
	requireRejection(t, putContentRequest(t, ctx, service, contentPut(t, other, "while-preparing", "YWxwaGEK")), "input_over_limit")
	wide := time.Now().Add(time.Hour)
	if err = w.Store().InstallFixturePolicy(ctx, content.FixturePolicy{Ref: alphaRef, Subject: contentPrincipal, Purpose: "verification", Revision: 2, ValidUntil: wide, RetainUntil: wide, Read: true, Process: true, Save: false, Disclose: true}, 1); err != nil {
		t.Fatal(err)
	}
	if _, err = service.Step(ctx); err != nil {
		t.Fatal(err)
	}
	requireRejection(t, putContentRequest(t, ctx, service, contentPut(t, other, "while-failed-staging-retained", "YWxwaGEK")), "input_over_limit")
	assertExactContentObjects(t, w.Directory, nil, alphaRef, other)
}

func TestContentNoClobberExistingBytesMustMatchBeforePublication(t *testing.T) {
	for _, body := range []string{"alpha\n", "wrong\n"} {
		t.Run(body[:5], func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			w := fixture.New(t, ctx)
			installContentPolicy(t, ctx, w, alphaRef)
			service := contentService(t, w)
			if _, ok := putContentRequest(t, ctx, service, contentPut(t, alphaRef, "original", "YWxwaGEK")).AsAccepted(); !ok {
				t.Fatal("normal refused")
			}
			const key = "da73fc3f262e288d3fbf5bf06db2ebcfbb031f2a5cacbc4e8be74a516714a028" // independent shared original-identity golden
			w.WriteIndependentObject(key, []byte(body))
			if _, err := service.Step(ctx); err != nil {
				t.Fatal(err)
			}
			view, err := service.Get(ctx, contentGetWire(t, alphaRef, nil), &contentPrincipal)
			if err != nil {
				t.Fatal(err)
			}
			if body == "alpha\n" {
				if _, ok := view.AsPublished(); !ok {
					t.Fatal("matching native bytes not adopted")
				}
				assertContentBody(t, ctx, service, alphaRef, nil, body)
			} else {
				failed, ok := view.AsFailed()
				if !ok || failed.Reason != "integrity" {
					t.Fatal("wrong existing native bytes declared published")
				}
			}
			data, err := os.ReadFile(filepath.Join(w.Directory, key))
			if err != nil || string(data) != body {
				t.Fatal("noclobber install overwrote existing independent bytes", err)
			}
		})
	}
}
