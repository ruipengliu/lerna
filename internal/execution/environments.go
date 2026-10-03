package execution

import (
	"context"
	"time"

	"github.com/ruipengliu/lerna/api"
	rt "github.com/ruipengliu/lerna/runtime"
)

const EnvironmentPrepareJob = "execution.environment.prepare"
const EnvironmentCleanupJob = "execution.environment.cleanup"
const HostCallJob = "execution.environment.hostcall"

// 首版环境只保存被动数据；不开放任何用户程序、原生进程、网络或宿主文件能力。
const PassiveEnvironmentFormat = "harness-passive-namespace/1"

type NamespaceBinding struct {
	Name    string `json:"name"`
	Kind    string `json:"kind"`
	String  string `json:"string,omitempty"`
	Decimal string `json:"decimal,omitempty"`
	Bool    *bool  `json:"bool,omitempty"`
}
type PassiveNamespace struct {
	Format   string             `json:"format"`
	Bindings []NamespaceBinding `json:"bindings"`
}
type Environment struct {
	EnvironmentID        string           `json:"environment_id"`
	Revision             uint64           `json:"revision"`
	ConfigRef            api.ComponentRef `json:"config_ref"`
	IsolationDigest      string           `json:"isolation_digest"`
	InstallLockRef       api.ComponentRef `json:"install_lock_ref"`
	InstanceID           string           `json:"instance_id"`
	Generation           uint64           `json:"generation"`
	Phase                string           `json:"phase"`
	RuntimeKind          string           `json:"runtime_kind"`
	Limits               []api.Amount     `json:"limits"`
	ExpiresAt            string           `json:"expires_at"`
	ProcessedSources     []api.ContentRef `json:"processed_sources"`
	NamespaceRef         *api.ContentRef  `json:"namespace_ref,omitempty"`
	NamespaceRevision    uint64           `json:"namespace_revision"`
	ReadyForCell         bool             `json:"ready_for_cell"`
	ActiveOperationIDs   []string         `json:"active_operation_ids"`
	StopResiduals        []string         `json:"stop_residuals"`
	ActuallyExited       bool             `json:"actually_exited"`
	HostCallIDs          []string         `json:"hostcall_ids"`
	HostCallParents      []string         `json:"hostcall_parents"`
	Principal            rt.Auth          `json:"principal"`
	PreparationCommandID string           `json:"preparation_command_id"`
}
type EnvironmentCreateInput struct {
	EnvironmentID  string           `json:"environment_id"`
	ConfigRef      api.ComponentRef `json:"config_ref"`
	InstallLockRef api.ComponentRef `json:"install_lock_ref"`
	Limits         []api.Amount     `json:"limits"`
	ExpiresAt      string           `json:"expires_at"`
	SourceRefs     []api.ContentRef `json:"source_refs"`
}
type EnvironmentIDInput struct {
	EnvironmentID string `json:"environment_id"`
}
type EnvironmentStopInput struct {
	EnvironmentID      string `json:"environment_id"`
	ExpectedGeneration uint64 `json:"expected_generation"`
	Reason             string `json:"reason"`
}
type EnvironmentDestroyInput struct {
	EnvironmentID      string `json:"environment_id"`
	ExpectedGeneration uint64 `json:"expected_generation"`
}
type EnvironmentCheckpointInput struct {
	EnvironmentID             string `json:"environment_id"`
	ExpectedGeneration        uint64 `json:"expected_generation"`
	ExpectedNamespaceRevision uint64 `json:"expected_namespace_revision"`
}
type EnvironmentRestoreInput struct {
	EnvironmentID      string           `json:"environment_id"`
	ExpectedGeneration uint64           `json:"expected_generation"`
	CheckpointRef      api.ObjectRef    `json:"checkpoint_ref"`
	ConfigRef          api.ComponentRef `json:"config_ref"`
	InstallLockRef     api.ComponentRef `json:"install_lock_ref"`
}
type Checkpoint struct {
	CheckpointID      string           `json:"checkpoint_id"`
	EnvironmentRef    api.ObjectRef    `json:"environment_ref"`
	Generation        uint64           `json:"generation"`
	NamespaceRevision uint64           `json:"namespace_revision"`
	ContentRef        api.ContentRef   `json:"content_ref"`
	ConfigRef         api.ComponentRef `json:"config_ref"`
	InstallLockRef    api.ComponentRef `json:"install_lock_ref"`
	Format            string           `json:"format"`
	ProcessedSources  []api.ContentRef `json:"processed_sources"`
	CreatedAt         string           `json:"created_at"`
}

