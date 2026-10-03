package memory

import (
	"bytes"
	"context"
	"encoding/base64"
	"io"
	"time"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/runtime"
)

type ReserveInput struct {
	TransferID       string           `json:"transfer_id"`
	ContentRef       api.ContentRef   `json:"content_ref"`
	PolicyRef        api.ComponentRef `json:"policy_ref"`
	ProcessedSources []api.ContentRef `json:"processed_sources"`
	RetentionUntil   string           `json:"retention_until"`
	TransferDeadline string           `json:"transfer_deadline"`
}
type ReserveOutput struct {
	TransferID     string         `json:"transfer_id"`
	ContentRef     api.ContentRef `json:"content_ref"`
	ExpiresAt      string         `json:"expires_at"`
	MaxBytes       uint64         `json:"max_bytes"`
	UploadLocation string         `json:"upload_location"`
}
type PutInput struct {
	ContentRef       api.ContentRef   `json:"content_ref"`
	TransferID       string           `json:"transfer_id"`
	PolicyRef        api.ComponentRef `json:"policy_ref"`
	ProcessedSources []api.ContentRef `json:"processed_sources"`
	DisclosedSources []api.ContentRef `json:"disclosed_sources"`
	RetentionUntil   string           `json:"retention_until"`
}
type PutOutput struct {
	ContentRef api.ContentRef `json:"content_ref"`
}
type PublicationRequest struct {
	ContentRef       api.ContentRef
	TransferID       string
	ReserveCommandID string
	PutCommandID     string
	PolicyRef        api.ComponentRef
	ProcessedSources []api.ContentRef
	DisclosedSources []api.ContentRef
	RetentionUntil   string
	TransferDeadline string
}

func (s *Service) reserve(ctx context.Context, tx runtime.Tx, auth runtime.Auth, c api.Command, in ReserveInput) (ReserveOutput, error) {
	if !api.ValidID(in.TransferID) || c.TargetID != in.ContentRef.ContentID {
		return ReserveOutput{}, api.E("invalid_request", "transfer_scope_mismatch")
	}
	if err := checkContentRef(tx.Scope(), in.ContentRef); err != nil {
		return ReserveOutput{}, err
	}
	if in.ContentRef.ByteLength > MaxContentBytes {
		return ReserveOutput{}, api.E("invalid_request", "content_too_large")
	}
	if err := s.validateSources(tx.Scope(), in.ProcessedSources); err != nil {
		return ReserveOutput{}, err
	}
	p, err := s.allowed(ctx, tx, auth, in.PolicyRef, "content.write", s.Location, false)
	if err != nil {
		return ReserveOutput{}, err
	}
	retention, err := future(ctx, tx, in.RetentionUntil)
	if err != nil {
		return ReserveOutput{}, err
	}
	policyExpiry, _ := api.ParseTime(p.Values.RetainUntil)
	if retention.After(policyExpiry) {
		return ReserveOutput{}, api.E("forbidden", "retention_scope_expansion")
	}
	deadline, err := future(ctx, tx, in.TransferDeadline)
	if err != nil {
		return ReserveOutput{}, err
	}
	if deadline.After(retention) {
		return ReserveOutput{}, api.E("invalid_request", "transfer_scope_mismatch")
	}
	var old Transfer
	_, err = tx.Get(ctx, "content.transfers", in.TransferID, &old)
	if err == nil {
		if old.PublisherID != auth.SubjectID || !api.Equal(old.ContentRef, in.ContentRef) || !api.Equal(old.PolicyRef, in.PolicyRef) || !api.Equal(old.ProcessedSources, in.ProcessedSources) || old.RetentionUntil != in.RetentionUntil || old.ExpiresAt != in.TransferDeadline {
			return ReserveOutput{}, api.E("idempotency_conflict", "transfer_input_changed")
		}
		return reserveResult(old), nil
	}
	if !api.IsCode(err, "not_found") {
		return ReserveOutput{}, err
	}
	var content ContentVersion
	_, err = tx.Get(ctx, "content.versions", contentKey(in.ContentRef), &content)
	if err == nil && !api.Equal(content.ContentRef, in.ContentRef) {
		return ReserveOutput{}, api.E("idempotency_conflict", "content_version_changed")
	}
	if err != nil && !api.IsCode(err, "not_found") {
		return ReserveOutput{}, err
	}
	t := Transfer{TransferID: in.TransferID, Revision: 1, Kind: "upload", CommandRef: tx.Scope().Ref(c.CommandID, 1), ContentRef: in.ContentRef, PolicyRef: in.PolicyRef, ProcessedSources: in.ProcessedSources, RetentionUntil: in.RetentionUntil, ExpiresAt: in.TransferDeadline, Phase: "reserved", PublisherID: auth.SubjectID, TargetHolder: auth.Ref(tx.Scope().OwnerID), ReferenceIntentRef: tx.Scope().Ref(c.CommandID, 1), MaxBytes: in.ContentRef.ByteLength}
	if err = tx.Create(ctx, "content.transfers", in.TransferID, contentKey(in.ContentRef), t); err != nil {
		return ReserveOutput{}, err
	}
	if err = tx.Bind(ctx, "content.transfers", contentKey(in.ContentRef), in.TransferID, in.ContentRef.Hash); err != nil {
		return ReserveOutput{}, err
	}
	if _, err = tx.Raise(ctx, "content.transfer_cleanup", in.TransferID, tx.Scope().Ref(in.TransferID, 1), deadline); err != nil {
		return ReserveOutput{}, err
	}
	return reserveResult(t), nil
}
func reserveResult(t Transfer) ReserveOutput {
	return ReserveOutput{t.TransferID, t.ContentRef, t.ExpiresAt, t.MaxBytes, "transfer:" + t.TransferID}
}

