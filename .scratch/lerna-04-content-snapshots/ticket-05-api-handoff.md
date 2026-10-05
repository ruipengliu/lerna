# 04票05当前内部API与资格（root已接受，整合交付待完成）

受测源码 `8db74e3dfd2677adb37fbdd13bd93e76b61b5eb7` 的七项AC已由root正式接受，核定文档提交 `94e9c289310601bbf1eab5580efe002028db0cc9`，见 [逐AC退出证据](ticket-05-exit-evidence.md)。worker正常merge root94e9至自身分支（merge30cb0c0，parents8db/94e9），已资格15份源码／SQL／test字节保持；本票本地resolved、累计22/41，实际root整合交付待完成，whole04仍in-progress、profile1.2 OFF。历史red、setup失败、原finite bounds、Close与推进事实保留在 [evidence](ticket-05-evidence.md)。不新增公开delete方法。

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

## 有界候选扫描（受测8db，root已接受）

Repository新增 `ScanContentPhase` 仅publish/policy_propagation，SQL phase限定先于LIMIT，Service最多两页各64候选、publication优先，Manager仅policy页。LifecycleRepository新增 `ScanBodyCleanup` 根据原完整保存Subject、primaryHolderID与尚可执行原seal窗口在LIMIT前选择。合法不同Subject/holder与原已到期候选排除；missing/非法JSON/形状/原record与seal不一致保解析错误或明确scope错误，不假空页。时间预选有1µs保守容差，锁后Go原fresh clock/Claim与完整身份仍决定资格，原Version→Job锁序/fence/allACK保持。runtime Core.Scan/迁移/Job状态与64旧责任/原bytes/deadline/history无修改。相同六个产品源已格式化，原64 public business case 正常20.418s/竞态26.476s完整尾真实通过；ONE Host坏deadline actualGo ParseError及合法特殊字符原primary/seal正常全ACK/独立absence/reopen尾另normal0.671s/race2.182s完整尾；不泛化为所有坏row/编码；第三Manager与合法完整委派Subject另有下述独立资格。

## 可披露最小元数据

MetadataPolicy固定准确ref、完整Subject、Purpose、Revision及ValidUntil，仅授权ref/evidence_available视图，不是第六正文动作或Grant。Get锁后核准确fullRef与fresh时间，全部传递祖先当前metadata许可适用，query不创工作。未gone的metadata-only不返回正文。PG列比较采用微秒编码一致性，当前许可仍按原JSON纳秒deadline精确判断。三层祖先/错delegation与purpose/真实700ms到期、原body保持已有normal/race资格。

## 最新检查、审查与资源事实

[受影响27项检查](ticket-05-expired-job-scan/final-affected-checks/README.md)在同一冻结15源实际完成：format、integration vet、非integration module normal/race各472 RUN/PASS，24原baseline与7 policy consumers各normal/race、三原SQL hash，以及低control、真实Linux正向进程、冻结原writer升级、三编号迁移／全部原SQL SHA、原policy／holder pages五个必要integration selector各normal/race。全部native exit0／实际Wait／groupAbsent／noTimeout，原预算保持；nonintegration module不冒充taggedPG或Linux。原outcome计数／scope登记缺口用sidecar澄清，原字节不改。

原60f业务反例full64 normal20.418s／race26.476s执行后续V2及live全ACK与旧64完整职责／alpha／receipt／history重开尾。Host坏deadline与合法特殊字符原primary/seal normal0.671s／race2.182s保Go ParseError并执行真实独立ACK／absence／reopen／metadata尾；Manager normal19.681s／race27.528s推进第三消费者原责任；完整合法委派Subject normal6.510s／race13.953s仅清自己的责任并保持其他64仍live。

[固定1a4...8db独立双轴](ticket-05-expired-job-scan/current-reviews/README.md)为Standards0硬性违规／1非阻塞P3，Spec缺失／范围扩张／实现错误各0。root采用[窄决定](ticket-05-expired-job-scan/current-reviews/acceptance-decision.md)，KEEP两个场景私有观察记录，无必要产品修正。此前[1a4审查](ticket-05-review-1a4e1d2/README.md)及[历史final17](ticket-05-final-checks-1a4e1d2/README.md)保持历史范围，不代替新source资格。

[当前只读资源观察](ticket-05-expired-job-scan/current-resource-audit/actual-summary.json)实际native3584995／start15030462／exit0，实际Wait及组消失；142原schema、14登记backend、52原进程组无当前残留。218准确路径中216不存在；观察连接588251在Tx前登记，原firstClose与rollback均成功，没有删除或DROP。两个历史unknown目录3977538271（33:315225）与4116685529（33:326495）仍匹配原inode，永久保留原UNKNOWN。absence不补历史logical Close，旧02未知scope仍受保护；未重复SIGKILL原unknown case或伪称新候选fresh RUN。

## 整合交付边界

root已接受本票七AC，worker已同步准确root94e9并更新本票resolved。本地票状态22/41；实际worker回integration的交付merge、准确push／新CI与票06frontier由root随后记录。whole04／完整1.2仍未接受或广告，CI不作为票06直接依赖之外的隐藏阻塞。证据只覆盖本机真实PG/Linux，不声明WAL／备份法证擦除、已披露字节撤回或生产多机保证。
