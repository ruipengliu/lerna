//go:build integration

package component_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	fixture "github.com/ruipengliu/lerna/conformance/internal/contentfixture"
	v "github.com/ruipengliu/lerna/contract/v1_2"
	"github.com/ruipengliu/lerna/domain/content"
	"github.com/ruipengliu/lerna/runtime"
)

// The original native CAS/attempt write occurs before this finite delay. The
// consumer's real final DB clock must still roll back the original transaction.
type legacyAttemptCutoff struct {
	content.LifecycleRepository
	ref     v.ContentRef
	until   time.Time
	bound   bool
	writes  int
	crossed bool
}

func (r *legacyAttemptCutoff) BindLegacyPrimary(ctx context.Context, tx runtime.Tx, expected content.Record, q content.LegacyPrimaryQualification) (content.Record, error) {
	result, err := r.LifecycleRepository.BindLegacyPrimary(ctx, tx, expected, q)
	if err == nil && expected.Ref == r.ref && expected.PrimaryHolderBinding == "" {
		r.bound = true
	}
	return result, err
}
func (r *legacyAttemptCutoff) SavePublicationAttempt(ctx context.Context, tx runtime.Tx, record content.Record) error {
	if err := r.LifecycleRepository.SavePublicationAttempt(ctx, tx, record); err != nil {
		return err
	}
	if record.Ref != r.ref {
		return nil
	}
	r.writes++
	if r.writes != 1 {
		return nil
	}
	timer := time.NewTimer(time.Until(r.until.Add(20 * time.Millisecond)))
	defer timer.Stop()
	select {
	case <-timer.C:
		r.crossed = true
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func TestContentLegacyOriginalQualificationCutoffRollsBackBindingAndAttemptWrites(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	w, original, q := fixture.NewStoppedLegacy(t, ctx, nil)
	first := original.Requests[0].Payload.ContentRef
	second := original.Requests[1].Payload.ContentRef
	// This is the first host consumer authorization. Persist its initial bound
	// before effects; never issue a longer one after the counterexample.
	q = w.FixInitialLegacyCutoff(q, time.Now().UTC().Add(700*time.Millisecond).Truncate(time.Microsecond))
	consumer := func(store content.LifecycleRepository) *content.Lifecycle {
		t.Helper()
		l, err := content.NewLifecycle(content.LifecycleConfig{ManagementConfig: content.ManagementConfig{Owner: contentOwner, Store: store, TrustedSubject: contentPrincipal, TrustedUntil: time.Now().Add(time.Hour), PageSize: 2, WorkBudget: time.Minute}, Objects: w.Objects, PrimaryHolderID: "primary", Worker: "legacy-original-cutoff", LegacyPrimary: &q})
		if err != nil {
			t.Fatal(err)
		}
		return l
	}
	// The same initial short window has a genuine normal binding control.
	normal, err := consumer(w.Store()).BindLegacyPrimary(ctx, &contentPrincipal, content.LegacyPrimaryRequest{Ref: second, Purpose: "verification"})
	if err != nil || normal.Binding != q.Binding || normal.QualificationID != q.ID || normal.EvidenceDigest != q.EvidenceDigest {
		t.Fatal("same original 700ms normal binding failed", normal, err)
	}
	assertContentBody(t, ctx, contentService(t, w), second, nil, "beta\n")
	delayed := &legacyAttemptCutoff{LifecycleRepository: w.Store(), ref: first, until: q.ValidUntil}
	_, err = consumer(delayed).BindLegacyPrimary(ctx, &contentPrincipal, content.LegacyPrimaryRequest{Ref: first, Purpose: "verification"})
	var refusal *v.ContractError
	if !errors.As(err, &refusal) || refusal.Code != "expired" || !delayed.bound || delayed.writes != 1 || !delayed.crossed {
		t.Fatal("original cutoff did not roll back real CAS and attempt write", err, delayed.bound, delayed.writes, delayed.crossed)
	}
	w.Reopen(ctx)
	service := contentService(t, w)
	view, err := service.Get(ctx, contentGetWire(t, first, nil), &contentPrincipal)
	unavailable, ok := view.AsUnavailable()
	if err != nil || !ok || unavailable.ContentRef != first {
		t.Fatal("expired transaction persisted original binding", view, err)
	}
	seal := content.SealRequest{Ref: first, Purpose: "verification", SealID: "legacy-cutoff-unbound", Deadline: time.Now().UTC().Add(time.Minute).Truncate(time.Microsecond)}
	if _, err = bodyLifecycle(t, w).Seal(ctx, &contentPrincipal, seal); !errors.Is(err, content.ErrHolderBinding) {
		t.Fatal("expired binding acquired holder authority", err)
	}
	if _, err = consumer(w.Store()).BindLegacyPrimary(ctx, &contentPrincipal, content.LegacyPrimaryRequest{Ref: first, Purpose: "verification"}); !errors.As(err, &refusal) || refusal.Code != "expired" {
		t.Fatal("same original expired authorization was revived", err)
	}
	assertContentBody(t, ctx, service, second, nil, "beta\n")
	for i, request := range original.Requests {
		_, key, err := content.VersionIdentity(request.Payload.ContentRef)
		if err != nil {
			t.Fatal(err)
		}
		body, err := os.ReadFile(filepath.Join(w.Directory, key))
		want := "alpha\n"
		if i == 1 {
			want = "beta\n"
		}
		if err != nil || string(body) != want {
			t.Fatal("cutoff altered original stopped-scope body", err)
		}
		before, err := v.Encode(original.Receipts[i])
		if err != nil {
			t.Fatal(err)
		}
		after, err := v.Encode(putContentRequest(t, ctx, service, request))
		if err != nil || string(before) != string(after) {
			t.Fatal("cutoff changed original receipt", err)
		}
		command, err := service.GetCommand(ctx, contentCommandGetWire(t, request.CommandID), &contentPrincipal)
		found, ok := command.AsFound()
		history, published := found.Progress.AsContent()
		if err != nil || !ok || !published || history.Publication != "published" {
			t.Fatal("cutoff changed original publication history", command, err)
		}
	}
}