// WriteTransfer 先提交原 writing 身份，再写介质；ready 的元数据提交未知时不发布。
func (s *Service) WriteTransfer(ctx context.Context, scope runtime.Scope, auth runtime.Auth, transferID string, src io.Reader) error {
	var t Transfer
	err := s.authWithin(ctx, scope, auth, func(tx runtime.Tx) error {
		if err := checkAuth(scope, auth); err != nil {
			return err
		}
		rev, err := tx.Get(ctx, "content.transfers", transferID, &t)
		if err != nil {
			return err
		}
		if t.PublisherID != auth.SubjectID {
			return api.E("forbidden", "transfer_principal_mismatch")
		}
		if err = s.checkTransfer(ctx, tx, auth, t); err != nil {
			return err
		}
		if t.Phase == "published" || t.Phase == "ready" {
			return nil
		}
		if t.Phase != "reserved" && t.Phase != "writing" {
			return api.E("invalid_state", "transfer_closed")
		}
		t.Phase = "writing"
		t.Revision = rev + 1
		return tx.Put(ctx, "content.transfers", transferID, rev, t)
	})
	if err != nil {
		return err
	}
	if t.Phase == "published" || t.Phase == "ready" {
		return nil
	}
	if t.ContentRef.MediaType == ExperienceMediaType {
		body, readErr := io.ReadAll(io.LimitReader(src, int64(t.ContentRef.ByteLength)+1))
		if readErr != nil {
			return readErr
		}
		if uint64(len(body)) != t.ContentRef.ByteLength || api.Hash(body) != t.ContentRef.Hash {
			return api.E("invalid_request", "content_hash_or_length_mismatch")
		}
		t.ExperienceOutcome, t.ExperienceProofRef, err = s.validateExperience(ctx, scope, auth, t, body)
		if err != nil {
			return err
		}
		src = bytes.NewReader(body)
	}
	loc, err := s.Objects.Write(ctx, t.ContentRef, src)
	if err != nil {
		return err
	}
	return s.authWithin(ctx, scope, auth, func(tx runtime.Tx) error {
		var current Transfer
		rev, err := tx.Get(ctx, "content.transfers", transferID, &current)
		if err != nil {
			return err
		}
		if current.PublisherID != auth.SubjectID || !api.Equal(t.ContentRef, current.ContentRef) {
			return api.E("idempotency_conflict", "transfer_input_changed")
		}
		if err = s.checkTransfer(ctx, tx, auth, current); err != nil {
			return err
		}
		if current.Phase == "published" || current.Phase == "ready" {
			if !api.Equal(current.ObjectLocation, loc) {
				return api.E("idempotency_conflict", "object_location_changed")
			}
			return nil
		}
		if current.Phase != "writing" {
			return api.E("invalid_state", "transfer_closed")
		}
		current.Phase = "ready"
		current.ObjectLocation = loc
		current.ExperienceOutcome = t.ExperienceOutcome
		current.ExperienceProofRef = t.ExperienceProofRef
		current.Revision = rev + 1
		return tx.Put(ctx, "content.transfers", transferID, rev, current)
	})
}

func (s *Service) checkTransfer(ctx context.Context, tx runtime.Tx, auth runtime.Auth, transfer Transfer) error {
	if transfer.PublisherID != auth.SubjectID || transfer.TargetHolder.Revision != auth.CredentialGeneration {
		return api.E("forbidden", "transfer_principal_mismatch")
	}
	if _, err := future(ctx, tx, transfer.ExpiresAt); err != nil {
		return api.E("expired", "transfer_expired")
	}
	if _, err := future(ctx, tx, transfer.RetentionUntil); err != nil {
		return err
	}
	if transfer.Kind == "mirror" {
		if transfer.Purpose == "" || transfer.TargetLocation != s.Location {
			return api.E("unsupported", "mirror_target_location_unconfigured")
		}
		_, err := s.CheckContentTx(ctx, tx, auth, transfer.ContentRef, transfer.Purpose, s.Location, false)
		return err
	}
	if _, err := s.allowed(ctx, tx, auth, transfer.PolicyRef, "content.write", s.Location, false); err != nil {
		return err
	}
	for _, ref := range transfer.ProcessedSources {
		if err := s.checkSourceGate(ctx, tx, auth, ref, "content.write", s.Location, false); err != nil {
			return err
		}
		if _, err := s.CheckContentTx(ctx, tx, auth, ref, "content.write", s.Location, false); err != nil {
			return err
		}
	}
	return nil
}

