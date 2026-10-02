# 结构化输入回答字节

[应用接入流程](../../../application-workflow.md) · [回答正文 Schema](../../schemas/input-answer.schema.json) · [检查入口](../../../validation/validate_input_answers.py)

本目录的 9 份回答文件保存 `input-answer/1` 的真实 UTF-8 JCS 字节，没有末尾换行。`cases.json` 保存每份正文的准确 `InputAnswerContentRef`、当前 `InputRequestView`、观察时间，以及适用的批准、队列和转交记录。协议序列 07、12、66 的全部 9 处 `answer_ref` 也指向这些已核对字节。问题、预览及候选内容仍是记录前提，本检查没有取得或验证它们的正文。

| 回答文件 | 核对内容 |
| --- | --- |
| [directory-text.json](directory-text.json) | 目录文本、必需预览、排队保存和原 `task.input` 转交。 |
| [directory-choice.json](directory-choice.json) | 目录选项使用 ID，显示标签可以独立变化。 |
| [five-types.json](five-types.json) | 五种字段；Unicode 码点、最小安全整数、`false`、选择声明顺序及可选字段省略。 |
| [five-types-empty-selection.json](five-types-empty-selection.json) | 最大安全整数、`true`、必填多选的空数组、原始空格及可选空文本。 |
| [text-preserves-whitespace.json](text-preserves-whitespace.json) | 空格和组合字符按原字节保存，不 trim 或 normalize。 |
| [text-empty.json](text-empty.json) | 必填文本要求字段存在，允许空字符串。 |
| [optional-field-omitted.json](optional-field-omitted.json) | 可选字段省略；省略与 `null` 不等价。 |
| [application-fields.json](application-fields.json) | 应用输入没有任务绑定，也不携带确认消费命令。 |
| [acceptance-approved.json](acceptance-approved.json) | 唯一 `accept` 决定、完整固定消费者、原 approved Confirmation、队列及交付命令全等。 |

112 条回答反例覆盖媒体类型、摘要、长度、UTF-8、重复键、非 JSON 数字、安全整数、JCS 字节、未知字段、类型、动作、选择、请求版本、候选、确认状态、意向摘要和消费者替换。一般变更会重新计算正文摘要和长度，使反例确实到达指定规则；摘要或长度错误例再单独破坏该引用。超过 1 MiB 的正文在检查时构造，不另存大文件。

另有 3 组 `Task.read` 输入发现投影和 10 条反例：完整披露时 `WaitReason.object_ref` 必须准确指向本任务当前 InputRequest；未披露时省去输入等待项，以 `QueryResult.gaps` 表达缺口，不能启用回答。非 input 等待仍可省略对象引用。[序列 66](../protocol/66-input-request-discovery.json) 给出无 Surface 的读取、准确请求查询和直接回答；检查器还核对协议序列中的 1 处输入等待引用。

在仓库根目录运行，需 Python、`validation/requirements.txt` 中的依赖和 Node：

```sh
python3 docs/architecture/validation/validate_input_answers.py
python3 docs/architecture/validation/validate_protocol.py
python3 docs/architecture/validation/validate_transport.py
```

正文 Schema 的 20 项共享定义与 `protocol.schema.json` 逐项核对，引用全部在本地解析；JCS 和 Confirmation 意向摘要复用现有传输校验实现。`cases.json` 的队列、批准观察、发现投影和变更路径只属于检查前提，没有增加公共 RPC 或业务输入字段。通过检查说明这些具体字节和记录关系符合合同，不证明真实身份、当前权限、可信呈现、原子消费或耐久提交成立。
