# 22 票豆包搜索接入记录

用户将公网搜索提供方从 DuckDuckGo 改为豆包搜索，并授权把搜索密钥保存到本地 `.env`。变量名为 `DOUBAO_SEARCH_API_KEY`；文件权限 0600，已被 Git 忽略。密钥正文不进入本文或版本库。用户后续另行授权开发阶段模型累计费用低于500元，输出token额度可配置。

## 当前实现

正式 Core/SDK 路线已接通豆包搜索。公网 CLI 默认 `AnswerFromSearch=true`：请求 `NeedSummary=true`、`Filter.NeedContent=false`，直接使用实际返回的 `Summary`，不请求原网页。每项总结具有独立引用视图，但保留原搜索响应作为授权、留存和恢复依据；缺少 Summary 不降级使用 Snippet 作证据。此模式只需允许搜索端点，不必授予候选网页访问权限。

有限公网总结 v4 已通过独立事实与引用核验，详见 [总结验收记录](22-public-summary-review.md)。以下初始装配与页面路线记录保留为历史，不再作为当前实现待办；整票已完成，28阶段全仓验证通过，见[最终验收](22-final-acceptance.md)；历史细节见[验收审计](22-acceptance-audit.md)。

## 官方接口依据

用户指定的 [API 文档](https://docs.volcengine.com/docs/87772/2272953?lang=zh) 本次浏览读取失败，接口鉴权与请求参数交叉核对火山官方仓库的 [API Key 调用实现](https://github.com/volcengine/mcp-server/blob/main/server/mcp_server_askecho_search_infinity/src/mcp_server_askecho_search_infinity/api/api_key_auth.py) 及 [请求模型](https://github.com/volcengine/mcp-server/blob/main/server/mcp_server_askecho_search_infinity/src/mcp_server_askecho_search_infinity/model.py)。

- 端点：`POST https://open.feedcoopapi.com/search_api/web_search`。
- 鉴权：`Authorization: Bearer <本地搜索密钥>`，JSON 请求。
- 发现参数：`Query`、`SearchType=web`、`Count`，可设置 `Filter.NeedContent` 和 `Filter.NeedUrl`。
- 实际响应包含 `ResponseMetadata` 和 `Result`；网页候选位于 `Result.WebResults`，字段包括 `Title`、`Url`、`Snippet`。

## 已运行的连通性检查

一次公开查询 `Go programming language official documentation`，Count=1、NeedContent=false、NeedUrl=true，20 秒超时，无重试，禁止重定向。返回 HTTP 200、17449 字节、非空 WebResults 数组。未打印或归档请求鉴权、响应正文及其他凭证。

该检查使用独立 Python HTTP 客户端，仅证明本次搜索端点和凭证可用；不代表正式 Core/SDK 搜索任务验收通过。没有调用答案模型。返回体即使 Count=1 也超过既有参考配置的 4096 字节，正式装配必须显式设置适当的有界响应额度，不能截断后当作合法空结果。

## 正式装配待办

需要增加消费 websearch.Searcher 的豆包 Adapter，并将 POST JSON 与 Bearer 凭证放入受控传输边界，接入原任务授权、预算、披露位置及恢复。不得把带凭证的 POST 简化为现有无凭证 GET 获取，也不得把搜索返回的 Content 自动当成独立获取的页面证据。当前正式运行时尚未切换提供方；原 DuckDuckGo 验证失败记录保留，但不再作为选定提供方的最终验收条件。

后续使用豆包完成原 22 票公网搜索与页面获取验收；真实答案模型评价仍按独立预算与执行锁定要求进行。

## 正式任务装配增量

上述“正式装配待办”记录初始检查点。现在 `adapters/research/doubaosearch` 已实现消费方 `websearch.Searcher`，`RuntimeConfig.SearchFormat="doubao"` 选择该提供方；SearchEndpoint 使用上述端点，URLs 必须显式允许该 POST 端点及候选页面，Networks 仍独立约束实际连接地址。SearchMaxBytes 应显式配置，例如 65536；不是默认扩大所有任务预算。SearchRecipient 与 DiscloseTo 必须反映已授权的实际处理位置，不从模型或网页获取授权。

`RunResearch(ctx, root, cfg, model, credential)` 可传入 `SearchCredential` 函数；该函数由可信宿主读取本地搜索密钥，不进入 RuntimeConfig、持久清单或任务参数。`ResumeResearch(ctx, root, model, credential)` 对需要继续行动的任务重新提供凭证；已发布答案查询无需搜索凭证。库不读取 `.env`，调用方负责凭证配置。

POST 复用受控获取的 URL/IP、当前权限、任务 Guard、响应格式和字节限制；不跟随任何重定向，不自动重试请求体。失败与成功都保留真实请求次数，搜索候选不提升为已获取页面正文。保留响应可经独立 EvidenceReader 重建候选，随后仍走单独页面获取与证据引用。

TDD：受控 POST 首先因缺 PostJSON 编译失败；正式运行时首先因缺 SearchCredential/可选参数编译失败。实现后真实 HTTP 授权与豆包错误响应组合 race 通过（4.152 秒）；豆包完整任务、答案前中断恢复及既有 JSON/DuckDuckGo 完整入口组合 race 通过（17.368 秒），vet 与 diff 检查通过。Spec 和 Standards 分别审查相对 b135e51 的增量，均无可操作发现。

本增量尚未取得新版全仓验证终态；旧 0ebbc06 的全仓通过不能代替本次验证。公网端点的独立连通性检查仍不是正式任务公网验收，实际答案模型评价预算及执行锁定缺口仍保留。22 票不据此关闭。
