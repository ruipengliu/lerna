package assembly

import (
	"context"

	"github.com/ruipengliu/lerna/infra/backup"
	"github.com/ruipengliu/lerna/infra/sqlite"
)

// CloseAndBackup 等待本宿主当前存储调用结束并关闭连接；调用者仍须停止其他全部写者且不再恢复原路径。
func (h *Harness) CloseAndBackup(ctx context.Context, directory string) (*backup.Manifest, error) {
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	s := h.StorageSettings()
	if e := h.Close(); e != nil {
		return nil, e
	}
	return backup.CreateStopped(ctx, h.path, directory, backup.Manifest{
		UserID: h.user, DomainID: h.domain, FormatVersion: s.FormatVersion, ContractVersion: s.ContractVersion,
		FormatDigest: s.FormatDigest, ImplementationProfile: s.ImplementationProfile,
		Environment: backup.Environment{
			Platform: s.Platform, SQLiteVersion: s.SQLiteVersion, SQLiteSourceID: s.SQLiteSourceID,
			SQLiteCompileOptionsHash: s.SQLiteCompileOptionsHash, JournalMode: s.JournalMode,
			Synchronous: s.Synchronous, FullFSync: s.FullFSync, DurabilityProfile: s.DurabilityProfile,
			PowerLossQualified: s.PowerLossQualified,
		},
	})
}

// RestoreBackup 在另一个空目录核验并复制最新停机副本，之后才使用生产装配打开原身份。
func RestoreBackup(ctx context.Context, directory, destination, user, domain string) (*Harness, error) {
	return RestoreBackupWithOptions(ctx, directory, destination, user, domain, Options{})
}

// RestoreBackupWithOptions 恢复数据库原身份，同时显式保留宿主的原外部环境。
func RestoreBackupWithOptions(ctx context.Context, directory, destination, user, domain string, options Options) (*Harness, error) {
	m, e := backup.Verify(ctx, directory, user, domain)
	if e != nil {
		return nil, e
	}
	if e = sqlite.CheckFormatCompatibility(m.FormatVersion, m.ContractVersion, m.ImplementationProfile, m.FormatDigest); e != nil {
		return nil, e
	}
	path, copied, e := backup.Restore(ctx, directory, destination, user, domain)
	if e != nil {
		return nil, e
	}
	if e = sqlite.CheckFormatCompatibility(copied.FormatVersion, copied.ContractVersion, copied.ImplementationProfile, copied.FormatDigest); e != nil {
		return nil, e
	}
	if e = ctx.Err(); e != nil {
		return nil, e
	}
	return OpenWithOptions(path, user, domain, options)
}
