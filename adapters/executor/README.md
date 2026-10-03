# 独立设备 Executor

此 adapter 把 Execution、有限 GrantLease、本机 Content 缓存和补传装配到独立 SQLite。设备不注册 Task/Brain/云端 Memory participant，不连接云端 PostgreSQL，也不决定云端 Task 成功。受信静态设备、P-256 密钥和 TLS peer 配对必须在管理配置中给出；缺失时在接纳业务前拒绝。

云端先在原 Task 准入事务中调用原 `AllocateLeaseTx`，固定一次预留。`SealAdmissionTx` 只签名和保存原准入：它同时绑定原 Orchestrator OperationIntent 摘要、Executor ExecutionIntent 摘要、原命令 ID、Capability/Binding/InstallLock、设备 owner/instance、原 GrantLease 和每个准确 Content 的用途及来源。它不再调用普通 Grant Use。传输凭据仅赋予 `executor_peer`；设备消费签名的原主体与代次，不复制用户或父服务的完整 token。

`executor.admission.install` 安装闭合签名 bundle。`executor.content.stage` 以原命令身份暂存最多 96 KiB 的原字节块；Job 在事务外核全量 hash/size 并完成 fsync，不提供任意内容签名入口。云端通过 `executor.admission.get` 确认原输入全部就绪，随后才签发原五秒 ControlWindow。`executor.control.install` 安装该准确 JWS；设备保留原 source/audience，不把本机 owner 冒充签发者。原 `execution.invoke` 原样进入设备 Dispatcher，其 principal 来自准确 bundle。

实际 StartBarrier 在设备事务内只核静态已登记签名、原 Task/Operation/lease/主体、最紧截止、本机已知撤权、Control/TaskGate 和资源 epoch。离线仅能运行已经完整缓存并接纳的原行动；五秒控制窗口、原 TTL 和有限分配均不刷新。过期后可以查询原 Attempt、迟到效果与用量；查询不再执行物理动作。

输出、证据和 UsageProof 保留设备的原 ContentRef 与准确来源。云端按原设备 owner 查询和补传，Task 与 Grant 只归并自己的账本。缺失来源正文必须返回明确缺口，不能生成同名替代 Content 或把 GoalRef 当作计费证明。真实双进程及故障验收结果将在工单 16 的完成依据中记录；本文的实现结构不代表外部真机、三 AZ 或生产资格。

普通输出使用原 `content.register_copy/get/release_copy` 合同。消费方先冻结本方 `reference_intent`、原 copy/command 身份与 holder/代次，再取得 `executor.content.current` 的原设备签名；`SourceClient` 实现 Memory 的 `ForeignContentPort`。该证明固定原 ContentRef、PolicyRef/完整值、来源边、source database、copy/holder/intent、用途、地点、当前控制及有限期限。`mode=control` 只保留原副本停止/清理事实，不能作为新读取许可；只读镜像失联也不能打开新使用。`VerifyTx` 只验固定登记公钥和准确有限证明，不出站；Memory 继续核自己的 held_copy_gate 和当前主体。

`ContentPermission.SourcePolicy/SubjectRefs` 是原云端 Authority 对准确输入来源的当前策略和主体代次快照，由原 AdmissionBundle 整体签名。`SourcePolicySnapshot` 必须来自原 Memory 当前读取用例；不能由适配器猜测其他主体代次。输出许可取全部 processed/disclosed 来源的主体、用途、地点及保留期交集，再受显式 `OutputSubjectRefs/OutputPurposes/OutputLocations` 配置收窄。省略配置只选择原 Authority 主体、该结果原用途和 cloud。旧 bundle 缺策略快照仍能恢复原缓存、Attempt 和账务；它不能新登记普通外部副本。每个输入至多 16 MiB，原 bundle 输入总量至多 32 MiB。

