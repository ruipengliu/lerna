//go:build integration

package component_test

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	adapter "github.com/ruipengliu/lerna/adapters/content/decision"
	engine "github.com/ruipengliu/lerna/components/decision_engine"
	fixture "github.com/ruipengliu/lerna/conformance/internal/decisionfixture"
	v "github.com/ruipengliu/lerna/contract/v1_1"
	c "github.com/ruipengliu/lerna/contract/v1_2"
	compiler "github.com/ruipengliu/lerna/domain/task/context"
)

// The gate forwards the actual request once. It creates no error and changes
// no row, permission or deadline; it lets the real publisher reach the input
// row wait after its preceding Current qualifications have completed.
type publicationStageEntry struct {
	adapter.Access
	entered chan<- c.ContentPutRequest
	forward <-chan struct{}
}

func (a publicationStageEntry) StagePublication(ctx context.Context, in compiler.Input, request c.ContentPutRequest) ([]byte, error) {
	select {
	case a.entered <- request:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	select {
	case <-a.forward:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return a.Access.StagePublication(ctx, in, request)
}

type publicationCauseResult struct {
	ref v.ContentRef
	err error
}

func launchPublicationCauseActor(t *testing.T, w *fixture.ContextWorld, call func() publicationCauseResult) (<-chan publicationCauseResult, <-chan struct{}) {
	t.Helper()
	result := make(chan publicationCauseResult, 1)
	done := make(chan struct{})
	if err := w.Fixture.BorrowWorkerExit(done); err != nil {
		close(done)
		t.Fatal(err)
	}
	go func() {
		defer close(done)
		result <- call()
	}()
	return result, done
}

func joinPublicationCauseActor(t *testing.T, ctx context.Context, result <-chan publicationCauseResult, done <-chan struct{}) publicationCauseResult {
	t.Helper()
	var value publicationCauseResult
	select {
	case value = <-result:
	case <-ctx.Done():
		t.Fatal("actual publication actor result unconfirmed", ctx.Err())
	}
	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("actual publication actor exit unconfirmed", ctx.Err())
	}
	return value
}

func assertPublicationCommandAbsent(t *testing.T, ctx context.Context, w *fixture.ContextWorld, request c.ContentPutRequest) {
	t.Helper()
	owner := c.OwnerRef{TenantID: request.Target.TenantID, OwnerID: request.Target.OwnerID}
	query, err := c.Encode(c.CommandGetRequest{ContractVersion: c.Version, Profile: "command", CommandID: "stage-cause-query", Target: c.CommandTarget{TenantID: owner.TenantID, OwnerID: owner.OwnerID, Kind: "command", ID: request.CommandID}, Method: "command.get", AcceptBefore: request.AcceptBefore, Payload: c.CommandGetPayload{CommandRef: c.CommandRef{Owner: owner, CommandID: request.CommandID}}})
	if err != nil {
		t.Fatal(err)
	}
	response, err := w.Content.GetCommand(ctx, query, &w.Subject)
	if err != nil {
		t.Fatal("current authorized publication command query failed", err)
	}
	if _, ok := response.AsNotFound(); !ok {
		t.Fatal("canceled original publication unexpectedly reached Content admission", response)
	}
}

func TestContentContextStagePublicationPreservesCanceledInputLockCause(t *testing.T) {
	t.Run("normal", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		w := fixture.NewContextWorld(t, ctx)
		bundle, err := w.Compile(ctx)
		if err != nil {
			t.Fatal(err)
		}
		source, err := adapter.ToRule(bundle.Mandatory.Ref)
		if err != nil {
			t.Fatal(err)
		}
		permission := adapter.PermissionFor(w.Input, w.Principal)
		body := []byte("stage cause normal\n")
		ref, err := w.Adapter.Publish(ctx, "stage-cause-normal", body, []v.ContentRef{source}, permission)
		if err != nil {
			t.Fatal("actual normal publication failed", err)
		}
		check := func() {
			t.Helper()
			read, err := w.Adapter.ReadPublished(ctx, ref, permission)
			if err != nil || !bytes.Equal(read, body) {
				t.Fatal("accurate normal publication bytes changed", err)
			}
		}
		check()
		w.Reopen(ctx)
		check()
	})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	w := fixture.NewContextWorld(t, ctx)
	bundle, err := w.Compile(ctx)
	if err != nil {
		t.Fatal(err)
	}
	source, err := adapter.ToRule(bundle.Mandatory.Ref)
	if err != nil {
		t.Fatal(err)
	}
	publisherPeer, err := w.OpenCompileBudgetPeer(ctx)
	if err != nil {
		t.Fatal(err)
	}
	lockerPeer, err := w.OpenCompileBudgetPeer(ctx)
	if err != nil {
		t.Fatal(err)
	}
	entered := make(chan c.ContentPutRequest, 1)
	forward := make(chan struct{})
	release := make(chan struct{})
	var forwardOnce, releaseOnce sync.Once
	allowStage := func() { forwardOnce.Do(func() { close(forward) }) }
	unlock := func() { releaseOnce.Do(func() { close(release) }) }
	publishCtx, cancelPublish := context.WithCancel(ctx)
	t.Cleanup(func() { cancelPublish(); allowStage(); unlock() })
	publisher, err := adapter.New(adapter.Config{Owner: w.Dispatcher.ContentOwner, Subject: w.Subject, Purpose: w.Input.Purpose, Content: w.Content, Access: publicationStageEntry{Access: publisherPeer.Dispatcher, entered: entered, forward: forward}})
	if err != nil {
		t.Fatal(err)
	}
	permission := adapter.PermissionFor(w.Input, w.Principal)
	actor, actorDone := launchPublicationCauseActor(t, w, func() publicationCauseResult {
		ref, err := publisher.Publish(publishCtx, "stage-cause-canceled", []byte("stage cause canceled\n"), []v.ContentRef{source}, permission)
		return publicationCauseResult{ref: ref, err: err}
	})
	var request c.ContentPutRequest
	select {
	case request = <-entered:
	case <-ctx.Done():
		t.Fatal("actual publication did not arrive at StagePublication", ctx.Err())
	}
	held := make(chan fixture.ContextInputLockObservation, 1)
	locker, lockerDone := launchPublicationCauseActor(t, w, func() publicationCauseResult {
		return publicationCauseResult{err: lockerPeer.Dispatcher.HoldContextInputRow(ctx, "context-input", held, release)}
	})
	var lock fixture.ContextInputLockObservation
	select {
	case lock = <-held:
	case <-ctx.Done():
		t.Fatal("actual input row lock unconfirmed", ctx.Err())
	}
	if lock.InputID != "context-input" || lock.BackendPID != lockerPeer.BackendPID {
		t.Fatal("actual row lock acknowledgement changed native scope")
	}
	allowStage()
	if err = w.Dispatcher.WaitContextInputRowBlocked(ctx, publisherPeer.BackendPID, lockerPeer.BackendPID); err != nil {
		t.Fatal("actual StagePublication backend did not wait on the original held input", err)
	}
	cancelPublish()
	result := joinPublicationCauseActor(t, ctx, actor, actorDone)
	unlock()
	if locked := joinPublicationCauseActor(t, ctx, locker, lockerDone); locked.err != nil {
		t.Fatal("actual input row locker exit failed", locked.err)
	}
	if err = publisherPeer.Close(); err != nil {
		t.Fatal(err)
	}
	if err = lockerPeer.Close(); err != nil {
		t.Fatal(err)
	}
	assertPublicationCommandAbsent(t, ctx, w, request)
	w.Reopen(ctx)
	assertPublicationCommandAbsent(t, ctx, w, request)
	if !errors.Is(result.err, context.Canceled) || errors.Is(result.err, engine.ErrForbidden) {
		t.Fatal("real canceled StagePublication input read lost its original cause", result.err)
	}
	t.Logf("actual original input row canceled after native wait: publisher=%d blocker=%d", publisherPeer.BackendPID, lockerPeer.BackendPID)
}
