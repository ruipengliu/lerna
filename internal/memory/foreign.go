package memory

import (
	"bytes"
	"context"
	"errors"
	"time"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/runtime"
)

// 外部来源的键显式包含 owner；原同 owner 键保持历史字节不变。
func sourceKey(scope runtime.Scope, ref api.ContentRef) string {
	if ref.OwnerID == scope.OwnerID {
		return contentKey(ref)
	}
	return "foreign/" + ref.OwnerID + "/" + contentKey(ref)
}

type foreignContextKey struct{}
type foreignClaimKey struct{}

func (s *Service) foreignWithin(ctx context.Context, scope runtime.Scope, auth runtime.Auth, fn func(runtime.Tx) error) error {
	return s.authWithin(ctx, scope, auth, func(tx runtime.Tx) error {
		if err := fn(tx); err != nil {
			return err
		}
		if claim, ok := ctx.Value(foreignClaimKey{}).(api.Claim); ok {
			return tx.Guard(ctx, claim)
		}
		return nil
	})
}

func (s *Service) checkForeignIO(ctx context.Context, scope runtime.Scope) error {
	if claim, ok := ctx.Value(foreignClaimKey{}).(api.Claim); ok {
		return s.Store.CheckClaim(ctx, scope, claim)
	}
	return ctx.Err()
}

// WithForeignUses 传递本请求取得的有限原证明；Tx 仍核当前身份、门禁、签名和期限。
func WithForeignUses(ctx context.Context, uses []ForeignUse) (context.Context, error) {
	if len(uses) > 100 {
		return ctx, api.E("overloaded", "foreign_source_limit")
	}
	copyUses := make([]ForeignUse, len(uses))
	for i, use := range uses {
		if len(api.Raw(use)) > 128<<10 {
			return ctx, api.E("invalid_request", "foreign_proof_too_large")
		}
		// 深拷贝 slice，避免发送后改写原证明。
		if err := api.Decode(api.Raw(use), &copyUses[i]); err != nil {
			return ctx, err
		}
	}
	return context.WithValue(ctx, foreignContextKey{}, copyUses), nil
}

type ForeignHeldCopy struct {
	Revision       uint64            `json:"revision"`
	Reference      ForeignReference  `json:"reference"`
	Principal      runtime.Auth      `json:"principal"`
	Phase          string            `json:"phase"`
	Proof          ForeignProof      `json:"proof"`
	ObjectLocation ObjectLocation    `json:"object_location"`
	KnownDeny      bool              `json:"known_deny"`
	UseState       string            `json:"use_state"`
	CleanupState   string            `json:"cleanup_state"`
	StopReport     *ReleaseCopyInput `json:"stop_report,omitempty"`
}

type foreignSourceState struct {
	Revision         uint64         `json:"revision"`
	ContentRef       api.ContentRef `json:"content_ref"`
	SourceDatabaseID string         `json:"source_database_id"`
	ControlRevision  uint64         `json:"control_revision"`
	State            string         `json:"state"`
	ClosureKind      string         `json:"closure_kind,omitempty"`
}

func observeForeignSource(ctx context.Context, tx runtime.Tx, proof ForeignProof) error {
	key := sourceKey(tx.Scope(), proof.ContentRef)
	var old foreignSourceState
	rev, err := tx.Get(ctx, "content.foreign_sources", key, &old)
	if err != nil && !api.IsCode(err, "not_found") {
		return err
	}
	if err == nil {
		if old.ContentRef != proof.ContentRef || old.SourceDatabaseID != proof.SourceDatabaseID || old.ControlRevision > proof.ControlRevision {
			return api.E("forbidden", "foreign_source_regressed")
		}
		if old.ControlRevision == proof.ControlRevision {
			if old.State != proof.SourceState || old.ClosureKind != proof.ClosureKind {
				return api.E("forbidden", "foreign_source_changed_without_control")
			}
			return nil
		}
		if old.State == "closed" && proof.SourceState != "closed" {
			return api.E("forbidden", "closed_source_cannot_reopen")
		}
	}
	value := foreignSourceState{Revision: rev + 1, ContentRef: proof.ContentRef, SourceDatabaseID: proof.SourceDatabaseID, ControlRevision: proof.ControlRevision, State: proof.SourceState, ClosureKind: proof.ClosureKind}
	if rev == 0 {
		err = tx.Create(ctx, "content.foreign_sources", key, "", value)
	} else {
		err = tx.Put(ctx, "content.foreign_sources", key, rev, value)
	}
	if err != nil {
		return err
	}
	head, err := loadHead(ctx, tx)
	if err != nil {
		return err
	}
	head.VisibilityRevision++
	if err = saveHead(ctx, tx, head); err != nil {
		return err
	}
	if value.State == "closed" {
		now, err := tx.Now(ctx)
		if err != nil {
			return err
		}
		noticeID := semanticID("foreign_notice", key+"/"+proof.Proof)
		if err = tx.Create(ctx, "content.foreign_notices", noticeID, key, proof); err != nil {
			return err
		}
		notice := tx.Scope().Ref(noticeID, 1)
		_, err = tx.Raise(ctx, "memory.source_impact", key, notice, now)
		return err
	}
	return nil
}

