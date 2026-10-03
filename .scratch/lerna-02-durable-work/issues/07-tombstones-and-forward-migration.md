# 07: 正文清理、去重墓碑与真实向前迁移

**What to build:** 正文清理不会允许旧身份重复执行，真实 v1 数据升级后仍能查询原决定并完成原工作。

**Blocked by:** 04 — 双适配器工作接替与真实 v2

**Status:** resolved

- [x] 正文清理与最小去重保留分别建模；保留 tenant／owner／command_id、必要主体／目标、准确摘要、原期限和固定决定，不新增无用途敏感正文表。
- [x] 清理后公开查询可返回原引用 gone，原命令重传仍返回原固定决定，异摘要同键仍稳定冲突；gone、TTL 或完成不能授权重建同义责任。
- [x] 未结 Job 及必要处理事实不可随清理丢失；本片不实现墓碑回收，不因租约过期或随机 ID 省略原身份。
- [x] 使用票据 01／02 实际 v1 writer 生成的输入以及 03／04 真正 v2 功能迁移，在两真实数据库运行旧→新升级、重开、原引用查询与继续处理。
- [x] 准确 writer 版本、迁移 checksum、命令、输入来源及清理范围可重演；迁移失败路径可恢复，不手造旧表、不空迁移、不删库重建。
- [x] 清理与重传／查询并发有正常对照，固定决定不可改写，跨租户／owner 隔离继续生效。


## Comments

2026-10-03，票07完成。独立 branch `codex/durable-work-ticket-07`、worktree `/tmp/lerna-worktrees/durable-work-07`；启动基线 clean `b1674b2d0252da73f3dfd753857484597dc0a080`，准确产品受测提交 `4a0871ab13e65aa8d603539a7229db4f9ea4f604`。完成前将最新 `codex/lerna-implementation`（仍为该基线）合入自身 branch，Git确认Already up to date。只提交自身分支；未push、未merge root、未创建PR、未清理worktree。切片02仍in-progress；05门禁/调度与08完整进程恢复等后票未由本票代替。07仍只依04，没有更改依赖图。

**实际实现与范围。** `internal/durableworkdemo` 消费方新增准确原CommandRef+expected input revision的Cleanup与单独Cleanup允许位；Host仅装配，PG/SQLite实现消费方RetentionRepository。受信完整SubjectBinding/delegation_chain与OwnerRef先鉴权，read/record不能自动删除。只接受内部record成功applied，核对方法/版本/profile、原receipt目标/修订及metadata目标；command→input→原project Job同锁序。当前input必须仍等于原applied revision，Job为done、work/completed=current revision、没有Claim，成功projection须绑定同一revision。ready/leased、已过租约但责任未结、旧完成后新work等均不能因此清理；不把将来05失败关闭当成功project。

清理在同一短Tx将真实text_value写成零长度bytea/BLOB，并将输入body_gone与这个准确原command自己的body_gone一起持久保存。新增真实DB约束gone⇒stored byte length零，实际adapter读取字节后计算StoredTextBytes并校验一致性；合法空字符串仍present。没有新增原Command正文表、历史正文表、TTL扫描或墓碑回收。原tenant/owner/id、digest、metadata、固定receipt、input revision、原Job identity/revisions与真实projection长期保留；正文清理不增revision、不造新工作、不改原决定。

公开command.get一致读取原receipt与独立marker，返回准确原引用gone。LockCommand/Record仍保留原固定决定与摘要比较；原raw重传（包括原接纳期限已过去）返回同一receipt，不重建正文/Job；同键改正文、期限、expected_revision或有资格的另一主体仍idempotency_conflict。新command+准确当前expected_revision恢复下一修订正文并触发原Job；旧command永久gone，oldCleanup得到already_gone且不碰新正文。未经清理的旧command发现revision变化则明确revision_changed。本票不会顺带清理其他历史命令。

