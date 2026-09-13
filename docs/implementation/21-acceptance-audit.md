# 21 验收证据核对

票据：`.scratch/harness-implementation/issues/21-fetch.md`。实现候选代码 `f0a8638`，说明与复审记录 `cc75512`。全仓 `make verify` 已完成，退出码 0；最终阶段与 profile 原始报告保存在 `docs/implementation/evidence/21-final-verify/`。下列路径均相对仓库根目录。

| 原要求 | 证据及覆盖范围 | 当前结论 |
| --- | --- | --- |
| 正式 Core/Brain/Execution/SDK 获取实际响应并交付来源、时间、媒体类型、正文及摘要 | `profiles/fetchcheck/action_test.go` 从真实授权 Catalog 选择 web.fetch，经原 Core 动作与 SDK 完成 HTTP；`task_test.go`、`adapters/httpfetch/fetch_test.go`、`content_test.go` 核对实际受控响应。模型是明确命名的本地协议夹具。 | 定向通过；不作为实际模型效果证明。 |
| 来源、用途、位置、重定向及实际连接均受当前授权约束 | `adapters/httpfetch/authorization_test.go`、`fetch_test.go` 核对 URL、数值拨号范围、跨跳授权、敏感头、总跳数；`control_test.go`、`inflight_test.go`、`deadline_test.go` 使用真实 Core 控制与期限。 | 定向通过。 |
| 尺寸、时间、跳转和任务用量有界，失败分别表达 | `response_test.go`、`failure_test.go`、`cancel_count_test.go`；SDK 可读取有限失败产物。`TestTaskFetchBudgetRejectionIsKnownThroughSDK` 核对预算拒绝终态和零新增请求；SQLite 预算并发与重开测试核对原身份。 | 定向通过；全局存储容量/提交不确定不伪称已持久成功。 |
| 保留和披露独立受权，网页不改变授权，不自动写长期 Memory | `adapters/httpfetch/content_test.go`、`output_lineage_test.go`、`context_test.go` 与 `brain_test.go` 核对当前来源、动态来源传递及撤权；装配的 Memory 通道为显式 DisabledMemories。 | 定向通过；未声称模型抗注入质量已验收。 |
| Task Context 保留实际证据与缺口 | `context_test.go`、`session_test.go` 验证证据投影、真实 Core 决策、SQLite Context 重开；`failure_test.go` 验证受控失败引用的 external-evidence-gap 及撤权拒绝；`brain_test.go` 验证受控答案发布。 | 定向通过。读取失败不会被改写成可披露缺口。 |
| 重复、取消、响应未知和实际进程退出保持原操作、原时间及预算 | `process_test.go` 的四个实际子进程退出窗口，及 SQLite 实际进程测试；`expiry_test.go` 核对证据到期、清理及撤权后不复活旧正文；原操作重放不新增请求。 | 定向通过。未知窗口仍如实未知，不静默重新获取。 |
| 替换 Adapter，固定重放、真实网络分别标注 | `replay_test.go` 和 `fixed-replay-v1` 使用固定字节，Requests/HTTPStatus 为零，保留本地读取时间；`local-task-v1` 使用实际 loopback HTTP；`public-https-v1` 为明确公共 HTTPS 观测。 | 本地 HTTP 和重放通过；两次公共观测均 DNS resolve 失败。未取得公网成功正文。 |
| 具名入口、依赖/环境/配置/结果/限制记录和独立审查 | `docs/implementation/21-fetchcheck-guide.md`、`21-content-acquisition.md`，原始证据在 evidence；`21-review-initial.md` 记录两轮独立 Standards/Spec 审查。 | Spec 原三项已解决，Standards 仅一项可选测试去重建议。 |
| 全仓验证 | `docs/implementation/evidence/21-full-verify.log` 对应已完成的 `make verify`；脚本包含生成差异、构建、vet、race、全部已有 profile，以及新增 fetch_local/fetch_replay。 | 退出码 0，28 个最终阶段全部通过；所有 profile 的必需用例通过，非必需占位保留原状态。 |

## 真实限制与失败记录

- 公共观测原始报告为 `21-fetch-cli-public.json` 和 `21-fetch-cli-public-recheck.json`。后者记录 resolve/unavailable、实际 HTTP 请求数 0；不将 loopback 或固定重放改称公网成功。
- 先前单独目录回归默认包级 10 分钟超时，记录 `21-fetch-action-regression.log` 保留失败；后续采用 45 分钟上限的动作定向回归通过。后续完整全仓 45 分钟包上限验证通过，catalogcheck 用时 1982.279 秒。
- 本票未调用付费模型，不接管 18 票的真实模型质量验收，也不提前宣称 22 或 83 票的联网问答/完整 V1 门槛。

## 交付判定

21 票完成。实际网络获取已由真实 loopback HTTP 经正式 Core/Brain/Execution/SDK 验证；公共联网条款要求保存结果或失败并禁止冒充离线成功，已有两次独立 Adapter 尝试及原始失败记录，满足记录要求，但不声明公共来源成功。全仓验证及双轴审查门槛已完成；模型质量、联网问答与完整 V1 仍按后续票独立验收。
