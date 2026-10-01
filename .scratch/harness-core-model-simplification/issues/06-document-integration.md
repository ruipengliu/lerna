# 06：架构导航与交付整合

Status: ready-for-agent
Progress: completed
Blocked by: 01, 02, 03, 04, 05

## Scope

同步架构主入口、详细总览、工程阶段与既有 Orchestrator 导航，使普通应用从少量核心概念进入，扩展与底层契约按需深入。完成全范围静态与兼容性核对，准备双轴审查。

Owned files: `docs/architecture/README.md`、`.draft/README.md`、`.draft/technical-overview.md`、`.draft/engineering.md`、`.draft/walkthrough.md`、`.draft/review.md`、`docs/architecture/ochestrator/README.md`；`.scratch/harness-core-model-simplification/verification.md`。根协调者保留 README、spec 状态与最终审查记录的修改权。

## Acceptance

- [x] 主入口、对象清单、默认应用流程、四类读写、参考覆盖矩阵与验收形成完整阅读路径；已有正文链接正确，无重复权威。
- [x] 最小开发装配与生产 ADR 要求准确区分，静态配置／按需扩展不成为普通请求额外前提。
- [x] 受影响架构／研究／票据链接与结构检查通过；机器 Schema、方法、验证程序、ADR 决策及历史来源基线保持未变，公开方法仍 105 个。
- [x] 交付记录明确实际检查与未运行项，记录固定比较点、分支、票据和追踪入口。
- [x] 所有依赖票据已完成并合入，为双轴 code-review 提供明确 diff 与规格来源。

## Comments

- 2026-10-01：本票完成后根协调者执行标准／规格双轴审查，统一修复问题，再关闭整个规格并清理实施工作树。
- 2026-10-01：从已合入 01～05 的集成起点 `d291e03c8285475988dfa51c1e46891bb2286e79` 创建 `codex/core-model-06`。读取 AGENTS、领域与 ADR、规格及 tdd 技能，沿用既定应用／SDK 验收切面；本票仅交付文档整合，不新增运行测试或编造 red／green。
- 2026-10-01：七份 Owned 架构文档已接通六组对象、默认应用工作流、四类读写、参考矩阵与 CM-01～17；保留九模块裁决、开发／生产 ADR、静态配置和按需合同缺口，沿用既有 ochestrator 目录。完整交付入口与复现命令见 [verification](../verification.md)。
- 2026-10-01：复用原文档检查器，architecture 为 60 篇／1846 本地链接／124 Mermaid 围栏；ADR 10 篇／13 链接，综合研究 9 篇／179 链接，五份项目报告合计 162 链接，均 0 错误。固定 review base 的保护范围 diff 与空白检查通过，105 个公开方法保持；54 条故事、24 项决策及 17 组验收编号连续，01～05 全部完成且已合入。
- 2026-10-01：本票完成设计整合；未重跑未变化机器向量，未执行 Harness、SDK、数据库、模型、平台、性能或 Mermaid 渲染验证。标准／规格双轴审查、统一修复和整个 spec 关闭仍由根协调者收尾。
