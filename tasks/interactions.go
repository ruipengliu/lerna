package tasks

import (
	"context"
	"lerna/authorization"
	"lerna/internal/randomid"
	"reflect"
	"slices"
)

type Question struct {
	QuestionRef                          string
	Constraint                           AnswerConstraint
	Dependency, DependencyRef, Responder string
	ExpiresUnix                          int64
}
type Interaction struct {
	ID                                string
	Question                          Question
	State, AnswerRef, AnswerOperation string
}
type InteractionPage struct {
	Ref          Ref
	Version      uint64
	Interactions []Interaction
	Inputs       []InputFact
}
type WaitRequest struct {
	ChangeID      string
	Qualification Qualification
	Questions     []Question
}
type InputRequest struct {
	OperationID              string
	Ref                      Ref
	ExpectedVersion          uint64
	InteractionID, AnswerRef string
}

func validConstraint(c AnswerConstraint) bool {
	if c.MaxBytes < 1 || c.MaxBytes > 4096 {
		return false
	}
	switch c.Kind {
	case "text":
		return len(c.Choices) == 0
	case "choice":
		if len(c.Choices) < 1 || len(c.Choices) > 16 {
			return false
		}
		seen := map[string]bool{}
		for _, v := range c.Choices {
			if !name(v) || len(v) > int(c.MaxBytes) || seen[v] {
				return false
			}
			seen[v] = true
		}
		return true
	}
	return false
}
func validQuestion(q Question, t Task, now int64) bool {
	if !name(q.QuestionRef) || !validConstraint(q.Constraint) || q.Responder != t.Subject || q.ExpiresUnix <= now || q.ExpiresUnix > t.Constraints.DeadlineUnix {
		return false
	}
	switch q.Dependency {
	case "goal", "facts":
		return q.DependencyRef == ""
	case "input":
		return slices.Contains(t.InputRefs, q.DependencyRef)
	}
	return false
}

