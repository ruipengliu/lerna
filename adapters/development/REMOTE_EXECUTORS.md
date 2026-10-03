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
完整 Cloud Task 的当前证据覆盖 File read，以及 File write 后由另一个原 Operation
独立读回、条件核验和 verified Result 出版；实际数据库与测试范围见下文。

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
宿主参数读取是 `execution.arguments`／`cloud`，设备参数输入是
`execution_arguments`／`device`，后继编码意图是 `execution_intent`／`device`；
三个准确用途分别声明，不作拼写别名。文件介质的
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

首次冻结 bundle 前，宿主先核原 Task／Operation／主体／Use／Claim，再在事务外为
原 Service 与原提交者按准确输入用途取得本次来源证明。随后的同库事务仍核完整策略
交集和当前许可，并在提交前重核 Claim；旧 bundle 不重建、不增加用途。证明准备后的
撤权、控制变更或 Claim 失效仍由强门禁拒绝，提交未知不进入设备 Prepare／Dispatch。

设备原缓存缺少的输入只沿原 permission 的准确用途读取；云端先为本方 `cloud` 读取
及 `device` 出站分别取得当前来源证明，再调用 Memory 的两位置强门禁。已存在的
签名 bundle 也遵循此流程，不借上一 Job 的证明，不重封 bundle 或增加用途。
设备缓存确认后，原 Task 仍以 `task.dispatch`／`cloud` 核完整 processed 来源；
宿主在签发控制窗口前为原 Service holder 取得此用途的当次证明并重核原 Claim。
该证明不替代 Task 随后的当前主体、控制和共同提交门禁。

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

`TestConfiguredRemoteExecutorTaskSavesReportReadsBackAndReopensOriginal` 在固定
`9f533523fb86cccf2f92d29a7c20fe62d7694cb1`、854 项源码清单和同一测试 binary 下，
SQLite 完整普通验证实际通过 36.368 秒，PostgreSQL 云端验证通过 51.997 秒。
两者使用独立 SQLite 设备，实际四次模型 POST、两个不同 Operation／Attempt，
设备写入与独立读回均为原 87 字节和同一摘要；Cloud 目标不存在。两项条件均为
usable／verified／pass，原 immutable Result 与正文已出版，原 USD 0.00096
结清且预留归零。云端与设备实际 Close/join，原配置、数据库、密钥、目标和 journals
重开后保持同一 Result／Operation／Attempt，不增加模型请求。

原源码另以 race binary 顺序验证 12 项 SQLite 边界和 6 项适用 PostgreSQL 边界，
实际通过 102.646 秒和 60.134 秒，均无缺失或 skip。边界包括当前源关闭、主体／代次
与用途匹配、新入口空证明、提交未知不出站、准备间取消／撤权、原窗口过期及旧 Claim
不得开始；具体集合固定在执行环境的 `final-guards-frozen-hy788u1x/plan.json`。
数据库范围如下，不能把固定 SQLite 用例当作 PostgreSQL 消费方验收：

| 消费路径 | SQLite race 项数 | PostgreSQL race 项数与准确范围 |
| --- | --- | --- |
| 设备 Source／原缓存策略 | 2 | 未重复选入；设备为 SQLite |
| Memory 当前 proof／准确 holder／提交未知 | 3 | 2 项独立 PG owner；1 项 PG source 加 SQLite consumer 的提交故障 |
| Task 准备间控制／凭据／原窗口 | 3 | 未重复选入；Task／receiver 为 SQLite |
| Execution 栅栏／旧 worker／取消／原准备恢复 | 4 | 3 项真实 PG Execution；准备 Attempt 的提交故障仅 SQLite |

完整正例索引分别为执行环境 `device-task-regression/` 下
`saved-rule-complete-frontier-142g56e5/verification.json` 与
`saved-rule-postgres-driver-o9_snwbx/verification.json`；边界索引为
`final-guards-frozen-hy788u1x/{sqlite,postgres}/verification.json`。
每轮实际退出、全部源码摘要、binary 和 driver 的前后稳定性均已核对。正例配置及介质
保存在私有 fixture，旧 180 秒 SavedRule 失败和早先参数／策略拒绝日志原样保留。
这些证据绑定受测 `9f5335`，后续 CPU／Session 集成不冒称已经重跑本链。

工单 16 保持 in-progress：完整报告的 race、SavedRule 专门拒绝矩阵、最终统一
检查／双轴审查仍待后续。上述边界 race 不替代这些独立范围；外部真机、生产身份及跨
AZ 资格另行验证。
