package development

import (
	"context"
	"fmt"
	"sort"

	"github.com/ruipengliu/lerna/adapters/executor"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/execution"
	"github.com/ruipengliu/lerna/internal/governance"
	"github.com/ruipengliu/lerna/internal/task"
	"github.com/ruipengliu/lerna/runtime"
)

// receipt 保存授权负责方返回的原 lease，不读取/改写该负责方的私有表。
type remoteAllocation struct {
	AdmissionHash string                `json:"admission_hash"`
	RouteDigest   string                `json:"route_digest"`
	Lease         governance.GrantLease `json:"lease"`
}

func remoteTarget(scope runtime.Scope, owner, id string) api.ObjectRef {
	return api.ObjectRef{TenantID: scope.TenantID, OwnerID: owner, ObjectID: id, Revision: 1}
}

func (a *App) authorizeRemoteActionTx(ctx context.Context, tx runtime.Tx, auth runtime.Auth, i task.OperationIntent, admission actionAdmission) error {
	if !auth.HasRole("service") || !api.Equal(auth.Ref(tx.Scope().OwnerID), a.ServiceAuth.Ref(tx.Scope().OwnerID)) {
		return api.E("forbidden", "remote_authority_identity_required")
	}
	if err := currentCredentialTx(ctx, tx, a.ServiceAuth); err != nil {
		return err
	}
	d := admission.Descriptor
	route, err := a.remoteExecutors.binding(i.BindingRef, i.CapabilityRef, i.InstallLockRef)
	if err != nil {
		return err
	}
	if d.ExecutorID != i.ExecutorID || d.RemoteConfigHash != route.Digest || d.Recipient != i.ExecutorID || d.Location != "device" || !api.Equal(admission.Prepared, i.PreparedAction) {
		return api.E("forbidden", "original_remote_admission_mismatch")
	}
	var original remoteAllocation
	if _, err = tx.Get(ctx, "platform.remote_allocations", i.OperationID, &original); err == nil {
		if original.AdmissionHash != i.IntentHash || original.RouteDigest != route.Digest || original.Lease.LeaseID != i.UseIntentRefs[0].ObjectID {
			return api.E("idempotency_conflict", "original_remote_allocation_changed")
		}
		_, err = a.Governance.CheckUseHeadsTx(ctx, tx, auth, i.UseIntentRefs[0], remoteTarget(tx.Scope(), i.ExecutorID, i.OperationID), i.IntentHash)
		return err
	} else if !api.IsCode(err, "not_found") {
		return err
	}
	lease, err := a.Governance.AllocateLeaseTx(ctx, tx, a.ServiceAuth, governance.LeaseAllocate{LeaseID: i.UseIntentRefs[0].ObjectID, EndpointID: i.ExecutorID, InstanceID: route.Config.InstanceID, Limits: i.CostBound, ExpiresAt: i.Deadline, CostMode: "strict", Scope: governance.UseRequest{UseID: i.UseIntentRefs[0].ObjectID, SubjectRef: auth.Ref(tx.Scope().OwnerID), TargetRef: remoteTarget(tx.Scope(), i.ExecutorID, i.OperationID), TargetKind: "operation", IntentHash: i.IntentHash, GrantRefs: []api.ObjectRef{d.GrantRef}, RequestedUnits: i.CostBound, Resources: admission.Resources, Actions: admission.Actions, Recipient: i.ExecutorID, Location: "device", Purposes: []string{i.AdmissionPurpose}, StartBefore: i.Deadline}})
	if err != nil {
		return err
	}
	return tx.Create(ctx, "platform.remote_allocations", i.OperationID, i.TaskRef.ObjectID, remoteAllocation{i.IntentHash, route.Digest, lease})
}

