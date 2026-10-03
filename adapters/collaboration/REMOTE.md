# 跨所有者 Agent 参考适配器

`NewRemote(RemoteConfig)` 在构造时核固定 owner、数据库、签名密钥、有限 profile 与静态 peer，不访问网络。宿主显式 `BindTask`、`Register`，同时登记对方 Memory 的 `ForeignSourceContracts`；没有对端配置时，不创建可冒充远程委派的同库子 Task。

`RemoteAgentContracts()` 返回同版闭合方法合同。父方 `collaboration.delegate` 在原命令事务保存 Delegation、Allocation 与首个 Job；子方 `collaboration.create` 保存原 creation key，工作者在线取得签名的父 scope、注册原 Content 副本，再以自己的事务接纳 allocation 和唯一子 Task。丢回执沿原 SDK journal 查询，不更换子 ID、命令或期限。

`collaboration.input.send` 固定父、子目标修订和原输入，`collaboration.input` 是接收方原命令。实际回答和 Steer 分别由原 Task 的 Input/Steer Job 决定该命令，输入不取得新增工具许可。回答保持原 request、Content owner/hash/version；派生 GoalDocument 保留真实来源。接收回执给出原 receiver command ref，便于查询准确消费结果。

暂停是父方有效控制与子自身控制的交集；父恢复不解除子自身暂停。取消和 `collaboration.allocation.close` 独立登记原命令。消费门禁可以先于创建或正文消费关闭，关闭后的迟到原回答／GoalDocument 出版不能改变目标。未知费用继续占用原 allocation，终态之后只归并可信原账单的累计差额。

宿主为每次入口调用 `NewParentScopeContext`，该工厂只建立有界内存。`PrepareChildContext` 和 `PrepareInputContext` 必须在事务外取得当前有限父证明及真实来源证明；后续 `CheckTaskCurrentTx` 在原 Task／预算锁之后纯核验。磁盘保存的旧肯定证明不能授权新使用。Memory 的当前证明通过同一次入口的 `ForeignUseProvider` 交接；网络不能放进 Runtime 工厂或数据库事务。

本切片的验收使用两个独立 SQLite 数据库、两个固定 owner、真实 TLS 请求、ES256、原 SDK fsync journal、真实 Memory／对象介质与 Task。身份／规则夹具只预批准有限主体及规则边界；它不证明公司的身份平台、任意自然语言质量或生产规模。实际覆盖原创建丢回执、唯一子身份、先关闭后创建、父暂停／子暂停、取消、迟到账单、原回答与 Steer、读回／出版在途时先关闭。

当前适配器切片尚不构成完整公开参考部署：可复用远程 Session、既有 ChildHandle 的 Transfer 投递、行动权限上限与父独立最终 Result、PostgreSQL 双库矩阵及 Development 的真实配置／重开入口继续在工单 17 完成。不能由本目录或局部测试通过宣称这些路径已开放。
