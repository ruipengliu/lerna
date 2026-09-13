# 搜索行动复核期限诊断

## 原始信号

新增页面拒绝路径单独运行通过；相关 race 回归中正常及拒绝用例停在 WAITING，只有一次搜索请求而没有页面请求。该次回归失败，未当成通过。串行复查仍能复现，排除仅由两个 Go 测试进程重叠造成的解释。

有限诊断确认：搜索账本 known=true、status=denied、requests=1；Execution 为已确认失败，任务随后 budget 等待。最小 SDK local 搜索通过，问题集中在较大的行动任务状态与查询源检查组合。

## 假设与证据

依次检验：复核期限不足、后台续租导致资格改变、查询输入/来源不匹配。边界探针仅记录检查类型、耗时、context 错误，不记录查询、正文或凭证。

四任务并发诊断稳定复现 4/4 失败：查询检查耗时 55.744–67.340ms 时 context deadline exceeded，随后任务检查观察到 context canceled。单次 WatchGuard 只给整组任务+查询检查 100ms，与 100ms 轮询周期混用；任务资格检查先消耗了其余时间。没有观察到真实撤权或输入改变。

## 修复与边界

保留 100ms 轮询周期，将单次复核上限设为已有的 1 秒授权检查上限；仍继承原获取 context，不能超过调用者的总请求时限。复核仍串行，取消后等待 watcher 退出；没有后台无限重试或用旧授权放行。原单秒 HTTP/Execution 配置未扩大。

修复前四任务诊断全部失败，修复后同组通过（6.528 秒）。移除临时探针，将四任务真实运行保留为并发回归。最终原单任务反馈循环、页面拒绝、未授权候选、四任务并发及在途 PAUSE/CANCEL 同组 race 通过（20.979 秒）；fetch/fetchcheck vet 通过。

命令：`go test -race ./profiles/fetchcheck -run 'Test(OneTask|ConcurrentResearch|TaskControlInterruptsInFlightAcquisition)' -count=1`。

这验证本次组合复核期限问题；真实超时与各层授权错误的全部分类仍须完整失败验收，不据此声称所有故障路径完成。临时 DEBUG-search22 探针与诊断测试名已从代码移除。