func validateForeignReference(scope runtime.Scope, auth runtime.Auth, in ForeignReference) error {
	if err := checkAuth(scope, auth); err != nil {
		return err
	}
	if err := api.ValidateRecord("ContentRef", in.ContentRef); err != nil {
		return err
	}
	if in.ContentRef.TenantID != scope.TenantID || in.ContentRef.OwnerID == scope.OwnerID || in.ContentRef.ByteLength > MaxContentBytes {
		return api.E("forbidden", "foreign_reference_scope_mismatch")
	}
	if !api.ValidID(in.CopyID) || !api.ValidID(in.RegisterCommandID) || !api.ValidID(in.ReleaseCommandID) || in.RegisterCommandID == in.ReleaseCommandID {
		return api.E("invalid_request", "invalid_foreign_identity")
	}
	if err := runtime.CheckRef(scope, in.ReferenceIntentRef); err != nil {
		return err
	}
	if !api.Equal(in.HolderRef, auth.Ref(scope.OwnerID)) {
		return api.E("forbidden", "copy_holder_mismatch")
	}
	if in.Purpose == "" || len(in.Purpose) > 128 || in.Location == "" || len(in.Location) > 128 {
		return api.E("invalid_request", "foreign_use_scope_required")
	}
	_, err := api.ParseTime(in.RetainUntil)
	return err
}

func (s *Service) verifyForeignProof(ctx context.Context, tx runtime.Tx, auth runtime.Auth, in ForeignReference, proof ForeignProof) error {
	if s.Foreign == nil {
		return api.E("dependency_unavailable", "source_authority_unavailable")
	}
	validationAuth := auth
	if proof.Mode == "control" && cleanupHolder(tx.Scope(), auth, in.HolderRef) {
		validationAuth.CredentialGeneration = in.HolderRef.Revision
	}
	if err := validateForeignReference(tx.Scope(), validationAuth, in); err != nil {
		return err
	}
	if !contains([]string{"use", "control"}, proof.Mode) || len(api.Raw(proof)) > 128<<10 || proof.Proof == "" || len(proof.Proof) > 32768 || len(proof.ProcessedSources) > 100 || len(proof.DisclosedSources) > 100 || len(proof.EvidenceRefs) > 100 {
		return api.E("invalid_request", "invalid_foreign_proof")
	}
	if proof.ContentRef != in.ContentRef || !api.Equal(proof.SubjectRef, in.HolderRef) || !api.Equal(proof.HolderRef, in.HolderRef) || !api.Equal(proof.ReferenceIntentRef, in.ReferenceIntentRef) || proof.CopyID != in.CopyID || proof.Purpose != in.Purpose || proof.Location != in.Location || proof.ControlRevision == 0 || proof.SourceDatabaseID == "" || len(proof.SourceDatabaseID) > 160 {
		return api.E("forbidden", "foreign_proof_binding_mismatch")
	}
	if err := policyValid(Policy{PolicyRef: proof.PolicyRef, Values: proof.PolicyValues, Revision: 1, State: "active"}); err != nil {
		return err
	}
	for _, refs := range [][]api.ContentRef{proof.ProcessedSources, proof.DisclosedSources, proof.EvidenceRefs} {
		if err := validateSourceRefs(tx.Scope(), refs); err != nil {
			return err
		}
	}
	for _, ref := range proof.DisclosedSources {
		found := false
		for _, source := range proof.ProcessedSources {
			if ref == source {
				found = true
				break
			}
		}
		if !found {
			return api.E("invalid_request", "foreign_disclosure_not_processed")
		}
	}
	if proof.Continuous != proof.PolicyValues.Continuous || proof.IndependentDerived != proof.PolicyValues.IndependentDerived {
		return api.E("forbidden", "foreign_policy_changed")
	}
	issued, e := api.ParseTime(proof.IssuedAt)
	if e != nil {
		return e
	}
	until, e := api.ParseTime(proof.StartBefore)
	if e != nil {
		return e
	}
	retain, e := api.ParseTime(proof.RetainUntil)
	if e != nil {
		return e
	}
	original, e := api.ParseTime(in.RetainUntil)
	if e != nil {
		return e
	}
	policyUntil, e := api.ParseTime(proof.PolicyValues.RetainUntil)
	if e != nil {
		return e
	}
	if !issued.Before(until) || until.Sub(issued) > time.Minute || retain.After(original) || retain.After(policyUntil) {
		return api.E("forbidden", "foreign_proof_scope_expansion")
	}
	if !contains([]string{"published", "closed"}, proof.SourceState) || !contains([]string{"allowed", "closing", "use_stopped"}, proof.UseState) || !contains([]string{"pending", "complete", "residual", "unknown"}, proof.CleanupState) {
		return api.E("invalid_request", "invalid_foreign_state")
	}
	return s.Foreign.VerifyTx(ctx, tx, auth, in, proof)
}

