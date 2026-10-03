package providers

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
	harness "github.com/ruipengliu/lerna/sdk/go"
)

// ForeignSourceClient 的SDK由宿主提供原peer/TLS/方法版本和持久journal；构造不出站。
type ForeignSourceClient struct {
	SDK                        *harness.Client
	Keys                       *platform.Keyring
	SourceScope, ConsumerScope runtime.Scope
}

var _ memory.ForeignContentPort = (*ForeignSourceClient)(nil)

func (c *ForeignSourceClient) reference(s runtime.Scope, a runtime.Auth, r memory.ForeignReference, control bool) error {
	if c == nil || c.SDK == nil || c.Keys == nil || s != c.ConsumerScope || a.TenantID != s.TenantID || a.CredentialGeneration == 0 || api.ValidateRecord("ContentRef", r.ContentRef) != nil || r.ContentRef.OwnerID != c.SourceScope.OwnerID || r.ContentRef.TenantID != s.TenantID || c.SourceScope.TenantID != s.TenantID || c.SourceScope.DatabaseID == "" || c.SDK.Discovery.LogicalServiceID != c.SourceScope.OwnerID || r.ContentRef.ByteLength > memory.MaxContentBytes || !api.ValidID(r.CopyID) || !api.ValidID(r.RegisterCommandID) || !api.ValidID(r.ReleaseCommandID) || api.ValidateRecord("ObjectRef", r.HolderRef) != nil || api.ValidateRecord("ObjectRef", r.ReferenceIntentRef) != nil || r.HolderRef.OwnerID != s.OwnerID || r.ReferenceIntentRef.OwnerID != s.OwnerID || r.HolderRef.TenantID != s.TenantID || r.ReferenceIntentRef.TenantID != s.TenantID || r.HolderRef.ObjectID != a.SubjectID || (!control && r.HolderRef.Revision != a.CredentialGeneration) || control && r.HolderRef.Revision > a.CredentialGeneration || r.Purpose == "" || r.Location == "" {
		return api.E("forbidden", "foreign_source_request_scope_mismatch")
	}
	_, err := api.ParseTime(r.RetainUntil)
	return err
}

