package tasks

import (
	"context"
	"lerna/authorization"
)

// PrepareAnswer ends action planning, preserving its decisions, reports and
// task-wide usage. It is not completion: the same task must reserve, generate
// and publish a governed answer. Original transition replays are idempotent.
func (p *ActionPort) PrepareAnswer(ctx context.Context, q Qualification) (RunSnapshot, error) {
	var out RunSnapshot
	err := p.service.transaction(ctx, func(j *journal, tx authorization.RuntimeTransaction) error {
		r, err := p.current(j, tx, q, false)
		if err != nil {
			return err
		}
		if r.Actions == nil {
			return failure(authorization.Conflict)
		}
		if r.Actions.AnswerQualification != nil {
			if *r.Actions.AnswerQualification != q {
				return failure(authorization.IdentityConflict)
			}
			out = r
			return nil
		}
		r, err = p.current(j, tx, q, true)
		if err != nil {
			return err
		}
		a := r.Actions
		if a.WrongWrite || len(a.Actions) == 0 || unresolvedActions(a) || r.Work[0].ExecutionOperation != "" || r.Task.ModelReservedRequests != 0 || r.Task.ModelReservedTokens != 0 || len(r.Generations) != 0 {
			return failure(authorization.Conflict)
		}
		for _, d := range a.Decisions {
			if d.Record == nil || !d.Admitted && d.Rejection == "" {
				return failure(authorization.Conflict)
			}
		}
		original := q
		a.AnswerQualification = &original
		r.Task.Version++
		r.Work[0].DecisionVersion = r.Task.Version
		r.Records = append(r.Records, Record{Kind: "action:answer-ready", Version: r.Task.Version})
		j.Runs[q.Ref.TaskID] = r
		out = r
		return nil
	})
	return out, err
}
