# 04票05：实际执行记录（进行中）

Base `1373112472761d32c22ed6d7603d806d8ef9a3c7`。独立WT为
`/tmp/lerna-worktrees/content-snapshots-05`，分支`codex/content-snapshots-ticket-05`。
票七AC尚未接受；本记录不证明跨进程、PG Lifecycle、第二holder或whole04退出。

## 第一vertical：准确holder封闭与重开

采用的测试seams为受信holder入口及独立准确对象字节；规格和正式handoff
已经授权，不重新请求确认。第一测试
`TestErasedExactVersionCannotBeReinstalledAfterReopen`先完成真实正常Put、
独立读取`alpha\n`，再在合法`FenceAndErase`的fail-closed scaffold产生业务red。
没有把undefined符号编译失败当业务red。

唯一LOCAL执行槽由root明确授予，本轮所有Go/gofmt步骤串行。
overlay为`/tmp/lerna-04-ticket05-execution`，0700、dev33/inode314996；
root和父目录实际fsync，`root-ack.json`保留ACK。有限120秒wrapper先固定
PID/PGID/starttime并fsync责任账本，再放行child。DSN仅由child env读取，
没有输出到日志或仓库。

| 原日志 | 实际结果 |
| --- | --- |
| `erasure-first-red.log` | PID/PGID2639982，start10986091；正常正文通过，合法清理stub在测试line31失败；exit1、groupAbsent=true、无timeout。 |
| `erasure-first-green.log` | PID/PGID2645985，start11012248；exact单测试实际通过，exit0、groupAbsent=true、无timeout。 |
| `erasure-first-race-close.log` | PID/PGID2646308，start11013321；exact同测试+`TestCloseDrainTimeoutRetainsActiveInvocationUntilRealReturn`实际race通过，exit0、groupAbsent=true、无timeout。 |

green实现所有协议Put/Read/Erase/Observe使用永久稳定lock inode及flock；
元数据pending/sealed marker阻止原key重建，文件Sync/Close和目录Sync成功
后才确认fence。Erase仅删准确final key与显式登记attempt；发现未登记
同key临时字节只保留residual。Observe独立重开marker/目录，有限页覆盖
同key正文残留，元数据不算正文。正文Close未知保留协调lock FD，后续
sticky Close不能提前释放它。

首red的root`lerna-local-lifetime-3977538271`（dev33/inode315225）未获得
测试内Store.Close确认，原责任及root保留。之后green和进程exit不补其
单个Close ACK。其他原01/02 resources、所有其他WT/overlay均未清理。

本轮已明确release LOCAL，无pending native。后续每次native重新等root授槽。
以上本机Linux真实文件/Sync/reopen及机械drain测试不证明掉电、法证擦除、
外部旧writer遵守新协议或生产多机保证。

## 第二vertical：原保存主体自愿封闭与重开

`TestContentTrustedSealSurvivesReopenAndPreservesOriginalReceipt`先正常真实PG
接纳/对象发布/public Get，再由受信原完整保存主体对准确原ref/用途进行
显式自愿Seal。首red为合法Seal scaffold返回unavailable，未改normal前态。

| 原日志 | 实际结果 |
| --- | --- |
| `body-seal-first-red.log` | PID/PGID2655388，start11050506；合法Seal在line37业务失败，exit1、groupAbsent=true、无timeout。 |
| `body-seal-first-green.log` | PID/PGID2664908，start11091867；exact正常测试通过，exit0、groupAbsent=true、无timeout。 |
| `body-seal-first-race.log` | PID/PGID2665345，start11093335；exact同测试race通过，exit0、groupAbsent=true、无timeout。 |

green新增Content0003（仍为本票未发布迁移），保持0001/0002实际SHA为
`00363b79dafb6eb1f373be9ef08e915fe23cc3db0fa55345e8ae6c051ce0c1ed`/
`99519565ff1146d7cfc468413b449538c6ba71335e86edc8fd447a3bbff922a8`。
同Content owner短Tx保存不可逆BodySeal、staging/primary holder责任及
原body_cleanup Job。全部真实publication attempt在Tx外Put前同Tx登记。
SaveVersion SQL不能清空或替换既有seal；原published史和Commandreceipt不改。

Seal后Put的新Command关联和最终准入、Get两个current门、动作closure、
AuthorizeUse及publication startup/final policy都消费同一BodySeal。
重开观察原seal/deadline/holder；新association与body Get拒绝；原Command
固定receipt及published progress保持。seal单独不擦除：独立key仍有原正文，
所有holder仍pending、CleanupComplete=false。Service.Step跳过body_cleanup，
没有claim或complete它；后续真实Lifecycle.Step才是此phase消费者。

该vertical不是旧policy事件自动强删。save-only pending尚未seal时原合法
read/disclose保持；后续policy责任入口必须在封闭前同Tx重核原完整保存
subject/purpose/current exact source basis和单调cap。短暂布尔撤销已恢复且
cap尚有效的未sealed版本可not_required并保留历史责任/reason；已sealed或
过期cap不可复活。不增加第六个正文action。

该轮所有native已实际completion，并明确release LOCAL。没有完整migration
旧writer、policy封闭、physicalerase、metadata-only或七AC接受的声明。

## 第三vertical：真实staging/primary删除及独立metadata-only视图

`TestContentActualStagingAndPrimaryErasureAllowsOnlyAuthorizedGone`使用真实
preparing与published两个正常前态。先经受信独立连接的staging观察，再
显式Seal及原Lifecycle.Step，重开后核staging NULL/精确final key缺失和
两个holder ACK；无metadata资格拒绝，独立明确metadata资格才返回准确gone。

| 原日志 | 实际结果 |
| --- | --- |
| `body-erasure-first-red.log` | PID/PGID2675689，start11138870；两正常前态后的合法ObserveStaging scaffold失败，exit1、groupAbsent=true。 |
| `body-erasure-first-green.log` | PID/PGID2688356，start11192152；Step真实拒绝违反原DeferClaim due>now的continuation，exit1、groupAbsent=true；不是green。 |
| `body-erasure-green-defer-repair.log` | PID/PGID2690102，start11199063；同原deadline内1µs后续due满足既有bounds，actual normal0.493s、exit0、groupAbsent=true。 |
| `body-erasure-race-seal-control.log` | PID/PGID2690794，start11201508；第三exact+第二sealed控制actual race2.573s、exit0、groupAbsent=true；0001/2真实hash仍等原冻结值。 |

每个native都有原120秒completion ACK，无timeout。ClearStaging是真正
SaveVersion Bytes=nil、独立PG事务核staging IS NULL，不把空bytea视不存在。
primary按登记attempt分页执行受锁durable FenceAndErase，再独立另一次
ObserveErasure；新Tx核原Claim/Seal/期限后才holder ACK。只有staging+primary
均独立ACK才单调BodyGone；全holder完成仍是另一个查询，不代表离线副本。

BodyGone与seal均有SQL单调保护。Get body拒绝后才尝试独立metadata资格，
后者不消费body cap或五动作，仍核当前fullRef/完整subject/purpose/revision、
锁后真实clock/expiry、适用全部来源的明确metadata资格。该test只覆盖零源
正常metadata与无许可拒绝，祖先metadata、过期、错fullRef等另待tracer。

scope限制：当前正确配置的一primary root正常删除已经通过；holderID尚
不足以证明重开仍为原物理root。静态发现错配到空root可能假ACK，下一
tracer必须持久固定原真实root身份并拒绝错root。没有为其claim现能力。
真实secondary、跨进程迟到效果、孤儿竞争、policy自动封闭及全页/失效恢复
仍待实际实施/证据，不用当前三vertical替七AC。

本轮已明确release LOCAL/no pending native；没有任何旧资源cleanup。

## 第四vertical：拒绝错误物理root的伪删除ACK（进行中）

`TestContentWrongPhysicalRootCannotAcknowledgeOriginalHolderErasure`沿原真实
PG接纳/发布/Get/自愿Seal，再把相同holderID映射到独立登记的空root。
首red暴露假ACK：`holder-binding-first-red.log` actual0.319s，line46
期望ErrHolderBinding却nil；PID/PGID2704020/start11259402/exit1，
groupAbsent=true、无timeout。已实际completion并明确release LOCAL。

当前静态修复从最初接纳记录原对象holder Open时真实FD Stat dev/inode；
Objects.Binding是纯访问器、不在PG锁内进行介质等待。原Record/PG独立列
及publication attempt固定该值；发布startup/final、实际Put、Get实际Read
和Seal消费原事实。primary holder identity携带同一binding，cleanup及
对象adapter在实际Fence/Observe前都核原绑定，错误root不能取得原ACK。
Binding相等不证明Close或物理删除。旧未binding记录保持未知，绝不自动
用当前配置空目录认领。真实受信来源回填/旧writer停止升级另待qualification。
| 原日志 | 实际结果 |
| --- | --- |
| `holder-binding-first-green.log` | exact normal0.382s；PID/PGID2709956/start11283913/exit0/groupAbsent=true。 |
| `holder-binding-race-controls.log` | component三exact race2.758s与local reopen/CloseDrain两个exact race1.116s均通过；PID/PGID2710457/start11285609/exit1/groupAbsent=true。末尾hash命令误写不存在的0002文件名导致整体exit1，此失败保留，不报整个命令green。 |
| `holder-binding-frozen-hash-repair.log` | 单独正确0001/0002_source_policies.sql哈希与原冻结值相等；PID/PGID2711539/start11289518/exit0/groupAbsent=true。 |

所有原wrapper均无timeout；无pending native，已明确release LOCAL。
此轮实际拒绝错root后，原root以同Seal/Deadline恢复并经独立精确字节观察
确认删除。前三vertical已在新binding source上作相关race控制，并覆盖本地
原holder reopen/CloseDrain。first-Seal已错root的独立正常/拒绝对照另待后续。
未实施受信legacy回填、真实停止旧writer升级、secondary、进程竞争或全七AC。

### 首次Seal错误root独立source qualification

`TestContentFirstSealCannotAdoptAnEmptyConfiguredRoot`是上述必要绑定修复的
独立正常/拒绝控制，未退产品代码或伪造新red。原真实publish/read后，
第一次Seal配置空独立root即ErrHolderBinding；原body仍正常read，正确
原root随后才首次创建Seal、实际清理，新建Lifecycle核同deadline与准确key
缺失。该.339原路径没有真实Store.Reopen，先前重开表述已纠正；后续增加
实际w.Reopen再独立运行qualification，原日志保留。
`holder-binding-first-seal-qualification.log` actual0.339s，PID/PGID2715309/
start11305713/exit0/groupAbsent=true/无timeout。已明确release LOCAL，
无pending native。此新增测试在第四产品pin上直接green，没有新产品修改。

真实Reopen补充运行 `holder-binding-first-seal-real-reopen.log` actual0.342s，
PID/PGID2722400/start11335939/exit0/groupAbsent=true/无timeout；原.339
无Reopen路径仍保持上述限定。新测试确实调用w.Reopen后再观察原seal/deadline。

## 第五vertical：真实第二holder复制、离线及原责任恢复

`TestContentIndependentSecondaryOfflineRetainsOriginalCleanupResponsibility`
正常PG接纳/publish/public Get后通过受信CopyToSecondary，copy首stub
unavailable使line42真实业务red。首red只运行到Copy，不证明后续场景。

| 原日志 | 实际结果 |
| --- | --- |
| `secondary-offline-first-red.log` | actual0.291s；PID/PGID2722861/start11337339/exit1/groupAbsent=true。 |
| `secondary-offline-first-green.log` | actual normal0.518s；PID/PGID2732956/start11380622/exit0/groupAbsent=true。 |
| `secondary-offline-race-controls.log` | sameexact+已有actualerase控制actual race3.883s；PID/PGID2733578/start11382920/exit0/groupAbsent=true；0001/2正确真实hash等原冻结值。 |

每个native都有原wrapper实际completion且无timeout，已明确release LOCAL/
no pending。copy登记在原版本锁内、Tx外实际primaryRead/secondaryPut之前，
原copyID/fullRef/subject/purpose/binding/attempt/finite effectdeadline固定。
目标及全部来源当前read/save/sync在两个短Tx核对，seal阻止新注册和迟到
复制资格确认；seal消费包括未confirmed复制的全部已登记责任。

test独立读两root准确key的alpha字节，确认不同inode，之后真实Close第二
对象holder并使用无活动secondary port配置。staging/primary独立erased后
只允许明确metadata gone，全holder仍pending；跨两页观察保持secondary原
responsible/清理deadline。该次运行未assert Binding/CopyID/EffectDeadline/
AttemptKeys；源代码持久保存它们，不能视为执行核验，后续相关qualification
补实际port binding/原copyID/effectdeadline断言另跑。离线副本
独立字节仍存在；secondary与Content World真实Reopen后推进同原seal预算，
准确副本消失且全部ACK。不把nil offlineport当native删除失败实验；真实
删除失败、ACKloss、copy/Seal并发当前门拒绝另待独立tracer。

