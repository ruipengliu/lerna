package executor

import (
	"context"
	"sort"
	"time"

	"github.com/ruipengliu/lerna/adapters/platform"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/memory"
	"github.com/ruipengliu/lerna/runtime"
)

func validateOutputConfig(c Config) error {
	if len(c.OutputSubjectRefs) > 32 || len(c.OutputPurposes) > 32 || len(c.OutputLocations) > 4 {
		return api.E("invalid_request", "output_policy_bounds")
	}
	for _, ref := range c.OutputSubjectRefs {
		if api.ValidateRecord("ObjectRef", ref) != nil || ref.TenantID != c.TenantID || ref.OwnerID != c.Authority.OwnerID {
			return api.E("forbidden", "output_subject_not_paired")
		}
	}
	for _, purpose := range c.OutputPurposes {
		if purpose == "" || purpose == "*" || len(purpose) > 128 {
			return api.E("invalid_request", "output_purpose_unbounded")
		}
	}
	for _, location := range c.OutputLocations {
		if !has([]string{"cloud", "device", "local"}, location) {
			return api.E("invalid_request", "output_location_unbounded")
		}
	}
	return nil
}

func uniqueStrings(in []string) []string {
	set := map[string]bool{}
	for _, s := range in {
		set[s] = true
	}
	out := make([]string, 0, len(set))
	for s := range set {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}
func (h *Host) outputPolicy(a runtime.Auth, permission ContentPermission, sources []contentRecord) (*memory.Policy, []api.ObjectRef, error) {
	readers := append([]api.ObjectRef{}, h.Config.OutputSubjectRefs...)
	if len(readers) == 0 {
		readers = []api.ObjectRef{a.Ref(h.Config.Authority.OwnerID)}
	}
	ids := []string{}
	for _, reader := range readers {
		ids = append(ids, reader.ObjectID)
	}
	purposes := uniqueStrings(append(append([]string{}, h.Config.OutputPurposes...), permission.Purposes...))
	locations := append([]string{}, h.Config.OutputLocations...)
	if len(locations) == 0 {
		locations = []string{"cloud"}
	}
	values := memory.PolicyValues{Subjects: uniqueStrings(ids), Purposes: purposes, Locations: uniqueStrings(locations), RetainUntil: permission.RetainUntil, Continuous: true, IndependentDerived: false}
	for _, source := range sources {
		policy, allowed := source.Permission.SourcePolicy, source.Permission.SubjectRefs
		if source.Published {
			policy, allowed = source.SourcePolicy, source.SourceReaders
		}
		// 缺原签名来源策略不能用配置补权。结果/证据仍保存，普通新引用保持关闭。
		if policy == nil || len(allowed) == 0 {
			return nil, []api.ObjectRef{}, nil
		}
		values.Subjects = intersect(values.Subjects, policy.Values.Subjects)
		values.Purposes = intersect(values.Purposes, policy.Values.Purposes)
		values.Locations = intersect(values.Locations, policy.Values.Locations)
		values.RetainUntil = earliest(values.RetainUntil, policy.Values.RetainUntil, source.Permission.RetainUntil)
		values.Continuous = values.Continuous && policy.Values.Continuous
		next := []api.ObjectRef{}
		for _, reader := range readers {
			for _, bound := range allowed {
				if api.Equal(reader, bound) {
					next = append(next, reader)
					break
				}
			}
		}
		readers = next
	}
	digest, err := api.Digest(values)
	if err != nil {
		return nil, nil, err
	}
	ref := api.ComponentRef{ComponentID: apiID("policy", contentKey(permission.ContentRef)), Version: "1", Digest: digest}
	return &memory.Policy{PolicyRef: ref, Values: values, Revision: 1, State: "active"}, readers, nil
}
func intersect(a, b []string) []string {
	out := []string{}
	for _, v := range a {
		if has(b, v) {
			out = append(out, v)
		}
	}
	return uniqueStrings(out)
}

func (h *Host) registerSource() error {
	if err := registerCommand(h.Registry, "content.register_copy", []string{Namespace}, h.registerSourceCopy); err != nil {
		return err
	}
	if err := registerCommand(h.Registry, "content.release_copy", []string{Namespace}, h.releaseSourceCopy); err != nil {
		return err
	}
	if err := registerQuery(h.Registry, "content.get", h.sourceGet); err != nil {
		return err
	}
	if err := h.Registry.RegisterJob(sourceCloseJob, h.closeSourceCopies); err != nil {
		return err
	}
	if err := h.Registry.RegisterJob(sourceCopyExpireJob, h.expireSourceCopy); err != nil {
		return err
	}
	return registerQuery(h.Registry, "executor.content.current", h.sourceCurrent)
}
func (h *Host) sourceContentTx(ctx context.Context, tx runtime.Tx, ref api.ContentRef) (contentRecord, error) {
	var rec contentRecord
	if ref.TenantID != tx.Scope().TenantID || ref.OwnerID != tx.Scope().OwnerID || api.ValidateRecord("ContentRef", ref) != nil {
		return rec, api.E("forbidden", "original_source_scope_mismatch")
	}
	if _, err := tx.Get(ctx, Namespace+".contents", contentKey(ref), &rec); err != nil {
		return rec, err
	}
	if !rec.Published || !rec.Complete || !api.Equal(rec.Permission.ContentRef, ref) || rec.ControlRevision == 0 {
		return rec, api.E("forbidden", "original_published_source_required")
	}
	return rec, nil
}
func (h *Host) sourceReader(rec contentRecord, r memory.ForeignReference) error {
	if rec.SourcePolicy == nil || len(rec.SourceReaders) == 0 {
		return api.E("forbidden", "original_source_policy_missing")
	}
	if api.ValidateRecord("ObjectRef", r.HolderRef) != nil || r.HolderRef.TenantID != h.Scope.TenantID || r.HolderRef.OwnerID != h.Config.Authority.OwnerID || api.ValidateRecord("ObjectRef", r.ReferenceIntentRef) != nil || r.ReferenceIntentRef.TenantID != h.Scope.TenantID || r.ReferenceIntentRef.OwnerID != r.HolderRef.OwnerID || !api.ValidID(r.CopyID) {
		return api.E("forbidden", "original_copy_holder_mismatch")
	}
	found := false
	for _, allowed := range rec.SourceReaders {
		if api.Equal(allowed, r.HolderRef) {
			found = true
		}
	}
	if !found || !has(rec.SourcePolicy.Values.Subjects, r.HolderRef.ObjectID) || !has(rec.SourcePolicy.Values.Purposes, r.Purpose) || !has(rec.SourcePolicy.Values.Locations, r.Location) {
		return api.E("forbidden", "original_source_policy_denied")
	}
	return nil
}
func referenceOf(in memory.RegisterCopyInput) memory.ForeignReference {
	return memory.ForeignReference{ContentRef: in.ContentRef, CopyID: in.CopyID, ReferenceIntentRef: in.ReferenceIntentRef, HolderRef: in.HolderRef, Purpose: in.Purpose, Location: in.Location, RetainUntil: in.RetainUntil}
}
func sourceCopyMatches(c memory.CopyHolder, r memory.ForeignReference) bool {
	return c.CopyID == r.CopyID && api.Equal(c.ContentRef, r.ContentRef) && api.Equal(c.HolderRef, r.HolderRef) && api.Equal(c.ReferenceIntentRef, r.ReferenceIntentRef) && c.Purpose == r.Purpose && c.Location == r.Location && c.RetainUntil == r.RetainUntil
}
func sourceCopyOutput(c memory.CopyHolder) memory.CopyOutput {
	return memory.CopyOutput{CopyID: c.CopyID, ControlRevision: c.ControlRevision, UseState: c.UseState, CleanupState: c.CleanupState}
}
func (h *Host) registerSourceCopy(ctx context.Context, tx runtime.Tx, a runtime.Auth, command api.Command, in memory.RegisterCopyInput) (memory.CopyOutput, error) {
	if err := h.peer(a); err != nil {
		return memory.CopyOutput{}, err
	}
	if command.TargetID != in.ContentRef.ContentID {
		return memory.CopyOutput{}, api.E("invalid_request", "target_mismatch")
	}
	rec, err := h.sourceContentTx(ctx, tx, in.ContentRef)
	if err != nil {
		return memory.CopyOutput{}, err
	}
	r := referenceOf(in)
	if err = h.sourceReader(rec, r); err != nil {
		return memory.CopyOutput{}, err
	}
	var old memory.CopyHolder
	if _, err = tx.Get(ctx, Namespace+".source_copies", in.CopyID, &old); err == nil {
		if !sourceCopyMatches(old, r) {
			return memory.CopyOutput{}, api.E("idempotency_conflict", "copy_input_changed")
		}
		return sourceCopyOutput(old), nil
	} else if !api.IsCode(err, "not_found") {
		return memory.CopyOutput{}, err
	}
	now, err := tx.Now(ctx)
	if err != nil {
		return memory.CopyOutput{}, err
	}
	if err = h.sourceAllowedTx(ctx, tx, rec, r, now); err != nil {
		return memory.CopyOutput{}, err
	}
	retain, err := api.ParseTime(r.RetainUntil)
	if err != nil || !now.Before(retain) {
		return memory.CopyOutput{}, api.E("expired", "copy_retention_expired")
	}
	limit, _ := api.ParseTime(earliest(rec.Permission.RetainUntil, rec.SourcePolicy.Values.RetainUntil))
	if retain.After(limit) {
		return memory.CopyOutput{}, api.E("forbidden", "retention_scope_expansion")
	}
	copy := memory.CopyHolder{CopyID: in.CopyID, Revision: 1, ContentRef: in.ContentRef, HolderRef: in.HolderRef, Purpose: in.Purpose, Location: in.Location, PolicyRef: rec.SourcePolicy.PolicyRef, RetainUntil: in.RetainUntil, ReferenceIntentRef: in.ReferenceIntentRef, UseState: "allowed", CleanupState: "pending", ControlRevision: rec.ControlRevision, EvidenceRefs: []api.ContentRef{}, PrincipalID: in.HolderRef.ObjectID, Kind: "external"}
	if err = tx.Create(ctx, Namespace+".source_copies", copy.CopyID, contentKey(copy.ContentRef), copy); err != nil {
		return memory.CopyOutput{}, err
	}
	if _, err = tx.Raise(ctx, sourceCopyExpireJob, copy.CopyID, tx.Scope().Ref(copy.CopyID, 1), retain); err != nil {
		return memory.CopyOutput{}, err
	}
	return sourceCopyOutput(copy), nil
}
func (h *Host) sourceAllowedTx(ctx context.Context, tx runtime.Tx, rec contentRecord, r memory.ForeignReference, now time.Time) error {
	if err := h.sourceReader(rec, r); err != nil {
		return err
	}
	if rec.SourceState != "published" || rec.SourcePolicy.State != "active" {
		return api.E("forbidden", "source_closed")
	}
	if err := before(now, earliest(rec.Permission.RetainUntil, rec.SourcePolicy.Values.RetainUntil, r.RetainUntil)); err != nil {
		return err
	}
	for _, ref := range []api.ObjectRef{r.HolderRef, rec.Principal.Auth().Ref(h.Config.Authority.OwnerID)} {
		if err := h.knownDenyTx(ctx, tx, ref); err != nil {
			return err
		}
	}
	return nil
}

func (h *Host) sourceProofTx(ctx context.Context, tx runtime.Tx, a runtime.Auth, r memory.ForeignReference, control bool) (memory.ForeignProof, error) {
	if err := h.peer(a); err != nil {
		return memory.ForeignProof{}, err
	}
	rec, err := h.sourceContentTx(ctx, tx, r.ContentRef)
	if err != nil {
		return memory.ForeignProof{}, err
	}
	var copy memory.CopyHolder
	if _, err = tx.Get(ctx, Namespace+".source_copies", r.CopyID, &copy); err != nil {
		return memory.ForeignProof{}, err
	}
	if !sourceCopyMatches(copy, r) {
		return memory.ForeignProof{}, api.E("forbidden", "original_copy_holder_mismatch")
	}
	now, err := tx.Now(ctx)
	if err != nil {
		return memory.ForeignProof{}, err
	}
	allowed := h.sourceAllowedTx(ctx, tx, rec, r, now)
	if !control && (allowed != nil || copy.UseState != "allowed") {
		if allowed != nil {
			return memory.ForeignProof{}, allowed
		}
		return memory.ForeignProof{}, api.E("forbidden", "source_copy_closed")
	}
	use := copy.UseState
	if allowed != nil && use == "allowed" {
		use = "closing"
	}
	until := api.Time(now.Add(5 * time.Second))
	if !control {
		until = earliest(until, rec.Permission.RetainUntil, rec.SourcePolicy.Values.RetainUntil, copy.RetainUntil)
	}
	p := memory.ForeignProof{ContentRef: rec.Permission.ContentRef, PolicyRef: rec.SourcePolicy.PolicyRef, PolicyValues: rec.SourcePolicy.Values, SourceDatabaseID: tx.Scope().DatabaseID, ControlRevision: rec.ControlRevision, RetainUntil: earliest(copy.RetainUntil, rec.Permission.RetainUntil, rec.SourcePolicy.Values.RetainUntil), ProcessedSources: rec.Permission.ProcessedSources, DisclosedSources: rec.Permission.DisclosedSources, IssuedAt: api.Time(now), StartBefore: until, SubjectRef: copy.HolderRef, HolderRef: copy.HolderRef, Purpose: copy.Purpose, Location: copy.Location, CopyID: copy.CopyID, ReferenceIntentRef: copy.ReferenceIntentRef, SourceState: rec.SourceState, ClosureKind: rec.ClosureKind, UseState: use, CleanupState: copy.CleanupState, EvidenceRefs: append([]api.ContentRef{}, copy.EvidenceRefs...), Continuous: rec.SourcePolicy.Values.Continuous, IndependentDerived: rec.SourcePolicy.Values.IndependentDerived}
	p.Mode = "use"
	if control {
		p.Mode = "control"
	}
	digest, err := ForeignContentDigest(p)
	if err != nil {
		return p, err
	}
	p.Proof, err = h.Keys.Sign("device-es256", foreignContentClaims(p, h.Config.Authority.OwnerID, digest))
	return p, err
}
func ForeignContentDigest(p memory.ForeignProof) (string, error) {
	p.Proof = ""
	return api.Digest(p)
}
func foreignContentClaims(p memory.ForeignProof, audience, digest string) platform.ProofClaims {
	return platform.ProofClaims{TenantID: p.ContentRef.TenantID, Issuer: p.ContentRef.OwnerID, Audience: audience, Purpose: "executor_content", ObjectRef: api.ObjectRef{TenantID: p.ContentRef.TenantID, OwnerID: p.ContentRef.OwnerID, ObjectID: p.ContentRef.ContentID, Revision: p.ContentRef.Version}, Digest: digest, ControlRevision: p.ControlRevision, WindowID: p.CopyID, IssuedAt: p.IssuedAt, StartBefore: p.StartBefore}
}
func (h *Host) sourceCurrent(ctx context.Context, st runtime.Store, scope runtime.Scope, a runtime.Auth, q api.Query, in SourceCurrent) (memory.ForeignProof, error) {
	var p memory.ForeignProof
	if q.TargetID != in.Reference.ContentRef.ContentID {
		return p, api.E("invalid_request", "target_mismatch")
	}
	status, err := st.Within(ctx, scope, []string{Namespace}, func(tx runtime.Tx) error {
		var e error
		p, e = h.sourceProofTx(ctx, tx, a, in.Reference, in.Control)
		return e
	})
	if status == runtime.CommitUnknown {
		return p, runtime.ErrCommitUnknown
	}
	return p, err
}
