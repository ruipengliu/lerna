package ports

import (
	"context"
	"errors"

	lernav1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
)

// Reasoner 是推理的公共接口（推理接口 3）。它只给出提议，不做裁决（G6）。
//
// Propose 收到的是一个模型调用请求接口，不是已获准的发送凭据：每次模型调用由受信宿主
// 封存完整调用描述，核心准入后才发送。推理不得直接调用执行适配器（R1）。
type Reasoner interface {
	// Describe 返回实现版本和支持的提议类型。
	Describe() ReasonerInfo
	// Propose 对一次提议请求求解。返回 ErrAwaitingModelCall 表示有模型调用尚未得到结果，
	// 核心在结果就绪后用同一请求重新运行；推理按调用位置拿回原结果，不重复发送。
	Propose(ctx context.Context, req *lernav1.ProposalRequest, snap *lernav1.ContextSnapshot, models ModelCaller) (*lernav1.Proposal, error)
}

// ReasonerInfo 描述推理实现。
type ReasonerInfo struct {
	Name    string
	Version string
}

// ModelCaller 是核心提供给推理的模型调用请求接口（推理接口 3）。
// "提议请求 + 调用位置"标识一次模型调用：同位置同描述返回原结果，同位置不同描述报冲突。
type ModelCaller interface {
	Call(ctx context.Context, position int32, desc *lernav1.ModelCallDescription) (*lernav1.ModelCallResult, error)
}

// ErrAwaitingModelCall 表示模型调用已准入但结果尚未就绪。
var ErrAwaitingModelCall = errors.New("reasoner: awaiting model call result")

// ErrPreparationUnrecoverable 表示推理无法重现相同的调用位置和描述（"准备不可恢复"），
// 由核心决定是否建立新的提议请求。
var ErrPreparationUnrecoverable = errors.New("reasoner: preparation unrecoverable")
