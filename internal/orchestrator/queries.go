package orchestrator

import (
	"context"
	"encoding/json"
	"sort"
	"time"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/contracts"
	"github.com/ruipengliu/lerna/internal/durable"
)

func (c *Coordinator) Query(tx *durable.Tx, caller api.Caller, q api.Query, generation string) (api.QueryResult, error) {
	if err := c.sameScope(tx); err != nil {
		return api.QueryResult{}, err
	}
	if err := c.Repository.Read(tx); err != nil {
		return api.QueryResult{}, err
	}
	now, err := tx.Now()
	if err != nil {
		return api.QueryResult{}, err
	}
	if err = c.Ports.Authority.Current(tx.Context(), caller, api.Access{Method: q.Method, TargetID: q.TargetID}, now); err != nil {
		return api.QueryResult{}, err
	}
	var value any
	gaps := []string{}
	switch q.Method {
	case "task.read", "task.result":
		t, err := c.Repository.Task(tx, q.TargetID)
		if err != nil {
			return api.QueryResult{}, err
		}
		if t == nil {
			return api.QueryResult{}, failure("not_found", "original task is absent")
		}
		view, missing, err := c.project(tx.Context(), caller, t, now)
		if err != nil {
			return api.QueryResult{}, err
		}
		gaps = append(gaps, missing...)
		if q.Method == "task.read" {
			value = view
		} else if t.Task.Status == "succeeded" {
			r, err := c.Repository.Get(tx, "result", t.Task.TaskID)
			if err != nil {
				return api.QueryResult{}, err
			}
			result, err := Decode[api.Result](r)
			if err != nil {
				return api.QueryResult{}, failure("precondition_failed", "fixed result is unavailable")
			}
			value = api.TaskResultView{Status: "succeeded", Result: &result}
			notices, e := c.Repository.Records(tx, Filter{TaskID: t.Task.TaskID, Kind: "result_notice", Limit: 1})
			if e != nil {
				return api.QueryResult{}, e
			}
			if len(notices) > 0 {
				gaps = append(gaps, "result_evidence_notice")
			}
		} else {
			reason := t.FailureReason
			if reason == "" {
				reason = "goal_work_pending"
			}
			effects := append([]string{}, view.OpenEffects...)
			open := view.AccountingOpen
			value = api.TaskResultView{Status: view.Status, Reason: &reason, UnresolvedOperationIDs: &effects, AccountingOpen: &open}
		}
	case "task.list":
		return c.list(tx, caller, q, generation, now)
	case "interaction.request_read":
		var in api.InteractionRequestReadInput
		_ = json.Unmarshal(q.Payload, &in)
		if q.TargetID != c.Config.Scope.OwnerID || in.RequestRef.OwnerID != c.Config.Scope.OwnerID {
			return api.QueryResult{}, failure("invalid_argument", "request must be read from its exact original owner")
		}
		r, err := c.Repository.Get(tx, "input", in.RequestRef.ID)
		if err != nil {
			return api.QueryResult{}, err
		}
		v, err := Decode[InputState](r)
		if err != nil {
			return api.QueryResult{}, err
		}
		if v.View.Revision != in.RequestRef.Revision {
			return api.QueryResult{}, failure("revision_conflict", "request reference is not the current exact revision")
		}
		if err = c.Ports.Authority.Current(tx.Context(), caller, api.Access{Method: q.Method, TargetID: in.RequestRef.ID, TaskID: r.TaskID, ContentRefs: append([]api.ContentRef{v.View.QuestionRef}, v.View.RequiredContentRefs...)}, now); err != nil {
			return api.QueryResult{}, err
		}
		value = api.InteractionRequestReadOutput{Request: v.View, Gaps: []string{}}
	case "budget.read":
		var in api.BudgetReadInput
		_ = json.Unmarshal(q.Payload, &in)
		if in.Role == "owner" {
			r, err := c.Repository.Get(tx, "allocation", q.TargetID)
			if err != nil {
				return api.QueryResult{}, err
			}
			v, err := Decode[AllocationState](r)
			if err != nil {
				return api.QueryResult{}, err
			}
			if err = c.Ports.Authority.Current(tx.Context(), caller, api.Access{Method: q.Method, TargetID: q.TargetID, TaskID: r.TaskID}, now); err != nil {
				return api.QueryResult{}, err
			}
			value = api.BudgetReadOutput{Role: "owner", Allocation: &v.Allocation}
		} else {
			v, err := c.Repository.Receiver(tx, q.TargetID)
			if err != nil {
				return api.QueryResult{}, err
			}
			if v == nil {
				return api.QueryResult{}, failure("not_found", "original receiver gate is absent")
			}
			value = api.BudgetReadOutput{Role: "receiver", Receiver: &v.Receiver}
		}
	default:
		return api.QueryResult{}, failure("unsupported", "query is not implemented")
	}
	raw := Raw(value)
	m, _ := contracts.Method(q.Method)
	if contracts.Validate(m.Output, raw) != nil {
		return api.QueryResult{}, durable.ErrInvariant
	}
	return api.QueryResult{Value: raw, Gaps: gaps}, nil
}
func (c *Coordinator) project(ctx context.Context, caller api.Caller, t *TaskState, now time.Time) (api.Task, []string, error) {
	view := t.Task
	view.WaitReasons = []api.WaitReason{}
	gaps := []string{}
	if err := c.Ports.Authority.Current(ctx, caller, api.Access{Method: "task.read", TargetID: t.Task.TaskID, TaskID: t.Task.TaskID, PolicyRef: &t.Task.PolicyRef, ContentRefs: []api.ContentRef{t.Task.GoalRef}}, now); err != nil {
		return view, gaps, err
	}
	for _, wait := range t.Task.WaitReasons {
		if wait.Kind == "input" {
			if wait.ObjectRef == nil {
				return view, gaps, durable.ErrInvariant
			}
			if err := c.Ports.Authority.Current(ctx, caller, api.Access{Method: "interaction.request_read", TargetID: wait.ObjectRef.ID, TaskID: t.Task.TaskID}, now); err != nil {
				gaps = append(gaps, "input_request_disclosure_incomplete")
				continue
			}
		}
		view.WaitReasons = append(view.WaitReasons, wait)
	}
	return view, gaps, nil
}
func (c *Coordinator) list(tx *durable.Tx, caller api.Caller, q api.Query, generation string, now time.Time) (api.QueryResult, error) {
	if q.TargetID != c.Config.Scope.OwnerID || generation == "" {
		return api.QueryResult{}, failure("precondition_failed", "complete current disclosure scope is required")
	}
	var in api.TaskListInput
	_ = json.Unmarshal(q.Payload, &in)
	if in.Limit > int64(c.Config.Limits.Page) {
		return api.QueryResult{}, failure("invalid_argument", "page limit exceeds the configured finite profile")
	}
	statuses := []string{}
	if in.Statuses != nil {
		statuses = append(statuses, (*in.Statuses)...)
		sort.Strings(statuses)
	}
	filter := Hash(statuses)
	upper := stamp(now)
	last := now.UnixMilli()
	lastID := ""
	if in.Cursor != nil {
		upper = in.Cursor.UpperBound
		u, err := time.Parse(time.RFC3339Nano, upper)
		if err != nil || u.After(now) {
			return api.QueryResult{}, failure("invalid_argument", "cursor upper bound is invalid")
		}
		parsed, err := time.Parse(time.RFC3339Nano, in.Cursor.LastCreatedAt)
		if err != nil || parsed.After(u) {
			return api.QueryResult{}, failure("invalid_argument", "cursor scan key is invalid")
		}
		last, lastID = parsed.UnixMilli(), in.Cursor.LastTaskID
	}
	id := ID("query", caller.ActorID, filter, generation, upper)
	gate := "query-" + id
	if _, err := c.Repository.Gate(tx, gate, false, true); err != nil {
		return api.QueryResult{}, err
	}
	row, err := c.Repository.Get(tx, "query", id)
	if err != nil {
		return api.QueryResult{}, err
	}
	if in.Cursor != nil {
		scope, err := Decode[QueryScope](row)
		if err != nil || scope.ActorID != caller.ActorID || scope.Generation != generation || scope.FilterDigest != filter || scope.UpperBound != upper {
			return api.QueryResult{}, failure("precondition_failed", "cursor disclosure scope changed or original query is absent")
		}
	} else {
		scope := QueryScope{ActorID: caller.ActorID, Generation: generation, FilterDigest: filter, UpperBound: upper}
		if err = c.Repository.PutGlobal(tx, gate, rec("query", id, "", 1, "closed", true, scope)); err != nil {
			return api.QueryResult{}, err
		}
	}
	u, _ := time.Parse(time.RFC3339Nano, upper)
	candidates, err := c.Repository.List(tx, u.UnixMilli(), last, lastID, c.Config.Limits.Scan)
	if err != nil {
		return api.QueryResult{}, err
	}
	out := api.TaskListOutput{OrchestratorID: c.Config.Scope.OwnerID, UpperBound: upper, Items: []api.TaskListOutputItemsItem{}, Gaps: []string{}}
	scanned := 0
	for _, t := range candidates {
		scanned++
		last, lastID = t.CreatedAt, t.Task.TaskID
		matches := len(statuses) == 0
		for _, status := range statuses {
			matches = matches || t.Task.Status == status
		}
		if !matches {
			continue
		}
		view, gaps, err := c.project(tx.Context(), caller, t, now)
		if err != nil {
			out.Gaps = append(out.Gaps, "task_disclosure_incomplete")
			continue
		}
		out.Items = append(out.Items, api.TaskListOutputItemsItem{CreatedAt: stamp(time.UnixMilli(t.CreatedAt)), Task: view})
		out.Gaps = append(out.Gaps, gaps...)
		if int64(len(out.Items)) == in.Limit {
			break
		}
	}
	if scanned < len(candidates) || len(candidates) == c.Config.Limits.Scan {
		out.NextCursor = &api.RuntimeTaskListCursor{UpperBound: upper, LastCreatedAt: stamp(time.UnixMilli(last)), LastTaskID: lastID}
		if len(out.Items) < int(in.Limit) {
			out.Gaps = append(out.Gaps, "finite_scan_budget_exhausted")
		}
	}
	if validateValue("TaskListOutput", out) != nil {
		return api.QueryResult{}, durable.ErrInvariant
	}
	return api.QueryResult{Value: Raw(out), Gaps: append([]string{}, out.Gaps...)}, nil
}
