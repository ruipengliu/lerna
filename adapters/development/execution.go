package development

import (
	"context"
	"fmt"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/execution"
	"github.com/ruipengliu/lerna/internal/governance"
	"github.com/ruipengliu/lerna/internal/task"
	"github.com/ruipengliu/lerna/runtime"
)

type encodedIntent struct {
	AdmissionHash string                    `json:"admission_hash"`
	Domain        execution.ExecutionIntent `json:"domain"`
	Ref           api.ContentRef            `json:"ref"`
	Hash          string                    `json:"hash"`
	Command       *api.Command              `json:"command,omitempty"`
	UseProofRefs  []api.ContentRef          `json:"use_proof_refs,omitempty"`
}
type executionBridge struct{ a *App }

// PrepareDispatch 出版原惰性输入和原Use证明，尚不准入执行或签发控制窗口。
func (e executionBridge) PrepareDispatch(ctx context.Context, s runtime.Scope, i task.OperationIntent) error {
	prepared, err := e.a.prepareRemoteAgentOperationParent(ctx, s, i)
	if err != nil {
		return err
	}
	ctx = prepared
	var fixed encodedIntent
	_, er := e.a.Store.Read(ctx, s, "platform.execution_intents", i.OperationID, 0, &fixed)
	if er != nil && !api.IsCode(er, "not_found") {
		return er
	}
	if api.IsCode(er, "not_found") {
		resourcesBytes, er := e.a.Memory.Read(ctx, s, e.a.ServiceAuth, i.ResourcesRef, "execution.arguments")
		if er != nil {
			return er
		}
		var resources []api.ObjectRef
		if er = api.Decode(resourcesBytes, &resources); er != nil {
			return er
		}
		domain := execution.ExecutionIntent{OperationID: i.OperationID, TaskRef: i.TaskRef, GoalRevision: i.GoalRevision, ControlRevision: i.ControlRevision, AdmissionSourceKind: i.AdmissionSourceKind, AdmissionSourceRef: i.AdmissionSourceRef, SourcePosition: i.SourcePosition, AdmissionPurpose: i.AdmissionPurpose, CapabilityRef: i.CapabilityRef, BindingRef: i.BindingRef, InstallLockRef: i.InstallLockRef, ArgumentsRef: i.ArgumentsRef, ResourceRefs: resources, RequirementRefs: i.RequirementRefs, CostBound: i.CostBound, ExecutorID: i.ExecutorID, Deadline: i.Deadline, TaskDeadline: i.Deadline, LogicalStepKey: i.LogicalStepKey, ProcessedSourceRefs: i.ProcessedSourceRefs, DisclosedSourceRefs: i.DisclosedSourceRefs}
		body := api.Raw(domain)
		hash, er := api.Digest(domain)
		if er != nil {
			return er
		}
		ref, er := e.a.Publish(ctx, s, e.a.ServiceAuth, stableID("content", "executor-intent/"+i.OperationID), "application/vnd.harness.execution-intent+json", body, append(append([]api.ContentRef{}, i.ProcessedSourceRefs...), i.ArgumentsRef, i.ResourcesRef), []api.ContentRef{})
		if er != nil {
			return er
		}
		proofs := []api.ContentRef{}
		if i.ExecutorID == s.OwnerID {
			proofs, er = e.prepareUseProofs(ctx, s, i.UseIntentRefs, i.IntentHash)
			if er != nil {
				return er
			}
		}
		fixed = encodedIntent{AdmissionHash: i.IntentHash, Domain: domain, Ref: ref, Hash: hash, UseProofRefs: proofs}
		status, er := e.a.Store.Within(ctx, s, []string{"platform"}, func(tx runtime.Tx) error {
			var old encodedIntent
			if _, er := tx.Get(ctx, "platform.execution_intents", i.OperationID, &old); er == nil {
				if old.AdmissionHash != i.IntentHash {
					return api.E("idempotency_conflict", "admission_changed")
				}
				fixed = old
				return nil
			} else if !api.IsCode(er, "not_found") {
				return er
			}
			return tx.Create(ctx, "platform.execution_intents", i.OperationID, i.TaskRef.ObjectID, fixed)
		})
		if status == runtime.CommitUnknown {
			return runtime.ErrCommitUnknown
		}
		if er != nil {
			return er
		}
	}
	if fixed.AdmissionHash != i.IntentHash {
		return api.E("idempotency_conflict", "admission_changed")
	}
	if i.ExecutorID != s.OwnerID {
		if err := e.a.freezeRemoteSubmitter(ctx, s, i); err != nil {
			return err
		}
		if er = e.prepareRemoteDispatch(ctx, s, i, fixed); er != nil {
			return er
		}
		// Task 在原五秒窗口前以 service holder 核 task.dispatch/cloud。
		// 设备缓存准备的 execution_* 证明不能替代此准确用途；旧 bundle 同样
		// 在每个新 Job 取得当前证明，原 Task/控制/Claim 事务仍随后完整重核。
		ctx, er = e.a.prepareForeignSources(ctx, s, e.a.ServiceAuth, i.ProcessedSourceRefs, "task.dispatch", "cloud")
		if er != nil {
			return fmt.Errorf("prepare original task.dispatch/cloud sources before control window: %w", er)
		}
		flow, ok := ctx.Value(foreignFlowKey{}).(runtime.Flow)
		if !ok || flow.Kind != "job" || flow.Scope != s || flow.Work == nil || flow.Work.Job.Kind != task.JobDispatchOperation || flow.Work.Job.SourceRef.ObjectID != i.OperationID {
			return api.E("forbidden", "original_remote_worker_claim_required")
		}
		return e.a.Store.CheckClaim(ctx, s, flow.Work.Claim)
	}
	return nil
}

