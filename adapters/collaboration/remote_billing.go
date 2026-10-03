package collaboration

import (
	"context"
	"time"

	"github.com/ruipengliu/lerna/adapters/platform"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/task"
	"github.com/ruipengliu/lerna/runtime"
)

type RemoteAllocationReadInput struct {
	AllocationRef api.ObjectRef `json:"allocation_ref"`
}
type RemoteAllocationReadOutput struct {
	Allocation       task.Allocation `json:"allocation"`
	SourceDatabaseID string          `json:"source_database_id"`
	IssuedAt         string          `json:"issued_at"`
	StartBefore      string          `json:"start_before"`
	Proof            string          `json:"proof"`
}

func allocationReadDigest(out RemoteAllocationReadOutput) (string, error) {
	out.Proof = ""
	return api.Digest(out)
}
func allocationReadClaims(scope runtime.Scope, out RemoteAllocationReadOutput, digest string) platform.ProofClaims {
	return platform.ProofClaims{TenantID: scope.TenantID, Issuer: scope.OwnerID, Audience: out.Allocation.ReceiverID, Purpose: "agent_allocation", ObjectRef: scope.Ref(out.Allocation.AllocationID, out.Allocation.Revision), Digest: digest, WindowID: out.Allocation.AllocationID, IssuedAt: out.IssuedAt, StartBefore: out.StartBefore}
}
func (r *Remote) allocationRead(ctx context.Context, peer runtime.Auth, q api.Query, in RemoteAllocationReadInput) (RemoteAllocationReadOutput, error) {
	var out RemoteAllocationReadOutput
	if in.AllocationRef.OwnerID != r.cfg.Scope.OwnerID || in.AllocationRef.TenantID != r.cfg.Scope.TenantID || q.TargetID != in.AllocationRef.ObjectID || api.ValidateRecord("ObjectRef", in.AllocationRef) != nil {
		return out, api.E("forbidden", "allocation_source_scope_mismatch")
	}
	s, err := r.service()
	if err != nil {
		return out, err
	}
	err = r.within(ctx, func(tx runtime.Tx) error {
		a, err := s.ReadAllocationTx(ctx, tx, r.cfg.Auth, in.AllocationRef.ObjectID)
		if err != nil {
			return err
		}
		if a.Revision < in.AllocationRef.Revision {
			return api.E("revision_conflict", "allocation_version_not_current")
		}
		if err = r.cfg.Authority.CheckPeerTx(ctx, tx, peer, a.ReceiverID); err != nil {
			return err
		}
		now, err := tx.Now(ctx)
		if err != nil {
			return err
		}
		out = RemoteAllocationReadOutput{Allocation: a, SourceDatabaseID: r.cfg.Scope.DatabaseID, IssuedAt: api.Time(now), StartBefore: api.Time(now.Add(30 * time.Second))}
		digest, err := allocationReadDigest(out)
		if err != nil {
			return err
		}
		out.Proof, err = r.cfg.Keys.Sign(r.cfg.SigningKeyID, allocationReadClaims(r.cfg.Scope, out, digest))
		return err
	})
	return out, err
}

type RemoteClosureInput struct {
	ClosureRef api.ObjectRef `json:"closure_ref"`
}
type RemoteClosureReport struct {
	ClosureRef       api.ObjectRef         `json:"closure_ref"`
	Closure          api.AllocationClosure `json:"closure"`
	SourceDatabaseID string                `json:"source_database_id"`
	IssuedAt         string                `json:"issued_at"`
	StartBefore      string                `json:"start_before"`
	Proof            string                `json:"proof"`
}

