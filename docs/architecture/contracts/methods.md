# 领域方法签名索引

[线格式](protocol.md) · [机器登记](schemas/methods.json) · [共享 Schema](schemas/protocol.schema.json)

本表是同版登记的 105 个方法的查阅视图。输入、输出定义名指向共享 Schema 的 `$defs`，完整字段以 Schema 为准；成功含义及异常责任按所属模块定义。`条件` 表示 Command 必须携带 expected_revision，Query 不携带。全部方法属于未发布草案。

## brain

[领域规则](../brain/README.md) · [实现设计](../brain/implementation.md)

| 方法 | 种类／条件 | 输入 → 输出 | 回执阶段 |
| --- | --- | --- | --- |
| `brain.cancel` | command | BrainCancelInput → BrainCancelOutput | applied、rejected |
| `brain.decide` | command | BrainDecideInput → BrainDecideOutput | accepted、applied、rejected |
| `brain.get` | query | BrainGetInput → BrainGetOutput | QueryResult／Error |

## collaboration

[领域规则](../collaboration/README.md) · [实现设计](../collaboration/implementation.md)

| 方法 | 种类／条件 | 输入 → 输出 | 回执阶段 |
| --- | --- | --- | --- |
| `collaboration.control` | command／条件 | CollaborationControlInput → CollaborationControlOutput | applied、rejected |
| `collaboration.delegate` | command | CollaborationDelegateInput → CollaborationDelegateOutput | applied、rejected |
| `collaboration.read` | query | CollaborationReadInput → CollaborationReadOutput | QueryResult／Error |
| `collaboration.reconcile` | command | CollaborationReconcileInput → CollaborationReconcileOutput | applied、rejected |
| `collaboration.submit_input` | command | CollaborationSubmit_InputInput → CollaborationSubmit_InputOutput | applied、rejected |

## evaluation

[领域规则](../evaluation/README.md) · [实现设计](../evaluation/implementation.md)

| 方法 | 种类／条件 | 输入 → 输出 | 回执阶段 |
| --- | --- | --- | --- |
| `evaluation.approval_check` | command | EvaluationApproval_CheckInput → EvaluationApproval_CheckOutput | applied、rejected |
| `evaluation.approval_lease` | command | EvaluationApproval_LeaseInput → EvaluationApproval_LeaseOutput | applied、rejected |
| `evaluation.approve` | command | EvaluationApproveInput → EvaluationApproveOutput | applied、rejected |
| `evaluation.cancel` | command／条件 | EvaluationCancelInput → EvaluationCancelOutput | applied、rejected |
| `evaluation.candidate_register` | command | CandidateRegisterInput → CandidateRegisterOutput | applied、rejected |
| `evaluation.exposure_record` | command | ExposureRecordInput → ExposureRecordOutput | applied、rejected |
| `evaluation.feedback_open` | command | FeedbackOpenInput → FeedbackOpenOutput | applied、rejected |
| `evaluation.partition_register` | command | PartitionRegisterInput → PartitionRegisterOutput | applied、rejected |
| `evaluation.plan_create` | command | PlanCreateInput → PlanCreateOutput | applied、rejected |
| `evaluation.read` | query | EvaluationReadInput → EvaluationReadOutput | QueryResult／Error |
| `evaluation.revoke` | command／条件 | EvaluationRevokeInput → EvaluationRevokeOutput | applied、rejected |
| `evaluation.rollout_read` | query | RolloutReadInput → RolloutReadOutput | QueryResult／Error |
| `evaluation.run` | command | EvaluationRunInput → EvaluationRunOutput | applied、rejected |

## execution

[领域规则](../execution/README.md) · [实现设计](../execution/implementation.md)

| 方法 | 种类／条件 | 输入 → 输出 | 回执阶段 |
| --- | --- | --- | --- |
| `capability.describe` | query | CapabilityDescribeInput → CapabilityDescribeOutput | QueryResult／Error |
| `capability.search` | query | CapabilitySearchInput → CapabilitySearchOutput | QueryResult／Error |
| `execution.cancel` | command | ExecutionCancelInput → ExecutionCancelOutput | applied、rejected |
| `execution.control` | command | ExecutionControlInput → ExecutionControlOutput | accepted、applied、rejected |
| `execution.control.get` | query | ExecutionControlGetInput → ExecutionControlGetOutput | QueryResult／Error |
| `execution.get` | query | ExecutionGetInput → ExecutionGetOutput | QueryResult／Error |
| `execution.list` | query | ExecutionListInput → ExecutionListOutput | QueryResult／Error |
| `execution.invoke` | command | ExecutionInvokeInput → ExecutionInvokeOutput | applied、rejected |
| `execution.reconcile` | command | ExecutionReconcileInput → ExecutionReconcileOutput | applied、rejected |
| `resource.acquire` | command | ResourceAcquireInput → ResourceAcquireOutput | applied、rejected |
| `resource.get` | query | ResourceGetInput → ResourceGetOutput | QueryResult／Error |
| `resource.observe` | command | ResourceObserveInput → ResourceObserveOutput | applied、rejected |
| `resource.release` | command | ResourceReleaseInput → ResourceReleaseOutput | applied、rejected |
| `resource.renew` | command／条件 | ResourceRenewInput → ResourceRenewOutput | applied、rejected |
| `resource.takeover` | command | ResourceTakeoverInput → ResourceTakeoverOutput | applied、rejected |

