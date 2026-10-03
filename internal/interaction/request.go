package interaction

import (
	"context"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/runtime"
)

// 两个消费者均校验原 owner 的准确请求；保存或缓存命中不能绕过当前生命周期。
func currentRequest(ctx context.Context, tx runtime.Tx, ref api.ObjectRef, view RequestView) error {
	request := view.Request
	if err := api.ValidateRecord("InputRequest", request); err != nil {
		return err
	}
	if err := runtime.CheckRef(tx.Scope(), request.TargetRef); err != nil {
		return err
	}
	if request.RequestID != ref.ObjectID || request.Revision != ref.Revision || request.OwnerID != ref.OwnerID || request.TenantID != ref.TenantID || request.TargetRef.OwnerID != request.OwnerID || view.Method == "" || len(view.Method) > 128 {
		return api.E("invalid_state", "request_target_mismatch")
	}
	if request.State != "pending" {
		return api.E("invalid_state", "input_request_closed")
	}
	now, err := tx.Now(ctx)
	if err != nil {
		return err
	}
	expires, err := api.ParseTime(request.ExpiresAt)
	if err != nil || !now.Before(expires) {
		return api.E("expired", "input_request_expired")
	}
	return nil
}
