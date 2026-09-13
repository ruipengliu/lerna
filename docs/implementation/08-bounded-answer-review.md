# 08 票双轴代码审查

按 `code-review` 技能由两个独立子代理并行审查。用户已确认基点 `e97bddc3891a07e17ad991ab1288055982a3cff3`；比较命令 `git diff e97bddc...HEAD`，初始实现提交 `8b47dbe`。规格为 [08 Agent Brief](../../.scratch/harness-implementation/issues/08-answer.md)；规范来源 AGENTS.md、CONTEXT.md、docs/agents/domain.md 和本地 issue 约定。最终修复提交 `56e6fdb`、`0eaae95`。

## Standards

初审：规范硬违规 0 项，启发式 1 项。

**[P2] Possible Shotgun Surgery / Duplicated Code：答案生成、查询与恢复的大小契约不一致。** 原 Brain 允许 64 KiB，查询与恢复固定 8 KiB；合法配置可能发布后不可读。统一为 `brain.MaxInputBytes/MaxAnswerBytes`，构造只允许收紧，共同调用方共享常量。

首次复核补出同一问题的编码膨胀路径：原始 JSON 合格，规范编码后 `<` 等字符转义可能超过 8 KiB。现已在最终编码后、保存前再次检查大小；`encoded-output-overflow` 先失败后通过，证明不发布不可读答案。

最终独立复核：原发现关闭，硬违规 0 项、启发式剩余 0 项。该发现属于启发式判断，不标作仓库规范硬违规。

## Spec

初审：2 项，无明显范围蔓延。

1. **[P1] 缺失用量被结算为零。** 违反“未知/异常用量不被伪装成零或正常结算”。原 uint64 零值使缺字段/null 混入已知消耗；现从精确 JSON 字段读取三个必需整数，缺失、null、异常总数或超限均不标记 Known。大小写别名不能覆盖原计数。新增测试先复现后修复。
2. **[P2] 固定 Schema 未严格校验字段名。** 违反“格式不合约时不能发布成功”。Go typed JSON 解码大小写不敏感，原可接纳 Answer/Sources 及混合覆盖；现先要求精确两键 answer/sources，再执行类型与语义解码。新增大小写与混合字段拒绝测试。

最终独立复核：原 2 项均关闭，剩余 0 项；复核使用本地替身与公开校验接口，没有执行真实模型调用。

Standards：初始 1 项、剩余 0，最严重 P2 已关闭；Spec：初始 2 项、剩余 0，最严重 P1 已关闭。最终完整验证见 [阶段报告](evidence/08-verification-stages.json)。
