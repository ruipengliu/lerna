# 09 实施审查

用户确认基点 `dea600fa09590a37601bd59de4e03a7e8496e36d`。按 code-review 技能独立并行审查 Standards 与 Spec；固定比较命令 `git diff dea600f...HEAD`。实现提交 `3674aff`、`8028fd6`；修复提交 `cb32358`。

规格为 `.scratch/harness-implementation/issues/09-updates.md` 的已确认 Agent Brief 及工作包共享约束。标准源为 AGENTS.md、docs/agents、CONTEXT.md 和工作包规范，同时采用技能的判断性 code smell baseline。

## Standards

初审无硬性规范违规；一项 P3 判断性 Duplicated Code：交互页重复 InputFact 编解码，与任务快照所用 helper 并存，容易在扩展字段时出现差异。

已复用 `encodeFacts` / `decodeFacts`，复核确认原项关闭，新增运行收尾和回答关联改动未发现新的硬性规范违规或需报告的判断性问题。

最终剩余：硬违规 0 项，判断性问题 0 项。

## Spec

初审两项实现错误：

- P1：工作 start 已持久化、生成预留尚未开始时更新任务，预留因旧版本失败，Runner 直接返回并遗留 InFlight。现在通过已停止的控制冲突路径收尾；精确屏障测试先复现失败，再验证无模型调用、无预留泄漏并重新排队。
- P2：回答只有独立文本，缺少原问题及对应身份；多个问题共用 `yes` 引用时语义丢失。现在持久绑定 QuestionRef/InteractionID，原问题经受控来源及大小检查进入上下文，答案块携带多项 ReplyTo。两种不同问题共用答案引用的 profile 验证关联及问题正文。

独立复核确认两项关闭，增量未发现新增规格偏差或范围扩张。复核者另外执行任务竞态及输入契约测试，通过。

最终剩余：0 项。

Standards 初始 1 项、Spec 初始 2 项；均已修复，两个维度最终均无未解决项。测试和契约报告见实施指南的最终验证记录。
