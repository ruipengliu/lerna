package updates

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"lerna/authorization"
	"lerna/tasks"
)

//go:embed testdata/08-runtime.gob
var oldRuntime []byte

//go:embed testdata/08-ref.json
var oldRef []byte

func upgrade(ctx context.Context) error {
	h, e := fresh(ctx, tasks.GenerationLimits{Requests: 1, InputTokens: 600, OutputTokens: 100})
	if e != nil {
		return e
	}
	defer h.destroy()
	if e = h.auth.UpdateRuntime(ctx, func(tx authorization.RuntimeTransaction) error { tx.SetData(oldRuntime); return nil }); e != nil {
		return e
	}
	var ref tasks.Ref
	if e = json.Unmarshal(oldRef, &ref); e != nil {
		return e
	}
	s, e := h.core.Load(ctx, ref)
	if e != nil {
		return e
	}
	if len(s.Interactions) != 0 || len(s.Task.InputFacts) != 0 || s.UpdateVersion != 0 || s.UpdateLimits != (tasks.UpdateLimits{}) || s.Task.GoalRef != "" || s.Task.GuidanceRef != "" || len(s.Generations) != 1 || s.Task.ModelReservedTokens != 700 || s.Task.Attempts != 1 || len(s.Task.InputRefs) != 1 || !s.Work[0].InFlight {
		return fmt.Errorf("08 facts altered by upgrade")
	}
	op, e := h.operation(ctx)
	if e != nil {
		return e
	}
	in := tasks.AdjustRequest{OperationID: op, Ref: ref, ExpectedVersion: s.Task.Version, Patch: tasks.LimitPatch{Tokens: tasks.LimitValue{Present: true, Value: 800}}}
	r, e := h.inputs.AdjustLimits(ctx, in)
	if e != nil {
		return e
	}
	other, e := open(ctx, h.root, h.token, h.location, tasks.GenerationLimits{Requests: 1, InputTokens: 600, OutputTokens: 100})
	if e != nil {
		return e
	}
	defer other.close()
	replay, e := other.inputs.AdjustLimits(ctx, in)
	if e != nil {
		return e
	}
	now, e := other.core.Load(ctx, ref)
	return demand(e == nil && replay == r && now.Task.ModelReservedTokens == 700 && now.Generations[0].Qualification == s.Generations[0].Qualification && now.Generations[0].OutputOperation == s.Generations[0].OutputOperation && now.Work[0].DecisionVersion == s.Work[0].DecisionVersion && now.Limits == s.Limits && now.Task.InputRefs[0] == s.Task.InputRefs[0])
}
