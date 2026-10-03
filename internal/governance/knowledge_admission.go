package governance

import (
	"context"
	"sort"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/runtime"
)

// 准确数据制品锁；不与 installations 的程序准备、实例和隔离资格混用。
type knowledgeDataLock struct {
	ID             string             `json:"id"`
	Revision       uint64             `json:"revision"`
	InstallLockRef api.ComponentRef   `json:"install_lock_ref"`
	Kind           string             `json:"kind"`
	OriginRef      *api.ObjectRef     `json:"origin_ref,omitempty"`
	SourceRefs     []api.ContentRef   `json:"source_refs"`
	ParentRefs     []api.ComponentRef `json:"parent_refs"`
}
type KnowledgeHolder struct {
	ID             string           `json:"id"`
	Revision       uint64           `json:"revision"`
	InstallLockRef api.ComponentRef `json:"install_lock_ref"`
	ConsumerRef    api.ObjectRef    `json:"consumer_ref"`
	ConsumerKind   string           `json:"consumer_kind"`
	SnapshotRef    api.ContentRef   `json:"snapshot_ref"`
	State          string           `json:"state"`
}

func contentIn(ref api.ContentRef, refs []api.ContentRef) bool {
	for _, candidate := range refs {
		if api.Equal(candidate, ref) {
			return true
		}
	}
	return false
}
func selectionPrincipal(scope runtime.Scope, auth runtime.Auth, selected KnowledgeSelection) error {
	if auth.TenantID != scope.TenantID || selected.SubjectRef.TenantID != scope.TenantID || selected.SubjectRef.OwnerID != scope.OwnerID || !api.ValidID(auth.SubjectID) || auth.CredentialGeneration == 0 {
		return api.E("forbidden", "knowledge_consumer_scope_mismatch")
	}
	if !api.Equal(auth.Ref(scope.OwnerID), selected.SubjectRef) && !auth.HasRole("service") {
		return api.E("forbidden", "knowledge_consumer_principal_changed")
	}
	return nil
}
func (s *Service) currentSelectionHeadsTx(ctx context.Context, tx runtime.Tx, selected KnowledgeSelection) ([]knowledgeDataLock, error) {
	if err := validateKnowledgeRequest(tx.Scope(), selected.Request); err != nil {
		return nil, err
	}
	if len(selected.SkillRecordRefs) != len(selected.Request.SkillRefs) || len(selected.SourceRefs) > 89 {
		return nil, api.E("invalid_state", "knowledge_selection_closure_invalid")
	}
	locks := []knowledgeDataLock{}
	ordered := append([]api.ObjectRef{}, selected.SkillRecordRefs...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].ObjectID < ordered[j].ObjectID })
	for _, ref := range ordered {
		if err := ownerRef(tx.Scope(), ref); err != nil {
			return nil, err
		}
		var current SkillRecord
		rev, err := tx.Get(ctx, ns("skills"), ref.ObjectID, &current)
		if err != nil {
			return nil, err
		}
		if current.State != "active" {
			return nil, api.E("invalid_state", "selected_skill_closed")
		}
		if rev != ref.Revision || !hasComponent(selected.Request.SkillRefs, current.Definition.SkillRef) || !hasComponent(selected.KnowledgeLockRefs, current.InstallLockRef) {
			return nil, api.E("revision_conflict", "selected_skill_changed")
		}
		locks = append(locks, knowledgeDataLock{ID: componentKey(current.InstallLockRef), Revision: 1, InstallLockRef: current.InstallLockRef, Kind: "skill_data", OriginRef: new(tx.Scope().Ref(current.ID, 1)), SourceRefs: skillContents(current.Definition), ParentRefs: []api.ComponentRef{}})
	}
	if selected.Request.AgentConfigRef != nil {
		if selected.AgentRecordRef == nil {
			return nil, api.E("invalid_state", "selected_agent_reference_missing")
		}
		if err := ownerRef(tx.Scope(), *selected.AgentRecordRef); err != nil {
			return nil, err
		}
		var current AgentConfigRecord
		rev, err := tx.Get(ctx, ns("agent_configs"), selected.AgentRecordRef.ObjectID, &current)
		if err != nil {
			return nil, err
		}
		if current.State != "active" {
			return nil, api.E("invalid_state", "selected_agent_config_closed")
		}
		if rev != selected.AgentRecordRef.Revision || !api.Equal(current.Definition.AgentConfigRef, *selected.Request.AgentConfigRef) || !hasComponent(selected.KnowledgeLockRefs, current.InstallLockRef) {
			return nil, api.E("revision_conflict", "selected_agent_config_changed")
		}
		locks = append(locks, knowledgeDataLock{ID: componentKey(current.InstallLockRef), Revision: 1, InstallLockRef: current.InstallLockRef, Kind: "agent_config_data", OriginRef: new(tx.Scope().Ref(current.ID, 1)), SourceRefs: agentContents(current.Definition), ParentRefs: []api.ComponentRef{}})
	} else if selected.AgentRecordRef != nil {
		return nil, api.E("invalid_state", "unexpected_selected_agent")
	}
	if len(locks) != len(selected.KnowledgeLockRefs) || !api.Equal(knowledgeSnapshotLock(selected.Request, selected.KnowledgeLockRefs), selected.InstallLockRef) {
		return nil, api.E("invalid_state", "knowledge_install_closure_invalid")
	}
	return locks, nil
}
func validateKnowledgeAdmission(scope runtime.Scope, in KnowledgeAdmission) error {
	if !api.ValidID(in.DecisionID) {
		return api.E("invalid_request", "knowledge_decision_id_invalid")
	}
	for _, ref := range []api.ContentRef{in.SnapshotRef, in.PacketRef} {
		if err := api.ValidateRecord("ContentRef", ref); err != nil {
			return err
		}
		if ref.TenantID != scope.TenantID || ref.OwnerID != scope.OwnerID {
			return api.E("forbidden", "knowledge_admission_content_scope_mismatch")
		}
	}
	if in.PacketRef.Hash != in.Selection.PacketHash || in.PacketRef.ByteLength != in.Selection.PacketByteLength {
		return api.E("invalid_request", "knowledge_packet_binding_mismatch")
	}
	bytes := api.Raw(in.Snapshot)
	if len(bytes) > api.MaxJSONBytes || api.Hash(bytes) != in.SnapshotRef.Hash || uint64(len(bytes)) != in.SnapshotRef.ByteLength {
		return api.E("invalid_request", "knowledge_snapshot_exact_bytes_mismatch")
	}
	if in.Snapshot.SnapshotID != in.Selection.Request.SnapshotID || in.Snapshot.Revision != 1 || !api.Equal(in.Snapshot.TaskRef, in.Selection.Request.TaskRef) || !api.Equal(in.Snapshot.ModelProfileRef, in.Selection.Request.BrainRef) || !api.Equal(in.Snapshot.InstallLockRef, in.Selection.InstallLockRef) || !api.Equal(in.Snapshot.CapabilityRefs, in.Selection.EffectiveCapabilityRefs) || len(in.Snapshot.BindingRefs) != len(in.Snapshot.CapabilityRefs) || in.Snapshot.ReservedOutputTokens > in.Selection.EffectiveControls.MaxOutputTokens {
		return api.E("invalid_request", "knowledge_snapshot_selection_mismatch")
	}
	for _, ref := range append(append([]api.ContentRef{}, in.Selection.SourceRefs...), in.PacketRef) {
		if !contentIn(ref, in.Snapshot.MaterialRefs) || !contentIn(ref, in.Snapshot.ProcessedSources) {
			return api.E("forbidden", "knowledge_sources_not_in_snapshot")
		}
	}
	return nil
}

