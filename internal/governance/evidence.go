package governance

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"time"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/runtime"
)

type DefectImpact struct {
	DefectID string `json:"defect_id"`
	Revision uint64 `json:"revision"`
	Cursor   string `json:"cursor,omitempty"`
}

func (s *Service) registerEvidence(r *runtime.Registry) error {
	for _, fn := range []func() error{
		func() error {
			return registerCommand[RuleDefinition, StateOutput](s, r, "evidence.rule.register", false, false, func(ctx context.Context, tx runtime.Tx, a runtime.Auth, c api.Command, in RuleDefinition) (runtime.Outcome, error) {
				if err := requireRole(a, "maintainer"); err != nil {
					return runtime.Outcome{}, err
				}
				ref, err := s.RegisterRuleTx(ctx, tx, in)
				return runtime.Applied(StateOutput{Ref: ref, State: "registered"}), err
			})
		},
		func() error {
			return registerCommand[ConditionCheck, StateOutput](s, r, "evidence.check.register", false, false, func(ctx context.Context, tx runtime.Tx, a runtime.Auth, c api.Command, in ConditionCheck) (runtime.Outcome, error) {
				if err := requireRole(a, "evidence_reporter"); err != nil {
					return runtime.Outcome{}, err
				}
				ref, err := s.RegisterCheckTx(ctx, tx, in)
				return runtime.Applied(StateOutput{Ref: ref, State: "registered"}), err
			})
		},
		func() error {
			return registerCommand[DefectRegister, DefectOutput](s, r, "evidence.defect.register", false, false, s.registerDefect)
		},
		func() error {
			return registerCommand[EligibilityRequest, EligibilityReceipt](s, r, "evidence.eligibility.check", false, false, s.eligibility)
		},
		func() error {
			return registerQuery[IDInput, ConditionCheck](r, "evidence.check.read", queryByID[ConditionCheck]("checks", nil))
		},
		func() error {
			return registerQuery[IDInput, EligibilityReceipt](r, "evidence.eligibility.read", queryByID[EligibilityReceipt]("eligibility", nil))
		},
		func() error {
			return registerQuery[IDInput, Defect](r, "evidence.defect.read", queryByID[Defect]("defects", nil))
		},
		func() error {
			return registerQuery[ChangesRequest, ChangesOutput](r, "evidence.defect.changes", s.changes)
		},
		func() error {
			return registerCommand[HolderAck, StateOutput](s, r, "evidence.holder.ack", false, false, s.ackHolder)
		},
		func() error {
			return registerQuery[api.ListInput, api.Page[ResultNotice]](r, "evidence.notice.list", queryPage[ResultNotice]("notices", "evidence_consumer"))
		},
	} {
		if err := fn(); err != nil {
			return err
		}
	}
	return nil
}
func (s *Service) RegisterRuleTx(ctx context.Context, tx runtime.Tx, in RuleDefinition) (api.ObjectRef, error) {
	if in.Kind != "effect" && in.Kind != "quality" {
		return api.ObjectRef{}, api.E("invalid_request", "invalid_rule_kind")
	}
	if in.Predicate != "historical_effect" && in.Predicate != "current_state" && in.Predicate != "quality" {
		return api.ObjectRef{}, api.E("invalid_request", "invalid_rule_predicate")
	}
	if in.RiskClass != "ordinary" && in.RiskClass != "high_impact" {
		return api.ObjectRef{}, api.E("invalid_request", "invalid_risk_class")
	}
	if in.Kind == "effect" && (in.AllowUserAcceptance || len(in.AllowedBasis) != 1 || in.AllowedBasis[0] != "verified") {
		return api.ObjectRef{}, api.E("invalid_request", "effect_requires_verified_basis")
	}
	if in.Predicate == "current_state" && in.MaxObservationAgeSeconds == 0 {
		return api.ObjectRef{}, api.E("invalid_request", "observation_age_required")
	}
	if in.MaxObservationAgeSeconds > 31536000 {
		return api.ObjectRef{}, api.E("invalid_request", "observation_age_out_of_range")
	}
	if !subset(in.AllowedBasis, []string{"verified", "assessed", "user_accepted"}) {
		return api.ObjectRef{}, api.E("invalid_request", "invalid_rule_basis")
	}
	id := componentKey(in.ComponentRef)
	var old RuleDefinition
	_, err := tx.Get(ctx, ns("rules"), id, &old)
	if err == nil {
		if !api.Equal(old, in) {
			return api.ObjectRef{}, api.E("idempotency_conflict", "rule_definition_changed")
		}
		return tx.Scope().Ref(id, 1), nil
	}
	if !errMissing(err) {
		return api.ObjectRef{}, err
	}
	if err = tx.Create(ctx, ns("rules"), id, "", in); err != nil {
		return api.ObjectRef{}, err
	}
	if _, err = s.ensureGate(ctx, tx, in.ComponentRef); err != nil {
		return api.ObjectRef{}, err
	}
	return tx.Scope().Ref(id, 1), nil
}
func (s *Service) ensureHead(ctx context.Context, tx runtime.Tx) (AuthorityHead, error) {
	id := digestID("authority", tx.Scope().OwnerID)
	var head AuthorityHead
	_, err := tx.Get(ctx, ns("authority"), id, &head)
	if errMissing(err) {
		head = AuthorityHead{ID: id, Revision: 1, Epoch: 1}
		err = tx.Create(ctx, ns("authority"), id, "", head)
	}
	return head, err
}
func (s *Service) ensureGate(ctx context.Context, tx runtime.Tx, implementation api.ComponentRef) (EvidenceGate, error) {
	id := componentKey(implementation)
	var gate EvidenceGate
	_, err := tx.Get(ctx, ns("evidence_gates"), id, &gate)
	if errMissing(err) {
		gate = EvidenceGate{ID: id, Revision: 1, ImplementationRef: implementation, AuthorityEpoch: 1}
		err = tx.Create(ctx, ns("evidence_gates"), id, "", gate)
	}
	return gate, err
}

