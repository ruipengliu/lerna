package task

import (
	"context"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/runtime"
)

// CurrentTaskGate 在本方根链及预算锁之后核已保存的当前父授权证明。
// 实现只读本库有限证明/已知拒绝事实，不得出站，也不授予新的原目标。
// 非远端 Task 可返回 nil；远端缺准确当前证明时必须拒绝新的开始。
type CurrentTaskGate interface {
	CheckTaskCurrentTx(context.Context, runtime.Tx, api.Task, bool) error
}