func (e executionBridge) prepareRemoteDispatch(ctx context.Context, s runtime.Scope, i task.OperationIntent, fixed encodedIntent) error {
	route, err := e.a.remoteExecutors.binding(i.BindingRef, i.CapabilityRef, i.InstallLockRef)
	if err != nil {
		return err
	}
	var bundle executor.AdmissionBundle
	_, err = e.a.Store.Read(ctx, s, "platform.remote_bundles", i.OperationID, 0, &bundle)
	if api.IsCode(err, "not_found") {
		filePermissions, err := e.a.remoteFilePermissions(ctx, s, i)
		if err != nil {
			return err
		}
		ctx, err = e.prepareRemoteAdmissionSources(ctx, s, i, fixed, filePermissions)
		if err != nil {
			return err
		}
		parts := []string{"task", "content", "memory", "governance", "platform", executor.Namespace}
		if e.a.RemoteAgent != nil {
			parts = append(parts, "collaboration")
		}
		status, err := e.a.Store.Within(ctx, s, parts, func(tx runtime.Tx) error {
			original, err := e.a.Task.OperationIntentTx(ctx, tx, i.OperationID)
			if err != nil {
				return err
			}
			if !api.Equal(original, i) {
				return api.E("idempotency_conflict", "original_remote_intent_changed")
			}
			if err = e.a.Task.CheckTaskCurrentTx(ctx, tx, e.a.ServiceAuth, i.TaskRef.ObjectID, true); err != nil {
				return err
			}
			if err = currentCredentialTx(ctx, tx, e.a.ServiceAuth); err != nil {
				return err
			}
			// 原提交回执确定本轮实际用户；只在本库当前身份核验后冻结代次。
			user, err := e.a.remoteOriginalSubmitterTx(ctx, tx, i.TaskRef)
			if err != nil {
				return err
			}
			if _, err = tx.Get(ctx, "platform.remote_bundles", i.OperationID, &bundle); err == nil {
				return nil
			} else if !api.IsCode(err, "not_found") {
				return err
			}
			var allocation remoteAllocation
			if err = tx.GetVersion(ctx, "platform.remote_allocations", i.OperationID, 1, &allocation); err != nil {
				return err
			}
			if allocation.AdmissionHash != i.IntentHash || allocation.RouteDigest != route.Digest {
				return api.E("forbidden", "original_remote_allocation_mismatch")
			}
			contents, err := e.a.remoteInputPermissionsTx(ctx, tx, i, fixed, user, filePermissions)
			if err != nil {
				return err
			}
			if _, err = e.a.Governance.CheckUseHeadsTx(ctx, tx, e.a.ServiceAuth, i.UseIntentRefs[0], remoteTarget(s, i.ExecutorID, i.OperationID), i.IntentHash); err != nil {
				return err
			}
			bundle = executor.AdmissionBundle{BundleID: stableID("bundle", "remote/"+i.OperationID), Revision: 1, AuthorityID: s.OwnerID, EndpointID: i.ExecutorID, DeviceDatabaseID: route.Config.DatabaseID, InstanceID: route.Config.InstanceID, OriginalCommandID: i.CommandID, AdmissionHash: i.IntentHash, ExecutionHash: fixed.Hash, IntentRef: fixed.Ref, ReservationRef: i.ReservationRef, Intent: fixed.Domain, Principal: executor.PrincipalOf(e.a.ServiceAuth), LeaseRef: i.UseIntentRefs[0], UseRefs: i.UseIntentRefs, Lease: allocation.Lease, Contents: contents, IssuedAt: allocation.Lease.IssuedAt, StartBefore: allocation.Lease.ExpiresAt}
			bundle, err = executor.SealAdmissionTx(ctx, tx, executor.Proof{Keys: e.a.Keys, SigningKeyID: "development-es256"}, bundle)
			if err != nil {
				return err
			}
			if err = tx.Create(ctx, "platform.remote_bundles", i.OperationID, i.TaskRef.ObjectID, bundle); err != nil {
				return err
			}
			flow, ok := ctx.Value(foreignFlowKey{}).(runtime.Flow)
			if !ok || flow.Scope != s || flow.Kind != "job" || flow.Work == nil {
				return api.E("forbidden", "original_remote_worker_claim_required")
			}
			return tx.Guard(ctx, flow.Work.Claim)
		})
		if status == runtime.CommitUnknown {
			return runtime.ErrCommitUnknown
		}
		if err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	if bundle.AdmissionHash != i.IntentHash || bundle.ExecutionHash != fixed.Hash || bundle.DeviceDatabaseID != route.Config.DatabaseID || bundle.InstanceID != route.Config.InstanceID || !api.Equal(bundle.Intent, fixed.Domain) {
		return api.E("forbidden", "original_remote_bundle_changed")
	}
	route, err = e.a.remoteExecutors.client(ctx, i.ExecutorID)
	if err != nil {
		return err
	}
	return route.Client.Prepare(ctx, bundle, func(ctx context.Context, p executor.ContentPermission) ([]byte, error) {
		purpose := p.Purposes[0]
		// 设备缺失的原缓存需要在云端读取准确字节。Memory 同时核本方读取位置
		// 和出站位置；即使 bundle 已存在，新 Job 也须取得这两个当前证明。
		for _, location := range []string{"cloud", "device"} {
			prepared, err := e.a.prepareForeignSources(ctx, s, e.a.ServiceAuth, []api.ContentRef{p.ContentRef}, purpose, location)
			if err != nil {
				return nil, fmt.Errorf("original device cache source %s@%d purpose=%s location=%s: %w", p.ContentRef.ContentID, p.ContentRef.Version, purpose, location, err)
			}
			ctx = prepared
		}
		return e.a.Memory.ReadBytes(ctx, s, e.a.ServiceAuth, p.ContentRef, purpose, "device")
	})
}

// 原Task的主体系事实通过公开读入口取得；当前证据仍由本库身份记录核验。
func (a *App) remoteOriginalSubmitterTx(ctx context.Context, tx runtime.Tx, ref api.ObjectRef) (runtime.Auth, error) {
	// Task 的不可变原命令身份来自宿主先前公开 Task.Read，避免直接读取 Task 私有行。
	var original struct {
		SubjectID string `json:"subject_id"`
	}
	if err := tx.GetVersion(ctx, "platform.remote_submitters", ref.ObjectID, 1, &original); err != nil {
		return runtime.Auth{}, err
	}
	var auth runtime.Auth
	if original.SubjectID == a.UserAuth.SubjectID {
		auth = a.UserAuth
	} else if original.SubjectID == a.ServiceAuth.SubjectID {
		auth = a.ServiceAuth
	} else {
		return auth, api.E("forbidden", "original_task_subject_not_configured")
	}
	return auth, currentCredentialTx(ctx, tx, auth)
}

func (a *App) freezeRemoteSubmitter(ctx context.Context, s runtime.Scope, i task.OperationIntent) error {
	t, err := a.Task.Read(ctx, a.Store, s, a.ServiceAuth, i.TaskRef.ObjectID)
	if err != nil {
		return err
	}
	original, err := a.Store.LookupCommand(ctx, s, t.SubmitCommandID)
	if err != nil {
		return err
	}
	if original.Command.Method != "task.submit" || original.Command.TargetID != t.TaskID || original.PrincipalID != a.UserAuth.SubjectID && original.PrincipalID != a.ServiceAuth.SubjectID {
		return api.E("forbidden", "original_task_subject_not_configured")
	}
	value := struct {
		SubjectID string `json:"subject_id"`
	}{original.PrincipalID}
	status, err := a.Store.Within(ctx, s, []string{"platform"}, func(tx runtime.Tx) error {
		var old struct {
			SubjectID string `json:"subject_id"`
		}
		if _, err := tx.Get(ctx, "platform.remote_submitters", t.TaskID, &old); err == nil {
			if !api.Equal(old, value) {
				return api.E("idempotency_conflict", "original_task_subject_changed")
			}
			return nil
		} else if !api.IsCode(err, "not_found") {
			return err
		}
		return tx.Create(ctx, "platform.remote_submitters", t.TaskID, t.TaskID, value)
	})
	if status == runtime.CommitUnknown {
		return runtime.ErrCommitUnknown
	}
	return err
}

func (a *App) remoteInputPermissionsTx(ctx context.Context, tx runtime.Tx, i task.OperationIntent, fixed encodedIntent, user runtime.Auth, filePermissions []remoteSourcePermission) ([]executor.ContentPermission, error) {
	queue := remotePermissionRoots(i, fixed, filePermissions)
	result := []executor.ContentPermission{}
	seen := map[api.ContentRef]int{}
	var bytes uint64
	for len(queue) > 0 {
		next := queue[0]
		queue = queue[1:]
		at, existing := seen[next.ref]
		if existing && containsString(result[at].Purposes, next.purpose) {
			continue
		}
		if len(queue) > 256 || (!existing && (len(result) >= 128 || next.ref.ByteLength > executor.MaxContentBytes || bytes+next.ref.ByteLength > 32<<20)) {
			return nil, api.E("overloaded", "remote_source_closure_limit")
		}
		service, err := a.Memory.SourcePolicySnapshotTx(ctx, tx, a.ServiceAuth, next.ref, next.purpose, "device")
		if err != nil {
			return nil, fmt.Errorf("original remote source ref=%s/%s@%d purpose=%s location=device subject=%s: %w", next.ref.OwnerID, next.ref.ContentID, next.ref.Version, next.purpose, a.ServiceAuth.SubjectID, err)
		}
		principal, err := a.Memory.SourcePolicySnapshotTx(ctx, tx, user, next.ref, next.purpose, "device")
		if err != nil {
			return nil, fmt.Errorf("original remote source ref=%s/%s@%d purpose=%s location=device subject=%s: %w", next.ref.OwnerID, next.ref.ContentID, next.ref.Version, next.purpose, user.SubjectID, err)
		}
		if !api.Equal(service.Policy, principal.Policy) || service.RetainUntil != principal.RetainUntil || service.ControlRevision != principal.ControlRevision {
			return nil, api.E("revision_conflict", "original_source_policy_changed")
		}
		v, err := a.Memory.CheckContentTx(ctx, tx, a.ServiceAuth, next.ref, next.purpose, "device", true)
		if err != nil {
			return nil, err
		}
		subjects := append([]api.ObjectRef{}, service.SubjectRefs...)
		for _, ref := range principal.SubjectRefs {
			found := false
			for _, old := range subjects {
				found = found || api.Equal(old, ref)
			}
			if !found {
				subjects = append(subjects, ref)
			}
		}
		policy := service.Policy
		if existing {
			old := &result[at]
			if old.SourcePolicy == nil || !api.Equal(*old.SourcePolicy, policy) || old.RetainUntil != service.RetainUntil || !api.Equal(old.SubjectRefs, subjects) || !api.Equal(old.ProcessedSources, v.ProcessedSources) || !api.Equal(old.DisclosedSources, v.DisclosedSources) {
				return nil, api.E("revision_conflict", "original_source_policy_changed")
			}
			// 同一正文的新用途也先核全部来源/主体，不能因前一用途已出现就追加。
			old.Purposes = append(old.Purposes, next.purpose)
			continue
		}
		seen[next.ref] = len(result)
		bytes += next.ref.ByteLength
		result = append(result, executor.ContentPermission{ContentRef: next.ref, Purposes: []string{next.purpose}, ProcessedSources: v.ProcessedSources, DisclosedSources: v.DisclosedSources, RetainUntil: service.RetainUntil, SourcePolicy: &policy, SubjectRefs: subjects})
		for _, ref := range uniqueSources(append(append([]api.ContentRef{}, v.ProcessedSources...), v.DisclosedSources...)) {
			queue = append(queue, remoteSourcePermission{ref, "execution_arguments"})
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].ContentRef.OwnerID+result[i].ContentRef.ContentID < result[j].ContentRef.OwnerID+result[j].ContentRef.ContentID
	})
	return result, nil
}

