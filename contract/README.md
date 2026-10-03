# 公共合同 1.0.0

本包供 Application / Component 公开边界使用。机器来源为 [`schema/1.0.0/values.json`](schema/1.0.0/values.json)；Go 与 TypeScript 类型由仓库生成器 **1.0.0** 同版生成并嵌入完整 Schema，运行时不读取源码目录，也不下载远程 Schema。

```go
revision, err := contract.Decode[contract.Revision]([]byte(`"9007199254740993"`))
wire, err := contract.Encode(revision)
```

```ts
import { decode, encode } from '@lerna/contract';
const revision = decode('Revision', '"9007199254740993"');
const wire = encode('Revision', revision);
```

ID 是区分大小写的 1..128 字节 ASCII 不透明值，不做 trim 或规范化。Revision、ContentRef.version、byte_length 和 Amount.integer_value 均为规范非负十进制字符串，范围 `0..9223372036854775807`，不得经过 JavaScript number。Amount.unit 是明确的计费单位，如 `USD:micro`；内部累计可使用 BigInt，并显式转回字符串，不隐式换币种或尺度。

Time 保留原始 UTC 字符串，精度固定六位微秒；验证真实 Gregorian 日期、年 0001..9999 和秒 00..59，拒绝偏移、自动日期修正、闰秒和精度截断。ContentRef 同时保留准确负责方、版本、SHA-256、介质类型与字节长度，本身不授予读取权限。ObjectRef 使用 tenant_id / owner_id / kind / id，revision 可省略但不可为 null；精确内容使用另有 version 的 ContentRef。

CollectionView 首版闭合 items 为 ObjectRef[]，最多 1000 项；exhausted 与 nullable cursor 对应，partial 等于 gaps 非空。cursor 最大 4096 个 Unicode 码点，gaps 最多 100 条，原因仅为 retention / authorization_changed / source_unavailable。read_scope 固定单 owner 和对象类型，watermark 表示该读取范围的提交水位，不能当对象 revision、跨 owner 序列或全局快照。items/cursor 不承诺完整全球数据视图。

Go `ParseJSON` 和 TS `parseJSON` 接受最多 1 MiB UTF-8 正文、最多 64 层容器；拒绝重复解码后键名、非法 UTF-8、孤立 surrogate、BOM、尾随值和 JSON 数字 token。首版数值全部使用字符串。Go `Decode` / `Encode` 和 TS `decode` / `encode` 先执行同一闭合 Schema 语义；编码也验证原值和原始边界，不默认补值或替换非法文本。`Validate` / `validate` 用于已经解析的数据，不能替代外部原始字节入口。

验证器为 Go jsonschema **v6.0.2** 和 TS Ajv 2020 **8.17.1**，启用 format 断言并关闭类型强制、默认补值和未知字段清理。小生成器仅支持当前结构及条件关键词；碰到未知影响语义的 Schema 关键词硬失败。生成类型负责结构，完整 Schema 是验证依据；本任务提供 `command.get` 请求合同与验证入口，尚未提供回执、事实读取或网络 profile。

共同夹具在 [`conformance/fixtures/1.0.0/values.json`](../conformance/fixtures/1.0.0/values.json)。`make test` 验证各语言公开入口；`make test-contract` 驱动双向真实编解码；`make generate` 重建生成物，`make check` 校验零差异。

## 命令验证入口

Go `ParseCommand` / TS `parseCommand` 返回通用 `CommandEnvelope`，只证明原始 JSON 及信封形状有效。通用 payload 为有界动态 JSON 对象（最多 1024 个顶层属性），可保存历史或尚未开放方法的业务内容；这个结果不提供执行资格。格式合法的 `task.pause` 信封仍不会成为已开放的方法。

Go `DecodeCommand` / TS `decodeCommand` 在通用验证后，按准确 `(contract_version, profile, method)` 选择完整方法 Schema。当前只有 `1.0.0 / command / command.get`，返回具体 `CommandGetRequest`。方法映射直接从 Schema 中的三个 const 生成，登记的 payload 在所有深度都必须闭合。已知方法 payload 无效为 `schema_invalid`，未知版本为 `version_unsupported`，已知版本但未登记的 profile / method 为 `unsupported`；不会自动降级。

`command.get` 的信封 command_id 是本次读关联身份，payload.command_ref 则保存原 owner 和原 command_id。target 必须是相同 tenant_id / owner_id、kind=command、id=原 command_id，不允许 target.revision 或 expected_revision。查询的 accept_before 只规定本次读取开始截止；格式验证不查看当前时钟，以便保存原请求与计算准确摘要。

