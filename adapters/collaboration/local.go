// Package collaboration 将同owner协作交接绑定到真实Dispatcher。
// Task负责目标及预算，本adapter只持久保存原转交输入并恢复原命令。
package collaboration

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/interaction"
	"github.com/ruipengliu/lerna/internal/task"
	"github.com/ruipengliu/lerna/runtime"
)

const Namespace = "collaboration"
const commandIntents = "collaboration.command_intents"

type DeliveryPort interface {
	Send(context.Context, runtime.Scope, runtime.Auth, api.Command) (api.Receipt, error)
	Lookup(context.Context, runtime.Scope, runtime.Auth, string, string) (api.Receipt, error)
}
type Config struct {
	Store        runtime.Store
	Registry     *runtime.Registry
	OwnerID      string
	Auth         runtime.Auth
	SubjectGate  task.SubjectGate
	Participants []string
	Delivery     DeliveryPort
}
type Adapter struct {
	config   Config
	delivery DeliveryPort
	mu       sync.RWMutex
	task     *task.Service
}
type commandIntent struct {
	SourceDigest string       `json:"source_digest"`
	Auth         runtime.Auth `json:"auth"`
	Command      api.Command  `json:"command"`
}
type localDelivery struct{ dispatch *runtime.Dispatcher }

func (d localDelivery) Send(ctx context.Context, _ runtime.Scope, auth runtime.Auth, command api.Command) (api.Receipt, error) {
	return d.dispatch.Command(ctx, auth, api.Raw(command))
}
func (d localDelivery) Lookup(ctx context.Context, _ runtime.Scope, auth runtime.Auth, owner, id string) (api.Receipt, error) {
	if owner != d.dispatch.OwnerID {
		return api.Receipt{}, api.E("unsupported", "remote_owner_not_configured")
	}
	return d.dispatch.Lookup(ctx, auth, id)
}

func New(config Config) (*Adapter, error) {
	if config.Store == nil || config.Registry == nil || !api.ValidID(config.OwnerID) || !api.ValidID(config.Auth.TenantID) || !api.ValidID(config.Auth.SubjectID) || config.Auth.CredentialGeneration == 0 || !config.Auth.HasRole("service") {
		return nil, fmt.Errorf("collaboration requires a store, registry, owner and trusted service auth")
	}
	if len(config.Participants) == 0 {
		config.Participants = []string{Namespace}
	}
	config.Participants = append([]string{}, config.Participants...)
	a := &Adapter{config: config, delivery: config.Delivery}
	if a.delivery == nil {
		a.delivery = localDelivery{&runtime.Dispatcher{Store: config.Store, Registry: config.Registry, OwnerID: config.OwnerID}}
	}
	return a, nil
}

// BindTask只在宿主装配时调用一次，随后所有Task调用均经受信用例。
func (a *Adapter) BindTask(service *task.Service) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if service == nil || a.task != nil {
		return fmt.Errorf("collaboration task binding must be configured once")
	}
	a.task = service
	return nil
}
func (a *Adapter) taskService() (*task.Service, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.task == nil {
		return nil, api.E("unsupported", "task_service_not_bound")
	}
	return a.task, nil
}
func (a *Adapter) checkScope(scope runtime.Scope) error {
	if scope.OwnerID != a.config.OwnerID || scope.TenantID != a.config.Auth.TenantID || scope.DatabaseID != a.config.Store.ID() {
		return api.E("forbidden", "collaboration_scope_mismatch")
	}
	return nil
}
func (a *Adapter) CheckCollaborationTx(_ context.Context, tx runtime.Tx, _ runtime.Auth, kind, receiver string) error {
	if err := a.checkScope(tx.Scope()); err != nil {
		return err
	}
	if receiver != a.config.OwnerID {
		return api.E("unsupported", "remote_collaboration_not_configured")
	}
	if a.config.SubjectGate == nil {
		return api.E("unsupported", "collaboration_subject_gate_not_configured")
	}
	if _, err := a.taskService(); err != nil {
		return err
	}
	if kind != "session" && kind != "transfer" {
		return api.E("unsupported", "local_delegation_requires_internal_mode")
	}
	return nil
}

