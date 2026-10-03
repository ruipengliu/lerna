package executor

import (
	"bytes"
	"context"
	"encoding/base64"
	"sort"
	"time"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/execution"
	"github.com/ruipengliu/lerna/internal/memory"
	"github.com/ruipengliu/lerna/runtime"
)

func (h *Host) materialize(ctx context.Context, st runtime.Store, s runtime.Scope, w runtime.Work) error {
	var rec contentRecord
	key := w.Job.SourceRef.ObjectID
	if _, err := st.Read(ctx, s, Namespace+".contents", key, 0, &rec); err != nil {
		return err
	}
	if !rec.Complete {
		chunks, err := st.List(ctx, s, Namespace+".chunks", key, "", 256)
		if err != nil {
			return err
		}
		if len(chunks) != int(rec.ChunkCount) {
			return api.E("dependency_unavailable", "original_content_chunks_missing")
		}
		ordered := make([]stagedChunk, len(chunks))
		for _, r := range chunks {
			var chunk stagedChunk
			if err = r.Decode(&chunk); err != nil {
				return err
			}
			if chunk.Index >= rec.ChunkCount {
				return api.E("invalid_request", "invalid_stored_chunk")
			}
			ordered[chunk.Index] = chunk
		}
		raw := make([]byte, 0, rec.Permission.ContentRef.ByteLength)
		for n, chunk := range ordered {
			if chunk.Index != uint64(n) {
				return api.E("dependency_unavailable", "original_content_chunks_missing")
			}
			data, err := base64.StdEncoding.Strict().DecodeString(chunk.DataBase64)
			if err != nil || api.Hash(data) != chunk.Hash {
				return api.E("dependency_unavailable", "original_content_chunk_corrupted")
			}
			raw = append(raw, data...)
		}
		if uint64(len(raw)) != rec.Permission.ContentRef.ByteLength || api.Hash(raw) != rec.Permission.ContentRef.Hash {
			return api.E("invalid_request", "original_content_hash_mismatch")
		}
		location, err := h.Objects.Write(ctx, rec.Permission.ContentRef, bytes.NewReader(raw))
		if err != nil {
			return err
		}
		rec.ObjectKey = location.Key
	}
	return runtime.Finish(ctx, st, s, []string{Namespace}, w, runtime.Done(), func(tx runtime.Tx) error {
		var current contentRecord
		rev, err := tx.Get(ctx, Namespace+".contents", key, &current)
		if err != nil {
			return err
		}
		if current.Complete {
			return nil
		}
		b, err := h.bundleTx(ctx, tx, current.BundleID)
		if err != nil {
			return err
		}
		if err = h.checkDenyTx(ctx, tx, b); err != nil {
			return err
		}
		// 复制责任保持原Content与bundle；即使准备窗口消耗完也只完成原缓存，不给新授权。
		current.Complete = true
		current.ObjectKey = rec.ObjectKey
		current.Revision = rev + 1
		return tx.Put(ctx, Namespace+".contents", key, rev, current)
	})
}

type deviceContent struct{ h *Host }

