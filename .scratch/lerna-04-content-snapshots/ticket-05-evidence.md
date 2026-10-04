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
CloseACK文件，父实际断言它再Wait/groupAbsent。kernel ACK不推逻辑Close。

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
