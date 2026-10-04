//go:build integration

package component_test

import (
	"context"
	fixture "github.com/ruipengliu/lerna/conformance/internal/contentfixture"
	v "github.com/ruipengliu/lerna/contract/v1_2"
	"github.com/ruipengliu/lerna/domain/content"
	"os"
	"testing"
	"time"
)

func waitUntil(t *testing.T, ctx context.Context, until time.Time) {
	t.Helper()
	timer := time.NewTimer(time.Until(until) + 15*time.Millisecond)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}
func TestContentLockWaitCannotReuseExpiredAdmissionClock(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	w := fixture.New(t, ctx)
	installContentPolicy(t, ctx, w, alphaRef)
	service := contentService(t, w)
	until := time.Now().UTC().Add(450 * time.Millisecond)
	request := contentPut(t, alphaRef, "waited-admission", "YWxwaGEK")
	request.AcceptBefore = v.Time(until.Truncate(time.Microsecond).Format("2006-01-02T15:04:05.000000Z"))
	raw, err := v.Encode(request)
	if err != nil {
		t.Fatal(err)
	}
	release, waitBlocked := w.HoldPolicy(ctx, alphaRef)
	done := make(chan struct {
		out v.TransportOutcome
		err error
	}, 1)
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		out, err := service.Put(ctx, raw, &contentPrincipal)
		done <- struct {
			out v.TransportOutcome
			err error
		}{out, err}
	}()
	t.Cleanup(func() {
		_ = release()
		cleanup, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		select {
		case <-finished:
		case <-cleanup.Done():
			w.RetainInvocation()
			t.Error("exact public invocation still active; preserve scope")
		}
	})
	if err = waitBlocked(ctx); err != nil {
		t.Fatal("real exact policy lock did not block admission", err)
	}
	waitUntil(t, ctx, until)
	if err = release(); err != nil {
		t.Fatal(err)
	}
	select {
	case result := <-done:
		if result.err != nil {
			t.Fatal(result.err)
		}
		received, ok := result.out.AsReceived()
		if !ok {
			t.Fatal("expired admission did not fix rejection")
		}
		rejected, ok := received.Receipt.AsRejected()
		if !ok || rejected.Reason != "expired" {
			t.Fatal("locked admission reused pre-wait trusted clock")
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	entries, err := os.ReadDir(w.Directory)
	if err != nil || len(entries) != 0 {
		t.Fatal("expired admission wrote bytes", err)
	}
	// A new, finite deadline succeeds after the exact lock's real release.
	if _, ok := putContentRequest(t, ctx, service, contentPut(t, alphaRef, "normal-after-wait", "YWxwaGEK")).AsAccepted(); !ok {
		t.Fatal("normal control refused")
	}
	if _, err = service.Step(ctx); err != nil {
		t.Fatal(err)
	}
	assertContentBody(t, ctx, service, alphaRef, nil, "alpha\n")
}
func TestContentPublicationWaitCannotReuseExpiredPolicyClock(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	w := fixture.New(t, ctx)
	installContentPolicy(t, ctx, w, alphaRef)
	service := contentService(t, w)
	request := contentPut(t, alphaRef, "waited-publication", "YWxwaGEK")
	if _, ok := putContentRequest(t, ctx, service, request).AsAccepted(); !ok {
		t.Fatal("normal admission refused")
	}
	until := time.Now().UTC().Add(450 * time.Millisecond)
	wide := time.Now().Add(time.Hour)
	if err := w.Store().InstallFixturePolicy(ctx, content.FixturePolicy{Ref: alphaRef, Subject: contentPrincipal, Purpose: "verification", Revision: 2, ValidUntil: until, RetainUntil: wide, Read: true, Process: true, Save: true, Disclose: true}, 1); err != nil {
		t.Fatal(err)
	}
	release, waitBlocked := w.HoldPolicy(ctx, alphaRef)
	done := make(chan error, 1)
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		processed, err := service.Step(ctx)
		if err == nil && !processed {
			err = content.ErrUnavailable
		}
		done <- err
	}()
	t.Cleanup(func() {
		_ = release()
		cleanup, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		select {
		case <-finished:
		case <-cleanup.Done():
			w.RetainInvocation()
			t.Error("exact publication still active; preserve scope")
		}
	})
	if err := waitBlocked(ctx); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, ctx, until)
	if err := release(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	entries, err := os.ReadDir(w.Directory)
	if err != nil || len(entries) != 0 {
		t.Fatal("expired policy still started real object bytes", err)
	}
	// Only a new policy restores reading; historical failure stays failed.
	if err = w.Store().InstallFixturePolicy(ctx, content.FixturePolicy{Ref: alphaRef, Subject: contentPrincipal, Purpose: "verification", Revision: 3, ValidUntil: wide, RetainUntil: wide, Read: true, Process: true, Save: true, Disclose: true}, 2); err != nil {
		t.Fatal(err)
	}
	view, err := service.Get(ctx, contentGetWire(t, alphaRef, nil), &contentPrincipal)
	if err != nil {
		t.Fatal(err)
	}
	failed, ok := view.AsFailed()
	if !ok || failed.Reason != "forbidden" {
		t.Fatal("expired waited policy erased historical failure")
	}
	control := alphaRef
	control.ContentID = "publication-control"
	installContentPolicy(t, ctx, w, control)
	if _, ok := putContentRequest(t, ctx, service, contentPut(t, control, "control", "YWxwaGEK")).AsAccepted(); !ok {
		t.Fatal("normal control refused")
	}
	if _, err = service.Step(ctx); err != nil {
		t.Fatal(err)
	}
	assertContentBody(t, ctx, service, control, nil, "alpha\n")
}
