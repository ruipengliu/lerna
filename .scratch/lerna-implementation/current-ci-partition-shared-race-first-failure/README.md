# 固定三组首次共享 race：Recovery 失败

准确源码 `fa4ad6acfa58927000c830619da55598a0f9a506` 的一次完整入口在 Recovery 阶段失败。`TestPGPoolRunReservedLaneProgress` 实际 0.54s，`pool_test.go:931` 返回 `owned Run lifecycle: driver: bad connection`；Recovery 包实际 90.418s／exit1，make exit2。原包 120s 与整体 1445s 未扩大，也未触发包或整体超时。

[完整原 44 行](partition-shared-race.log)和[实际 Release](shared-race-first-failure-release.json)保留。仅 Go 0012–0014 实际执行，退出为 0／0／1，原 PID 代次等待与整体进程组退出已确认。0015–0022、Component 发现、六个 Component race 组和 fixture 尾均未运行。正常资格独立保留；不以它证明缺失的 race 尾。

18 份[原副本和哈希](archive-proposal.json)均逐字节复制。[资源侧表](shared-race-first-failure-owned-scope-sidecar.json)保留原 ledger 1167–1436 的登记和当前有限路径事实；尚未查 PG catalog，路径或进程消失不补逻辑关闭。原正常阶段 SIGKILL 的目录 `lerna-local-lifetime-4236457110`、dev33／ino431541 保留，Store.Close 仍 UNKNOWN。

此 head 没有新的 Run 取消修正。旧 source 不重试；待修正交付后自然合并、绑定新源码，再完成共享 race。未 resolve、未扩大分组、未改写任何原失败或 UNKNOWN。
