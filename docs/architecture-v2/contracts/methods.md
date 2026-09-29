# 方法查阅索引

本索引从同版 [方法登记](schemas/methods.json) 展开，帮助实现者定位接口；字段以 [protocol.schema.json](schemas/protocol.schema.json) 的 `$defs` 为准。`条件` 表示 Command 必须携带 `expected_revision`，Query 不携带该字段。命令语义见 [共同契约](README.md)，实现与验证状态见 [review.md](../review.md)。

## brain

业务规则由 [brain](../brain.md) 定义；下表只提供签名导航。

| 方法 | 种类 / 条件 | target | 输入 → 输出 | 回执阶段 |
| --- | --- | --- | --- | --- |
| `brain.cancel` | command | `decision_id` | `BrainCancelInput` → `BrainCancelOutput` | applied、rejected |
| `brain.decide` | command | `service_id` | `BrainDecideInput` → `BrainDecideOutput` | accepted、applied、rejected |
| `brain.get` | query | `decision_id` | `BrainGetInput` → `BrainGetOutput` | QueryResult / Error |

## collaboration

业务规则由 [collaboration](../collaboration.md) 定义；下表只提供签名导航。

| 方法 | 种类 / 条件 | target | 输入 → 输出 | 回执阶段 |
| --- | --- | --- | --- | --- |
| `collaboration.control` | command / 条件 | `delegation_id` | `CollaborationControlInput` → `CollaborationControlOutput` | applied、rejected |
| `collaboration.delegate` | command | `payload.parent_task_id` | `CollaborationDelegateInput` → `CollaborationDelegateOutput` | applied、rejected |
| `collaboration.read` | query | `delegation_id` | `CollaborationReadInput` → `CollaborationReadOutput` | QueryResult / Error |
| `collaboration.reconcile` | command | `delegation_id` | `CollaborationReconcileInput` → `CollaborationReconcileOutput` | applied、rejected |
| `collaboration.submit_input` | command | `delegation_id` | `CollaborationSubmit_InputInput` → `CollaborationSubmit_InputOutput` | applied、rejected |

## evaluation

业务规则由 [evaluation](../evaluation.md) 定义；下表只提供签名导航。

| 方法 | 种类 / 条件 | target | 输入 → 输出 | 回执阶段 |
| --- | --- | --- | --- | --- |
| `evaluation.approval_check` | command | `approval_id` | `EvaluationApproval_CheckInput` → `EvaluationApproval_CheckOutput` | applied、rejected |
| `evaluation.approval_lease` | command | `payload.approval_id` | `EvaluationApproval_LeaseInput` → `EvaluationApproval_LeaseOutput` | applied、rejected |
| `evaluation.approve` | command | `evaluation_service_id` | `EvaluationApproveInput` → `EvaluationApproveOutput` | applied、rejected |
| `evaluation.cancel` | command / 条件 | `run_id` | `EvaluationCancelInput` → `EvaluationCancelOutput` | applied、rejected |
| `evaluation.candidate_register` | command | `evaluation_service_id` | `CandidateRegisterInput` → `CandidateRegisterOutput` | applied、rejected |
| `evaluation.exposure_record` | command | `evaluation_service_id` | `ExposureRecordInput` → `ExposureRecordOutput` | applied、rejected |
| `evaluation.feedback_open` | command | `report_id` | `FeedbackOpenInput` → `FeedbackOpenOutput` | applied、rejected |
| `evaluation.partition_register` | command | `evaluation_service_id` | `PartitionRegisterInput` → `PartitionRegisterOutput` | applied、rejected |
| `evaluation.plan_create` | command | `evaluation_service_id` | `PlanCreateInput` → `PlanCreateOutput` | applied、rejected |
| `evaluation.read` | query | `object_id` | `EvaluationReadInput` → `EvaluationReadOutput` | QueryResult / Error |
| `evaluation.revoke` | command / 条件 | `approval_id` | `EvaluationRevokeInput` → `EvaluationRevokeOutput` | applied、rejected |
| `evaluation.rollout_read` | query | `rollout_id` | `RolloutReadInput` → `RolloutReadOutput` | QueryResult / Error |
| `evaluation.run` | command | `payload.plan_id` | `EvaluationRunInput` → `EvaluationRunOutput` | applied、rejected |

## execution

业务规则由 [execution](../execution.md) 定义；下表只提供签名导航。

