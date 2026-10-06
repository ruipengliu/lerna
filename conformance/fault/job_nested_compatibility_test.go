//go:build fault

package fault_test

import (
	"context"
	"encoding/hex"
	"errors"
	"path/filepath"
	"testing"

	"github.com/ruipengliu/lerna/cmd/assembly"
	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
)

// 规则：G3、G11、R7、V4
func TestLiveClaimRefusesNestedUnknownSavedGoalMetadata(t *testing.T) {
	for _, field := range []string{"identity", "session", "ref"} {
		t.Run(field, func(t *testing.T) {
			f := newNestedGoalFixture(t)
			original, job := f.submit(t, "nested-goal")
			changed := injectNestedGoalMetadata(t, f.h, job, field)
			c := claimCommand("nested-claim", "nested-worker", 60000)
			c.Module = "sessions"
			r, e := f.h.Durable.ExecuteJob(f.ctx, f.caller, c)
			if e != nil || r.GetDecision() != v1.Decision_DECISION_REJECTED || r.GetError().GetCode() != "UNSUPPORTED_FEATURE" || len(r.GetJobs()) != 0 {
				t.Errorf("unsupported saved metadata was claimed: %v %v", r, e)
			}
			f.assertUnchanged(t, original, changed)
			q, e := f.h.Durable.QueryReceipt(f.ctx, f.caller, c.Identity)
			if e != nil || q.State != v1.ReceiptQueryState_RECEIPT_QUERY_STATE_DECIDED || !proto.Equal(q.Receipt, r) {
				t.Fatalf("new claim decision was not saved: %v %v", q, e)
			}
			replay, e := f.h.Durable.ExecuteJob(f.ctx, f.caller, c)
			if e != nil || !proto.Equal(replay, r) {
				t.Fatalf("original claim decision changed on replay: %v %v", replay, e)
			}
		})
	}
}

// 规则：G3、G11、R7、V4
func TestHeldGoalClaimRefusesNestedUnknownCurrentJob(t *testing.T) {
	for _, field := range []string{"identity", "session", "ref"} {
		t.Run(field, func(t *testing.T) {
			f := newNestedGoalFixture(t)
			original, _ := f.submit(t, "held-goal")
			c := claimCommand("held-nested-claim", "held-worker", 60000)
			c.Module = "sessions"
			r, e := f.h.Durable.ExecuteJob(f.ctx, f.caller, c)
			if e != nil || r.GetDecision() != v1.Decision_DECISION_ACCEPTED || len(r.GetJobs()) != 1 {
				t.Fatalf("public original claim: %v %v", r, e)
			}
			old := proto.Clone(r.Jobs[0]).(*v1.Job)
			changed := injectNestedGoalMetadata(t, f.h, r.Jobs[0], field)
			e = f.h.Sessions.ProcessClaim(f.ctx, old)
			var failure *command.Failure
			if !errors.As(e, &failure) || failure.Detail.Code != "UNSUPPORTED_FEATURE" {
				t.Errorf("old clean claim interpreted unsupported current job: %v", e)
			}
			f.assertUnchanged(t, original, changed)
			q, e := f.h.Durable.QueryReceipt(f.ctx, f.caller, c.Identity)
			if e != nil || !proto.Equal(q.GetReceipt(), r) {
				t.Fatalf("previously accepted claim receipt rewritten: %v %v", q, e)
			}
			replay, e := f.h.Durable.ExecuteJob(f.ctx, f.caller, c)
			if e != nil || !proto.Equal(replay, r) {
				t.Fatalf("original accepted claim replay changed: %v %v", replay, e)
			}
			f.assertUnchanged(t, original, changed)
		})
	}
}

