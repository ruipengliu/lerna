# 组件详细设计

本目录从系统全景逐层展开组件内部设计。任务运行内核已经形成详细设计与本地接口、存储及验收规格；其余章节仍为规划。规格成文不表示运行实现或验收已经完成。

详细设计是架构正文的下一层：正文说明组件职责、对外行为和关键过程，本目录展开内部结构、算法、数据实现和替换验证。系统语义继续在[子系统设计](../main/02-subsystems/README.md)、[跨模块机制](../main/03-coordination/README.md)与[契约参考](../reference/README.md)维护；本地实现规格由对应详细设计维护，规则移交记入[来源索引](../maintenance/content-map.md#runtime-design-transfer)。尚未定义的跨模块 Schema、实现和验证参数继续列为待定。

## 已完成的详细设计

从[01 任务运行内核](01-runtime-kernel.md)开始，再按需查阅[01A 接口与类型](01-runtime-kernel-contracts.md)、[01B 持久化与调度](01-runtime-kernel-storage.md)和[01C 验证与实施](01-runtime-kernel-validation.md)。配套 [Go 接口声明](assets/runtime-kernel-contracts.go)和 [SQLite DDL](assets/runtime-kernel-v1.sql)是可静态检查的设计资产。

内核配套图示：[内核分工与原子组](diagrams/runtime-kernel-structure.html)、[失联决策恢复](diagrams/runtime-kernel-decision-recovery.html)、[更新与派发竞争](diagrams/runtime-kernel-dispatch-race.html)。三张图均为独立 HTML，可直接在浏览器打开，图中省略的字段和完整约束以详细设计正文为准。

## 从全景到局部的展开顺序

先从[组件全景](../main/01-system/03-component-panorama.md)定位模块及其调用方，再说明模块对外提供的行为、内部组成、状态归属与处理过程，最后进入实现选择和验证。全景的九个外框是阅读分组，不直接规定 Go 包、进程或事务。

运行存储在全景中合并展示 RunStore 与 WorkStore，详细设计应解释两个接口如何共同满足原子提交；授权服务合并展示 Authorizer 与 GrantAuthority，详细设计应分别解释判定与签发。ContextAssembler 由 Core 协调，访问记忆不改变记忆数据的写入权威。不要把图中的能力标签自动扩成新组件。

## 章节规划与当前依据

| 规划章节 | 对应组件 | 未来详细设计须回答 | 当前阅读入口 |
| --- | --- | --- | --- |
| 01 任务运行内核（已成文） | Core、Scheduler／Worker、RunStore／WorkStore | 集中规则、原子变更、单有效决策、旧工作复核、条件领取及恢复，详见本章及配套规格。 | [详细设计](01-runtime-kernel.md)、[架构正文](../main/02-subsystems/01-runtime-kernel.md) |
| 02 上下文与记忆 | ContextAssembler、MemoryService、MemoryStore、IndexAdapter、MemorySync | 如何组织可追溯上下文？如何保存修订、建立索引、处理失效与同步？ | [记忆](../main/02-subsystems/02-context-and-memory.md)、[记忆细则](../reference/memory-details.md) |
| 03 规划与模型调用 | Brain、ModelAdapter | 规划策略怎样形成有限提案？生成、用量、格式修复与停止怎样衔接？ | [决策](../main/02-subsystems/03-planning-and-decision.md)、[参考策略](../reference/decision-details.md) |
| 04 能力发现与执行 | CapabilityService／Catalog、ExecutionCoordinator、ExecutionStore、Driver | 发现、准入、准备、启动、核验和回报如何分工？未知效果和资源接管怎样处理？ | [执行](../main/02-subsystems/04-tools-and-devices.md)、[执行细则](../reference/execution-details.md) |
| 05 应用与交互 | Renderer、UIStateManager、SurfaceStore | 显示状态、输入路由与业务接纳如何衔接？多端输入、窗口恢复如何实现？ | [交互](../main/02-subsystems/05-interaction.md)、[交互细则](../reference/interaction-and-task-control-details.md) |
| 06 身份与授权 | 身份来源、Authorizer、GrantAuthority、凭证访问职责 | 判定、签发、消费和撤销如何协作？离线核验与秘密材料如何管理？ | [授权](../main/02-subsystems/06-identity-and-authorization.md)、[授权细则](../reference/identity-and-authorization-details.md) |
| 07 协作与连接 | TaskService、ConnectionAdapter | 任务端点怎样接纳委派？连接怎样转交、补读和限流？如何遵循跨端移交协议？ | [组件职责](../main/02-subsystems/07-collaboration-and-connection.md)、[协作](../main/03-coordination/03-agent-collaboration.md)、[端云](../main/03-coordination/04-edge-cloud-coordination.md) |
| 08 扩展与运行保障 | ExtensionManager、ExtensionRuntime、Bootstrap、RecoveryController | 如何装配、激活和固定实现？运行器及恢复协调器怎样调用业务模块并保留其权威？ | [扩展](../main/02-subsystems/08-extensions-and-runtime.md)、[组装](../main/04-engineering/01-implementation-and-technology.md)、[恢复](../main/04-engineering/03-reliability-and-recovery.md) |
| 09 观测、评测与改进 | Observation、Evaluation、Evaluator、Evolution | 如何关联诊断、独立判分、比较候选和发布？管理状态与业务事实怎样分开？ | [组件职责](../main/02-subsystems/09-observation-and-improvement.md)、[评测发布](../main/04-engineering/04-evaluation-and-release.md)、[评测细则](../reference/evaluation-details.md) |

## 每篇详细设计的内容要求

1. **职责与选择。** 说明问题、负责的判断、其他模块承担的职责，以及选择理由、代价和适用条件。
2. **外部协作。** 标明调用方、依赖方、输入输出和依赖方向。接口包括调用顺序、权限、不变量、错误与配置要求。
3. **内部组成。** 展开内部模块及交接，解释哪些复杂性封装在接口后，哪些实现确实需要替换。
4. **数据与状态。** 标明记录的写入方、生命周期、版本、原子组和恢复依据，区分权威数据与可重建投影。
5. **关键过程。** 走通一次正常处理，再改变一个关键条件，说明并发、取消、超时或重启后的路径。例子辅助解释通用规则。
6. **实现与验证。** 说明默认 Adapter、资源限制、配置、替换验证与未定事项；完整字段和故障组合链接到查阅资料。
