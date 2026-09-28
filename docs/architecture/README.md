# Harness 技术方案

本方案设计一套面向个人智能应用的开源 Harness：接纳用户目标，组织大脑、记忆和执行能力，在授权与预算内持续推进任务，并保留足以解释结果和中断后继续处理的事实。默认生产形态是面向多用户的分布式服务，同一组契约支持端侧组件和端云混合装配；完整单体用于开发与调试。

本目录是独立的设计基线。需求、术语、规则和验收方法均在目录内定义。技术方案、已冻结范围的机器契约及示例共同约束参考实现；未纳入线格式的能力明确保留为设计接口，不能宣称已经互操作。内核、SDK、默认组件和运行验收仍须实现，具体状态见[交付审查](review.md)。

首次阅读先用本页建立分工，再沿[贯穿场景](walkthrough.md)看一次任务从正文形成到执行、核验和异常收尾。实现入口集中在本页第 4、5 节；[技术总览](technical-overview.md)补充抽象依据和全景图导读，可在需要理解建模理由时查阅。

## 1. 要解决的核心问题

一个用户可能让 Agent 核实信息、保存文档，再在手机上执行操作。模型可以提出错误步骤，工具可能已经产生效果却丢失答复，端点也可能在执行中失联。Harness 必须把这几件事分开处理：是否接受了目标、是否获准行动、行动发生了什么、结果达到什么质量、失败后由谁继续。

生产方案把 WSS 连接接入层、Orchestrator 应用、工作池和隔离执行宿主分开部署，使连接、任务推进和外部执行能够分别扩容与恢复。模块边界不等于服务或数据库边界：需要共同裁决的事实使用同一提交域，跨域交接保存原命令、回执与继续处理的责任。

生产基线为单地域三个可用区，优先采用托管 PostgreSQL、对象存储及连接池。单区失效时已确认账本的目标为 RPO=0，原 Orchestrator 控制与查询恢复 RTO≤60 秒；目标文件字节和整地域灾备另有边界。这些保证成立的条件、恢复顺序和负载预算集中在[生产部署](deployment-production.md)，基础设施选择归[存储与中间件](storage-and-middleware.md)。

方案以已确认规模与恢复目标为约束，先采用少量成熟组件，再用稳态、故障剩余容量及积压恢复的测量确定配置。上述目标仍需实际运行验收。

## 2. 系统分工与事实归属

图表示逻辑职责，方框不代表独立进程。实线是任务处理调用，虚线是控制或装配关系；调用返回仍沿原关系传递。

```mermaid
flowchart TB
    U[用户与应用] --> I[交互适配器]
    I --> H[任务编排器 · Orchestrator]
    H --> B[大脑接口<br/>规划与下一步提案]
    H --> M[记忆接口<br/>检索、来源与长期记忆]
    H --> E[执行接口<br/>API、设备与效果核对]
    H --> C[Agent 协作适配器]
    C --> A[其他 Agent]
    E --> X[外部系统与模拟手机]
    H -.授权及额度.-> P[权限与资源规则]
    E -.启动与接管检查.-> P
    R[宿主与扩展管理] -.装配、隔离与版本.-> H
    O[观测、评测与改进] -.评测及获准发布.-> R
```

**Orchestrator（任务编排器）**负责组织任务推进，保存目标、控制、预算和完成决定。每个任务通过 `orchestrator_id` 归属一个固定的逻辑负责方，可由本地宿主或共享权威数据库的云服务集群承载。该标识不指向某个工作进程；实例重启或更换后任务归属保持不变。同一用户的不同任务可以属于不同 Orchestrator。

| 事实 | 唯一裁决者 | 调用方取得的保证 |
| --- | --- | --- |
| 目标、控制、操作意图、任务预算、完成结果 | Orchestrator | 状态和下一项持久工作共同提交；重启可继续 |
| 模型请求、提案和调用费用记录 | 大脑适配器 | 提案绑定输入快照；提案不直接执行行动 |
| 实际启动、目标系统效果、设备占用 | Executor 及资源负责方 | 请求到达与实际效果分别记录；未知效果有继续核对的入口 |
| 记忆版本、来源、内容和副本清理 | 记忆或内容负责方 | 引用可定位准确版本；读取、同步和保存分别获准 |
| 许可、撤销、单次消费、离线额度 | 授权负责方 | 许可在指定范围内生效；离线撤销受已约定窗口限制 |
| 输入是否消费、外部子任务是否接纳、版本是否激活 | 对应业务处理方 | 各自有持久回执；界面展示或网络响应不能代替业务决定 |

