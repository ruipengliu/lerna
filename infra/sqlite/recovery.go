package sqlite

import (
	"context"

	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
)

// CheckRecoveryAllowed 核验已通过的平台启动资格和当前同一连接，不能使用请求自报状态。
func (s *Store) CheckRecoveryAllowed(ctx context.Context) error {
	blocked := func() error {
		return &command.Failure{Detail: &v1.ContractError{Code: "LOCAL_RECOVERY_BLOCKED", Category: v1.ErrorCategory_ERROR_CATEGORY_TRANSIENT, CommandAcceptance: v1.CommandAcceptance_COMMAND_ACCEPTANCE_NOT_SUBMITTED, RecoveryAction: "RESTORE_DEPENDENCY_AND_QUERY_ORIGINAL"}}
	}
	if !s.settings.PowerLossQualified || !LocalProfileSupported(s.settings) {
		return blocked()
	}
	return s.read(ctx, func(q querier) error {
		var synchronous, fullFSync int
		var journal string
		if e := q.QueryRowContext(ctx, "PRAGMA synchronous").Scan(&synchronous); e != nil {
			return storageError(e, false)
		}
		if e := q.QueryRowContext(ctx, "PRAGMA fullfsync").Scan(&fullFSync); e != nil {
			return storageError(e, false)
		}
		if e := q.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&journal); e != nil {
			return storageError(e, false)
		}
		if synchronous != 2 || fullFSync != 1 || journal != "wal" {
			return blocked()
		}
		return nil
	})
}
