# 获取能力验证入口（21 票实施中）

本地集成验证：

```sh
go run ./cmd/fetchcheck -profile local-task-v1
go test -race ./profiles/fetchcheck -count=1
```

CLI 运行真实本地 HTTP、Core/Capability SDK 正向及 403/404/410 失败用例。`core-sdk-actual-http-and-replay` 中 replay 指原调用身份重放，不是固定网络资料重放。控制、取消、截止、证据到期和真实子进程退出用例由 Go 测试运行；CLI 四项通过不代表整票验收完成。

明确发起一次公网验证：

```sh
go run ./cmd/fetchcheck -profile public-https-v1
```

固定来源为 https://example.com/，无项目 .env、模型、Cookie 或代理。DNS 解析最多 2 秒，解析后的全部地址必须为非私有单播地址，以各地址精确前缀作为实际拨号允许范围；获取时重新解析不匹配会拒绝。最多一个 HTTP 请求、16 KiB 正文、5 秒网络时限，无自动重试、跳转或离线替代。CLI 总上下文最多 30 秒，包含临时本地授权及 Content 组装。

公网成功需要实际获取并通过受控 Content 保存/读取核对，报告包括时间、来源、媒体类型、尺寸、摘要及请求次数。原正文仅临时保存在受控 Content，验证结束销毁临时目录；报告不携带正文、令牌、本地引用或敏感头。当前公共入口直接验证真实 Adapter 和 Content；Core/SDK 路径由独立本地集成覆盖。

JSON 中 `Passed=false` 时退出 1；不支持的参数退出 2。`Public.Stage` 指示 resolve/setup/acquire/retain/verify/complete 阶段；无实际正文时省略 FetchedAt。Build 信息取自 Go 编译器的 VCS 设置；`go run` 若不提供则为 `{}`，不得解释成已验证的提交版本。需要固定版本报告时先在目标提交构建，并核对 Build 内容。

2026-09-12 首次执行：本地 CLI 四项通过；公网尝试在 DNS 阶段失败，AllowedNetworks=null、Requests=0、Passed=false，未取得公共来源。原始报告为 `evidence/21-fetch-cli-public.json`，该次报告生成早于 Stage 字段加入，因此没有 Stage，FetchedAt 为零值；保留原始输出，不将其改写成成功或新的运行结果。随后独立 `getent ahosts example.com` 同样未解析成功。公网成功验收仍未完成。

执行宿主构造 `fetchexecution.Driver` 时必须提供 `TaskGuard`；缺少配置直接返回 Invalid。参考 `fetchtask.Guard` 使用真实 Core 与授权事务核对原资格，HTTP 在边界及在途等待期间执行复查。将低层 HTTP 观测入口接成用户任务时，不能省略这一层。
# 固定资料重放

运行 `go run ./cmd/fetchcheck -profile fixed-replay-v1`，通过同一 Core/SDK/Content 路径读取宿主固定的短文本。报告 Implementation 为 replayfetch-v1；检查包括外层 fixed-replay 标记和实际 HTTP 请求数为零。该模式的获取时间表示本地读取时间，不能用来证明公共网络可达性或原网页的新鲜度。

## 获取失败与上下文缺口

SDK 的 GetInvocation 保留 FAILURE 结果；在当前内容权限和保留条件允许时，Reference 指向外层受控失败产物。其 output.status 区分 invalid、denied、unavailable、timed_out、cancelled、too_large、unsupported、limit_exceeded 和 expired；output.reference 为空，表示不存在可交付的成功获取正文。evidence 只记录有限 status、mode 和实际请求数，不含失败正文或 URL。

任务获取预算拒绝保存原操作的零请求终态，查询为 NOT_STARTED / FAILURE / NOT_OCCURRED；原有 charged 预留不增加也不退款。再次使用相同操作进行核对不会重新获取，改变语义参数必须使用新操作身份。

宿主可用 fetchoutput.Failures 绑定当前 Content 身份和能力范围，再通过 fetchcontext.NewWithFailures 将受控失败引用纳入任务上下文。失败块角色为 external-evidence-gap，不视作已获取正文；成功和失败引用合计最多 8 项。来源撤权或引用不可读取时，上下文仍拒绝交付，不把权限错误包装成可披露缺口。

make verify 分别运行 fetch_local 和 fetch_replay 阶段，并将报告写到 build/fetch-local-report.json 与 build/fetch-replay-report.json。public-https-v1 仍需显式运行；失败记录不能替代成功获取证明。
