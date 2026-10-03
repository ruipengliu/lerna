# Agent Memory 开源实现与接入边界

[优化方案](https://github.com/ruipengliu/lerna/blob/99c6298a8beb2da5a65decfe925bb5e212ac2dc5/docs/architecture/memory/optimization-plan.md) · [Memory 模块](../architecture/memory/README.md) · [实现设计](https://github.com/ruipengliu/lerna/blob/99c6298a8beb2da5a65decfe925bb5e212ac2dc5/docs/architecture/memory/implementation.md) · [存储基线](https://github.com/ruipengliu/lerna/blob/99c6298a8beb2da5a65decfe925bb5e212ac2dc5/docs/architecture/storage-and-middleware.md)

调研日期：2026-09-28。本篇比较七组代表实现的当前开源代码，服务于本项目 Memory 优化；论文时窗与论文实验另行整理。库不要求在近半年内创建，比较对象是检索当日固定提交中的实际流程。只使用官方仓库、官方文档与已定位源码；没有安装这些库，没有运行其服务或复现其性能数字。

**建议保留 Go Memory owner、PostgreSQL 权威记录、ContentRef 与现有来源和清理契约，把外部实现用于算法参考和隔离评测。** 优先吸收 LangMem 的结构化提取接口、Graphiti 的时间建模与混合检索配方、memU 的外部合成与存储分工；Letta 和 OpenViking 的分层载入、后台整理适合作为后续对照组。整体替换会把本项目已经明确的修订、原命令、处理前授权和派生清理责任重新分散到多个运行时与存储中。[本项目职责与事务边界](https://github.com/ruipengliu/lerna/blob/99c6298a8beb2da5a65decfe925bb5e212ac2dc5/docs/architecture/memory/implementation.md#module-shape)是这个判断的依据。

## 1. 版本、许可证与证据边界

下表提交日期为 GitHub commit 元数据的 UTC 日期，不代表稳定发布日。链接固定到本次读取的提交；文档站点单独注明。许可证按仓库中的许可证文本核对，未对第三方依赖逐项审计。

| 实现 | 固定快照与提交日期 | 仓库许可证 | 本次已检查的范围 |
| --- | --- | --- | --- |
| Mem0 OSS | [94c3fe9f](https://github.com/mem0ai/mem0/commit/94c3fe9f238f3dbf29c9ce98643bd71eb13077cd)，2026-09-25 | [Apache-2.0][m0-license] | Python `Memory` 的写入、检索、显式修改、删除及实体清理；README 的 OSS／托管区分 |
| Graphiti | [6b4b56ff](https://github.com/getzep/graphiti/commit/6b4b56ff6f4b1e4e69c3c3c5487cf1b8762c483a)，2026-09-27 | [Apache-2.0][g-license] | episode 写入、时间字段、检索配方、`remove_episode`；不把 Zep 托管服务算入 |
| Letta Code | [c864f153](https://github.com/letta-ai/letta-code/commit/c864f1532b328aab4bb76cc68a86d5f014de27b8)，2026-09-28 | [Apache-2.0][l-license] | 当前 MemFS 文档、memory worker 与其操作说明；未审阅所有 Git 同步实现 |
| LangMem / LangGraph Store | LangMem [9d033b47](https://github.com/langchain-ai/langmem/commit/9d033b47d9ce53e37e92c92241b0496c0278932e)，2026-09-09；Store [07b33185](https://github.com/langchain-ai/langgraph/commit/07b33185eab893be2ed031eedae52f09314bf77c)，2026-09-27 | [MIT][lm-license] / [MIT][lg-license] | manager 的纯提取与带存储两条路径、管理工具、BaseStore 类型；未运行 PostgreSQL 后端 |
| MemOS | [a7367d07](https://github.com/MemTensor/MemOS/commit/a7367d07e55db61099f7b4e2c1108bc5831a24f3)，2026-09-22 | [Apache-2.0][mos-license] | Python TreeTextMemory 与部分组织代码、local plugin 检索；产品删除 API 仅核对文档 |
| memU | [2c050bc9](https://github.com/NevaMind-AI/memU/commit/2c050bc9681a4c0aff1af211a000e73d14f33356)，2026-09-21 | [Apache-2.0][mu-license] | 当前 `MemoryService`、`AgenticMixin`、memorize lifecycle；不沿用旧 category/item 流程 |
| OpenViking | [b2f1f9d2](https://github.com/volcengine/OpenViking/commit/b2f1f9d2f12cd031ec2f90b069b9b78528c5f2da)，2026-09-28 | 主项目 [AGPL-3.0][ov-license]；其他组件另定 | session／提取／检索／ACL 文档、memory updater 与 experience lineage 部分源码；未核验分布式商业部署 |

OpenViking README 另列 `crates/ov_cli` 与大部分 `examples` 为 Apache-2.0、Hermes 插件为 MIT、`third_party` 按各自许可证；不能把主项目或示例的许可证扩展到整个仓库。[组件许可证范围][ov-readme]

三处版本漂移会直接影响选型：

- **Mem0 当前写入是 ADD-only 抽取。** README 与 `_add_to_vector_store` 的单轮 additive 实现一致，`add` 局部 docstring 仍残留“add/update/delete”旧描述。显式 `update`、`delete` API 继续存在，不能混同为自动抽取中的更新／删除。[当前流程][m0-add]
- **Letta 原仓库已迁移。** `letta-ai/letta` 当前主分支说明 V1 API server 位于 `archive`，活跃实现转到 `letta-code`。不能把旧 MemGPT 的 Python server 结构当作本次当前版本。[迁移说明][l-migration]
- **memU 当前也是新流程。** `MemoryService` 明确为 embedding-only，记忆与技能由外部 agent 编写后提交；旧资料描述的内部 LLM 分类、充分性判断与逐层 RAG 不适用于这个提交。GitHub license API 对该仓库返回 `NOASSERTION`，但实际 `LICENSE.txt` 是 Apache-2.0，故采用许可证正文。[服务源码][mu-service]、[许可证][mu-license]

公开 README 中的性能数字不能直接构成本项目选型排名。尤其 Mem0 明示其当前 benchmark 数字含托管平台专有优化；Graphiti 明示 Zep 使用专有 Context Graph Engine；OpenViking 明示只开放 VikingMem 的部分能力，商业自管理部署另含分布式与支持能力。[Mem0 范围][m0-readme]、[Graphiti 与 Zep][g-readme]、[OpenViking 范围][ov-readme]

## 2. 从输入到删除的流程比较

这里的“删除”首先指各库公开的对象操作。只有证明了索引、原输入、历史、摘要、远端副本和恢复路径的关闭行为，才能与本项目的来源关闭等价；下表没有作这种等价声明。

| 实现 | 接纳、提取与更新 | 索引、召回与排序 | 整理、历史与删除 | 接入本项目的代价 |
| --- | --- | --- | --- | --- |
| Mem0 OSS | 消息按 user/agent/run 范围取得相关旧记忆，单轮 LLM 提取新增事实；批量 embedding、文本 hash 去重、写入与实体链接 | 向量召回形成候选；可叠加 BM25 与实体信号，另可配 reranker | 显式更新／删除写 history；过期过滤不等于擦除 | Python 或 TS 运行时、embedding／LLM、选定向量后端与 history；不能直接替代同事务 Memory 元数据。[源码][m0-add]、[检索][m0-search]、[删除][m0-delete] |
| Graphiti | episode → 节点与边抽取 → 实体／关系解析、去重与冲突失效 → 图写入 | BM25、向量、图遍历；RRF、MMR、节点距离与 cross-encoder 等配方 | 保留事实有效区间和系统失效时间；删除 episode 按其边和独占节点处理 | Python、图数据库、LLM 和 embedding；图索引与现有 PostgreSQL 间新增一致性和清理责任。[写入][g-ingest]、[配方][g-recipes]、[删除][g-delete] |
| Letta Code | agent 编辑 MemFS；后台 worker 在私有 worktree 准备变更，完成后合并 | 当前主线是可检查的 Git 记忆文件与按需读取；此处未将历史版本的 archival vector store 算入 | dreaming 整理最近会话；Git 保留版本，冲突保留工作分支并报告 | 接入的是 TypeScript agent harness；整体替换会与本项目 Orchestrator、Brain 职责重叠。[当前文档][l-memory]、[worker][l-worker] |
| LangMem / Store | `create_memory_manager` 返回结构化变更；带 Store 的 manager 还会检索旧项并执行 put/delete | namespace、字段过滤、可选向量搜索；检索与模型驱动整理分开 | 允许合并／更新／移除；Store 提供对象删除与 TTL，业务来源链由应用补充 | 纯提取可做隔离 Python 实验；直接接入管理工具将引入第二条写权威路径。[manager][lm-manager]、[Store][lg-store] |
| MemOS | Python 服务经 reader／MemCube／组织机制保存文本记忆；local plugin 是另一条本地流水线 | TreeTextMemory 组合向量、全文、图扩展与 rerank；local plugin 在 SQLite 上组合关键词／向量与记忆层 | 图组织可保留 `MERGED_TO` 关系；产品 API 文档支持按 memory/file/filter 删除 | Python 服务默认自建 Neo4j + Qdrant；local plugin 是 TS/SQLite，二者不能拼成一个已验证部署。[部署][mos-readme]、[Tree][mos-tree]、[local 检索][mos-local] |
| memU | prepare 完成会话工作区 → 外部 agent 生成 Markdown → `commit_results`；按文件名/track 或资源 URL 更新 | 文本与描述 embedding；segment 相似度召回后汇总到 file，resource 单独召回 | 外部 agent 决定修改／合并；提交对变化内容重建相应段，未变化内容复用向量 | Python + SQLite 或 PostgreSQL，合成过程可复用现有 Orchestrator；存储提交仍须重做本项目事务约束。[服务][mu-service]、[提交和检索][mu-agentic]、[lifecycle][mu-lifecycle] |
| OpenViking | 文件 parse/tree → 异步摘要／向量；session commit 触发记忆抽取、去重、merge/delete | L0 摘要、L1 概览、L2 内容；目录检索；`search` 增加查询意图分析与可选 rerank | session archive 保存 memory diff；经验／轨迹存在专门关联；ACL 随索引更新 | Python、AGFS／向量存储、LLM／embedding；统一上下文数据库覆盖范围大于本项目 Memory，当前许可证也须单独接受。[提取][ov-extraction]、[检索][ov-retrieval]、[session][ov-session]、[ACL][ov-acl] |

## 3. 逐项证据与可复用部分

### 3.1 Mem0：新增事实管线可参考，不能照搬记忆写权威

当前 `_add_to_vector_store` 先用输入消息检索已有记忆，给旧记忆建立临时编号，再以 additive prompt 生成新事实；随后批量向量化，在本批和已召回旧项中按正文 hash 排重。这个 hash 只表示文本重复，范围是当前批及此次旧项结果，不是全库的原命令幂等约束；相同正文也不意味着相同观察时间和来源。适合借鉴“先提出有限新增候选，再由确定规则发布”，本项目仍保留 `command_id`、`candidate_id` 和修订比较。[具体阶段][m0-add]

当前 `_search_vector_store` 分别请求向量和关键词结果，但用于 `score_and_rank` 的候选列表来自 `semantic_results`。因此这条源码路径不能被描述成“词法与向量候选取并集”：纯关键词命中若未进入语义候选，不会仅因 BM25 得分加入输出。本项目若实验 RRF，应明确两路候选并集、重复修订归并、每路截断和总预算，不能只复用“hybrid”名称。[候选构造][m0-search]

删除路径先删除 vector record，再向 history 写入含 `prev_value` 的 DELETE 事件；实体反向链接清理被注释为 non-fatal。`delete` 返回成功由此不能证明旧正文不再存在于 history、原始消息、日志或其他副本。代码还把 `reference_date` 明确标为 Platform-only；创建／更新时间、过期过滤也不等于本项目的事实有效时间与来源关闭。[删除实现][m0-delete]、[查询参数][m0-query]

**本项目选择：** 借鉴有限旧项辅助抽取、批量 embedding 与失败区分，不使用其 SDK 直接提交权威记忆。只有产品目标收缩为单服务个性化、可以接受 SDK 的更新与删除语义，并愿意重写现有契约时，整体采用才成为竞争方案。

### 3.2 Graphiti：时间与关系证据最有借鉴价值

Graphiti 区分 episode 的 `created_at` 与 `valid_at`；关系边还保存 `valid_at`、`invalid_at`、`expired_at`，分别描述事实开始、停止成立以及系统使其失效的时间。检索配方把 BM25／cosine／BFS 与 RRF／MMR／节点距离／cross-encoder 分开选择。值得先吸收的是这种可分离的时间和检索机制；不必为了保存这些字段先引入图数据库。[时间字段][g-edges]、[写入][g-ingest]、[检索配方][g-recipes]

源码的 `remove_episode` 找到该 episode 关联的关系，删除 `edge.episodes[0]` 等于被删 episode 的边，再删除仅由该 episode 提及的节点，最后删除 episode。这是具体的图维护规则；本次检查未看到它在这个方法中重算所有共享节点摘要、传递派生物或远端持有者。事实被新 episode 否定也只是时间失效，不能视为用户撤回处理许可。[删除源码][g-delete]

官方 README 对依赖与服务边界较明确：Graphiti 自建图后端，Zep 的托管专有引擎、用户／会话管理和规模化服务保证另属产品能力。当前 README 列 Neo4j、FalkorDB、Neptune 等，并标记 Kuzu deprecated；不能沿用旧推荐把 Kuzu 当默认新增依赖。[当前部署边界][g-readme]

**本项目选择：** 优先将断言观察时间、适用时间与收录时间拆开；RRF 在 QueryService 内评测。只有真实任务反复需要跨实体多跳、关系历史显著提升任务成功率，而且额外图存储的来源过滤与删除成本得到验证时，再把 Graphiti 作为派生候选服务。

### 3.3 Letta：后台整理的隔离和冲突处理有用，Git 历史不是删除证明

当前官方文档使用 MemFS：Git 保存可检查的记忆文件，dreaming 后台审阅近期对话并整理；触发可按完成步数或上下文压缩配置。文档中的“agent reviews”是第二个后台会话复核，不是用户确认。本项目需要独立的发布许可，不能把模型二次审阅当用户保存同意。[当前 memory 文档，读取于 2026-09-28][l-memory]

`runMemoryWorker` 的更新 worker 在私有 worktree 编辑，取消则丢弃该工作分支内容；成功后合并，冲突时保留分支并报告。源码还区分“已经提交／同步”和后续 system prompt 刷新，刷新失败不冒充写入失败。这与本项目“准备候选／正式发布／索引补齐分别确认”的思路相合，但 Git merge 没有替代 `expected_revision`、来源门禁或删除优先规则。[worker 实现][l-worker]

内置 memory worker 的操作说明要求保留事实范围和例外、避免推断用户偏好、修改后维护索引；当前布局区分核心记忆文件、延后读取目录和技能。可将这些要求转成提取 rubric 和读时 token 预算实验；不应把其 agent 工具权限、Git 同步和远端 Cloud 状态整套引入 Memory。[worker 说明与布局][l-worker-instructions]、[当前产品与 Cloud 边界][l-readme]

**本项目选择：** 复用隔离整理、冲突保留和有限核心上下文思路。Git 适合用户审阅编辑结果，但有意保留历史；若用于含私密内容的存储，必须另外设计历史、备份和远端副本关闭，不能把当前树删文件等同于擦除。

### 3.4 LangMem / Store：最贴近“模型提议、owner 决定”的接入方式

`create_memory_manager` 接收消息与已有记忆，按 schema 返回 `ExtractedMemory`；它本身不必决定存储实现。默认允许 insert/update、禁止 delete，配置可控制行为。这个函数可用于隔离实验，或按其接口思想在 Go 与 Brain 现有契约上实现：输出候选和依据，由 MemoryWriter 核对来源、范围、修订和发布许可。[纯 manager][lm-manager]

带存储的 `MemoryStoreManager` 则是另一条路径：先检索相关记忆，模型处理后执行 `aput`／`adelete`；当前 async 路径以 `asyncio.gather` 提交多个存储操作。管理工具也直接调用 Store 的 put/delete。这些代码提供方便的 agent 写工具，但从该调用层不能推出“原命令、所有变更、来源边、jobs 同事务”。本项目应使用纯提取层，避免直接挂这些写工具。[写回路径][lm-write]、[管理工具][lm-tools]

LangGraph Store 是跨会话 namespace/key JSON 数据容器，支持过滤、可选向量检索和 TTL；checkpointer 保存单会话图执行状态，两者职责不同。BaseStore 的 namespace 是数据定位范围，不能单凭命名就证明 caller 获得处理、披露或外发许可。TTL 还可在读取时刷新，必须另与固定保留上限和来源清理政策对齐。[Store 类型][lg-store]、[官方持久化分工][lg-docs]

**本项目选择：** 将 LangMem 作为提取策略的轻量对照组，先固定 schema、模型、输入及旧项数量；不引入 LangGraph 执行引擎。只有主应用未来统一迁移到 LangGraph，而且接受重新实现所有领域权威契约时，才考虑原生 Store 集成。

### 3.5 MemOS：先拆清 Python 服务与本地插件，再决定借鉴哪一层

MemOS 当前仓库包含两种明显不同的部署：Python Self-Host 路径列出 Neo4j + Qdrant；local plugin 使用 SQLite。TreeTextMemory 建立 graph store、embedder、组织器和 reranker，搜索入口组织 task goal、图检索、rerank 与 reasoner；local plugin 的检索源码则在关键词与向量渠道、不同记忆层之间并行召回，再做排序、LLM 相关性筛选和 trace／episode 去重。不能把二者的功能、依赖和性能拼接成同一“MemOS 能力”。[部署划分][mos-readme]、[Tree 实现][mos-tree]、[local 检索][mos-local]

组织代码保留 `MERGED_TO` lineage 边，体现“合并后保留关联”的价值；但这个关联仍须与本项目精确 ContentRef、实际处理输入和关闭传播分开核对。产品删除文档声称对图与向量做同步物理清理，并支持 file 关联删除；本次仅检查了该 API 文档，没有证明其跨后端原子性。已读的 TreeTextMemory.delete 则逐节点删除并捕获异常记录 warning。两者属于不同层，不能据文档措辞推出当前整个部署的强事务清理。[组织代码][mos-organize]、[产品删除文档][mos-delete-doc]、[Tree 删除][mos-tree-delete]

**本项目选择：** 参考 local plugin 的去重、缺 embedding 时保留关键词渠道以及分层成本观测。暂不引入完整 MemScheduler、图／向量双库、参数记忆或 KV-cache 管理；这些职责超出本轮跨任务事实与经验检索目标。若后续确实同时管理模型状态、多知识库及复杂图推理，再单独验证整体采用的收益。

### 3.6 memU：当前外部合成模式可简化接入，但提交并非整批原子

当前流程让外部 agent 阅读已完成会话，生成 memory/skill Markdown，`MemoryService` 只存储、向量化和检索。`commit_results` 先规划资源／文件变化，批量取得 embedding，再执行写入；未变化文本不重复调用 embedding。输入路径适合作为本项目“Orchestrator 负责提取、Memory 负责候选与发布”的工程参考。[服务职责][mu-service]、[提交实现][mu-commit]

源码明确指出各 repository 调用各自提交事务：embedding 失败发生在写入前，可以零写；存储失败不具备同样保证。`memorize` lifecycle 在 backend commit 返回后才推进跟踪 manifest，这比按意图推进检查点稳妥，但不能解决底层多个已写项的部分成功。本项目仍需原命令、候选唯一发布和同事务 jobs，不能因方法叫 `commit_results` 就赋予其本项目的 `applied` 含义。[提交边界][mu-commit]、[检查点顺序][mu-lifecycle]

`progressive_retrieve` 的当前实现是单次 query embedding、segment 向量召回、把命中片段汇总到文件以及 workspace resource 向量召回。代码注释明确没有意图路由、充分性判断或摘要循环。它说明可先保留简单可测的检索路径；“progressive”名称不保证存在自适应多轮搜索。[检索实现][mu-retrieve]

**本项目选择：** 参考合成与存储解耦、增量索引和文件／片段追踪，生成式提取与整理由现有 Brain／Orchestrator 完成；本地嵌入按[优化方案](https://github.com/ruipengliu/lerna/blob/99c6298a8beb2da5a65decfe925bb5e212ac2dc5/docs/architecture/memory/optimization-plan.md)的受控索引／查询适配器处理。将全文 wiki 的可读性用于用户管理视图，不把 wiki 文本覆盖升级成事实修订或来源合并机制。

### 3.7 OpenViking：分层上下文可试验，ACL 一致性直接影响替换结论

OpenViking 将资源解析与语义生成分开，目录维护 L0 摘要和 L1 概览，完整内容位于 L2；session commit 的归档与后续摘要／提取也分开完成。当前 session 文档描述候选 skip/create/none 与已有项 merge/delete，archive 内的 memory diff 保留修改前后及删除内容。它有利于可观察性，同时意味着删除一个当前记忆并没有抹掉历史 diff。[提取][ov-extraction]、[session 与 diff][ov-session]

检索区分 `find` 与 `search`：前者无意图分析，后者可基于会话生成至多五个 typed queries；随后按目录递归召回并按配置 rerank。层级概要可减少读入正文，但概要本身是处理来源形成的派生物，必须单独受授权、保留与关闭约束；目录命中或相似度较高不允许扩大处理范围。[检索设计][ov-retrieval]

当前源码为经验与轨迹保留专门 lineage tags，轨迹结果区分 success/failure/partial/unknown/unfinished；memory updater 还保存切分片段的原 message ID。可借鉴其“经验必须关联真实轨迹与结果”的结构。本次没有证明这些引用构成全部处理输入的完备闭包，不能取代本项目 `source_edges`。[经验关联][ov-lineage]、[输入片段映射][ov-updater]

官方 ACL 文档给出了决定性的替换限制：ACL 默认关闭；启用后权限字段仍存在 context index 中，变更随索引更新生效，明确不保证强一致。这与本项目“当前授权由权威库核验、索引仅产生候选、来源关闭先禁新使用”的契约不同。另一个 privacy 文档的能力是 skill 敏感值占位及读取恢复，不能被理解为覆盖所有记忆来源的撤权体系。[ACL 一致性][ov-acl]、[privacy 的实际范围][ov-privacy]

**本项目选择：** 只把分层摘要、检索轨迹和经验关联列为独立实验；来源边和有效性仍由现有 owner 保存。整体替换要同时接受更大的上下文数据库边界、当前 AGPL-3.0 许可证和重新实现强授权／关闭机制的成本，现阶段缺乏这样做的依据。

## 4. 对本项目优化的落实建议

这些是基于上述实现和本项目契约得出的建议，不是库已证明的性能结论。

| 优化点 | 推荐做法与所属组件 | 主要代价、边界与改选条件 |
| --- | --- | --- |
| 提取质量 | ExtractionCoordinator 传入有限已获准输入、有限旧项和结构化 schema；模型返回新增／修订建议与证据定位，MemoryWriter 再决定发布 | 增加候选校验与标注成本；不因相似度高自动覆盖、删除或合并不同时间／范围／来源的记录。先对照 LangMem 纯提取和 Mem0 additive 思路 |
| 中文与语义召回 | QueryService 保留原字面基线，独立评估中文词法候选及可选语义候选；在相同获准集合中合并、RRF，再冻结准确修订集合 | 增加 tokenization／embedding 成本；不能把 metadata filter 的存在当作处理前授权证明。若收益不显著，保留词法方案；Graphiti 配方只作为算法参考 |
| 时间判断 | 在候选正文／内部断言结构中区分观察、适用和收录时间；冲突保留证据，不把较新或较高分直接当裁决 | 会增加标注与查询规则；先不新增图数据库。只有多跳／历史关系任务显著受益，再引入派生图 |
| 经验与程序记忆 | 从正式任务终态、实际操作和验证结果生成 `experience` 候选，保留适用前提、失败与未知结果；与 skill 执行能力分开 | 提取会耗模型预算；不能因 agent 写出一份教程就声明步骤有效。OpenViking 轨迹结果分类、memU 外部 skill 合成仅提供组织参考 |
| 后台整理 | 复用既有 jobs／原提取任务；准备候选，按准确旧修订条件发布；冲突留待重新判断 | 整理涉及的所有输入都新增来源依赖；候选取消不得删除已发布记忆。Letta worktree 隔离思路可参考，最终裁决仍走现有事务 |
| 读时压缩 | 先评测有限片段选择，再评测摘要；正文读取资格、摘要使用依据、token 预算和缺口一起传给 Orchestrator | 摘要可能丢例外和时间，且本身需要派生清理；保持可追溯原引用，不能把 L0/L1 命中当完整证据 |
| 删除与撤权 | 所有外部索引、向量、摘要与实验服务登记为派生持有者；原 owner 先关闭，worker 分别核对停止使用和物理清理 | 自动合并越多，来源 fan-out 与清理成本越大；复用任何库都不能跳过现有 ClosureWorker 或以 TTL 替代显式关闭 |

最小接入优先采用**算法迁移或仅返回候选的隔离服务**：Go 负责固定输入、当前许可、超时和预算；外部运行时只处理这次有限输入并返回结果；Memory owner 保存正式修订、原答复、来源边和 jobs。同步查询路径优先复用已有 Go 组件，避免为单个排序公式增加 Python 服务。只有实验需要原库才能公平对照时才增加隔离适配器，测试完成后可移除。

## 5. 验证要求与本次未证事项

评测必须固定库提交、模型、prompt、语言、语料、输入切分、保存策略、候选预算、reranker 与硬件；分别计量写入成本、检索成本和最终任务结果。自动抽取会选择性丢弃输入，ADD-only、自动合并、全量事件保存也会改变题库可回答性，不能只比较最终回答分数。各库 README 的 LoCoMo 或 LongMemEval 数字不在此处横向排序。

| 最可能推翻优化的场景 | 本项目需要观察的结果 |
| --- | --- |
| 两条正文相同，但来源／观察时间不同 | 文本去重不丢来源或时间；按准确身份合并命中，事实合并需另行证明 |
| 提取重试、批量写入中途失败、两个发布者竞争 | 原候选只发布一次，原命令结果可查；无第二份事实或无责任的半成品 |
| 索引与授权更新错开 | 被撤权项不能参与新排名／模型处理；授权不由滞后索引裁决 |
| 来源关闭而图摘要、Git 历史、history 表仍存在 | 先停止新使用，再逐持有者保留清理责任；不将对象 API 成功等同全链擦除 |
| 整理后遗漏否定、范围、时间或失败条件 | 原有任务质量与安全边界不退化；保留准确来源以便回退和审阅 |
| 无本地 embedding／LLM 或外部服务失败 | 按已批准路径退回字面检索／等待提取，并返回缺口；不自动把私密输入发往云端 |

**已完成：** 当前仓库定位、固定提交与许可证核对、上述源码路径和官方流程文档静态检查、与项目契约的语义对照。**未完成：** 第三方服务运行、吞吐／成本／正确率复现、跨后端故障注入、来源关闭覆盖证明及传递依赖许可证审计。任何“可整体替换”的结论都需要这些运行证据，本文只推荐有界的算法和组织方式实验。

[m0-license]: https://github.com/mem0ai/mem0/blob/94c3fe9f238f3dbf29c9ce98643bd71eb13077cd/LICENSE
[m0-readme]: https://github.com/mem0ai/mem0/blob/94c3fe9f238f3dbf29c9ce98643bd71eb13077cd/README.md
[m0-add]: https://github.com/mem0ai/mem0/blob/94c3fe9f238f3dbf29c9ce98643bd71eb13077cd/mem0/memory/main.py#L881-L1103
[m0-search]: https://github.com/mem0ai/mem0/blob/94c3fe9f238f3dbf29c9ce98643bd71eb13077cd/mem0/memory/main.py#L1642-L1701
[m0-query]: https://github.com/mem0ai/mem0/blob/94c3fe9f238f3dbf29c9ce98643bd71eb13077cd/mem0/memory/main.py#L1393-L1436
[m0-delete]: https://github.com/mem0ai/mem0/blob/94c3fe9f238f3dbf29c9ce98643bd71eb13077cd/mem0/memory/main.py#L2114-L2142
[g-license]: https://github.com/getzep/graphiti/blob/6b4b56ff6f4b1e4e69c3c3c5487cf1b8762c483a/LICENSE
[g-readme]: https://github.com/getzep/graphiti/blob/6b4b56ff6f4b1e4e69c3c3c5487cf1b8762c483a/README.md
[g-ingest]: https://github.com/getzep/graphiti/blob/6b4b56ff6f4b1e4e69c3c3c5487cf1b8762c483a/graphiti_core/graphiti.py#L1113-L1227
[g-edges]: https://github.com/getzep/graphiti/blob/6b4b56ff6f4b1e4e69c3c3c5487cf1b8762c483a/graphiti_core/edges.py#L260-L285
[g-recipes]: https://github.com/getzep/graphiti/blob/6b4b56ff6f4b1e4e69c3c3c5487cf1b8762c483a/graphiti_core/search/search_config_recipes.py
[g-delete]: https://github.com/getzep/graphiti/blob/6b4b56ff6f4b1e4e69c3c3c5487cf1b8762c483a/graphiti_core/graphiti.py#L1824-L1852
[l-license]: https://github.com/letta-ai/letta-code/blob/c864f1532b328aab4bb76cc68a86d5f014de27b8/LICENSE
[l-migration]: https://github.com/letta-ai/letta/blob/5bcdd177d70fa2b31a754cfcd801e77b2e1ab16a/README.md
[l-readme]: https://github.com/letta-ai/letta-code/blob/c864f1532b328aab4bb76cc68a86d5f014de27b8/README.md
[l-memory]: https://docs.letta.com/configuration/memory
[l-worker]: https://github.com/letta-ai/letta-code/blob/c864f1532b328aab4bb76cc68a86d5f014de27b8/src/agent/subagents/memory-worker.ts#L69-L215
[l-worker-instructions]: https://github.com/letta-ai/letta-code/blob/c864f1532b328aab4bb76cc68a86d5f014de27b8/src/agent/subagents/builtin/memory-v2.md
[lm-license]: https://github.com/langchain-ai/langmem/blob/9d033b47d9ce53e37e92c92241b0496c0278932e/LICENSE
[lg-license]: https://github.com/langchain-ai/langgraph/blob/07b33185eab893be2ed031eedae52f09314bf77c/LICENSE
[lm-manager]: https://github.com/langchain-ai/langmem/blob/9d033b47d9ce53e37e92c92241b0496c0278932e/src/langmem/knowledge/extraction.py#L536-L692
[lm-write]: https://github.com/langchain-ai/langmem/blob/9d033b47d9ce53e37e92c92241b0496c0278932e/src/langmem/knowledge/extraction.py#L1022-L1137
[lm-tools]: https://github.com/langchain-ai/langmem/blob/9d033b47d9ce53e37e92c92241b0496c0278932e/src/langmem/knowledge/tools.py#L289-L335
[lg-store]: https://github.com/langchain-ai/langgraph/blob/07b33185eab893be2ed031eedae52f09314bf77c/libs/checkpoint/langgraph/store/base/__init__.py
[lg-docs]: https://docs.langchain.com/oss/python/langgraph/persistence
[mos-license]: https://github.com/MemTensor/MemOS/blob/a7367d07e55db61099f7b4e2c1108bc5831a24f3/LICENSE
[mos-readme]: https://github.com/MemTensor/MemOS/blob/a7367d07e55db61099f7b4e2c1108bc5831a24f3/README.md
[mos-tree]: https://github.com/MemTensor/MemOS/blob/a7367d07e55db61099f7b4e2c1108bc5831a24f3/src/memos/memories/textual/tree.py#L55-L210
[mos-tree-delete]: https://github.com/MemTensor/MemOS/blob/a7367d07e55db61099f7b4e2c1108bc5831a24f3/src/memos/memories/textual/tree.py#L402-L437
[mos-organize]: https://github.com/MemTensor/MemOS/blob/a7367d07e55db61099f7b4e2c1108bc5831a24f3/src/memos/memories/textual/tree_text_memory/organize/manager.py#L425-L453
[mos-local]: https://github.com/MemTensor/MemOS/blob/a7367d07e55db61099f7b4e2c1108bc5831a24f3/apps/memos-local-plugin/core/retrieval/retrieve.ts#L291-L542
[mos-delete-doc]: https://github.com/MemTensor/MemOS/blob/a7367d07e55db61099f7b4e2c1108bc5831a24f3/docs/en/open_source/open_source_api/core/delete_memory.md
[mu-license]: https://github.com/NevaMind-AI/memU/blob/2c050bc9681a4c0aff1af211a000e73d14f33356/LICENSE.txt
[mu-service]: https://github.com/NevaMind-AI/memU/blob/2c050bc9681a4c0aff1af211a000e73d14f33356/src/memu/app/service.py#L24-L56
[mu-agentic]: https://github.com/NevaMind-AI/memU/blob/2c050bc9681a4c0aff1af211a000e73d14f33356/src/memu/app/agentic.py
[mu-retrieve]: https://github.com/NevaMind-AI/memU/blob/2c050bc9681a4c0aff1af211a000e73d14f33356/src/memu/app/agentic.py#L187-L329
[mu-commit]: https://github.com/NevaMind-AI/memU/blob/2c050bc9681a4c0aff1af211a000e73d14f33356/src/memu/app/agentic.py#L343-L391
[mu-lifecycle]: https://github.com/NevaMind-AI/memU/blob/2c050bc9681a4c0aff1af211a000e73d14f33356/src/memu/app/memorize/lifecycle.py#L164-L176
[ov-license]: https://github.com/volcengine/OpenViking/blob/b2f1f9d2f12cd031ec2f90b069b9b78528c5f2da/LICENSE
[ov-readme]: https://github.com/volcengine/OpenViking/blob/b2f1f9d2f12cd031ec2f90b069b9b78528c5f2da/README.md
[ov-extraction]: https://github.com/volcengine/OpenViking/blob/b2f1f9d2f12cd031ec2f90b069b9b78528c5f2da/docs/en/concepts/06-extraction.md
[ov-retrieval]: https://github.com/volcengine/OpenViking/blob/b2f1f9d2f12cd031ec2f90b069b9b78528c5f2da/docs/en/concepts/07-retrieval.md
[ov-session]: https://github.com/volcengine/OpenViking/blob/b2f1f9d2f12cd031ec2f90b069b9b78528c5f2da/docs/en/concepts/08-session.md
[ov-acl]: https://github.com/volcengine/OpenViking/blob/b2f1f9d2f12cd031ec2f90b069b9b78528c5f2da/docs/en/concepts/15-acl.md
[ov-privacy]: https://github.com/volcengine/OpenViking/blob/b2f1f9d2f12cd031ec2f90b069b9b78528c5f2da/docs/en/concepts/13-privacy.md
[ov-lineage]: https://github.com/volcengine/OpenViking/blob/b2f1f9d2f12cd031ec2f90b069b9b78528c5f2da/openviking/session/memory/experience_lineage.py#L29-L108
[ov-updater]: https://github.com/volcengine/OpenViking/blob/b2f1f9d2f12cd031ec2f90b069b9b78528c5f2da/openviking/session/memory/memory_updater.py#L231-L278
