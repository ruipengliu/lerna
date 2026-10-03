package providers

import (
	"context"
	"encoding/base64"
	"fmt"
	"sort"
	"time"

	"github.com/ruipengliu/lerna/adapters/platform"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/memory"
	"github.com/ruipengliu/lerna/runtime"
)

const ForeignContentChunkBytes = 64 << 10

// ForeignSourceAuthority 由宿主核原受信peer、已登记consumer/subject范围和当前凭据。
// 返回的主体来自当前权威记录，不从请求角色/JSON制造。此接口必须是纯Tx。
type ForeignSourceAuthority interface {
	ResolveSourceSubjectTx(context.Context, runtime.Tx, runtime.Auth, memory.ForeignReference, bool) (runtime.Auth, error)
}
type ForeignSourceConfig struct {
	Store        runtime.Store
	Scope        runtime.Scope
	Memory       *memory.Service
	Keys         *platform.Keyring
	SigningKeyID string
	Authority    ForeignSourceAuthority
	Participants []string
}
type ForeignSource struct {
	cfg      ForeignSourceConfig
	parts    []string
	registry *runtime.Registry
}
type ForeignSourceCurrent struct {
	Reference memory.ForeignReference `json:"reference"`
	Control   bool                    `json:"control"`
}
type ForeignSourceGet struct {
	Reference  memory.ForeignReference `json:"reference"`
	ChunkIndex uint64                  `json:"chunk_index"`
}
type ForeignSourceChunk struct {
	ContentRef api.ContentRef `json:"content_ref"`
	ChunkIndex uint64         `json:"chunk_index"`
	ChunkCount uint64         `json:"chunk_count"`
	DataBase64 string         `json:"data_base64"`
}
type ForeignSourceRelease struct {
	Reference memory.ForeignReference `json:"reference"`
	Report    memory.ReleaseCopyInput `json:"report"`
}
type foreignSourceHolder struct {
	Reference     memory.ForeignReference `json:"reference"`
	PeerSubjectID string                  `json:"peer_subject_id"`
}

