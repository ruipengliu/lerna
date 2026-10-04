//go:build integration

package component_test

import (
	"context"
	"errors"
	fixture "github.com/ruipengliu/lerna/conformance/internal/contentfixture"
	"github.com/ruipengliu/lerna/contract"
	v "github.com/ruipengliu/lerna/contract/v1_2"
	"github.com/ruipengliu/lerna/domain/content"
	"github.com/ruipengliu/lerna/runtime"
	"os"
	"testing"
	"time"
)

// This seam is a mechanical temporary object failure, not a native I/O fault.
// The succeeding control delegates to the real local adapter.
type temporaryObjects struct {
	content.Objects
	fail func() error
}

func (o *temporaryObjects) Put(ctx context.Context, key, attempt, hash string, length int64, bytes []byte) error {
	if o.fail != nil {
		f := o.fail
		o.fail = nil
		return f()
	}
	return o.Objects.Put(ctx, key, attempt, hash, length, bytes)
}
func withPublication(t *testing.T, w *fixture.World, store content.Repository, objects content.Objects, lease, work time.Duration) *content.Service {
	t.Helper()
	s, err := content.New(content.Config{Owner: contentOwner, Store: store, Objects: objects, Limits: content.Limits{MaxPreparingVersions: 16, MaxStagingBytes: 1048576, Lease: lease, WorkTimeout: work}, PublishBudget: time.Minute, MaxPublicationAttempts: 3, Worker: "retry-conformance"})
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func TestContentTemporaryFailureKeepsNarrowedRetentionAfterReopen(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	w := fixture.New(t, ctx)
	installContentPolicy(t, ctx, w, alphaRef)
	service := contentService(t, w)
	request := contentPut(t, alphaRef, "original-retention", "YWxwaGEK")
	first := putContentRequest(t, ctx, service, request)
	if _, ok := first.AsAccepted(); !ok {
		t.Fatal("normal admission absent")
	}
	narrow := time.Now().UTC().Add(800 * time.Millisecond)
	wide := time.Now().UTC().Add(time.Hour)
	objects := &temporaryObjects{Objects: w.Objects, fail: func() error {
		if err := w.Store().InstallFixturePolicy(ctx, content.FixturePolicy{Ref: alphaRef, Subject: contentPrincipal, Purpose: "verification", Revision: 2, ValidUntil: wide, RetainUntil: narrow, Read: true, Process: true, Save: true, Disclose: true}, 1); err != nil {
			return err
		}
		return errors.New("mechanical temporary object fault")
	}}
	service = withPublication(t, w, w.Store(), objects, time.Minute, 5*time.Second)
	if processed, err := service.Step(ctx); err != nil || !processed {
		t.Fatal("temporary attempt not deferred", err)
	}
	w.Reopen(ctx)
	service = contentService(t, w)
	if err := w.Store().InstallFixturePolicy(ctx, content.FixturePolicy{Ref: alphaRef, Subject: contentPrincipal, Purpose: "verification", Revision: 3, ValidUntil: wide, RetainUntil: wide, Read: true, Process: true, Save: true, Disclose: true}, 2); err != nil {
		t.Fatal(err)
	}
	timer := time.NewTimer(time.Until(narrow) + 20*time.Millisecond)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if processed, err := service.Step(ctx); err != nil || !processed {
		t.Fatal("original retry did not terminate", err)
	}
	view, err := service.Get(ctx, contentGetWire(t, alphaRef, nil), &contentPrincipal)
	if err != nil {
		t.Fatal(err)
	}
	expired, ok := view.AsRejected()
	if !ok || expired.Reason != "expired" {
		t.Fatal("policy widening recovered a previously narrowed retention")
	}
	entries, err := os.ReadDir(w.Directory)
	if err != nil || len(entries) != 0 {
		t.Fatal("expired original still installed bytes", err)
	}
	original, err := service.GetCommand(ctx, contentCommandGetWire(t, request.CommandID), &contentPrincipal)
	if err != nil {
		t.Fatal(err)
	}
	found, ok := original.AsFound()
	if !ok {
		t.Fatal("original command absent")
	}
	progress, ok := found.Progress.AsContent()
	if !ok || progress.Publication != "failed" {
		t.Fatal("expired retry did not retain failed history")
	}
	a, _ := v.Encode(first)
	b, _ := v.Encode(found.Receipt)
	if string(a) != string(b) {
		t.Fatal("retry narrowed fixed accepted receipt")
	}
	// Fresh independently accepted content succeeds with the same real adapter.
	control := alphaRef
	control.ContentID = "normal-control"
	installContentPolicy(t, ctx, w, control)
	if _, ok := putContentRequest(t, ctx, service, contentPut(t, control, "control", "YWxwaGEK")).AsAccepted(); !ok {
		t.Fatal("control refused")
	}
	if _, err = service.Step(ctx); err != nil {
		t.Fatal(err)
	}
	assertContentBody(t, ctx, service, control, nil, "alpha\n")
}

// delayAfterCommit models finite scheduler delay after an actual durable Claim.
type delayAfterCommit struct {
	content.Repository
	delay time.Duration
}

func (r *delayAfterCommit) Within(ctx context.Context, owner contract.OwnerRef, fn func(context.Context, runtime.Tx) error) error {
	err := r.Repository.Within(ctx, owner, fn)
	if err == nil && r.delay > 0 {
		delay := r.delay
		r.delay = 0
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return err
}
func TestContentExpiredOriginalClaimCannotStartObjectIO(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	w := fixture.New(t, ctx)
	installContentPolicy(t, ctx, w, alphaRef)
	request := contentPut(t, alphaRef, "claim-expiration", "YWxwaGEK")
	service := contentService(t, w)
	if _, ok := putContentRequest(t, ctx, service, request).AsAccepted(); !ok {
		t.Fatal("normal admission absent")
	}
	delayed := &delayAfterCommit{Repository: w.Store(), delay: 450 * time.Millisecond}
	service = withPublication(t, w, delayed, w.Objects, 400*time.Millisecond, 300*time.Millisecond)
	_, err := service.Step(ctx)
	if !errors.Is(err, runtime.ErrClaim) {
		t.Fatal("expired native Claim not rejected", err)
	}
	entries, err := os.ReadDir(w.Directory)
	if err != nil || len(entries) != 0 {
		t.Fatal("expired Claim started real object installation", err)
	}
	service = withPublication(t, w, w.Store(), w.Objects, 400*time.Millisecond, 300*time.Millisecond)
	if processed, err := service.Step(ctx); err != nil || !processed {
		t.Fatal("fresh Claim could not resume original", err)
	}
	assertContentBody(t, ctx, service, alphaRef, nil, "alpha\n")
}
