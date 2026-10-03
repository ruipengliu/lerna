package executor

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"time"

	"github.com/ruipengliu/lerna/adapters/platform"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/execution"
	"github.com/ruipengliu/lerna/runtime"
)

type objectMapping struct {
	OperationID string `json:"operation_id"`
}

func (h *Host) register() error {
	if err := registerCommand(h.Registry, "executor.admission.install", []string{Namespace, "governance"}, h.install); err != nil {
		return err
	}
	if err := registerCommand(h.Registry, "executor.content.stage", []string{Namespace}, h.stage); err != nil {
		return err
	}
	if err := registerCommand(h.Registry, "executor.control.install", []string{Namespace}, h.installControl); err != nil {
		return err
	}
	if err := registerCommand(h.Registry, "executor.revocation.install", []string{Namespace}, h.installRevocation); err != nil {
		return err
	}
	if err := registerQuery(h.Registry, "executor.admission.get", h.admissionGet); err != nil {
		return err
	}
	if err := registerQuery(h.Registry, "executor.content.status", h.contentStatus); err != nil {
		return err
	}
	if err := registerQuery(h.Registry, "executor.content.get", h.contentGet); err != nil {
		return err
	}
	if err := registerQuery(h.Registry, "executor.lease.usage.get", h.leaseUsage); err != nil {
		return err
	}
	if err := h.registerSource(); err != nil {
		return err
	}
	return h.Registry.RegisterJob(MaterializeJob, h.materialize)
}
func registerCommand[I, O any](r *runtime.Registry, name string, parts []string, fn func(context.Context, runtime.Tx, runtime.Auth, api.Command, I) (O, error)) error {
	contract := api.Contract[I, O](name, "executor", "command", false, false)
	if strings.HasPrefix(name, "content.") {
		contract.Owner = "content"
	}
	if name == "executor.content.stage" {
		contract.InputSchema["properties"].(map[string]any)["data_base64"] = api.Schema{"type": "string", "maxLength": ChunkBytes*4/3 + 4}
	}
	return r.Register(runtime.Method{Contract: contract, Participants: parts, Apply: func(ctx context.Context, tx runtime.Tx, a runtime.Auth, c api.Command) (runtime.Outcome, error) {
		var in I
		if err := api.Decode(c.Payload, &in); err != nil {
			return runtime.Outcome{}, err
		}
		out, err := fn(ctx, tx, a, c, in)
		return runtime.Applied(out), err
	}})
}
func registerQuery[I, O any](r *runtime.Registry, name string, fn func(context.Context, runtime.Store, runtime.Scope, runtime.Auth, api.Query, I) (O, error)) error {
	contract := api.Contract[I, O](name, "executor", "query", false, false)
	if strings.HasPrefix(name, "content.") {
		contract.Owner = "content"
	}
	if name == "executor.content.get" {
		contract.OutputSchema["properties"].(map[string]any)["data_base64"] = api.Schema{"type": "string", "maxLength": 4 * ((ChunkBytes + 2) / 3)}
	}
	return r.Register(runtime.Method{Contract: contract, Query: func(ctx context.Context, st runtime.Store, s runtime.Scope, a runtime.Auth, q api.Query) (any, error) {
		var in I
		if err := api.Decode(q.Payload, &in); err != nil {
			return nil, err
		}
		return fn(ctx, st, s, a, q, in)
	}})
}
func (h *Host) peer(a runtime.Auth) error {
	if a.TenantID != h.Scope.TenantID || a.SubjectID != h.Config.Authority.OwnerID || !a.HasRole("executor_peer") {
		return api.E("forbidden", "paired_executor_peer_required")
	}
	return nil
}
func (h *Host) install(ctx context.Context, tx runtime.Tx, a runtime.Auth, c api.Command, b AdmissionBundle) (AdmissionOutput, error) {
	if err := h.peer(a); err != nil {
		return AdmissionOutput{}, err
	}
	if c.TargetID != b.Intent.OperationID {
		return AdmissionOutput{}, api.E("invalid_request", "admission_target_mismatch")
	}
	digest, err := BundleDigest(b)
	if err != nil {
		return AdmissionOutput{}, err
	}
	var old admissionRecord
	if _, err = tx.Get(ctx, Namespace+".admissions", b.Intent.OperationID, &old); err == nil {
		if old.Digest != digest || old.Bundle.Proof != b.Proof {
			return AdmissionOutput{}, api.E("idempotency_conflict", "admission_bundle_changed")
		}
		return admissionOutput(tx.Scope(), old.Bundle), nil
	} else if !api.IsCode(err, "not_found") {
		return AdmissionOutput{}, err
	}
	now, err := tx.Now(ctx)
	if err != nil {
		return AdmissionOutput{}, err
	}
	if err = h.verifyBundle(b, now, true); err != nil {
		return AdmissionOutput{}, err
	}
	if err = h.checkDenyTx(ctx, tx, b); err != nil {
		return AdmissionOutput{}, err
	}
	if err = h.Governance.InstallLeaseTx(ctx, tx, b.Lease, b.LeaseRef); err != nil {
		return AdmissionOutput{}, err
	}
	if err = tx.Create(ctx, Namespace+".admissions", b.Intent.OperationID, b.Intent.TaskRef.ObjectID, admissionRecord{b, digest}); err != nil {
		return AdmissionOutput{}, err
	}
	for ns, id := range map[string]string{"bundle_keys": b.BundleID, "command_keys": b.OriginalCommandID} {
		if err = tx.Create(ctx, Namespace+"."+ns, id, b.Intent.OperationID, objectMapping{b.Intent.OperationID}); err != nil {
			return AdmissionOutput{}, err
		}
	}
	for _, permission := range b.Contents {
		key := contentKey(permission.ContentRef)
		var old contentRecord
		if _, err = tx.Get(ctx, Namespace+".contents", key, &old); err == nil {
			if !api.Equal(old.Permission.ContentRef, permission.ContentRef) || !api.Equal(old.Permission.ProcessedSources, permission.ProcessedSources) || !api.Equal(old.Permission.DisclosedSources, permission.DisclosedSources) {
				return AdmissionOutput{}, api.E("idempotency_conflict", "original_source_metadata_changed")
			}
			originalPolicy, subjects := old.Permission.SourcePolicy, old.Permission.SubjectRefs
			if old.Published {
				originalPolicy, subjects = old.SourcePolicy, old.SourceReaders
			}
			if permission.SourcePolicy != nil && (!api.Equal(originalPolicy, permission.SourcePolicy) || !api.Equal(subjects, permission.SubjectRefs)) {
				return AdmissionOutput{}, api.E("idempotency_conflict", "original_cached_policy_changed")
			}
			continue
		} else if !api.IsCode(err, "not_found") {
			return AdmissionOutput{}, err
		}
		count := chunkCount(permission.ContentRef.ByteLength)
		if err = tx.Create(ctx, Namespace+".contents", key, b.Intent.OperationID, contentRecord{Permission: permission, BundleID: b.BundleID, Principal: b.Principal, Revision: 1, ChunkCount: count}); err != nil {
			return AdmissionOutput{}, err
		}
	}
	return admissionOutput(tx.Scope(), b), nil
}
func admissionOutput(s runtime.Scope, b AdmissionBundle) AdmissionOutput {
	return AdmissionOutput{BundleRef: api.ObjectRef{TenantID: s.TenantID, OwnerID: b.AuthorityID, ObjectID: b.BundleID, Revision: 1}, OperationRef: s.Ref(b.Intent.OperationID, 1), StartBefore: b.StartBefore}
}
func (h *Host) bundleTx(ctx context.Context, tx runtime.Tx, bundleID string) (AdmissionBundle, error) {
	var m objectMapping
	var rec admissionRecord
	if _, err := tx.Get(ctx, Namespace+".bundle_keys", bundleID, &m); err != nil {
		return rec.Bundle, err
	}
	_, err := tx.Get(ctx, Namespace+".admissions", m.OperationID, &rec)
	return rec.Bundle, err
}
func (h *Host) bundleRead(ctx context.Context, st runtime.Store, s runtime.Scope, bundleID string) (AdmissionBundle, error) {
	var m objectMapping
	var rec admissionRecord
	if _, err := st.Read(ctx, s, Namespace+".bundle_keys", bundleID, 0, &m); err != nil {
		return rec.Bundle, err
	}
	_, err := st.Read(ctx, s, Namespace+".admissions", m.OperationID, 0, &rec)
	return rec.Bundle, err
}
func permissionFor(b AdmissionBundle, r api.ContentRef) (ContentPermission, bool) {
	for _, p := range b.Contents {
		if api.Equal(p.ContentRef, r) {
			return p, true
		}
	}
	return ContentPermission{}, false
}
func chunkCount(n uint64) uint64 {
	if n == 0 {
		return 1
	}
	return (n + ChunkBytes - 1) / ChunkBytes
}
func (h *Host) stage(ctx context.Context, tx runtime.Tx, a runtime.Auth, c api.Command, in StageInput) (ContentOutput, error) {
	if err := h.peer(a); err != nil {
		return ContentOutput{}, err
	}
	if c.TargetID != in.ContentRef.ContentID {
		return ContentOutput{}, api.E("invalid_request", "content_target_mismatch")
	}
	b, err := h.bundleTx(ctx, tx, in.BundleID)
	if err != nil {
		return ContentOutput{}, err
	}
	permission, allowed := permissionFor(b, in.ContentRef)
	if !allowed {
		return ContentOutput{}, api.E("forbidden", "content_not_in_original_bundle")
	}
	now, err := tx.Now(ctx)
	if err != nil {
		return ContentOutput{}, err
	}
	if err = h.verifyBundle(b, now, true); err != nil {
		return ContentOutput{}, err
	}
	if err = h.checkDenyTx(ctx, tx, b); err != nil {
		return ContentOutput{}, err
	}
	if err = before(now, permission.RetainUntil); err != nil {
		return ContentOutput{}, err
	}
	if in.ChunkCount != chunkCount(in.ContentRef.ByteLength) || in.ChunkIndex >= in.ChunkCount || in.ChunkCount > 256 {
		return ContentOutput{}, api.E("invalid_request", "content_chunk_bounds")
	}
	raw, err := base64.StdEncoding.Strict().DecodeString(in.DataBase64)
	if err != nil || len(raw) > ChunkBytes {
		return ContentOutput{}, api.E("invalid_request", "invalid_content_chunk")
	}
	expected := uint64(ChunkBytes)
	if in.ChunkIndex == in.ChunkCount-1 {
		expected = in.ContentRef.ByteLength - in.ChunkIndex*ChunkBytes
	}
	if uint64(len(raw)) != expected {
		return ContentOutput{}, api.E("invalid_request", "content_chunk_length_mismatch")
	}
	key := contentKey(in.ContentRef)
	var record contentRecord
	if _, err = tx.Get(ctx, Namespace+".contents", key, &record); err != nil {
		return ContentOutput{}, err
	}
	chunkID := key + ":" + apiString(in.ChunkIndex)
	digest := api.Hash(raw)
	var old stagedChunk
	if _, err = tx.Get(ctx, Namespace+".chunks", chunkID, &old); err == nil {
		if old.Hash != digest || old.DataBase64 != in.DataBase64 {
			return ContentOutput{}, api.E("idempotency_conflict", "original_content_chunk_changed")
		}
	} else if api.IsCode(err, "not_found") {
		if err = tx.Create(ctx, Namespace+".chunks", chunkID, key, stagedChunk{in.ChunkIndex, digest, in.DataBase64}); err != nil {
			return ContentOutput{}, err
		}
	} else {
		return ContentOutput{}, err
	}
	chunks, err := tx.List(ctx, Namespace+".chunks", key, "", 256)
	if err != nil {
		return ContentOutput{}, err
	}
	if len(chunks) == int(in.ChunkCount) && !record.Complete {
		if _, err = tx.Raise(ctx, MaterializeJob, key, tx.Scope().Ref(key, record.Revision), now); err != nil {
			return ContentOutput{}, err
		}
	}
	return ContentOutput{ContentRef: in.ContentRef, Complete: record.Complete, StagedChunks: uint64(len(chunks))}, nil
}
func (h *Host) installControl(ctx context.Context, tx runtime.Tx, a runtime.Auth, c api.Command, in ControlDelivery) (api.ControlSnapshot, error) {
	if err := h.peer(a); err != nil {
		return api.ControlSnapshot{}, err
	}
	if c.TargetID != in.Snapshot.TaskID || in.Snapshot.OrchestratorID != h.Config.Authority.OwnerID || in.Snapshot.ProofRef.TenantID != h.Scope.TenantID {
		return api.ControlSnapshot{}, api.E("forbidden", "control_delivery_target_mismatch")
	}
	if err := verifyControl(h.Keys, in.Snapshot, in.Compact); err != nil {
		return api.ControlSnapshot{}, err
	}
	var old ControlDelivery
	if _, err := tx.Get(ctx, Namespace+".controls", in.Snapshot.WindowID, &old); err == nil {
		if !api.Equal(old, in) {
			return api.ControlSnapshot{}, api.E("idempotency_conflict", "control_window_changed")
		}
		return old.Snapshot, nil
	} else if !api.IsCode(err, "not_found") {
		return api.ControlSnapshot{}, err
	}
	if err := tx.Create(ctx, Namespace+".controls", in.Snapshot.WindowID, in.Snapshot.TaskID, in); err != nil {
		return api.ControlSnapshot{}, err
	}
	return in.Snapshot, nil
}
func revocationClaims(in Revocation, digest string) platform.ProofClaims {
	return platform.ProofClaims{TenantID: in.ObjectRef.TenantID, Issuer: in.AuthorityID, Audience: in.EndpointID, Purpose: "executor_revocation", ObjectRef: in.ObjectRef, Digest: digest, WindowID: in.ObjectRef.ObjectID, IssuedAt: in.IssuedAt, StartBefore: in.StartBefore}
}
func (h *Host) installRevocation(ctx context.Context, tx runtime.Tx, a runtime.Auth, c api.Command, in Revocation) (RevocationOutput, error) {
	if err := h.peer(a); err != nil {
		return RevocationOutput{}, err
	}
	if c.TargetID != in.ObjectRef.ObjectID || in.AuthorityID != h.Config.Authority.OwnerID || in.EndpointID != h.Scope.OwnerID || in.ObjectRef.OwnerID != in.AuthorityID || runtime.CheckRef(h.Scope, in.ObjectRef) != nil || !has([]string{"grant", "lease", "subject", "task"}, in.Kind) {
		return RevocationOutput{}, api.E("forbidden", "revocation_binding_mismatch")
	}
	unsigned := in
	unsigned.Proof = ""
	digest, err := api.Digest(unsigned)
	if err != nil {
		return RevocationOutput{}, err
	}
	if _, err = h.Keys.VerifySource(in.Proof, revocationClaims(in, digest)); err != nil {
		return RevocationOutput{}, err
	}
	key := denyKey(in.ObjectRef)
	var old denyRecord
	rev, err := tx.Get(ctx, Namespace+".denials", key, &old)
	if err == nil {
		if old.Revocation.ObjectRef.Revision > in.ObjectRef.Revision {
			return RevocationOutput{old.Revocation.ObjectRef, true}, nil
		}
		if old.Revocation.ObjectRef.Revision == in.ObjectRef.Revision {
			if digest != old.Digest {
				return RevocationOutput{}, api.E("idempotency_conflict", "revocation_fact_changed")
			}
			return RevocationOutput{in.ObjectRef, true}, nil
		}
		err = tx.Put(ctx, Namespace+".denials", key, rev, denyRecord{in, digest})
	} else if api.IsCode(err, "not_found") {
		err = tx.Create(ctx, Namespace+".denials", key, in.ObjectRef.ObjectID, denyRecord{in, digest})
	}
	return RevocationOutput{in.ObjectRef, true}, err
}
func (h *Host) admissionGet(ctx context.Context, st runtime.Store, s runtime.Scope, a runtime.Auth, q api.Query, in AdmissionID) (AdmissionView, error) {
	if err := h.peer(a); err != nil {
		return AdmissionView{}, err
	}
	b, err := h.bundleRead(ctx, st, s, in.BundleID)
	if err != nil {
		return AdmissionView{}, err
	}
	out := AdmissionView{Bundle: b, Complete: true, Missing: []api.ContentRef{}}
	for _, permission := range b.Contents {
		var record contentRecord
		if _, err = st.Read(ctx, s, Namespace+".contents", contentKey(permission.ContentRef), 0, &record); err != nil || !record.Complete {
			out.Missing = append(out.Missing, permission.ContentRef)
			out.Complete = false
		}
	}
	status, err := st.Within(ctx, s, []string{Namespace}, func(tx runtime.Tx) error {
		err := h.checkDenyTx(ctx, tx, b)
		if api.IsCode(err, "forbidden") {
			out.Denied = true
			return nil
		}
		return err
	})
	if status == runtime.CommitUnknown {
		return out, runtime.ErrCommitUnknown
	}
	return out, err
}
func (h *Host) contentStatus(ctx context.Context, st runtime.Store, s runtime.Scope, a runtime.Auth, q api.Query, in ContentID) (ContentOutput, error) {
	if err := h.peer(a); err != nil {
		return ContentOutput{}, err
	}
	var record contentRecord
	if _, err := st.Read(ctx, s, Namespace+".contents", contentKey(in.ContentRef), 0, &record); err != nil {
		return ContentOutput{}, err
	}
	if !api.Equal(record.Permission.ContentRef, in.ContentRef) {
		return ContentOutput{}, api.E("forbidden", "content_version_mismatch")
	}
	chunks, err := st.List(ctx, s, Namespace+".chunks", contentKey(in.ContentRef), "", 256)
	return ContentOutput{in.ContentRef, record.Complete, uint64(len(chunks))}, err
}