func (c *ForeignSourceClient) verify(scope runtime.Scope, a runtime.Auth, r memory.ForeignReference, p memory.ForeignProof, now time.Time) error {
	control := p.Mode == "control"
	if err := c.reference(scope, a, r, control); err != nil {
		return err
	}
	if c == nil || c.SDK == nil || c.Keys == nil || scope != c.ConsumerScope || c.SourceScope.TenantID != scope.TenantID || c.SourceScope.OwnerID != r.ContentRef.OwnerID || c.SDK.Discovery.LogicalServiceID != c.SourceScope.OwnerID || r.ContentRef != p.ContentRef || p.SourceDatabaseID != c.SourceScope.DatabaseID || r.HolderRef.OwnerID != scope.OwnerID || r.HolderRef.ObjectID != a.SubjectID || r.HolderRef.TenantID != a.TenantID || (!control && r.HolderRef.Revision != a.CredentialGeneration) || control && r.HolderRef.Revision > a.CredentialGeneration || !api.Equal(p.SubjectRef, r.HolderRef) || !api.Equal(p.HolderRef, r.HolderRef) || !api.Equal(p.ReferenceIntentRef, r.ReferenceIntentRef) || p.Purpose != r.Purpose || p.Location != r.Location || p.CopyID != r.CopyID || p.ControlRevision == 0 || p.Mode != "use" && !control {
		return api.E("forbidden", "foreign_source_binding_mismatch")
	}
	digest, err := ForeignProofDigest(p)
	if err != nil {
		return err
	}
	claims := foreignProofClaims(p, digest)
	if control {
		_, err = c.Keys.VerifySource(p.Proof, claims)
	} else {
		_, err = c.Keys.Verify(p.Proof, claims, now)
	}
	return err
}
func (c *ForeignSourceClient) VerifyTx(ctx context.Context, tx runtime.Tx, a runtime.Auth, r memory.ForeignReference, p memory.ForeignProof) error {
	now, err := tx.Now(ctx)
	if err != nil {
		return err
	}
	return c.verify(tx.Scope(), a, r, p, now)
}
func (c *ForeignSourceClient) query(ctx context.Context, method, target string, input, output any) error {
	if c.SDK == nil {
		return api.E("dependency_unavailable", "foreign_source_client_unconfigured")
	}
	q := api.Query{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: c.SourceScope.OwnerID, QueryID: api.NewID("query"), Method: method, TargetID: target, Payload: api.Raw(input)}
	raw, err := c.SDK.Query(ctx, q)
	if err != nil {
		return err
	}
	return api.Decode(raw, output)
}
func (c *ForeignSourceClient) Current(ctx context.Context, s runtime.Scope, a runtime.Auth, r memory.ForeignReference) (memory.ForeignProof, error) {
	return c.current(ctx, s, a, r, false)
}
func (c *ForeignSourceClient) Control(ctx context.Context, s runtime.Scope, a runtime.Auth, r memory.ForeignReference) (memory.ForeignProof, error) {
	return c.current(ctx, s, a, r, true)
}
func (c *ForeignSourceClient) current(ctx context.Context, s runtime.Scope, a runtime.Auth, r memory.ForeignReference, control bool) (memory.ForeignProof, error) {
	var p memory.ForeignProof
	if err := c.reference(s, a, r, control); err != nil {
		return p, err
	}
	err := c.query(ctx, "content.foreign.current", r.ContentRef.ContentID, ForeignSourceCurrent{r, control}, &p)
	if err != nil {
		return p, err
	}
	if (p.Mode == "control") != control {
		return p, api.E("forbidden", "foreign_source_mode_changed")
	}
	return p, c.verify(s, a, r, p, time.Now())
}
func (c *ForeignSourceClient) send(ctx context.Context, id, method, target, expires string, in any) error {
	if c.SDK == nil {
		return api.E("dependency_unavailable", "foreign_source_client_unconfigured")
	}
	receipt, err := c.SDK.Send(ctx, api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: c.SourceScope.OwnerID, CommandID: id, Method: method, TargetID: target, ExpiresAt: expires, Payload: api.Raw(in)})
	if err != nil {
		return err
	}
	if receipt.Error != nil {
		return receipt.Error
	}
	if receipt.Stage != "applied" {
		return api.E("dependency_unavailable", "foreign_source_command_pending")
	}
	return nil
}
func (c *ForeignSourceClient) RegisterCopy(ctx context.Context, s runtime.Scope, a runtime.Auth, r memory.ForeignReference) (memory.ForeignProof, error) {
	if err := c.reference(s, a, r, false); err != nil {
		return memory.ForeignProof{}, err
	}
	if err := c.send(ctx, r.RegisterCommandID, "content.foreign.register", r.ContentRef.ContentID, r.RetainUntil, r); err != nil {
		return memory.ForeignProof{}, err
	}
	return c.Current(ctx, s, a, r)
}
func (c *ForeignSourceClient) Read(ctx context.Context, s runtime.Scope, a runtime.Auth, r memory.ForeignReference, p memory.ForeignProof) ([]byte, error) {
	if p.Mode != "use" {
		return nil, api.E("forbidden", "control_proof_cannot_authorize_bytes")
	}
	if err := c.verify(s, a, r, p, time.Now()); err != nil {
		return nil, err
	}
	if p.SourceState != "published" || p.UseState != "allowed" || r.ContentRef.ByteLength > memory.MaxContentBytes {
		return nil, api.E("forbidden", "source_closed")
	}
	count := (r.ContentRef.ByteLength + ForeignContentChunkBytes - 1) / ForeignContentChunkBytes
	if count == 0 {
		count = 1
	}
	body := make([]byte, 0, r.ContentRef.ByteLength)
	for index := uint64(0); index < count; index++ {
		var chunk ForeignSourceChunk
		if err := c.query(ctx, "content.foreign.get", r.ContentRef.ContentID, ForeignSourceGet{r, index}, &chunk); err != nil {
			return nil, err
		}
		if chunk.ContentRef != r.ContentRef || chunk.ChunkIndex != index || chunk.ChunkCount != count {
			return nil, api.E("forbidden", "foreign_chunk_binding_mismatch")
		}
		part, err := base64.StdEncoding.Strict().DecodeString(chunk.DataBase64)
		expected := uint64(ForeignContentChunkBytes)
		if index == count-1 {
			expected = r.ContentRef.ByteLength - index*ForeignContentChunkBytes
		}
		if err != nil || uint64(len(part)) != expected {
			return nil, api.E("forbidden", "foreign_chunk_corrupted")
		}
		body = append(body, part...)
	}
	if api.Hash(body) != r.ContentRef.Hash || uint64(len(body)) != r.ContentRef.ByteLength {
		return nil, api.E("forbidden", "foreign_content_corrupted")
	}
	if err := c.verify(s, a, r, p, time.Now()); err != nil {
		return nil, err
	}
	return body, nil
}
func (c *ForeignSourceClient) Release(ctx context.Context, s runtime.Scope, a runtime.Auth, r memory.ForeignReference, in memory.ReleaseCopyInput) (memory.ForeignProof, error) {
	if err := c.reference(s, a, r, true); err != nil {
		return memory.ForeignProof{}, err
	}
	if in.ContentRef != r.ContentRef || in.CopyID != r.CopyID {
		return memory.ForeignProof{}, api.E("forbidden", "foreign_source_release_scope_mismatch")
	}
	expires := api.Time(time.Now().Add(10 * time.Minute))
	entry, err := c.SDK.Journal.Read(ctx, r.ReleaseCommandID)
	if err == nil {
		expires = entry.Command.ExpiresAt
	} else if !errors.Is(err, os.ErrNotExist) {
		return memory.ForeignProof{}, err
	}
	if err = c.send(ctx, r.ReleaseCommandID, "content.foreign.release", r.ContentRef.ContentID, expires, ForeignSourceRelease{r, in}); err != nil {
		return memory.ForeignProof{}, err
	}
	return c.Control(ctx, s, a, r)
}