**真实两库行为及故障。** 独立`retention_test.go`提供14个顶层两库案例及共同行为函数：非空成功投影→实际清理/零字节/gone/原receipt；合法空输入与gone区分；pending、有效Claim、过期未交回Claim拒绝清理并接替正常完成；权限、完整委托链、其他主体、tenant/owner隔离；新revision、旧命令重传/冲突与旧cleanup幂等；关闭/重开后的墓碑、原Job与新正文/投影；真实存储seam拒绝gone+nonempty；真实pre-COMMIT取消故障后正文与command状态一起回滚，正常重试清理。

清理/newrevision分别控制clean-first与record-first；清理/完成分别控制clean-first与complete-first，并加入claim-first的未结拒绝及正常完成对照；清理/query/replay分别控制clean-first、query-first、replay-first。实际TxRunner的有限channel gate只注入同步/取消，业务SQL/原键/对象/Job资格全部来自真实适配器；query使用真实CommandFactReader及公开GetCommand，允许线性化found/gone而不得not_found/半份/错误原引用，replay无论顺序都保持固定决定且不增加工作。PG独立事务与SQLite实际单写队列均被同步参与；没有任意长sleep、私有业务表查询或内部调用次数真值。业务正常投影使用同owner可信Clock；期限/租约故障注入有明确允许完成对照。

**真实历史来源与升级。** 独立`migration_test.go`在两库各跑normal及version2-record拒绝/移除故障/重试两条路径。写前核验全部5项SHA256SUMS：PG真实writer `988f8b7ec2a8fd3a28db44b11cf5a863af4593b2`，完整database.sql SHA256 `1d2195606544b288e14a0e2819844ee187b07e097fe7e96f83513013f1ad8b60`；SQLite真实writer `f4fb0576bc0a1e3371fb88ab43f729beb9ddf118`，closed database.sqlite SHA256 `4d8aafac077e2fab896358e35bc18c19a9c7150b533e3505eb33bf3a644b1900`。SQLite只复制后打开本轮自登记overlay临时文件，原fixture从不原地writer打开。

PG先成功CREATE安全随机schema并登记owns，保留创建Store仅用于独立有限清理；临时dump仅移除唯一原CREATE并替换固定schema，保留全部COPY、restrict/unrestrict及原数据。真正`psql -X -v ON_ERROR_STOP=1 --single-transaction --file <owned copy>`恢复，连接字段只在子进程env，不进argv/日志。原dump生成工具pg_dump18.6与恢复client分别记录：本地实际psql17.11→server18.6正式两路径成功；CI-only`scripts/ci-psql.sh`使用与服务相同锁定image的psql18.6，挂载明确TMPDIR同绝对路径，实际本地完整两路径也通过36.976s，包含COPY 2/1/1/1与原回执/Job继续处理。Make入口与直接测试缺工具硬失败并记录实际version；CI新增真实可执行client步骤，远端执行仍由root核验。没有手写SQL/COPY解析器或新增个人绝对路径依赖。

真实v1恢复后先public查询原applied/expired两决定，然后应用实际03/04 v2与本票0003_retention。升级/重开后原raw三份语料重传仍相同，原input与原pending Job保持identity/work1/completed0。实际worker领取原Job并投影原25字节输入为独立已知`sha256:eeebf3ebdb81d9e669ca989199225a42df8bfe152a9e247c1b5981052f615bbc`，正常完成→清理→再次关闭重开→原command gone/expired receipt保留/重传不恢复body或新工作。没有新库倒填旧行、空迁移、删库重建或更换Job ID。

真实失败由各测试scope的schema_migrations BEFORE INSERT trigger只拒绝version2：PG schema-local函数/trigger明确P0001，SQLite RAISE(ABORT)/ErrConstraintTrigger。真正v2 DDL执行后版本保存失败，同一Migrate Tx回滚；MigrationVersions仍只有原v1且public原receipt正常可读（ReadCommand支持retention列尚未存在）。移除准确故障trigger/function后同库retry成功，并继续上述原工作；未改产品SQL/driver、不加产品testhook，DDL没有用IF NOT EXISTS掩盖部分提交。