// StageSelectionTx 只可由宿主在原 Task/预算之后调用；不 IO、不 Raise Job。
// LoadKnowledge 的准确 bytes 选择是永久同库候选，接纳不能自报另一份 Packet。
func (s *Service) StageSelectionTx(ctx context.Context, tx runtime.Tx, auth runtime.Auth, in KnowledgeAdmission) (KnowledgeCommit, error) {
	if err := validateKnowledgeAdmission(tx.Scope(), in); err != nil {
		return KnowledgeCommit{}, err
	}
	var selected KnowledgeSelection
	if err := tx.GetVersion(ctx, ns("knowledge_candidates"), in.Selection.ID, 1, &selected); err != nil {
		return KnowledgeCommit{}, err
	}
	if !api.Equal(selected, in.Selection) {
		return KnowledgeCommit{}, api.E("invalid_request", "knowledge_candidate_mismatch")
	}
	if err := selectionPrincipal(tx.Scope(), auth, selected); err != nil {
		return KnowledgeCommit{}, err
	}
	now, err := tx.Now(ctx)
	if err != nil {
		return KnowledgeCommit{}, err
	}
	if err := before(now, selected.Request.ExpiresAt); err != nil {
		return KnowledgeCommit{}, err
	}
	refs := uniqueKnowledgeSources(append(append([]api.ContentRef{}, selected.SourceRefs...), in.PacketRef, in.SnapshotRef))
	if err := s.checkKnowledgeTx(ctx, tx, auth, refs, "knowledge.consume"); err != nil {
		return KnowledgeCommit{}, err
	}
	locks, err := s.currentSelectionHeadsTx(ctx, tx, selected)
	if err != nil {
		return KnowledgeCommit{}, err
	}
	var original KnowledgeCommit
	_, err = tx.Get(ctx, ns("knowledge_selections"), selected.ID, &original)
	if err == nil {
		if !api.Equal(original.Selection, selected) || !api.Equal(original.SnapshotRef, in.SnapshotRef) || !api.Equal(original.PacketRef, in.PacketRef) || original.DecisionID != in.DecisionID {
			return KnowledgeCommit{}, api.E("idempotency_conflict", "original_knowledge_admission_changed")
		}
		return original, nil
	}
	if !errMissing(err) {
		return KnowledgeCommit{}, err
	}
	locks = append(locks, knowledgeDataLock{ID: componentKey(selected.InstallLockRef), Revision: 1, InstallLockRef: selected.InstallLockRef, Kind: "snapshot_data", SourceRefs: refs, ParentRefs: append([]api.ComponentRef{selected.Request.ParentInstallLockRef}, selected.KnowledgeLockRefs...)})
	sort.Slice(locks, func(i, j int) bool { return locks[i].ID < locks[j].ID })
	result := KnowledgeCommit{ID: selected.ID, Revision: 1, Selection: selected, SnapshotRef: in.SnapshotRef, PacketRef: in.PacketRef, DecisionID: in.DecisionID, HolderRefs: []api.ObjectRef{}}
	for _, lock := range locks {
		var current knowledgeDataLock
		_, err := tx.Get(ctx, ns("knowledge_locks"), lock.ID, &current)
		if errMissing(err) {
			if err := tx.Create(ctx, ns("knowledge_locks"), lock.ID, "", lock); err != nil {
				return KnowledgeCommit{}, err
			}
		} else if err != nil {
			return KnowledgeCommit{}, err
		} else if !api.Equal(current, lock) {
			return KnowledgeCommit{}, api.E("idempotency_conflict", "knowledge_data_lock_changed")
		}
		for _, consumer := range []struct {
			Kind string
			Ref  api.ObjectRef
		}{{"task", selected.Request.TaskRef}, {"decision", tx.Scope().Ref(in.DecisionID, 1)}} {
			id := digestID("holder", []any{lock.InstallLockRef, consumer.Kind, consumer.Ref, in.SnapshotRef})
			holder := KnowledgeHolder{ID: id, Revision: 1, InstallLockRef: lock.InstallLockRef, ConsumerRef: consumer.Ref, ConsumerKind: consumer.Kind, SnapshotRef: in.SnapshotRef, State: "held"}
			if err := tx.Create(ctx, ns("knowledge_holders"), id, result.ID, holder); err != nil {
				return KnowledgeCommit{}, err
			}
			result.HolderRefs = append(result.HolderRefs, tx.Scope().Ref(id, 1))
		}
	}
	if err := tx.Create(ctx, ns("knowledge_selections"), result.ID, in.SnapshotRef.ContentID, result); err != nil {
		return KnowledgeCommit{}, err
	}
	digest, err := api.Digest(result)
	if err != nil {
		return KnowledgeCommit{}, err
	}
	if err := tx.Bind(ctx, ns("knowledge_selections"), "snapshot/"+in.SnapshotRef.ContentID, result.ID, digest); err != nil {
		return KnowledgeCommit{}, err
	}
	if err := tx.Bind(ctx, ns("knowledge_selections"), "decision/"+in.DecisionID, result.ID, digest); err != nil {
		return KnowledgeCommit{}, err
	}
	return result, nil
}