不可变字节缓存可以被另一条已获准 Operation 复用，普通输入读取必须按本次 `execution.run/reconcile` Job 的原 Operation 定位唯一 AdmissionBundle。每次读取核原签名、主体、准确 ContentRef/来源、该 bundle 的用途、保留期及本机已知撤权，再读取并核准确字节。ContextFactory 只投影当前入口路由，不读库或授予用途；不选择缓存首次 bundle，也不汇总其他操作的许可。旧缓存的策略及身份保持原样。已知拒绝可发生在准备 Attempt 之前；`not_started` 依据是未越过实际入口，不要求制造一条 Attempt。

原 `execution.usage.get` 只可恢复其准确原 intent 的账务依据，不读参数或正文、不消费新 Use。原 Attempt 核对自己的已出版 `execution_result` 时沿准确结果引用恢复；结果不必列在输入 bundle。真实 TLS、SQLite 设备和 SQLite/PG Authority 用例覆盖先写入再以另一原签名许可读同字节、当前用途未授、主体撤权、原结果核对与回执重放；Authority 夹具预置了有限批准，不替代完整公开 Task 的授权验收。

`executor.content.get` 的普通正文必须附准确已登记 `ForeignReference`，每个块重新核源 gate，最后独立核全量 hash/size。没有副本登记的查询仅允许原执行账务证据的窄用途。设备源 owner 的 `ClosePublishedContent` 管理端口先原子保存关闭与有界 holder 影响 Job；有限传输 peer 无此管理权。数据过期、停止及物理清理分别记账：实际 cleanup 未有源端可核的准确证据时报告 pending/unknown/residual，不能标 complete。收尾第一次实际报告先由 SDK journal 保存有限十分钟命令，重放保留原 TTL，与正文 retention 独立。

主体撤权的 `Revocation.ObjectRef.Revision` 明确表示实际被撤销的 credential generation 上界。旧代和旧签名准入继续拒绝；更高代必须重新取得完整 Authority 签名准入、原 lease 和配置上限。Grant/Task/lease 撤回继续封原责任。当前同主体较新代只可收尾原 holder，其签名仍固定原 holder 代次；它不能用旧副本许可取得正文。

`executor.lease.usage.get` 先取得同一本机 Execution 的实际累计用量，按原 use/source/revision 归并本机 lease，再发布独立 LeaseUsageProof 和原闭合引用。报告签名包含原设备 database_id、endpoint/instance、Cloud LeaseRef 和完整 UsageSnapshot 摘要。云端只调用原 `ApplyLeaseReportTx` 归并这一分配；同一个原 Cloud lease use 不再调用普通 `ApplySettlementTx`。Task 预算仍单独消费原 Operation Usage，不能把 lease 的源身份替代 Operation。

[独立进程入口](../../cmd/executor/README.md) 与 `Dial/Client` 使用真实 TLS、固定设备 owner/instance/database、有限 peer 文件凭据及 GoSDK fsync journal。已验证实际 CLI 子进程 SIGTERM 退出、丢回复后原回执恢复，以及 PostgreSQL Authority 的原一次 USD 1 预留与 SQLite 设备零费用闭合补传、不重复扣费。该 PG 用例预置了受信批准的 Grant 和准入 Task 引用；设备来源登记另有真实 TLS/两个 SQLite owner 的正反例，完整公开 Task/Brain 与云端 Memory 的来源门禁由工单 16／23 后续共同验证，尚未据此宣称完成。

升级后的 `Dial` 显式安装 [固定历史合同](legacy/README.md)。GoSDK `RetainDecoder` 只按原 journal 的 method/schema digest 选择受信合同，仍核当前 owner、profile、核心 Schema 和完整身份／数据库 scope。原命令先查询准确回执；只有来源明确返回 not_found 才发送原封套，不替换 payload、ID 或 TTL。旧输入和回执都使用原合同验证；未知摘要、错误 scope 或异参在出站前拒绝。历史合同只保留原责任，新命令仍按当前 Schema 与准入门禁处理。
