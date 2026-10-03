# 02: SQLite 同版接纳与单写耐久

**What to build:** 设备 Host 开发者在独立 SQLite 文件得到同等原子接纳、固定回执与关闭重开的保证。

**Blocked by:** 01 — PG 原命令原子接纳与恢复查询

**Status:** resolved

- [x] 使用与 PG 相同的 Host 接纳行为套件，证明正常接纳、原键重传、异摘要冲突、过期新／已有命令及固定回执，不以内存 SQLite 替代文件。
- [x] 每个相关连接显式配置并核验 WAL 与 FULL，记录驱动实际运行时 SQLite 版本；不把 CLI 或历史 smoke 设置当产品证据。
- [x] 同 owner 写入由明确单写协调器串行处理；跨进程第二写宿主受排除／协调，不能只用连接数或进程内队列声称多进程单写。
- [x] 队列仅协调写锁，业务事实、固定回执和 Job 仍同事务持久化；回滚、busy／取消与关闭重开均有正常对照及有限 context。
- [x] 公开 command.get 保留准确 owner 与原引用；隔离两租户／owner、错误事务 token 或跨库参与，不能隐式跨 owner 共提交。
- [x] 首次 SQLite 迁移 checksum 与版本记录真实建立；与票据 01 相同，保留 v1 writer 实际产生的待处理工作、固定拒绝和重传事实及准确来源。领取新增字段留给票据 04 的真实 v2 功能迁移，不手造旧表布局或空升级。
- [x] 使用独立临时文件与明确清理，集成入口在依赖缺失时失败；锁定驱动构建方式且无需个人 SQLite 头文件路径。

## Comments

2026-10-03，票02实现与真实检查完成；SQLite v1 writer 冻结于 `f4fb0576bc0a1e3371fb88ab43f729beb9ddf118`，先提交代码再从历史 Git archive 构建并执行 writer。切片02保持 in-progress，SQLite Claim、调度、进程 SIGKILL 等后票未提前实现。

**范围与同版套件。** SQLite 实现 runtime Tx/Clock/CommandStore/JobStore.Trigger 与根 internal/durableworkdemo 消费方 Repository；Host 仅装配，SQL/驱动在 adapter，业务前态仍由原消费者裁决。1.0.0 Schema、方法清单与 digest 黄金未改。原 PG 13 项接纳行为提取为同一适配器无关套件，PG 与 SQLite 都实际运行该函数集：原子 applied+输入+待处理 Job、原键重传/异含义冲突、零 Unicode 标量、固定前态与期限拒绝、准确主体/租户/owner 隔离、元数据、回滚、关闭重开与并发唯一责任。适配器专属配置/锁/故障保留独立测试，没有复制第二组镜像断言。业务事实只经 Host Observe 与公开 GetCommand 验证；Tx scope、配置和迁移版本使用已授权内部端口。事实/回执/Job 在相同 SQLite 短事务持久化，队列只协调锁。

**TDD。** 实际读取 .agents/skills/tdd/SKILL.md、tests.md、mocking.md，沿用此前批准 seams，无重复请求确认。真实 red→green：首个同版接纳因 SQLite package 缺失失败，再加入最小文件适配器；连接配置观察先缺 Settings；独立进程排除先缺 ErrWriterActive；迁移身份观察先缺 MigrationStatus；最重要的 Close 生命周期测试实际先失败“Close released ownership while original Tx was active”，随后修正为取消并等待整个 Within 退出后才释放跨进程锁。既有 13 项套件随后作为两实现共同回归执行；没有为制造 red 改坏已通过代码。其余真实故障验证已有有界配置与正常对照。

**单写与生命周期。** Linux 内核对数据库 inode 的非阻塞 flock 保持至写 Host 整个生命周期；跨进程第二 Host 实际返回 ErrWriterActive，关闭原 Host 后独立新进程正常接纳，再重开 Host 读原引用/原回执/同 Job ID。不是仅依赖连接数/进程队列，不用持久租约或陈旧锁文件作唯一责任。进程内 context-aware 单写协调器使用 BEGIN IMMEDIATE；截止覆盖排队与事务。Close 先取消活跃/排队 context，再有界等待完整回调/事务退出后关闭连接并释放 flock。真实同步 channel 测试证明回调正在取消收尾时新 Host 仍被排除；故意违反 context 的回调使 Close 在 100ms 内返回 ErrCloseTimeout 并保留锁，回调退出后再次 Close 成功、替换 Host 正常。未建立无界清理 goroutine。

**正常与故障结果。** 真实独立 raw-SQL 进程持有 SQLite 写锁作为 busy 故障注入，Host 在 busy_timeout 内返回 dependency_unavailable，实际驱动原因是 SQLITE_BUSY；释放锁后原键 not_found、输入/Job 不可见，正常同键重试成功。已取消请求、等待协调器至截止与写入三类事实后主动取消均不产生部分决定；正常对照继续提交。foreign Store/owner、过期 Tx 和 nil token 被拒绝。临时文件关闭重开、独立进程写入后的重开及历史 v1 file 恢复均保存原回执/Job。迁移真实记录 version1/checksum，幂等重跑保持，故意损坏 checksum 被拒绝。故障原始 SQL 仅建立锁或注入损坏，不作为业务真值读取。

