package development

import (
	"context"

	target "github.com/ruipengliu/lerna/adapters/execution"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/task"
	"github.com/ruipengliu/lerna/runtime"
)

type remoteSourcePermission struct {
	ref     api.ContentRef
	purpose string
}

// 文件驱动消费正文的用途由准确 Capability 和原参数决定。这里只读取已准入的
// 参数，不读取设备目标，也不把配置里的用途集合全数复制成设备使用许可。
func (a *App) remoteFilePermissions(ctx context.Context, scope runtime.Scope, intent task.OperationIntent) ([]remoteSourcePermission, error) {
	if api.Equal(intent.CapabilityRef, target.FileReadCapability().Ref) {
		permissions := make([]remoteSourcePermission, 0, len(intent.ProcessedSourceRefs))
		for _, ref := range uniqueSources(intent.ProcessedSourceRefs) {
			permissions = append(permissions, remoteSourcePermission{ref, "managed_file_read"})
		}
		return permissions, nil
	}
	if !api.Equal(intent.CapabilityRef, target.FileWriteCapability().Ref) {
		return nil, api.E("unsupported", "original_remote_file_capability_required")
	}
	raw, err := a.ReadContentBytes(ctx, scope, a.ServiceAuth, intent.ArgumentsRef, "execution_arguments", "device")
	if err != nil {
		return nil, err
	}
	var args target.FileWriteArguments
	if err = api.Decode(raw, &args); err != nil {
		return nil, err
	}
	if api.ValidateRecord("ContentRef", args.ContentRef) != nil || args.ContentRef.TenantID != scope.TenantID {
		return nil, api.E("forbidden", "original_managed_file_source_scope_mismatch")
	}
	for _, ref := range intent.ProcessedSourceRefs {
		if api.Equal(ref, args.ContentRef) {
			return []remoteSourcePermission{{args.ContentRef, "managed_file_write"}}, nil
		}
	}
	return nil, api.E("forbidden", "original_managed_file_source_not_declared")
}
