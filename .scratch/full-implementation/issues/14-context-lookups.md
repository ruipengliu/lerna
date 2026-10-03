# 14 context-lookups

Status: resolved
Blocked by: none（本地四种 Lookup 与有限 C2 偏好验收；对端配置的验收另见 16–18）
Implementer: task_impl

依据：Brain 首版 Proposal need_context、Orchestrator 既有获准材料读取、C2 跨任务记忆。

补齐四种闭合 Lookup 的编码、原查询责任、有界读取及当前许可；结果持久保存为下一 Snapshot 的材料，来源不能变成本人原文证明。临时依赖等待原对象，禁止用重新请求模型探测；验证保存、纠正和撤回偏好对下一任务材料及结果的实际影响。

## 完成依据

实现、公开行为、真实负责方和必要故障正反例通过后记录准确提交与制品。外部资格单列；尚未完成的本地路径不得归因于缺凭据。未达成的目标保持 partial。

## Comments

2026-10-03：全项目范围复核后补入原实施任务图，未改变用户授权或领域裁决。

2026-10-03，第一垂直片当时为 claimed：Brain/provider 已接闭合四 kind / 1..3 lookup 草稿，本方准确 publication query_local_id 转成原 QueryRef；真实 HTTP 模型一次请求及 SQLite 重开保留原 CallID/查询字节。Task 保存原查询 batch/Job、固定目标/控制、期限与累计 calls/bytes/tokens 保守上界；依赖只重试原查询，等待禁止新 Decision，已读材料保留而本人 SourceEvidence 不变。当时只有这些责任与 typed resolver/ContextCommitter seam 通过，宿主 resolver 与实际 C2 仍未验收。选定 Brain/Task 正反例 race 实际 exit 0：4.323s / 46.465s；`need_context` 丢原材料 / 未接原查询责任 RED 分别 1.236s / 2.095s。

2026-10-03，后续宿主实现：四种 resolver 已经使用准确已声明 Content、Memory 原查询、能力原描述及 Task/Operation/Check 原事实。查找不进行搜索、URL 获取或隐藏行动，累计 calls/bytes/tokens 与原 Snapshot 查询期限固定；普通 Memory 版本和完整来源在 batch 最后提交及下一次正准入重核。能力描述重核完整父 Grant 链，不消费 once 或新增预留。有限 `report.preference` 模板只在原目标允许的 plain/bullet 格式内选择，保留原标题、正文与路径，条件负责方独立核准确文件读回。

公开 resolver / 当前材料反例的 SQLite、PostgreSQL race 实际 exit 0：107.919s / 108.683s；Core batch 前项撤回反例 RED 1.523s 后 Task / Brain 选定 race 为 37.337s / 10.349s。原父链撤权 / Memory 撤回均有真实拒绝反例。能力原父链纯 Tx 小口单独提交 `cf9aed5`，供工单 17 复用，不能把元数据描述当作工具执行许可。

普通偏好、更正和撤回的三份实际报告在 SQLite normal 96.083s、PostgreSQL normal 432.301s 已分别完成；原 Result 不改写、三次各有三项实际文件操作和两项独立 verified 检查。它们是对应当时实现的 normal 证据。两次 SQLite C2 race 观察等待实际失败 202.934s / 202.124s 仍保留，第一次临时夹具已清理、没有准确引用制品，不能补造。第二次保留的原 Task 在观察到期时仍有进展，随后暴露原 deadline 未耐久收束的问题；`709d414` 独立修复后，同一原 Task 到期 failed，原 Goal、已闭操作、Submit 回执及无 Result 事实保持。新偏好验收只使用新 scope，不复活原 Task。

原 Task 仍为 5min、命令 1min、Control 5s、原 Lookup 期限不续；根据第二次公开阶段事实，本轮仅将测试观察者改为每报告 6min（原 Task 裁决后最多 1min 观察出版/费用关闭）、三报告全体 20min。该等待调整不是业务修复，原失败不改记通过。

最终固定 `1a47477` 加 477 个 Go/mod/sum 文件摘要的独占 SQLite C2 race 实际 exit 0：Go 623.820s、wall 625.676s。三份原报告分别保存偏好 bullet、更正 plain、撤回后明确默认 plain；九项真实文件操作、六项独立 verified 检查和三次原 Lookup 均完成。Memory 版本为 1、2、无材料，普通材料未变成本人 SourceEvidence。三份 Result 的 CompletedAt 均早于各自原 Task deadline；原 Result 和 submit 回执经同库重开保持。退出后仅用纯元数据接口再次重开，三份原 Task 均 accounting_open=false、USD spent/reserved=0；这个 RuleEngine/managed-file 夹具不做物理模型请求，不代表真实供应商零费用。

证据目录 `/workspace/harness-dev-environment/context14-preference-final-1a47477-sqlite-race` 包含准确 scope/三个原命令/Result、实际文件字节、日志和源码 manifest；最终索引为 `context14-final-verification-v2.json`。v2 只更正初版索引的字段名：`task.context_lookup` Job SourceRef 指向原 ContextBatch，不能称为单个 LookupID；初版保留。此前 PostgreSQL C2 normal、两库 resolver race、两次 C2 失败和同一原失败 Task 的 deadline 恢复制品均在索引中保留准确范围。

整合时 Brain preflight 原参与者不足曾真实 RED `undeclared_transaction_participant`（4.741s）；纯元数据 Tx 显式增加 Memory/Content/Governance 后，两库受影响 race PASS 44.201s，独立提交 `1a47477`。原父/外部来源准备仍在 Tx 外，最终完整 gate 不减少。本工单关闭的是本地四 resolver、固定查询责任/累计预算/当前撤回门禁和有限 C2 实际结果；跨 owner 新材料的当次证明、外部模型质量及生产部署分别由对应工单验收，不作为本地尚未编码的替代理由。
