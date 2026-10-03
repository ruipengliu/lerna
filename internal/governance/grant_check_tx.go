package governance

import (
	"context"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/runtime"
)

// CheckGrantTx 复用 grant.check 的完整当前父链/主体/费用门禁。
// 供显式同库宿主在材料准入事务中只读复核，不创建 Use、消费 once 或出站。
func (s *Service) CheckGrantTx(ctx context.Context, tx runtime.Tx, auth runtime.Auth, in UseRequest) (UseReceipt, error) {
	if auth.TenantID != tx.Scope().TenantID {
		return UseReceipt{}, api.E("forbidden", "tenant_mismatch")
	}
	return s.checkUse(ctx, tx, auth, in, false)
}
