# 06 实际实现与验收映射（本票已退出）

本票六AC已完成并 resolved。独立分支从正式 root `e29675d75f5135995614e4eb2faadf6c2ac1581d` 开始；直接前置只有已 resolved 的01与05。最终代码／双轴复核固定在 `a5005ab0d583f9a1906809cfd94c27daf89f5666`，其后的退出文档单独提交。整片03架构及关闭仍由root负责，不能由本票退出替代。以下进展记录保留各阶段实际限制，最后一节给出最终检查。

## 六条 AC 的实际出口

| AC | 实际行为与独立证据 | 源码与已运行结果 |
| --- | --- | --- |
| 1 | 双 owner 正常子进程发布原 Proposal／产物；完成事务 COMMIT 前后分别 SIGKILL，独立公开 Get 和 Publisher 读回原身份、固定回执及公开 usage。 | `conformance/recovery/decision_process_test.go`；正常 control normal0.688/race4.684；完成事务前 red1.029→normal1.959/race6.206（e0701e8）；成功 COMMIT 后 red1.107→normal1.152/race5.552（9093327）。 |
| 2 | 原 Source Publish 已提交、Decision Finish 前终止；真实 LeaseUntil 后公开 Step 恢复原 prepared key／digest／refs，独立出版 bytes、ref 与原 fixed receipt 一致。 | 同文件 `TestDecisionSIGKILLAfterPublicationRecoversOriginalRefs`；red1.159→normal2.025/race6.401（a29b54c）。这是原发布身份恢复，未宣称跨 owner 原子提交。 |
| 3 | 独立目标真实 COMMIT 前终止后当前处理事实及 cursor 未提交；真实 COMMIT 后／reply 前终止后普通 Query／Read 和独立 Observer 保留原 bytes、version、窗口、接收事实。 | `conformance/internal/testkit/target/process_test.go`；前边界正常 control＋red0.070→normal0.088/race3.174（c6e42f5）；后边界 red0.066→normal0.101/race4.204（43b4b16）。真实 WaitStatus SIGKILL 与独立 Reply EOF，提交后边界已有同 gate 正常释放；前边界首轮正常 control 未进入 gate，审查发现原概括不准确，后来真实同 gate 正常释放／SIGKILL pair normal0.082/race3.174补证。 |
| 4 | 重开同 DatabaseID／scenario／event／cursor，不重复原完成步骤；ReceiveOnly 保持 pending、过期不取消迟到责任，下一代从保存 cursor 应用原 key。另两个独立数据库及 scenario 同 Seed73 重演明确两步骤。 | pending normal0.139/race4.201（232973e）；同 seed normal0.102/race5.182，四个正常 child 都实际 Wait，事件／结果／cursor 相等且 DatabaseID 不同。DropResponse 是计划故障，未称物理 SIGKILL。 |
| 5 | 两实际消费者共用有限 Child／Inherited；Start 发布 handle 前不能假确认退出；双具体 owner 在 Start 前借用同 child；首次 native／file Close unknown 持续保留，pre-native drain timeout 可重试。 | `conformance/internal/testkit/process`、`decisionfixture/process_borrow.go`、目标 lifetime/file tests；机械 driver red→green 不称 native DB fault。受影响原02 SQLite三故事 normal3.556/race8.675；drain normal0.025/race1.036；实际 FD 三故事 normal0.082/race1.133（2306e69）。 |
| 6 | 只用正常 Decision／目标和直接前置01＋05；普通接口与 privileged Observer 分开；候选规则、取消、生产 provider／Task／Effect 与 whole03 未增加承诺。 | [进程 README](../../conformance/recovery/README.md)、[目标 README](../../conformance/internal/testkit/target/README.md)、本票原六条 AC。所有 SIGKILL 只覆盖进程故障，不证明断电。 |

## 实际端口与生命周期

`SourceDescriptor{Schema, Owner}` 只描述已登记 Source。私有 DSN 通过环境继承，不进入配置帧、ready、结果或日志。父进程独立负责两 scope 的 CREATE／迁移／seed／admin drop；child 只打开既有 scope。`World.BorrowChild` 在两个具体列表登记同 `*process.Child`；每个非 nil 构造返回值在尝试借用前已有独立有限 cleanup，拒绝借用不冒称登记成功。

`Child` 使用固定当前 test binary/helper、三条有限管道、64 KiB 帧、唯一原生 Wait 及有限启动完成等待。Stop-before-Start 封住将来 Start；启动尚未完成／原生 handle 未发布时返回有限 unknown，不能确认 scope 可删除。历史错误和实际退出确认分别保留；只有实际 Wait／所有父 FD 关闭确认后 owner 才关闭 SQL、删除准确 scope。未知 SQL native Close 独立保留；子进程退出不擦除父 handle 的首次结果。

