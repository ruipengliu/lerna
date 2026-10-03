# 切片02退出证据（2026-10-03）

**Completed：原八票53项、锁范围09五项、fixture10十项，共68项AC均resolved。** 最终受测修复代码 `f56d93095304f0956c23b5641d9b7b1e222c40c1`，准确整合代码 `554874470d5abeb71fa743708580f3121b8944f1`；两者产品与测试源码完全一致。前置01准确代码23bac17及完整退出记录已核对。集成分支 `codex/lerna-implementation`。

两轴分别关闭原Standards3项/Spec1项及票10后置各1项P2，最终新增均0；[原发现、修复与独立复核](code-review.md)分别保留，未合并排序。授权代理选择并grill的唯一fixture候选已实现，独立[收益复核](architecture-review.md)确认六类caller与pool减少生命周期知识，三张Store-pointer maps删除，实际Store直接交Host；历史loader、process与后端故障仍独立负责。已确认失败holder错误继续聚合而安全清理，未确认退出仍保留scope。没有新增领域ADR或未来03实现。

## 原规格七项验收

| AC | 票与真实入口 | 已验证行为 |
| --- | --- | --- |
| 1 原键决定、冲突与恢复 | 01/02接纳、07retention、08process shared Host/public command.get | 原Command/Job及固定回执恢复；异内容固定冲突；正文gone后墓碑仍阻止重建。PG提交确认丢失；SQLite真实成功commit后的storage-port故障。 |
| 2 提交前后进程终止 | 08 TestProcessCrashAdmissionRecovery | 真实SIGKILL precommit及postcommit-before-reply；有限Kill/Wait，接替恢复已提交责任，无孤立成功。 |
| 3 新工作与旧完成 | 03/04共同work、05严格Start整合、08两种进程先后 | 两种合法顺序保留新work和原Job；完成只推进claimed revision，原成功投影/receipt不被旧失败抹去。 |
| 4 原Job租约接替 | 03/04/08 claim/takeover/normal controls | 更高epoch沿原身份继续，完整Claim绑定、过期及迟到旧worker拒绝，新worker真正完成。 |
| 5 丢通知、due与等待 | 05共同wait/scan、06独立Run及review PoolWake/Run | 所有通知丢失仍扫描恢复；finite Timer在Tx外；全members的due/lease/current+claimed期限唤醒，非anchor、500ms执行/100ms退避/1s fallback窗口正常完成；quota0维护保留新责任。 |
| 6 双库设置及来源升级 | 01–08共享故事、真实v1/v2全dump/file恢复 | PG READ COMMITTED/sync-on、SQLite WAL/FULL/FK及单writer；四组真实来源27项hash，0001–0005升级保留原记录，迁移失败回滚/重试。 |
| 7 饱和、配额及有限队列 | 06 pool/FIFO/Run与wake修复 | 普通饱和时control/reconciliation真实Start/Finish；tenant×lane/queue真实竞争、动态等待FIFO/恢复入尾、分页高水位与有服务机会的有限维护。 |

[各票Comments](issues/01-pg-atomic-admission.md#comments)及[容量证据](issues/06-fair-capacity-and-quotas.md#comments)、[初轮review修复](code-review-fix-evidence.md)、[fixture清理修复](fixture-cleanup-fix-evidence.md)保存实际正常与故障观察。09协调键为schema-qualified JSON，有限32bit hash碰撞及旧binary需drain的部署边界保留。

## 准确版本、环境与检查

内部 `host-durable-work-1` / `host/durable_work.record`，公开合同 **1.0.0**仅完整 `command.get`，生成器 **1.0.0**。PG与SQLite host迁移均 **0001–0005**，已发布来源不改写。规则demo不是Task/Decision/Operation/Effect或第三方SDK/生产授权。

实际Go1.27.1、Node24.19.0、pnpm12.8.1、TS7.0.2、Prettier3.6.2、pgx5.11.0；PG18.6、go-sqlite3v1.14.52自带SQLite3.53.4/CGO/Linux amd64。SQLite使用本轮登记的`/workspace` overlayfs临时目录，非/tmp内存盘耐久推断。本地psql17.11实际完整恢复18.6 dump；CI固定同镜像psql18.6，镜像及有限恢复客户端依据PG README。

准确f56源码的`make fmt`、`make check`、`make test-race`、`go mod verify`、四份SHA256SUMS的全部27项及完整baseline diff检查通过。最后顺序执行 `make test-integration` **50.431s**，随后 `go test -race -count=1 -tags=integration -timeout=120s ./conformance/recovery/...` **93.834s**；normal同样count1/timeout120。没有skip、放宽产品/whole期限或并行DB/broad竞争。

准确merge5548744的[CI37162569420](https://github.com/ruipengliu/lerna/actions/runs/37162569420) completed/success；contracts111318872458、durable-admission111318872597及全部步骤success。固定18.6工具生命周期race **2.407s**，完整count1双库integration **20.985s**、race **45.463s**，27项manifest全部OK。原检查点CI不替代这一提交；[CI记录](ci-verification.md)保存各自范围。

## 清理与保留限制

最后fix独立有限观察只读即时fsync登记项：**285个PG namespaces、265个SQLite fixture目录均absent**，7个准确自有顶层TMPDIR在全部进程退出后清理。首个red的1个确证PG残留早期已按准确名字回收；最终观察recovered=0。原架构轮321PG/319目录是另一轮登记结果，不能混算或推成全环境清零。

旧04未知PG schema、10首版red两个成功CREATE却遗失准确名称的PG scopes、07unknown CREATE/不完整CID可能未启动container，均未猜删且无法确认清理。两次架构作者失败、cleanup red0.328s→green0.302s、补测17.203s初始化遗漏、旧118.936s并行race/7.282s holder提前退出等真实历史保留。产品Tx3s、独立fault holder10s/join11s、whole120s不变。

进程SIGKILL不证明断电、三可用区或跨区耐久；SQLite postcommit-port loss不证明原生Commit错误；数据库Claim拒绝不证明外部效果已隔离。有限FIFO/维护需要实际服务机会，不是无条件墙钟SLA。HTML已生成但xdg-open exit3无GUI，未声称浏览器呈现验证。没有真实供应商、财务费用、生产Grant、部署或PR证明。

F01–F04及通知/配额只关闭本片DB/Host范围；跨切片G2、G3第二业务实现与生产/外部效果仍待各自验收。03的真实依赖现已满足，发布前仍按本退出代码与最终文档SHA核对其TMP端口草稿，再开始六张票。
