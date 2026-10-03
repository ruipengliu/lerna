package wasi

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"github.com/ruipengliu/lerna/api"
	rt "github.com/ruipengliu/lerna/runtime"
)

// ReadOriginalReceipt 只核原私有日志中的计量元数据，不取得运行时所有权、
// probe 或运行用户模块。输出只含原回执字段；调用方必须再与原 Attempt
// 已登记的准确 EvidenceRef 字节摘要绑定，不能把任意日志当作原执行依据。
func ReadOriginalReceipt(ctx context.Context, path string, scope rt.Scope, operationID, attemptID string, config, install api.ComponentRef) (receipt Receipt, err error) {
	if err = ctx.Err(); err != nil {
		return receipt, err
	}
	if !filepath.IsAbs(path) || !api.ValidID(scope.TenantID) || !api.ValidID(scope.OwnerID) || scope.DatabaseID == "" || !api.ValidID(operationID) || !api.ValidID(attemptID) {
		return receipt, api.E("forbidden", "original_wasi_accounting_identity_required")
	}
	if err = verifyPrivateRoot(path); err != nil {
		return receipt, err
	}
	root, err := os.OpenRoot(path)
	if err != nil {
		return receipt, err
	}
	defer func() { err = errors.Join(err, root.Close()) }()
	r := Runtime{cfg: Config{Root: path, Scope: scope}, root: root}
	if err = r.readJSON("manifest.json", &r.manifest); err != nil {
		return receipt, err
	}
	if !api.Equal(install, r.InstallLockRef()) || !api.Equal(config, r.EnvironmentConfigRef()) {
		return receipt, api.E("forbidden", "original_wasi_accounting_lock_changed")
	}
	var original runRecord
	if err = r.readJSON(attemptID+".json", &original); err != nil {
		return receipt, err
	}
	if original.AttemptID != attemptID || original.BindingDigest == "" || original.SpawnCount > 1 || original.Result.Protocol != WorkerProtocol || api.ValidateAmounts(original.Usage) != nil {
		return receipt, api.E("forbidden", "original_wasi_accounting_journal_changed")
	}
	if original.Phase != "finished" && original.Phase != "lost" {
		return receipt, api.E("accounting_unknown", "original_wasi_exit_not_observed")
	}
	if err = ctx.Err(); err != nil {
		return receipt, err
	}
	// 日志中的原目标输出只计算回执原摘要，不解释、返回或授予普通数据用途。
	return Receipt{AttemptID: original.AttemptID, OperationID: operationID, BindingDigest: original.BindingDigest, InstallLock: install, Phase: original.Phase, SpawnCount: original.SpawnCount, SpawnCountKnown: original.SpawnCountKnown, Reason: original.Result.Reason, OutputHash: api.Hash(original.Result.Output), ActuallyExited: true, Usage: original.Usage, UsageFinal: original.UsageFinal}, nil
}
