//go:build integration

package component_test

import (
	"context"
	"fmt"
	fixture "github.com/ruipengliu/lerna/conformance/internal/contentfixture"
	v "github.com/ruipengliu/lerna/contract/v1_2"
	"github.com/ruipengliu/lerna/domain/content"
	"github.com/ruipengliu/lerna/runtime"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestContentHiddenAncestorControlsCurrentDerivedUse(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	w := fixture.New(t, ctx)
	service := contentService(t, w)
	refs := []v.ContentRef{alphaRef, alphaRef, alphaRef}
	refs[1].ContentID = "middle"
	refs[2].ContentID = "output"
	for i, ref := range refs {
		installContentPolicy(t, ctx, w, ref)
		req := contentPut(t, ref, string(ref.ContentID), "YWxwaGEK")
		if i > 0 {
			req.Payload.Sources = []v.ContentRef{refs[i-1]}
		}
		if _, ok := putContentRequest(t, ctx, service, req).AsAccepted(); !ok {
			t.Fatal("normal derivation refused")
		}
		if _, err := service.Step(ctx); err != nil {
			t.Fatal(err)
		}
		assertContentBody(t, ctx, service, ref, nil, "alpha\n")
	}
	wide := time.Now().Add(time.Hour)
	p := content.FixturePolicy{Ref: alphaRef, Subject: contentPrincipal, Purpose: "verification", Revision: 2, ValidUntil: wide, RetainUntil: wide, Read: true, Process: true, Save: true, Disclose: false}
	if err := w.Store().InstallFixturePolicy(ctx, p, 1); err != nil {
		t.Fatal(err)
	}
	w.Reopen(ctx)
	service = contentService(t, w)
	view, err := service.Get(ctx, contentGetWire(t, refs[2], nil), &contentPrincipal)
	if err != nil {
		t.Fatal(err)
	}
	denied, ok := view.AsRejected()
	if !ok || denied.Reason != "forbidden" {
		t.Fatal("hidden ancestor disclosure revocation bypassed", view)
	}
}

// Port timings are mechanical diagnostics, never behavior assertions. Each
// action set calls the real repository; logical flags are counted separately.
type closurePortTiming struct {
	calls   int
	actions int
	elapsed time.Duration
}
type closureTimingRepository struct {
	content.Repository
	stage   string
	timings map[string]closurePortTiming
}

func (r *closureTimingRepository) measure(operation string, started time.Time) {
	key := r.stage + "/" + operation
	stat := r.timings[key]
	stat.calls++
	stat.elapsed += time.Since(started)
	r.timings[key] = stat
}
func (r *closureTimingRepository) Now(ctx context.Context, tx runtime.Tx) (time.Time, error) {
	started := time.Now()
	defer r.measure("Now", started)
	return r.Repository.Now(ctx, tx)
}
func (r *closureTimingRepository) CheckPolicy(ctx context.Context, tx runtime.Tx, subject v.SubjectBinding, ref v.ContentRef, purpose string, actions []string, now time.Time) (*content.FixturePolicy, error) {
	started := time.Now()
	defer func() {
		r.measure("CheckPolicy", started)
		key := r.stage + "/CheckPolicy"
		stat := r.timings[key]
		stat.actions += len(actions)
		r.timings[key] = stat
	}()
	return r.Repository.CheckPolicy(ctx, tx, subject, ref, purpose, actions, now)
}
func (r *closureTimingRepository) LockVersion(ctx context.Context, tx runtime.Tx, ref v.ContentRef) (*content.Record, error) {
	started := time.Now()
	defer r.measure("LockVersion", started)
	return r.Repository.LockVersion(ctx, tx, ref)
}
func (r *closureTimingRepository) ScheduleRetention(ctx context.Context, tx runtime.Tx, policy *content.FixturePolicy, record content.Record, sources []content.Record, budget time.Duration) error {
	started := time.Now()
	defer r.measure("ScheduleRetention", started)
	return r.Repository.ScheduleRetention(ctx, tx, policy, record, sources, budget)
}

func TestContentFullClosureIncludesIntermediateVersionsAndExact64Bound(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	w := fixture.New(t, ctx)
	metrics := &closureTimingRepository{Repository: w.Store(), timings: map[string]closurePortTiming{}}
	service, err := content.New(content.Config{Owner: contentOwner, Store: metrics, Objects: w.Objects, Limits: content.Limits{MaxPreparingVersions: 16, MaxStagingBytes: 4 * 262144, Lease: time.Minute, WorkTimeout: 5 * time.Second}, PublishBudget: time.Minute, MaxPublicationAttempts: 3, Worker: "content-conformance"})
	if err != nil {
		t.Fatal(err)
	}
	var previous v.ContentRef
	// These timings diagnose the finite native case; public receipts and bytes
	// remain the oracle. The original context and full 64/65 inputs are fixed.
	var installTime, putTime, stepTime time.Duration
	var index int
	stage := "setup"
	started := time.Now()
	t.Cleanup(func() {
		t.Logf("closure timing i=%d stage=%s current=%s install=%s put=%s step=%s", index, stage, time.Since(started), installTime, putTime, stepTime)
		for _, phase := range []string{"put", "step", "get"} {
			for _, operation := range []string{"Now", "CheckPolicy", "LockVersion", "ScheduleRetention"} {
				stat := metrics.timings[phase+"/"+operation]
				t.Logf("closure port %s/%s calls=%d logical_actions=%d elapsed=%s", phase, operation, stat.calls, stat.actions, stat.elapsed)
			}
		}
	})
	for i := 0; i <= 65; i++ {
		index, stage, started = i, "install", time.Now()
		ref := alphaRef
		ref.ContentID = v.ID(fmt.Sprintf("closure-%02d", i))
		installContentPolicy(t, ctx, w, ref)
		installTime += time.Since(started)
		req := contentPut(t, ref, string(ref.ContentID), "YWxwaGEK")
		if i > 0 {
			req.Payload.Sources = []v.ContentRef{previous}
		}
		stage, started = "put", time.Now()
		metrics.stage = stage
		receipt := putContentRequest(t, ctx, service, req)
		putTime += time.Since(started)
		if i == 65 {
			requireRejection(t, receipt, "input_over_limit")
			break
		}
		if _, ok := receipt.AsAccepted(); !ok {
			t.Fatal("closure within limit refused", i, receipt)
		}
		stage, started = "step", time.Now()
		metrics.stage = stage
		if _, err := service.Step(ctx); err != nil {
			t.Fatal(i, err)
		}
		stepTime += time.Since(started)
		if i == 64 {
			metrics.stage = "get"
			assertContentBody(t, ctx, service, ref, nil, "alpha\n")
		}
		previous = ref
	}
}

func TestContentClosureDeduplicatesSharedAncestorAndRejectsForeignOwners(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	w := fixture.New(t, ctx)
	service := contentService(t, w)
	refs := []v.ContentRef{alphaRef, alphaRef, alphaRef, alphaRef}
	refs[1].ContentID = "left"
	refs[2].ContentID = "right"
	refs[3].ContentID = "shared-output"
	for i, ref := range refs {
		installContentPolicy(t, ctx, w, ref)
		req := contentPut(t, ref, string(ref.ContentID), "YWxwaGEK")
		if i == 1 || i == 2 {
			req.Payload.Sources = []v.ContentRef{alphaRef}
		}
		if i == 3 {
			req.Payload.Sources = []v.ContentRef{refs[1], refs[2]}
		}
		if _, ok := putContentRequest(t, ctx, service, req).AsAccepted(); !ok {
			t.Fatal("shared ancestor normal refused")
		}
		if _, err := service.Step(ctx); err != nil {
			t.Fatal(err)
		}
	}
	assertContentBody(t, ctx, service, refs[3], nil, "alpha\n")
	for i, code := range []v.ErrorCode{"unsupported", "forbidden", "integrity"} {
		ref := alphaRef
		ref.ContentID = v.ID(fmt.Sprintf("foreign-%d", i))
		installContentPolicy(t, ctx, w, ref)
		source := alphaRef
		if i == 0 {
			source.Owner.OwnerID = "other-owner"
		}
		if i == 1 {
			source.Owner.TenantID = "other-tenant"
		}
		if i == 2 {
			source = ref
		}
		req := contentPut(t, ref, string(ref.ContentID), "YWxwaGEK")
		req.Payload.Sources = []v.ContentRef{source}
		requireRejection(t, putContentRequest(t, ctx, service, req), code)
	}
}

func TestContentHiddenAncestorPurposeIntersectionAndOriginalCapSurviveFurtherDerivation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	w := fixture.New(t, ctx)
	service := contentService(t, w)
	manager := trustedContentManager(t, w, 2)
	refs := []v.ContentRef{alphaRef, alphaRef, alphaRef, alphaRef}
	refs[1].ContentID = "purpose-b"
	refs[2].ContentID = "purpose-middle"
	refs[3].ContentID = "purpose-output"
	for i, ref := range refs[:3] {
		installContentPolicy(t, ctx, w, ref)
		req := contentPut(t, ref, string(ref.ContentID), "YWxwaGEK")
		if i == 2 {
			req.Payload.Sources = []v.ContentRef{refs[0], refs[1]}
		}
		if _, ok := putContentRequest(t, ctx, service, req).AsAccepted(); !ok {
			t.Fatal("normal registered inputs refused")
		}
		if _, err := service.Step(ctx); err != nil {
			t.Fatal(err)
		}
		assertContentBody(t, ctx, service, ref, nil, "alpha\n")
	}
	cap := time.Now().UTC().Add(5 * time.Minute).Truncate(time.Microsecond)
	wide := time.Now().Add(time.Hour)
	p := content.FixturePolicy{Ref: refs[0], Subject: contentPrincipal, Purpose: "verification", Revision: 2, ValidUntil: wide, RetainUntil: cap, Read: true, Process: true, Save: true, Disclose: true}
	if _, err := manager.InstallPolicy(ctx, &contentPrincipal, p, 1); err != nil {
		t.Fatal(err)
	}
	// The middle and B permit the new purpose; hidden A initially does not.
	for _, ref := range refs[1:] {
		p.Ref = ref
		p.Purpose = "shared"
		p.Revision = 1
		p.RetainUntil = wide
		if _, err := manager.InstallPolicy(ctx, &contentPrincipal, p, 0); err != nil {
			t.Fatal(err)
		}
	}
	request := contentPut(t, refs[3], "purpose-denied", "YWxwaGEK")
	request.Payload.Purpose = "shared"
	request.Payload.Sources = []v.ContentRef{refs[2]}
	requireRejection(t, putContentRequest(t, ctx, service, request), "forbidden")
	p.Ref = refs[0]
	if _, err := manager.InstallPolicy(ctx, &contentPrincipal, p, 0); err != nil {
		t.Fatal(err)
	}
	request.CommandID = "purpose-normal"
	accepted, ok := putContentRequest(t, ctx, service, request).AsAccepted()
	want := v.Time(cap.Format("2006-01-02T15:04:05.000000Z"))
	if !ok || accepted.RetainUntil != want {
		t.Fatal("hidden ancestor original cap not inherited", accepted)
	}
	if _, err := service.Step(ctx); err != nil {
		t.Fatal(err)
	}
	w.Reopen(ctx)
	service = contentService(t, w)
	next := alphaRef
	next.ContentID = "purpose-further"
	p.Ref = next
	if _, err := trustedContentManager(t, w, 2).InstallPolicy(ctx, &contentPrincipal, p, 0); err != nil {
		t.Fatal(err)
	}
	request = contentPut(t, next, "further-normal", "YWxwaGEK")
	request.Payload.Purpose = "shared"
	request.Payload.Sources = []v.ContentRef{refs[3]}
	accepted, ok = putContentRequest(t, ctx, service, request).AsAccepted()
	if !ok || accepted.RetainUntil != want {
		t.Fatal("reopen/further derivation refreshed cap", accepted)
	}
	if _, err := service.Step(ctx); err != nil {
		t.Fatal(err)
	}
	read, err := v.DecodeGet(contentGetWire(t, next, nil))
	if err != nil {
		t.Fatal(err)
	}
	read.Payload.Purpose = "shared"
	raw, err := v.Encode(read)
	if err != nil {
		t.Fatal(err)
	}
	view, err := service.Get(ctx, raw, &contentPrincipal)
	published, ok := view.AsPublished()
	if err != nil || !ok || published.BytesBase64 != "YWxwaGEK" {
		t.Fatal("normal shared-purpose chain failed", err, view)
	}
}

