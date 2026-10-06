package sessions

import (
	"context"

	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
)

type Decisions interface {
	Execute(context.Context, *v1.Caller, *v1.CommandHeader, string, string, func(context.Context) (*v1.Ref, error)) (*v1.CommandReceipt, error)
}

// CreateSession 创建独立于任务生命周期的空会话。
func (s *Service) CreateSession(ctx context.Context, caller *v1.Caller, c *v1.CreateSessionCommand) (*v1.CommandReceipt, error) {
	if e := command.ValidateHeader(c.GetHeader(), c); e != nil {
		return nil, e
	}
	return s.durable.Execute(ctx, caller, c.Header, command.SemanticFingerprint("create-session"), "sessions.input", func(tx context.Context) (*v1.Ref, error) {
		ref := command.NewRef(s.user, s.domain, "session", "lerna.v1.Session")
		return ref, s.store.SaveSession(tx, &v1.Session{SessionId: ref.Name, Status: "ACTIVE", Revision: 1})
	})
}

// SubmitInput 只按显式类别和引用投递；事务序是唯一的会话顺序。
func (s *Service) SubmitInput(ctx context.Context, caller *v1.Caller, c *v1.SubmitInputCommand) (*v1.CommandReceipt, error) {
	if e := command.ValidateHeader(c.GetHeader(), c); e != nil {
		return nil, e
	}
	return s.durable.Execute(ctx, caller, c.Header, command.SemanticFingerprint("session-input", c), "sessions.input", func(tx context.Context) (*v1.Ref, error) {
		if e := command.CheckName(caller, c.SessionId, s.user, s.domain, "session"); e != nil {
			return nil, e
		}
		session, e := s.store.LoadSession(tx, c.SessionId)
		if e != nil {
			return nil, e
		}
		if session == nil {
			return nil, command.Fail("NOT_FOUND")
		}
		if session.Status != "ACTIVE" {
			return nil, command.Fail("SESSION_INACTIVE")
		}
		if e = validateInput(c, session); e != nil {
			return nil, e
		}
		if c.InputKind != "CONTROL" || c.ContentRef != nil {
			if e = s.content.CheckUsable(tx, caller, c.ContentRef); e != nil {
				return nil, e
			}
		}
		input := &v1.SessionInput{InputId: command.NewRef(s.user, s.domain, "input", "lerna.v1.SessionInput").Name, InputKind: c.InputKind, TaskId: c.TaskId, RequestRef: c.RequestRef, ExpectedInputVersion: c.ExpectedInputVersion, ExpectedRequirementsVersion: c.ExpectedRequirementsVersion, ContentRef: c.ContentRef, CommandIdentity: c.Header.Identity, RoutingStatus: "RECORDED"}
		ready, e := s.dependencies(tx, c)
		if e != nil {
			return nil, e
		}
		if ready {
			if e = s.deliver(tx, caller, c, session, input); e != nil {
				return nil, e
			}
		} else {
			input.RoutingStatus = "WAITING_DEPENDENCY"
		}

		session.LastCommittedSeq++
		session.Revision++
		input.SessionSeq = session.LastCommittedSeq
		session.Inputs = append(session.Inputs, input)
		ref := &v1.Ref{Name: input.InputId, Revision: 1, SchemaId: "lerna.v1.SessionInput"}
		input.DependsOn = c.DependsOn
		input.ObservedSessionSeq = c.ObservedSessionSeq
		if e = s.store.SaveSession(tx, session); e != nil {
			return nil, e
		}
		return ref, s.saveInputDelivery(tx, &v1.InputDelivery{Ref: ref, OriginalCommand: c, Input: input})
	})
}

func (s *Service) deliver(tx context.Context, caller *v1.Caller, c *v1.SubmitInputCommand, session *v1.Session, input *v1.SessionInput) error {
	var e error
	switch c.InputKind {
	case "GOAL":
		if c.TaskId != nil || c.RequestRef != nil {
			return command.Fail("INVALID_INPUT")
		}
		task, e := s.tasks.CreateInTransaction(tx, c.ContentRef)
		if e != nil {
			return e
		}
		if len(c.ExplicitConditions) > 0 {
			if e = s.tasks.AcceptExplicitInTransaction(tx, caller, task, c.ExplicitConditions, c.Header.Identity, &v1.Ref{Name: input.InputId, Revision: 1, SchemaId: "lerna.v1.SessionInput"}); e != nil {
				return e
			}
		}
		if e = s.tasks.RecordGoalInTransaction(tx, caller, task, input, c.ExplicitConditions); e != nil {
			return e
		}
		input.TaskId = task.Name
		input.RoutingStatus = "TASK_ACCEPTED"
		session.TaskRefs = append(session.TaskRefs, task)
	case "CONTROL":
		if !associated(session, c.TaskId) {
			return command.Fail("INVALID_TARGET")
		}
		if e = s.tasks.ControlInTransaction(tx, caller, c); e != nil {
			return e
		}
		input.TaskId = c.TaskId
		input.RoutingStatus = "TASK_ACCEPTED"
	case "ANSWER":
		if !associated(session, c.TaskId) {
			return command.Fail("INVALID_TARGET")
		}
		if e = s.answer(tx, caller, c, input); e != nil {
			return e
		}
	case "MODIFY":
		if !associated(session, c.TaskId) {
			return command.Fail("INVALID_TARGET")
		}
		if c.RequestRef != nil {
			return command.Fail("INVALID_INPUT")
		}
		if e = s.tasks.AcceptInputInTransaction(tx, caller, c, input, true); e != nil {
			return e
		}
	case "RECORD":
		if c.TaskId != nil || c.RequestRef != nil {
			return command.Fail("INVALID_INPUT")
		}
	default:
		return command.Fail("UNSUPPORTED_FEATURE")
	}
	return nil
}

func validateInput(c *v1.SubmitInputCommand, session *v1.Session) error {
	switch c.InputKind {
	case "GOAL", "RECORD":
		if c.TaskId != nil || c.RequestRef != nil || c.ExpectedInputVersion != 0 || c.ExpectedRequirementsVersion != 0 {
			return command.Fail("INVALID_INPUT")
		}
	case "MODIFY", "ANSWER":
		if !associated(session, c.TaskId) {
			return command.Fail("INVALID_TARGET")
		}
		if c.ExpectedInputVersion == 0 || c.ExpectedRequirementsVersion == 0 {
			return command.Fail("INVALID_INPUT")
		}
		if (c.InputKind == "MODIFY" && c.RequestRef != nil) || (c.InputKind == "ANSWER" && c.RequestRef == nil) {
			return command.Fail("INVALID_INPUT")
		}
	case "CONTROL":
		if !associated(session, c.TaskId) {
			return command.Fail("INVALID_TARGET")
		}
		if c.RequestRef != nil || c.ExpectedControlGeneration == 0 || c.ExpectedInputVersion != 0 || c.ExpectedRequirementsVersion != 0 || len(c.ExplicitConditions) > 0 {
			return command.Fail("INVALID_INPUT")
		}
	default:
		return command.Fail("UNSUPPORTED_FEATURE")
	}
	if c.InputKind != "CONTROL" && (c.Control != "" || c.ExpectedControlGeneration != 0) {
		return command.Fail("INVALID_INPUT")
	}
	if c.InputKind == "RECORD" && len(c.ExplicitConditions) > 0 {
		return command.Fail("INVALID_INPUT")
	}
	if c.ObservedSessionSeq > session.LastCommittedSeq {
		return command.Fail("STALE_SESSION")
	}
	return nil
}
