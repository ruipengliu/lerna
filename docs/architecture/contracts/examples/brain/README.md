# Brain 内部产出与有限计划构造用例

运行 `python3 docs/architecture/validation/validate_brain.py`。

这些资产校验内部模型输出的结构、局部引用、准确正文摘要、原保存命令标识，以及给定权威快照上的计划安装与实例化分支。它们不调用模型、数据库或内容服务；恢复向量表示“原内容保存已可查”的构造事实，不能证明真实崩溃恢复或只保存一次。

| 文件 | 覆盖 |
| --- | --- |
| [generation-and-plan.json](generation-and-plan.json) | 同轮生成报告和引用它的计划；沿固定保存身份回填 ContentRef；以 actions=[]、null 基线安装首次计划 |
| [invalid-generation.json](invalid-generation.json) | 缺失／重复局部标识、来源依赖成环、非法产出字段、存储结果冲突、未知引用及直接行动／计划路径冲突 |
| [plan-materialization.json](plan-materialization.json) | applied 的评估仍可 fail；unknown、失效／旧候选 pass 不放行；未来步骤及已有原操作两种输出绑定；旧计划和目标、参数缺口及类型错误 |

内部格式由 [brain-generation.schema.json](../../schemas/brain-generation.schema.json) 定义；最终 Proposal、BrainPlan 仍按 [protocol.schema.json](../../schemas/protocol.schema.json) 校验。用例输出正文的 JSON 键均为 ASCII，数值仅为整数，检查器的紧凑排序编码在此集合内与 JCS 一致；完整 JCS 实现另按传输向量验收。运行故障实验见 [BI-14–16](../../../brain/implementation.md#8-可重复故障实验)。
