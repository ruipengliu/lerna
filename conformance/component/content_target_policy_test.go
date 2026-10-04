//go:build integration

package component_test

import (
	"context"
	fixture "github.com/ruipengliu/lerna/conformance/internal/contentfixture"
	v "github.com/ruipengliu/lerna/contract/v1_2"
	"github.com/ruipengliu/lerna/domain/content"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestContentTargetReadPoliciesBindExactPublishedDeclaration(t *testing.T) {
	for _, field := range []string{"hash", "media", "length"} {
		for _, stage := range []string{"before-read", "after-native-read"} {
			t.Run(field+"/"+stage, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
				defer cancel()
				w := fixture.New(t, ctx)
				installContentPolicy(t, ctx, w, alphaRef)
				service := contentService(t, w)
				request := contentPut(t, alphaRef, "original-target", "YWxwaGEK")
				original := putContentRequest(t, ctx, service, request)
				if _, ok := original.AsAccepted(); !ok {
					t.Fatal("normal target admission refused")
				}
				if _, err := service.Step(ctx); err != nil {
					t.Fatal(err)
				}
				assertContentBody(t, ctx, service, alphaRef, nil, "alpha\n")
				altered := alphaRef
				switch field {
				case "hash":
					altered.Hash = "sha256:0000000000000000000000000000000000000000000000000000000000000000"
				case "media":
					altered.MediaType = "application/octet-stream"
				case "length":
					altered.ByteLength = "7"
				}
				// Bind policy to actual A before comparing the requested declaration B.
				// Authorization for actual A permits integrity for that incorrect request.
				mismatch, err := service.Get(ctx, contentGetWire(t, altered, nil), &contentPrincipal)
				integrity, ok := mismatch.AsRejected()
				if err != nil || !ok || integrity.Reason != "integrity" {
					t.Fatal("normal authorized declaration mismatch changed semantic", err)
				}
				wide := time.Now().Add(time.Hour)
				policy := content.FixturePolicy{Ref: altered, Subject: contentPrincipal, Purpose: "verification", Revision: 2, ValidUntil: wide, RetainUntil: wide, Read: true, Disclose: true, Process: true, Save: true}
				var view v.ContentGetResponse
				if stage == "before-read" {
					if err = w.Store().InstallFixturePolicy(ctx, policy, 1); err != nil {
						t.Fatal(err)
					}
					view, err = service.Get(ctx, contentGetWire(t, alphaRef, nil), &contentPrincipal)
				} else {
					objects := &heldContentRead{Objects: w.Objects, entered: make(chan struct{}), release: make(chan struct{})}
					service = withPublication(t, w, w.Store(), objects, time.Minute, 5*time.Second)
					var once sync.Once
					release := func() { once.Do(func() { close(objects.release) }) }
					done := make(chan v.ContentGetResponse, 1)
					failures := make(chan error, 1)
					finished := make(chan struct{})
					go func() {
						defer close(finished)
						result, e := service.Get(ctx, contentGetWire(t, alphaRef, nil), &contentPrincipal)
						done <- result
						failures <- e
					}()
					joinContentRead(t, w, func() error { release(); return nil }, finished)
					select {
					case <-objects.entered:
					case <-ctx.Done():
						t.Fatal(ctx.Err())
					}
					if err = w.Store().InstallFixturePolicy(ctx, policy, 1); err != nil {
						t.Fatal("altered trusted policy could not commit outside native I/O transaction", err)
					}
					release()
					select {
					case view = <-done:
						err = <-failures
					case <-ctx.Done():
						t.Fatal(ctx.Err())
					}
				}
				denied, ok := view.AsRejected()
				if err != nil || !ok || denied.Reason != "forbidden" {
					t.Fatalf("%s target policy after %s disclosed exact target body: %+v %v", field, stage, view, err)
				}
				entries, err := os.ReadDir(w.Directory)
				if err != nil || len(entries) != 1 {
					t.Fatal("query changed native object count", err)
				}
				bytes, err := os.ReadFile(filepath.Join(w.Directory, entries[0].Name()))
				if err != nil || string(bytes) != "alpha\n" {
					t.Fatal("query repaired or changed independent original bytes", err)
				}
				command, err := contentService(t, w).GetCommand(ctx, contentCommandGetWire(t, request.CommandID), &contentPrincipal)
				found, ok := command.AsFound()
				if err != nil || !ok {
					t.Fatal("query removed original command", err)
				}
				a, _ := v.Encode(original)
				b, _ := v.Encode(found.Receipt)
				if string(a) != string(b) {
					t.Fatal("query replaced original receipt")
				}
				progress, ok := found.Progress.AsContent()
				if !ok || progress.Publication != "published" {
					t.Fatal("query changed publication history")
				}
				policy.Ref = alphaRef
				policy.Revision = 3
				if err = w.Store().InstallFixturePolicy(ctx, policy, 2); err != nil {
					t.Fatal(err)
				}
				assertContentBody(t, ctx, contentService(t, w), alphaRef, nil, "alpha\n")
			})
		}
	}
}