// 规则：G3、G11、R7、V4
func TestMixedClaimBatchRefusesNestedUnknownWithoutPartialAdvance(t *testing.T) {
	for _, field := range []string{"identity", "session", "ref"} {
		t.Run(field, func(t *testing.T) {
			f := newNestedGoalFixture(t)
			receipts := make(map[string]*v1.CommandReceipt)
			for _, id := range []string{"mixed-first", "mixed-second"} {
				r, _ := f.submit(t, id)
				receipts[id] = r
			}
			pending, e := f.h.Durable.Pending(f.ctx, f.caller)
			if e != nil || len(pending) != 2 {
				t.Fatalf("actual mixed candidate ordering: %v %v", pending, e)
			}
			known := proto.Clone(pending[0]).(*v1.Job)
			changed := injectNestedGoalMetadata(t, f.h, pending[1], field)
			c := claimCommand("mixed-nested-claim", "mixed-worker", 60000)
			c.Module, c.Limit = "sessions", 2
			r, e := f.h.Durable.ExecuteJob(f.ctx, f.caller, c)
			if e != nil || r.GetDecision() != v1.Decision_DECISION_REJECTED || r.GetError().GetCode() != "UNSUPPORTED_FEATURE" || len(r.GetJobs()) != 0 {
				t.Errorf("mixed unsupported batch accepted: %v %v", r, e)
			}
			for _, j := range []*v1.Job{known, changed} {
				f.assertUnchanged(t, receipts[j.Responsibility.CommandId], j)
			}
			replay, e := f.h.Durable.ExecuteJob(f.ctx, f.caller, c)
			if e != nil || !proto.Equal(replay, r) {
				t.Fatalf("mixed rejected receipt replay changed: %v %v", replay, e)
			}
			for _, j := range []*v1.Job{known, changed} {
				f.assertUnchanged(t, receipts[j.Responsibility.CommandId], j)
			}
		})
	}
}

type nestedGoalFixture struct {
	h           *assembly.Harness
	ctx         context.Context
	caller      *v1.Caller
	session     *v1.Session
	seed        *v1.CommandReceipt
	seedTask    *v1.Task
	seedCommand *v1.SubmitGoalCommand
}

// newNestedGoalFixture 先经公开提交和处理取得真实会话，注入时保持宿主打开。
func newNestedGoalFixture(t *testing.T) *nestedGoalFixture {
	t.Helper()
	h, e := assembly.Open(filepath.Join(t.TempDir(), "live.db"), "alice", "local")
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { h.Close() })
	caller, c := goal()
	c.Identity.CommandId = "seed-session"
	f := &nestedGoalFixture{h: h, ctx: context.Background(), caller: caller, seedCommand: c}
	if _, e = h.Sessions.SubmitGoal(f.ctx, caller, c); e != nil {
		t.Fatal(e)
	}
	if e = h.Sessions.ProcessPending(f.ctx, caller); e != nil {
		t.Fatal(e)
	}
	q, e := h.Durable.QueryReceipt(f.ctx, caller, c.Identity)
	if e != nil || q.GetReceipt().GetSessionRef() == nil || q.GetReceipt().GetTaskRef() == nil {
		t.Fatalf("public session producer failed: %v %v", q, e)
	}
	f.seed = q.Receipt
	f.session, e = h.Sessions.QuerySession(f.ctx, caller, f.seed.SessionRef.Name)
	if e != nil || len(f.session.GetTaskRefs()) != 1 || f.session.Revision == 0 {
		t.Fatalf("actual session missing: %v %v", f.session, e)
	}
	f.seedTask, e = h.Tasks.QueryTask(f.ctx, caller, f.seed.TaskRef.Name)
	if e != nil {
		t.Fatal(e)
	}
	return f
}

func (f *nestedGoalFixture) submit(t *testing.T, id string) (*v1.CommandReceipt, *v1.Job) {
	t.Helper()
	c := proto.Clone(f.seedCommand).(*v1.SubmitGoalCommand)
	c.Identity.CommandId = id
	c.Session = proto.Clone(f.session.SessionId).(*v1.GlobalName)
	revision := f.session.Revision
	c.ExpectedRevision = &revision
	r, e := f.h.Sessions.SubmitGoal(f.ctx, f.caller, c)
	if e != nil || r.GetPhase() != v1.ReceiptPhase_RECEIPT_PHASE_SUBMITTED {
		t.Fatalf("real goal submission: %v %v", r, e)
	}
	j, e := f.h.Durable.QueryJob(f.ctx, f.caller, r.JobRef.Name)
	if e != nil || j.GetState() != "READY" || j.ClaimEpoch != 0 || j.Attempts != 0 || j.Goal.Command.Goal != "" || !proto.Equal(j.Goal.Command.Session, f.session.SessionId) {
		t.Fatalf("real saved goal job: %v %v", j, e)
	}
	return r, j
}

