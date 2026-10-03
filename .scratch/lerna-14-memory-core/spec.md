# 14 · 最小 Memory 合同与获准检索

Status: ready-for-agent
Phase: D
Implementation: not-started
Depends on: [06](../lerna-06-authorized-admission/spec.md)

## Problem Statement

D 阶段要求验证 Memory 的第二实现，但自动提取属于 E。缺少最小公共 Memory 合同会让互操作验收依赖未完成的长期记忆产品。

## Solution

提前实现受信显式写入、版本更正、当前权限检索、限制与删除的最小 Memory 合同，供 D 互操作使用；任务历史仍不自动转成长期记忆。

## User Stories

1. As a user, I want explicit authorized memory creation, so that task history is not saved automatically.
2. As a user, I want exact source and time metadata, so that remembered claims can be evaluated in context.
3. As a user, I want corrections versioned, so that older observations are not silently rewritten.
4. As a data owner, I want permission filtering before ranking, so that retrieval does not leak restricted records.
5. As a user, I want pagination invalidated after revocation, so that old cursors cannot continue exposing data.
6. As an operator, I want rebuildable projections, so that a lost index is not lost truth.
7. As a user, I want restrictions and deletion visible, so that cleanup residuals are not hidden.
8. As a component author, I want a minimal complete method contract, so that a second implementation can be tested independently.

## Implementation Decisions

- 实现 memory.create、replace、query、restrict、delete 的完整方法合同，MemoryRecord 保存类型、准确 ContentRef、sources、scope、observed_at、valid_interval、revision 与状态。
- 仅接纳受信显式写入或已证明的保存许可；检索使用当前用途和来源资格。未实现 extract 不广告支持，不用通用 JSON 接口接受半实现方法。
- 默认词法、准确字段和时间过滤，先限定获准集合再排序；查询只接收闭合结构化条件，不接受任意 SQL 或代码。
- 更正形成新版本和替代关系，同事务增加变更水位并保存投影 Job；水位依据提交顺序，不直接用数据库 sequence 分配序号。
- 分页固定查询、候选版本集合和权限代次，权限变化使 cursor 失效；返回 partial、gaps、exhausted 与准确来源，不把空页当完整无结果。
- restrict / delete 先封新使用，再持久传播清理；删除身份不重用。最小本机 holder 也必须记录，跨副本完整生命周期由 18 扩展。
- 索引是可重建投影，不是权威或授权来源；正文清理后说明不可回读。此切片是实施顺序细化，不修改 ADR 或把完整 E 提前。

## Testing Decisions

主边界为公开 Memory Component 方法，复用 01 合同、04 内容和 06 授权。真实 PG 上检索与更新并发，观察公开结果与可访问内容，不断言私有索引实现。

测试只断言公开行为、持久恢复结果和独立目标状态；不依赖私有表布局或内部调用次数。每个故障或拒绝案例必须配允许正常完成的对照。

验收条件：

1. 显式获准材料可建立记忆并按准确来源、时间和版本检索；无保存许可时拒绝，任务历史不自动出现。
2. 更正形成新版本，旧观察保留；并发 expected_revision 冲突不静默覆盖。
3. 先取第一页再撤权，后续旧 cursor 被拒绝且无受限结果；正常分页可得到完整获准集合。
4. 清空可重建索引后从获准源恢复相同语义结果；缺索引或缺来源时返回 partial / gaps。
5. 限制或删除立即封新使用，清理失败显示残留；删除身份不能被重用恢复读取。
6. 并发提交的变更水位不遗漏后提交记录；权限或日志缺口时要求重建同步视图。
7. 不支持的 extract 返回 unsupported，支持列表与已实现方法 Schema 和测试保持一致。

## Out of Scope

- 自动提取、冲突候选工作流、向量或关系索引。
- 完整跨设备副本清理与经验改善声明。

## Further Notes

这一前置拆分解决 D→E 的隐藏依赖：15 依赖本切片即可验证 Memory 替换，18 在此基础上补全记忆产品能力。

依赖项表示实现先决条件；`ready-for-agent` 表示规格已明确，不表示依赖已完成或能力已开放。全部验收通过并附准确版本、环境、命令、结果和限制后，才可将本切片记为完成。共同执行与证据规则见[切片索引](../lerna-implementation/README.md)。

依据：[capabilities](../../docs/architecture/capabilities.md#memory-管理跨任务可复用的信息)、[data-model](../../docs/architecture/data-model.md#隔离索引与保留)、[contracts](../../docs/architecture/contracts.md#能力模块方法)、[validation](../../docs/architecture/validation.md#组件合同与互操作)、[ADR-0007](../../docs/adr/0007-versioned-content-memory-snapshots.md)、[ADR-0008](../../docs/adr/0008-internal-contracts-external-protocol-adapters.md)。
