# 受信开发宿主与显式行动装配

该包装配可重开的开发参考宿主；公司身份、供应商质量、生产发现与跨设备资格须由相应部署另行提供。默认 `RuleEngine` 仅解释完整 `answer`／`report` 目标模板，报告仍经 File inspect → write → 独立 readback 和治理证据完成。默认配置不会开放 GUI、信息源或 WASI 的 Task 行动。

`Config.action_bindings` 是闭合的显式配置数组，上限 14 项，加原 File 两项总上限 16。每项只有 `capability_ref`、`binding_ref`、`install_lock_ref`、完整 `grant`。Capability 必须与已登记驱动的版本、摘要、闭合输入及输出 Schema 完全对应；全部 Schema 合计最多 96 KiB，持久装配记录仍受 Runtime 256 KiB 上限。配置不是模型提案，不能由模型创建或更换。

GUI v2 当前可从真实 App → HTTP 模型 → Task／Governance → Execution → 本机手机目标使用。新 Source Search／Body 和 WASI 的宿主挂点尚待对应驱动完整验证及后续装配提交；本版不会仅凭其类型存在开放 Task 能力。独立 Executor、Agent 和 EndpointChannel 的拓扑也须显式配置完成。

## GUI 配置与前提

应使用独立开发配置目录，不修改已有演示 scope、token 或许可。先用 `InitializeConfig` 建真实 SQLite 或 PostgreSQL 数据库；SQLite 需 CGO，PostgreSQL需可用的明确 DSN。初始化目录、token、私钥和手机原日志为私有文件。下例是配置既有 `cfg` 的完整 GUI 叶配置；`ctx`、已初始化的 `cfg` 由调用方提供，`SaveConfig` 写回自己的绝对路径。

```go
// imports: time + development, adapters/execution, adapters/platform, api, runtime
cfg.UserRoles = append(cfg.UserRoles, "device_controller") // 独立fixture显式授权
scope := runtime.Scope{TenantID: cfg.TenantID, OwnerID: cfg.OwnerID, DatabaseID: cfg.DatabaseID}
deviceID := platform.StableDevelopmentID("resource", "phone-one")
leafLock := api.ComponentRef{
    ComponentID: platform.StableDevelopmentID("component", "builtin-install-lock"),
    Version: "1.0.0", Digest: api.Hash([]byte("harness-builtin/builtin-install-lock/1")),
}
cfg.ActionBindings = []development.ActionBindingConfig{{
    CapabilityRef: execution.PhoneGUICapability().Ref,
    BindingRef: scope.Ref(api.NewID("binding"), 1), InstallLockRef: leafLock,
    Grant: api.Grant{
        GrantID: api.NewID("grant"), OwnerID: cfg.OwnerID, Revision: 1,
        SubjectRef: scope.Ref(cfg.OwnerID, 1),
        Resources: []string{deviceID}, Actions: []string{"gui.click"},
        Purposes: []string{"goal_action"}, Recipients: []string{cfg.OwnerID},
        Locations: []string{"cloud"}, Mode: "continuous", State: "active",
        NotBefore: api.Time(time.Now()), ExpiresAt: cfg.PolicyExpiresAt,
        Limits: []api.Amount{{Unit: "USD", Value: "1"}},
    },
}}
// development.SaveConfig("/absolute/independent-fixture/config.json", cfg)
// development.OpenApp(ctx, cfg, true) 初始化导入明确的上述Grant；重开用 false。
```

该段仅在配置首次建立时执行一次；保存后重启读取原配置，不能每次启动生成新的 binding／Grant ID、NotBefore、ExpiresAt 或角色。

原三个设备 ID 为 `phone-one`／`phone-two`／`phone-three` 经上述稳定函数映射的准确 ID。Grant 资源只能为明确配置的准确设备 ID；动作逐项为 `gui.click`、`gui.swipe`、`gui.input`、`gui.back`。只允许 click 的许可不能执行 input。GUI 使用宿主位置 `cloud`；原 Task 执行者为该宿主 owner。ServiceAuth 保留原角色，不自动取得设备控制权；公开观察由显式当前 `device_controller`／`executor`／`admin` 的 UserAuth 读取。缺角色、配置、驱动、当前 Grant 或原观察时闭合拒绝。

