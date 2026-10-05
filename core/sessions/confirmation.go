package sessions

import (
	"context"

	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

type ConfirmationStore interface {
	SaveConfirmation(context.Context, *v1.Confirmation) error
	LoadConfirmation(context.Context, *v1.Ref) (*v1.Confirmation, error)
	LoadCurrentConfirmation(context.Context, *v1.GlobalName) (*v1.Confirmation, error)
}
type ConfirmationDecisions interface {
	Execute(context.Context, *v1.Caller, *v1.CommandHeader, string, string, func(context.Context) (*v1.Ref, error)) (*v1.CommandReceipt, error)
}
type ConfirmationFacts interface {
	CheckConfirmationMatter(context.Context, *v1.Confirmation) error
}

func (s *Service) WithConfirmations(store ConfirmationStore, d ConfirmationDecisions, t, g ConfirmationFacts) *Service {
	s.confirmationStore = store
	s.confirmationDecisions = d
	s.operationFacts = t
	s.grantFacts = g
	return s
}
func (s *Service) checkConfirmation(ctx context.Context, c *v1.Confirmation) error {
	_, now, e := s.store.Position(ctx)
	if e != nil {
		return e
	}
	if now >= c.ExpiresAtUnixMs {
		return command.Fail("CONFIRMATION_EXPIRED")
	}
	if c.MatterType == "OPERATION_ADMISSION" {
		if e = s.operationFacts.CheckConfirmationMatter(ctx, c); e != nil {
			return e
		}
	} else if c.MatterType != "GRANT_ISSUANCE" {
		return command.Fail("CONFIRMATION_INVALID")
	}
	if e = s.grantFacts.CheckConfirmationMatter(ctx, c); e != nil {
		return e
	}
	if c.SessionId != nil {
		if command.CheckName(&v1.Caller{UserId: s.user, IssuerId: "host"}, c.SessionId, s.user, s.domain, "session") != nil {
			return command.Fail("CONFIRMATION_INVALID")
		}
		session, e := s.store.LoadSession(ctx, c.SessionId)
		if e != nil {
			return e
		}
		if session == nil || session.Status != "ACTIVE" {
			return command.Fail("CONFIRMATION_INVALID")
		}
		if m := c.GetOperationAdmission(); m != nil {
			found := false
			for _, r := range session.TaskRefs {
				if proto.Equal(r.Name, m.TaskId) {
					found = true
				}
			}
			if !found {
				return command.Fail("CONFIRMATION_INVALID")
			}
		}
	}
	return nil
}

// CreateConfirmationInTransaction 只接受核心固定的事项；呈现本身不产生回应。
func (s *Service) CreateConfirmationInTransaction(ctx context.Context, c *v1.Confirmation) (*v1.Confirmation, error) {
	if c == nil || c.Ref != nil || c.Description == "" || c.State != "" || c.RespondedBy != nil || c.ConsumedBy != nil {
		return nil, command.Fail("CONFIRMATION_INVALID")
	}
	if e := s.checkConfirmation(ctx, c); e != nil {
		return nil, e
	}
	c = proto.Clone(c).(*v1.Confirmation)
	c.Ref = command.NewRef(s.user, s.domain, "confirmation", "lerna.v1.Confirmation")
	c.State = "PENDING"
	c.BindingDigest = command.ConfirmationDigest(c)
	return c, s.confirmationStore.SaveConfirmation(ctx, c)
}
func (s *Service) QueryConfirmation(ctx context.Context, caller *v1.Caller, r *v1.Ref) (*v1.Confirmation, error) {
	if r == nil {
		return nil, command.Fail("INVALID_INPUT")
	}
	if e := command.CheckName(caller, r.Name, s.user, s.domain, "confirmation"); e != nil {
		return nil, e
	}
	c, e := s.confirmationStore.LoadConfirmation(ctx, r)
	if e == nil && c != nil && !proto.Equal(c.Ref, r) {
		return nil, command.Fail("STALE_REFERENCE")
	}
	return c, e
}
func (s *Service) QueryCurrentConfirmation(ctx context.Context, caller *v1.Caller, n *v1.GlobalName) (*v1.Confirmation, error) {
	if e := command.CheckName(caller, n, s.user, s.domain, "confirmation"); e != nil {
		return nil, e
	}
	return s.confirmationStore.LoadCurrentConfirmation(ctx, n)
}
func (s *Service) RespondConfirmation(ctx context.Context, caller *v1.Caller, c *v1.RespondConfirmationCommand) (*v1.CommandReceipt, error) {
	if e := command.ValidateHeader(c.GetHeader(), c); e != nil {
		return nil, e
	}
	return s.confirmationDecisions.Execute(ctx, caller, c.Header, command.SemanticFingerprint("respond-confirmation", c.ConfirmationRef, c.BindingDigest, c.Decision), "sessions.confirmation", func(tx context.Context) (*v1.Ref, error) {
		if caller.IssuerId != "host" && caller.IssuerId != "local-cli" {
			return nil, command.Fail("PERMISSION_DENIED")
		}
		if c.ConfirmationRef == nil {
			return nil, command.Fail("CONFIRMATION_INVALID")
		}
		v, e := s.QueryCurrentConfirmation(tx, caller, c.ConfirmationRef.Name)
		if e != nil {
			return nil, e
		}
		if v == nil || !proto.Equal(v.Ref, c.ConfirmationRef) || v.State != "PENDING" || v.BindingDigest != c.BindingDigest {
			return nil, command.Fail("CONFIRMATION_INVALID")
		}
		if e = s.checkConfirmation(tx, v); e != nil {
			return nil, e
		}
		switch c.Decision {
		case "APPROVE":
			v.State = "APPROVED"
		case "REJECT":
			v.State = "REJECTED"
		default:
			return nil, command.Fail("INVALID_INPUT")
		}
		v.RespondedBy = c.Header.Identity
		v.Ref.Revision++
		if v.SessionId != nil {
			session, e := s.store.LoadSession(tx, v.SessionId)
			if e != nil {
				return nil, e
			}
			session.LastCommittedSeq++
			session.Revision++
			var task *v1.GlobalName
			if m := v.GetOperationAdmission(); m != nil {
				task = m.TaskId
			}
			session.Inputs = append(session.Inputs, &v1.SessionInput{InputId: command.NewRef(s.user, s.domain, "input", "lerna.v1.SessionInput").Name, SessionSeq: session.LastCommittedSeq, TaskId: task, InputKind: "CONFIRMATION", ConfirmationRef: proto.Clone(v.Ref).(*v1.Ref), CommandIdentity: c.Header.Identity, RoutingStatus: "RECORDED"})
			if e = s.store.SaveSession(tx, session); e != nil {
				return nil, e
			}
			input := session.Inputs[len(session.Inputs)-1]
			inputRef := &v1.Ref{Name: input.InputId, Revision: 1, SchemaId: "lerna.v1.SessionInput"}
			if e = s.store.(DeliveryStore).SaveInputDelivery(tx, &v1.InputDelivery{Ref: inputRef, Input: input}); e != nil {
				return nil, e
			}
		}
		return v.Ref, s.confirmationStore.SaveConfirmation(tx, v)
	})
}

// ConsumeAdmissionConfirmation 与授权占用、预算预留及准入同事务消费。
func (s *Service) CheckAdmissionConfirmation(ctx context.Context, r *v1.Ref, a *v1.Admission, required bool) error {
	actor := &v1.Caller{UserId: s.user, IssuerId: "host"}
	h := &v1.CommandHeader{Identity: &v1.CommandIdentity{UserId: s.user, IssuerId: actor.IssuerId, TargetDomainId: s.domain, CommandId: "check-closure-confirmation:" + command.NewRef(s.user, s.domain, "command", "command").Name.LocalId}, ContractVersion: 1, FingerprintVersion: 1, SchemaId: "lerna.v1.AdmissionCommands"}
	receipt, e := s.confirmationDecisions.Execute(ctx, actor, h, command.SemanticFingerprint("check-admission-confirmation", r, command.OperationMatter(a), required), "sessions.confirmation", func(tx context.Context) (*v1.Ref, error) {
		_, e := s.admissionConfirmation(tx, r, a, required)
		if e != nil {
			return nil, e
		}
		return a.Origin, nil
	})
	if e != nil {
		return e
	}
	if receipt.Error != nil {
		return &command.Failure{Detail: receipt.Error}
	}
	return nil
}
func (s *Service) ConsumeAdmissionConfirmation(ctx context.Context, r *v1.Ref, a *v1.Admission, required bool) error {
	c, e := s.admissionConfirmation(ctx, r, a, required)
	if e != nil || c == nil {
		return e
	}
	return s.consumeConfirmation(ctx, c, "OPERATION_ADMISSION", a.Ref)
}
func (s *Service) admissionConfirmation(ctx context.Context, r *v1.Ref, a *v1.Admission, required bool) (*v1.Confirmation, error) {
	if r == nil {
		if required {
			return nil, command.Fail("CONFIRMATION_INVALID")
		}
		return nil, nil
	}
	c, e := s.QueryCurrentConfirmation(ctx, &v1.Caller{UserId: s.user, IssuerId: "host"}, r.Name)
	if e != nil {
		return nil, e
	}
	if c == nil || !proto.Equal(c.Ref, r) || c.State != "APPROVED" || c.MatterType != "OPERATION_ADMISSION" || !proto.Equal(c.GetOperationAdmission(), command.OperationMatter(a)) {
		return nil, command.Fail("CONFIRMATION_INVALID")
	}
	if e = s.checkConfirmation(ctx, c); e != nil {
		return nil, e
	}
	return c, nil
}

// ConsumeGrantConfirmationInTransaction 与动作确认共用唯一的消费状态转换。
func (s *Service) ConsumeGrantConfirmationInTransaction(ctx context.Context, r *v1.Ref, m *v1.GrantConfirmationMatter, target *v1.Ref) error {
	if r == nil {
		return command.Fail("CONFIRMATION_INVALID")
	}
	c, e := s.QueryCurrentConfirmation(ctx, &v1.Caller{UserId: s.user, IssuerId: "host"}, r.Name)
	if e != nil {
		return e
	}
	if c == nil || !proto.Equal(c.Ref, r) || c.State != "APPROVED" || c.MatterType != "GRANT_ISSUANCE" || !proto.Equal(c.GetGrantIssuance(), m) {
		return command.Fail("CONFIRMATION_INVALID")
	}
	if e = s.checkConfirmation(ctx, c); e != nil {
		return e
	}
	return s.consumeConfirmation(ctx, c, "GRANT_ISSUANCE", target)
}
func (s *Service) consumeConfirmation(ctx context.Context, c *v1.Confirmation, kind string, target *v1.Ref) error {
	if c.State != "APPROVED" || c.ConsumedBy != nil {
		return command.Fail("CONFIRMATION_INVALID")
	}
	switch kind {
	case "OPERATION_ADMISSION":
		c.ConsumedBy = &v1.Confirmation_ConsumedAdmissionRef{ConsumedAdmissionRef: target}
	case "GRANT_ISSUANCE":
		c.ConsumedBy = &v1.Confirmation_ConsumedGrantIssuanceRef{ConsumedGrantIssuanceRef: target}
	default:
		return command.Fail("CONFIRMATION_INVALID")
	}
	c.State = "CONSUMED"
	c.Ref.Revision++
	return s.confirmationStore.SaveConfirmation(ctx, c)
}

// WithdrawConfirmation 撤回未消费的批准，不恢复任何已消费资格。
func (s *Service) WithdrawConfirmation(ctx context.Context, caller *v1.Caller, c *v1.WithdrawConfirmationCommand) (*v1.CommandReceipt, error) {
	if e := command.ValidateHeader(c.GetHeader(), c); e != nil {
		return nil, e
	}
	return s.confirmationDecisions.Execute(ctx, caller, c.Header, command.SemanticFingerprint("withdraw-confirmation", c.ConfirmationRef), "sessions.confirmation", func(tx context.Context) (*v1.Ref, error) {
		if caller.IssuerId != "host" && caller.IssuerId != "local-cli" {
			return nil, command.Fail("PERMISSION_DENIED")
		}
		if c.ConfirmationRef == nil {
			return nil, command.Fail("CONFIRMATION_INVALID")
		}
		v, e := s.QueryCurrentConfirmation(tx, caller, c.ConfirmationRef.Name)
		if e != nil {
			return nil, e
		}
		if v == nil || !proto.Equal(v.Ref, c.ConfirmationRef) || v.State != "APPROVED" {
			return nil, command.Fail("CONFIRMATION_INVALID")
		}
		v.State = "WITHDRAWN"
		v.WithdrawnBy = c.Header.Identity
		v.Ref.Revision++
		if v.SessionId != nil {
			session, e := s.store.LoadSession(tx, v.SessionId)
			if e != nil {
				return nil, e
			}
			if session == nil {
				return nil, command.Fail("CONFIRMATION_INVALID")
			}
			session.LastCommittedSeq++
			session.Revision++
			var task *v1.GlobalName
			if m := v.GetOperationAdmission(); m != nil {
				task = m.TaskId
			}
			session.Inputs = append(session.Inputs, &v1.SessionInput{InputId: command.NewRef(s.user, s.domain, "input", "lerna.v1.SessionInput").Name, SessionSeq: session.LastCommittedSeq, TaskId: task, InputKind: "CONFIRMATION", ConfirmationRef: proto.Clone(v.Ref).(*v1.Ref), CommandIdentity: c.Header.Identity, RoutingStatus: "RECORDED"})
			if e = s.store.SaveSession(tx, session); e != nil {
				return nil, e
			}
			input := session.Inputs[len(session.Inputs)-1]
			inputRef := &v1.Ref{Name: input.InputId, Revision: 1, SchemaId: "lerna.v1.SessionInput"}
			if e = s.store.(DeliveryStore).SaveInputDelivery(tx, &v1.InputDelivery{Ref: inputRef, Input: input}); e != nil {
				return nil, e
			}
		}
		return v.Ref, s.confirmationStore.SaveConfirmation(tx, v)
	})
}
