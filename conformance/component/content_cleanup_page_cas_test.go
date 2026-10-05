//go:build integration

package component_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	fixture "github.com/ruipengliu/lerna/conformance/internal/contentfixture"
	"github.com/ruipengliu/lerna/contract"
	v "github.com/ruipengliu/lerna/contract/v1_2"
	"github.com/ruipengliu/lerna/domain/content"
	"github.com/ruipengliu/lerna/runtime"
)

// The candidate transaction really commits/releases before this finite return
// gate. Qualified writes remain original PostgreSQL effects, including rollback.
type heldOriginalCleanupCandidate struct {
	content.LifecycleRepository
	key              string
	entered, release chan struct{}
	selected, held   bool
	qualified        []v.ContentRef
}

func (r *heldOriginalCleanupCandidate) ReadChange(ctx context.Context, tx runtime.Tx, key string) (*content.PolicyChange, error) {
	change, err := r.LifecycleRepository.ReadChange(ctx, tx, key)
	if err == nil && change != nil && key == r.key && !r.held {
		r.selected = true
	}
	return change, err
}
func (r *heldOriginalCleanupCandidate) Within(ctx context.Context, owner contract.OwnerRef, fn func(context.Context, runtime.Tx) error) error {
	r.selected = false
	err := r.LifecycleRepository.Within(ctx, owner, fn)
	if err != nil || !r.selected || r.held {
		return err
	}
	r.held = true
	close(r.entered)
	finite, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	select {
	case <-r.release:
		return nil
	case <-finite.Done():
		return finite.Err()
	}
}
func (r *heldOriginalCleanupCandidate) QualifyPolicyCleanupNotRequired(ctx context.Context, tx runtime.Tx, expected content.CleanupResponsibility) error {
	err := r.LifecycleRepository.QualifyPolicyCleanupNotRequired(ctx, tx, expected)
	if err == nil {
		r.qualified = append(r.qualified, expected.Ref)
	}
	return err
}

