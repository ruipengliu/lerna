# 答案 Content 查询预算运行记录

实际执行 `go run ./cmd/searchcheck -profile frozen-loopback-v1 -case all`，退出 0。报告使用原公开虚构来源、本地 HTTP 和本地协议模型；未调用公网或真实模型。构建信息按报告保留，不能将未提交工作树的运行冒称提交后构建。

| 用例 | 原 Core 总查询记录 | 其中 Content 查询 |
| --- | --- | --- |
| answerable | 33 | 18 |
| insufficient | 33 | 18 |
| conflicting | 47 | 31 |
| fetch_failed | 39 | 24 |

所有记录仍低于原 64 条上限。ContentQueries 是 ActionQueries 的子集，不可相加。该运行证明答案证据、来源继承和正常受控发布的 Content 观察已计费，不证明行动宿主内部及获取账本等全部查询已经纳入。协议输出依然不是语义质量通过证据，报告明确 not_evaluated。
