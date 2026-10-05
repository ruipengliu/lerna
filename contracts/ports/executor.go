package ports

import (
	"context"

	lernav1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
)

// Executor 是执行适配器的公共接口（执行适配器接口 3）。
//
// 适配器声明能力（按具体能力、分维度，缺省最保守），只回报，不写效果（R3）；
// 不自行重试有外部效果的调用，超时回报为无法确定。它只能在出口闸门放行后被调用。
type Executor interface {
	// AdapterID 返回适配器标识。
	AdapterID() string
	// Declarations 返回版本化的能力声明。
	Declarations() []*lernav1.CapabilityDeclaration
	// Execute 进行一次调用；返回只表示拿到了回报。
	Execute(ctx context.Context, req *lernav1.ExecuteRequest) (*lernav1.ExecutionReport, error)
	// Query 按原尝试查询，不得重新执行原动作；不支持时返回 EXECUTION_STATUS_UNSUPPORTED。
	Query(ctx context.Context, req *lernav1.QueryRequest) (*lernav1.ExecutionReport, error)
}