func NewForeignSource(c ForeignSourceConfig) (*ForeignSource, error) {
	if c.Store == nil || c.Memory == nil || c.Memory.Store == nil || c.Memory.Store.ID() != c.Store.ID() || c.Scope.DatabaseID != c.Store.ID() || !api.ValidID(c.Scope.OwnerID) || !api.ValidID(c.Scope.TenantID) || c.Keys == nil || c.SigningKeyID == "" || c.Authority == nil {
		return nil, api.E("unsupported", "foreign_source_authority_unconfigured")
	}
	k, ok := c.Keys.Keys[c.SigningKeyID]
	if !ok || k.Private == nil || k.TenantID != c.Scope.TenantID || k.Issuer != c.Scope.OwnerID {
		return nil, api.E("forbidden", "foreign_source_signer_not_paired")
	}
	parts := map[string]bool{"providers": true, "content": true, "memory": true}
	for _, part := range append(append([]string{}, c.Memory.Participants...), c.Participants...) {
		if part == "" {
			return nil, api.E("invalid_request", "foreign_participant_required")
		}
		parts[part] = true
	}
	var list []string
	for p := range parts {
		list = append(list, p)
	}
	sort.Strings(list)
	return &ForeignSource{cfg: c, parts: list}, nil
}
func (s *ForeignSource) within(ctx context.Context, fn func(runtime.Tx) error) error {
	status, err := s.cfg.Store.Within(ctx, s.cfg.Scope, s.parts, fn)
	if status == runtime.CommitUnknown {
		return runtime.ErrCommitUnknown
	}
	return err
}
func (s *ForeignSource) reference(r memory.ForeignReference) error {
	if api.ValidateRecord("ContentRef", r.ContentRef) != nil || r.ContentRef.TenantID != s.cfg.Scope.TenantID || r.ContentRef.OwnerID != s.cfg.Scope.OwnerID || r.ContentRef.ByteLength > memory.MaxContentBytes || !api.ValidID(r.CopyID) || !api.ValidID(r.RegisterCommandID) || !api.ValidID(r.ReleaseCommandID) || r.ReleaseCommandID == r.RegisterCommandID || api.ValidateRecord("ObjectRef", r.HolderRef) != nil || api.ValidateRecord("ObjectRef", r.ReferenceIntentRef) != nil || r.HolderRef.TenantID != s.cfg.Scope.TenantID || r.ReferenceIntentRef.TenantID != s.cfg.Scope.TenantID || r.HolderRef.OwnerID != r.ReferenceIntentRef.OwnerID || r.HolderRef.OwnerID == s.cfg.Scope.OwnerID || r.Purpose == "" || r.Location == "" {
		return api.E("forbidden", "foreign_source_reference_scope_mismatch")
	}
	_, err := api.ParseTime(r.RetainUntil)
	return err
}
func (s *ForeignSource) subject(ctx context.Context, tx runtime.Tx, peer runtime.Auth, r memory.ForeignReference, control, registered bool) (runtime.Auth, error) {
	if err := s.reference(r); err != nil {
		return runtime.Auth{}, err
	}
	if tx.Scope() != s.cfg.Scope {
		return runtime.Auth{}, api.E("forbidden", "foreign_source_database_mismatch")
	}
	a, err := s.cfg.Authority.ResolveSourceSubjectTx(ctx, tx, peer, r, control)
	if err != nil {
		return a, err
	}
	if a.TenantID != r.ContentRef.TenantID || a.SubjectID != r.HolderRef.ObjectID || a.CredentialGeneration == 0 || (!control && a.CredentialGeneration != r.HolderRef.Revision) || control && a.CredentialGeneration < r.HolderRef.Revision {
		return a, api.E("forbidden", "foreign_source_subject_mismatch")
	}
	if registered {
		var saved foreignSourceHolder
		if _, err = tx.Get(ctx, "providers.foreign_holders", r.CopyID, &saved); err != nil {
			return a, err
		}
		if !api.Equal(saved.Reference, r) || saved.PeerSubjectID != peer.SubjectID {
			return a, api.E("idempotency_conflict", "original_foreign_holder_changed")
		}
	}
	return a, nil
}
func sourceCommand[I any](s *ForeignSource, r *runtime.Registry, contract api.MethodContract, fn func(context.Context, runtime.Tx, runtime.Auth, api.Command, I) (runtime.Outcome, error)) error {
	return r.Register(runtime.Method{Contract: contract, Participants: s.parts, Apply: func(ctx context.Context, tx runtime.Tx, a runtime.Auth, c api.Command) (runtime.Outcome, error) {
		var in I
		if err := api.Decode(c.Payload, &in); err != nil {
			return runtime.Outcome{}, err
		}
		return fn(ctx, tx, a, c, in)
	}})
}
func sourceQuery[I, O any](s *ForeignSource, r *runtime.Registry, contract api.MethodContract, fn func(context.Context, runtime.Auth, api.Query, I) (O, error)) error {
	return r.Register(runtime.Method{Contract: contract, Query: func(ctx context.Context, st runtime.Store, scope runtime.Scope, a runtime.Auth, q api.Query) (any, error) {
		if scope != s.cfg.Scope || st.ID() != s.cfg.Store.ID() {
			return nil, api.E("forbidden", "foreign_source_scope_mismatch")
		}
		var in I
		if err := api.Decode(q.Payload, &in); err != nil {
			return nil, err
		}
		return fn(ctx, a, q, in)
	}})
}

// ForeignSourceContracts 无构造、数据库或出站依赖；生成器和实际注册共用这一合同源。
// 每次返回独立 Schema，调用方不能修改后续源端登记。
func ForeignSourceContracts() []api.MethodContract {
	contracts := []api.MethodContract{
		api.Contract[memory.ForeignReference, memory.CopyOutput]("content.foreign.register", "content", "command", false, false),
		api.Contract[ForeignSourceRelease, memory.CopyOutput]("content.foreign.release", "content", "command", false, false),
		api.Contract[ForeignSourceCurrent, memory.ForeignProof]("content.foreign.current", "content", "query", false, false),
		api.Contract[ForeignSourceGet, ForeignSourceChunk]("content.foreign.get", "content", "query", false, false),
	}
	contracts[2].OutputSchema["properties"].(map[string]any)["proof"] = api.Schema{"type": "string", "maxLength": 32768}
	contracts[3].OutputSchema["properties"].(map[string]any)["data_base64"] = api.Schema{"type": "string", "maxLength": (ForeignContentChunkBytes + 2) / 3 * 4}
	return contracts
}