func validateSourceRefs(scope runtime.Scope, refs []api.ContentRef) error {
	if len(refs) > 100 {
		return api.E("invalid_request", "source_limit_exceeded")
	}
	seen := map[string]bool{}
	for _, ref := range refs {
		if err := api.ValidateRecord("ContentRef", ref); err != nil {
			return err
		}
		if ref.TenantID != scope.TenantID {
			return api.E("forbidden", "reference_scope_mismatch")
		}
		key := sourceKey(scope, ref)
		if seen[key] {
			return api.E("invalid_request", "duplicate_source")
		}
		seen[key] = true
	}
	return nil
}

func (s *Service) observeForeignProof(ctx context.Context, scope runtime.Scope, auth runtime.Auth, in ForeignReference, proof ForeignProof, location *ObjectLocation) (ForeignHeldCopy, error) {
	var held ForeignHeldCopy
	err := s.foreignWithin(ctx, scope, auth, func(tx runtime.Tx) error {
		if err := s.verifyForeignProof(ctx, tx, auth, in, proof); err != nil {
			return err
		}
		if err := observeForeignSource(ctx, tx, proof); err != nil {
			return err
		}
		rev, err := tx.Get(ctx, "content.held_copies", in.CopyID, &held)
		if err != nil {
			return err
		}
		if !api.Equal(held.Reference, in) || !(api.Equal(held.Reference.HolderRef, auth.Ref(scope.OwnerID)) || proof.Mode == "control" && cleanupHolder(scope, auth, held.Reference.HolderRef)) {
			return api.E("idempotency_conflict", "foreign_reference_changed")
		}
		if held.Proof.ControlRevision > proof.ControlRevision || held.Proof.SourceDatabaseID != "" && held.Proof.SourceDatabaseID != proof.SourceDatabaseID || held.Proof.PolicyRef.ComponentID != "" && !api.Equal(held.Proof.PolicyRef, proof.PolicyRef) {
			return api.E("forbidden", "foreign_proof_regressed")
		}
		now, err := tx.Now(ctx)
		if err != nil {
			return err
		}
		retain, _ := api.ParseTime(proof.RetainUntil)
		if proof.SourceState != "published" || proof.UseState != "allowed" || !now.Before(retain) {
			held.KnownDeny = true
			if held.UseState == "allowed" {
				held.UseState = "closing"
			}
		}
		held.Proof = proof
		if location != nil {
			held.ObjectLocation = *location
			held.Phase = "held"
		}
		held.Revision = rev + 1
		if err = tx.Put(ctx, "content.held_copies", in.CopyID, rev, held); err != nil {
			return err
		}
		_, err = tx.Raise(ctx, "content.foreign_reconcile", in.CopyID, in.ReferenceIntentRef, now.Add(30*time.Second))
		return err
	})
	return held, err
}