// PublishInTx 只发布已经 ready 的原准确版本，不读取或写入对象介质。
func (s *Service) PublishInTx(ctx context.Context, tx runtime.Tx, auth runtime.Auth, in PutInput) (api.ContentRef, error) {
	if _, err := loadHead(ctx, tx); err != nil {
		return api.ContentRef{}, err
	}
	if err := s.currentAuth(ctx, tx, auth); err != nil {
		return api.ContentRef{}, err
	}
	if err := checkContentRef(tx.Scope(), in.ContentRef); err != nil {
		return api.ContentRef{}, err
	}
	if err := s.validateSources(tx.Scope(), in.ProcessedSources); err != nil {
		return api.ContentRef{}, err
	}
	if err := s.validateSources(tx.Scope(), in.DisclosedSources); err != nil {
		return api.ContentRef{}, err
	}
	for _, disclosed := range in.DisclosedSources {
		found := false
		for _, processed := range in.ProcessedSources {
			if api.Equal(disclosed, processed) {
				found = true
			}
		}
		if !found {
			return api.ContentRef{}, api.E("invalid_request", "disclosed_source_not_processed")
		}
	}
	var old ContentVersion
	_, err := tx.Get(ctx, "content.versions", contentKey(in.ContentRef), &old)
	if err == nil {
		if old.PublisherID != auth.SubjectID || !api.Equal(old.ContentRef, in.ContentRef) || !api.Equal(old.PolicyRef, in.PolicyRef) || !api.Equal(old.ProcessedSources, in.ProcessedSources) || !api.Equal(old.DisclosedSources, in.DisclosedSources) || old.RetentionUntil != in.RetentionUntil {
			return api.ContentRef{}, api.E("idempotency_conflict", "content_version_changed")
		}
		if old.State != "published" {
			return api.ContentRef{}, api.E("invalid_state", "source_closed")
		}
		if _, err = s.CheckContentTx(ctx, tx, auth, old.ContentRef, "content.write", s.Location, false); err != nil {
			return api.ContentRef{}, err
		}
		return old.ContentRef, nil
	}
	if !api.IsCode(err, "not_found") {
		return api.ContentRef{}, err
	}
	var t Transfer
	rev, err := tx.Get(ctx, "content.transfers", in.TransferID, &t)
	if err != nil {
		return api.ContentRef{}, err
	}
	if t.Phase != "ready" {
		return api.ContentRef{}, api.E("dependency_unavailable", "transfer_not_ready")
	}
	if t.PublisherID != auth.SubjectID || !api.Equal(t.ContentRef, in.ContentRef) || !api.Equal(t.PolicyRef, in.PolicyRef) || !api.Equal(t.ProcessedSources, in.ProcessedSources) || t.RetentionUntil != in.RetentionUntil {
		return api.ContentRef{}, api.E("idempotency_conflict", "transfer_scope_mismatch")
	}
	p, err := s.allowed(ctx, tx, auth, in.PolicyRef, "content.write", s.Location, false)
	if err != nil {
		return api.ContentRef{}, err
	}
	retention, err := future(ctx, tx, in.RetentionUntil)
	if err != nil {
		return api.ContentRef{}, err
	}
	for _, source := range in.ProcessedSources {
		if err = s.checkSourceGate(ctx, tx, auth, source, "content.write", s.Location, false); err != nil {
			return api.ContentRef{}, err
		}
		if source.OwnerID == in.ContentRef.OwnerID && source.ContentID == in.ContentRef.ContentID && source.Version == in.ContentRef.Version {
			return api.ContentRef{}, api.E("invalid_request", "source_cycle")
		}
		v, err := s.CheckContentTx(ctx, tx, auth, source, "content.write", s.Location, false)
		if err != nil {
			return api.ContentRef{}, err
		}
		sp, err := s.sourcePolicy(ctx, tx, v)
		if err != nil {
			return api.ContentRef{}, err
		}
		if !subset(p.Values.Subjects, sp.Values.Subjects) || !subset(p.Values.Purposes, sp.Values.Purposes) || !subset(p.Values.Locations, sp.Values.Locations) {
			return api.ContentRef{}, api.E("forbidden", "source_scope_expansion")
		}
		expires, _ := api.ParseTime(v.RetentionUntil)
		if retention.After(expires) && (!p.Values.IndependentDerived || !sp.Values.IndependentDerived) {
			return api.ContentRef{}, api.E("forbidden", "retention_scope_expansion")
		}
	}
	now, err := tx.Now(ctx)
	if err != nil {
		return api.ContentRef{}, err
	}
	v := ContentVersion{ContentRef: in.ContentRef, ObjectLocation: t.ObjectLocation, State: "published", ControlRevision: 1, PolicyRef: in.PolicyRef, RetentionUntil: in.RetentionUntil, PublishedAt: api.Time(now), ProcessedSources: in.ProcessedSources, DisclosedSources: in.DisclosedSources, PublisherID: auth.SubjectID}
	v.ExperienceOutcome = t.ExperienceOutcome
	v.ExperienceProofRef = t.ExperienceProofRef
	if err = tx.Create(ctx, "content.versions", contentKey(in.ContentRef), in.ContentRef.ContentID, v); err != nil {
		return api.ContentRef{}, err
	}
	if _, err = tx.Raise(ctx, "content.expire", contentKey(in.ContentRef), tx.Scope().Ref(in.ContentRef.ContentID, in.ContentRef.Version), retention); err != nil {
		return api.ContentRef{}, err
	}
	for _, source := range in.ProcessedSources {
		edgeID := semanticID("edge", contentKey(in.ContentRef)+"/"+sourceKey(tx.Scope(), source))
		if err = tx.Create(ctx, "content.source_edges", edgeID, sourceKey(tx.Scope(), source), SourceEdge{in.ContentRef, source, "processed"}); err != nil {
			return api.ContentRef{}, err
		}
	}
	t.Phase = "published"
	t.Revision = rev + 1
	if err = tx.Put(ctx, "content.transfers", t.TransferID, rev, t); err != nil {
		return api.ContentRef{}, err
	}
	return in.ContentRef, nil
}

