# 契约用例导航

用例分为完成判断投影、完整领域调用、内部 Brain 构造和传输向量；按要审查的边界选择一组，避免把局部记录当作完整运行证据。运行入口与验证状态统一见 [review.md](../../review.md)。

| 用例组 | 回答的问题 | 入口 |
| --- | --- | --- |
| 完成判断投影 | Task 终态、完成依据、外部效果和费用未结如何并存 | 本页下表 |
| 领域调用序列 | 原请求、回执、查询和对象修订如何关联 | [protocol](protocol/README.md) |
| Brain 内部构造 | 局部正文如何保存为准确引用，计划如何实例化 | [brain](brain/README.md) |
| 传输与证明 | 帧、连接、投递、镜像和签名如何关联 | [transport](transport/README.md) |

完成投影从同一任务的完整权威条件和操作集合派生；投影只抽取审查所需字段，短 ID 是可读占位，生产格式归 [协议](../protocol.md)。

| 文件 | 展示的情形 |
| --- | --- |
| [succeeded-assessed.json](succeeded-assessed.json) | 通用质量评估与确定文件效果，费用仍可未结 |
| [succeeded-verified.json](succeeded-verified.json) | 必要条件均在客观检查范围内 |
| [succeeded-user-accepted.json](succeeded-user-accepted.json) | 用户验收质量，外部效果仍独立取证 |
| [active-unknown-effect.json](active-unknown-effect.json) | 原写入效果未知，暂不能完成 |
| [cancelled-late-effect.json](cancelled-late-effect.json) | 取消后归并迟到效果，终态保持 |
| [invalid-cases.json](invalid-cases.json) | 效果、条件、目标/成果修订、完成强度和租户关联反例 |

投影结构归 [task-outcome.schema.json](../schemas/task-outcome.schema.json)，业务语义归 [Orchestrator](../../orchestrator.md)与 [任务验证](../../verification.md)。投影不包含完整原目标覆盖或当前缺陷门禁，也不证明内容存在、来源合法、当前授权或判断准确；这些证据按 [验证要求](../../validation/README.md)分别取得。
