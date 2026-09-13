# 22 票首次可执行 profile 报告

实际执行方法：

```sh
go build -o /tmp/lerna-searchcheck-22 ./cmd/searchcheck
/tmp/lerna-searchcheck-22 -profile frozen-loopback-v1 -case all
```

`report.json` 为该二进制实际标准输出，退出码为 0，四例 RuntimeVerified 均为 true。模型为本地协议夹具，SemanticQuality 保持 not_evaluated，ExternalModelRequests 为 0；不是公网、真实模型或固定回放报告。

| 用例 | 模型请求 | Core 行动查询 | 网络预留 | 实际搜索 / 页面请求 |
| --- | --- | --- | --- | --- |
| answerable | 3 | 15 | 2 | 1 / 1 |
| insufficient | 3 | 15 | 2 | 1 / 1 |
| conflicting | 3 | 16 | 3 | 1 / 2 |
| fetch_failed | 3 | 15 | 2 | 1 / 1 |

每例 ModelTokens 为 282，来自协议夹具申报的符号用量及 Core 结算，不是实际模型 tokenizer 或计费观测。ActionQueries 仅表示 Core 记录的行动查询。失败页面的网络预留没有退回。

环境为 Go 1.26.1、Linux 当前单机工作区。Build 中 vcs.revision 为实施前 `4a0fc097edf53df3ade28e03c01cc02813266b39`，vcs.modified 为 true，准确表示这次候选尚未提交时构建；不是声称旧提交已经包含新 CLI。配置和局限详见 `profiles/searchcheck/README.md`。

构建前相关 race 回归通过：fetchcheck 62.544 秒，cmd/searchcheck 5.863 秒；两包 vet 通过。此处不替代 22 票最终全仓验证和双轴审查。
