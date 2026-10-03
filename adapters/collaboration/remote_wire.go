package collaboration

import (
	"context"
	"errors"
	"strings"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/memory"
	"github.com/ruipengliu/lerna/internal/task"
	"github.com/ruipengliu/lerna/runtime"
)

const (
	JobRemoteCreate = "collaboration.remote_create"
	remoteIncoming  = "collaboration.remote_incoming"
	remoteOutgoing  = "collaboration.remote_outgoing"
)

type RemoteCreateInput struct {
	CreationKey        string                    `json:"creation_key"`
	CreateCommandID    string                    `json:"create_command_id"`
	ChildTaskID        string                    `json:"child_task_id"`
	ProfileRef         api.ComponentRef          `json:"profile_ref"`
	DelegationRef      api.ObjectRef             `json:"delegation_ref"`
	AllocationRef      api.ObjectRef             `json:"allocation_ref"`
	SourceDatabaseID   string                    `json:"source_database_id"`
	SubjectRef         api.ObjectRef             `json:"subject_ref"`
	OriginalCommandRef api.ObjectRef             `json:"original_command_ref"`
	ParentSources      []api.SourceEvidence      `json:"parent_sources"`
	Input              task.DelegateInput        `json:"input"`
	AncestorTaskRefs   []api.ObjectRef           `json:"ancestor_task_refs"`
	ForeignReferences  []memory.ForeignReference `json:"foreign_references"`
	ParentAdmission    *RemoteParentAdmission    `json:"parent_admission,omitempty"`
}
type RemoteCreateOutput struct {
	CreationKey  string         `json:"creation_key"`
	Phase        string         `json:"phase"`
	ChildTaskRef *api.ObjectRef `json:"child_task_ref,omitempty"`
}
type remoteReceived struct {
	ParentSnapshot *RemoteAllocationSnapshot `json:"parent_snapshot,omitempty"`
	FactRevision   uint64                    `json:"fact_revision"`
	FactDigest     string                    `json:"fact_digest"`
	Revision       uint64                    `json:"revision"`
	Packet         RemoteCreateInput         `json:"packet"`
	PeerAuth       runtime.Auth              `json:"peer_auth"`
	Phase          string                    `json:"phase"`
	ChildTaskRef   *api.ObjectRef            `json:"child_task_ref,omitempty"`
}