// CreateWait is a trusted module admission port, deliberately absent from the
// business RPC. It cannot create authorization approval interactions.
func (u *UpdateService) CreateWait(ctx context.Context, token string, in WaitRequest) (InteractionPage, error) {
	if !name(in.ChangeID) || len(in.Questions) < 1 || len(in.Questions) > u.limits.MaxInteractions {
		return InteractionPage{}, failure(authorization.Invalid)
	}
	// Copy before I/O so caller-owned slices never alias persisted identity.
	in.Questions = append([]Question(nil), in.Questions...)
	for i := range in.Questions {
		in.Questions[i].Constraint.Choices = append([]string(nil), in.Questions[i].Constraint.Choices...)
	}
	var original RunSnapshot
	var replay *InteractionPage
	inspect := func(j *journal, tx authorization.RuntimeTransaction) (RunSnapshot, *InteractionPage, error) {
		id, e := tx.Authorize(token, u.core.config.Resource, "task.execute")
		if e != nil {
			return RunSnapshot{}, nil, e
		}
		q := in.Qualification
		r, ok := j.Runs[q.Ref.TaskID]
		if q.Ref.Namespace != id.Namespace || !ok || r.Task.Subject != id.Subject {
			return RunSnapshot{}, nil, failure(authorization.Denied)
		}
		if old, ok := j.Commits[q.Ref.TaskID][in.ChangeID]; ok {
			if old.WaitRequest == nil || !reflect.DeepEqual(*old.WaitRequest, in) {
				return RunSnapshot{}, nil, failure(authorization.IdentityConflict)
			}
			v := interactionView(old.Snapshot, tx.Now().Unix())
			return r, &v, nil
		}
		if terminal(r.Task) || controlIntent(r.Task) != "RUN" || QualificationOf(r) != q || r.Work[0].InFlight && (!actionWait(r, in) || r.Work[0].LeaseUntil <= tx.Now().UnixNano()) {
			return RunSnapshot{}, nil, failure(authorization.Conflict)
		}
		if (r.ControlLimits != (ControlLimits{}) && r.ControlLimits != u.limits.Control) || (j.ControlLimits != (ControlLimits{}) && j.ControlLimits != u.limits.Control) {
			return RunSnapshot{}, nil, failure(authorization.Invalid)
		}
		if r.UpdateLimits != (UpdateLimits{}) && r.UpdateLimits != u.limits {
			return RunSnapshot{}, nil, failure(authorization.Invalid)
		}
		if len(r.Interactions)+len(in.Questions) > u.limits.MaxInteractions || len(j.Commits[q.Ref.TaskID]) >= 256 {
			return RunSnapshot{}, nil, failure(authorization.Unavailable)
		}
		for _, question := range in.Questions {
			if !validQuestion(question, r.Task, tx.Now().Unix()) {
				return RunSnapshot{}, nil, failure(authorization.Invalid)
			}
		}
		return r, nil, nil
	}
	e := u.core.transaction(ctx, func(j *journal, tx authorization.RuntimeTransaction) error {
		var e error
		original, replay, e = inspect(j, tx)
		return e
	})
	if e != nil {
		return InteractionPage{}, e
	}
	if replay != nil {
		return *replay, nil
	}
	bounded, cancel := context.WithTimeout(ctx, u.limits.IOTimeout)
	defer cancel()
	refs := []string{}
	for _, q := range in.Questions {
		refs = append(refs, q.QuestionRef)
	}
	if e = u.validator.ValidateInput(bounded, token, original.Task, refs, nil); e != nil {
		return InteractionPage{}, e
	}
	ids := make([]string, len(in.Questions))
	for i := range ids {
		ids[i], e = randomid.New()
		if e != nil {
			return InteractionPage{}, e
		}
	}
	var out InteractionPage
	e = u.core.transaction(ctx, func(j *journal, tx authorization.RuntimeTransaction) error {
		r, old, e := inspect(j, tx)
		if e != nil {
			return e
		}
		if old != nil {
			out = *old
			return nil
		}
		for i, q := range in.Questions {
			r.Interactions = append(r.Interactions, Interaction{ID: ids[i], Question: q, State: "pending"})
		}
		if actionWait(r, in) {
			r.Work[0].InFlight = false
			r.Actions.Decisions[len(r.Actions.Decisions)-1].Admitted = true
		}
		r.Task.Version++
		r.UpdateVersion = r.Task.Version
		r.UpdateLimits = u.limits
		j.ControlLimits = u.limits.Control
		if r.ControlLimits == (ControlLimits{}) {
			r.ControlLimits = u.limits.Control
		}
		r.Work[0].LeaseUntil = 0
		r.Task.State = "WAITING"
		addWait(&r.Task, "input")
		syncWait(&r.Task)
		r.LastCommit = CommitReceipt{Ref: r.Task.Ref, ChangeID: in.ChangeID, Version: r.Task.Version}
		r.Records = append(r.Records, Record{Kind: "interaction:created", Version: r.Task.Version})
		copied := in
		j.Commits[r.Task.Ref.TaskID][in.ChangeID] = commit{WaitRequest: &copied, Receipt: r.LastCommit, Snapshot: r}
		j.Runs[r.Task.Ref.TaskID] = r
		out = interactionView(r, tx.Now().Unix())
		return nil
	})
	return out, e
}
func interactionView(r RunSnapshot, now int64) InteractionPage {
	out := InteractionPage{Ref: r.Task.Ref, Version: r.Task.Version, Interactions: append([]Interaction(nil), r.Interactions...), Inputs: append([]InputFact(nil), r.Task.InputFacts...)}
	for i, v := range out.Interactions {
		if v.State == "pending" && v.Question.ExpiresUnix <= now {
			out.Interactions[i].State = "expired"
		}
	}
	return out
}
func (u *UpdateService) Interactions(ctx context.Context, token string, ref Ref) (InteractionPage, error) {
	var out InteractionPage
	var task Task
	e := u.core.transaction(ctx, func(j *journal, tx authorization.RuntimeTransaction) error {
		id, e := tx.Authorize(token, u.core.config.Resource, "task.read")
		if e != nil {
			return e
		}
		r, ok := j.Runs[ref.TaskID]
		if ref.Namespace != id.Namespace || !ok || r.Task.Subject != id.Subject {
			return failure(authorization.Denied)
		}
		task = r.Task
		out = interactionView(r, tx.Now().Unix())
		return nil
	})
	if e != nil {
		return InteractionPage{}, e
	}
	if len(out.Interactions) > 0 {
		refs := []string{}
		for _, v := range out.Interactions {
			if !slices.Contains(refs, v.Question.QuestionRef) {
				refs = append(refs, v.Question.QuestionRef)
			}
		}
		bounded, cancel := context.WithTimeout(ctx, u.limits.IOTimeout)
		defer cancel()
		if e = u.validator.ValidateInput(bounded, token, task, refs, nil); e != nil {
			return InteractionPage{}, e
		}
	}
	return out, nil
}
func (u *UpdateService) ProvideInput(ctx context.Context, token string, in InputRequest) (InputReceipt, error) {
	return u.apply(ctx, token, inputChange{Reply: in})
}
func replyConstraint(r RunSnapshot, c inputChange, now int64) (*AnswerConstraint, error) {
	if c.Reply.OperationID == "" {
		return nil, nil
	}
	for _, v := range r.Interactions {
		if v.ID == c.Reply.InteractionID {
			if v.State != "pending" || v.Question.ExpiresUnix <= now || v.Question.Responder != r.Task.Subject {
				return nil, failure(authorization.Conflict)
			}
			q := v.Question.Constraint
			return &q, nil
		}
	}
	return nil, failure(authorization.Conflict)
}
func applyInteractionChange(r *RunSnapshot, c inputChange) {
	for i, v := range r.Interactions {
		if c.Reply.OperationID != "" && v.ID == c.Reply.InteractionID {
			r.Interactions[i].State = "answered"
			r.Interactions[i].AnswerRef = c.Reply.AnswerRef
			r.Interactions[i].AnswerOperation = c.Reply.OperationID
			continue
		}
		if v.State != "pending" {
			continue
		}
		if (v.Question.Dependency == "goal" && c.Update.Intent == "revise") || (v.Question.Dependency == "facts" && (c.Update.Intent != "" || c.Reply.OperationID != "")) {
			r.Interactions[i].State = "revoked"
		}
	}
	pending := false
	for _, v := range r.Interactions {
		if v.State == "pending" {
			pending = true
		}
	}
	if !pending {
		removeWait(&r.Task, "input")
	}
}