func (e executionBridge) prepareUseProofs(ctx context.Context, s runtime.Scope, uses []api.ObjectRef, hash string) ([]api.ContentRef, error) {
	proofs := make([]api.ContentRef, 0, len(uses))
	for _, ref := range uses {
		raw, err := e.a.query(ctx, "grant.use.get", ref.ObjectID, governance.IDInput{ID: ref.ObjectID})
		if err != nil {
			return nil, err
		}
		var use governance.UseReceipt
		if err = api.Decode(raw, &use); err != nil {
			return nil, err
		}
		if use.Decision != "allowed" || use.IntentHash != hash {
			return nil, api.E("forbidden", "use_binding_mismatch")
		}
		proof, err := e.a.Publish(ctx, s, e.a.ServiceAuth, stableID("content", "use-proof/"+ref.ObjectID), "application/jose", []byte(use.Proof), []api.ContentRef{}, []api.ContentRef{})
		if err != nil {
			return nil, err
		}
		proofs = append(proofs, proof)
	}
	return proofs, nil
}

func (e executionBridge) Dispatch(ctx context.Context, s runtime.Scope, i task.OperationIntent, window api.ControlSnapshot) error {
	// 旧宿主可缺少准备接口；新装配在短窗口签发前已完成该步骤。
	if err := e.PrepareDispatch(ctx, s, i); err != nil {
		return err
	}
	var fixed encodedIntent
	if _, err := e.a.Store.Read(ctx, s, "platform.execution_intents", i.OperationID, 0, &fixed); err != nil {
		return err
	}
	if fixed.AdmissionHash != i.IntentHash {
		return api.E("idempotency_conflict", "admission_changed")
	}
	if fixed.Command == nil {
		input := execution.InvokeInput{OperationID: i.OperationID, TaskRef: i.TaskRef, GoalRevision: i.GoalRevision, ControlRevision: i.ControlRevision, CapabilityRef: i.CapabilityRef, BindingRef: i.BindingRef, IntentRef: fixed.Ref, IntentHash: fixed.Hash, UseRefs: i.UseIntentRefs, Deadline: i.Deadline, ControlSnapshot: window, ReservationRef: i.ReservationRef}
		command := api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: i.ExecutorID, CommandID: i.CommandID, TargetID: i.OperationID, Method: "execution.invoke", ExpiresAt: i.Deadline, Payload: api.Raw(input)}
		status, err := e.a.Store.Within(ctx, s, []string{"platform"}, func(tx runtime.Tx) error {
			var old encodedIntent
			revision, err := tx.Get(ctx, "platform.execution_intents", i.OperationID, &old)
			if err != nil {
				return err
			}
			if old.AdmissionHash != i.IntentHash {
				return api.E("idempotency_conflict", "admission_changed")
			}
			if old.Command != nil {
				fixed = old
				return nil
			}
			old.Command = &command
			if err = tx.Put(ctx, "platform.execution_intents", i.OperationID, revision, old); err != nil {
				return err
			}
			fixed = old
			return nil
		})
		if status == runtime.CommitUnknown {
			return runtime.ErrCommitUnknown
		}
		if err != nil {
			return err
		}
	}
	// 不论新的调用携带什么窗口，都只投递首次耐久冻结的原命令。
	if i.ExecutorID != s.OwnerID {
		return e.dispatchRemote(ctx, s, i, fixed)
	}
	receipt, err := e.a.Dispatcher.Command(ctx, e.a.ServiceAuth, api.Raw(*fixed.Command))
	if err != nil {
		return err
	}
	if receipt.Error != nil {
		return receipt.Error
	}
	return nil
}
func (e executionBridge) Read(ctx context.Context, s runtime.Scope, ref api.ObjectRef) (api.Operation, error) {
	if ref.OwnerID != s.OwnerID {
		return e.readRemote(ctx, s, ref)
	}
	raw, er := e.a.query(ctx, "execution.get", ref.ObjectID, execution.OperationIDInput{OperationID: ref.ObjectID})
	if er != nil {
		return api.Operation{}, er
	}
	var v execution.OperationView
	if er = api.Decode(raw, &v); er != nil {
		return api.Operation{}, er
	}
	return v.Operation, nil
}
func (e executionBridge) Control(ctx context.Context, s runtime.Scope, owner string, c api.ControlSnapshot) error {
	command := api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: owner, CommandID: stableID("command", "control/"+c.WindowID), TargetID: c.TaskID, Method: "execution.control", ExpiresAt: c.StartBefore, Payload: api.Raw(execution.ControlInput{TaskRef: s.Ref(c.TaskID, 1), Snapshot: c})}
	if owner != s.OwnerID {
		return e.controlRemote(ctx, s, owner, command, c)
	}
	r, er := e.a.Dispatcher.Command(ctx, e.a.ServiceAuth, api.Raw(command))
	if er != nil {
		return er
	}
	if r.Error != nil {
		return r.Error
	}
	return nil
}
func (e executionBridge) Usage(ctx context.Context, s runtime.Scope, ref api.ObjectRef) (api.UsageSnapshot, error) {
	if ref.OwnerID != s.OwnerID {
		return e.remoteBillingUsage(ctx, s, ref)
	}
	u, err := e.rawUsage(ctx, s, ref)
	if err != nil {
		return u, err
	}
	intent, err := e.a.Task.ReadOperationIntent(ctx, e.a.Store, s, e.a.ServiceAuth, ref.ObjectID)
	if err != nil {
		return u, err
	}
	return u, e.a.settleUses(ctx, s, intent.UseIntentRefs, u)
}
func (e executionBridge) rawUsage(ctx context.Context, s runtime.Scope, ref api.ObjectRef) (api.UsageSnapshot, error) {
	if ref.OwnerID != s.OwnerID {
		return e.remoteUsage(ctx, s, ref)
	}
	raw, er := e.a.query(ctx, "execution.usage.get", ref.ObjectID, execution.OperationIDInput{OperationID: ref.ObjectID})
	if er != nil {
		return api.UsageSnapshot{}, er
	}
	var u api.UsageSnapshot
	er = api.Decode(raw, &u)
	return u, er
}
func (a *App) query(ctx context.Context, method, target string, payload any) ([]byte, error) {
	return a.queryAs(ctx, a.ServiceAuth, method, target, payload)
}
func (a *App) queryAs(ctx context.Context, auth runtime.Auth, method, target string, payload any) ([]byte, error) {
	return a.Dispatcher.Query(ctx, auth, api.Raw(api.Query{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: a.Scope.OwnerID, QueryID: api.NewID("query"), TargetID: target, Method: method, Payload: api.Raw(payload)}))
}

