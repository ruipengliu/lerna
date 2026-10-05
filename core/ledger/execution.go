package ledger

import (
	"context"

	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

type ExecutionWork interface {
	Execute(context.Context, *v1.Caller, *v1.CommandHeader, string, string, func(context.Context) (*v1.Ref, error)) (*v1.CommandReceipt, error)
	CheckExecutionClaimInTransaction(context.Context, *v1.Job) (*v1.Job, error)
	CheckExecutionClaimAt(context.Context, *v1.Job, int64) (*v1.Job, error)
}

func (s *Service) WithWork(work ExecutionWork) *Service { s.work = work; return s }

type Compiler interface {
	Compile(*v1.Operation, *v1.ExecutionAttempt) (*v1.CallDescriptor, *v1.ExecutionCapabilities, error)
}

func (s *Service) WithCompiler(adapter Compiler) *Service { s.adapter = adapter; return s }

// Prepare 固定原尝试与发送身份；任何重放都不触发外部 I/O。
func (s *Service) Prepare(ctx context.Context, caller *v1.Caller, c *v1.PrepareExecutionCommand) (*v1.CommandReceipt, error) {
	if e := command.ValidateHeader(c.GetHeader(), c); e != nil {
		return nil, e
	}
	if e := command.CheckIdentity(caller, c.Header.Identity, s.user, s.domain); e != nil {
		return nil, e
	}
	if caller.IssuerId != "host" && caller.IssuerId != "egress" {
		return nil, command.Fail("PERMISSION_DENIED")
	}
	return s.work.Execute(ctx, caller, c.Header, command.SemanticFingerprint("prepare", c.OperationId, c.ProcessInstance, c.Claim), "ledger.prepare", func(tx context.Context) (*v1.Ref, error) {
		job, e := s.work.CheckExecutionClaimInTransaction(tx, c.Claim)
		if e != nil {
			return nil, e
		}
		if !proto.Equal(job.SpecificationRef.Name, c.OperationId) || job.ProcessInstance != c.ProcessInstance {
			return nil, command.Fail("STALE_CLAIM")
		}
		if c.ProcessInstance == "" {
			return nil, command.Fail("INVALID_INPUT")
		}
		op, e := s.QueryOperation(tx, caller, c.OperationId)
		if e != nil {
			return nil, e
		}
		if op == nil {
			return nil, command.Fail("NOT_FOUND")
		}
		if op.Dispatch != "OPEN" {
			return nil, command.Fail("DISPATCH_SEALED")
		}
		if op.Execution != nil {
			// 已进入发送窗口的历史归属不可改写；只接替尚未开放的推进权。
			if op.Execution.Send.Phase == "REGISTERED" {
				op.Execution.Send.Ref.Revision++
				op.Execution.Send.ProcessInstance = job.ProcessInstance
				op.Execution.Send.ClaimEpoch = job.ClaimEpoch
				op.Execution.Send.LeaseUntilUnixMs = job.LeaseUntilUnixMs
				op.Ref.Revision++
				return op.Execution.Attempt.Ref, s.store.SaveOperation(tx, op)
			}
			return op.Execution.Attempt.Ref, nil
		}
		cap := op.CapabilitySnapshot
		if cap.Action != "CREATE" && cap.Action != "MODEL_INFER" && (cap.Action != "QUERY" || op.QuerySubject == nil || op.ClosureWorkRef == nil) {
			return nil, command.Fail("UNSUPPORTED_FEATURE")
		}
		attempt := &v1.ExecutionAttempt{Ref: command.NewRef(s.user, s.domain, "attempt", "lerna.v1.ExecutionAttempt"), OperationId: c.OperationId, AttemptNo: 1, Phase: "REGISTERED", ExternalKeyScope: cap.Resource}
		attempt.ExternalKey = attempt.Ref.Name.LocalId
		if s.adapter == nil {
			return nil, command.Fail("UNSUPPORTED_CAPABILITY")
		}
		descriptor, declaration, e := s.adapter.Compile(op, attempt)
		if e != nil {
			return nil, e
		}
		attempt.Capabilities = declaration
		send := &v1.PhysicalSend{Ref: command.NewRef(s.user, s.domain, "send", "lerna.v1.PhysicalSend"), AttemptId: attempt.Ref.Name, SendSeq: 1, Phase: "REGISTERED", ProcessInstance: job.ProcessInstance, ClaimEpoch: job.ClaimEpoch, LeaseUntilUnixMs: job.LeaseUntilUnixMs}
		op.Execution = &v1.Execution{Attempt: attempt, Send: send, CallDescriptor: descriptor}
		op.AttemptRefs = []*v1.Ref{attempt.Ref}
		op.Ref.Revision++
		op.Lifecycle = "ACTIVE"
		return attempt.Ref, s.store.SaveOperation(tx, op)
	})
}
func (s *Service) QueryExecution(ctx context.Context, c *v1.Caller, id *v1.GlobalName) (*v1.Execution, error) {
	op, e := s.QueryOperation(ctx, c, id)
	if e != nil || op == nil {
		return nil, e
	}
	return op.Execution, nil
}

// ValidateStart 从本域原记录核验调用描述和领取，不能信任请求自报的执行归属。
func (s *Service) ValidateStart(ctx context.Context, c *v1.Caller, b *v1.ExitCredentialBinding, d *v1.CallDescriptor, j *v1.Job, now int64) (*v1.Ref, error) {
	if b == nil || d == nil {
		return nil, command.Fail("CREDENTIAL_BINDING_MISMATCH")
	}
	job, e := s.work.CheckExecutionClaimAt(ctx, j, now)
	if e != nil {
		return nil, e
	}
	op, e := s.QueryOperation(ctx, c, b.OperationId)
	if e != nil {
		return nil, e
	}
	if op == nil || op.Execution == nil || op.Dispatch != "OPEN" {
		return nil, command.Fail("DISPATCH_SEALED")
	}
	x := op.Execution
	if !proto.Equal(job.SpecificationRef.Name, b.OperationId) || x.Send.Phase != "REGISTERED" || !proto.Equal(x.Attempt.Ref.Name, b.AttemptId) || x.Send.SendSeq != b.SendSeq || !proto.Equal(x.CallDescriptor, d) || x.CallDescriptor.Digest != b.DescriptorDigest || op.ExecutorEndpointId != b.ExecutorEndpointId || !proto.Equal(op.AdmissionRef, b.AdmissionRef) || x.Send.ProcessInstance != b.ExecutorInstance || job.ProcessInstance != b.ExecutorInstance || job.ClaimEpoch != x.Send.ClaimEpoch {
		return nil, command.Fail("CREDENTIAL_BINDING_MISMATCH")
	}
	return x.Send.Ref, nil
}

// CheckRecoveryAllowed 只从已核验的本地账本读取恢复状态。
func (s *Service) CheckRecoveryAllowed(ctx context.Context) error {
	return s.store.(interface{ CheckRecoveryAllowed(context.Context) error }).CheckRecoveryAllowed(ctx)
}