// PrepareForeignUse 先保存本方原引用意图/Job，再登记源 holder、读取字节并保存 held gate。
func (s *Service) PrepareForeignUse(ctx context.Context, scope runtime.Scope, auth runtime.Auth, in ForeignReference) (ForeignUse, error) {
	if s.Foreign == nil {
		return ForeignUse{}, api.E("dependency_unavailable", "source_authority_unavailable")
	}
	if err := validateForeignReference(scope, auth, in); err != nil {
		return ForeignUse{}, err
	}
	var held ForeignHeldCopy
	err := s.foreignWithin(ctx, scope, auth, func(tx runtime.Tx) error {
		rev, err := tx.Get(ctx, "content.held_copies", in.CopyID, &held)
		if err == nil {
			if !api.Equal(held.Reference, in) || !api.Equal(held.Principal.Ref(scope.OwnerID), auth.Ref(scope.OwnerID)) {
				return api.E("idempotency_conflict", "foreign_reference_changed")
			}
			_ = rev
			return nil
		}
		if !api.IsCode(err, "not_found") {
			return err
		}
		if _, err = future(ctx, tx, in.RetainUntil); err != nil {
			return err
		}
		held = ForeignHeldCopy{Revision: 1, Reference: in, Principal: auth, Phase: "reference_intent", UseState: "allowed", CleanupState: "pending"}
		if err = tx.Create(ctx, "content.reference_intents", in.CopyID, sourceKey(scope, in.ContentRef), in); err != nil {
			return err
		}
		if err = tx.Create(ctx, "content.held_copies", in.CopyID, sourceKey(scope, in.ContentRef), held); err != nil {
			return err
		}
		now, err := tx.Now(ctx)
		if err != nil {
			return err
		}
		_, err = tx.Raise(ctx, "content.foreign_reconcile", in.CopyID, in.ReferenceIntentRef, now)
		return err
	})
	if err != nil {
		return ForeignUse{}, err
	}
	var proof ForeignProof
	if err = s.checkForeignIO(ctx, scope); err != nil {
		return ForeignUse{}, err
	}
	if held.Phase == "reference_intent" {
		proof, err = s.Foreign.RegisterCopy(ctx, scope, auth, in)
	} else {
		proof, err = s.Foreign.Current(ctx, scope, auth, in)
	}
	if err != nil {
		return ForeignUse{}, err
	}
	held, err = s.observeForeignProof(ctx, scope, auth, in, proof, nil)
	if err != nil {
		return ForeignUse{}, err
	}
	if held.KnownDeny {
		return ForeignUse{}, api.E("forbidden", "source_closed")
	}
	use := ForeignUse{in, proof}
	checkedCtx, err := WithForeignUses(ctx, []ForeignUse{use})
	if err != nil {
		return ForeignUse{}, err
	}
	err = s.authWithin(checkedCtx, scope, auth, func(tx runtime.Tx) error {
		return s.foreignUseAllowed(checkedCtx, tx, auth, held, proof, in.Purpose, in.Location, false, false)
	})
	if err != nil {
		return ForeignUse{}, err
	}
	if held.Phase != "held" {
		if err = s.checkForeignIO(ctx, scope); err != nil {
			return ForeignUse{}, err
		}
		body, err := s.Foreign.Read(ctx, scope, auth, in, proof)
		if err != nil {
			return ForeignUse{}, err
		}
		if uint64(len(body)) != in.ContentRef.ByteLength || api.Hash(body) != in.ContentRef.Hash {
			return ForeignUse{}, api.E("dependency_unavailable", "foreign_content_corrupted")
		}
		if err = s.checkForeignIO(ctx, scope); err != nil {
			return ForeignUse{}, err
		}
		location, err := s.Objects.Write(ctx, in.ContentRef, bytes.NewReader(body))
		if err != nil {
			return ForeignUse{}, err
		}
		// 不将网络/对象 IO 放入本方事务；最后有限源检查后存在设计明确的跨库窗口。
		if err = s.checkForeignIO(ctx, scope); err != nil {
			return ForeignUse{}, err
		}
		proof, err = s.Foreign.Current(ctx, scope, auth, in)
		if err != nil {
			return ForeignUse{}, err
		}
		held, err = s.observeForeignProof(ctx, scope, auth, in, proof, &location)
		if err != nil {
			return ForeignUse{}, err
		}
		if held.KnownDeny {
			return ForeignUse{}, api.E("forbidden", "source_closed")
		}
		use.Proof = proof
	}
	return use, nil
}

func (s *Service) foreignUseAllowed(ctx context.Context, tx runtime.Tx, auth runtime.Auth, held ForeignHeldCopy, proof ForeignProof, purpose, location string, continuous, historical bool) error {
	if proof.Mode != "use" {
		return api.E("forbidden", "control_proof_cannot_authorize_bytes")
	}
	if budget, ok := ctx.Value(permissionBudgetKey{}).(*permissionBudget); ok {
		if budget.remaining == 0 {
			return api.E("overloaded", "query_permission_budget")
		}
		budget.remaining--
	}
	if err := s.verifyForeignProof(ctx, tx, auth, held.Reference, proof); err != nil {
		return err
	}
	var source foreignSourceState
	if _, err := tx.Get(ctx, "content.foreign_sources", sourceKey(tx.Scope(), proof.ContentRef), &source); err != nil {
		return err
	}
	if source.State != "published" || source.ControlRevision > proof.ControlRevision || source.SourceDatabaseID != proof.SourceDatabaseID {
		return api.E("forbidden", "source_closed")
	}
	if held.KnownDeny || held.UseState != "allowed" || held.Proof.ControlRevision > proof.ControlRevision || held.Proof.SourceDatabaseID != proof.SourceDatabaseID || purpose != held.Reference.Purpose || location != held.Reference.Location {
		return api.E("forbidden", "source_closed")
	}
	now, err := tx.Now(ctx)
	if err != nil {
		return err
	}
	issued, _ := api.ParseTime(proof.IssuedAt)
	until, _ := api.ParseTime(proof.StartBefore)
	retain, _ := api.ParseTime(proof.RetainUntil)
	if issued.After(now) || !now.Before(until) {
		return api.E("expired", "foreign_proof_expired")
	}
	if proof.SourceState != "published" || proof.UseState != "allowed" || !now.Before(retain) {
		return api.E("forbidden", "source_closed")
	}
	if !contains(proof.PolicyValues.Subjects, auth.SubjectID) || !contains(proof.PolicyValues.Purposes, purpose) || !contains(proof.PolicyValues.Locations, location) || continuous && !proof.Continuous {
		return api.E("forbidden", "source_forbidden")
	}
	return nil
}

