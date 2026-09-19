# DuckDuckGo 搜索适配器检查点

用户指定使用免费的 DuckDuckGo。适配器使用其公开 HTML 非 JavaScript 搜索入口 `https://html.duckduckgo.com/html/`，不需要搜索 API 密钥。此入口是网页接口，不是具有稳定 JSON 契约的搜索 API；结构变更或访问限制需要作为获取失败处理。官方入口说明见 [DuckDuckGo 非 JavaScript 搜索](https://duckduckgo.com/duckduckgo-help-pages/features/non-javascript)。

## 实现边界

`adapters/research/duckduckgo` 实现 `websearch.Searcher`，通过注入的 `fetch.Fetcher` 执行有界请求，沿用传输层的授权与网络计量。只解析首个响应中的自然结果，输出标题、摘要、目标 URL；摘要不能替代后续页面获取的正文证据。支持 DuckDuckGo 跳转链接的目标解码，页面目标仍须单独获得获取授权。

原始 HTML 通过已有 acquisition / Content 链路保存，恢复读取重新执行相同解析，不重新联网。EvidenceReader 的结果数量必须由装配方使用原已接受请求的 MaxResults 传入。未知 HTML、验证码与明确空结果分开处理；没有验证码求解、隐藏重试或切换入口的行为。

## 验证结果

- 真实本地 HTTP、授权、SQLite 和 Content 组件验证：首屏结果解析、原始 HTML 保留、恢复不重复搜索、撤权后禁止读取。
- 合成 HTML 验证：明确空结果、200 验证码页面、202 验证码响应、未知页面、不安全链接。不是公网成功样本。
- `go test -race ./profiles/fetchcheck -run '^Test(DuckDuckGo|JSONSearch|SearchRecoversAcrossActualProcessExit)' -count=1` 通过，耗时 6.376 秒。
- `go vet ./adapters/research/duckduckgo ./profiles/fetchcheck` 通过。
- 相对 `3215e50` 的 Standards / Spec 双轴增量审查均无可操作发现；这不是整票最终审查。

本环境曾对上述公网入口发出一次公开查询诊断，返回 HTTP 202、14,260 字节 HTML，包含验证页面且没有结果链接。该诊断使用独立 HTTP 客户端，并非受控运行时验收；未保存挑战令牌到仓库。公网正向验收尚未通过。

## 依赖与未完成范围

HTML 解析使用 `golang.org/x/net/html` v0.59.0。该版本要求 Go 1.26.0，因此 go.mod 的最低 Go 版本从 1.25.0 调整为 1.26.0，原 toolchain go1.26.1 保持。关联模块版本随 go mod tidy 更新。

这是提供方适配器检查点。现有 research / searchcheck 完整任务装配仍使用 JSON 搜索适配器；DuckDuckGo 尚未接入其发现上下文、回答上下文和 CLI 配置。全任务公网搜索、实际模型质量、全仓验证及 22 票最终审查仍未完成，不能据此关闭票据。

## SDK 接入进展

后续将受控搜索装配提取为消费 `websearch.Searcher` 的 `bindSearchProvider`，原 JSON 装配继续委托此入口。DuckDuckGo 现已通过真实 SDK 的 Invoke、Run、Drain、Reconcile 链路：重复调用保持原操作，只有一次 HTTP 请求和一次网络预算预留；原始 HTML 可通过 Content 读取。使用该证据的 searchcontext 可向 Brain 提供 `search-candidates` 块，未将 HTML 或发现摘要冒充页面正文。

`go test -race ./profiles/fetchcheck -run '^Test(SDKDuckDuckGo|SDKSearch|DuckDuckGo|SearchRecoversAcrossActualProcessExit)' -count=1` 通过（7.379 秒），`go vet ./profiles/fetchcheck` 通过。新增 SDK 用例未启用 Actions 查询端口，不能支持全任务查询计量已验收的结论。完整研究任务仍未选择 DuckDuckGo，以上进展不改变前述未完成范围。

随后补充上下文投影断言，`go test -race ./profiles/fetchcheck -run '^TestSDKDuckDuckGo' -count=1` 通过（1.939 秒）。相对 `7a1e7e0` 的 Standards / Spec 增量审查及新增断言复查均无可操作发现。

## 完整参考链路接入

上述未接入 research / CLI 的描述是早期检查点状态。现已支持 `ResearchConfig.SearchFormat` 选择 `json` 或 `duckduckgo-html`：提供方调用、发现上下文、空结果判断、答案上下文及关闭重开后的读取器一起选择。参考 action planner 的原请求固定 MaxResults=4，HTML reader 仅在该装配内采用相同上限；这不是处理任意请求的通用恢复配置。

CLI 新增 `-search-format duckduckgo-html`，仅对 reference-v2 生效，v1 格式覆盖及未知格式在执行前拒绝。`reference-loopback-v2` 使用实际本地 HTTP，`reference-replay-v2` 回放同一组 HTML；均保留原任务预算。回放适配器新增接受 text/html，仍校验摘要、字节上限和当前授权，没有网络后备请求。

验证记录：

- 首个 answerable 完整任务 race 通过（5.020 秒）。
- 扩为四类结果 × HTTP/回放/重开的 12 例后，HTTP 和重开 8 例通过，4 个回放因原适配器拒绝 HTML 失败。修复媒体类型后，4 回放 race 通过（19.054 秒），没有将首次失败记录改为成功。
- 空搜索正式任务 race 通过（3.499 秒），答案为 insufficient、无页面请求、无伪造页面证据。
- CLI 全包 race 通过（24.712 秒）；相关包 go vet 通过。
- 实际执行 `go run ./cmd/searchcheck -profile reference-loopback-v2 -search-format duckduckgo-html -case all` 退出 0，结果见 [完整参考报告](evidence/22-duckduckgo-reference/loopback.json)。SearchFormat 为 duckduckgo-html，Mode 为 loopback-http，SemanticQuality 为 not_evaluated。
- 相对 `33ce238` 的 Standards / Spec 增量审查及回放修复复查均无可操作发现。
- `go test -race -timeout=15m ./profiles/fetchcheck ./adapters/research/replayfetch -count=1` 最终通过：fetchcheck 全包 230.901 秒，replayfetch 无独立测试文件，其行为通过 fetchcheck 集成用例验证。这不等同全仓 make verify。

剩余范围：真正公网入口和动态候选授权的运行装配、实际模型与独立语义评价、全仓验证及 22 票最终审查。本次是完整参考任务链路通过，仍不是公网问答已验收。
