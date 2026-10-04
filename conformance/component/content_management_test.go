//go:build integration

package component_test

import (
	"context"
	"errors"
	"fmt"
	fixture "github.com/ruipengliu/lerna/conformance/internal/contentfixture"
	v "github.com/ruipengliu/lerna/contract/v1_2"
	"github.com/ruipengliu/lerna/domain/content"
	"github.com/ruipengliu/lerna/runtime"
	"sync"
	"testing"
	"time"
)

func TestContentTrustedManagementSealsUseAndReopensResponsibility(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	w := fixture.New(t, ctx)
	service := contentService(t, w)
	installContentPolicy(t, ctx, w, alphaRef)
	if _, ok := putContentRequest(t, ctx, service, contentPut(t, alphaRef, "source", "YWxwaGEK")).AsAccepted(); !ok {
		t.Fatal("normal refused")
	}
	if _, err := service.Step(ctx); err != nil {
		t.Fatal(err)
	}
	manager, err := content.NewManager(content.ManagementConfig{Owner: contentOwner, Store: w.Store(), TrustedSubject: contentPrincipal, TrustedUntil: time.Now().Add(time.Hour), PageSize: 2, WorkBudget: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	wide := time.Now().Add(time.Hour)
	p := content.FixturePolicy{Ref: alphaRef, Subject: contentPrincipal, Purpose: "verification", Revision: 2, ValidUntil: wide, RetainUntil: wide, Read: true, Process: true, Save: false, Disclose: true}
	change, err := manager.InstallPolicy(ctx, &contentPrincipal, p, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err = service.AuthorizeUse(ctx, &contentPrincipal, alphaRef, "verification", "save"); err == nil {
		t.Fatal("save revoke bypassed")
	}
	assertContentBody(t, ctx, service, alphaRef, nil, "alpha\n")
	w.Reopen(ctx)
	manager, err = content.NewManager(content.ManagementConfig{Owner: contentOwner, Store: w.Store(), TrustedSubject: contentPrincipal, TrustedUntil: time.Now().Add(time.Hour), PageSize: 2, WorkBudget: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	observation, err := manager.ObserveChange(ctx, &contentPrincipal, change.Key, "", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(observation.Responsibilities) != 1 || observation.Responsibilities[0].BodyCleanup != "pending" || !observation.Responsibilities[0].ObjectHolder {
		t.Fatal("holder responsibility lost", observation)
	}
	bad := contentPrincipal
	bad.SubjectID = "untrusted"
	if _, err = manager.ObserveChange(ctx, &bad, change.Key, "", 2); err == nil {
		t.Fatal("management leaked to untrusted")
	}
	wrong := p
	wrong.Revision = 3
	wrong.Ref.MediaType = "application/json"
	if _, err = manager.InstallPolicy(ctx, &contentPrincipal, wrong, 2); err == nil {
		t.Fatal("incorrect full resource binding accepted")
	}
	replay, err := manager.InstallPolicy(ctx, &contentPrincipal, p, 1)
	if err != nil || replay.Key != change.Key {
		t.Fatal("management replay changed responsibility", err)
	}
	p.Disclose = false
	if _, err = manager.InstallPolicy(ctx, &contentPrincipal, p, 1); err == nil {
		t.Fatal("revision reused for different policy")
	}
	var ce *content.ManagementConflict
	if !errors.As(err, &ce) {
		t.Fatal("conflict not identifiable", err)
	}
}

func trustedContentManager(t *testing.T, w *fixture.World, page int) *content.Manager {
	t.Helper()
	m, err := content.NewManager(content.ManagementConfig{Owner: contentOwner, Store: w.Store(), TrustedSubject: contentPrincipal, TrustedUntil: time.Now().Add(time.Hour), PageSize: page, WorkBudget: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	return m
}
func TestContentPolicyPropagationAllDescendantPagesRecoverFrozenWatermark(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	w := fixture.New(t, ctx)
	service := contentService(t, w)
	installContentPolicy(t, ctx, w, alphaRef)
	if _, ok := putContentRequest(t, ctx, service, contentPut(t, alphaRef, "source", "YWxwaGEK")).AsAccepted(); !ok {
		t.Fatal("source refused")
	}
	if _, err := service.Step(ctx); err != nil {
		t.Fatal(err)
	}
	refs := make([]v.ContentRef, 65)
	for i := range refs {
		ref := alphaRef
		ref.ContentID = v.ID(fmt.Sprintf("descendant-%02d", i))
		refs[i] = ref
		installContentPolicy(t, ctx, w, ref)
		req := contentPut(t, ref, string(ref.ContentID), "YWxwaGEK")
		req.Payload.Sources = []v.ContentRef{alphaRef}
		if _, ok := putContentRequest(t, ctx, service, req).AsAccepted(); !ok {
			t.Fatal("normal descendant refused", i)
		}
		if _, err := service.Step(ctx); err != nil {
			t.Fatal(err)
		}
	}
	m := trustedContentManager(t, w, 7)
	wide := time.Now().Add(time.Hour)
	p := content.FixturePolicy{Ref: alphaRef, Subject: contentPrincipal, Purpose: "verification", Revision: 2, ValidUntil: wide, RetainUntil: wide, Read: true, Process: true, Save: false, Disclose: true}
	change, err := m.InstallPolicy(ctx, &contentPrincipal, p, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err = service.AuthorizeUse(ctx, &contentPrincipal, refs[64], "verification", "save"); err == nil {
		t.Fatal("last descendant use was not immediately sealed")
	}
	if worked, err := m.Step(ctx, &contentPrincipal); err != nil || !worked {
		t.Fatal("first page", err)
	}
	first, err := m.ObserveChange(ctx, &contentPrincipal, change.Key, "", 64)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Responsibilities) != 8 || first.Change.Cursor == "" {
		t.Fatal("first bounded page not durable", first)
	}
	watermark := first.Change.Watermark
	deadline := first.Change.Deadline
	w.Reopen(ctx)
	m = trustedContentManager(t, w, 7)
	for i := 0; i < 12; i++ {
		observation, err := m.ObserveChange(ctx, &contentPrincipal, change.Key, "", 64)
		if err != nil {
			t.Fatal(err)
		}
		if observation.Change.State != "pending" {
			break
		}
		if _, err = m.Step(ctx, &contentPrincipal); err != nil {
			t.Fatal(err)
		}
	}
	seen := map[v.ContentRef]bool{}
	cursor := ""
	for i := 0; i < 12; i++ {
		observation, err := m.ObserveChange(ctx, &contentPrincipal, change.Key, cursor, 7)
		if err != nil {
			t.Fatal(err)
		}
		if observation.Change.State == "pending" || observation.Change.Watermark != watermark || !observation.Change.Deadline.Equal(deadline) && observation.Change.State != "scheduled" {
			t.Fatal("original progress lost", observation.Change)
		}
		for _, responsibility := range observation.Responsibilities {
			if responsibility.BodyCleanup != "pending" || !responsibility.ObjectHolder || responsibility.Residual != "holder_unconfirmed" {
				t.Fatal("physical cleanup falsely complete", responsibility)
			}
			if seen[responsibility.Ref] {
				t.Fatal("duplicate responsibility")
			}
			seen[responsibility.Ref] = true
		}
		cursor = observation.NextCursor
		if cursor == "" {
			break
		}
	}
	if len(seen) != 66 || !seen[refs[64]] || !seen[alphaRef] {
		t.Fatal("descendants truncated", len(seen))
	}
	assertContentBody(t, ctx, contentService(t, w), refs[64], nil, "alpha\n")
}

func TestContentFiveActionsAreIndependentCurrentQualification(t *testing.T) {
	for _, action := range []string{"read", "process", "save", "sync", "disclose"} {
		t.Run(action, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			w := fixture.New(t, ctx)
			service := contentService(t, w)
			installContentPolicy(t, ctx, w, alphaRef)
			if _, ok := putContentRequest(t, ctx, service, contentPut(t, alphaRef, "source", "YWxwaGEK")).AsAccepted(); !ok {
				t.Fatal("normal refused")
			}
			if _, err := service.Step(ctx); err != nil {
				t.Fatal(err)
			}
			m := trustedContentManager(t, w, 2)
			wide := time.Now().Add(time.Hour)
			p := content.FixturePolicy{Ref: alphaRef, Subject: contentPrincipal, Purpose: "verification", Revision: 2, ValidUntil: wide, RetainUntil: wide, Read: true, Process: true, Save: true, Sync: true, Disclose: true}
			if _, err := m.InstallPolicy(ctx, &contentPrincipal, p, 1); err != nil {
				t.Fatal(err)
			}
			for _, normal := range []string{"read", "process", "save", "sync", "disclose"} {
				if err := service.AuthorizeUse(ctx, &contentPrincipal, alphaRef, "verification", normal); err != nil {
					t.Fatal("normal action", normal, err)
				}
			}
			p.Revision = 3
			switch action {
			case "read":
				p.Read = false
			case "process":
				p.Process = false
			case "save":
				p.Save = false
			case "sync":
				p.Sync = false
			case "disclose":
				p.Disclose = false
			}
			if _, err := m.InstallPolicy(ctx, &contentPrincipal, p, 2); err != nil {
				t.Fatal(err)
			}
			for _, current := range []string{"read", "process", "save", "sync", "disclose"} {
				err := service.AuthorizeUse(ctx, &contentPrincipal, alphaRef, "verification", current)
				if (err != nil) != (current == action) {
					t.Fatal("action permissions coupled", current, action, err)
				}
			}
			wrong := contentPrincipal
			wrong.SubjectID = "wrong-subject"
			if err := service.AuthorizeUse(ctx, &wrong, alphaRef, "verification", "read"); err == nil {
				t.Fatal("wrong subject allowed")
			}
			if err := service.AuthorizeUse(ctx, &contentPrincipal, alphaRef, "different-purpose", "read"); err == nil {
				t.Fatal("wrong purpose allowed")
			}
			altered := alphaRef
			altered.Hash = "sha256:0000000000000000000000000000000000000000000000000000000000000000"
			if err := service.AuthorizeUse(ctx, &contentPrincipal, altered, "verification", "read"); err == nil {
				t.Fatal("wrong declaration allowed")
			}
		})
	}
}

func TestContentNaturalRetentionExpiryHasDurableOriginalMaintenance(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	w := fixture.New(t, ctx)
	m := trustedContentManager(t, w, 2)
	wide := time.Now().Add(time.Hour)
	due := time.Now().UTC().Add(3 * time.Second).Truncate(time.Microsecond)
	p := content.FixturePolicy{Ref: alphaRef, Subject: contentPrincipal, Purpose: "verification", Revision: 1, ValidUntil: wide, RetainUntil: due, Read: true, Process: true, Save: true, Disclose: true}
	change, err := m.InstallPolicy(ctx, &contentPrincipal, p, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err = w.Store().InstallFixtureCommandReader(ctx, contentOwner, contentPrincipal, wide); err != nil {
		t.Fatal(err)
	}
	service := contentService(t, w)
	if _, ok := putContentRequest(t, ctx, service, contentPut(t, alphaRef, "expiring", "YWxwaGEK")).AsAccepted(); !ok {
		t.Fatal("normal refused")
	}
	if _, err = service.Step(ctx); err != nil {
		t.Fatal(err)
	}
	assertContentBody(t, ctx, service, alphaRef, nil, "alpha\n")
	before, err := m.ObserveChange(ctx, &contentPrincipal, change.Key, "", 2)
	if err != nil || before.Change.State != "scheduled" || !before.Change.Due.Equal(due) {
		t.Fatal("original expiry maintenance absent", err, before)
	}
	deadline := before.Change.Deadline
	w.Reopen(ctx)
	m = trustedContentManager(t, w, 2)
	timer := time.NewTimer(time.Until(due))
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if _, err = m.Step(ctx, &contentPrincipal); err != nil {
		t.Fatal(err)
	}
	after, err := m.ObserveChange(ctx, &contentPrincipal, change.Key, "", 2)
	if err != nil || len(after.Responsibilities) != 1 || after.Responsibilities[0].BodyCleanup != "pending" || !after.Change.Deadline.Equal(deadline) {
		t.Fatal("expiry lost responsibility or refreshed deadline", err, after)
	}
	view, err := contentService(t, w).Get(ctx, contentGetWire(t, alphaRef, nil), &contentPrincipal)
	r, ok := view.AsRejected()
	if err != nil || !ok || r.Reason != "expired" {
		t.Fatal("expired bytes disclosed", err, view)
	}
}

func TestContentPolicyAndCleanupResponsibilityRollbackTogether(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	w := fixture.New(t, ctx)
	service := contentService(t, w)
	installContentPolicy(t, ctx, w, alphaRef)
	if _, ok := putContentRequest(t, ctx, service, contentPut(t, alphaRef, "source", "YWxwaGEK")).AsAccepted(); !ok {
		t.Fatal("normal refused")
	}
	if _, err := service.Step(ctx); err != nil {
		t.Fatal(err)
	}
	m := trustedContentManager(t, w, 2)
	wide := time.Now().Add(time.Hour)
	p := content.FixturePolicy{Ref: alphaRef, Subject: contentPrincipal, Purpose: "verification", Revision: 2, ValidUntil: wide, RetainUntil: wide, Read: true, Process: true, Save: false, Disclose: true}
	release := w.FailResponsibilityWrites(ctx)
	if _, err := m.InstallPolicy(ctx, &contentPrincipal, p, 1); err == nil {
		t.Fatal("native responsibility write failure not returned")
	}
	if err := release(); err != nil {
		t.Fatal(err)
	}
	w.Reopen(ctx)
	service = contentService(t, w)
	m = trustedContentManager(t, w, 2)
	if err := service.AuthorizeUse(ctx, &contentPrincipal, alphaRef, "verification", "save"); err != nil {
		t.Fatal("policy committed without responsibility", err)
	}
	change, err := m.InstallPolicy(ctx, &contentPrincipal, p, 1)
	if err != nil {
		t.Fatal("original revision not recovered", err)
	}
	if err = service.AuthorizeUse(ctx, &contentPrincipal, alphaRef, "verification", "save"); err == nil {
		t.Fatal("normal revocation bypassed")
	}
	observation, err := m.ObserveChange(ctx, &contentPrincipal, change.Key, "", 2)
	if err != nil || len(observation.Responsibilities) != 1 || observation.Responsibilities[0].BodyCleanup != "pending" {
		t.Fatal("normal responsibility absent", err, observation)
	}
}

func TestContentMalformedPolicyBindingRegistersActualSourceResponsibility(t *testing.T) {
	for _, scope := range []string{"writer", "other-subject", "other-purpose"} {
		t.Run(scope, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			w := fixture.New(t, ctx)
			service, request, source := publishedDirectContent(t, ctx, w)
			manager := trustedContentManager(t, w, 2)
			wide := time.Now().Add(time.Hour)
			p := content.FixturePolicy{Ref: source, Subject: contentPrincipal, Purpose: "verification", Revision: 2, ValidUntil: wide, RetainUntil: wide, Read: true, Process: true, Save: true, Sync: true, Disclose: true}
			expected := int64(1)
			if scope != "writer" {
				if scope == "other-subject" {
					p.Subject.SubjectID = "other-reader"
				} else {
					p.Purpose = "another-purpose"
				}
				p.Revision = 1
				if _, err := manager.InstallPolicy(ctx, &contentPrincipal, p, 0); err != nil {
					t.Fatal(err)
				}
				p.Revision = 2
			}
			p.Ref.MediaType = "application/json"
			change, err := manager.InstallFixturePolicy(ctx, &contentPrincipal, p, expected)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = manager.Step(ctx, &contentPrincipal); err != nil {
				t.Fatal(err)
			}
			w.Reopen(ctx)
			manager = trustedContentManager(t, w, 2)
			service = contentService(t, w)
			observation, err := manager.ObserveChange(ctx, &contentPrincipal, change.Key, "", 64)
			if err != nil || len(observation.Responsibilities) != 2 {
				t.Fatal("actual source and descendant responsibility absent", err, observation)
			}
			for _, r := range observation.Responsibilities {
				if len(r.Actions) != 5 {
					t.Fatal("malformed policy did not invalidate all five actions", r)
				}
				if r.Ref != source && r.Ref != request.Payload.ContentRef {
					t.Fatal("responsibility bound wrong full ref", r)
				}
				if scope == "writer" {
					if r.BodyCleanup != "pending" || r.Residual != "holder_unconfirmed" || !r.ObjectHolder {
						t.Fatal("invalid writer save basis lost holder responsibility", r)
					}
				} else if r.BodyCleanup != "not_required" {
					t.Fatal("other policy authorized deletion of writer body", r)
				}
			}
			if scope == "writer" {
				view, err := service.Get(ctx, contentGetWire(t, request.Payload.ContentRef, nil), &contentPrincipal)
				denied, ok := view.AsRejected()
				if err != nil || !ok || denied.Reason != "forbidden" {
					t.Fatal("malformed ancestor binding allowed bytes", err, view)
				}
			} else {
				assertContentBody(t, ctx, service, request.Payload.ContentRef, nil, "alpha\n")
			}
		})
	}
}

func TestContentAcceptedRequestCapHasOriginalExpiryMaintenance(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	w := fixture.New(t, ctx)
	manager := trustedContentManager(t, w, 2)
	wide := time.Now().Add(time.Hour)
	p := content.FixturePolicy{Ref: alphaRef, Subject: contentPrincipal, Purpose: "verification", Revision: 1, ValidUntil: wide, RetainUntil: wide, Read: true, Process: true, Save: true, Sync: true, Disclose: true}
	change, err := manager.InstallPolicy(ctx, &contentPrincipal, p, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err = w.Store().InstallFixtureCommandReader(ctx, contentOwner, contentPrincipal, wide); err != nil {
		t.Fatal(err)
	}
	due := time.Now().UTC().Add(2 * time.Second).Truncate(time.Microsecond)
	request := contentPut(t, alphaRef, "finite-request-cap", "YWxwaGEK")
	request.Payload.RetainUntil = v.Time(due.Format("2006-01-02T15:04:05.000000Z"))
	service := contentService(t, w)
	if _, ok := putContentRequest(t, ctx, service, request).AsAccepted(); !ok {
		t.Fatal("normal finite request refused")
	}
	if _, err = service.Step(ctx); err != nil {
		t.Fatal(err)
	}
	assertContentBody(t, ctx, service, alphaRef, nil, "alpha\n")
	before, err := manager.ObserveChange(ctx, &contentPrincipal, change.Key, "", 64)
	if err != nil || !before.Change.Due.Equal(due) {
		t.Fatal("accepted original cap not scheduled", err, before)
	}
	w.Reopen(ctx)
	manager = trustedContentManager(t, w, 2)
	waitUntil(t, ctx, due.Add(20*time.Millisecond))
	if _, err = manager.Step(ctx, &contentPrincipal); err != nil {
		t.Fatal(err)
	}
	after, err := manager.ObserveChange(ctx, &contentPrincipal, change.Key, "", 64)
	if err != nil || len(after.Responsibilities) != 1 {
		t.Fatal("request cap maintenance absent", err, after)
	}
	r := after.Responsibilities[0]
	if r.BodyCleanup != "pending" || len(r.Actions) != 5 || !r.ObjectHolder || !after.Change.Deadline.Equal(before.Change.Deadline) {
		t.Fatal("expired accepted cap has no cleanup/current-use responsibility", r, after.Change)
	}
}

// This finite barrier pauses after a real PG descendant read. It controls the
// trusted capability cutoff; it is not a PostgreSQL failure or an I/O operation.
type heldPolicyDescendants struct {
	content.ManagementRepository
	entered, release chan struct{}
}

func (p *heldPolicyDescendants) Descendants(ctx context.Context, tx runtime.Tx, ref v.ContentRef, watermark int64, cursor string, limit int) ([]v.ContentRef, string, error) {
	refs, next, err := p.ManagementRepository.Descendants(ctx, tx, ref, watermark, cursor, limit)
	if err != nil {
		return nil, "", err
	}
	close(p.entered)
	select {
	case <-p.release:
		return refs, next, nil
	case <-ctx.Done():
		return nil, "", ctx.Err()
	}
}
func TestContentManagementStepRechecksTrustedDeadlineAfterPageLocks(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	w := fixture.New(t, ctx)
	service, request, source := publishedDirectContent(t, ctx, w)
	manager := trustedContentManager(t, w, 1)
	wide := time.Now().Add(time.Hour)
	policy := content.FixturePolicy{Ref: source, Subject: contentPrincipal, Purpose: "verification", Revision: 2, ValidUntil: wide, RetainUntil: wide, Read: true, Process: true, Save: false, Disclose: true}
	change, err := manager.InstallPolicy(ctx, &contentPrincipal, policy, 1)
	if err != nil {
		t.Fatal(err)
	}
	held := &heldPolicyDescendants{ManagementRepository: w.Store(), entered: make(chan struct{}), release: make(chan struct{})}
	until := time.Now().Add(450 * time.Millisecond)
	limited, err := content.NewManager(content.ManagementConfig{Owner: contentOwner, Store: held, TrustedSubject: contentPrincipal, TrustedUntil: until, PageSize: 1, WorkBudget: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	finished := make(chan struct{})
	var once sync.Once
	release := func() error { once.Do(func() { close(held.release) }); return nil }
	go func() { defer close(finished); _, e := limited.Step(ctx, &contentPrincipal); done <- e }()
	joinContentRead(t, w, release, finished)
	select {
	case <-held.entered:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	waitUntil(t, ctx, until.Add(20*time.Millisecond))
	if err = release(); err != nil {
		t.Fatal(err)
	}
	select {
	case err = <-done:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	var denied *v.ContractError
	if !errors.As(err, &denied) || denied.Code != "forbidden" {
		t.Fatal("expired management capability committed page", err)
	}
	w.Reopen(ctx)
	manager = trustedContentManager(t, w, 1)
	observation, err := manager.ObserveChange(ctx, &contentPrincipal, change.Key, "", 64)
	if err != nil || observation.Change.Cursor != "" || len(observation.Responsibilities) != 1 {
		t.Fatal("expired management page did not roll back", err, observation)
	}
	if worked, err := manager.Step(ctx, &contentPrincipal); err != nil || !worked {
		t.Fatal("normal management resume failed", err)
	}
	assertContentBody(t, ctx, contentService(t, w), request.Payload.ContentRef, nil, "alpha\n")
	_ = service
}

func TestContentExpiredMaintenanceBudgetRetainsKnownHolderWithoutClaimingComplete(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	w := fixture.New(t, ctx)
	wide := time.Now().Add(time.Hour)
	manager, err := content.NewManager(content.ManagementConfig{Owner: contentOwner, Store: w.Store(), TrustedSubject: contentPrincipal, TrustedUntil: wide, PageSize: 1, WorkBudget: 5 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	p := content.FixturePolicy{Ref: alphaRef, Subject: contentPrincipal, Purpose: "verification", Revision: 1, ValidUntil: wide, RetainUntil: wide, Read: true, Process: true, Save: true, Disclose: true}
	change, err := manager.InstallPolicy(ctx, &contentPrincipal, p, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err = w.Store().InstallFixtureCommandReader(ctx, contentOwner, contentPrincipal, wide); err != nil {
		t.Fatal(err)
	}
	due := time.Now().UTC().Add(2 * time.Second).Truncate(time.Microsecond)
	request := contentPut(t, alphaRef, "deadline-residual", "YWxwaGEK")
	request.Payload.RetainUntil = v.Time(due.Format("2006-01-02T15:04:05.000000Z"))
	service := contentService(t, w)
	if _, ok := putContentRequest(t, ctx, service, request).AsAccepted(); !ok {
		t.Fatal("normal finite admission refused")
	}
	if _, err = service.Step(ctx); err != nil {
		t.Fatal(err)
	}
	assertContentBody(t, ctx, service, alphaRef, nil, "alpha\n")
	before, err := manager.ObserveChange(ctx, &contentPrincipal, change.Key, "", 64)
	if err != nil {
		t.Fatal(err)
	}
	waitUntil(t, ctx, due.Add(30*time.Millisecond))
	if _, err = manager.Step(ctx, &contentPrincipal); err != nil {
		t.Fatal(err)
	}
	after, err := manager.ObserveChange(ctx, &contentPrincipal, change.Key, "", 64)
	if err != nil || after.Change.State != "residual" || after.Change.Reason != "original_deadline_expired" || !after.Change.Deadline.Equal(before.Change.Deadline) {
		t.Fatal("expired original budget hidden/refreshed", err, after)
	}
	if len(after.Responsibilities) != 1 || after.Responsibilities[0].BodyCleanup != "pending" || !after.Responsibilities[0].ObjectHolder {
		t.Fatal("deadline expiry lost known holder responsibility", after)
	}
}
