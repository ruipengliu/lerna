# 04-05 扫描排除资格：BodySeal 精确字节补充决定

## 范围与结论

STATIC，只读 `/tmp/lerna-worktrees/content-snapshots-05`。承接已采用报告 SHA256 `58f54d9258ea1878931994eb4b1bea5ec7b71b87746be90d9710f32d9208ec4d`，不修改该报告。本轮实际读到正在补类型守卫的 WIP `work.go` SHA256 `e2a245a10bff3e51098411a53c6b6fd6b1d8c6c95649b4028d81cf693aa9272c`，不是已测试固定产品。原查询精确 SHA256 为 `f4e5ddeff887bb29b5f277accfadf575544c17ca9ce1e06a81a72b962e7091ca`。`facts.go` 仍为 `e56785902e481eafdaa7904f44fabc44770d77f3d605da93906df827fe8a42d5`。未执行 Go/native/PG，未修改源码。

**必要补足成立；JSONB equality 不足。采用仅针对当前 BodySeal 固定格式的规范字节识别资格，不实现通用 canonicalizer。** 识别的是实际 `v.body_seal` bytea 的完整文本；再与已通过完整类型/唯一键/准确绑定检查的 Record seal 做语义值一致核验。只识别、不重写或修复旧值、不生成任意 JSON。未证明规范编码的行仍进入有界错误候选，不能因 expired 或 different-subject 消失。

这是现“数据库先排除、再 LIMIT 64”方案必要的一小块编码格式责任，不能称 SQL 已天然继承 Go decoder/marshaler。若不实现该固定识别器，当前方案没有精确字节排除证明；不能以 JSONB 或 raw==raw 降格替代。此处不转入全 owner Go 扫描、无界翻页、DDL、PG 函数或修改旧 LockVersion。

## 1. 实际反例与原判定

`adapters/postgres/content/facts.go:84–137` 的 `LockVersion` 先完整 `json.Unmarshal(body,&Record)`，比较影子列，随后要求：

- `record.BodyGone == body_gone`；PrimaryHolderBinding 也须准确相同。
- 非 nil BodySeal 经 `json.Marshal(record.BodySeal)` 后与 `body_seal` **逐字节相等**；seal.Ref、seal.PrimaryHolderBinding 还须等于 Record 对应值。

因此同样字段和值但 stored seal 有空白、重排字段，原路径会 ErrScope；现 JSONB 相等却可能将其排为 expired。把 Record 原 seal 子串与 stored seal bytea 比较仍不足：两边同样带空白时依然相等，Go Marshal 却不同。仅“无空白”也不足：重排键、`\u0061` 代替 `a`、时间 `.1200Z` 等仍不是原 Go 写出形式。

当前 WIP 已追加 body_gone 列比较；须继续保留它。缺失/null 对 Go bool 的零值行为可准确映射 false，不能把字符串/数字也映射 false。

## 2. 最小固定识别器的边界

放在现 Content PG work 查询的私有固定资格中；不新增业务 port，也不改 Record/BodySeal 类型、写入格式、0001/0002/0003、期限或 Job。资格必须识别**整个 stored seal 文本**，不做子串匹配；UTF-8 原 bytea 转换失败仍是错误。不得 trim 后比较、jsonb::text 后比较或仅比较哈希。

识别器可以由几段私有常量拼成有限正则/等价固定词法检查，段只用于此 BodySeal。它不是可配置 schema 引擎。当前准确顺序来自 `domain/content/lifecycle.go:34` 及 `contract/gen/go/v1_2/values.go:13,24,419,423`：

