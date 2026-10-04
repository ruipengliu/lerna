//go:build integration

package component_test

import (
	"context"
	"fmt"
	fixture "github.com/ruipengliu/lerna/conformance/internal/contentfixture"
	v "github.com/ruipengliu/lerna/contract/v1_2"
	"github.com/ruipengliu/lerna/domain/content"
	"github.com/ruipengliu/lerna/runtime"
	"sync"
	"testing"
	"time"
)

func TestContentProcessingDeadlineAfterNativeDescendantLock(t *testing.T) {
	for _, consumer := range []string{"manager", "service"} {
		for _, blocked := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/blocked=%t", consumer, blocked), func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
				defer cancel()
				w := fixture.New(t, ctx)
				service := contentService(t, w)
				installContentPolicy(t, ctx, w, alphaRef)
				if _, ok := putContentRequest(t, ctx, service, contentPut(t, alphaRef, "deadline-source", "YWxwaGEK")).AsAccepted(); !ok {
					t.Fatal("normal source refused")
				}
				if _, err := service.Step(ctx); err != nil {
					t.Fatal(err)
				}
				refs := []v.ContentRef{alphaRef, alphaRef}
				for i := range refs {
					refs[i].ContentID = v.ID(fmt.Sprintf("deadline-child-%d", i))
					installContentPolicy(t, ctx, w, refs[i])
					req := contentPut(t, refs[i], string(refs[i].ContentID), "YWxwaGEK")
					req.Payload.Sources = []v.ContentRef{alphaRef}
					if _, ok := putContentRequest(t, ctx, service, req).AsAccepted(); !ok {
						t.Fatal("normal descendant refused")
					}
					if _, err := service.Step(ctx); err != nil {
						t.Fatal(err)
					}
					assertContentBody(t, ctx, service, refs[i], nil, "alpha\n")
				}
				wide := time.Now().Add(time.Hour)
				m, err := content.NewManager(content.ManagementConfig{Owner: contentOwner, Store: w.Store(), TrustedSubject: contentPrincipal, TrustedUntil: wide, PageSize: 2, WorkBudget: 500 * time.Millisecond})
				if err != nil {
					t.Fatal(err)
				}
				p := content.FixturePolicy{Ref: alphaRef, Subject: contentPrincipal, Purpose: "verification", Revision: 2, ValidUntil: wide, RetainUntil: wide, Read: true, Process: true, Save: false, Disclose: true}
				change, err := m.InstallPolicy(ctx, &contentPrincipal, p, 1)
				if err != nil {
					t.Fatal(err)
				}
				step := func() error {
					if consumer == "manager" {
						_, err := m.Step(ctx, &contentPrincipal)
						return err
					}
					_, err := service.Step(ctx)
					return err
				}
				if blocked {
					release, wait := w.HoldVersion(ctx, refs[0])
					done := make(chan error, 1)
					finished := make(chan struct{})
					go func() { defer close(finished); done <- step() }()
					joinContentRead(t, w, release, finished)
					if err = wait(ctx); err != nil {
						t.Fatal(err)
					}
					waitUntil(t, ctx, change.Deadline.Add(20*time.Millisecond))
					if err = release(); err != nil {
						t.Fatal(err)
					}
					select {
					case err = <-done:
					case <-ctx.Done():
						t.Fatal(ctx.Err())
					}
					if err != nil {
						t.Fatal(err)
					}
				} else if err = step(); err != nil {
					t.Fatal(err)
				}
				w.Reopen(ctx)
				m = trustedContentManager(t, w, 2)
				after, err := m.ObserveChange(ctx, &contentPrincipal, change.Key, "", 64)
				if err != nil {
					t.Fatal(err)
				}
				if blocked {
					if after.Change.State != "residual" || after.Change.Reason != "original_deadline_expired" || after.Change.Cursor != change.Cursor || !after.Change.Deadline.Equal(change.Deadline) || !after.Change.Due.Equal(change.Due) || after.Change.Watermark != change.Watermark {
						t.Fatal("native lock crossed original deadline but qualified page progress", after.Change)
					}
					if len(after.Responsibilities) < 1 || after.Responsibilities[0].BodyCleanup != "pending" {
						t.Fatal("known holder responsibility lost", after)
					}
				} else if after.Change.State != "scheduled" || len(after.Responsibilities) != 3 {
					t.Fatal("normal bounded page refused", after)
				}
			})
		}
	}
}

