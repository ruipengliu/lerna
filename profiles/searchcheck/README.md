# 联网问答 profile 入口

当前可分别运行冻结虚构来源的本地 HTTP 与固定回放协议验证：

```sh
go run ./cmd/searchcheck -profile frozen-loopback-v1 -case all
go run ./cmd/searchcheck -profile frozen-replay-v1 -case all
```

可将 `all` 换为 `answerable`、`insufficient`、`conflicting` 或 `fetch_failed`。命令不读取 `.env`、不使用模型凭证，不访问公网；所有查询和页面只来自嵌入的冻结材料。Go 与现有 SQLite/文件 Content 实现完成运行，无额外服务依赖。

可调用接口为 `profiles/fetchcheck.CheckFrozenResearch(ctx, caseID)`（HTTP）与 `CheckFrozenResearchReplay(ctx, caseID)`（固定回放）。它和原集成测试共享同一参考装配：Core 管理任务，ActionBrain 提出行动，Catalog 提供能力，SDK/Execution 执行所选 Adapter，证据经受控 Content 进入答案阶段，正式发布后再次运行核对没有新增调用。没有另起测试专用执行循环。

报告区分：

- `RuntimeVerified`：当前有限协议运行的状态、引用、原任务预算及无隐式重放检查。
- `SemanticQuality: not_evaluated`：命令没有独立评分；协议模型保留占位和预选响应，成功退出不表示问答正确率达标。
- `Record`：实际模型输入、原 JSON 输出、正式答案，以及原任务的模型请求/token、行动查询记录数、网络预留和实际搜索/页面请求数。
- `ExternalModelRequests: 0`：协议模型在本地执行，不能与真实 API 模型效果混淆。

v1每例固定上限为 30 秒、3 个行动操作、3 次模型请求、32768 token、64 条 Core 行动查询记录；单页共享网络预留为 2、双页为 3。`ActionQueries` 统计 Core 已记录的行动查询。v1上限保持，补齐计量后的多来源超限仍如实报告。

新增 `reference-loopback-v2` / `reference-replay-v2`，通过 `-queries` 在运行前配置1–128次观察额度，默认128；其他资源上限不变。报告包含QueryLimit，并明确它不是原v1计划的通过结果。例如 `go run ./cmd/searchcheck -profile reference-loopback-v2 -queries 128 -case all`。v1不允许用该参数覆盖预算。版本选择、实际四例结果和剩余验收见 [参考预算v2](../../docs/implementation/22-reference-budget-v2.md)。

无效参数/未配置模式退出 2，运行或输出失败退出 1，有限协议检查通过退出 0。未配置的公网或真实模型模式不会回退为本地成功。报告附带 Go 版本与可取得的构建版本信息；如果构建工具没有嵌入 VCS 信息，Build 为空，不编造版本。

临时 SQLite 和 Content 在每例结束后删除，报告中的输入输出快照仅用于虚构公开材料的离线核验，不是通用受控归档或新授权。固定回放只使用本地快照，成功材料的时间为实际本地读取时间；拒绝快照不伪造 HTTP 403 或页面正文。两模式使用相同冻结正文及任务预留，实际请求数、Mode、URL 与获取时间分别记录，不要求原输出字节相同。

通用可替换模型/搜索配置、公网/真实模型对比、完整持久化恢复、独立语义评价及最后全仓审查仍待完成。原四类期望不变，不能通过修改协议夹具去迎合评分材料宣称整票完成。

`CheckFrozenResearchReopen(ctx, caseID)` 另外提供实际存储关闭重开验证：在搜索与页面行动完成、答案尚未预留时关闭并重开 SQLite/Content，核对原身份、回执、证据与预算；将测试时钟前移 11 秒使原租约过期，再由原任务取得新的工作代次完成答案。返回的 Recovery 记录前后代次及恢复后行动模型调用数。它只验证该固定检查点和原公开测试配置，不是实际进程崩溃、任意中断点恢复或通用宿主配置恢复的证明。

`CheckFrozenResearchWithAnswerModel(ctx, caseID, model)` 可注入实现 `brain.Model` 的本地答案模型，沿用同一 Core/SDK/Content 发布链路。行动规划仍使用本地协议夹具，CLI 尚未接入此选项；远程处理位置在创建任务前被拒绝，不能通过修改位置标签绕过授权。模型须声明身份、结构化文本及硬上限能力，并兼容当前输入 2048、输出 512 token 上限。

替换路径不要求协议夹具特有的主张数量，但继续执行生产证据校验，包括逐字引用和全部已知获取失败的披露。`AnswerModel` 记录生成阶段使用的能力声明；`ModelRequests/ModelTokens` 是已结算用量，`ReservedModelRequests/ReservedModelTokens` 单独记录未知用量保留的请求数及 token 上限，不将未知值当作零消费或已知消费。新增入口的测试仍使用独立的本地协议实现，不构成真实模型语义评价。

独立覆盖评审可在生成结束后调用 `NewReview(caseID, answerJSON)` 创建工作表，逐个填写必要事实的 `covered/not_covered` 判断和理由，再用 `SummarizeReview` 核对答案/评分版本绑定并汇总。初始值为 `unreviewed`；删除、重复必要事实或更换答案会被拒绝。零分母为不适用；`CoverageReviewComplete` 只表示覆盖判断填写完整，不表示答案质量通过。接口不会自行判断语义，也不替代引用支持、范围或缺口评审。已保存协议输出的实例及复现方法见 `docs/implementation/evidence/22-coverage-review/`。

