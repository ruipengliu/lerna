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
