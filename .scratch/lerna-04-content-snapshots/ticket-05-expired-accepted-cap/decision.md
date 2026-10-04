# 04票05：原 accepted cap 到期的独立清理因果

**采用最小修正：ConsumePolicyCleanup 增加“原版本持久保存上限已到期”分支，复用现有同 Tx seal/holder/Job/原责任绑定以及 Lifecycle 全 ACK。当前宽 Save=true 不能使原 cap 重新有效；此分支不要求历史 Save true→false。** 无新公共合同、端口、迁移或通用清理框架。

固定产品源码：`e5b38a57dab458be56574237052f1ad7465bf321`，WT `/tmp/lerna-worktrees/content-snapshots-05`；本次核对 HEAD 及 domain/content、adapters/postgres/content 相对固定源无差异。新 `content_accepted_cap_cleanup_test.go` 是本次独立 tracer。本文只作 STATIC 决定，不执行 native、数据库或修改产品；原 9356B seal-ACK、restored-qualification、terminal-erasure-history 归档保持不变。

## 1. 当前真实反例与源码依据

已读 `/tmp/lerna-04-ticket05-execution/accepted-cap-first-red.log`：`TestContentExpiredOriginalAcceptedCapStartsCleanupDespiteCurrentWideRenewal` 在 2.216s 失败，PID/PGID 2916513、starttime 12171780、exit 1、group_absent=true、timed_out=false。它到达 Consume 后 Observe seal unavailable；不是进程未知，也不是已经通过的清理链。

该测试此前真实经历：V1 原 cap=2s 接纳及 alpha 出版；同绑定 Rev2 宽 Save=true；独立 V2 beta 出版；重开并等原 due+20ms；Manager.Step 登记原 pending，Reason=`accepted_retention_expired`，原责任 Deadline 仍有效；V1 Get expired、独立 alpha 正文仍在、V2 beta 可读。失败之后的实际删除、erased、再次重开和原回执断言尚未执行，不能引用为 green。`/tmp/lerna-05-expired-cap-static-scope.md` 的“未执行”是较早记录，本日志是后来的明确 red，勿改写旧记录。

源码缺口明确位于 `domain/content/policy_cleanup.go:45–50,92`：排除非空 Reason，要求 Previous.Save=true 且 Policy.Save=false，又排除 CurrentRetainUntil 已过期。三项合起来使该既有责任永远不进入 seal。相反，`management.go:register` 已在原保存主体/用途适用时持久收紧 CurrentRetainUntil，并用 fresh DB time 登记 `accepted_retention_expired`；`scheduleAdmission` 已按该 cap 安排原 natural due。消费方必须承接这个确定因果。

## 2. 最小分支与证据

将**共同身份/结构资格**与**具体保存失效原因**分开，保留已有 restored-save / 当前明确 save=false 路径的限制，不全局放宽 pending 过滤。

新分支候选严格是：原 BodyCleanup=pending、Residual=holder_unconfirmed、Reason=accepted_retention_expired；PolicyChange 身份准确、phase 已知，不能是 maintenance_coverage_unknown/current_basis_unconfirmed。Reason 只用于选择候选，不能自己充当删除许可。

在现有 qualification Tx 内必须重新取得并核对：

- 当前受信管理 owner、完整 TrustedSubject 和 TrustedUntil；原责任/Record/Change 保存主体及 Purpose 全部匹配，Ref 是完整准确版本，Record.ValidateIdentity 通过。其他主体、用途、同 tuple 的错完整 ref 不授权删除。
- 原 published Record 尚无 seal、BodyGone=false；原 change 来源确为该 target 或其**完整已登记闭包**成员。继续 registeredClosure 的 nil-actions 结构核验和全部准确来源锁定；缺来源、错绑定、循环/超限及不完整结构不得当成功。这里核结构，不要求已过期内容重新取得 save 使用资格。
- 解析并确认 Record.CurrentRetainUntil 是真实有效、非零的持久时间；锁等待后 fresh DB Now >= 该 cap 才证明当前保存依据到期。现 `cutoff` 会吞解析错误返回零，因此此破坏性分支不得把 malformed/空时间的零值当到期证据；最小可在本分支显式严格解析，失败保留责任并返回准确错误，无需重构所有时间类型。

