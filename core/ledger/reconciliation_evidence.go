package ledger

import (
	"context"
	"crypto/rand"
	"math/big"

	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

// applyReconciliationObservation 只追加原责任的证据，不改变其尝试、发送、端点或任务。
func (s *Service) applyReconciliationObservation(ctx context.Context, caller *v1.Caller, query *v1.Operation, raw *v1.RawObservation, facts *QueryEvidence) error {
	q, e := s.store.LoadClosureQuery(ctx, query.ClosureWorkRef)
	if e != nil {
		return e
	}
	if q == nil || q.QueryOperationRef == nil || !proto.Equal(q.QueryOperationRef.Name, query.Ref.Name) || !proto.Equal(q.Work.QuerySubject, query.QuerySubject) {
		return command.Fail("INVALID_QUERY_RELATION")
	}
	p, e := s.QueryReconciliation(ctx, caller, q.Work.QuerySubject.OperationId)
	if e != nil {
		return e
	}
	if p == nil {
		return command.Fail("INVARIANT_VIOLATION")
	}
	original, e := s.QueryOperation(ctx, caller, p.OperationId)
	if e != nil {
		return e
	}
	subject := q.Work.QuerySubject
	if original == nil || original.Execution == nil || !proto.Equal(subject.AttemptId, original.Execution.Attempt.Ref.Name) || subject.ExternalKey != original.Execution.Attempt.ExternalKey || subject.TargetScope != original.CapabilitySnapshot.Resource || subject.ExecutorEndpointId != original.ExecutorEndpointId || !proto.Equal(subject.CapabilityRef, original.CapabilitySnapshot.Ref) {
		return command.Fail("INVALID_QUERY_RELATION")
	}
	if facts == nil {
		return command.Fail("INVARIANT_VIOLATION")
	}
	conflict := facts.Conflict
	finding := &v1.ReconciliationFinding{Ref: command.NewRef(s.user, s.domain, "reconciliation-finding", "lerna.v1.ReconciliationFinding"), QueryRef: proto.Clone(q.Ref).(*v1.Ref), OperationId: original.Ref.Name, ObservationRef: raw.Ref, Rule: facts.Rule, Outcome: facts.Outcome, LateEffect: facts.LateEffect, Reason: facts.Reason}
	if e = s.saveReconciliationFinding(ctx, finding, p.TaskId, q.QueryOperationRef); e != nil {
		return e
	}
	q.Ref.Revision++
	q.ObservationRef = raw.Ref
	q.InterpretationRef = finding.Ref
	q.State = "OBSERVED"
	q.Reason = finding.Reason
	if facts.RetryAfterMs != nil {
		q.RetryAfterMs = *facts.RetryAfterMs
	}
	p.LastObservationRef = raw.Ref
	previousOutcome, previousLate := original.Effect.Outcome, original.Effect.LateEffect
	manualPause := p.State == "PAUSED" && p.PauseReason == "USER_PAUSED"
	original.Ref.Revision++
	original.Effect.Ref.Revision++
	original.Effect.EvidenceRefs = append(original.Effect.EvidenceRefs, raw.Ref)
	if finding.Outcome != "UNKNOWN" {
		if original.Effect.Outcome != "UNKNOWN" && original.Effect.Outcome != finding.Outcome {
			original.Effect.EvidenceConflict = true
		}
		if !original.Effect.EvidenceConflict {
			original.Effect.Outcome = finding.Outcome
			original.Effect.LateEffect = finding.LateEffect
		}
	}
	if conflict {
		original.Effect.EvidenceConflict = true
	}
	if original.Effect.EvidenceConflict {
		original.Effect.Outcome = "UNKNOWN"
		original.Effect.LateEffect = "MAY_OCCUR"
		original.Lifecycle = "ACTIVE"
	}
	if finding.LateEffect == "RULED_OUT" && !original.Effect.EvidenceConflict {
		original.Dispatch = "SEALED"
		original.Lifecycle = "SETTLED"
		original.ClosureEvidenceRefs = append(original.ClosureEvidenceRefs, finding.Ref)
	}
	original.EffectRef = original.Effect.Ref
	if e = s.saveOperation(ctx, original); e != nil {
		return e
	}
	if query.Lifecycle != "SETTLED" {
		p.State = "PAUSED"
		p.PauseReason = "QUERY_RESULT_UNKNOWN"
	} else if original.Lifecycle == "SETTLED" && original.Effect.Outcome != "UNKNOWN" {
		p.State = "COMPLETED"
		p.ActiveQueryRef = nil
	} else {
		_, now, e := s.store.LedgerPosition(ctx)
		if e != nil {
			return e
		}
		p.State = "WAITING"
		if original.Effect.Outcome != previousOutcome || original.Effect.LateEffect != previousLate {
			p.NoProgressCount = 0
		} else {
			p.NoProgressCount++
		}
		p.ActiveQueryRef = nil
		delay, e := reconciliationDelay(p.Limits, p.NoProgressCount, q.RetryAfterMs)
		if e != nil {
			return e
		}
		p.NextReconcileAtUnixMs = now + delay
		cap, e := s.reconciliationTasks.QueryCurrentCapability(ctx, caller, p.QueryCapabilityRef)
		if e != nil {
			return e
		}
		if reason := reconciliationLimitReason(p, now, cap); reason != "" {
			p.State = "PAUSED"
			p.PauseReason = reason
		}
	}
	if finding.Reason == "QUERY_RETENTION_EXPIRED" && p.State != "COMPLETED" {
		p.State = "PAUSED"
		p.PauseReason = finding.Reason
	}
	if manualPause && p.State == "WAITING" {
		p.State = "PAUSED"
		p.PauseReason = "USER_PAUSED"
	}
	if p.State == "COMPLETED" {
		p.PauseReason = ""
	}
	jobs, e := s.store.LedgerJobs(ctx, original.Ref.Name)
	if e != nil {
		return e
	}
	for _, j := range jobs {
		switch j.JobType {
		case "EXECUTE_OPERATION":
			if original.Lifecycle != "SETTLED" {
				continue
			}
			j.State = "COMPLETED"
		case "RECONCILE_OPERATION":
			j.ReadyAtUnixMs = p.NextReconcileAtUnixMs
			switch p.State {
			case "COMPLETED":
				j.State = "COMPLETED"
			case "PAUSED":
				j.State = "BLOCKED"
			default:
				j.State = "WAITING"
			}
		default:
			continue
		}
		j.Ref.Revision++
		if j.JobType == "RECONCILE_OPERATION" {
			p.JobRef = j.Ref
		}
		j.ProcessInstance = ""
		j.LeaseUntilUnixMs = 0
		j.WaitingReason = p.PauseReason
		if e = s.store.SaveLedgerJob(ctx, j); e != nil {
			return e
		}
	}
	return s.saveQueryAndPlan(ctx, p, q)
}

// reconciliationDelay 保存抽样结果；重复查询与重启只读取既有到期时间。
func reconciliationDelay(l *v1.ReconciliationLimits, n uint32, retryAfter int64) (int64, error) {
	upper := l.InitialDelayMs
	for i := uint32(0); i < n && upper < l.MaxDelayMs; i++ {
		upper = min(l.MaxDelayMs, upper*2)
	}
	draw, e := rand.Int(rand.Reader, big.NewInt(upper+1))
	if e != nil {
		return 0, e
	}
	return max(draw.Int64(), retryAfter), nil
}

// applyOriginalReconciliationFact 迟到的原始响应独立于调度领取和暂停状态，仍然更新既有责任。
func (s *Service) applyOriginalReconciliationFact(ctx context.Context, c *v1.Caller, op *v1.Operation) error {
	p, e := s.QueryReconciliation(ctx, c, op.Ref.Name)
	if e != nil || p == nil {
		return e
	}
	if op.Lifecycle == "SETTLED" && op.Effect.Outcome != "UNKNOWN" && p.ActiveQueryRef == nil {
		p.State = "COMPLETED"
		p.PauseReason = ""
		if e = s.saveReconciliationJob(ctx, p); e != nil {
			return e
		}
	}
	p.Ref.Revision++
	return s.saveReconciliation(ctx, p)
}
