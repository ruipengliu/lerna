# 04 双轴代码审查

维护者确认基点 `4e88885dadeee1b175c60ae452598cbfcd69b8f5`，采用 `git diff 4e88885...HEAD`。初次审查覆盖实现提交 `72442d3`；修复提交 `eff59e3` 经原审查者只读复核。两个独立审查者并行执行，未相互替代结论。

Spec 来源为 [04 票及 Agent Brief](../../.scratch/harness-implementation/issues/04-worker.md)和[实施共同规格](../../.scratch/harness-implementation/spec.md)。Standards 核对 AGENTS.md、CONTEXT.md、docs/agents 下的领域、tracker 与分诊约定，以及共同规格和 code-review 的代码异味基线。

## Standards

文档规范违规：0 项。Runner 依赖消费方接口，SQLite 与协议转换留在适配层；Brain 提案与 Core 状态推进责任明确，领域用语未偏离词汇表。

可行动的基线异味：0 项。有限操作分支和局部辅助函数有当前票据需要，未发现值得单独要求修复的重复、过度抽象或职责错置。

修复复核：提案摘要集中在单一辅助函数中，保留原始字节语义并避免持久化被拒绝的大载荷；版本校验顺序调整、针对性回归和文档说明一致。未发现新增标准问题。

## Spec

初次发现 1 项 P2：超大无效提案会阻止停止状态落盘。规范要求“为……无效提案……明确停止原因及 WAITING/FAILED 等适用状态”。代码虽设置 FAILED/invalid_proposal，却将完整无效提案保存在提交日志中；结果超过运行分区的 8 MiB 上限时，整个事务返回 UNAVAILABLE，任务仍为 RUNNING，停止原因丢失。

修复：保存规范化 WorkChange 与原始提案类型/结果字节的 SHA-256 摘要，支持原身份核对而不保留拒绝的大载荷。公开接口回归验证 9 MiB 无效结果能够保存 FAILED/invalid_proposal，同请求重放返回原回执，不同载荷产生身份冲突。另将提案基线版本校验前移，过期的无效提案不能推进当前状态。

原审查者复核确认 P2 已关闭，未发现新增问题。

Standards：0 项、未解决 0 项；Spec：1 项 P2、已关闭 1 项、未解决 0 项。最终修订通过 [完整验证](evidence/04-verification-stages.json)。
