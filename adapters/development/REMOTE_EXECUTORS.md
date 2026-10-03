# 显式独立设备行动装配

此处是受信开发参考配置。云端保留 Task、原行动准入和 Task 预算；独立
[Executor](../executor/README.md) 使用自己的 SQLite、文件根、签名密钥和原 Attempt。
云端不打开设备目标，也不把设备 ContentRef 改成云端 owner。默认配置没有远端路由；
缺少完整静态配对、ActionBinding 或当前 Grant 时不开放候选。

## 配置前提

先按 [设备入口](../../cmd/executor/README.md) 用显式 `setup` 保存设备真实
`database_id`。保存设备原 owner/instance、Authority 公钥、TLS CA 和有限 peer token
的私有文件引用；重开使用原库、原密钥和原实例。云端也必须使用自己的原数据库身份。
不要从 hostname 推导 owner/database，不使用用户 token 作为设备 peer 凭据。

`Config.remote_executors` 是闭合数组，上限 4 个设备；每个 `RemoteExecutorConfig`
只有 `owner_id/database_id/instance_id/endpoint/tls_ca_file/peer_token_file/public_key_id/
public_x/public_y/bindings`。地址必须是显式 `grpcs://`，不带 userinfo、路径、查询或片段。
两个文件路径均为绝对路径。公钥必须为准确配对的 P-256 设备公钥。

每台设备至多 16 个准确 `executor.Binding`。当前宿主消费 File read/write 两种准确
Capability；BindingRef 仍归原设备 owner，InstallLock、`managed-files` 资源和
`file.read/file.write` 动作必须与设备登记相同。该数组不自行授予权限；云端还须在
`action_bindings` 中配置同一 cap/binding/leaf lock 和完整有限 Grant。
完整 Cloud Task 的当前证据覆盖 File read；File write 配对已登记，完整 Task 写入链
仍须单独取证，不能把设备独立驱动验收当作该装配的成功结果。

以下片段配置已经管理初始化的 `cfg`。`deviceConfig` 是读取的原设备配置，
`pairedKey` 是管理端核验的原设备 P-256 公钥登记；这些依赖不能由模型或请求填入。
准确主体代次来自当前已认证宿主，不猜测其他主体为 gen1。

```go
// imports: development, adapters/executor, api, runtime, time
scope := runtime.Scope{TenantID: cfg.TenantID, OwnerID: cfg.OwnerID, DatabaseID: cfg.DatabaseID}
binding := deviceConfig.Bindings[0] // 必须是已登记的 File read 叶
cfg.RemoteExecutors = []development.RemoteExecutorConfig{{
    OwnerID: deviceConfig.OwnerID, DatabaseID: deviceConfig.DatabaseID,
    InstanceID: deviceConfig.InstanceID, Endpoint: "grpcs://device.example:9443",
    TLSCAFile: "/absolute/private/device-ca.pem",
    PeerTokenFile: "/absolute/private/device-peer-token",
    PublicKeyID: pairedKey.KeyID, PublicX: pairedKey.X, PublicY: pairedKey.Y,
    Bindings: []executor.Binding{binding},
}}
cfg.ActionBindings = []development.ActionBindingConfig{{
    CapabilityRef: binding.CapabilityRef, BindingRef: binding.BindingRef,
    InstallLockRef: binding.InstallLockRef,
    Grant: api.Grant{
        GrantID: api.NewID("grant"), OwnerID: cfg.OwnerID, Revision: 1,
        SubjectRef: scope.Ref(cfg.OwnerID, currentServiceGeneration),
        Resources: []string{"managed-files"}, Actions: []string{"file.read"},
        Purposes: []string{"goal_action"}, Recipients: []string{deviceConfig.OwnerID},
        Locations: []string{"device"}, Mode: "once", State: "active",
        NotBefore: api.Time(time.Now()), ExpiresAt: cfg.PolicyExpiresAt,
        Limits: []api.Amount{{Unit: "USD", Value: "1"}},
    },
}}
// 首次显式 SaveConfig/OpenApp(..., true) 导入上述原 Grant。
// 随后只读原配置并 OpenApp(..., false)，不重新生成这些身份或期限。
```

Grant 的准确 action/resource/recipient/location 与原 typed 参数投影相同。
File read 的 Grant 不开放 write。`once` 许可在 Context 非消费候选检查时不扣额度；
Task 原准入事务调用一次 `AllocateLeaseTx`，不另做普通 Use。默认 File RuleEngine
保持自己的本机策略；模型选择远端 binding 还需完整显式 Model 配置及其数据出站许可。

设备 `output_subject_refs/output_purposes/output_locations` 只是上限。需要用户读取时，
云端准备原输入分别以真实当前 Service/User 主体取得 `SourcePolicySnapshot`；原 bundle
签入这些准确策略和主体代次。设备输出取完整 processed/disclosed 来源交集，再受设备
配置收窄。省略输出配置不会让任意云端用户读取结果。来源不允许新用途时，不能靠增加
本地 Content policy 或读取旧镜像取得权限。

完整写入后独立读回的显式 profile 还须声明原设备事实可以用于后继参数：
宿主参数读取是 `execution.arguments`／`cloud`，传往设备是
`execution_arguments`／`device`；这是两个准确用途，不作拼写别名。文件介质的
`managed_file_write`／`managed_file_read` 用途也分别核验。上述用途须同时在真实
输入源策略和设备输出上限内，且各自使用原 Service/User holder 的当前证明。
新配置不能补权已经出版的旧结果，缺用途的原登记回执仍为 rejected。

