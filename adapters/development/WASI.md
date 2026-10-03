# WASI 的准确 Task 行动装配

本装配把 [Linux 受限 WASI 驱动](../wasi/README.md)接入原 Task、Snapshot、Brain 提案、Grant、Execution 和预算路径。它使用明确配置的程序与输入，不允许模型替换代码、运行时、Environment 或命名空间版本。默认 `Config.WASI == nil`，不会新增程序入口或 CPU 预算。

## 配置和前提

先按驱动文档静态构建 `cmd/wasi-worker`，记录准确 worker 摘要，再配置开发宿主。`configureWASI` 在目标所属 worker 中取得私有运行目录的独占锁并实际探测隔离；不拥有目标的进程只读原 Manifest 登记合同。平台探针失败时返回具体 `unsupported`，不创建可运行的替代环境。

| `Config.wasi` 字段 | 约束 |
| --- | --- |
| `worker_path` | 已安装静态 worker 的绝对路径 |
| `worker_hash` | 原安装字节的 `sha256:` 摘要 |
| `max_concurrent` | 1–4；探针和 Cell 共用活动槽，实际退出后释放 |
| `cpu_seconds_budget_limit` | 要开放 Task Cell 时必须显式设置，大于 0 且不超过 3600；冻结至提交 Task 的原 Policy |

CPU policy 配置后，宿主才将 `RequiredWASIContentPurposes()` 合入本开发 scope 的内容用途，并把 `cpu_seconds` 加入 Task policy。已有内容的当前许可和完整来源链仍需通过；旧 Task 沿首次保存的 policy 恢复，不因新配置扩大原预算。

随后由受信 owner 按公开 `environment.create` 准备 Environment。本切片要求 Environment principal 与原 ServiceAuth 主体一致、runtime 为 `restricted_wasi_preview1`、准确 ConfigRef／InstallLockRef 与本 worker 一致、已实际退出且 `ReadyForCell`，没有活动操作或停止残留。用户所属 Environment 不能被 ServiceAuth 越权借用。

`Config.action_bindings` 中的 WASI 项使用 `execution.WASIRunCellCapability().Ref`，并追加闭合 `cell`。已有准确 `env`、`codeRef` 和 `inputRef` 时，该字段使用：

```go
Cell: &development.WASICellConfig{
    EnvironmentID: env.EnvironmentID,
    CodeRef:       codeRef,
    InputRef:      inputRef,
}
```

`code_ref` 是准确 `application/wasm` ContentRef，`input_ref` 是被动 JSON 输入的准确 ContentRef。同项还必须给准确 `binding_ref`、运行时原 `install_lock_ref` 和完整显式 Grant。Grant 固定原 ServiceAuth 主体、唯一资源 `environment:<原 ID>`、动作 `environment.run_cell`、用途 `goal_action`、receiver 为原 owner、location 为 `cloud`，以及有限有效期和 CPU 额度；初始化不会复活原已撤回或已消费的一次许可。Task 提交也必须给有限 `cpu_seconds` 预算。

## 原提案、来源和提交

Context 对准确 Grant 做不消费的当前检查，读取原 Environment 后冻结其 generation、namespace revision、namespace ref、代码／输入和完整来源。闭合声明进入普通模型材料；WASM 原 bytes 只留在 `ProcessedSources`，不会转换成模型正文或 system 指令。

模型只能建议声明中的准确 `ComputeArguments`。提案的原 capability／binding／InstallLock、Environment CAS 和全部来源须逐项一致；改动预期 namespace revision 会在 Task 准入前拒绝。运行时 Prepare 不运行用户代码。Task 预算、原 once Use、持久准入意图和 Job 按原共同提交集合处理；CPU 预算不足时不消费 once Grant，不创建 Operation，也没有 native Attempt 日志。

CPU 预留覆盖 Linux CPU 软限加一秒的硬限余量，软限仅允许 1–5 秒。实际费用来自原 worker wait 的 user+system CPU，由 Execution、Task 和 Grant 分别沿原使用量归并；模型 USD 与 CPU 分账，不把固定预留当实际消耗。

只有原 Cell 已 `closed/applied`、`MayApplyLater == false`，并核准原 Operation、generation 和 CAS 后，Context 才装载该 CellResult 指向的完整已提交 namespace。准确字节长度／摘要和闭合 namespace 必须通过当前 Content 许可。它不读取新的 Environment head 来替换原结果。后续报告保存与独立读回、条件检查和 verified Result 仍归原 Task／Evidence 路径，namespace 出现 `42` 本身不等于 Task 成功。

取消或来源撤回保留已发生的效果与原已知费用，禁止新的来源字节读取；原 Attempt 和完整 namespace 的恢复不启动第二次程序。运行时隔离、恶意程序与真实退出证据见驱动文档；本宿主的公共链路验收位于 [wasi_action_test.go](wasi_action_test.go)。

## 验证边界

验收使用本机实际 Linux 隔离进程、真实 SQLite／PostgreSQL 和明确费率的合同 HTTP 模型。它验证原 Task 的准确提案、预算、一次许可、实际 namespace 和 CPU、真实文件回读及不可变 Result，不裁定模型自然语言质量或真实供应商资格。

完整报告测试的 Task 期限固定为五分钟；六分钟仅是覆盖原期限及费用收尾的测试观察。验收还直接核对 `Result.CompletedAt` 早于原 Task deadline。观察超时、已知费用关闭或 namespace 提交都不能代替该结果；失败日志和原 Scope／Attempt 继续保留，未知效果不会重写为已知读回。

2026-10-03 的真实 PostgreSQL 完整 Worker 验收运行 287.11 秒，实际进程 exit 0。原 Result 在五分钟期限前 26.957 秒完成，两项独立条件检查通过；原正文出版和费用结清后才取消并 join Worker，然后实际关闭、重开原库。6 个模型 POST 共 USD 0.00144，原 Cell 只启动一次，实测 CPU 0.004321 秒；4 个 Operation 各保持原唯一 Attempt，完整 namespace 和 18 字节文件独立读回摘要一致。固定源码为 `e148d3a`／集成祖先 `61e8abd`，实现文件摘要 `96c1dc04…`、测试文件摘要 `cd29b13b…`；完整 source manifest、实际 exit、日志、同次 CPU profile 与 test binary 保存于执行环境 `wasi15-worker-wait-publication-20261003T165143249902Z-myw7_32d`。

此前观察超时、Task 超期和正文仍待出版的失败各保留其原 Scope 与记录。本次通过不翻写这些历史事实；准确代码来源撤回后的公共 Worker 负例及受影响 race 尚待独立验证，驱动层的恶意程序／取消测试不代替该公共 Task 交接。

其他 OS／架构、任意原生程序、自定义 guest hostcall、跨 owner／独立设备权威和生产容灾需分别验收。当前容器 PID1 的孤儿进程收养能力不属于 driver 的原进程 Wait 证据；部署资格须同时确认宿主能回收孤儿进程。CI 中的探针定义与本地通过不表示托管 CI 已执行。
