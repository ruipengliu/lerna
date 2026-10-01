# 核心模型收敛双轴审查

日期：2026-10-01。使用仓库 code-review 技能，两个代理独立、并行、只读审查；以下保留修复前报告，修复结果单列。

- 固定比较点：`1b647dc970317173f349296e47efd41a4cf3a23c`。
- 审查 HEAD：`08d078adbc3624f65a750bd23322b803f1c5fb72`。
- 差异：`git diff 1b647dc970317173f349296e47efd41a4cf3a23c...HEAD`；审查期间 HEAD 保持上述提交。
- 规格：[spec.md](spec.md)。标准：根 [AGENTS.md](../../AGENTS.md)、[领域词汇](../../CONTEXT.md)、[issue tracker](../../docs/agents/issue-tracker.md)、[triage](../../docs/agents/triage-labels.md)、[领域指引](../../docs/agents/domain.md)及适用 [ADR](../../docs/adr/)。Standards 同时采用技能规定的完整 Fowler 异味启发，仓库规则优先。
- 交付范围：架构设计和静态检查；本次审查不产生运行、数据库、模型、平台或性能证据。

## Standards

**S-1 [P3，明确规范违规] 新评论应追加到历史末尾。** [spec.md](spec.md) 第 193 行新增的“用户调用 implement-spec 实施本目录”插在已有创建和验证记录之前。[issue-tracker.md](../../docs/agents/issue-tracker.md) 第 11 行明确要求：“Comments and conversation history append to the bottom of the file under a `## Comments` heading”。将该新增条目移到现有 Comments 末尾即可。

未发现本次架构差异引入的 ADR 冲突或有依据的基线异味。Standards 共 1 项明确违规，0 项判断性发现。

## Spec

未发现可确认的规格遗漏、范围扩张或错误实现，Spec 轴 0 项发现。

六组对象归属、默认接口字段及成功点、四类请求流程、配置与扩展边界均与规格一致；冷恢复已区分重建和后续续行。五个固定快照的关键恢复语义抽查与矩阵相符，现有机器契约和 ADR 未改变。

本结论限于文档设计与静态语义核对；CM-01～17 仍为待运行验收，未据此认定运行正确性或性能收益。

## 协调检查补充

以下由根协调者交叉核对发现，独立于上述两轴报告，不合并或重排两轴发现。

| 编号 | 位置与修正范围 | 状态 |
| --- | --- | --- |
| C-1 | [对象目录](../../docs/architecture/.draft/core-data-model.md)：InputRequest 的读取入口应为 interaction.request_read；input_read 读取 InputSubmission | 待统一修复 |
| C-2 | [请求流程](../../docs/architecture/.draft/request-data-flows.md)：把 CM-03 已明确的 C 重建计量终点与后续续行，同步到流程定义及计量正文；不改变恢复规则或历史研究 | 待统一修复 |
| C-3 | [追踪表](traceability.md)：ID-24 及当前整合状态须反映票据 06 已完成，运行状态仍未验证 | 待统一修复 |
| C-4 | [票据 05](issues/05-acceptance-traceability.md)：保留全部 Comments，按创建、开始、完成及检查的顺序组织 | 待统一修复 |
| C-5 | [验证记录](verification.md)：将未重跑机器向量的说明限定到票据 06，保留票据 04 实际执行过协议／Brain 静态检查的事实 | 待统一修复 |

## 修复与复核

所有修复由同一个实现代理在独立工作树和 `codex/core-model-review-fix` 分支完成。当前待修复合入、针对性复核与最终静态检查；整个规格尚未关闭。完成记录将保留修复提交和实际证据。

初审：Standards 1 项，最严重为 P3 评论追加顺序；Spec 0 项，无严重项。协调补充另计 5 项。
