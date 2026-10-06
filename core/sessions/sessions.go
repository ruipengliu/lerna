// Package sessions 接纳输入并维护会话顺序，不直接修改任务事实。
package sessions

import (
	"context"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
)

type Store interface {
	TraceSource
	SaveSession(context.Context, *v1.Session) error
	LoadSession(context.Context, *v1.GlobalName) (*v1.Session, error)
	Position(context.Context) (uint64, int64, error)
}
type Durable interface {
	Decisions
	Pending(context.Context, *v1.Caller) ([]*v1.Job, error)
	Submit(context.Context, *v1.Caller, *v1.SubmitGoalCommand, *v1.Ref) (*v1.CommandReceipt, error)
	ExecuteJob(context.Context, *v1.Caller, *v1.JobCommand) (*v1.CommandReceipt, error)
	Decide(context.Context, *v1.Job, func(context.Context, *v1.PendingGoal) (*v1.Ref, *v1.Ref, error)) error
}
type Tasks interface {
	RecordGoalInTransaction(context.Context, *v1.Caller, *v1.Ref, *v1.SessionInput, []*v1.Requirement) error
	ControlInTransaction(context.Context, *v1.Caller, *v1.SubmitInputCommand) error
	AcceptExplicitInTransaction(context.Context, *v1.Caller, *v1.Ref, []*v1.Requirement, *v1.CommandIdentity, *v1.Ref) error
	QueryTask(context.Context, *v1.Caller, *v1.GlobalName) (*v1.Task, error)
	AcceptInputInTransaction(context.Context, *v1.Caller, *v1.SubmitInputCommand, *v1.SessionInput, bool) error
	CreateInTransaction(context.Context, *v1.Ref) (*v1.Ref, error)
}
type Content interface {
	CheckUsable(context.Context, *v1.Caller, *v1.Ref) error
	Stage(context.Context, *v1.Caller, *v1.SubmitGoalCommand) (*v1.Ref, error)
}
type Service struct {
	confirmationStore     ConfirmationStore
	confirmationDecisions ConfirmationDecisions
	operationFacts        ConfirmationFacts
	grantFacts            ConfirmationFacts
	store                 Store
	durable               Durable
	tasks                 Tasks
	content               Content
	user, domain          string
	processInstance       string
}

func New(s Store, d Durable, t Tasks, c Content, user, domain string) *Service {
	return &Service{store: s, durable: d, tasks: t, content: c, user: user, domain: domain, processInstance: command.NewRef(user, domain, "worker", "worker").Name.LocalId}
}
func (s *Service) SubmitGoal(ctx context.Context, caller *v1.Caller, c *v1.SubmitGoalCommand) (*v1.CommandReceipt, error) {
	if err := command.ValidateGoal(c); err != nil {
		return nil, err
	}
	if err := command.CheckIdentity(caller, c.Identity, s.user, s.domain); err != nil {
		return nil, err
	}
	if c.Session != nil {
		if err := command.CheckName(caller, c.Session, s.user, s.domain, "session"); err != nil {
			return nil, err
		}
	}
	ref, err := s.content.Stage(ctx, caller, c)
	if err != nil {
		return nil, err
	}
	return s.durable.Submit(ctx, caller, c, ref)
}

// ProcessPending 原子领取可运行的目标工作；新宿主使用新实例身份。
func (s *Service) ProcessPending(ctx context.Context, caller *v1.Caller) error {
	for {
		id := command.NewRef(s.user, s.domain, "command", "command").Name.LocalId
		r, err := s.durable.ExecuteJob(ctx, caller, &v1.JobCommand{Identity: &v1.CommandIdentity{UserId: s.user, IssuerId: caller.GetIssuerId(), TargetDomainId: s.domain, CommandId: id}, ContractVersion: 1, Action: "CLAIM", ProcessInstance: s.processInstance, AllowedTypes: []string{"DECIDE_GOAL"}, Limit: 1, LeaseMs: 30000})
		if err != nil {
			return err
		}
		if r.Decision != v1.Decision_DECISION_ACCEPTED {
			return &command.Failure{Detail: r.Error}
		}
		if len(r.Jobs) == 0 {
			return nil
		}
		if err := s.ProcessClaim(ctx, r.Jobs[0]); err != nil {
			return err
		}
	}
}