func (s *Service) sourcePolicy(ctx context.Context, tx runtime.Tx, v ContentVersion) (Policy, error) {
	if v.ContentRef.OwnerID == tx.Scope().OwnerID {
		return s.policy(ctx, tx, v.PolicyRef)
	}
	uses, _ := ctx.Value(foreignContextKey{}).([]ForeignUse)
	for _, use := range uses {
		if use.Proof.ContentRef == v.ContentRef && api.Equal(use.Proof.PolicyRef, v.PolicyRef) {
			return Policy{PolicyRef: v.PolicyRef, Values: use.Proof.PolicyValues, Revision: 1, State: "active"}, nil
		}
	}
	return Policy{}, api.E("dependency_unavailable", "current_foreign_proof_required")
}

func (s *Service) SourcePolicySnapshot(ctx context.Context, scope runtime.Scope, auth runtime.Auth, ref api.ContentRef, purpose, location string) (ContentPolicySnapshot, error) {
	ctx, err := s.PrepareForeignContext(ctx, scope, auth, []api.ContentRef{ref}, purpose, location)
	if err != nil {
		return ContentPolicySnapshot{}, err
	}
	var out ContentPolicySnapshot
	err = s.authWithin(ctx, scope, auth, func(tx runtime.Tx) error {
		var err error
		out, err = s.SourcePolicySnapshotTx(ctx, tx, auth, ref, purpose, location)
		return err
	})
	return out, err
}

// SourcePolicySnapshotTx 仅消费已取得的当前外部证明，不执行跨 owner RPC。
func (s *Service) SourcePolicySnapshotTx(ctx context.Context, tx runtime.Tx, auth runtime.Auth, ref api.ContentRef, purpose, location string) (ContentPolicySnapshot, error) {
	v, err := s.CheckContentTx(ctx, tx, auth, ref, purpose, location, false)
	if err != nil {
		return ContentPolicySnapshot{}, err
	}
	policy, err := s.sourcePolicy(ctx, tx, v)
	if err != nil {
		return ContentPolicySnapshot{}, err
	}
	return ContentPolicySnapshot{ContentRef: ref, Policy: policy, SubjectRefs: []api.ObjectRef{auth.Ref(tx.Scope().OwnerID)}, RetainUntil: v.RetentionUntil, ControlRevision: v.ControlRevision}, nil
}

func (s *Service) checkForeignContent(ctx context.Context, tx runtime.Tx, auth runtime.Auth, ref api.ContentRef, purpose, location string, continuous, historical bool) (ContentVersion, error) {
	uses, _ := ctx.Value(foreignContextKey{}).([]ForeignUse)
	for _, use := range uses {
		if use.Reference.ContentRef != ref || use.Reference.Purpose != purpose || use.Reference.Location != location {
			continue
		}
		var held ForeignHeldCopy
		_, err := tx.Get(ctx, "content.held_copies", use.Reference.CopyID, &held)
		if err != nil {
			return ContentVersion{}, err
		}
		if held.Phase != "held" || !api.Equal(held.Reference, use.Reference) {
			return ContentVersion{}, api.E("dependency_unavailable", "foreign_copy_not_held")
		}
		if err = s.foreignUseAllowed(ctx, tx, auth, held, use.Proof, purpose, location, continuous, historical); err != nil {
			return ContentVersion{}, err
		}
		if err = s.checkSourceGate(ctx, tx, auth, ref, purpose, location, continuous); err != nil {
			return ContentVersion{}, err
		}
		p := use.Proof
		return ContentVersion{ContentRef: ref, ObjectLocation: held.ObjectLocation, State: p.SourceState, ControlRevision: p.ControlRevision, PolicyRef: p.PolicyRef, RetentionUntil: p.RetainUntil, ProcessedSources: p.ProcessedSources, DisclosedSources: p.DisclosedSources, ClosureKind: p.ClosureKind}, nil
	}
	return ContentVersion{}, api.E("dependency_unavailable", "current_foreign_proof_required")
}