type executionAuthority struct{ a *App }

func (e executionAuthority) VerifyControl(ctx context.Context, tx runtime.Tx, auth runtime.Auth, c api.ControlSnapshot) error {
	return e.a.verifyControlTx(ctx, tx, auth, c)
}
func (e executionAuthority) PrepareStart(ctx context.Context, s runtime.Scope, r execution.StartRequest) (execution.PreparedStart, error) {
	var fixed encodedIntent
	if _, er := e.a.Store.Read(ctx, s, "platform.execution_intents", r.Invoke.OperationID, 0, &fixed); er != nil {
		return execution.PreparedStart{}, er
	}
	if fixed.Hash != r.Invoke.IntentHash || !api.Equal(fixed.Domain, r.Intent) {
		return execution.PreparedStart{}, api.E("forbidden", "intent_encoding_changed")
	}
	proofs := fixed.UseProofRefs
	if len(proofs) == 0 {
		// 原内联记录在旧版中未保存准备证明；仍沿原Use/原Content身份恢复。
		var er error
		proofs, er = (executionBridge{e.a}).prepareUseProofs(ctx, s, r.Invoke.UseRefs, fixed.AdmissionHash)
		if er != nil {
			return execution.PreparedStart{}, er
		}
	}
	if len(proofs) != 1 {
		return execution.PreparedStart{}, api.E("unsupported", "exact_single_use_proof_required")
	}
	return execution.PreparedStart{OperationID: r.Invoke.OperationID, IntentHash: r.Invoke.IntentHash, Recipient: s.OwnerID, UseRefs: r.Invoke.UseRefs, ApprovalRefs: []api.ObjectRef{}, AuthorityRevision: 1, StartBefore: r.ControlWindow.StartBefore, ProofRef: proofs[0]}, nil
}
func (e executionAuthority) VerifyStart(ctx context.Context, tx runtime.Tx, r execution.StartRequest, p execution.PreparedStart) (execution.StartPermit, error) {
	original, er := e.a.Task.OperationIntentTx(ctx, tx, r.Invoke.OperationID)
	if er != nil {
		return execution.StartPermit{}, er
	}
	// 原准备许可不替代本次开始的当前 Task/父范围门禁。
	// 沿已锁原根、预算先核材料和父门，再取得平台凭据锁。
	if er = e.a.Task.CheckTaskCurrentTx(ctx, tx, r.Auth, original.TaskRef.ObjectID, true); er != nil {
		return execution.StartPermit{}, er
	}
	if er := currentCredentialTx(ctx, tx, r.Auth); er != nil {
		return execution.StartPermit{}, er
	}
	var fixed encodedIntent
	if _, er = tx.Get(ctx, "platform.execution_intents", r.Invoke.OperationID, &fixed); er != nil {
		return execution.StartPermit{}, er
	}
	if fixed.AdmissionHash != original.IntentHash || fixed.Hash != r.Invoke.IntentHash || !api.Equal(fixed.Domain, r.Intent) || !api.Equal(original.UseIntentRefs, r.Invoke.UseRefs) || !api.Equal(original.CapabilityRef, r.Invoke.CapabilityRef) || !api.Equal(original.BindingRef, r.Invoke.BindingRef) {
		return execution.StartPermit{}, api.E("forbidden", "original_admission_mismatch")
	}
	if _, er = e.a.readActionAdmissionTx(ctx, tx, original); er != nil {
		return execution.StartPermit{}, er
	}
	if er = e.VerifyControl(ctx, tx, r.Auth, r.ControlWindow); er != nil {
		return execution.StartPermit{}, er
	}
	now, er := tx.Now(ctx)
	if er != nil {
		return execution.StartPermit{}, er
	}
	if len(r.Invoke.UseRefs) != 1 {
		return execution.StartPermit{}, api.E("forbidden", "authorization_missing")
	}
	for _, ref := range r.Invoke.UseRefs {
		if er = e.a.Governance.CheckUseTx(ctx, tx, r.Auth, ref, tx.Scope().Ref(r.Invoke.OperationID, 1), original.IntentHash, now); er != nil {
			return execution.StartPermit{}, er
		}
	}
	return execution.StartPermit{StartBefore: p.StartBefore, ProofRefs: []api.ContentRef{p.ProofRef}}, nil
}

