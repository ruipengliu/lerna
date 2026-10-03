# 08: 提交边界杀进程与双库接替

**What to build:** 独立 Host 进程在提交前后被杀，调用方仍沿原身份恢复确定事实，不把未知提交当作未发生。

**Blocked by:** 04 — 双适配器工作接替

**Status:** resolved

- [x] 独立子进程通过有限管道／同步点确认事务提交前与提交成功答复前阶段，再 SIGKILL；不靠 sleep 猜测提交点，两库执行相同故障脚本。
- [x] 提交前终止不留下成功回执或部分业务／Job；提交后答复丢失，重开并重传原命令返回同一固定决定且业务责任唯一，正常对照可完成。
- [x] 提交答复无法判定时保留 commit_unknown 与原引用，经原 command.get 或重传恢复；区分数据库 COMMIT 确认丢失与 Host 成功答复丢失，记录实际注入边界，不把提交前确定回滚冒充未知提交。不换 owner、不延期限、不新建同义命令。
- [x] 跨进程接替沿原 Job／对象推进，旧 worker 迟到提交拒绝；新增工作和旧修订条件提交仍正确。
- [x] 父进程通过独立连接的 Host 业务观察／公开查询验证实际保存的演示事实，不新增外部副作用目标；不查私有表或以内部调用次数证明持久性，故障路径有有限清理和可重演同步阶段。
- [x] 记录代码／驱动／两库版本、配置、命令、阶段、实际结果及范围；SIGKILL 仅证明进程崩溃恢复，不替代断电、3AZ 或生产 RPO0。
- [x] 本任务仅负责故障套件；全片证据汇总、审查与退出由主任务等待所有票据完成后处理，不添加隐式最终关闭依赖。


## Comments

2026-10-03，票08在自身 `codex/durable-work-ticket-08` 工作树完成。开始时工作区 clean，准确基线 `b1674b2d0252da73f3dfd753857484597dc0a080` 已含04与09；实际故障代码 `73310e268b4fb52dbb7ea84dca3b8de933baa910`。完成前执行 `git merge --no-edit codex/lerna-implementation`，最新 root 仍为上述基线，结果 Already up to date。本票不 push、不向 root merge、不创建 PR、不清理工作树；全片02仍 in-progress，等待后票和 root 整片审查/退出。

**实现与 seam。** 实际读取根 AGENTS/CONTEXT、ADR-0003/0004、runtime 设计、本片 spec/decisions/admission/layout/process-fault-decisions、01–04/09 Comments，以及 `.agents/skills/tdd/{SKILL.md,tests.md,mocking.md}` 和 implement-spec。沿已确认的内部 Host.Record / Host Worker、真实 runtime TxRunner/ClaimStore 系统存储边界、Host.Observe 与 public `contract.GetCommand` seams。只新增 `conformance/recovery/process_test.go` 与 `process_fixture_test.go`，通过当前 Go test binary 的真实独立 OS child 装配产品 adapter/Host；没有产品测试 hook、新公开方法、Task、外部副作用目标、伪 SQL rows/driver 或手造 TransportOutcome。内部 `host-durable-work-1` 与公共1.0.0仍分别保持；scripts 的现有格式检查/Make fmt 已覆盖两个新 Go 文件。

**双库同一故障脚本与正常对照。** 两库执行同一 `runProcessAdmission`。precommit 装饰实际 TxRunner 的 callback：真实原输入、Job、固定 receipt 写入全部成功后，callback 尚未 return、adapter 尚未进入 Commit，控制 pipe 报 `writes_staged_before_commit`。父收到完整 frame 后 SIGKILL，并由 WaitStatus 确认真实 SIGKILL+Wait；单独业务答复 pipe 必须 EOF。独立新 Store/Host 的原 `command.get` 精确 not_found，同一原 raw 创建命令首次 applied/revision1，准确 hello 输入、一个原对象 project Job、work_revision1/completed_revision0/ready；再次原键重传 receipt 相同、revision与Job ID不变，真实 batch64扫描仅取得该一个 Job。此处是确定未提交，没有标成 commit_unknown，也没有把任意 Observe 错误当不存在。

