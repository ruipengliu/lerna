# 行动结束到答案阶段的存储重开

`report.json` 是实际调用 `fetchcheck.CheckFrozenResearchReopen` 四次得到的输出，退出码 0。调用程序保存在 `run.go.txt`：

```sh
cp docs/implementation/evidence/22-answer-phase-reopen/run.go.txt /tmp/lerna-research-reopen-run.go
go run /tmp/lerna-research-reopen-run.go
```

每例都先通过真实本地 HTTP、Core、SDK 完成搜索与页面行动并进入持久化答案阶段，再关闭并重开实际 SQLite/Content。原 Actions（含调用身份与答案阶段标记）、执行回执、模型用量、获取预算、原结果身份、正文/摘要/获取时间逐项核对。新建行动模型实例的调用次数必须为零；随后继续原任务答案生成与正式发布。

为验证租约恢复，重开后将测试时钟前移 11 秒，使原 10 秒租约过期。四例工作代次均从 1 变为 2，恢复后行动模型调用均为 0。不是实际等待 11 秒，也不是将时钟改变当作进程退出。

| 用例 | 任务模型请求 | 实际搜索 / 页面请求 | 保留的网络预留 |
| --- | --- | --- | --- |
| answerable | 3 | 1 / 1 | 2 |
| insufficient | 3 | 1 / 1 | 2 |
| conflicting | 3 | 1 / 2 | 3 |
| fetch_failed | 3 | 1 / 1 | 2 |

模型请求为原行动 2 次及答案 1 次，token 仍是协议夹具申报的符号用量。ActionQueries 仍只报告 Core 行动查询，不把恢复验证的额外观测读取冒充完整查询计量。

新增入口测试及代次记录测试先因缺少实现编译失败；首次三路径单次通过（6.365 秒），相关普通/回放/重开 race 回归通过（54.637 秒），fetchcheck vet 通过。另实际调用 API 完成全部四例，报告中无 Error，代次和预算逐项核对通过。

此证据只覆盖公开冻结配置及“行动已结束、答案尚未预留”的检查点。它不是实际进程崩溃、答案请求中断、撤权配置重载、任意故障点或真实模型语义验收的证明。原任务和动态授权治理的进一步验证继续保留。
