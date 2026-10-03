# 切片 01 可执行技术决策（2026-10-03）

依据：根 AGENTS.md、CONTEXT.md、ADR-0003/0004/0008、architecture/contracts.md 与 data-model.md、切片01 spec及已确认6 tickets、22切片索引。以下细化现有规则，不改变领域不变量，不需新增ADR。没有修改产品代码。

## 1. 发布和扩展

- 首版准确合同版本 `1.0.0`，目录 `contract/schema/1.0.0/`，profile `command`，只广告 `command.get`。支持清单必须注明本版是合同入口和注入事实源，无生产存储或网络保证。
- 不使用设计占位 `v2-design-1`，不广告未完整实现的 `core`。
- 先解析有界原始信封，再按 `(contract_version, profile, method)` 注册表选择该方法的完整输入Schema。版本未知为 `version_unsupported`；版本已知但profile/method未知为 `unsupported`；已知方法负载不符为 `schema_invalid`。不能用一张仅允许 command.get 的全局闭合方法枚举，直接把未来方法统一报schema_invalid。
- `MethodName` 公共值仅约束合法语法；每个当前可调用方法的请求Schema以const冻结method/profile/version。历史回执的method或原命令引用不能使用“本宿主当前支持的方法”枚举，否则后续下线方法会导致旧回执不可读取。
- 后续切片新增方法时发布新准确版本（例如1.1.0），添加对应闭合profile和方法Schema，保留1.0.0验证与协商路径。跨版本不会自动兼容；是否并存由注册清单显式声明。旧Schema和夹具不可原地扩宽。方法Schema摘要必须覆盖其可达公共定义；共享定义改变也要改变摘要。
- 不为22切片预建空方法、空包或未来字段。需要时添加新方法类型，原版不可变。

## 2. 准确值与有限边界

| 类型/限制 | 决定 |
|---|---|
| ID | ASCII `[A-Za-z0-9][A-Za-z0-9._:-]{0,127}`，1..128字节；大小写敏感，不trim、不Unicode规范化；不得放PII |
| Revision、ContentRef.version、byte_length | 规范非负十进制字符串，`0`或非零首位，范围0..9223372036854775807 |
| Amount.integer_value | 同上非负int64范围；退款/负调整未来单独定义，不偷偷扩大现有Amount |
| Amount.unit | 大小写敏感ASCII机器名，1..64字符；明确币种及最小计费单位如 `USD:micro`，不换算或大小写转换 |
| Time | UTC `YYYY-MM-DDTHH:mm:ss.ffffffZ`，固定6位微秒，年0001..9999，真实Gregorian日期；秒00..59；拒绝闰秒、时区偏移、缺失/过多小数；保持字符串，不经JS Date |
| 摘要 | `sha256:`加64位小写十六进制 |
| JSON原始正文 | 最多1,048,576 UTF-8字节，包含空白；请求与输出都受上限约束；大内容走ContentRef |
| 嵌套 | 最多64层容器，根容器深度1 |
| CollectionView.items | 最多1000个；cursor最多4096字符；gaps最多100条；读取范围和水位明确owner；多owner不存在一个全局revision |
| 委托链 | 最多16个结构化主体绑定，顺序有意义 |

int64上限贴合PG BIGINT与SQLite INTEGER；仍覆盖JS安全整数以外值，减少后续存储隐式溢出。若将来某金额确需更大范围，在新版本引入明确decimal类型，不把本版bigint无界化。微秒精度可直接落PG timestamp，不需要后续截断或更改命令摘要。

每个最大值必须有合法边界和越界反例。数字范围不要只靠正则长度：做十进制长度+字典序比较或BigInt/ParseInt，绝不先转float。Time需日历检查，正则或JS Date自动修正不足以验证2月30日。

## 3. 严格JSON和规范化摘要

### 严格入口

- 所有外部JSON必须先经过原始字节入口。拒绝非法UTF-8、BOM、尾随第二个JSON值、非法空白、嵌套重复键、无效转义、孤立UTF-16 surrogate、非有限数及超限正文/嵌套。
- 重复键按解码后的名称比较，`a`与`\u0061`重复；不能先JSON.parse/Unmarshal成map再检查。JS对象应使用无原型字典或Map，不能让 `__proto__` 改写原型。
- Go `encoding/json.Decoder.UseNumber`及Token可保留原始数值并检查重复键，但默认对无效UTF-8/surrogate有替换行为，须另做严格词法检查。TS须在JSON.parse信息丢失前完成重复键与数字检查。
- 首版所有线上数值都用字符串，公共Schema不含JSON number/integer字段。因此原始数字token可在公共解析入口直接拒绝，不需要把数字转Number。Schema文件自身的maxLength等是生成时机器数据，不受这个线上负载规则限制。
- 语法/Schema验证不检查当前时钟；否则已过accept_before的原命令不能再算出原摘要。接纳截止是处理入口规则，后续持久去重必须先查原键再决定能否首次接纳。

