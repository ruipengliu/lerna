package collaboration

import (
	"context"
	"errors"
	"time"

	"github.com/ruipengliu/lerna/adapters/platform"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/memory"
	"github.com/ruipengliu/lerna/internal/task"
	"github.com/ruipengliu/lerna/runtime"
)

const remoteProofs = "collaboration.remote_proof_publications"

type RemoteKeyInput struct {
	ParentOwnerID string `json:"parent_owner_id"`
	CreationKey   string `json:"creation_key"`
}
type RemoteAllocationSnapshot struct {
	Packet        RemoteCreateInput `json:"packet"`
	Allocation    task.Allocation   `json:"allocation"`
	ParentTask    RemoteParentTask  `json:"parent_task"`
	Allowed       bool              `json:"allowed"`
	Reason        string            `json:"reason"`
	ScopeRevision uint64            `json:"scope_revision"`
	IssuedAt      string            `json:"issued_at"`
	StartBefore   string            `json:"start_before"`
	Proof         string            `json:"proof"`
	ProofRef      api.ContentRef    `json:"proof_ref"`
}

// 只投影本次门禁所需原事实，避免把父完整条件与大上下文复制成每次 wire。
type RemoteParentTask struct {
	TenantID        string `json:"tenant_id"`
	OrchestratorID  string `json:"orchestrator_id"`
	TaskID          string `json:"task_id"`
	Revision        uint64 `json:"revision"`
	GoalRevision    uint64 `json:"goal_revision"`
	ControlRevision uint64 `json:"control_revision"`
	Status          string `json:"status"`
	Control         string `json:"control"`
	Deadline        string `json:"deadline"`
}

func remoteParentTask(t api.Task) RemoteParentTask {
	return RemoteParentTask{t.TenantID, t.OrchestratorID, t.TaskID, t.Revision, t.GoalRevision, t.ControlRevision, t.Status, t.Control, t.Deadline}
}

type remoteProofPublication struct {
	Request memory.PublicationRequest `json:"request"`
	Bytes   []byte                    `json:"bytes"`
}
type remoteSent struct {
	Packet        RemoteCreateInput `json:"packet"`
	Command       api.Command       `json:"command"`
	ScopeRevision uint64            `json:"scope_revision"`
	ScopeDigest   string            `json:"scope_digest"`
}

func remoteScopeDigest(p RemoteAllocationSnapshot) (string, error) {
	p.ScopeRevision = 0
	p.IssuedAt, p.StartBefore, p.Proof = "", "", ""
	p.ProofRef = api.ContentRef{}
	return api.Digest(p)
}

