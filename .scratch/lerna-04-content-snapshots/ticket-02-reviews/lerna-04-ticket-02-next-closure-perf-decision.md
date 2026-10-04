# 04 票02：一次准确政策行上的有限动作交集

2026-10-04；授权 Astra/high 下一必要窄性能决定。只读当前 WT `/tmp/lerna-worktrees/content-snapshots-02` 的域 closure/service/ports/management、PG policy/facts/management、实际显式装饰器与指定日志。WIP 基于固定 `75202ee0077b5bf12b432b5017ad97a1dfcab681`；不是新的固定交付，也不证明 pending 修正或七AC已经退出。未运行 native/Go/DB/build、未修改源码、未读其他轴报告。

## 1. 默认采用：改一个现有端口，不加第二个旁路端口

**将当前 `Repository.CheckPolicy` 的 `action string` 改为 `actions []string`，限定同一准确资源、完整主体、同一用途的一组1–5个已知动作；一次锁定政策行、一次锁后 fresh DB Now、逐个动作取交集。** 继续返回当前 `*FixturePolicy/error`，不返回可复用 permit。PG 仍为唯一实际适配器，域仍掌握完整闭包和最终门。

这是当前实际“对同一政策行连续读三次”的职责收拢，不是跨 ref 批量政策系统。Put / publicationPolicy 的来源检查传 `[read, process, save]`；Get 来源传 `[read, disclose]`；AuthorizeUse 来源传单元素。现有目标 save / AuthorizeUse 目标等单动作调用传单元素；**Get 目标原先两次调用与 F1 授权观察顺序先保持**，各自传单元素，避免把这次优化扩成目标读取重写。nil-actions 结构遍历继续完全不调用政策端口。

保留一个名称/一条消费路径，显式实现旧签名的 Repository 装饰器必须同步更新，不会像另增 `CheckPolicies` 那样经嵌入底层 Repository 悄悄绕过原方法。无需 public schema、SQL迁移、0002 checksum 修改、新索引或配置开关。

## 2. 准确授权时刻与 PG 行为

PG 入口按现顺序保留 owner/token 校验、完整 subjectKey 严格编码/校验、同 tenant/owner/subject_key/content_id/version/purpose 的 FOR SHARE 查询、**真实锁等待后** `core.Now`、body 解码、tuple/purpose/ValidUntil 核对、完整 SubjectBinding 验证或已采用的严格相等复用。之后按传入有序动作逐项检查实际独立的 Read/Process/Save/Sync/Disclose flag，任何一项不允许返回 nil，不能 Read⇒Process、Save⇒Disclose 或以一个 flag 替另一项。

单个操作的含义是：在该次锁后可信时刻，当前这一行政策同时允许全部请求动作。政策在同一 Tx 内受 FOR SHARE 保护；逐flag检查是同一内存行的纯计算，没有第二个受管资源或外部动作。因此不需要为同一不可变行再次 SELECT/解码/哈希三遍，也不需要为三次纯 flag 检查分别读 DB clock。**这保留锁后资格时刻，明确把三个内部调用合成一个联合检查；不谎称保留旧执行中三个不同纳秒的采样点。**

动作集合必须有限且非空；未知动作、空集合、超5或重复动作均不得形成“空交集即成功”。推荐内部 fail-closed 返回 nil policy（与当前未知 action 的拒绝形式一致），不新增公开错误码。使用固定小集合，无需位集框架或动态权限表达式。实际域调用方只传上述闭合集合。

SQL/解码/可信时钟/严格主体校验错误仍直接保留真实 error；nil仍是资格拒绝，不变成 source_not_found。返回的 policy.Ref 仍由域在进入来源元数据观察前做准确 fullRef 核对；目标 Get 原 record.Ref/requestRef 的授权优先顺序保持。

## 3. 闭包、最终门与责任不能删

`registeredClosure` 将每节点原 action 循环的多次端口调用换成一次集合调用，成功后一次登记该政策的 ValidUntil/RetainUntil（同一行，原三次的最小值相同）。完整 tuple/fullRef/owner/cycle/≤64/中间版本、LockVersion、published/metadata 判定、带动作每节点原 pre/post-record Now 与 cap 检查，以及 caller 的最终 freshNow/bound 资格继续保留；post-record 时钟的位置作下述必要小调整。