postcommit 子进程只在真实 Host.Record 返回 received/applied且 adapter 已实际确认提交后发送 `commit_confirmed_before_host_reply`；正常业务 reply pipe 仍受门闩封闭。父收到控制阶段后杀进程，业务通道 EOF；重开 public command.get 查真实原 applied/revision1，Host.Observe 与同 raw 重传验证原固定决定/原Job唯一。该注入层准确是 Host 业务答复丢失，adapter/Host当时已知道成功，不报告 adapter未知。每个 gate 配单独 NormalReply 对照，释放真实门闩后完整 received reply 到达，clean child Wait后独立重开验证相同事实。控制事件不携带可冒充恢复查询的 receipt。

**两个确认丢失层分开记录。** 当前准确代码运行并复用既有 `TestPGCommitConfirmationLossPreservesUnknownAndOriginalIdentity`，没有重造 proxy。真实 PG wire proxy 转发 COMMIT，观察服务器 CommandComplete(COMMIT)+ReadyForQuery，抑制确认并断连；实际 adapter native Commit错误→真实 Host commit_unknown→独立连接 public查询 applied→原raw重传唯一责任。PG正常确认对照由现有 SharedAdmissionBehaviors及本票两阶段 NormalReply真实提交覆盖。

SQLite仅接受 process-fault-decisions 明确批准的 conformance-only `storagePortConfirmationLoss`：完整 delegate 原ctx/owner/callback给实际 Within及其真实SQL/Commit，仅实际返回nil后向消费方返回包装 ErrCommitUnknown；真实 Host.Record/runtime.Admit因此产生保留准确原CommandRef和 query_or_retransmit_original 的可Encode commit_unknown。原writer实际 Close完成后才独立 Open/Host查询原applied/revision1、准确输入/原Job并同raw重传不重复，真实正常worker可处理；不丢确认的 NormalConfirmation配对通过。实际 callback的 idempotency_conflict原样保留，未被改名unknown。装饰器仅装配到指定业务接纳调用，不拦迁移/观察事务。它证明真实SQLite提交与storage-port confirmation loss后的消费/恢复协同；**没有执行或证明 sqlite3 native Commit异常分支**。

**跨进程旧消息、接替与新工作。** 子进程A真实 Claim事务完成后，对已固定 hello/revision1/epoch1/原Job做实际 Project，事务外发送准确 Claim+已知SHA256结果；父保留真实消息，SIGKILL+Wait A。共同可信Clock由父装配，T0固定 `2026-10-03T01:00:00Z`、generation1，lease一分钟；父检查A lease_until等于独立预期T0+一分钟，按自己固定计划推进T1=`01:01:00Z`、generation2，不由worker消息或payload决定now。子进程B先真实 record新 world/revision2，再领取同Job/epoch2/revision2；父向B当前真实 Worker.Complete投递A旧消息，稳定 ErrClaim，B的真实 Host.Observe仍为 input2/work_revision2/completed0/leased、projection nil。B自己的真实新消息正常完成，clean Close/Wait后父独立Host观察done2、原Job、新world准确黄金SHA256；原source/update固定receipt分别仍applied1/applied2，真实扫描无额外责任。

NormalCompletion配对通过相同child Claim/Project/message流程，在旧Claim有效且无接替时正常完成hello/revision1。另两个独立真实child脚本先产生旧结果、终止并Wait child，再在仍有效的T0由当前Host消费buffered消息；分别提交“新Trigger→旧Complete”和“旧Complete→新Trigger”，两种真实提交顺序均保留原Job/work_revision2/completed_revision1/ready和旧准确hello投影，随后epoch2领取world/revision2并正常done2。没有宣称已杀死进程复活或外部fencing。Record/worker/replacement同一owner可信Clock，原raw accept_before固定`2026-10-03T02:00:00.000000Z`，没有用真实2026接纳、2100处理掩盖资格。

**有限期限与实际TDD。** 每个进程物理context12s，child测试总超时15s，真实transaction3s，父异常kill/Wait额外清理界2s、exec WaitDelay1s。控制/事件/业务答复各用独立pipe；frame4字节长度头、最大64KiB、严格未知字段/尾随JSON拒绝，检查scenario/stage/generation/时点，EOF/重复或错阶段/子进程早退/截止均硬失败。IO的context.AfterFunc关闭对应pipe并join其活动callback，Wait goroutine由父拥有且有界回收；不靠长sleep猜Commit或lease。

