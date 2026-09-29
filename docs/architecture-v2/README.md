# Harness 架构

Harness 接纳用户目标，组织大脑、记忆、工具和其他 Agent 持续工作，并让每次行动、外部效果和完成判断都有明确负责方。本系列面向未参与设计讨论的高级工程师，按实现责任组织，完整阅读不需要其他架构文档。

## 从哪里开始

先读[系统主线](overview.md)，了解一次任务的交接与事务边界；再读[任务编排](orchestrator.md)和[完成验证](verification.md)，然后进入负责的模块。精确消息字段查本目录 [Schema](contracts/README.md)，领域词义遵循根 [CONTEXT.md](../../CONTEXT.md)。

| 阅读目的 | 文档 |
| --- | --- |
| 理解任务从目标到结果 | [系统主线](overview.md)、[范围与目标](goals.md) |
| 实现任务、条件和预算 | [任务编排](orchestrator.md)、[完成验证](verification.md) |
| 实现提案和行动 | [大脑](brain.md)、[执行](execution.md) |
| 实现信息与许可 | [记忆和内容](memory.md)、[授权](authorization.md) |
| 实现用户与其他 Agent 的交接 | [交互](interaction.md)、[协作](collaboration.md) |
| 实现安装、评测与发布 | [扩展宿主](extensions.md)、[评测治理](evaluation.md) |
| 实现共用持久机制 | [可靠接纳与持久工作](reliability.md) |
| 连接独立实现 | [调用契约](contracts/README.md)、[方法索引](contracts/methods.md)、[编码](contracts/protocol.md)、[WSS](contracts/transport.md)、[gRPC](contracts/grpc.md) |
| 装配、扩容与恢复 | [部署存储](deployment.md)、[容量指标](capacity.md)、[工程顺序](engineering.md) |
| 评审和验收 | [验收规则](validation/README.md)、[故障实验](validation/fault-experiments.md)、[待决问题](open-questions.md)、[实现与验证记录](review.md) |

## 规则在哪里定义

每项规则在下表指定的位置定义，其他文档只解释交接并链接。模块正文给出正常路径、状态、事务和失败处理；保证强度、前提及限制集中在所属文档的「保证与限制」。实现与验证状态只记录于 [review](review.md)。

| 规则 | 唯一维护位置 |
| --- | --- |
| 任务状态、控制、行动准入、计划实例化、预算与费用归并 | [orchestrator](orchestrator.md) |
| 条件、目标覆盖、检查结果、证据适用性、完成依据 | [verification](verification.md) |
| 原命令、回执、错误、幂等和最小终态记录 | [contracts](contracts/README.md) |
| 公共短事务、JobStore、领取与新责任竞争 | [reliability](reliability.md) |
| 当前身份、Grant、Confirmation、Use 与离线结算 | [authorization](authorization.md) |
| Content、来源、copy、Memory 版本与变化水位 | [memory](memory.md) |
| 实际启动、资源互斥、效果与安全重试 | [execution](execution.md) |
| 用户来源目录、物理存储、进程租约及故障恢复 | [deployment](deployment.md) |
| 运行限额、容量假设与 SLO 口径 | [capacity](capacity.md) |
| 正式样本、统计门槛和实验断言 | [validation](validation/README.md) |
| 字段类型、枚举、必填项与方法登记 | [contracts/schemas](contracts/README.md) |

文件中的内部接口和表是参考实现设计，只有方法登记列出的接口属于线协议。协议资产和验证器随本系列保存；文档写明的行为与机器字段发生冲突时，列入待决问题并停止发布冲突组合，不由实现者自行放宽。
