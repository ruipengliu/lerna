package task

import (
	"context"
	"fmt"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/runtime"
)

type OperationFact struct {
	Ref            api.ObjectRef
	Purpose        string
	Closed         bool
	Effect         string
	MayApplyLater  bool
	SourceRevision uint64
	SourceDigest   string
}
type ContextOperation struct {
	Intent OperationIntent
	Fact   OperationFact
}
type ContextFacts struct {
	Task                  api.Task
	FactRefs              []api.ObjectRef
	Operations            []ContextOperation
	Checks                []api.ConditionResult
	Artifacts             []api.ContentRef
	UnresolvedCollections []api.CollectionSummary
	SourceRefs            []api.SourceEvidence
	Delegations           []Delegation
}

// ContextFacts 在一次有界原库读取中保留全部未结关系，不从UI页重构Snapshot。
func (s *Service) ContextFacts(ctx context.Context, store runtime.Store, scope runtime.Scope, auth runtime.Auth, taskID string) (ContextFacts, error) {
	out := ContextFacts{FactRefs: []api.ObjectRef{}, Operations: []ContextOperation{}, Checks: []api.ConditionResult{}, Artifacts: []api.ContentRef{}, UnresolvedCollections: []api.CollectionSummary{}, SourceRefs: []api.SourceEvidence{}, Delegations: []Delegation{}}
	err := s.transaction(ctx, store, scope, func(tx runtime.Tx) error {
		t, e := getTask(ctx, tx, taskID)
		if e != nil {
			return e
		}
		if e = principal(auth, t); e != nil {
			return e
		}
		out.Task = t.Task
		out.Artifacts = t.CurrentArtifactRefs
		out.SourceRefs = t.SourceRefs
		rows, e := s.fullRelations(ctx, tx, taskID)
		if e != nil {
			return e
		}
		unresolvedDelegations := uint64(0)
		for _, r := range rows {
			out.FactRefs = append(out.FactRefs, r.Ref)
			switch r.Kind {
			case "operation":
				var intent OperationIntent
				if _, e = tx.Get(ctx, intents, r.Ref.ObjectID, &intent); e != nil {
					return e
				}
				out.Operations = append(out.Operations, ContextOperation{Intent: intent, Fact: OperationFact{Ref: r.Ref, Purpose: r.Purpose, Closed: r.Closed, Effect: r.Effect, MayApplyLater: r.MayApplyLater, SourceRevision: r.SourceRevision, SourceDigest: r.SourceDigest}})
			case "delegation":
				var d Delegation
				if _, e = tx.Get(ctx, delegations, r.Ref.ObjectID, &d); e != nil {
					return e
				}
				out.Delegations = append(out.Delegations, d)
				if !d.GoalWorkClosed || !d.EffectsClosed {
					unresolvedDelegations++
				}
			}
		}
		for _, requirement := range t.Task.Requirements {
			key := t.Task.TaskID + "/" + fmt.Sprintf("%020d", t.Task.GoalRevision) + "/" + requirement.RequirementID
			var selection selectedCheck
			if _, e = tx.Get(ctx, selections, key, &selection); confirmedNotFound(e) {
				continue
			} else if e != nil {
				return e
			}
			var result api.ConditionResult
			if e = tx.GetVersion(ctx, checks, selection.Ref.ObjectID, selection.Ref.Revision, &result); e != nil {
				return e
			}
			out.Checks = append(out.Checks, result)
			out.FactRefs = append(out.FactRefs, selection.Ref)
		}
		out.UnresolvedCollections = []api.CollectionSummary{t.Task.OpenEffects, {CollectionRevision: t.RelationRevision, TotalCount: uint64(len(out.Delegations)), UnresolvedCount: unresolvedDelegations, Complete: true}}
		return nil
	})
	return out, err
}
