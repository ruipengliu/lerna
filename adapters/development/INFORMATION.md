# 显式信息源的 Task 行动装配

本开发参考宿主可将固定公开 HTTP 源的 `information.search`／`information.body` 装配至真实 Task 行动。配置只登记当前候选；Task 仍以原 Grant 使用记录准入，Execution 在原启动栅栏与实际 HTTP 出口重新核验。免费源仍需明确的数据用途和保存许可。

本切片已实现实际取得、当前许可缓存读取、明确披露拒绝和原账务恢复。默认 File RuleEngine 不生成信息源行动；完整参考问答条件和成功 Result 尚在下一切片，不把 HTTP 已取得当作用户问题已答对。供应商账户、通用自然语言质量、跨 owner 和独立 Executor 装配另行验证。

## 配置与依赖

使用自己的独立配置与数据目录。Go／SQLite／PostgreSQL、对象介质和明确 HTTP 模型配置沿 [宿主说明](README.md)；不修改已有演示 scope、token、角色或 Grant。一个源只声明一个准确 origin、受限路径、CIDR、receiver/location，以及字节、结果、等待和并发上限。Search 源还须实现 [参考 wire 协议](../providers/INFORMATION.md)，普通网页不能冒充 Search 服务。

以下是在已 `InitializeConfig` 的独立 `cfg` 上添加本机 Body 源的完整叶配置示例。调用方先准备仅供该 fixture 使用的 `/facts/version.json`，通过明确绑定 `127.0.0.1:8098` 的 HTTP 服务返回 JSON；该服务、文件和时限为真实外部前提。生产配置不能开放开发 HTTP loopback 例外。

```go
// imports: time + development, providers, api, runtime
cfg.Information = []development.InformationSourceConfig{{
    Source: providers.InformationSourceDescriptor{
        SourceRef: api.ComponentRef{ComponentID: api.NewID("source"), Version: "1"},
        Origin: "http://127.0.0.1:8098", FetchPathPrefixes: []string{"/facts/"},
        AllowedCIDRs: []string{"127.0.0.1/32"}, Receiver: api.NewID("source"),
        Location: "cloud", PublicUnbilled: true,
        MaxQueryBytes: 4096, MaxItems: 20, MaxResponseBytes: 65536,
        TimeoutMillis: 2000, MaxConcurrent: 1,
    },
    AllowHTTPForLoopback: true,
    RetainUntil: api.Time(time.Now().Add(time.Hour)),
}}
// 构造只登记本机合同，不解析DNS或发送HTTP。
discovery, err := development.OpenApp(ctx, cfg, true)
if err != nil { return err }
source := discovery.Information[0]
capability := source.BodyDriver().Capability().Ref
sourceRef := source.Descriptor().SourceRef
lock, err := development.InformationInstallLock(source)
if err != nil { return errors.Join(err, discovery.Close()) }
if err = discovery.Close(); err != nil { return err }
scope := runtime.Scope{TenantID: cfg.TenantID, OwnerID: cfg.OwnerID, DatabaseID: cfg.DatabaseID}
cfg.ActionBindings = append(cfg.ActionBindings, development.ActionBindingConfig{
    CapabilityRef: capability, BindingRef: scope.Ref(api.NewID("binding"), 1),
    InstallLockRef: lock,
    Grant: api.Grant{
        GrantID: api.NewID("grant"), OwnerID: cfg.OwnerID, Revision: 1,
        SubjectRef: scope.Ref(cfg.OwnerID, 1),
        Resources: []string{"source:" + sourceRef.ComponentID},
        Actions: []string{"information.body"}, Purposes: []string{"goal_action"},
        Recipients: []string{cfg.Information[0].Source.Receiver}, Locations: []string{"cloud"},
        Mode: "continuous", State: "active", NotBefore: api.Time(time.Now()),
        ExpiresAt: cfg.PolicyExpiresAt, Limits: []api.Amount{{Unit: "USD", Value: "1"}},
    },
})
// 只在独立fixture首次配置时保存并显式initialize导入Grant。
// SaveConfig("/absolute/independent-fixture/config.json", cfg)
// OpenApp(ctx, cfg, true)；以后读取原配置并 OpenApp(ctx, cfg, false)。
```

