# 19: 封闭交付收进一个深模块

**What to build:** 完成、取消和非成功关闭的封闭交付只保留一份算法。三种封闭作为任务编排内固定的封闭类别表达，对外接口收窄，持久格式和事务点保持原值。

**Blocked by:** 14（完成与取消共用原封闭回执交付）、15（非成功关闭接入共同封闭交付）.

**Status:** resolved

- [x] `core/tasks` 用未导出的封闭集合 `closureKind`（三个实现，无注册入口）替代 `closureDeliveryKind` 枚举及其全部 switch；领取、回执查询、补交、回执校验、源确认只写一遍。
- [x] 任务编排声明一个 `ClosureRecipient`（三个 `CloseFor*` 与 `QueryReceipt`）和一个 `ClosureJobs`（按 `JobType` 入队、读取领取、完成），替代三对 `*Jobs`／`*Closer` 端口及三个 `With*Closures`。
- [x] `core/durable` 三个封闭工作文件合成一个，只接受 `DELIVER_COMPLETION_CLOSURE`、`DELIVER_CANCELLATION_CLOSURE`、`DELIVER_TASK_CLOSURE`。
- [x] 公开 `ProcessClosureClaim(job)` 按**已保存** Job 的 `JobType` 分派，替代三个 `Process*ClosureClaim`；`ProcessTaskClosures` 改为未导出（`ProcessCompletionClosures` 保留公开，见 Answer）；`ProcessCancellations` 与三个 `Recover*` 保持，宿主启动与手动阶段顺序不变。
- [x] `JobType`、`PurposeKey` 前缀、发起方、指纹标签、结果类型、事务点名、proto 类型全部保持原值；不改协议、迁移和设计文档。
- [x] 行为收紧：完成与取消的领取副本按非成功关闭已有的严格校验（契约版本、未知字段、类型、范围、责任、端点、负责域）拒绝，返回 `INVALID_JOB`，接收方不被调用。
- [x] 行为收紧：三种封闭在源确认事务内都核验已保存意图的命令与交付命令一致，否则 `INVARIANT_VIOLATION`，不完成原工作、不保存回执。
- [x] 公共测试改用新入口，断言不变；M1 验收源映射保持为历史记录。
- [x] 受影响包的 `make check-code`，以及三种封闭的提交前/后/丢回执故障切片通过。

## Comments

- 2026-10-07: Claimed on `codex/interfaces-19`. Follows the architecture review: closure delivery was one concept with three copies in tasks and durable; receiver-side (`ledger` CloseForX, `grant_closure`, budget chain re-check) is left for a later ticket.

## Answer

2026-10-07: 封闭交付收进任务编排内一个封闭类别集合 `closureKind`（`completionClosure`、`cancellationClosure`、`taskClosure`），领取、回执查询、仅 `NOT_FOUND` 时补交、回执校验和源确认事务只写一遍；原 5 处按类别 switch 与 `closureDeliveryKind` 删除。任务编排的封闭端口由 3 对 `*Jobs`／`*Closer`（共约 22 个方法）收为 `ClosureJobs` 与 `ClosureRecipient` 两个，`WithClosures` 替代三个 setter；`core/durable` 的三个封闭工作文件合成 `closure.go`，用固定白名单只接受三种 `DELIVER_*`。`ProcessClosureClaim` 先读保存的 Job，再按保存的 `JobType` 分派。`JobType`、`PurposeKey` 前缀、发起方、指纹标签、结果类型、事务点名与 proto 类型均保持原值；未改协议、迁移或设计文档。

偏离讨论结论一处：`ProcessCompletionClosures` 保留公开。6 处公共测试只需"只交付封闭"，改用 `ProcessCompletions` 会多执行轮次重查，改变测试语义。

行为收紧两处，先写失败测试再实现：

- `TestClosureClaimRejectsForgedCopyForEveryKind`：完成、取消原先接受契约版本等字段被改的副本并实际交付（RED：`forged version copy: <nil>`）；现与非成功关闭一样返回 `INVALID_JOB`，接收方调用 0 次，原工作和回执不变。
- `TestClosureAcknowledgmentRejectsChangedSavedIntent`：完成、非成功关闭原先在源确认时不核对已保存意图的命令（RED：`changed saved intent: <nil>`）；现三种都返回 `INVARIANT_VIOLATION`，原工作和回执不变。

三个源确认的持久化点由各类别的 `receiptTransaction` 在事务调用处保留字面名称，符合任务编排设计"完整登记检查不接受动态持久化点"；首版把名称放进 `closureSpec` 字段，被 `TestEveryPersistencePointIsRegistered` 拒绝（`persistence point must be named`），已改正。

检查（基于 `b2d9e66` 的最终工作树）：

- `make check-code`：fmt、普通及 fault lint（0 issues）、规则检查、全部普通构建 race 测试通过；修正持久化点后以 `CHECK_PACKAGES='./core/... ./cmd/... ./conformance/sessions ./conformance/admission'` 重跑通过（admission 842.8s）。
- `make test-fault`：完整 fault 构建范围（`./conformance/...`、`./infra/sqlite/...`）通过，`conformance/fault` 4629.5s，总计 1:17:19。

未运行完整 `make check`（生成代码检查与 buf 未受影响）；最终集成验收按开发规范 4.1 执行。M1 验收源映射保持为冻结树的历史记录，未随测试调用改名更新。
