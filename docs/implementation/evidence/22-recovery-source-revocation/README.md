# 进程恢复期间撤销实际来源

复现命令：

```sh
go test -race ./profiles/fetchcheck -run 'Test(EvidenceAnswerRecoversAcrossProcessExit|EvidenceBrain|AnswerInheritsAcquired)' -count=1 -v
```

`test.log` 保存实际输出，退出码 0，用时 17.140 秒；fetchcheck vet 通过。新增验证入口先因缺少实现而编译失败，接入后单例通过（1.093 秒）。

action_content_revoked 用例在同任务获取行动完成后，令答案子进程在受控 Save 完成后退出（76）。父进程重开存储，先确认原输出操作对应的答案确实 available，然后只撤销实际网页来源 web/start；web/final 和任务目标来源仍获准，避免用全局拒绝替代来源继承验证。

验证结果：

- 原答案 Recover 被拒绝，任务没有正式结果，也未变为 COMPLETED。
- 对原网页和派生答案直接 READ 均返回 PERMISSION_DENIED 且无数据。
- 原 Actions、ExecutionReports 及未知模型请求/token 预留没有被修改或退回。
- 恢复明确的公开测试来源策略后，Recover 发布先前保存的同一答案引用；原模型序号仍不可重用，HTTP 总数保持 1。

最终同任务记录保留已知模型请求 1、未知预留请求 1/token 2560。这是本地协议模型的故障恢复与当前来源治理验证，不是实际 API 计费或语义质量报告。

撤权发生在父进程重开之后、发布之前。本例不证明来源策略变更自身已经持久化并跨重启加载；通用宿主配置持久化及其他中断点仍需另验。恢复授权只针对本用例拥有的虚构公开测试配置。
