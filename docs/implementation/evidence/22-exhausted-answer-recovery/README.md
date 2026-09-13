# 已保存答案不能绕过耗尽的查询预算

复现：

```sh
go test -race ./profiles/fetchcheck -run 'Test(EvidenceAnswerRecoversAcrossProcessExit|ResearchContentQueries)' -count=1 -v
```

实际通过（14.228 秒），fetchcheck vet 通过。新增 action_content_budget_exhausted 使用同任务真实获取、答案 Save 后子进程退出 76，再以实际授权 READ 消耗剩余额度。独立观察者确认输出操作仍关联 available 答案，随后再次关闭重开真实 SQLite/Content，绑定原任务恢复 Port。

首次和重复 Recover 均返回 QUERY_BUDGET_EXCEEDED。恢复前后查询均为 32/32；任务没有正式结果，观测状态为 RUNNING。原行动、回执、输出身份及请求序号保持；HTTP 总数 1，模型已知请求 1、未知保留请求 1/token 2560，没有退款或重新生成。

测试起初缺少耗尽装配；接入后又因重开宿主未安装 inline goal 策略，观察者无法确认答案存在。现先加载与正常恢复一致的公开测试策略，再进行独立存在性核验；未放宽数据权限，也未声称策略自动持久化。单次测试随后通过（1.252 秒）。

此处只证明预算耗尽时不能发布保存答案，不证明后续超时调度或所有任务查询已被计量；仍是本地协议模型，非真实模型效果验收。
