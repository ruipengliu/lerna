# 16 票凭证生命周期

本票已完成下述本地参考能力与验收，范围及恢复限制见本文。实施基点 a7e0164，用户已授权逐票实施决策；规范为 16 票 Agent Brief 与授权生命周期分册。

## 已实现的首个切片

KeyLifecycle 是受信宿主接口，将 Prepare、Activate、Retire 与已有 KeySource 分开表达。Linux 文件 Adapter 的准备操作以稳定身份持久关联版本，不切换当前加密密钥；重开后的同操作重试复用材料。激活验证操作与版本对应，旧已激活操作不能把活动版本倒退。材料读取仍校验文件、摘要和格式，最多保留 8 个密钥；准备身份最多保留 32 条，不自动驱逐以免旧操作复活，文件仍有 8192 字节硬上限。

Retire 是受信材料端口，拒绝删除活动密钥。当前已由 Lifecycle.RetireKey 接入现存记录/备份围栏检查，普通管理调用应经过 Lifecycle；绑定 Driver 不暴露材料端口。管理 CLI 已接入对应的持久操作入口。

Lifecycle.StartRotation 保存原操作和可信绑定；StepRotation 每次至多迁移一条记录。先保存新版本及写入围栏，再激活材料；同时只允许一个未完成轮换。普通凭证写入在同一 SQLite 事务中检查当前轮换版本。轮换记录包含准备时的活动密钥与迁移来源版本；从激活阶段起，即使轮换已完成，迟到写入也不能再使用这些旧版本。围栏作用于共享密钥供应的所有新写入，已有旧密文仍可按各自版本读取；新写入使用当前新版本。空绑定轮换也记录此前活动版本，防止尚未落盘的新引用漏过围栏。

LifecycleStore.CommitLifecycle 将密文 CAS 与进度提交放在同一事务中；失去回包后重新读取原操作，避免仅凭内存进度重复重加密。状态最多 32 个操作、每操作 64 个记录引用，文档上限 524288 字节；操作超时沿 Broker 配置，测试为 1 秒。生命周期状态与密文同库，密钥和 nonce 水位保持独立。

## 备份与受控退役切片

Lifecycle.CreateBackup 先持久化原操作及获准绑定内的密文快照，再向 BackupArchive 写入不可变材料。回包丢失或重启仍重放原快照；即便当前凭证已更新，也不替换原备份内容。可用后库内只保留摘要、版本集合、归档引用与状态，不重复保留整份快照。Pending 快照仍只有密文，没有主密钥或凭证明文。

Linux credentialbackups Adapter 使用独立 0700 目录、0600 文件、稳定文件锁、文件和目录 fsync，以及原子 rename。单快照最多 1 MiB，目录最多 64 个条目，操作最多 2 秒；生命周期的更严格调用期限仍生效。归档身份绑定配置标签与实际目录的设备/inode，重开保持，复制到另一目录则改变；不同归档不能冒充原引用，迁移需明确恢复核对。它不证明整台宿主/文件系统快照克隆的新鲜性。

DisposeBackup 先持久化 disposing，实际删除完成后才记 disposed。Archive 删除持久化与摘要绑定的墓碑，再移除快照并同步目录；迟到 Put 不能重新创建同一快照，删除中断可继续。未处置的允许备份均保留密钥引用。

RetireKey 只处理本绑定已完成轮换中记录的旧版本，任何未完成轮换先阻止清理；活跃备份引用导致拒绝。SQLite 在同一事务中检查全部绑定的现存密文并持久化退役围栏；其他账号的记录也会阻止清理。普通写入与轮换原子提交都不能再使用被围栏禁止的密钥，围栏不能在后续普通生命周期提交中移除。材料删除最后执行，未知回包通过 pending 状态继续，完成后重复请求不产生新工作。

生命周期最多保留 32 个备份、一个待归档快照和 32 个退役记录，总状态文档仍受 524288 字节限制，达到限制明确拒绝，不通过驱逐旧身份释放重放空间。密钥版本在核心仍是受限不透明标识，没有依赖文件 Adapter 的 SHA-256 版本命名实现。

## 第三方续期切片

