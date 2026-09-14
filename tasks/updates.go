package tasks

import (
	"context"
	"lerna/authorization"
	"lerna/internal/randomid"
	"reflect"
	"slices"
	"time"
)

// UpdateLimits freeze storage and disposition bounds on first update admission.
type UpdateLimits struct {
	MaxOperations, MaxInteractions, MaxRefs int
	IOTimeout                               time.Duration
	Control                                 ControlLimits
}

func (l UpdateLimits) valid() bool {
	return l.MaxOperations >= 1 && l.MaxOperations <= 64 && l.MaxInteractions >= 1 && l.MaxInteractions <= 16 && l.MaxRefs >= 1 && l.MaxRefs <= 16 && l.IOTimeout >= time.Millisecond && l.IOTimeout <= time.Second && l.Control.valid()
}

type AnswerConstraint struct {
	Kind     string
	MaxBytes uint32
	Choices  []string
}

// InputValidator is a trusted, bounded content/policy seam. Calls occur outside
// runtime transactions and must validate immutable references under the token.
type InputValidator interface {
	ValidateInput(context.Context, string, Task, []string, *AnswerConstraint) error
}
type UpdateService struct {
	core      *Service
	validator InputValidator
	limits    UpdateLimits
}

func (s *Service) Updates(v InputValidator, l UpdateLimits) (*UpdateService, error) {
	if v == nil || !l.valid() {
		return nil, failure(authorization.Invalid)
	}
	return &UpdateService{s, v, l}, nil
}

type UpdateRequest struct {
	OperationID          string
	Ref                  Ref
	ExpectedVersion      uint64
	Intent               string
	InputRefs            []string
	GoalRef, GuidanceRef string
}
type InputReceipt struct {
	OperationID         string
	Ref                 Ref
	Kind, InteractionID string
	Version             uint64
}
type InputFact struct{ OperationID, Subject, Kind, Reference, InteractionID, QuestionRef string }
type inputChange struct {
	Update UpdateRequest
	Reply  InputRequest
	Adjust AdjustRequest
}
type inputRecord struct {
	Subject string
	Change  inputChange
	Receipt InputReceipt
}

func changeAction(c inputChange) string {
	if c.Adjust.OperationID != "" {
		return "task.adjust"
	}
	if c.Reply.OperationID != "" {
		return "task.input"
	}
	switch c.Update.Intent {
	case "append":
		return "task.append"
	case "revise":
		return "task.revise"
	}
	return ""
}
func (c inputChange) meta() (string, Ref, uint64) {
	if c.Adjust.OperationID != "" {
		return c.Adjust.OperationID, c.Adjust.Ref, c.Adjust.ExpectedVersion
	}
	if c.Reply.OperationID != "" {
		return c.Reply.OperationID, c.Reply.Ref, c.Reply.ExpectedVersion
	}
	return c.Update.OperationID, c.Update.Ref, c.Update.ExpectedVersion
}
func (c inputChange) refs() []string {
	if c.Reply.OperationID != "" {
		return []string{c.Reply.AnswerRef}
	}
	v := append([]string(nil), c.Update.InputRefs...)
	if c.Update.GoalRef != "" {
		v = append(v, c.Update.GoalRef)
	}
	if c.Update.GuidanceRef != "" {
		v = append(v, c.Update.GuidanceRef)
	}
	return v
}
func (c inputChange) valid(l UpdateLimits) bool {
	if c.Adjust.OperationID != "" && !reflect.DeepEqual(c, inputChange{Adjust: c.Adjust}) {
		return false
	}
	if c.Reply.OperationID != "" && !reflect.DeepEqual(c, inputChange{Reply: c.Reply}) {
		return false
	}
	op, ref, version := c.meta()
	if op == "" || len(op) > 1024 || !name(ref.Namespace) || !name(ref.TaskID) || version == 0 || changeAction(c) == "" {
		return false
	}
	if c.Adjust.OperationID != "" {
		return c.Adjust.Patch.valid()
	}
	if c.Reply.OperationID != "" {
		return reflect.DeepEqual(c.Update, UpdateRequest{}) && name(c.Reply.InteractionID) && name(c.Reply.AnswerRef)
	}
	r := c.Update
	if r.Intent == "append" {
		if len(r.InputRefs) == 0 || r.GoalRef != "" || r.GuidanceRef != "" {
			return false
		}
	} else if len(r.InputRefs) != 0 || (r.GoalRef == "" && r.GuidanceRef == "") {
		return false
	}
	refs := c.refs()
	if len(refs) > l.MaxRefs {
		return false
	}
	seen := map[string]bool{}
	for _, r := range refs {
		if !name(r) || seen[r] {
			return false
		}
		seen[r] = true
	}
	return true
}
func (u *UpdateService) SubmitUpdate(ctx context.Context, token string, in UpdateRequest) (InputReceipt, error) {
	return u.apply(ctx, token, inputChange{Update: in})
}