trace_context 首版为可选闭合对象 `{trace_id:ID}`，不授予权限，也不参与摘要。未知字段、显式 null 的可选字段以及数值 token 均拒绝。Go `Validate` / TS `validate` 也检查程序构造的动态值，拒绝数字、非 JSON 值和过深对象；编码检查原始值，不能让序列化静默删除 undefined、修补 NaN 或替换非法文本。

Go 错误可使用 `errors.As(err, &contractError)` 后判断 `Code`；TS 使用 `error instanceof ContractError` 和 `error.code`。公共错误只有 `{code}`，代码取值由 `ErrorCode` Schema 冻结。Go 发送 `contractError.PublicError`，TS 发送 `error.toPublicError()`；cause / Error 元数据仅用于本地诊断。此入口不实现身份认证、期限接纳、预算、持久去重或业务状态迁移。

命令正反例在 [`conformance/fixtures/1.0.0/commands.json`](../conformance/fixtures/1.0.0/commands.json)，与值夹具共享 Go / TS 实际编解码运行器。`make test` 同时运行公开生成命令的失败探针，防止未知 Schema 关键词、动态对象范围扩大、重复登记或开放方法 payload 静默发布。

## 原命令摘要

Go `CommandDigest(commandJSON, trustedSubjectJSON)` 与 TS `await commandDigest(commandWire, trustedSubjectWire)` 从原始 JSON 边界验证通用信封和闭合 `SubjectBinding`，然后产生 `sha256:` 加小写十六进制摘要。宿主必须从受信认证上下文提供主体绑定；调用方提交的 payload 不得替代该上下文。主体格式为 `{tenant_id, subject_id, delegation_chain:[{tenant_id, subject_id}]}`，链最多 16 项、顺序保留。这验证结构，实际身份认证、委托签名及查询权限由读取入口承担。

算法固定为 `lerna-command-digest-1`：SHA-256 输入是 UTF-8 `lerna-command-digest-1\n` 前缀加规范 JSON。字段包括 `contract_version / profile / method / target / payload / accept_before / subject_binding`；原字段存在时加入 `expected_revision`。`command_id` 是原命令键，不进入业务内容摘要；`trace_context` 不进入摘要。连接、发送次数位于传输层，不属于命令字段。更换版本、主体、owner、参数、期限或修订产生不同摘要；改变 trace 或编码空白不会改变它。

规范 JSON 使用 RFC 8785 的无数字子集：键按 UTF-16 代码单元排序，数组原序；字符串仅转义引号、反斜杠与控制字符，其他 Unicode 标量原样保留。不得 trim、NFC/NFD 转换、HTML 或 U+2028/U+2029 转义、金额换算、路径清理或浮点转换。缺省 `expected_revision` 与显式值不同，显式 null 仍属无效输入。格式验证不检查当前时钟，已过截止的原请求仍能计算原摘要。

```ts
const subject = encode('SubjectBinding', {
  tenant_id: 'tenant-a', subject_id: 'subject-a', delegation_chain: [],
}); // 宿主从已认证上下文生成；不得取自请求 payload。
const original = parseCommand(originalWire);
const digest = await commandDigest(originalWire, subject);
const retransmit = encode('CommandEnvelope', {
  ...original, trace_context: { trace_id: 'trace-next' },
}); // 原 command_id、target owner、accept_before 全部保留。
if (await commandDigest(retransmit, subject) !== digest) throw new Error('changed request');
```

修改业务内容时调用方必须创建新 command_id；本入口计算摘要，不检测数据库原键冲突。摘要可用于保存尚未开放方法的准确原内容，`fixture.write` 夹具始终未登记，不能因计算摘要成功获得执行资格；业务执行仍须完整方法 Schema、授权和接纳检查。两个原始输入各自受 1 MiB 线正文上限约束；加入主体后的内部哈希输入不是新的网络正文，不能使合法原命令被错误拒绝。

[`digests.json`](../conformance/fixtures/1.0.0/digests.json) 保存独立规范字面量与 Python hashlib 预期摘要；Go 与 TS 分别经公开入口匹配这些预期，包含非 BMP 键的 UTF-16 排序、准确大整数、控制字符、字段变化及严格反例。`make test-contract` 同时运行此套件。边界夹具的 `@B@` 以 `body_repeat_count` 个 `x` 展开，线上命令仍不包含 JSON number。