| 方法 | 种类 / 条件 | target | 输入 → 输出 | 回执阶段 |
| --- | --- | --- | --- | --- |
| `capability.describe` | query | `executor_id` | `CapabilityDescribeInput` → `CapabilityDescribeOutput` | QueryResult / Error |
| `capability.search` | query | `executor_id` | `CapabilitySearchInput` → `CapabilitySearchOutput` | QueryResult / Error |
| `execution.cancel` | command | `executor_id` | `ExecutionCancelInput` → `ExecutionCancelOutput` | applied、rejected |
| `execution.control` | command | `payload.executor_id` | `ExecutionControlInput` → `ExecutionControlOutput` | accepted、applied、rejected |
| `execution.control.get` | query | `executor_id` | `ExecutionControlGetInput` → `ExecutionControlGetOutput` | QueryResult / Error |
| `execution.get` | query | `operation_id` | `ExecutionGetInput` → `ExecutionGetOutput` | QueryResult / Error |
| `execution.invoke` | command | `payload.control_snapshot.executor_id` | `ExecutionInvokeInput` → `ExecutionInvokeOutput` | applied、rejected |
| `execution.reconcile` | command | `operation_id` | `ExecutionReconcileInput` → `ExecutionReconcileOutput` | applied、rejected |
| `resource.acquire` | command | `resource_id` | `ResourceAcquireInput` → `ResourceAcquireOutput` | applied、rejected |
| `resource.get` | query | `resource_id` | `ResourceGetInput` → `ResourceGetOutput` | QueryResult / Error |
| `resource.observe` | command | `payload.resource_id` | `ResourceObserveInput` → `ResourceObserveOutput` | applied、rejected |
| `resource.release` | command | `resource_id` | `ResourceReleaseInput` → `ResourceReleaseOutput` | applied、rejected |
| `resource.renew` | command / 条件 | `resource_id` | `ResourceRenewInput` → `ResourceRenewOutput` | applied、rejected |
| `resource.takeover` | command | `resource_id` | `ResourceTakeoverInput` → `ResourceTakeoverOutput` | applied、rejected |
| `execution.list` | query | `owner_id` | `ExecutionListInput` → `ExecutionListOutput` | QueryResult / Error |

## extensions

业务规则由 [extensions](../extensions.md) 定义；下表只提供签名导航。

| 方法 | 种类 / 条件 | target | 输入 → 输出 | 回执阶段 |
| --- | --- | --- | --- | --- |
| `extensions.activate` | command | `payload.target_id` | `ExtensionsActivateInput` → `ExtensionsActivateOutput` | applied、rejected |
| `extensions.deactivate` | command / 条件 | `target_id` | `ExtensionsDeactivateInput` → `DeactivationResult` | applied、rejected |
| `extensions.dispose` | command / 条件 | `lock_id` | `ExtensionsDisposeInput` → `ExtensionsDisposeOutput` | applied、rejected |
| `extensions.prepare` | command | `extension_manager_id` | `ExtensionsPrepareInput` → `ExtensionsPrepareOutput` | applied、rejected |
| `extensions.read` | query | `activation_id` | `ExtensionsReadInput` → `ExtensionsReadOutput` | QueryResult / Error |
| `extensions.list` | query | `owner_id` | `ExtensionsListInput` → `ExtensionsListOutput` | QueryResult / Error |

## interaction

业务规则由 [interaction](../interaction.md) 定义；下表只提供签名导航。

| 方法 | 种类 / 条件 | target | 输入 → 输出 | 回执阶段 |
| --- | --- | --- | --- | --- |
| `interaction.application_event` | command | `payload.surface_id` | `InteractionApplication_EventInput` → `InteractionApplication_EventOutput` | applied、rejected |
| `interaction.input` | command | `payload.surface_id` | `InteractionInputInput` → `InteractionInputOutput` | applied、rejected |
| `interaction.input_read` | query | `input_id` | `InteractionInput_ReadInput` → `InteractionInput_ReadOutput` | QueryResult / Error |
| `interaction.input_withdraw` | command / 条件 | `input_id` | `InteractionInput_WithdrawInput` → `InteractionInput_WithdrawOutput` | applied、rejected |
| `interaction.present` | command / 条件 | `surface_id` | `InteractionPresentInput` → `InteractionPresentOutput` | applied、rejected |
| `interaction.surface_create` | command | `owner_id` | `InteractionSurface_CreateInput` → `InteractionSurface_CreateOutput` | applied、rejected |
| `interaction.surface_list` | query | `owner_id` | `InteractionSurface_ListInput` → `InteractionSurface_ListOutput` | QueryResult / Error |
| `interaction.surface_read` | query | `surface_id` | `InteractionSurface_ReadInput` → `InteractionSurface_ReadOutput` | QueryResult / Error |
| `interaction.surface_update` | command / 条件 | `surface_id` | `InteractionSurface_UpdateInput` → `InteractionSurface_UpdateOutput` | applied、rejected |
| `interaction.request_read` | query | `payload.request_ref.owner_id` | `InteractionRequest_ReadInput` → `InteractionRequest_ReadOutput` | QueryResult / Error |

