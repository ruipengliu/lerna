package interaction

import (
	"context"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/runtime"
)

// 当前主体门禁放在原根、内容和领域读取之后，不在平台锁之后
// 再追加 Task/Memory 上游锁。静态 owner 检查仍由各原读取保留。
func (s *Service) checkDisclosureTx(ctx context.Context, tx runtime.Tx, auth runtime.Auth) error {
	if err := identity(auth, tx.Scope()); err != nil {
		return err
	}
	if s.ports.SubjectGate == nil {
		return api.E("dependency_unavailable", "current_subject_authority_unavailable")
	}
	return s.ports.SubjectGate.CheckSubjectTx(ctx, tx, auth)
}

func currentDisclosure[T any](ctx context.Context, s *Service, store runtime.Store, scope runtime.Scope, auth runtime.Auth, read func(runtime.Tx) (T, error)) (T, error) {
	var out T
	status, err := store.Within(ctx, scope, s.config.Participants, func(tx runtime.Tx) error {
		value, err := read(tx)
		if err != nil {
			return err
		}
		if err = s.checkDisclosureTx(ctx, tx, auth); err != nil {
			return err
		}
		out = value
		return nil
	})
	if status == runtime.CommitUnknown {
		var zero T
		return zero, runtime.ErrCommitUnknown
	}
	if err != nil {
		var zero T
		return zero, err
	}
	return out, nil
}
