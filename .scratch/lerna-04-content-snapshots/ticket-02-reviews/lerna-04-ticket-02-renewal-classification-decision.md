# 04票02：自然到期与历史撤销的续期分类

2026-10-04。只读当前 `/tmp/lerna-worktrees/content-snapshots-02/domain/content/management.go` 与 PG management 的相关 WIP 分支，及已采用 ticket-02-oracle-decisions / inherited-expiry-decision。57ea 是此前固定审查基线；本次所读 AdmissionTarget 等是其后进行中源码，**不是固定交付审查、native证据或实现正确性声明**。未执行测试/DB/build、未改源码。05条件稿仍硬等whole04，不在本决定范围。

**决定：需要修。source-wide nil change 的纯自然到期阶段，应与定点义务一样核当前准确保存依据；主动政策变更阶段仍传播原历史事实。两阶段不能仅以 worker 当前 now 是否已越过 ExpiryDue 区分。原 pending/history、准确主体用途、单调 cap、原水位/截止均保留。** 无须新ADR、Job框架或SQL迁移。

## 1. 实际问题与两种语义

当前 `registerAdmission` 用 LockPolicy→fresh DB Now→当前准确 Policy 分类；普通 `advanceJob` 对源自身和每个水位内后代仍直接 `register(change)`→`affected(change.Policy, now)`。因而 A.ValidUntil 尚未到时已合法续期，旧自然检查仍可把旧截止当成当前全部动作失效，误记 A 保存责任 pending；D 的定点检查却为 not_required。该差异没有领域依据。

但是“当前许可已恢复”也不能取消**历史真实撤销**：例如 R2.Save=false 已提交、尚未完成传播，随后 R3.Save=true。R2 的原水位传播仍必须登记其撤销曾产生的责任，不能只看 R3。因此采用以下准确区分：

- **policy_change：** 安装修订产生的初始传播。用不可变原 Policy/Previous、准确 actual source 和原水位，保留真实动作撤销、错误 fullRef、实际保留 cap 收紧；晚执行不把它改造成自然检查。新宽 policy 不改其事实。
- **natural_expiry：** 为原有限到期预约的检查。原 Due 表示“届时必须核对”，不是断言旧许可届时仍是当前许可；用当前相同 policy key 的准确 fullRef/subject/purpose/revision、五动作和有效期限，另核 actual source/target CurrentRetainUntil。当前依据合法且有独立有限后继维护覆盖，才可得这次自然检查 not_required。

ExpiryDue/ExpiryDeadline 始终是原自然检查时刻/预算；Due/Deadline 是当前阶段的原时刻/预算。跨锁等待用 fresh Now 和已采用最后预算校验，不能把“现在已经过 ExpiryDue”当阶段切换授权。

具体实现还要避免初始传播里的同一种误判：policy_change 用原安装时刻（该阶段原 Due）判定这次修订当时是否已过期，并传播原 flags/fullRef/retention 收紧事实；不能仍用 worker now 把后来已被续期的旧 ValidUntil 临时加成历史撤销。当前 cap 到期和当下维护仍按可信 fresh Now 独立核。若延迟初始页已经越过自然时刻，可在原预算内额外作当前自然核对，但不能用它覆盖历史，也不延长原 Deadline。原安装已提交过期许可的事实仍是历史失效；“安装时未过期、执行时旧时间到了”不是同一事实。

## 2. 最小阶段表示与旧记录

推荐在内部 PolicyChange JSON 增加可选闭合 `phase`（`policy_change` / `natural_expiry`），并以已有字段校验，不加数据库列/公共合同：

1. install 初始传播明确 policy_change；scheduleAdmission 把原 complete 转为自然维护时设 natural_expiry；restoreLegacyMaintenance 的纯自然预约和所有 AdmissionTarget 原定点义务设 natural_expiry。若 legacy 回填已发现真实 flags/完整绑定失效，应显式保持其历史失效登记，不能假称只是尚未发生的自然检查。
2. 原政策传播完成而尚未到 ExpiryDue，原代码转 scheduled、清游标、Due=ExpiryDue、Deadline=ExpiryDeadline 的同一 Tx 同时转 natural_expiry；保留原 key/watermark 和原预算。已经迟于自然时刻的初始传播不能仅改 phase 后重新开始预算；按原有限截止闭合/残留，不伪造额外轮次。
3. 旧 JSON AdmissionTarget 非nil可确定是 natural_expiry；旧 nil `State=scheduled` 且 Due/Deadline 分别等于 ExpiryDue/ExpiryDeadline，可按现生产路径确定为自然阶段。旧 pending 一概不能凭 now 或单独 Due 相等猜：install 恰在截止、回填已到期、真实撤销都有可能。能由已保存来源事实唯一判定才补 phase；否则保留原事实与未知分类/残留，不制造“已合法续期所以历史撤销不存在”。不要为了兼容重写全部旧记录或改0001/0002。
4. phase 是当前工作阶段，不改原 Policy/Previous，不伪造某次政策修订。State 是 scheduled/pending/residual/complete 的工作进展，不能独自承担永久原因类型；residual/重开也保留原 phase 与截止。