// PrepareForeignContext 仅沿本方已经登记的原 holder 获取当前许可，不创建替代引用身份。
func (s *Service) PrepareForeignContext(ctx context.Context, scope runtime.Scope, auth runtime.Auth, refs []api.ContentRef, purpose, location string) (context.Context, error) {
	var requests []ForeignReference
	err := s.authWithin(ctx, scope, auth, func(tx runtime.Tx) error {
		pending := append([]api.ContentRef{}, refs...)
		seen := map[string]bool{}
		for len(pending) > 0 {
			ref := pending[0]
			pending = pending[1:]
			if err := api.ValidateRecord("ContentRef", ref); err != nil {
				return err
			}
			if ref.TenantID != scope.TenantID {
				return api.E("forbidden", "reference_scope_mismatch")
			}
			key := sourceKey(scope, ref)
			if seen[key] {
				continue
			}
			seen[key] = true
			if len(seen) > 200 || len(pending) > 200 {
				return api.E("overloaded", "source_closure_limit")
			}
			if ref.OwnerID == scope.OwnerID {
				var v ContentVersion
				_, err := tx.Get(ctx, "content.versions", contentKey(ref), &v)
				if err != nil {
					return err
				}
				if v.ContentRef != ref {
					return api.E("idempotency_conflict", "content_reference_changed")
				}
				pending = append(pending, v.ProcessedSources...)
				continue
			}
			rows, err := tx.List(ctx, "content.held_copies", key, "", 101)
			if err != nil {
				return err
			}
			if len(rows) > 100 {
				return api.E("overloaded", "foreign_holder_limit")
			}
			found := false
			for _, row := range rows {
				var held ForeignHeldCopy
				if err = row.Decode(&held); err != nil {
					return err
				}
				if held.Reference.ContentRef == ref && api.Equal(held.Reference.HolderRef, auth.Ref(scope.OwnerID)) && held.Reference.Purpose == purpose && held.Reference.Location == location {
					if held.KnownDeny || held.UseState != "allowed" {
						return api.E("forbidden", "source_closed")
					}
					requests = append(requests, held.Reference)
					found = true
					break
				}
			}
			if !found {
				return api.E("dependency_unavailable", "foreign_reference_not_registered")
			}
		}
		return nil
	})
	if err != nil {
		return ctx, err
	}
	if len(requests) == 0 {
		return ctx, nil
	}
	if s.Foreign == nil {
		return ctx, api.E("dependency_unavailable", "source_authority_unavailable")
	}
	uses := make([]ForeignUse, 0, len(requests))
	for _, in := range requests {
		proof, err := s.Foreign.Current(ctx, scope, auth, in)
		if err != nil {
			return ctx, err
		}
		held, err := s.observeForeignProof(ctx, scope, auth, in, proof, nil)
		if err != nil {
			return ctx, err
		}
		if held.KnownDeny {
			return ctx, api.E("forbidden", "source_closed")
		}
		uses = append(uses, ForeignUse{in, proof})
	}
	return WithForeignUses(ctx, uses)
}

// CurrentForeignCopy 是源权威元数据端口。签名在 adapter 中进行，不产生隐式许可。
func (s *Service) CurrentForeignCopy(ctx context.Context, scope runtime.Scope, auth runtime.Auth, in ForeignReference, control bool) (ForeignProof, error) {
	var out ForeignProof
	err := s.authWithin(ctx, scope, auth, func(tx runtime.Tx) error {
		var holder CopyHolder
		_, err := tx.Get(ctx, "content.holders", in.CopyID, &holder)
		if err != nil {
			return err
		}
		if holder.PrincipalID != auth.SubjectID || holder.ContentRef != in.ContentRef || !api.Equal(holder.HolderRef, in.HolderRef) || holder.HolderRef.ObjectID != auth.SubjectID || (!control && holder.HolderRef.Revision != auth.CredentialGeneration) || control && auth.CredentialGeneration < holder.HolderRef.Revision || !api.Equal(holder.ReferenceIntentRef, in.ReferenceIntentRef) || holder.Purpose != in.Purpose || holder.Location != in.Location || holder.RetainUntil != in.RetainUntil {
			return api.E("forbidden", "copy_holder_mismatch")
		}
		var v ContentVersion
		_, err = tx.Get(ctx, "content.versions", contentKey(in.ContentRef), &v)
		if err != nil {
			return err
		}
		if v.ContentRef != in.ContentRef {
			return api.E("idempotency_conflict", "content_reference_changed")
		}
		policy, err := s.policy(ctx, tx, v.PolicyRef)
		if err != nil {
			return err
		}
		now, err := tx.Now(ctx)
		if err != nil {
			return err
		}
		useState := holder.UseState
		if !control {
			_, err = s.CheckContentTx(ctx, tx, auth, v.ContentRef, in.Purpose, in.Location, false)
			if err != nil {
				if !api.IsCode(err, "forbidden") && !api.IsCode(err, "expired") && !api.IsCode(err, "gone") {
					return err
				}
				useState = "closing"
			}
		}
		retain, _ := api.ParseTime(holder.RetainUntil)
		sourceRetain, _ := api.ParseTime(v.RetentionUntil)
		if sourceRetain.Before(retain) {
			retain = sourceRetain
		}
		if v.State != "published" || !now.Before(retain) {
			if useState == "allowed" {
				useState = "closing"
			}
		}
		until := now.Add(30 * time.Second)
		mode := "use"
		if control {
			mode = "control"
		}
		out = ForeignProof{ContentRef: v.ContentRef, PolicyRef: v.PolicyRef, PolicyValues: policy.Values, SourceDatabaseID: scope.DatabaseID, ControlRevision: v.ControlRevision, RetainUntil: api.Time(retain), ProcessedSources: append([]api.ContentRef{}, v.ProcessedSources...), DisclosedSources: append([]api.ContentRef{}, v.DisclosedSources...), IssuedAt: api.Time(now), StartBefore: api.Time(until), SubjectRef: auth.Ref(in.HolderRef.OwnerID), HolderRef: holder.HolderRef, Purpose: holder.Purpose, Location: holder.Location, CopyID: holder.CopyID, ReferenceIntentRef: holder.ReferenceIntentRef, SourceState: v.State, ClosureKind: v.ClosureKind, UseState: useState, CleanupState: holder.CleanupState, EvidenceRefs: append([]api.ContentRef{}, holder.EvidenceRefs...), Continuous: policy.Values.Continuous, IndependentDerived: policy.Values.IndependentDerived}
		out.Mode = mode
		out.SubjectRef = holder.HolderRef
		return nil
	})
	return out, err
}

