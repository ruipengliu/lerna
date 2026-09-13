# Harness 持久运行机制与存储选项

核对日期：2026-09-09。状态：研究证据，未选定持久运行机制。

对应目标：[项目目标基线](../harness-project-goals.md)中的任务运行与端云协同、工具调用与设备操作、授权与用户控制。沿用 [Harness 领域词汇](../../CONTEXT.md)。本报告补充[既有 Harness 调研](agent-harness-landscape-2026-09.md)，只比较 LangGraph、Temporal 与自建任务日志／检查点机制。

整合说明：研究期间用户已明确核心采用 Go，开发期使用 SQLite 等无需独立外部服务的组件。下文跨语言框架比较保留作机制与可选 Adapter 的参考，不表示核心语言或开发期基础设施仍待选择。

## 1. 对后续决策最有影响的结论

1. **三种恢复不能混为一谈：恢复任务状态、再次交付调用、确认外部动作结果。** LangGraph 提供图检查点；Temporal 通过事件历史恢复 Workflow；数据库提供事务和数据库恢复。这些机制都不直接证明一次手机点击或外部写入只发生一次。下面的官方机制支持这一判断；外部动作仍需执行系统提供幂等或核对能力。
2. **必须决定谁是任务状态的唯一权威。** 这是 Harness 的设计责任：可以由内核状态机持有权威状态，也可以由运行引擎承担状态机实现、内核提供契约与策略。不能让图状态、Workflow 状态、业务任务表各自独立决定同一任务的终态。
3. **本地独立运行应按部署单元评价。** LangGraph 库配本地持久化可以在本机执行；Temporal Worker 要与 Temporal Service 协作。Temporal 也能本地部署，但官方将嵌入式 SQLite 部署限定为开发和测试，不能直接据此承诺生产级轻量离线节点。[LangGraph 检查点](https://docs.langchain.com/oss/python/langgraph/checkpointers)、[Temporal 嵌入式服务](https://docs.temporal.io/self-hosted-guide/embedded-server)
4. **没有一种方案免除未知动作结果处理。** 推荐把“结果待核对”作为动作层显式状态；不能因为通信超时就自动推断动作失败。这是基于下述失败窗口的架构建议，不是框架提供的统一状态名。

## 2. LangGraph：图执行恢复机制

**已核实事实。** 检查点按 `thread_id` 管理图状态，并保留步骤与中间写入。持久化模式包括 `exit`、`async`、`sync`：分别在退出时写入、与下一步并行写入、在下一步前完成写入。选用 `sync` 只增强检查点落盘时序；它不是外部服务事务。Python 提供独立安装的 SQLite 和 PostgreSQL 检查点包，以及对应异步实现。官方将 SQLite 描述为本地工作流／实验用途，将 PostgreSQL 描述为生产用途。[Checkpointers](https://docs.langchain.com/oss/python/langgraph/checkpointers)

`interrupt()` 暂停图并等待外部输入，恢复需使用同一个 `thread_id`。**恢复会从包含中断的节点起点重新运行，中断前的代码可能再次执行。** 因此暂停审批不等于任意位置的进程快照，也不等于取消已发出的设备动作。[Interrupts](https://docs.langchain.com/oss/python/langgraph/interrupts)

存在官方 JavaScript／TypeScript 实现，可直接安装库并构建图。不能据此推断其持久化插件、序列化格式或细节行为与 Python 完全相同；跨语言检查点迁移未在本次核对中得到保证。[JavaScript LangGraph overview](https://docs.langchain.com/oss/javascript/langgraph/overview)

**对本项目的解释与建议。** 可以把 LangGraph 作为大脑系统的规划与上下文执行实现，让 Harness 内核负责跨节点任务生命周期；也可以研究用图表示整个任务。后者必须先验证任务终态、并发恢复、取消、版本升级与外部 Agent 接入如何保持内核契约。库本身不要求托管云服务；“离线可运行”仍取决于模型、工具、检查点与授权是否全部在本地可用。不要把库的最小依赖与 Agent Server 的部署依赖混用。

## 3. Temporal：Workflow 与 Activity 的职责

**已核实事实。** Workflow 的恢复依靠事件历史重放，运行代码生成的 Commands 要与已记录历史相容。Worker 将 Commands 交给 Temporal Service，由 Service 记录相应事件。它并不是将 Python 或 JavaScript 进程内存原样恢复。[Workflow Execution](https://docs.temporal.io/workflow-execution)

Workflow 代码须遵守确定性约束；网络调用、LLM 调用等不确定操作应放入 Activity。Activity 默认有重试策略，而整个 Workflow Execution 默认不配置重试策略；两者不能合称“失败就重跑任务”。重试策略需要与任务预算、期限和动作类型一并配置。[Retry policies](https://docs.temporal.io/encyclopedia/retry-policies)

Activity 在 Worker 执行，返回后由 Worker 把完成结果发送到 Service 并进入历史。官方建议 Activity 幂等；一次重试默认从 Activity 初始状态开始，也可以由应用利用心跳详情保存进度。因此存在“外部操作完成，完成结果尚未写入历史”的窗口。[Activities](https://docs.temporal.io/activities)

取消通常需要执行代码配合。TypeScript 文档明确：普通 Activity 接收取消请求需发送 Heartbeat 并配置 Heartbeat Timeout；Local Activity 不要求同样的心跳条件。请求取消不是撤销已经发生的副作用。[TypeScript cancellation](https://docs.temporal.io/develop/typescript/workflows/cancellation)

官方 Workflow 文档链接 Python、TypeScript、Go、Java、.NET 等 SDK 的执行与重放 API。存在 SDK 不意味着这些语言可共享进程内对象或任意互换 Workflow 代码；需要稳定的数据契约与兼容部署。[Workflow Execution](https://docs.temporal.io/workflow-execution)

**部署事实。** 本地可运行 Temporal Service，也可将服务嵌入 Go 程序；SQLite 嵌入方式官方仅建议开发测试，生产持久化建议 MySQL、PostgreSQL 或 Cassandra。Service 内含 frontend、history、matching、worker 等服务角色；“嵌入”没有消除这些角色。[Embedded server](https://docs.temporal.io/self-hosted-guide/embedded-server)

Visibility 是 Temporal Service 的所需存储能力，用于列举和筛选 Workflow。当前文档列出 PostgreSQL 等 SQL 后端；其中 PostgreSQL 12+ 的 Advanced Visibility 从 Temporal Server 1.20 起支持。因此 Elasticsearch 不是所有部署的必需组件；需按所选 Server 版本配置 Persistence 与 Visibility。[Visibility setup](https://docs.temporal.io/self-hosted-guide/visibility)

**对本项目的解释与建议。** 生产参考部署至少需要应用 Worker、Temporal Service、受支持的持久化与 Visibility 配置；选择 PostgreSQL 时可评估由同一数据库服务承载不同存储逻辑。断开远程 Service 的 Worker 不能被视为一个独立调度系统。已在运行的 Activity 可能仍产生动作；其记录、心跳、取消与后续调度都受连接影响。若需要设备离线自主执行，应明确本地任务权威与云端任务之间的委派关系，不应假设 Worker 会自动合并端云分叉历史。

## 4. 自建任务日志与检查点：数据库提供什么

| 项目 | SQLite | PostgreSQL |
|---|---|---|
| 已核实的持久化基础 | 支持事务日志与 WAL；WAL 允许读写并行，但同一时刻只有一个写者 | WAL 在数据页写入前记录变更，支持崩溃后 REDO 恢复 |
| 并发边界 | WAL 要求访问数据库的进程位于同一主机，不适用于网络文件系统 | 有多个事务隔离级别；默认 Read Committed，Serializable 可能报序列化失败，应用需重试整个事务 |
| 本项目部署推论 | 适合评估为运行节点的本地任务日志；跨端同步必须经协议实现 | 适合评估为多进程服务端任务记录；运行节点无法连接远程数据库时，不能继续依赖该库提交状态 |
| 数据库不会替项目实现的部分 | 任务状态机、执行器调度、租约、去重、恢复、取消、授权、同步 | 同左；数据库的事务重试也不是重试外部动作的授权 |

事实来源：[SQLite WAL](https://www.sqlite.org/wal.html)、[PostgreSQL WAL](https://www.postgresql.org/docs/current/wal-intro.html)、[PostgreSQL 事务隔离](https://www.postgresql.org/docs/current/transaction-iso.html)。本次 PostgreSQL `current` 文档解析到 18；这是阅读版本，不是项目版本选择。

**建议的最小自建边界。** 在同一数据库事务中更新任务状态、追加任务事件、登记待发送消息；执行节点先持久化动作接收和去重信息，再调用驱动。发送成功与对端持久化确认分开记录。运行中的动作使用稳定动作标识关联意图、尝试和观察结果。此设计还需解决所有权转移与旧执行者失效，不能只靠一个 `running` 字段防止双执行。

本节建议属于自建工作量清单，并非已证明正确的实现。数据库 WAL 是数据库内部恢复日志，与需要长期查询、迁移和审计的 Harness 任务事件不是同一类交付对象。

## 5. 放在同一决策框架内比较

以下是基于前述事实的设计比较，而非厂商兼容性承诺。

| 决策面 | LangGraph 路线 | Temporal 路线 | 自建路线 |
|---|---|---|---|
| 任务状态唯一所有者 | 明确图只是大脑内部状态，还是整个任务状态的权威实现 | 明确 Workflow 是权威实现，还是内核委派的子任务；业务表可作投影 | 内核状态机直接拥有任务状态，需自行保障并发与版本 |
| 调用重试 | 节点恢复可能重新执行代码，执行适配器承担副作用契约 | Activity 重试成熟，但必须按幂等与核对能力配置 | 自行实现重试、退避、预算、去重与故障分类 |
| 取消 | 中断提供暂停点；动作取消还需驱动与内核协作 | 内建协作式取消；Activity 心跳与处理代码影响响应 | 自行设计取消请求、确认、完成竞态与副作用报告 |
| 断连 | 本地依赖齐备可继续；没有自动跨端任务所有权协调结论 | Service 可恢复调度；远程 Worker 失联不是独立离线调度 | 可按目标精确定义本地续跑，但需自行处理重连、分叉与授权 |
| 外部结果不明 | 独立核对，避免直接重放有副作用的节点 | 独立核对，避免重试已执行但未回报的 Activity | 独立核对；本地事务同样不能覆盖手机或第三方服务 |
| 主要代价 | 框架状态到通用任务契约的映射、并发恢复与升级验证 | Service 运维、确定性约束、历史兼容、端侧部署边界 | 状态机与故障恢复正确性、调度器与运维全部由项目维护 |

一种可讨论的组合是“内核持有任务契约，LangGraph 作为可替换大脑实现”；另一种是“Temporal 实现内核任务运行，端侧独立任务通过契约受托执行”。这些是候选架构，尚未得到项目采纳，也不意味着必须同时引入两种框架。

## 6. 必须通过原型或故障注入回答的问题

| 验证题 | 最低可观察结果 |
|---|---|
| 谁能最终提交同一任务状态？ | 两个进程同时恢复、旧节点复活、租约过期时，只有当前所有者能推进权威状态；过期执行结果被识别 |
| 外部动作执行后立刻杀进程，随后重启 | 动作标识和记录保留；可核对动作回到确认状态，不可核对动作保留未知结果；不能默认再次点击 |
| 完成回报丢失后同时发起取消与恢复 | 分别记录取消请求、执行端确认、已发生结果，任务终态不掩盖副作用 |
| 云端失联与本地进程重启同时发生 | 本地能力足够且授权有效的任务能恢复；依赖云端的步骤等待；重连不重复执行 |
| 更换大脑或升级运行代码后恢复旧任务 | 数据与代码版本有明确相容范围；不相容任务不会静默继续或丢失状态 |
| SQLite／PostgreSQL 实现是否满足同一任务契约？ | 使用同一故障用例；比较提交时序、锁竞争、事件完整性、恢复延迟与数据增长 |
| 默认部署是否足够轻量？ | 固定版本与设备测量包体、内存、启动时间、持久化耗时和断连恢复；禁止只引用框架宣传数据 |

尚未核实：各候选版本在目标手机环境的可部署性、Python 与 TypeScript 检查点互换、节点间历史迁移、长期保留与升级成本、驱动的真实取消／核对能力。公开机制文档不能代替这些项目级验证。

## 7. 版本与证据使用说明

本报告使用 12 个官方页面，事实在正文就近引用。LangGraph 的旧 `durable-execution` 地址本次重定向到 `persistence`，恢复与持久化模式细节实际在 `checkpointers`；实施时需固定包版本并复核 API，不把滚动文档视为版本锁。Temporal SQL Visibility 的最低版本和 SQLite 嵌入式部署限制是文档当前陈述；不得把示例部署当作所有平台的最低依赖，也不得把 Temporal 对 SQLite 的生产限制推广为 SQLite 本身不能用于生产。

本报告没有运行框架、部署数据库或模拟故障。它解决机制与部署约束的事实调查，任务状态权威、默认运行引擎、参考部署和量化阈值仍需在后续决策票中确定。
