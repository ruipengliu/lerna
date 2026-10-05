package tasks

import (
	"context"

	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

// ProcessInput 只有受信宿主可以根据持久输入逐条解释；模型输出不是处理决定。
func (s *Service) ProcessInput(ctx context.Context, caller *v1.Caller, c *v1.ProcessInputCommand) (*v1.CommandReceipt, error) {
	if e := command.ValidateHeader(c.GetHeader(), c); e != nil {
		return nil, e
	}
	return s.decisions.Execute(ctx, caller, c.Header, command.SemanticFingerprint("process-input", c), "tasks.input", func(tx context.Context) (*v1.Ref, error) {
		if caller.IssuerId != "host" {
			return nil, command.Fail("PERMISSION_DENIED")
		}
		if c.TaskRef == nil || c.TaskRef.SchemaId != "lerna.v1.Task" {
			return nil, command.Fail("INVALID_INPUT")
		}
		t, e := s.QueryTask(tx, caller, c.TaskRef.Name)
		if e != nil {
			return nil, e
		}
		if t == nil {
			return nil, command.Fail("NOT_FOUND")
		}
		if t.Lifecycle != v1.TaskLifecycle_TASK_LIFECYCLE_OPEN || t.Revision != c.TaskRef.Revision {
			return nil, command.Fail("STALE_INPUT")
		}
		if c.InputVersion != t.BoundInputVersion+1 {
			return nil, command.Fail("INPUT_ORDER")
		}
		h, e := s.store.(InputStore).LoadTaskInputs(tx, t.TaskId)
		if e != nil {
			return nil, e
		}
		var input *v1.TaskInputRecord
		for _, r := range h.Inputs {
			if r.InputVersion == c.InputVersion {
				input = r
			}
		}
		if input == nil || input.ProcessingStatus == "PROCESSED" {
			return nil, command.Fail("INPUT_ORDER")
		}
		p, e := s.store.LoadPlanning(tx, t.TaskId)
		if e != nil {
			return nil, e
		}
		switch c.Outcome {
		case "CLARIFY":
			if len(c.Conditions) != 0 || c.SourceInputRef != nil {
				return nil, command.Fail("INVALID_INPUT")
			}
			input.ProcessingStatus = "WAITING_USER"
			setInputWaiting(t, "USER_CLARIFICATION")
		case "UNCHANGED", "REPLACE":
			if c.Source != "TRUSTED_TEMPLATE" && c.Source != "USER_EXPLICIT" {
				return nil, command.Fail("UNSUPPORTED_FEATURE")
			}
			if c.Outcome == "UNCHANGED" && (p.Requirements == nil || len(c.Conditions) != 0 || c.Source != "TRUSTED_TEMPLATE") {
				return nil, command.Fail("INVALID_REQUIREMENTS")
			}
			var sourceInput *v1.TaskInputRecord
			if c.Source == "USER_EXPLICIT" {
				sourceInput = input
				if c.SourceInputRef != nil {
					sourceInput = nil
					for _, candidate := range h.Inputs {
						if proto.Equal(candidate.InputRef, c.SourceInputRef) {
							sourceInput = candidate
							break
						}
					}
				}
				if sourceInput == nil || sourceInput.InputVersion < input.InputVersion || !proto.Equal(&v1.AcceptRequirementsCommand{Conditions: c.Conditions}, &v1.AcceptRequirementsCommand{Conditions: sourceInput.ExplicitConditions}) {
					return nil, command.Fail("INVALID_REQUIREMENTS")
				}
			} else if c.SourceInputRef != nil {
				return nil, command.Fail("INVALID_REQUIREMENTS")
			}
			if c.Outcome == "REPLACE" {
				if e = s.validateConditions(tx, caller, c.Conditions); e != nil {
					return nil, e
				}
				if p.Requirements != nil {
					t.RequirementsVersion++
				}
				t.ControlGeneration++
				p.Requirements = &v1.Requirements{TaskId: t.TaskId, RequirementsVersion: t.RequirementsVersion, Source: c.Source, Conditions: c.Conditions}
				if sourceInput != nil {
					p.Requirements.SourceInputRef = sourceInput.InputRef
				}
			} else {
				p.Requirements = proto.Clone(p.Requirements).(*v1.Requirements)
			}
			input.ProcessingStatus = "PROCESSED"
			t.BoundInputVersion = c.InputVersion
			// 已接纳的非依据答案不能越过前面的修改；处理前项后才连续推进。
			for _, r := range h.Inputs {
				if r.InputVersion == t.BoundInputVersion+1 && r.ProcessingStatus == "PROCESSED" {
					t.BoundInputVersion++
				}
			}
			p.Requirements.Ref = command.NewRef(s.user, s.domain, "requirements", "lerna.v1.Requirements")
			p.Requirements.BoundInputVersion = t.BoundInputVersion
			p.Requirements.AcceptedBy = c.Header.Identity
			t.RequirementsStatus = v1.RequirementsStatus_REQUIREMENTS_STATUS_ACCEPTED
			if t.BoundInputVersion == t.InputVersion {
				setInputWaiting(t, "")
			} else {
				setInputWaiting(t, "INPUT_PROCESSING")
			}
			if e = s.store.SaveRequirements(tx, p.Requirements); e != nil {
				return nil, e
			}
		default:
			return nil, command.Fail("UNSUPPORTED_FEATURE")
		}
		input.ProcessingDecision = c.Header.Identity
		t.Revision++
		if e = s.invalidatePlanning(tx, t, p); e != nil {
			return nil, e
		}
		if e = s.store.SaveTask(tx, t); e != nil {
			return nil, e
		}
		if e = s.store.SavePlanning(tx, p); e != nil {
			return nil, e
		}
		if e = s.store.(InputStore).SaveTaskInputs(tx, h); e != nil {
			return nil, e
		}
		return &v1.Ref{Name: t.TaskId, Revision: t.Revision, SchemaId: "lerna.v1.Task"}, nil
	})
}
func (s *Service) validateConditions(ctx context.Context, caller *v1.Caller, conditions []*v1.Requirement) error {
	if len(conditions) == 0 {
		return command.Fail("INVALID_REQUIREMENTS")
	}
	seen := map[string]bool{}
	for _, condition := range conditions {
		if condition == nil || condition.ConditionId == "" || seen[condition.ConditionId] || condition.DescriptionRef == nil || condition.RuleVersion != 1 || (condition.VerificationRule != "TARGET_RECORD" && condition.VerificationRule != "USER_EVALUATION") {
			return command.Fail("INVALID_REQUIREMENTS")
		}
		if e := s.content.CheckUsable(ctx, caller, condition.DescriptionRef); e != nil {
			return e
		}
		seen[condition.ConditionId] = true
	}
	return nil
}

// AcceptExplicitInTransaction 只接纳会话保存的用户条件，不接受模型整理来源。
func (s *Service) AcceptExplicitInTransaction(ctx context.Context, caller *v1.Caller, ref *v1.Ref, conditions []*v1.Requirement, source *v1.CommandIdentity, inputRef *v1.Ref) error {
	if e := s.validateConditions(ctx, caller, conditions); e != nil {
		return e
	}
	t, e := s.QueryTask(ctx, caller, ref.Name)
	if e != nil {
		return e
	}
	if t == nil || t.Revision != ref.Revision || t.InputVersion != 1 {
		return command.Fail("STALE_INPUT")
	}
	p, e := s.store.LoadPlanning(ctx, t.TaskId)
	if e != nil {
		return e
	}
	p.Requirements = &v1.Requirements{Ref: command.NewRef(s.user, s.domain, "requirements", "lerna.v1.Requirements"), TaskId: t.TaskId, RequirementsVersion: 1, BoundInputVersion: 1, Source: "USER_EXPLICIT", Conditions: conditions, AcceptedBy: source, SourceInputRef: inputRef}
	t.BoundInputVersion = 1
	t.RequirementsStatus = v1.RequirementsStatus_REQUIREMENTS_STATUS_ACCEPTED
	t.Progress = v1.TaskProgress_TASK_PROGRESS_RUNNING
	t.WaitingOn = nil
	t.Revision++
	if e = s.store.SaveRequirements(ctx, p.Requirements); e != nil {
		return e
	}
	if e = s.store.SavePlanning(ctx, p); e != nil {
		return e
	}
	return s.store.SaveTask(ctx, t)
}