// This finite barrier follows an actual PG due read. It tests the final clock
// across all visited changes; it is a mechanical wait, not a native PG fault.
type heldProcessingDue struct {
	content.ManagementRepository
	entered, release chan struct{}
	held             bool
}

func (h *heldProcessingDue) NextPolicyDue(ctx context.Context, tx runtime.Tx, id string) (time.Time, error) {
	due, err := h.ManagementRepository.NextPolicyDue(ctx, tx, id)
	if err != nil || h.held {
		return due, err
	}
	h.held = true
	close(h.entered)
	select {
	case <-h.release:
		return due, nil
	case <-ctx.Done():
		return time.Time{}, ctx.Err()
	}
}
func TestContentProcessingDeadlineRechecksAllChangesAtFinalDecision(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	w := fixture.New(t, ctx)
	_, request, source := publishedDirectContent(t, ctx, w)
	wide := time.Now().Add(time.Hour)
	m, err := content.NewManager(content.ManagementConfig{Owner: contentOwner, Store: w.Store(), TrustedSubject: contentPrincipal, TrustedUntil: wide, PageSize: 2, WorkBudget: 500 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	policies := []content.FixturePolicy{
		{Ref: source, Subject: contentPrincipal, Purpose: "verification", Revision: 2, ValidUntil: wide, RetainUntil: wide, Read: true, Process: true, Save: false, Disclose: true},
		{Ref: source, Subject: contentPrincipal, Purpose: "other-purpose", Revision: 1, ValidUntil: wide, RetainUntil: wide, Read: true, Process: true, Save: false, Disclose: true},
	}
	changes := make([]content.PolicyChange, 2)
	for i, p := range policies {
		expected := int64(0)
		if i == 0 {
			expected = 1
		}
		changes[i], err = m.InstallPolicy(ctx, &contentPrincipal, p, expected)
		if err != nil {
			t.Fatal(err)
		}
	}
	h := &heldProcessingDue{ManagementRepository: w.Store(), entered: make(chan struct{}), release: make(chan struct{})}
	m, err = content.NewManager(content.ManagementConfig{Owner: contentOwner, Store: h, TrustedSubject: contentPrincipal, TrustedUntil: wide, PageSize: 2, WorkBudget: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	finished := make(chan struct{})
	var once sync.Once
	release := func() error { once.Do(func() { close(h.release) }); return nil }
	go func() { defer close(finished); _, e := m.Step(ctx, &contentPrincipal); done <- e }()
	joinContentRead(t, w, release, finished)
	select {
	case <-h.entered:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	waitUntil(t, ctx, changes[1].Deadline.Add(20*time.Millisecond))
	if err = release(); err != nil {
		t.Fatal(err)
	}
	select {
	case err = <-done:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if err != nil {
		t.Fatal(err)
	}
	w.Reopen(ctx)
	m = trustedContentManager(t, w, 2)
	for _, original := range changes {
		view, e := m.ObserveChange(ctx, &contentPrincipal, original.Key, "", 64)
		if e != nil || view.Change.State != "residual" || !view.Change.Deadline.Equal(original.Deadline) || view.Change.Cursor != original.Cursor || view.Change.Reason != "original_deadline_expired" {
			t.Fatal("final decision missed an earlier original change deadline", e, view.Change)
		}
		if len(view.Responsibilities) != 2 {
			t.Fatal("already recorded actual holder facts lost", view)
		}
	}
	_ = request
}

func TestContentHistoricalTighteningClassifiesNewCapAtCurrentTime(t *testing.T) {
	for _, late := range []bool{false, true} {
		t.Run(fmt.Sprintf("late=%t", late), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			w := fixture.New(t, ctx)
			_, request, source := publishedDirectContent(t, ctx, w)
			m := trustedContentManager(t, w, 2)
			narrow := time.Now().UTC().Add(500 * time.Millisecond).Truncate(time.Microsecond)
			wide := time.Now().Add(time.Hour)
			policy := content.FixturePolicy{Ref: source, Subject: contentPrincipal, Purpose: "verification", Revision: 2, ValidUntil: wide, RetainUntil: narrow, Read: true, Process: true, Save: true, Disclose: true}
			change, err := m.InstallPolicy(ctx, &contentPrincipal, policy, 1)
			if err != nil {
				t.Fatal(err)
			}
			if late {
				waitUntil(t, ctx, narrow.Add(20*time.Millisecond))
			}
			if _, err = m.Step(ctx, &contentPrincipal); err != nil {
				t.Fatal(err)
			}
			view, err := m.ObserveChange(ctx, &contentPrincipal, change.Key, "", 64)
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, r := range view.Responsibilities {
				if r.Ref == request.Payload.ContentRef {
					found = true
					if late && (r.BodyCleanup != "pending" || len(r.Actions) != 5 || !r.ObjectHolder) {
						t.Fatal("newly persisted cap is already expired but holder recorded not_required", r)
					}
					if !late && r.BodyCleanup != "not_required" {
						t.Fatal("future narrower retention misclassified", r)
					}
				}
			}
			if !found {
				t.Fatal("derived holder missing", view)
			}
		})
	}
}

func TestContentDelayedHistoricalPageHandsOffOriginalNaturalValidUntil(t *testing.T) {
	for _, late := range []bool{false, true} {
		t.Run(fmt.Sprintf("late=%t", late), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			w := fixture.New(t, ctx)
			service, request, source := publishedDirectContent(t, ctx, w)
			m := trustedContentManager(t, w, 2)
			due := time.Now().UTC().Add(500 * time.Millisecond).Truncate(time.Microsecond)
			wide := time.Now().Add(time.Hour)
			policy := content.FixturePolicy{Ref: source, Subject: contentPrincipal, Purpose: "verification", Revision: 2, ValidUntil: due, RetainUntil: wide, Read: true, Process: true, Save: true, Disclose: true}
			original, err := m.InstallPolicy(ctx, &contentPrincipal, policy, 1)
			if err != nil {
				t.Fatal(err)
			}
			if late {
				waitUntil(t, ctx, due.Add(20*time.Millisecond))
			}
			for i := 0; i < 3; i++ {
				if _, err = m.Step(ctx, &contentPrincipal); err != nil {
					t.Fatal(err)
				}
			}
			view, err := m.ObserveChange(ctx, &contentPrincipal, original.Key, "", 64)
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, r := range view.Responsibilities {
				if r.Ref == request.Payload.ContentRef {
					found = true
					if late && (r.BodyCleanup != "pending" || len(r.Actions) != 5 || !r.ObjectHolder) {
						t.Fatal("initial history completed after ValidUntil without durable original natural check", r, view.Change)
					}
					if !late && r.BodyCleanup != "not_required" {
						t.Fatal("normal future ValidUntil misclassified", r)
					}
				}
			}
			if !found {
				t.Fatal("target responsibility absent", view)
			}
			if late {
				if view.Change.Phase != "natural_expiry" || !view.Change.Due.Equal(original.ExpiryDue) || !view.Change.Deadline.Equal(original.ExpiryDeadline) || view.Change.Watermark != original.Watermark {
					t.Fatal("natural handoff refreshed original cutoff/watermark", view.Change)
				}
			} else {
				assertContentBody(t, ctx, service, request.Payload.ContentRef, nil, "alpha\n")
			}
		})
	}
}
