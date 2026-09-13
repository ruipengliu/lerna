# 搜索能力的真实进程退出恢复

复现命令：

```sh
go test -race ./profiles/fetchcheck -run 'Test(SearchRecoversAcrossActualProcessExit|AcquisitionRecoversAcrossActualProcessExit|SDKSearchPersistsOriginalDiscovery)' -count=1 -v
```

`test.log` 为实际输出，退出码 0，用时 8.462 秒；fetchcheck vet 通过。先前首次运行因子进程探针尚不存在而失败，接入后四窗口单次通过（1.859 秒）。

父进程通过正式 SDK 接纳搜索操作并关闭存储。子进程重新打开实际 SQLite/Content，运行真实搜索 Adapter，在指定边界直接 `os.Exit(75)`，不执行清理 defer。父进程确认实际退出码 75 后重开存储，经原 SDK/Execution Reconcile 恢复。HTTP 服务仍由父进程持有，以原子计数观察是否发生补发。

| 退出窗口 | 实际 HTTP 总数 | 恢复结果 | 搜索能力任务状态 | 原预算预留 |
| --- | --- | --- | --- | --- |
| 预留后、发出请求前 | 0 | UNKNOWN | WAITING | 1 |
| 收到响应、保存内容前 | 1 | UNKNOWN | WAITING | 1 |
| 保存内容后、提交获取结果前 | 1 | SUCCESS | COMPLETED | 1 |
| 提交获取结果后 | 1 | SUCCESS | COMPLETED | 1 |

所有窗口在恢复后再次 Run/Drain 都不增加 HTTP 数，原 Execution 输出操作身份、获取证据操作身份和输入指纹保持。可恢复窗口核对原受控正文/摘要/获取时间一致，并用正式搜索 EvidenceReader 解码原候选 URL、标题及摘要；未保存窗口不伪造引用或已取得结果，预留不退款。

候选网址只是搜索服务提供的发现信息，测试没有请求候选页面，也不将其作为已读页面证据。COMPLETED 指本测试的搜索能力任务，不代表完整研究问答已经完成。

这是搜索执行的进程退出验证，与先前存储重开验证分开。答案阶段进程退出、模型请求中断、动态授权/来源变化以及整票最终验收仍需继续，不能由本测试替代。
