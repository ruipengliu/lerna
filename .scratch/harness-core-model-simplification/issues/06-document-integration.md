# 06：架构导航与交付整合

Status: ready-for-agent
Progress: pending
Blocked by: 01, 02, 03, 04, 05

## Scope

同步架构主入口、详细总览、工程阶段与既有 Orchestrator 导航，使普通应用从少量核心概念进入，扩展与底层契约按需深入。完成全范围静态与兼容性核对，准备双轴审查。

Owned files: `docs/architecture/README.md`、`.draft/README.md`、`.draft/technical-overview.md`、`.draft/engineering.md`、`.draft/walkthrough.md`、`.draft/review.md`、`docs/architecture/ochestrator/README.md`；`.scratch/harness-core-model-simplification/verification.md`。根协调者保留 README、spec 状态与最终审查记录的修改权。

## Acceptance

- [ ] 主入口、对象清单、默认应用流程、四类读写、参考覆盖矩阵与验收形成完整阅读路径；已有正文链接正确，无重复权威。
- [ ] 最小开发装配与生产 ADR 要求准确区分，静态配置／按需扩展不成为普通请求额外前提。
- [ ] 受影响架构／研究／票据链接与结构检查通过；机器 Schema、方法、验证程序、ADR 决策及历史来源基线保持未变，公开方法仍 105 个。
- [ ] 交付记录明确实际检查与未运行项，记录固定比较点、分支、票据和追踪入口。
- [ ] 所有依赖票据已完成并合入，为双轴 code-review 提供明确 diff 与规格来源。

## Comments

- 2026-10-01：本票完成后根协调者执行标准／规格双轴审查，统一修复问题，再关闭整个规格并清理实施工作树。
