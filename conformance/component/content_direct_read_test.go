//go:build integration

package component_test

import (
	"context"
	fixture "github.com/ruipengliu/lerna/conformance/internal/contentfixture"
	v "github.com/ruipengliu/lerna/contract/v1_2"
	"github.com/ruipengliu/lerna/domain/content"
	"sync"
	"testing"
	"time"
)

func publishedDirectContent(t *testing.T, ctx context.Context, w *fixture.World) (*content.Service, v.ContentPutRequest, v.ContentRef) {
	t.Helper()
	service := contentService(t, w)
	second := alphaRef
	second.ContentID = "second-source"
	for i, ref := range []v.ContentRef{alphaRef, second} {
		installContentPolicy(t, ctx, w, ref)
		id := "first-source"
		if i == 1 {
			id = "second-source"
		}
		if _, ok := putContentRequest(t, ctx, service, contentPut(t, ref, id, "YWxwaGEK")).AsAccepted(); !ok {
			t.Fatal("normal source admission")
		}
		if _, err := service.Step(ctx); err != nil {
			t.Fatal(err)
		}
		assertContentBody(t, ctx, service, ref, nil, "alpha\n")
	}
	target := alphaRef
	target.ContentID = "direct-target"
	installContentPolicy(t, ctx, w, target)
	request := contentPut(t, target, "original-direct", "YWxwaGEK")
	request.Payload.Sources = []v.ContentRef{alphaRef, second}
	if _, ok := putContentRequest(t, ctx, service, request).AsAccepted(); !ok {
		t.Fatal("normal derived admission")
	}
	if _, err := service.Step(ctx); err != nil {
		t.Fatal(err)
	}
	assertContentBody(t, ctx, service, target, nil, "alpha\n")
	return service, request, second
}
func TestContentDirectReadPermissionsAreCurrentAndSeparate(t *testing.T) {
	for _, action := range []string{"read", "disclose", "during-read", "during-disclose", "expired", "process-save-control"} {
		t.Run(action, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			w := fixture.New(t, ctx)
			service, request, source := publishedDirectContent(t, ctx, w)
			wide := time.Now().Add(time.Hour)
			policy := content.FixturePolicy{Ref: source, Subject: contentPrincipal, Purpose: "verification", Revision: 2, ValidUntil: wide, RetainUntil: wide, Read: true, Disclose: true, Process: true, Save: true}
			switch action {
			case "read", "during-read":
				policy.Read = false
			case "disclose", "during-disclose":
				policy.Disclose = false
			case "expired":
				policy.RetainUntil = time.Now().Add(-time.Second)
			case "process-save-control":
				policy.Process = false
				policy.Save = false
			}
			var view v.ContentGetResponse
			var err error
			if action == "during-read" || action == "during-disclose" {
				objects := &heldContentRead{Objects: w.Objects, entered: make(chan struct{}), release: make(chan struct{})}
				service = withPublication(t, w, w.Store(), objects, time.Minute, 5*time.Second)
				var once sync.Once
				release := func() { once.Do(func() { close(objects.release) }) }
				finished := make(chan struct{})
				done := make(chan v.ContentGetResponse, 1)
				failures := make(chan error, 1)
				go func() {
					defer close(finished)
					result, e := service.Get(ctx, contentGetWire(t, request.Payload.ContentRef, nil), &contentPrincipal)
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
					t.Fatal(err)
				}
				release()
				select {
				case view = <-done:
					err = <-failures
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
			} else {
				if err = w.Store().InstallFixturePolicy(ctx, policy, 1); err != nil {
					t.Fatal(err)
				}
				view, err = service.Get(ctx, contentGetWire(t, request.Payload.ContentRef, nil), &contentPrincipal)
			}
			if err != nil {
				t.Fatal(err)
			}
			if action == "process-save-control" {
				if _, ok := view.AsPublished(); !ok {
					t.Fatal("independent process/save denial blocked permitted read/disclosure")
				}
				return
			}
			reason := v.ErrorCode("forbidden")
			if action == "expired" {
				reason = "expired"
			}
			denied, ok := view.AsRejected()
			if !ok || denied.Reason != reason {
				t.Fatalf("direct source %s did not prevent body: %+v", action, view)
			}
			policy.Revision = 3
			policy.Read = true
			policy.Disclose = true
			policy.RetainUntil = wide
			if err = w.Store().InstallFixturePolicy(ctx, policy, 2); err != nil {
				t.Fatal(err)
			}
			assertContentBody(t, ctx, contentService(t, w), request.Payload.ContentRef, nil, "alpha\n")
		})
	}
}
