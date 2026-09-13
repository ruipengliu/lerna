# 01：SDK 契约验证样例

本实现对应 [01 票](../../.scratch/harness-implementation/issues/01-sdk.md) 的 Agent Brief。任务消息、动态 Schema 与操作身份以已确认的[消息目录](../architecture/03-contract-message-catalog.md)和[标识模型](../architecture/03-identifier-model.md)为依据。

## 使用与固定依赖

参考环境：Go **1.26.1**、protoc **36.1**。模块最低 Go 版本为 1.25.0，但本票实际验证固定到 1.26.1。`make` 和脚本显式设置 GOTOOLCHAIN；直接调用 `go` 时应自行使用同一版本，报告记录实际运行版本。

| 资产 | 固定方式 |
| --- | --- |
| Protobuf Go runtime / protoc-gen-go | 同为 v1.36.11，生成器直接从同一 go.mod 依赖构建；go.sum 锁定模块校验值。 |
| JSON Schema 验证器 | github.com/santhosh-tekuri/jsonschema/v6 v6.0.3，启用格式断言与词汇断言，固定 2020-12。 |
| 间接运行依赖 | golang.org/x/text v0.14.0，及 go.sum 中的验证/测试依赖；不自动使用 latest。 |
| 固定消息 | proto/harness/v1/contract.proto 为唯一结构来源；生成的 Go 类型提交到仓库。 |
| 动态 Schema | 注册集中的原始文档以 SHA-256 摘要绑定；示例资源编译进夹具，报告记录摘要。 |