逐tracer真实red→green：首个双库precommit故事先缺runProcessAdmission编译red，再加最小真实进程设施green；SQLite端口故事先缺装饰器编译red，再实际delegate成功green；Claim故事在child尚无Claim分支时真实EOF/schema_invalid red，增加真实Claim/Project/message/Complete分支后green；首次nil json.RawMessage在pipe编码成null，被误当业务命令，明确omitempty后green。postcommit/正常对照及旧修订双顺序在已有正确机制上直接green，如实作为新增覆盖。审查新增 blocked precommit pipe故事实际先失败“child pipe ignored finite transaction context”（5.224s）：exec继承fd为blocking，NewFile未加入Go poller，Close不能打断阻塞read。child将fd0/3/4设nonblocking后重新NewFile（stdin同样重包），3s真实事务截止可退出，测试green3.614s；独立GetCommand精确not_found，同原raw正常恢复applied1。此故障也有正常门闩释放对照，没有故意改坏产品制造red。

**版本/配置与最终验证。** Go1.27.1/linux-amd64/CGO_ENABLED1，GCC Debian14.2.0-19，pnpm12.8.1；pgx/v5 v5.11.0与go-sqlite3 v1.14.52未改锁文件。实际PG18.6 (Debian18.6-1.pgdg12+2)，READ COMMITTED、synchronous_commit=on、statement_timeout2s、lock_timeout1s；每个真实child在实际事务观察并确认配置，再报告configured阶段。driver bundled SQLite3.53.4 (3053004)，source ID `2026-07-24 19:02:57 bf7c7f30031888f4e796e429ab3978879485813aaca6f641c7b33e4e09459bcc`，真实每连接WAL/FULL(2)/foreign_keysON(1)/busy_timeout100ms；现有连续3次关闭重开配置测试实际再次验证。adapter单写flock原样使用，父观察只在child死并Wait、或原Store.Close释放后重开，没有同时第二writer、删锁/绕过协调、手动checkpoint/复制/重建修复故障。

实际通过 make bootstrap、make fmt、make check（vet、生成一致性、Go/TS两方向各158共同fixture、build）、make test-race、go mod verify、git diff --check；两库十项v1 SHA256SUMS保持。最终准确故障代码上 mandatory `make test-integration` 的 `-count=1 -tags=integration -timeout=120s ./conformance/recovery/...` 通过33.547s；随后 `go test -race -count=1 -tags=integration -timeout=120s ./conformance/recovery/... ./internal/durableworkdemo/...` 通过46.993s/1.501s，两轮没有cached。修复阻塞pipe前的完整22.068s/55.310s通过仅为历史阶段，最终以修复后的上述结果为准。显式移除LERNA_TEST_POSTGRES_DSN后make test-integration按预期硬失败，不能skip必需服务绿灯。

DSN只由程序从600保护文件读到显式LERNA_TEST_POSTGRES_DSN环境，child仅继承，不cat/打印/提交、不读services.env。SQLite及Go临时文件由本轮登记的 `/workspace/lerna-08-*` TMPDIR承载，实际overlayfs；finally只清理自己登记范围。PG由父先实际CREATE并登记随机schema，children只打开此scope、不CREATE/DROP；schema创建Store保持开放供父有限cleanup，所有本票轮次登记scope正常清理。没有DROP数据库、访问smoke或猜删04此前无法重新确认归属的历史scope。

**整合与证明范围。** 当前08依赖仍仅04，完成的是准确04/09基线的真实进程/确认边界；05授权决策后来要求处理成功入口显式permissions/policy/Start，05合入时root/05需将本harness真实旧消息来源与正常对照适配为Start后的准确输入，并走严格Finish/Complete，不能用缺Start的拒绝冒充旧Claim隔离。本票不新增05依赖，也不提前宣称05行为已验证。准确feature tip远端CI及全片审查由root后续执行。

可宣称两库真实进程提交边界恢复、PG真实wire确认丢失、SQLite真实提交后的存储端口确认丢失及应用原身份恢复、跨进程原Job/Claim epoch隔离。明确未验证SQLite native Commit异常分支、SQLite VFS/I/O/掉电、磁盘丢失、异地/3AZ耐久、生产RPO0或任何外部副作用恰好一次；本票SIGKILL不替代这些结论，不隐藏整片最终关闭依赖。

2026-10-03 root将本票合入d89e789并核实push CI37151050492 success：真实两库集成9.718s/race26.419s均-count=1、十项v1hash不变。准确CI记录见[进程故障检查点](../ci-verification.md#双库进程故障检查点)。仍由05整合后复验实际Start门禁，不以本票代替整片退出。
