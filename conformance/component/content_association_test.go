//go:build integration

package component_test

import (
	"context"
	"errors"
	fixture "github.com/ruipengliu/lerna/conformance/internal/contentfixture"
	v "github.com/ruipengliu/lerna/contract/v1_2"
	"github.com/ruipengliu/lerna/domain/content"
	"os"
	"testing"
	"time"
)

func TestContentNewAssociationRequiresCurrentDirectSourceAdmission(t *testing.T) {
	for _, action := range []string{"process", "save"} {
		t.Run(action, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			w := fixture.New(t, ctx)
			service, request, source := publishedDirectContent(t, ctx, w)
			original := putContentRequest(t, ctx, service, request)
			normal := request
			normal.CommandID = "normal-association"
			if _, ok := putContentRequest(t, ctx, service, normal).AsAccepted(); !ok {
				t.Fatal("normal association refused")
			}
			wide := time.Now().Add(time.Hour)
			policy := content.FixturePolicy{Ref: source, Subject: contentPrincipal, Purpose: "verification", Revision: 2, ValidUntil: wide, RetainUntil: wide, Read: true, Process: true, Save: true, Disclose: true}
			if action == "process" {
				policy.Process = false
			} else {
				policy.Save = false
			}
			if err := w.Store().InstallFixturePolicy(ctx, policy, 1); err != nil {
				t.Fatal(err)
			}
			alias := request
			alias.CommandID = "denied-association"
			requireRejection(t, putContentRequest(t, ctx, service, alias), "forbidden")
			before, _ := v.Encode(original)
			after, _ := v.Encode(putContentRequest(t, ctx, service, request))
			if string(before) != string(after) {
				t.Fatal("current put denial replaced original receipt")
			}
			assertContentBody(t, ctx, service, request.Payload.ContentRef, nil, "alpha\n")
			if processed, err := service.Step(ctx); err != nil || processed {
				t.Fatal("association reopened publication", err)
			}
			entries, err := os.ReadDir(w.Directory)
			if err != nil || len(entries) != 3 {
				t.Fatal("association wrote extra native object", err)
			}
		})
	}
}
func TestContentAssociationCannotExtendObservedRetention(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	w := fixture.New(t, ctx)
	installContentPolicy(t, ctx, w, alphaRef)
	service := contentService(t, w)
	request := contentPut(t, alphaRef, "original", "YWxwaGEK")
	original := putContentRequest(t, ctx, service, request)
	if _, err := service.Step(ctx); err != nil {
		t.Fatal(err)
	}
	assertContentBody(t, ctx, service, alphaRef, nil, "alpha\n")
	narrow := time.Now().UTC().Add(800 * time.Millisecond).Truncate(time.Microsecond)
	wide := time.Now().Add(time.Hour)
	policy := content.FixturePolicy{Ref: alphaRef, Subject: contentPrincipal, Purpose: "verification", Revision: 2, ValidUntil: wide, RetainUntil: narrow, Read: true, Process: true, Save: true, Disclose: true}
	if err := w.Store().InstallFixturePolicy(ctx, policy, 1); err != nil {
		t.Fatal(err)
	}
	alias := request
	alias.CommandID = "tightened-association"
	accepted, ok := putContentRequest(t, ctx, service, alias).AsAccepted()
	first, _ := original.AsAccepted()
	if !ok || accepted.RetainUntil != first.RetainUntil {
		t.Fatal("legitimate association changed original effective receipt")
	}
	w.Reopen(ctx)
	service = contentService(t, w)
	policy.Revision = 3
	policy.RetainUntil = wide
	if err := w.Store().InstallFixturePolicy(ctx, policy, 2); err != nil {
		t.Fatal(err)
	}
	timer := time.NewTimer(time.Until(narrow) + 20*time.Millisecond)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	alias.CommandID = "expired-association"
	requireRejection(t, putContentRequest(t, ctx, service, alias), "expired")
	view, err := service.Get(ctx, contentGetWire(t, alphaRef, nil), &contentPrincipal)
	denied, ok := view.AsRejected()
	if err != nil || !ok || denied.Reason != "expired" {
		t.Fatal("observed cap widened after reopen", err)
	}
	before, _ := v.Encode(original)
	after, _ := v.Encode(putContentRequest(t, ctx, service, request))
	if string(before) != string(after) {
		t.Fatal("expired current content erased historical accepted")
	}
}
func TestContentReplayRechecksReaderAfterCommandLock(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	w := fixture.New(t, ctx)
	installContentPolicy(t, ctx, w, alphaRef)
	service := contentService(t, w)
	request := contentPut(t, alphaRef, "original", "YWxwaGEK")
	until := time.Now().UTC().Add(450 * time.Millisecond)
	request.AcceptBefore = v.Time(until.Truncate(time.Microsecond).Format("2006-01-02T15:04:05.000000Z"))
	original := putContentRequest(t, ctx, service, request)
	if _, err := service.Step(ctx); err != nil {
		t.Fatal(err)
	}
	if err := w.Store().InstallFixtureCommandReader(ctx, contentOwner, contentPrincipal, until); err != nil {
		t.Fatal(err)
	}
	release, waitBlocked := w.HoldCommand(ctx, v.CommandRef{Owner: contentOwner, CommandID: request.CommandID})
	raw, err := v.Encode(request)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	finished := make(chan struct{})
	go func() { defer close(finished); _, err := service.Put(ctx, raw, &contentPrincipal); done <- err }()
	joinContentRead(t, w, release, finished)
	if err = waitBlocked(ctx); err != nil {
		t.Fatal(err)
	}
	timer := time.NewTimer(time.Until(until) + 30*time.Millisecond)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if err = release(); err != nil {
		t.Fatal(err)
	}
	select {
	case err = <-done:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	var refusal *v.ContractError
	if !errors.As(err, &refusal) || refusal.Code != "forbidden" {
		t.Fatal("reader expired during original command wait but receipt disclosed", err)
	}
	if err = w.Store().InstallFixtureCommandReader(ctx, contentOwner, contentPrincipal, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	before, _ := v.Encode(original)
	after, _ := v.Encode(putContentRequest(t, ctx, service, request))
	if string(before) != string(after) {
		t.Fatal("old admission deadline replaced authorized fixed replay")
	}
	assertContentBody(t, ctx, service, alphaRef, nil, "alpha\n")
}

func TestContentAssociationKeepsPreparingCapacityAndFailedHistory(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	w := fixture.New(t, ctx)
	installContentPolicy(t, ctx, w, alphaRef)
	service, err := content.New(content.Config{Owner: contentOwner, Store: w.Store(), Objects: w.Objects, Limits: content.Limits{MaxPreparingVersions: 1, MaxStagingBytes: 6, Lease: time.Second, WorkTimeout: time.Second}, PublishBudget: 400 * time.Millisecond, MaxPublicationAttempts: 1, Worker: "association-normal"})
	if err != nil {
		t.Fatal(err)
	}
	request := contentPut(t, alphaRef, "original-finite", "YWxwaGEK")
	first := putContentRequest(t, ctx, service, request)
	if _, ok := first.AsAccepted(); !ok {
		t.Fatal("normal admission refused")
	}
	alias := request
	alias.CommandID = "full-capacity-alias"
	if _, ok := putContentRequest(t, ctx, service, alias).AsAccepted(); !ok {
		t.Fatal("alias incorrectly charged another staging allocation")
	}
	waitUntil(t, ctx, time.Now().Add(450*time.Millisecond))
	if processed, err := service.Step(ctx); err != nil || !processed {
		t.Fatal("original finite responsibility not closed", err)
	}
	view, err := service.Get(ctx, contentGetWire(t, alphaRef, nil), &contentPrincipal)
	failed, ok := view.AsFailed()
	if err != nil || !ok || failed.Reason != "expired" {
		t.Fatal("finite publication deadline did not fail original", err)
	}
	alias.CommandID = "failed-history-alias"
	if _, ok := putContentRequest(t, ctx, service, alias).AsAccepted(); !ok {
		t.Fatal("legitimate failed association reused old publication deadline")
	}
	if processed, err := service.Step(ctx); err != nil || processed {
		t.Fatal("failed alias reopened job", err)
	}
	found, err := service.GetCommand(ctx, contentCommandGetWire(t, alias.CommandID), &contentPrincipal)
	record, ok := found.AsFound()
	if err != nil || !ok {
		t.Fatal("failed association absent", err)
	}
	progress, ok := record.Progress.AsContent()
	if !ok || progress.Publication != "failed" {
		t.Fatal("failed association changed historical publication")
	}
	entries, err := os.ReadDir(w.Directory)
	if err != nil || len(entries) != 0 {
		t.Fatal("failed association wrote bytes", err)
	}
}
