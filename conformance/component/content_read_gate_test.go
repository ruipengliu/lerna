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

// The finite pause controls disclosure ordering after real native whole-object
// verification. It is a mechanical I/O gate, not a native storage fault.
type heldContentRead struct {
	content.Objects
	entered, release chan struct{}
}

func (o *heldContentRead) Read(ctx context.Context, key, hash string, length int64) ([]byte, error) {
	bytes, err := o.Objects.Read(ctx, key, hash, length)
	if err != nil {
		return nil, err
	}
	close(o.entered)
	select {
	case <-o.release:
		return bytes, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
func TestContentReturnedBytesRequireCurrentReadAndDisclose(t *testing.T) {
	for _, action := range []string{"read", "disclose"} {
		t.Run(action, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			w := fixture.New(t, ctx)
			installContentPolicy(t, ctx, w, alphaRef)
			service := contentService(t, w)
			request := contentPut(t, alphaRef, "original", "YWxwaGEK")
			if _, ok := putContentRequest(t, ctx, service, request).AsAccepted(); !ok {
				t.Fatal("normal admission refused")
			}
			if _, err := service.Step(ctx); err != nil {
				t.Fatal(err)
			}
			assertContentBody(t, ctx, service, alphaRef, nil, "alpha\n")
			objects := &heldContentRead{Objects: w.Objects, entered: make(chan struct{}), release: make(chan struct{})}
			service = withPublication(t, w, w.Store(), objects, time.Minute, 5*time.Second)
			var once sync.Once
			release := func() { once.Do(func() { close(objects.release) }) }
			done := make(chan struct {
				view v.ContentGetResponse
				err  error
			}, 1)
			finished := make(chan struct{})
			go func() {
				defer close(finished)
				view, err := service.Get(ctx, contentGetWire(t, alphaRef, nil), &contentPrincipal)
				done <- struct {
					view v.ContentGetResponse
					err  error
				}{view, err}
			}()
			t.Cleanup(func() {
				release()
				cleanup, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				defer cancel()
				select {
				case <-finished:
				case <-cleanup.Done():
					w.RetainInvocation()
					t.Error("retain exact read invocation until actual return")
				}
			})
			select {
			case <-objects.entered:
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			wide := time.Now().Add(time.Hour)
			policy := content.FixturePolicy{Ref: alphaRef, Subject: contentPrincipal, Purpose: "verification", Revision: 2, ValidUntil: wide, RetainUntil: wide, Read: true, Process: true, Save: true, Disclose: true}
			if action == "read" {
				policy.Read = false
			} else {
				policy.Disclose = false
			}
			if err := w.Store().InstallFixturePolicy(ctx, policy, 1); err != nil {
				t.Fatal("policy change must commit while no I/O transaction holds its row", err)
			}
			release()
			select {
			case result := <-done:
				if result.err != nil {
					t.Fatal(result.err)
				}
				rejected, ok := result.view.AsRejected()
				if !ok || rejected.Reason != "forbidden" {
					t.Fatal("already committed current permission denial still returned body")
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			original, err := contentService(t, w).GetCommand(ctx, contentCommandGetWire(t, request.CommandID), &contentPrincipal)
			if err != nil {
				t.Fatal(err)
			}
			found, ok := original.AsFound()
			if !ok {
				t.Fatal("read denial removed original command")
			}
			progress, ok := found.Progress.AsContent()
			if !ok || progress.Publication != "published" {
				t.Fatal("query denial changed publication history")
			}
			assertExactContentObjects(t, w.Directory, map[v.ContentRef]string{alphaRef: "alpha\n"})
			// A new permitted gate succeeds, then later revocation cannot retract that
			// already returned observation. No global socket/peer atomicity is asserted.
			policy.Revision = 3
			policy.Read = true
			policy.Disclose = true
			if err = w.Store().InstallFixturePolicy(ctx, policy, 2); err != nil {
				t.Fatal(err)
			}
			service = contentService(t, w)
			assertContentBody(t, ctx, service, alphaRef, nil, "alpha\n")
			policy.Revision = 4
			policy.Disclose = false
			if err = w.Store().InstallFixturePolicy(ctx, policy, 3); err != nil {
				t.Fatal(err)
			}
		})
	}
}
func TestContentReadAdmissionDeadlineDoesNotBecomeCompletionDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	w := fixture.New(t, ctx)
	installContentPolicy(t, ctx, w, alphaRef)
	service := contentService(t, w)
	if _, ok := putContentRequest(t, ctx, service, contentPut(t, alphaRef, "original", "YWxwaGEK")).AsAccepted(); !ok {
		t.Fatal("normal refused")
	}
	if _, err := service.Step(ctx); err != nil {
		t.Fatal(err)
	}
	objects := &heldContentRead{Objects: w.Objects, entered: make(chan struct{}), release: make(chan struct{})}
	service = withPublication(t, w, w.Store(), objects, time.Minute, 5*time.Second)
	until := time.Now().UTC().Add(450 * time.Millisecond)
	request, err := v.DecodeGet(contentGetWire(t, alphaRef, nil))
	if err != nil {
		t.Fatal(err)
	}
	request.AcceptBefore = v.Time(until.Truncate(time.Microsecond).Format("2006-01-02T15:04:05.000000Z"))
	raw, err := v.Encode(request)
	if err != nil {
		t.Fatal(err)
	}
	var once sync.Once
	release := func() { once.Do(func() { close(objects.release) }) }
	done := make(chan struct {
		view v.ContentGetResponse
		err  error
	}, 1)
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		view, err := service.Get(ctx, raw, &contentPrincipal)
		done <- struct {
			view v.ContentGetResponse
			err  error
		}{view, err}
	}()
	t.Cleanup(func() {
		release()
		cleanup, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		select {
		case <-finished:
		case <-cleanup.Done():
			w.RetainInvocation()
			t.Error("retain exact read")
		}
	})
	select {
	case <-objects.entered:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	waitUntil(t, ctx, until)
	release()
	select {
	case result := <-done:
		if result.err != nil {
			t.Fatal(result.err)
		}
		published, ok := result.view.AsPublished()
		if !ok || published.BytesBase64 != "YWxwaGEK" {
			t.Fatal("valid admitted read incorrectly used envelope as I/O completion deadline")
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}
