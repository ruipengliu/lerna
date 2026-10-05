# 05：固定 1a4e1d2 的 17 项实际检查

固定源码 `1a4e1d2d704261a59491dfc6e0c5d26b80ec5107`，17 项原命令均实际 exit0、group_absent=true、timed_out=false。原静态命令清单不改写；实际结果另见 [outcomes.json](outcomes.json)。逐份原日志按字节保存，SHA256、进程身份、具名 RUN/PASS 与实际包时长均登记。root 机械核对全部具名结果，并全文读取短集成日志；两个整仓 Go 日志各 472 RUN/PASS，无 FAIL/SKIP/cached/race warning。

基线四组每模式各 24 个顶层用例，消费者两组每模式各七个，实际运行均匹配原清单。三个冻结 SQL 的原 SHA 保持。整仓 Go 不带 integration，因此不证明 Linux 进程清理协议。

协调状态恢复时原 worker 已不可达，root 使用原完成 ACK 与当前全部 17 原进程组无成员恢复 LOCAL 空闲，不伪称原 worker 明确 RELEASE。进程组无成员不替代逻辑 Close；原 02/05 unknown 资源继续保护。

独立 Spec 的过期任务遮挡问题仍需新真实 red 和最小修复；本检查不接受七 AC，不广告完整 1.2，不替代新源码 CI。