仅判断 scheduled 能覆盖眼前正常案例，但不足以说明已到期回填和后续 residual 的准确含义；上述一个内部字段是必要最小区分，不是泛状态机平台。

## 3. 共用当前分类，但不覆盖历史责任

把现 registerAdmission 的当前资格读取收窄成实际两消费者共用的私有自然检查路径：定点目标和 nil 自然页（含 source 自身）。读取 current 后 fresh Now，保留原 change key、subject/purpose、原 Deadline、原水位/游标；构造仅用于分类的 policy 视图，**不要 SaveChange 把历史 Policy 换成最新 revision**。自然分类不沿用旧 Previous 来推算“这次新撤销差量”，应按当前动作有效性分类；否则旧 before=false 会漏当前仍 false 的动作。

- current 缺失/错误 fullRef/过期：相应动作无当前资格；原保存主体/用途确依赖此 policy key 才能 body_cleanup=pending。不同主体/用途只相关 use_review，不能收紧不相关 writer 的 cap/授权全局删除。
- 当前准确续期只延长可续的许可窗口，不能延长已存 CurrentRetainUntil。A 或 D 的适用 cap 到期仍须 pending；旧 RetainUntil 曾真实收紧的后代，即使传播尚未扫到，也不能因读取新宽 policy 丢掉那条原历史收紧责任。
- 当前较新政策须有其原维护 change/原截止及该目标覆盖：source A 应由安装/接纳时真实登记覆盖；水位内 D 由新 change 覆盖，后入场 D 由准确 AdmissionTarget 覆盖。缺覆盖保持待核对，不把宽 policy 当无期限续存；不扩大旧水位、不造查询 Job、不刷新 WorkBudget。
- 原自然检查只证明当前维护是否需要；not_required 不证明物理已擦除，也不宣布历史清理责任已经闭合。

**必须一起保留历史责任：** 当前 PG `SaveResponsibility` 对 `(change_key, object_id)` 直接覆盖 body，原 key 又复用初始传播和自然阶段。若同 key/ref 已有 pending/holder_unconfirmed，后来自然 not_required 不能覆盖掉它。最小接法是在现写入责任处同 Tx 对原项作保守合并：保留先前 pending/未知 holder、原更早截止、已记录受影响动作/原因；新核对可以补当前观察，但不隐式确认清理完成。不同 change key 的历史当然各自保留。若无既有 pending，则记录自然本次 not_required 正常事实。

这里不新增清理终结能力：票02没有真实 erase/reconcile，无法以 renewal 自己消掉未确认责任。未来05有独立真实确认后再作明确终结；本次不先实现05。正常永久 cap/原 receipt/准确版本/原 publication 一律不复活、不回写历史。

## 4. 必要公开 normal / fault 对照

1. A 与 D 正常发布；A 的旧 ValidUntil 早于双方 retention，**在旧 ValidUntil 前**安装同 fullRef/subject/purpose 的更高修订合法续期。原 source-wide 自然检查与 D 定点检查到时都不凭旧 ValidUntil 新增错误 pending；新修订有限维护责任可见，独立 Get 正常，原 bytes/receipt 不变。真实 reopen 保持原旧 key/Due/Deadline/水位。
2. A.Save 真撤销后又恢复：原撤销传播尚未完成及已登记两种次序都保留原 pending/历史；旧 change 到自然阶段不能覆盖成 not_required。read-only 收紧不误成 save cleanup，不同 subject/purpose 对照不全局删。
3. 原 RetainUntil/CurrentRetainUntil 被收紧或已到期后再宽 policy，A/D 原版本仍不复活；只新准确合法版本可正常。自然 phase 读取当前 policy 不漏原单调 cap。
4. 跨 ExpiryDue 但仍在初始 policy_change 页上的真实锁等待，不把原主动撤销改判自然续期；跨原 Deadline 按已采用 residual 规则保原 cursor/截止，不 false complete。没有当前 policy/后继维护覆盖的反例保留未知。
5. 至少覆盖旧 scheduled 双读、已过期 legacy 回填和明确 phase 重开；每个正常/反例用受信管理观察及公开 Content 观察，无私表业务 oracle。分类注入不冒 native 故障，本稿没有实际 red/green。

这是对 inherited-expiry §4 当前分类边界的必要补充，适用两种真实自然维护消费者；不改变已采纳“主动撤销不被最新政策洗掉”的要求。Root采用后由 solefixer 在同一修复轮次落地和验证。
