package tasks

import (
	"context"
	"fmt"
	"strings"

	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

type completionStore interface {
	SaveVerification(context.Context, *v1.Verification) error
	LoadVerification(context.Context, *v1.Ref) (*v1.Verification, error)
	AllVerifications(context.Context) ([]*v1.Verification, error)
	SaveResult(context.Context, *v1.Result) error
	LoadResult(context.Context, *v1.GlobalName) (*v1.Result, error)
}
type CompletionFacts interface {
	QueryOperation(context.Context, *v1.Caller, *v1.GlobalName) (*v1.Operation, error)
	QueryCompletionSeal(context.Context, *v1.Caller, *v1.Ref) (*v1.CompletionSeal, error)
	QueryObservation(context.Context, *v1.Caller, *v1.Ref) (*v1.RawObservation, error)
	QueryInterpretation(context.Context, *v1.Caller, *v1.Ref) (*v1.EffectInterpretation, error)
	QueryReconciliationFinding(context.Context, *v1.Caller, *v1.Ref) (*v1.ReconciliationFinding, error)
	QueryReconciliationQuery(context.Context, *v1.Caller, *v1.Ref) (*v1.ReconciliationQuery, error)
}
type CompletionBudget interface {
	QueryBudget(context.Context, *v1.Caller, *v1.GlobalName) (*v1.Budget, error)
}

func (s *Service) WithCompletion(f CompletionFacts, b CompletionBudget) *Service {
	s.completionFacts = f
	s.completionBudget = b
	return s
}

// BeginCompletion 以递增后的控制代次固定本轮裁决依据；提议本身没有关闭权。
func (s *Service) BeginCompletion(ctx context.Context, caller *v1.Caller, c *v1.BeginCompletionCommand) (*v1.CommandReceipt, error) {
	if e := command.ValidateHeader(c.GetHeader(), c); e != nil {
		return nil, e
	}
	return s.decisions.Execute(ctx, caller, c.Header, command.SemanticFingerprint("begin-completion", c.TaskId, c.ProposalRef), "tasks.verification", func(tx context.Context) (*v1.Ref, error) {
		t, e := s.QueryTask(tx, caller, c.TaskId)
		if e != nil {
			return nil, e
		}
		if t == nil {
			return nil, command.Fail("NOT_FOUND")
		}
		p, e := s.store.LoadPlanning(tx, t.TaskId)
		if e != nil {
			return nil, e
		}
		_, now, e := s.store.Position(tx)
		if e != nil {
			return nil, e
		}
		q, snap := p.Proposal, p.Snapshot
		if q == nil || snap == nil || q.Kind != "COMPLETE" || p.ProposalConsumed || !proto.Equal(c.ProposalRef, q.Ref) || !proto.Equal(q.RequestRef, snap.RequestRef) || !proto.Equal(q.ContextSnapshotRef, snap.Ref) || now >= snap.ExpiresAtUnixMs || q.PlanningGeneration != t.PlanningGeneration || q.PlanningGeneration != snap.PlanningGeneration {
			return nil, command.Fail("STALE_PROPOSAL")
		}
		if q.RequirementsVersion != t.RequirementsVersion || q.RequirementsVersion != snap.RequirementsVersion {
			return nil, command.Fail("STALE_REQUIREMENT")
		}
		if q.InputVersion != t.InputVersion || q.InputVersion != snap.InputVersion {
			return nil, command.Fail("STALE_INPUT")
		}
		if q.ControlGeneration != t.ControlGeneration || q.ControlGeneration != snap.ControlGeneration {
			return nil, command.Fail("STALE_GENERATION")
		}
		if t.ParentTaskRef != nil {
			return nil, command.Fail("UNSUPPORTED_FEATURE")
		}
		if t.Lifecycle != v1.TaskLifecycle_TASK_LIFECYCLE_OPEN || t.Control != v1.TaskControl_TASK_CONTROL_ACTIVE || p.VerificationFreeze != 0 {
			return nil, command.Fail("TASK_NOT_ACTIVE")
		}
		if t.RequirementsStatus != v1.RequirementsStatus_REQUIREMENTS_STATUS_ACCEPTED || t.BoundInputVersion != t.InputVersion || p.Requirements == nil {
			return nil, command.Fail("REQUIREMENTS_NOT_ACCEPTED")
		}
		t.ControlGeneration++
		t.Revision++
		p.VerificationRound++
		p.VerificationFreeze = p.VerificationRound
		p.ProposalConsumed = true
		v := &v1.Verification{Ref: command.NewRef(s.user, s.domain, "verification", "lerna.v1.Verification"), TaskId: t.TaskId, Round: p.VerificationRound, Status: "VERIFYING", RequirementsRef: p.Requirements.Ref, RequirementsVersion: t.RequirementsVersion, InputVersion: t.InputVersion, ControlGeneration: t.ControlGeneration, ProposalRef: q.Ref, AdmissionRefs: p.AdmissionRefs, Candidates: q.CompletionEvidence, StartedAtUnixMs: now}
		if e = s.addCompletionClosures(tx, v, v.AdmissionRefs, v.Ref); e != nil {
			return nil, e
		}
		p.VerificationRef = v.Ref
		setCompletionWaiting(t, []string{"COMPLETION:VERIFYING"})
		if e = s.saveVerification(tx, v); e != nil {
			return nil, e
		}
		if e = s.store.SavePlanning(tx, p); e != nil {
			return nil, e
		}
		return v.Ref, s.saveTask(tx, t)
	})
}
func (s *Service) QueryVerification(ctx context.Context, c *v1.Caller, r *v1.Ref) (*v1.Verification, error) {
	if r == nil || r.Revision == 0 || r.SchemaId != "lerna.v1.Verification" {
		return nil, command.Fail("INVALID_REFERENCE")
	}
	if e := command.CheckName(c, r.Name, s.user, s.domain, "verification"); e != nil {
		return nil, e
	}
	v, e := s.store.(completionStore).LoadVerification(ctx, r)
	if e == nil && v != nil && !proto.Equal(v.Ref, r) {
		return nil, command.Fail("INVALID_REFERENCE")
	}
	return v, e
}
func (s *Service) QueryResult(ctx context.Context, c *v1.Caller, id *v1.GlobalName) (*v1.Result, error) {
	if e := command.CheckName(c, id, s.user, s.domain, "task"); e != nil {
		return nil, e
	}
	r, e := s.store.(completionStore).LoadResult(ctx, id)
	if e != nil || r == nil {
		return r, e
	}
	for _, finding := range r.Conditions {
		if e = s.content.CheckUsable(ctx, c, finding.Condition.DescriptionRef); e != nil {
			return nil, e
		}
	}
	return r, nil
}
func setCompletionWaiting(t *v1.Task, gaps []string) {
	waits := make([]string, 0, len(t.WaitingOn)+len(gaps))
	for _, w := range t.WaitingOn {
		if !strings.HasPrefix(w, "COMPLETION:") {
			waits = append(waits, w)
		}
	}
	t.WaitingOn = append(waits, gaps...)
	t.Progress = v1.TaskProgress_TASK_PROGRESS_RUNNING
	if len(t.WaitingOn) > 0 {
		t.Progress = v1.TaskProgress_TASK_PROGRESS_WAITING
	}
}

// RecheckCompletion 只按当前轮次复核；旧回报不能关闭任务或释放新冻结。
func (s *Service) RecheckCompletion(ctx context.Context, caller *v1.Caller, c *v1.RecheckCompletionCommand) (*v1.CommandReceipt, error) {
	if e := command.ValidateHeader(c.GetHeader(), c); e != nil {
		return nil, e
	}
	return s.decisions.Execute(ctx, caller, c.Header, command.SemanticFingerprint("recheck-completion", c.VerificationRef), "tasks.completion", func(tx context.Context) (*v1.Ref, error) {
		v, e := s.QueryVerification(tx, caller, c.VerificationRef)
		if e != nil {
			return nil, e
		}
		if v == nil {
			return nil, command.Fail("NOT_FOUND")
		}
		t, e := s.QueryTask(tx, caller, v.TaskId)
		if e != nil {
			return nil, e
		}
		p, e := s.store.LoadPlanning(tx, v.TaskId)
		if e != nil {
			return nil, e
		}
		if v.Status != "VERIFYING" || p.VerificationFreeze != v.Round || !proto.Equal(p.VerificationRef, v.Ref) || t.Lifecycle != v1.TaskLifecycle_TASK_LIFECYCLE_OPEN || t.Control != v1.TaskControl_TASK_CONTROL_ACTIVE {
			return nil, command.Fail("STALE_VERIFICATION")
		}
		if t.InputVersion != v.InputVersion || t.ControlGeneration != v.ControlGeneration || t.RequirementsVersion != v.RequirementsVersion || !proto.Equal(p.Requirements.GetRef(), v.RequirementsRef) {
			return nil, command.Fail("STALE_VERIFICATION")
		}
		if t.RequirementsStatus != v1.RequirementsStatus_REQUIREMENTS_STATUS_ACCEPTED || t.BoundInputVersion != t.InputVersion {
			return nil, command.Fail("REQUIREMENTS_NOT_ACCEPTED")
		}
		if t.ParentTaskRef != nil {
			return nil, command.Fail("UNSUPPORTED_FEATURE")
		}
		old := proto.Clone(v).(*v1.Verification)
		v.AdmissionRefs = p.AdmissionRefs
		v.OperationRefs = nil
		v.Gaps = nil
		v.Conditions = nil
		nextScope := proto.Clone(v.Ref).(*v1.Ref)
		nextScope.Revision++
		if e = s.addCompletionClosures(tx, v, p.AdmissionRefs, nextScope); e != nil {
			return nil, e
		}
		for _, ref := range v.ClosureIntentRefs {
			intent, e := s.store.(completionIntentStore).LoadCompletionIntent(tx, ref)
			if e != nil {
				return nil, e
			}
			if intent == nil {
				return nil, command.Fail("INVARIANT_VIOLATION")
			}
			if intent.RecipientReceipt == nil {
				v.Gaps = append(v.Gaps, "COMPLETION:CLOSURE:"+ref.Name.LocalId)
				continue
			}
			seal, e := s.completionFacts.QueryCompletionSeal(tx, caller, intent.RecipientReceipt.ResultRef)
			if e != nil {
				return nil, e
			}
			if seal == nil || !proto.Equal(seal.IntentRef, intent.Ref) || !proto.Equal(seal.VerificationRef, intent.Command.VerificationRef) || !proto.Equal(seal.AdmissionRef, intent.Command.AdmissionRef) || !proto.Equal(seal.OperationId, intent.Command.OperationId) || seal.ExecutorEndpointId != intent.Command.ExecutorEndpointId {
				return nil, command.Fail("INVALID_CLOSURE")
			}
		}
		ops := map[string]*v1.Operation{}
		admissions := map[string]*v1.Admission{}
		for _, ref := range v.AdmissionRefs {
			a, e := s.QueryAdmission(tx, caller, ref)
			if e != nil {
				return nil, e
			}
			if a == nil || !proto.Equal(a.TaskId, t.TaskId) {
				return nil, command.Fail("INVARIANT_VIOLATION")
			}
			h, e := s.store.LoadHandoff(tx, ref)
			if e != nil {
				return nil, e
			}
			op, e := s.completionFacts.QueryOperation(tx, caller, a.OperationId)
			if e != nil {
				return nil, e
			}
			key := a.OperationId.LocalId
			admissions[key] = a
			if op == nil || h == nil || h.RecipientReceipt == nil {
				v.Gaps = append(v.Gaps, "COMPLETION:HANDOFF:"+key)
				continue
			}
			if !proto.Equal(op.AdmissionRef, a.Ref) {
				return nil, command.Fail("INVARIANT_VIOLATION")
			}
			ops[key] = op
			v.OperationRefs = append(v.OperationRefs, op.Ref)
			if op.Lifecycle != "SETTLED" || op.Dispatch != "SEALED" || op.Effect == nil || op.Effect.LateEffect != "RULED_OUT" || op.Effect.EvidenceConflict {
				v.Gaps = append(v.Gaps, "COMPLETION:UNSETTLED:"+key)
			}
		}
		for _, condition := range p.Requirements.Conditions {
			finding, e := s.checkCondition(tx, caller, p.Requirements, condition, v.Candidates, admissions, ops)
			if e != nil {
				return nil, e
			}
			v.Conditions = append(v.Conditions, finding)
			if condition.Necessary && finding.Conclusion != "SATISFIED" {
				v.Gaps = append(v.Gaps, "COMPLETION:CONDITION:"+condition.ConditionId+":"+finding.Gap)
			}
		}
		disproved := false
		for _, finding := range v.Conditions {
			if finding.Condition.Necessary && finding.Conclusion == "UNSATISFIED" {
				disproved = true
			}
		}
		if disproved {
			_, now, e := s.store.Position(tx)
			if e != nil {
				return nil, e
			}
			v.Status = "REJECTED"
			v.EndedReason = "REQUIRED_CONDITION_NOT_MET"
			v.FinishedAtUnixMs = now
			v.Ref.Revision++
			p.VerificationRef = v.Ref
			p.VerificationFreeze = 0
			p.RejectedVerificationOperations = nil
			for _, ref := range v.AdmissionRefs {
				a, e := s.store.LoadAdmission(tx, ref)
				if e != nil {
					return nil, e
				}
				p.RejectedVerificationOperations = append(p.RejectedVerificationOperations, &v1.Ref{Name: a.OperationId, Revision: 1, SchemaId: "lerna.v1.Operation"})
			}
			t.Revision++
			setCompletionWaiting(t, append([]string{"COMPLETION:REPLAN_REQUIRED"}, v.Gaps...))
			if e = s.saveVerification(tx, v); e != nil {
				return nil, e
			}
			if e = s.store.SavePlanning(tx, p); e != nil {
				return nil, e
			}
			return v.Ref, s.saveTask(tx, t)
		}
		if len(v.Gaps) > 0 {
			setCompletionWaiting(t, v.Gaps)
			if proto.Equal(old, v) {
				return v.Ref, nil
			}
			v.Ref.Revision++
			p.VerificationRef = v.Ref
			t.Revision++
			if e = s.saveVerification(tx, v); e != nil {
				return nil, e
			}
			if e = s.store.SavePlanning(tx, p); e != nil {
				return nil, e
			}
			return v.Ref, s.saveTask(tx, t)
		}
		_, now, e := s.store.Position(tx)
		if e != nil {
			return nil, e
		}
		v.Status = "PASSED"
		v.Ref.Revision++
		v.FinishedAtUnixMs = now
		p.VerificationRef = v.Ref
		p.VerificationFreeze = 0
		result := &v1.Result{Ref: command.NewRef(s.user, s.domain, "result", "lerna.v1.Result"), TaskId: t.TaskId, Outcome: "SUCCEEDED", CloseReason: "VERIFICATION_PASSED", VerificationRef: v.Ref, RequirementsRef: v.RequirementsRef, Conditions: v.Conditions, OperationRefs: v.OperationRefs, ClosedAtUnixMs: now}
		result.UsageSnapshot, e = s.completionBudget.QueryBudget(tx, caller, t.TaskId)
		if e != nil {
			return nil, e
		}
		for _, ref := range v.AdmissionRefs {
			a, e := s.QueryAdmission(tx, caller, ref)
			if e != nil {
				return nil, e
			}
			result.ReservationRefs = append(result.ReservationRefs, a.BudgetBasis.ReservationRef)
			if op := ops[a.OperationId.LocalId]; op.Effect.Outcome == "UNKNOWN" {
				result.UnknownOperationRefs = append(result.UnknownOperationRefs, op.Ref)
			}
		}
		t.Lifecycle = v1.TaskLifecycle_TASK_LIFECYCLE_CLOSED
		t.ResultRef = result.Ref
		t.Revision++
		setCompletionWaiting(t, nil)
		if e = s.saveVerification(tx, v); e != nil {
			return nil, e
		}
		if e = s.saveResult(tx, result); e != nil {
			return nil, e
		}
		if e = s.store.SavePlanning(tx, p); e != nil {
			return nil, e
		}
		return result.Ref, s.saveTask(tx, t)
	})
}

func (s *Service) checkCondition(ctx context.Context, c *v1.Caller, r *v1.Requirements, condition *v1.Requirement, candidates []*v1.CompletionEvidence, admissions map[string]*v1.Admission, ops map[string]*v1.Operation) (*v1.ConditionFinding, error) {
	f := &v1.ConditionFinding{Condition: condition, Source: r.Source, SourceInputRef: r.SourceInputRef, Conclusion: "UNKNOWN", Gap: "EVIDENCE_MISSING"}
	if e := s.content.CheckUsable(ctx, c, condition.DescriptionRef); e != nil {
		return nil, e
	}
	if condition.VerificationRule != "TARGET_RECORD" || condition.RuleVersion != 1 {
		f.Gap = "RULE_UNSUPPORTED"
		return f, nil
	}
	scope := condition.TargetRecord
	if scope == nil || scope.CapabilityRef == nil || scope.ParametersRef == nil {
		f.Gap = "TARGET_SCOPE_MISSING"
		return f, nil
	}
	if e := s.content.CheckUsable(ctx, c, scope.ParametersRef); e != nil {
		return nil, e
	}
	if r.Source != "TRUSTED_TEMPLATE" && r.Source != "USER_EXPLICIT" {
		f.Gap = "SOURCE_UNTRUSTED"
		return f, nil
	}
	for _, candidate := range candidates {
		if candidate.ConditionId != condition.ConditionId {
			continue
		}
		a := admissions[candidate.OperationId.LocalId]
		op := ops[candidate.OperationId.LocalId]
		if a == nil || op == nil || !proto.Equal(candidate.OperationId, op.Ref.Name) || a.RequirementsVersion != r.RequirementsVersion || !proto.Equal(a.CapabilityRef, scope.CapabilityRef) || !proto.Equal(a.ParametersRef, scope.ParametersRef) {
			f.Gap = "EVIDENCE_SCOPE_MISMATCH"
			return f, nil
		}
		if op.Effect == nil || op.Effect.EvidenceConflict {
			return f, nil
		}
		for _, ref := range op.Effect.EvidenceRefs {
			raw, e := s.completionFacts.QueryObservation(ctx, c, ref)
			if e != nil {
				return nil, e
			}
			if raw == nil {
				continue
			}
			ir := &v1.Ref{Name: &v1.GlobalName{UserId: raw.Ref.Name.UserId, AuthorityDomainId: raw.Ref.Name.AuthorityDomainId, ObjectKind: "interpretation", LocalId: raw.Ref.Name.LocalId}, Revision: 1, SchemaId: "lerna.v1.EffectInterpretation"}
			proof, e := s.completionFacts.QueryInterpretation(ctx, c, ir)
			if e != nil {
				return nil, e
			}
			outcome := proof.GetOutcome()
			var evidence []*v1.Ref
			if raw.QuerySubject != nil {
				outcome, evidence, e = s.queryConditionEvidence(ctx, c, r, raw, proof, op, admissions, ops)
				if e != nil {
					return nil, e
				}
				if outcome == "" {
					continue
				}
			} else {
				if proof == nil || (proof.Rule != "reference-target-v1" && proof.Rule != "managed-file-v1") || (proof.Outcome != "APPLIED" && proof.Outcome != "NOT_APPLIED") || raw.Source != "TRUSTED_IO" || !proto.Equal(raw.OperationId, op.Ref.Name) || !proto.Equal(raw.TaskId, r.TaskId) || !proto.Equal(proof.ObservationRef, raw.Ref) {
					continue
				}
				if raw.Protocol == "FILE" && outcome == "APPLIED" && !durableFileCondition(raw) {
					continue
				}
				evidence = []*v1.Ref{proof.Ref, raw.Ref, raw.BodyRef}
			}
			if e = s.content.CheckUsable(ctx, c, raw.BodyRef); e != nil {
				return nil, e
			}
			if outcome == "NOT_APPLIED" && (proof.LateEffect != "RULED_OUT" || !allScopedActionsNotApplied(r, scope, admissions, ops)) {
				continue
			}
			f.Conclusion = "SATISFIED"
			f.Gap = ""
			if outcome == "NOT_APPLIED" {
				f.Conclusion = "UNSATISFIED"
				f.Gap = "REQUIRED_ACTION_NOT_APPLIED"
			}
			f.EvidenceRefs = evidence
			f.OperationRef = op.Ref
			return f, nil
		}
	}
	return f, nil
}

// ProcessCompletions 重读持久轮次与权威事实；唤醒只是提示，不持有第二份动作清单。
func (s *Service) ProcessCompletions(ctx context.Context, c *v1.Caller) error {
	if c.GetUserId() != s.user {
		return command.Fail("PERMISSION_DENIED")
	}
	for {
		if e := s.ProcessCompletionClosures(ctx, c); e != nil {
			return e
		}
		rounds, e := s.store.(completionStore).AllVerifications(ctx)
		if e != nil {
			return e
		}
		addedClosures := false
		for _, v := range rounds {
			if v.Status != "VERIFYING" {
				continue
			}
			actor := &v1.Caller{UserId: s.user, IssuerId: "tasks-completion"}
			r, e := s.RecheckCompletion(ctx, actor, &v1.RecheckCompletionCommand{Header: completionHeader(s.user, s.domain, actor.IssuerId, fmt.Sprintf("recheck:%s:%s", v.Ref.Name.LocalId, command.NewRef(s.user, s.domain, "command", "command").Name.LocalId)), VerificationRef: v.Ref})
			if e != nil {
				return e
			}
			if r.Error != nil && r.Error.Code != "STALE_VERIFICATION" {
				return &command.Failure{Detail: r.Error}
			}
			if r.ResultRef.GetSchemaId() == "lerna.v1.Verification" {
				updated, e := s.QueryVerification(ctx, actor, r.ResultRef)
				if e != nil {
					return e
				}
				if updated != nil && len(updated.ClosureIntentRefs) > len(v.ClosureIntentRefs) {
					addedClosures = true
				}
			}
		}
		if !addedClosures {
			return nil
		}
	}
}

func completionHeader(user, domain, issuer, id string) *v1.CommandHeader {
	return &v1.CommandHeader{Identity: &v1.CommandIdentity{UserId: user, TargetDomainId: domain, IssuerId: issuer, CommandId: id}, ContractVersion: 1, FingerprintVersion: 1, SchemaId: "lerna.v1.AdmissionCommands"}
}

func allScopedActionsNotApplied(r *v1.Requirements, scope *v1.TargetRecordAssertion, admissions map[string]*v1.Admission, ops map[string]*v1.Operation) bool {
	found := false
	for key, a := range admissions {
		if a.RequirementsVersion != r.RequirementsVersion || !proto.Equal(a.CapabilityRef, scope.CapabilityRef) || !proto.Equal(a.ParametersRef, scope.ParametersRef) {
			continue
		}
		found = true
		op := ops[key]
		if op == nil || op.Lifecycle != "SETTLED" || op.Effect == nil || op.Effect.Outcome != "NOT_APPLIED" || op.Effect.LateEffect != "RULED_OUT" || op.Effect.EvidenceConflict {
			return false
		}
	}
	return found
}

// durableFileCondition 区分已观察到发布与完成条件要求的耐久、读回双证据。
func durableFileCondition(raw *v1.RawObservation) bool {
	e := raw.GetFileEvidence()
	return e != nil && e.Rule == "managed-file-v1" && e.Published && e.DurabilityConfirmed && e.ReadbackVerified && e.Terminal && e.ErrorCode == "" && e.Commit != nil
}