目标 `Open`／`OpenObserver` 在失败而 cleanup 未确认时返回初始化清理 holder＋error。实际 writer file 与 parent directory FD 复用首次 Close 结果。Target 在 native Close 前等待操作 gate，超时可重试；第一次 native Close／release 结果一经出现则 sticky，不能用 `database/sql` 第二次 nil 擦除未知。contextless native Close 无法凭有限调用 context 保证自身可取消，不能遗弃 goroutine 后删 scope。机械 tests 使用已标明的 driver／FD 替身，没有物理 scope，不称实际 SQLite、PG 或文件 native fault。

Decision 三个 gate 位于私有 integration test consumer：真实 Publisher.Publish 成功后；原 callback SaveDecision＋Complete 返回 nil 后、Core COMMIT 前；原 Store.Within 返回 nil 后、reply 前。后一 gate 使用原外层有限 context，避免复用已取消 transaction context。公开 Get 和独立 publisher 事实分别确认真实阶段。目标 test-binary setter 只配置私有实际 COMMIT 前后 hook；普通 Config／Request 没有 fault 参数，已完成事件的固定结果不再次触发执行。

## 环境、登记与失败历史

实际已测：Go1.27.1／Linux CGO，SQLite3.53.4／go-sqlite3 v1.14.52、WAL／FULL／FK，PG18.6／synchronous_commit on／read committed；Decision实际0001＋0002，目标冻结0001＋0002。没有新依赖或新公开 profile。最终锁定工具版本与全部实际存在 SHA256SUMS 的逐项数目，待基础检查追加，不能把未合入的02／03新 archive 当成本分支已有源。

耐久目标 scope 位于真实 `/workspace` overlay。每次成功 Mkdir／CREATE 即登记并 fsync，先确认具体 child Wait／FD／writer／observer native Close 后才移除本次确切路径。reviewfix批后已审计120条外部 target 登记、44条原02 SQLite登记、118条PG登记（recovery22＋Decision96）均 absent；另六条早期 target 只做本地 write＋fsync，作者漏传外部 registry，六个确切路径单列且 absent，未补造外部登记。父 overlay 保留供最终回归。原旧02未知 PG、票10失名 scope、票07未知 CID，以及其他票未知构建 scope 均未触碰；不宣称全环境零资源。

失败历史保留：共享进程提取缺函数／多余 import 两次编译失败不称 behavioral red；blocked-pipe 预期 exit1 的历史错误未获确认导致第一次 runnable failure，已最小 WaitFailure 修复；首次 FD normal／race 调度未确认非重叠，后来严格串行重跑通过；首次 psql audit URI 放错 PGDATABASE 只读失败后改 private parsed env；sameSeed 首次文件审计误选 `sqlite ` 前缀得到零条，立即以实际 `sqlite-target ` 前缀 assert23条全部 absent，未据零条宣称清理。

完整按批 red／green／code pin／session／cleanup 记录在本地 `/tmp/lerna-03-ticket-06-evidence.md`。pending 日志是如实后录的 tool transcript；drain、FD affected 与 sameSeed 日志是实际运行期间 raw capture。最终检查会保存运行期间原日志，票仍 claimed，root负责真实整合／push。

## 固定 fdc76b9 的首轮最终检查与审查发现

完整target normal1.899/race15.937（真实04→05升级）、process normal0.045/race1.068、新Decision四故事 normal5.895/race15.909、旧02三个受影响故事 normal4.486/race14.647，bootstrap及makecheck均实际exit0。旧blocked-pipe故事仅有SQLite，另外两故事实际PG＋SQLite，未造不存在的PG case。raw日志保存于本地 `/tmp/lerna-03-ticket-06-final-*.log`。最后确切审计target116外部FS、recovery30FS＋10PG、Decision68PG全部absent；旧六local-only单列absent，scope及两owner退出后释放测试槽。

Standards轴：一项文档标准问题（COMMIT前same-gate正常对照表述过度）；一项 judgement（三个Decision kill／EOF／Stop重复）。Spec轴：a0、b0、c2，一项P2旧demo覆盖前代holder可丢失首次FDunknown阻断删除责任，一项P3同gate对照资格。修复由原implementer承担：保每代具体holder、有限16代在New前拒绝；补实际before_commit正常释放；仅共享具体Rule-child物理安全序列。机械生命周期守卫不创建物理scope、不称原生FD故障。补测与剩余最终基础检查尚未运行，既有成功不关闭这些发现，票仍claimed。

## 原 implementer 的实际修复与复验（最终基础检查前）

Standards hard／Spec P3：补实际 `before_commit` 正常 release，与原 SIGKILL 相同 hook，明确接收独立 reply、zero Wait、FD Stop，再普通 Query／Read 与 Observer 证明原准确 bytes／version1／一次接收／cursor1。正常0.082、race3.174均exit0；这是首个 same-gate 正常证据，未倒写 fdc76b9。

