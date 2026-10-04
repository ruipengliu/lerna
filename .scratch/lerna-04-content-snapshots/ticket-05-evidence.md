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
