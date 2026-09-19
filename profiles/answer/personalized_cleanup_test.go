package answer

import (
	"context"
	"crypto/sha256"
	"fmt"
	contextmemory "lerna/adapters/context/memory"
	"lerna/authorization"
	"lerna/brain"
	"lerna/contextassembly"
	wire "lerna/gen/harness/v1"
	"lerna/tasks"
	"strings"
	"testing"
	"time"
)

func TestAnswerHostCleansDeletedContextAutomatically(t *testing.T) {
	for _, mutation := range []string{"delete", "revoke"} {
		t.Run(mutation, func(t *testing.T) { checkAnswerHostAutomaticCleanup(t, mutation) })
	}
}
func checkAnswerHostAutomaticCleanup(t *testing.T, mutation string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	limits := tasks.GenerationLimits{Requests: 1, InputTokens: 600, OutputTokens: 100}
	h, err := fresh(ctx, limits)
	if err != nil {
		t.Fatal(err)
	}
	defer h.destroy()
	state, err := h.submit(ctx, limits)
	if err != nil {
		t.Fatal(err)
	}
	state, err = start(ctx, h, state)
	if err != nil {
		t.Fatal(err)
	}
	p, err := h.personalize(ctx, state, "concise", true)
	if err != nil {
		t.Fatal(err)
	}
	defer p.close()
	if _, err = p.session.Assemble(ctx, state.Task, h.location, brain.MaxInputBytes); err != nil {
		t.Fatal(err)
	}
	key := contextassembly.Key{Namespace: "local", TaskID: state.Task.Ref.TaskID, Decision: 1}
	if _, err = p.snapshots.Read(ctx, key); err != nil {
		t.Fatal(err)
	}
	if err = p.change(ctx, h, mutation); err != nil {
		t.Fatal(err)
	}
	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		snapshot, e := p.snapshots.Read(ctx, key)
		if e == contextassembly.Invalidated && len(snapshot.Document) == 0 {
			break
		}
		if e != nil {
			t.Fatal(e)
		}
		select {
		case <-deadline.C:
			t.Fatal("host did not consume deletion cleanup")
		case <-tick.C:
		}
	}
}

func TestAnswerRecoveryKeepsOriginalLeaseDuringLongWork(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	l := tasks.GenerationLimits{Requests: 1, InputTokens: 600, OutputTokens: 100}
	h, err := fresh(ctx, l)
	if err != nil {
		t.Fatal(err)
	}
	defer h.destroy()
	initial, err := h.submit(ctx, l)
	if err != nil {
		t.Fatal(err)
	}
	initial, err = start(ctx, h, initial)
	if err != nil {
		t.Fatal(err)
	}
	stop := keepAnswerRecoveryLease(ctx, h, tasks.QualificationOf(initial))
	defer stop()
	// Cross the original lease boundary; renewal must keep the same decision.
	timer := time.NewTimer(time.Until(time.Unix(0, initial.Work[0].LeaseUntil)) + 100*time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	case <-timer.C:
	}
	current, err := h.core.Load(ctx, initial.Task.Ref)
	if err != nil {
		t.Fatal(err)
	}
	if tasks.QualificationOf(current) != tasks.QualificationOf(initial) || current.Work[0].LeaseUntil <= time.Now().UnixNano() || len(current.Generations) != 1 || current.Generations[0].OutputOperation != initial.Generations[0].OutputOperation {
		t.Fatalf("recovery changed identity or lost lease: %+v", current.Work)
	}
}

