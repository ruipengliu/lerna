# 22 票冻结材料的本地 HTTP 协议运行

此目录保存四类冻结材料及相关任务回归的原始测试日志。来源为 `profiles/searchcheck/testdata/sources-v1.json`，原预登记版本和摘要不变；评分材料未加载进运行宿主。网络是实际回环 HTTP，不是公网，也不是 fixed-replay。

执行入口：

```sh
go test -race ./profiles/fetchcheck -run 'Test(FrozenResearch|OneTask|ActionBrainHands|ConcurrentResearch)' -count=1 -v
```

每个冻结用例运行真实 Core、SQLite、Content、SDK、搜索与页面 Adapter 和正式答案恢复发布。任务最多 3 个执行操作、3 次模型请求、32768 token、64 次必要查询；共享网络预留单页为 2、双页为 3。搜索输入来自冻结查询，页面正文与状态由本地 HTTP 服务交付；取得正文后再次读取受控证据核对原文及获取时间。运行结束核对再次 Run 不增加网络或模型调用。

本轮模型为 `local-protocol-fixture`。不足处置为预选协议响应；双页也使用预设冲突响应，不具备通用语义分类能力。可回答例的 `Fixture claim` 是占位主张，不满足必要事实覆盖验收。日志保存正式输出，不能将测试 PASS 转写成语义正确率、引用支持率或覆盖率。独立评分、真实模型、固定回放和公网运行仍需另行完成。

测试结束会移除临时 SQLite 和受控 Content。日志中的引用是本次任务原身份，不能在新运行中读取；可复查正文来自冻结来源及日志引用片段。后续可独立执行的 profile 仍需提供持久化证据归档，不能声称本测试目录已经交付该入口。