## extensions

[领域规则](../extensions/README.md) · [实现设计](../extensions/implementation.md)

| 方法 | 种类／条件 | 输入 → 输出 | 回执阶段 |
| --- | --- | --- | --- |
| `extensions.activate` | command | ExtensionsActivateInput → ExtensionsActivateOutput | applied、rejected |
| `extensions.deactivate` | command／条件 | ExtensionsDeactivateInput → DeactivationResult | applied、rejected |
| `extensions.dispose` | command／条件 | ExtensionsDisposeInput → ExtensionsDisposeOutput | applied、rejected |
| `extensions.prepare` | command | ExtensionsPrepareInput → ExtensionsPrepareOutput | applied、rejected |
| `extensions.read` | query | ExtensionsReadInput → ExtensionsReadOutput | QueryResult／Error |
| `extensions.list` | query | ExtensionsListInput → ExtensionsListOutput | QueryResult／Error |

## interaction

[领域规则](../interaction/README.md) · [实现设计](../interaction/implementation.md)

| 方法 | 种类／条件 | 输入 → 输出 | 回执阶段 |
| --- | --- | --- | --- |
| `interaction.application_event` | command | InteractionApplication_EventInput → InteractionApplication_EventOutput | applied、rejected |
| `interaction.input` | command | InteractionInputInput → InteractionInputOutput | applied、rejected |
| `interaction.input_read` | query | InteractionInput_ReadInput → InteractionInput_ReadOutput | QueryResult／Error |
| `interaction.input_withdraw` | command／条件 | InteractionInput_WithdrawInput → InteractionInput_WithdrawOutput | applied、rejected |
| `interaction.present` | command／条件 | InteractionPresentInput → InteractionPresentOutput | applied、rejected |
| `interaction.request_read` | query | InteractionRequest_ReadInput → InteractionRequest_ReadOutput | QueryResult／Error |
| `interaction.surface_create` | command | InteractionSurface_CreateInput → InteractionSurface_CreateOutput | applied、rejected |
| `interaction.surface_list` | query | InteractionSurface_ListInput → InteractionSurface_ListOutput | QueryResult／Error |
| `interaction.surface_read` | query | InteractionSurface_ReadInput → InteractionSurface_ReadOutput | QueryResult／Error |
| `interaction.surface_update` | command／条件 | InteractionSurface_UpdateInput → InteractionSurface_UpdateOutput | applied、rejected |

## memory

[领域规则](../memory/README.md) · [实现设计](../memory/implementation.md)

| 方法 | 种类／条件 | 输入 → 输出 | 回执阶段 |
| --- | --- | --- | --- |
| `content.close` | command／条件 | ContentCloseInput → ContentCloseOutput | applied、rejected |
| `content.get` | query | ContentGetInput → ContentGetOutput | QueryResult／Error |
| `content.put` | command | ContentPutInput → ContentPutOutput | applied、rejected |
| `content.register_copy` | command | ContentRegister_CopyInput → ContentRegister_CopyOutput | applied、rejected |
| `content.release_copy` | command | ContentRelease_CopyInput → ContentRelease_CopyOutput | applied、rejected |
| `memory.cleanup.get` | query | MemoryCleanupGetInput → MemoryCleanupGetOutput | QueryResult／Error |
| `memory.create` | command | MemoryCreateInput → MemoryCreateOutput | applied、rejected |
| `memory.delete` | command／条件 | MemoryDeleteInput → MemoryDeleteOutput | applied、rejected |
| `memory.extract` | command | MemoryExtractInput → MemoryExtractOutput | applied、rejected |
| `memory.inspect` | query | MemoryInspectInput → MemoryInspectOutput | QueryResult／Error |
| `memory.list` | query | MemoryListInput → MemoryListOutput | QueryResult／Error |
| `memory.query` | query | MemoryQueryInput → MemoryQueryOutput | QueryResult／Error |
| `memory.read` | query | MemoryReadInput → MemoryReadOutput | QueryResult／Error |
| `memory.replace` | command／条件 | MemoryReplaceInput → MemoryReplaceOutput | applied、rejected |
| `memory.restrict` | command／条件 | MemoryRestrictInput → MemoryRestrictOutput | applied、rejected |
| `memory.view.ack` | command | MemoryViewAckInput → MemoryViewAckOutput | applied、rejected |
| `memory.view.open` | command | MemoryViewOpenInput → MemoryViewOpenOutput | applied、rejected |
| `memory.view.pull` | query | MemoryViewPullInput → MemoryViewPullOutput | QueryResult／Error |

