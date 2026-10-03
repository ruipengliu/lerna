# 研究结论与设计依据

本目录保留能继续用于设计、实现和评审的结论、比较分析及必要来源。现行实现合同以[技术设计](../architecture/README.md)与[ADR](../adr/)为准；研究建议和历史检查结果不自动成为现行要求，也不表示代码或生产能力已经验证。

## 1 先读哪些结论

- [五项目综合优化分析](agent-harness-comparison/architecture-optimization.md)：先保留稳定的事实归属与恢复责任，再借鉴上下文、工具绑定、输入竞争及扩展组织
- [六组核心对象与语义覆盖](agent-harness-comparison/core-model-semantic-coverage.md)：判断参考行为由哪些对象承载，避免按上游组件数量增加本项目模块
- [数据对象与读写比较](agent-harness-comparison/data-flow-io-comparison.md)：区分逻辑记录、实际持久化、暖请求和冷恢复，不能用静态计数冒充性能测试
- [各模块优化建议](ai-report-2026-09-project-implications.md)与[一手来源核验](ai-report-2026-09-source-check.md)：保留建议与证据的对应关系，收益仍需独立实验

## 2 按主题查依据

| 主题 | 保留成果 |
| --- | --- |
| Agent Harness 实现 | [项目与版本入口](agent-harness-comparison/README.md)，包含 Codex、Pi、DeepSeek Harness、Prime Agent、Crush 五份详细报告及读写推导 |
| 长期记忆 | [开源实现](agent-memory-libraries-2026-09-28.md)、[论文](agent-memory-papers-2026-09-28.md)；重点是写入权威、时态、来源限制和删除边界 |
| 小模型与策略替换 | [System One 与相邻路线](system-one-models-2026-09-28.md)；产品主张、静态实现与待验证收益分开 |
| 形式化验证 | [方法与历史关键结果](formal-methods-for-architecture.md)；保留责任交接、反例、活性前提和运行验证边界 |
| 技术写作 | [公开文档比较](public-technical-docs-comparison-2026-09-25.md)、[公开写作样本](opus-5-5-document-samples-2026-09-25.md)、[技术品味评价](model-taste-x-2026-09-25.md) |

写作流程的局部试用留下三点：关键选择要有能区分备选方案的理由；独立读者应能简短复述职责、取舍和边界；已有解释充分时允许“不必改写”。这只是少量片段试用，没有证明新流程稳定改善整篇文档质量。当前规则见[根 AGENTS.md](../../AGENTS.md)，原试用记录保留在[Git 历史](https://github.com/ruipengliu/lerna/blob/f6b8f300dc034817cfcdac9c95ce6cfa3ee6a986/docs/research/module-design-workflow-check-2026-09-25.md)。 当时的[工作流](https://github.com/ruipengliu/lerna/blob/8cd158fdec19efb5bca5863053f8a4187cea1fab/.agents/skills/module-design/SKILL.md)和[对照案例](https://github.com/ruipengliu/lerna/blob/8cd158fdec19efb5bca5863053f8a4187cea1fab/.agents/skills/module-design/references/design-reasoning-examples.md)仅作历史来源，不是当前技能前置依赖。

## 3 来源、复核与清理原则

[来源清单](agent-harness-comparison/sources.json)保留五个项目的仓库地址、准确提交及许可核对边界。研究正文中的固定版本源码链接继续保留；架构历史引用固定到当时提交，不改指现行内容来替换原论据。论文和其他库的版本来源在对应专题报告中。

日常只维护保留成果、链接与来源元数据，检查命令见[文档验收说明](../architecture/validation/README.md#6-全仓链接与研究来源维护)。参考项目代码不常驻仓库；需要重新查源码或运行实验时，按准确 remote 和 commit 下载到独立缓存，再记录当次检验范围和结果。未下载或未运行的部分不能报告通过。

已移除旧验证脚本、机器输出、逐用例过程库存和局部试用过程记录；有用结论与适用限制已归并到本入口和形式化研究。原过程文件仍可从 Git 历史恢复，本次清理前的未提交版本另有仓库外备份。清理不改变研究时的结论，也不重新宣称已完成源码、形式化或生产实验。