RenewalProvider 是受信固定出口。StartRenewal 保存操作身份、提供方契约身份、完整绑定与原凭证密文；StepRenewal 在每次发送前持久计数和 checking 状态。传输成功也不直接认定续期成功，下一步通过 Inspect 核对原提供方操作。只有确认未发生且提供方明确保证相同操作身份不会重复产生效果时才补发，最多初次加两次补发。查询最多八次；持续未知、不能安全补发或三次发送后仍只得到暂时未找到时进入 needs_reconciliation，保留原凭证密文并阻止同引用用新操作身份绕过。暂时未找到不构成提供方终局围栏，尚可能存在已经预留但未送达的请求，不能据此释放原引用或声称失败终结。

更新后的秘密仅在受信观察接口进入 Broker 加密路径，密文与 completed 状态原子提交，普通结果只有状态与引用。若期间仅发生重加密，当前秘密与期限仍与原记录一致，则基于当前修订 CAS 保存续期结果；如果凭证语义已经被其他管理操作改变，转入待核对。尚未解决的续期持有必要旧密钥引用，阻止提前退役。

参考 HTTP 提供方按固定服务、账号、Driver、用途请求，路径来自受信配置。配置标签、完整绑定、两个规范化固定路由与重放契约共同形成提供方身份摘要，改路由不能悄悄继续旧操作。管理与使用权限每次复核；HTTP 请求不继承调用方 trace context。参考通道禁用代理、重定向、连接复用及 HTTP/2，使用一次有界 HTTP/1 请求，避免传输层自行补发绕过操作预算。

参考续期接口使用 POST + Idempotency-Key，只有固定状态结果；核对接口使用 GET + 相同 Idempotency-Key，返回严格 JSON 的 state、Base64 secret 和 expires_unix。state 为 applied/not_occurred/unknown；正文最多 16384 字节，拒绝重复或未知字段，秘密最多 4096 字节。具体提供方若没有可核对原操作或安全重放的契约，必须明确返回未知/不支持安全补发，不能用该模拟协议假定所有 OAuth 服务具备这些行为。

当前未知预算耗尽后保留 needs_reconciliation，不能继续自动查询或发送；没有交付任意外部提供方的人工核对流程或适配器。该状态不是成功，也不能通过新操作解锁原未知结果。

## 管理入口

原先仅用 key-dir 直接激活的 rotate-key 命令已改为受当前授权约束的持久轮换操作；init-key 仍负责独立材料的首次初始化。每个 step 都是有界工作，调用者读取结果 Phase 后决定是否推进，命令自身没有无限循环。

```sh
credentialctl rotate-key -config /private/config.json -operation rotation-1
credentialctl rotation-step -config /private/config.json -operation rotation-1
credentialctl backup -config /private/config.json -operation backup-1
credentialctl dispose-backup -config /private/config.json -operation backup-1
credentialctl retire-key -config /private/config.json -key-version OLD_VERSION
credentialctl renew -config /private/config.json -operation renewal-1 -ref account-api
credentialctl renewal-step -config /private/config.json -operation renewal-1
```

基础配置沿 15 票。备份增加 BackupDirectory 和 ArchiveID，备份目录不能处在密钥目录内；CLI 可建立 0700 目录。续期增加 Renewal 对象，其字段为 ProviderID、RenewPath、InspectPath、AllowLoopbackHTTP 和 ReplaySafe；完整绑定继续来自基础 Binding。AllowLoopbackHTTP 只允许明确模拟使用的字面回环地址。ReplaySafe 是受信提供方契约声明，不能由模型或普通调用载荷选择。续期需要 credential.manage 和 credential.use 的现有 policy + Grant。输出不含秘密正文，密钥版本取自当前元数据回执。

## 当前验证

测试接口已在 Agent Brief 固定。Prepare、Activate 与 Lifecycle 起初不存在，相应测试编译失败后实现通过。当前定向命令：

```sh
go test -race ./credentials ./adapters/credentialhttp ./adapters/credentialbackups ./adapters/filekeys ./adapters/sqlitecredentials ./profiles/credentialcheck ./cmd/credentialctl
go vet ./credentials ./adapters/credentialhttp ./adapters/credentialbackups ./adapters/filekeys ./adapters/sqlitecredentials ./profiles/credentialcheck ./cmd/credentialctl
```

