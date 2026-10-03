package execution

import (
	"context"
	"errors"
	"time"

	"github.com/ruipengliu/lerna/api"
	rt "github.com/ruipengliu/lerna/runtime"
)

func (s *Service) loadWork(ctx context.Context, st rt.Store, sc rt.Scope, w rt.Work) (operationRecord, error) {
	var r operationRecord
	if w.Job.SourceRef.OwnerID != sc.OwnerID || w.Job.SourceRef.TenantID != sc.TenantID {
		return r, api.E("forbidden", "job_scope_mismatch")
	}
	_, err := st.Read(ctx, sc, Namespace+".operations", w.Job.SourceRef.ObjectID, 0, &r)
	return r, err
}
func (s *Service) finish(ctx context.Context, st rt.Store, sc rt.Scope, w rt.Work, d rt.Disposition, fn func(rt.Tx) error) error {
	return rt.Finish(ctx, st, sc, s.participants(), w, d, fn)
}
func (s *Service) run(ctx context.Context, st rt.Store, sc rt.Scope, w rt.Work) error {
	op, err := s.loadWork(ctx, st, sc, w)
	if err != nil {
		return err
	}
	if op.NewAttemptsClosed {
		return s.finish(ctx, st, sc, w, rt.Done(), nil)
	}
	d, err := s.driver(op.Invoke.CapabilityRef)
	if err != nil {
		return s.closeUnstarted(ctx, st, sc, w, err)
	}
	if len(op.AttemptIDs) > 0 {
		var attempt Attempt
		if _, err = st.Read(ctx, sc, Namespace+".attempts", op.AttemptIDs[len(op.AttemptIDs)-1], 0, &attempt); err != nil {
			return err
		}
		if attempt.Phase != "prepared" {
			return s.reconcileWork(ctx, st, sc, w)
		}
		return s.startPrepared(ctx, st, sc, w, op, attempt, d)
	}
	if s.cfg.Content == nil || s.cfg.Authority == nil {
		return s.closeUnstarted(ctx, st, sc, w, api.E("dependency_unavailable", "execution_dependencies_not_configured"))
	}
	raw, err := s.cfg.Content.ReadBytes(ctx, sc, op.Principal, op.Invoke.IntentRef, "execution_intent", s.cfg.Location)
	if err != nil {
		if businessError(err) {
			return s.closeUnstarted(ctx, st, sc, w, err)
		}
		return err
	}
	canonical, err := api.Canonical(raw)
	digest := api.Hash(canonical)
	if err != nil {
		return s.closeUnstarted(ctx, st, sc, w, err)
	}
	if digest != op.Invoke.IntentHash {
		return s.closeUnstarted(ctx, st, sc, w, api.E("idempotency_conflict", "intent_mismatch"))
	}
	validator, err := api.NewValidator(api.SchemaFor[ExecutionIntent]())
	if err != nil {
		return err
	}
	if err = validator.Validate(raw); err != nil {
		return s.closeUnstarted(ctx, st, sc, w, err)
	}
	var intent ExecutionIntent
	if err = api.Decode(raw, &intent); err != nil {
		return s.closeUnstarted(ctx, st, sc, w, err)
	}
	if err = validateIntent(op.Invoke, intent); err != nil {
		return s.closeUnstarted(ctx, st, sc, w, err)
	}
	if intent.ExecutorID != sc.OwnerID {
		return s.closeUnstarted(ctx, st, sc, w, api.E("invalid_request", "wrong_executor"))
	}
	args, err := s.cfg.Content.ReadBytes(ctx, sc, op.Principal, intent.ArgumentsRef, "execution_arguments", s.cfg.Location)
	if err != nil {
		if businessError(err) {
			return s.closeUnstarted(ctx, st, sc, w, err)
		}
		return err
	}
	inputValidator, err := api.NewValidator(d.Capability().InputSchema)
	if err != nil {
		return err
	}
	if err = inputValidator.Validate(args); err != nil {
		return s.closeUnstarted(ctx, st, sc, w, err)
	}
	prepared, err := d.Prepare(ctx, sc, op.Principal, op.Invoke, intent, args)
	if err != nil {
		if businessError(err) {
			return s.closeUnstarted(ctx, st, sc, w, err)
		}
		return err
	}
	if len(prepared.Encoded) > api.MaxJSONBytes || prepared.Digest != api.Hash(prepared.Encoded) {
		return s.closeUnstarted(ctx, st, sc, w, api.E("invalid_request", "encoded_request_mismatch"))
	}
	window, err := s.selectControlWindow(ctx, st, sc, op.Invoke)
	if err != nil {
		return s.closeUnstarted(ctx, st, sc, w, err)
	}
	attempt := Attempt{AttemptID: api.NewID("attempt"), OperationID: op.Operation.OperationID, Revision: 1, AttemptNo: 1, Phase: "prepared", Prepared: prepared, Permit: StartPermit{ProofRefs: []api.ContentRef{}}, Effect: "not_started", MayApplyLater: false, ControlWindowID: window.WindowID, ControlWindow: window, EvidenceRefs: []api.ContentRef{}, Usage: []api.Amount{}, ActuallyStopped: true}
	authority, err := s.cfg.Authority.PrepareStart(ctx, sc, StartRequest{ControlWindow: window, Invoke: op.Invoke, Intent: intent, AttemptID: attempt.AttemptID, Auth: op.Principal})
	if err != nil {
		if businessError(err) {
			return s.closeUnstarted(ctx, st, sc, w, err)
		}
		return err
	}
	if authority.AuthorityRevision == 0 || authority.ProofRef.TenantID != sc.TenantID || api.ValidateRecord("ContentRef", authority.ProofRef) != nil {
		return s.closeUnstarted(ctx, st, sc, w, api.E("forbidden", "start_proof_missing"))
	}
	attempt.PreparedAuthority = authority
	if err = st.CheckClaim(ctx, sc, w.Claim); err != nil {
		return err
	}
	status, err := st.Within(ctx, sc, s.participants(), func(tx rt.Tx) error {
		var current operationRecord
		rev, err := tx.Get(ctx, Namespace+".operations", op.Operation.OperationID, &current)
		if err != nil {
			return err
		}
		if current.NewAttemptsClosed || len(current.AttemptIDs) > 0 {
			return api.E("invalid_state", "operation_not_startable")
		}
		current.Intent = &intent
		current.AttemptIDs = append(current.AttemptIDs, attempt.AttemptID)
		current.Operation.Attempts.TotalCount++
		current.Operation.Attempts.CollectionRevision++
		current.Operation.Attempts.UnresolvedCount++
		if err = tx.Create(ctx, Namespace+".attempts", attempt.AttemptID, current.Operation.OperationID, attempt); err != nil {
			return err
		}
		if err = putOperation(ctx, tx, &current, rev); err != nil {
			return err
		}
		op = current
		return tx.Guard(ctx, w.Claim)
	})
	if status == rt.CommitUnknown {
		return rt.ErrCommitUnknown
	}
	if err != nil {
		return err
	}
	return s.startPrepared(ctx, st, sc, w, op, attempt, d)
}
func (s *Service) startPrepared(ctx context.Context, st rt.Store, sc rt.Scope, w rt.Work, op operationRecord, attempt Attempt, d Driver) error {
	if op.Intent == nil {
		return api.E("invalid_state", "attempt_intent_missing")
	}
	entered := false
	called := false
	barrier := func(callCtx context.Context) error {
		if called {
			return api.E("invalid_state", "duplicate_start_barrier")
		}
		called = true
		status, err := st.Within(callCtx, sc, s.participants(), func(tx rt.Tx) error {
			// 外部准备已固定原输入；同库 Authority 先取得上游 Task/预算锁。
			// 随后锁执行门禁并重验这些准确字段，不能沿旧快照绕过当前门禁。
			permit, err := s.cfg.Authority.VerifyStart(callCtx, tx, StartRequest{ControlWindow: attempt.ControlWindow, Invoke: op.Invoke, Intent: *op.Intent, AttemptID: attempt.AttemptID, Auth: op.Principal}, attempt.PreparedAuthority)
			if err != nil {
				return err
			}
			if err := lockAttemptGates(callCtx, tx, op.Invoke, attempt, true); err != nil {
				return err
			}
			var current operationRecord
			rev, err := tx.Get(callCtx, Namespace+".operations", op.Operation.OperationID, &current)
			if err != nil {
				return err
			}
			var a Attempt
			ar, err := tx.Get(callCtx, Namespace+".attempts", attempt.AttemptID, &a)
			if err != nil {
				return err
			}
			if a.Phase != "prepared" || current.NewAttemptsClosed {
				return api.E("invalid_state", "operation_permanently_closed")
			}
			if current.Operation.OperationID != op.Operation.OperationID || !api.Equal(current.Invoke, op.Invoke) || !api.Equal(current.Principal, op.Principal) || !api.Equal(current.Intent, op.Intent) || !api.Equal(a, attempt) {
				return api.E("revision_conflict", "prepared_start_changed")
			}
			var gate TaskGate
			if _, err = tx.Get(callCtx, Namespace+".gates", gateID(op.Invoke.TaskRef.OwnerID, op.Invoke.TaskRef.ObjectID), &gate); err != nil {
				return err
			}
			if gate.GoalRevision != op.Invoke.GoalRevision || gate.ControlRevision != op.Invoke.ControlRevision || !permittedGate(gate) || gate.Status == "cancelled" || gate.Status == "succeeded" || gate.Status == "failed" {
				return api.E("invalid_state", "task_gate_stale")
			}
			var boundWindow api.ControlSnapshot
			if _, err = tx.Get(callCtx, Namespace+".windows", a.ControlWindowID, &boundWindow); err != nil {
				return err
			}
			if !api.Equal(boundWindow, a.ControlWindow) || boundWindow.TaskID != op.Invoke.TaskRef.ObjectID || boundWindow.OrchestratorID != op.Invoke.TaskRef.OwnerID || boundWindow.GoalRevision != op.Invoke.GoalRevision || boundWindow.ControlRevision != op.Invoke.ControlRevision {
				return api.E("forbidden", "control_window_binding_mismatch")
			}
			now, err := tx.Now(callCtx)
			if err != nil {
				return err
			}
			if err = minDeadline(now, op.Invoke.Deadline, op.Intent.TaskDeadline, a.ControlWindow.StartBefore, a.PreparedAuthority.StartBefore, permit.StartBefore); err != nil {
				return err
			}
			if a.PreparedAuthority.OperationID != op.Operation.OperationID || a.PreparedAuthority.IntentHash != op.Invoke.IntentHash || a.PreparedAuthority.Recipient != sc.OwnerID || !api.Equal(a.PreparedAuthority.UseRefs, op.Invoke.UseRefs) {
				return api.E("forbidden", "use_intent_mismatch")
			}
			if err = s.resourceBarrier(callCtx, tx, current, a, now); err != nil {
				return err
			}
			if err = s.cellBarrier(callCtx, tx, current, a, now); err != nil {
				return err
			}
			a.Revision = ar + 1
			a.Phase = "possibly_sent"
			a.Permit = permit
			a.StartedAt = api.Time(now)
			a.Effect = "unknown"
			a.MayApplyLater = "unknown"
			a.ActuallyStopped = false
			if err = tx.Put(callCtx, Namespace+".attempts", a.AttemptID, ar, a); err != nil {
				return err
			}
			current.ActuallyStopped = false
			current.Operation.ExecutionState = "started"
			current.Operation.Effect = "unknown"
			current.Operation.MayApplyLater = "unknown"
			current.Operation.NextAction = "query_original"
			current.Operation.UsageFinal = false
			if err = putOperation(callCtx, tx, &current, rev); err != nil {
				return err
			}
			_, err = tx.Raise(callCtx, ReconcileJob, current.Operation.OperationID, sc.Ref(current.Operation.OperationID, current.Revision), now.Add(5*time.Second))
			attempt = a
			if err != nil {
				return err
			}
			return tx.Guard(callCtx, w.Claim)
		})
		if status == rt.CommitUnknown {
			return rt.ErrCommitUnknown
		}
		if err != nil {
			return err
		}
		entered = true
		return nil
	}
	request := AttemptRequest{Scope: sc, Invoke: op.Invoke, Intent: *op.Intent, Attempt: attempt, Auth: op.Principal}
	fact, sendErr := d.Start(ctx, request, barrier)
	if !entered {
		if errors.Is(sendErr, rt.ErrCommitUnknown) || errors.Is(sendErr, rt.ErrClaimLost) {
			return sendErr
		}
		if sendErr == nil {
			return api.E("invalid_state", "driver_bypassed_start_barrier")
		}
		return s.closeUnstarted(ctx, st, sc, w, sendErr)
	}
	// 真实出口错误没有可信反证，保留可能发送；从不以 context 取消伪造未发送。
	if sendErr != nil {
		fact = Fact{Revision: 1, Effect: "unknown", MayApplyLater: "unknown", Usage: []api.Amount{}, Evidence: []api.ContentRef{}}
	}
	return s.saveFact(ctx, st, sc, w, op, attempt, fact)
}
func (s *Service) closeUnstarted(ctx context.Context, st rt.Store, sc rt.Scope, w rt.Work, cause error) error {
	return s.finish(ctx, st, sc, w, rt.Done(), func(tx rt.Tx) error {
		var r operationRecord
		rev, err := tx.Get(ctx, Namespace+".operations", w.Job.SourceRef.ObjectID, &r)
		if err != nil {
			return err
		}
		if r.Operation.ExecutionState == "started" {
			return api.E("effect_unknown", "attempt_already_possible")
		}
		r.NewAttemptsClosed = true
		r.ActuallyStopped = true
		r.Operation.ExecutionState = "closed"
		r.Operation.Effect = "not_started"
		r.Operation.MayApplyLater = false
		r.Operation.UsageFinal = true
		r.Operation.NextAction = "none"
		r.CancelReason = cause.Error()
		for _, id := range r.AttemptIDs {
			var a Attempt
			ar, e := tx.Get(ctx, Namespace+".attempts", id, &a)
			if e != nil {
				return e
			}
			if a.Phase != "prepared" {
				return api.E("effect_unknown", "attempt_already_possible")
			}
			a.Revision = ar + 1
			a.Phase = "reconciled"
			a.UsageFinal = true
			a.ActuallyStopped = true
			if e = tx.Put(ctx, Namespace+".attempts", id, ar, a); e != nil {
				return e
			}
		}
		r.Operation.Attempts.UnresolvedCount = 0
		return putOperation(ctx, tx, &r, rev)
	})
}
func (s *Service) reconcileWork(ctx context.Context, st rt.Store, sc rt.Scope, w rt.Work) error {
	op, err := s.loadWork(ctx, st, sc, w)
	if err != nil {
		return err
	}
	if len(op.AttemptIDs) == 0 {
		return s.finish(ctx, st, sc, w, rt.Done(), nil)
	}
	if op.Intent == nil {
		return api.E("invalid_state", "attempt_intent_missing")
	}
	d, err := s.driver(op.Invoke.CapabilityRef)
	if err != nil {
		return err
	}
	// 完整集合有界 100，不选择最后一次响应掩盖旧未知。
	for _, id := range op.AttemptIDs {
		var a Attempt
		if _, err = st.Read(ctx, sc, Namespace+".attempts", id, 0, &a); err != nil {
			return err
		}
		if a.Phase == "prepared" {
			continue
		}
		fact, err := d.Reconcile(ctx, AttemptRequest{Scope: sc, Invoke: op.Invoke, Intent: *op.Intent, Attempt: a, Auth: op.Principal})
		if err != nil {
			return err
		}
		if err = s.saveFact(ctx, st, sc, w, op, a, fact); err != nil {
			return err
		}
		if len(op.AttemptIDs) > 1 {
			return api.E("unsupported", "multi_attempt_reconcile_requires_fresh_claim")
		}
	}
	return nil
}
func (s *Service) saveFact(ctx context.Context, st rt.Store, sc rt.Scope, w rt.Work, op operationRecord, a Attempt, f Fact) error {
	if f.Usage == nil {
		f.Usage = []api.Amount{}
	}
	if f.Evidence == nil {
		f.Evidence = []api.ContentRef{}
	}
	if err := validFact(f); err != nil {
		return err
	}
	var namespaceRef *api.ContentRef
	if a.Prepared.Cell != nil && !a.CellCommitted && f.Effect == "applied" {
		cell := a.Prepared.Cell
		ref, err := s.cfg.Content.Publish(ctx, sc, op.Principal, Publication{ContentID: stableID("content", a.AttemptID+":namespace"), MediaType: "application/json", Purpose: "environment_namespace", Location: s.cfg.Location, ProcessedSources: cell.Sources, DisclosedSources: []api.ContentRef{}}, api.Raw(cell.Namespace))
		if err != nil {
			return err
		}
		namespaceRef = &ref
		f.Output = api.Raw(CellResult{OperationRef: sc.Ref(op.Operation.OperationID, op.Revision+1), NamespaceRef: ref, NamespaceRevision: cell.ExpectedNamespaceRevision + 1, Generation: cell.ExpectedGeneration})
	}
	digest, err := api.Digest(struct {
		Revision      uint64           `json:"revision"`
		Effect        string           `json:"effect"`
		MayApplyLater any              `json:"may_apply_later"`
		OutputHash    string           `json:"output_hash"`
		Evidence      []api.ContentRef `json:"evidence"`
		Usage         []api.Amount     `json:"usage"`
		UsageFinal    bool             `json:"usage_final"`
		Disputed      bool             `json:"disputed"`
	}{f.Revision, f.Effect, f.MayApplyLater, api.Hash(f.Output), f.Evidence, f.Usage, f.UsageFinal, f.Disputed})
	if err != nil {
		return err
	}
	var outputRef *api.ContentRef
	if len(f.Output) > 0 {
		d, err := s.driver(op.Invoke.CapabilityRef)
		if err != nil {
			return err
		}
		v, err := api.NewValidator(d.Capability().OutputSchema)
		if err != nil {
			return err
		}
		if err = v.Validate(f.Output); err != nil {
			return api.E("invalid_request", "driver_output_schema_violation")
		}
		if s.cfg.Content == nil {
			return api.E("dependency_unavailable", "content_not_configured")
		}
		// 输出身份由原 Attempt 与事实修订确定；失答复后不创建另一个同义产物。
		contentID := stableID("content", a.AttemptID+":"+api.Hash(f.Output))
		media := f.MediaType
		if media == "" {
			media = "application/json"
		}
		ref, err := s.cfg.Content.Publish(ctx, sc, op.Principal, Publication{ContentID: contentID, MediaType: media, Purpose: "execution_result", Location: s.cfg.Location, ProcessedSources: op.Intent.ProcessedSourceRefs, DisclosedSources: op.Intent.DisclosedSourceRefs}, f.Output)
		if err != nil {
			return err
		}
		outputRef = &ref
	}
	disposition := rt.Done()
	if w.Job.Kind == ReconcileJob && op.ReconcileCount < 9 && (f.Effect == "unknown" || !noLater(f.MayApplyLater) || !f.UsageFinal) {
		disposition = rt.Waiting(time.Now().UTC().Add(30 * time.Second))
	}
	return s.finish(ctx, st, sc, w, disposition, func(tx rt.Tx) error {
		if err := lockAttemptGates(ctx, tx, op.Invoke, a, false); err != nil {
			return err
		}
		var current operationRecord
		rev, err := tx.Get(ctx, Namespace+".operations", op.Operation.OperationID, &current)
		if err != nil {
			return err
		}
		var old Attempt
		ar, err := tx.Get(ctx, Namespace+".attempts", a.AttemptID, &old)
		if err != nil {
			return err
		}
		if old.FactRevision > f.Revision {
			return nil
		}
		if old.FactRevision == f.Revision {
			if old.FactDigest != digest {
				return api.E("idempotency_conflict", "effect_fact_changed")
			}
			return nil
		}
		if a.Prepared.Cell != nil && !old.CellCommitted {
			valid, err := s.commitCell(ctx, tx, current, old, namespaceRef)
			if err != nil {
				return err
			}
			old.CellCommitted = valid && namespaceRef != nil
			if !valid && f.Effect == "applied" {
				f.Effect = "not_applied"
				outputRef = nil
			}
		}
		if old.Effect == "applied" && f.Effect != "applied" {
			old.EffectDisputed = true
			current.EffectDisputed = true
		} else {
			old.Effect = f.Effect
		}
		old.EffectDisputed = old.EffectDisputed || f.Disputed
		old.MayApplyLater = f.MayApplyLater
		if old.EffectDisputed {
			old.MayApplyLater = "unknown"
		}
		old.Usage = f.Usage
		old.UsageFinal = f.UsageFinal
		old.EvidenceRefs = appendUniqueSources(old.EvidenceRefs, f.Evidence...)
		old.FactRevision = f.Revision
		old.FactDigest = digest
		old.ResultRef = outputRef
		old.ActuallyStopped = noLater(f.MayApplyLater)
		old.Phase = "response_known"
		if noLater(f.MayApplyLater) && f.Effect != "unknown" {
			old.Phase = "reconciled"
		}
		now, err := tx.Now(ctx)
		if err != nil {
			return err
		}
		old.ObservedAt = api.Time(now)
		old.Revision = ar + 1
		if err = tx.Put(ctx, Namespace+".attempts", old.AttemptID, ar, old); err != nil {
			return err
		}
		if err = s.aggregate(ctx, tx, &current); err != nil {
			return err
		}
		if w.Job.Kind == ReconcileJob {
			current.ReconcileCount++
			if current.ReconcileCount >= 10 && current.Operation.Attempts.UnresolvedCount > 0 {
				current.Operation.NextAction = "provide_evidence"
			}
		}
		if old.Phase == "reconciled" {
			if err = s.releaseInflight(ctx, tx, old); err != nil {
				return err
			}
		}
		if err = putOperation(ctx, tx, &current, rev); err != nil {
			return err
		}
		if w.Job.Kind != ReconcileJob && (current.Operation.Effect == "unknown" || !noLater(current.Operation.MayApplyLater) || !current.Operation.UsageFinal) {
			_, err = tx.Raise(ctx, ReconcileJob, current.Operation.OperationID, sc.Ref(current.Operation.OperationID, current.Revision), now.Add(30*time.Second))
		}
		return err
	})
}
func (s *Service) aggregate(ctx context.Context, tx rt.Tx, op *operationRecord) error {
	allNotStarted := true
	allNotApplied := true
	allNoLater := true
	applied := false
	final := true
	unresolved := uint64(0)
	usage := map[string]string{}
	evidence := []api.ContentRef{}
	var result *api.ContentRef
	for _, id := range op.AttemptIDs {
		var a Attempt
		if _, err := tx.Get(ctx, Namespace+".attempts", id, &a); err != nil {
			return err
		}
		allNotStarted = allNotStarted && a.Effect == "not_started"
		allNotApplied = allNotApplied && (a.Effect == "not_applied" || a.Effect == "not_started")
		allNoLater = allNoLater && noLater(a.MayApplyLater)
		applied = applied || a.Effect == "applied"
		final = final && a.UsageFinal
		if a.Effect == "unknown" || !noLater(a.MayApplyLater) || !a.UsageFinal {
			unresolved++
		}
		op.EffectDisputed = op.EffectDisputed || a.EffectDisputed
		for _, u := range a.Usage {
			previous := usage[u.Unit]
			if previous == "" {
				previous = "0"
			}
			sum, err := api.AddDecimal(previous, u.Value)
			if err != nil {
				return err
			}
			usage[u.Unit] = sum
		}
		evidence = append(evidence, a.EvidenceRefs...)
		if a.ResultRef != nil && a.Effect != "unknown" {
			result = a.ResultRef
		}
	}
	op.Operation.Effect = "unknown"
	switch {
	case applied:
		op.Operation.Effect = "applied"
	case allNotStarted && allNoLater:
		op.Operation.Effect = "not_started"
	case allNotApplied && allNoLater:
		op.Operation.Effect = "not_applied"
	}
	op.Operation.MayApplyLater = "unknown"
	if allNoLater {
		op.Operation.MayApplyLater = false
	}
	op.Operation.UsageFinal = final
	op.Operation.Usage = []api.Amount{}
	for unit, value := range usage {
		op.Operation.Usage = append(op.Operation.Usage, api.Amount{Unit: unit, Value: value})
	}
	sortAmounts(op.Operation.Usage)
	op.Operation.EvidenceRefs = evidence
	op.Operation.Attempts.UnresolvedCount = unresolved
	op.Operation.Attempts.CollectionRevision++
	op.ActuallyStopped = allNoLater
	if allNoLater && op.Operation.Effect != "unknown" && !op.EffectDisputed {
		op.Operation.ResultRef = result
		op.NewAttemptsClosed = true
		op.Operation.ExecutionState = "closed"
	}
	op.Operation.NextAction = "query_original"
	if unresolved == 0 {
		op.Operation.NextAction = "none"
	}
	return nil
}
func (s *Service) stopWork(ctx context.Context, st rt.Store, sc rt.Scope, w rt.Work) error {
	op, err := s.loadWork(ctx, st, sc, w)
	if err != nil {
		return err
	}
	if len(op.AttemptIDs) == 0 {
		return s.finish(ctx, st, sc, w, rt.Done(), nil)
	}
	d, err := s.driver(op.Invoke.CapabilityRef)
	if err != nil {
		return err
	}
	if op.Intent == nil {
		return api.E("invalid_state", "attempt_intent_missing")
	}
	stopped := true
	for _, id := range op.AttemptIDs {
		var a Attempt
		if _, err = st.Read(ctx, sc, Namespace+".attempts", id, 0, &a); err != nil {
			return err
		}
		if a.Phase == "prepared" {
			continue
		}
		f, err := d.Stop(ctx, AttemptRequest{Scope: sc, Invoke: op.Invoke, Intent: *op.Intent, Attempt: a, Auth: op.Principal})
		if err != nil {
			return err
		}
		stopped = stopped && f.ActuallyStopped && noLater(f.MayApplyLater)
	}
	disposition := rt.Done()
	if !stopped {
		disposition = rt.Waiting(time.Now().UTC().Add(30 * time.Second))
	}
	return s.finish(ctx, st, sc, w, disposition, func(tx rt.Tx) error {
		var current operationRecord
		rev, err := tx.Get(ctx, Namespace+".operations", op.Operation.OperationID, &current)
		if err != nil {
			return err
		}
		current.ActuallyStopped = stopped
		now, err := tx.Now(ctx)
		if err != nil {
			return err
		}
		if _, err = tx.Raise(ctx, ReconcileJob, current.Operation.OperationID, sc.Ref(current.Operation.OperationID, rev), now); err != nil {
			return err
		}
		return putOperation(ctx, tx, &current, rev)
	})
}

