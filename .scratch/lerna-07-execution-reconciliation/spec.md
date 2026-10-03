# 07 · 受管写入与未知效果核对

Status: ready-for-agent
Phase: B
Implementation: not-started
Depends on: [06](../lerna-06-authorized-admission/spec.md)

## Problem Statement

报告可能已经写入，但 Executor 在保存结果前崩溃。用户需要恢复原效果，不希望重试按钮或 worker 接替造成第二次写入，也不能接受把未知当失败后盲目重做。

## Solution

交付受管文件 Executor 与可恢复的 Operation。系统在当前授权、预算和资源门禁内写入准确内容，沿原身份查询或读回，分别报告效果与是否仍可能迟到。

## User Stories

1. As a user, I want exact authorized writes, so that the saved artifact matches the approved action.
2. As a user, I want lost write responses reconciled, so that recovery does not repeat an uncertain effect.
3. As a user, I want unknown effects visible, so that a timeout is not mistaken for no action.
4. As a caller, I want stable operation identities, so that worker replacement does not create another action.
5. As a resource owner, I want start-time authorization checks, so that queued work cannot bypass revocation.
6. As a user, I want cancellation tombstones, so that delayed invocation cannot restart cancelled work.
7. As an operator, I want occupancy and isolation evidence, so that a stale executor cannot race a replacement.
8. As an application developer, I want immutable acceptance receipts, so that later execution failure is queried separately.
9. As a user, I want independent readback, so that a successful API call is not the only proof of saving.

## Implementation Decisions

- 实现 executor.invoke、get、reconcile、cancel、apply_control；保存 Operation、Attempt、EffectObservation、TaskGate 与 ResourceClaim。阶段与 effect、may_apply_later 分开。
- OperationIntent 已由 06 固定；Executor 接纳和发送前记录各在自己的事务提交。实际发送前复核准确使用、资源许可、TaskGate、绑定就绪与占用。
- 受管文件驱动限制获准根目录、规范路径、符号链接和覆盖权限，尽可能原子替换，核验耐久和准确版本摘要；读取同样走许可、限额与使用记录。
- 外部写入与本地事实无原子提交保证。发送前记录之后出现不确定即核对原操作；只有目标幂等窗口或可信未发送且不会迟到的证据才允许安全 Attempt。
- 效果归并只接受已认证目标证据，保留冲突观察；不能按设备时间戳取最后一条。unknown 与 may_apply_later 不被超时强行关闭。
- 取消先于 invoke 时验证原 Task owner 凭据与绑定摘要，持久保存墓碑；高控制修订覆盖低修订，同修订异摘要冲突，恢复不能复活已关闭 Operation。
- 资源租约到期不足以允许冲突新输入，必须有旧实例隔离或真实退出依据；固定静态组件也要有准确制品、批准、就绪和 holder，动态生命周期由 16 扩展。
- Orchestrator 派发前已登记责任，Executor 回执丢失不移除该登记；跨 owner 结果通过原对象修订归并。

## Testing Decisions

以 Application 行动流程为主，辅以公开 Executor 合同；在真实 PG / SQLite、真实受管目录和 03 的可控目标上测试。目标字节、版本、效果次数由独立观察核实。

测试只断言公开行为、持久恢复结果和独立目标状态；不依赖私有表布局或内部调用次数。每个故障或拒绝案例必须配允许正常完成的对照。

验收条件：

1. 合法报告写入获准位置并独立读回相同摘要；路径越界、符号链接逃逸和未授权覆盖无实际效果。
2. 写入后、结果落库前杀 Executor，恢复只核对原 Operation；幂等目标不产生重复效果，不可查询目标保持 unknown 并阻止同义盲重做。
3. 已 accepted 后驱动报错，原回执不变，get 返回失败或未知事实；调用方不新建同义操作。
4. 先送 cancel 再送 invoke，原绑定墓碑拒绝迟到启动；伪造 owner 或不同意图摘要不能关闭他人操作。
5. 高版本暂停后送低版本允许，低版本拒绝；当前允许窗口也不能复活已关闭 Operation。
6. 准入后撤权、窗口过期或就绪失效时，真实发送被阻止；仍有效的合法窗口可完成，不能靠拒绝全部动作通过。
7. 旧执行实例未被隔离时，不允许冲突输入；独立核对完成且无迟到可能后才关闭效果责任。

8. 四项相互独立行动全部获准后，让一项外部执行失败：各 Operation 分别保存效果，原准入事实不变；已成功或未知的其他项不盲目重做，由新 Decision 根据真实反馈重新规划。

## Out of Scope

- 任意第三方恰好一次、撤销历史效果、无证据自动重试。
- GUI 与任意代码沙箱驱动；本切片只提供受管文件及合同测试驱动。

## Further Notes

真实写入从本切片开始开放，必须同时具备 06 的准入与本切片启动门禁。文件系统的具体持久保证需在目标平台记录，不能把模拟目标保证套用到真实文件系统。

依赖项表示实现先决条件；`ready-for-agent` 表示规格已明确，不表示依赖已完成或能力已开放。全部验收通过并附准确版本、环境、命令、结果和限制后，才可将本切片记为完成。共同执行与证据规则见[切片索引](../lerna-implementation/README.md)。

依据：[runtime](../../docs/architecture/runtime.md#外部效果的四个阶段)、[runtime](../../docs/architecture/runtime.md#跨-owner-交接)、[capabilities](../../docs/architecture/capabilities.md#executor-统一管理实际行动)、[contracts](../../docs/architecture/contracts.md#能力模块方法)、[ADR-0003](../../docs/adr/0003-stable-owner-distributed-deployment.md)、[ADR-0005](../../docs/adr/0005-explicit-effect-uncertainty.md)、[ADR-0006](../../docs/adr/0006-authorization-budget-at-action-boundaries.md)、[ADR-0009](../../docs/adr/0009-versioned-activation-and-recovery.md)。