func remoteAllocationDigest(p RemoteAllocationSnapshot) (string, error) {
	p.Proof = ""
	p.ProofRef = api.ContentRef{}
	return api.Digest(p)
}
func remoteAllocationClaims(p RemoteAllocationSnapshot, digest string) platform.ProofClaims {
	return platform.ProofClaims{TenantID: p.Packet.SubjectRef.TenantID, Issuer: p.Packet.DelegationRef.OwnerID, Audience: p.Packet.Input.ReceiverID, Purpose: "agent_allocation", ObjectRef: p.Packet.AllocationRef, Digest: digest, ControlRevision: p.ParentTask.ControlRevision, WindowID: p.Packet.CreationKey, IssuedAt: p.IssuedAt, StartBefore: p.StartBefore}
}
func (r *Remote) sourceAllocation(ctx context.Context, peer runtime.Auth, q api.Query, in RemoteKeyInput) (RemoteAllocationSnapshot, error) {
	var out RemoteAllocationSnapshot
	if in.ParentOwnerID != r.cfg.Scope.OwnerID || q.TargetID != in.CreationKey {
		return out, api.E("forbidden", "allocation_source_scope_mismatch")
	}
	var saved remoteSent
	id := remoteID("handoff", r.cfg.Scope.TenantID, in.ParentOwnerID, in.CreationKey)
	if _, err := r.cfg.Store.Read(ctx, r.cfg.Scope, remoteOutgoing, id, 0, &saved); err != nil {
		return out, err
	}
	_, err := r.validatePacket(saved.Packet)
	if err != nil {
		return out, err
	}
	if r.cfg.Memory == nil || api.ValidateRecord("ComponentRef", r.cfg.ProofPolicy.PolicyRef) != nil {
		return out, api.E("unsupported", "agent_proof_publication_unconfigured")
	}
	s, err := r.service()
	if err != nil {
		return out, err
	}
	var publication remoteProofPublication
	err = r.within(ctx, func(tx runtime.Tx) error {
		var current task.DelegationScope
		var gateErr error
		current, gateErr = s.CheckDelegationScopeTx(ctx, tx, r.cfg.Auth, in.CreationKey)
		if gateErr != nil && !api.IsCode(gateErr, "invalid_state") && !api.IsCode(gateErr, "expired") && !api.IsCode(gateErr, "forbidden") {
			return gateErr
		}
		if current.Delegation.DelegationID == "" {
			return gateErr
		}
		if err := r.cfg.Authority.CheckPeerTx(ctx, tx, peer, saved.Packet.Input.ReceiverID); err != nil {
			return err
		}
		if current.SubjectRef != saved.Packet.SubjectRef || !api.Equal(current.Delegation.DelegateInput, saved.Packet.Input) || current.Delegation.AllocationRef != saved.Packet.AllocationRef {
			return api.E("idempotency_conflict", "original_remote_scope_changed")
		}
		actor, subjectErr := r.cfg.Authority.ResolveSubjectTx(ctx, tx, current.SubjectRef, r.profiles[profileKey(saved.Packet.ProfileRef)], false)
		if subjectErr != nil && !api.IsCode(subjectErr, "forbidden") && !api.IsCode(subjectErr, "expired") {
			return subjectErr
		}
		allowed := gateErr == nil && subjectErr == nil
		if allowed && r.cfg.ScopeGate != nil {
			if saved.Packet.ParentAdmission == nil {
				gateErr = api.E("unsupported", "original_parent_admission_unavailable")
			} else {
				gateErr = r.cfg.ScopeGate.CheckScopeTx(ctx, tx, actor, current, r.profiles[profileKey(saved.Packet.ProfileRef)], *saved.Packet.ParentAdmission)
			}
			if gateErr != nil && !api.IsCode(gateErr, "forbidden") && !api.IsCode(gateErr, "expired") && !api.IsCode(gateErr, "invalid_state") {
				return gateErr
			}
			allowed = gateErr == nil
		}
		if allowed && saved.Packet.SessionContext != nil {
			gateErr = r.checkOriginalSessionContextTx(ctx, tx, actor, current.Delegation, saved.Packet.SessionContext)
			if gateErr != nil && !api.IsCode(gateErr, "forbidden") && !api.IsCode(gateErr, "expired") && !api.IsCode(gateErr, "invalid_state") {
				return gateErr
			}
			allowed = gateErr == nil
		}
		reason := ""
		if !allowed {
			var e *api.Error
			if errors.As(gateErr, &e) || errors.As(subjectErr, &e) {
				reason = e.Reason
			}
		}
		now, err := tx.Now(ctx)
		if err != nil {
			return err
		}
		until := now.Add(30 * time.Second)
		deadline, err := api.ParseTime(current.Allocation.Deadline)
		if err != nil {
			return err
		}
		if allowed && deadline.Before(until) {
			until = deadline
		}
		if allowed && saved.Packet.ParentAdmission != nil {
			bound, err := api.ParseTime(saved.Packet.ParentAdmission.ValidUntil)
			if err != nil {
				return err
			}
			if bound.Before(until) {
				until = bound
			}
		}
		out = RemoteAllocationSnapshot{Packet: saved.Packet, Allocation: current.Allocation, ParentTask: remoteParentTask(current.ParentTask), Allowed: allowed, Reason: reason, IssuedAt: api.Time(now), StartBefore: api.Time(until)}
		semantic, err := remoteScopeDigest(out)
		if err != nil {
			return err
		}
		var latest remoteSent
		rev, err := tx.Get(ctx, remoteOutgoing, id, &latest)
		if err != nil {
			return err
		}
		if !api.Equal(latest.Packet, saved.Packet) {
			return api.E("idempotency_conflict", "original_remote_source_changed")
		}
		if latest.ScopeRevision == 0 || latest.ScopeDigest != semantic {
			latest.ScopeRevision++
			latest.ScopeDigest = semantic
			if err = tx.Put(ctx, remoteOutgoing, id, rev, latest); err != nil {
				return err
			}
		}
		out.ScopeRevision = latest.ScopeRevision
		digest, err := remoteAllocationDigest(out)
		if err != nil {
			return err
		}
		out.Proof, err = r.cfg.Keys.Sign(r.cfg.SigningKeyID, remoteAllocationClaims(out, digest))
		if err != nil {
			return err
		}
		body := []byte(out.Proof)
		ref := api.ContentRef{TenantID: r.cfg.Scope.TenantID, OwnerID: r.cfg.Scope.OwnerID, ContentID: remoteID("content", "agent-allocation", digest, api.Hash(body)), Version: 1, Hash: api.Hash(body), MediaType: "application/jose", ByteLength: uint64(len(body))}
		out.ProofRef = ref
		var old remoteProofPublication
		_, err = tx.Get(ctx, remoteProofs, ref.ContentID, &old)
		if err == nil {
			publication = old
			return nil
		}
		if !api.IsCode(err, "not_found") {
			return err
		}
		retain, err := api.ParseTime(r.cfg.ProofPolicy.Values.RetainUntil)
		if err != nil {
			return err
		}
		if deadline.Before(retain) {
			retain = deadline
		}
		if !now.Before(retain) {
			return api.E("expired", "agent_proof_retention_elapsed")
		}
		transferDeadline := now.Add(time.Minute)
		if retain.Before(transferDeadline) {
			transferDeadline = retain
		}
		publication = remoteProofPublication{Request: memory.PublicationRequest{ContentRef: ref, TransferID: remoteID("transfer", ref.ContentID), ReserveCommandID: remoteID("command", ref.ContentID, "reserve"), PutCommandID: remoteID("command", ref.ContentID, "put"), PolicyRef: r.cfg.ProofPolicy.PolicyRef, ProcessedSources: []api.ContentRef{}, DisclosedSources: []api.ContentRef{}, RetentionUntil: api.Time(retain), TransferDeadline: api.Time(transferDeadline)}, Bytes: body}
		return tx.Create(ctx, remoteProofs, ref.ContentID, saved.Packet.CreationKey, publication)
	})
	if err != nil {
		return out, err
	}
	if _, err = r.cfg.Memory.Upload(ctx, r.cfg.Scope, r.cfg.Auth, publication.Request, publication.Bytes); err != nil {
		return out, err
	}
	return out, nil
}
func (r *Remote) readParent(ctx context.Context, p RemoteCreateInput) (RemoteAllocationSnapshot, error) {
	var out RemoteAllocationSnapshot
	peer, ok := r.peers[p.DelegationRef.OwnerID]
	if !ok {
		return out, api.E("unsupported", "remote_allocation_source_unconfigured")
	}
	raw, err := peer.Client.Query(ctx, api.Query{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: peer.Scope.OwnerID, QueryID: api.NewID("query"), Method: "collaboration.allocation.current", TargetID: p.CreationKey, Payload: api.Raw(RemoteKeyInput{p.DelegationRef.OwnerID, p.CreationKey})})
	if err != nil {
		return out, err
	}
	if err = api.Decode(raw, &out); err != nil {
		return out, err
	}
	if err = r.verifyParent(peer, p, out, time.Now(), true); err != nil {
		return out, err
	}
	return out, nil
}
func (r *Remote) verifyParent(peer RemotePeer, p RemoteCreateInput, out RemoteAllocationSnapshot, now time.Time, window bool) error {
	if out.ScopeRevision == 0 || !api.Equal(out.Packet, p) || peer.Scope.DatabaseID != p.SourceDatabaseID || out.Allocation.ParentTaskRef.OwnerID != p.DelegationRef.OwnerID || out.Allocation.ParentTaskRef.ObjectID != p.Input.ParentTaskRef.ObjectID || out.Allocation.AllocationID != p.AllocationRef.ObjectID || out.Allocation.ReceiverID != p.Input.ReceiverID || out.Allocation.Deadline != p.Input.Deadline || !api.Equal(out.Allocation.Limits, p.Input.Budget) || out.ParentTask.TaskID != p.Input.ParentTaskRef.ObjectID || out.ParentTask.OrchestratorID != p.DelegationRef.OwnerID || out.ParentTask.TenantID != r.cfg.Scope.TenantID || api.ValidateRecord("ContentRef", out.ProofRef) != nil || out.ProofRef.TenantID != p.SubjectRef.TenantID || out.ProofRef.OwnerID != p.DelegationRef.OwnerID || out.ProofRef.Hash != api.Hash([]byte(out.Proof)) || out.ProofRef.ByteLength != uint64(len(out.Proof)) || out.ProofRef.MediaType != "application/jose" {
		return api.E("forbidden", "original_parent_allocation_mismatch")
	}
	issued, err := api.ParseTime(out.IssuedAt)
	if err != nil {
		return err
	}
	until, err := api.ParseTime(out.StartBefore)
	if err != nil {
		return err
	}
	if !issued.Before(until) || until.Sub(issued) > 30*time.Second {
		return api.E("forbidden", "parent_window_scope_exceeded")
	}
	digest, err := remoteAllocationDigest(out)
	if err != nil {
		return err
	}
	if window {
		_, err = peer.Keys.Verify(out.Proof, remoteAllocationClaims(out, digest), now)
	} else {
		_, err = peer.Keys.VerifySource(out.Proof, remoteAllocationClaims(out, digest))
	}
	if err != nil {
		return err
	}
	if out.Allowed && (out.ParentTask.Status != "active" || out.ParentTask.Control != "running" || out.ParentTask.GoalRevision != p.Input.ParentGoalRevision || out.Allocation.State != "open" && out.Allocation.State != "preparing") {
		return api.E("forbidden", "parent_scope_not_current")
	}
	return nil
}
func remoteQueryMethod[I, O any](r *Remote, name string, fn func(context.Context, runtime.Auth, api.Query, I) (O, error)) runtime.Method {
	contract := api.Contract[I, O](name, "orchestrator", "query", false, false)
	if properties, ok := contract.OutputSchema["properties"].(map[string]any); ok {
		if _, exists := properties["proof"]; exists {
			properties["proof"] = api.Schema{"type": "string", "maxLength": 32768}
		}
	}
	return runtime.Method{Contract: contract, Query: func(ctx context.Context, store runtime.Store, scope runtime.Scope, a runtime.Auth, q api.Query) (any, error) {
		if scope != r.cfg.Scope || store.ID() != r.cfg.Store.ID() {
			return nil, api.E("forbidden", "remote_query_database_mismatch")
		}
		var in I
		if err := api.Decode(q.Payload, &in); err != nil {
			return nil, err
		}
		return fn(ctx, a, q, in)
	}}
}