五个partial vertical不等于七AC接受；还缺跨进程迟到安装两序、精确孤儿
publish竞争、policy cleanup消费、legacy停止writer真实回填、全部attempt/
传播页及metadata拒绝/祖先路径。没有清理其他owner资源或push。

### 第二holder原字段与真实删除失败独立qualification

补充offline断言不是镜像attempt常量：原actual Objects.Binding、CopyRequest.ID
及CopyRequest.Deadline分别与跨holder页观察的Binding/CopyID/EffectDeadline
核对。`secondary-offline-original-copy-fields-qualification.log` actual0.631s，
PID/PGID2741795/start11417526/exit0/groupAbsent=true；原.518/3.883未assert
这些字段的限定保持。AttemptKeys当前只有持久source与实际效果覆盖，未claim
独立字段断言。

`TestContentSecondaryNativeRemovalFailureRetainsOriginalResponsibility`先真实
正常复制及独立两root不同inode字节核对。受限owned fixture把原准确key
的copy inode保在key/body并以非空目录占据准确key，Linux原生Remove真实
返回ENOTEMPTY；不是注入适配器error。primary/staging可gone，secondary
保留residual/responsible/binding/原copy/effect及清理deadline，全global不ACK；
独立key/body仍为原alpha。fixture只恢复本own已登记准确inode/key，随后两
World真实Reopen，以同原seal/deadline最终擦除并确认globalACK。

`secondary-native-removal-failure-qualification.log` actual0.486s，
PID/PGID2741403/start11416103/exit0/groupAbsent=true。已有协议直接green，
未造新red；该异常目录和恢复是受信故障实验，不宣传协议阻挡任意外部
OS写者。fixture新目录Sync/Close由原setup ownership跟踪；不碰任何旧root。
两个native均无timeout且actualcompletion，明确release LOCAL/no pending。
ACKloss与真实跨进程晚写/孤儿/旧writer受控升级仍待各自tracer。

## 跨进程第一顺序独立qualification：Put先持真实key锁

`TestCrossProcessPutFinishesBeforeOriginalErasure`使用actual测试子进程，
prestart责任在Start前fsync登记；实际Setsid PID/PGID/starttime ACK再次
fsync后才释放pipe effectgate。child真实temp.Sync返回之后、Close/Link
之前SIGSTOP。父通过独立temp alpha字节及20ms有限Erase deadline拒绝
观察实际跨进程flock；SIGCONT后child真正Put/Store.Close返回nil并写明确
CloseACK文件；父先actualWait/groupAbsent，再独立断言explicit CloseACK。
kernel ACK不推逻辑Close。

`crossprocess-put-first-qualification.log` actual0.050s、outerPID/PGID2752480/
start11464036/exit0/groupAbsent=true/无timeout；ownroot3768632004 dev33/
inode325796，childPID/PGID2752568/start11464086/exit0/groupAbsent=true。
原callerctx5s deadline原样传给child，不重启业务预算；child测试timeout6s，
outer go test timeout10s、原wrapper120s。所有pipes均真实Close，Wait有限。
父原Erase及独立Open确认fenced/erased/准确final key缺失，父holder Close确认
后才释放此自own root。失败/KILL时缺逻辑Close则根保持未知，不cleanup。

该case是已有协议直接green，机械停点只控制真实Sync后的位置，不注入FS
结果。反序墓碑先完成/迟到Put以及SIGKILL旧holder未知scope尚待独立case；
不能用本单顺序或原receipt门宣称全部跨进程AC。明确release LOCAL/no pending。

### 跨进程反序独立qualification：耐久墓碑先于迟到Put

`TestCrossProcessClosedKeyRejectsLatePutAndSurvivesHolderDeath`先正常Put/独立alpha/
正向Close。eraser child真实FenceAndErase返回之后、Store.Close之前SIGSTOP；
另一个late child真实Put必须ErrBodySealed，并实际Store.Close确认。再以原
进程身份SIGKILL eraser，实际Wait验证SIGKILL与groupAbsent；completion的
ledger/Sync/Close/absence错误单独completionErr必须为nil，不把expected
ExitError匹配吞掉额外错误。父独立Open Observe确认原fence和body/late temp
缺失并Close自己holder。这不补原eraser的未知logical Close。

`crossprocess-seal-first-kill-qualification.log` actual0.081s，两exact（反序+
前一正常顺序相关control）通过；outerPID/PGID2765340/start11519327/exit0/
groupAbsent=true/无timeout。原5s caller deadline传给所有child，child测试6s、
outerGo10s及wrapper120s保持。ledger明确分列prestart duty、actualStart ACK、
positive logicalClose、kernel completion和old logicalClose未知，各mode身份准确。

| 原root与child | 实际责任 |
| --- | --- |
| firstcontrol root3844484877 dev33/inode326489；putchild2765408/start11519375 | actual Store.Close ACK与kernel0/groupAbsent，父Close/观察后确认此ownscope。 |
| reverse root4116685529 dev33/inode326495；eraser2765413/start11519378 | actual Fence完成但Store.Close前SIGKILL；kernel exit-1/groupAbsent，logical Close UNKNOWN，cleanup_allowed=false。该准确root永久保留。 |
| reverse latechild2765420/start11519380 | original key迟到Put拒绝；自己的actual Store.Close ACK及kernel0/groupAbsent；不代表原eraser Close。 |

第一.050日志仍是旧单顺序资格，不伪称当时已有新增永久logicalClose ledger
事件。本.081才具有该新增事件。原CloseACK仍是子执行actual nil返回后的
事实，父先Wait再独立断言，kernel ACK不替逻辑Close。已有协议directgreen，
没有造新red；两次序本机Linux事实不证明不守协议旧writer被防住。
明确release LOCAL/no pending native。原初failedroot3977538271逻辑Close未知
亦继续保留，后续绿色及此process Truth不能洗白它。

反序case现名称最小修正为`...RejectsLatePutAndSurvivesHolderDeath`：实际latePut
拒绝发生在eraser SIGKILL之前，之后独立Truth证明death不破坏已成fence。
原.081命令及日志仍是旧`...AfterHolderDeath`名称，不改历史记录，也不claim
死亡之后另一次latePut已执行。rename本身未另跑native，最终相关suite覆盖。

## 精确孤儿条件封闭vertical与publish先赢资格

`TestContentOrphanSealWinsBeforeLatePublicationAndPreservesLiveVersion`先正常
同content_id Version2完整publish/Get/独立alpha，再接纳Version1的原Command。
真实登记attempt并完成nativePut后，有限managed gate停在PG finalization之前。
SealOrphan原stub对合法已登记未发布版本unavailable使line68业务red。cleanup
释放gate并等待实际finished；不是把ctxcancel当已join。首red只运行到Seal。

| 原日志 | 实际结果 |
| --- | --- |
| `orphan-first-red.log` | actual0.234s；PID/PGID2775878/start11564513/exit1/groupAbsent=true。 |
| `orphan-first-green.log` | actual0.411s；PID/PGID2780347/start11583178/exit0/groupAbsent=true。 |
| `orphan-race-controls.log` | actual2.431s，两exact race通过/frozenhash准确；PID/PGID2780777/start11584525/exit0/groupAbsent=true。但错误使用outer test45s，原授权为30s；此日志保留，不作本轮原30s资格。 |
| `orphan-race-original-timeout-repair.log` | 同两exact改回原test30s actual2.481s；PID/PGID2782075/start11589763/exit0/groupAbsent=true。 |
| `orphan-published-wins-qualification.log` | 新publish-wins独立case+原orphan已绿相关control actual0.531s；PID/PGID2787276/start11612319/exit0/groupAbsent=true，原test30s。 |

所有wrapper均120s无timeout，业务caller20s/publish5s/原cleanup join3s保持。
共享seal短Tx在同原LockVersion与current管理资格/准确ref/保存主体/用途内，
只允许preparing/failed的孤儿选择；published活引用ErrOrphanReferenced。
没有外Tx检查后再普通Seal的竞态。orphan先赢时沿原已登记attempt及精确key
实际擦除，迟到PG Finish不能恢复published，原Commandreceipt固定不变；
独立Version2准确对象与public body始终保持，真实World重开同seal/原期限。

`TestContentPublishedReferenceWinsOrphanSelectionWithoutCreatingCleanup`真实
Version1/2 published后选择原Version1 orphan被拒绝；Observe无seal，Step无
active cleanup，两准确key独立字节及public body正常。真实World.Reopen后
两版仍可读，原Version1 Command历史仍published。该case已有guard直接green，
不是新red。不把精确目标Seam当任意root扫描删除器。完整多attempt/未知旧
归属及停止legacy writer升级仍待独立tracer，不用目前单attempt声明全页。
均明确release LOCAL/no pending native；原两个CloseUNKNOWN根继续保留。

publish-wins只核原Command的published/fullRef历史；该case没有capture/replay
Encode原receipt，不宣称字节级receipt相等。orphan-wins另有实际Encode比较。

## 当前save恢复后消费旧policy责任：首业务red

`TestContentRestoredSaveBasisMakesOriginalCleanupNotRequiredWithoutSealing`真实
normal publish/Get之后，Manager.InstallPolicy撤save，Manager.Step执行原
传播并保存pending责任。正文此时仍可读。后续恢复当前save，AuthorizeUse
正常，再actual World.Reopen，进入trusted ConsumePolicyCleanup stub。
line62 ErrUnavailable是业务red；消费后的not_required/无seal/无工作/独立
正文及再次重开断言尚未执行，不当绿色或全部policy消费者资格。

`/tmp/lerna-04-ticket05-execution/policy-cleanup-restored-first-red.log`
actual0.246s，PID/PGID2802811/start11679653/nativeexit1/groupAbsent=true，
无timeout。原caller20s/test30s/wrapper120s；source env/gofmt failclosed。
session40608真实结束，明确release LOCAL/no pending。绿色待Astra窄复核，
本次没有改变普通SaveResponsibility保pending语义或实现qualified writer。

随后全文采用root批准的
`/tmp/lerna-04-ticket-05-policy-cleanup-qualification-decision.md`，最小新增
`LifecycleRepository.QualifyPolicyCleanupNotRequired`。独立候选Observe Tx
结束后，新owner Tx先取得整页目标/祖先的全部当前saving basis及Version
锁，才依次责任FOR UPDATE/完整expected比较/实际SQL CAS。最后fresh DB
clock重核管理授权及本次全部policy/cap界限，失败rollback本页。成功之后
沿独立Manager.Observe重新读取当前历史；不写change/Job/cursor。

原Record.Subject包含完整delegation且Purpose准确匹配，当前save许可及全
祖先save当前资格是核对依据；历史change的ValidUntil/RetainUntil不代替
今天权限，也不复用跨Tx授权观察。单调cap/BodySeal/BodyGone仍禁止取消。
普通SaveResponsibility不改，qualified writer只更新已存在准确原pending
行的BodyCleanup=not_required/Residual=current_save_basis_restored；原actions/
reason/deadline/holders/attempt/publication/history原样。Record.CleanupPending
不粗清，not_required不是erased。未知因果/coverage/sealed/expiredcap及仍
无当前save的情况保留pending；本首分支尚不自动Seal。

| 绿色日志 | 实际结果 |
| --- | --- |
| `policy-cleanup-restored-first-green.log` | actual0.366s，PID/PGID2810470/start11712612/nativeexit0/groupAbsent=true。 |
| `policy-cleanup-restored-race.log` | actual1.745s，PID/PGID2810947/start11714058/nativeexit0/groupAbsent=true；0001/0002 frozen SHA准确。 |

两个原caller20s/test30s/wrapper120s，无timeout；session39471/35799已真实
完成，明确release LOCAL/no pending。实际not_required/原历史及deadline/
无seal/无bodycleanup工作/独立原alpha字节/public Get/第二World重开保持
均执行通过。当前窗口、祖先拒绝、cap过期、责任CAS竞争、授权跨等待、
genuine policy→seal以及ObjectHolder独立必要修正均待后续单tracer。

## 当前save仍撤销：真实policy consumer之后缺seal的首业务red

`TestContentCurrentSaveWithdrawalStartsOriginalSealAndRealCleanupConsumer`真实
publish/Get正常，然后Save=false，Service.Step实际推进policy_propagation，
Manager.ObserveChange得到准确原pending责任，正文此时仍可读。actual
World.Reopen之后Consume返回旧pending；Lifecycle.Observe在line136返回
ErrUnavailable/noSeal，这是当前未实现条件封闭的真实业务red。

`/tmp/lerna-04-ticket05-execution/policy-cleanup-current-first-red.log`包actual
0.406s/case0.36s；PID/PGID2817085/start11740188/nativeexit1/groupAbsent=true，
无timeout，session60824实际结束。原caller20s/test30s/wrapper120s及failclosed
shell/gofmt。后续正文门、真正两介质清理、metadata gone、receipt/history、
原seal/change准确物理ACK与重开尚未执行，不claim。明确release LOCAL，
无pending。绿色等待root/Astra窄决定，当前没有新增Seal/Change关联代码。

