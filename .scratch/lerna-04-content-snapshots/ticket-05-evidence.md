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
