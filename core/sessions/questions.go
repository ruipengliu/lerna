package sessions

import (
	"context"

	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

type QuestionStore interface {
	SaveQuestion(context.Context, *v1.Question) error
	LoadQuestion(context.Context, *v1.Ref) (*v1.Question, error)
}

func (s *Service) PublishQuestion(ctx context.Context, caller *v1.Caller, c *v1.PublishQuestionCommand) (*v1.CommandReceipt, error) {
	if e := command.ValidateHeader(c.GetHeader(), c); e != nil {
		return nil, e
	}
	return s.durable.Execute(ctx, caller, c.Header, command.SemanticFingerprint("question", c), "sessions.input", func(tx context.Context) (*v1.Ref, error) {
		if caller.IssuerId != "host" {
			return nil, command.Fail("PERMISSION_DENIED")
		}
		if c.TaskRef == nil || c.TaskRef.SchemaId != "lerna.v1.Task" {
			return nil, command.Fail("INVALID_INPUT")
		}
		session, e := s.QuerySession(tx, caller, c.SessionId)
		if e != nil {
			return nil, e
		}
		if session == nil || session.Status != "ACTIVE" || !associated(session, c.TaskRef.Name) {
			return nil, command.Fail("INVALID_TARGET")
		}
		task, e := s.tasks.QueryTask(tx, caller, c.TaskRef.Name)
		if e != nil {
			return nil, e
		}
		if task == nil || task.Revision != c.TaskRef.Revision || task.Lifecycle != v1.TaskLifecycle_TASK_LIFECYCLE_OPEN {
			return nil, command.Fail("STALE_INPUT")
		}
		if e = s.content.CheckUsable(tx, caller, c.ContentRef); e != nil {
			return nil, e
		}
		_, now, e := s.store.Position(tx)
		if e != nil {
			return nil, e
		}
		if c.ExpiresAtUnixMs <= now {
			return nil, command.Fail("REQUEST_INVALID")
		}
		q := &v1.Question{Ref: command.NewRef(s.user, s.domain, "question", "lerna.v1.Question"), SessionId: c.SessionId, TaskId: task.TaskId, ContentRef: c.ContentRef, ChangesBasis: c.ChangesBasis, RequirementsVersion: task.RequirementsVersion, InputVersion: task.InputVersion, ControlGeneration: task.ControlGeneration, ExpiresAtUnixMs: c.ExpiresAtUnixMs, Status: "PENDING", PublishedBy: c.Header.Identity}
		return q.Ref, s.saveQuestion(tx, q)
	})
}
func (s *Service) QueryQuestion(ctx context.Context, caller *v1.Caller, r *v1.Ref) (*v1.Question, error) {
	if r == nil || r.SchemaId != "lerna.v1.Question" || r.Revision != 1 {
		return nil, command.Fail("INVALID_INPUT")
	}
	if e := command.CheckName(caller, r.Name, s.user, s.domain, "question"); e != nil {
		return nil, e
	}
	q, e := s.store.(QuestionStore).LoadQuestion(ctx, r)
	if e == nil && q != nil && !proto.Equal(q.Ref, r) {
		return nil, command.Fail("STALE_REFERENCE")
	}
	return q, e
}
func associated(s *v1.Session, id *v1.GlobalName) bool {
	for _, r := range s.TaskRefs {
		if proto.Equal(r.Name, id) {
			return true
		}
	}
	return false
}
func (s *Service) answer(ctx context.Context, caller *v1.Caller, c *v1.SubmitInputCommand, input *v1.SessionInput) error {
	q, e := s.QueryQuestion(ctx, caller, c.RequestRef)
	if e != nil {
		return e
	}
	_, now, e := s.store.Position(ctx)
	if e != nil {
		return e
	}
	if q == nil || q.Status != "PENDING" || q.ExpiresAtUnixMs <= now || !proto.Equal(q.SessionId, c.SessionId) || !proto.Equal(q.TaskId, c.TaskId) {
		return command.Fail("REQUEST_INVALID")
	}
	t, e := s.tasks.QueryTask(ctx, caller, c.TaskId)
	if e != nil {
		return e
	}
	if t == nil || t.InputVersion != q.InputVersion || t.RequirementsVersion != q.RequirementsVersion || t.ControlGeneration != q.ControlGeneration {
		return command.Fail("REQUEST_INVALID")
	}
	if !q.ChangesBasis && len(c.ExplicitConditions) > 0 {
		return command.Fail("INVALID_INPUT")
	}
	if e = s.tasks.AcceptInputInTransaction(ctx, caller, c, input, q.ChangesBasis); e != nil {
		return e
	}
	input.RequestRef = q.Ref
	q.Status = "ANSWERED"
	q.ResponseInputRef = &v1.Ref{Name: input.InputId, Revision: 1, SchemaId: "lerna.v1.SessionInput"}
	return s.saveQuestion(ctx, q)
}