查询计量以消费方调用边界为单位：一次 Content GET/READ/LOOKUP 或 OutcomeStore.Outcome 在实际 I/O 前计费，失败不退款。ContentQueries 与 OutcomeQueries 是 ActionQueries 的子集，不能再次相加；不等同于底层 SQL 语句数。Core 自身资格、租约、控制与扣费事务不递归计费。

研究宿主的输入装配与复查、搜索候选读取、完成判断中的新观察、答案证据与来源继承、正式发布查询，以及获取/搜索 SDK 的执行 Content 读取现已接入原任务额度。重建消费视图或关闭重开存储不能获得新额度。搜索隐私复查也已按原请求接入，包括网络等待时的新观察；驱动内部的 Outcome/证据读取现也通过原请求的 ObservationScope 接入，取消与恢复等边界仍待完整验证。当前冻结冲突和部分失败场景在补计隐私读取后耗尽原 64 条额度，完整组合尚未通过。

同一串行宿主可复用已取得的不可变终结 Outcome，未知结果不缓存；交接到答案阶段不重新读取同一事实，独立入口和重开后重新读取并计费。每次正文访问仍检查当前 Content 权限与来源，失败产物仍与原账本事实核对。外部观察者与候选任务的读取状态隔离，不能预热任务缓存。

任务消费视图最多保留八个引用的已验证长度；执行输入也可使用任务自身成功 PUT 返回的有界长度。每次实际 READ 仍核对完整记录、内容完整性、可用性与当前用途/处理/披露权限，错误长度不能返回部分输入。失败事实缓存同样要求每次受控 READ，不缓存授权决定或失败响应正文。单来源搜索装配的受控读取之后仅作本地投影；多来源末尾复查及模型调用前后的 Validate 保持。

brain.SnapshotAssessor 是可信宿主可选的无 I/O 纯计算接口。MeteredAssessor 则要求宿主在原任务端口逐项计费所有新观察，故不再额外扣外层 goal；旧宿主保持原路径。没有业务读取的模型接纳步骤不扣查询费，模型请求/token 预留保持。这些接口不是模型输出或外部协议可声明的免计费选项。

当前覆盖与剩余入口见 `docs/implementation/22-query-accounting-audit.md`；有限协议运行不代替真实模型语义验收、完整计量审计或最终全仓验证。

获取/搜索驱动的观察作用域可保留最多十二条本作用域成功提交的终结事实，复用前仍检查原请求的当前执行资格；未知或失败提交不缓存。新作用域重新读取账本并计费，不继承旧作用域的长度或事实。此机制不免除证据 LOOKUP/READ 或当前来源检查。

参考宿主当前来源策略保存于SQLite，包含原规则、期限和版本。答案交接不写策略；已有身份恢复只加载现存有效规则，缺失或损坏报错，不重新授予默认权限。每次策略检查读取当前快照，另一实例的撤权可见。此能力不包括策略历史审计或旧参考数据库的自动迁移。
# DuckDuckGo 格式的参考验证

`go run ./cmd/searchcheck -profile reference-loopback-v2 -search-format duckduckgo-html -case all` 使用合成 HTML 搜索响应，经实际本地 HTTP、Core/SDK、页面获取与 Brain 发布答案。使用 `reference-replay-v2` 可运行同材料的零网络回放。缺省搜索格式仍为 `json`；冻结 v1 不接受 HTML 格式覆盖。

这两个入口均不访问 DuckDuckGo 公网，也不验证实际模型语义质量。报告中的 SearchFormat、Mode 与 SemanticQuality 应一起解读。公网集成仍需独立配置与验收。

独立语义评审使用 `NewSemanticReview(caseID, answerJSON, inputJSON)` 与 `SummarizeSemanticReview`，绑定实际输入，保留原覆盖判断，并逐条记录引用支持、答案正文、范围、处置和禁止项。它核对证据结构与评审完整性，不自行判定语义，也不输出综合质量通过。记录格式和支持分母定义见 `docs/implementation/evidence/22-semantic-review/README.md`；该目录四例是历史协议输出的事后补评，不能宣称真实模型或预注册正式质量验收。


## 显式模型位置的参考装配

`CheckReferenceResearch` 现在接受可信宿主填写的 `ResearchConfig.DiscloseTo` 与 `ModelTokens`。二者未配置时保持原 local 模型限制；显式配置时，模型按原声明位置处理，必须有匹配披露许可及足以覆盖两次行动决策和一次答案预留的任务 token 上界。处理许可仅针对本次冻结公开材料，不增加能力执行权。HTTP 与固定回放使用同一授权/预算/发布链，回放不发送搜索或页面请求；如果传入远端模型，答案生成仍会访问其服务。

例如调用方可传 `ResearchConfig{Replay: true, MaxQueries: 128, AnswerModel: model, DiscloseTo: []string{"external-provider"}, ModelTokens: 262144}`；位置许可必须由宿主在配置模型时决定，此例不构成给任意模型位置自动授权的建议。具体预算必须足以容纳模型的原 InputUpper，不能通过改标签或减少能力声明取得接纳。入口不会读取凭证文件，也不代表外部调用费用已获授权。

回放重开保留原页面能力标识及当前持久化策略，防止把原页面误判成搜索候选；未知答案用量仍保留原模型预留。本轮验证使用本地协议实现声明的外部位置，不证明真实云模型可用或语义正确。下一轮方法登记见 `docs/implementation/22-prospective-evaluation.md`，其执行锁定及实际预算授权仍未完成。