已通过真实 SQLite 重开、原操作恢复、已提交但回包丢失、记录 CAS 失败时进度一同回滚，以及 15 票既有秘密出口/恢复回归。新增测试通过：原备份写入完成但回包丢失，当前凭证随后更新，仍按原快照恢复；备份未处置及其他绑定的现存记录分别阻止旧密钥清理；实际文件删除、迟到写入拒绝、墓碑边界故障恢复、归档目录身份隔离。墓碑测试直接构造磁盘故障边界，未冒充真实子进程退出。内核故障测试中的接口替身只用于精确边界；新增集成测试已使用真实 Harness policy + Grant、真实凭证 SQLite 和独立有状态 HTTP 提供方数据库。八种情况通过：真实连接丢失后的续期核对、持续未知、三次发送上限、不具备安全重放契约、目标错绑、权限撤销、续期期间重加密及提供方路由变化。实际验证新令牌可使用、旧令牌拒绝，查询/发送/续期效果/后续使用次数来自独立数据库。CLI 测试另覆盖受理不发送、分步完成、受控备份处置与退役及无秘密输出。

开发日志为 build/16-prepare-red.log、16-prepare-green.log、16-activate-red.log、16-activate-green.log、16-rotation-red.log、16-rotation-green.log、16-checkpoint-tests.log 和 16-checkpoint-vet.log；备份开发日志另见 build/16-backup-red.log、16-backup-green.log、16-backup-boundaries.log、16-backup-recovery.log 和 16-backup-vet.log；续期日志为 build/16-renewal-red.log、16-renewal-green.log、16-http-renewal-red.log、16-http-renewal-green.log、16-renewal-http-integration.log（模拟目标事务错误导致的真实失败）、16-renewal-http-integration-green.log、16-cli-red.log、16-cli-green.log、16-renewal-checkpoint.log 和 16-renewal-vet.log；完整具名 profile credential-lifecycle-v1 已接入 contractcheck 和 make verify：11 个持久化/授权/备份/并发用例、8 个独立 HTTP 提供方用例、5 个实际子进程退出用例，共 24 项。包测试与最终完整运行通过，证据已归档。

## 最终验收

初审发现的完成后旧版本写入和迟到续期引用释放问题已有真实并发反例，先失败后修复通过。HTTP 请求构造已统一，取消、截止时间与认证绑定头遵守共同边界。五个真实子进程分别在准备后、激活前/后、记录事务前/后退出；重开后验证进度与密文一致、两代材料读取、新旧 nonce 约束及独立目标效果。

2026-09-11，完整 `make verify` 退出 0。候选 `89294e69b1dc85eabec0e9783733fcb8920fa661`，全部具名报告记录 dirty=false；环境 linux/amd64、Go 1.26.1、protoc 36.1、生成器 v1.36.11，依赖精确版本及摘要见报告。累计 1452 项必需检查通过，含五项可选检查共 1457 项；本票 profile 为 24 项。

依赖验证、生成一致性、全包编译、go vet、完整 `go test -mod=readonly -p 1 -race ./...`、SDK 样例和全部具名回归通过。独立 Standards 与 Spec 复审均无剩余发现。并发与进程包测试不重复计入 profile 数量。

[本票报告](evidence/16-credential-lifecycle-report.json)、[验证阶段](evidence/16-verification-stages.json)、[完整日志](evidence/16-make-verify.log)、[并发修复前](evidence/16-concurrency-red.log)、[并发修复后](evidence/16-concurrency-green.log)、[真实进程测试](evidence/16-process-tests.log)、[候选测试](evidence/16-candidate-tests.log)、[双轴审查](16-credential-lifecycle-review.md)、[证据摘要](evidence/16-evidence-sha256.json)。同目录的 16-*-regression-report.json 保存其余具名回归；开发阶段真实失败日志一并保留，未改记为通过。

数据库旧快照及身份克隆仍需受信隔离恢复；AEAD 不证明整条旧记录新鲜，密钥目录不能随数据库回滚。rollback-quarantine 用例实际撤销独立权限源的使用权限、恢复旧凭证库、在拒绝新调用期间重加密核对、再重新授权；它不宣称自动检测任意克隆，也不宣称这是完整通用备份恢复产品。
