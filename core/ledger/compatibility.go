package ledger

import (
	"context"

	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"github.com/ruipengliu/lerna/core/durable"
)

type recoveryOperations interface {
	RecoveryOperations(context.Context) ([]*v1.Operation, error)
}

// RecoveryCompiler 只核验固定原实现可解释，不编译或保存另一份调用描述。
type RecoveryCompiler interface {
	CheckRecoverySupported(*v1.Operation) error
}

// CheckStartupCompatibility 在任一域自动恢复前核验原动作，未知和已关闭任务同样保留。
func (s *Service) CheckStartupCompatibility(ctx context.Context) error {
	all, e := s.store.RecoveryOperations(ctx)
	if e != nil {
		return e
	}
	for _, op := range all {
		if e = durable.RequireDependencies("ledger", durable.Dependency{Name: "rules", Value: s.rules}); e != nil {
			return e
		}
		if e = s.rules.CheckSupported(op); e != nil {
			return e
		}
		if e = s.adapter.CheckRecoverySupported(op); e != nil {
			return e
		}
	}
	return nil
}