func (a *Adapter) plan(ctx context.Context, scope runtime.Scope, id string, auth runtime.Auth, source any, build func() api.Command) (commandIntent, error) {
	var saved commandIntent
	if err := a.checkScope(scope); err != nil {
		return saved, err
	}
	if !api.ValidID(id) || auth.TenantID != scope.TenantID || !api.ValidID(auth.SubjectID) || auth.CredentialGeneration == 0 {
		return saved, api.E("forbidden", "original_collaboration_identity_unavailable")
	}
	digest, err := api.Digest(source)
	if err != nil {
		return saved, err
	}
	status, err := a.config.Store.Within(ctx, scope, a.config.Participants, func(tx runtime.Tx) error {
		_, err := tx.Get(ctx, commandIntents, id, &saved)
		if err == nil {
			if saved.SourceDigest != digest || !api.Equal(saved.Auth, auth) {
				return api.E("idempotency_conflict", "original_collaboration_input_changed")
			}
			return nil
		}
		if !errors.Is(err, runtime.ErrNotFound) && !api.IsCode(err, "not_found") {
			return err
		}
		if a.config.SubjectGate == nil {
			return api.E("unsupported", "collaboration_subject_gate_not_configured")
		}
		if err = a.config.SubjectGate.CheckSubjectTx(ctx, tx, auth); err != nil {
			return err
		}
		saved = commandIntent{SourceDigest: digest, Auth: auth, Command: build()}
		if saved.Command.CommandID != id || saved.Command.LogicalServiceID != scope.OwnerID {
			return api.E("invalid_request", "original_collaboration_command_mismatch")
		}
		return tx.Create(ctx, commandIntents, id, auth.SubjectID, saved)
	})
	if status == runtime.CommitUnknown {
		return saved, runtime.ErrCommitUnknown
	}
	return saved, err
}
func (a *Adapter) forward(ctx context.Context, scope runtime.Scope, saved commandIntent, guard func(runtime.Tx) error) (api.Receipt, error) {
	lookup := func() (api.Receipt, error) {
		return a.delivery.Lookup(ctx, scope, saved.Auth, saved.Command.LogicalServiceID, saved.Command.CommandID)
	}
	r, err := lookup()
	if err == nil {
		return terminalReceipt(r)
	}
	if !errors.Is(err, runtime.ErrNotFound) && !api.IsCode(err, "not_found") {
		return api.Receipt{}, err
	}
	status, err := a.config.Store.Within(ctx, scope, a.config.Participants, func(tx runtime.Tx) error {
		if a.config.SubjectGate == nil {
			return api.E("unsupported", "collaboration_subject_gate_not_configured")
		}
		if guard != nil {
			if err := guard(tx); err != nil {
				return err
			}
		}
		return a.config.SubjectGate.CheckSubjectTx(ctx, tx, saved.Auth)
	})
	if status == runtime.CommitUnknown {
		return api.Receipt{}, runtime.ErrCommitUnknown
	}
	if err != nil {
		return api.Receipt{}, err
	}
	r, err = a.delivery.Send(ctx, scope, saved.Auth, saved.Command)
	if err != nil {
		// 发送未知只查同一原命令；不能制造替代命令或业务失败。
		if original, lookupErr := lookup(); lookupErr == nil {
			return terminalReceipt(original)
		} else {
			return api.Receipt{}, lookupErr
		}
	}
	return terminalReceipt(r)
}
func terminalReceipt(receipt api.Receipt) (api.Receipt, error) {
	switch receipt.Stage {
	case "applied":
		return receipt, nil
	case "accepted":
		return receipt, api.E("dependency_unavailable", "original_collaboration_command_pending")
	case "rejected":
		if receipt.Error != nil {
			return receipt, &task.OriginalCommandRejection{Receipt: receipt}
		}
		return receipt, api.E("invalid_state", "original_collaboration_command_rejected")
	default:
		return receipt, api.E("dependency_unavailable", "original_collaboration_receipt_unknown")
	}
}
func (a *Adapter) CreateSession(ctx context.Context, scope runtime.Scope, h task.ChildHandle) (api.ObjectRef, error) {
	if h.SessionOwnerID != a.config.OwnerID || h.SessionCommandRef.OwnerID != a.config.OwnerID || h.SessionCommandRef.TenantID != scope.TenantID {
		return api.ObjectRef{}, api.E("unsupported", "remote_session_not_configured")
	}
	auth := runtime.Auth{TenantID: scope.TenantID, SubjectID: h.SubjectID, CredentialGeneration: h.SubjectGeneration, Roles: append([]string{}, h.SubjectRoles...)}
	source := struct {
		Input   task.ChildCreateInput
		Command api.ObjectRef
	}{h.ChildCreateInput, h.SessionCommandRef}
	saved, err := a.plan(ctx, scope, h.SessionCommandRef.ObjectID, auth, source, func() api.Command {
		return api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: h.SessionOwnerID, CommandID: h.SessionCommandRef.ObjectID, Method: "session.create", TargetID: h.SessionOwnerID, ExpiresAt: h.PrepareDeadline, Payload: api.Raw(interaction.CreateSessionInput{SessionID: api.NewID("session"), DefaultBranchID: api.NewID("branch"), ConfigRef: h.SessionConfigRef})}
	})
	if err != nil {
		return api.ObjectRef{}, err
	}
	r, err := a.forward(ctx, scope, saved, nil)
	if err != nil {
		return api.ObjectRef{}, err
	}
	var out interaction.SessionOutput
	if err = api.Decode(r.Output, &out); err != nil {
		return api.ObjectRef{}, err
	}
	if out.SessionRef.TenantID != scope.TenantID || out.SessionRef.OwnerID != h.SessionOwnerID || out.SessionRef.Revision == 0 {
		return api.ObjectRef{}, api.E("forbidden", "original_session_result_scope_mismatch")
	}
	return out.SessionRef, nil
}
func (*Adapter) Create(context.Context, runtime.Scope, task.Delegation, task.Allocation) (task.DelegationFact, error) {
	return task.DelegationFact{}, api.E("unsupported", "remote_delegation_not_configured")
}
func (*Adapter) Read(context.Context, runtime.Scope, task.Delegation) (task.DelegationFact, error) {
	return task.DelegationFact{}, api.E("unsupported", "remote_delegation_not_configured")
}
func (*Adapter) Control(context.Context, runtime.Scope, task.Delegation, string) error {
	return api.E("unsupported", "remote_delegation_not_configured")
}
func (*Adapter) CloseAllocation(context.Context, runtime.Scope, task.Allocation) error {
	return api.E("unsupported", "remote_allocation_not_configured")
}
func (a *Adapter) ReadAllocation(ctx context.Context, scope runtime.Scope, ref api.ObjectRef) (task.Allocation, error) {
	service, err := a.taskService()
	if err != nil {
		return task.Allocation{}, err
	}
	if err = a.checkScope(scope); err != nil || ref.OwnerID != scope.OwnerID || ref.TenantID != scope.TenantID {
		return task.Allocation{}, api.E("unsupported", "remote_allocation_not_configured")
	}
	return service.AllocationRead(ctx, a.config.Store, scope, a.config.Auth, ref.ObjectID)
}
func (a *Adapter) ReadClosure(ctx context.Context, scope runtime.Scope, ref api.ObjectRef) (api.AllocationClosure, error) {
	service, err := a.taskService()
	if err != nil {
		return api.AllocationClosure{}, err
	}
	if err = a.checkScope(scope); err != nil {
		return api.AllocationClosure{}, err
	}
	return service.AllocationClosureRead(ctx, a.config.Store, scope, a.config.Auth, ref)
}
func (a *Adapter) Transfer(ctx context.Context, scope runtime.Scope, tr task.Transfer) error {
	if err := a.checkScope(scope); err != nil {
		return err
	}
	service, err := a.taskService()
	if err != nil {
		return err
	}
	source := struct {
		ID           string
		DelegationID string
		CommandRef   api.ObjectRef
		Input        task.ChildSendInput
		SourceRef    api.ObjectRef
		ExpiresAt    string
	}{tr.TransferID, tr.DelegationID, tr.CommandRef, tr.Input, tr.SourceSubmissionRef, tr.ExpiresAt}
	digest, err := api.Digest(source)
	if err != nil {
		return err
	}
	var saved commandIntent
	_, err = a.config.Store.Read(ctx, scope, commandIntents, tr.CommandRef.ObjectID, 0, &saved)
	if err == nil {
		if saved.SourceDigest != digest || !api.Equal(saved.Auth, tr.ActorAuth) {
			return api.E("idempotency_conflict", "original_transfer_input_changed")
		}
	} else {
		if !errors.Is(err, runtime.ErrNotFound) && !api.IsCode(err, "not_found") {
			return err
		}
		d, err := service.DelegationRead(ctx, a.config.Store, scope, a.config.Auth, tr.DelegationID)
		if err != nil {
			return err
		}
		if !d.Internal || d.ReceiverID != a.config.OwnerID || d.ChildTaskRef == nil || d.ChildTaskRef.OwnerID != scope.OwnerID || d.ChildTaskRef.TenantID != scope.TenantID {
			return api.E("unsupported", "remote_transfer_not_configured")
		}
		var method string
		var input any
		switch tr.Input.Kind {
		case "steer":
			if tr.Input.ContentRef == nil || tr.Input.ChildGoalRevision == 0 {
				return api.E("invalid_request", "original_steer_transfer_incomplete")
			}
			method = "task.steer"
			input = task.SteerInput{TaskID: d.ChildTaskRef.ObjectID, BaseGoalRevision: tr.Input.ChildGoalRevision, AmendmentRef: *tr.Input.ContentRef, SourceSubmissionRef: tr.SourceSubmissionRef, PrepareDeadline: tr.ExpiresAt}
		case "answer_request":
			if tr.Input.RequestRef == nil || tr.Input.AnswerRef == nil {
				return api.E("invalid_request", "original_answer_transfer_incomplete")
			}
			request, err := service.InputRequestRead(ctx, a.config.Store, scope, tr.ActorAuth, tr.Input.RequestRef.ObjectID, tr.Input.RequestRef.Revision)
			if err != nil {
				return err
			}
			if request.Request.TargetRef.ObjectID != d.ChildTaskRef.ObjectID || request.Request.GoalRevision == nil {
				return api.E("forbidden", "request_outside_original_child")
			}
			method = "task.input"
			input = task.InputAnswer{TaskID: d.ChildTaskRef.ObjectID, RequestRef: *tr.Input.RequestRef, GoalRevision: *request.Request.GoalRevision, AnswerRef: *tr.Input.AnswerRef}
		default:
			return api.E("unsupported", "transfer_kind_not_configured")
		}
		saved, err = a.plan(ctx, scope, tr.CommandRef.ObjectID, tr.ActorAuth, source, func() api.Command {
			return api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: scope.OwnerID, CommandID: tr.CommandRef.ObjectID, TargetID: d.ChildTaskRef.ObjectID, Method: method, ExpiresAt: tr.ExpiresAt, Payload: api.Raw(input)}
		})
		if err != nil {
			return err
		}
	}
	_, err = a.forward(ctx, scope, saved, func(tx runtime.Tx) error {
		return service.CheckTransferTx(ctx, tx, saved.Auth, tr.TransferID)
	})
	return err
}

var _ task.CollaborationPort = (*Adapter)(nil)
var _ task.CollaborationAdmission = (*Adapter)(nil)
