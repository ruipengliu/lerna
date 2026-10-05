package content

import (
	"context"

	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

type ObservationWork interface {
	QueryReceipt(context.Context, *v1.Caller, *v1.CommandIdentity) (*v1.ReceiptQuery, error)
	Execute(context.Context, *v1.Caller, *v1.CommandHeader, string, string, func(context.Context) (*v1.Ref, error)) (*v1.CommandReceipt, error)
}
type ObservationLedger interface {
	QueryOperation(context.Context, *v1.Caller, *v1.GlobalName) (*v1.Operation, error)
	QueryExecution(context.Context, *v1.Caller, *v1.GlobalName) (*v1.Execution, error)
	AcceptObservation(context.Context, *v1.Caller, *v1.AcceptObservationCommand) (*v1.CommandReceipt, error)
	QueryReceipt(context.Context, *v1.Caller, *v1.CommandIdentity) (*v1.ReceiptQuery, error)
}
type observationStore interface {
	SaveObservationHandoff(context.Context, *v1.ObservationHandoff) error
	LoadObservationHandoff(context.Context, *v1.Ref) (*v1.ObservationHandoff, error)
	PendingObservations(context.Context) ([]*v1.ObservationHandoff, error)
}

func (s *Service) WithObservations(w ObservationWork, l ObservationLedger) *Service {
	s.work = w
	s.ledger = l
	return s
}
func (s *Service) RegisterObservation(ctx context.Context, caller *v1.Caller, c *v1.RegisterObservationCommand) (*v1.CommandReceipt, error) {
	if e := command.ValidateHeader(c.GetHeader(), c); e != nil {
		return nil, e
	}
	if caller.GetIssuerId() != "egress-io" {
		return nil, command.Fail("PERMISSION_DENIED")
	}
	return s.work.Execute(ctx, caller, c.Header, command.SemanticFingerprint("register-observation", c.Observation, append([]byte{}, c.Body...)), "content.observation", func(tx context.Context) (*v1.Ref, error) {
		o := c.Observation
		if o == nil || o.Ref == nil || o.Ref.Name == nil || o.UserId != s.user || o.Ref.Name.UserId != s.user || o.Ref.Revision != 1 || o.Ref.SchemaId != "lerna.v1.RawObservation" || o.Source != "TRUSTED_IO" || o.BodyRef != nil || o.OperationId == nil {
			return nil, command.Fail("INVALID_OBSERVATION")
		}
		x, e := s.ledger.QueryExecution(tx, caller, o.OperationId)
		if e != nil {
			return nil, e
		}
		if x == nil || !proto.Equal(x.Send.ObservationRef, o.Ref) || !proto.Equal(x.Send.Ref, o.SendRef) || !proto.Equal(x.Attempt.Ref.Name, o.AttemptId) || x.Send.SendSeq != o.SendSeq || x.Send.Phase != "DISPATCH_POSSIBLE" || x.Attempt.ExternalKey != o.ExternalKey || x.CallDescriptor.Target != o.Target || c.Header.Identity.CommandId != "observe:"+o.Ref.Name.LocalId {
			return nil, command.Fail("INVALID_OBSERVATION")
		}
		if e = s.checkAssociation(tx, caller, o.TaskId, o.OperationId, o.AttemptId); e != nil {
			return nil, e
		}
		o = proto.Clone(o).(*v1.RawObservation)
		body := &v1.Content{Ref: command.NewRef(s.user, s.domain, "content", "lerna.v1.Content"), Source: c.Header.Identity, MediaType: "application/octet-stream", ProcessingPurposes: []string{"CURRENT_TASK", "EFFECT_EVIDENCE"}, ContentVersion: 1, Kind: "RAW_OBSERVATION", TaskId: o.TaskId, OperationId: o.OperationId, AttemptId: o.AttemptId, SourceDescriptor: &v1.ContentSourceDescriptor{Kind: "TRUSTED_IO", Locator: o.Target, AcquisitionMethod: "EGRESS", ProviderVersion: "egress-v1", SourceTimeUnixMs: o.FinishedAtUnixMs, ObservationRef: o.Ref}}
		body.ContentId = body.Ref.Name.LocalId
		o.BodyRef = body.Ref
		h := &v1.ObservationHandoff{Observation: o, Command: &v1.AcceptObservationCommand{Header: observationHeader(s.user, "content-observation", o.OperationId.AuthorityDomainId, "observe:"+o.Ref.Name.LocalId), Observation: o}}
		store := s.store.(observationStore)
		if e = s.prepareRegistration(tx, body, c.Body, c.Header.Identity); e != nil {
			return nil, e
		}
		return o.Ref, store.SaveObservationHandoff(tx, h)
	})
}
func observationHeader(user, issuer, domain, id string) *v1.CommandHeader {
	return &v1.CommandHeader{Identity: &v1.CommandIdentity{UserId: user, IssuerId: issuer, TargetDomainId: domain, CommandId: id}, ContractVersion: 1, FingerprintVersion: 1, SchemaId: "lerna.v1.AdmissionCommands"}
}
func (s *Service) QueryObservation(ctx context.Context, caller *v1.Caller, r *v1.Ref) (*v1.RawObservation, error) {
	if r == nil || r.Name == nil || r.Name.UserId != s.user || caller.GetUserId() != s.user || r.Name.ObjectKind != "observation" || r.Revision != 1 || r.SchemaId != "lerna.v1.RawObservation" {
		return nil, command.Fail("INVALID_REFERENCE")
	}
	h, e := s.store.(observationStore).LoadObservationHandoff(ctx, r)
	if e != nil || h == nil {
		return nil, e
	}
	if !proto.Equal(h.Observation.Ref, r) {
		return nil, command.Fail("INVALID_REFERENCE")
	}
	return h.Observation, nil
}

// ProcessObservations 从内容域保存的原命令恢复，只重发证据，不重新执行外部动作。
func (s *Service) ProcessObservations(ctx context.Context, caller *v1.Caller) error {
	if caller.GetUserId() != s.user {
		return command.Fail("PERMISSION_DENIED")
	}
	if e := s.ProcessRegistrations(ctx, caller); e != nil {
		return e
	}
	all, e := s.store.(observationStore).PendingObservations(ctx)
	if e != nil {
		return e
	}
	actor := &v1.Caller{UserId: s.user, IssuerId: "content-observation"}
	for _, h := range all {
		q, e := s.ledger.QueryReceipt(ctx, actor, h.Command.Header.Identity)
		if e != nil {
			return e
		}
		var r *v1.CommandReceipt
		switch q.State {
		case v1.ReceiptQueryState_RECEIPT_QUERY_STATE_DECIDED:
			r = q.Receipt
		case v1.ReceiptQueryState_RECEIPT_QUERY_STATE_NOT_FOUND:
			r, e = s.ledger.AcceptObservation(ctx, actor, h.Command)
		default:
			return command.Fail("DEPENDENCY_UNAVAILABLE")
		}
		if e != nil {
			return e
		}
		if r.Decision != v1.Decision_DECISION_ACCEPTED {
			return command.Fail("OBSERVATION_REJECTED")
		}
		h.RecipientReceipt = r
		ack := observationHeader(s.user, "content-observation", s.domain, "ack:"+h.Observation.Ref.Name.LocalId)
		_, e = s.work.Execute(ctx, actor, ack, command.SemanticFingerprint("observation-ack", h.Observation.Ref, r), "content.observation_ack", func(tx context.Context) (*v1.Ref, error) {
			return h.Observation.Ref, s.store.(observationStore).SaveObservationHandoff(tx, h)
		})
		if e != nil {
			return e
		}
	}
	return nil
}
