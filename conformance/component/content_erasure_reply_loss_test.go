//go:build integration

package component_test

import (
	"context"
	"errors"
	fixture "github.com/ruipengliu/lerna/conformance/internal/contentfixture"
	"github.com/ruipengliu/lerna/domain/content"
	"os"
	"path/filepath"
	"testing"
	"time"
)

var errErasureReplyLost = errors.New("mechanical holder reply loss after actual erasure")

// This drops one successful return after real native erasure. It neither
// fabricates a filesystem failure nor simulates PostgreSQL commit_unknown.
type erasureReplyLoss struct {
	content.ErasingObjects
	Lost   bool
	Actual content.ErasureObservation
}

func (p *erasureReplyLoss) FenceAndErase(ctx context.Context, id content.ErasureIdentity, attempts []string) (content.ErasureObservation, error) {
	actual, err := p.ErasingObjects.FenceAndErase(ctx, id, attempts)
	if err != nil {
		return actual, err
	}
	if !p.Lost {
		p.Lost = true
		p.Actual = actual
		return content.ErasureObservation{}, errErasureReplyLost
	}
	return actual, nil
}

func TestContentLostActualErasureReplyRetainsOriginalDutyAndRecoversAfterConfirmedDefer(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	w := fixture.New(t, ctx)
	installContentPolicy(t, ctx, w, alphaRef)
	service := contentService(t, w)
	if _, ok := putContentRequest(t, ctx, service, contentPut(t, alphaRef, "erasure-reply-original", "YWxwaGEK")).AsAccepted(); !ok {
		t.Fatal("normal original admission refused")
	}
	if _, err := service.Step(ctx); err != nil {
		t.Fatal(err)
	}
	assertContentBody(t, ctx, service, alphaRef, nil, "alpha\n")
	m := trustedContentManager(t, w, 2)
	wide := time.Now().Add(time.Hour)
	policy := content.FixturePolicy{Ref: alphaRef, Subject: contentPrincipal, Purpose: "verification", Revision: 2, ValidUntil: wide, RetainUntil: wide, Read: true, Process: true, Save: false, Sync: true, Disclose: true}
	change, err := m.InstallPolicy(ctx, &contentPrincipal, policy, 1)
	if err != nil {
		t.Fatal(err)
	}
	l := bodyLifecycle(t, w)
	if _, err = l.ConsumePolicyCleanup(ctx, &contentPrincipal, change.Key, ""); err != nil {
		t.Fatal(err)
	}
	original, err := l.Observe(ctx, &contentPrincipal, alphaRef, "")
	if err != nil || original.CleanupComplete {
		t.Fatal("original duty missing before reply loss", original, err)
	}
	var identity content.ErasureIdentity
	for _, holder := range original.Holders {
		if holder.Kind == "primary" {
			identity = holder.Identity
		}
	}
	if identity.ObjectKey == "" {
		t.Fatal("original primary responsibility missing")
	}
	fault := &erasureReplyLoss{ErasingObjects: w.Objects}
	withLoss, err := content.NewLifecycle(content.LifecycleConfig{ManagementConfig: content.ManagementConfig{Owner: contentOwner, Store: w.Store(), TrustedSubject: contentPrincipal, TrustedUntil: wide, PageSize: 2, WorkBudget: time.Minute}, PrimaryHolderID: "primary", Objects: fault, Worker: "content-body-cleanup"})
	if err != nil {
		t.Fatal(err)
	}
	lost := false
	for i := 0; i < 3; i++ {
		worked, e := withLoss.Step(ctx, &contentPrincipal)
		if errors.Is(e, errErasureReplyLost) {
			lost = true
			break
		}
		if e != nil || !worked {
			t.Fatal("normal staging progress before reply loss failed", worked, e)
		}
	}
	if !lost || !fault.Lost || fault.Actual.Identity != identity || !fault.Actual.Fenced || !fault.Actual.Erased {
		t.Fatal("fault did not follow an actual successful native erase", fault.Actual)
	}
	failedAt := time.Now()
	if _, err = os.ReadFile(filepath.Join(w.Directory, identity.ObjectKey)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("actual erase effect did not precede lost reply", err)
	}
	physical, err := w.Objects.ObserveErasure(ctx, identity, "", 2)
	if err != nil || !physical.Fenced || !physical.Erased || physical.NextCursor != "" {
		t.Fatal("independent exact holder observation did not see physical erase", physical, err)
	}
	residual, err := l.Observe(ctx, &contentPrincipal, alphaRef, "")
	if err != nil || residual.CleanupComplete || residual.Seal.ID != original.Seal.ID || !residual.Seal.Deadline.Equal(original.Seal.Deadline) {
		t.Fatal("lost reply became global ACK or refreshed original duty", residual, err)
	}
	foundResidual := false
	for _, holder := range residual.Holders {
		if holder.Kind == "primary" {
			foundResidual = holder.Identity == identity && holder.State == "residual" && holder.Responsible == "primary" && holder.Reason == "holder_unconfirmed" && holder.Deadline.Equal(original.Seal.Deadline)
		}
	}
	if !foundResidual {
		t.Fatal("lost reply discarded primary responsibility", residual)
	}
	obligation, err := m.ObserveChange(ctx, &contentPrincipal, change.Key, "", 2)
	if err != nil || len(obligation.Responsibilities) != 1 || obligation.Responsibilities[0].BodyCleanup != "pending" {
		t.Fatal("lost reply prematurely ACKed policy duty", obligation, err)
	}
	metadata := content.MetadataPolicy{Ref: alphaRef, Subject: contentPrincipal, Purpose: "verification", Revision: 1, ValidUntil: wide}
	if err = l.InstallMetadataPolicy(ctx, &contentPrincipal, metadata, 0); err != nil {
		t.Fatal(err)
	}
	view, err := service.Get(ctx, contentGetWire(t, alphaRef, nil), &contentPrincipal)
	denied, ok := view.AsRejected()
	if err != nil || !ok || denied.Reason != "forbidden" {
		t.Fatal("physical absence without authoritative ACK did not remain forbidden", view, err)
	}
	w.Reopen(ctx)
	// Step really committed residual+DeferClaim before returning this reply
	// error. Its original 100ms continuation is sufficient: no lease rewrite,
	// one-minute wait, new seal deadline or PostgreSQL failure is invented.
	waitUntil(t, ctx, failedAt.Add(120*time.Millisecond))
	l = bodyLifecycle(t, w)
	if worked, err := l.Step(ctx, &contentPrincipal); err != nil || !worked {
		t.Fatal("confirmed original deferred duty did not recover", worked, err)
	}
	recovered, err := l.Observe(ctx, &contentPrincipal, alphaRef, "")
	if err != nil || !recovered.CleanupComplete || recovered.Seal.ID != original.Seal.ID || !recovered.Seal.Deadline.Equal(original.Seal.Deadline) {
		t.Fatal("retry lost original seal/deadline", recovered, err)
	}
	obligation, err = trustedContentManager(t, w, 2).ObserveChange(ctx, &contentPrincipal, change.Key, "", 2)
	if err != nil || len(obligation.Responsibilities) != 1 || obligation.Responsibilities[0].BodyCleanup != "erased" || !obligation.Responsibilities[0].Deadline.Equal(original.Seal.Deadline) {
		t.Fatal("recovered actual ACK did not finish original policy duty", obligation, err)
	}
	view, err = contentService(t, w).Get(ctx, contentGetWire(t, alphaRef, nil), &contentPrincipal)
	gone, ok := view.AsGone()
	if err != nil || !ok || gone.ContentRef != alphaRef || gone.EvidenceAvailable {
		t.Fatal("recovered authoritative ACK did not allow exact metadata gone", view, err)
	}
}
