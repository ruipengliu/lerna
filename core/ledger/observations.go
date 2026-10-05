package ledger

import (
	"context"

	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

type ObservationContent interface {
	Read(context.Context, *v1.Caller, *v1.Ref) (*v1.Content, error)
	QueryObservation(context.Context, *v1.Caller, *v1.Ref) (*v1.RawObservation, error)
}
type observationStore interface {
	SaveLedgerObservation(context.Context, *v1.RawObservation) error
	LoadLedgerObservation(context.Context, *v1.Ref) (*v1.RawObservation, error)
}

func (s *Service) WithObservations(c ObservationContent) *Service { s.observations = c; return s }

// AcceptObservation 接收内容域已提交的原证据；晚到证据不受推进租约约束。
func (s *Service) AcceptObservation(ctx context.Context, caller *v1.Caller, c *v1.AcceptObservationCommand) (*v1.CommandReceipt, error) {
	if e := command.ValidateHeader(c.GetHeader(), c); e != nil {
		return nil, e
	}
	if caller.GetIssuerId() != "content-observation" {
		return nil, command.Fail("PERMISSION_DENIED")
	}
	return s.work.Execute(ctx, caller, c.Header, command.SemanticFingerprint("accept-observation", c.Observation), "ledger.observation", func(tx context.Context) (*v1.Ref, error) {
		o := c.Observation
		if o == nil || o.Ref == nil || o.Ref.Name == nil || o.OperationId == nil || o.BodyRef == nil {
			return nil, command.Fail("INVALID_OBSERVATION")
		}
		authoritative, e := s.observations.QueryObservation(tx, caller, o.Ref)
		if e != nil {
			return nil, e
		}
		if authoritative == nil || !proto.Equal(authoritative, o) {
			return nil, command.Fail("INVALID_OBSERVATION")
		}
		op, e := s.QueryOperation(tx, caller, o.OperationId)
		if e != nil {
			return nil, e
		}
		if op == nil || op.Execution == nil {
			return nil, command.Fail("INVALID_OBSERVATION")
		}
		send := executionSend(op.Execution, o.SendRef)
		if send == nil || !proto.Equal(send.ObservationRef, o.Ref) || !proto.Equal(send.Ref, o.SendRef) || send.Phase != "DISPATCH_POSSIBLE" || !proto.Equal(send.AttemptId, o.AttemptId) || c.Header.Identity.CommandId != "observe:"+o.Ref.Name.LocalId {
			return nil, command.Fail("INVALID_OBSERVATION")
		}
		if e = s.store.(observationStore).SaveLedgerObservation(tx, o); e != nil {
			return nil, e
		}
		x := op.Execution
		send.Ref.Revision++
		send.Phase = "OBSERVED"
		x.Attempt.Ref.Revision++
		x.Attempt.Phase = "OBSERVED"
		op.Ref.Revision++
		op.AttemptRefs = []*v1.Ref{x.Attempt.Ref}
		op.Effect.Ref.Revision++
		op.Effect.EvidenceRefs = append(op.Effect.EvidenceRefs, o.Ref)
		op.Effect.CoveredAttemptIds = []*v1.GlobalName{x.Attempt.Ref.Name}
		op.EffectRef = op.Effect.Ref
		if e = s.createReports(tx, o, op); e != nil {
			return nil, e
		}
		return o.Ref, s.store.SaveOperation(tx, op)
	})
}
func (s *Service) QueryObservation(ctx context.Context, caller *v1.Caller, r *v1.Ref) (*v1.RawObservation, error) {
	if r == nil || r.Revision != 1 || r.SchemaId != "lerna.v1.RawObservation" {
		return nil, command.Fail("INVALID_REFERENCE")
	}
	if e := command.CheckName(caller, r.Name, s.user, s.domain, "observation"); e != nil {
		return nil, e
	}
	v, e := s.store.(observationStore).LoadLedgerObservation(ctx, r)
	if v != nil && !proto.Equal(v.Ref, r) {
		return nil, command.Fail("INVALID_REFERENCE")
	}
	return v, e
}