func remoteID(kind string, parts ...string) string {
	return kind + "_" + strings.TrimPrefix(api.Hash([]byte(strings.Join(parts, "/"))), "sha256:")[:32]
}
func packetID(p RemoteCreateInput) string {
	return remoteID("handoff", p.DelegationRef.TenantID, p.DelegationRef.OwnerID, p.CreationKey)
}
func (r *Remote) profile(ref api.ComponentRef) (RemoteAgentProfile, error) {
	key, err := api.Digest(ref)
	if err != nil {
		return RemoteAgentProfile{}, err
	}
	p, ok := r.profiles[key]
	if !ok {
		return p, api.E("unsupported", "remote_agent_profile_unconfigured")
	}
	return p, nil
}
func remoteBudget(in, limits []api.Amount) error {
	if err := api.ValidateAmounts(in); err != nil {
		return err
	}
	if len(in) == 0 {
		return api.E("invalid_request", "remote_budget_required")
	}
	for _, a := range in {
		found := false
		for _, limit := range limits {
			if limit.Unit == a.Unit {
				cmp, err := api.CompareDecimal(a.Value, limit.Value)
				if err != nil {
					return err
				}
				if cmp > 0 {
					return api.E("forbidden", "remote_budget_scope_exceeded")
				}
				found = true
				break
			}
		}
		if !found {
			return api.E("forbidden", "remote_budget_unit_not_paired")
		}
	}
	return nil
}
func subsetRefs(in, limits []api.ObjectRef) bool {
	for _, ref := range in {
		found := false
		for _, limit := range limits {
			if ref == limit {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}
func (r *Remote) validatePacket(p RemoteCreateInput) (RemoteAgentProfile, error) {
	profile, err := r.profile(p.ProfileRef)
	if err != nil {
		return profile, err
	}
	v := profile.Values
	if !api.ValidID(p.CreationKey) || !api.ValidID(p.CreateCommandID) || !api.ValidID(p.ChildTaskID) || !api.ValidID(p.SourceDatabaseID) || p.DelegationRef.TenantID != r.cfg.Scope.TenantID || p.DelegationRef.OwnerID != v.ParentOwnerID || p.DelegationRef.ObjectID != p.CreationKey || p.DelegationRef.Revision != 1 || p.AllocationRef.TenantID != r.cfg.Scope.TenantID || p.AllocationRef.OwnerID != v.ParentOwnerID || p.AllocationRef.Revision != 1 || api.ValidateRecord("ObjectRef", p.AllocationRef) != nil || api.ValidateRecord("ObjectRef", p.OriginalCommandRef) != nil || p.OriginalCommandRef.OwnerID != v.ParentOwnerID || p.OriginalCommandRef.TenantID != r.cfg.Scope.TenantID || len(p.ParentSources) > 100 || p.Input.Internal || p.Input.DelegationID != p.CreationKey || p.Input.ParentTaskRef.OwnerID != v.ParentOwnerID || p.Input.ParentTaskRef.TenantID != r.cfg.Scope.TenantID || api.ValidateRecord("ObjectRef", p.Input.ParentTaskRef) != nil || p.Input.AgentBindingRef != v.AgentBindingRef || p.Input.ReceiverID != v.ReceiverID || !api.Equal(p.Input.PolicyRef, v.PolicyRef) || !subsetRefs(p.Input.PermissionRefs, v.PermissionRefs) || !subsetRefs([]api.ObjectRef{p.SubjectRef}, v.SubjectRefs) || p.Input.ParentGoalRevision == 0 || uint64(len(p.Input.InputRefs)) > v.MaxInputs || len(p.AncestorTaskRefs) == 0 || uint64(len(p.AncestorTaskRefs)) >= v.MaxDepth || len(p.ForeignReferences) > 100 {
		return profile, api.E("forbidden", "remote_delegation_scope_exceeded")
	}
	if err = remoteBudget(p.Input.Budget, v.BudgetLimits); err != nil {
		return profile, err
	}
	seen := map[string]bool{}
	for _, ref := range p.AncestorTaskRefs {
		key := ref.OwnerID + "/" + ref.ObjectID
		if api.ValidateRecord("ObjectRef", ref) != nil || ref.TenantID != r.cfg.Scope.TenantID || ref.OwnerID == v.ReceiverID && ref.ObjectID == p.ChildTaskID || seen[key] {
			return profile, api.E("invalid_request", "remote_ancestor_cycle")
		}
		seen[key] = true
	}
	last := p.AncestorTaskRefs[len(p.AncestorTaskRefs)-1]
	if last.OwnerID != p.Input.ParentTaskRef.OwnerID || last.ObjectID != p.Input.ParentTaskRef.ObjectID {
		return profile, api.E("forbidden", "remote_parent_ancestry_changed")
	}
	for _, ref := range append([]api.ContentRef{p.Input.GoalRef}, p.Input.InputRefs...) {
		if api.ValidateRecord("ContentRef", ref) != nil || ref.TenantID != r.cfg.Scope.TenantID || ref.ByteLength > memory.MaxContentBytes {
			return profile, api.E("forbidden", "remote_material_scope_invalid")
		}
	}
	if _, err = api.ParseTime(p.Input.Deadline); err != nil {
		return profile, err
	}
	if err = validateRemoteAdmission(p, profile); err != nil {
		return profile, err
	}
	if _, err = api.Canonical(api.Raw(p)); err != nil {
		return profile, err
	}
	return profile, nil
}
func remoteCommandMethod[I, O any](r *Remote, name string, accepted bool, fn func(context.Context, runtime.Tx, runtime.Auth, api.Command, I) (runtime.Outcome, error)) runtime.Method {
	return runtime.Method{Contract: api.Contract[I, O](name, "orchestrator", "command", false, accepted), Participants: r.parts, Apply: func(ctx context.Context, tx runtime.Tx, a runtime.Auth, c api.Command) (runtime.Outcome, error) {
		var in I
		if err := api.Decode(c.Payload, &in); err != nil {
			return runtime.Outcome{}, err
		}
		var out runtime.Outcome
		err := task.WithJobIntents(ctx, tx, func(tx runtime.Tx) error {
			var err error
			out, err = fn(ctx, tx, a, c, in)
			return err
		})
		return out, err
	}}
}
func (r *Remote) Register() error {
	if _, err := r.service(); err != nil {
		return err
	}
	methods := []runtime.Method{
		remoteCommandMethod[task.DelegateInput, task.DelegateOutput](r, "collaboration.delegate", false, r.delegate),
		remoteCommandMethod[RemoteCreateInput, RemoteCreateOutput](r, "collaboration.create", true, r.receiveCreate),
		remoteCommandMethod[task.AllocationCloseInput, task.AllocationOutput](r, "collaboration.allocation.close", false, r.receiveClose),
		remoteCommandMethod[RemoteControlInput, RemoteControlOutput](r, "collaboration.control", true, r.receiveControl),
		remoteCommandMethod[RemoteClosureReport, task.AllocationOutput](r, "collaboration.closure.report", false, r.receiveClosure),
		remoteQueryMethod[RemoteAllocationReadInput, RemoteAllocationReadOutput](r, "collaboration.allocation.get", r.allocationRead),
		remoteQueryMethod[RemoteClosureInput, RemoteClosureReport](r, "collaboration.closure.get", r.closureRead),
		remoteQueryMethod[RemoteKeyInput, RemoteAllocationSnapshot](r, "collaboration.allocation.current", r.sourceAllocation),
		remoteQueryMethod[RemoteKeyInput, RemoteState](r, "collaboration.state", r.state),
		remoteCommandMethod[RemoteInputSend, RemoteInputAck](r, "collaboration.input.send", true, r.sendInput),
	}
	inputMethod := remoteCommandMethod[RemoteInputPacket, RemoteInputAck](r, "collaboration.input", true, r.receiveInput)
	inputMethod.Contract = remoteInputContract()
	methods = append(methods, inputMethod)
	for _, m := range methods {
		if err := r.cfg.Registry.Register(m); err != nil {
			return err
		}
	}
	if err := r.cfg.Registry.RegisterJob(JobRemoteCreate, r.receiveCreateJob); err != nil {
		return err
	}
	if err := r.cfg.Registry.RegisterJob(JobRemoteControl, r.controlJob); err != nil {
		return err
	}
	if err := r.cfg.Registry.RegisterJob(JobRemoteInputSend, r.inputSendJob); err != nil {
		return err
	}
	if err := r.cfg.Registry.RegisterJob(JobRemoteInputReceive, r.inputReceiveJob); err != nil {
		return err
	}
	if err := r.registerRemoteSessions(); err != nil {
		return err
	}
	return r.cfg.Registry.RegisterJob(JobRemoteProof, r.proofJob)
}
func (r *Remote) delegate(ctx context.Context, tx runtime.Tx, auth runtime.Auth, c api.Command, in task.DelegateInput) (runtime.Outcome, error) {
	s, err := r.service()
	if err != nil {
		return runtime.Outcome{}, err
	}
	out, err := s.DelegateTx(ctx, tx, auth, c, in)
	if err == nil && r.cfg.ScopeGate != nil && in.ReceiverID != tx.Scope().OwnerID {
		current, scopeErr := s.CheckDelegationScopeTx(ctx, tx, r.cfg.Auth, in.DelegationID)
		if scopeErr != nil {
			return runtime.Outcome{}, scopeErr
		}
		if _, scopeErr = r.planCreateTx(ctx, tx, current.Delegation, current.Allocation); scopeErr != nil {
			return runtime.Outcome{}, scopeErr
		}
	}
	return runtime.Applied(out), err
}
func (r *Remote) receiveCreate(ctx context.Context, tx runtime.Tx, peer runtime.Auth, c api.Command, p RemoteCreateInput) (runtime.Outcome, error) {
	if err := r.checkScope(tx.Scope()); err != nil {
		return runtime.Outcome{}, err
	}
	profile, err := r.validatePacket(p)
	if err != nil {
		return runtime.Outcome{}, err
	}
	if profile.Values.ReceiverID != tx.Scope().OwnerID || c.CommandID != p.CreateCommandID || c.TargetID != p.CreationKey || c.ExpiresAt != p.Input.Deadline {
		return runtime.Outcome{}, api.E("invalid_request", "original_remote_create_changed")
	}
	if err = r.cfg.Authority.CheckPeerTx(ctx, tx, peer, p.DelegationRef.OwnerID); err != nil {
		return runtime.Outcome{}, err
	}
	id := packetID(p)
	var saved remoteReceived
	_, err = tx.Get(ctx, remoteIncoming, id, &saved)
	if err == nil {
		if !api.Equal(saved.Packet, p) || !api.Equal(saved.PeerAuth, peer) {
			return runtime.Outcome{}, api.E("idempotency_conflict", "original_creation_key_changed")
		}
		out := RemoteCreateOutput{p.CreationKey, saved.Phase, saved.ChildTaskRef}
		if saved.Phase == "applied" {
			return runtime.Applied(out), nil
		}
		return runtime.Accepted(out), nil
	}
	if !api.IsCode(err, "not_found") {
		return runtime.Outcome{}, err
	}
	saved = remoteReceived{Revision: 1, Packet: p, PeerAuth: peer, Phase: "preparing"}
	if err = tx.Create(ctx, remoteIncoming, id, p.DelegationRef.OwnerID, saved); err != nil {
		return runtime.Outcome{}, err
	}
	digest, err := api.Digest(p)
	if err != nil {
		return runtime.Outcome{}, err
	}
	if err = tx.Bind(ctx, remoteIncoming, p.DelegationRef.OwnerID+"/"+p.CreationKey, id, digest); err != nil {
		return runtime.Outcome{}, err
	}
	now, err := tx.Now(ctx)
	if err != nil {
		return runtime.Outcome{}, err
	}
	if _, err = tx.Raise(ctx, JobRemoteCreate, "create/"+id, tx.Scope().Ref(id, 1), now); err != nil {
		return runtime.Outcome{}, err
	}
	return runtime.Accepted(RemoteCreateOutput{p.CreationKey, "preparing", nil}), nil
}
func (r *Remote) receiveClose(ctx context.Context, tx runtime.Tx, peer runtime.Auth, c api.Command, in task.AllocationCloseInput) (runtime.Outcome, error) {
	if err := r.checkScope(tx.Scope()); err != nil {
		return runtime.Outcome{}, err
	}
	s, err := r.service()
	if err != nil {
		return runtime.Outcome{}, err
	}
	if peer.SubjectID != in.AllocationRef.OwnerID {
		return runtime.Outcome{}, api.E("forbidden", "allocation_sender_not_parent")
	}
	out, err := s.CloseAllocationTx(ctx, tx, peer, c, in)
	if err != nil {
		return runtime.Outcome{}, err
	}
	if err = r.cfg.Authority.CheckPeerTx(ctx, tx, peer, in.AllocationRef.OwnerID); err != nil {
		return runtime.Outcome{}, err
	}
	return runtime.Applied(out), err
}
func (r *Remote) receiveCreateJob(ctx context.Context, store runtime.Store, scope runtime.Scope, work runtime.Work) error {
	if err := r.checkScope(scope); err != nil {
		return err
	}
	if store.ID() != r.cfg.Store.ID() {
		return api.E("forbidden", "remote_job_database_mismatch")
	}
	var saved remoteReceived
	if _, err := store.Read(ctx, scope, remoteIncoming, work.Job.SourceRef.ObjectID, 0, &saved); err != nil {
		return err
	}
	if saved.Phase != "preparing" {
		return runtime.Finish(ctx, store, scope, r.parts, work, runtime.Done(), nil)
	}
	s, err := r.service()
	if err != nil {
		return err
	}
	inc, err := s.IncomingRead(ctx, store, scope, saved.PeerAuth, saved.Packet.AllocationRef)
	if err == nil && inc.Gate != "open" {
		return r.rejectCreate(ctx, store, scope, work, saved, api.E("invalid_state", "allocation_closed"))
	}
	if err != nil && !api.IsCode(err, "not_found") {
		return err
	}
	var control remoteControlGate
	_, err = store.Read(ctx, scope, remoteControls, packetID(saved.Packet), 0, &control)
	if err != nil && !api.IsCode(err, "not_found") {
		return err
	}
	if control.Cancelled {
		return r.rejectCreate(ctx, store, scope, work, saved, api.E("invalid_state", "remote_goal_closed"))
	}
	err = r.createOriginal(ctx, store, scope, work, saved)
	if api.IsCode(err, "invalid_state") {
		var business *api.Error
		if errors.As(err, &business) && (business.Reason == "allocation_closed" || business.Reason == "remote_goal_closed") {
			return r.rejectCreate(ctx, store, scope, work, saved, business)
		}
	}
	return err
}

func (r *Remote) rejectCreate(ctx context.Context, store runtime.Store, scope runtime.Scope, work runtime.Work, saved remoteReceived, reason *api.Error) error {
	return runtime.Finish(ctx, store, scope, r.parts, work, runtime.Done(), func(tx runtime.Tx) error {
		if _, err := tx.LoadCommand(ctx, saved.Packet.CreateCommandID); err != nil {
			return err
		}
		var current remoteReceived
		rev, err := tx.Get(ctx, remoteIncoming, packetID(saved.Packet), &current)
		if err != nil {
			return err
		}
		if current.Phase != "preparing" {
			return nil
		}
		current.Phase = "rejected"
		current.Revision++
		if err = tx.Put(ctx, remoteIncoming, packetID(saved.Packet), rev, current); err != nil {
			return err
		}
		return runtime.Decide(ctx, tx, current.Packet.CreateCommandID, nil, reason)
	})
}
