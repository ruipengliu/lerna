package executor

import (
	"context"
	"encoding/base64"
	"errors"
	"os"
	"time"

	"github.com/ruipengliu/lerna/adapters/platform"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/memory"
	"github.com/ruipengliu/lerna/runtime"
)

// SourceClient 消费设备的准确来源合同；固定公钥来自管理配对，不从响应取钥。
// Memory 自己的 held_copy_gate/当前主体门禁在 VerifyTx 之后继续裁决；本端口不做RPC。
type SourceClient struct {
	Client *Client
	Keys   *platform.Keyring
}

var _ memory.ForeignContentPort = (*SourceClient)(nil)

func (c *SourceClient) reference(scope runtime.Scope, a runtime.Auth, r memory.ForeignReference, control bool) error {
	sameHolder := api.Equal(r.HolderRef, a.Ref(scope.OwnerID))
	if control {
		sameHolder = r.HolderRef.TenantID == a.TenantID && r.HolderRef.OwnerID == scope.OwnerID && r.HolderRef.ObjectID == a.SubjectID && r.HolderRef.Revision > 0 && a.CredentialGeneration >= r.HolderRef.Revision
	}
	if c == nil || c.Client == nil || c.Keys == nil || scope.TenantID != c.Client.Config.TenantID || scope.OwnerID != c.Client.Config.AuthorityID || a.TenantID != scope.TenantID || a.CredentialGeneration == 0 || !sameHolder || r.ReferenceIntentRef.TenantID != scope.TenantID || r.ReferenceIntentRef.OwnerID != scope.OwnerID || api.ValidateRecord("ObjectRef", r.ReferenceIntentRef) != nil || r.ContentRef.TenantID != scope.TenantID || r.ContentRef.OwnerID != c.Client.Config.OwnerID || api.ValidateRecord("ContentRef", r.ContentRef) != nil || r.ContentRef.ByteLength > MaxContentBytes || !api.ValidID(r.CopyID) || !api.ValidID(r.RegisterCommandID) || !api.ValidID(r.ReleaseCommandID) || r.Purpose == "" || r.Location == "" {
		return api.E("forbidden", "original_foreign_reference_scope_mismatch")
	}
	return nil
}
func (c *SourceClient) verify(scope runtime.Scope, a runtime.Auth, r memory.ForeignReference, p memory.ForeignProof, now time.Time, control bool) error {
	if err := c.reference(scope, a, r, control); err != nil {
		return err
	}
	if !api.Equal(p.ContentRef, r.ContentRef) || p.SourceDatabaseID != c.Client.Config.DatabaseID || !api.Equal(p.SubjectRef, r.HolderRef) || !api.Equal(p.HolderRef, r.HolderRef) || !api.Equal(p.ReferenceIntentRef, r.ReferenceIntentRef) || p.CopyID != r.CopyID || p.Purpose != r.Purpose || p.Location != r.Location || p.ControlRevision == 0 || api.ValidateRecord("ComponentRef", p.PolicyRef) != nil || p.Continuous != p.PolicyValues.Continuous || p.IndependentDerived != p.PolicyValues.IndependentDerived {
		return api.E("forbidden", "original_foreign_proof_binding_mismatch")
	}
	digest, err := ForeignContentDigest(p)
	if err != nil {
		return err
	}
	claims := foreignContentClaims(p, scope.OwnerID, digest)
	if control {
		if p.Mode != "control" {
			return api.E("forbidden", "foreign_control_proof_required")
		}
		_, err = c.Keys.VerifySource(p.Proof, claims)
		return err
	}
	if p.Mode != "use" || p.SourceState != "published" || p.UseState != "allowed" || p.CleanupState == "complete" || !has(p.PolicyValues.Subjects, a.SubjectID) || !has(p.PolicyValues.Purposes, r.Purpose) || !has(p.PolicyValues.Locations, r.Location) {
		return api.E("forbidden", "foreign_current_use_denied")
	}
	if policyDigest, e := api.Digest(p.PolicyValues); e != nil || policyDigest != p.PolicyRef.Digest {
		return api.E("forbidden", "foreign_source_policy_digest_mismatch")
	}
	if err = before(now, earliest(p.RetainUntil, p.PolicyValues.RetainUntil, r.RetainUntil)); err != nil {
		return err
	}
	if p.RetainUntil != earliest(p.RetainUntil, p.PolicyValues.RetainUntil, r.RetainUntil) || p.StartBefore != earliest(p.StartBefore, p.RetainUntil) {
		return api.E("forbidden", "foreign_retention_scope_expansion")
	}
	_, err = c.Keys.Verify(p.Proof, claims, now)
	return err
}
func (c *SourceClient) VerifyTx(ctx context.Context, tx runtime.Tx, a runtime.Auth, r memory.ForeignReference, p memory.ForeignProof) error {
	now, err := tx.Now(ctx)
	if err != nil {
		return err
	}
	// 验真同样用于原副本收尾；mode签入完整body，Memory自己的use gate
	// 仍只接受mode=use。control证明只能保存原停止事实，不能升级成正文许可。
	return c.verify(tx.Scope(), a, r, p, now, p.Mode == "control")
}
func (c *SourceClient) RegisterCopy(ctx context.Context, scope runtime.Scope, a runtime.Auth, r memory.ForeignReference) (memory.ForeignProof, error) {
	if err := c.reference(scope, a, r, false); err != nil {
		return memory.ForeignProof{}, err
	}
	in := memory.RegisterCopyInput{CopyID: r.CopyID, ContentRef: r.ContentRef, HolderRef: r.HolderRef, Purpose: r.Purpose, Location: r.Location, RetainUntil: r.RetainUntil, ReferenceIntentRef: r.ReferenceIntentRef}
	command := commandFor(c.Client.Config.OwnerID, r.RegisterCommandID, "content.register_copy", r.ContentRef.ContentID, r.RetainUntil, in)
	if err := applied(c.Client.SDK.Send(ctx, command)); err != nil {
		return memory.ForeignProof{}, err
	}
	return c.Current(ctx, scope, a, r)
}
func (c *SourceClient) current(ctx context.Context, scope runtime.Scope, a runtime.Auth, r memory.ForeignReference, control bool) (memory.ForeignProof, error) {
	var p memory.ForeignProof
	if err := c.reference(scope, a, r, control); err != nil {
		return p, err
	}
	if err := c.Client.query(ctx, "executor.content.current", r.ContentRef.ContentID, SourceCurrent{Reference: r, Control: control}, &p); err != nil {
		return p, err
	}
	return p, c.verify(scope, a, r, p, time.Now(), control)
}
func (c *SourceClient) Current(ctx context.Context, scope runtime.Scope, a runtime.Auth, r memory.ForeignReference) (memory.ForeignProof, error) {
	return c.current(ctx, scope, a, r, false)
}
func (c *SourceClient) Control(ctx context.Context, scope runtime.Scope, a runtime.Auth, r memory.ForeignReference) (memory.ForeignProof, error) {
	return c.current(ctx, scope, a, r, true)
}
func (c *SourceClient) Read(ctx context.Context, scope runtime.Scope, a runtime.Auth, r memory.ForeignReference, p memory.ForeignProof) ([]byte, error) {
	if err := c.verify(scope, a, r, p, time.Now(), false); err != nil {
		return nil, err
	}
	raw := make([]byte, 0, r.ContentRef.ByteLength)
	var permission ContentPermission
	for index := uint64(0); index < chunkCount(r.ContentRef.ByteLength); index++ {
		var chunk ContentChunk
		if err := c.Client.query(ctx, "executor.content.get", r.ContentRef.ContentID, ContentGet{ContentRef: r.ContentRef, ChunkIndex: index, Reference: &r}, &chunk); err != nil {
			return nil, err
		}
		if !api.Equal(chunk.Permission.ContentRef, r.ContentRef) || chunk.ChunkIndex != index || chunk.ChunkCount != chunkCount(r.ContentRef.ByteLength) || !api.Equal(chunk.Permission.ProcessedSources, p.ProcessedSources) || !api.Equal(chunk.Permission.DisclosedSources, p.DisclosedSources) {
			return nil, api.E("forbidden", "original_registered_bytes_binding_mismatch")
		}
		if index == 0 {
			permission = chunk.Permission
		} else if !api.Equal(permission, chunk.Permission) {
			return nil, api.E("forbidden", "original_source_metadata_changed")
		}
		part, err := base64.StdEncoding.Strict().DecodeString(chunk.DataBase64)
		expected := uint64(ChunkBytes)
		if index == chunkCount(r.ContentRef.ByteLength)-1 {
			expected = r.ContentRef.ByteLength - index*ChunkBytes
		}
		if err != nil || uint64(len(part)) != expected {
			return nil, api.E("invalid_request", "original_registered_chunk_invalid")
		}
		raw = append(raw, part...)
	}
	if uint64(len(raw)) != r.ContentRef.ByteLength || api.Hash(raw) != r.ContentRef.Hash {
		return nil, api.E("forbidden", "original_registered_hash_mismatch")
	}
	if err := c.verify(scope, a, r, p, time.Now(), false); err != nil {
		return nil, err
	}
	return raw, nil
}
func (c *SourceClient) Release(ctx context.Context, scope runtime.Scope, a runtime.Auth, r memory.ForeignReference, in memory.ReleaseCopyInput) (memory.ForeignProof, error) {
	if err := c.reference(scope, a, r, true); err != nil {
		return memory.ForeignProof{}, err
	}
	if in.CopyID != r.CopyID || !api.Equal(in.ContentRef, r.ContentRef) {
		return memory.ForeignProof{}, api.E("forbidden", "original_release_binding_mismatch")
	}
	// 数据保留期不禁止收尾。首次实际report保存有限TTL；恢复只取原journal，绝不刷新。
	command := commandFor(c.Client.Config.OwnerID, r.ReleaseCommandID, "content.release_copy", r.ContentRef.ContentID, api.Time(time.Now().Add(10*time.Minute)), in)
	entry, err := c.Client.SDK.Journal.Read(ctx, r.ReleaseCommandID)
	if err == nil {
		command.ExpiresAt = entry.Command.ExpiresAt
		if !api.Equal(command, entry.Command) {
			return memory.ForeignProof{}, api.E("idempotency_conflict", "original_cleanup_report_changed")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return memory.ForeignProof{}, err
	}
	if err := applied(c.Client.SDK.Send(ctx, command)); err != nil {
		return memory.ForeignProof{}, err
	}
	return c.Control(ctx, scope, a, r)
}
