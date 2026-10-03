# 模型供应商出口

`NewOpenAI(OpenAIConfig)` 装配 OpenAI-compatible Chat Completions 的单次 HTTP
出口，实现当前宿主内部 `brain.Engine` 端口。没有凭据时返回 `unsupported`。
构造、`Encode`、`Lookup` 和反序列化不会发送模型请求。这些 Go 端口未声明为
稳定的第三方扩展 API，也没有增加 SDK 业务方法。

宿主显式提供 Runtime Store、固定 tenant/owner/database Scope、HTTPS endpoint、
准确 model/receiver/location、受控凭据装配的 APIKey 与 CredentialID、Profile、
计数器、每百万 token 的输入/缓存输入/输出 USD 费率、有界响应与并发。
Profile.Ref 的 component_id/version 必须是准确身份；空 digest 由冻结配置计算，
已有 digest 不符则拒绝启动。它包含出口、计数器、费率、计费终结合同、完整
输出 Schema、提示协议与全部限额。重启恢复必须保留这些准确配置。

`Profile()`、`TokenizerRef()` 给 Context 与 Brain 同时装配。Snapshot 中两者
必须准确匹配；实际 goal bytes 必须匹配 GoalRef 的摘要和长度。整个 HTTP body
（含系统协议、JSON Schema、完整 Snapshot 与 goal）作为不透明 `[]byte` 冻结、
计数、计算摘要，并在发送前重新核对。非零 ReservedOutputTokens 直接成为
HTTP max_completion_tokens；零只供编译阶段使用配置最大值。

可选 `MaterialResolver.ReadMaterial(ctx, ref)` 由宿主绑定原 scope、主体与
`brain.input` 用途。Encode 先核全部 MaterialRefs 为 ProcessedSources 的准确
子集、无重复且总声明字节有界，再在 Tx 外读取，逐项核原 hash/length/UTF8，
将 ref 与原 body_utf8 放入同一冻结请求。引用不会自动扩展；未配置 reader
时不处理含材料的输入，二进制材料当前不开放。Request/Lookup 只核原冻结
材料，不再次访问 reader。InputTokens 与 EncodedDigest 是派生元数据，编码
前清空，避免 Context 保存这些值后 Brain 再编码改变原请求字节。

`UTF8UpperBound` 是显式可选的保守计数合同：模型每个 token 必须消耗至少一个
UTF-8 字节，两条消息的额外 framing 必须不超过 64 token。它按完整编码字节
加 64 计数，声明 `upper_bound`，不宣称精确 tokenizer 或适用任意供应商。
其他算法可通过本机 TokenCounter 端口接入并提供准确版本与摘要。

`CostBound(inputTokens, reservedOutputTokens)` 只返回 USD。它采用非缓存与缓存
输入费率中较高者，不假定缓存优惠，与全部预留输出求和后向上取整到 6 位
小数。`Generated.Usage` 根据真实回复的 prompt/cache/completion token 和冻结
费率求和，再向上取整到 9 位小数。真实 token、供应商 response ID 和原回复
摘要由 `Call(ctx, callID)` 查询，不额外创造预算单位。BillingFinal 默认 false；
仅在宿主确认供应商 usage 与冻结费率构成最终费用合同后显式启用 true。
本适配器尚未实现供应商后续账单查询，未结费用持续保留。

原调用账属于 `providers.model_calls`，使用显式参与者 `providers` 的短事务。
先提交原 CallID、编码摘要和 send_started，再在事务外 POST。原回复与实际
token 事实共同持久保存，原字节按最多 4 个 64KiB base64 片段与主记录共同
提交；片段与整体摘要均复核，数据库 JSON 规范化不会改变它。恢复时从原
片段按相同冻结合同解析，避免 raw reply 与完整 Generated 双份膨胀超过单行上限。
提交未知即停止推进。重复 Request 或 Lookup 只读取同一 CallID：已保存的原
回复可以恢复；无原回复返回 effect_unknown。供应商没有原 CallID 查询合同，
故不发补偿 POST、身份变更查询或另一次模型请求。

HTTP/1 transport 禁止连接复用、可重放 Body、HTTP/2 自动重试与重定向跟随。
请求头传递原 X-Harness-Call-ID；凭据仅在原请求 Authorization 中。并发、等待、
请求超时、编码和回复字节都有固定上限。生产只接收 HTTPS；HTTP 仅可明确
允许数值 loopback 地址用于合同测试。平台 HTTPS 配置不由此适配器隐式创建。

`ModelOutputSchema` 与 `ParseGenerated` 闭合五种草稿：refine_requirements、act、
complete、request_input、fail。只包含本地内容与准确候选引用，禁止 Grant、
成功、效果、预算和自报费用。重复键、未知字段、错误媒体类型、未发布本地
引用或非实际来源披露均拒绝。Action 的 capability/binding 必须在准确 Snapshot
中；完整权限、规则接纳、真实效果和完成条件仍由各自领域负责方检查。
模型输出无效但费用已知时保留实际费用，不把错误输出当未知费用或零费用。

验证入口：`go test ./conformance/providers`、`go test -race ./conformance/providers`、
`go vet ./adapters/providers ./conformance/providers`、`go build ./adapters/providers`。
测试在真实 SQLite 与 httptest HTTP 上核对准确字节、原回复重启恢复、连接中断、
提交回复丢失、重定向、配置漂移、严格解析和费用上界。它们是协议和恢复证据，
没有真实供应商账户、自然语言质量、生产额度或地域保证的验证结论。
Search/Body 供应商、原调用远程查询、账单结清和跨位置能力当前均未开放。

设置 `HARNESS_TEST_POSTGRES_DSN` 后可运行 `go test ./conformance/providers
-run TestPostgres`：单连接真实 PG 下，HTTP 期间通过公开 Call 端口读取已提交
的发送标记，核验连接在出站前释放与原回复恢复。未提供 DSN 时明确 skip。