## 原身份与恢复

Context 固定本轮总装配 lock、准确远端描述和叶 lock。可信 Reader 保存原参数、
原 ExecutorID 与准入投影；模型不能自报 recipient 或替换设备数据库。旧快照不拿
当前装配自动升级。未结责任仍需保留原静态路由和公钥，不能把新库冒充原设备。

Task 准入先保存原 Operation、预留和有限 lease。窗口前准备固定签名 AdmissionBundle
以及原输入字节，设备确认后才沿原命令投递五秒 ControlWindow。最终发送前重核当前
Task、原主体、原源策略、原 Grant head、原控制和当前 Claim。提交未知时当次不出站。
SDK journal 保存原命令、准确 payload/TTL；恢复先取原回执，不造同义新操作。

云端 `executionBridge.Usage` 只归并一份设备签名 LeaseReport。准确 Operation Usage
来自该报告的原证据，因此 Task 和 Grant 不会分别查询不同进度再双扣。
原 Cloud lease 只调用 `ApplyLeaseReportTx`；普通本方 Use 仍沿自己的原结算方法。
查询原账务和最小状态不授正文，数据门禁拒绝不应伪造零费用或释放未知预留。

新的 Command/Query/Job 建立空的有界证明载体。仅同一调用树、同 Scope 和完整相同
Auth 的延续可深拷刚取得的准确证明；不同用途仍须自己的源 Current。载体最多 100 项、
每项 128 KiB、合计 1 MiB，只保存 whole proof，不保存正文、磁盘权限或无限 tombstone。
Memory 在每次纯 Tx 继续核签名、原 holder/代次、数据库、期限及当前 known-deny。
Control/Release 和失败的 Current 移除本次相应证明；Factory 不执行 RPC。

设备 Dial 每个静态路由有界，构造不拨号。TLS/SDK journal 在 Tx 外和锁外建立。
Close 取消并等待实际 Dial 退出，再关客户端；纯 VerifyTx 不等连接。客户端 journal
按 owner 和明确进程角色分开，分类 worker 再含 PoolID，Channel application 再含
准确 ApplicationInstanceID，避免不同进程争用同一目录锁。

## 当前证据与剩余范围

`TestConfiguredRemoteExecutorTaskReadsOriginalDeviceBytesAndSettlesOnce` 使用真正云端
SQLite/PostgreSQL、真实模型 HTTP、TLS 和独立设备 SQLite/文件；不预置 Task/lease/use。
三次真实模型 POST 中一次提出 File read，设备只产生一个原 Attempt。独立读回字节和
原 Content owner/hash/size 均核对；最后模型明确提出 fail，Task/Grant 原已知
USD 0.00072 均结清。只读 Fact 的 `effect=not_applied` 是已完成读取的正常事实，
不是 Task 成功依据。此片不把失败提案用例冒充成功 Result。

同一真实来源上的公开 Runtime query/job 验证同调用续处理正例，以及角色、用途、独立
新 Query 和新 Job 不借旧证明的反例。两库普通验证日志保存到实施环境
`device-task-regression/`；外部真机、生产身份和跨 AZ 资格不由本机证据替代。
最后 race 的 PG 子例实际通过 138.14 秒；该次 SQLite 因准确
`revision_conflict:device_usage_source_advanced` 让测试立即失败，整次退出 1 的原日志保留。
测试随后仅对此已回滚源 head 冲突沿原 Job/Claim 恢复，不新增行动或扩大期限；
SQLite 独立 race 重跑实际通过 99.826 秒。它不证明任意错误都可重试。

检查 Job 与完成 Job 各自按真实 `task.context`／`task.complete` 用途取得当次原来源
证明，不继承上一 Job 的肯定证明。前者仅沿原 artifact/parameters/evidence；后者只在
原全部检查已完成之后准备。Task/Governance 仍负责原检查选择、当前强门禁和最终 Result。

`TestConfiguredRemoteExecutorPublishesVerifiedTaskResultAndReopensOriginal` 的 SQLite
切片实际通过 25.603 秒：公开 Task 出版 verified Result，准确成果等于实际设备读取；
三次 POST、原 USD 0.00072、零预留和唯一 Attempt 不变。云端与设备均实际关闭并等待
退出，再沿原数据库、密钥、目标和配置重开，保持同一 Result／Operation／Attempt。
此前真实 RED 180.050 秒的公开 Checks 为零，准确原因是检查用途的原 foreign copy 尚未
登记；该 Task 沿保留的 SQLite 一致快照到原 deadline 关闭为 failed，原账务已闭合。
这份最低责任恢复不冒称未保存的运行时 App 配置已经重开。

测试可显式设置 `HARNESS_TEST_REMOTE_FIXTURE_ROOT` 为绝对路径，保留 0700 目录和
0600 的准确 Cloud/Device 配置、原 Task 身份、目标、介质及 SDK journals；默认测试仍
使用临时目录。日志只记录公开身份，不输出凭据。

同一已提交宿主片的 PostgreSQL 成功切片实际通过 65.044 秒，准确 Result、三次 POST、
原费用和云端／设备原库重开同样核验。设备账单 head 推进引起一次明确回滚冲突后，
原 Job／Claim 恢复并结清；没有新 Use／Attempt。这轮与另一 minimum-invoice race
有短暂并行，不能称为独占验证，外部制品保留此事实。

工单 16 保持 partial：成功 Result 的 race、必要拒绝故障矩阵和最终统一
检查／审查仍待后续；完整 Task 写入链也尚未据此取证，不能把这些本地未完成路径归因于
外部凭据。