“负责方”是一项裁决职责，并不要求新增服务。同一 Orchestrator 分区内需要共同裁决的事实共享提交域，只有独立事实归属、信任隔离或伸缩需求需要时才拆开。字段与接口的权威位置见[契约索引](contracts/README.md#domains)。

## 3. 决定方案形状的选择

| 选择与推荐依据 | 竞争方案及代价 | 改变选择的条件 |
| --- | --- | --- |
| 每任务固定 Orchestrator，同用户可有多个 Orchestrator；把唯一写者限制在一个任务，而不是整个用户 | 自动跨端接管能提高同一任务可用性，但分区时必须隔离旧写者、核对在途效果并转移权限与预算；当前由失联任务承担等待 | 用户确实需要同一任务离线接管，且原端隔离与效果核对已有可验证实现 |
| 同提交域共同事务，跨域按业务命令持久交接；生产按连接、应用、工作池及执行隔离部署 | 按模块拆成九套服务和数据库会增加无必要的交接。少量生产角色保持独立故障与伸缩能力，由原分区保存业务事实和 jobs | 新的事实归属、信任或伸缩边界足以补偿额外交接成本时，再拆提交域 |
| 大脑按快照给出一轮提案，Orchestrator 掌握行动循环 | 大脑自管长循环也可逐次申请准入，但会形成两个恢复进度；当前交接开销由多轮 GUI 和模型任务承担 | 测量表明往返主导延迟，且可提供相同控制检查与可恢复检查点的批处理实现 |
| 完成依据分为客观验证、质量评估、用户验收 | 每类目标预装专用工作流会提高接入成本；只让模型自报完成又无法解释效果。应用需展示实际依据 | 特定业务愿意以目标类型受限换取全部可机械验证的结果 |
| Go 核心；端云 WSS，服务间 gRPC；同进程直接调用 | WSS 支持双向交互和主动推送，接入层承担长连接资源与恢复成本；Protobuf 外壳复用严格 JSON，避免两套字段权威但保留编码开销 | 只有测量证明编码或传输成为瓶颈才更换绑定；原命令、业务回执和当前权限规则保持 |
| 共享资源由资源负责方裁决，跨 Orchestrator 预算预分配 | 任意端离线消费共享余额会透支或重复消费。用户承担预分配额度暂不可回收、远端撤权延迟 | 明确接受更弱的额度保证，或有可用的共享在线裁决能力 |
| 默认交付审核插件；不可信代码按平台验证隔离后开放 | 直接加载任意代码可减少接入阻力，却无法靠接口检查约束宿主权限；任意原生沙箱又明显增加平台成本 | 某平台的文件、网络、凭证和资源隔离已经通过攻击与故障验证 |

共同选型、端云装配与宿主接口见[技术基线与宿主装配](deployment.md)；[生产部署与运行](deployment-production.md)连续说明进程分工、任务路由、连接恢复、负载推导、故障及发布；存储、连接池、工作唤醒及中间件改选条件见[存储与中间件](storage-and-middleware.md)。任何选择都不要求把业务事实解释为外部效果的全局“恰好一次”。

会改变用户可见行为与验收含义的选择集中在[设计决策与行为基线](decisions.md)：目标修订、暂停期间完成、完成依据、取消乱序、模型并发保证及统计门禁均有明确边界，不能在替换实现时静默改义。

<a id="reading-path"></a>
## 4. 连续阅读路径

跨模块处理例子在[贯穿场景](walkthrough.md#2-正常主链)连续展开；行为规则在所属模块集中定义，场景和图册链接这些规则。完成这条主线后，按负责的实现范围进入下表，无需先通读所有专题。

| 顺序 | 文档 | 阅读所得 |
| --- | --- | --- |
| 1．建立主线 | 本页 → [贯穿场景](walkthrough.md) → [任务编排器](orchestrator/README.md)及[实现](orchestrator/implementation.md) | 谁形成提案、谁准入，正文、效果和完成证据如何交接，异常后谁继续 |
| 2．落实负责的模块 | [大脑](brain/README.md)／[执行](execution/README.md)／[记忆与内容](memory/README.md)／[权限](security/README.md)／[交互](interaction/README.md)／[协作](collaboration/README.md) | 按主链的交接点查完整规则，再进入本页第 5 节对应的实现章节 |
| 3．连接独立实现 | [共同契约](contracts/README.md) → [方法索引](contracts/methods.md) → [线格式与机器资产](contracts/protocol.md) | 输入输出、成功点、错误、原身份恢复及对应构造序列 |
| 4．装配与运行 | [宿主装配](deployment.md) → [可靠接纳与持久工作框架](reliable-work.md) → [生产部署](deployment-production.md) → [存储与中间件](storage-and-middleware.md)；按需查[扩展](extensions/README.md)与[评测改进](evaluation/README.md) | 公共模板与领域处理器、事务及责任槽、生产故障边界、安装切换及隔离评测 |
| 5．交付切片 | [验收建设顺序](validation/README.md#5-建设顺序与退出条件) → [交付审查](review.md) | 首个闭环依赖、退出证据及当前实际检查范围 |

[目标与功能](goals.md)保存范围和指标，[设计决策](decisions.md)保存关键选择及改选条件；[技术总览](technical-overview.md)解释四种边界和建模依据。图形查阅使用[架构图集](diagrams/architecture-atlas.html)、[可编辑全景图](diagrams/system-panorama.drawio)及[UML 导读](uml-models.md)，不以图中容器数量决定服务数量。

任务验证沿[条件、规则与验证器生命周期](orchestrator/verification.md)阅读，再查[核验持久化](orchestrator/implementation.md#condition-storage)及[存储访问路径](orchestrator/access-paths.md)。前者集中定义完成依据、异常与证据适用性，后两者给出恢复、查询和性能验收约束；当前仍是设计规格。

第三方实现从同一方法索引进入 [Schema](contracts/schemas/protocol.schema.json)、[登记表](contracts/schemas/methods.json)及[协议序列](contracts/examples/protocol/README.md)；跨端再查[WSS](contracts/transport.md)或[gRPC](contracts/grpc.md)。声明一个方法须同时承担其查询、错误和恢复义务，参考实现表结构不属于替换要求。

### 目录与后续细化

九个模块各有独立目录，以 `README.md` 保存完整行为链，以 `implementation.md` 展开内部职责、持久记录、事务、恢复与故障验证。全局目标、决策、场景和部署位于顶层，图源在 `diagrams/`，机器契约及例子在 `contracts/`，系统验收与静态校验在 `validation/`。

细化某个模块时，先更新该目录的 `README.md`，保留职责、关键决策和完整处理链。独立机制需要展开时，再在同目录增加按主题命名的文件，并由模块入口给出阅读顺序；专属图示和示例随模块保存。共同字段、方法登记和跨模块用例继续归 `contracts/` 与 `validation/`，模块正文链接其权威定义。

<a id="detailed-design"></a>
## 5. 从主线进入详细实现

先读各模块 README 中的行为与取舍，再沿实现文档的“模块形状与依赖 → 对象流转 → 关键事务时序 → 生产约束”阅读。模块结构图表达软件依赖，流程／时序图表达运行中的对象交接，部署图表达进程及故障域；三种视角分别给出，图中节点不自动对应独立微服务。公共字段继续以同版 Schema 为准。

实现共同持久机制时先读[框架接口](reliable-work.md#interfaces)和[条件提交](reliable-work.md#completion)，再进入各模块的[接入节](reliable-work.md#integration)。框架统一参考实现的接纳与有限工作模板、逻辑 JobStore；各模块明确自己的事务参与者、责任键、成功与恢复判断，正式运行库及适配器仍待实现。

<a id="design-coverage"></a>

| 模块 | 形状与组件依赖 | 数据对象与流转 | 关键时序 | 生产约束 |
| --- | --- | --- | --- | --- |
| 任务编排器 | [入口与准入](orchestrator/implementation.md#module-shape) | [任务及工作](orchestrator/implementation.md#data-flow) | [提交与恢复](orchestrator/implementation.md#key-sequence) | [调度与热键](orchestrator/implementation.md#production) |
| 大脑 | [决策与适配器](brain/implementation.md#module-shape) | [上下文与提案](brain/implementation.md#data-flow) | [调用与归并](brain/implementation.md#key-sequence) | [并发与费用](brain/implementation.md#production) |
| 执行 | [门禁与驱动](execution/implementation.md#module-shape) | [操作与效果](execution/implementation.md#data-flow) | [发送与核对](execution/implementation.md#key-sequence) | [资源与隔离](execution/implementation.md#production) |
| 权限与隔离 | [身份与许可](security/implementation.md#module-shape) | [许可与使用](security/implementation.md#data-flow) | [裁决与消费](security/implementation.md#key-sequence) | [权威与热点](security/implementation.md#production) |
| 记忆与内容 | [检索与内容](memory/implementation.md#module-shape) | [修订与副本](memory/implementation.md#data-flow) | [发布与读取](memory/implementation.md#key-sequence) | [索引与清理](memory/implementation.md#production) |
| Agent 协作 | [映射与转交](collaboration/implementation.md#module-shape) | [委派与额度](collaboration/implementation.md#data-flow) | [创建与恢复](collaboration/implementation.md#key-sequence) | [跨 Orchestrator 等待](collaboration/implementation.md#production) |
| 应用与交互 | [快照与输入](interaction/implementation.md#module-shape) | [请求与消费](interaction/implementation.md#data-flow) | [转交与确认](interaction/implementation.md#key-sequence) | [连接与积压](interaction/implementation.md#production) |
| 扩展与宿主 | [装配与隔离](extensions/implementation.md#module-shape) | [安装与实例](extensions/implementation.md#data-flow) | [激活与就绪](extensions/implementation.md#key-sequence) | [发布与可用性](extensions/implementation.md#production) |
| 观测评测与改进 | [评测与发布](evaluation/implementation.md#module-shape) | [计划与证据](evaluation/implementation.md#data-flow) | [封存与批准](evaluation/implementation.md#key-sequence) | [隔离与容量](evaluation/implementation.md#production) |

跨端部署继续读[传输契约](contracts/transport.md)；生产拓扑、稳定 Orchestrator 路由、内部通道重绑、单区故障恢复和性能预算读[生产部署与运行](deployment-production.md)，基础设施与备份职责读[存储与中间件](storage-and-middleware.md)，共同事务与时钟接口读[技术基线与宿主装配](deployment.md#4-默认宿主的装配与持久接口)。完成实现后按[故障实验](validation/fault-experiments.md)及生产用例取得运行证据，当前文档和静态检查不代表 L2 达成。

## 6. 启用前提与边界

首个参考实现按生产分布式形态装配 Orchestrator、默认三系统、CLI／Web 交互、搜索与内容获取适配器、文件能力以及多个有状态模拟手机，首阶段即验证多副本与恢复。完整单体保留为开发调试入口；本地 Brain、Memory、Executor 和混合装配仍是需验证的能力。云模型、搜索服务和外部 Agent 是可配置依赖；缺失时对应任务等待或明确返回不支持，全部权威与依赖在本地的任务仍按原契约执行。

跨端需要已配对身份、目标服务的持久幂等记录、有效权限及有限额度；缺少任一条件，不派发新动作。离线权限还需要对应平台的时钟与防回滚保证，缺失时关闭依赖远端缓存的权限使用，不影响全部权威在本机的模式。

运行中跨 Orchestrator 迁移、离线共同修改同一权威对象、任意来源原生插件、真实手机支持和跨格式无停机升级不属于参考实现的完成承诺。它们的接入条件在对应专题列出；本轮对 C1–C9、A1–A4 和两个专项的设计覆盖不因此减少。
