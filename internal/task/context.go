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

// OperationIntentTx 只读本方已耐久准入的准确意图，供受信机械编码装配核对。
func (s *Service) OperationIntentTx(ctx context.Context, tx runtime.Tx, operationID string) (OperationIntent, error) {
	var intent OperationIntent
	if _, err := tx.Get(ctx, intents, operationID, &intent); err != nil {
		return intent, err
	}
	if intent.OperationID != operationID || intent.TaskRef.TenantID != tx.Scope().TenantID || intent.TaskRef.OwnerID != tx.Scope().OwnerID {
		return OperationIntent{}, api.E("forbidden", "operation_intent_scope_mismatch")
	}
	return intent, nil
}

func (s *Service) ReadOperationIntent(ctx context.Context, store runtime.Store, scope runtime.Scope, auth runtime.Auth, operationID string) (OperationIntent, error) {
	var intent OperationIntent
	if _, err := store.Read(ctx, scope, intents, operationID, 1, &intent); err != nil {
		return intent, err
	}
	if _, err := s.readState(ctx, store, scope, auth, intent.TaskRef.ObjectID, 0); err != nil {
		return OperationIntent{}, err
	}
	if intent.OperationID != operationID || intent.TaskRef.TenantID != scope.TenantID || intent.TaskRef.OwnerID != scope.OwnerID {
		return OperationIntent{}, api.E("forbidden", "operation_intent_scope_mismatch")
	}
	return intent, nil
}

// CheckDecisionTx 核已准入的原 Decision 与当前目标/控制，不给模型新的准入权。
func (s *Service) CheckDecisionTx(ctx context.Context, tx runtime.Tx, auth runtime.Auth, decisionID string) error {
	if !auth.HasRole("service") && !auth.HasRole("task_admin") {
		return api.E("forbidden", "trusted_brain_gate_required")
	}
	var d decisionState
	if _, err := tx.Get(ctx, decisions, decisionID, &d); err != nil {
		return err
	}
	if d.Consumed {
		return api.E("invalid_state", "decision_consumed")
	}
	t, err := getTask(ctx, tx, d.Intent.TaskRef.ObjectID)
	if err != nil {
		return err
	}
	if err = s.CheckCurrent(ctx, tx, t, true); err != nil {
		return err
	}
	if t.PendingCompletionID != "" {
		return api.E("invalid_state", "completion_checks_pending")
	}
	if d.Intent.TaskRef.TenantID != tx.Scope().TenantID || d.Intent.TaskRef.OwnerID != tx.Scope().OwnerID || d.Snapshot.GoalRevision != t.Task.GoalRevision || d.Snapshot.ControlRevision != t.Task.ControlRevision || !api.Equal(d.Snapshot.GoalRef, t.Task.GoalRef) || d.Snapshot.RequirementsDigest != t.Task.RequirementsDigest || !api.Equal(d.Snapshot.PolicyRef, t.Task.PolicyRef) {
		return api.E("revision_conflict", "decision_control_stale")
	}
	if d.Snapshot.Purpose == "decide" && t.Task.RequirementsState != "ready" || t.Task.RequirementsState == "awaiting_input" {
		return api.E("invalid_state", "requirements_not_ready")
	}
	return nil
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