不会把来源 CurrentRetainUntil 变成一次政策值；不会跳过后续来源的独立检查。Put 新准入和 alias 的 post-schedule 原键回滚/最终门、出版 I/O 前后独立 Tx、Get 两个披露门和 AuthorizeUse 各自独立 Tx 都仍取得自己的当前观察，不跨 Tx缓存。管理的原主体/用途 LockPolicy、basis、原 D/A due/deadline/watermark、自然期和历史责任完全不改；它不是 CheckPolicy 的联合动作消费方。

元数据/错误顺序必须是“该来源完整动作资格 → LockVersion → 等待后的当前资格 → 该来源元数据 → 后续来源”，不得为了进一步省时把所有来源 metadata 先取出再补权限。政策端口故障/拒绝仍在 LockVersion 前返回；**policy.Ref 精确匹配仍在域层、LockVersion 前完成**，不是只核同 tuple。

具体默认：LockVersion 本身返回错误仍保留原操作错误（不将 DB 错误冒成不存在）；成功返回后，将原已有 post-record Now 移至 nil/fullRef/publication 结果之前，先核本节点已锁 policy 的 `now < ValidUntil`（失效 forbidden），再核 `now < RetainUntil`（过期 expired），然后才允许返回 source_unavailable 或消费记录，并按同一次 now 核 CurrentRetainUntil。这只移动现有一次读钟并添加准确 bound 比较，不增加 per-node 时钟次数或再读政策；nil-actions 分支不因此新增时钟或权限检查。

理由是 flags 循环或随后真实记录锁等待均可能跨 ValidUntil。一次联合快照不保证资格到锁返回仍有效；不能让“最终 caller 才复核”之前的缺失/损坏来源元数据结果绕过已经失效的本节点政策。原逐动作 flag 都必须检查；任一 false 原本就 forbidden，联合时刻过期也 forbidden；联合时刻有效而记录等待后过期则先 forbidden，不以 source_unavailable 披露状态。保留原 caller 的整组最终门，覆盖后续来源/调度等待及成功出口；不声称不同实现的墙钟纳秒轨迹相同。Get **目标**的两次调用、trusted time/实际 recordRef 授权优先 F1 顺序不改。

## 4. 真实 consumer 与故障入口

本次实际 rg 发现显式 `CheckPolicy` 覆盖位于 `conformance/component/content_closure_test.go` 的 `closureTimingRepository`；更新其签名、原样传完整 actions、分别计端口调用和逻辑动作数，避免把“调用少了”报告成“动作覆盖少了”。其他现有 Now/LockVersion/Within/postScheduleFence 入口不变。实施时再 rg 实际新 WIP，不能假设此刻列表永久完整。

若需要按动作故障装饰器，覆盖**同一 CheckPolicy 方法**，在原传入有序 actions 中识别真实目标动作，注入指定 error 或对 delegated 实际结果作明确故障观察；不能忽略该动作、假装底层执行过三个 SQL，也不能让 mock 直接给一个永远允许的 policy。常规五动作拒绝以真实安装政策 flags 的独立 false 情况及 public 结果验证；机械 injected error 只标机械故障。

有限新增验证重点：正常三动作和两动作交集；每个必须动作单独拒绝（尤其 process=false/save=false/disclose=false，另已有 sync 单动作）；空/超5/重复/未知集合不被接受；真实政策锁等待跨 ValidUntil 后拒绝；联合通过后真实记录锁等待跨 ValidUntil 且返回缺失/不匹配 metadata 的组合先 forbidden，另未过期组合保留 source_unavailable；当前 fullRef/完整 delegation chain 不匹配及损坏政策仍原错误出口；出版前后和 Get 返回前撤权仍拒绝。每个故障保留正常对照。不为端口参数变化造第二套通用政策测试平台。

## 5. 证据、收益范围与停止条件

已实际读取 `closure-identity-reuse-race.log`：i63 Step，60.09s失败，native1/group_absent=True；不是未知外层退出，也不是完整64/65通过。Put/Step CheckPolicy 6112/11951次、约7.6305/14.7398s；LockVersion2144/4004次、约3.3677/6.3386s；Now4480/8262次、约2.0474/3.7552s；ScheduleRetention64次约13.4923s，其中包含子调用不能相加。

