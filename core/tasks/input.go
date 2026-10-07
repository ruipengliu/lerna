package tasks

import (
	"context"

	"google.golang.org/protobuf/proto"

	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
)

type InputStore interface {
	SaveTaskInputs(context.Context, *v1.TaskInputHistory) error
	LoadTaskInputs(context.Context, *v1.GlobalName) (*v1.TaskInputHistory, error)
}

// AcceptInputInTransaction 接纳先作废旧依据，可信处理稍后才能推进绑定水位。
func (s *Service) AcceptInputInTransaction(ctx context.Context, caller *v1.Caller, c *v1.SubmitInputCommand, input *v1.SessionInput, changesBasis bool) error {
	t, e := s.QueryTask(ctx, caller, c.TaskId)
	if e != nil {
		return e
	}
	if t == nil {
		return command.Fail("NOT_FOUND")
	}
	if t.Lifecycle != v1.TaskLifecycle_TASK_LIFECYCLE_OPEN {
		return command.Fail("TASK_CLOSED")
	}
	if e = s.checkTaskNotClosing(ctx, t); e != nil {
		return e
	}
	if t.RequirementsVersion != c.ExpectedRequirementsVersion || t.InputVersion != c.ExpectedInputVersion {
		return command.Fail("STALE_INPUT")
	}
	p, e := s.store.LoadPlanning(ctx, t.TaskId)
	if e != nil {
		return e
	}
	history, e := s.store.(InputStore).LoadTaskInputs(ctx, t.TaskId)
	if e != nil {
		return e
	}
	t.InputVersion++
	t.Revision++
	if changesBasis {
		t.ControlGeneration++
		setInputWaiting(t, "INPUT_PROCESSING")
	}
	if !changesBasis && t.BoundInputVersion == t.InputVersion-1 {
		t.BoundInputVersion = t.InputVersion
		if p.Requirements != nil {
			p.Requirements = proto.Clone(p.Requirements).(*v1.Requirements)
			p.Requirements.Ref = command.NewRef(s.user, s.domain, "requirements", "lerna.v1.Requirements")
			p.Requirements.BoundInputVersion = t.BoundInputVersion
			if e = s.saveRequirements(ctx, p.Requirements); e != nil {
				return e
			}
		}
	}
	if e = s.invalidatePlanning(ctx, t, p); e != nil {
		return e
	}
	input.TaskId = t.TaskId
	input.RoutingStatus = "TASK_ACCEPTED"
	input.ExpectedRequirementsVersion = c.ExpectedRequirementsVersion
	input.ExpectedInputVersion = c.ExpectedInputVersion
	input.TaskInputSeq = uint64(len(history.Inputs)) + 1
	status := "ACCEPTED"
	if !changesBasis {
		status = "PROCESSED"
	}
	history.Inputs = append(history.Inputs, &v1.TaskInputRecord{InputRef: &v1.Ref{Name: input.InputId, Revision: 1, SchemaId: "lerna.v1.SessionInput"}, ContentRef: c.ContentRef, InputVersion: t.InputVersion, TaskInputSeq: input.TaskInputSeq, ChangesBasis: changesBasis, ExplicitConditions: c.ExplicitConditions, ProcessingStatus: status})
	if e = s.saveTask(ctx, t); e != nil {
		return e
	}
	if e = s.store.SavePlanning(ctx, p); e != nil {
		return e
	}
	return s.store.(InputStore).SaveTaskInputs(ctx, history)
}
func (s *Service) QueryInputs(ctx context.Context, caller *v1.Caller, id *v1.GlobalName) (*v1.TaskInputHistory, error) {
	if e := command.CheckName(caller, id, s.user, s.domain, "task"); e != nil {
		return nil, e
	}
	return s.store.(InputStore).LoadTaskInputs(ctx, id)
}

