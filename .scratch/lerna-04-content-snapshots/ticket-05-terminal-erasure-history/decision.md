# 04票05：普通自然phase不得撤销原责任的正向擦除ACK

**采用必要最小修正：对同一准确责任row，普通SaveResponsibility永久保留已确认的erased终态及其历史字段；当前自然phase可以增加受影响Actions，不能把真实全holder ACK降回pending。** 不新增任意transition flag，不把不同change或保存basis一起洗成erased。

固定源码 `51e331ea18d42ce32442003a343aa451d30e460c`，WT `/tmp/lerna-worktrees/content-snapshots-05`。已读实际PG SaveResponsibility、domain register、当前Lifecycle ACK ports/completeCleanup及新测试。root归档两份已采用决定已与此前全文读取的本轴原文逐字比对：cleanup-qualification 10396 bytes/SHA256 `a59918ea977e3c81aff6856bbf1ac5438c6516a440ec350a7278d7c37efcd026`；seal-ack 9356 bytes/SHA256 `c443e8e57318ba9d8ea8a49d2c225673f3a5bceae08b452f7270f3b8e164dae5`，完全相同。

本次只读，无native执行。root另报告的lateFinish WIP一行 `&&!BodyGone`及其red/green/race仍是独立资格，不冒充此普通writer已经修改或本反例已green。

## 1. 本次真实反例与根因

实际读取 `/tmp/lerna-04-ticket05-execution/policy-ack-natural-phase-first-red.log`：测试 `TestContentNaturalPolicyPhaseCannotUndoOriginalErasureAcknowledgement` 2.309s，native PID/PGID2862036、starttime11934038、exit1、groupAbsent=true、timed_out=false。失败在原真实policy→seal→全holder ACK之后，重开并等原安装的2秒自然due+20ms，再实际Manager.Step注册同key责任：BodyCleanup从erased变pending、Residual变holder_unconfirmed、原ObjectHolder历史true变false。它是该业务反例，不是外层unknown或编译失败。

`domain/content/management.go:register`正确从当前Record构造本次候选（已gone后当前ObjectHolder可为false）；PG `management.go:SaveResponsibility`只在previous.BodyCleanup==pending时保留历史，因此previous==erased时直接被候选覆盖。新的候选反映当前自然资格，不是撤销旧物理ACK的授权；问题位于持久责任合并边界。

## 2. 精确合并规则

仍锁现有 `(tenant, owner, change_key, object_id)` row。合并前必须校验：Tx owner与Ref.Owner一致；previous.ChangeKey、**完整Ref**、**完整Subject/DelegationChain**、Purpose均匹配incoming。不能只比object ID或SubjectID。不同准确tuple为scope/conflict拒绝，不能套用旧erased。新ChangeKey是另一条责任，不从同版本的其他row继承ACK。

推荐在现合并处明确分支，不简单把所有字段放入同一个 `pending || erased` 分支：

| previous状态 | 普通写入行为 |
|---|---|
| pending | 保持原保守pending/residual、holder union、非空旧reason/attempt等既有合并规则；本修正不顺便冻结所有pending publication/空reason，从而改变尚在推进的责任语义。 |
| erased | **BodyCleanup与Residual精确保留previous**；合法erased的Residual必须为空，非空组合属于不一致，拒绝/暴露错误，不擅自“修”为另一个状态。**Reason、AttemptKey、Publication全部精确保留previous，包括空值。** StagingHolder/ObjectHolder按历史OR保留正向事实；不得由当前Record的false抹旧true。 |
| 其他现有状态 | 保留当前合法合并规则；不借此扩展新终态或取消资格。 |

全部状态继续现有 **Actions五动作确定顺序union**、**Deadline取原与新候选较早者**；同原自然phase正常应保留原较早deadline，绝不按now或自然ExpiryDeadline刷新。保留更早deadline不宣称ACK是在一个后来收紧的期限内完成；本修正只保历史事实，不重新授予执行窗口。

erased分支不能写 `if previous.Reason != ""` 再保留：原空Reason也是ACK时真实历史，不能让后续accepted_retention_expired等新phase原因替换它。新的phase原因仍在PolicyChange/当前资格事实表达；该责任已有Actions union保留新增影响。AttemptKey空值/Publication同理，不能从较晚当前记录补写成“原ACK时就有”。

普通SaveResponsibility本身不得产生新的erased：其实际Manager调用者只提交pending/not_required等普通候选。若incoming携带erased且没有同row既有合法erased，明确拒绝，不能INSERT/UPDATE为新ACK；更简单可对普通入口所有主动传入erased直接拒绝。只有既有 `AcknowledgePolicyCleanupErased` 在准确seal/原row匹配且Lifecycle真实全holder确认后写入这个正向终态。

## 3. ACK来源、锁序与当前时间

当前PG `policy_cleanup.go:policySealMatches`绑定原ChangeKey、完整Ref、完整主体、Purpose及原Deadline；`AcknowledgePolicyCleanupErased`只接受准确pending/合法erased，精确row CAS仅改状态/residual。domain `completeCleanup`在ACK前后都重核当前管理时间、原seal Deadline和Claim，再同Tx Complete；实际全holder检查在其真实调用者完成。继续这条唯一起源，不让普通自然注册重新制造ACK。

保存既有erased只是在同row保留已确认历史，不需要重新核查/删除物理对象，不要求在原旧deadline内“再完成一次”。自然phase自身仍必须遵守原当前authority、phase/deadline、watermark、Claim和最终fresh-clock门禁，不能因旧erased跳过它们。

合并仍由现有Version→责任行的路径进入，责任行FOR UPDATE后不新增反向Version/Seal/Policy锁。普通PG合并不自行开Tx或读时钟充当domain授权；它不改变调用者实际fresh-clock要求。查询未知/Close未知/并发row不匹配不能作为erased来源。无需新迁移、关联表或权限框架。

## 4. 当前Record与历史责任是两种事实

当前权威gone后 `Record.ObjectHolder=false`与旧责任ObjectHolder=true可以同时正确：前者是当前primary持有观察，后者保留该原责任曾实际承担的holder正向历史。真正全ACK由责任erased及准确seal/holder记录表达，不用把历史布尔清零。publication/receipt也保留原历史，不据bodygone改成未出版。

本决定只保持**同原policy key自身后续自然phase**的历史。它不承接第二policy、其他主体/用途、另一fullRef、主动seal或同object ID下的所有责任；这些不能读取本row erased就自动通过。之前未采用多政策建议已独立到 `/tmp/lerna-04-ticket-05-multiple-policy-seal-followup.md`，旧seal报告恢复原9356字节，不改变其采用引用。

## 5. 本tracer及剩余验证资格

当前测试真实经历publication、policy传播、seal、独立holder清理、原责任erased，再重开等待原真实due并由Manager消费自然phase；因此修后同测试能验证这条实际顺序中终态/历史保留、五动作union、原watermark/due/ExpiryDeadline不刷新，以及再次重开的耐久观察。它通过受信Manager/Lifecycle事实验证，未使用私表镜像oracle。

它不单独证明所有fulltuple拒绝、并发ACK/普通writer两序或其他policy不继承。最小后续相关正常/拒绝检查应覆盖准确同row自然重放、不同change仍独立pending、错完整主体/用途/ref被拒、普通入口不能自造erased；必要机械port测试标明机械资格，不冒充原生删除故障。真实源修复后的normal/race及相关suite由soleowner执行，本文只给必要决定，不称已green或整票通过。
