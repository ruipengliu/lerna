# 受信参考宿主

本包实现 [治理端口](../../internal/governance/ports.go)，只服务宿主明确预登记的静态内置组件；不执行上传制品，不声称具有不可信 native 代码隔离能力。授权、绑定当前代次和正式数据资格仍由业务数据库裁决。

`NewBuiltinHost(BuiltinHostConfig)` 需要独占用途的 Linux 私有目录、固定 tenant/owner、绑定真实主体的 `Content`、受信时钟、完整安装清单 allowlist 和有限 readiness TTL。准备核验准确制品字节，并进行真实私有文件写入、fsync、回读和删除。初始化先保存原 instance/generation 意图，再启动原实例并由它执行自检；就绪前没有模型或目标请求。Fence 等待实际句柄退出，进程丢失通过 `/proc` 原 PID/starttime 核对；同 ID 不创建替身。Dispose 先关准入，活实例保留 residual，最小终态 journal 保留以防复活。

`Content.Read/Publish` 都在数据库事务外调用；实现必须核验当前来源用途，并按准确 ID 幂等出版。证明只含摘要和绑定，`ProcessedSources` 保留实际引用，`DisclosedSources` 为空。目录需 mode 0700、当前进程 UID 所有。真实记录采用 Linux flock、文件 fsync、原子 rename 和父目录 fsync；`os.Root` 限定所有本地路径。关闭宿主前须调用 `Close` 收束原实例：关闭先拒绝新启动，再在原启动的同一锁域核对全部句柄并逐一 Fence；五秒内不能确认退出时返回错误并保留原责任。

验证：`go test -race ./adapters/governance` 使用真实文件内容、实例句柄和 SIGKILL 子进程；测试核验准确 allowlist、拒绝错误代次、实际退出、活实例残留、崩溃后原 ID 不复活及死进程 fence。这只证明本机受信内置宿主范围。真实外部组件、跨主机进程接管和高影响校准没有配置时继续拒绝。

## 精确文件规则运行器

`NewReferenceRunner(ReferenceRunnerConfig)` 同样需要私有目录、固定 scope、绑定主体的 Content 和受信时钟，另需已登记的 `platform.Keyring` 及 `ReferenceImplementation` allowlist。样本 `Class` 固定为 `reference-rule-file`；准确输入必须是 [GoalSpec](../../internal/brain/rules.go) 的 report 模板，`save_path` 为私有环境内相对路径。`ReferenceReportV1` 生成标题与正文；`ReferenceReportBodyV0` 实际只生成正文，作为可独立测出缺陷的旧模板。两者都是受信编译代码，不加载模型、插件或自然语言策略。

prepare 及实际写入前均核验原 ES256 有限许可：固定注册 key/tenant/issuer/audience/purpose、原 SampleRun/Attempt 对象、完整输入摘要及截止。两臂分别使用真实目录和输入文件，supervisor 的只读真值位于两臂之外；生成函数仅取得输入，不取得真值或目录权力。重用准备环境也核验当前来源和实际两臂文件。真实写入前再检查 Content 当前用途，实际写入、fsync 和独立回读后才出版 Observation。`ModelClaimedComplete=false`，因为没有调用模型；`SystemAccepted` 仅表示本参考规则的独立字节核验。实际无供应商调用，Usage 与 upper bound 均为 USD 0，不计未度量的宿主基础设施成本。

原 arm 一次绑定原 Attempt，原 journal 先于目标效果 fsync。回答丢失从 `Lookup` 取原观察；未留下观察的已启动记录保留未知，不重新生成尝试或改写目标。Seal 先持久关闭入口，再取消本宿主原运行并等待实际退出，取得同一进程间锁后才删除两臂和 judge 目录。超时保留具体未收束责任。已封环境、原尝试最小记录和原未知事实不复活；过期许可仍可查原结果。所有目标 IO 都是本机受信同步文件操作，不使用外部 API；跨宿主在途接管和不可信组件没有本端口支持。

真实平台测试额外验证两臂独立文件和冻结字面真值、旧模板失败、丢答复/重复查询、准确签名与时间反例、当前来源失效、原在途取消与等待，以及目标写后 SIGKILL 重启仍未知且不重写。只报告这一 `reference-rule-file` 范围，不将它称为开放自然语言质量、通用模型改善或千次供应商 API 的验收。正式 holdout 谱系及高影响校准仍需独立登记端口；未配置时业务层拒绝正式保证。