// ControlInTransaction 只封闭未来开始资格；取消不删除原动作或交接责任。
func (s *Service) ControlInTransaction(ctx context.Context, caller *v1.Caller, c *v1.SubmitInputCommand) error {
	t, e := s.QueryTask(ctx, caller, c.TaskId)
	if e != nil {
		return e
	}
	if t == nil {
		return command.Fail("NOT_FOUND")
	}
	if t.Lifecycle != v1.TaskLifecycle_TASK_LIFECYCLE_OPEN || t.ControlGeneration != c.ExpectedControlGeneration {
		return command.Fail("STALE_CONTROL")
	}
	if e = s.checkTaskNotClosing(ctx, t); e != nil {
		return e
	}
	switch c.Control {
	case "PAUSE":
		if t.Control != v1.TaskControl_TASK_CONTROL_ACTIVE {
			return command.Fail("INVALID_CONTROL")
		}
		t.Control = v1.TaskControl_TASK_CONTROL_PAUSED
	case "RESUME":
		if t.Control != v1.TaskControl_TASK_CONTROL_PAUSED {
			return command.Fail("INVALID_CONTROL")
		}
		t.Control = v1.TaskControl_TASK_CONTROL_ACTIVE
	case "CANCEL":
		if t.Control == v1.TaskControl_TASK_CONTROL_CANCELLING {
			return command.Fail("INVALID_CONTROL")
		}
		t.Control = v1.TaskControl_TASK_CONTROL_CANCELLING
		t.Progress = v1.TaskProgress_TASK_PROGRESS_WAITING
		t.WaitingOn = append(t.WaitingOn, "CANCELLATION_CLOSURE")
	default:
		return command.Fail("UNSUPPORTED_FEATURE")
	}
	p, e := s.store.LoadPlanning(ctx, t.TaskId)
	if e != nil {
		return e
	}
	t.ControlGeneration++
	t.Revision++
	if c.Control == "CANCEL" {
		if e = s.saveCancellation(ctx, t, p, c); e != nil {
			return e
		}
	}
	if e = s.invalidatePlanning(ctx, t, p); e != nil {
		return e
	}
	if e = s.saveTask(ctx, t); e != nil {
		return e
	}
	return s.store.SavePlanning(ctx, p)
}

// recordGoalInTransaction 初始目标也属于任务输入序，不能留作隐式上下文。
func (s *Service) recordGoalInTransaction(ctx context.Context, caller *v1.Caller, task *v1.Ref, input *v1.SessionInput, conditions []*v1.Requirement) error {
	t, e := s.QueryTask(ctx, caller, task.Name)
	if e != nil {
		return e
	}
	if t == nil {
		return command.Fail("NOT_FOUND")
	}
	input.TaskInputSeq = 1
	r := &v1.TaskInputRecord{InputRef: &v1.Ref{Name: input.InputId, Revision: 1, SchemaId: "lerna.v1.SessionInput"}, ContentRef: input.ContentRef, InputVersion: 1, TaskInputSeq: 1, ChangesBasis: true, ProcessingStatus: "ACCEPTED", ExplicitConditions: conditions}
	if t.BoundInputVersion == 1 {
		r.ProcessingStatus = "PROCESSED"
		r.ProcessingDecision = input.CommandIdentity
	}
	return s.store.(InputStore).SaveTaskInputs(ctx, &v1.TaskInputHistory{TaskId: t.TaskId, Inputs: []*v1.TaskInputRecord{r}})
}

// setInputWaiting 只替换输入处理负责的缺口，保留核对、授权和取消收尾的责任。
func setInputWaiting(t *v1.Task, reason string) {
	remaining := make([]string, 0, len(t.WaitingOn)+1)
	for _, wait := range t.WaitingOn {
		switch wait {
		case "REQUIREMENTS", "INPUT_PROCESSING", "USER_CLARIFICATION":
		default:
			remaining = append(remaining, wait)
		}
	}
	if reason != "" {
		remaining = append(remaining, reason)
	}
	t.WaitingOn = remaining
	if len(remaining) == 0 {
		t.Progress = v1.TaskProgress_TASK_PROGRESS_RUNNING
	} else {
		t.Progress = v1.TaskProgress_TASK_PROGRESS_WAITING
	}
}
