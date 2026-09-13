# 行动阶段获取结果账本查询

实际运行 `go run ./cmd/searchcheck -profile frozen-loopback-v1 -case all`，退出 0。使用本地 HTTP 与本地协议模型；不读取凭证，不调用公网模型。报告保留当时的构建信息及 not_evaluated 语义状态。

| 用例 | Core 总查询 | Content 子集 | Outcome 子集 |
| --- | --- | --- | --- |
| answerable | 37 | 16 | 6 |
| insufficient | 37 | 16 | 6 |
| conflicting | 52 | 27 | 9 |
| fetch_failed | 42 | 21 | 6 |

OutcomeQueries 来自行动态势判断与后续规划所需的获取结果账本读取，每次读存储前扣原 Core 额度。两项子计数均已包含在总数中，不能再次相加到总数。Core 自身资格/租约/计费状态核对不作为业务结果账本查询重复计费。

单部件研究上下文已在其 Assemble 结尾完成 Validate，外层不再重复相同检查；多部件仍在组合完成后统一复查。模型前后及正式发布处的当前权限检查保持。不得以这个优化为由缓存权限判断或跳过恢复校验。

行动宿主的其他内部 Content 读取及答案装配中的部分账本读取仍待接入。这不是完整计量或真实模型效果证明。