// RemoteAgentContracts 供静态配对时创建同版SDK，尚未装配的宿主不自动开放方法。
func RemoteAgentContracts() []api.MethodContract {
	contracts := []api.MethodContract{
		api.Contract[task.DelegateInput, task.DelegateOutput]("collaboration.delegate", "orchestrator", "command", false, false),
		api.Contract[RemoteCreateInput, RemoteCreateOutput]("collaboration.create", "orchestrator", "command", false, true),
		api.Contract[task.AllocationCloseInput, task.AllocationOutput]("collaboration.allocation.close", "orchestrator", "command", false, false),
		api.Contract[RemoteControlInput, RemoteControlOutput]("collaboration.control", "orchestrator", "command", false, true),
		api.Contract[RemoteClosureReport, task.AllocationOutput]("collaboration.closure.report", "orchestrator", "command", false, false),
		api.Contract[RemoteAllocationReadInput, RemoteAllocationReadOutput]("collaboration.allocation.get", "orchestrator", "query", false, false),
		api.Contract[RemoteClosureInput, RemoteClosureReport]("collaboration.closure.get", "orchestrator", "query", false, false),
		api.Contract[RemoteKeyInput, RemoteAllocationSnapshot]("collaboration.allocation.current", "orchestrator", "query", false, false),
		api.Contract[RemoteKeyInput, RemoteState]("collaboration.state", "orchestrator", "query", false, false),
		api.Contract[RemoteInputSend, RemoteInputAck]("collaboration.input.send", "orchestrator", "command", false, true),
		remoteInputContract(),
	}
	for i := range contracts {
		if properties, ok := contracts[i].OutputSchema["properties"].(map[string]any); ok {
			if _, exists := properties["proof"]; exists {
				properties["proof"] = api.Schema{"type": "string", "maxLength": 32768}
			}
		}
		contracts[i].SchemaDigest, _ = api.Digest([]any{contracts[i].InputSchema, contracts[i].OutputSchema})
	}
	return contracts
}
