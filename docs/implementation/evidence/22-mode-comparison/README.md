# 同一候选的 HTTP / 固定回放对比

本轮使用同一份实际编译二进制先后运行两种模式，两次退出码均为 0：

```sh
go build -o /tmp/lerna-searchcheck-modes ./cmd/searchcheck
/tmp/lerna-searchcheck-modes -profile frozen-loopback-v1 -case all
/tmp/lerna-searchcheck-modes -profile frozen-replay-v1 -case all
```

`loopback.json` 与 `replay.json` 保存各自实际标准输出。两份 Build 一致，均为候选修改状态；SourceSHA256 同为原冻结版本。逐例核对正文与 SHA256 一致、状态一致，但引用身份、地址和实际读取时间可以不同，不按输出字节相等评分。

| 用例 | HTTP 实际请求（搜索+页面） | 回放实际 HTTP 请求 | 两模式各自保留的任务预留 |
| --- | --- | --- | --- |
| answerable | 2 | 0 | 2 |
| insufficient | 2 | 0 | 2 |
| conflicting | 3 | 0 | 3 |
| fetch_failed | 2 | 0 | 2 |

HTTP 失败例实际得到 403，有限事实为 denied、http、Requests=1。回放对应显式拒绝快照，有限事实为 denied、fixed-replay、Requests=0；未伪造 HTTP 403、正文或页面获取时间。成功回放证据的 FetchedAt 是实际本地读取时间，不冒充源站当前信息。回放 Adapter 没有 HTTP 客户端；参考宿主的监听器用于确认意外网络调用次数为零。

两模式仍使用协议夹具，SemanticQuality 均为 not_evaluated，外部模型请求均为 0。相同占位/预设响应不能证明通用语义能力；该对比只证明所选 Adapter、实际执行事实与任务治理差异被保留。

本轮相关 race 回归通过：fetchcheck 61.748 秒、cmd/searchcheck 10.718 秒；replayfetch/fetchcheck/searchcheck 命令相关 vet 通过。旧有 text/plain 成功回放继续通过。此记录不替代最终全仓验证或独立语义验收。
