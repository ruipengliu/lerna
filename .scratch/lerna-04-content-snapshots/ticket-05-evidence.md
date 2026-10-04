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