func (s *Service) registerEnvironments(r *rt.Registry) error {
	fs := []func() error{
		func() error {
			return r.Register(rt.Method{Contract: api.Contract[EnvironmentCreateInput, Environment]("environment.create", Namespace, "command", false, true), Participants: []string{Namespace}, Apply: func(ctx context.Context, tx rt.Tx, a rt.Auth, c api.Command) (rt.Outcome, error) {
				var p EnvironmentCreateInput
				if err := api.Decode(c.Payload, &p); err != nil {
					return rt.Outcome{}, err
				}
				v, err := s.environmentCreate(ctx, tx, a, c, p)
				return rt.Accepted(v), err
			}})
		},
		func() error {
			return RegisterCommand(r, "environment.stop", true, []string{Namespace}, s.environmentStop)
		},
		func() error {
			return RegisterCommand(r, "environment.destroy", true, []string{Namespace}, s.environmentDestroy)
		},
		func() error {
			return RegisterCommand(r, "environment.checkpoint", true, []string{Namespace}, s.environmentCheckpoint)
		},
		func() error {
			return RegisterCommand(r, "environment.restore", true, []string{Namespace}, s.environmentRestore)
		},
		func() error { return RegisterQuery(r, "environment.get", s.environmentGet) },
		func() error { return RegisterQuery(r, "environment.list", s.environmentList) },
	}
	for _, f := range fs {
		if err := f(); err != nil {
			return err
		}
	}
	if err := r.RegisterJob(EnvironmentPrepareJob, s.environmentPrepareWork); err != nil {
		return err
	}
	if err := r.RegisterJob(EnvironmentCleanupJob, s.environmentCleanupWork); err != nil {
		return err
	}
	return r.RegisterJob(HostCallJob, s.hostCallWork)
}
func (s *Service) environmentCreate(ctx context.Context, tx rt.Tx, a rt.Auth, c api.Command, p EnvironmentCreateInput) (Environment, error) {
	if p.EnvironmentID != c.TargetID || !api.ValidID(p.EnvironmentID) {
		return Environment{}, api.E("invalid_request", "environment_identity_mismatch")
	}
	passive := p.ConfigRef.ComponentID == BuiltinComponentID("environment.passive") && p.ConfigRef.Version == "1" && p.ConfigRef.Digest == api.Hash([]byte(PassiveEnvironmentFormat))
	isolation := EnvironmentIsolation{RuntimeKind: "trusted_passive_data", IsolationDigest: p.ConfigRef.Digest}
	if !passive {
		if s.cfg.EnvironmentAdmission == nil {
			return Environment{}, api.E("unsupported", "untrusted_program_isolation_not_verified")
		}
		var err error
		isolation, err = s.cfg.EnvironmentAdmission.Check(p.ConfigRef, p.InstallLockRef, p.Limits)
		if err != nil {
			return Environment{}, err
		}
	}
	if s.cfg.Content == nil {
		return Environment{}, api.E("dependency_unavailable", "content_not_configured")
	}
	if err := api.ValidateAmounts(p.Limits); err != nil {
		return Environment{}, err
	}
	if passive && (len(p.Limits) != 1 || p.Limits[0].Unit != "namespace_bytes") {
		return Environment{}, api.E("unsupported", "passive_environment_limits_not_supported")
	}
	if _, err := NamespaceByteLimit(p.Limits); err != nil {
		return Environment{}, err
	}
	now, err := tx.Now(ctx)
	if err != nil {
		return Environment{}, err
	}
	expires, err := api.ParseTime(p.ExpiresAt)
	if err != nil || !expires.After(now) {
		return Environment{}, api.E("expired", "environment_expired")
	}
	env := Environment{EnvironmentID: p.EnvironmentID, Revision: 1, ConfigRef: p.ConfigRef, IsolationDigest: isolation.IsolationDigest, InstallLockRef: p.InstallLockRef, InstanceID: api.NewID("instance"), Generation: 1, Phase: "preparing", RuntimeKind: isolation.RuntimeKind, Limits: p.Limits, ExpiresAt: p.ExpiresAt, ProcessedSources: p.SourceRefs, ActiveOperationIDs: []string{}, StopResiduals: []string{}, HostCallIDs: []string{}, HostCallParents: []string{}, Principal: a, PreparationCommandID: c.CommandID, ActuallyExited: true}
	if err = tx.Create(ctx, Namespace+".environments", p.EnvironmentID, "", env); err != nil {
		return env, err
	}
	if err = bumpCollection(ctx, tx, "environments", true); err != nil {
		return env, err
	}
	_, err = tx.Raise(ctx, EnvironmentPrepareJob, p.EnvironmentID, tx.Scope().Ref(p.EnvironmentID, 1), now)
	return env, err
}
func readEnvironment(ctx context.Context, tx rt.Tx, a rt.Auth, c api.Command, id string, generation uint64) (Environment, uint64, error) {
	var env Environment
	rev, err := tx.Get(ctx, Namespace+".environments", id, &env)
	if err != nil {
		return env, rev, err
	}
	if id != c.TargetID || c.ExpectedRevision == nil || *c.ExpectedRevision != rev {
		return env, rev, api.E("revision_conflict", "environment_revision_changed")
	}
	if generation != env.Generation {
		return env, rev, api.E("revision_conflict", "generation_changed")
	}
	if a.SubjectID != env.Principal.SubjectID && !a.HasRole("executor") && !a.HasRole("admin") {
		return env, rev, api.E("forbidden", "environment_not_disclosed")
	}
	return env, rev, nil
}
func (s *Service) environmentStop(ctx context.Context, tx rt.Tx, a rt.Auth, c api.Command, p EnvironmentStopInput) (Environment, error) {
	env, rev, err := readEnvironment(ctx, tx, a, c, p.EnvironmentID, p.ExpectedGeneration)
	if err != nil {
		return env, err
	}
	if env.Phase == "closed" {
		return env, nil
	}
	if env.Phase != "active" && env.Phase != "preparing" && env.Phase != "closing" {
		return env, api.E("invalid_state", "environment_not_stoppable")
	}
	if env.Phase != "closing" {
		env.Generation++
		env.Phase = "closing"
		env.ReadyForCell = false
		env.Revision = rev + 1
		env.StopResiduals = append([]string{}, env.HostCallIDs...)
		if err = putEnvironment(ctx, tx, env.EnvironmentID, rev, env); err != nil {
			return env, err
		}
	}
	now, err := tx.Now(ctx)
	if err != nil {
		return env, err
	}
	for _, id := range env.ActiveOperationIDs {
		var op operationRecord
		or, err := tx.Get(ctx, Namespace+".operations", id, &op)
		if err != nil {
			return env, err
		}
		op.NewAttemptsClosed = true
		op.Operation.ExecutionState = "closed"
		if err = putOperation(ctx, tx, &op, or); err != nil {
			return env, err
		}
		if _, err = tx.Raise(ctx, StopJob, id, tx.Scope().Ref(id, op.Revision), now); err != nil {
			return env, err
		}
	}
	for _, id := range env.HostCallIDs {
		var call HostCall
		callRevision, e := tx.Get(ctx, Namespace+".hostcalls", id, &call)
		if e != nil {
			return env, e
		}
		call.Revision = callRevision + 1
		if e = tx.Put(ctx, Namespace+".hostcalls", id, callRevision, call); e != nil {
			return env, e
		}
		if _, err = tx.Raise(ctx, HostCallJob, id, tx.Scope().Ref(id, call.Revision), now); err != nil {
			return env, err
		}
	}
	_, err = tx.Raise(ctx, EnvironmentCleanupJob, env.EnvironmentID, tx.Scope().Ref(env.EnvironmentID, env.Revision), now)
	return env, err
}
func (s *Service) environmentDestroy(ctx context.Context, tx rt.Tx, a rt.Auth, c api.Command, p EnvironmentDestroyInput) (Environment, error) {
	env, rev, err := readEnvironment(ctx, tx, a, c, p.EnvironmentID, p.ExpectedGeneration)
	if err != nil {
		return env, err
	}
	if env.Phase == "destroyed" {
		return env, nil
	}
	if env.Phase != "closed" || !env.ActuallyExited || len(env.StopResiduals) > 0 {
		return env, api.E("invalid_state", "environment_not_closed")
	}
	env.Phase = "destroying"
	env.Revision = rev + 1
	env.ReadyForCell = false
	if err = putEnvironment(ctx, tx, env.EnvironmentID, rev, env); err != nil {
		return env, err
	}
	now, err := tx.Now(ctx)
	if err != nil {
		return env, err
	}
	_, err = tx.Raise(ctx, EnvironmentCleanupJob, env.EnvironmentID, tx.Scope().Ref(env.EnvironmentID, env.Revision), now)
	return env, err
}
func (s *Service) environmentCheckpoint(ctx context.Context, tx rt.Tx, a rt.Auth, c api.Command, p EnvironmentCheckpointInput) (Checkpoint, error) {
	env, _, err := readEnvironment(ctx, tx, a, c, p.EnvironmentID, p.ExpectedGeneration)
	if err != nil {
		return Checkpoint{}, err
	}
	if p.ExpectedNamespaceRevision != env.NamespaceRevision {
		return Checkpoint{}, api.E("revision_conflict", "namespace_changed")
	}
	if env.NamespaceRef == nil || !env.ActuallyExited || len(env.ActiveOperationIDs) > 0 || len(env.StopResiduals) > 0 || (env.Phase != "active" && env.Phase != "closed") {
		return Checkpoint{}, api.E("invalid_state", "checkpoint_not_quiescent")
	}
	now, err := tx.Now(ctx)
	if err != nil {
		return Checkpoint{}, err
	}
	cp := Checkpoint{CheckpointID: stableID("checkpoint", c.CommandID), EnvironmentRef: tx.Scope().Ref(env.EnvironmentID, env.Revision), Generation: env.Generation, NamespaceRevision: env.NamespaceRevision, ContentRef: *env.NamespaceRef, ConfigRef: env.ConfigRef, InstallLockRef: env.InstallLockRef, Format: PassiveEnvironmentFormat, ProcessedSources: env.ProcessedSources, CreatedAt: api.Time(now)}
	return cp, tx.Create(ctx, Namespace+".checkpoints", cp.CheckpointID, env.EnvironmentID, cp)
}
func (s *Service) environmentRestore(ctx context.Context, tx rt.Tx, a rt.Auth, c api.Command, p EnvironmentRestoreInput) (Environment, error) {
	env, rev, err := readEnvironment(ctx, tx, a, c, p.EnvironmentID, p.ExpectedGeneration)
	if err != nil {
		return env, err
	}
	if env.Phase != "closed" || !env.ActuallyExited || len(env.StopResiduals) > 0 {
		return env, api.E("invalid_state", "environment_not_closed")
	}
	if err = rt.CheckRef(tx.Scope(), p.CheckpointRef); err != nil {
		return env, err
	}
	var cp Checkpoint
	if err = tx.GetVersion(ctx, Namespace+".checkpoints", p.CheckpointRef.ObjectID, p.CheckpointRef.Revision, &cp); err != nil {
		return env, err
	}
	if cp.EnvironmentRef.ObjectID != env.EnvironmentID || cp.Format != PassiveEnvironmentFormat || !api.Equal(cp.ConfigRef, p.ConfigRef) || !api.Equal(cp.InstallLockRef, p.InstallLockRef) {
		return env, api.E("unsupported", "checkpoint_incompatible")
	}
	env.Generation++
	env.InstanceID = api.NewID("instance")
	env.Phase = "preparing"
	env.Revision = rev + 1
	env.NamespaceRef = &cp.ContentRef
	env.NamespaceRevision = cp.NamespaceRevision
	env.ProcessedSources = cp.ProcessedSources
	env.PreparationCommandID = ""
	if err = putEnvironment(ctx, tx, env.EnvironmentID, rev, env); err != nil {
		return env, err
	}
	now, err := tx.Now(ctx)
	if err != nil {
		return env, err
	}
	_, err = tx.Raise(ctx, EnvironmentPrepareJob, env.EnvironmentID, tx.Scope().Ref(env.EnvironmentID, env.Revision), now)
	return env, err
}
func (s *Service) environmentGet(ctx context.Context, st rt.Store, sc rt.Scope, a rt.Auth, q api.Query, p EnvironmentIDInput) (Environment, error) {
	var env Environment
	if p.EnvironmentID != q.TargetID {
		return env, api.E("invalid_request", "environment_target_mismatch")
	}
	_, err := st.Read(ctx, sc, Namespace+".environments", p.EnvironmentID, 0, &env)
	if err != nil {
		return env, err
	}
	if a.SubjectID != env.Principal.SubjectID && !a.HasRole("executor") && !a.HasRole("admin") {
		return Environment{}, api.E("forbidden", "environment_not_disclosed")
	}
	return env, nil
}
func (s *Service) environmentList(ctx context.Context, st rt.Store, sc rt.Scope, a rt.Auth, q api.Query, p api.ListInput) (api.Page[Environment], error) {
	records, revision, next, exhausted, err := pageRecords(ctx, st, sc, a, "environments", p)
	page := api.Page[Environment]{Items: []Environment{}, CollectionRevision: revision, NextCursor: next, Exhausted: exhausted, Gaps: []string{}}
	if err != nil {
		return page, err
	}
	for _, r := range records {
		var env Environment
		if err = r.Decode(&env); err != nil {
			return page, err
		}
		if a.SubjectID != env.Principal.SubjectID && !a.HasRole("admin") && !a.HasRole("executor") {
			return page, api.E("forbidden", "environment_not_disclosed")
		}
		page.Items = append(page.Items, env)
	}
	return page, nil
}
func (s *Service) environmentPrepareWork(ctx context.Context, st rt.Store, sc rt.Scope, w rt.Work) error {
	var env Environment
	if _, err := st.Read(ctx, sc, Namespace+".environments", w.Job.SourceRef.ObjectID, 0, &env); err != nil {
		return err
	}
	if env.Phase != "preparing" {
		return s.finish(ctx, st, sc, w, rt.Done(), nil)
	}
	if env.RuntimeKind != "trusted_passive_data" {
		if s.cfg.EnvironmentAdmission == nil {
			return s.failEnvironmentPreparation(ctx, st, sc, w, env, api.E("unsupported", "runtime_not_ready"))
		}
		if err := s.cfg.EnvironmentAdmission.Prepare(ctx, sc, env.Principal, env); err != nil {
			if businessError(err) {
				return s.failEnvironmentPreparation(ctx, st, sc, w, env, err)
			}
			return err
		}
	}
	var ref api.ContentRef
	var err error
	if env.NamespaceRef != nil {
		ref = *env.NamespaceRef
		var b []byte
		b, err = s.cfg.Content.ReadBytes(ctx, sc, env.Principal, ref, "environment_restore", s.cfg.Location)
		if err == nil {
			var ns PassiveNamespace
			err = api.Decode(b, &ns)
			if err == nil && ns.Format != PassiveEnvironmentFormat {
				err = api.E("unsupported", "checkpoint_incompatible")
			}
		}
	} else {
		ref, err = s.cfg.Content.Publish(ctx, sc, env.Principal, Publication{ContentID: stableID("content", env.EnvironmentID+":namespace:1"), MediaType: "application/json", Purpose: "environment_namespace", Location: s.cfg.Location, ProcessedSources: env.ProcessedSources, DisclosedSources: []api.ContentRef{}}, api.Raw(PassiveNamespace{Format: PassiveEnvironmentFormat, Bindings: []NamespaceBinding{}}))
	}
	if err != nil {
		if businessError(err) {
			return s.failEnvironmentPreparation(ctx, st, sc, w, env, err)
		}
		return err
	}
	return s.finish(ctx, st, sc, w, rt.Done(), func(tx rt.Tx) error {
		var current Environment
		rev, err := tx.Get(ctx, Namespace+".environments", env.EnvironmentID, &current)
		if err != nil {
			return err
		}
		if current.Generation != env.Generation || current.Phase != "preparing" {
			return nil
		}
		now, err := tx.Now(ctx)
		if err != nil {
			return err
		}
		if err = minDeadline(now, current.ExpiresAt); err != nil {
			// 被动命名空间还未创建运行实例；过期准备必须固定拒绝并关闭，不能永远重领。
			current.Revision = rev + 1
			current.Generation++
			current.Phase = "closed"
			current.ReadyForCell = false
			current.ActuallyExited = true
			if writeErr := putEnvironment(ctx, tx, current.EnvironmentID, rev, current); writeErr != nil {
				return writeErr
			}
			if current.PreparationCommandID != "" {
				return rt.Decide(ctx, tx, current.PreparationCommandID, nil, api.E("expired", "environment_expired"))
			}
			return nil
		}
		current.Revision = rev + 1
		current.Phase = "active"
		current.ReadyForCell = true
		current.ActuallyExited = true
		current.NamespaceRef = &ref
		if current.NamespaceRevision == 0 {
			current.NamespaceRevision = 1
		}
		if err = putEnvironment(ctx, tx, current.EnvironmentID, rev, current); err != nil {
			return err
		}
		if current.PreparationCommandID != "" {
			return rt.Decide(ctx, tx, current.PreparationCommandID, current, nil)
		}
		return nil
	})
}
func (s *Service) environmentCleanupWork(ctx context.Context, st rt.Store, sc rt.Scope, w rt.Work) error {
	var env Environment
	if _, err := st.Read(ctx, sc, Namespace+".environments", w.Job.SourceRef.ObjectID, 0, &env); err != nil {
		return err
	}
	if env.Phase != "closing" && env.Phase != "destroying" {
		return s.finish(ctx, st, sc, w, rt.Done(), nil)
	}
	unresolved := []string{}
	for _, id := range env.ActiveOperationIDs {
		var op operationRecord
		if _, err := st.Read(ctx, sc, Namespace+".operations", id, 0, &op); err != nil {
			return err
		}
		if !op.ActuallyStopped || !noLater(op.Operation.MayApplyLater) {
			unresolved = append(unresolved, id)
		}
	}
	for _, id := range env.HostCallIDs {
		var h HostCall
		if _, err := st.Read(ctx, sc, Namespace+".hostcalls", id, 0, &h); err != nil {
			return err
		}
		if h.Phase != "closed" && h.Phase != "denied" {
			unresolved = append(unresolved, id)
		}
	}
	d := rt.Done()
	if len(unresolved) > 0 {
		d = rt.Waiting(time.Now().UTC().Add(time.Minute))
	}
	return s.finish(ctx, st, sc, w, d, func(tx rt.Tx) error {
		var current Environment
		rev, err := tx.Get(ctx, Namespace+".environments", env.EnvironmentID, &current)
		if err != nil {
			return err
		}
		if current.Generation != env.Generation {
			return nil
		}
		current.Revision = rev + 1
		current.StopResiduals = unresolved
		current.ReadyForCell = false
		if len(unresolved) == 0 {
			current.ActuallyExited = true
			current.ActiveOperationIDs = []string{}
			if current.Phase == "destroying" {
				current.Phase = "destroyed"
				current.NamespaceRef = nil
			} else {
				current.Phase = "closed"
			}
		}
		return putEnvironment(ctx, tx, current.EnvironmentID, rev, current)
	})
}

