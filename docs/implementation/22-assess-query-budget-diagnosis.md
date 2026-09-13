# 22 票 Assess 查询计费诊断

初始检查点为 beefb68。以下记录当时未通过、未提交的工作树及失败证据；后续修正与验证结果见末尾。22 票整体仍保持 in-progress。

已做的改动：

- Assess 使用原决策绑定的 Content 读取判断搜索结果是否为空，并计费。
- 有 READY/DISPATCHED 行动时暂不进行下一阶段判断；页面/失败终结事实仅用于转入答案装配，不直接发布答案。
- 一次串行宿主内保留按搜索引用区分的空/非空事实；不是正文或权限缓存。后续模型上下文仍读取受控来源。此调整的阶段边界授权行为仍需进一步核验。
- 失败产物与原 Outcome 的一致性检查移至实际答案上下文读取边界（researchFailures），不增加额外预读。
- 发布失败诊断增加原 Core 的查询数量、限额和任务状态。

失败证据：

```text
go test ./profiles/fetchcheck -run '^TestResearchAnswerRetainsEveryFailedPage$' -count=1
--- FAIL: TestResearchAnswerRetainsEveryFailedPage (2.26s)
    partial_research_test.go:44: combined action and answer task failed: queries=64/64 state=RUNNING cause=INPUT_INVALIDATED
FAIL lerna/profiles/fetchcheck 2.276s
```

此前完整普通子集也失败，不能用其他单例的通过覆盖。已确认失败时原 64 条额度耗尽，不能通过扩大上限或排除必要读取标记成功。旧的 INPUT_INVALIDATED 信息没有暴露计数，本次诊断使下一步可直接核对超额的读取边界。

下一步需减少重复的实际读取，同时保持模型输入、失败事实、内容可用性与当前 process/disclose 权限检查，特别核验阶段交接及缓存事实不会替代当前授权。之后重跑普通子集，再运行相关 race/权限/恢复回归。当前未运行修正后的 race，未声称此草稿通过，未改票据验收框。

## 后续修正与验证

随后定位到发布 Port 对同一结果先 LOOKUP、再 GET。现改为先按原 generation 找出输出操作，在输入与来源校验之后做一次 LOOKUP，同时核对原操作身份、结果引用和当前 available 状态；没有复用早先的可用性快照，也没有免计实际读取。独立 ValidateResult API 保留给外部调用者。

`go test ./profiles/fetchcheck -run 'Test(ResearchAnswer|OneTask|EvidenceAnswerRecoversAcrossProcessExit|EvidenceBrain)' -count=1` 已通过（25.021 秒），包含原双失败反例及恢复/撤权路径。共享 Port 的普通答案、提取答案与联网答案全 profile race 回归正在运行；此处先记录普通测试结果，不提前宣称兼容性验证完成。

最终 `go test -race ./profiles/answer ./profiles/extractioncheck ./profiles/fetchcheck -run 'Test' -count=1` 全部通过：answer 247.402 秒、extractioncheck 97.849 秒、fetchcheck 184.880 秒。对应 vet/diff 检查通过。该验证覆盖共享发布 Port 的相关 profile；不是全仓验收或 22 票完成证明。本诊断中的 64/64 发布失败已解决，其他计量缺口见 `22-query-accounting-audit.md`。