type SourceEdge struct {
	DerivedRef api.ContentRef `json:"derived_ref"`
	SourceRef  api.ContentRef `json:"source_ref"`
	Relation   string         `json:"relation"`
}

// CheckContentTx 用于显式共同 Tx 的引用门禁；跨 owner 不作临时网络查询。
func (s *Service) CheckContentTx(ctx context.Context, tx runtime.Tx, auth runtime.Auth, ref api.ContentRef, purpose, location string, continuous bool) (ContentVersion, error) {
	if _, err := loadHead(ctx, tx); err != nil {
		return ContentVersion{}, err
	}
	if err := s.currentAuth(ctx, tx, auth); err != nil {
		return ContentVersion{}, err
	}
	return s.checkContent(ctx, tx, auth, ref, purpose, location, continuous, map[string]bool{}, 0, false)
}
func (s *Service) checkContent(ctx context.Context, tx runtime.Tx, auth runtime.Auth, ref api.ContentRef, purpose, location string, continuous bool, seen map[string]bool, depth int, independent bool) (ContentVersion, error) {
	if ref.OwnerID != tx.Scope().OwnerID {
		return s.checkForeignContent(ctx, tx, auth, ref, purpose, location, continuous, independent && depth > 0, depth > 0)
	}
	if err := checkContentRef(tx.Scope(), ref); err != nil {
		return ContentVersion{}, err
	}
	if depth > 32 || len(seen) >= 200 {
		return ContentVersion{}, api.E("dependency_unavailable", "source_closure_limit")
	}
	if depth > 0 {
		if err := s.checkSourceGate(ctx, tx, auth, ref, purpose, location, continuous); err != nil {
			return ContentVersion{}, err
		}
	}
	key := contentKey(ref)
	var v ContentVersion
	_, err := tx.Get(ctx, "content.versions", key, &v)
	if err != nil {
		return v, err
	}
	if !api.Equal(ref, v.ContentRef) {
		return v, api.E("idempotency_conflict", "content_reference_changed")
	}
	p, err := s.policy(ctx, tx, v.PolicyRef)
	if err != nil {
		return v, err
	}
	historical := independent && depth > 0 && p.Values.IndependentDerived && (v.State == "published" || v.ClosureKind == "retention")
	if v.State != "published" && !historical {
		return v, api.E("forbidden", "source_closed")
	}
	if _, err = future(ctx, tx, v.RetentionUntil); err != nil && !historical {
		return v, err
	}
	if historical {
		if err = s.allowedHistorical(ctx, tx, auth, p, purpose, location, continuous); err != nil {
			return v, err
		}
	} else if _, err = s.allowed(ctx, tx, auth, v.PolicyRef, purpose, location, continuous); err != nil {
		return v, err
	}
	if seen[key] {
		return v, nil
	}
	seen[key] = true
	for _, source := range v.ProcessedSources {
		if _, err = s.checkContent(ctx, tx, auth, source, purpose, location, continuous, seen, depth+1, p.Values.IndependentDerived); err != nil {
			return v, err
		}
	}
	return v, nil
}

func (s *Service) Upload(ctx context.Context, scope runtime.Scope, auth runtime.Auth, request PublicationRequest, body []byte) (api.ContentRef, error) {
	if uint64(len(body)) != request.ContentRef.ByteLength || api.Hash(body) != request.ContentRef.Hash {
		return api.ContentRef{}, api.E("invalid_request", "content_hash_or_length_mismatch")
	}
	registry := s.commands()
	d := runtime.Dispatcher{Store: s.Store, OwnerID: scope.OwnerID, Registry: registry}
	reserve := ReserveInput{request.TransferID, request.ContentRef, request.PolicyRef, request.ProcessedSources, request.RetentionUntil, request.TransferDeadline}
	c := api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: scope.OwnerID, CommandID: request.ReserveCommandID, Method: "content.upload_reserve", TargetID: request.ContentRef.ContentID, ExpiresAt: request.TransferDeadline, Payload: api.Raw(reserve)}
	r, err := d.Command(ctx, auth, api.Raw(c))
	if err != nil {
		return api.ContentRef{}, err
	}
	if r.Error != nil {
		return api.ContentRef{}, r.Error
	}
	if err = s.WriteTransfer(ctx, scope, auth, request.TransferID, bytes.NewReader(body)); err != nil {
		return api.ContentRef{}, err
	}
	in := PutInput{request.ContentRef, request.TransferID, request.PolicyRef, request.ProcessedSources, request.DisclosedSources, request.RetentionUntil}
	c.CommandID = request.PutCommandID
	c.Method = "content.put"
	c.Payload = api.Raw(in)
	r, err = d.Command(ctx, auth, api.Raw(c))
	if err != nil {
		return api.ContentRef{}, err
	}
	if r.Error != nil {
		return api.ContentRef{}, r.Error
	}
	return request.ContentRef, nil
}

