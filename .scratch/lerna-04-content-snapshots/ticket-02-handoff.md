**正式采用：2026-10-04。** Root已接受01全部8AC，交付1a7、正式merge eba、检查点7c0bce515f7b2fa443e2d00e1b14cfee22dccfa9已实际push。原01 worker明确释放唯一LOCAL执行槽。

Root全文读取下列Astra high条件接法，实际核当前ports/identity/PG facts/policy/0001 SQL与原46对象相等，14ead修正仅Get准确授权顺序且已接受。由此采用以下小范围接法作为02实施依据；原“不可claim/须等01/F1open”保留为准备时历史，当前前置已满足。02仅7AC，物理删除仍归05。缺少本片实际执行时不得宣称02已完成。

完整≤64闭包含中间版本、用途/动作独立、资格取当前真实政策/原caps；收紧与责任同Tx、全部后代按冻结水位分页，不限制为第一页或64后代。查询不创造责任。1.2/旧1.0/1.1及原receipts保持，追加0002而不改0001；新管理入口不冒充客户端Command或生产Grant。

实现时兼容性须针对实际测试处理：首票已受信安装的same-identity异完整Ref反例，及新管理入口对错误绑定的拒绝都要保留真实允许对照。不能删掉原Get完整绑定保护或用私表注入冒充公开业务验收；任何新未覆盖实际冲突仍由授权Astra high分析后明确采用。

---

# 04 票02 条件接法：完整来源与政策收紧责任

2026-10-04。只读基于 tested-source `46d6ca26e4c2a4e9cf6db95e890b641f6bf2aa97` / delivery `72da5ee820a69695553ff208e10a8ba31804d0b8`，以及04 spec/decisions/final-api-handoff、票02七AC。**这是条件准备，不能实施/claim：硬等票01接受合入及其最终真实 SHA/API 复核。** 当前 F1 target-policy 授权顺序仍待新 pin 关闭；本稿不把该修正当已落地。没有 native/build/test/DB、服务、资源或仓库修改。

## 已采用约束与实际可复用接口

已采用：同 tenant/Content owner；全部已登记来源及中间版本计入≤64；用途逐动作取交集、保留期只收紧；最终发布同 owner Tx 复核；撤权先封使用再登记传播/清理，正文删除归05。外部 sources 只证明声明，不证明真实完整生成过程。首票1.2三方法不扩成 delete/Grant，完整 profile 不提前广告。

实际 `domain/content/ports.go` 已有 Record.Sources/Purpose/Subject/CurrentRetainUntil、StagingHolder/ObjectHolder/CleanupPending，以及 LockVersion/SaveVersion/CheckPolicy/Job/Claim/Now。`TupleDigest` 固定原 put payload，sources 按集合规范排序；`VersionIdentity` 仅取 owner/id/version，完整 ref 另核。实际政策管理仍是 PG `InstallFixturePolicy` 自己更新一行；Repository 没有政策写入、反向来源、传播或责任观察端口。0001 jobs 限 `phase='publish'` 且 FK 指向 Content version。以下是补足这些实际缺口的默认选择，不声称已有接口。

## 1. 完整闭包：保留原 tuple，派生资格另算

**采用：Record.Sources 仍表示原准确直接声明；不把展开后的集合塞回 sources 并重算原 TupleDigest。** 领域实现一个私有、有界 closure 操作：遍历直接 refs，逐个锁/核准确版本并继续其已登记 Sources；闭包包含每个直接与祖先中间版本，不含正在创建的 target。每个新唯一版本立即计数，第65个即 input_over_limit，无截断或“只留叶子”。

按 `(owner,id,version)` 去重，同时保存完整 ref；同身份异 hash/media/length 是冲突，不可合并。同路径已访问祖先、自引用或图环 fail closed（采用既有 rejected integrity；不制造新错误枚举）；共享祖先正常去重。合法 immutable 版本只能依赖已 published 的准确来源，故新接纳不会形成环；仍需识别历史/存储不一致，不能死循环。跨 tenant forbidden，跨 owner unsupported；未知数据/DB 错为 unavailable，不能当空来源。

每次新 put（含新 Command 同声明关联）、出版启动/最终提交、当前 Get 两个 gate 都用完整闭包。首票遗留记录也不能只检查其直接集合。当前64上限限制**单个版本来源闭包**，不限制某 source 有多少后代；全后代传播不能在第64个停下。

不要求新公共字段：来源索引可保存完整闭包的准确边 `(ancestor exact version → target exact version)`，同时保留原直接边标识；这是从不可变声明可重建的 owner 投影，不能成为宽松授权真值。建立/核验索引与新 Content 记录/receipt/Job 同 Tx；完整权限仍读取真实政策/版本，不只信索引标志。

## 2. 五动作与当前资格的最小接法

沿已采用矩阵：Get 对 target 和闭包每项要求当前 caller/get.purpose 的 read+disclose；put/关联及出版要求 target save、闭包 read+process+save（出版用原 Record.Subject/Purpose）；process/save 不被偷加到 Get。sync 是独立动作，缺许可拒绝，不借 read/save 放行。当前没有同步传输，票02只能通过受信内容管理/使用裁决入口验证 sync 动作判定，不广告已同步副本。

