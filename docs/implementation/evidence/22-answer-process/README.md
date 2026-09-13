# 证据答案的真实进程退出恢复

复现命令：

```sh
go test -race ./profiles/fetchcheck -run 'Test(EvidenceAnswerRecoversAcrossProcessExit|EvidenceBrain|SearchRecoversAcrossActualProcessExit)' -count=1 -v
```

`test.log` 为实际输出，退出码 0，用时 13.859 秒；fetchcheck vet 通过。首次测试因子进程探针未实现而失败，探针接入后两个窗口单次通过（1.083 秒）。

父进程先经真实 HTTP/SDK 保存页面，并为消费该证据的答案任务预留一次生成。子进程重新打开真实 SQLite/Content，运行 EvidenceAnswerBrain，在协议模型生成响应后或受控答案保存后直接退出（76），不执行用量结算 defer。父进程只使用 BindEvidencePort.Recover，不构造模型或重新获取材料。

| 退出窗口 | 恢复时任务状态 | 正式结果 | 模型已知请求 / 保留请求 | 保留 token |
| --- | --- | --- | --- | --- |
| 模型响应后、保存前 | RUNNING | 无；恢复报错 | 0 / 1 | 2560 |
| 受控答案保存后 | COMPLETED | 原输出操作对应的答案 | 0 / 1 | 2560 |

两种窗口均保留原 generation.OutputOperation 和 Started=1，原模型请求序号 0 不可再次使用。页面 HTTP 总数始终 1。可恢复窗口重新读取正式答案，按当前受控证据校验完整引用契约；重复 Recover 不改变正式结果身份。没有持久化用量结算，所以即使答案已完成，未知模型消耗也不退款或伪造为已知用量。

此处模型为本地协议夹具，2560 是原预留上界，不是实际 API 计费观测。页面获取任务与消费证据的答案任务是分开的；本测试不声称验证了完整 search→actions→answer 单任务在同一故障点跨进程恢复。模型外部 API、超时后的 worker 调度及更晚的发布检查点仍需另验。