// injectNestedGoalMetadata 只改原保存消息的未知格式元数据，反向恢复验证业务字段未动。
func injectNestedGoalMetadata(t *testing.T, h *assembly.Harness, original *v1.Job, field string) *v1.Job {
	t.Helper()
	changed := proto.Clone(original).(*v1.Job)
	nestedGoalMessage(changed, field).ProtoReflect().SetUnknown(protowire.AppendVarint(protowire.AppendTag(nil, 19001, protowire.VarintType), 1))
	back := proto.Clone(changed).(*v1.Job)
	nestedGoalMessage(back, field).ProtoReflect().SetUnknown(nil)
	if !proto.Equal(back, original) {
		t.Fatal("metadata fault altered original business fields")
	}
	b, e := proto.Marshal(changed)
	if e != nil {
		t.Fatal(e)
	}
	var roundtrip v1.Job
	if e = proto.Unmarshal(b, &roundtrip); e != nil || !proto.Equal(&roundtrip, changed) {
		t.Fatalf("unknown metadata did not roundtrip: %v", e)
	}
	n := original.Ref.Name
	if e = h.StorageFaultSQL("UPDATE jobs SET record=X'" + hex.EncodeToString(b) + "' WHERE user_id='" + n.UserId + "' AND domain_id='" + n.AuthorityDomainId + "' AND id='" + n.LocalId + "'"); e != nil {
		t.Fatal(e)
	}
	saved, e := h.Durable.QueryJob(context.Background(), &v1.Caller{UserId: "alice", IssuerId: "cli"}, n)
	if e != nil || !proto.Equal(saved, changed) {
		t.Fatalf("actual saved metadata fault missing: %v %v", saved, e)
	}
	return changed
}

func nestedGoalMessage(j *v1.Job, field string) proto.Message {
	switch field {
	case "identity":
		return j.Goal.Command.Identity
	case "session":
		return j.Goal.Command.Session
	case "ref":
		return j.Ref
	default:
		panic("unknown metadata fixture")
	}
}

func (f *nestedGoalFixture) assertUnchanged(t *testing.T, original *v1.CommandReceipt, job *v1.Job) {
	t.Helper()
	after, e := f.h.Durable.QueryJob(f.ctx, f.caller, job.Ref.Name)
	if e != nil || !proto.Equal(after, job) {
		t.Errorf("unsupported saved job advanced: %v %v", after, e)
	}
	q, e := f.h.Durable.QueryReceipt(f.ctx, f.caller, original.Identity)
	if e != nil || q.State != v1.ReceiptQueryState_RECEIPT_QUERY_STATE_SUBMITTED || !proto.Equal(q.Receipt, original) {
		t.Errorf("original submitted receipt changed: %v %v", q, e)
	}
	session, e := f.h.Sessions.QuerySession(f.ctx, f.caller, f.session.SessionId)
	if e != nil || !proto.Equal(session, f.session) {
		t.Errorf("original session/task scope changed: %v %v", session, e)
	}
	seed, e := f.h.Durable.QueryReceipt(f.ctx, f.caller, f.seed.Identity)
	if e != nil || !proto.Equal(seed.Receipt, f.seed) {
		t.Errorf("prior decided receipt changed: %v %v", seed, e)
	}
	task, e := f.h.Tasks.QueryTask(f.ctx, f.caller, f.seed.TaskRef.Name)
	if e != nil || !proto.Equal(task, f.seedTask) {
		t.Errorf("original task changed: %v %v", task, e)
	}
}
