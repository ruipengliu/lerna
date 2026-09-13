# 同任务行动到答案的进程恢复

复现命令：

```sh
go test -race ./profiles/fetchcheck -run 'Test(EvidenceAnswerRecoversAcrossProcessExit|EvidenceBrain|ActionBrainHands)' -count=1 -v
```

`test.log` 为实际输出，退出码 0，用时 16.338 秒；fetchcheck vet 通过。新增准备入口先因未实现而编译失败，接入后包含新旧四窗口的单次运行通过（2.945 秒）。

新增 action_response/action_content 两例使用同一个 TaskRef：真实 ActionBrain 经 Catalog 选择获取能力，SDK/Execution 实际读取页面，Core 结算行动并转入答案阶段，随后在同任务预留答案 generation。子进程重新打开 SQLite/Content；新建行动模型的调用数必须为零，已完成行动不能重新规划。证据答案生成后或受控答案保存后直接退出（76）。

父进程通过原 BindEvidencePort 恢复，并逐项比较原 Actions（包括调用身份、决策记录和答案阶段标记）以及 ExecutionReports 不变。原页面 HTTP 总数和任务获取预留均保持 1；generation.OutputOperation 和 Started=1 保持；同一模型请求序号不能重用。

| 同任务退出窗口 | 正式结果 | 已知模型请求 / 未知预留请求 | 未知 token 预留 |
| --- | --- | --- | --- |
| 答案响应后、保存前 | 无；观测时 RUNNING | 1 / 1 | 2560 |
| 受控答案保存后 | 沿原身份恢复，COMPLETED | 1 / 1 | 2560 |

已知请求来自先前行动决策，未知预留属于退出前的答案请求；既不丢失先前行动用量，也不因发布成功退回未知消耗。协议模型没有实际 API 计费观测。原独立证据消费任务的两个窗口仍一同回归。

本例证明一个实际获取行动到证据答案的同任务进程恢复，不将其冒称多轮搜索、跨端或所有中断点验收。真实模型、完整查询计量、动态权限/来源变化以及最终全仓/双轴审查仍须完成。
