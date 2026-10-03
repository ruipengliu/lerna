package memory

import (
	"bytes"
	"context"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/runtime"
)

// LookupTransfer 只返回原主体可恢复的阶段，不披露对象存储定位。
func (s *Service) LookupTransfer(ctx context.Context, scope runtime.Scope, auth runtime.Auth, transferID string) (TransferStatus, error) {
	var out TransferStatus
	err := s.within(ctx, scope, func(tx runtime.Tx) error {
		if err := checkAuth(scope, auth); err != nil {
			return err
		}
		var t Transfer
		_, err := tx.Get(ctx, "content.transfers", transferID, &t)
		if err != nil {
			return err
		}
		if t.PublisherID != auth.SubjectID {
			return api.E("forbidden", "transfer_principal_mismatch")
		}
		out = TransferStatus{TransferID: t.TransferID, ContentRef: t.ContentRef, Phase: t.Phase, ExpiresAt: t.ExpiresAt, Durability: t.ObjectLocation.Durability}
		return nil
	})
	return out, err
}

// ReceiveTransferBytes 用原 ticket 接收准确字节；不能靠上传创建另一份元数据。
func (s *Service) ReceiveTransferBytes(ctx context.Context, scope runtime.Scope, auth runtime.Auth, transferID string, body []byte) (TransferStatus, error) {
	original, err := s.LookupTransfer(ctx, scope, auth, transferID)
	if err != nil {
		return TransferStatus{}, err
	}
	if uint64(len(body)) != original.ContentRef.ByteLength || api.Hash(body) != original.ContentRef.Hash {
		return TransferStatus{}, api.E("invalid_request", "content_hash_or_length_mismatch")
	}
	if err = s.WriteTransfer(ctx, scope, auth, transferID, bytes.NewReader(body)); err != nil {
		return TransferStatus{}, err
	}
	return s.LookupTransfer(ctx, scope, auth, transferID)
}

type CompleteMirrorInput struct {
	TransferID string `json:"transfer_id"`
	CopyID     string `json:"copy_id"`
	Purpose    string `json:"purpose"`
	Location   string `json:"location"`
}

func (s *Service) completeMirror(ctx context.Context, tx runtime.Tx, auth runtime.Auth, c api.Command, in CompleteMirrorInput) (CopyOutput, error) {
	var t Transfer
	rev, err := tx.Get(ctx, "content.transfers", in.TransferID, &t)
	if err != nil {
		return CopyOutput{}, err
	}
	if c.TargetID != t.ContentRef.ContentID || t.PublisherID != auth.SubjectID || t.Kind != "mirror" {
		return CopyOutput{}, api.E("forbidden", "mirror_scope_mismatch")
	}
	if t.Phase != "ready" && t.Phase != "published" {
		return CopyOutput{}, api.E("dependency_unavailable", "transfer_not_ready")
	}
	if _, err = future(ctx, tx, t.ExpiresAt); err != nil {
		return CopyOutput{}, api.E("expired", "transfer_expired")
	}
	out, err := s.RegisterCopyTx(ctx, tx, auth, RegisterCopyInput{CopyID: in.CopyID, ContentRef: t.ContentRef, HolderRef: t.TargetHolder, Purpose: in.Purpose, Location: in.Location, RetainUntil: t.ExpiresAt, ReferenceIntentRef: t.ReferenceIntentRef})
	if err != nil {
		return CopyOutput{}, err
	}
	if t.Phase != "published" {
		t.Phase = "published"
		t.Revision = rev + 1
		if err = tx.Put(ctx, "content.transfers", t.TransferID, rev, t); err != nil {
			return CopyOutput{}, err
		}
	}
	return out, nil
}