// CheckSelectionTx 返回原上限供 Brain/行动门禁收紧；它不刷新任何字段或期限。
func (s *Service) CheckSelectionTx(ctx context.Context, tx runtime.Tx, auth runtime.Auth, snapshotRef api.ContentRef, decisionID string) (KnowledgeCommit, error) {
	if err := api.ValidateRecord("ContentRef", snapshotRef); err != nil {
		return KnowledgeCommit{}, err
	}
	if snapshotRef.TenantID != tx.Scope().TenantID || snapshotRef.OwnerID != tx.Scope().OwnerID || !api.ValidID(decisionID) {
		return KnowledgeCommit{}, api.E("forbidden", "knowledge_admission_scope_mismatch")
	}
	key, err := tx.LookupKey(ctx, ns("knowledge_selections"), "snapshot/"+snapshotRef.ContentID)
	if err != nil {
		return KnowledgeCommit{}, err
	}
	var original KnowledgeCommit
	if err := tx.GetVersion(ctx, ns("knowledge_selections"), key.ObjectID, 1, &original); err != nil {
		return KnowledgeCommit{}, err
	}
	if !api.Equal(original.SnapshotRef, snapshotRef) || original.DecisionID != decisionID {
		return KnowledgeCommit{}, api.E("forbidden", "knowledge_original_consumer_mismatch")
	}
	digest, err := api.Digest(original)
	if err != nil || digest != key.Digest {
		return KnowledgeCommit{}, api.E("invalid_state", "knowledge_admission_digest_mismatch")
	}
	if err := selectionPrincipal(tx.Scope(), auth, original.Selection); err != nil {
		return KnowledgeCommit{}, err
	}
	now, err := tx.Now(ctx)
	if err != nil {
		return KnowledgeCommit{}, err
	}
	if err := before(now, original.Selection.Request.ExpiresAt); err != nil {
		return KnowledgeCommit{}, err
	}
	refs := uniqueKnowledgeSources(append(append([]api.ContentRef{}, original.Selection.SourceRefs...), original.PacketRef, original.SnapshotRef))
	if err := s.checkKnowledgeTx(ctx, tx, auth, refs, "knowledge.consume"); err != nil {
		return KnowledgeCommit{}, err
	}
	if _, err := s.currentSelectionHeadsTx(ctx, tx, original.Selection); err != nil {
		return KnowledgeCommit{}, err
	}
	for _, ref := range original.HolderRefs {
		var holder KnowledgeHolder
		if err := tx.GetVersion(ctx, ns("knowledge_holders"), ref.ObjectID, ref.Revision, &holder); err != nil {
			return KnowledgeCommit{}, err
		}
		if holder.State != "held" || !api.Equal(holder.SnapshotRef, snapshotRef) {
			return KnowledgeCommit{}, api.E("invalid_state", "original_knowledge_holder_invalid")
		}
	}
	return original, nil
}
func (s *Service) readKnowledgeSelection(ctx context.Context, store runtime.Store, scope runtime.Scope, auth runtime.Auth, _ api.Query, in KnowledgeSelectionReference) (KnowledgeCommit, error) {
	var result KnowledgeCommit
	status, err := store.Within(ctx, scope, s.participants(), func(tx runtime.Tx) error {
		var err error
		result, err = s.CheckSelectionTx(ctx, tx, auth, in.SnapshotRef, in.DecisionID)
		return err
	})
	if status == runtime.CommitUnknown {
		return KnowledgeCommit{}, runtime.ErrCommitUnknown
	}
	return result, err
}