// RecoverUpload 只出版原已 ready 的上传，不接收替代字节、不续期、不创建另一票据。
func (s *Service) RecoverUpload(ctx context.Context, scope runtime.Scope, auth runtime.Auth, request PublicationRequest) (api.ContentRef, error) {
	reserve := api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: scope.OwnerID, CommandID: request.ReserveCommandID, Method: "content.upload_reserve", TargetID: request.ContentRef.ContentID, ExpiresAt: request.TransferDeadline, Payload: api.Raw(ReserveInput{request.TransferID, request.ContentRef, request.PolicyRef, request.ProcessedSources, request.RetentionUntil, request.TransferDeadline})}
	original, err := s.Store.LookupCommand(ctx, scope, request.ReserveCommandID)
	if err != nil {
		return api.ContentRef{}, err
	}
	if original.PrincipalID != auth.SubjectID || !api.Equal(original.Command, reserve) {
		return api.ContentRef{}, api.E("idempotency_conflict", "upload_recovery_identity_changed")
	}
	if original.Receipt.Error != nil {
		return api.ContentRef{}, original.Receipt.Error
	}
	t, err := s.LookupTransfer(ctx, scope, auth, request.TransferID)
	if err != nil {
		return api.ContentRef{}, err
	}
	if t.ContentRef != request.ContentRef || t.ExpiresAt != request.TransferDeadline {
		return api.ContentRef{}, api.E("idempotency_conflict", "upload_recovery_identity_changed")
	}
	if t.Phase != "ready" && t.Phase != "published" {
		return api.ContentRef{}, api.E("dependency_unavailable", "original_upload_bytes_unavailable")
	}
	put := reserve
	put.CommandID, put.Method = request.PutCommandID, "content.put"
	put.Payload = api.Raw(PutInput{request.ContentRef, request.TransferID, request.PolicyRef, request.ProcessedSources, request.DisclosedSources, request.RetentionUntil})
	d := runtime.Dispatcher{Store: s.Store, OwnerID: scope.OwnerID, Registry: s.commands()}
	r, err := d.Command(ctx, auth, api.Raw(put))
	if err != nil {
		return api.ContentRef{}, err
	}
	if r.Error != nil {
		return api.ContentRef{}, r.Error
	}
	return request.ContentRef, nil
}