## security

[领域规则](../security/README.md) · [实现设计](../security/implementation.md)

| 方法 | 种类／条件 | 输入 → 输出 | 回执阶段 |
| --- | --- | --- | --- |
| `confirmation.decide` | command／条件 | ConfirmationDecideInput → ConfirmationRecord | applied、rejected |
| `confirmation.read` | query | ConfirmationReadInput → ConfirmationRecord | QueryResult／Error |
| `confirmation.request` | command | ConfirmationRequestInput → ConfirmationRecord | applied、rejected |
| `endpoint.pair.approve` | command／条件 | PairApproveInput → PairApproveOutput | applied、rejected |
| `endpoint.pair.begin` | command | PairBeginInput → PairBeginOutput | applied、rejected |
| `endpoint.pair.claim` | command | PairClaimInput → PairClaimOutput | applied、rejected |
| `endpoint.revoke` | command／条件 | EndpointRevokeInput → EndpointRevokeOutput | applied、rejected |
| `grant.check` | query | GrantCheckInput → GrantCheckOutput | QueryResult／Error |
| `grant.issue` | command | GrantIssueInput → GrantIssueOutput | applied、rejected |
| `grant.lease.allocate` | command | LeaseAllocationInput → LeaseAllocationOutput | applied、rejected |
| `grant.lease.settle` | command／条件 | LeaseSettlementInput → LeaseSettlementOutput | applied、rejected |
| `grant.read` | query | GrantReadInput → GrantReadOutput | QueryResult／Error |
| `grant.list` | query | GrantListInput → GrantListOutput | QueryResult／Error |
| `grant.revoke` | command／条件 | GrantRevokeInput → GrantRevokeOutput | applied、rejected |
| `grant.use` | command | GrantUseInput → GrantUseOutput | applied、rejected |
| `grant.use.get` | query | GrantUseGetInput → GrantUseGetOutput | QueryResult／Error |
| `grant.use.settle` | command／条件 | UseSettlementInput → UseSettlementRecord | applied、rejected |
| `grant.use.settlement` | query | UseSettlementQueryInput → UseSettlementRecord | QueryResult／Error |

## orchestrator

[领域规则](../orchestrator/README.md) · [实现设计](../orchestrator/implementation.md)

| 方法 | 种类／条件 | 输入 → 输出 | 回执阶段 |
| --- | --- | --- | --- |
| `budget.allocate` | command | BudgetAllocateInput → BudgetAllocateOutput | applied、rejected |
| `budget.close` | command | BudgetCloseInput → BudgetCloseOutput | applied、rejected |
| `budget.read` | query | BudgetReadInput → BudgetReadOutput | QueryResult／Error |
| `budget.settle` | command／条件 | BudgetSettleInput → BudgetSettleOutput | applied、rejected |
| `task.accept_result` | command | TaskAccept_ResultInput → TaskAccept_ResultOutput | applied、rejected |
| `task.adjust_budget` | command／条件 | TaskAdjustBudgetInput → TaskAdjustBudgetOutput | applied、rejected |
| `task.attach_evidence` | command | TaskAttach_EvidenceInput → TaskAttach_EvidenceOutput | applied、rejected |
| `task.billing_reconcile` | command | TaskBilling_ReconcileInput → TaskBilling_ReconcileOutput | applied、rejected |
| `task.cancel` | command／条件 | TaskCancelInput → TaskCancelOutput | applied、rejected |
| `task.input` | command | TaskInputInput → TaskInputOutput | applied、rejected |
| `task.list` | query | TaskListInput → TaskListOutput | QueryResult／Error |
| `task.pause` | command／条件 | TaskPauseInput → TaskPauseOutput | applied、rejected |
| `task.read` | query | TaskReadInput → TaskReadOutput | QueryResult／Error |
| `task.result` | query | TaskResultInput → TaskResultOutput | QueryResult／Error |
| `task.resume` | command／条件 | TaskResumeInput → TaskResumeOutput | applied、rejected |
| `task.revise` | command／条件 | TaskReviseInput → TaskReviseOutput | applied、rejected |
| `task.submit` | command | TaskSubmitInput → TaskSubmitOutput | applied、rejected |
