# 04-05：扫描不得把不可解码 Record 当过期或其他主体排除

## 1. 范围与必要决定

STATIC，只读当前 WT `/tmp/lerna-worktrees/content-snapshots-05`。HEAD `85a10674f0073875e12701332b2edf94312f09e3`；本轮 `adapters/postgres/content/work.go` 为未资格的新SQL，所读 SHA256 `f4e5ddeff887bb29b5f277accfadf575544c17ca9ce1e06a81a72b962e7091ca`。本文不把WIP归为固定HEAD已实现或已通过。未运行Go/native/PG、未修改任何源码。

**必要修正成立。** 当前SQL所证明的Ref/Subject/Seal部分形状不足以让整个Record取得“可安全从候选排除”的资格。`purpose:123`在Record与Seal都为数字时可相等，但原`LockObject`先`json.Unmarshal(body,&d.Record)`，会拒绝number→Go string；旧路径还会转入LockVersion重读/核对完整事实。不能让expired或different-subject筛选吞掉这样的原错误。

采用最小补足：仅在 `work.go` 的固定 Content Record 候选查询中，增加一个**保守的完整类型可解码证明**，并与现有scope/identity/window证明相与，作为排除资格。只对证明成功的行执行“非本scope/已过期而不返回”；其它行保留到有界错误候选，沿当前`readContentJobs`原Go解码提供原因，解码本身成功但shape不受支持则显式ErrScope。不是重写Go decoder，不新DDL/Core/port/通用JSON schema执行器，也不改其他Content读写路径。

原真实64backlog red18.131只资格原调度缺陷；本类型问题是root和本报告的静态反例。候选green仍pending。原已固定新测试15/45/90/4/120边界全部保持。

## 2. 固定结构必须覆盖的字段

实现可用几组**常量字段名列表 + 同一SQL类型判断**，避免每个字段复制长CASE；列表只对应当前d.Record/BodySeal，不反射生成任意schema、不把schema名称变配置。

### Record

- string / string alias：`legacy_primary_qualification_id`、`legacy_primary_evidence_digest`、`primary_holder_binding`、`purpose`、`tuple_digest`、`object_id`、`object_key`、`requested_retain_until`、`effective_retain_until`、`current_retain_until`、`admitted_at`、`publish_deadline`、`io_deadline`、`publication`、`failure`、`attempt_key`。至少所有**出现且非null**的值必须为JSON string。`purpose`尤其不能只用`seal.purpose=record.purpose`或`->>`文本比较替代类型证明。
- boolean：`staging_holder`、`object_holder`、`cleanup_pending`、`body_gone`，出现且非null时必须为JSON boolean。不能把`"false"`、0/1作为false；body_gone若纳入现列一致性判断须与实际列值一致，不能忽略原LockVersion的该检查。
- Go整数：`revision`为int64；`attempts`与`max_publication_attempts`为实际Go int。出现且非null时必须是真正Go整数可接受的**原始JSON整数token**且不溢出，详见§3。不通过`->>`等于列文本的偶然结果证明它原来就是整数。
- `content_ref`、`subject`：保留现有准确Ref/完整Subject形状与identity检查；不能把字段选择器相等看作其父值一定为object。subject的delegation_chain保持全部元素类型验证。
- `sources`：缺失/null或数组是Go切片可解码的形状；若数组，每个元素都须能按当前ContentRef结构解码，包含owner对象和全部已出现的string alias字段。当前writer产生的每项为完整object；最小保守实现可仅把这种完整object列为可排除形状，null元素/缺损元素留作错误候选，不默当空来源。数组包含数字、字符串、数组、嵌套owner不对、ref字段数字等不得取得排除资格。不要仅检查`jsonb_typeof(sources)='array'`。
- `body_seal`：cleanup可排除资格仍要求非nullobject及现有Seal/column一致性。`Bytes`标记`json:"-"`，不是应从正文读取或引入SQL保存的字段。

### BodySeal

`policy_change_key`（可省略）、`primary_holder_binding`、`primary_holder_id`、`id`、`purpose`均按string类型检查。`content_ref`、`subject`是完整相同结构；现有与Record的相等关系保留，但必须在各自类型证明之外，不能用“两个同样错误的值相等”代替类型。

`started_at`、`deadline`是**time.Time**，不是Record中的v.Time字符串别名。当前 `^[0-9]{4}-...T`前缀+PG宽松timestamptz解析不等同Go的JSON RFC3339解码。排除资格须限定Go明确可解码的完整RFC3339形状（含合法日期/时间/时区、有限小数秒）；可以保守只支持当前writer的规范形式，其它不确定形式保留为错误候选，而不能因为PG接受就排除。保持真实时间窗口与现有微秒向后容差，最后领域精确clock仍决定能否执行。

对于原Go能接受的缺失/null默认值，不要谎称它们本身一定导致Unmarshal错误；后续fullidentity/scope/window可拒绝。可排除证明允许保守缩小到现writer正常形状，但不能把“证明不了”翻译成“该责任不存在”。