随后完整读取采用
`/tmp/lerna-04-ticket-05-policy-seal-ack-decision.md`。BodySeal.PolicyChangeKey
准确绑定first原政策触发（voluntary为空），与完整ref/保存主体用途/原责任
Deadline及稳定sealID共同形成同Tx关联；不可覆盖既有seal或借同objectID
清其他主体/用途/其他change。CurrentSavingPolicy只读完整原policy FOR SHARE，
确切fulltuple/真实Save=false才是已知因果；CheckPolicy=nil不推撤save。

同mutation Tx整页先policy/Version与结构来源资格，再共享sealLocked（现
voluntary/orphan/policy三路径同实现）登记原staging/primary、全部既有copy、
原Job；最后原责任全字段锁后比较，失配整体rollback。Policy期限用原
责任Deadline，不能借Change转natural phase后的ExpiryDeadline。所有写后
fresh管理资格/DBclock与原执行界限。当前首case未扩为expiredcap/未知归属。

Step两个Complete出口共用completeCleanup：已独立确认所有原holders之后，
先最新Claim/原sealDeadline/current资格，再原政策责任准确锁/tuple/实际CAS
置erased，最后锁等待后freshNow/Claim/Deadline再Complete，同Tx提交。
primary+PGstaging gone不替secondary ALLACK；空policyKey不找相似责任。
原holder union/actions/reason/attempt/publication/deadline始终保历史。

| 原日志 | 实际结果 |
| --- | --- |
| `policy-cleanup-current-first-green.log` | actual0.249s，PID/PGID2830612/start11799792/nativeexit1/groupAbsent=true；Consume scope失败，不是green。原受信policy JSON带纳秒，而PG timestamptz只能承载微秒，新列一致检查过严。 |
| `policy-cleanup-current-time-precision-repair.log` | 按实际微秒表示核ValidUntil列（不改policy/window/deadline）后，exact actual0.465s，PID/PGID2833084/start11810190/nativeexit0/groupAbsent=true。 |
| `policy-cleanup-current-race-controls.log` | 五个已存在exact（current/restored-safe/voluntarySeal/orphan-wins/actualstaging+primary）actual3.659s，PID/PGID2834055/start11814181/nativeexit0/groupAbsent=true；两个frozen SHA准确。 |

原caller20s/test30s/wrapper120s保持，无timeout。session64905/57014/89427均
actual结束，明确release LOCAL/no pending。正常链实际执行：seal提交时原
alpha仍独立存在，正文Get关闭；Service.Step不冒cleanup owner；真实重开
Lifecycle处理staging+primary并独立观察，全ACK后原责任erased/残留为空，
全部历史字段仍原样；metadata gone、Encode原receipt/published历史、再
World重开原seal/期限保持。此firstkey范围不证明其他trigger/ancestor/
expiredcap/锁等待/CAS竞争/责任全页/secondary政策ACK。ObjectHolder独立
修正以及后续自然phase普通写是否维持erased结论仍待独立tracer。

## 权威真实ACK之后当前primary holder事实（独立vertical）

`TestContentAuthoritativeErasureStopsNewPrimaryHolderClaimsAndKeepsOldHistory`
先真实published alpha及原政策责任ObjectHolder=true，沿已绿policy协议
完成实际staging/primary/ALLACK，再独立文件缺失/PGstaging无正文、当前
metadata gone、实际World.Reopen。新Rev3 policy责任仍声称ObjectHolder=true
使line88业务red；不借私表镜像断言，不用policy过期或文件not_found冒ACK。

最小修复仅真实AuthoritativeBodyErased确认后，条件
`!BodyGone || ObjectHolder`同Tx BodyGone=true/ObjectHolder=false。false反映
当前primary已独立擦除，不改旧责任holder union、secondary或published历史。
该条件包含已gone但旧flag=true的修复位置；本normalcase并未独立执行该
旧状态子分支，后续lateFinish/source资格另查。

| 日志 | 实际结果 |
| --- | --- |
| `holder-fact-authority-first-red.log` | actual0.335s/case0.32s，PID/PGID2844392/start11859473/nativeexit1/groupAbsent=true。 |
| `holder-fact-authority-first-green.log` | exact actual0.365s，PID/PGID2845040/start11861917/nativeexit0/groupAbsent=true。 |
| `holder-fact-authority-race-controls.log` | authority/currentpolicy/actualstagingprimary/secondaryoffline四已存在exact actual3.649s，PID/PGID2846168/start11866248/nativeexit0/groupAbsent=true。 |

均原caller20s/test30s/wrapper120s，无timeout；session78596/87064/85850实际
完成并明确release LOCAL/no pending。新责任ObjectHolder=false，旧原责任
ObjectHolder=true且BodyCleanup=erased，Command published/fullref历史不改，
save恢复后metadata仍gone均实际执行。lateFinish guard未改/未launch，不
把此第一bool修复当迟到callback已验证，原CloseUNKNOWN根继续保留。

## 成功nativePut的迟到Finish不得复活当前holder（独立vertical）

`TestContentLateSuccessfulPutCannotRestoreErasedPrimaryHolderFact`真实Put返回
成功、独立alpha存在后，managed gate停在PG Finish前。SealOrphan/实际
staging+primary/ALLACK以及独立absence均完成，再释放gate并收到实际done
与finished（原join3s），actual World.Reopen。新政策公开责任ObjectHolder
仍true造成line196业务red；不是native错误注入/取消caller假join。

仅失败Finish赋值改为`ioErr == nil && !record.BodyGone`，保留现seal/current
claim/ref门。物理隔离仍靠flock/tombstone，bool不替代物理确认。历史已
failed符合孤儿选择胜出，原Encode receipt保持，原seal/期限/实际ALLACK不变。

| 日志 | 实际结果 |
| --- | --- |
| `holder-fact-late-finish-first-red.log` | actual0.283s/case0.27s，PID/PGID2852499/start11892827/nativeexit1/groupAbsent=true。 |
| `holder-fact-late-finish-first-green.log` | exact actual0.282s，PID/PGID2853328/start11895930/nativeexit0/groupAbsent=true。 |
| `holder-fact-late-finish-race-controls.log` | lateFinish/authority/orphan-wins/published-wins四已存在exact actual2.990s，PID/PGID2853907/start11897902/nativeexit0/groupAbsent=true。 |

原caller20/publish5/join3/test30/wrapper120保持，均无timeout，session39931/
82375/14233真实完成，明确release LOCAL/no pending。新责任false、原receipt
字节固定/failed progress/fullref、旧seal及deadline、独立原文件不重建均
实际执行。此资格与前published authority history独立，不覆盖自然phase。

## 同原责任自然phase不得撤销已确认erased（独立vertical）

`TestContentNaturalPolicyPhaseCannotUndoOriginalErasureAcknowledgement`真实
normal publish；Rev2 savefalse的原ValidUntil为当前+2s，原RetainUntil仍wide。
Service.Step实际转natural_expiry并保存原due；policy seal/真实ALLACK后
原责任erased与历史ObjectHolder=true。actualWorld.Reopen，有限等待原due
加20ms，真实Manager.Step自然注册该same key，line74回pending/residual
holder_unconfirmed并将旧holdertrue抹false，造成真实业务red。

随后完整读root正式采用归档
`/workspace/lerna/.scratch/lerna-04-content-snapshots/ticket-05-terminal-erasure-history/decision.md`
7389B/SHA2563185552b6a06156793a6faaf20c01b46f2decb26fe4e02de3c44544e5553adb7。
普通SaveResponsibility先核txOwner/原完整key/ref/完整subject/purpose；主动
输入erased拒绝。原pending分支未放宽/不冻新publication；独立previous
erased分支精确保留合法空Residual、Reason/AttemptKey/Publication（含空），
holder历史OR、全部状态原Actions确定union和Deadline既有min继续。责任锁
后不新增反向Version/Seal/Policy锁，唯一起源仍Lifecycle全ACK专用port。

| 日志 | 实际结果 |
| --- | --- |
| `policy-ack-natural-phase-first-red.log` | actual2.309s/case2.30s，PID/PGID2862036/start11934038/nativeexit1/groupAbsent=true。 |
| `policy-ack-natural-phase-first-green.log` | exact actual2.293s，PID/PGID2870627/start11972161/nativeexit0/groupAbsent=true。 |
| `policy-ack-natural-phase-race-controls.log` | natural/currentpolicy/restored/authority/lateFinish五已有exact actual5.570s，PID/PGID2871622/start11976142/nativeexit0/groupAbsent=true，两frozen迁移SHA准确。 |

原caller20/test30/wrapper120及原2s due+20ms保持，无SQLdue/DBclock伪造，
没有延原责任期限/Claim。session51771/92106/99140 actual完成，明确release
LOCAL/no pending。same原责任自然phase后erased/空residual/原完整tuple/
holder历史true/原attempt/publication/reason/deadline、五动作union与原
watermark/due/ExpiryDeadline不刷新，以及再次World.Reopen均实际通过。
不同change不继承/完整tuple拒绝/ordinary不得造ACK等边界目前为source
guard，尚不作为分别执行证据；ACKloss/metadata/legacy等剩余AC继续。

## 实际擦除返回ACK丢失与同原延后责任恢复（独立vertical）

`TestContentLostActualErasureReplyRetainsOriginalDutyAndRecoversAfterConfirmedDefer`
先normal publish/read，再真实save撤回生成原policy seal。机械wrapper仅在
真实原Objects.FenceAndErase成功后丢一条返回ACK，不伪造native failure，
也不是PostgreSQL commit_unknown。原准确文件已独立缺失且受锁独立Observe
确认fenced/erased，但原primary责任residual/holder_unconfirmed、global未ACK、
原policy责任pending、metadata仍forbidden。Step已真实提交原DeferClaim
100ms并释放原lease；actualWorld.Reopen后仅等120ms，以同原seal/ref/holder/
deadline重试独立确认，才完成ALLACK和原policy责任erased、授权metadata gone。

首run真实碰到另一个合法端口缺陷：受信MetadataPolicy允许纳秒ValidUntil，
但CheckMetadataPolicy拿原JSON纳秒与PG timestamptz微秒列直接等值比较，
使Get unavailable。这是metadata精度失败，不是erasure业务red，也不是gone
泄漏。中间fixture truncate只是暂时输入回避，不能当原纳秒输入source资格。
最后复原原wide=time.Now().Add(time.Hour)，最小产品修复仅将列编码一致性
比较截到微秒，仍使用原JSON纳秒deadline严格now.Before；LockMetadataPolicy
只读原body/CAS且无列比较，未改。未拓宽原授权窗或擦除deadline。

| 日志（均位于 `/tmp/lerna-04-ticket05-execution/`） | 实际结果 |
| --- | --- |
| `erasure-reply-loss-first-run.log` | actual0.286s/case0.27s，PID/PGID2884700/start12033681/nativeexit1/groupAbsent=true；实际erase/replyloss/pending已通过，metadata精度失败。 |
| `erasure-reply-loss-precision-repair.log` | 暂时truncate fixture输入actual0.472s，PID/PGID2886112/start12039709/nativeexit0/groupAbsent=true；非原纳秒source资格。 |
| `erasure-reply-loss-race-controls.log` | 暂时truncate输入actual3.361s，PID/PGID2886527/start12041086/nativeexit0/groupAbsent=true；replyloss/currentpolicy/lateFinish及regex实际选中的已有secondary native ENOTEMPTY，两个frozen SHA正确；未选中IndependentSecondaryOffline。 |
| `erasure-reply-loss-offline-selector-repair.log` | 独立exact已有secondary offline race actual1.853s，PID/PGID2887421/start12044636/nativeexit0/groupAbsent=true。 |
| `erasure-reply-loss-original-ns-repair.log` | 产品微秒列编码修复、原纳秒输入exact actual0.437s，PID/PGID2891521/start12062530/nativeexit0/groupAbsent=true。 |
| `erasure-reply-loss-original-ns-race.log` | 同原纳秒exact race actual1.793s，PID/PGID2892078/start12064588/nativeexit0/groupAbsent=true；两个frozen迁移SHA准确。 |

原caller20/test30/wrapper120、原policyJob/seal/责任截止保持，无timeout，
上述session均actual结束并两次明确release LOCAL/no pending。当前公开
观察核同原完整ErasureIdentity/seal/deadline、原policy pending→erased；
原Claim释放与due机制由实际Step提交后恢复及source核对共同资格，不声称
测试读取了私有Claim epoch/Lease字段。此case不覆盖PG commit_unknown、
原执行scope过期后重建预算、未确认Defer或其他holder晚callback。

## 派生gone的完整当前metadata范围（独立vertical）