func (a *App) remoteControlDelivery(ctx context.Context, s runtime.Scope, c api.ControlSnapshot) (executor.ControlDelivery, error) {
	var proof sealedProof
	if _, err := a.Store.Read(ctx, s, "platform.proofs", c.ProofRef.ContentID, 0, &proof); err != nil {
		return executor.ControlDelivery{}, err
	}
	unsigned := c
	unsigned.ProofRef = api.ContentRef{}
	if proof.Control == nil || !api.Equal(*proof.Control, unsigned) || !api.Equal(proof.Ref, c.ProofRef) || api.Hash([]byte(proof.Compact)) != c.ProofRef.Hash {
		return executor.ControlDelivery{}, api.E("forbidden", "original_remote_control_mismatch")
	}
	return executor.ControlDelivery{Snapshot: c, Compact: proof.Compact}, nil
}

func (e executionBridge) dispatchRemote(ctx context.Context, s runtime.Scope, i task.OperationIntent, fixed encodedIntent) error {
	if fixed.Command == nil {
		return api.E("dependency_unavailable", "original_remote_command_missing")
	}
	var input execution.InvokeInput
	if err := api.Decode(fixed.Command.Payload, &input); err != nil {
		return err
	}
	delivery, err := e.a.remoteControlDelivery(ctx, s, input.ControlSnapshot)
	if err != nil {
		return err
	}
	route, err := e.a.remoteExecutors.client(ctx, i.ExecutorID)
	if err != nil {
		return err
	}
	ctx, err = e.checkRemoteDispatch(ctx, s, i, fixed, input)
	if err != nil {
		return err
	}
	receipt, err := route.Client.Dispatch(ctx, *fixed.Command, delivery)
	if err != nil {
		return err
	}
	if receipt.Error != nil {
		return receipt.Error
	}
	if receipt.Stage != "applied" && receipt.Stage != "accepted" {
		return api.E("dependency_unavailable", "original_remote_invoke_pending")
	}
	return nil
}

