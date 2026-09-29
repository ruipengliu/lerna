# 领域序列查阅

本目录按跨调用观察链组织 JSON 夹具，用于核对同版 [方法登记](../../schemas/methods.json)和 [领域 Schema](../../schemas/protocol.schema.json)。选择最接近待实现流程的序列，再从 `invalid-mutations.json` 查看对应路径与应拒绝规则；领域行为本身只在所属模块定义。

| 工作流 | 代表序列 |
| --- | --- |
| 任务主循环、原命令及控制 | [任务主线](01-task-loop.json)、[原命令](02-original-command.json)、[控制乱序](03-control-order.json)、[先取消后执行](04-cancel-before-invoke.json)、[任务控制](05-task-control.json)、[目标修订](06-goal-revision.json) |
| 输入、验收与完成 | [输入消费](07-input-consumption.json)、[验收](08-acceptance.json)、[完成查询](16-active-to-result.json)、[可选条件](17-optional-condition.json)、[无关效果](18-unrelated-effect.json) |
| Brain、许可及预算恢复 | [Brain](09-brain-recovery.json)、[使用恢复](10-use-recovery.json)、[预算分配](20-budget-allocation.json)、[跨 Orchestrator](23-cross-orchestrator-budget.json)、[终态后费用](60-task-billing-reconcile.json) |
| 能力与设备资源 | [能力目录](21-capability-catalog.json)、[资源生命周期](22-resource-lifecycle.json) |
| 授权与配对 | [Grant](30-grant-lifecycle.json)、[配对](31-pairing-lifecycle.json)、[离线结算](32-lease-settlement.json)、[线上结算](50-online-use-settlement.json)、[零费用 once](51-online-use-once-zero.json)、[lease 封账](58-lease-open-close-ledger.json)、[最终费用更正](61-lease-final-correction.json) |
| 可信确认 | [Grant 确认](52-confirmation-grant.json)、[发布确认](53-confirmation-release.json)、[验收确认](54-confirmation-acceptance.json) |
| 协作与关闭 | [委派映射](12-delegation-mapping.json)、[phase 投影](59-delegation-phase-projection.json) |
| 安装、批准与重启 | [在线激活](13-activation-online.json)、[离线批准](14-approval-offline.json)、[拒绝分支](15-rejected-branches.json)、[安装生命周期](33-installation-lifecycle.json)、[首装兼容](34-compatibility-release.json)、[本地重启](36-local-install-restart.json)、[远端重开](37-remote-instance-reopen.json)、[撤回后启动](38-revoked-approval-startup.json)、[独立旧批准回退](65-approved-rollback.json) |
| 正式评测 | [暴露与撤回](35-improvement-exposure.json)、[取消环境](39-evaluation-cancel.json) |
| 内容与持有者 | [内容生命周期](40-content-lifecycle.json)、[用途收紧](46-content-restriction.json)、[holder 控制](55-content-holder-control.json) |
| 记忆、来源与派生视图 | [记忆管理](11-memory-management.json)、[查询与列表](41-memory-query-list.json)、[提取](42-memory-extraction.json)、[视图同步](43-memory-view-sync.json)、[变化分页](48-memory-changed-pages.json)、[来源投影](49-source-projection.json) |
| Surface 与业务输入 | [Surface](44-surface-lifecycle.json)、[应用投递](45-application-delivery.json)、[声明式表单](47-declarative-form.json) |
| 共同集合恢复 | [owner 集合分页](56-owner-collection-pages.json)、[权限范围改变](57-collection-authorization-reset.json) |

夹具的 auth、capabilities、input_requests、approvals 和 confirmations 是明确提供的权威前提，不是调用者可自报的权限字段。collection、activation_view、delegation_facts、holder_gate 仅表示校验观察，不随 WSS/gRPC 发送。内容、ID、证明和真值也可能是构造占位，合法格式不证明真实授权、字节、签名或效果。

反例应命中它所声称破坏的关系，不能靠无关格式错误算通过；校验器的额外有界构造用于检查超过单页影响、账单原因和最终更正关系。执行入口、最新覆盖与运行证据统一见 [review.md](../../../review.md)。[Brain 内部构造](../brain/README.md)与 [完成投影](../README.md)使用独立边界。
