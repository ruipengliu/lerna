package execution

import (
	"context"
	"strconv"
	"time"

	"github.com/ruipengliu/lerna/api"
	rt "github.com/ruipengliu/lerna/runtime"
)

// HostCallPosition 由受信宿主在原父 Operation 事务分配；程序不能提供位置。
type HostCall struct {
	HostCallID        string         `json:"hostcall_id"`
	Revision          uint64         `json:"revision"`
	ParentOperationID string         `json:"parent_operation_id"`
	EnvironmentID     string         `json:"environment_id"`
	Generation        uint64         `json:"generation"`
	CallPosition      string         `json:"call_position"`
	Kind              string         `json:"kind"`
	TypedInputRef     api.ContentRef `json:"typed_input_ref"`
	InputDigest       string         `json:"input_digest"`
	TaskRef           api.ObjectRef  `json:"task_ref"`
	CommandRef        api.ObjectRef  `json:"command_ref"`
	Phase             string         `json:"phase"`
	TargetKind        string         `json:"target_kind,omitempty"`
	TargetRef         *api.ObjectRef `json:"target_ref,omitempty"`
	ReceiptRef        *api.ObjectRef `json:"receipt_ref,omitempty"`
	DenialReason      string         `json:"denial_reason,omitempty"`
}
type HostCallInput struct {
	Kind               string
	TypedInputRef      api.ContentRef
	InputDigest        string
	ExpectedGeneration uint64
}
type HostCallDecision struct {
	Denied     bool
	Reason     string
	TargetKind string
	TargetRef  api.ObjectRef
	ReceiptRef api.ObjectRef
}
type HostCallPort interface {
	Admit(context.Context, rt.Scope, HostCall) (HostCallDecision, error)
	Resolve(context.Context, rt.Scope, HostCall) (HostCallDecision, error)
	Close(context.Context, rt.Scope, HostCall) (bool, error)
}

// AllocateHostCallTx 与调用方原准入/outbox共事务；participants 必须含 execution。
func (s *Service) AllocateHostCallTx(ctx context.Context, tx rt.Tx, environmentID, parentOperationID string, input HostCallInput) (HostCall, error) {
	if s.cfg.HostCalls == nil {
		return HostCall{}, api.E("unsupported", "hostcall_admission_not_ready")
	}
	if input.Kind != "operation" && input.Kind != "decision" && input.Kind != "delegation" {
		return HostCall{}, api.E("invalid_request", "invalid_hostcall_kind")
	}
	if input.TypedInputRef.TenantID != tx.Scope().TenantID || input.InputDigest == "" {
		return HostCall{}, api.E("forbidden", "hostcall_input_scope_mismatch")
	}
	var env Environment
	rev, err := tx.Get(ctx, Namespace+".environments", environmentID, &env)
	if err != nil {
		return HostCall{}, err
	}
	if env.Generation != input.ExpectedGeneration {
		return HostCall{}, api.E("revision_conflict", "generation_changed")
	}
	if env.Phase != "active" || len(env.StopResiduals) > 0 {
		return HostCall{}, api.E("invalid_state", "environment_not_active")
	}
	var parent operationRecord
	if _, err = tx.Get(ctx, Namespace+".operations", parentOperationID, &parent); err != nil {
		return HostCall{}, err
	}
	if parent.NewAttemptsClosed || parent.Intent == nil {
		return HostCall{}, api.E("invalid_state", "parent_cell_closed")
	}
	found := false
	for _, id := range env.ActiveOperationIDs {
		found = found || id == parentOperationID
	}
	if !found {
		return HostCall{}, api.E("forbidden", "parent_not_in_environment")
	}
	records, err := tx.List(ctx, Namespace+".hostcalls", parentOperationID, "", 100)
	if err != nil {
		return HostCall{}, err
	}
	if len(records) >= 100 {
		return HostCall{}, api.E("overloaded", "hostcall_limit_reached")
	}
	position := strconv.Itoa(len(records) + 1)
	id := stableID("hostcall", parentOperationID+":"+position)
	h := HostCall{HostCallID: id, Revision: 1, ParentOperationID: parentOperationID, EnvironmentID: environmentID, Generation: env.Generation, CallPosition: position, Kind: input.Kind, TypedInputRef: input.TypedInputRef, InputDigest: input.InputDigest, TaskRef: parent.Invoke.TaskRef, CommandRef: api.ObjectRef{TenantID: tx.Scope().TenantID, OwnerID: parent.Invoke.TaskRef.OwnerID, ObjectID: stableID("command", id), Revision: 1}, Phase: "prepared"}
	if err = tx.Create(ctx, Namespace+".hostcalls", id, parentOperationID, h); err != nil {
		return h, err
	}
	env.HostCallIDs = append(env.HostCallIDs, id)
	env.Revision = rev + 1
	if err = tx.Put(ctx, Namespace+".environments", environmentID, rev, env); err != nil {
		return h, err
	}
	now, err := tx.Now(ctx)
	if err != nil {
		return h, err
	}
	_, err = tx.Raise(ctx, HostCallJob, id, tx.Scope().Ref(id, 1), now)
	return h, err
}