type heldContentWrite struct {
	content.Objects
	entered, release chan struct{}
}

func (o *heldContentWrite) Put(ctx context.Context, key, attempt, hash string, length int64, body []byte) error {
	if err := o.Objects.Put(ctx, key, attempt, hash, length, body); err != nil {
		return err
	}
	close(o.entered)
	select {
	case <-o.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func TestContentPublicationFinalTransactionChecksHiddenAncestorAfterRealObjectWrite(t *testing.T) {
	for _, mode := range []string{"permitted", "ancestor-process-revoked"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			w := fixture.New(t, ctx)
			service := contentService(t, w)
			refs := []v.ContentRef{alphaRef, alphaRef, alphaRef, alphaRef}
			refs[1].ContentID = "final-b"
			refs[2].ContentID = "final-middle"
			refs[3].ContentID = "final-output"
			for i, ref := range refs {
				installContentPolicy(t, ctx, w, ref)
				req := contentPut(t, ref, string(ref.ContentID), "YWxwaGEK")
				if i == 2 {
					req.Payload.Sources = []v.ContentRef{refs[0], refs[1]}
				}
				if i == 3 {
					req.Payload.Sources = []v.ContentRef{refs[2]}
				}
				if _, ok := putContentRequest(t, ctx, service, req).AsAccepted(); !ok {
					t.Fatal("normal admission refused")
				}
				if i < 3 {
					if _, err := service.Step(ctx); err != nil {
						t.Fatal(err)
					}
				}
			}
			objects := &heldContentWrite{Objects: w.Objects, entered: make(chan struct{}), release: make(chan struct{})}
			service = withPublication(t, w, w.Store(), objects, time.Minute, 5*time.Second)
			var once sync.Once
			release := func() error { once.Do(func() { close(objects.release) }); return nil }
			finished := make(chan struct{})
			done := make(chan error, 1)
			go func() { defer close(finished); _, err := service.Step(ctx); done <- err }()
			joinContentRead(t, w, release, finished)
			select {
			case <-objects.entered:
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			if mode != "permitted" {
				wide := time.Now().Add(time.Hour)
				p := content.FixturePolicy{Ref: refs[0], Subject: contentPrincipal, Purpose: "verification", Revision: 2, ValidUntil: wide, RetainUntil: wide, Read: true, Process: false, Save: true, Disclose: true}
				if _, err := trustedContentManager(t, w, 2).InstallPolicy(ctx, &contentPrincipal, p, 1); err != nil {
					t.Fatal(err)
				}
			}
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
			service = contentService(t, w)
			if mode == "permitted" {
				assertContentBody(t, ctx, service, refs[3], nil, "alpha\n")
			} else {
				view, err := service.Get(ctx, contentGetWire(t, refs[3], nil), &contentPrincipal)
				failed, ok := view.AsFailed()
				if err != nil || !ok || failed.Reason != "forbidden" {
					t.Fatal("hidden ancestor revocation bypassed final publication", err, view)
				}
				assertContentBody(t, ctx, service, refs[2], nil, "alpha\n")
			}
			entries, err := os.ReadDir(w.Directory)
			if err != nil || len(entries) != 4 {
				t.Fatal("real in-flight bytes falsely erased", err)
			}
			for _, entry := range entries {
				body, err := os.ReadFile(filepath.Join(w.Directory, entry.Name()))
				if err != nil || string(body) != "alpha\n" {
					t.Fatal("independent real object changed", err)
				}
			}
		})
	}
}
