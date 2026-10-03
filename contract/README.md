# 公共值合同 1.0.0

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

验证器为 Go jsonschema **v6.0.2** 和 TS Ajv 2020 **8.17.1**，启用 format 断言并关闭类型强制、默认补值和未知字段清理。小生成器仅支持当前结构及条件关键词；碰到未知影响语义的 Schema 关键词硬失败。生成类型负责结构，完整 Schema 是验证依据；本任务尚未开放命令方法、回执或网络 profile。

共同夹具在 [`conformance/fixtures/1.0.0/values.json`](../conformance/fixtures/1.0.0/values.json)。`make test` 验证各语言公开入口；`make test-contract` 驱动双向真实编解码；`make generate` 重建生成物，`make check` 校验零差异。
