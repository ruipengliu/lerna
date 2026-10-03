# Ticket06：准确版本、方法支持及Schema摘要

2026-10-03。依据切片01 ticket06、当前values.json、generator、Go/TS命令摘要与已完成受信查询入口。仅读取仓库和前置实现；本文件是代决策记录，不修改产品代码，不需新增ADR。

## 最小机器登记来源

增加 `contract/schema/1.0.0/methods.json`，其内容只登记实际完成的方法输入/输出根：

```
{"methods":[{"input_schema":"CommandGetRequest","output_schema":"CommandGetResponse"}]}
```

version/profile/method从input根已有properties中的const派生，不在清单重复这三份事实。清单是唯一开放登记源：生成器不再因扫描发现某请求Schema便自动把它视为已支持。原自动识别逻辑可用于验证清单条目，但不能成为第二个支持来源。

生成时检查：所有条目字段闭合；input/output根存在；input身份三const存在且为字符串；无重复根或身份三元组；input及其payload闭合；output全部可达分支闭合；全部$ref可解析且位于当前本地schema。未列出的Schema、设计方法、通用CommandEnvelope和摘要fixture方法都不广告。首版只有1.0.0 / command / command.get，不广告core、task、memory、delegation、Schedule或Environment。

## 公开接口

批准当前实现者的最小平面类型和接口：

```
MethodSupport {
  contract_version, profile, method,
  input_schema, output_schema,
  input_schema_digest, output_schema_digest
}
SupportedMethods() -> []MethodSupport
Negotiate(rawJSON) -> MethodSupport | PublicError
```

Go/TS沿用各自命名约定；input_schema/output_schema是当前Schema资源中的准确$defs根名称（CommandGetRequest/CommandGetResponse），不是任意远端URL。MethodSupport结构由Schema生成，列表值由methods.json和源Schema生成。SupportedMethods返回防御性副本，不能让调用方改写内部支持事实。

闭合NegotiationRequest仅包含 `contract_version, profile, method, input_schema_digest, output_schema_digest` 五个必填字段。身份字段引用已有语法类型ContractVersion/ProfileName/MethodName，不能先以const1.0.0在Schema里拒绝未知版本，否则无法提供正确的version_unsupported分类。digest字段格式为 `^sha256:[0-9a-f]{64}$`。不接受semver范围、latest、备用版本或缺失摘要。

协商是无业务副作用的本地合同操作，不需要command_id、accept_before、owner路由或持久协商会话。返回描述符不是授权令牌，也不放宽后续原命令的严格验证与受信身份检查。正常证据必须串起协商 -> 构造command.get -> GetCommand受信入口 -> 严格解码回执/进展。

错误分类固定：

| 条件 | 公共错误 |
|---|---|
| 非法JSON、重复键、未知/缺失字段、错误类型、digest格式错误 | schema_invalid |
| 格式合法但准确contract_version未知/不兼容 | version_unsupported |
| 已知版本但profile或method未登记 | unsupported |
| 身份三元组受支持，但任一合法格式的Schema digest不相等 | version_unsupported |

摘要不一致说明同名合同不能被视为同版兼容，不发明新错误码。先识别版本，再profile/method，再比较摘要。不把未知方法转成新版或默认profile继续调用。

## Schema摘要的准确预映像

算法标识 `lerna-schema-digest-1`；摘要值表示为 `sha256:` 加64个小写十六进制字符。它与命令摘要使用不同域前缀，不可互换。

每个输入/输出方向单独构建确定性bundle：

```
{
  "$schema": source.$schema,
  "$id": source.$id,
  "$ref": "#/$defs/<root>",
  "$defs": {<root及从它递归可达的所有定义>}
}
```

从根定义开始，遍历实际Schema子节点中的$ref，递归收集全部可达$defs；每个定义只保留一次，使用visited集合处理潜在循环。当前只接受 `#/$defs/<Name>` 本地引用；未解析或外部引用生成失败。根定义本身也必须进入$defs。各定义按原Schema完整内容保留，包括description、format、const、约束及条件分支；不能只摘字段名或一级引用。未可达定义不纳入本方法摘要。

最终摘要：

```
SHA256( UTF8("lerna-schema-digest-1\n" + canonical(bundle)) )
```

canonical沿用命令摘要的UTF-16键排序、Unicode标量字符串规则、数组保持顺序和无空白编码；Schema元数据允许**安全非负整数**，按普通十进制整数编码（0..9007199254740991，不用指数，不接受-0、浮点或非有限值）。当前约束中的maxItems/maxLength/maxProperties都属于此范围。

这是Schema元数据语法，不能送进禁止线上JSON number的ParseJSON/parseJSON，再以失败为由删除这些约束或把它们转字符串。Go读取随包嵌入的SchemaJSON时用UseNumber保留整数词法后准确检查；TS源Schema由生成器审查数字类型/范围。源Schema属于受控构建输入，不增加接收任意外部Schema的新公开远程功能。

可最小重用内部canonical函数，为受控Schema调用显式允许上述安全整数；公共命令解析仍在进入canonical前拒绝所有原始数字，命令digest的既有golden必须保持不变。不要建立通用多版本注册框架或一般JSON数字规范化平台。

## 生成与独立验证

生成器可以输出固定MethodSupport描述符和摘要供同步SupportedMethods/Negotiate使用。但Go/TS共同golden必须包含各自实现按同一已嵌入源Schema**独立重新计算**的方法输入/输出摘要，不能只是两端读取同一生成常量再彼此比较。

可以提供小的SchemaDigest(rootName)工具入口，或在各语言合同测试中使用同包内部实现验证；不要求为测试强行扩大稳定公开API。必要黄金数据至少记录准确算法标识、root、预期digest，并通过生成一致性检查防止常量脱离机器源。

最低变更敏感性验证：改变根约束、嵌套可达公共定义约束、所引用的深层oneOf分支，摘要都必须变；仅调整JSON空白/对象键顺序摘要不变；改变未可达定义不影响该方向摘要。修改数组顺序仍改变摘要，即使某些Schema数组在逻辑上可交换，因为本规则只规范编码、不做Schema逻辑等价化。

Schema摘要证明准确机器契约标识，不证明实现正确性，也不取代Go/TS共同fixtures。format名在源Schema中，format的规范语义仍由准确合同版本和测试固定；实现变更不能借Schema digest未变偷偷扩大int64或时间接受范围。

## 交付范围

输入输出支持清单、Schema、生成类型和夹具一起维护；新增字段即使可选也必须重新协商准确合同，未来正式发布新版本不能覆写旧版本含义。当前切片尚未发布前的ticket内整合可以更新1.0.0初版材料，但切片退出时冻结该版证据。

本票据不得把支持协商成功描述为已提供生产持久查询/认证服务/网络发现；这里只完成合同元数据和可控事实源路径。整片完成仍须核对六张ticket全部证据，包括独立的ticket03命令摘要；06不能替代它。