对正常三动作节点，本方案将同一 policy 的3次 SELECT+freshClock/解码缩为1次，单次节点省2组实际往返和重复身份工作；逻辑动作数保持3。新旧 CheckPolicy 调用的单位已经不同：新的一次可能核验三个动作，诊断须注明集合大小及逻辑动作数，不能直接用总调用数比例称吞吐或性能倍数。真实耗时还包含数据库等待、其他端口、责任调度和 CPU，不能据此宣称某个倍数或保证60秒通过。原 identity CPU profile 是旧采样且累计有重叠，本稿不拿它当新实现 benchmark。

该候选已有明确重复单位和当前端口计数支撑，**推荐由 sole owner 现在实施这个小结构修正**，而非继续盲挤编码 CPU 或先增加新的诊断接口。原60秒完整测试、120秒外层、业务 lease/budget/原 deadline 均不变；新编译/正常/完整 race 真正通过前，性能阻塞和七AC状态不变。若仍不足，先记录新的有限阶段/端口计数，再作下一具体判断，不累计无证据优化。

## 6. 本次读取的 WIP 对象指纹

这些 SHA256 标识本次所读工作文件，不能代替最终 commit，也不把 root 提供的9文件组合摘要猜成 Git SHA：

| 文件 | SHA256 |
|---|---|
| domain/content/closure.go | 4c4b0cf5ee0bf20389ae033fb76ff7305860cd5d3d803475024c636f9f50e9f6 |
| domain/content/service.go | f5b9062adcc52f2fb2bab36219bce771c32dc2b31c9cc807f5b980862b55e06f |
| domain/content/ports.go | 4208d1060d5c41f46367a0472e9e759fd39c98af1a8b39863a3831c363d14aa1 |
| domain/content/management.go | ffec01c66703998b0a467526eb0f446dd539497630ccbac0a2e969d39e3f295d |
| adapters/postgres/content/policy.go | e72b25a38667a25f14a6b17e2be0d6b10b690b85546d6518c11a6d60edaba12b |
| adapters/postgres/content/facts.go | 8d53cb773a4711ff9866f5974b399de96eec137d82a92b97886c0825802cb069 |
| adapters/postgres/content/management.go | 515210a582c48d2e7a8ac34b35b628ce211414c5cfe2b0a27a9afb491e34629a |

## 7. 最终顺序补充：联合资格后的记录锁等待

本节为 root 首轮全文读取后的明确采用补充；§3/§4/§5也已同步相同顺序、组合验证和诊断单位，不能仅以此前文本理解为“原metadata顺序逐字不变”。

**采用以下有限顺序：**

1. CheckPolicy 对同一资源一次锁后 freshNow 联合核全部实际 flags；error保留、nil拒绝。域层先检查该 policy.Ref 与该来源完整 ref 相同，失败 forbidden。
2. LockVersion；其真实操作错误照原 error 返回，不解释为版本不存在。
3. 对带 actions 的节点，立刻执行原本已有的 post-record Store.Now；将它从原 nil/fullRef/publication 判定之后移到之前。先核该政策 ValidUntil，过期 forbidden；再核该政策 RetainUntil，过期 expired。
4. 当前政策仍有效才判断 record 是否存在、准确 ref/published；正常记录再以同一次 now 核 CurrentRetainUntil。随后继续原完整遍历和 caller 最终资格门。

这相对原“元数据先于 postNow”的次序是**联合资格所需的保守当前授权修正**：多个 flag 可以同一锁定时刻一次裁决，但随后记录锁等待不能让失效资格仍获得 source_unavailable 等元数据差别。不能称原逐动作不同采样点/旧元数据优先级完全不变，也不能拿最终 caller 门掩盖中途早退。缺政策flag仍 forbidden；正常政策但已失效先 forbidden，正常仍有效的记录缺失才 source_unavailable。数据层实际 error 仍只是 unavailable/原错误，不伪造哪条事实存在。

没有新增每节点 clock，没有删除当前 checks；nil-actions 结构遍历不调用新集合门或clock。Get **目标**两单动作调用及既有 F1 实际 record.Ref 授权-before-disclosure 次序保持，不在本次重写。实施以完整新旧组合公开结果与有限锁等待红绿资格复核；本稿不声称已执行。