func (s *Service) RegisterCheckTx(ctx context.Context, tx runtime.Tx, check ConditionCheck) (api.ObjectRef, error) {
	if check.AuthorityID != tx.Scope().OwnerID || check.Revision != 1 || !api.ValidID(check.CheckID) || !api.ValidID(check.TaskID) || check.GoalRevision == 0 || check.RequirementRevision == 0 {
		return api.ObjectRef{}, api.E("invalid_request", "check_binding_invalid")
	}
	if check.ArtifactRef.TenantID != tx.Scope().TenantID || check.ScopeRef.TenantID != tx.Scope().TenantID || check.ReportRef.TenantID != tx.Scope().TenantID {
		return api.ObjectRef{}, api.E("forbidden", "check_scope_mismatch")
	}
	if check.Verdict != "pass" && check.Verdict != "fail" && check.Verdict != "unknown" {
		return api.ObjectRef{}, api.E("invalid_request", "invalid_verdict")
	}
	if len(check.DependentCheckRefs) > 128 {
		return api.ObjectRef{}, api.E("invalid_request", "dependency_limit")
	}
	var rule RuleDefinition
	if err := tx.GetVersion(ctx, ns("rules"), componentKey(check.RuleRef), 1, &rule); err != nil {
		return api.ObjectRef{}, err
	}
	if !contains(rule.AllowedBasis, check.Basis) || check.Basis == "user_accepted" && !rule.AllowUserAcceptance {
		return api.ObjectRef{}, api.E("invalid_request", "basis_not_allowed")
	}
	if _, err := api.ParseTime(check.CheckedAt); err != nil {
		return api.ObjectRef{}, api.E("invalid_request", "invalid_check_time")
	}
	if check.Verdict == "pass" && check.ObservedAt == "" {
		return api.ObjectRef{}, api.E("invalid_request", "observation_required")
	}
	var old ConditionCheck
	_, err := tx.Get(ctx, ns("checks"), check.CheckID, &old)
	if err == nil {
		if !api.Equal(old, check) {
			return api.ObjectRef{}, api.E("idempotency_conflict", "check_identity_changed")
		}
		return tx.Scope().Ref(check.CheckID, 1), nil
	}
	if !errMissing(err) {
		return api.ObjectRef{}, err
	}
	implementations := []api.ComponentRef{check.RuleRef, check.EvaluatorRef}
	sort.Slice(implementations, func(i, j int) bool { return componentKey(implementations[i]) < componentKey(implementations[j]) })
	for _, impl := range implementations {
		if _, err = s.ensureGate(ctx, tx, impl); err != nil {
			return api.ObjectRef{}, err
		}
	}
	if err = tx.Create(ctx, ns("checks"), check.CheckID, check.TaskID, check); err != nil {
		return api.ObjectRef{}, err
	}
	if _, _, err = s.dependencies(ctx, tx, tx.Scope().Ref(check.CheckID, 1)); err != nil {
		return api.ObjectRef{}, err
	}
	return tx.Scope().Ref(check.CheckID, 1), nil
}

// dependencies 遍历完整 DAG，不以一页或根 pass 代替全部子证据。
func (s *Service) dependencies(ctx context.Context, tx runtime.Tx, root api.ObjectRef) ([]ConditionCheck, string, error) {
	seen := map[string]bool{}
	active := map[string]bool{}
	checks := []ConditionCheck{}
	refs := []api.ObjectRef{}
	var visit func(api.ObjectRef, int) error
	visit = func(ref api.ObjectRef, depth int) error {
		if depth > 8 {
			return api.E("invalid_request", "dependency_depth_exceeded")
		}
		if ref.TenantID != tx.Scope().TenantID {
			return api.E("forbidden", "reference_scope_mismatch")
		}
		if ref.OwnerID != tx.Scope().OwnerID {
			return api.E("unsupported", "unsupported_multi_authority")
		}
		key := refKey(ref)
		if active[key] {
			return api.E("invalid_request", "dependency_cycle")
		}
		if seen[key] {
			return nil
		}
		if len(seen) >= 128 {
			return api.E("invalid_request", "dependency_limit")
		}
		var check ConditionCheck
		if err := tx.GetVersion(ctx, ns("checks"), ref.ObjectID, ref.Revision, &check); err != nil {
			return api.E("invalid_state", "dependency_incomplete")
		}
		if check.AuthorityID != tx.Scope().OwnerID {
			return api.E("unsupported", "unsupported_multi_authority")
		}
		seen[key] = true
		active[key] = true
		for _, dep := range check.DependentCheckRefs {
			if err := visit(dep, depth+1); err != nil {
				return err
			}
		}
		active[key] = false
		checks = append(checks, check)
		refs = append(refs, ref)
		return nil
	}
	if err := visit(root, 1); err != nil {
		return nil, "", err
	}
	sort.Slice(refs, func(i, j int) bool { return refKey(refs[i]) < refKey(refs[j]) })
	digest, err := api.Digest(refs)
	return checks, digest, err
}