func closureReportDigest(p RemoteClosureReport) (string, error) { p.Proof = ""; return api.Digest(p) }
func closureReportClaims(scope runtime.Scope, p RemoteClosureReport, digest string) platform.ProofClaims {
	return platform.ProofClaims{TenantID: scope.TenantID, Issuer: scope.OwnerID, Audience: p.Closure.ParentOwnerID, Purpose: "agent_state", ObjectRef: p.ClosureRef, Digest: digest, WindowID: p.Closure.AllocationID, IssuedAt: p.IssuedAt, StartBefore: p.StartBefore}
}
func (r *Remote) verifyClosure(peer RemotePeer, p RemoteClosureReport, now time.Time) error {
	if api.ValidateRecord("AllocationClosure", p.Closure) != nil || api.ValidateRecord("ObjectRef", p.ClosureRef) != nil || p.SourceDatabaseID != peer.Scope.DatabaseID || p.ClosureRef.TenantID != r.cfg.Scope.TenantID || p.ClosureRef.OwnerID != peer.Scope.OwnerID || p.Closure.ParentOwnerID != r.cfg.Scope.OwnerID || p.Closure.ReceiverID != peer.Scope.OwnerID || !p.Closure.SpendingClosed || p.Closure.ProofRef.OwnerID != peer.Scope.OwnerID || p.Closure.ProofRef.TenantID != r.cfg.Scope.TenantID {
		return api.E("forbidden", "original_closure_source_mismatch")
	}
	issued, err := api.ParseTime(p.IssuedAt)
	if err != nil {
		return err
	}
	until, err := api.ParseTime(p.StartBefore)
	if err != nil {
		return err
	}
	if !issued.Before(until) || until.Sub(issued) > 10*time.Minute {
		return api.E("forbidden", "closure_window_exceeded")
	}
	digest, err := closureReportDigest(p)
	if err != nil {
		return err
	}
	_, err = peer.Keys.Verify(p.Proof, closureReportClaims(peer.Scope, p, digest), now)
	return err
}
func (r *Remote) closureRead(ctx context.Context, peer runtime.Auth, q api.Query, in RemoteClosureInput) (RemoteClosureReport, error) {
	var out RemoteClosureReport
	if in.ClosureRef.OwnerID != r.cfg.Scope.OwnerID || in.ClosureRef.TenantID != r.cfg.Scope.TenantID || q.TargetID != in.ClosureRef.ObjectID {
		return out, api.E("forbidden", "closure_source_scope_mismatch")
	}
	s, err := r.service()
	if err != nil {
		return out, err
	}
	c, err := s.AllocationClosureRead(ctx, r.cfg.Store, r.cfg.Scope, r.cfg.Auth, in.ClosureRef)
	if err != nil {
		return out, err
	}
	err = r.within(ctx, func(tx runtime.Tx) error {
		if err := r.cfg.Authority.CheckPeerTx(ctx, tx, peer, c.ParentOwnerID); err != nil {
			return err
		}
		now, err := tx.Now(ctx)
		if err != nil {
			return err
		}
		out = RemoteClosureReport{ClosureRef: in.ClosureRef, Closure: c, SourceDatabaseID: r.cfg.Scope.DatabaseID, IssuedAt: api.Time(now), StartBefore: api.Time(now.Add(30 * time.Second))}
		digest, err := closureReportDigest(out)
		if err != nil {
			return err
		}
		out.Proof, err = r.cfg.Keys.Sign(r.cfg.SigningKeyID, closureReportClaims(r.cfg.Scope, out, digest))
		return err
	})
	return out, err
}
func (r *Remote) receiveClosure(ctx context.Context, tx runtime.Tx, peerAuth runtime.Auth, c api.Command, in RemoteClosureReport) (runtime.Outcome, error) {
	if err := r.checkScope(tx.Scope()); err != nil {
		return runtime.Outcome{}, err
	}
	if c.TargetID != in.Closure.AllocationID {
		return runtime.Outcome{}, api.E("invalid_request", "closure_target_mismatch")
	}
	s, err := r.service()
	if err != nil {
		return runtime.Outcome{}, err
	}
	a, err := s.ReadAllocationTx(ctx, tx, r.cfg.Auth, in.Closure.AllocationID)
	if err != nil {
		return runtime.Outcome{}, err
	}
	peer, ok := r.peers[a.ReceiverID]
	if !ok {
		return runtime.Outcome{}, api.E("unsupported", "closure_peer_unconfigured")
	}
	if err = r.cfg.Authority.CheckPeerTx(ctx, tx, peerAuth, a.ReceiverID); err != nil {
		return runtime.Outcome{}, err
	}
	now, err := tx.Now(ctx)
	if err != nil {
		return runtime.Outcome{}, err
	}
	if err = r.verifyClosure(peer, in, now); err != nil {
		return runtime.Outcome{}, err
	}
	if err = s.ReconcileClosureTx(ctx, tx, r.cfg.Auth, a.AllocationID, in.Closure, in.ClosureRef); err != nil {
		return runtime.Outcome{}, err
	}
	return runtime.Applied(task.AllocationOutput{AllocationRef: tx.Scope().Ref(a.AllocationID, a.Revision+1), State: "settled"}), nil
}
func (r *Remote) ReportClosure(ctx context.Context, scope runtime.Scope, parent string, ref api.ObjectRef, c api.AllocationClosure) error {
	if err := r.checkScope(scope); err != nil {
		return err
	}
	peer, ok := r.peers[parent]
	if !ok {
		return api.E("unsupported", "closure_parent_unconfigured")
	}
	s, err := r.service()
	if err != nil {
		return err
	}
	original, err := s.AllocationClosureRead(ctx, r.cfg.Store, scope, r.cfg.Auth, ref)
	if err != nil {
		return err
	}
	if !api.Equal(original, c) || c.ParentOwnerID != parent || c.ReceiverID != scope.OwnerID {
		return api.E("idempotency_conflict", "original_closure_changed")
	}
	// 原封存证明字节必须已实际出版；恢复只沿原准确上传身份。
	var publication remoteProofPublication
	if _, err = r.cfg.Store.Read(ctx, scope, remoteProofs, c.ProofRef.ContentID, 0, &publication); err != nil {
		return err
	}
	if !api.Equal(publication.Request.ContentRef, c.ProofRef) {
		return api.E("idempotency_conflict", "original_closure_proof_changed")
	}
	if _, err = r.cfg.Memory.Upload(ctx, scope, r.cfg.Auth, publication.Request, publication.Bytes); err != nil {
		return err
	}
	inc, err := s.IncomingRead(ctx, r.cfg.Store, scope, r.cfg.Auth, api.ObjectRef{TenantID: scope.TenantID, OwnerID: parent, ObjectID: c.AllocationID, Revision: 1})
	if err != nil {
		return err
	}
	var command api.Command
	err = r.within(ctx, func(tx runtime.Tx) error {
		if inc.TaskRef != nil {
			if _, err := s.ReadTaskTx(ctx, tx, r.cfg.Auth, inc.TaskRef.ObjectID); err != nil {
				return err
			}
		}
		digest, err := api.Digest(ref)
		if err != nil {
			return err
		}
		id := remoteID("command", scope.TenantID, scope.OwnerID, "closure-report", digest)
		var saved remoteCommand
		_, err = tx.Get(ctx, remoteCommands, id, &saved)
		if err == nil {
			command = saved.Command
			return nil
		}
		if !api.IsCode(err, "not_found") {
			return err
		}
		now, err := tx.Now(ctx)
		if err != nil {
			return err
		}
		p := RemoteClosureReport{ClosureRef: ref, Closure: c, SourceDatabaseID: scope.DatabaseID, IssuedAt: api.Time(now), StartBefore: api.Time(now.Add(10 * time.Minute))}
		digest, err = closureReportDigest(p)
		if err != nil {
			return err
		}
		p.Proof, err = r.cfg.Keys.Sign(r.cfg.SigningKeyID, closureReportClaims(scope, p, digest))
		if err != nil {
			return err
		}
		command = api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: parent, CommandID: id, Method: "collaboration.closure.report", TargetID: c.AllocationID, ExpiresAt: p.StartBefore, Payload: api.Raw(p)}
		return tx.Create(ctx, remoteCommands, id, c.AllocationID, remoteCommand{command})
	})
	if err != nil {
		return err
	}
	receipt, err := sendOriginal(ctx, peer, command, nil)
	return knownReceipt(receipt, err)
}