`TestContentDerivedGoneRequiresCurrentExactMetadataForReaderAndEveryAncestor`
真实root→middle→derived全部发布/read；仅derived voluntary seal并真实
staging+primary全ACK，独立缺失、actualWorld.Reopen后两个ancestor原正文
仍normal。distinct完整delegated reader仅target许可时forbidden，追加直接
middle许可仍forbidden，追加root许可才返回可编码/解码的最小gone且
EvidenceAvailable=false。无正文read许可也不会借metadata访问正文。

同reader不同delegation/purpose拒绝；已有实际准确ref许可后wrongHash
query精确integrity，保持既有F1p资格先于实际声明核准的顺序。另distinct
reader真实登记错误hash的MetadataPolicy再查真实derived ref，精确forbidden；
无许可missing ref也forbidden，不披露not_found。实际root许可Rev2期限为
当前+700ms，有限等待原期限+20ms后完整derived视图forbidden，target与
middle许可仍wide且未改。查询后原seal/deadline/ALLACK保持，真实Lifecycle
Step没有新work，两个ancestor仍正常。未写SQL时间或Job due，未新增产品
变化、没有为已实现协议捏造新red。

| 日志（位于 `/tmp/lerna-04-ticket05-execution/`） | 实际结果 |
| --- | --- |
| `metadata-derived-first-run.log` | exact actual1.229s，PID/PGID2899973/start12098619/nativeexit0/groupAbsent=true。 |
| `metadata-derived-race-controls.log` | 该case/currentpolicy/实际staging+primarygone三个已有exact actual4.035s，PID/PGID2900359/start12100014/nativeexit0/groupAbsent=true，两个frozen迁移SHA正确。 |

原caller20/test30/wrapper120及700ms许可到期+20ms保持，无timeout；session
56194/19363 actual完成并明确release LOCAL/no pending。上述证明现行full
subject/delegation/purpose/ref和传递两级祖先许可，不宣称真实PG锁等待后
expiry/CAS竞争、已sealed祖先的独立metadata子场景或retentioncap consumer
路径已执行；后续必要义务仍沿真实单tracer。

## 原accepted cap真实到期的独立清理因果

`TestContentExpiredOriginalAcceptedCapStartsCleanupDespiteCurrentWideRenewal`
真实原policy Rev1 wide、V1请求接纳current+2s RetainUntil，原scheduled
Due/ExpiryDue固定该cap；原alpha normal。当前Rev2 wide/save=true不能抬
旧cap，独立同ContentID V2 beta normal。actualWorld.Reopen等原cap+20ms，
真实Manager.Step登记同原key pending/accepted_retention_expired/历史holder
true、仍live原Deadline及原ExpiryDeadline保持，V1 Get expired而独立alpha
仍存在。Consume后无seal/Observe unavailable造成line107业务red，红后
删除/ACK/replay没有执行，不用随后green洗这条日志。

完整读root采用的归档
`/workspace/lerna/.scratch/lerna-04-content-snapshots/ticket-05-expired-accepted-cap/decision.md`
8172B/SHA256134530eb6f9a43f8ca339b3341a05645813d23f155c80fe599f9d816bff34fd8。
最小分支仅原pending/holder_unconfirmed/accepted_retention_expired候选；
Reason不当删除许可，同Tx原fullRef/fullSubject/Purpose/knownphase/结构
闭包资格照旧。严格canonical解析非zero/year有效原持久cap，锁后fresh
DB Now>=cap且原责任Deadline仍live、原WorkBudget/TrustedUntil内；缺宽
currentpolicy不解释成Save=false，也不能使确定cap到期重新可续存。仍
传递实际policy/结构查询错误，全页先Version/policy/结构后holder/Job/原
责任CAS；最后cap<=freshNow<原Deadline再核，任失败全页rollback。旧
restored-safe/currentfalse原因门不扩大，AdmissionTarget/multipolicy不拓展。
原sealID/key/Deadline及Lifecycle ALLACK专用原责任ACK未新增框架。

| 日志（位于 `/tmp/lerna-04-ticket05-execution/`） | 实际结果 |
| --- | --- |
| `accepted-cap-first-red.log` | actual2.216s/case2.21s，PID/PGID2916513/start12171780/nativeexit1/groupAbsent=true。 |
| `accepted-cap-malformed-unit.log` | exact纯unit actual0.010s，PID/PGID2931542/start12234225/nativeexit0/groupAbsent=true；非法/空/zero/year0/非canonical不得成为到期证据，合法原时间不变。不是PG故障注入或业务row mutation。 |
| `accepted-cap-first-green.log` | exact actual2.469s，PID/PGID2932078/start12236270/nativeexit0/groupAbsent=true。 |
| `accepted-cap-race-controls.log` | cap/currentpolicy/restored-safe/natural-erasure-history四授权exact actual6.864s，PID/PGID2932551/start12237850/nativeexit0/groupAbsent=true，两个frozen迁移SHA准确。 |

原caller20/test30/wrapper120、原2s cap/+20ms以及live原责任窗口保持，无
SQLclock/due/Deadline伪造或time扩展。session40474/1898/31542/72537均
actual结束并明确release LOCAL/no pending。green真实执行首red未到部分：
同原key/Deadline seal仍pending、实际staging/primary清理、再次World.Reopen
原责任erased且保accepted_retention_expired原因和原Deadline、独立原alpha
缺失/全部holder ACK、V2beta正常，以及严格Encode原回执逐字相同。当前
target自身cap因果与pure malformed解析已执行；祖先/AdmissionTarget、
缺政策组合、原执行窗已过、真实锁等待/CAS与整页仍待各自必要资格。

## 原政策与holder的完整有限页（独立qualification）

`TestContentOriginalPolicyAndHolderPagesResumeWithoutErasingOtherSavingBasis`
原PageSize2，真实root及三后代A全部published/read；完整delegated保存主体B
在同root有独立current Save/read/sync=true且其独立派生body normal；同
ContentID独立V2beta normal。A一后代已真实复制到不同inode/root，copyID/
binding/effectDeadline原值固定并独立alpha回读，然后实际secondary.Close，
活动port为nil。原A root Save撤回后按公开原changeKey观察有限Manager.Step
实际partial Cursor，首2条加后页实际责任>2，原watermark/deadline保持，
actualWorld.Reopen完成剩余页。全部5原责任（root+3后代A+后代B）逐页
核准，无重复/漏页/续Deadline；B原责任not_required。

消费所有原责任页，四个A原ref各同原changeKey/deadline seal。A仅boolean
撤回而未BodySeal之前，B完整basis仍normal；source BodySeal后所有新
来源正文动作门正确使B派生Get forbidden，但B独立已保存alpha字节不被
A清理，B责任not_required保持。此区别不能误写成postSeal B公开仍可读；
V2无该source且始终normal。

真实Lifecycle处理到nilport离线失败，copied后代的首holder页2条必须按
typedKind/fullIdentity为pg-staging+primary erased；末页为同原copy/ref/key/
seal/binding/CopyID/effectDeadline/原cleanupDeadline的secondary residual/
负责方secondary，global不complete。独立primary缺失但secondaryalpha仍
存在。两个World实际重开、原确认Defer100ms后120ms观察恢复，剩余全部
holder清理/allACK以及全部原A政策责任erased、原Deadline保持；独立每个
原准确primarykey及原copykey缺失，V2beta和B独立alpha保留均执行。

| 日志（位于 `/tmp/lerna-04-ticket05-execution/`） | 实际结果 |
| --- | --- |
| `cleanup-pages-first-run.log` | exact actual0.962s，PID/PGID2942494/start12280800/nativeexit0/groupAbsent=true；已有协议直接真实qualification，没有新业务red或产品变化。 |
| `cleanup-pages-race-controls.log` | 该case及原secondaryoffline两个授权exact actual3.607s，PID/PGID2944656/start12290008/nativeexit0/groupAbsent=true；两个frozen迁移SHA准确。 |

原caller20/test30/wrapper120、PageSize2及各原时间/水位/CopyID/binding保持，
无timeout。session8008/82350 actual完成并明确release LOCAL/no pending。
此为真实policy责任页/holder页，不把它当all publication attempts页、
跨锁时钟/CAS竞争或legacy升级已执行证据；后续必要独立义务继续。

### 原全部 publication attempts 有限页及独立 unknown 效果（partial）

`TestContentEveryOriginalAttemptPagePreservesUnknownSameKeyResidual` 从原同一
Command 实际执行三次 native Objects.Put，逐次捕获真实 key/attempt/hash/
length/body 参数；前两次实际成功且独立 final alpha 存在后机械丢返回 ACK，
原已确认 Defer100ms 后观察120ms继续，不称 native Put 故障或 PG commit_unknown。
三次原 attempt 参数各异，最后正常published/read，独立 Version2 beta正常。

PageSize2 Seal 后 primary 首页公开 AttemptCursor 精确等于第二个原实际
attempt，仍 pending/attempt_page_pending，不因正文已删而全ACK；实际
World.Reopen 后最后登记页继续。另由新setup owner预先登记、真实打开并
独立 Stat inode、Write/Sync/Close/fsync 的同key `.999.tmp` 未登记为产品
publicationattempt；产品保留其原inode/独立bytes并显示 residual/
holder_unconfirmed/原responsible/deadline。独立 Objects.ObserveErasure
见 durable fence、准确 unknown residual、非erased；全部三个原登记attempt
及final原key确实缺失，V2仍正常。仅该setup owner明确核原inode后移除自己
效果、Sync，再原Defer恢复/真实重开，原Seal/fullRef/deadline allACK，V2和
原Command byte-level receipt不变。未知名称未被猜测采用或产品擦除。

| `/tmp/lerna-04-ticket05-execution/` 日志 | 实际结果 |
| --- | --- |
| `cleanup-attempt-pages-first-run.log` | exactnormal0.857s，PID/PGID3003670/start12548088/nativeexit0/groupAbsent=true；直接现协议qualification，无虚构新red。 |
| `cleanup-attempt-pages-race.log` | 同exactrace2.305s，PID/PGID3004128/start12549542/nativeexit0/groupAbsent=true；原0001/0002两SHA准确。 |

normal原ownscope `lerna_test_df028acffd3f6d6cedbdef79` / objects1605225831
(dev33/inode349105)，unknown inode349113；race原ownscope
`lerna_test_9d0e3f12d1c8d57994134d11` / objects570150497(dev33/inode349433)，
unknown inode349441。责任、inode和owner移除均实际fsynced ledger记录。
原caller20/Go30/wrapper120/原5sPublicationIO固定，无timeout，sessions92425/
73523实际完成并明确RELEASE/no pending。测试运行时另有尚未执行 legacy
binder 的WIP接线；本partial只固定本两资格源码及此准确边界，不把legacy
CAS、旧producer重建入口或全部七AC当已green。原legacy firstred另行归档。


### 原真实停止 legacy writer 的受信确切绑定（partial）

采用根归档 `ticket-05-legacy-holder-binding/decision.md`（10358B，
SHA785ac19e6e0737cafd554ddc281161e4c66a7d66d4634ad92cc17143b1037cc4）。
`TestContentStoppedOriginalLegacyScopeBindsOnlyAfterPositiveWriterClose`
先在新预登记 namespace/root 实际运行冻结1a7 writer。两个原版本真实
published/read，捕获每次原 Put 的准确参数。原实际 PID/PGID/starttime ACK
先于效果 gate；writer 尚在时无停止资格的绑定拒绝。原 Objects/Store/
witness/gates 显式 Close ACK、原实际 FD 关闭、actual Wait/groupAbsent
分别登记，然后才升级该准确 namespace 并打开当前对象介质。

当前 unbound Get unavailable、原字节仍在、原 receipt 字节级 replay 与
published history保持、unbound Seal拒绝均先执行。最初 binder stub 的
真正 business red 位于以上正常/拒绝链之后。最小 dedicated whole-Record
CAS仅从空 binding 变为该原介质 binding/资格ID/证据digest；两个短Tx夹
原 media actual Read/hash/length 校验，原完整 attempts 同Tx登记，原 caps/
receipt/费用/发布历史不变。普通 SaveVersion binding/provenance immutability
保持，无新迁移。当前受信构造深复制固定资格；公开请求只有ref/purpose。
同资格幂等、caller修改原DTO不改变实例、错空root/真实copyroot/namespace
拒绝、实际 World.Reopen 后 provenance保持均已执行。之后真实新协议 Seal/
清理全部原 effects/最小gone，独立 Version2 beta 与两个 receipt/history
保持。未停止、未知旧scope或未知 logicalClose 不被自动采用。

冻结入口随仓库重建，不依赖外部 absolute producer env。75原文件共466704B
及固定manifest由有限 fixture校验/物化；独立driver只import冻结包，compiler
15s clipped caller20、producer5s/supervisor6s、Go30/outer120保持。compiler
使用实际 Process.Kill 取消并另核Wait/groupAbsent；不以复用数值PGID盲杀。
根独立逐文件audit `/tmp/lerna-05-frozen1a7-source-audit.json` 为source provenance
ONLY（75 files/0errors），不替执行证据。原one-off personal-path build脚本
移至本目录 historical artifact，fixture不执行它。