`errors` 是上例的标准库导入。这些 ID、原 `NotBefore`／`ExpiresAt`／`RetainUntil` 和 binding／lock 应保存一次，不在每次重开时刷新。Source 的能力摘要固定 descriptor、闭合 Schema、凭据摘要及 TLS 根；既有未结行动必须保留其原源配置，不能用当前新源替换。

`Config.information` 最多 8 个源，每源提供两个已登记 driver；仅明确 `action_bindings` 的 pair 可进入 Snapshot，仍受总 16 项与 Schema 96 KiB 上限。凭据仅经 `credential_env` 引用；`credential_required=true` 缺值时闭合拒绝。`tls_root_file` 必须为绝对路径，最多 64 KiB。`retain_until` 是明确的数据保存上限，和原开始窗口独立。源当前位置为 `cloud`，receiver 取准确 descriptor；更广位置须另行完成装配合同。

初始化只沿 `GrantExistsTx` 导入完整许可，撤回或 once 已消费的原 ID 不会重新建 active。Source 不要求 GUI 的 `device_controller`；UserAuth 保留当前显式角色，实际行动使用受信 ServiceAuth 的原 Grant 主体／代次。公开用户读取还须通过当前 Content policy，不能因模型或工具已取得而放宽。

## 原参数与披露

Body 的闭合入参为 `{"url":"http://127.0.0.1:8098/facts/version.json"}`。模型须以 `DraftAction.disclosed_local_ids:["args"]` 明确披露已出版参数 LocalID；名字按原草稿实际 LocalID填写。没有该声明时在 Task 使用前拒绝，0 HTTP、0 Operation、0 Grant 消费。参数来源 DAG 仍以 `information.source`、准确 receiver/location 核验，不能把 URL 位于参数中当作无需许可。

Search 参数为 `{query_ref, limit, cursor?}`。QueryRef 的真实字节必须在原参数 publication 的 `disclosed_sources` 与 Action 的 processed 集合中双声明；宿主只机械保留原声明，不自动补公开 Query 或整个参数 JSON。限最多 20 命中，实际 wire 由源协议固定。返回 `cursor`、`exhausted` 与 `coverage=source_index` 只表达原源范围，不是全网穷尽、事实更新或答案正确的证据。

HTTP 的固定 IP、路径、请求摘要和原 Attempt 仅执行一次。正文进入受治理的真实 Content 介质，SQL journal 保留准确 ref／hash／长度和原身份。`RecoverReceived` 只沿原 reserve／ready／put 身份，不重新 GET／POST、不延长期限。CommitUnknown 当次停止出站。

## 当前数据许可与最低账务

保存期限取原源数据许可、原 Grant、所有实际输入来源和宿主 policy 的最紧值。每次缓存或派生读取重新核当前主体、准确原源配置、数据截止与原 Use 的全部当前 Grant head；撤回、当前数据期限收紧、凭据或原源不可用时不披露正文。Search snippets 与 Operation 结果 metadata 也派生自原 BodyRef，不能成为绕过该门禁的副本。

最低 `execution_usage_proof` 只由原公开执行账本、原 encodedIntent、准确 Attempt、原 source／revision／digest 和实际已记费用形成。有限 authority-only policy 允许其原 publication 的 `content.write` 与 `execution_usage_proof`，不包含正文、标题、snippets 或 quote，不挂数据来源 DAG。读取还要求本 owner 的当前 `usage_reporter`，用户和普通 Context 无该资格。数据许可撤回不会改写已知费用、释放未知预留或使这个最低证明取得正文资格。Task 和 Grant 各自在原账本归并，重开仍核原上传／ProofRef／累计用量，0 新 HTTP。

## 验证边界

[真实 App 测试](information_test.go) 使用独立 HTTP 源与模型、真实 SQLite／PostgreSQL、实际对象介质和公开 Task／Grant／Execution 方法；最小账务测试在原 1 GET 和 applied Fact 后、首次 billing 前公开撤回许可并取消 Task，核已知费用、最低证明用途拒绝、数据库重开和原身份无重发。它们不替代完整问答成功条件或真实供应商质量。受影响 race 与原 File 行为回归的实际结果记录在工单 15 与执行环境制品。
