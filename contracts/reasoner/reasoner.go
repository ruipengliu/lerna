// Package reasoner 定义无执行权限的公共推理端口。
package reasoner

import (
	"context"

	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
)

type Reasoner interface {
	DescribeReasoner(uint32) (*v1.ReasonerDescription, error)
	Propose(context.Context, *v1.ContextSnapshot, ...*Invocation) (*v1.Proposal, error)
}

// Invocation 只包含固定材料和请求范围内的窄模型端口，不携带领取或授权。
type Invocation struct {
	Model        ModelCaller
	Requirements *v1.Requirements
	Capabilities []*v1.Capability
	InputRefs    []*v1.Ref
}
type ModelRequest struct {
	Position  uint32
	Settings  *v1.ModelSettings
	InputRefs []*v1.Ref
}
type ModelResponse struct {
	Result *v1.ModelCallResult
	Body   []byte
}
type ModelCaller interface {
	Call(context.Context, *ModelRequest) (*ModelResponse, error)
}