type actionAuthorization struct{ a *App }

func (a actionAuthorization) AuthorizeAction(ctx context.Context, tx runtime.Tx, auth runtime.Auth, i task.OperationIntent) error {
	if len(i.UseIntentRefs) != 1 {
		return api.E("forbidden", "operation_authority_missing")
	}
	admission, er := a.a.readActionAdmissionTx(ctx, tx, i)
	if er != nil {
		return er
	}
	if er := currentCredentialTx(ctx, tx, auth); er != nil {
		return er
	}
	if er := a.a.Knowledge.CheckActionTx(ctx, tx, auth, i); er != nil {
		return er
	}
	if er := a.a.checkRemoteActionTx(ctx, tx, auth, i, admission); er != nil {
		return er
	}
	if i.ExecutorID != tx.Scope().OwnerID {
		return a.a.authorizeRemoteActionTx(ctx, tx, auth, i, admission)
	}
	use, er := a.a.Governance.UseTx(ctx, tx, auth, governance.UseRequest{UseID: i.UseIntentRefs[0].ObjectID, SubjectRef: auth.Ref(tx.Scope().OwnerID), TargetRef: tx.Scope().Ref(i.OperationID, 1), TargetKind: "operation", IntentHash: i.IntentHash, GrantRefs: []api.ObjectRef{admission.Descriptor.GrantRef}, RequestedUnits: i.CostBound, Resources: admission.Resources, Actions: admission.Actions, Recipient: admission.Descriptor.Recipient, Location: admission.Descriptor.Location, Purposes: []string{i.AdmissionPurpose}, StartBefore: i.Deadline})
	if er != nil {
		return er
	}
	if use.Decision != "allowed" {
		return api.E("forbidden", "operation_grant_denied")
	}
	return nil
}
