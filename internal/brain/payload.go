package brain

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strconv"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/runtime"
)

const (
	payloads         = "brain.payloads"
	payloadChunkSize = 64 << 10
	payloadMaxBytes  = 2 << 20
	payloadMaxChunks = payloadMaxBytes / payloadChunkSize
)

// 私有复合载荷不扩张任一公开 JSON 合同。其各领域输入仍经过原协议边界；
// Encoding 的 base64 以及发布责任的正文副本不能挤入 Runtime 单条记录。
type decisionPayload struct {
	Snapshot     *api.Snapshot    `json:"snapshot,omitempty"`
	Encoding     *Encoding        `json:"encoding,omitempty"`
	Generated    *Generated       `json:"generated,omitempty"`
	Publications []pendingContent `json:"publications"`
}

type payloadManifest struct {
	Digest     string          `json:"digest"`
	ByteLength uint64          `json:"byte_length"`
	Chunks     []api.ObjectRef `json:"chunks"`
}

type payloadChunk struct {
	DecisionID string `json:"decision_id"`
	Digest     string `json:"digest"`
	Index      uint64 `json:"index"`
	Body       []byte `json:"body"`
}

func payloadChunkID(decisionID, digest string, index int) string {
	return "payload_" + api.Hash([]byte(decisionID + "\n" + digest + "\n" + strconv.Itoa(index)))[7:]
}

// 分片不可变；分片与准确主头/CAS、原 Claim/回执在调用方的同一事务提交。
// 不删除旧主头使用过的分片，避免并发读到旧头后失去其原准确载荷。
func putDecision(ctx context.Context, tx runtime.Tx, id string, revision uint64, d decision) error {
	payload := decisionPayload{d.Snapshot, d.Encoding, d.Generated, d.Publications}
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if len(raw) > payloadMaxBytes {
		return api.E("invalid_request", "brain_payload_over_limit")
	}
	manifest := payloadManifest{Digest: api.Hash(raw), ByteLength: uint64(len(raw)), Chunks: []api.ObjectRef{}}
	for index, offset := 0, 0; offset < len(raw); index, offset = index+1, offset+payloadChunkSize {
		end := offset + payloadChunkSize
		if end > len(raw) {
			end = len(raw)
		}
		chunkID := payloadChunkID(id, manifest.Digest, index)
		chunk := payloadChunk{DecisionID: id, Digest: manifest.Digest, Index: uint64(index), Body: raw[offset:end]}
		var old payloadChunk
		if err = tx.GetVersion(ctx, payloads, chunkID, 1, &old); api.IsCode(err, "not_found") {
			if err = tx.Create(ctx, payloads, chunkID, id, chunk); err != nil {
				return err
			}
		} else if err != nil {
			return err
		} else if old.DecisionID != id || old.Digest != chunk.Digest || old.Index != chunk.Index || !bytes.Equal(old.Body, chunk.Body) {
			return api.E("dependency_unavailable", "brain_payload_corrupt")
		}
		manifest.Chunks = append(manifest.Chunks, tx.Scope().Ref(chunkID, 1))
	}
	d.Payload = &manifest
	d.Snapshot, d.Encoding, d.Generated, d.Publications = nil, nil, nil, nil
	return tx.Put(ctx, records, id, revision, d)
}

func hydrateDecision(ctx context.Context, scope runtime.Scope, d *decision, fetch func(context.Context, api.ObjectRef, *payloadChunk) error) error {
	if d.Payload == nil {
		return nil // 兼容原 inline 记录；不从当前 Content 重建原编码或回复。
	}
	manifest := d.Payload
	if d.Snapshot != nil || d.Encoding != nil || d.Generated != nil || len(d.Publications) != 0 || d.Input.DecisionID != d.Record.DecisionID || manifest.ByteLength == 0 || manifest.ByteLength > payloadMaxBytes || len(manifest.Chunks) == 0 || len(manifest.Chunks) > payloadMaxChunks || uint64(len(manifest.Chunks)) != (manifest.ByteLength+payloadChunkSize-1)/payloadChunkSize {
		return api.E("dependency_unavailable", "brain_payload_corrupt")
	}
	raw := make([]byte, 0, int(manifest.ByteLength))
	for index, ref := range manifest.Chunks {
		if ref.TenantID != scope.TenantID || ref.OwnerID != scope.OwnerID || ref.Revision != 1 || ref.ObjectID != payloadChunkID(d.Input.DecisionID, manifest.Digest, index) {
			return api.E("dependency_unavailable", "brain_payload_corrupt")
		}
		var chunk payloadChunk
		if err := fetch(ctx, ref, &chunk); err != nil {
			return err
		}
		length := manifest.ByteLength - uint64(index*payloadChunkSize)
		if length > payloadChunkSize {
			length = payloadChunkSize
		}
		if chunk.DecisionID != d.Input.DecisionID || chunk.Digest != manifest.Digest || chunk.Index != uint64(index) || uint64(len(chunk.Body)) != length {
			return api.E("dependency_unavailable", "brain_payload_corrupt")
		}
		raw = append(raw, chunk.Body...)
	}
	if uint64(len(raw)) != manifest.ByteLength || api.Hash(raw) != manifest.Digest {
		return api.E("dependency_unavailable", "brain_payload_corrupt")
	}
	// 仅此私有复合容器可超过公开单字段上限；使用固定闭合类型和 2MiB 硬界。
	// 整体完整性核验先于解码；公开输入/供应商回复仍使用原严格 api.Decode。
	var payload decisionPayload
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&payload); err != nil {
		return api.E("dependency_unavailable", "brain_payload_corrupt")
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return api.E("dependency_unavailable", "brain_payload_corrupt")
	}
	d.Snapshot, d.Encoding, d.Generated, d.Publications = payload.Snapshot, payload.Encoding, payload.Generated, payload.Publications
	return nil
}

func hydrateStoredDecision(ctx context.Context, store runtime.Store, scope runtime.Scope, d *decision) error {
	return hydrateDecision(ctx, scope, d, func(ctx context.Context, ref api.ObjectRef, chunk *payloadChunk) error {
		_, err := store.Read(ctx, scope, payloads, ref.ObjectID, ref.Revision, chunk)
		return err
	})
}

func getDecisionTx(ctx context.Context, tx runtime.Tx, id string, d *decision) (uint64, error) {
	revision, err := tx.Get(ctx, records, id, d)
	if err != nil {
		return revision, err
	}
	err = hydrateDecision(ctx, tx.Scope(), d, func(ctx context.Context, ref api.ObjectRef, chunk *payloadChunk) error {
		return tx.GetVersion(ctx, payloads, ref.ObjectID, ref.Revision, chunk)
	})
	return revision, err
}
