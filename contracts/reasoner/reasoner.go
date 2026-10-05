// Package reasoner 定义无执行权限的公共推理端口。
package reasoner

import (
	"context"

	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
)

type Reasoner interface {
	Propose(context.Context, *v1.ContextSnapshot) (*v1.Proposal, error)
}
