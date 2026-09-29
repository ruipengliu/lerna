# 阅读路径与文档索引

[方案入口](README.md) · [技术总览](technical-overview.md) · [交付状态](status.md)

本页供模块实现者、协议适配者和部署维护者定位详细规格。首次阅读先完成第 1 节的任务主线，再进入负责的模块；第 2 节按结构、数据、时序和生产约束提供实现索引。

## 1. 连续阅读路径

跨模块处理例子在[贯穿场景](walkthrough.md#2-正常主链)连续展开；行为规则在所属模块集中定义，场景和图册链接这些规则。完成这条主线后，按负责的实现范围进入下表，无需先通读所有专题。

| 顺序 | 文档 | 阅读所得 |
| --- | --- | --- |
| 1．建立主线 | [方案入口](README.md) → [贯穿场景](walkthrough.md) → [任务编排器](orchestrator/README.md)及[实现](orchestrator/implementation.md) | 谁形成提案、谁准入，正文、效果和完成证据如何交接，异常后谁继续 |
| 2．落实负责的模块 | [大脑](brain/README.md)／[执行](execution/README.md)／[记忆与内容](memory/README.md)／[权限](security/README.md)／[交互](interaction/README.md)／[协作](collaboration/README.md) | 按主链的交接点查完整规则，再进入第 2 节对应的实现章节 |
| 3．连接独立实现 | [共同契约](contracts/README.md) → [方法索引](contracts/methods.md) → [协议编码格式与机器资产](contracts/protocol.md) | 输入输出、成功点、错误、按原命令或对象标识恢复及对应构造序列 |
| 4．装配与运行 | [宿主装配](deployment.md) → [可靠接纳与持久工作框架](reliable-work.md) → [生产部署](deployment-production.md) → [存储与中间件](storage-and-middleware.md)；按需查[扩展](extensions/README.md)与[评测改进](evaluation/README.md) | 公共模板与领域处理器、事务及作业记录、生产故障边界、安装切换及隔离评测 |
| 5．交付切片 | [工程落地方案](engineering.md) → [验收建设顺序](validation/README.md#5-建设顺序与退出条件) → [交付状态](status.md)；按需查[历史审查](review.md) | 技术栈、代码目录、开发切片、生产准入及当前实际检查范围 |

[目标与功能](goals.md)保存范围和指标，[设计决策](decisions.md)保存关键选择及改选条件；[技术总览](technical-overview.md)解释四种边界和建模依据。图形查阅使用[架构图集](diagrams/architecture-atlas.html)、[可编辑全景图](diagrams/system-panorama.drawio)及[UML 导读](uml-models.md)，不以图中容器数量决定服务数量。

任务验证沿[条件、规则与验证器生命周期](orchestrator/verification.md)阅读，再查[核验持久化](orchestrator/implementation.md#condition-storage)及[存储访问路径](orchestrator/access-paths.md)。前者集中定义完成依据、异常与证据适用性，后两者给出恢复、查询和性能验收约束；当前仍是设计规格。

已确认的九模块优化按[预期效果、组件对照与验收](validation/optimization-evidence.md)查阅：24 项工作映射到所属模块，重点补齐原目标覆盖、输入与工具结果语义、实际制品及失败归因。规则进入方案，压缩、记忆整理、Skill、规划与协作候选保留实验验收条件；方案采用与运行收益分别记录。

第三方实现从同一方法索引进入 [Schema](contracts/schemas/protocol.schema.json)、[登记表](contracts/schemas/methods.json)及[协议序列](contracts/examples/protocol/README.md)；跨端再查[WSS](contracts/transport.md)或[gRPC](contracts/grpc.md)。声明一个方法须同时承担其查询、错误和恢复义务，参考实现表结构不属于替换要求。

### 目录与维护方式

九个模块各有独立目录，以 `README.md` 说明职责和完整处理流程，以 `implementation.md` 说明内部职责、持久记录、事务、恢复与故障验证。全局目标、决策、场景和部署位于顶层，图源在 `diagrams/`，机器契约及例子在 `contracts/`，系统验收与静态校验在 `validation/`。

细化某个模块时，先更新该目录的 `README.md`，保留职责、关键决策和完整处理链。独立机制需要展开时，再在同目录增加按主题命名的文件，并由模块入口给出阅读顺序；专属图示和示例随模块保存。共同字段、方法登记和跨模块用例继续归 `contracts/` 与 `validation/`，模块正文链接其权威定义。

<a id="detailed-design"></a>
## 2. 从主线进入详细实现

先读各模块 README 中的行为与取舍，再沿实现文档的「模块结构与依赖 → 对象流转 → 关键事务时序 → 生产约束」阅读。模块结构图表达软件依赖，流程／时序图表达运行中的对象交接，部署图表达进程及故障域；三种视角分别给出，图中节点不自动对应独立微服务。公共字段继续以同版 Schema 为准。

实现共同持久机制时先读[框架接口](reliable-work.md#interfaces)和[条件提交](reliable-work.md#completion)，再进入各模块的[接入节](reliable-work.md#integration)。框架统一参考实现的接纳与有限工作模板、逻辑 JobStore；各模块明确自己的事务参与者、责任键、成功与恢复判断，正式运行库及适配器仍待实现。

<a id="design-coverage"></a>

| 模块 | 形状与组件依赖 | 数据对象与流转 | 关键时序 | 生产约束 |
| --- | --- | --- | --- | --- |
| 任务编排器 | [入口与准入](orchestrator/implementation.md#module-shape) | [任务及工作](orchestrator/implementation.md#data-flow) | [提交与恢复](orchestrator/implementation.md#key-sequence) | [调度与热键](orchestrator/implementation.md#production) |
| 大脑 | [决策与适配器](brain/implementation.md#module-shape) | [上下文与提案](brain/implementation.md#data-flow) | [调用与归并](brain/implementation.md#key-sequence) | [并发与费用](brain/implementation.md#production) |
| 执行 | [启动检查与驱动](execution/implementation.md#module-shape) | [操作与效果](execution/implementation.md#data-flow) | [发送与核对](execution/implementation.md#key-sequence) | [资源与隔离](execution/implementation.md#production) |
| 权限与隔离 | [身份与许可](security/implementation.md#module-shape) | [许可与使用](security/implementation.md#data-flow) | [裁决与消费](security/implementation.md#key-sequence) | [权威与热点](security/implementation.md#production) |
| 记忆与内容 | [检索与内容](memory/implementation.md#module-shape) | [修订与副本](memory/implementation.md#data-flow) | [发布与读取](memory/implementation.md#key-sequence) | [索引与清理](memory/implementation.md#production) |
| Agent 协作 | [映射与转交](collaboration/implementation.md#module-shape) | [委派与额度](collaboration/implementation.md#data-flow) | [创建与恢复](collaboration/implementation.md#key-sequence) | [跨 Orchestrator 等待](collaboration/implementation.md#production) |
| 应用与交互 | [快照与输入](interaction/implementation.md#module-shape) | [请求与消费](interaction/implementation.md#data-flow) | [转交与确认](interaction/implementation.md#key-sequence) | [连接与积压](interaction/implementation.md#production) |
| 扩展与宿主 | [装配与隔离](extensions/implementation.md#module-shape) | [安装与实例](extensions/implementation.md#data-flow) | [激活与就绪](extensions/implementation.md#key-sequence) | [发布与可用性](extensions/implementation.md#production) |
| 观测评测与改进 | [评测与发布](evaluation/implementation.md#module-shape) | [计划与证据](evaluation/implementation.md#data-flow) | [封存与批准](evaluation/implementation.md#key-sequence) | [隔离与容量](evaluation/implementation.md#production) |

跨端部署继续读[传输契约](contracts/transport.md)；生产拓扑、稳定 Orchestrator 路由、内部通道重绑、单区故障恢复和性能预算读[生产部署与运行](deployment-production.md)，基础设施与备份职责读[存储与中间件](storage-and-middleware.md)，共同事务与时钟接口读[技术基线与宿主装配](deployment.md#4-默认宿主的装配与持久接口)。完成实现后按[故障实验](validation/fault-experiments.md)及生产用例取得运行证据，当前文档和静态检查不代表 L2 达成。
