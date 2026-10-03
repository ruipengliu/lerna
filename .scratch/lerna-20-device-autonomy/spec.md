# 20 · 独立设备目标与有限离线执行

Status: ready-for-agent
Phase: E
Implementation: not-started
Depends on: [13](../lerna-13-distributed-runtime/spec.md)、[16](../lerna-16-activation-lifecycle/spec.md)、[19](../lerna-19-bounded-delegation/spec.md)

## Problem Statement

设备断网时可能仍需推进本机目标，但直接接管云端 Task 会产生双重权威；用户接管资源和不可信时钟也会让已签离线许可不再适用。

## Solution

提供显式启用的独立设备 Orchestrator、自己的 Task 账本与有限离线策略，可通过委派与云端协作。设备使用本地资源门禁并在重连后核对原控制、效果与用量。

## User Stories

1. As a device user, I want explicit autonomous local tasks, so that offline work has a clear owner.
2. As a user, I want cloud tasks kept separate, so that losing connectivity cannot create two authorities for one goal.
3. As a user, I want finite offline grants, so that a disconnected device cannot expand its actions.
4. As a device user, I want immediate resource takeover, so that automated actions stop competing with my input.
5. As an operator, I want restart to preserve original expirations, so that rebooting cannot renew permissions.
6. As a user, I want synchronization purposes checked, so that local material is not automatically uploaded.
7. As a user, I want reconnection to reconcile old effects first, so that new work does not hide unresolved actions.
8. As an operator, I want invalid time or fencing to block starts, so that unavailable evidence is not treated as permission.

## Implementation Decisions

- 设备自主模式必须显式部署独立 Orchestrator 和自有 owner / Task；复用内部运行合同与本地 SQLite 耐久存储，绝不因失联接管云端原 Task。
- 设备自有 Task 使用完整条件、控制、许可、预算、Result 与收尾语义；云端协作沿 19 的 Delegation 建立父子责任，不共享同一 Task 写权。
- 有限离线 GrantUse 固定动作、额度、截止和本机门禁，重启不延长期限，不继承旧实例就绪；需要在线当前许可的能力断网等待。
- 本机用户接管首先关闭新自动输入，再核对已发动作和 ResourceClaim；恢复冲突资源必须有旧实例隔离或真实退出依据。
- 本切片使用受管本地资源验证接管，不隐式开放 GUI 驱动。设备任务可以使用已批准规则组件，无网络模型时显示能力限制。
- 重连按身份与版本、撤权与控制、原效果与累计用量、最后新行动的顺序恢复，不按设备墙钟排序业务事实。
- 资料同步独立核验用途与来源交集，离线残留与清理期限可见；设备列表和云端目录聚合保持 owner 区别。

## Testing Decisions

在云端与设备两个真实进程组中，通过各自 Application 合同和公开 Delegation 操作验证。设备使用真实 SQLite、隔离网络与受控本地资源；复用 13 重连与 19 父子账本套件。

测试只断言公开行为、持久恢复结果和独立目标状态；不依赖私有表布局或内部调用次数。每个故障或拒绝案例必须配允许正常完成的对照。

验收条件：

1. 断开云端网络不会让设备写云端 Task 终态；显式创建的本机 Task 有不同 owner 和身份，可在许可范围内完成。
2. 离线窗口到期、时钟不可信或需在线核验时停止新启动；合法有限窗口仍可执行受管动作。
3. 设备重启后原许可截止不延长，原费用和效果不丢失，新实例必须重新就绪。
4. 用户接管先封自动输入，迟到动作仍核对；旧实例未隔离时不允许冲突新输入。
5. 重连先取得更高控制与撤权，再补传原事实，新行动不得抢在同步之前启动。
6. 跨端委派费用留在原根分配，未知子消费不释放；没有同步许可的材料不上传。
7. 设备和云端都能查询各自准确 Result、限制及未结责任，不以一个合并 done 隐去未知。

## Out of Scope

- 移动平台 GUI 驱动、设备集群统一控制面。
- 设备无期限自治、云端 Task 自动故障迁移。

## Further Notes

该能力是 E 的显式可选开放项，默认端云模式仍仅由设备执行。启用本机自主模式必须通过本切片全部合同与恢复验收。

依赖项表示实现先决条件；`ready-for-agent` 表示规格已明确，不表示依赖已完成或能力已开放。全部验收通过并附准确版本、环境、命令、结果和限制后，才可将本切片记为完成。共同执行与证据规则见[切片索引](../lerna-implementation/README.md)。

依据：[deployment](../../docs/architecture/deployment.md#默认端云行为)、[governance](../../docs/architecture/governance.md#撤销传播与离线执行)、[capabilities](../../docs/architecture/capabilities.md#executor-统一管理实际行动)、[ADR-0003](../../docs/adr/0003-stable-owner-distributed-deployment.md)、[ADR-0005](../../docs/adr/0005-explicit-effect-uncertainty.md)、[ADR-0006](../../docs/adr/0006-authorization-budget-at-action-boundaries.md)、[ADR-0009](../../docs/adr/0009-versioned-activation-and-recovery.md)。
