//go:build fault

package fault_test

import (
	"context"
	"encoding/hex"
	"path/filepath"
	"testing"

	"github.com/ruipengliu/lerna/cmd/assembly"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

// 规则：G3、G11、V4
func TestStoredFutureJobContractCannotBeClaimed(t *testing.T) {
	h, e := assembly.Open(filepath.Join(t.TempDir(), "claim.db"), "alice", "local")
	if e != nil {
		t.Fatal(e)
	}
	defer h.Close()
	caller, c := goal()
	r, e := h.Sessions.SubmitGoal(context.Background(), caller, c)
	if e != nil {
		t.Fatal(e)
	}
	j, e := h.Durable.QueryJob(context.Background(), caller, r.JobRef.Name)
	if e != nil {
		t.Fatal(e)
	}
	injectFutureJob(t, h, "jobs", j)
	receipt, e := h.Durable.ExecuteJob(context.Background(), caller, claimCommand("future-claim", "worker", 30000))
	if e != nil || receipt.GetDecision() != v1.Decision_DECISION_REJECTED || receipt.GetError().GetCode() != "UNSUPPORTED_CONTRACT" {
		t.Fatalf("future job accepted: %v %v", receipt, e)
	}
	after, e := h.Durable.QueryJob(context.Background(), caller, j.Ref.Name)
	if e != nil || after.State != "READY" || after.ClaimEpoch != 0 || after.ContractVersion != 2 {
		t.Fatalf("future responsibility changed: %v %v", after, e)
	}
}

// 规则：G3、G11、V4
func TestMixedKnownAndFutureClaimBatchHasNoPartialAdvance(t *testing.T) {
	h, e := assembly.Open(filepath.Join(t.TempDir(), "batch.db"), "alice", "local")
	if e != nil {
		t.Fatal(e)
	}
	defer h.Close()
	caller, c := goal()
	var jobs []*v1.Job
	for _, id := range []string{"first", "second"} {
		c = proto.Clone(c).(*v1.SubmitGoalCommand)
		c.Identity.CommandId = id
		r, e := h.Sessions.SubmitGoal(context.Background(), caller, c)
		if e != nil {
			t.Fatal(e)
		}
		j, e := h.Durable.QueryJob(context.Background(), caller, r.JobRef.Name)
		if e != nil {
			t.Fatal(e)
		}
		jobs = append(jobs, j)
	}
	pending, e := h.Durable.Pending(context.Background(), caller)
	if e != nil || len(pending) != 2 {
		t.Fatalf("pending: %v %v", pending, e)
	}
	injectFutureJob(t, h, "jobs", pending[1])
	command := claimCommand("mixed-batch", "worker", 30000)
	command.Limit = 2
	receipt, e := h.Durable.ExecuteJob(context.Background(), caller, command)
	if e != nil || receipt.GetDecision() != v1.Decision_DECISION_REJECTED || receipt.GetError().GetCode() != "UNSUPPORTED_CONTRACT" {
		t.Errorf("batch accepted: %v %v", receipt, e)
	}
	for _, j := range jobs {
		after, e := h.Durable.QueryJob(context.Background(), caller, j.Ref.Name)
		if e != nil || after.State != "READY" || after.ClaimEpoch != 0 || after.Attempts != 0 {
			t.Fatalf("partial claim changed original responsibility: %v %v", after, e)
		}
	}
}

// 规则：G3、G11、V4
func TestHeldOldClaimCannotProcessFutureStoredContract(t *testing.T) {
	h, e := assembly.Open(filepath.Join(t.TempDir(), "held.db"), "alice", "local")
	if e != nil {
		t.Fatal(e)
	}
	defer h.Close()
	caller, c := goal()
	original, e := h.Sessions.SubmitGoal(context.Background(), caller, c)
	if e != nil {
		t.Fatal(e)
	}
	receipt, e := h.Durable.ExecuteJob(context.Background(), caller, claimCommand("held", "worker", 30000))
	if e != nil || len(receipt.Jobs) != 1 {
		t.Fatalf("claim: %v %v", receipt, e)
	}
	old := proto.Clone(receipt.Jobs[0]).(*v1.Job)
	injectFutureJob(t, h, "jobs", receipt.Jobs[0])
	if e = h.Sessions.ProcessClaim(context.Background(), old); e == nil {
		t.Error("held version-1 claim interpreted stored version-2 job")
	}
	q, e := h.Durable.QueryReceipt(context.Background(), caller, c.Identity)
	if e != nil || !proto.Equal(q.Receipt, original) {
		t.Fatalf("incompatible processing changed original receipt: %v %v", q, e)
	}
	after, e := h.Durable.QueryJob(context.Background(), caller, old.Ref.Name)
	if e != nil || after.State != "CLAIMED" || after.ContractVersion != 2 || after.ClaimEpoch != old.ClaimEpoch || after.Ref.Revision != old.Ref.Revision {
		t.Fatalf("incompatible processing changed job: %v %v", after, e)
	}
}

// injectFutureJob 只在真实公开工作上注入格式版本故障，业务判据来自公开查询。
func injectFutureJob(t *testing.T, h *assembly.Harness, table string, j *v1.Job) {
	t.Helper()
	mutated := proto.Clone(j).(*v1.Job)
	mutated.ContractVersion = 2
	back := proto.Clone(mutated).(*v1.Job)
	back.ContractVersion = j.ContractVersion
	if !proto.Equal(back, j) {
		t.Fatal("metadata fixture altered other fields")
	}
	b, e := proto.Marshal(mutated)
	if e != nil {
		t.Fatal(e)
	}
	sql := "UPDATE " + table + " SET record=X'" + hex.EncodeToString(b) + "' WHERE user_id='" + j.Ref.Name.UserId + "' AND domain_id='" + j.Ref.Name.AuthorityDomainId + "' AND id='" + j.Ref.Name.LocalId + "'"
	if e = h.StorageFaultSQL(sql); e != nil {
		t.Fatal(e)
	}
}

// 规则：G3、G11、V4
func TestOpenChecksAllOwnerContractsBeforeAnyRecoveryDecision(t *testing.T) {
	path := filepath.Join(t.TempDir(), "startup.db")
	h, e := assembly.Open(path, "alice", "local")
	if e != nil {
		t.Fatal(e)
	}
	defer h.Close()
	caller, c := goal()
	original, e := h.Sessions.SubmitGoal(context.Background(), caller, c)
	if e != nil {
		t.Fatal(e)
	}
	sources, e := h.Trace.QuerySources(context.Background(), caller)
	if e != nil || len(sources) == 0 {
		t.Fatalf("real source producer: %v %v", sources, e)
	}
	source := proto.Clone(sources[0]).(*v1.TraceSourceRecord)
	source.Command.Header.ContractVersion = 2
	b, e := proto.Marshal(source)
	if e != nil {
		t.Fatal(e)
	}
	id := source.Command.Header.Identity
	statement := "UPDATE source_trace_outbox SET record=X'" + hex.EncodeToString(b) + "' WHERE user_id='alice' AND issuer='" + id.IssuerId + "' AND target_domain='" + id.TargetDomainId + "' AND id='" + id.CommandId + "'"
	if e = h.StorageFaultSQL(statement); e != nil {
		t.Fatal(e)
	}
	candidate, e := assembly.Open(path, "alice", "local")
	if candidate != nil {
		if closeErr := candidate.Close(); closeErr != nil {
			t.Fatal(closeErr)
		}
	}
	if e == nil {
		t.Error("unsupported trace-source contract accepted at startup")
	}
	after, e := h.Durable.QueryReceipt(context.Background(), caller, c.Identity)
	if e != nil || !proto.Equal(after.Receipt, original) {
		t.Fatalf("startup advanced another owner before compatibility refusal: %v %v", after, e)
	}
	job, e := h.Durable.QueryJob(context.Background(), caller, original.JobRef.Name)
	if e != nil || job.State != "READY" || job.ClaimEpoch != 0 {
		t.Fatalf("startup modified original job: %v %v", job, e)
	}
}
