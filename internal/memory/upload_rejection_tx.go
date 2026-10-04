package memory

import (
	"context"
	"sort"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/runtime"
)

// CheckUploadRejectionTx 只核原上传从未获准的持久依据，不读取介质、不授予
// 新上传许可。调用方须先在事务外确认原字节不存在，再共同提交自身失败责任。
// false 表示仍有原上传责任或缺少确定依据，不能据此完成未知发布或清理。
func (s *Service) CheckUploadRejectionTx(ctx context.Context, tx runtime.Tx, auth runtime.Auth, request PublicationRequest) (api.Receipt, bool, error) {
	if !auth.HasRole("service") {
		return api.Receipt{}, false, api.E("forbidden", "trusted_publication_recovery_required")
	}
	if err := checkContentRef(tx.Scope(), request.ContentRef); err != nil {
		return api.Receipt{}, false, err
	}
	if !api.ValidID(request.TransferID) || !api.ValidID(request.ReserveCommandID) || !api.ValidID(request.PutCommandID) || request.ReserveCommandID == request.PutCommandID {
		return api.Receipt{}, false, api.E("invalid_request", "upload_recovery_identity_required")
	}
	// 缺失命令键也由实际 Runtime 锁定，命令锁先于 Memory 变更头。
	ids := []string{request.ReserveCommandID, request.PutCommandID}
	sort.Strings(ids)
	var reserve runtime.StoredCommand
	reserveFound, putFound := false, false
	for _, id := range ids {
		command, err := tx.LoadCommand(ctx, id)
		if api.IsCode(err, "not_found") {
			continue
		}
		if err != nil {
			return api.Receipt{}, false, err
		}
		if id == request.ReserveCommandID {
			reserve, reserveFound = command, true
		} else {
			putFound = true
		}
	}
	if _, err := loadHead(ctx, tx); err != nil {
		return api.Receipt{}, false, err
	}
	if err := s.currentAuth(ctx, tx, auth); err != nil {
		return api.Receipt{}, false, err
	}
	if !reserveFound || putFound || reserve.Tombstone {
		return api.Receipt{}, false, nil
	}
	expected := api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: tx.Scope().OwnerID, CommandID: request.ReserveCommandID, Method: "content.upload_reserve", TargetID: request.ContentRef.ContentID, ExpiresAt: request.TransferDeadline, Payload: api.Raw(ReserveInput{request.TransferID, request.ContentRef, request.PolicyRef, request.ProcessedSources, request.RetentionUntil, request.TransferDeadline})}
	digest, err := api.Digest(expected)
	if err != nil {
		return api.Receipt{}, false, err
	}
	if reserve.PrincipalID != auth.SubjectID || !api.Equal(reserve.Command, expected) || reserve.Digest != digest || reserve.Receipt.CommandID != expected.CommandID || reserve.Receipt.RequestDigest != digest {
		return api.Receipt{}, false, api.E("idempotency_conflict", "upload_recovery_identity_changed")
	}
	r := reserve.Receipt
	if r.Stage != "rejected" || r.Error == nil || r.Error.Code != "expired" || r.Error.Reason != "command_expired" || r.AcceptedAt != "" || len(r.Output) != 0 {
		return api.Receipt{}, false, nil
	}
	deadline, err := api.ParseTime(expected.ExpiresAt)
	if err != nil {
		return api.Receipt{}, false, err
	}
	decided, err := api.ParseTime(r.DecidedAt)
	if err != nil {
		return api.Receipt{}, false, err
	}
	if decided.Before(deadline) {
		return api.Receipt{}, false, api.E("idempotency_conflict", "upload_rejection_before_original_deadline")
	}
	var transfer Transfer
	if _, err = tx.Get(ctx, "content.transfers", request.TransferID, &transfer); err == nil {
		return api.Receipt{}, false, nil
	} else if !api.IsCode(err, "not_found") {
		return api.Receipt{}, false, err
	}
	var content ContentVersion
	if _, err = tx.Get(ctx, "content.versions", contentKey(request.ContentRef), &content); err == nil {
		return api.Receipt{}, false, nil
	} else if !api.IsCode(err, "not_found") {
		return api.Receipt{}, false, err
	}
	// 同一准确版本的另一原票据或最小绑定也不能被视为从未写入。
	if _, err = tx.LookupKey(ctx, "content.transfers", contentKey(request.ContentRef)); err == nil {
		return api.Receipt{}, false, nil
	} else if !api.IsCode(err, "not_found") {
		return api.Receipt{}, false, err
	}
	return r, true, nil
}