| `/tmp/lerna-04-ticket05-execution/` 日志 | 实际结果与范围 |
| --- | --- |
| `legacy-frozen-first-build.log` | 原one-off build成功，PID2991399/start12495385/exit0/absent。 |
| `legacy-stopped-first-red.log` | 编译输入FAIL（named Purpose/string），2992181/start12498188/exit1/absent，未启动旧producer。 |
| `legacy-stopped-first-red-compile-repair.log` | 真business red0.401s，2993176/start12501940/exit1/absent；旧producer2993333/start12502208/exit0/absent并原显式Close ACK。 |
| `legacy-stopped-first-green.log` | portable入口调整后的unused import编译FAIL，3031363/start12664402/exit1/absent。 |
| `legacy-stopped-first-green-compile-repair.log` | exactnormal实际PASS2.694s，3032216/start12667752/exit0/absent；旧producer3032524/start12668228、compiler3032369/start12668037均exit0/absent与显式Close ACK。 |
| `legacy-stopped-race.log` | sameexact race实际PASS4.267s，3049877/start12746651/exit0/absent。 |
| `legacy-related-wrong-root-race.log` | 原wrongroot exactrace PASS1.727s，3050875/start12749662/exit0/absent。 |
| `legacy-related-first-seal-race.log` | 原firstSeal（真实Reopen）exactrace PASS1.846s，3051381/start12751576/exit0/absent。 |
| `legacy-related-attempt-pages-race.log` | 原allattempt exactrace PASS2.496s，3051890/start12753457/exit0/absent；0001/2/3三冻结SHA均准确。 |

调度核对保留：normal首轮明确root grant后root发相矛盾的03未release消息，
05仅完成已运行有限operation，随后hold/release，未继续native或重跑掩盖。
root确认其调度证据错误；后来完整tick/mtime核对明确首次compilerFAIL与03B
shared，而正常2.694的start12667752在B finalmtime后，正常执行可独立资格。
此结论来自root核对，不把文件mtime冒充tool/message completion timestamp。
新四race由重新明确唯一LOCAL grant逐一串行执行，03明确hold；所有actual
completion和显式RELEASE已报告。非verbose package PASS不宣称个别RUN输出。

sameexact race原scope `lerna_test_3c85bd9055a3dbaf32e927e2` / objects4077323202
(dev33/inode352357)，compiler3050180/start12747441和producer3050364/
start12747648实际exit0/absent；gateClose ACK、原FD8/9关闭、原Objects/Store
Close ACK分别成立。资格digest0c6745d38013112833f6119007adea9e1d475efa8aa03b20249ea490ebc01be0。
完整原 frames/effects/资格在 `owned-scopes.log` 405–421行；不回填任何历史
owner02或unknown root。本partial尚未执行legacy CAS竞争/原cutoff跨越/
PG commit-reply-loss恢复；决定§5这些最小义务继续，七AC均未accept。


### 原执行截止跨真实当前 policy 锁等待（partial）

`TestContentOriginalCleanupDeadlineCannotBeExtendedByCurrentPolicyLockWait`
两个fresh own scopes从首次Manager/Lifecycle固定WorkBudget700ms与2s原
acceptedcap、当前wideRev2，公开观察原cap+700ms ExpiryDeadline。两边都
实际published/read alpha与独立V2，并在真实cap+20ms由Manager.Step生成
原pending/holder_unconfirmed/accepted_retention_expired责任；原Get expired、
原物理alpha还在。两边原HoldPolicy FORUPDATE均独立见真实consumer blocked。

同装配正常对照实际立即rollback/连接Close放行，原期限内seal、真实staging+
primary allACK/policyerased与独立原body缺失、实际Reopen后的allACK/V2/固定
receipt保持。拒绝对照保持原locker到原责任Deadline+20ms才同原positive
rollback/Close；原consumer实际finish/join后exact原责任tuple保持pending，
无seal/无active body_cleanup，原alpha独立bytes保留，实际Reopen继续拒绝，
V2/receipt正常。没有扩大700ms或改stored due/clock/lease；这是已有协议
直接qualification，没有虚构新red或产品改动。

| `/tmp/lerna-04-ticket05-execution/` 日志 | 实际结果 |
| --- | --- |
| `cleanup-deadline-wait-first-run.log` | -v exactnormal packagePASS5.276s，两个实际RUN/subPASS2.36/2.90；PID/PGID3073776/start12821909/nativeexit0/groupAbsent。 |
| `cleanup-deadline-wait-race.log` | -v sameexact race测试PASS6.552s（sub2.56/2.95），末sha命令误写不存在的0002_dependencies.sql，组合native3074658/start12825369/exit1/groupAbsent；保留失败，不称组合exit0。 |
| `cleanup-deadline-wait-frozen-hash-repair.log` | 独立机械sha真实0001_content.sql/0002_source_policies.sql/0003_body_cleanup.sql三path，3075435/start12828474/exit0/groupAbsent，三冻结SHA准确；未重跑已green测试。 |

normal ownscopes `lerna_test_3a4a1eb42573e25c2fcb0803` / objects746700952
(dev33/inode355886) 与 `lerna_test_b73a59e99d1859e69cdc568d` / objects607349708
(dev33/inode355895)；实际PG locker PID501167/501187分别登记。race ownscopes
`lerna_test_cd9d6dd91ceb34408c43c064` / objects3985671651(dev33/inode356221)
及 `lerna_test_1d5793bc2e9b51d42223b7da` / objects2190470251(dev33/inode356229)，
PG locker501334/501347分别登记。原caller20/Go30/wrapper120/join3及原cap/
预算/期限保持，三个native无timeout/groupAbsent、consumer实际finished，
所有操作actualcompleted与显式RELEASE/no pending已报告。此范围不替whole-page
责任CAS、legacy决定§5回填CAS/cutoff/commitreplyloss或其它cap因果前沿。


### Legacy 原 whole-Record 竞争拒绝与同资格恢复（partial）

`TestContentLegacyWholeRecordRaceRefusesStaleBindingAndReplaysOriginalQualification`
先新ownscope真实冻结writer两原版本正常/正向CloseWait后升级。新消费者Tx1
已释放，真实原media Read（含FD Close）成功后有限3s返回gate。此时当前
Manager合法InstallPolicy同原Version1 Rev2/RetainUntil30min，实际普通
SaveVersion收紧原cap/递增Revision并提交；不是私表tuple改写或假的CAS故障。
放行/actualbinderfinish后精确runtime.ErrClaim，当前unavailable/Seal
ErrHolderBinding和两原实际bytes/固定receipt/出版历史保持。实际World.Reopen
后同原固定qID/digest/binding/期限重新资格，正常绑定两个原版本并回读；再
真实Reopen幂等provenance/原receipt/pub/V2正常。没有更新q或重Put尝试成功。

| `/tmp/lerna-04-ticket05-execution/` 日志 | 实际结果 |
| --- | --- |
| `legacy-whole-record-race-first-run.log` | -v exactnormal actualPASS2.273s，3103592/start12950303/exit0/groupAbsent/noTimeout。 |
| `legacy-whole-record-race-race.log` | -v sameexactrace actualPASS3.852s（case2.80），3104848/start12954675/exit0/groupAbsent/noTimeout；真实0001/2/3三原SHA保持，组合exit0。 |

normal scope `lerna_test_34bbeac53b62a39bd159fac9` / objects3784529484
(dev33/inode357702)，compiler3103736/start12950656，原producer3103910/
start12950833；race scope `lerna_test_b99864442fb008a0baaecd16` /
objects620974924(dev33/inode358192)，compiler3105055/start12955200，原producer
3105230/start12955376。各compiler actualWait/groupAbsent/controlClose ACK，
原producerexit0/Wait/groupAbsent与全部显式Objects/Store/witness/gate Close
ACK/原FD关闭分列登记；binder release/actualfinished/join确认。原20/15clip/
5+6/Go30/wrapper120/读gate3/join3保持，session52540/22750实际完成并显式
RELEASE/no pending。旧unknowns不触，当前产品fixed bb6e1f2/35df18e未修改。

此是已有wholeexpected保护的直接真实normal/refusal/recovery qualification，
没有新产品red/green变更。运行时另外replyloss/cutoff三个静态WIP包编译但
未执行；本partial只固定该case源码/evidence，不称另两例已qualification、
PGcommit_unknown、原时钟cutoff回滚、七AC已accept。两后续最小义务继续。


### Legacy 真实已提交回填的返回 reply loss 恢复（partial）

`TestContentLegacyCommittedBindingReplyLossRecoversOnlyByOriginalQualification`
在新own原scope真实冻结writer正常/显式CloseWait后，typed机械decorator
完全委托原Store：准确Version1 empty→binding CAS实际成功，原Within实际
返回nil确认commit后才一次丢RETURN reply。调用者独立见loss error/真实
commit marker；这不是PG原生commit错误或commit_unknown。没有换root/qID/
digest/期限、换Command或rePut。实际World.Reopen使用原fixedq观察binding/
provenance/ref/pub，两原版本正常read，固定receipt/pub保持。之后新协议
原Seal/deadline、真实全部holder/all原登记attempt清理、独立原body和全部
捕获attempt缺失，真实再Reopen allACK、V2正常及原receipt/pub保持执行。

| `/tmp/lerna-04-ticket05-execution/` 日志 | 实际结果 |
| --- | --- |
| `legacy-committed-reply-loss-first-run.log` | -v exactnormal PASS2.316s，3107805/start12966595/exit0/groupAbsent/noTimeout。 |
| `legacy-committed-reply-loss-race.log` | -v sameexactrace PASS3.832s（case2.79），3109232/start12971889/exit0/groupAbsent/noTimeout；三原SQLsha完整，组合exit0。 |

normal scope `lerna_test_3156572eed72d417c8f25f1b` / objects989245160
(dev33/inode358745)，compiler3107950/start12966881、producer3108109/
start12967060；race scope `lerna_test_50df380efb931ea300e833f7` /
objects1534808723(dev33/inode359238)，compiler3109407/start12972349、producer
3109586/start12972529。每个compiler/producer实际exit0/Wait/groupAbsent、
compilercontrolClose ACK和原producer显式Objects/Store/witness/gate Close/
原FD关闭分列成立，原owned ledger全frames保存。原caller20/15clip/5+6/
Go30/wrapper120不变，sessions79601/90319实际完成，显式RELEASE/no pending。
这是已有协议直接failure/recovery qualification，无新业务red/product变更；
cutoff两个静态WIP编译但未执行，初次700ms截止整Tx回滚仍未claim。未知旧
scopes保留，七AC未accept。


### Legacy 最初准确短资格跨界：真实CAS/attempt写的整Tx回滚（partial）

`TestContentLegacyOriginalQualificationCutoffRollsBackBindingAndAttemptWrites`
在新原scope冻结writer真实正常并positiveCloseWait之后、构造/开始任何
binder之前，本case host首次一次调用FixInitialLegacyCutoff把原q仅缩短到
700ms并fsync准确原namespace/ID/binding/digest/ref/attempts/cutoff事件。
该函数校原ownedroot与strictlyshorterfuture；本case一次调用，不宣称通用
onceguard能阻止所有二次授权，原失败后绝不续期。既有消费者WorkBudget1min
未扩大，实际授权被原q700ms严格clipped。

同一initialq700ms独立原Version2实际正常绑定/read，证明该短窗口正常路径
可行。随后Version1真实dedicatedCAS成功、同Tx首条原SavePublicationAttempt
真实写成功后，机械等待原q.ValidUntil+20ms；真实最终DBclock回报expired并
实际整Tx rollback。公开Get unavailable/Seal ErrHolderBinding、同原已过期
q重放expired、真实World.Reopen与两原独立bytes/receipt/pub、Version2正常
执行。实际delegated write count=1仅为实际写入机制信号，**不是持久私表
计数执行断言**。绑定回滚有独立公开refusal证据；attempt持久回滚关联原
事务原子性为SOURCE资格，不冒公开attempt-table查询或擦除ACK。

| `/tmp/lerna-04-ticket05-execution/` 日志 | 实际结果 |
| --- | --- |
| `legacy-initial-cutoff-first-run.log` | -v exactnormal PASS2.702s，3112680/start12985938/exit0/groupAbsent/noTimeout。 |
| `legacy-initial-cutoff-race.log` | -v sameexactrace PASS4.133s（case3.09），3113881/start12990192/exit0/groupAbsent/noTimeout；三原SQLsha正确/组合exit0。 |

