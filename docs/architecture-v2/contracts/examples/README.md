# 完成判断的契约示例

[任务规则](../../orchestrator/README.md#state) · [共同契约](../README.md) · [系统验收](../../validation/README.md)

本组示例用于评审任务终态、完成依据、外部效果和未结费用之间的关系。先按下表选择状态场景，再运行校验器检查投影中的结构与关联。

文件是为审查抽取的状态投影，不是完整 WSS／gRPC 消息、数据库转储或实际任务记录。示例中的短 ID 仅为可读占位，生产 ID 必须满足共同契约的随机性要求。完整信封、已冻结方法的输入／输出、回执和多步轨迹见[协议示例](../protocol.md)，两组检查分别声明覆盖范围。

本组没有完整原目标或内部目标覆盖记录，不能检出「所有已登记条件通过但原要求漏项」；该保证须按[目标覆盖组合用例](../../validation/optimization-evidence.md#scenarios)另取运行与独立质量证据。

投影含 task 的标识、目标修订、状态和必要条件 ID；result 为[Orchestrator Result](../../orchestrator/README.md#records)或 null；operations 为[执行事实](../../execution/README.md)中需要判断完成的字段；accounting_open 单列费用核对。真实实现必须从同一任务的完整权威操作集合派生这个投影，不能漏掉操作或必要条件以制造通过。

| 文件 | 要说明的规则 |
| --- | --- |
| [succeeded-assessed.json](succeeded-assessed.json) | 报告质量经通用评估并明示尚无专项校准，文件效果已证实；最终费用可以继续核对 |
| [succeeded-verified.json](succeeded-verified.json) | 仅包含客观检查覆盖的必要条件，才标 verified |
| [succeeded-user-accepted.json](succeeded-user-accepted.json) | 用户验收质量，但外部效果仍有确定证据 |
| [active-unknown-effect.json](active-unknown-effect.json) | 写入不明时保留 active 与原操作，不能发布成功结果 |
| [cancelled-late-effect.json](cancelled-late-effect.json) | 取消后收到写入成功，仅补原效果，任务不复活 |
| [invalid-cases.json](invalid-cases.json) | 未知效果、未来效果、保证抬高、必要条件缺失、旧目标／旧成果证据、条件失败、取消与成功冲突以及跨租户引用均拒绝 |

[Schema](../schemas/task-outcome.schema.json)采用 [JSON Schema 2020-12](https://json-schema.org/draft/2020-12)，检查结构、枚举和成功时的效果限制；[校验器](../../validation/validate.py)另检查目标、成果和条件之间的关联。ContentRef 字段来源在[记忆契约](../../memory/README.md)，示例校验不能证明实际字节存在、来源合法或授权有效。

从仓库根目录执行：

```sh
python3 -m pip install -r docs/architecture-v2/validation/requirements.txt
python3 docs/architecture-v2/validation/validate.py
```

应接受 5 个正例，拒绝 9 个反例。上述仅证明文档示例与声明规则一致，不证明事务、模型质量、设备操作、权限、恢复或互操作已经运行通过。

原投影只检查完成时固定的条件结果，不携带当前缺陷门禁。规则通过原任务、目标修订及条件定位；当前证据适用性、普通停用与判断缺陷的区别见[任务验证](../../orchestrator/verification.md)。静态正例通过不能证明实现已经核验当前缺陷，也不能证明评估准确率。