### 规范化 `lerna-command-digest-1`

SHA-256输入为 UTF-8域分离前缀 `lerna-command-digest-1\n` 加规范JSON。规范对象字段：

```
{
  contract_version, profile, method, target, payload,
  accept_before,
  expected_revision: 原字段存在时才保留,
  subject_binding: { tenant_id, subject_id, delegation_chain }
}
```

主体绑定由受信上下文提供且先验证格式；委托链保存准确顺序及结构化身份。`command_id`属于幂等键而不属于业务内容摘要，明确排除；trace_context、连接、发送次数排除。业务字段必须全部纳入，不允许只哈希payload。

规范编码采用RFC8785的无JSON数字子集：对象键按UTF-16代码单元排序（TS默认排序；Go需要utf16比较，不能假定Go字节序等价）；数组保持顺序；null/bool按JSON字面量；字符串保留Unicode标量，不做NFC/NFD/trim。仅转义引号、反斜杠和控制字符；控制字符使用JSON标准短转义，其余U+0000..001F使用小写hex；不转义斜杠、HTML字符、U+2028/U+2029。没有缺省补值；缺省字段不同于显式null。

首版闭合对象键均ASCII，但仍明确排序算法，避免后续新增受控map时改变既有实现。不能直接依赖Go json.Marshal默认HTML与U+2028转义。

Schema摘要另用域前缀 `lerna-schema-digest-1\n`；对根Schema及所有可达本地$ref组成的确定性bundle做规范编码，Schema数字只允许精确安全整数。或者直接哈希明确规定的bundle文件字节并固定LF/排序生成，这同样有效，但必须写清“格式变化会改变Schema摘要”。不得只哈希根文件而漏掉公共定义。

## 4. JSON Schema到类型、验证的最低复杂度路线

推荐JSON Schema 2020-12作为唯一机器合同源；运行时使用成熟实现：Go `github.com/santhosh-tekuri/jsonschema/v6`，TS Ajv 2020模式。依赖实际安装验证后锁定具体版本与校验。Ajv禁用coerceTypes、useDefaults、removeAdditional，不允许远程$ref下载；Go只注册仓库随包嵌入的资源。精确字符串范围/时间语义可使用一致的命名format，并明确开启format断言及共同夹具；能写入标准Schema约束的规则不要只写手工业务代码。

生成策略按当前实际复杂度选一个小范围、同仓锁定的生成器：遍历有名称的$defs，以 `$ref`、string/boolean、enum/const、closed object、bounded array、带判别字段oneOf生成Go类型与TS类型。必须遇到未知影响语义的Schema关键字就失败，不能静默丢弃。生成器版本写入脚本常量和输出头；Schema定义实际边界，生成类型负责结构，完整Schema验证仍是唯一接纳判定。这样不必同时维护两个独立手写结构集，也不需要引入庞大多语言生成平台。

若实现团队采用成熟json-schema-to-typescript/Go生成器，可替代上述小生成器，但先验证判别联合、const、additionalProperties、可选字段和null差异；避免为了生成器方便放松Schema。Go对oneOf需生成分支结构及安全包装，不能把所有条件字段摊成一个随意可空结构后声称类型已表达状态关系。TS必须是真正判别联合。不要使用any作为payload或逃过验证的类型断言。

仅手写严格词法边界、规范化、小型格式断言和必要跨字段语义；不建议再手写一套完整JSON Schema解释器。成熟验证器给后续22切片更低维护负担。全部资源在二进制/SDK中嵌入，工作目录任意时均可验证，不能运行时依赖源码树路径。

## 5. command.get、身份与只读端口

