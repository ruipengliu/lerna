# Orchestrator 领域模块

2026-10-02：用户确认实现本模块完整领域职责，以公开 Go ports、runtime 装配、PostgreSQL/SQLite 及真实故障实验交付。Brain、Executor、Content、Grant 等协作方通过声明的 ports 注入；本轮不交付这些模块的真实实现或 WSS/gRPC。默认开发宿主继续提供诊断，业务接纳须具备实际依赖及恢复资格。

## 依据与边界

- [领域记录](../../docs/architecture/orchestrator/records.md)、[生命周期](../../docs/architecture/orchestrator/task-lifecycle.md)、[预算](../../docs/architecture/orchestrator/budget.md)、[核验](../../docs/architecture/orchestrator/verification.md)。
- [实现与故障断点](../../docs/architecture/orchestrator/implementation.md)、[有界访问](../../docs/architecture/orchestrator/access-paths.md)、[可靠工作接入](../../internal/durable/README.md)。
- 沿用 ADR-0001/0004/0005/0006/0009；不增设 Task、Operation 或费用的第二负责方。
- 默认 ReAct、strict 预算。可选有限 Plan、typed S1、estimate 及应用 Schedule 保持未启用并明确拒绝；它们不作为默认模块运行的先决条件。
- 17 个 task/budget 方法及原 owner 的 interaction.request_read；DTO/方法映射从根契约生成。没有公开网络入口，Go 组件入口同样校验固定契约与原身份。

## 必须兑现的规则

1. 原逻辑 owner 和 Command/Decision/Operation 固定，回执、事实与下一责任共同提交；外部调用不在事务闭包中。
2. status、control、wait、open_effects、accounting_open 独立。终态不重开；取消、修订、暂停不抹去原效果或费用。
3. 目标/控制/整体修订分别维护，真实条件变化先提交；同提案行动失效，同条件与重复反馈不制造推进。
4. 原快照、调用参数、消费与意图不可变；行动重新检查控制、资格、预算、期限和有限容量。
5. 输入请求与准确等待引用共同更新，一次消费；本人验收/确认由受信入口在实际 owner 的事务中消费。
6. 精确十进制账本；严格费用先预留可信上界，按物理来源累计修订只追差额。unknown/终态/失联不释放；可信超额照实入账并停新计费。
7. allocation/receiver 消费门禁分别持久保存，关闭先于接纳仍能拒绝迟到创建。首次结算与已结更正区别处理，关闭证明、原身份和可靠交回责任独立保留。
8. 成功需要独立完整目标覆盖、全部必要条件的当前适用检查、准确成果和完整未结集合；暂停可用既有证据完成。证据缺陷与完成按本地 gate 串行，原 Result 固定，迟到缺陷另附说明。
9. 七类责任 decide/dispatch/poll/verify/control/settle/extract 复用公共 JobStore。claim 失效整笔回滚；旧完成不吞新工作。
10. 续行/修复/无进展和自动查询有界，原身份只计一次；修订、重启或控制不能重置累计额度。控制、效果核对、费用收尾保留容量。
11. 控制同库有界子树按祖先顺序共同提交，逐执行端确认；未知远端状态不能声称全部落实。
12. 列表有限扫描、稳定游标、当前披露范围；完成及收尾查询走当前集合与未结索引，不遍历全部历史。

## 实施与验收清单

- [x] 契约生成、严格输入检查、公开领域 ports 与受信依赖约束。
- [x] 显式两方言迁移、独立 SQL/sqlc、受限 repository，兼容版本检查。
- [x] 17 方法及请求读取、原命令查阅，领域决定与 Raise 共同提交。
- [x] 原快照、决策消费、准入、可信事实归并、完成与证据门禁。
- [x] 精确预算、来源结算、allocation/关闭/上调、可靠交回。
- [x] 七类处理器、有限恢复、控制/收尾容量、公平候选与资源上限。
- [x] runtime 嵌入装配、排空/关闭与新接纳门禁；缺依赖保持关闭。
- [x] 真实 PG/SQLite 领域用例、跨进程崩溃、旧领取、提交未知和八条访问路径证据。
- [x] make build/check/durable-check/contracts-check/sql-check，文档结构、链接及生成可复现。

## 证据口径

受控、有状态协作 ports 只证明本模块事务、归并和恢复。真实 Brain/Executor/Grant、外部内容、模型准确性、网络互操作、公司平台 Clock/身份/批准、290/500 新任务每秒及生产故障剩余容量分别保留待接入或待验证，不用领域数据库测试替代。

本地验收：2026-10-02，`make build`、`make check`、`make durable-check` 和架构文档校验均通过；耐久实验实际使用 PostgreSQL 18.6 与 SQLite 3.53.4，覆盖两个数据库方言的领域恢复、并行 worker、进程崩溃、真实提交确认丢失及 Orchestrator 访问路径。命令修订冲突由调用方刷新后重发是预期并发行为。该结果不扩展上述外部集成与生产容量证据范围。