func (r *Remote) ReadClosure(ctx context.Context, scope runtime.Scope, ref api.ObjectRef) (api.AllocationClosure, error) {
	if err := r.checkScope(scope); err != nil {
		return api.AllocationClosure{}, err
	}
	if ref.OwnerID == scope.OwnerID {
		s, err := r.service()
		if err != nil {
			return api.AllocationClosure{}, err
		}
		return s.AllocationClosureRead(ctx, r.cfg.Store, scope, r.cfg.Auth, ref)
	}
	peer, ok := r.peers[ref.OwnerID]
	if !ok {
		return api.AllocationClosure{}, api.E("unsupported", "closure_source_unconfigured")
	}
	raw, err := peer.Client.Query(ctx, api.Query{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: ref.OwnerID, QueryID: api.NewID("query"), Method: "collaboration.closure.get", TargetID: ref.ObjectID, Payload: api.Raw(RemoteClosureInput{ref})})
	if err != nil {
		return api.AllocationClosure{}, err
	}
	var out RemoteClosureReport
	if err = api.Decode(raw, &out); err != nil {
		return out.Closure, err
	}
	if out.ClosureRef != ref {
		return api.AllocationClosure{}, api.E("forbidden", "original_closure_ref_changed")
	}
	if err = r.verifyClosure(peer, out, time.Now()); err != nil {
		return api.AllocationClosure{}, err
	}
	return out.Closure, nil
}
