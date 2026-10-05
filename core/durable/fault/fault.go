// Package fault 提供持久化点的故障注入钩子（规格"辅助切面一"）。
//
// 生产构建（没有 fault 构建标签）中 Hit 是空操作，钩子代码不编译进去。
// 故障注入测试用构建标签 fault 编译，按名称为持久化点配置"崩溃"或"回执丢失"。
//
// 命名约定：每个持久事务由 durable 自动登记两个点 "<标签>:before_commit"
// 和 "<标签>:after_commit"；不经事务的点（例如实际出口 I/O 前后）直接调用 Hit。
package fault

import "errors"

// ErrLost 表示注入的"回执丢失"：操作已经发生，调用方拿不到结果。
var ErrLost = errors.New("fault: injected receipt loss")