// 准备缓存与已签原lease不能替代当前云端开始门禁。最后一次实际来源刷新后，
// 在短Tx按Task/预算→Content→Grant→Job核原scope；提交未知绝不投递invoke。
func (e executionBridge) checkRemoteDispatch(ctx context.Context, s runtime.Scope, i task.OperationIntent, fixed encodedIntent, input execution.InvokeInput) (context.Context, error) {
	var bundle executor.AdmissionBundle
	if _, err := e.a.Store.Read(ctx, s, "platform.remote_bundles", i.OperationID, 1, &bundle); err != nil {
		return ctx, err
	}
	var user runtime.Auth
	// Task 当前材料门禁也对空集合取得 Memory head；所有当前门禁所需参与者显式声明。
	parts := []string{"task", "content", "memory", "governance", "platform"}
	if e.a.RemoteAgent != nil {
		parts = append(parts, "collaboration")
	}
	metadata, err := e.a.Store.Within(ctx, s, parts, func(tx runtime.Tx) error {
		if err := e.a.Task.CheckTaskCurrentTx(ctx, tx, e.a.ServiceAuth, i.TaskRef.ObjectID, true); err != nil {
			return err
		}
		if err := currentCredentialTx(ctx, tx, e.a.ServiceAuth); err != nil {
			return err
		}
		var err error
		user, err = e.a.remoteOriginalSubmitterTx(ctx, tx, i.TaskRef)
		return err
	})
	if metadata == runtime.CommitUnknown {
		return ctx, runtime.ErrCommitUnknown
	}
	if err != nil {
		return ctx, err
	}
	type sourceGroup struct {
		auth    runtime.Auth
		purpose string
		refs    []api.ContentRef
	}
	groups := map[string]*sourceGroup{}
	for _, permission := range bundle.Contents {
		for _, purpose := range permission.Purposes {
			for _, auth := range []runtime.Auth{e.a.ServiceAuth, user} {
				key := auth.SubjectID + "/" + purpose
				group := groups[key]
				if group == nil {
					group = &sourceGroup{auth: auth, purpose: purpose}
					groups[key] = group
				}
				group.refs = append(group.refs, permission.ContentRef)
			}
		}
	}
	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		group := groups[key]
		ctx, err = e.a.prepareForeignSources(ctx, s, group.auth, uniqueSources(group.refs), group.purpose, "device")
		if err != nil {
			return ctx, err
		}
	}
	status, err := e.a.Store.Within(ctx, s, parts, func(tx runtime.Tx) error {
		original, err := e.a.Task.OperationIntentTx(ctx, tx, i.OperationID)
		if err != nil {
			return err
		}
		if !api.Equal(original, i) || bundle.AdmissionHash != i.IntentHash || bundle.ExecutionHash != fixed.Hash || !api.Equal(bundle.Intent, fixed.Domain) || !api.Equal(bundle.UseRefs, i.UseIntentRefs) {
			return api.E("forbidden", "original_remote_dispatch_changed")
		}
		if err = e.a.Task.CheckTaskCurrentTx(ctx, tx, e.a.ServiceAuth, i.TaskRef.ObjectID, true); err != nil {
			return err
		}
		current, err := e.a.Task.ReadTaskTx(ctx, tx, e.a.ServiceAuth, i.TaskRef.ObjectID)
		if err != nil {
			return err
		}
		if current.GoalRevision != i.GoalRevision || current.ControlRevision != i.ControlRevision || input.ControlSnapshot.TaskID != current.TaskID || input.ControlSnapshot.ControlRevision != current.ControlRevision {
			return api.E("revision_conflict", "original_task_control_changed")
		}
		if err = currentCredentialTx(ctx, tx, e.a.ServiceAuth); err != nil {
			return err
		}
		if err = currentCredentialTx(ctx, tx, user); err != nil {
			return err
		}
		for _, permission := range bundle.Contents {
			for _, purpose := range permission.Purposes {
				for _, auth := range []runtime.Auth{e.a.ServiceAuth, user} {
					current, err := e.a.Memory.SourcePolicySnapshotTx(ctx, tx, auth, permission.ContentRef, purpose, "device")
					if err != nil {
						return err
					}
					if permission.SourcePolicy == nil || !api.Equal(current.Policy, *permission.SourcePolicy) || current.RetainUntil != permission.RetainUntil {
						return api.E("forbidden", "original_remote_source_policy_changed")
					}
					for _, subject := range current.SubjectRefs {
						found := false
						for _, admitted := range permission.SubjectRefs {
							found = found || api.Equal(subject, admitted)
						}
						if !found {
							return api.E("forbidden", "original_remote_source_subject_changed")
						}
					}
				}
			}
		}
		if err = e.a.Knowledge.CheckActionTx(ctx, tx, e.a.ServiceAuth, i); err != nil {
			return err
		}
		if err = e.a.verifyControlTx(ctx, tx, e.a.ServiceAuth, input.ControlSnapshot); err != nil {
			return err
		}
		if _, err = e.a.Governance.CheckUseHeadsTx(ctx, tx, e.a.ServiceAuth, i.UseIntentRefs[0], remoteTarget(s, i.ExecutorID, i.OperationID), i.IntentHash); err != nil {
			return err
		}
		flow, ok := ctx.Value(foreignFlowKey{}).(runtime.Flow)
		if !ok || flow.Scope != s || flow.Kind != "job" || flow.Work == nil {
			return api.E("forbidden", "original_remote_worker_claim_required")
		}
		return tx.Guard(ctx, flow.Work.Claim)
	})
	if status == runtime.CommitUnknown {
		return ctx, runtime.ErrCommitUnknown
	}
	if err != nil {
		return ctx, err
	}
	if status != runtime.Committed {
		return ctx, api.E("dependency_unavailable", "original_remote_start_not_committed")
	}
	return ctx, nil
}
