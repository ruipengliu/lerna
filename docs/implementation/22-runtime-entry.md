# 配置服务的 Go 运行入口

`profiles/fetchcheck.RunResearch(ctx, root, config, model)` 在新的本地运行目录中启动一项正式研究任务，复用 Core、能力 SDK、搜索 Adapter、获取证据及受控答案发布。它接受 `brain.Model`；不会自行读取 `.env`、选择模型或加载凭证。

调用方创建私有空目录，并通过 `RuntimeConfig` 提供以下受信配置：

| 字段 | 含义 |
| --- | --- |
| Goal / Query | 本次目标与搜索文本。 |
| SearchFormat / SearchEndpoint | `json` 或 `duckduckgo-html`；DuckDuckGo 公网端点为代码中 `duckduckgo.Endpoint`，数值 loopback 的 `/html/` 供协议验证。 |
| SearchRecipient | 查询实际接收位置，不能将外部服务标为 local。 |
| SearchMaxBytes | 搜索响应上界，1 至 1MiB。 |
| PageMaxBytes | 单页面响应上界，1 至 1MiB；零值保持原 1024 字节。能力 Schema、实际驱动和重开检查使用同一值。 |
| URLs / Networks | 分别许可完整 URL 和 IP 网段；搜索 URL 必须包含实际编码查询，页面及每跳重定向同样受约束。发现的候选不能扩大许可。 |
| AllowLoopbackHTTP | 仅用于明确许可的 loopback HTTP 服务；公网仍需 HTTPS。 |
| DiscloseTo | 允许本次输入及获取来源处理、发现、披露的位置；非 local 的搜索及模型位置必须在其中。此字段由宿主决定，不能来自网页或模型。 |
| MaxQueries / NetworkLimit / MaxSteps | 必要查询、网络操作、行动步数的原任务上界，分别为 1–128、1–128、1–12。 |
| ModelTokens | 显式任务 token 预算，最高 1Mi；包括行动规划和答案生成，不能仅按答案输出设置。 |

调用方式：

```go
record, err := fetchcheck.RunResearch(ctx, privateDirectory, config, answerModel)
```

仅在新宿主初始化时安装上述数据位置许可，不授予相应位置的能力执行权。运行目录保存原传输清单、当前授权、来源策略、SQLite 状态及内容。`research-host.json` 为 0600 文件，包含配置、模型能力及本地身份凭证；整个目录需要私有保管。入口不会清理调用方目录，失败状态和证据可保留检查；重复使用同一目录不会自动重建或重授权限。提交成功后、首次外部行动前，以私有 `research-task.json` 保存原任务身份。`QueryResearch(ctx, root)` 重新打开现有目录，经正式 SDK 和当前 Content/来源权限读取原任务的发布结果；返回 `answers.View`，不调用模型或网络、不重新安装授权、不标记 UI 已送达。配置或任务清单缺失、损坏时拒绝查询，不补交新任务。`ResumeResearch(ctx, root, model)` 可继续行动已落定且答案生成尚未预留的原任务，并核对模型能力与原清单一致；再次恢复已完成任务只查询。它不重新安装授权或扩大原任务预算。已有答案生成预留时，仅续租仍有效的原资格。未开始且未结算的预留可经原 BeginRequest 单次门开始首次生成；已开始或已结算的请求只经原输出操作核对已保存答案并恢复发布，不生成替代答案。原资格失效、输出缺失或来源撤权均拒绝，原用量保留。行动阶段通过同一 Core/SDK 循环继续：保留原目录、输入、操作身份和冻结预算，先核对 DISPATCHED 行动，再执行尚未派发的工作。原任务约束与 ActionLimits 必须匹配；尚未初始化 ActionState 时，可从 QUEUED、已领取未启动、已启动三个窗口继续；已领取/启动的恢复仍要求原租约有效，并由 Core 检查当前控制和权限，不接管过期的初始化租约。

返回记录包含实际模型输入、输出及答案，应按调用方的披露规则处理，不能直接作为公开日志。结构可追溯不等同引用语义支持；质量验证仍需独立评审。

