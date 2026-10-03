package execution

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/ruipengliu/lerna/api"
	rt "github.com/ruipengliu/lerna/runtime"
)

type Service struct {
	cfg     Config
	drivers map[string]Driver
}

func driverKey(r api.ComponentRef) string { return r.ComponentID + "/" + r.Version + "/" + r.Digest }
func New(c Config) (*Service, error) {
	if !api.ValidID(c.OwnerID) || c.Location == "" {
		return nil, api.E("invalid_request", "invalid_executor_configuration")
	}
	s := &Service{cfg: c, drivers: map[string]Driver{}}
	for _, d := range c.Drivers {
		cap := d.Capability()
		if cap.MaxAttempts < 1 || cap.MaxAttempts > 100 || cap.Ref.ComponentID == "" || cap.Ref.Digest == "" {
			return nil, api.E("invalid_request", "invalid_capability")
		}
		if cap.EffectClass != "read_only" && cap.EffectClass != "target_idempotent" && cap.EffectClass != "no_idempotency_guarantee" {
			return nil, api.E("invalid_request", "invalid_effect_class")
		}
		k := driverKey(cap.Ref)
		if s.drivers[k] != nil {
			return nil, api.E("invalid_request", "duplicate_capability")
		}
		if _, err := api.NewValidator(cap.InputSchema); err != nil {
			return nil, err
		}
		if _, err := api.NewValidator(cap.OutputSchema); err != nil {
			return nil, err
		}
		s.drivers[k] = d
	}
	return s, nil
}
func (s *Service) participants() []string {
	p := []string{Namespace}
	for _, x := range s.cfg.AuthorityParticipants {
		if x != Namespace {
			p = append(p, x)
		}
	}
	return p
}
func RegisterCommand[I, O any](r *rt.Registry, name string, cas bool, parts []string, fn func(context.Context, rt.Tx, rt.Auth, api.Command, I) (O, error)) error {
	return r.Register(rt.Method{Contract: closedContract[I, O](name, "command", cas, false), Participants: parts, Apply: func(ctx context.Context, tx rt.Tx, a rt.Auth, c api.Command) (rt.Outcome, error) {
		var p I
		if err := api.Decode(c.Payload, &p); err != nil {
			return rt.Outcome{}, err
		}
		v, err := fn(ctx, tx, a, c, p)
		return rt.Applied(v), err
	}})
}
func RegisterQuery[I, O any](r *rt.Registry, name string, fn func(context.Context, rt.Store, rt.Scope, rt.Auth, api.Query, I) (O, error)) error {
	return r.Register(rt.Method{Contract: closedContract[I, O](name, "query", false, false), Query: func(ctx context.Context, st rt.Store, sc rt.Scope, a rt.Auth, q api.Query) (any, error) {
		var p I
		if err := api.Decode(q.Payload, &p); err != nil {
			return nil, err
		}
		return fn(ctx, st, sc, a, q, p)
	}})
}
func (s *Service) Register(r *rt.Registry) error {
	registrations := []func() error{
		func() error { return RegisterCommand(r, "execution.invoke", false, s.participants(), s.invoke) },
		func() error { return RegisterCommand(r, "execution.cancel", false, s.participants(), s.cancel) },
		func() error { return RegisterCommand(r, "execution.control", false, s.participants(), s.control) },
		func() error {
			return RegisterCommand(r, "execution.reconcile", false, []string{Namespace}, s.reconcile)
		},
		func() error { return RegisterQuery(r, "execution.get", s.get) },
		func() error { return RegisterQuery(r, "execution.list", s.list) },
		func() error { return RegisterQuery(r, "execution.control.get", s.controlGet) },
		func() error { return RegisterQuery(r, "execution.usage.get", s.usageGet) },
	}
	for _, f := range registrations {
		if err := f(); err != nil {
			return err
		}
	}
	for _, x := range []struct {
		k string
		h rt.JobHandler
	}{{RunJob, s.run}, {ReconcileJob, s.reconcileWork}, {StopJob, s.stopWork}} {
		if err := r.RegisterJob(x.k, x.h); err != nil {
			return err
		}
	}
	if err := s.registerResources(r); err != nil {
		return err
	}
	return s.registerEnvironments(r)
}
func gateID(owner, task string) string { return owner + ":" + task }
func controlDigest(p api.ControlSnapshot) (string, error) {
	return api.Digest(gateFact{p.OrchestratorID, p.TaskID, p.GoalRevision, p.ControlRevision, p.Status, p.Control})
}
func (s *Service) saveControl(ctx context.Context, tx rt.Tx, a rt.Auth, task api.ObjectRef, p api.ControlSnapshot) (TaskGate, error) {
	if s.cfg.Authority == nil {
		return TaskGate{}, api.E("dependency_unavailable", "authority_not_configured")
	}
	if err := rt.CheckRef(tx.Scope(), task); err != nil {
		return TaskGate{}, err
	}
	if task.OwnerID != p.OrchestratorID || task.ObjectID != p.TaskID || p.GoalRevision == 0 || p.ControlRevision == 0 || !api.ValidID(p.WindowID) {
		return TaskGate{}, api.E("invalid_request", "control_binding_mismatch")
	}
	issued, err := api.ParseTime(p.IssuedAt)
	if err != nil {
		return TaskGate{}, api.E("invalid_request", "invalid_control_time")
	}
	before, err := api.ParseTime(p.StartBefore)
	if err != nil || !before.After(issued) {
		return TaskGate{}, api.E("invalid_request", "invalid_control_window")
	}
	if err = s.cfg.Authority.VerifyControl(ctx, tx, a, p); err != nil {
		return TaskGate{}, err
	}
	digest, err := controlDigest(p)
	if err != nil {
		return TaskGate{}, err
	}
	key := gateID(task.OwnerID, task.ObjectID)
	var g TaskGate
	rev, err := tx.Get(ctx, Namespace+".gates", key, &g)
	if api.IsCode(err, "not_found") {
		g = TaskGate{TaskRef: task, Revision: 1, GoalRevision: p.GoalRevision, ControlRevision: p.ControlRevision, Status: p.Status, Control: p.Control, ControlDigest: digest}
		if err = tx.Create(ctx, Namespace+".gates", key, task.ObjectID, g); err != nil {
			return TaskGate{}, err
		}
	} else if err != nil {
		return TaskGate{}, err
	} else if p.ControlRevision < g.ControlRevision {
		return TaskGate{}, api.E("revision_conflict", "task_gate_stale")
	} else if p.ControlRevision == g.ControlRevision {
		if digest != g.ControlDigest {
			return TaskGate{}, api.E("idempotency_conflict", "control_fact_changed")
		}
	} else {
		g = TaskGate{TaskRef: task, Revision: rev + 1, GoalRevision: p.GoalRevision, ControlRevision: p.ControlRevision, Status: p.Status, Control: p.Control, ControlDigest: digest}
		if err = tx.Put(ctx, Namespace+".gates", key, rev, g); err != nil {
			return TaskGate{}, err
		}
	}
	var original api.ControlSnapshot
	if _, err = tx.Get(ctx, Namespace+".windows", p.WindowID, &original); err == nil {
		if !api.Equal(original, p) {
			return TaskGate{}, api.E("idempotency_conflict", "control_window_changed")
		}
	} else if api.IsCode(err, "not_found") {
		if err = tx.Create(ctx, Namespace+".windows", p.WindowID, key, p); err != nil {
			return TaskGate{}, err
		}
	} else {
		return TaskGate{}, err
	}
	return g, nil
}
func permittedGate(g TaskGate) bool {
	return g.Status == "active" && g.Control == "running"
}
func (s *Service) invoke(ctx context.Context, tx rt.Tx, a rt.Auth, c api.Command, p InvokeInput) (OperationOutput, error) {
	if p.OperationID != c.TargetID || p.TaskRef.TenantID != a.TenantID || p.GoalRevision == 0 || p.ControlRevision == 0 || !api.ValidID(p.OperationID) {
		return OperationOutput{}, api.E("invalid_request", "invoke_binding_mismatch")
	}
	if s.drivers[driverKey(p.CapabilityRef)] == nil {
		return OperationOutput{}, api.E("unsupported", "capability_not_ready")
	}
	g, err := s.saveControl(ctx, tx, a, p.TaskRef, p.ControlSnapshot)
	if err != nil {
		return OperationOutput{}, err
	}
	if g.GoalRevision != p.GoalRevision || g.ControlRevision != p.ControlRevision || !permittedGate(g) || g.Status == "cancelled" || g.Status == "succeeded" || g.Status == "failed" {
		return OperationOutput{}, api.E("invalid_state", "task_gate_stale")
	}
	deadline, err := api.ParseTime(p.Deadline)
	if err != nil {
		return OperationOutput{}, api.E("invalid_request", "invalid_operation_deadline")
	}
	now, err := tx.Now(ctx)
	if err != nil {
		return OperationOutput{}, err
	}
	if !now.Before(deadline) {
		return OperationOutput{}, api.E("expired", "operation_expired")
	}
	for _, ref := range append(append([]api.ObjectRef{p.BindingRef, p.ReservationRef}, p.UseRefs...), p.TaskRef) {
		if err = rt.CheckRef(tx.Scope(), ref); err != nil {
			return OperationOutput{}, err
		}
	}
	if len(p.UseRefs) == 0 {
		return OperationOutput{}, api.E("forbidden", "use_receipt_required")
	}
	if p.IntentRef.TenantID != a.TenantID {
		return OperationOutput{}, api.E("forbidden", "intent_scope_mismatch")
	}
	digest, err := api.Digest(p)
	if err != nil {
		return OperationOutput{}, err
	}
	var old operationRecord
	if _, err = tx.Get(ctx, Namespace+".operations", p.OperationID, &old); err == nil {
		if old.Tombstone {
			return OperationOutput{}, api.E("invalid_state", "operation_permanently_cancelled")
		}
		if digest != old.InputDigest {
			return OperationOutput{}, api.E("idempotency_conflict", "intent_mismatch")
		}
		return operationOutput(tx.Scope(), old), nil
	} else if !api.IsCode(err, "not_found") {
		return OperationOutput{}, err
	}
	rec := operationRecord{Operation: api.Operation{OperationID: p.OperationID, OwnerID: s.cfg.OwnerID, TaskRef: p.TaskRef, Revision: 1, ExecutionState: "accepted", Effect: "not_started", MayApplyLater: false, Attempts: api.CollectionSummary{CollectionRevision: 1, Complete: true}, EvidenceRefs: []api.ContentRef{}, Usage: []api.Amount{}, NextAction: "wait"}, Invoke: p, Revision: 1, InputDigest: digest, Principal: a, ActuallyStopped: true, AttemptIDs: []string{}}
	if err = tx.Create(ctx, Namespace+".operations", p.OperationID, p.TaskRef.ObjectID, rec); err != nil {
		return OperationOutput{}, err
	}
	_, err = tx.Raise(ctx, RunJob, p.OperationID, tx.Scope().Ref(p.OperationID, 1), now)
	return operationOutput(tx.Scope(), rec), err
}
func operationOutput(sc rt.Scope, r operationRecord) OperationOutput {
	return OperationOutput{OperationRef: sc.Ref(r.Operation.OperationID, r.Revision), ExecutionState: r.Operation.ExecutionState, Effect: r.Operation.Effect, MayApplyLater: r.Operation.MayApplyLater, NewAttemptsClosed: r.NewAttemptsClosed, ActuallyStopped: r.ActuallyStopped}
}
func (s *Service) cancel(ctx context.Context, tx rt.Tx, a rt.Auth, c api.Command, p CancelInput) (OperationOutput, error) {
	if p.OperationID != c.TargetID || p.OrchestratorID != p.TaskRef.OwnerID || p.TaskRef.TenantID != a.TenantID {
		return OperationOutput{}, api.E("invalid_request", "cancel_binding_mismatch")
	}
	if !a.HasRole("orchestrator") && !a.HasRole("executor") && !a.HasRole("admin") && a.SubjectID != p.OrchestratorID {
		return OperationOutput{}, api.E("forbidden", "cancel_source_untrusted")
	}
	if err := rt.CheckRef(tx.Scope(), p.TaskRef); err != nil {
		return OperationOutput{}, err
	}
	var rec operationRecord
	rev, err := tx.Get(ctx, Namespace+".operations", p.OperationID, &rec)
	if api.IsCode(err, "not_found") {
		rec = operationRecord{Operation: api.Operation{OperationID: p.OperationID, OwnerID: s.cfg.OwnerID, TaskRef: p.TaskRef, Revision: 1, ExecutionState: "closed", Effect: "not_started", MayApplyLater: false, Attempts: api.CollectionSummary{CollectionRevision: 1, Complete: true}, EvidenceRefs: []api.ContentRef{}, Usage: []api.Amount{}, UsageFinal: true, NextAction: "none"}, Revision: 1, Principal: a, NewAttemptsClosed: true, ActuallyStopped: true, CancelReason: p.Reason, Tombstone: true, AttemptIDs: []string{}}
		if err = tx.Create(ctx, Namespace+".operations", p.OperationID, p.TaskRef.ObjectID, rec); err != nil {
			return OperationOutput{}, err
		}
		return operationOutput(tx.Scope(), rec), nil
	}
	if err != nil {
		return OperationOutput{}, err
	}
	if rec.Operation.TaskRef.OwnerID != p.OrchestratorID || rec.Operation.TaskRef.ObjectID != p.TaskRef.ObjectID {
		return OperationOutput{}, api.E("forbidden", "cancel_task_mismatch")
	}
	if !rec.NewAttemptsClosed {
		rec.NewAttemptsClosed = true
		rec.CancelReason = p.Reason
		rec.Operation.ExecutionState = "closed"
		if len(rec.AttemptIDs) == 0 {
			rec.Operation.UsageFinal = true
			rec.Operation.NextAction = "none"
		}
		if err = putOperation(ctx, tx, &rec, rev); err != nil {
			return OperationOutput{}, err
		}
	}
	now, err := tx.Now(ctx)
	if err != nil {
		return OperationOutput{}, err
	}
	if _, err = tx.Raise(ctx, StopJob, p.OperationID, tx.Scope().Ref(p.OperationID, rec.Revision), now); err != nil {
		return OperationOutput{}, err
	}
	return operationOutput(tx.Scope(), rec), nil
}
func putOperation(ctx context.Context, tx rt.Tx, r *operationRecord, old uint64) error {
	r.Revision = old + 1
	r.Operation.Revision = r.Revision
	return tx.Put(ctx, Namespace+".operations", r.Operation.OperationID, old, *r)
}
func (s *Service) control(ctx context.Context, tx rt.Tx, a rt.Auth, c api.Command, p ControlInput) (ControlView, error) {
	if c.TargetID != p.TaskRef.ObjectID {
		return ControlView{}, api.E("invalid_request", "control_target_mismatch")
	}
	g, err := s.saveControl(ctx, tx, a, p.TaskRef, p.Snapshot)
	if err != nil {
		return ControlView{}, err
	}
	if !permittedGate(g) {
		records, err := tx.List(ctx, Namespace+".operations", p.TaskRef.ObjectID, "", 100)
		if err != nil {
			return ControlView{}, err
		}
		if len(records) == 100 {
			return ControlView{}, api.E("overloaded", "control_target_set_requires_batch")
		}
		now, err := tx.Now(ctx)
		if err != nil {
			return ControlView{}, err
		}
		for _, r := range records {
			var op operationRecord
			if err = r.Decode(&op); err != nil {
				return ControlView{}, err
			}
			if !op.NewAttemptsClosed {
				op.NewAttemptsClosed = true
				op.Operation.ExecutionState = "closed"
				if err = putOperation(ctx, tx, &op, r.Revision); err != nil {
					return ControlView{}, err
				}
				if _, err = tx.Raise(ctx, StopJob, op.Operation.OperationID, tx.Scope().Ref(op.Operation.OperationID, op.Revision), now); err != nil {
					return ControlView{}, err
				}
			}
		}
	}
	return ControlView{Gate: g, Windows: []api.ControlSnapshot{p.Snapshot}}, nil
}
func (s *Service) reconcile(ctx context.Context, tx rt.Tx, a rt.Auth, c api.Command, p ReconcileInput) (OperationOutput, error) {
	var rec operationRecord
	if _, err := tx.Get(ctx, Namespace+".operations", p.OperationID, &rec); err != nil {
		return OperationOutput{}, err
	}
	if p.OperationID != c.TargetID {
		return OperationOutput{}, api.E("invalid_request", "operation_target_mismatch")
	}
	if err := disclose(a, rec); err != nil {
		return OperationOutput{}, err
	}
	now, err := tx.Now(ctx)
	if err != nil {
		return OperationOutput{}, err
	}
	if err = putOperation(ctx, tx, &rec, rec.Revision); err != nil {
		return OperationOutput{}, err
	}
	_, err = tx.Raise(ctx, ReconcileJob, p.OperationID, tx.Scope().Ref(p.OperationID, rec.Revision), now)
	return operationOutput(tx.Scope(), rec), err
}
func disclose(a rt.Auth, r operationRecord) error {
	if a.SubjectID == r.Principal.SubjectID || a.HasRole("admin") || a.HasRole("orchestrator") || a.HasRole("executor") {
		return nil
	}
	return api.E("forbidden", "operation_not_disclosed")
}
func (s *Service) get(ctx context.Context, st rt.Store, sc rt.Scope, a rt.Auth, q api.Query, p OperationIDInput) (OperationView, error) {
	var rec operationRecord
	if p.OperationID != q.TargetID {
		return OperationView{}, api.E("invalid_request", "operation_target_mismatch")
	}
	if _, err := st.Read(ctx, sc, Namespace+".operations", p.OperationID, 0, &rec); err != nil {
		return OperationView{}, err
	}
	if err := disclose(a, rec); err != nil {
		return OperationView{}, err
	}
	records, err := st.List(ctx, sc, Namespace+".attempts", p.OperationID, "", 100)
	if err != nil {
		return OperationView{}, err
	}
	attempts := []AttemptView{}
	for _, r := range records {
		var x Attempt
		if err = r.Decode(&x); err != nil {
			return OperationView{}, err
		}
		attempts = append(attempts, publicAttempt(x))
	}
	return OperationView{Operation: rec.Operation, NewAttemptsClosed: rec.NewAttemptsClosed, ActuallyStopped: rec.ActuallyStopped, EffectDisputed: rec.EffectDisputed, Attempts: api.Page[AttemptView]{Items: attempts, CollectionRevision: rec.Operation.Attempts.CollectionRevision, Exhausted: len(records) < 100, Partial: len(records) == 100, Gaps: []string{}}}, nil
}
func (s *Service) list(ctx context.Context, st rt.Store, sc rt.Scope, a rt.Auth, q api.Query, p api.ListInput) (api.Page[OperationOutput], error) {
	if p.Limit < 1 || p.Limit > 100 {
		return api.Page[OperationOutput]{}, api.E("invalid_request", "invalid_page_limit")
	}
	records, err := st.List(ctx, sc, Namespace+".operations", "", p.Cursor, int(p.Limit)+1)
	if err != nil {
		return api.Page[OperationOutput]{}, err
	}
	page := api.Page[OperationOutput]{Items: []OperationOutput{}, CollectionRevision: 1, Gaps: []string{}}
	for i, r := range records {
		if i == int(p.Limit) {
			page.NextCursor = records[i-1].ID
			break
		}
		var x operationRecord
		if err = r.Decode(&x); err != nil {
			return page, err
		}
		if err = disclose(a, x); err != nil {
			return page, err
		}
		page.Items = append(page.Items, operationOutput(sc, x))
		page.CollectionRevision += r.Revision
	}
	page.Exhausted = len(records) <= int(p.Limit)
	return page, nil
}
func (s *Service) controlGet(ctx context.Context, st rt.Store, sc rt.Scope, a rt.Auth, q api.Query, p ControlGetInput) (ControlView, error) {
	if !a.HasRole("admin") && !a.HasRole("orchestrator") && !a.HasRole("executor") {
		return ControlView{}, api.E("forbidden", "control_not_disclosed")
	}
	if p.TaskID != q.TargetID {
		return ControlView{}, api.E("invalid_request", "task_target_mismatch")
	}
	records, err := st.List(ctx, sc, Namespace+".gates", p.TaskID, "", 2)
	if err != nil {
		return ControlView{}, err
	}
	if len(records) != 1 {
		return ControlView{}, api.E("not_found", "task_gate_not_found")
	}
	var g TaskGate
	if err = records[0].Decode(&g); err != nil {
		return ControlView{}, err
	}
	windows, err := st.List(ctx, sc, Namespace+".windows", records[0].ID, "", 100)
	if err != nil {
		return ControlView{}, err
	}
	out := ControlView{Gate: g, Windows: []api.ControlSnapshot{}}
	for _, r := range windows {
		var w api.ControlSnapshot
		if err = r.Decode(&w); err != nil {
			return out, err
		}
		out.Windows = append(out.Windows, w)
	}
	return out, nil
}
func minDeadline(now time.Time, deadlines ...string) error {
	for _, v := range deadlines {
		if v == "" {
			return api.E("forbidden", "start_deadline_missing")
		}
		t, err := api.ParseTime(v)
		if err != nil || !now.Before(t) {
			return api.E("expired", "use_window_expired")
		}
	}
	return nil
}
func validFact(f Fact) error {
	if f.Revision < 1 || f.Revision > api.MaxSafeInteger {
		return api.E("invalid_request", "invalid_fact_revision")
	}
	if f.Effect != "applied" && f.Effect != "not_applied" && f.Effect != "unknown" && f.Effect != "not_started" {
		return api.E("invalid_request", "invalid_effect")
	}
	switch v := f.MayApplyLater.(type) {
	case bool:
		_ = v
	case string:
		if v != "unknown" {
			return api.E("invalid_request", "invalid_may_apply_later")
		}
	default:
		return api.E("invalid_request", "invalid_may_apply_later")
	}
	return api.ValidateAmounts(f.Usage)
}
func noLater(x any) bool { v, ok := x.(bool); return ok && !v }
func (s *Service) driver(ref api.ComponentRef) (Driver, error) {
	d := s.drivers[driverKey(ref)]
	if d == nil {
		return nil, api.E("unsupported", "capability_not_ready")
	}
	return d, nil
}
func businessError(err error) bool {
	var e *api.Error
	return errors.As(err, &e) && e.Code != "dependency_unavailable" && e.Code != "overloaded"
}
func validateIntent(p InvokeInput, i ExecutionIntent) error {
	if i.OperationID != p.OperationID || !api.Equal(i.TaskRef, p.TaskRef) || i.ExecutorID == "" || i.GoalRevision != p.GoalRevision || i.ControlRevision != p.ControlRevision || !api.Equal(i.CapabilityRef, p.CapabilityRef) || !api.Equal(i.BindingRef, p.BindingRef) || i.Deadline != p.Deadline {
		return api.E("idempotency_conflict", "intent_mismatch")
	}
	if i.AdmissionPurpose != "goal_action" && i.AdmissionPurpose != "requirement_check" {
		return api.E("invalid_request", "invalid_admission_purpose")
	}
	if !strings.Contains("|decision|plan|check|hostcall|", "|"+i.AdmissionSourceKind+"|") {
		return api.E("invalid_request", "invalid_admission_source")
	}
	return api.ValidateAmounts(i.CostBound)
}