## 3. 原JSON信息不能由jsonb归一化抹掉

仅补jsonb_typeof仍不充分：

- `attempts:1.0`或`1e0`可被jsonb归一化为整数表示，但Go int解码原token会失败；revision同理。
- 重复成员可能先出现错误类型、后出现看似合法值。jsonb只保留后值；原Go对整段解码可能已经返回错误。不同大小写成员还可能被Go匹配到同一字段，SQL仅看lowercase可能遗漏它。

因此保留原body的JSON文本/`json`表示，jsonb只做结构选择：

1. 在排除证明中要求原body是OBJECT且**递归唯一键**。目标PG能力允许使用原文本 `IS JSON OBJECT WITH UNIQUE KEYS`；不要对已经变成jsonb的值再检查唯一键。重复/语法不受支持行须可见，非法UTF8或PG解析失败保留实际错误。不能清洗重写原body。
2. 对三个整数从原`json`字段取token文本，而非jsonb归一化值；同时确认原JSON类型number。token匹配`^-?(0|[1-9][0-9]*)$`并在实际类型界内；`-0`是Go可接受整数，不能需要拒绝才“凑严格”。revision界int64，attempts两个界按实际`strconv.IntSize`确定（当前64位时同int64），可作为固定参数传入，不新增运行时策略。null/缺失按§2处理；fraction/exponent/overflow不能消失。
3. 对Record以及嵌套已知struct采用当前精确tag名称的有限白名单作为排除证明；未知/大小写变体不授排除资格，避免Go case-insensitive字段覆盖被SQL忽略。这是对**新快速扫描排除路径**的保守资格，不宣称原json.Unmarshal就是closed schema，也不更改公共合同。
4. 用CASE控制array展开/数值或时间转换的前提，不依赖SQL AND左到右短路。实际PG错误必须返回，不捕获后转成false/零/空页。SQL NULL逻辑最后统一`IS TRUE`用于“确有排除资格”，不能`COALESCE(...,true)`。

这些检查只定义固定Record正常编码的保守子集，不应扩展为SQL逐字重建Go MarshalJSON或一般JSON canonicalizer。现有shadow列/Seal字节一致性仍须诚实保留；JSONB语义相等**不代表**已证明旧LockVersion的全部原字节一致性。若实现发现新的实际“Go原读取失败而SQL可排除”的shadow/canonical编码反例，应留为错误候选并具体补该边界，不以本类型决定宣称所有可能损坏编码已覆盖。

## 4. 有界错误出口与主体范围

`WHERE`的结构应是：**无法证明可排除的错误/未知形状候选** OR **确证形状且属于本保存Subject/primary-holder且原窗口仍可能有效**，然后原order/LIMIT。不能先过期/other-subject排掉，再做类型检查；这样错误会永久消失。

对选中的invalid候选，保留原body交给现有Go `json.Unmarshal`，错误仍带原cause；若Go成功但保守资格/identity/window不成立，可返回原scope错误，不称物理清理或错误行修复成功。不要把unsupported字段的原Go成功说成原Go失败。有限错误页可能因损坏记录阻止继续工作，这是显式完整性故障，不能通过“跳损坏行以让green”吞掉；正常64合法expired记录应全部有资格排出，让第65个真工作可达。

合法different-subject仍可在准确类型/完整subject证明后排除；没有授权读取其它主体的metadata到公开返回。所有原Records/Job/期限/历史不写不修复，所有实际删除与ACK仍由原Lifecycle当前门承担。

## 5. 最小修复资格（仅准备，未执行）

由原solefixer/root安排，不改变原已固定60f业务测试输入和15/45/90/4/120期限：

- 保留原真实小backlog正常对照与64expired真实red；新候选修正后同原finite正常/race验证后续publication/livecleanup可达，旧expired责任/字节/deadline/history不被ACK或续期。
- 为这次SQL排除证明补**受控机械损坏**测试，明确它不是合法业务writer事实：相同expired/other-subject候选，body+seal `purpose:123`；sources数组内错误类型；bool用string；attempts非整数token/overflow；重复字段先错误后合法；大小写字段覆盖；PG能解析而Go不能解码的seal时间。用已存在窄SQL fixture fault入口/适配器测试范围，不把私人表伪造当64业务backlog或真实数据损坏发生证据。
- 至少正常canonical原Record、optional缺省、sources空/合法、多主体匹配与不匹配各有正向对照。结果必须显示不能静默变成空候选/expired排除，Go可解码但不受快速路径支持的case应准确标scope资格，不断言假的Unmarshal cause。
- 数据库查询/Scan/Rows.Err/Close原因仍保留。无新wire/DDD/通用validator生成器、无全owner读取到Go内存、无无限页扫描、无改Core/旧SQL migration。

本次尚无新增native证据；独立sourcecause/Standards修复及whole04/票05最终验收保持各自范围。
