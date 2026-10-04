# 06 实际实现与验收映射（最终检查前）

本票仍 claimed。独立分支从正式 root `e29675d75f5135995614e4eb2faadf6c2ac1581d` 开始；直接前置只有已 resolved 的01与05。当前六条行为均有实际运行证据，最终受影响回归、基础检查及独立评审待完成，不能据此关闭本票或整片。

## 六条 AC 的实际出口

| AC | 实际行为与独立证据 | 源码与已运行结果 |
| --- | --- | --- |
| 1 | 双 owner 正常子进程发布原 Proposal／产物；完成事务 COMMIT 前后分别 SIGKILL，独立公开 Get 和 Publisher 读回原身份、固定回执及公开 usage。 | `conformance/recovery/decision_process_test.go`；正常 control normal0.688/race4.684；完成事务前 red1.029→normal1.959/race6.206（e0701e8）；成功 COMMIT 后 red1.107→normal1.152/race5.552（9093327）。 |
| 2 | 原 Source Publish 已提交、Decision Finish 前终止；真实 LeaseUntil 后公开 Step 恢复原 prepared key／digest／refs，独立出版 bytes、ref 与原 fixed receipt 一致。 | 同文件 `TestDecisionSIGKILLAfterPublicationRecoversOriginalRefs`；red1.159→normal2.025/race6.401（a29b54c）。这是原发布身份恢复，未宣称跨 owner 原子提交。 |
| 3 | 独立目标真实 COMMIT 前终止后当前处理事实及 cursor 未提交；真实 COMMIT 后／reply 前终止后普通 Query／Read 和独立 Observer 保留原 bytes、version、窗口、接收事实。 | `conformance/internal/testkit/target/process_test.go`；前边界正常 control＋red0.070→normal0.088/race3.174（c6e42f5）；后边界 red0.066→normal0.101/race4.204（43b4b16）。真实 WaitStatus SIGKILL 与独立 Reply EOF，各有相同 gate 正常释放。 |
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

耐久目标 scope 位于真实 `/workspace` overlay。每次成功 Mkdir／CREATE 即登记并 fsync，先确认具体 child Wait／FD／writer／observer native Close 后才移除本次确切路径。当前已审计23条外部 target 登记、18条原02 SQLite登记、40条PG登记均 absent；另六条早期 target 只做本地 write＋fsync，作者漏传外部 registry，六个确切路径单列且 absent，未补造外部登记。父 overlay 保留供最终回归。原旧02未知 PG、票10失名 scope、票07未知 CID，以及其他票未知构建 scope 均未触碰；不宣称全环境零资源。

失败历史保留：共享进程提取缺函数／多余 import 两次编译失败不称 behavioral red；blocked-pipe 预期 exit1 的历史错误未获确认导致第一次 runnable failure，已最小 WaitFailure 修复；首次 FD normal／race 调度未确认非重叠，后来严格串行重跑通过；首次 psql audit URI 放错 PGDATABASE 只读失败后改 private parsed env；sameSeed 首次文件审计误选 `sqlite ` 前缀得到零条，立即以实际 `sqlite-target ` 前缀 assert23条全部 absent，未据零条宣称清理。

完整按批 red／green／code pin／session／cleanup 记录在本地 `/tmp/lerna-03-ticket-06-evidence.md`。pending 日志是如实后录的 tool transcript；drain、FD affected 与 sameSeed 日志是实际运行期间 raw capture。最终检查会保存运行期间原日志，票仍 claimed，root负责真实整合／push。