func (s *ForeignSource) Register(r *runtime.Registry) error {
	if r == nil || s.registry != nil {
		return api.E("invalid_state", "foreign_source_registry_fixed")
	}
	for _, name := range []string{"content.register_copy", "content.release_copy"} {
		if _, ok := r.Method(name); !ok {
			return fmt.Errorf("foreign source requires registered %s", name)
		}
	}
	s.registry = r
	contracts := ForeignSourceContracts()
	if err := sourceCommand(s, r, contracts[0], s.register); err != nil {
		return err
	}
	if err := sourceCommand(s, r, contracts[1], s.release); err != nil {
		return err
	}
	if err := sourceQuery(s, r, contracts[2], s.current); err != nil {
		return err
	}
	return sourceQuery(s, r, contracts[3], s.get)
}
func (s *ForeignSource) applyOriginal(ctx context.Context, tx runtime.Tx, a runtime.Auth, c api.Command, method string, payload any) (runtime.Outcome, error) {
	m, ok := s.registry.Method(method)
	if !ok {
		return runtime.Outcome{}, api.E("unsupported", "source_original_method_missing")
	}
	c.Method = method
	c.Payload = api.Raw(payload)
	return m.Apply(ctx, tx, a, c)
}
func (s *ForeignSource) register(ctx context.Context, tx runtime.Tx, peer runtime.Auth, c api.Command, in memory.ForeignReference) (runtime.Outcome, error) {
	a, err := s.subject(ctx, tx, peer, in, false, false)
	if err != nil {
		return runtime.Outcome{}, err
	}
	if c.CommandID != in.RegisterCommandID || c.TargetID != in.ContentRef.ContentID || c.ExpiresAt != in.RetainUntil {
		return runtime.Outcome{}, api.E("invalid_request", "original_foreign_register_command_changed")
	}
	if _, err = s.cfg.Memory.CheckContentTx(ctx, tx, a, in.ContentRef, in.Purpose, in.Location, false); err != nil {
		return runtime.Outcome{}, err
	}
	var old foreignSourceHolder
	_, err = tx.Get(ctx, "providers.foreign_holders", in.CopyID, &old)
	if err == nil {
		if !api.Equal(old.Reference, in) || old.PeerSubjectID != peer.SubjectID {
			return runtime.Outcome{}, api.E("idempotency_conflict", "original_foreign_holder_changed")
		}
	} else if api.IsCode(err, "not_found") {
		err = tx.Create(ctx, "providers.foreign_holders", in.CopyID, in.HolderRef.OwnerID, foreignSourceHolder{in, peer.SubjectID})
	} else {
		return runtime.Outcome{}, err
	}
	if err != nil {
		return runtime.Outcome{}, err
	}
	return s.applyOriginal(ctx, tx, a, c, "content.register_copy", memory.RegisterCopyInput{CopyID: in.CopyID, ContentRef: in.ContentRef, HolderRef: in.HolderRef, Purpose: in.Purpose, Location: in.Location, RetainUntil: in.RetainUntil, ReferenceIntentRef: in.ReferenceIntentRef})
}
func (s *ForeignSource) release(ctx context.Context, tx runtime.Tx, peer runtime.Auth, c api.Command, in ForeignSourceRelease) (runtime.Outcome, error) {
	a, err := s.subject(ctx, tx, peer, in.Reference, true, true)
	if err != nil {
		return runtime.Outcome{}, err
	}
	if c.CommandID != in.Reference.ReleaseCommandID || c.TargetID != in.Reference.ContentRef.ContentID || in.Report.CopyID != in.Reference.CopyID || in.Report.ContentRef != in.Reference.ContentRef {
		return runtime.Outcome{}, api.E("invalid_request", "original_foreign_release_command_changed")
	}
	return s.applyOriginal(ctx, tx, a, c, "content.release_copy", in.Report)
}
func ForeignProofDigest(p memory.ForeignProof) (string, error) { p.Proof = ""; return api.Digest(p) }
func foreignProofClaims(p memory.ForeignProof, digest string) platform.ProofClaims {
	return platform.ProofClaims{TenantID: p.ContentRef.TenantID, Issuer: p.ContentRef.OwnerID, Audience: p.HolderRef.OwnerID, Purpose: "foreign_content", ObjectRef: api.ObjectRef{TenantID: p.ContentRef.TenantID, OwnerID: p.ContentRef.OwnerID, ObjectID: p.ContentRef.ContentID, Revision: p.ContentRef.Version}, Digest: digest, ControlRevision: p.ControlRevision, WindowID: p.CopyID, IssuedAt: p.IssuedAt, StartBefore: p.StartBefore}
}
func (s *ForeignSource) proof(ctx context.Context, peer runtime.Auth, in memory.ForeignReference, control bool) (memory.ForeignProof, error) {
	var a runtime.Auth
	err := s.within(ctx, func(tx runtime.Tx) error {
		var err error
		a, err = s.subject(ctx, tx, peer, in, control, true)
		return err
	})
	if err != nil {
		return memory.ForeignProof{}, err
	}
	p, err := s.cfg.Memory.CurrentForeignCopy(ctx, s.cfg.Scope, a, in, control)
	if err != nil {
		return p, err
	}
	digest, err := ForeignProofDigest(p)
	if err != nil {
		return p, err
	}
	p.Proof, err = s.cfg.Keys.Sign(s.cfg.SigningKeyID, foreignProofClaims(p, digest))
	return p, err
}
func (s *ForeignSource) current(ctx context.Context, a runtime.Auth, q api.Query, in ForeignSourceCurrent) (memory.ForeignProof, error) {
	if q.TargetID != in.Reference.ContentRef.ContentID {
		return memory.ForeignProof{}, api.E("invalid_request", "target_mismatch")
	}
	return s.proof(ctx, a, in.Reference, in.Control)
}
func (s *ForeignSource) get(ctx context.Context, peer runtime.Auth, q api.Query, in ForeignSourceGet) (ForeignSourceChunk, error) {
	r := in.Reference
	if q.TargetID != r.ContentRef.ContentID {
		return ForeignSourceChunk{}, api.E("invalid_request", "target_mismatch")
	}
	count := (r.ContentRef.ByteLength + ForeignContentChunkBytes - 1) / ForeignContentChunkBytes
	if count == 0 {
		count = 1
	}
	if in.ChunkIndex >= count || count > 256 {
		return ForeignSourceChunk{}, api.E("invalid_request", "foreign_chunk_limit")
	}
	var a runtime.Auth
	err := s.within(ctx, func(tx runtime.Tx) error {
		var err error
		a, err = s.subject(ctx, tx, peer, r, false, true)
		return err
	})
	if err != nil {
		return ForeignSourceChunk{}, err
	}
	p, err := s.cfg.Memory.CurrentForeignCopy(ctx, s.cfg.Scope, a, r, false)
	if err != nil {
		return ForeignSourceChunk{}, err
	}
	now := time.Now()
	retain, _ := api.ParseTime(p.RetainUntil)
	if p.UseState != "allowed" || p.SourceState != "published" || !now.Before(retain) {
		return ForeignSourceChunk{}, api.E("forbidden", "foreign_copy_not_current")
	}
	body, err := s.cfg.Memory.ReadBytes(ctx, s.cfg.Scope, a, r.ContentRef, r.Purpose, r.Location)
	if err != nil {
		return ForeignSourceChunk{}, err
	}
	p, err = s.cfg.Memory.CurrentForeignCopy(ctx, s.cfg.Scope, a, r, false)
	if err != nil {
		return ForeignSourceChunk{}, err
	}
	if p.UseState != "allowed" || p.SourceState != "published" {
		return ForeignSourceChunk{}, api.E("forbidden", "foreign_copy_not_current")
	}
	start := in.ChunkIndex * ForeignContentChunkBytes
	end := start + ForeignContentChunkBytes
	if end > uint64(len(body)) {
		end = uint64(len(body))
	}
	return ForeignSourceChunk{r.ContentRef, in.ChunkIndex, count, base64.StdEncoding.EncodeToString(body[start:end])}, nil
}