## memory

业务规则由 [memory](../memory.md) 定义；下表只提供签名导航。

| 方法 | 种类 / 条件 | target | 输入 → 输出 | 回执阶段 |
| --- | --- | --- | --- | --- |
| `content.close` | command / 条件 | `payload.content_ref.owner_id` | `ContentCloseInput` → `ContentCloseOutput` | applied、rejected |
| `content.get` | query | `payload.content_ref.owner_id` | `ContentGetInput` → `ContentGetOutput` | QueryResult / Error |
| `content.put` | command | `payload.content_ref.owner_id` | `ContentPutInput` → `ContentPutOutput` | applied、rejected |
| `content.register_copy` | command | `payload.content_ref.owner_id` | `ContentRegister_CopyInput` → `ContentRegister_CopyOutput` | applied、rejected |
| `content.release_copy` | command | `payload.content_ref.owner_id` | `ContentRelease_CopyInput` → `ContentRelease_CopyOutput` | applied、rejected |
| `memory.cleanup.get` | query | `payload.object_ref.owner_id` | `MemoryCleanupGetInput` → `MemoryCleanupGetOutput` | QueryResult / Error |
| `memory.create` | command | `owner_id` | `MemoryCreateInput` → `MemoryCreateOutput` | applied、rejected |
| `memory.delete` | command / 条件 | `memory_id` | `MemoryDeleteInput` → `MemoryDeleteOutput` | applied、rejected |
| `memory.extract` | command | `owner_id` | `MemoryExtractInput` → `MemoryExtractOutput` | applied、rejected |
| `memory.inspect` | query | `memory_id` | `MemoryInspectInput` → `MemoryInspectOutput` | QueryResult / Error |
| `memory.list` | query | `owner_id` | `MemoryListInput` → `MemoryListOutput` | QueryResult / Error |
| `memory.query` | query | `owner_id` | `MemoryQueryInput` → `MemoryQueryOutput` | QueryResult / Error |
| `memory.read` | query | `memory_id` | `MemoryReadInput` → `MemoryReadOutput` | QueryResult / Error |
| `memory.replace` | command / 条件 | `memory_id` | `MemoryReplaceInput` → `MemoryReplaceOutput` | applied、rejected |
| `memory.restrict` | command / 条件 | `memory_id` | `MemoryRestrictInput` → `MemoryRestrictOutput` | applied、rejected |
| `memory.view.ack` | command | `payload.view_id` | `MemoryViewAckInput` → `MemoryViewAckOutput` | applied、rejected |
| `memory.view.open` | command | `owner_id` | `MemoryViewOpenInput` → `MemoryViewOpenOutput` | applied、rejected |
| `memory.view.pull` | query | `payload.view_id` | `MemoryViewPullInput` → `MemoryViewPullOutput` | QueryResult / Error |

## authorization

业务规则由 [authorization](../authorization.md) 定义；下表只提供签名导航。