normal scope `lerna_test_c110b63674a3362ed15e8fec` / objects2748550027
(dev33/inode359821)，compiler3112846/start12986221、producer3113001/
start12986386；initialq截止2026-10-05T00:53:46.421782Z。race scope
`lerna_test_584ea31ec3f8817c127ad318` / objects443607912(dev33/inode360310)，
compiler3114105/start12990687、producer3114282/start12990861；initialq截止
2026-10-05T00:54:31.262132Z。各实际exit0/Wait/groupAbsent、compiler gateClose
及原producer Objects/Store/witness/gates显式Close ACK/原FD关闭均分列成立。
原caller20/15clip/5+6/Go30/wrapper120/q700ms/+20ms保持，sessions19338/70491
actualcomplete、显式RELEASE/no pending。这是原保护直接qualification无新
red/product修改；legacy§5最小CAS/已commitreplyloss/截止回滚分别已执行，
不因此声明全部七AC接受或任意unknown旧scope可升级。剩余policywholepage
CAS和准确祖先/AdmissionTarget清理因果前沿继续独立处理。


### 同原政策责任整页实际CAS竞争的早写回滚（partial）

`TestContentOriginalCleanupPageCASRollsBackEarlierQualificationsAfterConcurrentConsumer`
原root+两derived真实published/read与V2正常；原Save撤回传播有限页创建
三完整pending holder责任后当前Save恢复/完整当前closure合法。公开limit2
确定实际first/second Ref，独立limit1核同first且获取真实NextCursor；不猜
hashed VersionIdentity按ContentID排序或root先。旧candidate真实Observe Tx
实际commit/releases后有限3s返回gate。plain PageSize1 contender只处理该
公开cursor后的second原责任，实际NotRequired提交；first完整原tuple未改。

放行旧PageSize2 consumer，原PG first实际资格写成功后second全expected
真实ManagementConflict（errors.As准确类型）。实际整Tx回滚；公开first
exact原pendingtuple未改、second仍contender提交结果，证明早写回滚。成功
写调用Ref记录只机械信号，公开完整责任状态为独立业务oracle。无私表值
改写、错误ErrClaim期待、source断言冒执行或新产品修改。全部原实际alpha
bytes保留/无Seal/Lifecycle.Step false，fresh plain同原candidate有限页正常
全部NotRequired，真实World.Reopen保三原fulltuple/history/deadline（只合法
state/residual变化）与原body/byteReceipt/V2正常。没有刷新watermark/cap/
Deadline或切另一policykey。

| `/tmp/lerna-04-ticket05-execution/` 日志 | 实际结果 |
| --- | --- |
| `cleanup-whole-page-cas-first-run.log` | -v exactnormal actualPASS0.555s，3124586/start13036591/exit0/groupAbsent/noTimeout。 |
| `cleanup-whole-page-cas-race.log` | -v sameexactrace PASS2.272s（case1.22），3125777/start13041513/exit0/groupAbsent/noTimeout；三原SQLsha全准/组合exit0。 |

normal ownscope `lerna_test_f50217ad2bd635ac357fb951` / objects1578290075
(dev33/inode361190)；race `lerna_test_1dea3e2b73a7f1a8370c9564` /
objects860950023(dev33/inode361528)。原caller20/Go30/wrapper120/3s gate/
join3/WorkBudget1min保持，actualstale consumerfinished、candidate Tx释放、
contendercommit与rollback/freshreplay确认，sessions37167/17530实际完成、
显式RELEASE/no pending。现协议直接资格，无虚构businessred。此wholepage
竞争不替ancestor/AdmissionTarget清理原因、缺结构/未知权限，七AC未accept。


### 两祖先实际继承 cap 的准确 AdmissionTarget 清理：首业务red

`TestContentInheritedExpiredCapConsumesItsExactAdmissionTargetWithoutDeletingLiveAncestor`
真实A原acceptedcap2s后published/read、当前Rev2宽Save续期；独立B原alpha
仍live正常。target Sources[A,B]真实接纳后的effective receipt cap精确等于
A原cap，实际published/read/save完整closure正常，独立V2 beta正常。公开
全部finiteAdmission pages准确选择A/full保存subject/purpose/currentRev2/
AdmissionTargettarget、原capDue/cap+1minDeadline及watermark，不取首行冒充
该责任。原receipt编码已检查error，但samekey字节级replay在红后tail尚未执行。

actual World.Reopen、原cap+20ms，实际Manager.Step按exactoriginalkey Observe
至真实target pending/holder_unconfirmed/accepted_retention_expired、原完整
Ref/subject/purpose/objectholder/publication/liveDeadline；A/target公开body
expired而三原介质准确alpha仍在，B/V2公开正常均先执行。然后仅首
ConsumePolicyCleanup(originalAdmissionKey)在源码190行返回runtime.ErrScope
(transaction scope mismatch or expired)，已有目标化identity被排除的真实
business red，不是setup/compile失败。红后seal/allACK/原责任erased/再次
Reopen/no误删A或B/原receiptreplay与历史断言均未执行，不能当green。

`/tmp/lerna-04-ticket05-execution/inherited-cap-admission-first-red.log`:
-v exact实际FAIL2.223s（case2.21），PID/PGID3136486/start13089058/nativeexit1/
groupAbsent=true/noTimeout，session4680 actualcomplete。原ownscope
`lerna_test_e23759e204671bb26c33bd60` / objects812158325(dev33/inode362268)，
原caller20/Go30/wrapper120/initialcap2s/+20ms/原minuteDeadline固定，所有
native实际结束且明确RELEASE/no pending。产品未改；沿adopted target-only
cap决定，准确AdmissionTarget/祖先因果必须由此实际red交root/Astra窄决定，
不自行放宽changeKey/authority/nilPolicy或采用未正式§7多policy关联。

### 原 AdmissionTarget 的继承 cap：采用最小身份门并完成原 red 尾部

root正式采用 `/tmp/lerna-04-ticket-05-inherited-cap-admission-decision.md`
（SHA b42d94d141b470ba5fa879c809ee2f7f3d73d2a18b7e84c2ed685d233d03888e）。
最小产品变化仅 policy_cleanup.go：ordinary原key/旧支路保持；targeted完整
target/source ref、owner、原完整subject/purpose/revision、known natural阶段、
nil Previous、原Due/Deadline shape及正且<=24h expirybudget资格后复用原
admissionKey。每个原责任精确target，仅accepted_retention_expired/pending/
holder_unconfirmed进入现有cap支路，原due已到/原deadline不晚于原expiry
deadline；末freshDBclock仍查due/cap/原责任deadline。原fullsavingbasis、
completeclosure/全部当前policy读、全页expected CAS、原sealkey/PG ACK不变。
nilpolicy不是Save=false；真实目标持久cap过期是独立因果，无新schema/port/
迁移/Management调度或多trigger关联。

同actual Admission在cap前Consume observation完全相等、无seal、A/B/target
三alpha与V2 beta公开正常；实际到期pending时另一valid同owner SubjectBinding
通过真实公开Consume得到精确forbidden，独立原完整observation未改。然后
原red尾部完整执行：target-only seal绑定原key/deadline，真实originalholder
allACK、World.Reopen原责任erased/原reason/deadline/watermark保持；独立target
准确文件不存在。A/B实际原alpha仍在且未生成seal，B/V2正常；原三个命令
samekey receipt逐字相等与published history保持。不是重Put新版本代替回放。

| `/tmp/lerna-04-ticket05-execution/` 日志 | 实际结果 |
| --- | --- |
| `inherited-cap-admission-first-green.log` | -v exact PASS2.422s（case2.41），3166051/start13220479/exit0/groupAbsent/noTimeout。 |
| `inherited-cap-admission-race.log` | -v sameexact-race PASS3.913s（case2.85），3166937/start13224041/exit0/groupAbsent/noTimeout。 |

normal ownscope `lerna_test_6f1d2b5dc25033eda18a488d` / objects1428731090
(dev33/inode363826)；race `lerna_test_a924b880ef1a8e36f20f3678` /
objects610752923(dev33/inode364173)。原caller20/Go30/wrapper120/initial2s
cap/+20ms/WorkBudget1min不变；sessions70358/1545实际complete，全部native
actualWait/groupAbsent，明确RELEASE/no pending。首红2.223原log/失败边界
保留，不以green抹掉；三原migration源未修改，本轮未声称额外hash命令执行。
targeted policy lock-wait截止仍是单独pending最小tracer，旧ordinary wait及
旧NotRequired whole-page CAS不冒称新targeted支路执行。不同creator、未知
sourcepolicy或任意深图/副本等未由本case全部覆盖；七AC仍claimed未accept。

### 准确 AdmissionTarget 原期限：same700ms 正常/真实锁等待越界

`TestContentOriginalAdmissionCleanupDeadlineSurvivesCurrentTargetPolicyLockWait`
是新targeted支路的独立真实资格，不用旧ordinarykey wait替代。两个新owned
World均首次Manager/Lifecycle WorkBudget700ms、A原acceptedcap2s/currentRev2
宽续期、target真实继承cap/完整保存closure正常与独立V2正常；public全部
finiteAdmission pages确定原target/source/revision/capDue/cap+700msDeadline。
真实cap+20ms，有限Manager.Step观察exactkey实际pending原责任，不假设队列
首job；target正文expired但准确物理alpha仍在，原deadline未过。

HoldPolicy(target)真实consumer阻塞观察ACK后，normal立刻positive release/
rollback+连接Close；cutoff等待原dutyDeadline+20ms再同原release。consumer
returned和finished均真实join。normal同原700ms实际seal/fullholder allACK/
独立target物理absence；cutoff完整原公开observation未改，pending原reason/
residual/deadline保持，无seal/activebodycleanup且原targetbytes保留。实际
World.Reopen分别保持结果、source原alpha/noSeal及V2正常；source+target
samekey byteReceipt/pubhistory不改。未扩大700ms、改due/SQLclock/原cap。

| `/tmp/lerna-04-ticket05-execution/` 日志 | 实际结果 |
| --- | --- |
| `admission-cleanup-deadline-first-run.log` | -v exact两scope PASS5.325s（2.43/2.87），3175419/start13260429/exit0/groupAbsent/noTimeout。 |
| `admission-cleanup-deadline-race.log` | -v sameexact-race两scope PASS6.905s（2.84/3.02），3176452/start13264854/exit0/groupAbsent/noTimeout。 |

normal own namespaces62e80867416441ef44acb611与004b84cf037633b4ea8c0c29；
race61f1d1ba80358b6bf1b3851c与a4d69799558d7cc8abc54b47，实际lockerPG PIDs
normal518385/518399与race518574/518587登记。normalroots866594183(dev33/
inode364806)、3750705853(33/364816)，raceroots106745217(33/365146)、3578505999
(33/365157)精确登记；所有scope原Close路径完成，不从kernel退出替logical
Close。caller20/Go30/wrapper120/join3/2s+20ms/700ms固定；sessions33979/7473
actualcomplete、明确RELEASE/no pending。产品固定301be6d不改，首执行直接
正常qualification，无虚构businessred。最小未知结构/currentpolicy错误gate与
既有typedFS目录oracles及最终source/suites/reviews仍待；七AC未accept。

### 新 cap 支路的最小未知结构和当前政策 port 错误门

在sameexisting inherited-cap真实两祖先/实际原pending duty之后，两个明确
机械观察仅在真实原PG操作完成后返回：source LockVersion实际成功后观察nil，
或ancestor CurrentSavingPolicy实际成功后返回固定port错误。它们不伪造私表
或合法政策state，不把nilpolicy解释成false。公共原Consume分别保持完整原
pendingobservation或传播同错误；独立重新Observe fulltuple未改、无target
seal且三个原alpha准确文件仍在。plainconsumer随后同原key完成正常全ACK/
reopen/ancestor保留/固定receipt历史尾部，错误未知资格不借cap删除。缺结构
观察是原结构资格失败；cap因果仍独立允许实际nilpolicy，未造相反oracle。

| `/tmp/lerna-04-ticket05-execution/` 日志 | 实际结果 |
| --- | --- |
| `inherited-cap-unknown-gates-first-run.log` | package compile/input FAIL：3187323/start13310616/exit1/groupAbsent/noTimeout；旧oracle WIP删entries后3处局部err未声明，无业务执行。 |
| `inherited-cap-unknown-gates-compile-repair.log` | 3处if err:=机械修复，-v exact PASS2.392s（case2.38），3190444/start13324294/exit0/groupAbsent/noTimeout。 |
| `inherited-cap-unknown-gates-race.log` | sameexact-race PASS3.893s（case2.85），3191680/start13329410/exit0/groupAbsent/noTimeout。 |

normal ownschema d29c01f7c2f3ee2ae738f43e/root3176456827(dev33/inode366284)，
race5d54ca6ddb66a96fd637eb73/root2267177482(33/366627)。原20/30/120/2s+20ms/
minuteDeadline固定，sessions93683/8128 actualcomplete、全部nativeactualWait/
groupAbsent、明确RELEASE/no pending。compileFAIL首log保留不是businessred，
产品固定301be6d无改。15旧目录oracle文件同pkg已编译，但其case尚未执行，
不借本exactpass声称baselines通过；需要独立finite affected suites/root审查。