func (s *Service) failEnvironmentPreparation(ctx context.Context, st rt.Store, sc rt.Scope, w rt.Work, env Environment, cause error) error {
	return s.finish(ctx, st, sc, w, rt.Done(), func(tx rt.Tx) error {
		var current Environment
		rev, err := tx.Get(ctx, Namespace+".environments", env.EnvironmentID, &current)
		if err != nil {
			return err
		}
		if current.Generation != env.Generation || current.Phase != "preparing" {
			return nil
		}
		current.Revision = rev + 1
		current.Generation++
		current.Phase = "closing"
		current.ReadyForCell = false
		current.StopResiduals = []string{}
		if err = putEnvironment(ctx, tx, current.EnvironmentID, rev, current); err != nil {
			return err
		}
		now, err := tx.Now(ctx)
		if err != nil {
			return err
		}
		if _, err = tx.Raise(ctx, EnvironmentCleanupJob, current.EnvironmentID, sc.Ref(current.EnvironmentID, current.Revision), now); err != nil {
			return err
		}
		if current.PreparationCommandID != "" {
			var rejection *api.Error
			if e, ok := cause.(*api.Error); ok {
				rejection = e
			} else {
				rejection = api.E("invalid_state", "runtime_not_ready")
			}
			return rt.Decide(ctx, tx, current.PreparationCommandID, nil, rejection)
		}
		return nil
	})
}
