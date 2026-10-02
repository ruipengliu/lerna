# 正式架构运行维护读者报告

审校范围为 `docs/architecture` 的正式正文。先从 README 阅读，再完整阅读 deployment、deployment-production、engineering、storage-and-middleware、reliable-work，并沿链接阅读 Orchestrator 持久恢复、Brain 发送恢复、Executor 文件及设备入口恢复、Memory 来源关闭与副本恢复、Extensions 激活及迁移入口。初读时未使用 .draft、研究或其他 scratch 报告；以下复述依正式正文形成，随后仅用相关 ADR 和 .draft 核对发现的边界。

结论：正式正文足以建立装配、数据库故障切换、恢复、迁移、备份与取证的责任模型。未发现须补充新领域字段或新增基础设施才能成立的交接。初读发现的执行排空范围歧义已由作者修订并经下述复核关闭，当前所审运行维护主线通过；这不是运行验收通过。

## 独立复述

### 报告已经写成但答复丢失

Task、目标条件、OperationIntent、派发原命令、jobs 和最终结果归固定 Orchestrator；Executor/file owner 保存原 Operation、Attempt、原回执、路径占用及写入 journal。报告的 Content 字节与用户目录中的实际目标文件是两种存储。接替者先按原 command_id／operation_id 查询，再隔离旧本地发送者、锁原路径，核对 journal、已记录临时文件身份、目标身份／摘要及目录持久性；目标内容相同不足以证明原操作已经执行。证据不能唯一绑定原操作时维持 effect=unknown 和后续写入隔离，不另建写操作。声明允许的安全重放仍受原身份、控制、许可、期限、尝试数及预算限制。独立读回是另一次获准操作，写入成功回执不能替代它。