- 请求仍采用共同信封，`method=command.get`、`profile=command`；`payload={command_ref:{owner:{tenant_id,owner_id},command_id}}`。target用ObjectRef `{tenant_id,owner_id,kind:"command",id:原command_id}`，必须与payload引用一致。信封的command_id是本次查询关联身份，不能与payload原命令身份混淆。
- 禁止expected_revision：读取不需要并发写前态。查询信封accept_before只约束这次读请求开始，不约束原命令记录的可查询寿命。原CommandRef不必再重复携带原accept_before；原截止继续保留在原记录及摘要。
- 读请求不保存CommandReceipt、不创建Job、不占持久接纳表。新查询可用新读关联ID；原写重传必须保留原ID/owner/截止。文档应明确两者差异。
- 输出以 `status` 判别：`found {command_ref, receipt, progress}`；`not_found {command_ref}`；`gone {command_ref}`；`unavailable {command_ref,reason:dependency_unavailable}`；`rejected {reason}`。forbidden拒绝不返回原决定、对象内容或进展；也不返回任意后端错误字符串。
- CommandReceipt只允许accepted/applied/rejected。accepted必有object_ref；applied有object_ref和revision；rejected必有reason且禁止object_ref/revision。缺失字段用省略，不能用空字符串；next_action定义闭合枚举。accepted进展失败不能改写receipt。
- Progress建议本版只定义最小明确的TaskProgress分支（`kind:task,object_ref(kind=task),revision,status=已定义的6种Task状态`），另有 `kind:none` 和 `kind:unavailable`。这仅是读取已有记录的类型，不广告task.get/task.submit。不要为了通用化提前创造混淆Operation和Task的全局succeeded/done语义；后续组件增加具体Progress分支时发布新合同版本。必须检查progress.object_ref与receipt.object_ref一致。
- TransportOutcome独立表达commit_unknown；不放入receipt或read结果固定接纳状态。

消费方最小端口：

```
ReadAuthorizer.AuthorizeCommandRead(ctx, trustedPrincipal, commandRef) -> allow/forbidden
OwnerDirectory.Resolve(ctx, OwnerRef) -> ResolvedOwner{OwnerRef, opaque endpoint}
CommandFactReader.Get(ctx, resolvedOwner, commandRef) -> read observation
```

TS对应接口；ctx/AbortSignal贯穿。Principal由宿主注入，含tenant/subject/结构化委托链；不从payload构造可信主体。调用序：严格验证 -> 要求可信身份 -> target/ref租户owner一致 -> 授权（不能以对象存在性作为公开差异） -> 固定owner的目录解析 -> 只读事实源 -> 验证输出引用与Schema。解析回来的owner必须等于请求owner。授权服务不可用需fail closed，不能默认全允许。

授权接口必须在查事实前对确切commandRef检查读取权限，包含未找到情况。不能只在found后比subject，否则越权者能区分对象存在/不存在。注入的测试authorizer可使用精确scope授权正常主体；未获准主体对存在和不存在一律forbidden。委托链能绑定摘要但本版不声称已验证真实签名/委托服务。

目录更新同一owner的endpoint可成功读取；默认owner更新与此接口无关。故障/not_found不能查询另一个owner或修改原ref/截止。接口只提供read函数，并用公开可观察状态快照前后相等验证只读性；不需要模拟真实数据库事务或数私有调用次数。

## 6. 工具与交付证据

实测PATH工具报告Go1.27.1、Node24.19.0、pnpm12.8.1。环境README记录官方Go归档/SHA校验和已通过的真实编译，用户日期为2026-10-03；不能仅因版本比模型熟悉版本新而断言其不存在或强制降级。可锁定这些已验证准确版本，TypeScript版本再由项目锁定安装复验；bootstrap只校验与锁定安装，不隐式升级系统。README中复制正式下载来源和校验信息到仓库独立说明，项目不依赖 `/workspace/.lerna-env`。

尚需实现时取得的证据：干净pnpm frozen-lockfile安装、Go/TS编译、共同原始JSON反例、双向真实encode/decode、固定预期摘要、schema可达依赖摘要、生成两次无差异、读入口正常/故障/越权行为。没有网络/存储时明确范围，不能把这份决策或本机工具存在当作完成六ticket的证据。

## CollectionView 补充定案（供ticket01直接实现）

具体首版线类型使用 `items:ObjectRef[]`（maxItems 1000）、`cursor:string|null`、`exhausted:boolean`、`partial:boolean`、`gaps:Gap[]`、`read_scope:{owner:OwnerRef,object_type:Kind}`、`watermark:Revision`。语言可提供generic便利，但机器Schema必须验证具体items类型，不能以items:{}当作闭合合同。

- exhausted=true时cursor=null；exhausted=false时cursor为1..4096字符的非空不透明字符串。
- Gap首版仅 `{reason:"retention"|"authorization_changed"|"source_unavailable"}`，最多100条；partial等于gaps是否非空。不预建start/end；它们很容易错误混用对象revision和读取提交水位。
- watermark是read_scope中单owner的读取提交水位，字符串的编码复用Revision，但不是集合内对象revision，不跨owner排序。以后实现真实增量时，水位必须与提交同步，不能用PG sequence分配序冒充提交顺序。
- read_scope一致并不自动构成全局快照；权限变化或gaps出现时调用方按相应合同重建，不以partial=false断言全球数据完整。
- 窄生成器遇到未知影响语义的Schema关键字硬拒绝可行。严格原始codec在01实现、02增加command方法Schema与处理入口是合理分工。