当前入口沿用有界参考运行条件：总调用时限 30 秒，任务期限 1 分钟，单次 HTTP 时限 1 秒，页面默认上界 1024 字节（可显式配置），搜索最多四条候选，答案输出预留 512 token。答案阶段按原 Worker 资格续租，结束时停止；续租不延长任务或调用时限，也不增加模型请求及 token 预算。Content 对象大小及 Brain 的 32768 字节输入上界独立生效，增大页面获取额度不保证所有已获取内容都能进入一次模型输入。较慢服务或较大页面可能失败；入口没有自动放宽预算、绕过验证码或重试其他搜索路线。行动规划仍为本地协议规划器，答案模型由调用方提供。模型的原始位置与输入上界用于装配，不改标签或减少其声明的预留。

外部包 `TestConfiguredResearchRunsThroughPublicEntry` 通过该入口验证 JSON 与 DuckDuckGo HTML 本地协议、真实 HTTP 获取和正式引用，并验证缺少披露许可时零 HTTP、零模型调用。它不构成真实 DuckDuckGo 公网成功或火山模型效果验收。

恢复回归通过真实本地 DuckDuckGo HTML/页面 HTTP 及模型服务契约边界制造中断，核对原任务身份、实际引用、仅增加一次答案模型请求，重复恢复不新增搜索/页面/模型调用。更换模型契约拒绝；恢复前撤销来源规则时，不调用答案模型也不返回正文。这不是进程强杀或全部检查点恢复的验收。

已保存答案恢复另以真实 Content PUT 成功后取消调用验证；模型仅返回响应但未保存，以及保存后撤权，均不重新调用模型，不重置原生成身份或预算。这是服务边界中断测试，不宣称完成进程强杀或过期 Worker 接管验证。

研究宿主统一跟踪初始与恢复执行服务，关闭前等待后台观察和结果保存收尾。前台关闭最多等待 8 秒；超过该时间则由后台继续保留存储，直到已登记回调返回后再释放，目录清理也遵守此顺序。不协作的回调可能长期保留资源，前台关闭返回不等于清理完成。此收尾时间独立于原 30 秒调用时限，不扩大任务期限或预算。

## 公网宿主入口（2026-09-13）

`go run ./cmd/searcheval -execute -compact-evidence -public-config CONFIG.json -out NEW_DIRECTORY` 运行一项真实治理任务。CONFIG.json 为可信宿主 RuntimeConfig，限制65536字节，拒绝未知字段及第二个对象。目录必须新建，命令负责创建0700任务目录；凭证由环境 ARK_API_KEY 与 DOUBAO_SEARCH_API_KEY 提供，不写入配置或证据。库本身不读取.env。

SearchTimeoutMS 为0时保留1000ms，宿主可选1..3000ms；签名描述符、行动参数、驱动及恢复保持同一上界。其他预算仍独立生效。该入口每任务最多一次真实答案模型请求。AnswerOutputTokens 可配置，公网CLI未指定时使用4096；库的零值保留旧512条件以恢复历史任务。生成前预留该额度，恢复不能增加；另有独立8192字节答案上界。公网请求计数应读取实际取得记录的 Mode/Requests，参考服务器计数不适用于公网。

当前实际失败与限制见 [公网验收记录](22-public-acceptance.md)，不可将可重复入口视为公网验收已通过。

用户已授权取消固定512输出token上限。扩大额度不增加调用次数、不重置原任务；改变额度后运行新验收任务，历史截断记录继续保留。测试覆盖4096额度经原任务恢复及600输出token的实际计量结算。

## 豆包总结默认路径

公网CLI默认启用AnswerFromSearch；直接使用SDK时显式设置true。豆包请求NeedSummary=true，消费实际WebResults.Summary，完成搜索后直接生成答案，不执行候选URL抓取。SearchMaxResults可设1..4，本次公网验收采用2。缺少Summary时返回信息不足，不把Snippet提升为总结。来源权限、原任务计量及恢复保持约束。

总结引用为原响应的稳定视图`ContentRef#summary-index`，不是新增Content对象或网页下载凭证；父响应关联与精确Summary正文保存在受控输入，答复须标注总结来源。通过记录见22-search-summary-mode.md。