// ProcessClaim 供受信宿主恢复固定目标处理，不允许调用方注册处理函数。
func (s *Service) ProcessClaim(ctx context.Context, job *v1.Job) error {
	return s.durable.Decide(ctx, job, s.accept)
}

func (s *Service) accept(ctx context.Context, pending *v1.PendingGoal) (*v1.Ref, *v1.Ref, error) {
	c := pending.Command
	_, now, err := s.store.Position(ctx)
	if err != nil {
		return nil, nil, err
	}
	if c.AcceptUntilUnixMs != nil && *c.AcceptUntilUnixMs <= now {
		return nil, nil, command.Fail("INVALID_INPUT")
	}
	var session *v1.Session
	if c.Session != nil {
		session, err = s.store.LoadSession(ctx, c.Session)
		if err != nil {
			return nil, nil, err
		}
		if session == nil {
			return nil, nil, command.Fail("INVALID_INPUT")
		}
		if session.Status != "ACTIVE" {
			return nil, nil, command.Fail("INVALID_INPUT")
		}
		if c.ExpectedRevision != nil && *c.ExpectedRevision != session.Revision {
			return nil, nil, command.Fail("REVISION_CONFLICT")
		}
	} else {
		if c.ExpectedRevision != nil {
			return nil, nil, command.Fail("INVALID_INPUT")
		}
		session = &v1.Session{SessionId: command.NewRef(s.user, s.domain, "session", "lerna.v1.Session").Name, Status: "ACTIVE"}
	}
	task, err := s.tasks.CreateInTransaction(ctx, pending.ContentRef)
	if err != nil {
		return nil, nil, err
	}
	session.LastCommittedSeq++
	session.Revision++
	session.TaskRefs = append(session.TaskRefs, task)
	input := &v1.SessionInput{InputId: command.NewRef(s.user, s.domain, "input", "lerna.v1.SessionInput").Name, SessionSeq: session.LastCommittedSeq, TaskId: task.Name, InputKind: "GOAL", ContentRef: pending.ContentRef, CommandIdentity: c.Identity, RoutingStatus: "DELIVERED"}
	if err = s.tasks.RecordGoalInTransaction(ctx, &v1.Caller{UserId: s.user, IssuerId: c.Identity.IssuerId}, task, input, nil); err != nil {
		return nil, nil, err
	}
	session.Inputs = append(session.Inputs, input)
	if err := s.store.SaveSession(ctx, session); err != nil {
		return nil, nil, err
	}
	ref := &v1.Ref{Name: input.InputId, Revision: 1, SchemaId: "lerna.v1.SessionInput"}
	// 回查只呈现原语义命令，不暴露认证或诊断字段。
	original := proto.Clone(c).(*v1.SubmitGoalCommand)
	original.Credential = ""
	original.TraceId = ""
	if err = s.saveInputDelivery(ctx, &v1.InputDelivery{Ref: ref, OriginalGoalCommand: original, Input: input}); err != nil {
		return nil, nil, err
	}
	return &v1.Ref{Name: session.SessionId, Revision: session.Revision, SchemaId: "lerna.v1.Session"}, task, nil
}
func (s *Service) QuerySession(ctx context.Context, caller *v1.Caller, id *v1.GlobalName) (*v1.Session, error) {
	if err := command.CheckName(caller, id, s.user, s.domain, "session"); err != nil {
		return nil, err
	}
	return s.store.LoadSession(ctx, id)
}

// RecoverPending 在启动期限内等待现有领取自然过期，再恢复原目标责任。
// 等待不持有事务锁；到期后的写入仍由存储时间和领取代次裁决。
func (s *Service) RecoverPending(ctx context.Context, caller *v1.Caller) error {
	for {
		if err := s.ProcessPending(ctx, caller); err != nil {
			return err
		}
		jobs, err := s.durable.Pending(ctx, caller)
		if err != nil {
			return err
		}
		pending := false
		for _, job := range jobs {
			if job.JobType == "DECIDE_GOAL" {
				pending = true
				break
			}
		}
		if !pending {
			return nil
		}
		timer := time.NewTimer(25 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}
