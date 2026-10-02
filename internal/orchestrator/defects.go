package orchestrator

import (
	"context"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/durable"
)

func (c *Coordinator) PrepareDefect(ctx context.Context, caller api.Caller, d Defect) error {
	if caller.TenantID != c.Config.Scope.TenantID || caller.SourceOwnerID == "" || validateValue("Id", d.ID) != nil || validateValue("ComponentRef", d.RuleRef) != nil || validateValue("ComponentRef", d.EvaluatorRef) != nil || d.GoalRevision < 0 || d.GoalRevision > 9007199254740991 {
		return failure("invalid_argument", "authenticated exact maintenance defect required")
	}
	if e := c.Ports.Authority.Current(ctx, caller, api.Access{Method: "evidence.defect.register", TargetID: d.EvaluatorRef.ID, TaskID: d.TaskID, ContentRefs: []api.ContentRef{d.ProofRef}, Components: []api.ComponentRef{d.RuleRef, d.EvaluatorRef}}, c.Ports.Clock.Now()); e != nil {
		return e
	}
	_, e := c.content(ctx, caller, d.ProofRef)
	return e
}
func (c *Coordinator) RegisterDefect(tx *durable.Tx, caller api.Caller, d Defect) error {
	if e := c.Repository.Read(tx); e != nil {
		return e
	}
	now, e := tx.Now()
	if e != nil {
		return e
	}
	if e = c.Ports.Authority.Current(tx.Context(), caller, api.Access{Method: "evidence.defect.register", TargetID: d.EvaluatorRef.ID, Components: []api.ComponentRef{d.RuleRef, d.EvaluatorRef}, ContentRefs: []api.ContentRef{d.ProofRef}}, now); e != nil {
		return e
	}
	key := gateKey(d.EvaluatorRef)
	if _, e = c.Repository.Gate(tx, key, false, true); e != nil {
		return e
	}
	old, e := c.Repository.Get(tx, "defect", d.ID)
	if e != nil {
		return e
	}
	if old != nil {
		if Hash(old.Data) != Hash(Raw(d)) {
			return failure("idempotency_conflict", "defect identity has another original scope")
		}
		return nil
	}
	if e = c.Repository.PutGlobal(tx, key, rec("defect", d.ID, key, 1, "open", true, d)); e != nil {
		return e
	}
	impact := DefectImpact{Defect: d, UpperBound: now.UnixMilli(), LastCreatedAt: now.UnixMilli() + 1}
	if e = c.Repository.PutGlobal(tx, key, rec("defect_impact", d.ID, key, 1, "open", false, impact)); e != nil {
		return e
	}
	if e = c.Repository.AdvanceGate(tx, key); e != nil {
		return e
	}
	_, e = tx.Raise(workKey(WorkRef{Kind: "verify", ObjectKind: "defect", ObjectID: d.ID}), d.ID, now)
	return e
}
func (c *Coordinator) impactDefect(ctx context.Context, u Unit, f frame) error {
	return u.Step(ctx, func() error {
		return outcome(u.Within(ctx, func(tx *durable.Tx) error {
			if e := c.Repository.Read(tx); e != nil {
				return e
			}
			r, e := c.Repository.Get(tx, "defect_impact", f.Ref.ObjectID)
			if e != nil {
				return e
			}
			impact, e := Decode[DefectImpact](r)
			if e != nil {
				return e
			}
			tasks, e := c.Repository.List(tx, impact.UpperBound, impact.LastCreatedAt, impact.LastTaskID, min(c.Config.Limits.Page, 8))
			if e != nil {
				return e
			}
			if len(tasks) == 0 {
				key := gateKey(impact.Defect.EvaluatorRef)
				if _, e = c.Repository.Gate(tx, key, false, false); e != nil {
					return e
				}
				if e = c.Repository.PutGlobal(tx, key, rec("defect_impact", impact.Defect.ID, key, r.Revision+1, "closed", false, impact)); e != nil {
					return e
				}
				_, e = tx.Finish(u.Claim(), durable.Done())
				return e
			}
			first := tasks[0]
			ch, _, e := c.load(tx, first.Task.TaskID, false, tasks)
			if e != nil {
				return e
			}
			key := gateKey(impact.Defect.EvaluatorRef)
			if _, e = c.Repository.Gate(tx, key, false, false); e != nil {
				return e
			}
			for _, original := range tasks {
				t := ch.Tasks[original.Task.TaskID]
				d := impact.Defect
				if d.TaskID != "" && d.TaskID != t.Task.TaskID || d.GoalRevision != 0 && d.GoalRevision != t.Task.GoalRevision {
					continue
				}
				checks, e := c.Repository.Records(tx, Filter{TaskID: t.Task.TaskID, Kind: "check", Current: "@current", Limit: c.Config.Limits.Checks + 1})
				if e != nil {
					return e
				}
				if len(checks) > c.Config.Limits.Checks {
					return failure("precondition_failed", "defect impact checks are incomplete")
				}
				hit := false
				for _, row := range checks {
					check, e := Decode[api.CheckEvidence](&row)
					if e != nil {
						return e
					}
					hit = hit || Hash(check.RuleRef) == Hash(d.RuleRef) && Hash(check.Result.EvaluatorRef) == Hash(d.EvaluatorRef) && (d.ArtifactHash == "" || d.ArtifactHash == check.Result.ArtifactRef.Hash)
				}
				coverage, e := c.Repository.Records(tx, Filter{TaskID: t.Task.TaskID, Kind: "coverage", Current: "coverage", Limit: 2})
				if e != nil {
					return e
				}
				for _, row := range coverage {
					v, e := Decode[api.CoverageEvidence](&row)
					if e != nil {
						return e
					}
					hit = hit || Hash(v.RuleRef) == Hash(d.RuleRef) && Hash(v.EvaluatorRef) == Hash(d.EvaluatorRef)
				}
				if hit {
					if terminal(t) {
						if e = c.Repository.Put(tx, t, rec("result_notice", d.ID, t.Task.TaskID, 1, "closed", true, d)); e != nil {
							return e
						}
					} else {
						queue(ch, t, "verify", "task", t.Task.TaskID)
					}
				}
			}
			last := tasks[len(tasks)-1]
			impact.LastCreatedAt = last.CreatedAt
			impact.LastTaskID = last.Task.TaskID
			if e = c.Repository.PutGlobal(tx, key, rec("defect_impact", impact.Defect.ID, key, r.Revision+1, "open", false, impact)); e != nil {
				return e
			}
			claim := u.Claim()
			return c.flush(tx, ch, &claim, durable.Waiting(ch.Now.Add(c.Config.Limits.Backoff), "defect_impact_next_page"))
		}))
	})
}