新增的受信使用裁决只返回当前许可/拒绝和明确限制，不是可离线复用的授权 token；后续真实行动仍重核。限定准确五动作，不建通用 policy DSL。相同具体 purpose 必须在全部适用政策上获准即是本次用途交集；不需要先建任意用途集合引擎。policy.Ref、subject、purpose、当前 revision/ValidUntil 及 source exact-published/CurrentRetainUntil 全部核验。

当前保留 cap 取请求/原 Effective/原 Current/全部适用来源最严值；政策 ValidUntil 约束此次资格，不冒充正文删除事实。保持初始 Get AcceptBefore 的准入语义、最终披露门禁与授权优先 F1 顺序；所有阻塞锁后重采 DB Now。原 Command 同摘要重放仍只依赖当前原命令 reader/主体，不重新跑已经失效的 put 准入。合法 failed 新关联不套旧 PublishDeadline、不复活 Job。

全闭包≤64允许在一个有限 Content owner Tx 中锁定其准确政策/版本，最终出版沿同样集合复核；不在 Tx 内读对象正文。所有 gate 使用一致的确定性来源次序；死锁/超时是本次未知/不可用，不报告发布成功。对象读/写已在 Tx 外开始的有限窗口不能声称被撤权瞬时物理停止。

## 3. 收紧政策的领域管理步骤：先封使用与耐久责任同提交

**必要职责移动：** 新增 `domain/content` 的受信 policy-management 方法（可同 Service 的显式 host 方法），处理 expectedRevision CAS、完整准确绑定、变更分类、传播与清理责任。fixture 宿主通过它安装/收紧；PG adapter 仅提供同 Tx 锁读/写政策、存责任、索引分页与原 Job primitives。不得继续让原 adapter 管理入口单独改政策而绕过责任；可保留兼容包装供测试装配，但必须走同一领域步骤。

变更身份固定为准确 policy key（owner/subject/resource/purpose）+新 revision，绑定规范完整政策摘要。同 key/revision/相同内容的管理重试返回原管理观察；异内容 conflict；旧 expectedRevision 不覆盖新事实。它是受信管理操作，**不是新增客户端 Command 方法或生产 Grant**。

对已存在准确 source 的动作撤销、ValidUntil 缩短、RetainUntil 收紧：同一 owner Tx 写新 policy revision（各当前 gate 从此拒绝不合格的新动作）、保存原 change 传播责任/光标及本 source 已知 holder 的清理核对责任，触发原 source 版本的 `policy_propagation` Job；这些一起提交，失败一起 rollback。不能先改 policy、事后 best effort 创建责任。

政策可在 source 创建前安装。准确版本根本不存在时，不伪造 Content 或挂违反 FK 的 Job：该政策键的耐久变更观察可确认当时无本 owner 已登记 Content/后代；后来 put 必须锁同准确版本身份并以当前政策接纳。版本存在但 policy.FullRef 不匹配则拒绝管理更新，不用错误完整 ref 传播到实际版本。

传播与当前封使用解耦：所有使用 gate 都直接核完整 closure 的最新政策，**因此不等待后代分页完成才拒绝**。传播只登记/收紧派生责任与有据可查的 holder 后续核对，不是最终授权缓存。

## 4. 全后代分页、代次和期限

采用 owner 投影中的 ancestor→descendant 索引，不要求递归传播成通用图执行器。每个原 source 的传播 Job 下有按准确 policy-change 身份去重的待处理项；source 的每次排队用独立单调 work revision，不能拿不同主体各自的 policy.Revision 当同一 Job 的全局递增版本。每轮仅处理有限页（默认64，可配置更小测试），页游标按完整稳定身份排序，进展/子责任/Claim 条件推进同 Tx。

登记全部后代不能依靠“第一页空”或 offset 跳页。change 开始固定 owner 来源索引 generation/watermark；准入新增边与 generation 提升同 Tx。处理冻结水位内所有页；变更后的新增派生必须在自己的接纳 Tx 重核当前政策并单调继承约束，不能越过已封闭资格。传播完成意味着该 change 的完整旧水位已登记，不意味着所有 bytes 已清理。迟到旧 change 只做交集/幂等登记，不把较新 cap/撤权覆盖回去。

原绝对 deadline 与每轮有限处理预算随责任固定，不因重开刷新。超时保留 pending/residual/阻塞原因，不能“成功跳过剩余页”。有限 worker 调用退出可以留下耐久可恢复责任；未知 holder 不因任务过期而失去负责方。

政策自然到期也不能只靠下一次 query 才建清理责任：新准入/政策收紧时，登记原绝对截止的维护待办，由同类 owner 工作检查到期并推进。若沿现有 Job，每个 source 的 work revision 与最早 due 需由实际 adapter 单调维护，避免新 revision 把更早待办推后。查询仍不创建任何 Job。

## 5. holder责任与公开观察，不提前实现05删除