不可变迁移身份保留：PG v1 `sha256:f8d04d373b039a425b4f6d0a7b7dd4410971c00faf91cdba3f68a9204579127e`、v2 `sha256:cdb7dea9f55ee8ac9201a943cecf8096108b48bc208409e1372bbf17295b2297`；SQLite v1 `sha256:324dd9c72a00438095596b59c80bf21e66a02eb53d7182ddba67e4784e2c0203`、v2 `sha256:3791b3fc5ca49c18eee04b2afcaa54c2aae9c5afbf3f1e3fd98f5cce8715e01c`。本票当前未发布0003_retention：PG `sha256:ec7e5d35deb0c19a1fccc5cd965fba8367b02ab3df3b46d97da8782a0b5f7249`，SQLite `sha256:81d6f0397b65e53dc00a906dcef139f621bda0f22b656a9930302830bf4a1b45`。已有metadata测试仅扩展真实新版本，同时保留原v1/v2独立checksum与业务断言；不改已发布migration或artifact。将来整合若编号被占，只能调整尚未发布的本票版本，不能改已发布其他票迁移。

**TDD与实际失败记录。** 实读implement-spec及tdd/SKILL.md、tests.md、mocking.md，沿用已经批准的Host、内部Tx/storage、public GetCommand seams，无重复人类确认。首个非空清理tracer实际编译red：缺Cleanup能力/API、RetentionRepository和BodyGone/StoredTextBytes；最小真实两库产品后green。其余语义在此机制上逐故事回归，直接通过的不伪造red。新revision重开测试真实失败在第二次reopen缺辅助函数注册，修正本票重开装配后green。首次PG完整恢复真实client exit2：将URI放PGDATABASE未能连接；改为解析URI到受控PGHOST/PORT/USER/PASSWORD/DATABASE/TLS env后两路径green，失败轮仅清理确实创建登记的scope。只读辅助代理保存/tmp恢复设计草稿，没有写共享repo或执行产品测试；正式实现、采纳、测试和提交由本票implementer完成。

**工具、配置、准确命令与结果。** Go1.27.1、Node24.19.0、pnpm12.8.1、pgx/v5 v5.11.0、go-sqlite3 v1.14.52、GCC14.2.0、driver bundled SQLite3.53.4；PG18.6 READ COMMITTED/synchronous_commit on/statement2s/lock1s，SQLite WAL/FULL(2)/foreign_keysON(1)/busy_timeout100ms，每个真实连接由suite核验。Tx3s、suite120s、测试/restore同步15s、PG清理5s。专用mode600 DSN只由启动程序读取到显式测试env；不打印/提交、不读services.env或smoke。SQLite各轮TMPDIR为/workspace自登记overlay目录，最后正常移除该临时范围；PG只DROP自身成功创建登记schema，不DROP database，不碰04那个无法重新确认归属的失败轮scope。

实际通过`make bootstrap`、`make fmt`、`make check`（vet、Go/TS及双向158共同fixture、生成一致性、build）、`make test-race`、`go mod verify`、`bash -n scripts/ci-psql.sh`与`git diff --check`；mandatory双库`LERNA_TEST_POSTGRES_DSN=<专用URI> make test-integration`内含-count=1和两v1 manifest校验，初完整34.044s、最终产品同源15.126s。实际`go test -race -count=1 -tags=integration -timeout=120s ./conformance/recovery/... ./internal/durableworkdemo/...`最终29.440s/1.323s green（前轮47.827s/1.710s）。隔离选择清理/真实竞争与历史升级先行通过；真实psql18.6容器恢复对照通过36.976s。`env -u LERNA_TEST_POSTGRES_DSN make test-integration`预期硬失败；正确package cwd运行编译集成test binary且PATH无psql时，历史恢复明确硬失败而非skip。

限制：以上证明当前真实双库live记录、事务型DDL/版本写入回滚/重试、并发与原工作恢复；不声称WAL/MVCC/备份/复制/内存/原请求取证级擦除、墓碑回收、SIGKILL/断电、生产故障域耐久、外部效果或远端CI。清理仅当前准确成功project修订，非全历史归档。当前04基线Worker正常完成seam与05后续授权/Start/Finish严格门禁需要root整合时按其实际API保持同一成功前置，不能以本票验收替代05处理权限/期限；不因此扩大07依赖或宣称05完成。