Protobuf 官方建议生成器与运行库使用相同版本；生成流程遵循[官方 Go 指南](https://protobuf.dev/reference/go/go-generated/)。Schema 验证器的编译资源注册、离线加载器和格式断言使用其 [v6.0.3 API](https://pkg.go.dev/github.com/santhosh-tekuri/jsonschema/v6@v6.0.3)。

```sh
make generate                  # 检查 protoc 36.1，按锁定依赖生成 Go 类型
make build                     # 编译所有包
make sample                    # 独立 main 包导入 SDK，打印 fixture 任务引用
go run ./cmd/contractcheck -profile sdk-contract-v1
make verify                    # 完整验证，输出见 build/
```

`make verify` 比较重新生成的代码与现有生成代码，执行编译、go vet、全量 race 测试和样例。生成不一致或任一步失败都会返回非零退出码；`build/verification-stages.json` 保留该阶段失败及后续未运行状态。修改 proto 后先显式 `make generate` 并审查生成差异，再运行 verify。

首次工具链/模块下载是构建行为。运行样例和 Schema 验证不进行联网检索或远程 `$ref` 获取，无外部常驻服务依赖。protoc 由开发环境提供，脚本不自动下载安装二进制。

## 接口与依赖边界

- `sdk.Client.Submit` 接收 `Submission`，经消费方注入的 `Transport.Exchange` 交换 Protobuf 字节，返回经过关联校验的响应。动态参数校验器也是接口依赖。
- `protocol` 检查样例支持的请求/响应结构、presence、版本、标识及尺寸；生成类型位于 `gen/harness/v1`，无数据库或模型供应商类型。
- `schema.Registry` 建立不可变的、每类型选择一个精确版本的注册集；同一注册集可被并发读取。注册时拒绝身份冲突、非法文档和未登记引用，支持注册集内部引用。
- `contractfixture` 是显式内存契约夹具，保留当前实例的消息与操作语义，用于观察身份行为；没有授权、持久化、任务执行或真实副作用。
- `conformance.Run` 接受具名 `Profile` 和检查函数，比较预期与实际结果；后续 profile 可以登记错误、权限或恢复用例。`profiles/sdkcontract` 是首个实现，CLI 只暴露已登记的 profile。

样例 `.proto` 定义信封、Submit 请求/响应、TaskRef、动态载荷、夹具证据和最小错误集合。TaskService.Submit 描述符使用信封承载关联与操作身份，未生成或实现 gRPC 服务；其余原生方法、完整错误确定性、bootstrap、流、deadline、授权上下文及 UI 交互留给相应后续票。未实现的主体或版本被拒绝，不能将此子集用作生产端云协议。

## 校验与身份语义

固定字段使用 Protobuf；goal 是 optional，以区分缺失与空值，样例两者均拒绝。请求不能带 reply_to，响应必须关联原请求并匹配命名空间和 operation_id。成功只返回 `EVIDENCE_CONTRACT_FIXTURE`，不返回持久化受理或任务完成承诺。

动态 JSON 绑定类型、Schema ID、版本和原始文档摘要。所有类型在此 profile 中都是必需类型，未知时拒绝；未实现可选扩展忽略策略。Schema 资源必须为带固定 2020-12 声明及匹配绝对 `$id` 的对象文档，不支持运行中增改注册集。

JSON 严格解析拒绝重复键（包括转义后同名键）、非法 UTF-8、尾随数据以及超限输入；数字保留精确值，不先转 float64。整数绝对值不得超过 9007199254740991，精度敏感整数用 Schema 约束的十进制字符串表示。样例正反例包含 required 缺失/null、可空字段、长小数、日期有效性和时区。所有格式均按验证器已支持的词汇处理，不能把自定义关键字当作已有实现。

本 profile 的资源上限为：信封 2 MiB，JSON 文档/载荷 1 MiB，JSON 递归层级 64，数字 token 长度 128、指数范围 [-308,308]；这些是首个样例的固定约束，不是端云协商实现。

原消息重发保留身份并返回原响应；新消息可以引用同一操作且得到同一任务引用。既有消息换内容或既有操作换意图被拒绝。操作语义比较忽略 JSON 键顺序、空白及等值数字表示，区分缺失与 null，不用原始序列化字节作为操作语义。命名空间隔离操作身份。fixture 的已接受记录仅在当前实例有效，应在每个验证用例使用新实例；它不是可无限接收不可信请求的服务。

## 报告与退出码

`build/sdk-contract-report.json` 包含 profile/版本、配置和 Schema 摘要、实际 Go/平台、构建修订及 dirty 状态（不可用则明确标记）、依赖版本/校验值、命令、每项输入、预期、实际、结果和限制。通用报告入口不自动收集凭据；后续用例应只提供适合披露的输入描述。

结果为 `not_implemented`、`not_run`、`passed`、`failed`、`unsupported`。声明 passed 不能代替执行；任何必需项非 passed 或任意断言失败都会使 profile 失败。没有必需项或空 profile 也不通过。预期拒绝被正确观察才算负例通过，异常不会自动当成预期拒绝。

CLI 退出码：0 表示当前 profile 通过，1 表示未通过（含未知 profile），2 表示命令使用、初始化或报告输出错误。真实任务运行、跨进程互操作、其他协议主版本分别列为非必需的未实现、未运行、不支持；它们不会被 fixture 的通过覆盖。生成和编译由独立阶段报告记录，契约 runner 不伪造这些检查。

## 验收记录

2026-09-10，在 linux/amd64、Go 1.26.1、protoc 36.1 下对源码修订 `984761b1c803a0b2168ca4ee58f73345095d372e`（构建时 clean）运行 `make verify`：依赖校验、生成一致性、编译、go vet、全量 `go test -race ./...`、契约 runner 和 SDK 消费样例全部通过。此后只补验收文档及票据记录。

[完整契约报告](evidence/01-sdk-contract-report.json)有 32 项必需断言通过，另有 1 项未实现、1 项未运行、1 项不支持，均为显式范围外证据。[验证阶段报告](evidence/01-verification-stages.json)独立记录生成、编译与测试结果。SDK 消费样例返回关联 `message-1` 的 `fixture-response/1`，证据类型为 `contract_fixture`。

[双轴审查记录](01-sdk-contract-review.md)：Standards 无发现；Spec 的 1 项 P2 已修复并复核通过，当前无未解决发现。真实服务、真实模型和千级 API 质量评测不属于本票。