func TestAnswerHostCleansDeletedDerivedContentAutomatically(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	l := tasks.GenerationLimits{Requests: 1, InputTokens: 600, OutputTokens: 100}
	h, err := fresh(ctx, l)
	if err != nil {
		t.Fatal(err)
	}
	defer h.destroy()
	state, err := h.submit(ctx, l)
	if err != nil {
		t.Fatal(err)
	}
	state, err = start(ctx, h, state)
	if err != nil {
		t.Fatal(err)
	}
	p, err := h.personalize(ctx, state, "concise", true)
	if err != nil {
		t.Fatal(err)
	}
	defer p.close()
	op, err := h.auth.NewOperation(ctx, h.token)
	if err != nil {
		t.Fatal(err)
	}
	data := []byte(strings.Repeat("synthetic derived preference ", 4))
	digest := sha256.Sum256(data)
	source := contextmemory.SourceReference(contextassembly.Reference{Namespace: "local", Collection: "personal", Key: "answer-style", Revision: 1})
	before, err := h.blobs.List(ctx, 64)
	if err != nil {
		t.Fatal(err)
	}
	known := map[string]bool{}
	for _, file := range before {
		known[file.Key] = true
	}
	result, err := h.content.Call(ctx, h.binding(), &wire.ContentRequest{Method: "PUT", OperationId: op, Spec: &wire.ContentSpec{Kind: "evidence", Resource: "root", Purpose: "task", Sources: []*wire.ContentSource{source}, AcquiredAt: time.Now().Unix(), MediaType: "text/plain", Size: uint64(len(data)), Sha256: fmt.Sprintf("%x", digest), RetainUntil: time.Now().Add(time.Minute).Unix()}, Data: data})
	if err != nil {
		t.Fatal(err)
	}
	if result.Record.State != "available" {
		t.Fatal("derived object unavailable before deletion")
	}
	files, err := h.blobs.List(ctx, 64)
	if err != nil {
		t.Fatal(err)
	}
	var derivedFile string
	for _, file := range files {
		if !known[file.Key] {
			if derivedFile != "" {
				t.Fatal("unexpected extra object")
			}
			derivedFile = file.Key
		}
	}
	if derivedFile == "" {
		t.Fatal("derived body was not stored as a file")
	}
	if err = p.change(ctx, h, "delete"); err != nil {
		t.Fatal(err)
	}
	wait, stop := context.WithTimeout(ctx, 3*time.Second)
	defer stop()
	for {
		files, e := h.blobs.List(wait, 64)
		if e != nil {
			t.Fatal(e)
		}
		present := false
		for _, file := range files {
			present = present || file.Key == derivedFile
		}
		if !present {
			out, e := h.content.Call(ctx, h.binding(), &wire.ContentRequest{Method: "READ", Ref: result.Record.Ref, Purpose: "task", Limit: 1})
			if e == nil || out != nil {
				t.Fatal("deleted source still disclosed derived content")
			}
			return
		}
		select {
		case <-wait.Done():
			t.Fatal("host did not clean derived object after deletion")
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func TestAnswerHostErasesDeletedMemoryAdmissionComparison(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	l := tasks.GenerationLimits{Requests: 1, InputTokens: 600, OutputTokens: 100}
	h, err := fresh(ctx, l)
	if err != nil {
		t.Fatal(err)
	}
	defer h.destroy()
	state, err := h.submit(ctx, l)
	if err != nil {
		t.Fatal(err)
	}
	state, err = start(ctx, h, state)
	if err != nil {
		t.Fatal(err)
	}
	p, err := h.personalize(ctx, state, "concise", true)
	if err != nil {
		t.Fatal(err)
	}
	defer p.close()
	original, err := h.auth.InspectMemoryOperation(ctx, h.token, p.original.OperationId)
	if err != nil || original.Admission.SemanticSHA256 == "" {
		t.Fatal("missing original admission")
	}
	if err = p.change(ctx, h, "delete"); err != nil {
		t.Fatal(err)
	}
	wait, stop := context.WithTimeout(ctx, 3*time.Second)
	defer stop()
	for {
		current, e := h.auth.InspectMemoryOperation(wait, h.token, p.original.OperationId)
		if e != nil {
			t.Fatal(e)
		}
		if current.Admission.SemanticSHA256 == "" {
			if current.State != "reserved" || current.Admission.Subject != original.Admission.Subject {
				t.Fatal("cleanup lost original admission")
			}
			_, e = h.auth.ReserveMemoryOperation(ctx, h.token, p.original.OperationId, original.Admission.SemanticSHA256, original.Admission.Action)
			if !authorization.Is(e, authorization.ResultOnly) {
				t.Fatalf("erased operation admitted again: %v", e)
			}
			return
		}
		select {
		case <-wait.Done():
			t.Fatal("host retained deleted memory comparison")
		case <-time.After(20 * time.Millisecond):
		}
	}
}