### 旧目录 oracle 对新永久锁协议的准确静态整改

15旧test文件的24个实际test函数将目录总数/entries[0]换为原fullRef推导的
准确body key及显式literal原bytes/缺失。独立helper逐个读原body、明确缺失
ref的Lstat不存在，再核整个root目录：只允许这些knownkeys的空regular .lock
文件，原body必须exact，任意tmp/额外body/未知metadata/symlink/目录均失败。
这些旧scenario未发Seal，不忽略.sealed/.sealed.pending，也不宽化未知文件。
损坏/缺失fault原先entries[0]操作现在只操作alphaRef的准确已核正文key。
所有旧currentnormal/拒绝/receipt/history/Close路径保持；两个copy-archive
legacy normal-body expectation精确改为dependency_unavailable，原因是原
archive复制到新root无原media binding/原writerClose资格，政策回填不能认领。
原copy bytes和全部receipt/history/65政策pages仍核；真实sameoriginalscope
停writer后正向升级正常由独立已green frozenlegacy tracer承接。

该整改仅test代码、没有产品或fixture/oldarchive/SQL修改。已在unknown-gates
精确normal/race包编译，但这24个函数尚未执行，不能以包编译冒运行证据。
其最终有限正向selectors normal/race与格式/vet/whole-Go及resource审计计划
单列供rootFULL审查授槽。原3处err声明compileFAIL已留存，再局部:=修复。


### 固定1a4e1d2候选final17实际完成与恢复

原worker在全部17实际native完成后退出协调列表；root逐原log检查completion ACK与当前原PGID absence，并保存 [准确检查归档](ticket-05-final-checks-1a4e1d2/README.md)。原STATIC command manifest保持原字节/标签，实际结果另列outcomes.json。17操作均exit0/groupAbsent/noTimeout；非integration全Go每模式472 RUN/PASS，24旧baseline+7 consumer分别normal/race真实完成。consumer2race为15.042s、PID/PGID3232377/start13491003，不猜toolsession。非integration全Go未执行linux integration holder测试，不制造新未知Close也不代替它们原资格。没有因协调worker消失重跑17。

当前handoff整合为实际现状，已实施legacy、三恢复资格、targeted lockwait与unknown gates不再列未实施或pending；历史推进仍保本台账。整合前handoff原字节留于 `/tmp/lerna-05-api-handoff-pre-consolidation-1a4e1d2.md`，其SHA256：83b6942cc019af7c560e6bbb69e529eb6ed38f4fdb692715ac84c3f2bfe8bd7b。此文档修正不改变产品。

固定双轴Spec唯一P2mixed Scan64 starvation由Astra窄决定采用，当前没有新business red或源码fix。新测试仅STATIC写入，候选的此前绿色结果不覆盖未来产品差量。七AC仍claimed、15/41、profile OFF，旧两个unknown roots原Close与inode事实保持，不清理。


### 新调度反例的独立低积压准备观察失败（非business red）

root审原公共seam/初始caller90、共同seal15s、live45s、Step4、Go/outer120后授fmt与ONE count1 normal。fmt actual0/absent/noTimeout，PID3279394/start13696897；test格式化源SHA28eb405c20056cd284a20c39c1cea13199b195b25453a0672ada964d84fa4935。lowcount1实际FAIL15.339s（case15.33），PID/PGID3279876/start13698741/exit1/groupAbsent/noTimeout、session40596完成，STOP/RELEASE。原log `/tmp/lerna-04-ticket05-execution/expired-backlog-low-control.log` 与failed源/JSON保存；真实owned schema d5d70f509f6d4351cc2147ee/root2823625114 dev33/inode376497。

失败在真实旧seal截止+20ms/reopen后原publicObservation reflect.DeepEqual不等、err nil；V2尚未接纳，目标调度/后续livecleanup尾部未执行。原raw没有逐字段差异，不声明cause已确认。静态Core.Now直接clock_timestamp返回time.Time、Seal观察direct BodySeal而重开从JSON解码，StartedAt location结构有候选差异；只增加公开tuple/时间Equal+Location诊断，原断言及bounds不改，待另grant一次samecontrol诊断。未跑64、无native重试或产品fix。


### same低积压一次诊断：实际确认StartedAt时区结构差异

root另授仅fmt+ONE samecount1 diagnostic；fmt3285260/start13720463实际0/absent/noTimeout，prelaunch诊断源SHA6f7ab31c35aaa38af0ed06359257bf623c34793db9c1be86799c9eb2f2143c62另存，不改旧源/原log。actualFAIL15.408s（case15.39），3285295/start13720476/exit1/groupAbsent/noTimeout，session91471完成，STOP/RELEASE。公开before/after整个Observation JSON逐字相等；原StartedAt.Equal=true但Location Local/UTC，Deadline.Equal=true且均UTC，holders reflect.DeepEqual=true。原seal/ref/deadline/holders没有业务变化，仅非公共时间表示结构使reflect失配；该确认来自实际诊断而非先前静态假说。

own schema d2e8ba97c90849253932fd5e/root18610967 dev33/inode376996；raw `/tmp/lerna-04-ticket05-execution/expired-backlog-low-control-diagnostic.log`、独立prelaunch源+JSON与outside诊断结果JSON留存。V2未接纳，后续live tail未运行，仍非调度business red。没有删除断言/改产品/扩大预算/自动重试64。建议完整公共编码等值校验替代wholeObservation反射结构等值，须root先采用才局部修正/另grant正常control。


### 独立低积压正常control完整尾已真实通过

root FULL actualdiagnostic确认时间Local/UTC仅结构差异后，采用仅test完整JSON Marshal错误fail+公共全字节等值，替两处wholeObservation反射比较。所有fields/array shape/原refs、时间瞬时、holders/deadline/history仍比较，不改产品时钟/BodySeal/预算。fmt3287983/start13731933实际0/absent/noTimeout，prelaunch源SHA60f817d14355d1565f744be850f9309d1ae0c2d0f7190be0e204929d0823823b独立保存。

ONE samecount1 normal actualPASS15.464s（case15.45），3287994/start13731941/exit0/groupAbsent/noTimeout，session27474完成，STOP/RELEASE；schema ae060348e9859d84008298f4/root490049524 dev33/inode377394。`expired-backlog-low-control-green.log`保存。实际新V2 beta published/准确独立bytes；后续45s原live seal全部两个holder ACK/物理absence；第二trueReopen旧完整duty/原alpha/固定byteReceipt/publication history保持、V2正常。旧两准备FAIL/raw/source仍保留不是businessred，64新首red尚未运行。


### 64原到期body Job实际阻挡后续真实V2：首business red

root另授ONE full64 sameexact60f817源；执行前fresh源与命令plan独立保存/fsync并目录fsync，原90/共同seal15/live45/4Steps/Go120/outer120保持。真实64准确refs顺序published/公开正常与独立alpha，额外live控制ref预先published；64原Seal登记共同初始deadline与两holder完整页，过原deadline+20ms后trueReopen全部完整公共Observation/原alpha均保持。真实独立V2 beta随后accepted；4次原Service.Step无error后准确原Command仍publication=preparing/exactRef=true，独立V2key Lstat NotExist（明确检查key与非不存在错误）。此为实际目标调度business red，不是准备/编译失败。

`expired-backlog-full-page-first-red.log` actualFAIL18.131s（case18.12），PID/PGID3290594/start13743233/exit1/groupAbsent/noTimeout，session60810完成，STOP/RELEASE。ownschema8750735b58e5560d1a0a33b1/root1248967092 dev33/inode377785，outside完整结果 `/tmp/lerna-05-expired-backlog-full64-first-red-outcome.json`。首publication断言FAIL，后续live45s/globalACK/第二history尾未执行；不能称另一live/policy red已发生。产品仍1a4e1d2、没有fix/race/retry；须root FULL-read采用实际red与窄候选扫描决定后才实施。


### 窄候选扫描修正后原64业务正常完整尾通过

root采用原3b16扫描职责决定、58f54固定原Record解码资格与5a796 stored BodySeal规范识别补充。新增两Content-owned窄扫描口：普通publish/policy分别LIMIT前、Service最多两页publish优先、Manager仅policy；Lifecycle原完整Subject/PrimaryHolder/原可执行seal窗口LIMIT前预选。原runtime Scan/migrations与expired64责任/字节/期限不写不改，锁后原身份/freshclock/Claim/fence/全holderACK仍独立authority。

原text unique keys、实际Go全Record类型/原整数token和范围、Sources nestedRef、ShadowBodyGone与storedSeal固定Go canonical识别均为“能否排除”资格。JSONB相等不冒Go Marshal字节证明；当前stored识别支持完整固定编码，Record内结构空白/顺序不要求canonical。特殊Unicode/escape/time及机械坏row资格尚未执行，不把这次正常scope当这些case证据。

root授仅fmt6→ONE original full64normal。fmt3429147/start14360495实际0/absent/noTimeout；formatted6产品源+60f原test与三旧SQL静态hash/explicitHEAD85a1067-WIP副本，prelaunch/fsync在 `/tmp/lerna-04-ticket05-execution/expired-backlog-scan-fix-first-normal-source/manifest.json`。原caller90/seal15/live45/Steps4/Go120/outer120保持，GOTOOLCHAINlocal/modreadonly/p1/integration。

`expired-backlog-scan-fix-first-normal.log` actualPASS20.418s（case20.41），3429161/start14360507/exit0/groupAbsent/noTimeout，session5593完成，STOP/RELEASE。scope73c2ee2ae74c7a750cc030b8/root1033797676 dev33/inode384078。真实原64prestate→新V2beta published/exactbody→后续live原seal全部holderACK/物理absence→第二trueReopen64旧duty/alpha/固定receipt/pubhistory保持全部尾执行；旧首red18.131及准备两FAIL原证据保留。尚未race/lowcontrol新源码/第三policyconsumer与其他Subject/机械负向编码/资源审核/新审查或接受七AC。


### 相同产品源与原64业务竞态完整尾通过

root独立FULL normal5line与全部10个source/SQL hash、7个原源副本后另授ONE original full64 race。执行前重新核same6产品源（work.go f690a261、stored canonical helper7286fe4f）/原test60f817及三SQL hash，fresh prelaunch/fsync在 `/tmp/lerna-04-ticket05-execution/expired-backlog-scan-fix-race-prelaunch.json`；explicit HEAD85a1067-WIP。未fmt、未改源或原caller90/seal15/live45/Steps4/Go120/outer120。

`expired-backlog-scan-fix-race.log` actualPASS26.476s（case25.41），PID/PGID3433733/start14380509/exit0/groupAbsent/noTimeout，session81472完成，STOP/RELEASE。own schema adc1a12fd7197b1fc6b272b3/root2968571310 dev33/inode384711；actual outcome `/tmp/lerna-05-expired-backlog-scan-fix-race-outcome.json`。原64prestate、新V2 published/exactbeta、后续live全两holderACK/物理absence、第二trueReopen旧64完整duty/alpha/固定receipt/pubhistory及V2正常全部尾真实执行。该竞态资格与normal同源；未运行新源低积压control、第三policy consumer/其他完整Subject、机械坏row/特殊编码、其他suite/资源审计或新review，不称七AC已接受或本票交付。


### ONE Host实际PG坏deadline保原因与真实合法特殊字符seal正常尾

root FULL采用v2两源/plan，另授仅fmt2→ONE exactnormal。Host tracer通过真实Store.Within/Now/ScanBodyCleanup观察consumer-owned port，正常两个真实published ref/原Seal与有界第一页；Host专属fixture只注入准确首candidate双deadline字面`not-a-time`并保存原bytes供恢复，私表读不是业务oracle。原caller20、fault child3/statement2/lock1、restoration fresh5/statement2/lock1/rowsAffected1、最多8 total真实Lifecycle.Step、Go120/outer120保持。

正常counterpart用接口原允许的非ASCII/quote/backslash/space/HTML/U2028实际PrimaryHolderID与两SealID≤128bytes；原 Objects.Binding、Subject/ref/holder身份固定，真实Go Marshal/PG存储shadow由产品入口完成。fmt2 PID3467224/start14522702实际0/absent/noTimeout，formatted两源SHA7c57d9bf/7f12df34与10旧源/SQL pin在独立prelaunch fsynced `/tmp/lerna-04-ticket05-execution/scan-deadline-mechanical-first-normal-source/manifest.json`，6产品源f690/7286及原60f业务test未改。

`scan-deadline-mechanical-first-normal.log` actualPASS0.671s（case0.66），PID/PGID3467904/start14525665/exit0/groupAbsent/noTimeout、toolsession25126完成，STOP/RELEASE。实际错误明确Go time.ParseError，不是PG cast error、不是nil/静默空页/无cause ErrScope，也不是新business red；本normal实际无需CASE产品改动。own schema cd55ac0aa1641873a0b4ccf4/root2084815391 dev33/inode386516、Host fault PG PID568204，完整outcome `/tmp/lerna-05-scan-deadline-mechanical-first-normal-outcome.json`。