**版本、耐久与时间。** 锁定 go-sqlite3 v1.14.52/go.sum，Go 1.27.1，Linux amd64，Debian GCC 14.2.0-19，CGO 驱动内置 SQLite 实际版本 3.53.4（3053004），source ID `2026-07-24 19:02:57 bf7c7f30031888f4e796e429ab3978879485813aaca6f641c7b33e4e09459bcc`。无需个人 SQLite 头文件或 libsqlite3 tag，不把 CLI3.46.1 当产品版本。每个产品连接经 DSN 配置 WAL/FULL、foreign_keys 与有限 busy_timeout；每次事务开始实际核验 WAL、synchronous2、foreign_keys1、busy_timeout100ms。连续关闭/新开三个真实连接实际核验同样配置。事务最大3s，子进程5s，清理有界。持久 UTC 时点固定9位小数；整秒及100ms+1ns经真实 Host 往返保持准确，未来04的 SQL 时间比较必须使用一致绑定格式，未预建 Claim/lease。设备可信时钟来自显式 Host UTC 墙钟，不声称防设备改时。

**v1 来源。** [SQLite v1 provenance](../../../conformance/fixtures/durable-work/sqlite-v1/provenance.md)、完整 database.sqlite、commands.json、0001_admission.sql、writer-observation.json、writer-revision.txt 和 SHA256SUMS 已保留。实际执行 `scripts/generate-sqlite-v1-fixture.sh f4fb057 /workspace/lerna-sqlite-v1-export-02`；实际 writer 创建 applied/待处理原 Job、expired 固定拒绝与原键重传，然后关闭唯一连接留下完整数据库文件，不手造旧行、不依赖 CLI 导出。另从相同历史源码独立重新生成，迁移/语料/代码出处一致且行为通过。已提交 artifact 的副本经当前 Host/GetCommand 实际恢复，原 Job ID、固定两类决定与修订保持。迁移 checksum `sha256:324dd9c72a00438095596b59c80bf21e66a02eb53d7182ddba67e4784e2c0203`；完整 file digest `sha256:4d8aafac077e2fab896358e35bc18c19a9c7150b533e3505eb33bf3a644b1900`。PG 已冻结0001及988f8b7来源 artifact 未改；本 SQLite v1 不含任何 Claim 列，04才真正v2。

**检查与限制。** `go mod verify`、`make fmt`、`make check`、`make test-race`、两库 `make test-integration` 与 `go test -count=1 -race -tags=integration -timeout=120s ./conformance/recovery/... ./internal/durableworkdemo/...` 已实际通过；集成及 CI race 使用 -count=1，不复用结果缓存。基础 check 不访问外部服务。缺 PG 配置、不可用 PG 服务与 CGO_ENABLED=0 的必需入口均实际硬失败。默认/tmp是tmpfs，故另通过标准 TMPDIR 注入/workspace下本轮独立登记临时目录，在本地 overlayfs 运行完整两库集成/race；历史 writer 的实际输出同样在 overlayfs。配置仅入口注入，DSN 从 protected 文件读取到子进程环境，未输出凭据；PG 每例仅清理新随机登记 schema，SQLite 仅清理本例临时文件，未碰 smoke。CI 已扩成两库必跑，准确远端新 tip 状态由 root 后续核实，不把配置当 success。写 Host 排除目前仅支持 Linux 本地稳定 regular file，hard link 明确拒绝；未验证 macOS/Windows/网络文件系统、SIGKILL、断电或生产故障域。Close 的保证要求回调响应有限 context；违反者有界失败并保留锁。这些限制见 adapter README。

**最终整合复核。** 已合并最新集成 `6ccdb6df7901f721aeea0830c994d9e598ec7408`，保留03的PG真正v2 Claim及0002、原0001和988f8b7 artifact；解决的冲突仅为能力说明/验收记录，SQLite仍v1、没有实现Claim。组合后的 `make check`、`make test-race` 通过；按CI顺序并禁用结果缓存的 `make test-integration` 通过（recovery 7.501s），随后 integration-race 通过（recovery15.058s、consumer1.356s）。两库实际接纳仍共享13项同版断言，PG含03共29项行为，SQLite本票22项行为；两个子进程helper入口不另算验收能力。

**实际并行失败记录。** 一次同时运行普通集成与integration-race时，03的 `TestPGConcurrentNewWorkAndCompletionBothCommitOrders/new-work-first` 初次Claim得到空batch、nil error，使普通套件失败；同轮race通过。当前PG advisory锁键未包含schema，同一专用DB内两套虽然数据各属随机schema，却仍使用相同owner/input锁域，Claim可合法跳过另一套所持锁；不能据此宣称工作修订丢失，也不能宣称随机schema实现了完整锁隔离。之后按真实CI顺序完整两库普通/race均通过，未放宽测试或重写03 product来掩盖；root已另委派授权决策判定是否把存储namespace纳入锁域。独立schema的清理/数据隔离事实保持，跨套件锁隔离限制明确保留。
