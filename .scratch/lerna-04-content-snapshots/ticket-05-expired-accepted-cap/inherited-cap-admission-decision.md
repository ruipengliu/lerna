# 04-05：继承 accepted cap 的 AdmissionTarget 清理资格

## 1. 固定范围与决定

STATIC 决定；产品固定 `1a52d24a051442cd8ccdd3c3ef01ff7707cc84a7`，工作树 `/tmp/lerna-worktrees/content-snapshots-05`。新增测试与证据仍是 WIP，本报告不确认其产品修复或测试通过。仅承接已采用 `ticket-05-expired-accepted-cap/decision.md` §5 留出的 ancestor / multisource / AdmissionTarget 前沿，不改旧报告、已发布 SQL、普通政策责任语义或多政策 seal 关联规则。

**采用最小域内修改：准确识别原 AdmissionTarget key，并仅让该 target 的 `accepted_retention_expired` 原责任进入现有 cap 因果清理支路。** 复用当前完整结构核验、当前原保存主体/用途政策读取、全页两阶段资格、`sealLocked`、`BindPolicyCleanupSeal` 与实际全 holder ACK。无需新 port、迁移、通用清理框架或对象级批量 ACK。

## 2. 已读事实与执行边界

实际读取 `/tmp/lerna-04-ticket05-execution/inherited-cap-admission-first-red.log`、完整 `conformance/component/content_inherited_cap_cleanup_test.go`、`/tmp/lerna-05-cap-causal-frontiers-next-scope.md`，以及当前 `domain/content/management.go`、`policy_cleanup.go` 和 PG management / policy_cleanup 实现；另读 owner 的 static gates 输入，不把它当已采用实现。

本轮真实 red：2.223s，native PID/PGID 3136486、start 13089058、exit 1、group_absent=true、无 timeout。失败在测试约 line 190 第一次消费 exact admission key：`transaction scope mismatch or expired`。此前已执行：A 原 accepted cap 2s、A 当前 Rev2 wide Save=true、B live、双 source target receipt 固定 min cap、独立 V2 正常；有限完整 Admission pages 找到原 target/A/Rev2/due/deadline；重开、真实到期、Manager 推进后观察原责任 pending / holder_unconfirmed / accepted_retention_expired，原 deadline 仍有效，A/target Get expired，B/V2 正常，三个原 alpha 正文独立仍在。

**红点之后的 seal、删除、全 ACK、重开、A/B 原字节保留、原 receipt/publication-history 回放均未执行，不能算已证明。** 本报告没有运行 native、DB、build 或测试。原 whole-page CAS normal .555 / race 2.272 为 parent 已资格的独立既有证据，本次不重称亲自执行，也不替代新增支路的测试。

现有直接原因有两处：`changeKeyForCleanup` 只计算普通 `changeKey(change.Policy)`；即使通过它，consumer 又无条件排除 `change.AdmissionTarget != nil`。现有 PG Bind/ACK 不限定普通 key 前缀，已按 exact change key / full ref / 完整 subject 编码 / purpose / deadline 和 expected responsibility 绑定，因此不是 PG schema 缺能力。

## 3. 准确身份与支路范围

将私有 key 资格助手做成明确的普通/targeted 两分支；可返回 `(string, error)` 使无效形状显式失败，不需要新导出抽象。