// ValidInteractions validates the public metadata shape; it grants no authority.
func ValidInteractions(p InteractionPage) bool {
	if !name(p.Ref.Namespace) || !name(p.Ref.TaskID) || p.Version == 0 || len(p.Interactions) > 16 || len(p.Inputs) > 1024 {
		return false
	}
	seen := map[string]bool{}
	for _, v := range p.Interactions {
		if !name(v.ID) || seen[v.ID] || !name(v.Question.QuestionRef) || !name(v.Question.Responder) || !validConstraint(v.Question.Constraint) || v.Question.ExpiresUnix <= 0 {
			return false
		}
		seen[v.ID] = true
		switch v.Question.Dependency {
		case "goal", "facts":
			if v.Question.DependencyRef != "" {
				return false
			}
		case "input":
			if !name(v.Question.DependencyRef) {
				return false
			}
		default:
			return false
		}
		switch v.State {
		case "pending", "expired", "revoked":
			if v.AnswerRef != "" || v.AnswerOperation != "" {
				return false
			}
		case "answered":
			if !name(v.AnswerRef) || v.AnswerOperation == "" {
				return false
			}
		default:
			return false
		}
	}
	for _, v := range p.Inputs {
		if !validFactAssociation(v) || v.OperationID == "" || !name(v.Subject) || !name(v.Reference) {
			return false
		}
		switch v.Kind {
		case "append", "revise", "reply":
		default:
			return false
		}
	}
	return true
}

func validFactAssociation(f InputFact) bool {
	if f.Kind == "reply" {
		return name(f.InteractionID) && name(f.QuestionRef)
	}
	return f.InteractionID == "" && f.QuestionRef == ""
}