1. BodySeal：可选且仅非空时出现 `policy_change_key`，随后 `primary_holder_binding`、`primary_holder_id`、`id`、`content_ref`、`subject`、`purpose`、`started_at`、`deadline`；无其它键，无额外结构空白。
2. ContentRef：`owner`（tenant_id、owner_id），content_id、version、hash、media_type、byte_length，全部固定顺序、固定 string 类型。
3. SubjectBinding：tenant_id、subject_id、delegation_chain；链每项 tenant_id、subject_id。现资格要求数组且最多16，保留原数组次序；不能排序或把 nil/null 当成空数组。当前合法 writer 数组形状有明确正向对照。
4. 字符串 token 必须是 Go encoding/json 默认 Marshal 的规范形式，**不能只接受 ASCII holder/id**。普通合法 UTF-8 字符直接出现，但双引号、反斜杠、U+0000–001F、`<`、`>`、`&`、U+2028/U+2029 不可原样出现。允许的反斜杠形式仅为 Go 实际输出：`\"`、`\\`、`\b`、`\f`、`\n`、`\r`、`\t`；其余控制字符使用相应小写 `\u00xx`（排除已有短转义的五个）；以及 `\u0026`、`\u003c`、`\u003e`、`\u2028`、`\u2029`。普通 `/` 不转义，不能接受 `\/`；普通字母不接受 `\u0061`；不接受 surrogate escape 代替实际 UTF-8 字符。正确转义可以含空格，不能用“全串无空白字符”误拒字符串内部空格。若实现采用正则，需核实际 PG 转义层与完整起止匹配，不在本文把未经执行的表达式声称为已验证实现。
5. 两个 time.Time token 必须是 RFC3339Nano **重新 Marshal 后保持不变**的形式：四位年、真实有效日历/时分秒，秒小数缺省或1–9位且最后一位非0；零偏移为 `Z`，不接受 `+00:00`/`-00:00`；非零偏移为有效 `±HH:MM`。保留现 started/deadline/window 校验，不能只靠正则承认2月31日；PG日期解析失败继续返回实际错误。不能以 PG 微秒 round-trip 检查纳秒文本，否则会拒绝正常 Go 纳秒值。识别精度与后续+1µs保守选择是两件事。

Ref、Subject 的既有合同值限制/完整类型守卫仍先决；上述规范 string token 识别不是新的公开值限制。PrimaryHolderID、SealID 当前允许128字节内非空字符串，必须保留它们实际合法 Unicode/转义形式。Purpose 正常合同为 ASCII标识符，但不借此假设其余 string 都是 ASCII。

## 3. 为什么无需重建整个 Go JSON

在原 body 通过完整类型、递归唯一键、准确tag资格后，且 stored seal 已被证明是上述固定 Go 类型的规范字节时，保留 `record_seal_jsonb = stored_seal_jsonb` 可以把两者的**完整值**相绑定。相同值重新 Marshal 必然得到已识别的 stored 字节；字段顺序、转义和 time 规范性已由独立字节识别负责，不是由 JSONB 负责。

这比要求 `Record.rawSealBytes == storedSealBytes` 更准确：Record 内 seal 可以有不影响原解码的结构空白或键顺序，stored seal 仍是规范值，原 LockVersion 可成功；不必因为前者空白误造必要限制。相反，两边都非规范的例子会在 stored 识别失败，不能取得排除资格。缺失/显式null/空optional等原 Go 可接受但本固定证明不支持的形状，仍按已采用报告保守返回资格错误，不声称原 Unmarshal 必定失败。

选中 invalid 行时，现 Go 完整 body 解码继续保留真实解码 cause；若解码成功但规范 seal 不成立，返回 ErrScope。可读取选中行的 stored seal 并调用原 Go Marshal 作准确原因核对，但这仅服务**最多64个已选候选**，不能用它证明此前被 SQL 排除的行。所有实际工作仍走原 LockObject/LockVersion 锁、fresh clock、原 Claim、holder ACK；识别器不成为删除授权。

## 4. 必须验证的窄范围（未执行）

由 solefixer 在 root 采用后执行，不改原真实64backlog业务输入/15、45、90、4、120期限：

- 正常原 Go writer canonical seal：原小 backlog 与完整64 expired backlog仍能排除旧义务并到达新工作；旧字节、历史、期限、ACK不变。
- 受控机械 bytea 损坏：仅 stored seal 加结构空白；仅重排键；Record与stored都同样非规范。三者均不能被expired/other-subject静默排除。此为机械损坏注入，不称真实业务生产者写出损坏数据。
- 正向：Record内非规范结构空白/顺序而 stored保持Go规范，类型/值完全相同；Go原路径成功时不能把它说成原错误。
- 正常 holder/id 含 Unicode、引号、反斜杠、空格、HTML字符；正常有/无 policy_change_key、有/无纳秒、非零时区。对照其真正 `json.Marshal(BodySeal)` 输出，不手抄“golden canonical”替代原消费者。
- 负向：相同语义但非规范转义、时间尾零/零偏移写法；body_gone 与列相反。保留原错误/资格错误类别，不把某个 scope 错误包装成 physical erased。

这是静态具体反例的修正准备；尚无该识别器 native green，不扩成一般 JSON fuzz 平台，不把原64backlog red或此前 lifecycle greens当本补足已通过。若实际固定实现不能覆盖上列正常 Go 编码，先报告具体不支持编码，不通过限制公开 holder/id 或忽略原 byte check 获得 green。