依据：[任务恢复](../../docs/architecture/orchestrator/durable-work.md#durable-before-send)、[Executor 发送时序](../../docs/architecture/execution/implementation.md#key-sequence)、[受管文件驱动](../../docs/architecture/execution/implementation.md#6-能力目录与-api-驱动装配)。

### 单区数据库切换

应用实例不选主，也不另建空库；稳定 Orchestrator／owner 经受信放置映射连接原分片的新主。平台先证明旧主隔离、候选包含全部已确认提交，且第二耐久副本与同步提交恢复，应用再恢复关闭、撤权、原回执和未决责任。诊断及获准原记录查询可在条件满足时先开放；持久控制与核对还需要写资格、当前控制、时钟、恢复处理器和收尾容量。新任务及新的效果／费用启动在所需恢复入口、安装、批准、依赖与剩余容量具备后才开放，并再次走具体行动门禁。

时差或暂停证据不足时封闭受影响租约续约与回收；不能先复用额度，再期待旧进程停止。单区账本 RPO=0、控制／查询 RTO≤60 秒是待平台运行验收目标。受控文件根另有故障域，根失联时即使账本已恢复也停止该根新写、保留原效果 unknown；跨区文件接管需独立介质及旧写者隔离证据。

依据：[主库切换](../../docs/architecture/deployment-production.md#availability)、[各入口就绪](../../docs/architecture/deployment.md#recovery-readiness)、[时间与租约](../../docs/architecture/deployment.md#clock-adapter)、[目标文件故障域](../../docs/architecture/deployment-production.md#2-拓扑路由及数据放置)。

### 旧工作者恢复运行

Job 的 lease_epoch 约束领取者，work_revision 保存新增责任，Claim 固定 observed_work_revision。旧领取者的 Guard／Finish 不成立时，受保护事务整笔回滚；即使领取仍有效，旧观察版本也不能把新增控制或费用责任结束或延后。可信迟到结果可以由原 owner 的独立事实入口验证来源、业务身份和修订后收取，不能借此恢复旧工作者的任意写权。

领取到期仅是本地提交资格变化，不能证明网络字节未发出或目标动作已结束。Brain 无 send_started 的 prepared 可在当前门禁成立时进行原首次发送；有 send_started 则查询原调用或保留 unknown。Executor 准备后不能证明未交接时按可能发送处理。原 Operation、资源实际入口、TaskGate、目标证据和准确能力合同共同决定是否允许继续；换 worker 不增加物理尝试或许可额度。

依据：[条件提交](../../docs/architecture/reliable-work.md#completion)、[独立事实归并](../../docs/architecture/reliable-work.md#recovery)、[Brain 原调用恢复](../../docs/architecture/brain/implementation.md#key-sequence)、[设备入口恢复](../../docs/architecture/execution/implementation.md#entrance-recovery)。

### 内容源关闭

内容 owner 保存控制修订、持有者登记和来源依赖；消费者在正文交付前登记准确 copy。关闭先封闭新使用并保存逐持有者责任，停止使用和物理清理分别取得事实。普通跨库 copy 由原 holder 的持久作业通过 content.get(mode=control) 读取原内容与自身副本控制，再封闭关联新使用、核对已进入的有限使用单元、清理正文／派生物／索引／缓存／暂存，最后以 content.release_copy 回报。owner 保留未确认责任；通知成功、查询成功或 release_copy 的 applied 都不能把 pending／residual／unknown 提升为清理完成。

跨库关闭与发布只有本地已查回控制的互斥，没有即时全局互斥。源端刚关闭、本地尚未查回时发布仍可能提交；它随后在线复核来源，查到关闭即禁用并进入清理。来源暂时不可达保留 source_unavailable，并阻塞依赖当前核验的使用；已明确开放的离线许可按原期限单独验收。恢复先加载当前关闭记录、来源限制和未结清理，再开放正文读取，旧备份不能复活关闭内容。

依据：[Memory 关闭主线](../../docs/architecture/memory/README.md#53-禁用删除与恢复)、[跨库发布](../../docs/architecture/memory/implementation.md#reference-gate)、[关闭恢复实现](../../docs/architecture/memory/implementation.md#6-关闭清理与恢复)。

## 运行维护交接检查

| 工作 | 据正式正文可以确定的执行边界 | 结论 |
| --- | --- | --- |
| 装配 | 角色、受限 repository／ports、原 owner 事务范围、kind 处理器映射、身份、ClockAdapter、字节介质、精确安装及当前批准分别注入；缺处理器关闭该类新接纳 | 通过设计交接，源码及配置资产待实现 |
| 启动恢复 | 原权威和完整记录先于组件就绪；查询、控制／核对和新工作有不同条件；有限未结索引恢复，不要求遍历全部历史或清空 unknown | 通过 |
| 排空发布 | 网关 socket 关闭后条件释放额度；应用流重绑；worker 停新领取并保留可能发送责任；组件切换和执行宿主退出分别裁决，后者封闭新实际启动 | 通过，初读歧义已修订复核 |
| 迁移回退 | 独立迁移动作及互斥锁、固定 migration_id／步骤、格式摘要和检查点；兼容回填限量，不兼容切换维护；应用回退须当前独立批准且可读现存格式 | 通过，工具版本表不能代替业务迁移记录 |
| 备份灾备 | 数据库切点、准确 Content 版本、安装锁定清单、去重及终态记录、映射／发布历史、来源目录及授权权威、密钥恢复依据组成清单；先隔离旧写者，再补完整后续事实 | 通过；缺历史时只读诊断，目标文件根不从账本备份自动获得跨区保证 |
| 取证 | 原对象、命令、job、领取／责任版本、事务结果、目标证据、费用累计修订及未结责任串联；敏感正文不入通用遥测，日志不作账务真值 | 通过；实际运行实验才证明恢复、容量及效果 |

## 初读发现与修订复核

### P2 执行排空范围歧义 已关闭

编号：O-R01。状态：resolved。

- 正式位置：[execution/implementation.md 第 9 节](../../docs/architecture/execution/implementation.md#9-调度恢复和停机)；对照 [deployment-production.md 第 6.1 节](../../docs/architecture/deployment-production.md#61-按角色发布和排空)、[engineering.md 平台排空](../../docs/architecture/engineering.md#platform) 和 [extensions/implementation.md 第 8 节](../../docs/architecture/extensions/implementation.md#8-停用回退与格式迁移)。
- 读者实际理解：执行实现写“升级排空只阻止新的原操作接纳”，按这句实施时，只关闭 execution.invoke 仍可让已接纳但尚未发送的操作在排空期间首次进入驱动。生产角色表却要求执行宿主／驱动“封闭新实际启动”，平台生命周期入口同样要求执行入口停止新启动。停机维护时这两种入口开放范围不同。
- 依据：Extensions 已明确，组件停用前已接纳的工作进入 residual_work，是否还能物理启动由领域控制、批准与当前安全检查裁决。因此组件绑定切换和宿主退出本可以采用不同边界；执行实现的“只”与紧邻停机说明没有标明区别，不能据它确定进程排空期间已接纳但未启动操作应如何处置。
- 必要修订：将这一句限定为组件绑定切换，或明确链接 Extensions 的 residual_work 规则；随后单独说明执行宿主／驱动退出时封闭新实际启动，未启动原操作交回原责任，已可能发送者保留原效果／停止／费用核对。无需增加公共字段或把全部组件停用改成同一种策略。
- 来源核对：.draft 的执行、生产和扩展篇保留相同措辞，说明此处不是正式化漏迁一条既有规则；相关 ADR 没有另设排空语义。建议修订正式正文的范围表达，不改变既有默认行为。

2026-10-02 复核：已读取作者修订后的 execution/implementation.md 第 9 节及其引用的 Extensions 第 5 节、engineering 平台生命周期和 production 按角色排空。新正文明确组件绑定／版本切换停止向旧绑定接纳新 Operation，已有操作仍固定原版本及配置，达到资源交接边界后切换；未知责任保持 blocked 并保留可信旧版本核对能力。它另行明确执行宿主退出时封闭新实际启动，已接纳但未发送的操作也不能在退出期间首次越过发送边界，原效果、停止状态及费用核对继续。引用规则与新段落一致，没有把退出或版本切换当作目标动作终结证据。此项关闭，无新增待修订发现。

## 默认选择与证据范围

生产角色独立、单地域三可用区、托管 PostgreSQL／对象存储、原数据库 jobs 与可丢通知、固定 Orchestrator、受管文件根及词法默认均为设计选择。MQ／共享缓存／向量索引、目标可验证 fencing 的自动接管与文件根跨区接管均有条件，不能从图示或技术栈名推导已可用。Go／TS 端口、SQLite／PG 适配、SDK、运行库和阶段切片尚待实现；公司平台故障保证、真实外部依赖、质量和最终规模需各自运行证据。所读正文和 ADR 对这一证据边界一致，没有把静态例证写成运行达标。
