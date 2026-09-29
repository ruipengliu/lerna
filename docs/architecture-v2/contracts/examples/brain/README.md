# Brain 构造用例

本组把内部生成格式、准确正文保存和有限计划实例化拆开审查。内部局部引用不是领域线接口；最终 Proposal 和 BrainPlan 仍使用公共 [领域 Schema](../../schemas/protocol.schema.json)，行为归 [大脑](../../../brain.md)。

| 文件 | 观察内容 |
| --- | --- |
| [generation-and-plan.json](generation-and-plan.json) | 同轮报告及引用它的计划，固定保存身份回填 ContentRef，首次计划安装 |
| [invalid-generation.json](invalid-generation.json) | 局部标识、来源环、非法产出、保存冲突、未知引用和互斥提案反例 |
| [plan-materialization.json](plan-materialization.json) | 条件核验 fail/unknown、旧证据、前序输出绑定、旧计划及参数类型 |

输入结构归 [brain-generation.schema.json](../../schemas/brain-generation.schema.json)。其中的权威快照、内容已保存结果与恢复观察均是构造前提；它们用于检查关联，不调用模型、数据库或内容服务。本文组的 ASCII 键和整数正文可由紧凑排序编码核对，完整 JCS 要求另见 [传输向量](../transport/README.md)。运行入口、覆盖与验证状态见 [review.md](../../../review.md)。
