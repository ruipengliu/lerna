package execution

import (
	"context"
	"github.com/ruipengliu/lerna/api"
	rt "github.com/ruipengliu/lerna/runtime"
	"time"
)

const ResourceObserveJob = "execution.resource.observe"
const ResourceFenceJob = "execution.resource.fence"

type ResourceDriver interface {
	Fence(context.Context, rt.Scope, ResourceLease) (StopFact, error)
	Observe(context.Context, rt.Scope, ResourceLease, string) (Observation, error)
}
type ObservationSchemaDriver interface{ ObservationSchema() api.Schema }
type AcquireInput struct {
	ResourceID           string `json:"resource_id"`
	HolderID             string `json:"holder_id"`
	InstanceID           string `json:"instance_id"`
	LeaseUntil           string `json:"lease_until"`
	ExpectedControlEpoch uint64 `json:"expected_control_epoch"`
}
type ResourceInput struct {
	ResourceID string `json:"resource_id"`
}
type LeaseInput struct {
	ResourceID   string `json:"resource_id"`
	HolderID     string `json:"holder_id"`
	InstanceID   string `json:"instance_id"`
	ControlEpoch uint64 `json:"control_epoch"`
	LeaseUntil   string `json:"lease_until,omitempty"`
}
type TakeoverInput struct {
	ResourceID string `json:"resource_id"`
	Reason     string `json:"reason"`
}
type ObserveInput struct {
	ResourceID    string `json:"resource_id"`
	ObservationID string `json:"observation_id"`
	HolderID      string `json:"holder_id"`
	InstanceID    string `json:"instance_id"`
	ControlEpoch  uint64 `json:"control_epoch"`
}
type ObserveOutput struct {
	ObservationRef api.ObjectRef `json:"observation_ref"`
	Ready          bool          `json:"ready"`
	Observation    *Observation  `json:"observation,omitempty"`
}
type ObservationIDInput struct {
	ObservationID string `json:"observation_id"`
}
type observeRequest struct {
	Revision  uint64       `json:"revision"`
	CommandID string       `json:"command_id"`
	Principal rt.Auth      `json:"principal"`
	Input     ObserveInput `json:"input"`
	Result    *Observation `json:"result,omitempty"`
}

