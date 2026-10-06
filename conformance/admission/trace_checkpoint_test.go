package admission_test

import (
	"context"
	"testing"

	"github.com/ruipengliu/lerna/cmd/assembly"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

// 规则：G1、G3、G11、G12、R7
func TestTraceCheckpointMismatchReplaysOriginalItemsAcrossDisconnectAndRestart(t *testing.T) {
	f := newFixture(t, 100, 80, false)
	sources, err := f.h.Trace.QuerySources(f.ctx, f.caller)
	if err != nil {
		t.Fatal(err)
	}
	var later *v1.AcceptTraceCommand
	for _, source := range sources {
		if source.Command.Event.SourceStreamId == "d/tasks" && source.Command.Event.SourceSeq == 2 {
			later = source.Command
		}
	}
	if later == nil {
		t.Fatal("missing actual original second source item")
	}
	actor := &v1.Caller{UserId: "u", IssuerId: later.Header.Identity.IssuerId}
	receipt, err := f.h.Trace.Accept(f.ctx, actor, later)
	accepted(t, receipt, err)
	duplicate, err := f.h.Trace.Accept(f.ctx, actor, later)
	accepted(t, duplicate, err)
	if !proto.Equal(receipt, duplicate) {
		t.Fatal("reordered duplicate changed the original receiver receipt")
	}
	if err = f.h.Trace.Index(f.ctx, f.caller); err != nil {
		t.Fatal(err)
	}
	before, err := f.h.Trace.Query(f.ctx, f.caller, &v1.TraceQuery{TaskId: f.task.Name})
	if err != nil {
		t.Fatal(err)
	}
	var progress *v1.TraceProgress
	for _, p := range before.Sources {
		if p.SourceStreamId == "d/tasks" {
			progress = p
		}
	}
	if before.Complete || progress == nil || progress.AcceptedContiguous != 0 || progress.IndexedContiguous != 0 || len(progress.Gaps) == 0 || progress.Gaps[0].First != 1 || progress.PendingReceipts == 0 {
		t.Fatalf("seq2 crossed an unconfirmed continuous prefix: %v", before)
	}
	pending, err := f.h.Trace.QuerySources(f.ctx, f.caller)
	if err != nil || len(pending) != len(sources) {
		t.Fatalf("source population changed: %v", err)
	}
	for i := range sources {
		if !proto.Equal(sources[i], pending[i]) {
			t.Fatal("receiver acceptance implicitly advanced source acknowledgement")
		}
	}
	disconnected, cancel := context.WithCancel(f.ctx)
	cancel()
	if err = f.h.Trace.Collect(disconnected, f.caller); err == nil {
		t.Fatal("simulated disconnected collection was hidden")
	}
	afterDisconnect, err := f.h.Trace.Query(f.ctx, f.caller, &v1.TraceQuery{TaskId: f.task.Name})
	if err != nil || !proto.Equal(before, afterDisconnect) {
		t.Fatalf("disconnection advanced receiver progress: %v", err)
	}
	unchanged, err := f.h.Trace.QuerySources(f.ctx, f.caller)
	if err != nil || len(unchanged) != len(pending) {
		t.Fatalf("disconnection changed source population: %v", err)
	}
	for i := range pending {
		if !proto.Equal(pending[i], unchanged[i]) {
			t.Fatal("disconnection saved an unconfirmed source receipt")
		}
	}
	q, err := f.h.Trace.QueryReceipt(f.ctx, actor, later.Header.Identity)
	if err != nil || !proto.Equal(q.Receipt, receipt) {
		t.Fatalf("disconnection changed original receiver receipt: %v %v", q, err)
	}
	if err = f.h.Close(); err != nil {
		t.Fatal(err)
	}
	f.h, err = assembly.Open(f.path, "u", "d")
	if err != nil {
		t.Fatal(err)
	}
	restored, err := f.h.Trace.Query(f.ctx, f.caller, &v1.TraceQuery{TaskId: f.task.Name})
	if err != nil || !restored.Complete || restored.PendingReceipts != 0 {
		t.Fatalf("original pending items failed to repair the prefix: %v %v", restored, err)
	}
	for _, p := range restored.Sources {
		if p.AcceptedContiguous != p.Cutoff || p.IndexedContiguous != p.Cutoff || len(p.Gaps) != 0 {
			t.Fatalf("recovery falsely advanced a discontinuous checkpoint: %v", p)
		}
	}
	recovered, err := f.h.Trace.QuerySources(f.ctx, f.caller)
	if err != nil || len(recovered) != len(sources) {
		t.Fatalf("recovery replaced source identities: %v", err)
	}
	for i, source := range recovered {
		if !proto.Equal(source.Command, sources[i].Command) || source.Receipt == nil {
			t.Fatal("recovery lost original source item or per-item acknowledgement")
		}
		owner := &v1.Caller{UserId: "u", IssuerId: source.Command.Header.Identity.IssuerId}
		item, e := f.h.Trace.QueryReceipt(f.ctx, owner, source.Command.Header.Identity)
		if e != nil || !proto.Equal(item.Receipt, source.Receipt) || proto.Equal(source.Command, later) && !proto.Equal(item.Receipt, receipt) {
			t.Fatalf("source checkpoint was not confirmed by its original receipt: %v %v", item, e)
		}
	}
	count := 0
	for _, event := range restored.Events {
		if proto.Equal(event.Ref, later.Event.Ref) {
			count++
			if !proto.Equal(event, later.Event) {
				t.Fatal("replay changed the original reordered event")
			}
		}
	}
	if count != 1 {
		t.Fatalf("reordered event appeared %d times", count)
	}
	if err = f.h.Trace.Recover(f.ctx, f.caller); err != nil {
		t.Fatal(err)
	}
	repeated, err := f.h.Trace.Query(f.ctx, f.caller, &v1.TraceQuery{TaskId: f.task.Name})
	if err != nil || !proto.Equal(restored, repeated) || f.calls.Load() != 0 {
		t.Fatalf("repeat original-item recovery changed view or physical target: %v", err)
	}
}