新增最小耐久 `PolicyChange/PropagationObservation` 与 `ContentCleanupResponsibility` 事实：原 change key/revision、准确 ref、原责任身份、适用主体/用途/受影响动作、传播水位/游标、原 deadline、已知 staging/object holder、待核实 residual、当前状态/原因。已有 StagingHolder/ObjectHolder 是首票责任记录，不是物理存在的最终证明；尤其 in-flight attempt 字节可能迟到，必须保留原 AttemptKey/Claim 信息或关联原 publication record。

**动作分别处理：** 单独 revoke disclose/read/process 不自动等于所有主体的 save 许可撤销，更不授权删掉其他用途的正文。它们登记受影响使用/传播核对；只有原保存依据失效或真实适用 retention 到期，才登记该 holder 的正文清理 pending。不适用清理可明示 not_required 与当前依据；不能把 pending 伪记 erased。原出版历史、固定 receipt 保持。05承接这些准确责任做 fence/delete/确认，本票不添加 Objects.Delete、不返回已擦除。

管理写入口和只读管理观察入口由显式受信 host 能力提供；观察有准确 owner/主体/用途许可、有限分页/cursor、当前 auth 和读取截止，不回原文，不对无权者泄露来源/后代存在性。**测试经这些声明的管理 interface 观察真实责任、重开再观察**，而非 SELECT 私表或看内部 Job 数量。标准 content.get/command.get 继续原闭合合同，不塞额外字段。管理入口不是 Application SDK 支持方法，不新增 profile 广告。

## 6. 实际最小 ports / migration /升级

- 消费方小端口增加：锁定/保存当前 policy（管理 CAS）；保存/按原 key 读取 change 与 cleanup responsibility；按冻结水位分页 descendant refs；保存准入来源边；相关传播 progress/Job 操作。签名在首票 final pin 后定，仍由 domain 声明、PG 实现；不令 domain 导入具体 PG 或 conformance。
- **追加 Content owner 0002**（实施时核实未占号）：来源索引/generation、policy-change 与 cleanup-responsibility 表；将 jobs phase 约束明确扩到实际 `publish`/`policy_propagation`，不编辑已交付0001/checksum。保留现有 Content version FK；传播 Job 只挂真实已存在 source version，前述未创建政策另记管理事实。
- 当前 Step 会把非 publish Job 当 scope 错，不能只加数据库 phase：扩为对这两个真实 phase 的显式 dispatch，或 repository 按 phase 扫描的两个具体 worker；默认前者，仍沿原 Claim/有限调用，不造动态 handler registry。05 cleanup 仅 pending事实，当前不新增假清理执行器。
- 旧01记录保持原 Sources/TupleDigest/receipt/object key。升级采用明确停旧 writer 的本地受控窗口，追加 schema 后由新 owner 做有限可恢复的旧记录索引回填；未完成全量水位前不得宣布传播 ready。原查询须完整遍历实际来源或 fail closed，不能把尚未回填当无后代。错误/环/>64的旧记录保留历史但新使用拒绝并有明确限制，不删库重建。
- 不广告旧01 writer 与新 policy 模式混跑；没有引入16动态升级前置。实际从01正常/已failed/有直接来源数据升级、原 receipt无损、新授权 gate和回填恢复必须覆盖；真实变动只在新增 Content owner migrations/内部事实，不动1.0/1.1/archive。

## 七AC对应的独立观察出口

| AC | 本票正常与拒绝/恢复观察 |
| --- | --- |
| 1 五动作准确裁决 | 真实政策当前主体/用途/ref/revision各自允许；逐动作撤销互不冒充，缺失/失效拒绝；sync只证明判定，不证明传输 |
| 2 完整有限闭包 | A/B→中间M→输出N，包含A/B/M；共享祖先去重；准确64正常/65拒绝、异完整ref/环/跨owner拒绝，不截断 |
| 3 交集/期限 | 全部来源共同用途正常，隐藏祖先用途拒绝；原严格 cap经再派生与重开不刷新 |
| 4 当前最终Tx | 实际出版前收紧祖先政策则不发布；先发布后撤权，新的完整 Get/处理门禁拒绝；各有正常对照，不声称物理瞬停 |
| 5 责任原子与恢复 | 受信管理更新后公开见立即封使用和原change责任；真实重开、故障恢复继续原分页；超过一页后代全部登记，pending holder仍可见，不报告删除 |
| 6 无泄露/真实声明范围 | 无权 caller 不得从状态/来源/cursor泄露；合法主体可正常派生/读取；来源声明和实际生成真值明确分开 |
| 7 真实出口 | 实际PG+本地Content、公开业务/受信管理interface及独立字节；至少政策change同Tx失败/重开与分页中断正常恢复；不靠私表业务oracle |

本稿新增的是当前实际缺口的默认接法，不是先建大治理平台。首票正式合入后先核其最终 ports/SQL/关闭/读取顺序的实际差量，再采用为票02实施 handoff；当前无票02实现或执行证据，也不将未来外部服务当本次准备阻塞。