要由模型建议 GUI 行动，还需完整显式 `Config.model`（[类型与装配](model.go)、[HTTP 供应商合同](../providers/README.md)），包括准确 Profile、receiver/location、环境变量凭据引用、CredentialID、有限并发、字节及超时上限、受信 tokenizer 合同和明确费率。缺凭据不会进行模型 HTTP 出站。本机合同测试的 HTTP 服务只提供闭合草稿，不代表模型自然语言质量。默认 File RuleEngine 即使配置 GUI，也不会自行改成 GUI 策略。

GUI 入参沿已登记闭合 `PhoneGUIArguments`：公共字段为原 `resource_id`、`instance_id`、`control_epoch`、`observation_id`、`target_version`、`action_before`，随后为 click(point)、swipe(from/to)、input(text)、back 四个排他的分支。必须先公开 acquire／observe 得到真实 lease／控件树，并使用原观察的完整身份与期限；同一个旧观察不能靠替换 target_version 延长。Task 资源串行键为 `device:<原resource_id>`，Execution 原 ResourceRefs 包含该 Observation 的准确 ResourceRef。[GUI 驱动合同](../execution/GUI.md)描述目标版本、人类接管及未知效果恢复。

## 原身份与当前门禁

Context 对每个准确 cap／binding 调用非消费 `grant.check`，仅声明当前候选。`once` 许可不会被 Snapshot 编译或提案准备消费。每轮 `platform.action_snapshots` 固定 scope、原总装配 lock 及准确叶描述，模型只看到有限 Schema、cap/binding/leaf lock 和允许动作／资源；完整 Grant 与配置摘要留在本库。

受信 `ReadProposal` 读取原 Snapshot、Proposal 与准确 Arguments，核原 pair、typed 动作和资源，并通过公开原观察查询确定 GUI ResourceRefs。非消费当前 Grant 预检可以拒绝撤回后的原快照；它不能替代 Task 同事务的真实 `UseTx`。原 PreparedAction 与 `platform.action_admissions` 授权投影同事务提交，原 OperationID 下字段变化返回冲突。CommitUnknown 时当次不派发，恢复只沿原行与原命令，不重建参数或期限。

Task 授权强读该原投影并比对完整 PreparedAction，再以固定 Grant 调用治理 `UseTx`。Execution 在原启动栅栏核 Task 原意图、准确 lock／binding、固定 Use、当前主体和当前许可 head。旧 File 叶 lock 仍解释原 File Snapshot／已准备 Proposal，新增装配总 lock 不会替换旧行动的 lock。重新初始化只沿 `GrantExistsTx` 确认原 ID；撤回或已消费许可不会被重新导入。

## 本版验证

`action_registry_test.go` 通过公开开发 Config、真实 HTTP、Task／Governance／Execution 方法以及实际 SQLite／PostgreSQL 验证 GUI 原许可、准确目标动作及数据库重开；独立手机原日志证明一次 click。另验证跨 capability／binding、click 许可不准 input、Context 不消费 once，以及原 Snapshot 生成后经公开确认流程撤回 Grant 阻止目标动作。配置增加另一个 binding、改变总装配 lock 后，重开的原已准入 Operation 保持完整原意图和叶 lock，目标仍只执行一次；ServiceAuth 的公开设备查询继续拒绝。

带真实 PostgreSQL 测试 DSN 的 `go test -mod=mod -race ./adapters/development -run '^TestConfiguredGUI' -count=1` 七个子例通过，运行 290.484 秒。配置变更恢复独立执行 `-run '^TestPreparedGUIActionKeepsOriginalLeafLockAfterAssemblyChanges$'`，两库 race 通过，运行 138.953 秒。原完整 File Report 与 GUI 正例的两库普通回归通过，运行 151.697 秒。精确制品保存在执行环境的 `action-assembly-verification.json`；供应商质量、远端设备、Source／WASI 装配不由这些测试代替。
