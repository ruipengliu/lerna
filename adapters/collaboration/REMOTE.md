# 跨所有者 Agent 参考适配器

`NewRemote(RemoteConfig)` 在构造时核固定 owner、数据库、签名密钥、有限 profile 与静态 peer，不访问网络。宿主显式 `BindTask`、`Register`，同时登记对方 Memory 的 `ForeignSourceContracts`；没有对端配置时，不创建可冒充远程委派的同库子 Task。

`RemoteAgentContracts()` 返回同版闭合方法合同。父方 `collaboration.delegate` 在原命令事务保存 Delegation、Allocation 与首个 Job；子方 `collaboration.create` 保存原 creation key，工作者在线取得签名的父 scope、注册原 Content 副本，再以自己的事务接纳 allocation 和唯一子 Task。丢回执沿原 SDK journal 查询，不更换子 ID、命令或期限。

`collaboration.input.send` 固定父、子目标修订和原输入，`collaboration.input` 是接收方原命令。实际回答和 Steer 分别由原 Task 的 Input/Steer Job 决定该命令，输入不取得新增工具许可。回答保持原 request、Content owner/hash/version；派生 GoalDocument 保留真实来源。接收回执给出原 receiver command ref，便于查询准确消费结果。

暂停是父方有效控制与子自身控制的交集；父恢复不解除子自身暂停。取消和 `collaboration.allocation.close` 独立登记原命令。消费门禁可以先于创建或正文消费关闭，关闭后的迟到原回答／GoalDocument 出版不能改变目标。未知费用继续占用原 allocation，终态之后只归并可信原账单的累计差额。

宿主为每次入口调用 `NewParentScopeContext`，该工厂只建立有界内存。`PrepareChildContext` 和 `PrepareInputContext` 必须在事务外取得当前有限父证明及真实来源证明；后续 `CheckTaskCurrentTx` 在原 Task／预算锁之后纯核验。磁盘保存的旧肯定证明不能授权新使用。Memory 的当前证明通过同一次入口的 `ForeignUseProvider` 交接；网络不能放进 Runtime 工厂或数据库事务。

公开参考宿主通过 `Config.RemoteAgent` 显式配对静态 peer、原主体／服务主体、签名 key 和 profile；`ForeignSourceTLS` 给真实 `App.Run` HTTPS 入口提供固定证书引用。构造、LoadConfig 与重开不读取正文或发送 RPC。Task 的 Decision、Operation、prepared Attempt 与完成阶段各自在新工作入口取得当前父证明，原 Claim 和最终短事务门禁继续强核；已发送或未知效果的核对不能被当成一次新的开始。

行动上限来自原父 Decision／Snapshot 的能力、当前 Knowledge selection、原父许可与明确 Agent profile 的交集。Grant 的资源名必须与准确版本 ResourceRef 成对保存，不能从名字猜摘要。在线 `delegation` Use 在原委派事务消费 once 并预留真实有限预算；离线 Lease 的三种目标没有因此增加委派能力。`material_purposes` 最多32项且逐项显式声明，旧零用途或16项 profile 的值、摘要和许可不变；资料的原 policy 仍须单独允许每种实际用途。

远端累计费用只接受原 child 签名 State／AllocationClosure，绑定原数据库、creation key、父 Delegation／Allocation、child Task 和原 Use。接收时在本方短事务保存准确原事实与本方连续投影，并结算原 Grant；迟到费用不被父终态抹掉，重复同一账单不再次扣费，once 不退款。普通费用证明必须由原出版 Job 真正保存准确字节，再经当前 Memory 门禁读取；只存在 ProofRef 不代表正文已出版。

现有有限证据包括真实双 owner／独立数据库、TLS、ES256、原 SDK fsync journal、Memory／对象介质和 Task。原创建丢回执、关闭先到、暂停／取消、输入、显式 once 预留、签名迟到费用及 prepared Attempt 重开后的父暂停拒绝分别有公开正反例。两库 Knowledge fixture 已核原选定能力／控制与 Grant 交集；这些证据仍保留各自源码、二进制和夹具范围，不证明公司身份平台、供应商最终账单、任意自然语言或生产规模。

工单17仍未整体通过。`remote-agent-17-completion-parent-gate-r5` 的 SQLite 双 owner 完整实跑已取得 child 和 parent 各自三项原 Operation、两项独立检查及已出版 Result；父方成功使用自己的条件和目标真值。该轮整体退出1：原 Incoming 仍 open，没有最终 Closure，父 Task 和 Grant 各自保留 USD2 预留，未到双方费用关闭／原完整结果重开断言。此失败及只读账务索引保存在 `/workspace/harness-dev-environment/remote-agent-17-completion-parent-gate-r5/`。正常成功后的原费用工作、完整 PostgreSQL parent／SQLite child 轨迹，以及可复用远程 Session／Transfer 的完整交接继续完成；不得把局部通过或可编译依赖 checkpoint 写成完整 profile 已受支持。