func (s *Service) selectControlWindow(ctx context.Context, st rt.Store, sc rt.Scope, p InvokeInput) (api.ControlSnapshot, error) {
	records, err := st.List(ctx, sc, Namespace+".windows", gateID(p.TaskRef.OwnerID, p.TaskRef.ObjectID), "", 100)
	if err != nil {
		return api.ControlSnapshot{}, err
	}
	if len(records) >= 100 {
		return api.ControlSnapshot{}, api.E("overloaded", "control_window_set_requires_batch")
	}
	selected := p.ControlSnapshot
	now := time.Now().UTC()
	var latest time.Time
	for _, r := range records {
		var candidate api.ControlSnapshot
		if err = r.Decode(&candidate); err != nil {
			return api.ControlSnapshot{}, err
		}
		if candidate.ControlRevision != p.ControlRevision || candidate.GoalRevision != p.GoalRevision || candidate.Status != "active" || candidate.Control != "running" {
			continue
		}
		issued, err := api.ParseTime(candidate.IssuedAt)
		if err != nil {
			return api.ControlSnapshot{}, err
		}
		before, err := api.ParseTime(candidate.StartBefore)
		if err != nil {
			return api.ControlSnapshot{}, err
		}
		if now.Before(before) && (latest.IsZero() || issued.After(latest)) {
			latest = issued
			selected = candidate
		}
	}
	return selected, nil
}

// 门禁→Operation→Attempt→Job。门禁预先锁定，避免 control/takeover/stop 与出口逆序。
func lockAttemptGates(ctx context.Context, tx rt.Tx, p InvokeInput, a Attempt, task bool) error {
	if task {
		var gate TaskGate
		if _, err := tx.Get(ctx, Namespace+".gates", gateID(p.TaskRef.OwnerID, p.TaskRef.ObjectID), &gate); err != nil {
			return err
		}
	}
	if a.Prepared.ResourceID != "" {
		var resource ResourceLease
		if _, err := tx.Get(ctx, Namespace+".resources", a.Prepared.ResourceID, &resource); err != nil {
			return err
		}
	}
	if a.Prepared.Cell != nil {
		var env Environment
		if _, err := tx.Get(ctx, Namespace+".environments", a.Prepared.Cell.EnvironmentRef.ObjectID, &env); err != nil {
			return err
		}
	}
	return nil
}