func (s *Service) checkCurrent(ctx context.Context, tx runtime.Tx, checks []ConditionCheck, maxAge uint64) ([]api.ObjectRef, string, error) {
	now, err := tx.Now(ctx)
	if err != nil {
		return nil, "", err
	}
	implementations := map[string]api.ComponentRef{}
	for _, check := range checks {
		implementations[componentKey(check.RuleRef)] = check.RuleRef
		implementations[componentKey(check.EvaluatorRef)] = check.EvaluatorRef
	}
	ids := make([]string, 0, len(implementations))
	for id := range implementations {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	gateRefs := []api.ObjectRef{}
	for _, id := range ids {
		gate, err := s.ensureGate(ctx, tx, implementations[id])
		if err != nil {
			return nil, "", err
		}
		gateRefs = append(gateRefs, tx.Scope().Ref(gate.ID, gate.Revision))
	}
	expires := api.Time(now.Add(24 * time.Hour))
	for _, check := range checks {
		if check.Verdict != "pass" {
			return nil, "", api.E("invalid_state", "evidence_not_pass")
		}
		var rule RuleDefinition
		if err = tx.GetVersion(ctx, ns("rules"), componentKey(check.RuleRef), 1, &rule); err != nil {
			return nil, "", err
		}
		if rule.RiskClass == "high_impact" && (!rule.Calibrated || maxAge != 0) {
			return nil, "", api.E("unsupported", "high_impact_requires_calibrated_zero_staleness")
		}
		if rule.RiskClass == "high_impact" {
			if s.Ports.CalibrationGate == nil {
				return nil, "", api.E("unsupported", "independent_calibration_unavailable")
			}
			if err = s.Ports.CalibrationGate.CheckTx(ctx, tx, rule); err != nil {
				return nil, "", err
			}
		}
		if !contains(rule.AllowedBasis, check.Basis) || rule.Kind == "effect" && check.Basis != "verified" {
			return nil, "", api.E("invalid_state", "basis_not_allowed")
		}
		observed, err := api.ParseTime(check.ObservedAt)
		if err != nil || observed.After(now) {
			return nil, "", api.E("invalid_state", "observation_unknown")
		}
		if rule.Predicate == "current_state" {
			cutoff := observed.Add(time.Duration(rule.MaxObservationAgeSeconds) * time.Second)
			if !now.Before(cutoff) {
				return nil, "", api.E("invalid_state", "observation_stale")
			}
			expires = minTime(expires, api.Time(cutoff))
		}
		rows, err := tx.List(ctx, ns("defect_matches"), digestID("match", []any{check.RuleRef, check.EvaluatorRef, check.ScopeRef}), "", 1)
		if err != nil {
			return nil, "", err
		}
		for _, row := range rows {
			var defect Defect
			if err = row.Decode(&defect); err != nil {
				return nil, "", err
			}
			if api.Equal(defect.RuleRef, check.RuleRef) && api.Equal(defect.EvaluatorRef, check.EvaluatorRef) && api.Equal(defect.ScopeRef, check.ScopeRef) {
				return nil, "", api.E("invalid_state", "evidence_defective")
			}
		}
	}
	return gateRefs, expires, nil
}
func (s *Service) registerHolder(ctx context.Context, tx runtime.Tx, consumer, checkRef api.ObjectRef, check ConditionCheck, deps []ConditionCheck, digest string, result *api.ObjectRef) (EvidenceHolder, error) {
	head, err := s.ensureHead(ctx, tx)
	if err != nil {
		return EvidenceHolder{}, err
	}
	id := digestID("holder", []any{consumer.TenantID, consumer.OwnerID, consumer.ObjectID, checkRef, digest, head.Epoch})
	var holder EvidenceHolder
	rev, err := tx.Get(ctx, ns("evidence_holders"), id, &holder)
	if err == nil {
		if holder.AuthorityEpoch != head.Epoch || holder.State != "active" {
			return holder, api.E("invalid_state", "holder_baseline_stale")
		}
		if result != nil {
			if holder.ResultRef != nil && !api.Equal(holder.ResultRef, result) {
				return holder, api.E("idempotency_conflict", "holder_result_changed")
			}
			if holder.ResultRef == nil {
				holder.ResultRef = result
				holder.Revision = rev + 1
				err = tx.Put(ctx, ns("evidence_holders"), id, rev, holder)
			}
		}
		return holder, err
	}
	if !errMissing(err) {
		return holder, err
	}
	head.HolderHead++
	head.Revision++
	if err = tx.Put(ctx, ns("authority"), head.ID, head.Revision-1, head); err != nil {
		return holder, err
	}
	depRefs := make([]api.ObjectRef, 0, len(deps))
	for _, d := range deps {
		depRefs = append(depRefs, tx.Scope().Ref(d.CheckID, d.Revision))
	}
	holder = EvidenceHolder{HolderID: id, Revision: 1, ConsumerTaskRef: consumer, CheckRef: checkRef, ReportHash: check.ReportRef.Hash, DependencyDigest: digest, DependencyRefs: depRefs, ScopeRef: check.ScopeRef, AuthorityEpoch: head.Epoch, RegistrationCursor: head.Cursor, RegistrationIndex: head.HolderHead, LastAckedCursor: head.Cursor, State: "active", ResultRef: result}
	err = tx.Create(ctx, ns("evidence_holders"), id, consumer.ObjectID, holder)
	return holder, err
}

func (s *Service) CheckEvidenceTx(ctx context.Context, tx runtime.Tx, in EvidenceCompletion) (EvidenceDecision, error) {
	if err := runtime.CheckRef(tx.Scope(), in.ConsumerTaskRef); err != nil {
		return EvidenceDecision{}, err
	}
	if len(in.Checks) == 0 || len(in.Checks) > 128 {
		return EvidenceDecision{}, api.E("invalid_request", "dependency_incomplete")
	}
	if in.ResultRef != nil {
		if err := ownerRef(tx.Scope(), *in.ResultRef); err != nil {
			return EvidenceDecision{}, err
		}
	}
	out := EvidenceDecision{Eligible: true, GateRefs: []api.ObjectRef{}, HolderRefs: []api.ObjectRef{}, Limitations: []string{}}
	type localRoot struct {
		ref    api.ObjectRef
		root   ConditionCheck
		deps   []ConditionCheck
		digest string
	}
	roots := []localRoot{}
	all := []ConditionCheck{}
	seen := map[string]bool{}
	for _, input := range in.Checks {
		if input.CheckRef.OwnerID != tx.Scope().OwnerID {
			if in.MaxStalenessSeconds == 0 {
				return out, api.E("unsupported", "zero_staleness_requires_same_transaction_authority")
			}
			decision, err := s.checkImported(ctx, tx, in, input.CheckRef)
			if err != nil {
				return out, err
			}
			out.ExpiresAt = minTime(out.ExpiresAt, decision.ExpiresAt)
			out.GateRefs = append(out.GateRefs, decision.GateRefs...)
			out.HolderRefs = append(out.HolderRefs, decision.HolderRefs...)
			out.Limitations = append(out.Limitations, "remote_defect_window_"+strconv.FormatUint(in.MaxStalenessSeconds, 10)+"_seconds")
			continue
		}
		deps, digest, err := s.dependencies(ctx, tx, input.CheckRef)
		if err != nil {
			return out, err
		}
		var root ConditionCheck
		if err = tx.GetVersion(ctx, ns("checks"), input.CheckRef.ObjectID, input.CheckRef.Revision, &root); err != nil {
			return out, err
		}
		if root.TaskID != in.ConsumerTaskRef.ObjectID {
			return out, api.E("forbidden", "check_task_mismatch")
		}
		for _, check := range deps {
			if !seen[check.CheckID] {
				seen[check.CheckID] = true
				all = append(all, check)
			}
		}
		if len(all) > 128 {
			return out, api.E("invalid_request", "dependency_limit")
		}
		roots = append(roots, localRoot{input.CheckRef, root, deps, digest})
	}
	// 完整集合先按统一顺序锁所有 gate，随后才允许 authority head/holder。
	// 分别逐根查 gate 再登记 holder 会在第二个根颠倒锁序。
	if len(roots) == 0 {
		return out, nil
	}
	gates, expires, err := s.checkCurrent(ctx, tx, all, in.MaxStalenessSeconds)
	if err != nil {
		return out, err
	}
	out.GateRefs = append(out.GateRefs, gates...)
	out.ExpiresAt = minTime(out.ExpiresAt, expires)
	sort.Slice(roots, func(i, j int) bool { return refKey(roots[i].ref) < refKey(roots[j].ref) })
	for _, root := range roots {
		holder, err := s.registerHolder(ctx, tx, in.ConsumerTaskRef, root.ref, root.root, root.deps, root.digest, in.ResultRef)
		if err != nil {
			return out, err
		}
		out.HolderRefs = append(out.HolderRefs, tx.Scope().Ref(holder.HolderID, holder.Revision))
	}
	return out, nil
}

// AdvanceAuthorityEpochTx 仅供受信宿主在实际负责方切换时调用。
// 全局 cursor 和 holder 截止永久保留；旧 epoch 不能以查询延长资格。
func (s *Service) AdvanceAuthorityEpochTx(ctx context.Context, tx runtime.Tx, expectedRevision, nextEpoch uint64) (AuthorityHead, error) {
	head, err := s.ensureHead(ctx, tx)
	if err != nil {
		return head, err
	}
	if head.Revision != expectedRevision || nextEpoch <= head.Epoch {
		return head, api.E("revision_conflict", "authority_epoch_conflict")
	}
	head.Epoch = nextEpoch
	head.Revision++
	err = tx.Put(ctx, ns("authority"), head.ID, expectedRevision, head)
	return head, err
}

func (s *Service) registerDefect(ctx context.Context, tx runtime.Tx, auth runtime.Auth, c api.Command, in DefectRegister) (runtime.Outcome, error) {
	if err := requireRole(auth, "maintainer"); err != nil {
		return runtime.Outcome{}, err
	}
	if in.DefectID != c.TargetID || in.ScopeRef.TenantID != tx.Scope().TenantID || in.EvidenceRef.TenantID != tx.Scope().TenantID {
		return runtime.Outcome{}, api.E("forbidden", "defect_scope_mismatch")
	}
	refs := []api.ComponentRef{in.RuleRef, in.EvaluatorRef}
	sort.Slice(refs, func(i, j int) bool { return componentKey(refs[i]) < componentKey(refs[j]) })
	gateRevision := uint64(0)
	for _, impl := range refs {
		gate, err := s.ensureGate(ctx, tx, impl)
		if err != nil {
			return runtime.Outcome{}, err
		}
		old := gate.Revision
		gate.Revision++
		if err = tx.Put(ctx, ns("evidence_gates"), gate.ID, old, gate); err != nil {
			return runtime.Outcome{}, err
		}
		if gate.Revision > gateRevision {
			gateRevision = gate.Revision
		}
	}
	head, err := s.ensureHead(ctx, tx)
	if err != nil {
		return runtime.Outcome{}, err
	}
	head.Cursor++
	old := head.Revision
	head.Revision++
	if err = tx.Put(ctx, ns("authority"), head.ID, old, head); err != nil {
		return runtime.Outcome{}, err
	}
	now, err := tx.Now(ctx)
	if err != nil {
		return runtime.Outcome{}, err
	}
	defect := Defect{DefectID: in.DefectID, RuleRef: in.RuleRef, EvaluatorRef: in.EvaluatorRef, ScopeRef: in.ScopeRef, EvidenceRef: in.EvidenceRef, RegisteredAt: api.Time(now), Cursor: head.Cursor, AuthorityEpoch: head.Epoch, HolderCutoff: head.HolderHead}
	if err = tx.Create(ctx, ns("defects"), in.DefectID, componentKey(in.EvaluatorRef), defect); err != nil {
		return runtime.Outcome{}, err
	}
	if err = tx.Create(ctx, ns("defect_matches"), in.DefectID, digestID("match", []any{in.RuleRef, in.EvaluatorRef, in.ScopeRef}), defect); err != nil {
		return runtime.Outcome{}, err
	}
	if err = tx.Create(ctx, ns("changes"), fmt.Sprintf("%020d", head.Cursor), "", defect); err != nil {
		return runtime.Outcome{}, err
	}
	impact := DefectImpact{DefectID: in.DefectID, Revision: 1}
	if err = tx.Create(ctx, ns("defect_impacts"), in.DefectID, "", impact); err != nil {
		return runtime.Outcome{}, err
	}
	if _, err = tx.Raise(ctx, "governance.defect", in.DefectID, tx.Scope().Ref(in.DefectID, 1), now); err != nil {
		return runtime.Outcome{}, err
	}
	return runtime.Applied(DefectOutput{DefectRef: tx.Scope().Ref(in.DefectID, 1), GateRevision: gateRevision, ChangeHead: head.Cursor}), nil
}

func (s *Service) eligibility(ctx context.Context, tx runtime.Tx, auth runtime.Auth, c api.Command, in EligibilityRequest) (runtime.Outcome, error) {
	if err := requireRole(auth, "evidence_consumer"); err != nil {
		return runtime.Outcome{}, err
	}
	if err := ownerRef(tx.Scope(), in.CheckRef); err != nil {
		return runtime.Outcome{}, err
	}
	deps, digest, err := s.dependencies(ctx, tx, in.CheckRef)
	if err != nil {
		return runtime.Outcome{}, err
	}
	if digest != in.DependencyDigest {
		return runtime.Outcome{}, api.E("invalid_request", "dependency_incomplete")
	}
	var check ConditionCheck
	if err = tx.GetVersion(ctx, ns("checks"), in.CheckRef.ObjectID, in.CheckRef.Revision, &check); err != nil {
		return runtime.Outcome{}, err
	}
	if !api.Equal(check.RuleRef, in.RuleRef) || !api.Equal(check.EvaluatorRef, in.EvaluatorRef) || !api.Equal(check.ReportRef, in.ReportRef) || !api.Equal(check.ScopeRef, in.ScopeRef) {
		return runtime.Outcome{}, api.E("invalid_request", "eligibility_binding_mismatch")
	}
	if in.RequestedMaxAgeSeconds == 0 {
		return runtime.Outcome{}, api.E("unsupported", "remote_zero_staleness_not_supported")
	}
	if in.RequestedMaxAgeSeconds > 31536000 {
		return runtime.Outcome{}, api.E("invalid_request", "staleness_out_of_range")
	}
	gates, expires, err := s.checkCurrent(ctx, tx, deps, in.RequestedMaxAgeSeconds)
	if err != nil {
		return runtime.Outcome{}, err
	}
	holder, err := s.registerHolder(ctx, tx, in.ConsumerTaskRef, in.CheckRef, check, deps, digest, nil)
	if err != nil {
		return runtime.Outcome{}, err
	}
	head, err := s.ensureHead(ctx, tx)
	if err != nil {
		return runtime.Outcome{}, err
	}
	now, err := tx.Now(ctx)
	if err != nil {
		return runtime.Outcome{}, err
	}
	expires = minTime(expires, api.Time(now.Add(time.Duration(in.RequestedMaxAgeSeconds)*time.Second)))
	expires = minTime(expires, in.PrepareDeadline)
	if err = before(now, expires); err != nil {
		return runtime.Outcome{}, err
	}
	requestDigest, err := api.Digest(in)
	if err != nil {
		return runtime.Outcome{}, err
	}
	revision := uint64(1)
	for _, g := range gates {
		if g.Revision > revision {
			revision = g.Revision
		}
	}
	receipt := EligibilityReceipt{ReceiptID: c.CommandID, CheckRef: in.CheckRef, ConsumerRef: in.ConsumerTaskRef, RequestDigest: requestDigest, RuleRef: in.RuleRef, EvaluatorRef: in.EvaluatorRef, ReportRef: in.ReportRef, DependencyDigest: digest, Verdict: "eligible", AuthorityEpoch: head.Epoch, DefectRevision: revision, Cursor: head.Cursor, CheckedAt: api.Time(now), IssuedAt: api.Time(now), ExpiresAt: expires, HolderRef: tx.Scope().Ref(holder.HolderID, holder.Revision)}
	receipt.DependencyBindings = make([]EvidenceBinding, 0, len(deps))
	for _, d := range deps {
		receipt.DependencyBindings = append(receipt.DependencyBindings, EvidenceBinding{CheckRef: tx.Scope().Ref(d.CheckID, d.Revision), RuleRef: d.RuleRef, EvaluatorRef: d.EvaluatorRef, ScopeRef: d.ScopeRef})
	}
	sort.Slice(receipt.DependencyBindings, func(i, j int) bool {
		return refKey(receipt.DependencyBindings[i].CheckRef) < refKey(receipt.DependencyBindings[j].CheckRef)
	})
	if s.Ports.Proof != nil {
		proofDigest, digestErr := EligibilityReceiptDigest(receipt)
		if digestErr != nil {
			return runtime.Outcome{}, digestErr
		}
		receipt.Proof, err = s.Ports.Proof.SignLocal(ProofStatement{TenantID: tx.Scope().TenantID, IssuerID: tx.Scope().OwnerID, AudienceID: in.ConsumerTaskRef.OwnerID, Purpose: "evidence_eligibility", ObjectRef: tx.Scope().Ref(receipt.ReceiptID, 1), Digest: proofDigest, IssuedAt: receipt.IssuedAt, StartBefore: expires})
		if err != nil {
			return runtime.Outcome{}, err
		}
	}
	if err = tx.Create(ctx, ns("eligibility"), receipt.ReceiptID, in.ConsumerTaskRef.ObjectID, receipt); err != nil {
		return runtime.Outcome{}, err
	}
	return runtime.Applied(receipt), nil
}

func changePage(ctx context.Context, tx runtime.Tx, holder EvidenceHolder, epoch, cursor, limit uint64) (ChangesOutput, error) {
	headID := digestID("authority", tx.Scope().OwnerID)
	var head AuthorityHead
	if _, err := tx.Get(ctx, ns("authority"), headID, &head); err != nil {
		return ChangesOutput{}, err
	}
	if epoch != head.Epoch || holder.AuthorityEpoch != epoch {
		return ChangesOutput{}, api.E("authority_changed", "authority_epoch_changed")
	}
	if limit < 1 || limit > 100 || cursor < holder.RegistrationCursor || cursor > head.Cursor {
		return ChangesOutput{}, api.E("snapshot_required", "change_cursor_gap")
	}
	now, err := tx.Now(ctx)
	if err != nil {
		return ChangesOutput{}, err
	}
	out := ChangesOutput{HolderRef: tx.Scope().Ref(holder.HolderID, holder.Revision), FromCursor: cursor, Changes: []Defect{}, Head: head.Cursor, NextCursor: cursor, AuthorityEpoch: epoch, IssuedAt: api.Time(now), ExpiresAt: api.Time(now.Add(5 * time.Minute))}
	through := cursor + limit
	if through > head.Cursor {
		through = head.Cursor
	}
	for next := cursor + 1; next <= through; next++ {
		var defect Defect
		if _, err := tx.Get(ctx, ns("changes"), fmt.Sprintf("%020d", next), &defect); err != nil {
			return out, api.E("snapshot_required", "change_cursor_gap")
		}
		if defect.Cursor != next || defect.AuthorityEpoch != epoch {
			return out, api.E("snapshot_required", "change_cursor_gap")
		}
		out.Changes = append(out.Changes, defect)
		out.NextCursor = next
	}
	out.Partial = out.NextCursor < head.Cursor
	digest, err := api.Digest([]any{holder.HolderID, epoch, cursor, out.NextCursor, out.Changes})
	out.Digest = digest
	return out, err
}
func (s *Service) changes(ctx context.Context, store runtime.Store, scope runtime.Scope, auth runtime.Auth, q api.Query, in ChangesRequest) (ChangesOutput, error) {
	if err := requireRole(auth, "evidence_consumer"); err != nil {
		return ChangesOutput{}, err
	}
	var out ChangesOutput
	status, err := store.Within(ctx, scope, s.participants(), func(tx runtime.Tx) error {
		if err := ownerRef(scope, in.HolderRef); err != nil {
			return err
		}
		if _, err := s.ensureHead(ctx, tx); err != nil {
			return err
		}
		var holder EvidenceHolder
		if _, err := tx.Get(ctx, ns("evidence_holders"), in.HolderRef.ObjectID, &holder); err != nil {
			return err
		}
		var err error
		out, err = changePage(ctx, tx, holder, in.AuthorityEpoch, in.Cursor, in.Limit)
		if err == nil && s.Ports.Proof != nil {
			digest, digestErr := EvidenceChangesDigest(out)
			if digestErr != nil {
				return digestErr
			}
			out.Proof, err = s.Ports.Proof.SignLocal(ProofStatement{TenantID: scope.TenantID, IssuerID: scope.OwnerID, AudienceID: holder.ConsumerTaskRef.OwnerID, Purpose: "evidence_changes", ObjectRef: out.HolderRef, Digest: digest, IssuedAt: out.IssuedAt, StartBefore: out.ExpiresAt})
		}
		return err
	})
	if status == runtime.CommitUnknown {
		return out, runtime.ErrCommitUnknown
	}
	return out, err
}
func (s *Service) ackHolder(ctx context.Context, tx runtime.Tx, auth runtime.Auth, c api.Command, in HolderAck) (runtime.Outcome, error) {
	if err := requireRole(auth, "evidence_consumer"); err != nil {
		return runtime.Outcome{}, err
	}
	if err := ownerRef(tx.Scope(), in.HolderRef); err != nil {
		return runtime.Outcome{}, err
	}
	head, err := s.ensureHead(ctx, tx)
	if err != nil {
		return runtime.Outcome{}, err
	}
	if head.Epoch != in.AuthorityEpoch {
		return runtime.Outcome{}, api.E("authority_changed", "authority_epoch_changed")
	}
	var holder EvidenceHolder
	rev, err := tx.Get(ctx, ns("evidence_holders"), in.HolderRef.ObjectID, &holder)
	if err != nil {
		return runtime.Outcome{}, err
	}
	if holder.AuthorityEpoch != in.AuthorityEpoch {
		return runtime.Outcome{}, api.E("authority_changed", "authority_epoch_changed")
	}
	if in.ThroughCursor < holder.LastAckedCursor {
		return runtime.Applied(StateOutput{Ref: tx.Scope().Ref(holder.HolderID, holder.Revision), State: holder.State}), nil
	}
	if in.ThroughCursor == holder.LastAckedCursor {
		if holder.LastAckDigest != "" && holder.LastAckDigest != in.ImportDigest {
			return runtime.Outcome{}, api.E("idempotency_conflict", "ack_digest_changed")
		}
		return runtime.Applied(StateOutput{Ref: tx.Scope().Ref(holder.HolderID, holder.Revision), State: holder.State}), nil
	}
	distance := in.ThroughCursor - holder.LastAckedCursor
	if distance > 100 {
		return runtime.Outcome{}, api.E("snapshot_required", "ack_range_too_large")
	}
	page, err := changePage(ctx, tx, holder, in.AuthorityEpoch, holder.LastAckedCursor, distance)
	if err != nil {
		return runtime.Outcome{}, err
	}
	if page.NextCursor != in.ThroughCursor || page.Digest != in.ImportDigest {
		return runtime.Outcome{}, api.E("idempotency_conflict", "ack_import_digest_mismatch")
	}
	holder.Revision = rev + 1
	holder.LastAckedCursor = in.ThroughCursor
	holder.LastAckDigest = in.ImportDigest
	if err = tx.Put(ctx, ns("evidence_holders"), holder.HolderID, rev, holder); err != nil {
		return runtime.Outcome{}, err
	}
	return runtime.Applied(StateOutput{Ref: tx.Scope().Ref(holder.HolderID, holder.Revision), State: holder.State}), nil
}

func (s *Service) continueDefect(ctx context.Context, store runtime.Store, scope runtime.Scope, work runtime.Work) error {
	return finish(ctx, store, scope, s.participants(), work, runtime.Done(), func(tx runtime.Tx) error {
		var impact DefectImpact
		rev, err := tx.Get(ctx, ns("defect_impacts"), work.Job.ResponsibilityKey, &impact)
		if err != nil {
			return err
		}
		var defect Defect
		if _, err = tx.Get(ctx, ns("defects"), impact.DefectID, &defect); err != nil {
			return err
		}
		rows, err := tx.List(ctx, ns("evidence_holders"), "", impact.Cursor, 100)
		if err != nil {
			return err
		}
		for _, row := range rows {
			var holder EvidenceHolder
			if err = row.Decode(&holder); err != nil {
				return err
			}
			impact.Cursor = row.ID
			if holder.RegistrationIndex > defect.HolderCutoff || holder.State != "active" {
				continue
			}
			matched := false
			for _, ref := range holder.DependencyRefs {
				var check ConditionCheck
				if err = tx.GetVersion(ctx, ns("checks"), ref.ObjectID, ref.Revision, &check); err != nil {
					return err
				}
				if api.Equal(check.RuleRef, defect.RuleRef) && api.Equal(check.EvaluatorRef, defect.EvaluatorRef) && api.Equal(check.ScopeRef, defect.ScopeRef) {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
			if holder.ResultRef != nil {
				id := digestID("notice", []string{holder.HolderID, defect.DefectID})
				notice := ResultNotice{NoticeID: id, HolderRef: scope.Ref(holder.HolderID, holder.Revision), ResultRef: *holder.ResultRef, DefectRef: scope.Ref(defect.DefectID, 1), ConsumerTaskRef: holder.ConsumerTaskRef, Reason: "evidence_defective_after_result", RegisteredAt: defect.RegisteredAt}
				var old ResultNotice
				if _, e := tx.Get(ctx, ns("notices"), id, &old); errMissing(e) {
					if err = tx.Create(ctx, ns("notices"), id, holder.ConsumerTaskRef.ObjectID, notice); err != nil {
						return err
					}
				} else if e != nil {
					return e
				} else {
					// ACK 等后续 holder 修订不能重写已经创建的原通知依据。
					notice.HolderRef.Revision = old.HolderRef.Revision
					if old.HolderRef.Revision == 0 || old.HolderRef.Revision > holder.Revision || !api.Equal(old, notice) {
						return api.E("idempotency_conflict", "original_result_notice_changed")
					}
					notice = old
				}
				if s.Ports.ResultNotices != nil {
					if err = s.Ports.ResultNotices.RecordNoticeTx(ctx, tx, notice); err != nil {
						return err
					}
				}
			}
		}
		impact.Revision = rev + 1
		if err = tx.Put(ctx, ns("defect_impacts"), impact.DefectID, rev, impact); err != nil {
			return err
		}
		if len(rows) == 100 {
			now, err := tx.Now(ctx)
			if err != nil {
				return err
			}
			return tx.Hint(ctx, work.Job.JobID, now)
		}
		return nil
	})
}

// EligibilityReceiptDigest 绑定整份闭合资格事实，排除承载签名的字段。
func EligibilityReceiptDigest(receipt EligibilityReceipt) (string, error) {
	receipt.Proof = ""
	receipt.ProofRef = nil
	return api.Digest(receipt)
}
func EvidenceChangesDigest(page ChangesOutput) (string, error) {
	page.Proof = ""
	return api.Digest(page)
}

// InstallEvidenceBaselineTx 在本地登记密钥下核验完整 authority 事实。
// verified 是宿主已完成准入的标记，不能代替此处原签名核验。
// receipt 和连续水位同消费方 gate 原子提交，缺口不会被新 receipt 查询掩盖。
func (s *Service) InstallEvidenceBaselineTx(ctx context.Context, tx runtime.Tx, receipt EligibilityReceipt, verified bool) error {
	if !verified || receipt.ConsumerRef.OwnerID != tx.Scope().OwnerID || receipt.ConsumerRef.TenantID != tx.Scope().TenantID || receipt.Verdict != "eligible" {
		return api.E("forbidden", "eligibility_unverified")
	}
	if s.Ports.Proof == nil || receipt.Proof == "" {
		return api.E("unsupported", "authority_signature_unavailable")
	}
	now, err := tx.Now(ctx)
	if err != nil {
		return err
	}
	digest, err := EligibilityReceiptDigest(receipt)
	if err != nil {
		return err
	}
	statement := ProofStatement{TenantID: tx.Scope().TenantID, IssuerID: receipt.CheckRef.OwnerID, AudienceID: tx.Scope().OwnerID, Purpose: "evidence_eligibility", ObjectRef: api.ObjectRef{TenantID: tx.Scope().TenantID, OwnerID: receipt.CheckRef.OwnerID, ObjectID: receipt.ReceiptID, Revision: 1}, Digest: digest, IssuedAt: receipt.IssuedAt, StartBefore: receipt.ExpiresAt}
	if err = s.Ports.Proof.VerifyLocal(receipt.Proof, statement, now); err != nil {
		return err
	}
	if len(receipt.DependencyBindings) == 0 || len(receipt.DependencyBindings) > 128 {
		return api.E("invalid_request", "dependency_incomplete")
	}
	refs := make([]api.ObjectRef, 0, len(receipt.DependencyBindings))
	rootFound := false
	for _, binding := range receipt.DependencyBindings {
		if binding.CheckRef.OwnerID != receipt.CheckRef.OwnerID || binding.CheckRef.TenantID != tx.Scope().TenantID {
			return api.E("unsupported", "unsupported_multi_authority")
		}
		refs = append(refs, binding.CheckRef)
		if api.Equal(binding.CheckRef, receipt.CheckRef) {
			rootFound = true
		}
	}
	sort.Slice(refs, func(i, j int) bool { return refKey(refs[i]) < refKey(refs[j]) })
	depsDigest, err := api.Digest(refs)
	if err != nil {
		return err
	}
	if !rootFound || depsDigest != receipt.DependencyDigest {
		return api.E("invalid_request", "dependency_incomplete")
	}
	id := digestID("import", receipt.CheckRef)
	old := EvidenceImport{}
	rev, err := tx.Get(ctx, ns("evidence_imports"), id, &old)
	next := EvidenceImport{ID: id, Revision: 1, Receipt: receipt, ImportedCursor: receipt.Cursor, AuthorityEpoch: receipt.AuthorityEpoch, Defects: []Defect{}, Digest: receipt.RequestDigest}
	if errMissing(err) {
		return tx.Create(ctx, ns("evidence_imports"), id, receipt.ConsumerRef.ObjectID, next)
	}
	if err != nil {
		return err
	}
	if receipt.AuthorityEpoch < old.AuthorityEpoch || old.AuthorityEpoch == receipt.AuthorityEpoch && receipt.Cursor < old.ImportedCursor {
		return api.E("snapshot_required", "baseline_cursor_regressed")
	}
	next.Revision = rev + 1
	return tx.Put(ctx, ns("evidence_imports"), id, rev, next)
}

// MarkEvidenceGapTx 由受信源传输在收到 authority_changed/snapshot_required
// 后同库关闭已知旧基线；刷新原 receipt 查询不能清除此门禁。
func (s *Service) MarkEvidenceGapTx(ctx context.Context, tx runtime.Tx, checkRef api.ObjectRef, observedEpoch uint64) error {
	if err := runtime.CheckRef(tx.Scope(), checkRef); err != nil {
		return err
	}
	id := digestID("import", checkRef)
	var imported EvidenceImport
	rev, err := tx.Get(ctx, ns("evidence_imports"), id, &imported)
	if err != nil {
		return err
	}
	if !api.Equal(imported.Receipt.CheckRef, checkRef) {
		return api.E("forbidden", "eligibility_binding_mismatch")
	}
	imported.Gap = true
	if observedEpoch > imported.AuthorityEpoch {
		imported.AuthorityEpoch = observedEpoch
	}
	imported.Revision = rev + 1
	return tx.Put(ctx, ns("evidence_imports"), id, rev, imported)
}
func (s *Service) ImportEvidenceChangesTx(ctx context.Context, tx runtime.Tx, checkRef api.ObjectRef, page ChangesOutput) error {
	id := digestID("import", checkRef)
	var imported EvidenceImport
	rev, err := tx.Get(ctx, ns("evidence_imports"), id, &imported)
	if err != nil {
		return err
	}
	if page.AuthorityEpoch != imported.AuthorityEpoch {
		return api.E("authority_changed", "authority_epoch_changed")
	}
	if s.Ports.Proof == nil || page.Proof == "" {
		return api.E("unsupported", "authority_signature_unavailable")
	}
	if page.HolderRef.OwnerID != checkRef.OwnerID || page.HolderRef.TenantID != tx.Scope().TenantID || page.HolderRef.ObjectID != imported.Receipt.HolderRef.ObjectID {
		return api.E("forbidden", "change_holder_binding_mismatch")
	}
	now, err := tx.Now(ctx)
	if err != nil {
		return err
	}
	digest, err := EvidenceChangesDigest(page)
	if err != nil {
		return err
	}
	if err = s.Ports.Proof.VerifyLocal(page.Proof, ProofStatement{TenantID: tx.Scope().TenantID, IssuerID: checkRef.OwnerID, AudienceID: tx.Scope().OwnerID, Purpose: "evidence_changes", ObjectRef: page.HolderRef, Digest: digest, IssuedAt: page.IssuedAt, StartBefore: page.ExpiresAt}, now); err != nil {
		return err
	}
	exactDigest, err := api.Digest([]any{page.HolderRef.ObjectID, page.AuthorityEpoch, page.FromCursor, page.NextCursor, page.Changes})
	if err != nil {
		return err
	}
	if exactDigest != page.Digest || page.NextCursor > page.Head || len(page.Changes) > 100 {
		return api.E("invalid_request", "change_page_binding_mismatch")
	}
	cursor := imported.ImportedCursor
	if page.NextCursor == cursor && page.Digest == imported.Digest {
		return nil
	}
	if page.FromCursor != cursor {
		imported.Gap = true
		imported.Revision = rev + 1
		return tx.Put(ctx, ns("evidence_imports"), id, rev, imported)
	}
	for _, defect := range page.Changes {
		if defect.Cursor != cursor+1 {
			imported.Gap = true
			imported.Revision = rev + 1
			return tx.Put(ctx, ns("evidence_imports"), id, rev, imported)
		}
		cursor++
		imported.Defects = append(imported.Defects, defect)
	}
	if cursor != page.NextCursor || len(imported.Defects) > 128 {
		imported.Gap = true
	}
	imported.ImportedCursor = cursor
	imported.Digest = page.Digest
	imported.Revision = rev + 1
	return tx.Put(ctx, ns("evidence_imports"), id, rev, imported)
}
func (s *Service) checkImported(ctx context.Context, tx runtime.Tx, in EvidenceCompletion, check api.ObjectRef) (EvidenceDecision, error) {
	if in.MaxStalenessSeconds > 31536000 {
		return EvidenceDecision{}, api.E("invalid_request", "staleness_out_of_range")
	}
	var imported EvidenceImport
	if _, err := tx.Get(ctx, ns("evidence_imports"), digestID("import", check), &imported); err != nil {
		return EvidenceDecision{}, err
	}
	r := imported.Receipt
	now, err := tx.Now(ctx)
	if err != nil {
		return EvidenceDecision{}, err
	}
	checked, err := api.ParseTime(r.CheckedAt)
	if err != nil {
		return EvidenceDecision{}, err
	}
	expires := minTime(r.ExpiresAt, api.Time(checked.Add(time.Duration(in.MaxStalenessSeconds)*time.Second)))
	if imported.Gap || imported.AuthorityEpoch != r.AuthorityEpoch || !api.Equal(r.ConsumerRef, in.ConsumerTaskRef) || r.Verdict != "eligible" || before(now, expires) != nil {
		return EvidenceDecision{}, api.E("snapshot_required", "eligibility_baseline_stale")
	}
	for _, defect := range imported.Defects {
		for _, binding := range r.DependencyBindings {
			if api.Equal(defect.RuleRef, binding.RuleRef) && api.Equal(defect.EvaluatorRef, binding.EvaluatorRef) && api.Equal(defect.ScopeRef, binding.ScopeRef) {
				return EvidenceDecision{}, api.E("invalid_state", "remote_evidence_defect_requires_new_baseline")
			}
		}
	}
	return EvidenceDecision{Eligible: true, ExpiresAt: expires, GateRefs: []api.ObjectRef{tx.Scope().Ref(imported.ID, imported.Revision)}, HolderRefs: []api.ObjectRef{r.HolderRef}, Limitations: []string{}}, nil
}