exact原bytes restoration+originalfirstClose后整原job页/全部公共Observation/独立alpha保持；随后真实两ref全holder原完整Identity/Kind/deadline ACK、CleanupComplete及独立准确body LstatNotExist，真实reopen完整最终Observation、fixed receipt/published history与当前授权gone metadata全部尾已执行。只证明这一个真实字段组合与原deadline错误cause；未验证ByteLength计算顺序/任意坏row或编码矩阵，未race、新Manager/fullSubject缺口、其他suite/最新reviews/正式AC接受。


### 同源ONE Host坏deadline/合法原seal身份竞态完整尾

root FULL normal原6raw/outcome/prelaunch及12SHA/flatcopy后，另授same7c57/7f12 ONE exact -race，未fmt/sourcefix/改bounds。新Manager2b364只包compiled dependency，不是本selector RUN或业务资格；13source/SQL pins预启动核真保存 `scan-deadline-mechanical-race-prelaunch.json`，执行期间源写冻结。

`scan-deadline-mechanical-race.log` actualPASS2.182s（case1.14），PID/PGID3481197/start14584530/exit0/groupAbsent/noTimeout、session11866完成，STOP/RELEASE。actual仍Go time.ParseError；正常合法UTF8/quote/backslash/space/HTML/U2028原primary/seal、exactfaultrestore/firstClose/原整job页/Observation/alpha、真实全holderACK/独立准确bodyabsence、实际reopen/immutable receipt/pubhistory/当前gone metadata全部尾执行。own schema398ac6ceb182431638a1317b/root1463348232 dev33/inode387376、fault PG570554；outcome `/tmp/lerna-05-scan-deadline-mechanical-race-outcome.json`。未借本case声称Manager/delegated边界已运行，不需要为了可接受GoParse原因改CASE。其余consumer/affectedsuite/review/AC等闭合仍待。


### 第三真实Manager消费者通过64旧到期责任：ONE正常完整尾

root FULL采用新公共Manager source/plan后，另授仅fmt1→ONE exactnormal。原60f测试与6产品/3SQL/2mechanical source均保持；新独立helpers仅组织该case真实64public publication/Seal15s/expiry+20ms/reopen，不以私Job表做反例或业务oracle。caller90、共同Seal15/+20ms、原Manager Page2/WorkBudget60/≤4 Step、Go120/outer120不变。

fmt3495146/start14648084实际0/absent/noTimeout，preformat2b364原字节保留；actualformatdiff仅注释连续第二行加一个tab，无语义修改；postformat81bc5d898d4c0275fd0a1aa9b7047716fe633cd9ee4fabc669cd46d92f1b8ed6。formatted源及12依赖pin/explicitWIP、formatdiff/ACK/fsyncedprelaunch在 `/tmp/lerna-04-ticket05-execution/manager-expired-first-normal-source/manifest.json`。

`manager-expired-first-normal.log` actualPASS19.681s（case19.67），PID/PGID3495830/start14651192/exit0/groupAbsent/noTimeout、session11514完成，STOP/RELEASE。own schemaa0e085fa52383284ea333314/root3158398028 dev33/inode388291；outcome `/tmp/lerna-05-manager-expired-first-normal-outcome.json`。真实64完整旧职责/holder/alpha/receipt/history预态→已有独立later source/realderivedchild正常save/body→InstallPolicy rev2 savefalse原CAS/即时原SaveForbidden→≤4 actualManagerStep传播source+child两完整原责任/wholeChange原key/watermark及5项合法scheduled/naturalExpiry转移→第二trueReopen wholePropagation+旧64全Observation/alpha/immutable receipt/pubhistory与later原body/history、SaveForbidden全尾通过。责任仍pending/holder_unconfirmed/原policyDeadline/Save-only，source整原责任不变，政策传播不是物理ACK。未对later责任执行ConsumePolicyCleanup/Seal/erasure/allACK；本结果不冒新policycleanup红或任何旧Close复原。

该normal提供真实第三Managerconsumer behindfull64expired资格，当前已有扫描修正，因此没有伪造第二pre-fixred。没有race、delegated第二tracer源码/执行、CASE/sourcefix/其他selectors、受影响suite/最新review或7AC接受。


### 合法完整委派Subject在64其他live责任前页后仅清自己的原责任：ONE正常完整尾

root FULL adopted新单一publicscope/v2plan；completedholders oracle精化为原完整Identity+Kind+deadline一一原index匹配，明确duplicate/omission拒绝，原2holder全部ACK而非仅len2。现source85d995/11121B，fmt1 PID3510349/start14714811实际0/absent/noTimeout且actualformatdiff空；preformat与formatted源/13依赖pin/初始bounds/ACK/fsyncedprelaunch在 `/tmp/lerna-04-ticket05-execution/delegated-cleanup-first-normal-source/manifest.json`。原60f/81bc/6产品/3SQL不改。caller90、初始common liveSeal45、原固定trusted1h/work60/page2/≤4 secondLifecycleStep、Goouter120不变；这是新scope最初live窗，不是原15s expiredscope续期/raise，准备与最终proof均明确原64deadline仍live。

`delegated-cleanup-first-normal.log` actualPASS6.510s（case6.50），PID/PGID3511127/start14718304/exit0/groupAbsent/noTimeout、session89539结束，STOP/RELEASE。own schemac0035b7fa47a0d4c294453b6/root1169068078 dev33/inode389360；outcome `/tmp/lerna-05-delegated-cleanup-first-normal-outcome.json`。两实际合法完整Subject同tenant/leaf、不同非空delegationchain分别保存真实Manager policy/CommandReader/publicPut/Savepermission与原Seal；同primary/binding；first64 live原责任先于second ownSeal。firsttrueReopen wholeObs/alpha/固定receipt/history完整预态→≤4 second真实Step仅原ownDuty、两原holder一一全ACK/独立准确bodyabsence→secondtrueReopen first64 wholeObs/alpha/receipt/history保原仍live、own完整Seal/ACK/immutable receipt/pubhistory/currentauthorizedgone metadata全部尾执行。合法第一Subject无Step/Claim/续期/ACK/停车/原字节改写，不伪造privatetablejob或错误主体oracle。

这是完整保存主体preLIMIT实际正行为资格；没有race/Managerselector/其他suite/sourcefix/CASE/反例重试，不泛化多机生产或7AC正式接受。当前source已窄scanfix，未制造新增pre-fixred。


### 新Manager与合法完整委派Subject同源两串行竞态完整尾

root另授仅两个精确race序列，先Manager、实际Join/PGIDabsence后才delegated；不fmt/sourcechange/合并selectors或其他native。14源/SQL预启动再核，同81bc/85d995与原所有bounds保持，各独立freshprelaunch。Manager race `manager-expired-race.log` actualPASS27.528s（case26.47），PID/PGID3514791/start14733329/exit0/groupAbsent/noTimeout、session40254实际结束；schema7ad72f42ca7949a32c007daa/root3657138946 dev33/inode389919，outcome `/tmp/lerna-05-manager-expired-race-outcome.json`。old64完整prestate→later源/子原save撤权与实际传播two-duty→whole原key/watermark/合法naturalExpiry转移/第二reopen旧64Observation/alpha/receipt/history/laterread正常SaveForbidden全部尾执行。

delegated race `delegated-cleanup-race.log` actualPASS13.953s（case12.90），PID/PGID3517014/start14742643/exit0/groupAbsent/noTimeout、session26786完成；schemae09fffb3e39c50636d4824f7/root19833061 dev33/inode390411，outcome `/tmp/lerna-05-delegated-cleanup-race-outcome.json`。同tenant/leaf不同实际合法完整chain且同primary/binding；first64原责任在最终仍live，secondown全2原holder一一独立ACK/准确absence、第二reopen first64完整duty/alpha/receipt/history与own原Seal/ACK/immutable receipt/pubhistory/currentmetadata全部尾执行。两个race最终STOP/RELEASE事实另 `/tmp/lerna-05-two-qualification-races-release.json`，firstoutcome保“中间slot continuation”字节而不是伪写早已RELEASE。

Manager race copied旧false delegated-written标签精确sidecar `/tmp/lerna-05-manager-expired-race-metadata-sidecar.json`：当时delegated85d995已写且normal6.510qualified、包compileddependency，但该Manager race没有delegatedselector RUN。原outcome字节永留不改；不由该false声称当时test未写。本记录依实际独立后序delegatedrace资格，不合并具名执行。freshlowcontrol/actualLinuxpositive/正确3migration/受影响groups+module/两轴+resource/rootAC仍待；旧SIGKILL原unknown不重新认领。

### 静态发现并纠正原迁移test过期两版本oracle（未运行）

旧integration migration_test两处len2未适配已冻结0003；原final17非integrationmodule/vet只是编译该test而未RUN，不称其曾green。root FULL实际test与Migrate3个embeddedSQL采用最小test-only纠正：repeat及restoredreopen都核实际3编号与ALL原固定SQL SHA。原10s caller/checksum1真实corrupt-refusal/restore/firstClose/ownedcleanup不改，SQL/runtime/14已资格源保持。新test staticSHAb8c9c471 source/diff与27个currentcandidate受影响checks提案在 `/tmp/lerna-05-final-affected-static/`；尚未fmt/Go/DB/native，不造businessred。旧17准确原manifest/字节原标签永留；当前plan不是actualpass或最终接受。


### 同一冻结候选15源的受影响27项真实串行检查全部通过

root FULL采用27项原命令/selector及test-only三版本oracle后，授仅fmt该migration test再顺序检查，未重跑此前已经fresh-qualified的full64/Host mechanical/Manager/delegated四业务selector。fmt PID3535304/start14824359实际0/absence/noTimeout，formatdiff空，migration test仍SHAb8c9c471；原3SQL与六产品、60f/7c57/7f12/81bc/85d995全部14既有pin不改。完整一次formatted15源副本与actualformat/prelaunch manifest保留 `/tmp/lerna-04-ticket05-execution/scan-current-final-checks/formatted-base-manifest.json`。原静态plan/manifest标签不修改、不冒执行事实。

实际toolsession31037已完成exit0，27精确operations串行、每个原ownedwrapper120/原Go120/read-only/p1/count1与原case所有caller/claim/seal/cap/restoration窗口保持。每项预启动重新核15源pin并fsync小型provenance，实际PID/PGID/starttick先于effects登记，实际Wait/ACK/groupabsence后才下一项。format、integration vet、非integration全module normal/race各472 RUN/PASS；四baseline groups分别60全部深度RUN/PASS、24原顶层selectors；两consumer groups分别11全部深度RUN/PASS、7原顶层selectors；三冻结SQL SHA全部核真。非integrationmodule不代表实际Linux或PG tagged qualifiers。

五个必要既有实际integration selectors各normal/race完整通过：原60f低积压control16.370/18.411s；Linux跨进程Put→原erasure0.706/3.186s；冻结原writer正向Close后升级3.753/6.695s；真实三版本迁移/全部原SHA与corrupt1拒绝/恢复1.238/2.669s；原政策/holder多页与其他保存basis2.315/4.625s。每项原raw `scan-current-<name>.log` 与各独立prelaunch/outcome在同executionroot及 `scan-current-final-checks/`；完整汇总 `batch-complete.json`。27项实际全部exit0/groupAbsent/noTimeout；最后PID3551930/start14886240，actualSTOP/RELEASE，无FAIL重试或自动source修复。

原逐项outcome `pass_count` 为未缩进顶层PASS，并未计嵌套子例；`resource_records`未收非JSON的scope行。原outcome与raw字节保持，统一 `counts-scopes-release-sidecar.json` SHA37e5d78e5bb68e62f17fb9dcc0de1c880bfc9a2a42f8b3560c261d65692e6666精确补全部层RUN/PASS、原台账各actualnative START→completion行间scope/child记录；独立当前27PGIDabsence、postexecution15源pin全部未变。此静态澄清不补logicalClose，不做数据库/对象删除。

未重复SIGKILL原holder case/未知原Close，仅保留旧真实killedcase资格与永久unknown3977538271 dev33/inode315225、4116685529 dev33/inode326495。actualLinux正向新资格不把旧unknown变成closed。此前全17原日志/原manifest不覆盖，新27不以旧green代替。当前最新源码必要验证已完成，尚待exactqualification commit后双轴审查、owner-only实际资源审核、root正式7AC接受/merge及准确CI；whole04仍15/41/profile1.2OFF。

root归档上述current actual27与静态原plan/source至 `ticket-05-expired-job-scan/final-affected-checks/`；STATIC owner-only当前资源输入与有界只读审核计划另 `/tmp/lerna-05-own-resource-audit-current-inputs.json` / `/tmp/lerna-05-own-resource-audit-plan.md`，未实际运行PG/catalog/root审计。
