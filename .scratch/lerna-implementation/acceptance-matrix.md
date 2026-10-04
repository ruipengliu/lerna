# 目标、故障反例与验收归属

本矩阵把 [实施与验证](../../docs/architecture/validation.md) 的目标和反例映射到切片的 `Testing Decisions`。**列入验收不表示已经通过测试。** 实现顺序、外部条件与完成证据见[切片索引](README.md)。

## 目标到切片

| 目标 | 主要切片 | 必须取得的结果 |
| --- | --- | --- |
| G1 持续目标 | [05](../lerna-05-task-requirements/spec.md#testing-decisions)、[08](../lerna-08-verified-results/spec.md#testing-decisions)、[09](../lerna-09-task-control/spec.md#testing-decisions)、[11](../lerna-11-application-sdk/spec.md#testing-decisions)、[12](../lerna-12-research-report-app/spec.md#testing-decisions) | 原文、条件、修订、等待、控制到固定 Result 可追溯；旧提案不能作用于新目标。 |
| G2 故障恢复 | [02](../lerna-02-durable-work/spec.md#testing-decisions)、[03](../lerna-03-deterministic-harness/spec.md#testing-decisions)、[07](../lerna-07-execution-reconciliation/spec.md#testing-decisions)、[09](../lerna-09-task-control/spec.md#testing-decisions)、[13](../lerna-13-distributed-runtime/spec.md#testing-decisions)、[17](../lerna-17-production-readiness/spec.md#testing-decisions) | 真实数据库、目标和故障域证据；原身份、未结效果与费用不丢失。 |
| G3 能力替换 | [01](../lerna-01-command-contracts/spec.md#testing-decisions)、[14](../lerna-14-memory-core/spec.md#testing-decisions)、[15](../lerna-15-component-interoperability/spec.md#testing-decisions)、[16](../lerna-16-activation-lifecycle/spec.md#testing-decisions) | 三系统第二独立实现，至少一个异构语言真实远程运行，在途版本可恢复。 |
| G4 端云协作 | [13](../lerna-13-distributed-runtime/spec.md#testing-decisions)、[19](../lerna-19-bounded-delegation/spec.md#testing-decisions)、[20](../lerna-20-device-autonomy/spec.md#testing-decisions) | 有限离线窗口、设备接管、独立 owner、委派控制与根预算可核对；GUI 平台另验。 |
| G5 受控且可核验 | [04](../lerna-04-content-snapshots/spec.md#testing-decisions)、[06](../lerna-06-authorized-admission/spec.md#testing-decisions)、[07](../lerna-07-execution-reconciliation/spec.md#testing-decisions)、[08](../lerna-08-verified-results/spec.md#testing-decisions)、[10](../lerna-10-model-search-adapters/spec.md#testing-decisions)、[18](../lerna-18-memory-lifecycle/spec.md#testing-decisions)、[21](../lerna-21-evaluation-release/spec.md#testing-decisions) | 准入与真实启动、来源权限、预算、必要条件和独立真值共同受测；unknown 保留。 |
| G6 接入与改进 | [11](../lerna-11-application-sdk/spec.md#testing-decisions)、[12](../lerna-12-research-report-app/spec.md#testing-decisions)、[15](../lerna-15-component-interoperability/spec.md#testing-decisions)、[21](../lerna-21-evaluation-release/spec.md#testing-decisions)、[22](../lerna-22-capacity-recovery/spec.md#testing-decisions) | 真实应用与组件读者试验、配对改进实验、批准发布、回退和成本证据。 |

## 原设计故障反例全表

每项必须配正常允许动作的对照。编号是本矩阵的覆盖编号，不是自动化测试已经存在的声明；可在实现时分成多个测试，但不得删除原语义。

| 编号 | 原设计场景 | 验收切片 | 关键断言或未开放边界 |
| --- | --- | --- | --- |
| F01 | 提交成功但回执丢失 | [02](../lerna-02-durable-work/spec.md#testing-decisions)、[05](../lerna-05-task-requirements/spec.md#testing-decisions)、[07](../lerna-07-execution-reconciliation/spec.md#testing-decisions)、[13](../lerna-13-distributed-runtime/spec.md#testing-decisions) | 查询或重传原 Command，仅一个原对象；提交未知不换 owner。 |
| F02 | 原键被用于不同输入 | [01](../lerna-01-command-contracts/spec.md#testing-decisions)、[02](../lerna-02-durable-work/spec.md#testing-decisions) | 摘要不同固定冲突，第二份内容不执行。 |
| F03 | 新工作遇到旧完成 | [02](../lerna-02-durable-work/spec.md#testing-decisions) | 旧完成仅推进 claimed_revision，新 work_revision 仍可领取。 |
| F04 | 旧 worker 迟到 | [02](../lerna-02-durable-work/spec.md#testing-decisions)、[07](../lerna-07-execution-reconciliation/spec.md#testing-decisions)、[17](../lerna-17-production-readiness/spec.md#testing-decisions) | 旧 epoch 拒绝；对实际外部发送另核隔离与效果。 |
| F05 | 写入已发生但结果未落库 | [07](../lerna-07-execution-reconciliation/spec.md#testing-decisions)、[09](../lerna-09-task-control/spec.md#testing-decisions) | 沿原 Operation 核对或保持 unknown，不盲目重复写。 |
| F06 | 取消先于 invoke | [07](../lerna-07-execution-reconciliation/spec.md#testing-decisions)、[09](../lerna-09-task-control/spec.md#testing-decisions) | 原 owner 和意图绑定墓碑持久保留，迟到启动拒绝。 |
| F07 | 取消后仍有迟到效果 | [09](../lerna-09-task-control/spec.md#testing-decisions)、[13](../lerna-13-distributed-runtime/spec.md#testing-decisions) | 取消 Result 固定，合法迟到事实与收尾独立更新。 |
| F08 | 目标修订与旧 Proposal 竞争 | [05](../lerna-05-task-requirements/spec.md#testing-decisions)、[09](../lerna-09-task-control/spec.md#testing-decisions) | 原目标和控制版本过期时不得准入。 |
| F09 | 父目标变化时子任务继续 | [19](../lerna-19-bounded-delegation/spec.md#testing-decisions) | 封旧委派新资格，传播责任持久化，旧有限窗口和旧结果再核验。 |
| F10 | 控制消息乱序 | [07](../lerna-07-execution-reconciliation/spec.md#testing-decisions)、[09](../lerna-09-task-control/spec.md#testing-decisions)、[13](../lerna-13-distributed-runtime/spec.md#testing-decisions) | 低 control_revision 拒绝，resume 不复活墓碑。 |
| F11 | 接纳后执行失败 | [07](../lerna-07-execution-reconciliation/spec.md#testing-decisions) | accepted 回执不变，执行事实另查，不重建同义操作。 |
| F12 | 强制上下文超过模型上限 | [04](../lerna-04-content-snapshots/spec.md#testing-decisions)、[05](../lerna-05-task-requirements/spec.md#testing-decisions) | context_overflow 可观察，模型出口没有发送，强制约束不裁掉。 |
| F13 | 候选完成时还有在途创建 | [08](../lerna-08-verified-results/spec.md#testing-decisions) | 完整 Task Responsibility 阻止成功，不依赖远端列表是否为空。 |
| F14 | 批量行动部分执行失败 | [06](../lerna-06-authorized-admission/spec.md#testing-decisions)、[07](../lerna-07-execution-reconciliation/spec.md#testing-decisions) | 本地准入全收或全拒；全部获准后一项执行失败，各效果分开保存并重新规划。 |
| F15 | 旧授权窗口被重复使用 | [06](../lerna-06-authorized-admission/spec.md#testing-decisions)、[16](../lerna-16-activation-lifecycle/spec.md#testing-decisions)、[20](../lerna-20-device-autonomy/spec.md#testing-decisions) | 重启、取消和回退不能恢复一次性消费或延长期限，当前资格继续核验。 |
| F16 | 费用晚到或更正 | [06](../lerna-06-authorized-admission/spec.md#testing-decisions)、[09](../lerna-09-task-control/spec.md#testing-decisions)、[19](../lerna-19-bounded-delegation/spec.md#testing-decisions) | 累计修订只结算增量，未结消费不当零释放，终态后仍记更正。 |
| F17 | 插件激活竞争 | [16](../lerna-16-activation-lifecycle/spec.md#testing-decisions) | 目标头代次比较，旧初始化和清理不能覆盖新实例。 |
| F18 | 普通升级与旧任务下一轮 | [16](../lerna-16-activation-lifecycle/spec.md#testing-decisions) | 普通升级保留合格旧 holder，显式停用封旧版本全部新调用。 |
| F19 | 旧版本回退资格失效 | [16](../lerna-16-activation-lifecycle/spec.md#testing-decisions)、[21](../lerna-21-evaluation-release/spec.md#testing-decisions) | 无有效旧批准、兼容格式或正确代次时拒绝回退。 |
| F20 | 远端任务 completed 但业务失败 | [08](../lerna-08-verified-results/spec.md#testing-decisions)、[10](../lerna-10-model-search-adapters/spec.md#testing-decisions)、[19](../lerna-19-bounded-delegation/spec.md#testing-decisions) | 本地必要条件与实际产物独立核验，MCP / A2A 状态不完成本地 Task。 |
| F21 | 记忆撤权与分页竞争 | [14](../lerna-14-memory-core/spec.md#testing-decisions)、[18](../lerna-18-memory-lifecycle/spec.md#testing-decisions) | 权限代次变更使旧 cursor 失效，后续页不泄露受限结果。 |
| F22 | 内容发布中断 | [04](../lerna-04-content-snapshots/spec.md#testing-decisions) | 恢复原发布或清理孤儿，不发布指向缺失字节的引用。 |
| F23 | 环境 cell 崩溃 | [22](../lerna-22-capacity-recovery/spec.md#testing-decisions) | 当前 Environment 未开放，验证 unsupported；启用前新增专项切片，hostcall 效果不随变量回滚且不透明重放 cell。 |

F23 的实际 cell 崩溃恢复尚未实现，不能标记为通过；22 只验默认拒绝。启用 Environment 前必须增加专项实现规格与对应正反例。其他明确未开放 profile 的要求见[可选范围](README.md#显式保留的可选范围)。

## 横贯切片的附加验收

| 检查面 | 切片 | 正反例与证据 |
| --- | --- | --- |
| 机器契约与版本 | [01](../lerna-01-command-contracts/spec.md#testing-decisions)、[11](../lerna-11-application-sdk/spec.md#testing-decisions)、[13](../lerna-13-distributed-runtime/spec.md#testing-decisions)、[15](../lerna-15-component-interoperability/spec.md#testing-decisions) | 严格 JSON、跨语言整数精度、未知字段、方法成功点、SDK 与 Schema 同版；未实现方法明确拒绝。 |
| 租户与来源隔离 | [04](../lerna-04-content-snapshots/spec.md#testing-decisions)、[06](../lerna-06-authorized-admission/spec.md#testing-decisions)、[10](../lerna-10-model-search-adapters/spec.md#testing-decisions)、[13](../lerna-13-distributed-runtime/spec.md#testing-decisions)、[14](../lerna-14-memory-core/spec.md#testing-decisions)、[18](../lerna-18-memory-lifecycle/spec.md#testing-decisions) | 认证身份、当前用途、完整处理来源、恶意材料、查询不泄露存在性；正常获准用例仍成功。 |
| 确认与受信呈现 | [06](../lerna-06-authorized-admission/spec.md#testing-decisions)、[11](../lerna-11-application-sdk/spec.md#testing-decisions) | 完整正文成功呈现、准确摘要与修订、本人绑定、一次消费；过期或参数变化不能复用。 |
| 资源门禁与设备接管 | [07](../lerna-07-execution-reconciliation/spec.md#testing-decisions)、[13](../lerna-13-distributed-runtime/spec.md#testing-decisions)、[20](../lerna-20-device-autonomy/spec.md#testing-decisions) | 租约到期不证明隔离；接管先封新输入，重启不延长离线窗口。 |
| 故障与通知恢复 | [02](../lerna-02-durable-work/spec.md#testing-decisions)、[11](../lerna-11-application-sdk/spec.md#testing-decisions)、[13](../lerna-13-distributed-runtime/spec.md#testing-decisions) | 丢掉唤醒、订阅和回执仍有扫描或查询路径；等待不占长事务。 |
| MCP / A2A | [10](../lerna-10-model-search-adapters/spec.md#testing-decisions)、[19](../lerna-19-bounded-delegation/spec.md#testing-decisions) | 真实声明版本的服务；句柄过期、取消不终止、业务错误、断连及不支持保证。 |
| 来源更正与清理 | [04](../lerna-04-content-snapshots/spec.md#testing-decisions)、[14](../lerna-14-memory-core/spec.md#testing-decisions)、[18](../lerna-18-memory-lifecycle/spec.md#testing-decisions) | 准确版本、提交水位、索引重建、跨 holder 残留、证据失效及原 Result 不可变。 |
| 首次生产 | [17](../lerna-17-production-readiness/spec.md#testing-decisions) | 真实单区故障：已确认账本 RPO=0，控制与查询恢复≤60秒；旧主隔离及依赖故障域。 |
| 质量与人工参与 | [21](../lerna-21-evaluation-release/spec.md#testing-decisions)、[22](../lerna-22-capacity-recovery/spec.md#testing-decisions) | 冻结任务、真值和上限，全部分支成本，失败与 unknown 分母，人工代做单列，适当统计区间。 |
| 最终规模与异地灾备 | [22](../lerna-22-capacity-recovery/spec.md#testing-decisions) | 分任务负载、尾延迟、失效点、备用容量；先冻结异地 RPO / RTO，再实际演练并核对外部效果。 |

## 验收记录的使用

完成一个切片时，逐项提供 对应规格中所有验收条件的证据，并标注本矩阵关联反例。单项拒绝测试通过不等于全部安全保证，测试范围内没有未授权实际效果也不能宣称不存在漏洞。

对供应商、平台或实际读者尚不可用的检查，记录缺少的具体条件与负责角色。记录“未运行”“不适用”“未通过”必须保留原因；不得把它们改写成零成本、零未知或默认成功。

## 实际退出记录

### 01：共同命令与机器契约

2026-10-03，准确实现代码 `23bac17ba0909c7a4d49d846eb08bc63391b99f0`、合同 **1.0.0**、生成器 **1.0.0**。全部 6 项本片验收、审查修复、架构优化和准确 push CI success；正常与拒绝、环境、命令和限制见 [spec退出证据](../lerna-01-command-contracts/spec.md#切片退出证据2026-10-03) 与 [CI记录](../lerna-01-command-contracts/ci-verification.md)。

- **机器契约与版本：01范围通过。** 158 个共同夹具前向 / 反序真实双向 typed 往返，46 项独立命令摘要案例，28 项受信查询，15 项协商及 2 个独立 Schema digest goldens；支持清单仅完整 command.get。
- **F02：只完成摘要组成和变化证据。** 持久原键异摘要冲突、不执行第二份输入、清理后墓碑仍阻止重建均留给02；F02整体尚未关闭。
- **G3：只完成同版公共合同基础。** 两种语言的 codec 不是三个业务系统的第二独立实现；15的真实异构组件与在途恢复仍未验收。
- 注入受信身份 / 目录 / 只读事实源的隔离与有限取消通过；真实认证、数据库、传输及全部其他目标 / 故障反例保持未验收。

### 02：SQLite 接纳票据检查点

2026-10-03，票02已 resolved；v1实际 writer `f4fb057` 及完整真实 file 来源已保留。两库复用13项 Host接纳行为，SQLite正常/跨进程第二Host排除/关闭接替、busy、取消、scope、迁移checksum与文件恢复均通过；准确版本、命令和限制见[票02 Comments](../lerna-02-durable-work/issues/02-sqlite-durable-admission.md#comments)。

F01/F02仅完成本票的原回执/原责任/同键冲突与重开范围；SIGKILL、SQLite Claim、调度、正文清理后的墓碑和整个G2出口仍需后票。TMPDIR指向本轮自登记overlayfs目录的真实两库integration/race通过；未宣称断电或生产故障域耐久。

## 切片02票06完整本地检查点（整片仍在进行）

2026-10-03，产品06ab246、worker cdc7ae6经merger合入96a0ecc。八张核心票53项及额外09五项验收已resolved；最终完整mandatory双库count1集成60.305s、integration-race91.786s通过，来源27项checksum不变。以下是本机Host范围证据，整片尚待两轴审查、架构审查和准确最终CI。

- 普通有效Claim饱和时，真实三类别运行循环仍完成control/reconciliation的Start、literal hello hash及Finish；跨tenant、同tenant多owner和8并发worker共享数据库配额。
- 有限queue最后位置竞争只接纳一份，落败原身份可重试；活跃原Job的新revision复用位置，done重触发背压，缩容overhang保责任。
- 持久tenant FIFO、等待/分配观察、due/Job有界分页和高水位通过：新/恢复者进入现等待者尾部，重开仍保序；PG真实65个锁前缀另验证跳锁后的健康后缀，SQLite保留实际单writer范围。
- quota0到期和attempt耗尽维护无需新执行Claim，准确关闭原revision并保留新工作、异revision活Claim和原成功Projection；正常最后Start不会被提前关闭。
- 原04/07/08业务恢复已经统一实际Start、pool门禁与Clock，真实v1/v2来源在0005升级后继续原身份/Job。scope错装及完整SQLite副本反例有正常对照；路由/部署fork仍依赖可信装配与drain。

准确版本、全部正常/故障观察、命令和限制见[06 Comments](../lerna-02-durable-work/issues/06-fair-capacity-and-quotas.md#comments)。04历史未知schema和07未知CREATE容器未猜删；这些证据不关闭真实外部效果、生产故障域或G2的其他切片。

## 切片02票03的PG证据（历史检查点）

2026-10-03，准确实现 `18b80ce`，Go1.27.1/pgx5.11.0/PostgreSQL18.6，READ COMMITTED、同步提交和有限事务/statement/lock期限；全部29个真实PG测试、基础check/race及integration-race通过。详情见[票03 Comments](../lerna-02-durable-work/issues/03-pg-revision-claims.md#comments)。

- **F03：PG受控数据库路径已验证。** 领取一致输入快照；新工作先提交与旧完成先提交两种真实并发顺序都只推进 claimed_revision，保留原 Job 的新修订及原固定回执；新 worker 正常完成。SQLite同套路径仍待票04。
- **F04：PG Claim受控数据库写入已验证。** 续租/完成核验原 Job、对象、worker、revision、epoch和有效租约；过期尚未被替代也拒绝，接替保留身份并递增epoch；并发/锁等待到期与准确整数上界有正常对照。无外部动作，不证明外部旧进程或效果隔离。
- **遗漏通知恢复：PG基础领取已验证。** 持久有界索引扫描不依赖通知，锁竞争可少领且责任保留。持久等待/退避、公平/配额、真实旧 writer 完整迁移恢复和SIGKILL仍留给后票，不关闭G2或切片02整体。


## 切片02票04的两库工作证据（切片仍在进行）

2026-10-03，准确受测代码 `ff22936`，SQLite实际v2与13个共同Host工作故事在PG/SQLite通过同一断言。完整 mandatory integration11.269s和affected integration-race22.544s，基础check/race、immutable v1 artifacts校验通过；准确版本、TDD、命令和失败轮清理限制见[票04 Comments](../lerna-02-durable-work/issues/04-sqlite-claim-conformance.md#comments)。

- **F03：两库受控数据库路径已验证。** 原Job旧快照完成仅推进领取修订，新工作先提交/旧完成先提交两种顺序均保留最新工作；固定原receipt和输入身份不变。
- **F04：两库Claim受控数据库写入已验证。** 全绑定、未替换但已过期拒绝、原Job epoch接替、关闭重开、准确微秒截止和新worker正常完成成立；不证明外部进程/效果已隔离。
- SQLite真实历史v1 writer file可应用真实v2并Claim/完成；完整迁移失败恢复、持久等待、容量/配额、墓碑和SIGKILL继续由后票承担，不关闭G2或切片02。

## 切片02票07/08检查点（切片仍在进行）

07已合入0f27475，真实v1来源→v2→v3升级及版本保存失败/回滚/重试、正文零字节/gone/原命令固定决定、独立墓碑和新修订不被旧清理删除通过，详见[07证据](../lerna-02-durable-work/issues/07-tombstones-and-forward-migration.md#comments)。F02的正文清理后原身份去重范围有双库证据，不宣称墓碑GC、历史/备份取证级擦除。

08代码73310e2的整合d89e789及[准确远端CI](https://github.com/ruipengliu/lerna/actions/runs/37151050492)通过：F01的提交前/提交确认后Host答复前真实SIGKILL、原回执/原责任重开，以及F03/F04原Job跨进程更高epoch接替、旧完成拒绝和正常对照有双库证据。SQLite确认丢失仅为真实已提交的storage-port装饰故障，未覆盖nativeCommit异常；不宣称断电或外部效果隔离。

05/06仍未退出，最终必须在其真实Start及pool门禁下重跑这些旧业务路径，再进行两轴审查/架构优化和准确CI；这些检查点不关闭G2或切片02。04未知schema及07未知CREATE未启动容器的清理限制保持，不依据猜测删除。

2026-10-03票05检查点：原Job在全部通知丢失后通过bounded scan恢复；future due与事务外finite Timer、waiting释放Claim/业务单连接/Tx、重开保留条件/有限attempt/deadline、旧失败/停止保留新work及旧成功Projection，均有PG/SQLite共同Host观察与public receipt正常对照。04/07/08现统一真实Start/资格/policy门禁与ownerClock，真实v2原r1 Claim/work r2升级保留且到期后首次adoption，详见[05完整证据](../lerna-02-durable-work/issues/05-persistent-wait-and-scan.md#comments)。准确受测8a1df46，本地count1 wholeintegration26.132s/race57.172s通过；06pool/公平/配额仍待实施，远端最终CI及整片两轴审查/优化待后续，不关闭G2或切片02。原04/07清理限制保持。

## 切片02正式退出（DB/Host范围）

2026-10-03，最终源码f56d930/整合5548744、10票68AC、双轴原问题关闭且新增0、fixture收益闭合、准确CI37162569420 success。原七项验收映射、环境/命令/限制见[退出证据](../lerna-02-durable-work/exit-evidence.md)。本地count1正常50.431s/race93.834s顺序timeout120；CI20.985s/45.463s，两库及27真实来源全执行。

- **F01/F02：本片持久原键范围通过。** 真正提交/丢答复/重开、异摘要冲突及正文gone后墓碑恢复，原成功/拒绝不改写；其他Task/Operation/网络范围仍待各自切片。
- **F03/F04：本片DB写入及进程接替范围通过。** 两种新旧修订先后、完整Claim/epoch门禁和新worker正常完成；不证明外部旧写端或迟到效果已停止。
- **通知/等待/配额范围通过。** 丢通知仍扫描、Tx外等待、all-members due/lease/current+claimed期限、三独立lane、有限queue/tenant×lane、动态FIFO/有界页及quota0维护均有正常对照；服务机会前提保留，不是无条件墙钟SLA。
- **G2整体及G3仍未完成。** 03/07/09/13/17的目标、外部责任/生产耐久与15真实第二业务实现待验；1.0 codec不是三个系统互操作。

本fix285PG/265目录absence仅指准确登记项，原架构轮321/319另列；10两未知PG名、旧04未知schema和07unknownCREATE/CID不猜删。SIGKILL/postcommit-port fault不冒称断电/nativeSQLite Commit异常或跨区耐久。此前章节为历史检查点，状态以本节及progress当前记录为准。

## 切片03规则首票检查点（整片尚未退出）

2026-10-04，01/04/05合计21/42AC已resolved；01准确源696ac49、交付d806a92、整合5307702。完整tree、实际新PG/旧双库normal/race、原键与prepared恢复、真实旧writer升级和两个独立轴0遗留见[首票证据](../lerna-03-deterministic-harness/ticket-01-exit-evidence.md)。原1.0合同不扩，新1.1 Decision profile未完整广告，新检查点远端CI尚待核验。

- **原身份与确定性规则：首票范围通过。** 当前鉴权后固定accepted、双摘要去重/冲突、原bytes/refs/用量与Proposal恢复、独立来源及发布读回成立；不裁决Task终态。
- **G2及03验收尚未整体关闭。** 全部候选/依赖反例、取消/限额、真实SIGKILL分别仍由02/03/06完成；目标普通事实/plan迟到不冒称Executor Effect或供应商保证。
- **生命周期证据有分层限制。** sql.OpenDB无物理scope的机械故障与实际正常数据库恢复分列；818PG/256FS登记项全absent不代表旧未知范围全清。断电、生产AZ、真实provider和15第二业务实现均未由此证明。


## 切片03进程恢复票检查点（整片尚未退出）

2026-10-04，06六AC resolved，源a5005ab、交付6904b2d、整合58f8f0f；01／04／05／06合计27/42AC。实际Source发布成功后的原键恢复、Decision真实事务COMMIT前后和独立target COMMIT前后的进程SIGKILL均与正常对照分开核验。pending迟到责任、原scenario／event／cursor以及另一个隔离scenario同seed有限重演有独立target普通Query／Read与Observer事实；不是Executor Effect、供应商查询或断电证据。

原两轴发现已关闭，修后check／base-race／模块／27＋71＋3冻结校验均通过；完整证据与历史边界见[06最终交接](../lerna-03-deterministic-harness/ticket-06-api-handoff.md#最终检查与本票退出)。184外部target FS、44recovery FS、118PG均absent，六local-only另列；旧未知范围保留。02候选特殊恢复、03取消／资源及整个profile／架构／最终CI仍待实际验收，不关闭G2、G3或whole03。


## 切片03有界候选票检查点（整片尚未退出）

2026-10-04，02七AC resolved，本交付树累计34/42AC。产品3d60b6a、完整新PG／原两库回归25287d5、
严格旧driver资格与自身两套真实producer升级正常13.311／race15.403及两轴固定复核8f94f26。
全部五种候选、四独立行动、当前条件／完整processed／真正Source用途消费和原限额有正常与拒绝；
五分支实际lost-reply恢复保原bytes／refs／usage／fee。83新＋158原fixture真实跨语言双向两序通过，
Prepared v1和原published SQL／archives不改；无新wire、DDL或完整profile广告。

逐AC、源码pin、命令及限制见[02退出](../lerna-03-deterministic-harness/ticket-02-exit-evidence.md)。
Source成功关闭后的真正读取失败不是native Close或COMMIT故障；旧driver的strict ErrClaim与有限lease／publication
观察合用，不把sentinel泛化为所有过期。1628独立PG／64targetFS／244recoveryFS／15archive／27确认组及owned root
准确范围absent，七个toolcache晚inventory另列。两轴无遗留不代替root实际整片架构、远端CI或G2／G3验收。


## 切片03取消与限额正式整合（整片尚未退出）

2026-10-04，六票42/42子AC resolved，03受测882e97b／交付8385b9b／整合4b94cb6。真实受信控制、nilInput关闭、两种并发提交顺序、原资源与期限、每项双prepared发布门禁、全部来源及旧writer重开均有[八AC退出证据](../lerna-03-deterministic-harness/ticket-03-exit-evidence.md)。完整真实PG正常、Source race、102项互斥Component有限race及原双库／native恢复正常37.201／race87.979通过。原Component整包120.073超时及旧未知目录仍保留，不扩大成native故障或全环境清理证明。完整profile／分组CI／整片审查与架构／最终CI尚待完成；G2整体、G3及生产指标不因此关闭。


## 切片03完整退出

2026-10-04，六票42AC及原六项、完整1.1四方法/共同黄金、真实DB与104动态race、两轴最终0、架构0必要新重构、准确47ebce1 CI37194868564通过。[证据](../lerna-03-deterministic-harness/exit-evidence.md)映射原身份/候选拒绝/独立效果/窗口不足/重启计划/有限正常对照。A阶段01–03退出；G2仅此本地DB/规则/测试目标范围，G3第二业务实现、Task/Grant/Provider及生产质量容量待后续。120.073超时和未知资源保持，10worktrees清理不代表全环境零。

## 切片04首票检查点（整片未退出）

2026-10-04，首票8AC已resolved，真实有限准确Content从PG accepted到独立对象published/回读、版本冲突/完整声明、当前主体/用途/read-disclose、查询不修补、真实重开及旧reader桥接通过。[证据](../lerna-04-content-snapshots/ticket-01-exit-evidence.md)绑定14ead受测/1a7交付/eba合并。F22杀进程、F12完整上下文、完整来源闭包和跨holder清理仍留后票，未关闭04整片或G5整体；新准确CI待核。
