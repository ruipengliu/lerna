package interaction

import (
	"context"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/runtime"
)

func (s *Service) requestViews(ctx context.Context, tx runtime.Tx, auth runtime.Auth, refs []api.ObjectRef) ([]RequestView, error) {
	if len(refs) > 20 {
		return nil, invalid("request_limit")
	}
	if len(refs) == 0 {
		return []RequestView{}, nil
	}
	if s.ports.Requests == nil {
		return nil, api.E("unsupported", "request_views_unconfigured")
	}
	seen := map[string]bool{}
	for _, ref := range refs {
		if err := runtime.CheckRef(tx.Scope(), ref); err != nil {
			return nil, err
		}
		key := ref.OwnerID + ":" + ref.ObjectID
		if seen[key] {
			return nil, invalid("duplicate_request_ref")
		}
		seen[key] = true
	}
	var views []RequestView
	var err error
	if batch, ok := s.ports.Requests.(RequestBatchPort); ok {
		views, err = batch.CheckBatchTx(ctx, tx, auth, refs)
	} else if len(refs) == 1 {
		var view RequestView
		view, err = s.ports.Requests.CheckTx(ctx, tx, auth, refs[0])
		views = []RequestView{view}
	} else {
		return nil, api.E("unsupported", "batch_request_views_unconfigured")
	}
	if err != nil {
		return nil, err
	}
	if len(views) != len(refs) {
		return nil, api.E("invalid_state", "request_view_count_mismatch")
	}
	return views, nil
}

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
