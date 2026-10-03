# 数据对象、请求流程与读写频次对比

研究日期：2026-10-01。参考仓库固定提交沿[源码清单](README.md#reports)；本项目依据本轮更新后的架构设计。参考项目通过源码静态追踪计数，本项目没有可运行存储适配器；本文不给出实测 IOPS、时延或跨项目性能排名。

核心判断：本项目需要轻量 Session，但不应让 Session 再拥有任务执行状态机。比会话日志更多的持久事实来自任务、授权、效果恢复和费用承诺；过度设计风险在于把每项逻辑事实都拆成表、事务、RPC，或让所有可选能力进入普通请求。对象数、日志条数、SQL 数和磁盘 IO 必须分别比较。

## 1. 统一场景与计数口径

统一为已打开、已有历史和标题的暖会话；输入为短文本，固定同一模型输出与工具结果，不触发 reasoning、压缩、自动标题、记忆、子 Agent、审批、重试、持续 Goal 或定时任务。默认已有合法权限，模型费用正常返回，系统提示及工具配置保持不变。冷打开、新会话首次落盘、故障恢复及可选插件另列。

| 场景 | 固定外部工作量 | 观察终点 |
| --- | --- | --- |
| A：直接回答 | 1 条用户输入，1 次模型请求，1 个完整助手答复 | 完整答复进入原项目的历史／结果保存路径 |
| B：读取后回答 | 1 条用户输入，第 1 次模型发出 1 个普通工具调用，1 个短文本工具结果，第 2 次模型给最终答复 | 工具结果和完整答复进入原项目的保存路径 |
| C：关闭进程后重新打开 | 使用同一历史与原对象，不新增用户意图 | 恢复历史及原未决责任，尚不自动再发模型请求 |

工具选择单次读取已有文本文件；文件正文那次业务读取单列，不计入 Harness 元数据读取。Prime 默认工具形式若使用程序 cell，按其本身 host 路径解释，不把 kernel 执行次数换算成其他项目普通工具次数。各项目同样得到回答，并不意味着有相同的取消、授权、恢复或完成保证。本项目若还需要独立质量评估／效果验证，其新增调用和写入另外计入，不能让它们混入固定为 1／2 次模型的分母。

本项目这一比较具体选择：每条新输入创建一个 Task，每次模型调用恰由一个 Decision 承担，没有额外的规则 Decision；应用消息采用 Content 引用。有界内联消息、继续已有 Task 或增加规则判断都是合法变体，但要另算所触发阶段。工具内部的文件历史、缓存或其他存储工作也须单列，不能冒充下面仅统计核心循环的总次数。

| 记号 | 计数单位 | 不能推导的结论 |
| --- | --- | --- |
| E | 写入历史的逻辑记录／事件条数 | 多条记录可合并一次 write；单条可能分多次写 |
| R／W | 文件读取／追加入口或 SQL SELECT／INSERT／UPDATE 调用次数，注明是哪种 | 不等于块设备 IO，也不能跨不同单位直接排名 |
| Tx | 明确数据库提交或协议要求的持久提交阶段 | 多个对象和语句可在同一事务；不同权威事务范围不能假装一个事务，同域受信参与者可共享 Tx |
| F／S | flush／sync 方法调用，分别说明具体语义 | flush 可能只排空内存队列；sync 的操作系统、文件系统和硬件保证仍需测试 |
| H／K | 冷读历史记录数／该答复流式更新回调数 | 暖请求不必重读 H；K 不等于模型 token 数 |

存储页缓存、WAL、索引触发器、数据库配置、同步复制、日志及对象存储会改变实际 IO。次数明确的结论只覆盖下列指定路径，公式中的可选增量必须保留。

## 2. 数据对象与事实归属

| 项目 | 连续工作与输入 | 模型／工具与结果 | 主要持久化组织 |
| --- | --- | --- | --- |
| 本项目 | Session／Message（应用内部）；Task、Goal／Requirement、Command／Receipt、InputRequest／Submission | Snapshot／BrainContext、Decision／ModelCall／Proposal；OperationIntent、Operation／Attempt、Effect／Result、ConditionResult | 生产 PG、开发 SQLite 的领域事实与 jobs；正文为版本化 Content；授权／使用、预算／账单、安装及评测各有原记录 |
| Codex | Thread／Session、Turn／Step、用户输入 | ResponseItem、EventMsg、工具调用／输出、TurnContext、模型用量 | rollout JSONL；thread-history SQLite 是历史投影；thread 元数据、goal／memory 工作状态按各自路径保存 |
| Pi 经典 CLI | AgentSession、SessionManager、SessionEntry 的父链／分支、Message | user／assistant／toolResult 消息、ToolCall、CompactionEntry、模型／思考配置 entry | 内存树投影＋v3 JSONL；AgentHarness v4 和 pi-durable 是另外的运行／存储合同，不混计 |
| DeepSeek Harness | SessionMetadata、SessionEvent、turn、model surface | message、llm request／response、tool call／result、配置与上下文更新；Goal／Schedule 可选 | session 内存事件＋JSONL 存储、checkpoint policy；模型 surface 从日志形成，插件可额外记录发送与确认 |
| Prime Agent | SessionEngine、SessionEntry、用户／助手历史、worker command／queue | 模型步骤、工具／RLM cell、上下文压缩记录；GoalDriver、子会话 ledger 和 refinement 可选 | 会话 JSONL；command journal、worker queue、goal／cron／harness 等独立状态文件各有提交路径 |
| Crush | Session、Message、ContentPart、内存 RunID／提交队列 | assistant 内容／推理／工具调用、tool result、用量和费用 | SQLite session/message 及相关记录；流式消息更新与会话聚合会产生 UPDATE；UI pubsub 是呈现通道 |

对象定义和存储限制分别见 [Codex](../codex/README.md)、[Pi](../pi/README.md)、[DeepSeek](../deepseek-harness/README.md)、[Prime](../prime-agent/README.md)、[Crush](../crush/README.md)。这些名字不是逐一对应关系：例如上游一个 Turn 通常包含多个模型请求，不能等同于本项目只允许零／一次模型请求的 Decision。

本项目还有按功能触发的对象组：Grant／Use 与结算、MemoryRevision／ExtractionCandidate 与来源持有者、Delegation／子 Task 与额度、InstallLock／Activation、Candidate／EvaluationPlan／Run／Report／Approval。普通 A／B 请求不会创建所有这些对象；没有跨任务保存就不创建 Memory，没有协作就不创建 Delegation，没有软件发布就不创建 EvaluationPlan。准确 Content 引用可在多个对象之间复用，引用条数不等于正文副本数。

## 3. 暖请求的参考实现计数

各项目的源码路径与公式见本节完成的逐项追踪；表格不把逻辑记录与 SQL／sync 横向相加。

所有 A／B 都分别消费 1／2 份模型输入。暖历史“不重读文件”只说明持久存储访问，仍可能遍历、复制、裁剪和编码内存历史；因此还要记录每轮实际上下文字节。下面的 0 次历史正文读取不包括租约文件、配置、技能、工具目标或后台工作。

| 指定路径 | 暖请求的读取 | A 的历史／核心写入 | B 的历史／核心写入 | 持久化边界 |
| --- | --- | --- | --- | --- |
| Codex，本地默认 Paginated | 模型历史主要从内存取得；投影读取游标与日志新增后缀，元数据另计 | 条件示例 9 条 rollout 记录，另加投影写 | 条件示例 14 条 rollout 记录，另加投影写 | JSONL 行 write_all＋flush 后更新 SQLite 投影；普通 JSONL 追加无显式 fsync。总行数与投影事务分别计 |
| Pi，经典 v3 CLI | 暖历史正文重读 0 次，读内存消息树 | 2 条 message／2 次 appendFileSync | 4 条 message／4 次 appendFileSync | 只在完整 message_end 保存；appendFileSync 不等于显式 fsync |
| DeepSeek，原 sdk-minimal，小文本日志扩展接受 | 暖历史正文重读 0 次；每非空批写有文件 stat | 9 条事件（内核 8＋日志接受 1） | 15 条事件（内核 13＋日志接受 2） | 200ms 有界缓冲；每个非空写入批次 writeFile＋sync。该 profile 无语义 checkpoint，最终回复时尾部可能未落盘 |
| Prime，稳态基础消息路径 | 已载入 Session／cache 复用，不为每条 message 重读整文件 | 2 条 message | 4 条 message | 每条 append_cached 执行 write_all、flush、sync_data；条件状态／快照等另计 |
| Crush，直接 Coordinator→Run 核心路径 | A：5 个显式 SELECT；B：7 个，客户端查询另计 | 2 个 message INSERT＋1 个 session UPDATE＋U_A 个 message UPDATE | 4 个 message INSERT＋2 个 session UPDATE＋U_B 个 message UPDATE；普通 view 另加 1 次读取登记 | 默认 33ms 合并 message 更新；SQL 次数取决于 flush 和终结回调，索引／触发器／WAL 另计 |
| 本项目，当前设计 | 逐轮读取 Task、内容及当前资格，未确定 SQL 映射 | Brain 域 4 个基本持久阶段，另有 Task／内容／权限等 | Brain 域 8 个基本阶段，Executor 域 3 个基本阶段及适用入口，另有其他责任 | 各域本地事务与正文先耐久后引用；可共库合并，不能作为已测总事务数 |

### Pi 与 Prime：记录相同，确认点不同

两者基础消息数都是 `1 + M + T`：1 条用户消息、M 条完整助手消息、T 条工具结果；B 的工具调用包含在第一条助手消息内。流式 message update 不逐片追加该会话历史。差别在于 Prime 基础追加路径显式 sync_data，Pi 经典路径没有显式 fsync；附加 usage、配置、git 状态或宿主工作记录仍按实际触发另算。[Pi／Crush 追踪](io/pi-crush.md)、[Codex／Prime 追踪](io/codex-prime.md)

### DeepSeek：同一个项目也有不同 checkpoint 配置

无额外事件贡献时 `E_core = 5 + 3M + 2T`，A=8、B=13；原 sdk-minimal 的官方日志扩展若每次成功接受一个可装入请求的事件前缀，则再加 M，得到 9／15。关闭扩展或前缀不可装入时，不能仍套用 9／15。其事件包含 inbox、turn／step、消息及工具边界，条数自然多于只保存 message 的路径。[源码追踪与逐条推导](io/deepseek.md#4-ab-的逐条追加与变量公式)

原 sdk-minimal 没挂 checkpoint；base 支撑的 sdk 则挂载它，且默认 zstd，与 minimal 的纯文本 JSONL 不同。显式 checkpoint 组合的语义 flush 为 `2M + T`，A=2、B=5；若再主动等待 turn 结束后的最终 flush，各加 1。每次 flush 可能遇到空缓冲，因此不是相同次数的 sync。统计所有尾部最终耐久后，实际非空批次为 `n_batch`，每批一次文件 sync；无故障时 `1 ≤ n_batch ≤ E_actual`，具体值由时序决定。[持久批次与同步边界](io/deepseek.md#5-checkpointflush批写与-fsync-的公式)

### Crush：流式输出会影响数据库写次数

`U_A/U_B` 是实际落到数据库的 message UPDATE 数，不是 token 数。无 reasoning 的指定回调路径中，A 的终结更新基数为 1；B 的四次基础更新由 ToolInputStart、完整 ToolCall（OnToolCall）、第一步 Finish 和最终步 Finish 触发，toolResult 本身则是 INSERT。X 是增量计时刷新及刷新期间新状态导致的实际额外成功 UPDATE 数，核心 DML 为 A=`4+X`、B=`10+X`。B 的普通文本 view 还执行一次 read_files upsert，所以该完整指定路径为 7 次 SELECT、`11+X` 次 DML，合计 `18+X` 次显式 SQL。没有 ToolInputStart 回调时减 1；TUI 提交前的待登记文件、channel 变化等另加。SQL 触发器还会更新 session 聚合，但不另算应用发出的 SQL 调用，见[SQL 与触发器展开](io/pi-crush.md#42-公式与-b-的额外读取登记)。

### Codex：不能只数用户和助手消息

rollout 同时保存模型 ResponseItem、经过策略筛选的 EventMsg，以及上下文和用量记录；普通流式 delta 并非全部保存。当前 LocalThreadStore 将新建会话默认设为 Paginated，不能仅看到 trait 的 Legacy 默认值就忽略实现覆盖。[实际默认实现](https://github.com/openai/codex/blob/d4a475adda850d80b6149c76454de94e0cf4fd51/codex-rs/thread-store/src/local/mod.rs#L478-L481)

作为另一条明确配置的示例：严格限定 Legacy、最终单个纯文本段、usage 完整、1 条 TurnContext、无 WorldState／额外上下文／reasoning／commentary，且 B 使用终结事件被过滤的普通命令工具时，A=9 行、B=13 行。工具若产生被保留的终结事件，或增加模型／上下文项，需要另加。A 的 2 个或 B 的 4 个核心模型项只是组成部分，详细公式及持久过滤见[源码追踪](io/codex-prime.md#2-逻辑追加数核心记录与附加项分开)。

相同限定下，Paginated 为 A=9、B=14：B 额外保留普通命令的 CommandExecution ItemCompleted。主表采用这一条件示例；模型多输出一个完整 item、额外 usage／呈现事件或上下文变化，均需按实际类型调整，不能把 9／14 推广为所有提示的常数。

这个 Legacy 路径的 thread_history 投影提交为 0，不表示全部 SQLite 写入为 0；threads 元数据等另计。Paginated 模式在 JSONL 可读后物化新增后缀，按非空批次产生投影事务；不应以 message 数推导其 SQL 次数。Prime 的 daemon 路径还会读取租约 owner 文件，默认 ipython cell 可能安排延迟 kernel 快照；这些伴随 IO 都没有被基础 2／4 条消息公式覆盖。

### 冷打开 C 与暖路径不能混计

| 项目 | 冷打开／恢复读取 | 正常暖请求的区别 |
| --- | --- | --- |
| Codex | 读取 rollout 并重建历史与上下文；分页历史模式可以使用 SQLite 投影，投影缺失／修复另计 | 不为每个输出 delta 重扫整个 rollout；按实际 history mode 核对伴随状态访问 |
| Pi 经典 CLI | 读 JSONL、解析条目及父链，构建当前分支；分支／压缩改变模型视图 | 暖请求读取内存消息树；新会话首次持久化可能重写已缓冲条目，不能代入稳定 append 次数 |
| DeepSeek | 查 generation／租约，稳定读取当前日志、解析并重建投影；新 Agent 可另写 resume header | 同一暖 Agent 无变化时不重扫正文；每非空批写的文件 stat 仍是元数据访问 |
| Prime | 读取已有 JSONL 并恢复 Session 状态，worker resume 可能追加状态行；快照／kernel 恢复另计 | 暖引擎及 SessionStore cache 复用历史，基础 message append 与恢复文件读写分开 |
| Crush | 从 SQLite 取得 Session 与 messages，客户端还会读列表／视图 | Run 本身仍查会话与历史；“暖会话”不代表每个实现都已把全部历史只放在内存 |

以上是路径和数据规模关系，不是一次物理磁盘读取的承诺。历史长度 H、文件修复、数据库页缓存和具体宿主均会改变读取字节与耗时；恢复新发送还受各项目自己的合同约束。

## 4. 本项目：能从设计确定哪些读写

本项目仍是方案，不能诚实地填成“每次请求 R 次 SQL、W 次 IO”。可以确定的是领域持久阶段，以及哪些阶段可以同库合并。以下按当前设计分域列出，不是最终总事务数。

| 负责方／对象 | A／B 中触发频次 | 读写及可合并范围 |
| --- | --- | --- |
| 应用 Session／Message／原命令 | 输入各 1 次；最终答复关联各 1 次 | 保存原提交及任务引用，正文只引用 Content；跨 owner 交接需要原命令恢复。同进程共库可减少往返，Session 不另存一份任务状态 |
| Orchestrator Task／目标／预算／回执／首 job | 新目标各接纳 1 次 | 一次接纳事务共同保存，不按对象数拆事务；重投读原回执 |
| Snapshot／Decision 派发 | A 1 轮，B 2 轮 | 每轮按当前 Task 和获准内容组装，条件提交固定输入、原 Decision、预留与派发责任。已有当前投影可复用，不每轮读全部历史 |
| Brain Decision／ModelCall | A 1 个，B 2 个 | 每个模型 Decision 有接纳、调用准备、send_started、终态四个明确持久阶段；本地 Brain 接纳可与 Orchestrator 的对应接纳共享受限 Tx，准备与发送门禁仍是两个提交点 |
| Brain 内容发布 | 有新生成正文时逐个 Decision 触发 | 另有 publication 身份准备、正文保存与内容引用提交；准确内容数量、大小和 owner 放置决定附加读写，不能遗漏也不能按 token 计次 |
| Orchestrator 提案消费 | A 1 次，B 2 次 | 在 Task 事务内消费原 Decision；B 的首次消费可同时准入 OperationIntent、预留和 dispatch job，不为每个名词独立提交 |
| Executor Operation／Attempt | A 0 个，B 1 个操作和 1 次尝试 | 接纳、Attempt 准备、原结果归并是三个明确持久阶段；具体资源入口若需独立 StartBarrier／占用，另有其事务。结果、费用、下一 job 与 Finish 可共同提交 |
| Orchestrator 工具事实归并 | A 0 次，B 1 次 | 读原 Operation／版本并归并，在允许的同库事务中同时创建下一推进责任；不能把工具推送当作已归并事实 |
| Grant／Use／结算与当前批准 | 按实际用途、来源和费用单位触发 | grant.use 是有写入的裁决，不能计成普通缓存查询；同域裁决可共 Tx，独立 Grant owner 要保留原使用与恢复。当前资格也不能仅用历史 Receipt 代替 |
| Task 核验／Result／费用收尾 | 按目标与实际账单触发 | 完成证据、目标覆盖及未决责任满足后才提交 Result；最终答复不是自动成功。迟到账单另增原修订，不重做模型／工具 |
| JobStore／Surface | 按领取、有限续约、正式快照触发 | 领取有事务；Raise 与领域事实同事务，Finish 与结果尽量同事务，不每个状态空建 job。临时流式输出不逐片写库 |

依据：[任务事务与逻辑表](https://github.com/ruipengliu/lerna/blob/1b647dc970317173f349296e47efd41a4cf3a23c/docs/architecture/.draft/orchestrator/implementation.md#2-持久表与索引)、[Brain 接纳与发送](https://github.com/ruipengliu/lerna/blob/1b647dc970317173f349296e47efd41a4cf3a23c/docs/architecture/.draft/brain/implementation.md#32-接纳事务)、[正文发布](https://github.com/ruipengliu/lerna/blob/1b647dc970317173f349296e47efd41a4cf3a23c/docs/architecture/.draft/brain/implementation.md#generated-content)、[执行持久边界](https://github.com/ruipengliu/lerna/blob/1b647dc970317173f349296e47efd41a4cf3a23c/docs/architecture/.draft/execution/implementation.md#reliable-work-integration)、[原使用与结算](https://github.com/ruipengliu/lerna/blob/1b647dc970317173f349296e47efd41a4cf3a23c/docs/architecture/.draft/security/implementation.md#5-一次使用与并发裁决)、[公共工作计量](https://github.com/ruipengliu/lerna/blob/1b647dc970317173f349296e47efd41a4cf3a23c/docs/architecture/.draft/reliable-work.md#10-观测与成本计量)。

因此，单看 Brain 域，A 有 4 个、B 有 8 个基本持久阶段，另加实际内容发布等责任；B 的 Executor 再有 3 个基本阶段及适用的资源入口。它们说明本项目比简单追加消息承担更多确认工作，**不能相加后宣称已经得到请求总 Tx／SQL／fsync 数**。Task、内容、权限、领取、结算及合法合并都尚未完成物理映射。

读侧同理：每轮要取得当前 Task／控制、准确输入、来源及当前资格，工具前还要取得准确 Capability／Binding；这些是逻辑依赖，可能通过有界批读、同库 join 或当前投影一次取得。`query`／`view.open` 等固定集合入口还可能写临时查询状态；不能凭方法名称以为全是只读。初版静态小目录无需每轮 search／describe，普通对话也不必每轮检索长期记忆或运行评测。

## 5. 流程简化结论

本项目存在首版范围过宽和读写放大的风险，但目前没有运行数据证明它已经出现性能瓶颈。保留 Task／Decision／Operation 的不同身份是为了恢复、计费与外部效果；机械地把它们实现成多套服务，才会增加无必要的交接。

| 应保留的行为 | 应收敛的实现 |
| --- | --- |
| Session 连续对话与 Task 独立目标 | Session 只存消息与任务引用；不新增持久 Turn／Run、会话预算、独立会话执行器 |
| 原命令、可能发送、已观察效果可恢复 | 同次接纳共事务，结果与下一责任共事务；不是每个逻辑记录都单独 commit |
| 实际输入、来源和结果可解释 | 清单附原 Decision／Operation；原字节单份 Content，UI／模型视图按需投影 |
| 当前权限、控制及准确绑定 | 合并同库读取并复用仍有效的有界依据；不省略真实出站检查，也不无条件重复全目录与全部历史读取 |
| 可丢流式呈现与可查询正式结果 | 有限内存缓冲与合并推送，禁止逐 token 建 job 或持久 SurfaceSnapshot |
| 可独立替换与生产恢复目标 | 初版一个进程及少量 facade；按真实隔离、归属、伸缩边界拆分，不按九模块部署九套服务 |
| 经验、协作、动态扩展和程序工具 | 按需启用，各自证据充分再开放；没有使用时不进入请求热路径 |

落实位置：[Session 与 Task](https://github.com/ruipengliu/lerna/blob/1b647dc970317173f349296e47efd41a4cf3a23c/docs/architecture/.draft/interaction/session-and-task.md)、[最小工程装配](https://github.com/ruipengliu/lerna/blob/1b647dc970317173f349296e47efd41a4cf3a23c/docs/architecture/.draft/engineering.md#minimum-profile)、[新增故障与计量向量](https://github.com/ruipengliu/lerna/blob/1b647dc970317173f349296e47efd41a4cf3a23c/docs/architecture/.draft/validation/harness-scenarios.md)。首个性能原型应只贯通 A／B 及文件保存读回，先验证实际提交数量和恢复正确性，再决定还需不需要拆更多边界。

## 6. 如何获得可横向使用的实测数据

为每个指定版本固定模型回放、输入和工具输出，在存储接口、SQL driver、文件 append／flush／sync 和实际网络出口计数；固定 stream 回调分块、缓存与会话历史长度。分别测暖 A／B、冷 C、首次会话和崩溃恢复，不把初始化与背景作业悄悄排除或混入。

每组同时报告 E、R/W、Tx、F/S、读写字节、WAL、数据库与文件配置、返回答复至耐久完成的间隔，以及成功／失败／unknown。CPU、时延、真实模型费用和同步复制另记。要比较强耐久配置，应让各项目在相同确认点停止计时；追加到内存与 sync 完成不能作为同一种“保存成功”。本轮未执行这些运行实验。