// FindSelectionTx 只允许原无知识 Snapshot 的兼容分支；有记录时全部当前门禁仍强核。
func (s *Service) FindSelectionTx(ctx context.Context, tx runtime.Tx, auth runtime.Auth, snapshotRef api.ContentRef, decisionID string) (KnowledgeCommit, bool, error) {
	if err := api.ValidateRecord("ContentRef", snapshotRef); err != nil {
		return KnowledgeCommit{}, false, err
	}
	if snapshotRef.TenantID != tx.Scope().TenantID || snapshotRef.OwnerID != tx.Scope().OwnerID || !api.ValidID(decisionID) {
		return KnowledgeCommit{}, false, api.E("forbidden", "knowledge_admission_scope_mismatch")
	}
	if _, err := tx.LookupKey(ctx, ns("knowledge_selections"), "snapshot/"+snapshotRef.ContentID); err != nil {
		if err == runtime.ErrNotFound {
			return KnowledgeCommit{}, false, nil
		}
		return KnowledgeCommit{}, false, err
	}
	out, err := s.CheckSelectionTx(ctx, tx, auth, snapshotRef, decisionID)
	return out, true, err
}

// FindDecisionSelectionTx 供已持有原 Task/预算的行动准入核原 Decision 的选择。
func (s *Service) FindDecisionSelectionTx(ctx context.Context, tx runtime.Tx, auth runtime.Auth, decisionID string) (KnowledgeCommit, bool, error) {
	if !api.ValidID(decisionID) {
		return KnowledgeCommit{}, false, api.E("invalid_request", "knowledge_decision_id_invalid")
	}
	key, err := tx.LookupKey(ctx, ns("knowledge_selections"), "decision/"+decisionID)
	if err == runtime.ErrNotFound {
		return KnowledgeCommit{}, false, nil
	}
	if err != nil {
		return KnowledgeCommit{}, false, err
	}
	var original KnowledgeCommit
	if err := tx.GetVersion(ctx, ns("knowledge_selections"), key.ObjectID, 1, &original); err != nil {
		return KnowledgeCommit{}, true, err
	}
	out, err := s.CheckSelectionTx(ctx, tx, auth, original.SnapshotRef, decisionID)
	return out, true, err
}