// inspect is repeated inside the final transaction after controlled I/O. Known
// requests reconcile before fresh version/lifecycle checks or content reads.
func (u *UpdateService) inspect(j *journal, tx authorization.RuntimeTransaction, token string, c inputChange) (RunSnapshot, *InputReceipt, error) {
	op, ref, version := c.meta()
	id, e := tx.Authorize(token, u.core.config.Resource, changeAction(c))
	if e != nil {
		return RunSnapshot{}, nil, e
	}
	if ref.Namespace != id.Namespace {
		return RunSnapshot{}, nil, failure(authorization.Denied)
	}
	if e = tx.Operation(op, id.Subject, false); e != nil {
		return RunSnapshot{}, nil, e
	}
	if delegationOperation(j, op) {
		return RunSnapshot{}, nil, failure(authorization.IdentityConflict)
	}
	if _, ok := j.Operations[op]; ok {
		return RunSnapshot{}, nil, failure(authorization.IdentityConflict)
	}
	if _, ok := j.Controls[op]; ok {
		return RunSnapshot{}, nil, failure(authorization.IdentityConflict)
	}
	if old, ok := j.InputChanges[op]; ok {
		if old.Subject != id.Subject {
			return RunSnapshot{}, nil, failure(authorization.Denied)
		}
		if !reflect.DeepEqual(old.Change, c) {
			return RunSnapshot{}, nil, failure(authorization.IdentityConflict)
		}
		if _, e = tx.Authorize(token, u.core.config.Resource, "task.read"); e != nil {
			return RunSnapshot{}, nil, e
		}
		v := old.Receipt
		return RunSnapshot{}, &v, nil
	}
	r, ok := j.Runs[ref.TaskID]
	if !ok || r.Task.Subject != id.Subject {
		return RunSnapshot{}, nil, failure(authorization.Denied)
	}
	if terminal(r.Task) {
		return RunSnapshot{}, nil, failure(authorization.Conflict)
	}
	if version != r.Task.Version {
		return RunSnapshot{}, nil, failure(authorization.Conflict)
	}
	if (r.ControlLimits != (ControlLimits{}) && r.ControlLimits != u.limits.Control) || (j.ControlLimits != (ControlLimits{}) && j.ControlLimits != u.limits.Control) {
		return RunSnapshot{}, nil, failure(authorization.Invalid)
	}
	if r.UpdateLimits != (UpdateLimits{}) && r.UpdateLimits != u.limits {
		return RunSnapshot{}, nil, failure(authorization.Invalid)
	}
	count := 0
	for _, old := range j.InputChanges {
		if old.Receipt.Ref == ref {
			count++
		}
	}
	if count >= u.limits.MaxOperations || len(j.Commits[ref.TaskID]) >= 256 {
		return RunSnapshot{}, nil, failure(authorization.Unavailable)
	}
	if c.Adjust.OperationID != "" {
		if _, e := adjusted(r.Task, c.Adjust.Patch); e != nil {
			return RunSnapshot{}, nil, e
		}
	}
	if _, e = replyConstraint(r, c, tx.Now().Unix()); e != nil {
		return RunSnapshot{}, nil, e
	}
	return r, nil, nil
}
func (u *UpdateService) apply(ctx context.Context, token string, c inputChange) (InputReceipt, error) {
	if !c.valid(u.limits) {
		return InputReceipt{}, failure(authorization.Invalid)
	}
	c.Update.InputRefs = append([]string(nil), c.Update.InputRefs...)
	var original RunSnapshot
	var replay *InputReceipt
	e := u.core.transaction(ctx, func(j *journal, tx authorization.RuntimeTransaction) error {
		var e error
		original, replay, e = u.inspect(j, tx, token, c)
		return e
	})
	if e != nil {
		return InputReceipt{}, e
	}
	if replay != nil {
		return *replay, nil
	}
	bounded, cancel := context.WithTimeout(ctx, u.limits.IOTimeout)
	defer cancel()
	var constraint *AnswerConstraint
	if c.Reply.OperationID != "" {
		for _, v := range original.Interactions {
			if v.ID == c.Reply.InteractionID {
				copy := v.Question.Constraint
				constraint = &copy
				if e = u.validator.ValidateInput(bounded, token, original.Task, []string{v.Question.QuestionRef}, nil); e != nil {
					return InputReceipt{}, e
				}
			}
		}
	}
	if len(c.refs()) > 0 {
		if e = u.validator.ValidateInput(bounded, token, original.Task, c.refs(), constraint); e != nil {
			return InputReceipt{}, e
		}
	}
	changeID, e := randomid.New()
	if e != nil {
		return InputReceipt{}, e
	}
	var out InputReceipt
	e = u.core.transaction(ctx, func(j *journal, tx authorization.RuntimeTransaction) error {
		r, old, e := u.inspect(j, tx, token, c)
		if e != nil {
			return e
		}
		if old != nil {
			out = *old
			return nil
		}
		op, ref, _ := c.meta()
		if e = tx.Operation(op, r.Task.Subject, true); e != nil {
			return e
		}
		for _, v := range c.refs() {
			if !slices.Contains(r.Task.InputRefs, v) {
				r.Task.InputRefs = append(r.Task.InputRefs, v)
			}
			fact := InputFact{OperationID: op, Subject: r.Task.Subject, Kind: inputKind(c), Reference: v}
			if c.Reply.OperationID != "" {
				for _, interaction := range r.Interactions {
					if interaction.ID == c.Reply.InteractionID {
						fact.InteractionID = interaction.ID
						fact.QuestionRef = interaction.Question.QuestionRef
						if !slices.Contains(r.Task.InputRefs, fact.QuestionRef) {
							r.Task.InputRefs = append(r.Task.InputRefs, fact.QuestionRef)
						}
					}
				}
			}
			r.Task.InputFacts = append(r.Task.InputFacts, fact)
		}
		if len(r.Task.InputRefs) > u.limits.MaxRefs && len(r.Task.InputRefs) > len(original.Task.InputRefs) {
			return failure(authorization.Unavailable)
		}
		if c.Update.GoalRef != "" {
			r.Task.Goal = ""
			r.Task.GoalRef = c.Update.GoalRef
		}
		if c.Update.GuidanceRef != "" {
			r.Task.GuidanceRef = c.Update.GuidanceRef
		}
		if e = applyLimits(&r, c); e != nil {
			return e
		}
		applyInteractionChange(&r, c)
		r.Task.Version++
		r.UpdateVersion = r.Task.Version
		r.UpdateLimits = u.limits
		j.ControlLimits = u.limits.Control
		if r.ControlLimits == (ControlLimits{}) {
			r.ControlLimits = u.limits.Control
		}
		if r.Work[0].InFlight {
			r.Task.State = "WAITING"
			addWait(&r.Task, "reconciliation")
			syncWait(&r.Task)
		} else {
			r.Work[0].LeaseUntil = 0
			settleControl(&r, tx, token)
		}
		out = InputReceipt{OperationID: op, Ref: ref, Kind: inputKind(c), InteractionID: c.Reply.InteractionID, Version: r.Task.Version}
		r.LastCommit = CommitReceipt{Ref: ref, ChangeID: changeID, Version: r.Task.Version}
		r.Records = append(r.Records, Record{Kind: "input:" + inputKind(c), Version: r.Task.Version})
		j.InputChanges[op] = inputRecord{Subject: r.Task.Subject, Change: c, Receipt: out}
		j.Runs[ref.TaskID] = r
		j.Commits[ref.TaskID][changeID] = commit{Receipt: r.LastCommit, InputOperation: op, Snapshot: r}
		return nil
	})
	if e != nil {
		return InputReceipt{}, e
	}
	return out, nil
}
func (u *UpdateService) Lookup(ctx context.Context, token, namespace, op string) (InputReceipt, error) {
	var out InputReceipt
	e := u.core.transaction(ctx, func(j *journal, tx authorization.RuntimeTransaction) error {
		id, e := tx.Authorize(token, u.core.config.Resource, "task.read")
		if e != nil {
			return e
		}
		if namespace != id.Namespace {
			return failure(authorization.Denied)
		}
		if e = tx.Operation(op, id.Subject, false); e != nil {
			return e
		}
		old, ok := j.InputChanges[op]
		if !ok || old.Subject != id.Subject {
			return failure(authorization.Denied)
		}
		out = old.Receipt
		return nil
	})
	return out, e
}

func inputKind(c inputChange) string {
	if c.Adjust.OperationID != "" {
		return "limits"
	}
	if c.Reply.OperationID != "" {
		return "reply"
	}
	return c.Update.Intent
}