- `AdmissionTarget == nil`：保持现有普通 `changeKey(change.Policy)` 计算与旧支路，不把本次新 target 约束倒灌普通自然阶段。
- `AdmissionTarget != nil`：复用现有 `admissionKey(*change.AdmissionTarget, change.Policy, change.ExpiryDue)`，不重新设计 hash。此函数原身份已包含 domain、full target、full source ref、完整 Subject、Purpose、原 Revision、原 Due 与 natural_expiry 标签。不能用当前政策 revision / 新 expiry 计算替代 key，也不能只识别 `ip-` 前缀。
- 先核 full target / policy ref / subject 结构与本 owner；要求现有 `propagationPhase` 识别为 natural_expiry（其已有合法 legacy empty-phase 解释可复用），而非接受任意非空 phase。targeted 原形状的 Previous 应为空；原 Due/ExpiryDue、Deadline/ExpiryDeadline 应保持 `scheduleTarget` 写入的对应一致关系，原有限 expiry 窗口有效（正且不超过现有 24h 上界）。无效形状返回 scope 错误，不能靠 cap 分支绕过。
- requested key、读出 change.Key、准确重算 key 三者相同；页内每个 `original.ChangeKey` 必须匹配，并且 **`original.Ref == *AdmissionTarget`**。越界 responsibility 应使本页事务失败，不能静默清理另一 ref。随后现有 LockVersion / ValidateIdentity 再确认 `record.Ref` 与它完整相同。

新增消费范围只包括 `BodyCleanup=pending`、`Residual=holder_unconfirmed`、`Reason=accepted_retention_expired`。change.Reason 仍保持原有限允许集（空或 original_deadline_expired），不接纳 maintenance_coverage_unknown / 未知原因；后者不因为目标到期就自动洗成可删除。targeted 的 reason 空、单纯 read/process 撤回、Save unknown/false 等其他分支仍保留原拒绝/未消费行为，本次不广开所有 AdmissionTarget。

Change.State complete 是传播完成，不是正文完成；不能因此跳过仍 pending 的原正文责任，也不能据此直接标 erased。已 erased 的责任仍沿原幂等观察与 terminal-history 规则。

## 4. 目标 cap 因果、完整来源与有限权限

保持原消费事务的以下门：

1. 当前受信管理主体/owner/TrustedUntil；原 responsibility.Subject、record.Subject、change.Policy.Subject 完整相等，Purpose 同一原保存用途；target full ref 和 record identity 精确一致，published、未 seal、未 BodyGone。这里是目标原保存资格，不是当前来访者随意选的新保存用途。
2. 严格解析目标 **持久 `CurrentRetainUntil`**，实际新 DB clock 已到该 cap。不能把 parse 错误/零值当过期；不能以 policy 新 retention、当前时间加预算或原请求重放抬高 target cap。
3. 在同 Tx 内完整 `registeredClosure(..., nil)`，包含全部中间祖先且不截断。原 `change.Policy.Ref` 必须是该 target 自身或其准确注册祖先；本次真实场景是 A 祖先。保留 source full-ref、真实版本、结构完整性及缺失/不一致的拒绝。**不要求每个 source Record.Subject 都等于 target.Subject**：祖先自身 creator 可以不同，目标使用祖先的资格按目标原完整 Subject/Purpose 读取。
4. 保留 target 与全部祖先在目标原 Subject/Purpose 下的 CurrentSavingPolicy 读取；发生 store 错误仍失败。nil 意味着未知/无匹配当前资格，不能宣称 Save=false。这里已独立证实的 target 持久 cap expiry 可以作为清理因果，故不要求当前 source Save=false、历史 policy 窗口仍授权或 current revision 等于历史 revision；Rev2 wide Save=true 也不能复活 cap。
5. 对 targeted duty，仅在其原自然到期已经到达的窗口消费；采用原 duty.Deadline，且它不得超过原 targeted ExpiryDeadline。责任已有更早 monotonic deadline 时保留更早值。保留原 `Deadline <= now+WorkBudget`、`Deadline <= TrustedUntil` 检查，不改写任何 due/deadline、水位、budget 或 policy revision。若原 deadline 已过，保留 pending/residual，不能创建新 seal 预算。

记录 cap 因果不要求 A 当前 cap 恰好仍等于旧 Due，更不能重新从现在的 A/B policy 推导替代目标 cap；后续合法收窄可能更早。原 key 固定当时的责任身份，当前 target cap 决定现在的独立失效事实。新增门以严格未过原责任期限为准。

## 5. 同页提交、seal 与 ACK 精确落点

