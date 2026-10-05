# 04票05当前内部API与资格（未交付）

原候选 `1a4e1d2d704261a59491dfc6e0c5d26b80ec5107` 已完成历史 final17。当前为 `85a1067` 后的窄扫描修正候选，原64及补充消费者已正常/竞态资格，受影响27项检查亦已实际全部通过；本票七AC仍 claimed，whole04 为15/41、profile1.2 OFF。此处说明当前行为与剩余资格；历史red、setup失败、预算、Close事实和推进次序保留在 [evidence](ticket-05-evidence.md)。不新增公开 delete 方法。

## 封闭与真实清理

`Lifecycle.Seal`按有限受信配置核准确ref、原完整保存Subject/Purpose、固定SealID及初始Deadline。同一PG事务保存单调seal、原两holder责任与原Job。重传必须完全匹配原seal/deadline，查询不创建工作。`SealOrphan`共享原Version锁，published活引用拒绝；只选择实际preparing/failed原记录，不遍历其他scope。真实迟到Put/Finish不能发布sealed版本、重建已gone正文或恢复当前ObjectHolder。独立Version2仍正常。

`Lifecycle.Observe`返回有限holder页；CleanupComplete基于全部原holders，首个空页、Claim或seal不是物理ACK。`Lifecycle.Step`在原Claim、准确Seal与原Deadline内，先实际清PG staging并独立事务ObserveStaging，再执行primary文件删除与独立受锁ObserveErasure。原身份ACK与完成/有限Defer在同owner新Tx提交。deadline不因重开续期。BodyGone只表明权威staging+primary独立确认不可回读；secondary pending时全holder cleanup仍不complete。

`Lifecycle.CopyToSecondary`固定真实不同root binding、原CopyID、完整保存主体/用途、原attempt与effectDeadline。当前全部祖先read/save/sync均适用；Tx外primaryRead→secondaryPut→独立Read后新Tx重核资格。seal分页包含原pending及confirmed复制，不以当前配置发明旧holder。真实离线/ENOTEMPTY与恢复、独立两页2+1 holder、全部原publicationattempt页已资格；未知准确key临时文件继续residual，产品不猜删。

## 原政策责任与期限

`ConsumePolicyCleanup(ctx, subject, originalKey, cursor)`只消费原有限责任页。短candidate Tx结束后，新Tx完成全页policy/Version与全部祖先保存basis资格，再锁完整责任CAS，最后fresh时间门；任一失配全部rollback。暂时save撤销已恢复且cap仍live、尚未sealed时可NotRequired，保留历史原因/actions/deadline/attempt/pub事实。read/process/disclose-only撤销不授权删除。nil/错误policy不解释为Save=false。query及CleanupPending不隐式Seal。

原持久accepted cap真正到期是独立保存失效原因，当前宽Save续期不能提高旧cap。目标化AdmissionTarget复用原admissionKey(fulltarget, originalpolicy, originalExpiryDue)，核完整原shape/owner/Subject/Purpose/Revision、natural阶段、nil Previous、原due/deadline及有限expirybudget。仅exacttarget的accepted_retention_expired pending/holder_unconfirmed候选可接现有cap支路；全部结构/原保存basis/currentpolicy读与原responsibilitydeadline<=原ExpiryDeadline、末fresh clock保持。普通changeKey不变。实际missing结构/currentpolicy port错误保原pending、无Seal/原bytes，原错误传播。

policy seal的immutable PolicyChangeKey连原完整ref/Subject/Purpose、原责任Deadline与稳定sealID。所有原holder独立ACK后准确原责任CAS为erased，再fresh时间/Claim/原Deadline完成。已erased责任的自然到期登记保终态与原Reason/AttemptKey/Publication、historical holder OR；Actions union、Deadline取较早。当前ObjectHolder在权威真实ACK后false；历史holder责任不抹除。

普通与目标化700ms原deadline真实policy锁等待分别已有normal/race资格：deadline内实际rollback/连接Close后allACK；原deadline+20ms后放行保持完整pending、无seal/activecleanup与原bytes。整页真实CAS竞争保持first原pending与second contender已提交结果，证明早写整Tx rollback。原预算未扩大。

## 原物理介质与legacy升级

`Objects.Binding()`来自Open实际目录FD设备号/inode，纯访问器不是用户输入或CloseACK。最初preparing接纳持久原PrimaryHolderBinding与独立PG列，原publication attempts同绑定；既有记录不能换root。publish startup/final、Put/Read、Seal/Claim与Fence/Observe各实际入口核原介质。wrongroot与first-Seal、真实重开拒绝已有资格。

Linux适配器的固定原key `.lock` inode永久保留；flock包围实际Put/Read/Erase/Observe，sealed/pending marker不含正文。FenceAndErase只删准确finalkey和明确登记attempt，另一次ObserveErasure确认。body nativeClose未知保留lockFD/holder责任。真实跨进程Put→Erase及Seal→latePut拒绝两序已分别资格；SIGKILL原eraser的logicalClose仍unknown，独立重开Truth不能补它的Close。非协议旧writer必须先真实停机才能升级。

`LifecycleConfig.LegacyPrimary`已实施有限host原scope资格并深复制。`BindLegacyPrimary(ctx, trustedSubject, {Ref,Purpose})`只绑定原介质责任，不授正文权限、不发布、不续cap、不擦除。host根据冻结原writer的prestart duty/PID/PGID/start、effectgate与显式Objects/Store Close、actualWait/groupAbsent形成资格；公开caller不得传停机bool或认领empty/copyroot。仅真实published、完整原actual attempts可绑定；未知/failed/preparing保守拒绝。

