# 核心对象与语义：关键结论

研究日期：2026-10-01。本页保留五项目比较得到的对象组织与语义边界；当前接口、字段和开放范围以[整体数据设计](../../architecture/data/README.md)及[实施覆盖清单](../../architecture/engineering/implementation-readiness.md#coverage)为准。设计对应关系不等于可运行能力。

<a id="core-model-scope"></a>
## 1 六组对象组织共同主线

| 对象 | 负责的事实 | 需要保持的边界 |
| --- | --- | --- |
| Session | 消息、分支、输入队列与任务关联 | 归档会话不取消 Task；输入接纳和业务消费分开 |
| Task | 目标、条件、控制、预算、进展与完成 | 固定 Orchestrator 裁决；Task 可以独立于 Session |
| Decision | 固定输入下的一次决策及原模型请求 | 每 Decision 至多一次物理模型请求；辅助推理和重试另行登记 |
| Operation | 获准行动的尝试、实际效果和继续核对责任 | 原意图由 Orchestrator 保存，效果由 Executor 核对；unknown 不等于未发生 |
| Content | 准确版本的正文与来源 | 引用复用不等于拥有使用权；关闭、保留和副本清理分别成立 |
| Grant | 资源、用途和期限内的许可与使用 | 业务 Confirmation 仍由实际业务负责方一次消费；批准不证明效果发生 |

六组对象不限定六张表或六个服务。Capability、Binding、RuleDefinition、InstallLock 等配置与有独立生命周期的内部记录仍需保留。

## 2 十六类语义的落位

| 原研究编号 / 语义 | 保留的结论 | 当前设计入口 |
| --- | --- | --- |
| SM-01 会话历史 | 保存原输入、顺序、来源和 Task 关联，历史展示不裁决任务 | [交互](../../architecture/interaction/README.md) |
| SM-02 Turn / Run | 回合结束、工作进程退出与目标完成分别表达；避免平行目标状态机 | [任务编排](../../architecture/orchestrator/README.md) |
| SM-03 输入调度 | 新目标、steer 和原请求回答分开；单输入撤回不等于取消整个 Task | [输入](../../architecture/interaction/README.md#2-三种自由输入有明确语义) |
| SM-04 压缩与视图 | 摘要是派生材料，硬约束和未知效果从权威事实恢复 | [Brain](../../architecture/brain/README.md) |
| SM-05 模型步骤 | 输入、真实物理调用、产出与费用有原身份，关闭透明重试 | [模型出口](../../architecture/brain/README.md#3-模型请求只有一个真实出口) |
| SM-06 工具效果 | Attempt、Effect、结果和结算分别记录；未知结果沿原键核对 | [执行](../../architecture/execution/README.md) |
| SM-07 批准与隔离 | Grant、本人确认、安装声明和沙箱能力分别核验 | [安全](../../architecture/security/README.md) |
| SM-08 持续目标 | active、持久工作存在、当前允许新行动分别成立 | [有界推进](../../architecture/orchestrator/README.md#7-有限自主推进) |
| SM-09 分支与回退 | 改变未来上下文不撤销历史效果，也不复制未结责任 | [分支](../../architecture/interaction/README.md#5-分支只改变未来上下文) |
| SM-10 子 Agent | 唯一子映射、结果范围、控制、效果关闭和费用各自核对 | [协作](../../architecture/collaboration/README.md) |
| SM-11 定时触发 | 规则、触发 occurrence 与创建出的 Task 各有身份和生命周期 | [Schedule](../../architecture/interaction/README.md#6-未来和周期规则) |
| SM-12 执行环境 | cell 与 hostcall 映射可恢复，首版不重放已开始 cell | [环境](../../architecture/execution/README.md#7-程序化工具与可复用环境) |
| SM-13 长期记忆 | 取得独立保存许可；来源、当前权限和派生清理贯穿使用 | [Memory](../../architecture/memory/README.md) |
| SM-14 Skill / 插件 | 代码和操作知识不授予权限；准确制品及当前实例就绪才可采用 | [扩展](../../architecture/extensions/README.md) |
| SM-15 流呈现 | 有界通知可以丢失，正式结果从原权威记录读取 | [状态展示](../../architecture/interaction/README.md#4-状态展示和集合恢复) |
| SM-16 冷恢复 | 恢复原状态与重新取得继续执行资格分开，旧领取不得提交 | [Runtime](../../architecture/runtime/README.md) |

## 3 使用这些结论时的限制

参考项目同名 Session、Task、Turn 或 Operation 并不承诺相同语义。尤其 Pi 的经典 CLI、AgentHarness 与独立 durable 路径，以及 DeepSeek 的不同装配，需要分别比较；项目差异见[比较结论](README.md#1-五个项目值得借鉴什么)。

先完成一条请求的输入、准入、效果、核验和交付闭环，再按实际需求开放 Schedule、环境、子会话、动态扩展与正式评测。公开前补齐对应 Schema、SDK、适配器与运行证据；字段可序列化、图示完整或模型检查通过都不能替代这些交付。