func (c deviceContent) ReadBytes(ctx context.Context, s runtime.Scope, a runtime.Auth, r api.ContentRef, purpose, location string) ([]byte, error) {
	if s != c.h.Scope || location != "device" || a.TenantID != s.TenantID {
		return nil, api.E("forbidden", "device_content_scope_mismatch")
	}
	var rec contentRecord
	if _, err := c.h.Store.Read(ctx, s, Namespace+".contents", contentKey(r), 0, &rec); err != nil {
		return nil, api.E("dependency_unavailable", "original_content_not_cached")
	}
	if !rec.Complete || !api.Equal(rec.Permission.ContentRef, r) || !api.Equal(rec.Principal, PrincipalOf(a)) {
		return nil, api.E("forbidden", "original_content_permission_mismatch")
	}
	// 账务/原Attempt恢复仍需准确输入依据；它们不能开启新行动。其余读取受原保留期限约束。
	if err := before(time.Now(), rec.Permission.RetainUntil); err != nil {
		return nil, err
	}
	allowed := has(rec.Permission.Purposes, purpose)
	if !allowed && rec.BundleID != "" {
		b, err := c.h.bundleRead(ctx, c.h.Store, s, rec.BundleID)
		if err != nil {
			return nil, err
		}
		p, ok := permissionFor(b, r)
		allowed = ok && has(p.Purposes, purpose)
	}
	if !allowed {
		return nil, api.E("forbidden", "original_content_purpose_not_allowed")
	}
	return c.h.Objects.Read(ctx, memory.ObjectLocation{Key: rec.ObjectKey, Version: r.Hash, Durability: "local_fsync"}, r, MaxContentBytes)
}
func (c deviceContent) Publish(ctx context.Context, s runtime.Scope, a runtime.Auth, p execution.Publication, raw []byte) (api.ContentRef, error) {
	ref := api.ContentRef{TenantID: s.TenantID, OwnerID: s.OwnerID, ContentID: p.ContentID, Version: 1, Hash: api.Hash(raw), MediaType: p.MediaType, ByteLength: uint64(len(raw))}
	if s != c.h.Scope || p.Location != "device" || a.SubjectID != c.h.Config.Authority.OwnerID || a.TenantID != s.TenantID || len(raw) > MaxContentBytes || api.ValidateRecord("ContentRef", ref) != nil {
		return ref, api.E("forbidden", "device_publication_scope_mismatch")
	}
	// 写端只有受信驱动/Execution内部port，公开peer不能调用此函数或选择签名正文。
	loc, err := c.h.Objects.Write(ctx, ref, bytes.NewReader(raw))
	if err != nil {
		return ref, err
	}
	permission := ContentPermission{ContentRef: ref, Purposes: []string{p.Purpose}, ProcessedSources: uniqueRefs(p.ProcessedSources), DisclosedSources: uniqueRefs(p.DisclosedSources), RetainUntil: api.Time(time.Now().Add(24 * time.Hour))}
	status, err := c.h.Store.Within(ctx, s, []string{Namespace}, func(tx runtime.Tx) error {
		var old contentRecord
		key := contentKey(ref)
		if _, err := tx.Get(ctx, Namespace+".contents", key, &old); err == nil {
			if !api.Equal(old.Permission.ContentRef, ref) || !api.Equal(old.Permission.ProcessedSources, permission.ProcessedSources) || !api.Equal(old.Permission.DisclosedSources, permission.DisclosedSources) || !api.Equal(old.Principal, PrincipalOf(a)) {
				return api.E("idempotency_conflict", "device_publication_changed")
			}
			return nil
		} else if !api.IsCode(err, "not_found") {
			return err
		}
		for _, source := range uniqueRefs(append(append([]api.ContentRef{}, permission.ProcessedSources...), permission.DisclosedSources...)) {
			var basis contentRecord
			if _, err := tx.Get(ctx, Namespace+".contents", contentKey(source), &basis); err != nil {
				return api.E("dependency_unavailable", "original_publication_source_missing")
			}
			if !basis.Complete || !api.Equal(basis.Permission.ContentRef, source) {
				return api.E("forbidden", "original_publication_source_mismatch")
			}
			permission.RetainUntil = earliest(permission.RetainUntil, basis.Permission.RetainUntil)
		}
		return tx.Create(ctx, Namespace+".contents", key, a.SubjectID, contentRecord{Permission: permission, Principal: PrincipalOf(a), Revision: 1, ChunkCount: chunkCount(ref.ByteLength), Complete: true, Published: true, ObjectKey: loc.Key})
	})
	if status == runtime.CommitUnknown {
		return ref, runtime.ErrCommitUnknown
	}
	return ref, err
}
func uniqueRefs(refs []api.ContentRef) []api.ContentRef {
	out := []api.ContentRef{}
	seen := map[string]bool{}
	for _, r := range refs {
		key := contentKey(r)
		if !seen[key] {
			seen[key] = true
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool { return contentKey(out[i]) < contentKey(out[j]) })
	return out
}
func (h *Host) contentGet(ctx context.Context, st runtime.Store, s runtime.Scope, a runtime.Auth, q api.Query, in ContentGet) (ContentChunk, error) {
	if err := h.peer(a); err != nil {
		return ContentChunk{}, err
	}
	var rec contentRecord
	if _, err := st.Read(ctx, s, Namespace+".contents", contentKey(in.ContentRef), 0, &rec); err != nil {
		return ContentChunk{}, err
	}
	if !rec.Complete || !rec.Published || !api.Equal(in.ContentRef, rec.Permission.ContentRef) || in.ChunkIndex >= chunkCount(in.ContentRef.ByteLength) {
		return ContentChunk{}, api.E("forbidden", "device_content_not_disclosed")
	}
	if err := before(time.Now(), rec.Permission.RetainUntil); err != nil {
		return ContentChunk{}, err
	}
	raw, err := h.Objects.Read(ctx, memory.ObjectLocation{Key: rec.ObjectKey, Version: in.ContentRef.Hash, Durability: "local_fsync"}, in.ContentRef, MaxContentBytes)
	if err != nil {
		return ContentChunk{}, err
	}
	start := in.ChunkIndex * ChunkBytes
	end := start + ChunkBytes
	if end > uint64(len(raw)) {
		end = uint64(len(raw))
	}
	return ContentChunk{Permission: rec.Permission, ChunkIndex: in.ChunkIndex, ChunkCount: chunkCount(uint64(len(raw))), DataBase64: base64.StdEncoding.EncodeToString(raw[start:end])}, nil
}