func resourceAuth(a rt.Auth) error {
	if a.HasRole("executor") || a.HasRole("device_controller") || a.HasRole("admin") {
		return nil
	}
	return api.E("forbidden", "resource_control_not_authorized")
}
func (s *Service) registerResources(r *rt.Registry) error {
	fs := []func() error{
		func() error { return RegisterCommand(r, "resource.acquire", false, []string{Namespace}, s.acquire) },
		func() error { return RegisterCommand(r, "resource.renew", true, []string{Namespace}, s.renew) },
		func() error { return RegisterCommand(r, "resource.release", true, []string{Namespace}, s.release) },
		func() error { return RegisterCommand(r, "resource.takeover", true, []string{Namespace}, s.takeover) },
		func() error { return RegisterQuery(r, "resource.get", s.resourceGet) },
		func() error {
			m := rt.Method{Contract: s.observationContract("resource.observation.get", "query", false), Query: func(ctx context.Context, st rt.Store, sc rt.Scope, a rt.Auth, q api.Query) (any, error) {
				var p ObservationIDInput
				if err := api.Decode(q.Payload, &p); err != nil {
					return nil, err
				}
				return s.observationGet(ctx, st, sc, a, q, p)
			}}
			return r.Register(m)
		},
	}
	for _, f := range fs {
		if err := f(); err != nil {
			return err
		}
	}
	if err := r.Register(rt.Method{Contract: s.observationContract("resource.observe", "command", true), Participants: []string{Namespace}, Apply: func(ctx context.Context, tx rt.Tx, a rt.Auth, c api.Command) (rt.Outcome, error) {
		var p ObserveInput
		if err := api.Decode(c.Payload, &p); err != nil {
			return rt.Outcome{}, err
		}
		out, err := s.observe(ctx, tx, a, c, p)
		return rt.Accepted(out), err
	}}); err != nil {
		return err
	}
	if err := r.RegisterJob(ResourceObserveJob, s.observeWork); err != nil {
		return err
	}
	return r.RegisterJob(ResourceFenceJob, s.fenceWork)
}
func (s *Service) acquire(ctx context.Context, tx rt.Tx, a rt.Auth, c api.Command, p AcquireInput) (ResourceLease, error) {
	if err := resourceAuth(a); err != nil {
		return ResourceLease{}, err
	}
	if s.cfg.ResourceDriver == nil {
		return ResourceLease{}, api.E("unsupported", "resource_driver_not_ready")
	}
	if p.ResourceID != c.TargetID || !api.ValidID(p.ResourceID) || !api.ValidID(p.HolderID) || !api.ValidID(p.InstanceID) {
		return ResourceLease{}, api.E("invalid_request", "resource_binding_mismatch")
	}
	now, err := tx.Now(ctx)
	if err != nil {
		return ResourceLease{}, err
	}
	until, err := api.ParseTime(p.LeaseUntil)
	if err != nil || !until.After(now) || until.Sub(now) > 5*time.Minute {
		return ResourceLease{}, api.E("invalid_request", "invalid_resource_window")
	}
	var lease ResourceLease
	rev, err := tx.Get(ctx, Namespace+".resources", p.ResourceID, &lease)
	if api.IsCode(err, "not_found") {
		if p.ExpectedControlEpoch != 0 {
			return lease, api.E("revision_conflict", "resource_epoch_changed")
		}
		lease = ResourceLease{ResourceID: p.ResourceID, OwnerID: s.cfg.OwnerID, HolderID: p.HolderID, InstanceID: p.InstanceID, ControlEpoch: 1, Revision: 1, LeaseUntil: p.LeaseUntil, State: "held", OperationIDs: []string{}, ActuallyStopped: false}
		if err = tx.Create(ctx, Namespace+".resources", p.ResourceID, "", lease); err != nil {
			return lease, err
		}
	} else if err != nil {
		return lease, err
	} else {
		if lease.ControlEpoch != p.ExpectedControlEpoch {
			return lease, api.E("revision_conflict", "resource_epoch_changed")
		}
		if lease.State != "released" || !lease.ActuallyStopped || lease.InflightWrite != "" {
			return lease, api.E("invalid_state", "resource_conflict")
		}
		lease.Revision = rev + 1
		lease.ControlEpoch++
		lease.HolderID = p.HolderID
		lease.InstanceID = p.InstanceID
		lease.LeaseUntil = p.LeaseUntil
		lease.State = "held"
		lease.TakeoverRequested = false
		lease.ActuallyStopped = false
		if err = tx.Put(ctx, Namespace+".resources", p.ResourceID, rev, lease); err != nil {
			return lease, err
		}
	}
	_, err = tx.Raise(ctx, ResourceFenceJob, p.ResourceID, tx.Scope().Ref(p.ResourceID, lease.Revision), now)
	return lease, err
}
func loadLease(ctx context.Context, tx rt.Tx, c api.Command, p LeaseInput) (ResourceLease, uint64, error) {
	var lease ResourceLease
	rev, err := tx.Get(ctx, Namespace+".resources", p.ResourceID, &lease)
	if err != nil {
		return lease, rev, err
	}
	if c.TargetID != p.ResourceID || c.ExpectedRevision == nil || *c.ExpectedRevision != rev {
		return lease, rev, api.E("revision_conflict", "resource_revision_changed")
	}
	if lease.HolderID != p.HolderID || lease.InstanceID != p.InstanceID || lease.ControlEpoch != p.ControlEpoch {
		return lease, rev, api.E("revision_conflict", "resource_epoch_changed")
	}
	return lease, rev, nil
}
func (s *Service) renew(ctx context.Context, tx rt.Tx, a rt.Auth, c api.Command, p LeaseInput) (ResourceLease, error) {
	if err := resourceAuth(a); err != nil {
		return ResourceLease{}, err
	}
	lease, rev, err := loadLease(ctx, tx, c, p)
	if err != nil {
		return lease, err
	}
	if lease.State != "held" || lease.TakeoverRequested {
		return lease, api.E("invalid_state", "resource_not_held")
	}
	now, err := tx.Now(ctx)
	if err != nil {
		return lease, err
	}
	until, err := api.ParseTime(p.LeaseUntil)
	if err != nil || until.Sub(now) > 5*time.Minute || !until.After(now) {
		return lease, api.E("invalid_request", "invalid_resource_window")
	}
	lease.Revision = rev + 1
	lease.LeaseUntil = p.LeaseUntil
	return lease, tx.Put(ctx, Namespace+".resources", lease.ResourceID, rev, lease)
}
func (s *Service) release(ctx context.Context, tx rt.Tx, a rt.Auth, c api.Command, p LeaseInput) (ResourceLease, error) {
	if err := resourceAuth(a); err != nil {
		return ResourceLease{}, err
	}
	lease, rev, err := loadLease(ctx, tx, c, p)
	if err != nil {
		return lease, err
	}
	lease.Revision = rev + 1
	lease.State = "releasing"
	lease.ActuallyStopped = false
	if err = tx.Put(ctx, Namespace+".resources", lease.ResourceID, rev, lease); err != nil {
		return lease, err
	}
	now, err := tx.Now(ctx)
	if err != nil {
		return lease, err
	}
	_, err = tx.Raise(ctx, ResourceFenceJob, lease.ResourceID, tx.Scope().Ref(lease.ResourceID, lease.Revision), now)
	return lease, err
}
func (s *Service) takeover(ctx context.Context, tx rt.Tx, a rt.Auth, c api.Command, p TakeoverInput) (ResourceLease, error) {
	if !a.HasRole("admin") && !a.HasRole("device_owner") {
		return ResourceLease{}, api.E("forbidden", "takeover_requires_owner")
	}
	var lease ResourceLease
	rev, err := tx.Get(ctx, Namespace+".resources", p.ResourceID, &lease)
	if err != nil {
		return lease, err
	}
	if p.ResourceID != c.TargetID || c.ExpectedRevision == nil || *c.ExpectedRevision != rev {
		return lease, api.E("revision_conflict", "resource_revision_changed")
	}
	lease.Revision = rev + 1
	lease.ControlEpoch++
	lease.TakeoverRequested = true
	lease.State = "unknown"
	lease.ActuallyStopped = false
	if err = tx.Put(ctx, Namespace+".resources", p.ResourceID, rev, lease); err != nil {
		return lease, err
	}
	now, err := tx.Now(ctx)
	if err != nil {
		return lease, err
	}
	_, err = tx.Raise(ctx, ResourceFenceJob, lease.ResourceID, tx.Scope().Ref(lease.ResourceID, lease.Revision), now)
	if err != nil {
		return lease, err
	}
	for _, id := range lease.OperationIDs {
		var op operationRecord
		or, err := tx.Get(ctx, Namespace+".operations", id, &op)
		if err != nil {
			return lease, err
		}
		op.NewAttemptsClosed = true
		op.Operation.ExecutionState = "closed"
		if err = putOperation(ctx, tx, &op, or); err != nil {
			return lease, err
		}
		if _, err = tx.Raise(ctx, StopJob, id, tx.Scope().Ref(id, op.Revision), now); err != nil {
			return lease, err
		}
	}
	return lease, nil
}
func (s *Service) resourceGet(ctx context.Context, st rt.Store, sc rt.Scope, a rt.Auth, q api.Query, p ResourceInput) (ResourceLease, error) {
	if err := resourceAuth(a); err != nil {
		return ResourceLease{}, err
	}
	var lease ResourceLease
	if p.ResourceID != q.TargetID {
		return lease, api.E("invalid_request", "resource_target_mismatch")
	}
	_, err := st.Read(ctx, sc, Namespace+".resources", p.ResourceID, 0, &lease)
	return lease, err
}
func (s *Service) observe(ctx context.Context, tx rt.Tx, a rt.Auth, c api.Command, p ObserveInput) (ObserveOutput, error) {
	if err := resourceAuth(a); err != nil {
		return ObserveOutput{}, err
	}
	if s.cfg.ResourceDriver == nil {
		return ObserveOutput{}, api.E("unsupported", "resource_driver_not_ready")
	}
	if p.ResourceID != c.TargetID || !api.ValidID(p.ObservationID) {
		return ObserveOutput{}, api.E("invalid_request", "observation_binding_mismatch")
	}
	var lease ResourceLease
	if _, err := tx.Get(ctx, Namespace+".resources", p.ResourceID, &lease); err != nil {
		return ObserveOutput{}, err
	}
	if lease.ControlEpoch != p.ControlEpoch || lease.HolderID != p.HolderID || lease.InstanceID != p.InstanceID || lease.State != "held" || lease.TakeoverRequested {
		return ObserveOutput{}, api.E("invalid_state", "resource_conflict")
	}
	request := observeRequest{Revision: 1, CommandID: c.CommandID, Principal: a, Input: p}
	if err := tx.Create(ctx, Namespace+".observations", p.ObservationID, p.ResourceID, request); err != nil {
		return ObserveOutput{}, err
	}
	now, err := tx.Now(ctx)
	if err != nil {
		return ObserveOutput{}, err
	}
	_, err = tx.Raise(ctx, ResourceObserveJob, p.ObservationID, tx.Scope().Ref(p.ObservationID, 1), now)
	return ObserveOutput{ObservationRef: tx.Scope().Ref(p.ObservationID, 1)}, err
}
func (s *Service) observationGet(ctx context.Context, st rt.Store, sc rt.Scope, a rt.Auth, q api.Query, p ObservationIDInput) (ObserveOutput, error) {
	if err := resourceAuth(a); err != nil {
		return ObserveOutput{}, err
	}
	if p.ObservationID != q.TargetID {
		return ObserveOutput{}, api.E("invalid_request", "observation_target_mismatch")
	}
	var request observeRequest
	rev, err := st.Read(ctx, sc, Namespace+".observations", p.ObservationID, 0, &request)
	if err != nil {
		return ObserveOutput{}, err
	}
	return ObserveOutput{ObservationRef: sc.Ref(p.ObservationID, rev), Ready: request.Result != nil, Observation: request.Result}, nil
}
func (s *Service) observeWork(ctx context.Context, st rt.Store, sc rt.Scope, w rt.Work) error {
	var req observeRequest
	if _, err := st.Read(ctx, sc, Namespace+".observations", w.Job.SourceRef.ObjectID, 0, &req); err != nil {
		return err
	}
	if req.Result != nil {
		return s.finish(ctx, st, sc, w, rt.Done(), nil)
	}
	var lease ResourceLease
	if _, err := st.Read(ctx, sc, Namespace+".resources", req.Input.ResourceID, 0, &lease); err != nil {
		return err
	}
	observed, err := s.cfg.ResourceDriver.Observe(ctx, sc, lease, req.Input.ObservationID)
	if err != nil {
		return err
	}
	return s.finish(ctx, st, sc, w, rt.Done(), func(tx rt.Tx) error {
		var latest ResourceLease
		if _, err := tx.Get(ctx, Namespace+".resources", lease.ResourceID, &latest); err != nil {
			return err
		}
		if latest.ControlEpoch != lease.ControlEpoch || latest.State != "held" || latest.TakeoverRequested {
			return api.E("invalid_state", "resource_epoch_changed")
		}
		var original observeRequest
		rev, err := tx.Get(ctx, Namespace+".observations", req.Input.ObservationID, &original)
		if err != nil {
			return err
		}
		original.Revision = rev + 1
		original.Result = &observed
		if err = tx.Put(ctx, Namespace+".observations", req.Input.ObservationID, rev, original); err != nil {
			return err
		}
		return rt.Decide(ctx, tx, req.CommandID, ObserveOutput{ObservationRef: sc.Ref(req.Input.ObservationID, original.Revision), Ready: true, Observation: &observed}, nil)
	})
}
func (s *Service) fenceWork(ctx context.Context, st rt.Store, sc rt.Scope, w rt.Work) error {
	var lease ResourceLease
	if _, err := st.Read(ctx, sc, Namespace+".resources", w.Job.SourceRef.ObjectID, 0, &lease); err != nil {
		return err
	}
	fact, err := s.cfg.ResourceDriver.Fence(ctx, sc, lease)
	if err != nil {
		return err
	}
	d := rt.Done()
	if !fact.ActuallyStopped || !noLater(fact.MayApplyLater) {
		d = rt.Waiting(time.Now().UTC().Add(30 * time.Second))
	}
	return s.finish(ctx, st, sc, w, d, func(tx rt.Tx) error {
		var current ResourceLease
		rev, err := tx.Get(ctx, Namespace+".resources", lease.ResourceID, &current)
		if err != nil {
			return err
		}
		if current.ControlEpoch != lease.ControlEpoch || current.Revision != lease.Revision {
			return nil
		}
		current.Revision = rev + 1
		current.ActuallyStopped = fact.ActuallyStopped && noLater(fact.MayApplyLater)
		if current.ActuallyStopped && current.State != "held" && current.InflightWrite == "" {
			current.State = "released"
		}
		return tx.Put(ctx, Namespace+".resources", current.ResourceID, rev, current)
	})
}
func (s *Service) resourceBarrier(ctx context.Context, tx rt.Tx, op operationRecord, a Attempt, now time.Time) error {
	if a.Prepared.ResourceID == "" {
		return nil
	}
	var lease ResourceLease
	rev, err := tx.Get(ctx, Namespace+".resources", a.Prepared.ResourceID, &lease)
	if err != nil {
		return err
	}
	if lease.State != "held" || lease.TakeoverRequested || !lease.ActuallyStopped || lease.ControlEpoch != a.Prepared.ResourceEpoch {
		return api.E("invalid_state", "resource_conflict")
	}
	if err = minDeadline(now, lease.LeaseUntil, a.Prepared.ObservationBefore); err != nil {
		return err
	}
	var obs observeRequest
	if _, err = tx.Get(ctx, Namespace+".observations", a.Prepared.ObservationID, &obs); err != nil {
		return err
	}
	if obs.Result == nil || obs.Result.ControlEpoch != lease.ControlEpoch || obs.Result.InstanceID != lease.InstanceID || obs.Result.ActionBefore != a.Prepared.ObservationBefore {
		return api.E("revision_conflict", "observation_stale")
	}
	matched := false
	for _, ref := range op.Intent.ResourceRefs {
		if ref.OwnerID == tx.Scope().OwnerID && ref.ObjectID == lease.ResourceID && ref.TenantID == tx.Scope().TenantID {
			matched = true
		}
	}
	if !matched {
		return api.E("forbidden", "resource_not_in_intent")
	}
	d, err := s.driver(op.Invoke.CapabilityRef)
	if err != nil {
		return err
	}
	if d.Capability().EffectClass != "read_only" && lease.InflightWrite != "" {
		return api.E("invalid_state", "resource_conflict")
	}
	lease.Revision = rev + 1
	lease.OperationIDs = append(lease.OperationIDs, op.Operation.OperationID)
	if d.Capability().EffectClass != "read_only" {
		lease.InflightWrite = a.AttemptID
	}
	return tx.Put(ctx, Namespace+".resources", lease.ResourceID, rev, lease)
}
func (s *Service) releaseInflight(ctx context.Context, tx rt.Tx, a Attempt) error {
	if a.Prepared.ResourceID == "" {
		return nil
	}
	var lease ResourceLease
	rev, err := tx.Get(ctx, Namespace+".resources", a.Prepared.ResourceID, &lease)
	if err != nil {
		return err
	}
	if lease.InflightWrite == a.AttemptID {
		lease.InflightWrite = ""
		lease.ActuallyStopped = true
		lease.Revision = rev + 1
		return tx.Put(ctx, Namespace+".resources", lease.ResourceID, rev, lease)
	}
	return nil
}

func (s *Service) observationContract(name, kind string, accepted bool) api.MethodContract {
	c := closedContract[ObserveInput, ObserveOutput](name, kind, false, accepted)
	if kind == "query" {
		c.InputSchema = api.SchemaFor[ObservationIDInput]()
	}
	properties := c.OutputSchema["properties"].(map[string]any)
	observation := properties["observation"].(map[string]any)
	data := api.Schema{"type": "object", "properties": map[string]any{}, "required": []string{}, "additionalProperties": false}
	if d, ok := s.cfg.ResourceDriver.(ObservationSchemaDriver); ok {
		data = d.ObservationSchema()
	}
	observation["properties"].(map[string]any)["data"] = data
	return c
}