func (s *Service) Read(ctx context.Context, scope runtime.Scope, auth runtime.Auth, ref api.ContentRef, purpose string) ([]byte, error) {
	return s.ReadBytes(ctx, scope, auth, ref, purpose, s.Location)
}
func (s *Service) ReadBytes(ctx context.Context, scope runtime.Scope, auth runtime.Auth, ref api.ContentRef, purpose, location string) ([]byte, error) {
	var err error
	if s.Foreign != nil {
		ctx, err = s.PrepareForeignContext(ctx, scope, auth, []api.ContentRef{ref}, purpose, location)
		if err != nil {
			return nil, err
		}
	}
	var v ContentVersion
	err = s.authWithin(ctx, scope, auth, func(tx runtime.Tx) error {
		var err error
		v, err = s.CheckContentTx(ctx, tx, auth, ref, purpose, location, false)
		if err != nil {
			return err
		}
		if location != s.Location {
			_, err = s.CheckContentTx(ctx, tx, auth, ref, purpose, s.Location, false)
		}
		return err
	})
	if err != nil {
		return nil, err
	}
	b, err := s.Objects.Read(ctx, v.ObjectLocation, ref, MaxContentBytes)
	if err != nil {
		return nil, err
	}
	if s.Foreign != nil {
		ctx, err = s.PrepareForeignContext(ctx, scope, auth, []api.ContentRef{ref}, purpose, location)
		if err != nil {
			return nil, err
		}
	}
	err = s.authWithin(ctx, scope, auth, func(tx runtime.Tx) error {
		current, err := s.CheckContentTx(ctx, tx, auth, ref, purpose, location, false)
		if err != nil {
			return err
		}
		if current.ControlRevision != v.ControlRevision {
			return api.E("forbidden", "source_closed")
		}
		if location != s.Location {
			_, err = s.CheckContentTx(ctx, tx, auth, ref, purpose, s.Location, false)
			if err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return b, nil
}

type RegisterCopyInput struct {
	CopyID             string         `json:"copy_id"`
	ContentRef         api.ContentRef `json:"content_ref"`
	HolderRef          api.ObjectRef  `json:"holder_ref"`
	Purpose            string         `json:"purpose"`
	Location           string         `json:"location"`
	RetainUntil        string         `json:"retain_until"`
	ReferenceIntentRef api.ObjectRef  `json:"reference_intent_ref"`
}
type CopyOutput struct {
	CopyID          string `json:"copy_id"`
	ControlRevision uint64 `json:"control_revision"`
	UseState        string `json:"use_state"`
	CleanupState    string `json:"cleanup_state"`
}

func copyOutput(h CopyHolder) CopyOutput {
	return CopyOutput{h.CopyID, h.ControlRevision, h.UseState, h.CleanupState}
}
func (s *Service) RegisterCopyTx(ctx context.Context, tx runtime.Tx, auth runtime.Auth, in RegisterCopyInput) (CopyOutput, error) {
	if !api.ValidID(in.CopyID) {
		return CopyOutput{}, api.E("invalid_request", "invalid_copy_identity")
	}
	if err := checkCopyOwnerRefs(tx.Scope(), in.HolderRef, in.ReferenceIntentRef); err != nil {
		return CopyOutput{}, err
	}
	v, err := s.CheckContentTx(ctx, tx, auth, in.ContentRef, in.Purpose, in.Location, false)
	if err != nil {
		return CopyOutput{}, err
	}
	retain, err := future(ctx, tx, in.RetainUntil)
	if err != nil {
		return CopyOutput{}, err
	}
	expires, _ := api.ParseTime(v.RetentionUntil)
	if retain.After(expires) {
		return CopyOutput{}, api.E("forbidden", "retention_scope_expansion")
	}
	var old CopyHolder
	_, err = tx.Get(ctx, "content.holders", in.CopyID, &old)
	if err == nil {
		if old.PrincipalID != auth.SubjectID || !api.Equal(old.ContentRef, in.ContentRef) || !api.Equal(old.HolderRef, in.HolderRef) || old.Purpose != in.Purpose || old.Location != in.Location || old.RetainUntil != in.RetainUntil || !api.Equal(old.ReferenceIntentRef, in.ReferenceIntentRef) {
			return CopyOutput{}, api.E("idempotency_conflict", "copy_input_changed")
		}
		return copyOutput(old), nil
	}
	if !api.IsCode(err, "not_found") {
		return CopyOutput{}, err
	}
	h := CopyHolder{CopyID: in.CopyID, Revision: 1, ContentRef: in.ContentRef, HolderRef: in.HolderRef, Purpose: in.Purpose, Location: in.Location, PolicyRef: v.PolicyRef, RetainUntil: in.RetainUntil, ReferenceIntentRef: in.ReferenceIntentRef, UseState: "allowed", CleanupState: "pending", ControlRevision: v.ControlRevision, EvidenceRefs: []api.ContentRef{}, PrincipalID: auth.SubjectID, Kind: "external"}
	if err = tx.Create(ctx, "content.holders", h.CopyID, contentKey(in.ContentRef), h); err != nil {
		return CopyOutput{}, err
	}
	if _, err = tx.Raise(ctx, "content.copy_expire", h.CopyID, tx.Scope().Ref(h.CopyID, 1), retain); err != nil {
		return CopyOutput{}, err
	}
	return copyOutput(h), nil
}

type CloseInput struct {
	ContentRef api.ContentRef `json:"content_ref"`
	Reason     string         `json:"reason"`
}
type CloseOutput struct {
	ControlRevision uint64 `json:"control_revision"`
	State           string `json:"state"`
	CleanupState    string `json:"cleanup_state"`
}

func (s *Service) close(ctx context.Context, tx runtime.Tx, auth runtime.Auth, expected *uint64, in CloseInput) (CloseOutput, error) {
	if err := checkContentRef(tx.Scope(), in.ContentRef); err != nil {
		return CloseOutput{}, err
	}
	// Memory head 在内容门禁之前取得，保证同库变化和影响责任串行。
	head, err := loadHead(ctx, tx)
	if err != nil {
		return CloseOutput{}, err
	}
	var v ContentVersion
	rev, err := tx.Get(ctx, "content.versions", contentKey(in.ContentRef), &v)
	if err != nil {
		return CloseOutput{}, err
	}
	if err = checkAuth(tx.Scope(), auth); err != nil {
		return CloseOutput{}, err
	}
	if v.PublisherID != auth.SubjectID && !auth.HasRole("content_admin") {
		return CloseOutput{}, api.E("forbidden", "content_management_required")
	}
	if !api.Equal(v.ContentRef, in.ContentRef) {
		return CloseOutput{}, api.E("idempotency_conflict", "content_reference_changed")
	}
	if err = compareExpected(expected, v.ControlRevision); err != nil {
		return CloseOutput{}, err
	}
	if (v.State == "closed" || v.State == "deleted") && v.ClosureKind != "retention" {
		return CloseOutput{v.ControlRevision, v.State, "pending"}, nil
	}
	if v.State != "deleted" {
		v.State = "closed"
	}
	v.ClosureKind = "active_close"
	v.ControlRevision++
	if err = tx.Put(ctx, "content.versions", contentKey(in.ContentRef), rev, v); err != nil {
		return CloseOutput{}, err
	}
	head.VisibilityRevision++
	if err = saveHead(ctx, tx, head); err != nil {
		return CloseOutput{}, err
	}
	now, err := tx.Now(ctx)
	if err != nil {
		return CloseOutput{}, err
	}
	notice, err := contentControlNotice(ctx, tx, v)
	if err != nil {
		return CloseOutput{}, err
	}
	if _, err = tx.Raise(ctx, "content.cleanup", contentKey(in.ContentRef), notice, now); err != nil {
		return CloseOutput{}, err
	}
	if _, err = tx.Raise(ctx, "memory.source_impact", contentKey(in.ContentRef), notice, now); err != nil {
		return CloseOutput{}, err
	}
	return CloseOutput{v.ControlRevision, v.State, "pending"}, nil
}

type ReleaseCopyInput struct {
	CopyID          string           `json:"copy_id"`
	ContentRef      api.ContentRef   `json:"content_ref"`
	ControlRevision uint64           `json:"control_revision"`
	UseStopped      bool             `json:"use_stopped"`
	CleanupState    string           `json:"cleanup_state"`
	EvidenceRefs    []api.ContentRef `json:"evidence_refs"`
	ResidualReason  string           `json:"residual_reason,omitempty"`
}

func (s *Service) releaseCopy(ctx context.Context, tx runtime.Tx, auth runtime.Auth, in ReleaseCopyInput) (CopyOutput, error) {
	if err := checkAuth(tx.Scope(), auth); err != nil {
		return CopyOutput{}, err
	}
	var h CopyHolder
	rev, err := tx.Get(ctx, "content.holders", in.CopyID, &h)
	if err != nil {
		return CopyOutput{}, err
	}
	if h.PrincipalID != auth.SubjectID || !api.Equal(h.ContentRef, in.ContentRef) {
		return CopyOutput{}, api.E("forbidden", "copy_holder_mismatch")
	}
	var v ContentVersion
	_, err = tx.Get(ctx, "content.versions", contentKey(in.ContentRef), &v)
	if err != nil {
		return CopyOutput{}, err
	}
	if in.ControlRevision != v.ControlRevision {
		return CopyOutput{}, runtime.ErrConflict
	}
	if !contains([]string{"pending", "complete", "residual", "unknown"}, in.CleanupState) || in.CleanupState == "complete" && (!in.UseStopped || len(in.EvidenceRefs) == 0) || in.CleanupState == "residual" && in.ResidualReason == "" {
		return CopyOutput{}, api.E("invalid_request", "invalid_cleanup_report")
	}
	if h.UseState == "use_stopped" && !in.UseStopped {
		return CopyOutput{}, api.E("invalid_state", "copy_cannot_resume")
	}
	if h.CleanupState == "complete" && in.CleanupState != "complete" {
		return CopyOutput{}, api.E("invalid_state", "cleanup_cannot_regress")
	}
	for _, evidence := range in.EvidenceRefs {
		if err = checkContentRef(tx.Scope(), evidence); err != nil {
			return CopyOutput{}, err
		}
		var receipt ContentVersion
		if _, err = tx.Get(ctx, "content.versions", contentKey(evidence), &receipt); err != nil {
			return CopyOutput{}, err
		}
		if !api.Equal(receipt.ContentRef, evidence) {
			return CopyOutput{}, api.E("idempotency_conflict", "cleanup_evidence_changed")
		}
	}
	h.ControlRevision = v.ControlRevision
	h.CleanupState = in.CleanupState
	h.EvidenceRefs = in.EvidenceRefs
	h.ResidualReason = in.ResidualReason
	h.Revision = rev + 1
	if in.UseStopped {
		h.UseState = "use_stopped"
	}
	if err = tx.Put(ctx, "content.holders", h.CopyID, rev, h); err != nil {
		return CopyOutput{}, err
	}
	if v.State != "published" {
		now, err := tx.Now(ctx)
		if err != nil {
			return CopyOutput{}, err
		}
		if _, err = tx.Raise(ctx, "content.cleanup", contentKey(in.ContentRef), tx.Scope().Ref(h.CopyID, h.Revision), now); err != nil {
			return CopyOutput{}, err
		}
	}
	return copyOutput(h), nil
}

type GetContentInput struct {
	ContentRef api.ContentRef `json:"content_ref"`
	Mode       string         `json:"mode"`
	CopyID     string         `json:"copy_id,omitempty"`
	Purpose    string         `json:"purpose,omitempty"`
	Location   string         `json:"location,omitempty"`
}
type GetContentOutput struct {
	ContentRef      api.ContentRef `json:"content_ref"`
	Mode            string         `json:"mode"`
	BytesBase64     string         `json:"bytes_base64,omitempty"`
	ControlRevision uint64         `json:"control_revision"`
	UseState        string         `json:"use_state,omitempty"`
	CleanupState    string         `json:"cleanup_state,omitempty"`
}

func (s *Service) getContent(ctx context.Context, scope runtime.Scope, auth runtime.Auth, in GetContentInput) (GetContentOutput, error) {
	if in.Mode == "bytes" {
		if in.Purpose == "" || in.Location == "" {
			return GetContentOutput{}, api.E("invalid_request", "read_scope_required")
		}
		// 线协议响应有界；大正文通过独立准确字节端口读取，不伪造无限票据。
		if in.ContentRef.ByteLength > 2048 {
			return GetContentOutput{}, api.E("unsupported", "use_bounded_byte_transport")
		}
		body, err := s.ReadBytes(ctx, scope, auth, in.ContentRef, in.Purpose, in.Location)
		if err != nil {
			return GetContentOutput{}, err
		}
		return GetContentOutput{ContentRef: in.ContentRef, Mode: "bytes", BytesBase64: base64.StdEncoding.EncodeToString(body), ControlRevision: 0}, nil
	}
	if in.Mode != "control" || in.CopyID == "" {
		return GetContentOutput{}, api.E("invalid_request", "control_copy_required")
	}
	var out GetContentOutput
	err := s.authWithin(ctx, scope, auth, func(tx runtime.Tx) error {
		if err := checkAuth(scope, auth); err != nil {
			return err
		}
		var h CopyHolder
		_, err := tx.Get(ctx, "content.holders", in.CopyID, &h)
		if err != nil {
			return err
		}
		if h.PrincipalID != auth.SubjectID || !api.Equal(h.ContentRef, in.ContentRef) {
			return api.E("forbidden", "copy_holder_mismatch")
		}
		var v ContentVersion
		_, err = tx.Get(ctx, "content.versions", contentKey(in.ContentRef), &v)
		if err != nil {
			return err
		}
		state := h.UseState
		now, err := tx.Now(ctx)
		if err != nil {
			return err
		}
		deadline, err := api.ParseTime(h.RetainUntil)
		if err != nil {
			return err
		}
		if (v.State != "published" || !now.Before(deadline)) && state == "allowed" {
			state = "closing"
		}
		out = GetContentOutput{ContentRef: h.ContentRef, Mode: "control", ControlRevision: v.ControlRevision, UseState: state, CleanupState: h.CleanupState}
		return nil
	})
	return out, err
}

type MirrorInput struct {
	TransferID         string         `json:"transfer_id"`
	ContentRef         api.ContentRef `json:"content_ref"`
	SourceHolder       api.ObjectRef  `json:"source_holder"`
	TargetHolder       api.ObjectRef  `json:"target_holder"`
	Purpose            string         `json:"purpose"`
	Location           string         `json:"location"`
	MaxBytes           uint64         `json:"max_bytes"`
	ExpiresAt          string         `json:"expires_at"`
	ReferenceIntentRef api.ObjectRef  `json:"reference_intent_ref"`
}

func (s *Service) mirror(ctx context.Context, tx runtime.Tx, auth runtime.Auth, c api.Command, in MirrorInput) (ReserveOutput, error) {
	if err := runtime.CheckRef(tx.Scope(), in.SourceHolder); err != nil {
		return ReserveOutput{}, err
	}
	if err := runtime.CheckRef(tx.Scope(), in.TargetHolder); err != nil {
		return ReserveOutput{}, err
	}
	if err := runtime.CheckRef(tx.Scope(), in.ReferenceIntentRef); err != nil {
		return ReserveOutput{}, err
	}
	if in.SourceHolder.ObjectID != auth.SubjectID || in.TargetHolder.ObjectID != auth.SubjectID || in.SourceHolder.Revision != auth.CredentialGeneration || in.TargetHolder.Revision != auth.CredentialGeneration || !api.ValidID(in.TransferID) || in.MaxBytes < in.ContentRef.ByteLength || in.MaxBytes > MaxContentBytes {
		return ReserveOutput{}, api.E("forbidden", "mirror_scope_mismatch")
	}
	if in.Location != s.Location {
		return ReserveOutput{}, api.E("unsupported", "mirror_target_location_unconfigured")
	}
	v, err := s.CheckContentTx(ctx, tx, auth, in.ContentRef, in.Purpose, in.Location, false)
	if err != nil {
		return ReserveOutput{}, err
	}
	expires, err := future(ctx, tx, in.ExpiresAt)
	if err != nil {
		return ReserveOutput{}, err
	}
	retention, _ := api.ParseTime(v.RetentionUntil)
	if expires.After(retention) {
		return ReserveOutput{}, api.E("forbidden", "retention_scope_expansion")
	}
	digest, _ := api.Digest(in)
	var old Transfer
	_, err = tx.Get(ctx, "content.transfers", in.TransferID, &old)
	if err == nil {
		if err = tx.Bind(ctx, "content.transfers", "mirror:"+in.TransferID, in.TransferID, digest); err != nil {
			return ReserveOutput{}, err
		}
		if old.Kind != "mirror" {
			return ReserveOutput{}, api.E("idempotency_conflict", "transfer_input_changed")
		}
		return reserveResult(old), nil
	}
	if !api.IsCode(err, "not_found") {
		return ReserveOutput{}, err
	}
	t := Transfer{TransferID: in.TransferID, Revision: 1, Kind: "mirror", CommandRef: tx.Scope().Ref(c.CommandID, 1), ContentRef: in.ContentRef, PolicyRef: v.PolicyRef, ProcessedSources: v.ProcessedSources, RetentionUntil: v.RetentionUntil, ExpiresAt: in.ExpiresAt, Phase: "reserved", PublisherID: auth.SubjectID, SourceHolder: &in.SourceHolder, TargetHolder: in.TargetHolder, ReferenceIntentRef: in.ReferenceIntentRef, MaxBytes: in.MaxBytes, Purpose: in.Purpose, TargetLocation: in.Location}
	if err = tx.Create(ctx, "content.transfers", in.TransferID, contentKey(in.ContentRef), t); err != nil {
		return ReserveOutput{}, err
	}
	if err = tx.Bind(ctx, "content.transfers", "mirror:"+in.TransferID, in.TransferID, digest); err != nil {
		return ReserveOutput{}, err
	}
	if _, err = tx.Raise(ctx, "content.transfer_cleanup", in.TransferID, tx.Scope().Ref(in.TransferID, 1), expires); err != nil {
		return ReserveOutput{}, err
	}
	return reserveResult(t), nil
}

// TicketDuration 的默认仅供调用者固定开发期限，读取不会延长已有期限。
const TicketDuration = 5 * time.Minute
