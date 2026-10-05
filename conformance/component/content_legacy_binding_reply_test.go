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
	"github.com/ruipengliu/lerna/contract"
	v "github.com/ruipengliu/lerna/contract/v1_2"
	"github.com/ruipengliu/lerna/domain/content"
	"github.com/ruipengliu/lerna/runtime"
)

// Lose only a returned reply after the original Store confirms actual commit.
// This is not a PostgreSQL commit failure or an unknown native outcome.
type lostLegacyCommitReply struct {
	content.LifecycleRepository
	ref                    v.ContentRef
	cause                  error
	wrote, lost, committed bool
}

func (r *lostLegacyCommitReply) BindLegacyPrimary(ctx context.Context, tx runtime.Tx, expected content.Record, qualification content.LegacyPrimaryQualification) (content.Record, error) {
	next, err := r.LifecycleRepository.BindLegacyPrimary(ctx, tx, expected, qualification)
	if err == nil && expected.Ref == r.ref && expected.PrimaryHolderBinding == "" {
		r.wrote = true
	}
	return next, err
}
func (r *lostLegacyCommitReply) Within(ctx context.Context, owner contract.OwnerRef, fn func(context.Context, runtime.Tx) error) error {
	r.wrote = false
	err := r.LifecycleRepository.Within(ctx, owner, fn)
	if err == nil && r.wrote && !r.lost {
		r.committed = true
		r.lost = true
		return r.cause
	}
	return err
}

func TestContentLegacyCommittedBindingReplyLossRecoversOnlyByOriginalQualification(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	w, original, qualified := fixture.NewStoppedLegacy(t, ctx, nil)
	first := original.Requests[0].Payload.ContentRef
	second := original.Requests[1].Payload.ContentRef
	lost := errors.New("one mechanically lost legacy binding commit reply")
	decorated := &lostLegacyCommitReply{LifecycleRepository: w.Store(), ref: first, cause: lost}
	consumer := func(store content.LifecycleRepository) *content.Lifecycle {
		t.Helper()
		l, err := content.NewLifecycle(content.LifecycleConfig{ManagementConfig: content.ManagementConfig{Owner: contentOwner, Store: store, TrustedSubject: contentPrincipal, TrustedUntil: time.Now().Add(time.Hour), PageSize: 2, WorkBudget: time.Minute}, Objects: w.Objects, PrimaryHolderID: "primary", Worker: "legacy-reply-loss", LegacyPrimary: &qualified})
		if err != nil {
			t.Fatal(err)
		}
		return l
	}
	request := content.LegacyPrimaryRequest{Ref: first, Purpose: "verification"}
	if _, err := consumer(decorated).BindLegacyPrimary(ctx, &contentPrincipal, request); !errors.Is(err, lost) || !decorated.committed || !decorated.lost {
		t.Fatal("actual confirmed commit reply was not lost", err, decorated.committed)
	}
	w.Reopen(ctx)
	l := consumer(w.Store())
	recovered, err := l.BindLegacyPrimary(ctx, &contentPrincipal, request)
	if err != nil || recovered.Ref != first || recovered.Binding != qualified.Binding || recovered.QualificationID != qualified.ID || recovered.EvidenceDigest != qualified.EvidenceDigest || recovered.Publication != "published" {
		t.Fatal("same original qualification did not recover confirmed commit", recovered, err)
	}
	if _, err = l.BindLegacyPrimary(ctx, &contentPrincipal, content.LegacyPrimaryRequest{Ref: second, Purpose: "verification"}); err != nil {
		t.Fatal("original independent Version2 failed", err)
	}
	service := contentService(t, w)
	assertContentBody(t, ctx, service, first, nil, "alpha\n")
	assertContentBody(t, ctx, service, second, nil, "beta\n")
	assertHistory := func() {
		t.Helper()
		for i, request := range original.Requests {
			before, err := v.Encode(original.Receipts[i])
			if err != nil {
				t.Fatal(err)
			}
			after, err := v.Encode(putContentRequest(t, ctx, service, request))
			if err != nil || string(before) != string(after) {
				t.Fatal("reply loss changed original receipt", err)
			}
			command, err := service.GetCommand(ctx, contentCommandGetWire(t, request.CommandID), &contentPrincipal)
			found, ok := command.AsFound()
			history, published := found.Progress.AsContent()
			if err != nil || !ok || !published || history.Publication != "published" {
				t.Fatal("reply loss changed original publication history", command, err)
			}
		}
	}
	assertHistory()
	sealRequest := content.SealRequest{Ref: first, Purpose: "verification", SealID: "legacy-confirmed-commit-cleanup", Deadline: time.Now().UTC().Add(time.Minute).Truncate(time.Microsecond)}
	seal, err := l.Seal(ctx, &contentPrincipal, sealRequest)
	if err != nil || seal.Seal.PrimaryHolderBinding != qualified.Binding {
		t.Fatal("recovered original responsibility cannot seal", seal, err)
	}
	for i := 0; i < 4; i++ {
		if _, err = l.Step(ctx, &contentPrincipal); err != nil {
			t.Fatal(err)
		}
		seal, err = l.Observe(ctx, &contentPrincipal, first, "")
		if err != nil {
			t.Fatal(err)
		}
		if seal.CleanupComplete {
			break
		}
	}
	if !seal.CleanupComplete || seal.Seal.ID != sealRequest.SealID || !seal.Seal.Deadline.Equal(sealRequest.Deadline) {
		t.Fatal("confirmed commit left partial holder responsibility", seal)
	}
	_, key, err := content.VersionIdentity(first)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = os.Lstat(filepath.Join(w.Directory, key)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("actual original body survived confirmed cleanup", err)
	}
	for _, effect := range original.Attempts[key] {
		if _, err = os.Lstat(filepath.Join(w.Directory, effect.Attempt)); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("original captured attempt survived committed responsibility cleanup", effect, err)
		}
	}
	w.Reopen(ctx)
	service = contentService(t, w)
	complete, err := consumer(w.Store()).Observe(ctx, &contentPrincipal, first, "")
	if err != nil || !complete.CleanupComplete || complete.Seal.ID != sealRequest.SealID {
		t.Fatal("reopen lost recovered cleanup", complete, err)
	}
	assertContentBody(t, ctx, service, second, nil, "beta\n")
	assertHistory()
}
