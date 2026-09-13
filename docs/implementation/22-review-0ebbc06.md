# 22 票整合审查：0ebbc06

整票比较范围：`git diff 6d40e16...0ebbc06`。本轮重点复核 `git diff 6174dbf...0ebbc06`，结合此前对整票及 4442dce 后变更的独立审查；不声称本轮重新逐行审阅全部历史代码。

规格来源：`.scratch/harness-implementation/issues/22-search.md`。两轴使用独立审查代理，均只读，不运行外部服务。相关运行证据及旧失败保存在 `22-acceptance-audit.md`。

## Standards

没有新增明确规范违反或可操作设计缺陷。已有接纳记录按 read 权限核对，缺失记录走正式 admission；恢复驱动重绑当前 guard 与原查询额度。未开始的原 Generation 通过 BeginRequest 首次调用，其他状态只恢复输出，用量检查保留原预留。

保留一项既有维护性建议：搜索和页面驱动重复维护 Outcome→Observation 映射，包括受控获取、过期事实和引用隐藏。集中映射可降低后续语义分叉风险；当前不是功能错误或阻断发现，本轮不扩大为新增严重问题。

结论：硬规则违反 0 项；既有设计建议 1 项。

## Spec

未发现新增可操作实现问题。此前 P2 的三个具体窗口已各有修复及相应边界测试：

| 窗口 | 原身份恢复方式 | 已有证据 |
| --- | --- | --- |
| Core 已派发、Execution 尚未登记 | 按完整原 Request 核验准入，经 SDK Invoke 登记同 OperationID；拒绝不能解释为不存在。 | 未登记恢复、资源版本变更拒绝、原请求及累计查询计数。 |
| Execution 已登记、尚未启动 | Run 的事务门仅启动原未 Started 记录；当前恢复 guard 与隐私查询端口重新绑定。 | 已登记等待恢复、一次 HTTP、原资格及预算；已有记录 invoke 撤权后仍可读取核对。 |
| 答案额度已预留、尚未 BeginRequest | 原 Started==0 且未 Settled 的 Generation 进入首次生成；不另行预留。 | 原身份/输出操作不变；begun、settled、撤权、响应未保存及已保存窗口分开验证。 |

已开始或未知请求只核对原结果；当前权限、原额度和身份检查保留。上述三个具体发现关闭，不等于任意进程强杀、全部搜索/页面中断点或过期资格接管均已验收。

## 验证状态

- 6174dbf 的完整 fetchcheck race 通过（382.982秒），不覆盖之后的代码。
- 最后执行接纳恢复组合 race 通过（10.170秒）；最后答案预留及保存恢复七类组合 race 通过（26.260秒）。相关 vet 通过。
- `make verify` 正在固定 worktree `/tmp/lerna22-verification-0ebbc06` 运行；日志 `/tmp/lerna22-verification-0ebbc06.log`。本审查时已进入测试阶段，未取得终态。运行中的阶段 JSON 初始 not_run 值不能用作最终结论。
- 旧 4442dce 全仓失败保持失败，不能以本轮静态审查或定向通过覆盖。
- 真实模型追加预算尚未授权，评价 execution-lock.json 尚未完成，DuckDuckGo 公网正向尚未通过；本轮未访问 .env、调用公网或付费模型。

22 票仍为 in-progress，原验收项不改写、不勾选为通过。