func TestContentTargetAuthorizationPrecedesExistenceAndMismatchObservation(t *testing.T) {
	for _, state := range []string{"existing-other-request", "existing-policy-request", "absent"} {
		t.Run(state, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			w := fixture.New(t, ctx)
			installContentPolicy(t, ctx, w, alphaRef)
			service := contentService(t, w)
			if state != "absent" {
				if _, ok := putContentRequest(t, ctx, service, contentPut(t, alphaRef, "original-authorized", "YWxwaGEK")).AsAccepted(); !ok {
					t.Fatal("normal actual declaration admission refused")
				}
				if _, err := service.Step(ctx); err != nil {
					t.Fatal(err)
				}
				assertContentBody(t, ctx, service, alphaRef, nil, "alpha\n")
			} else {
				normal, err := service.Get(ctx, contentGetWire(t, alphaRef, nil), &contentPrincipal)
				if _, ok := normal.AsNotFound(); err != nil || !ok {
					t.Fatal("correct exact policy did not allow normal absent observation", err)
				}
			}
			wrongPolicy := alphaRef
			wrongPolicy.MediaType = "application/octet-stream"
			requestRef := alphaRef
			if state == "existing-other-request" {
				requestRef.MediaType = "application/json"
			}
			if state == "existing-policy-request" {
				requestRef = wrongPolicy
			}
			if state != "absent" {
				normal, err := service.Get(ctx, contentGetWire(t, requestRef, nil), &contentPrincipal)
				declared, ok := normal.AsRejected()
				if err != nil || !ok || declared.Reason != "integrity" {
					t.Fatal("policy A/request B-or-C/actual A lost permitted integrity observation", err)
				}
			}
			wide := time.Now().Add(time.Hour)
			policy := content.FixturePolicy{Ref: wrongPolicy, Subject: contentPrincipal, Purpose: "verification", Revision: 2, ValidUntil: wide, RetainUntil: wide, Read: true, Disclose: true, Process: true, Save: true}
			if err := w.Store().InstallFixturePolicy(ctx, policy, 1); err != nil {
				t.Fatal(err)
			}
			view, err := service.Get(ctx, contentGetWire(t, requestRef, nil), &contentPrincipal)
			denied, ok := view.AsRejected()
			if err != nil || !ok || denied.Reason != "forbidden" {
				t.Fatalf("wrong full policy disclosed %s existence/declaration observation: %+v %v", state, view, err)
			}
			entries, err := os.ReadDir(w.Directory)
			expected := 1
			if state == "absent" {
				expected = 0
			}
			if err != nil || len(entries) != expected {
				t.Fatal("unauthorized observation changed native objects", err)
			}
			policy.Ref = alphaRef
			policy.Revision = 3
			if err = w.Store().InstallFixturePolicy(ctx, policy, 2); err != nil {
				t.Fatal(err)
			}
			if state == "absent" {
				normal, err := service.Get(ctx, contentGetWire(t, alphaRef, nil), &contentPrincipal)
				if _, ok := normal.AsNotFound(); err != nil || !ok {
					t.Fatal("restored exact absent authorization refused", err)
				}
			} else {
				assertContentBody(t, ctx, service, alphaRef, nil, "alpha\n")
			}
		})
	}
}
