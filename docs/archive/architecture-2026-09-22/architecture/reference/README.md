# 契约参考与实现细则

本目录是当前字段、接口、状态、故障和配置的维护位置。正文解释原因与过程，细则补充实现顺序及局部条件，综合附录维护完整清单。查阅时同时核对适用版本、权限、原子提交和未知结果条件。

归档链接仅用于追溯设计来源，不继续承担现行规格维护责任。发现内容实质冲突时记录到[设计缺口](../maintenance/open-questions.md)，不得按文件版本自动覆盖。

| 查阅资料 | 主要内容 |
| --- | --- |
| [附录 01：对象、标识与字段](01-data-model.md) | 对象关系、写入方、标识与版本、原子组、回执及保留边界。 |
| [附录 02：接口、消息与兼容配置](02-api-and-message-catalog.md) | 原生服务方法、可替换组件接口、错误与恢复、UI 消息、固定协议及依赖配置。 |
| [附录 03：状态转换与故障检查表](03-state-and-failure-matrices.md) | 状态与阶段、故障初态、允许与禁止结果、核对证据和用例编号。 |
| [附录 04：容量演算与存储预算](04-capacity-model.md) | 连接、任务、模型、事务、WAL、分层存储、热点和恢复积压的演算及报告模板。 |
| [附录 05：验收配置与能力追溯](05-validation-profiles-and-traceability.md) | 能力与成熟度、完整样本和预算、专项判分、统计比较、发布及支持声明。 |

## 各章实现细则

正文先解释职责、正常过程与判断条件。以下资料集中补足实现顺序、局部限制和验证场景；字段、方法全集与固定配置仍查上方五篇附录。

| 正文 | 配套资料 |
| --- | --- |
| [2.1 任务运行内核](../main/02-subsystems/01-runtime-kernel.md) | [提交与调度](runtime-details.md) |
| [2.2 上下文、记忆与证据](../main/02-subsystems/02-context-and-memory.md) | [接口、存储与副本清理](memory-details.md) |
| [2.3 规划、决策与模型调用](../main/02-subsystems/03-planning-and-decision.md) | [候选、上下文与停止参考策略](decision-details.md) |
| [2.4 能力与执行](../main/02-subsystems/04-tools-and-devices.md) | [目录、Driver、资源控制与验证](execution-details.md) |
| [2.5 应用与交互](../main/02-subsystems/05-interaction.md) | [可靠输入、控制、窗口恢复及扩展](interaction-and-task-control-details.md) |
| [2.6 身份与授权](../main/02-subsystems/06-identity-and-authorization.md) | [认证、签发、消费、离线及凭证](identity-and-authorization-details.md) |
| [2.8 扩展与运行保障](../main/02-subsystems/08-extensions-and-runtime.md) | [资产、协议、运行器与验证](extension-details.md) |
| [3.3 多 Agent 协作](../main/03-coordination/03-agent-collaboration.md) | [控制传播与协作验证](collaboration-details.md) |
| [3.4 端云协同与离线运行](../main/03-coordination/04-edge-cloud-coordination.md) | [可靠流与跨节点验证](edge-cloud-details.md) |
| [4.2 部署、存储与规模扩展](../main/04-engineering/02-deployment-and-storage.md) | [入口认证与用户隔离](deployment-details.md) |
| [4.3 故障恢复与运行保障](../main/04-engineering/03-reliability-and-recovery.md) | [协调阶段、工作恢复与 HA 配置](recovery-details.md) |
| [4.1 技术选型与开发组装](../main/04-engineering/01-implementation-and-technology.md) | [包组织、第二后端与依赖锁定](implementation-details.md) |
| [4.4 评测、比较与发布](../main/04-engineering/04-evaluation-and-release.md) | [证据、统计方法与保留期限](evaluation-details.md) |
| [4.5 实施路线与能力验收](../main/04-engineering/05-roadmap-and-validation.md) | [独立替换证据](roadmap-details.md) |

《规划、决策与模型调用》的[决策参考策略](decision-details.md)保留候选召回、上下文、停止阈值及其比较方法。参数属于待验证建议，与既定验收上限分开。