// Call 只把已签名的原 principal 用于已冻结 Operation。pair 的普通 token 不获得业务角色。
func (h *Host) Call(ctx context.Context, a runtime.Auth, kind string, payload json.RawMessage) (string, json.RawMessage, error) {
	if err := h.peer(a); err != nil {
		return "", nil, err
	}
	principal := a
	switch kind {
	case "command":
		var c api.Command
		if err := api.Decode(payload, &c); err != nil {
			return "", nil, err
		}
		if strings.HasPrefix(c.Method, "execution.") {
			if c.Method == "execution.control" || c.Method == "execution.cancel" {
				// 受信pair只拥有向其云owner保存停止信号的权力，不授予一般业务角色。
				principal = runtime.Auth{TenantID: a.TenantID, SubjectID: a.SubjectID, CredentialGeneration: a.CredentialGeneration, Roles: []string{"orchestrator"}}
			} else {
				var rec admissionRecord
				_, err := h.Store.Read(ctx, h.Scope, Namespace+".admissions", c.TargetID, 0, &rec)
				if err != nil {
					return "", nil, api.E("forbidden", "original_device_admission_required")
				}
				if err = h.verifyBundle(rec.Bundle, time.Now(), false); err != nil {
					return "", nil, err
				}
				principal = rec.Bundle.Principal.Auth()
				if c.Method == "execution.invoke" && c.CommandID != rec.Bundle.OriginalCommandID {
					return "", nil, api.E("forbidden", "original_invoke_command_required")
				}
				if c.Method == "execution.invoke" {
					var in execution.InvokeInput
					if err := api.Decode(c.Payload, &in); err != nil {
						return "", nil, err
					}
					b := rec.Bundle
					if !api.Equal(in.TaskRef, b.Intent.TaskRef) || in.GoalRevision != b.Intent.GoalRevision || in.ControlRevision != b.Intent.ControlRevision || !api.Equal(in.CapabilityRef, b.Intent.CapabilityRef) || !api.Equal(in.BindingRef, b.Intent.BindingRef) || !api.Equal(in.IntentRef, b.IntentRef) || in.IntentHash != b.ExecutionHash || !api.Equal(in.UseRefs, b.UseRefs) || !api.Equal(in.ReservationRef, b.ReservationRef) || in.Deadline != b.Intent.Deadline || c.ExpiresAt != b.Intent.Deadline {
						return "", nil, api.E("forbidden", "original_invoke_bundle_mismatch")
					}
				}
			}
		} else if !strings.HasPrefix(c.Method, "executor.") && c.Method != "content.register_copy" && c.Method != "content.release_copy" {
			return "", nil, api.E("unsupported", "device_method_not_configured")
		}
		r, err := h.Dispatcher.Command(ctx, principal, payload)
		return "receipt", api.Raw(r), err
	case "query":
		var q api.Query
		if err := api.Decode(payload, &q); err != nil {
			return "", nil, err
		}
		if q.Method == "execution.control.get" {
			// 原停止门禁可以先于任何Operation到达；TaskID不是admission键。
			// peer只读设备本库已知控制事实，领域仍不暴露云端Task。
			principal = runtime.Auth{TenantID: a.TenantID, SubjectID: a.SubjectID, CredentialGeneration: a.CredentialGeneration, Roles: []string{"orchestrator"}}
		} else if strings.HasPrefix(q.Method, "execution.") {
			var rec admissionRecord
			if _, err := h.Store.Read(ctx, h.Scope, Namespace+".admissions", q.TargetID, 0, &rec); err != nil {
				return "", nil, api.E("forbidden", "original_device_admission_required")
			}
			principal = rec.Bundle.Principal.Auth()
		} else if !strings.HasPrefix(q.Method, "executor.") && q.Method != "content.get" {
			return "", nil, api.E("unsupported", "device_method_not_configured")
		}
		out, err := h.Dispatcher.Query(ctx, principal, payload)
		return "query_result", out, err
	case "receipt_lookup":
		var in api.ReceiptLookup
		if err := api.Decode(payload, &in); err != nil {
			return "", nil, err
		}
		var m objectMapping
		if _, err := h.Store.Read(ctx, h.Scope, Namespace+".command_keys", in.CommandID, 0, &m); err == nil {
			var rec admissionRecord
			if _, err = h.Store.Read(ctx, h.Scope, Namespace+".admissions", m.OperationID, 0, &rec); err != nil {
				return "", nil, err
			}
			principal = rec.Bundle.Principal.Auth()
		} else if !api.IsCode(err, "not_found") {
			return "", nil, err
		}
		out, err := h.Dispatcher.Lookup(ctx, principal, in.CommandID)
		return "receipt", api.Raw(out), err
	default:
		return "", nil, api.E("unsupported", "device_frame_not_supported")
	}
}