| 方法 | 种类 / 条件 | target | 输入 → 输出 | 回执阶段 |
| --- | --- | --- | --- | --- |
| `endpoint.pair.approve` | command / 条件 | `pairing_id` | `PairApproveInput` → `PairApproveOutput` | applied、rejected |
| `endpoint.pair.begin` | command | `pairing_service_id` | `PairBeginInput` → `PairBeginOutput` | applied、rejected |
| `endpoint.pair.claim` | command | `pairing_id` | `PairClaimInput` → `PairClaimOutput` | applied、rejected |
| `endpoint.revoke` | command / 条件 | `endpoint_id` | `EndpointRevokeInput` → `EndpointRevokeOutput` | applied、rejected |
| `grant.check` | query | `owner_id` | `GrantCheckInput` → `GrantCheckOutput` | QueryResult / Error |
| `grant.issue` | command | `owner_id` | `GrantIssueInput` → `GrantIssueOutput` | applied、rejected |
| `grant.lease.allocate` | command | `owner_id` | `LeaseAllocationInput` → `LeaseAllocationOutput` | applied、rejected |
| `grant.lease.settle` | command / 条件 | `lease_id` | `LeaseSettlementInput` → `LeaseSettlementOutput` | applied、rejected |
| `grant.read` | query | `grant_id` | `GrantReadInput` → `GrantReadOutput` | QueryResult / Error |
| `grant.revoke` | command / 条件 | `grant_id` | `GrantRevokeInput` → `GrantRevokeOutput` | applied、rejected |
| `grant.use` | command | `owner_id` | `GrantUseInput` → `GrantUseOutput` | applied、rejected |
| `grant.use.get` | query | `use_id` | `GrantUseGetInput` → `GrantUseGetOutput` | QueryResult / Error |
| `grant.use.settle` | command / 条件 | `use_id` | `UseSettlementInput` → `UseSettlementRecord` | applied、rejected |
| `grant.use.settlement` | query | `use_id` | `UseSettlementQueryInput` → `UseSettlementRecord` | QueryResult / Error |
| `confirmation.request` | command | `consumer_owner_id` | `ConfirmationRequestInput` → `ConfirmationRecord` | applied、rejected |
| `confirmation.read` | query | `confirmation_id` | `ConfirmationReadInput` → `ConfirmationRecord` | QueryResult / Error |
| `confirmation.decide` | command / 条件 | `confirmation_id` | `ConfirmationDecideInput` → `ConfirmationRecord` | applied、rejected |
| `grant.list` | query | `owner_id` | `GrantListInput` → `GrantListOutput` | QueryResult / Error |

## orchestrator

业务规则由 [orchestrator](../orchestrator.md) 定义；下表只提供签名导航。

| 方法 | 种类 / 条件 | target | 输入 → 输出 | 回执阶段 |
| --- | --- | --- | --- | --- |
| `budget.allocate` | command | `payload.parent_task_id` | `BudgetAllocateInput` → `BudgetAllocateOutput` | applied、rejected |
| `budget.settle` | command / 条件 | `payload.allocation_id` | `BudgetSettleInput` → `BudgetSettleOutput` | applied、rejected |
| `task.accept_result` | command | `task_id` | `TaskAccept_ResultInput` → `TaskAccept_ResultOutput` | applied、rejected |
| `task.adjust_budget` | command / 条件 | `task_id` | `TaskAdjustBudgetInput` → `TaskAdjustBudgetOutput` | applied、rejected |
| `task.attach_evidence` | command | `task_id` | `TaskAttach_EvidenceInput` → `TaskAttach_EvidenceOutput` | applied、rejected |
| `task.billing_reconcile` | command | `task_id` | `TaskBilling_ReconcileInput` → `TaskBilling_ReconcileOutput` | applied、rejected |
| `task.cancel` | command / 条件 | `task_id` | `TaskCancelInput` → `TaskCancelOutput` | applied、rejected |
| `task.input` | command | `task_id` | `TaskInputInput` → `TaskInputOutput` | applied、rejected |
| `task.list` | query | `orchestrator_id` | `TaskListInput` → `TaskListOutput` | QueryResult / Error |
| `task.pause` | command / 条件 | `task_id` | `TaskPauseInput` → `TaskPauseOutput` | applied、rejected |
| `task.read` | query | `task_id` | `TaskReadInput` → `TaskReadOutput` | QueryResult / Error |
| `task.result` | query | `task_id` | `TaskResultInput` → `TaskResultOutput` | QueryResult / Error |
| `task.resume` | command / 条件 | `task_id` | `TaskResumeInput` → `TaskResumeOutput` | applied、rejected |
| `task.revise` | command / 条件 | `task_id` | `TaskReviseInput` → `TaskReviseOutput` | applied、rejected |
| `task.submit` | command | `payload.orchestrator_id` | `TaskSubmitInput` → `TaskSubmitOutput` | applied、rejected |
| `budget.read` | query | `allocation_id` | `BudgetReadInput` → `BudgetReadOutput` | QueryResult / Error |
| `budget.close` | command | `payload.allocation_ref.id` | `BudgetCloseInput` → `BudgetCloseOutput` | applied、rejected |
