# 21 · 冻结评测与受控改进发布

Status: ready-for-agent
Phase: F
Implementation: not-started
Depends on: [12](../lerna-12-research-report-app/spec.md)、[16](../lerna-16-activation-lifecycle/spec.md)、[18](../lerna-18-memory-lifecycle/spec.md)、[19](../lerna-19-bounded-delegation/spec.md)

## Problem Statement

团队需要判断记忆、多 Agent 或新策略是否改善真实成果，而不是只看模型自评、成功样本或最好一次运行；评测通过也不等于有权发布。

## Solution

交付冻结实验计划、可恢复样本运行、完整费用与结果报告，以及候选、批准、分批发布和有资格回退的流程。对照证据明确适用范围、统计限制和人工参与。

## User Stories

1. As an evaluator, I want plans frozen before runs, so that success criteria cannot change after seeing results.
2. As an application owner, I want independent outcome evidence, so that model self-evaluation cannot define success.
3. As an evaluator, I want failed and unknown samples retained, so that reported rates are not biased by filtering.
4. As an operator, I want sample identities preserved across retries, so that recovery cannot enlarge the denominator.
5. As a user, I want all branch and maintenance costs counted, so that a strategy cannot appear cheaper by hiding work.
6. As a data owner, I want evaluation purpose checks, so that task traces are not reused without permission.
7. As a release approver, I want evidence separate from approval, so that a passing experiment cannot publish code by itself.
8. As an operator, I want bounded rollout and qualified rollback, so that regression stops expansion without reviving unsafe versions.
9. As a reviewer, I want uncertainty and human assistance reported, so that autonomous success claims remain interpretable.

## Implementation Decisions

- EvaluationRun 固定样本、实验臂、模型可得版本、提示、策略、工具、环境、数据快照、根预算、重试、期限、指标与停止条件；调参与最终验收材料分离。
- SampleRun 按样本与实验臂唯一登记责任，重启继续原记录；重试不新增样本，未完成、失败和 unknown 保留总分母。
- 交付可配置配对实验：无记忆 / 获准记忆、单 Agent / 有界委派、固定 / 预算感知策略、全文 / 词法检索、规则 / 规则加独立评估、不更新 / 候选验证后发布。未实现候选标不适用，不伪装已比较。
- 所有分支、汇总、提取、索引和维护费用计入同根预算；独立真值与运行时自评隔离，实际效果由目标证据判断。
- 小样本先发现流程问题并估算方差，正式样本量按容错率和区间精度预先冻结；报告配对差异、重复运行与适当区间。供应商内部版本不可固定时注明可重复性限制。
- 正常澄清与授权按预设交互计入；人工纠错、提供答案或代做单独标记，不计自主成功，样本仍留总分母。
- 经验、策略和代码候选分别记录用途许可、证据和批准；普通获准记忆更新可自动生效，插件与内核代码只形成可审查补丁，由维护者批准发布。
- 复用 16 的 ReleaseApproval 与 Activation，分批监控错误动作、错误完成、延迟、费用和 unknown；扩大范围或改门槛需新的受信批准，回退必须有独立有效旧资格。

## Testing Decisions

测试评测与发布的公开管理接口，并通过 Application 执行样本；复用 12 真实任务、18 记忆、19 委派与 16 生命周期。用独立可知真值夹具验证统计管道，再运行冻结真实样本，二者分开报告。

测试只断言公开行为、持久恢复结果和独立目标状态；不依赖私有表布局或内部调用次数。每个故障或拒绝案例必须配允许正常完成的对照。

验收条件：

1. 冻结后更改样本、阈值、版本或预算被拒绝或生成新计划，原实验记录不可覆盖。
2. 同一 SampleRun 在丢回执或进程退出后恢复仍算一个样本；失败、超时、unknown 和人工干预保留在正确分母及分类。
3. 已知结果夹具能核对首次调用、有限重试成功、错误完成、错误拒绝及费用计算；不能删失败提高成功率。
4. 记忆与委派实验计入所有提取、维护、子分支和汇总费用，实验臂共用相同资源约束。
5. 真实冻结实验提供独立真值、全部样本结果与区间，未校准或小样本明确不能支持 90% / 95% 声明。
6. 无评测用途许可的轨迹不可导入；脱敏或模型声明不能自行解除来源限制。
7. 评测通过但无 ReleaseApproval 不能启用；有批准但实例未就绪也不能服务。
8. 质量下降触发封新使用或符合资格的回退；修改发布范围不能绕过批准，正常获准批次可推进。

## Out of Scope

- 无限自我改进、策略修改自己的门槛、未经维护者批准发布代码。
- 复现所有研究论文或强制实现每一种候选算法。

## Further Notes

本切片交付评测和受控发布能力及适用实验结果；最终 API 覆盖与生产规模在 22 验收。缺乏收益的策略保持可选或停用，不为已投入实现成本放宽指标。

依赖项表示实现先决条件；`ready-for-agent` 表示规格已明确，不表示依赖已完成或能力已开放。全部验收通过并附准确版本、环境、命令、结果和限制后，才可将本切片记为完成。共同执行与证据规则见[切片索引](../lerna-implementation/README.md)。

依据：[validation](../../docs/architecture/validation.md#能力质量与优化实验)、[validation](../../docs/architecture/validation.md#质量目标与人工参与)、[governance](../../docs/architecture/governance.md#自动改进的控制)、[data-model](../../docs/architecture/data-model.md#可选能力记录)、[ADR-0010](../../docs/adr/0010-evidence-gated-improvement.md)、[ADR-0006](../../docs/adr/0006-authorization-budget-at-action-boundaries.md)、[ADR-0009](../../docs/adr/0009-versioned-activation-and-recovery.md)。