func TestContentOriginalCleanupPageCASRollsBackEarlierQualificationsAfterConcurrentConsumer(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	w := fixture.New(t, ctx)
	service := contentService(t, w)
	manager := trustedContentManager(t, w, 2)
	wide := time.Now().Add(time.Hour)
	root := alphaRef
	root.ContentID = "0-page-cas-root"
	refs := []v.ContentRef{root, root, root}
	refs[1].ContentID = "1-page-cas-derived"
	refs[2].ContentID = "2-page-cas-derived"
	requests := make([]v.ContentPutRequest, 3)
	receipts := make([][]byte, 3)
	for i, ref := range refs {
		policy := content.FixturePolicy{Ref: ref, Subject: contentPrincipal, Purpose: "verification", Revision: 1, ValidUntil: wide, RetainUntil: wide, Read: true, Process: true, Save: true, Sync: true, Disclose: true}
		if _, err := manager.InstallPolicy(ctx, &contentPrincipal, policy, 0); err != nil {
			t.Fatal(err)
		}
		if err := w.Store().InstallFixtureCommandReader(ctx, contentOwner, contentPrincipal, wide); err != nil {
			t.Fatal(err)
		}
		request := contentPut(t, ref, string(ref.ContentID)+"-command", "YWxwaGEK")
		if i > 0 {
			request.Payload.Sources = []v.ContentRef{root}
		}
		receipt := putContentRequest(t, ctx, service, request)
		if _, ok := receipt.AsAccepted(); !ok {
			t.Fatal("normal original page scope refused", ref)
		}
		if _, err := service.Step(ctx); err != nil {
			t.Fatal(err)
		}
		assertContentBody(t, ctx, service, ref, nil, "alpha\n")
		raw, err := v.Encode(receipt)
		if err != nil {
			t.Fatal(err)
		}
		requests[i], receipts[i] = request, raw
	}
	version2 := alphaRef
	version2.Version = "2"
	version2.Hash = "sha256:f2c82decdd7181cf98945929a62598db7e6b477e11f6e0eb0ae97020eff151ad"
	version2.ByteLength = "5"
	installContentPolicy(t, ctx, w, version2)
	if _, ok := putContentRequest(t, ctx, service, contentPut(t, version2, "page-cas-independent", "YmV0YQo=")).AsAccepted(); !ok {
		t.Fatal("normal independent Version2 refused")
	}
	if _, err := service.Step(ctx); err != nil {
		t.Fatal(err)
	}
	assertContentBody(t, ctx, service, version2, nil, "beta\n")
	withdrawal := content.FixturePolicy{Ref: root, Subject: contentPrincipal, Purpose: "verification", Revision: 2, ValidUntil: wide, RetainUntil: wide, Read: true, Process: true, Save: false, Sync: true, Disclose: true}
	change, err := manager.InstallPolicy(ctx, &contentPrincipal, withdrawal, 1)
	if err != nil {
		t.Fatal(err)
	}
	completed := false
	for i := 0; i < 8; i++ {
		if _, err = manager.Step(ctx, &contentPrincipal); err != nil {
			t.Fatal(err)
		}
		actual, err := manager.ObserveChange(ctx, &contentPrincipal, change.Key, "", 2)
		if err != nil {
			t.Fatal(err)
		}
		if actual.Change.State == "scheduled" && actual.Change.Phase == "natural_expiry" {
			completed = true
			break
		}
	}
	if !completed {
		t.Fatal("original finite propagation did not finish")
	}
	original := map[v.ContentRef]content.CleanupResponsibility{}
	cursor := ""
	for i := 0; i < 3; i++ {
		page, err := manager.ObserveChange(ctx, &contentPrincipal, change.Key, cursor, 2)
		if err != nil {
			t.Fatal(err)
		}
		for _, duty := range page.Responsibilities {
			known := false
			for _, ref := range refs {
				known = known || duty.Ref == ref
			}
			if !known || duty.BodyCleanup != "pending" || duty.Residual != "holder_unconfirmed" || duty.Reason != "" || !duty.ObjectHolder || duty.Publication != "published" || !reflect.DeepEqual(duty.Subject, contentPrincipal) || duty.Purpose != "verification" {
				t.Fatal("actual original page responsibility malformed", duty)
			}
			if _, duplicate := original[duty.Ref]; duplicate {
				t.Fatal("original finite page repeated responsibility", duty.Ref)
			}
			original[duty.Ref] = duty
		}
		cursor = page.NextCursor
		if cursor == "" {
			break
		}
	}
	if cursor != "" || len(original) != len(refs) {
		t.Fatal("original responsibility pages incomplete", original, cursor)
	}
	withdrawal.Revision = 3
	withdrawal.Save = true
	if _, err = manager.InstallPolicy(ctx, &contentPrincipal, withdrawal, 2); err != nil {
		t.Fatal(err)
	}
	for _, request := range requests {
		assertContentBody(t, ctx, service, request.Payload.ContentRef, nil, "alpha\n")
		if err = service.AuthorizeUse(ctx, &contentPrincipal, request.Payload.ContentRef, "verification", "save"); err != nil {
			t.Fatal("restored current full basis unavailable", err)
		}
	}
	lifecycle := func(store content.LifecycleRepository, size int) *content.Lifecycle {
		t.Helper()
		l, err := content.NewLifecycle(content.LifecycleConfig{ManagementConfig: content.ManagementConfig{Owner: contentOwner, Store: store, TrustedSubject: contentPrincipal, TrustedUntil: wide, PageSize: size, WorkBudget: time.Minute}, Objects: w.Objects, PrimaryHolderID: "primary", Worker: "original-page-cas"})
		if err != nil {
			t.Fatal(err)
		}
		return l
	}
	// Public pagination fixes the real order. Hash object IDs are not assumed
	// to preserve the displayed ContentID names or put the root first.
	selected, err := manager.ObserveChange(ctx, &contentPrincipal, change.Key, "", 2)
	if err != nil || len(selected.Responsibilities) != 2 {
		t.Fatal(selected, err)
	}
	first, second := selected.Responsibilities[0], selected.Responsibilities[1]
	prefix, err := manager.ObserveChange(ctx, &contentPrincipal, change.Key, "", 1)
	if err != nil || len(prefix.Responsibilities) != 1 || prefix.Responsibilities[0].Ref != first.Ref || prefix.NextCursor == "" || !reflect.DeepEqual(first, original[first.Ref]) || !reflect.DeepEqual(second, original[second.Ref]) {
		t.Fatal("public original page order or cursor changed", prefix, err)
	}
	held := &heldOriginalCleanupCandidate{LifecycleRepository: w.Store(), key: change.Key, entered: make(chan struct{}), release: make(chan struct{})}
	var once sync.Once
	release := func() error { once.Do(func() { close(held.release) }); return nil }
	result := make(chan error, 1)
	finished := make(chan struct{})
	stale := lifecycle(held, 2)
	go func() {
		defer close(finished)
		_, err := stale.ConsumePolicyCleanup(ctx, &contentPrincipal, change.Key, "")
		result <- err
	}()
	joinContentRead(t, w, release, finished)
	select {
	case <-held.entered:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	contender, err := lifecycle(w.Store(), 1).ConsumePolicyCleanup(ctx, &contentPrincipal, change.Key, prefix.NextCursor)
	if err != nil || len(contender.Responsibilities) != 1 || contender.Responsibilities[0].Ref != second.Ref || contender.Responsibilities[0].BodyCleanup != "not_required" || !contender.Responsibilities[0].Deadline.Equal(second.Deadline) {
		t.Fatal("real same original second-duty contender did not commit", contender, err)
	}
	changed := contender.Responsibilities[0]
	unchanged, err := manager.ObserveChange(ctx, &contentPrincipal, change.Key, "", 1)
	if err != nil || len(unchanged.Responsibilities) != 1 || !reflect.DeepEqual(unchanged.Responsibilities[0], first) {
		t.Fatal("contender changed earlier duty", unchanged, err)
	}
	if err = release(); err != nil {
		t.Fatal(err)
	}
	select {
	case err = <-result:
		var conflict *content.ManagementConflict
		if !errors.As(err, &conflict) {
			t.Fatal("stale original second-duty CAS did not conflict", err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	select {
	case <-finished:
	case <-ctx.Done():
		t.Fatal("actual stale consumer did not finish", ctx.Err())
	}
	if len(held.qualified) != 1 || held.qualified[0] != first.Ref {
		t.Fatal("earlier real write did not precede conflicting second CAS", held.qualified)
	}
	rolledBack, err := manager.ObserveChange(ctx, &contentPrincipal, change.Key, "", 2)
	if err != nil || len(rolledBack.Responsibilities) != 2 || !reflect.DeepEqual(rolledBack.Responsibilities[0], first) || !reflect.DeepEqual(rolledBack.Responsibilities[1], changed) {
		t.Fatal("stale whole page did not roll back earlier qualified write", rolledBack, err)
	}
	for _, ref := range refs {
		if _, err = lifecycle(w.Store(), 2).Observe(ctx, &contentPrincipal, ref, ""); !errors.Is(err, content.ErrUnavailable) {
			t.Fatal("nonphysical original qualification created a seal", ref, err)
		}
		_, key, err := content.VersionIdentity(ref)
		if err != nil {
			t.Fatal(err)
		}
		body, err := os.ReadFile(filepath.Join(w.Directory, key))
		if err != nil || string(body) != "alpha\n" {
			t.Fatal("CAS competition changed original bytes", ref, err)
		}
	}
	if worked, err := lifecycle(w.Store(), 2).Step(ctx, &contentPrincipal); err != nil || worked {
		t.Fatal("nonphysical CAS competition created active cleanup", worked, err)
	}
	cursor = ""
	for i := 0; i < 3; i++ {
		page, err := lifecycle(w.Store(), 2).ConsumePolicyCleanup(ctx, &contentPrincipal, change.Key, cursor)
		if err != nil {
			t.Fatal("fresh same original candidate cannot qualify", err)
		}
		cursor = page.NextCursor
		if cursor == "" {
			break
		}
	}
	if cursor != "" {
		t.Fatal("fresh bounded original page recovery incomplete")
	}
	w.Reopen(ctx)
	manager = trustedContentManager(t, w, 2)
	service = contentService(t, w)
	cursor = ""
	count := 0
	for i := 0; i < 3; i++ {
		page, err := manager.ObserveChange(ctx, &contentPrincipal, change.Key, cursor, 2)
		if err != nil {
			t.Fatal(err)
		}
		for _, duty := range page.Responsibilities {
			expected, ok := original[duty.Ref]
			if !ok || duty.BodyCleanup != "not_required" || duty.Residual != "current_save_basis_restored" {
				t.Fatal("reopen lost actual current qualification", duty)
			}
			expected.BodyCleanup = duty.BodyCleanup
			expected.Residual = duty.Residual
			if !reflect.DeepEqual(duty, expected) {
				t.Fatal("CAS recovery altered original duty history or deadline", duty)
			}
			count++
		}
		cursor = page.NextCursor
		if cursor == "" {
			break
		}
	}
	if cursor != "" || count != len(refs) {
		t.Fatal("reopen responsibility pages lost original membership", count, cursor)
	}
	for i, request := range requests {
		assertContentBody(t, ctx, service, request.Payload.ContentRef, nil, "alpha\n")
		replay, err := v.Encode(putContentRequest(t, ctx, service, request))
		if err != nil || string(replay) != string(receipts[i]) {
			t.Fatal("CAS competition changed original receipt", err)
		}
	}
	assertContentBody(t, ctx, service, version2, nil, "beta\n")
}