保留既有选择 Tx（ObserveChange/ReadChange）先结束，资格 Tx 对整页先完成全部 Version / 当前 policy / closure 读取与资格，再做 holder/job/seal 写入，最后 responsibility 全 expected-row CAS。不得为了重读 change 在末段增加逆序锁；原 immutable 身份由既有 writer 协议承接，当前目标/政策/责任变化分别由锁与 exact CAS 阻断。

对合格 admission duty，现有 `policySealID(originalAdmissionKey, targetFullRef)` 与 `PolicyChangeKey=originalAdmissionKey` 正是所需原身份。沿现有 `sealLocked` 写入目标 seal/原 holders/job，`BindPolicyCleanupSeal` 匹配该条原责任。末尾真实 fresh clock 必须再次满足 target cap <= now < 原 duty deadline，并保留 manager.current；新增原 due 检查也应覆盖最终时刻。任何 late / CAS mismatch / scope error 回滚全页，不能留下部分 seal 或把新工作隐为成功。

仅 Lifecycle.Step 对**该目标 seal 的全部原 holders**取得独立实际 ACK 后，现有 `AcknowledgePolicyCleanupErased` 才关闭 exact admission duty。原 holder 历史 union、Actions、Reason、AttemptKey、Publication、水位、原 deadline 保留。target 的 primary gone 不等于 secondary ACK，不等于 A/B gone，更不等于 A/B 其他政策责任已完成。

已有 voluntary/其他 policy-key seal 或 BodyGone 分支仍保守保留原行为；本次不采纳尚未授权的多 trigger 关联方案。不得利用同 ObjectID 或同源 policy 扫描整 namespace ACK。普通 SaveResponsibility 不获得生成 erased 的权限。

## 6. 最小真实验收与拒绝对照

由 sole owner 在获准 LOCAL 后执行，本报告不占槽、不宣称 green：

- **先完成原 exact red 的尾部**：同 A/B/V2/target、原 key/receipt/deadline，不重 Put 原版本。实际 seal→原 holder 清理→全 ACK→reopen→原 duty erased；target 原字节独立确认不存在；A/B 原字节仍存在且没有被本消费生成 seal；B/V2 仍正常；原三个 Command 固定回执逐字相等、publication history 不变。再做受影响 race。此一场景的 B 与 V2 是实际 live 对照，不能删掉来简化。
- **最少新增边界对照**：同真实 admission 在原 cap 尚未到期时调用不得 seal；对真实可发现的另一 target 或 Subject/Purpose duty，非其原主体不能消费本 target。精确 key/full-ref/原 Due/Revision 不匹配须 fail closed；可用已有窄 repository decorator 给机械协议违例输入验证此门，但必须标机械防御测试，不以伪造私表行冒充合法管理事实。至少一个拒绝要来自真实管理/公开入口，而非所有拒绝都是 decorator。
- **时间与并发**：沿已有 whole-page CAS / 最后锁等待 clock 场景验证新支路仍回滚，不需重新发明全框架；若既有测试仅跑普通 key，增加同 seam 的 targeted 个案或准确承认其未覆盖。原 deadline 过期后不得 seal/续预算。未知 source policy 不能作为 save=false 的原因；target cap 分支只按上述独立事实行事。
- **证据限度**：现有 red 已真实覆盖双直接 source 的继承 cap 与 AdmissionTarget 身份阻塞，尚不证明任意深图、不同 creator、离线 secondary、全部并发/错误组合已在本支路运行。用既有相应独立合格 tracer 承接时标明其 scope；没有对应执行就保留 pending，不借本次普通 green 宣称全票/whole04退出。

默认修正集中在 `domain/content/policy_cleanup.go` 的私有 identity 资格、targeted cap 准入和原 due/deadline 检查，以及该真实 tracer/最小反例。Management 的 admissionKey、调度、水位，PG Bind/ACK、普通 history writer、原 migration 均 KEEP。若实现发现其中任一现有 writer 会改变 targeted immutable identity，须另报告具体反例，不能在消费端猜原身份重建。
