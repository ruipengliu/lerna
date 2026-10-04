//go:build integration

package component_test

import (
	"context"
	fixture "github.com/ruipengliu/lerna/conformance/internal/contentfixture"
	v "github.com/ruipengliu/lerna/contract/v1_2"
	"github.com/ruipengliu/lerna/domain/content"
	"testing"
	"time"
)

func joinContentRead(t *testing.T, w *fixture.World, release func() error, finished <-chan struct{}) {
	t.Helper()
	t.Cleanup(func() {
		if err := release(); err != nil {
			t.Error(err)
		}
		cleanup, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		select {
		case <-finished:
		case <-cleanup.Done():
			w.RetainInvocation()
			t.Error("retain exact blocked read until actual exit")
		}
	})
}
func contentReadDeadline(t *testing.T, ref v.ContentRef, until time.Time) []byte {
	t.Helper()
	request, err := v.DecodeGet(contentGetWire(t, ref, nil))
	if err != nil {
		t.Fatal(err)
	}
	request.AcceptBefore = v.Time(until.UTC().Truncate(time.Microsecond).Format("2006-01-02T15:04:05.000000Z"))
	raw, err := v.Encode(request)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
func commandReadDeadline(t *testing.T, id v.ID, until time.Time) []byte {
	t.Helper()
	request, err := v.DecodeCommand(contentCommandGetWire(t, id))
	if err != nil {
		t.Fatal(err)
	}
	request.AcceptBefore = v.Time(until.UTC().Truncate(time.Microsecond).Format("2006-01-02T15:04:05.000000Z"))
	raw, err := v.Encode(request)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
func TestContentReadPolicyWaitRespectsAdmissionForEveryObservation(t *testing.T) {
	for _, state := range []string{"not_found", "preparing", "failed", "published", "mismatched"} {
		t.Run(state, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			w := fixture.New(t, ctx)
			installContentPolicy(t, ctx, w, alphaRef)
			service := contentService(t, w)
			if state != "not_found" {
				if _, ok := putContentRequest(t, ctx, service, contentPut(t, alphaRef, "original", "YWxwaGEK")).AsAccepted(); !ok {
					t.Fatal("normal refused")
				}
			}
			if state == "failed" {
				wide := time.Now().Add(time.Hour)
				if err := w.Store().InstallFixturePolicy(ctx, content.FixturePolicy{Ref: alphaRef, Subject: contentPrincipal, Purpose: "verification", Revision: 2, ValidUntil: wide, RetainUntil: wide, Read: true, Process: true, Save: false, Disclose: true}, 1); err != nil {
					t.Fatal(err)
				}
			}
			if state == "failed" || state == "published" || state == "mismatched" {
				if _, err := service.Step(ctx); err != nil {
					t.Fatal(err)
				}
			}
			wanted := alphaRef
			if state == "mismatched" {
				wanted.MediaType = "application/octet-stream"
			}
			release, waitBlocked := w.HoldPolicy(ctx, alphaRef)
			until := time.Now().Add(450 * time.Millisecond)
			raw := contentReadDeadline(t, wanted, until)
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
			joinContentRead(t, w, release, finished)
			if err := waitBlocked(ctx); err != nil {
				t.Fatal(err)
			}
			waitUntil(t, ctx, until)
			if err := release(); err != nil {
				t.Fatal(err)
			}
			select {
			case result := <-done:
				if result.err != nil {
					t.Fatal(result.err)
				}
				r, ok := result.view.AsRejected()
				if !ok || r.Reason != "expired" {
					t.Fatal("policy lock crossed cutoff but returned original observation")
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			normal, err := service.Get(ctx, contentGetWire(t, wanted, nil), &contentPrincipal)
			if err != nil {
				t.Fatal(err)
			}
			wire, err := v.Encode(normal)
			if err != nil {
				t.Fatal(err)
			}
			status, err := v.ParseJSON(wire)
			if err != nil {
				t.Fatal(err)
			}
			if state == "mismatched" {
				r, ok := normal.AsRejected()
				if !ok || r.Reason != "integrity" {
					t.Fatal("normal exact metadata guard changed")
				}
			} else if status.(map[string]any)["status"] != state {
				t.Fatal("normal permitted state unavailable")
			}
		})
	}
}
func TestContentReadVersionWaitCannotReuseAdmissionClock(t *testing.T) {
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
	release, waitBlocked := w.HoldVersion(ctx, alphaRef)
	until := time.Now().Add(450 * time.Millisecond)
	raw := contentReadDeadline(t, alphaRef, until)
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
	joinContentRead(t, w, release, finished)
	if err := waitBlocked(ctx); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, ctx, until)
	if err := release(); err != nil {
		t.Fatal(err)
	}
	select {
	case result := <-done:
		if result.err != nil {
			t.Fatal(result.err)
		}
		r, ok := result.view.AsRejected()
		if !ok || r.Reason != "expired" {
			t.Fatal("version lock crossed cutoff but returned bytes")
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	assertContentBody(t, ctx, service, alphaRef, nil, "alpha\n")
}
func TestContentCommandWaitRechecksReaderAndAdmission(t *testing.T) {
	for _, kind := range []string{"reader-envelope", "version-envelope", "version-reader-expiry", "reader-absent"} {
		t.Run(kind, func(t *testing.T) {
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
			until := time.Now().Add(450 * time.Millisecond)
			var release func() error
			var waitBlocked func(context.Context) error
			id := v.ID("original")
			deadline := until
			reason := v.ErrorCode("expired")
			if kind == "version-reader-expiry" {
				if err := w.Store().InstallFixtureCommandReader(ctx, contentOwner, contentPrincipal, until); err != nil {
					t.Fatal(err)
				}
				deadline = time.Now().Add(time.Minute)
				reason = "forbidden"
			}
			if kind == "reader-envelope" || kind == "reader-absent" {
				release, waitBlocked = w.HoldCommandReader(ctx, contentOwner)
			} else {
				release, waitBlocked = w.HoldVersion(ctx, alphaRef)
			}
			if kind == "reader-absent" {
				id = "absent"
			}
			raw := commandReadDeadline(t, id, deadline)
			done := make(chan struct {
				view v.CommandGetResponse
				err  error
			}, 1)
			finished := make(chan struct{})
			go func() {
				defer close(finished)
				view, err := service.GetCommand(ctx, raw, &contentPrincipal)
				done <- struct {
					view v.CommandGetResponse
					err  error
				}{view, err}
			}()
			joinContentRead(t, w, release, finished)
			if err := waitBlocked(ctx); err != nil {
				t.Fatal(err)
			}
			waitUntil(t, ctx, until)
			if err := release(); err != nil {
				t.Fatal(err)
			}
			select {
			case result := <-done:
				if result.err != nil {
					t.Fatal(result.err)
				}
				r, ok := result.view.AsRejected()
				if !ok || r.Reason != reason {
					t.Fatal("command lock reused expired original read eligibility")
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			if err := w.Store().InstallFixtureCommandReader(ctx, contentOwner, contentPrincipal, time.Now().Add(time.Hour)); err != nil {
				t.Fatal(err)
			}
			normal, err := service.GetCommand(ctx, contentCommandGetWire(t, id), &contentPrincipal)
			if err != nil {
				t.Fatal(err)
			}
			if id == "absent" {
				if _, ok := normal.AsNotFound(); !ok {
					t.Fatal("normal permitted absent query refused")
				}
			} else {
				found, ok := normal.AsFound()
				if !ok {
					t.Fatal("normal original command refused")
				}
				progress, ok := found.Progress.AsContent()
				if !ok || progress.Publication != "published" {
					t.Fatal("read query altered historical publication")
				}
			}
		})
	}
}

func TestContentLastDirectSourceWaitCannotReuseReadAdmission(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	w := fixture.New(t, ctx)
	service, request, last := publishedDirectContent(t, ctx, w)
	release, waitBlocked := w.HoldVersion(ctx, last)
	until := time.Now().Add(450 * time.Millisecond)
	raw := contentReadDeadline(t, request.Payload.ContentRef, until)
	done := make(chan v.ContentGetResponse, 1)
	failures := make(chan error, 1)
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		view, err := service.Get(ctx, raw, &contentPrincipal)
		done <- view
		failures <- err
	}()
	joinContentRead(t, w, release, finished)
	if err := waitBlocked(ctx); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, ctx, until)
	if err := release(); err != nil {
		t.Fatal(err)
	}
	select {
	case view := <-done:
		if err := <-failures; err != nil {
			t.Fatal(err)
		}
		denied, ok := view.AsRejected()
		if !ok || denied.Reason != "expired" {
			t.Fatal("last source lock reused old admission time")
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	assertContentBody(t, ctx, service, request.Payload.ContentRef, nil, "alpha\n")
}