func checkCopyOwnerRefs(scope runtime.Scope, holder, intent api.ObjectRef) error {
	if err := api.ValidateRecord("ObjectRef", holder); err != nil {
		return err
	}
	if err := api.ValidateRecord("ObjectRef", intent); err != nil {
		return err
	}
	if holder.TenantID != scope.TenantID || intent.TenantID != scope.TenantID || holder.OwnerID != intent.OwnerID {
		return api.E("forbidden", "copy_reference_scope_mismatch")
	}
	return nil
}

func (s *Service) heldForeignCopy(ctx context.Context, scope runtime.Scope, auth runtime.Auth, copyID string) (ForeignHeldCopy, error) {
	var held ForeignHeldCopy
	err := s.authWithin(ctx, scope, auth, func(tx runtime.Tx) error {
		_, err := tx.Get(ctx, "content.held_copies", copyID, &held)
		if err != nil {
			return err
		}
		if !cleanupHolder(scope, auth, held.Reference.HolderRef) {
			return api.E("forbidden", "copy_holder_mismatch")
		}
		return nil
	})
	return held, err
}

// ControlForeignCopy 保留原 holder 的控制查询，即使正文许可已经封闭。
func (s *Service) ControlForeignCopy(ctx context.Context, scope runtime.Scope, auth runtime.Auth, copyID string) (ForeignUse, error) {
	held, err := s.heldForeignCopy(ctx, scope, auth, copyID)
	if err != nil {
		return ForeignUse{}, err
	}
	if s.Foreign == nil {
		return ForeignUse{}, api.E("dependency_unavailable", "source_authority_unavailable")
	}
	proof, err := s.Foreign.Control(ctx, scope, auth, held.Reference)
	if err != nil {
		return ForeignUse{}, err
	}
	_, err = s.observeForeignProof(ctx, scope, auth, held.Reference, proof, nil)
	return ForeignUse{held.Reference, proof}, err
}

// StopForeignCopy 先关闭本方新使用，再删除本方准确镜像并报告原停止责任。
// 本方擦除不伪造源能核验的证据；源 cleanup 仍为 pending，直至收到合格报告。
func (s *Service) StopForeignCopy(ctx context.Context, scope runtime.Scope, auth runtime.Auth, copyID string) error {
	var held ForeignHeldCopy
	err := s.authWithin(ctx, scope, auth, func(tx runtime.Tx) error {
		rev, err := tx.Get(ctx, "content.held_copies", copyID, &held)
		if err != nil {
			return err
		}
		if !cleanupHolder(scope, auth, held.Reference.HolderRef) {
			return api.E("forbidden", "copy_holder_mismatch")
		}
		if held.UseState == "use_stopped" && held.Phase == "released" {
			return nil
		}
		held.UseState = "use_stopped"
		held.KnownDeny = true
		held.Revision = rev + 1
		if err = tx.Put(ctx, "content.held_copies", copyID, rev, held); err != nil {
			return err
		}
		now, err := tx.Now(ctx)
		if err != nil {
			return err
		}
		_, err = tx.Raise(ctx, "content.foreign_reconcile", copyID, held.Reference.ReferenceIntentRef, now)
		return err
	})
	if err != nil {
		return err
	}
	if held.Phase == "released" {
		return nil
	}
	return s.stopForeignCopy(ctx, scope, auth, held, nil)
}

func cleanupHolder(scope runtime.Scope, auth runtime.Auth, holder api.ObjectRef) bool {
	return holder.TenantID == scope.TenantID && holder.OwnerID == scope.OwnerID && holder.ObjectID == auth.SubjectID && holder.Revision <= auth.CredentialGeneration && holder.Revision > 0
}

func (s *Service) foreignClaim(ctx context.Context, scope runtime.Scope, claim *api.Claim) error {
	if claim == nil {
		return ctx.Err()
	}
	return s.Store.CheckClaim(ctx, scope, *claim)
}