binder两短Tx之间真实原介质Read/hash/length；PG专用wholeRecord CAS从空binding固定qualificationID/digest。普通SaveVersion不能换绑或注入provenance。同fixedq幂等且仍核原qDeadline。portable75资产与producer可在fixture/CI有限重建，历史one-off脚本不参与入口。normal2.694/race4.267、wrongroot/firstSeal/allattempt控制，以及wholeRecord失配2.273/3.852、已确认commit返回replyloss2.316/3.832、首次q700ms整Tx回滚2.702/4.133分别真实资格。replyloss不是PG commit_unknown；unknown历史scope不获升级。

## 有界候选扫描（候选已通过业务与受影响检查）

Repository新增 `ScanContentPhase` 仅publish/policy_propagation，SQL phase限定先于LIMIT，Service最多两页各64候选、publication优先，Manager仅policy页。LifecycleRepository新增 `ScanBodyCleanup` 根据原完整保存Subject、primaryHolderID与尚可执行原seal窗口在LIMIT前选择。合法不同Subject/holder与原已到期候选排除；missing/非法JSON/形状/原record与seal不一致保解析错误或明确scope错误，不假空页。时间预选有1µs保守容差，锁后Go原fresh clock/Claim与完整身份仍决定资格，原Version→Job锁序/fence/allACK保持。runtime Core.Scan/迁移/Job状态与64旧责任/原bytes/deadline/history无修改。相同六个产品源已格式化，原64 public business case 正常20.418s/竞态26.476s完整尾真实通过；ONE Host坏deadline actualGo ParseError及合法特殊字符原primary/seal正常全ACK/独立absence/reopen尾另normal0.671s/race2.182s完整尾；不泛化为所有坏row/编码；第三Manager与合法完整委派Subject另有下述独立资格。

## 可披露最小元数据

MetadataPolicy固定准确ref、完整Subject、Purpose、Revision及ValidUntil，仅授权ref/evidence_available视图，不是第六正文动作或Grant。Get锁后核准确fullRef与fresh时间，全部传递祖先当前metadata许可适用，query不创工作。未gone的metadata-only不返回正文。PG列比较采用微秒编码一致性，当前许可仍按原JSON纳秒deadline精确判断。三层祖先/错delegation与purpose/真实700ms到期、原body保持已有normal/race资格。

## 当前验证与尚未闭合范围

final17的准确命令、原日志、SHA与actual native ACK见 [原候选检查](ticket-05-final-checks-1a4e1d2/README.md)：format、integration vet、非integration全Go normal/race各472 RUN/PASS，24 baseline与7 policy consumers分别normal/race，三冻结SQL hash，全部native exit0/groupAbsent/noTimeout。非integration全Go不执行Linux integration holder测试；这些结果不是新增scan源码的资格或whole04 CI。

[固定候选双轴](ticket-05-review-1a4e1d2/README.md)的Standards文档状态矛盾由本次当前状态整合处理，可选legacy replay重复保持。Spec唯一P2为64个原到期body Job遮挡后续publish/livecleanup/policy work；root已采用窄Content consumer候选扫描决定，已完成真实64 public首red（新V2 preparing/独立key缺失）；最小产品修正已格式化，原60f817测试在相同初始finite bounds正常20.418s/竞态26.476s完整尾真实通过，全部64旧责任/字节与历史保持。ONE Host坏deadline保Go ParseError与合法特殊字符原seal正常全ACK/独立absence/reopen/receipt/metadata已normal0.671s/race2.182s完整尾通过；第三Managerconsumer behind64expired normal19.681s完整传播/reopen旧职责尾另通过；合法同tenant/leaf不同完整委派Subject在64原live责任后只清own duty另normal6.510s完整尾通过；Manager/委派同源竞态27.528s/13.953s完整尾亦通过；同一冻结15源的新源低control、actual Linux正向进程、冻结原writer升级、固定3版本迁移test、原policy/holder pages及受影响suite正常/竞态已实际通过；新pin双轴审查、资源审核与root正式接受仍待，不称finding正式闭合或七AC已接受。

当前受影响27项实际检查与15源pin验证已完成：format、integration vet、非integration module normal/race各472 RUN/PASS，原24 baseline与7 policy consumers各normal/race、三SQL hash及五个必要实际integration selector各normal/race，全部native exit0/groupAbsent/noTimeout。原初始finite bounds未提高；迁移test仅纠正实际三编号/全部原SQL SHA的过期oracle。完整原raw、预启动pin和实际ACK归档在 [新候选受影响检查](ticket-05-expired-job-scan/final-affected-checks/README.md)，实际汇总为 `scan-current-final-checks/batch-complete.json`；计数/原scope区间/独立groupabsence的澄清sidecar保留原outcome字节。Linux killed原unknown case未重复，不称其已在新候选fresh RUN。

owned资源实际审核、新pin双轴审查、root正式七AC接受/合入与准确新CI仍待。此前旧unknown roots永久保留：`lerna-local-lifetime-3977538271` dev33/inode315225与`lerna-local-lifetime-4116685529` dev33/inode326495；kernel/group absence或独立Truth不替原logicalClose。旧02未知scope继续受原台账保护。不声明WAL/备份法证擦除、已披露字节撤回、生产多机保证。