// ReadHostCallPositionTx 只读原位置；异参永远冲突，不刷新控制或新建子责任。
func (s *Service) ReadHostCallPositionTx(ctx context.Context, tx rt.Tx, parentID, position, digest string) (HostCall, error) {
	var h HostCall
	id := stableID("hostcall", parentID+":"+position)
	_, err := tx.Get(ctx, Namespace+".hostcalls", id, &h)
	if err != nil {
		return h, err
	}
	if h.InputDigest != digest {
		return h, api.E("idempotency_conflict", "hostcall_input_changed")
	}
	return h, nil
}
func (s *Service) hostCallWork(ctx context.Context, st rt.Store, sc rt.Scope, w rt.Work) error {
	if s.cfg.HostCalls == nil {
		return api.E("unsupported", "hostcall_admission_not_ready")
	}
	var h HostCall
	if _, err := st.Read(ctx, sc, Namespace+".hostcalls", w.Job.SourceRef.ObjectID, 0, &h); err != nil {
		return err
	}
	var env Environment
	if _, err := st.Read(ctx, sc, Namespace+".environments", h.EnvironmentID, 0, &env); err != nil {
		return err
	}
	closing := env.Phase != "active" || env.Generation != h.Generation
	if closing && (h.Phase == "mapped" || h.Phase == "closing") {
		closed, err := s.cfg.HostCalls.Close(ctx, sc, h)
		if err != nil {
			return err
		}
		disp := rt.Done()
		if !closed {
			disp = rt.Waiting(time.Now().UTC().Add(time.Minute))
		}
		return s.finish(ctx, st, sc, w, disp, func(tx rt.Tx) error {
			var current HostCall
			rev, err := tx.Get(ctx, Namespace+".hostcalls", h.HostCallID, &current)
			if err != nil {
				return err
			}
			current.Revision = rev + 1
			current.Phase = "closing"
			if closed {
				current.Phase = "closed"
			}
			if err = tx.Put(ctx, Namespace+".hostcalls", h.HostCallID, rev, current); err != nil {
				return err
			}
			if closed {
				return s.wakeEnvironmentCleanup(ctx, tx, h.EnvironmentID)
			}
			return nil
		})
	}
	if h.Phase == "closed" || h.Phase == "denied" || h.Phase == "mapped" {
		return s.finish(ctx, st, sc, w, rt.Done(), nil)
	}
	var decision HostCallDecision
	var err error
	if h.Phase == "prepared" {
		if closing {
			return s.finish(ctx, st, sc, w, rt.Done(), func(tx rt.Tx) error {
				var current HostCall
				rev, err := tx.Get(ctx, Namespace+".hostcalls", h.HostCallID, &current)
				if err != nil {
					return err
				}
				current.Revision = rev + 1
				current.Phase = "denied"
				current.DenialReason = "environment_closed"
				return tx.Put(ctx, Namespace+".hostcalls", h.HostCallID, rev, current)
			})
		}
		status, e := st.Within(ctx, sc, []string{Namespace}, func(tx rt.Tx) error {
			if e := tx.Guard(ctx, w.Claim); e != nil {
				return e
			}
			var current HostCall
			rev, e := tx.Get(ctx, Namespace+".hostcalls", h.HostCallID, &current)
			if e != nil {
				return e
			}
			if current.Phase != "prepared" {
				return api.E("invalid_state", "hostcall_already_sent")
			}
			current.Revision = rev + 1
			current.Phase = "possibly_sent"
			return tx.Put(ctx, Namespace+".hostcalls", h.HostCallID, rev, current)
		})
		if status == rt.CommitUnknown {
			return rt.ErrCommitUnknown
		}
		if e != nil {
			return e
		}
		h.Phase = "possibly_sent"
		decision, err = s.cfg.HostCalls.Admit(ctx, sc, h)
	} else {
		decision, err = s.cfg.HostCalls.Resolve(ctx, sc, h)
	}
	if err != nil {
		return err
	}
	if !decision.Denied && (decision.TargetKind != h.Kind || decision.TargetRef.TenantID != sc.TenantID || decision.TargetRef.Revision == 0) {
		return api.E("invalid_request", "hostcall_target_kind_mismatch")
	}
	return s.finish(ctx, st, sc, w, rt.Done(), func(tx rt.Tx) error {
		var current HostCall
		rev, err := tx.Get(ctx, Namespace+".hostcalls", h.HostCallID, &current)
		if err != nil {
			return err
		}
		current.Revision = rev + 1
		if decision.Denied {
			current.Phase = "denied"
			current.DenialReason = decision.Reason
		} else {
			current.Phase = "mapped"
			current.TargetKind = decision.TargetKind
			current.TargetRef = &decision.TargetRef
			current.ReceiptRef = &decision.ReceiptRef
		}
		if err = tx.Put(ctx, Namespace+".hostcalls", h.HostCallID, rev, current); err != nil {
			return err
		}
		var latest Environment
		if _, err = tx.Get(ctx, Namespace+".environments", h.EnvironmentID, &latest); err != nil {
			return err
		}
		if latest.Phase != "active" || latest.Generation != h.Generation {
			now, err := tx.Now(ctx)
			if err != nil {
				return err
			}
			_, err = tx.Raise(ctx, HostCallJob, h.HostCallID, sc.Ref(h.HostCallID, current.Revision), now)
			return err
		}
		return nil
	})
}

func (s *Service) wakeEnvironmentCleanup(ctx context.Context, tx rt.Tx, id string) error {
	var env Environment
	rev, err := tx.Get(ctx, Namespace+".environments", id, &env)
	if err != nil {
		return err
	}
	if env.Phase != "closing" && env.Phase != "destroying" {
		return nil
	}
	env.Revision = rev + 1
	if err = tx.Put(ctx, Namespace+".environments", id, rev, env); err != nil {
		return err
	}
	now, err := tx.Now(ctx)
	if err != nil {
		return err
	}
	_, err = tx.Raise(ctx, EnvironmentCleanupJob, id, tx.Scope().Ref(id, env.Revision), now)
	return err
}
