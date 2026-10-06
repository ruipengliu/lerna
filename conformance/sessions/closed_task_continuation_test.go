package sessions_test

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/ruipengliu/lerna/cmd/assembly"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

// 规则：G2、G3、G4、G11、G12、完成-4、V4
func TestClosedTaskKeepsOriginalSessionForRecordAndOneNewGoal(t *testing.T) {
	for _, outcome := range []string{"FAILED", "CANCELLED"} {
		t.Run(outcome, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "continuation.db")
			h, e := assembly.Open(path, "alice", "local")
			if e != nil {
				t.Fatal(e)
			}
			t.Cleanup(func() {
				if h != nil {
					h.Close()
				}
			})
			create := &v1.CreateSessionCommand{Header: header("original-session")}
			r, e := h.Sessions.CreateSession(ctx, caller, create)
			createReceipt := accepted(t, r, e)
			id := createReceipt.ResultRef.Name
			first := &v1.SubmitInputCommand{Header: header("original-goal"), SessionId: id, InputKind: "GOAL", ContentRef: stage(t, h, "original-goal-content", "original task")}
			r, e = h.Sessions.SubmitInput(ctx, caller, first)
			firstReceipt := accepted(t, r, e)
			session, e := h.Sessions.QuerySession(ctx, caller, id)
			if e != nil || len(session.GetTaskRefs()) != 1 || len(session.GetInputs()) != 1 || session.LastCommittedSeq != 1 {
				t.Fatalf("real original goal/session missing: %v %v", session, e)
			}
			oldRef := session.TaskRefs[0]
			old, e := h.Tasks.QueryTask(ctx, caller, oldRef.Name)
			if e != nil || old.GetLifecycle() != v1.TaskLifecycle_TASK_LIFECYCLE_OPEN {
				t.Fatalf("real original open task missing: %v %v", old, e)
			}
			var cancel *v1.SubmitInputCommand
			var cancelReceipt *v1.CommandReceipt
			var cancellation *v1.Cancellation
			if outcome == "CANCELLED" {
				cancel = &v1.SubmitInputCommand{Header: header("original-cancel"), SessionId: id, TaskId: old.TaskId, InputKind: "CONTROL", Control: "CANCEL", ExpectedControlGeneration: old.ControlGeneration}
				r, e = h.Sessions.SubmitInput(ctx, caller, cancel)
				cancelReceipt = accepted(t, r, e)
				if e = h.Tasks.ProcessCancellations(ctx, caller); e != nil {
					t.Fatal(e)
				}
				cancellation, e = h.Tasks.QueryCancellation(ctx, caller, old.TaskId)
				if e != nil || cancellation == nil || len(cancellation.AdmissionRefs) != 0 {
					t.Fatalf("real original cancellation missing: %v %v", cancellation, e)
				}
				old, e = h.Tasks.QueryTask(ctx, caller, oldRef.Name)
				if e != nil || old.Control != v1.TaskControl_TASK_CONTROL_CANCELLING {
					t.Fatalf("original cancel control missing: %v %v", old, e)
				}
			}
			closeHeader := header("original-close")
			closeHeader.Identity.IssuerId = "host"
			host := &v1.Caller{UserId: "alice", IssuerId: "host"}
			closeCommand := &v1.BeginTaskCloseCommand{Header: closeHeader, TaskRef: &v1.Ref{Name: old.TaskId, Revision: old.Revision, SchemaId: "lerna.v1.Task"}, ExpectedControlGeneration: old.ControlGeneration, Outcome: outcome, CloseReason: "USER_STOPPED"}
			if cancellation != nil {
				closeCommand.CancellationRef = cancellation.Ref
			}
			r, e = h.Tasks.BeginTaskClose(ctx, host, closeCommand)
			closeReceipt := accepted(t, r, e)
			if e = h.Tasks.ProcessTaskClosings(ctx, host); e != nil {
				t.Fatal(e)
			}
			closed, e := h.Tasks.QueryTaskClosingView(ctx, caller, old.TaskId)
			if e != nil || closed.GetTask().GetLifecycle() != v1.TaskLifecycle_TASK_LIFECYCLE_CLOSED || closed.GetResult().GetOutcome() != outcome || len(closed.Closing.AdmissionRefs) != 0 || len(closed.ClosureIntents) != 0 || len(closed.ClosureSeals) != 0 {
				t.Fatalf("owner protocol did not close original no-admission scope: %v %v", closed, e)
			}
			fixed, e := proto.MarshalOptions{Deterministic: true}.Marshal(closed.Result)
			if e != nil {
				t.Fatal(e)
			}
			oldPlanning, e := h.Tasks.QueryPlanning(ctx, caller, old.TaskId)
			if e != nil {
				t.Fatal(e)
			}
			prefix, e := h.Sessions.QuerySession(ctx, caller, id)
			prefixLength := 1
			if outcome == "CANCELLED" {
				prefixLength = 2
			}
			if e != nil || prefix.GetStatus() != "ACTIVE" || len(prefix.Inputs) != prefixLength || prefix.LastCommittedSeq != uint64(prefixLength) || !proto.Equal(prefix.TaskRefs[0], oldRef) {
				t.Fatalf("closing removed original session/history: %v %v", prefix, e)
			}
			record := &v1.SubmitInputCommand{Header: header("followup-record"), SessionId: id, InputKind: "RECORD", ObservedSessionSeq: prefix.LastCommittedSeq, ContentRef: stage(t, h, "followup-content", "follow-up after closing")}
			r, e = h.Sessions.SubmitInput(ctx, caller, record)
			recordReceipt := accepted(t, r, e)
			afterRecord, e := h.Sessions.QuerySession(ctx, caller, id)
			if e != nil || afterRecord.Revision != prefix.Revision+1 || afterRecord.LastCommittedSeq != uint64(prefixLength+1) || len(afterRecord.TaskRefs) != 1 {
				t.Fatalf("record changed old task association: %v %v", afterRecord, e)
			}
			input := afterRecord.Inputs[prefixLength]
			if input.TaskId != nil || input.RoutingStatus != "RECORDED" || input.InputKind != "RECORD" || !proto.Equal(input.CommandIdentity, record.Header.Identity) {
				t.Fatalf("follow-up implicitly targeted closed task: %v", input)
			}
			expected := afterRecord.Revision
			next := &v1.SubmitGoalCommand{Identity: header("new-goal").Identity, ContractVersion: 1, SchemaId: "lerna.v1.SubmitGoal", FingerprintVersion: 1, Goal: "one distinct new task", Session: id, ExpectedRevision: &expected}
			r, e = h.Sessions.SubmitGoal(ctx, caller, next)
			if e != nil || r.GetPhase() != v1.ReceiptPhase_RECEIPT_PHASE_SUBMITTED {
				t.Fatalf("new same-session goal not saved: %v %v", r, e)
			}
			if e = h.Sessions.ProcessPending(ctx, caller); e != nil {
				t.Fatal(e)
			}
			q, e := h.Durable.QueryReceipt(ctx, caller, next.Identity)
			nextReceipt := accepted(t, q.GetReceipt(), e)
			if nextReceipt.TaskRef == nil || !proto.Equal(nextReceipt.SessionRef.Name, id) || proto.Equal(nextReceipt.TaskRef.Name, old.TaskId) {
				t.Fatalf("new goal did not create distinct task in original session: %v", nextReceipt)
			}
			complete, e := h.Sessions.QuerySession(ctx, caller, id)
			if e != nil || complete.Status != "ACTIVE" || len(complete.TaskRefs) != 2 || len(complete.Inputs) != prefixLength+2 || complete.LastCommittedSeq != uint64(prefixLength+2) || complete.Revision != afterRecord.Revision+1 || !proto.Equal(complete.TaskRefs[0], oldRef) || !proto.Equal(complete.TaskRefs[1], nextReceipt.TaskRef) {
				t.Fatalf("same-session history/one-new-task invariant failed: %v %v", complete, e)
			}
			for i, input := range complete.Inputs {
				if input.SessionSeq != uint64(i+1) || i < prefixLength && !proto.Equal(input, prefix.Inputs[i]) {
					t.Fatalf("original history or monotonic sequence changed: %v", input)
				}
			}
			newInput := complete.Inputs[prefixLength+1]
			if newInput.InputKind != "GOAL" || newInput.TaskInputSeq != 1 || !proto.Equal(newInput.TaskId, nextReceipt.TaskRef.Name) || !proto.Equal(newInput.CommandIdentity, next.Identity) {
				t.Fatalf("new task input not independent: %v", newInput)
			}
			newTask, e := h.Tasks.QueryTask(ctx, caller, nextReceipt.TaskRef.Name)
			if e != nil || newTask.GetLifecycle() != v1.TaskLifecycle_TASK_LIFECYCLE_OPEN {
				t.Fatalf("distinct new task not open: %v %v", newTask, e)
			}
			receipts := []*v1.CommandReceipt{createReceipt, firstReceipt, closeReceipt, recordReceipt, nextReceipt}
			if cancelReceipt != nil {
				receipts = append(receipts, cancelReceipt)
			}
			assertStable := func() {
				t.Helper()
				view, e := h.Tasks.QueryTaskClosingView(ctx, caller, old.TaskId)
				if e != nil || !proto.Equal(view, closed) {
					t.Fatalf("old closed task/control/closing/Result changed: %v %v", view, e)
				}
				b, e := proto.MarshalOptions{Deterministic: true}.Marshal(view.Result)
				if e != nil || !bytes.Equal(b, fixed) {
					t.Fatalf("old fixed Result bytes changed: %v", e)
				}
				planning, e := h.Tasks.QueryPlanning(ctx, caller, old.TaskId)
				if e != nil || !proto.Equal(planning, oldPlanning) {
					t.Fatalf("old planning/control basis changed: %v %v", planning, e)
				}
				session, e := h.Sessions.QuerySession(ctx, caller, id)
				if e != nil || !proto.Equal(session, complete) {
					t.Fatalf("replay/restart duplicated or rewrote session: %v %v", session, e)
				}
				task, e := h.Tasks.QueryTask(ctx, caller, nextReceipt.TaskRef.Name)
				if e != nil || !proto.Equal(task, newTask) {
					t.Fatalf("new original task changed: %v %v", task, e)
				}
				if cancellation != nil {
					scope, e := h.Tasks.QueryCancellation(ctx, caller, old.TaskId)
					if e != nil || !proto.Equal(scope, cancellation) {
						t.Fatalf("original cancellation responsibility changed: %v %v", scope, e)
					}
				}
				for _, receipt := range receipts {
					issuer := &v1.Caller{UserId: "alice", IssuerId: receipt.Identity.IssuerId}
					q, e := h.Durable.QueryReceipt(ctx, issuer, receipt.Identity)
					if e != nil || !proto.Equal(q.GetReceipt(), receipt) {
						t.Fatalf("original receipt changed: %v %v", q, e)
					}
				}
			}
			replay := func() {
				t.Helper()
				r, e := h.Sessions.CreateSession(ctx, caller, create)
				if !proto.Equal(accepted(t, r, e), createReceipt) {
					t.Fatal("session replay created another session")
				}
				r, e = h.Sessions.SubmitInput(ctx, caller, first)
				if !proto.Equal(accepted(t, r, e), firstReceipt) {
					t.Fatal("original goal receipt replay changed")
				}
				if cancel != nil {
					r, e = h.Sessions.SubmitInput(ctx, caller, cancel)
					if !proto.Equal(accepted(t, r, e), cancelReceipt) {
						t.Fatal("original cancellation replay changed")
					}
				}
				r, e = h.Tasks.BeginTaskClose(ctx, host, closeCommand)
				if !proto.Equal(accepted(t, r, e), closeReceipt) {
					t.Fatal("original closing replay changed")
				}
				r, e = h.Sessions.SubmitInput(ctx, caller, record)
				if !proto.Equal(accepted(t, r, e), recordReceipt) {
					t.Fatal("record replay changed")
				}
				r, e = h.Sessions.SubmitGoal(ctx, caller, next)
				if !proto.Equal(accepted(t, r, e), nextReceipt) {
					t.Fatal("new goal replay changed decided receipt")
				}
				if e = h.Sessions.ProcessPending(ctx, caller); e != nil {
					t.Fatal(e)
				}
			}
			assertStable()
			replay()
			assertStable()
			if e = h.Close(); e != nil {
				t.Fatal(e)
			}
			h, e = assembly.Open(path, "alice", "local")
			if e != nil {
				t.Fatal(e)
			}
			assertStable()
			replay()
			assertStable()
			t.Logf("outcome=%s session=%s retained_task=%s new_task=%s distinct_tasks=2 original_prefix=%d final_inputs=%d session_seq=%d old_admissions=0 Result_bytes=%d restart/replay_no_duplicates=true", outcome, id.LocalId, old.TaskId.LocalId, nextReceipt.TaskRef.Name.LocalId, prefixLength, len(complete.Inputs), complete.LastCommittedSeq, len(fixed))
		})
	}
}