func (s *Service) stopForeignCopy(ctx context.Context, scope runtime.Scope, auth runtime.Auth, held ForeignHeldCopy, claim *api.Claim) error {
	if s.Foreign == nil {
		return api.E("dependency_unavailable", "source_authority_unavailable")
	}
	if err := s.foreignClaim(ctx, scope, claim); err != nil {
		return err
	}
	proof, err := s.Foreign.Control(ctx, scope, auth, held.Reference)
	if err != nil {
		return err
	}
	held, err = s.observeForeignProof(ctx, scope, auth, held.Reference, proof, nil)
	if err != nil {
		return err
	}
	if held.ObjectLocation.Key != "" && held.CleanupState != "complete" {
		if err = s.foreignClaim(ctx, scope, claim); err != nil {
			return err
		}
		if err = s.Objects.Delete(ctx, held.ObjectLocation); err != nil {
			return err
		}
	}
	err = s.authWithin(ctx, scope, auth, func(tx runtime.Tx) error {
		var current ForeignHeldCopy
		rev, err := tx.Get(ctx, "content.held_copies", held.Reference.CopyID, &current)
		if err != nil {
			return err
		}
		if current.UseState != "use_stopped" {
			return api.E("invalid_state", "foreign_copy_still_in_use")
		}
		current.CleanupState = "complete"
		current.Revision = rev + 1
		if current.StopReport == nil {
			current.StopReport = &ReleaseCopyInput{CopyID: current.Reference.CopyID, ContentRef: current.Reference.ContentRef, ControlRevision: proof.ControlRevision, UseStopped: true, CleanupState: "pending", EvidenceRefs: []api.ContentRef{}}
		}
		if err = tx.Put(ctx, "content.held_copies", current.Reference.CopyID, rev, current); err != nil {
			return err
		}
		held = current
		if claim != nil {
			return tx.Guard(ctx, *claim)
		}
		return nil
	})
	if err != nil {
		return err
	}
	if err = s.foreignClaim(ctx, scope, claim); err != nil {
		return err
	}
	proof, err = s.Foreign.Release(ctx, scope, auth, held.Reference, *held.StopReport)
	if err != nil {
		return err
	}
	if _, err = s.observeForeignProof(ctx, scope, auth, held.Reference, proof, nil); err != nil {
		return err
	}
	return s.authWithin(ctx, scope, auth, func(tx runtime.Tx) error {
		var current ForeignHeldCopy
		rev, err := tx.Get(ctx, "content.held_copies", held.Reference.CopyID, &current)
		if err != nil {
			return err
		}
		if proof.UseState != "use_stopped" {
			return api.E("dependency_unavailable", "original_copy_stop_unconfirmed")
		}
		current.Phase = "released"
		current.Revision = rev + 1
		if err = tx.Put(ctx, "content.held_copies", current.Reference.CopyID, rev, current); err != nil {
			return err
		}
		if claim != nil {
			return tx.Guard(ctx, *claim)
		}
		return nil
	})
}

func (s *Service) foreignReconcileJob(ctx context.Context, store runtime.Store, scope runtime.Scope, work runtime.Work) error {
	ctx = context.WithValue(ctx, foreignClaimKey{}, work.Claim)
	var held ForeignHeldCopy
	err := s.within(ctx, scope, func(tx runtime.Tx) error {
		_, err := tx.Get(ctx, "content.held_copies", work.Job.ResponsibilityKey, &held)
		if err != nil {
			return err
		}
		return tx.Guard(ctx, work.Claim)
	})
	if err != nil {
		return err
	}
	if held.Phase != "released" {
		if s.Foreign == nil {
			return api.E("dependency_unavailable", "source_authority_unavailable")
		}
		if err = s.foreignClaim(ctx, scope, &work.Claim); err != nil {
			return err
		}
		if held.UseState == "use_stopped" {
			err = s.stopForeignCopy(ctx, scope, held.Principal, held, &work.Claim)
		} else if held.Phase == "reference_intent" {
			_, err = s.PrepareForeignUse(ctx, scope, held.Principal, held.Reference)
		} else {
			var proof ForeignProof
			proof, err = s.Foreign.Current(ctx, scope, held.Principal, held.Reference)
			if err == nil {
				_, err = s.observeForeignProof(ctx, scope, held.Principal, held.Reference, proof, nil)
			}
		}
	}
	if errors.Is(err, runtime.ErrCommitUnknown) || errors.Is(err, context.Canceled) || errors.Is(err, runtime.ErrClaimLost) {
		return err
	}
	finishErr := finishWork(s, ctx, store, scope, work, func(tx runtime.Tx) (runtime.Disposition, error) {
		var current ForeignHeldCopy
		_, loadErr := tx.Get(ctx, "content.held_copies", held.Reference.CopyID, &current)
		if loadErr != nil {
			return runtime.Disposition{}, loadErr
		}
		now, loadErr := tx.Now(ctx)
		if loadErr != nil {
			return runtime.Disposition{}, loadErr
		}
		if current.Phase == "released" {
			return runtime.Done(), nil
		}
		if err != nil {
			return runtime.Waiting(now.Add(5 * time.Second)), nil
		}
		return runtime.Waiting(now.Add(30 * time.Second)), nil
	})
	return errors.Join(err, finishErr)
}
