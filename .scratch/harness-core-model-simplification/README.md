# 核心数据模型收敛实施

[规格](spec.md) · [领域词汇](../../CONTEXT.md)

Integration branch: `codex/harness-core-model-simplification`
Review base: `1b647dc970317173f349296e47efd41a4cf3a23c`

该固定点保存本轮开始前已有的架构刷新和规格，后续审查只比较本次实施差异。用户于 2026-10-01 指定实施本规格。交付范围为设计文档与静态验证，运行能力和实测收益不计为已完成。

每张票的 Status 是 canonical triage，保持 ready-for-agent；Progress 用 pending、in-progress、completed 记录执行。依赖全部完成才进入 frontier；完成时勾选验收条件并在 Comments 记录提交和验证证据。实现代理在独立工作树提交，合并代理统一合入集成分支。

| 票据 | 依赖 | Progress |
| --- | --- | --- |
| [01 核心对象与数据归属](issues/01-core-object-ownership.md) | 无 | completed |
| [02 参考语义覆盖矩阵](issues/02-reference-semantic-coverage.md) | 无 | in-progress |
| [03 应用入口与四类数据流程](issues/03-application-workflows.md) | 01 | pending |
| [04 领域规则与扩展生命周期](issues/04-domain-and-extension-semantics.md) | 01、02 | pending |
| [05 验收场景与规格追踪](issues/05-acceptance-traceability.md) | 02、03、04 | pending |
| [06 架构导航与交付整合](issues/06-document-integration.md) | 01～05 | pending |

## Comments

- 2026-10-01：规格目录此前没有票据；按 implement-spec 的任务图要求补齐上述六张独立票据。沿用规格的应用／SDK 工作流及必要模块契约作为验收切面；本轮不新增运行测试或测试专用接口。
- 2026-10-01：实施前静态检查通过：架构 55 篇／1605 本地链接，ADR 10 篇／13 链接，综合研究 8 篇／134 链接；方法登记仍为 105 项。01、02 已在独立工作树并行启动，后续差异以 Review base 为固定比较点。