Spec P2：ownedFixture 保留每代具体 hostProcess；所有代 Stop／FD确认完成后才准许 SQLClose／Drop，首次 unknown cause 保留且后代 nil 不能放行。16代限制在 physical New 前拒绝，partial nonnil holder仍在 Start前登记。`TestOwnedFixtureRetainsEarlierGenerationCleanupUnknown` 无物理scope／child／FD／连接，仅替换 native Stop 清理知识边界：真实 red0.010（旧前代 cause 丢为nil）→最小 green0.011／race1.043。它不证明原生FD故障。实际原三故事＋`TestProcessClaimTakeoverRejectsBufferedOldCompletion` 两DB回归 normal5.322／race18.233均exit0，真实换代保留前代责任。

Standards judgement：三处仅共用私有具体 `killRuleChild` 的 SIGKILL／独立 Reply EOF／确认 Stop 序列，业务 Proposal／receipt／usage断言仍留各故事。Decision四故事 normal5.366／race17.028均exit0。

全部严格确认前 shellsession exit后才启动下一组；实时 raw日志 `/tmp/lerna-03-ticket-06-final-review-{mechanical-red,mechanical-green,mechanical-race,pregate-normal,pregate-race,decision-normal,decision-race,legacy-normal,legacy-race}.log`。最终exact审计120 target FS、44 recovery FS、118 PG均absent，旧六local-only另列absent；所有child／FD／writer／observer关闭确认后释放slot。实现及复验已完成，独立复核和修复后的makecheck／base-race／modverify／27＋71＋3 hash尚待，继续claimed。

## 最终检查与本票退出

实际代码 `a5005ab0d583f9a1906809cfd94c27daf89f5666`，Standards固定复核0hard／0smell，Spec固定复核a0／b0／c0，完整15commits／24paths；原失败报告保留，两个followup分别为 `/tmp/lerna-03-ticket-06-standards-followup.md` 和 `/tmp/lerna-03-ticket-06-spec-followup.md`。root明确最终整片03架构会在实际合并树另做，当前预览并非本票架构通过证据，不引入本票新的wholeclose依赖。

最终实际严格串行命令：

```sh
make check GOFLAGS='-mod=readonly -p=1'
go test -race -p=1 -count=1 -timeout=120s ./...
go mod verify
```

每条前一个shellsession实际exit后才开始。makecheck session90585 exit0，含gofmt／prettier／vet、生成一致性、Go／TS、81＋158共同合同正反序与真实双向roundtrip、build；base-race session51409 exit0，其中目标完整race16.526s。锁定bootstrap已在首轮真实exit0，工具／依赖未改，因此未再次无端安装。Go1.27.1、Node24.19.0、pnpm12.8.1、TypeScript7.0.2实际校验；PG18.6、SQLite3.53.4及各实际阶段／计划／迁移原始输出由运行中的raw日志保存。

当前分支六个实际 `SHA256SUMS` 均执行 `sha256sum -c SHA256SUMS` 退出0：durable pg-v1 5、pg-v2 9、sqlite-v1 5、sqlite-v2 8，共27；legacy-970fd90 71；deterministic-target/v1 3。没有声称未合入的新02／03 FINAL01 archive已在本树验证。未改冻结04源码、0001／已发布0002 SQL或任何公开profile。文档相对文件链接存在，git diff检查通过；代码不变后未重复无关integration／wholePG套件。

现场raw日志：`/tmp/lerna-03-ticket-06-final-postfix-check.log`、`/tmp/lerna-03-ticket-06-final-postfix-base-race.log`、`/tmp/lerna-03-ticket-06-final-modverify.log`、`/tmp/lerna-03-ticket-06-final-hashes.log`。首轮fdc日志未覆盖；早期pending两份是明确标注的后录tool transcript，不称现场tee。完整逐vertical红绿／错误／code pin／命令／session记录 `/tmp/lerna-03-ticket-06-evidence.md`。

最后审计 `/tmp/lerna-03-ticket-06-final-audit.json` exit0：184个外部登记target目录、44个recovery目录、118个确切PGschema均absent；六个早期本地ack／fsync而漏外部账本的路径另列absent，未补造登记。先确认所有children实际Wait／全部FD／writer／observer nativeClose，再核对自己的目录。首次空目录guard因仅Node24.19工具compile cache非空而exit1，没有删除；所有工具命令实际exit0后，检查准确七文件及目录、无symlink，先inventory登记／fsync，再逐文件unlink、逐空目录rmdir并fsync父目录。确切owned overlay已absent；保留账本作证据。工具生成的cache在发现后登记，未假称它也由fixture即时Mkdir登记。只删除自己的精确路径，未猜删任何旧未知PG／CID／其他票未知scope。

六条AC已由原作者实现、真实验证、独立双轴复核并resolved。所有exec／build／test／DB／child sessions已退出。root负责正式merge／push、后续CI、实际合并树架构审查与whole03关闭；本票worktree／branch保留。证据只支持正常Decision／target的进程SIGKILL及所测正常控制，不支持断电、provider幂等、生产容量、可用区耐久、Executor Effect或whole03完成。