新分支不要求 Previous 非空、旧政策 Save 撤销或当前 Save=false；Rev2 的较宽 retain/valid/save 不能覆盖 Record 的单调上限。也不写回宽 cap、不重新接纳原 Command、不新建版本来伪装同一责任恢复。

当前 target cap 的确切到期本身是独立保存失效事实，不依赖把当前 policy 查询 nil 解释为 false。保留当前需要的 policy/完整保存 basis 核对及错误传播；数据库错误、错结构、未知管理资格仍不能删除。若当前 policy 缺失，它不提供新的撤权证据，也不能把**已经准确验证的原 cap 到期**改成可续存；cap 分支的权限来自当前受信管理资格，因果来自原准确 Record，不来自猜测 policy。当前 tracer 的政策均存在，缺失政策组合须独立验证，本文不冒称已覆盖。

## 3. 原执行窗口、整页原子性与锁序

cap 是**正文可保存期限**；原责任 Deadline 是**这次清理可执行期限**。前者已过期正是清理原因，后者必须仍有效。使用 `original.Deadline` 原值，继续核不超过当前有限 WorkBudget/TrustedUntil；不能使用后继 phase 的 Deadline、ExpiryDeadline 或 now+预算续命。窗口已耗尽则保持 pending/residual/历史和负责方，不建能发效果的新 seal/Job。

先独立 Observe 候选并释放锁；随后全页先锁并核所有 Record/policy/结构来源，再执行共享 sealLocked，最后 BindPolicyCleanupSeal 的完整 expected-row CAS。保持现有 Version/policy→holder/Job→责任行次序，不新增责任行之后反向获取 Version/policy/change 锁。任一页内资格、绑定、CAS 或最后时钟失败，整页 Tx 回滚，包括刚建立的 seal。

新 cap 候选携带本次锁定 cap，最终全部写入/责任锁等待后再次核当前管理资格与 DB Now：每条仍满足 cap<=Now<原责任 Deadline。现 restored-safe 候选仍核其原当前许可 bound；两种分支不能共享一个“必须 cap 尚有效”的条件。所有检查在同一有限 owner Tx 完成；未确认提交按原 change/ref/seal 观察恢复，不换身份重发。

## 4. 原 seal 关联与正向 ACK

复用稳定 `policySealID(originalChangeKey, fullRef)`、原保存 Subject/Purpose、原责任 Deadline、BodySeal.PolicyChangeKey，以及现 BindPolicyCleanupSeal。该步骤只建立并封新使用，不把原 pending 提前变 erased。

真实 Lifecycle.Step 仍在全部原 holder 独立确认、当前 Claim/管理时钟/原 Deadline 全通过之后，才经专用 ACK port 把**该条原责任**改 erased 并同 Tx Complete。primary gone 不等于全 ACK，离线/未知 secondary 不被洗掉。保留 Reason=accepted_retention_expired、Actions、原 Deadline、历史 holder union/attempt/publication；后续同政策 natural phase 按已采用 terminal-history 规则保留 erased 正向事实。

若 Record 已有同一 seal，按原身份观察/恢复；若是主动 seal 或另一政策 seal，不改其不可变 PolicyChangeKey，不把第二 key 自动套入本条 ACK。现分支对已经 sealed 的版本保持既有保守行为；未采用的多政策关联建议不因本反例自动生效。已 sealed/bodygone 也不因此获得新预算。

## 5. 本次范围与后续必要 tracer

当前 first red 只证明**原 target 自身 cap 到期、同绑定政策续宽、原责任执行窗口仍有效**的消费缺口。最小修后应完成测试剩余真实链：原 key/deadline seal、实际 holder 清理、独立 alpha 不存在、原责任 erased、重开仍成立、V2 beta 正常、原 Command 回执不变；由 sole owner 执行 normal/race 和相关回归，本文不作执行结论。

祖先到期/后代责任、多个祖先、AdmissionTarget 专用 change identity、整页 CAS/锁等待跨界、原 cleanup Deadline 已过、缺失政策/结构和 secondary pending，仍需各自准确正常/拒绝/恢复 tracer。本分支可以保持完整结构核验，不把只验证 target 的测试写成祖先证明；不得顺手放宽现 AdmissionTarget/key 验证来宣称已支持目标化义务。原 read/process-only 不强删、restored-safe 不 seal、错主体/用途拒绝等正常对照继续保留。无整票或 whole04 退出声明。
