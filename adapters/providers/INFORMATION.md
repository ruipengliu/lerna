# 授权 HTTP 信息源参考实现

`NewHTTPInformation` 只装配本机端口，不解析 DNS、不迁移数据库、不访问目标。
每个准确 `InformationSourceDescriptor` 提供 `information.search` 与
`information.body` 的可替换 `execution.Driver`；`Capability()` 返回闭合参数和
结果 Schema、准确版本摘要、`read_only` 和 `MaxAttempts=1`。未给 `InformationEgress`
或 `InformationContent` 时构造失败。源码注册不表示当前授权或信息源可达。

参考源必须显式声明 `public_unbilled=true`，原费用为 USD 0 且 final；此合同不适用于
需要按量计费的供应商。凭据经宿主的环境引用提供，body/journal 不保存密钥。声明必须
有凭据却未提供时 `Prepare` 返回 unsupported，不能隐式选另一源。

## 请求与恢复

Search 参数为 `{query_ref, limit, cursor?}`；query 的原字节必须已在 Intent 的
`processed_source_refs` 与 `disclosed_source_refs` 中双声明，不能由宿主补授权。
实际 POST `/search`（路径由 descriptor 冻结）采用以下闭合 wire 合同：

```json
{"protocol":"harness.information/1","request_id":"operation_0123456789abcdef0123456789abcdef","query":"版本信息","limit":2}
```

原 `operation_id` 是服务端请求身份；HTTP 另携带原 `X-Harness-Attempt-ID`。
参考响应为 `{protocol, request_id, items, exhausted, cursor?, coverage}`；每个
item 为 `{url, title, snippet, observed_at?}`。coverage 和 exhausted 只表达该源
声明的索引范围，LIMIT、命中数量和取得成功不能证明全网穷尽或事实正确。

Body 参数为 `{url}`，URL 必须位于 descriptor 的准确 origin 和路径前缀。
因 URL 位于参数中，原 ArgumentsRef 必须被该 Action 明确披露；Brain 可用闭合的
`disclosed_local_ids:["args"]` 映射已出版的原参数引用，不形成 Content 自引用。
Search 只要求参数受信处理及 query_ref 双声明，不要求披露整个参数 JSON。
请求只能 GET，不能携带任意查询参数、用户信息、代理、重定向、请求头或地址覆盖。
实际 DNS 地址必须全部通过冻结 CIDR 和内网/link-local/metadata 地址门禁；仅显式
开发配置允许 literal loopback。出口许可绑定准确地址、请求摘要、receiver/location、
当前原主体与用途；Dial 只用已许可的原 IP，无第二次 DNS。HTTPS 校验原 host 与受信
根证书；HTTP 只对明确开发 loopback 开放。

Start 在原 Attempt/Operation 单次键、当前 EgressPermit、原截止与持久 StartBarrier
全部成立后提交 `send_started`，再执行一次 HTTP/1 请求。禁连接复用、自动重试、
HTTP/2 和可重放 Body。任一提交未知即停止推进。断连不能变成 not_applied；Reconcile
只核原记录和已获 Content，不再次 GET/POST；无法证明远端停止时 Stop 保留 unknown。
重新取得新信息必须是新授权 Operation。

每源最多 32 并发、20 命中、4096 查询字节、1 MiB 响应、16 地址/路径范围、30 秒等待。
实际来源结果 metadata 最多 128 KiB；来源列表编码最多 32 KiB。字节截断、读失败、
HTTP 错误、重定向、媒体类型缺失、coverage/cache 的未知状态均显式表示。

## 字节与引用

原响应字节仅通过 `PublishReceived` 发布到真实 Content 介质，journal 保存原 ref、
摘要、URI、取得时点和阶段。Search 的 title/snippet 只在当前许可下从原 Content
重新解码，不另存不可治理的 SQL 正文副本。`RecoverReceived` 只沿原 publication/
upload/ready/put 身份恢复，不续 TTL、不补抓源。原字节缺失时返回明确 gap。

`ObtainedAt` 是实际收到该响应后的受信数据库时间；Last-Modified 和 JSON 中的时间
是来源声明，不能把本次下载日期冒充事实更新日期。处理、披露的准确输入来源进入
Content 来源 DAG；当前关闭、期限和主体授权仍影响恢复读取。

出口许可另给有限 `RetainUntil`，publication 冻结原 SourceRef/AttemptID/UseRefs。
宿主以它和全部真实输入来源的最紧期限保存原 publicationPlan；`StartBefore` 仅是
开始窗口，不能用作另一次缓存续期或把免费访问推导为无限保存许可。

`NewReferenceEvaluator` 的 profile 为 `reference-json-string-facts/1`。问题声明有限
JSONPointer 与可选期望字符串；答案从实际 leaf 读取，引用带准确 BodyRef、来源、
URL、取得/观察时间与原 quote。它区分 supported、conflict、stale、insufficient、
retrieval_failed，并检查取得及事实观察时间的最大年龄，不能靠重下载刷新旧事实。
Reader 必须从原可信行动账核验身份与内容，不能接收调用者自报 Observation。
评估器只输出证据评估，Task 的条件选择与完成仍由原 Task/Evidence 端口裁决。

`conformance/providers/information_test.go` 使用真实 HTTP、SQLite/PostgreSQL、对象
介质和 Content 上传合同；外部授权故障为明确测试 seam。有限 JSON 事实问答不证明
通用自然语言答案质量，也不表示真实供应商账户或生产网络性能已验收。
