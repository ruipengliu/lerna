package ledger

import (
	"context"
	"errors"

	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

// sendClosureFacts 只携带原发送历史的证明材料，各流程保存自己的 typed seal。
type sendClosureFacts struct {
	operationRef            *v1.Ref
	closedSendRefs          []*v1.Ref
	noSendProven            bool
	physicalSendWasPossible bool
}

// closeOperationSends 在原封闭事务与出口临界区内共用记录算法，不保存 seal 或处理费用。
func (s *Service) closeOperationSends(ctx context.Context, op *v1.Operation, seal *v1.Ref) (*sendClosureFacts, error) {
	facts := &sendClosureFacts{noSendProven: true}
	if op == nil {
		return facts, nil
	}
	if x := op.Execution; x != nil {
		// 部分原历史是内部记录缺口，整笔回滚，不固定业务拒绝以免阻断同身份重试。
		if x.Attempt == nil || x.Attempt.Ref == nil || x.Attempt.Ref.Name == nil {
			return nil, errors.New("incomplete execution history")
		}
		sends := append([]*v1.PhysicalSend{x.Send}, x.PreviousSends...)
		for _, send := range sends {
			if send == nil || send.Ref == nil || send.Ref.Name == nil {
				return nil, errors.New("incomplete execution history")
			}
		}
		facts.physicalSendWasPossible = executionMayHaveSent(x)
		facts.noSendProven = !facts.physicalSendWasPossible
		for _, send := range sends {
			if send.Phase == "REGISTERED" {
				send.Ref.Revision++
				send.Phase = "CLOSED"
			}
			if send.Phase == "CLOSED" {
				facts.closedSendRefs = append(facts.closedSendRefs, proto.Clone(send.Ref).(*v1.Ref))
			}
		}
	} else if len(op.AttemptRefs) > 0 {
		// 有尝试引用却无完整执行记录时，不具备内部未发送证明。
		facts.noSendProven, facts.physicalSendWasPossible = false, true
	}
	op.Ref.Revision++
	facts.operationRef = proto.Clone(op.Ref).(*v1.Ref)
	op.Dispatch = "SEALED"
	op.ClosureEvidenceRefs = append(op.ClosureEvidenceRefs, seal)
	if facts.noSendProven {
		applyNoSendClosure(op)
	}
	jobs, e := s.store.LedgerJobs(ctx, op.Ref.Name)
	if e != nil {
		return nil, e
	}
	for _, j := range jobs {
		if j.JobType != "EXECUTE_OPERATION" {
			continue
		}
		j.Ref.Revision++
		j.ProcessInstance = ""
		j.LeaseUntilUnixMs = 0
		j.State = "WAITING"
		if op.Lifecycle == "SETTLED" {
			j.State = "COMPLETED"
		}
		if e = s.store.SaveLedgerJob(ctx, j); e != nil {
			return nil, e
		}
	}
	return facts, nil
}

func applyNoSendClosure(op *v1.Operation) {
	op.Dispatch = "SEALED"
	op.Lifecycle = "SETTLED"
	op.Effect.Ref.Revision++
	op.Effect.Outcome = "NOT_APPLIED"
	op.Effect.LateEffect = "RULED_OUT"
	op.EffectRef = op.Effect.Ref
	if op.Execution != nil {
		if op.Execution.Send.Phase != "CLOSED" {
			op.Execution.Send.Ref.Revision++
			op.Execution.Send.Phase = "CLOSED"
		}
		op.Execution.Attempt.Ref.Revision++
		op.Execution.Attempt.Phase = "CLOSED"
		op.AttemptRefs = []*v1.Ref{op.Execution.Attempt.Ref}
	}
}
