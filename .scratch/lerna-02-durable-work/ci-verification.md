# 切片 02 远端 CI 证据

## PG 原子接纳检查点

2026-10-03，通过 GitHub Actions API 和实际 job 日志核实：

- 准确提交：`e5f26b87fb8914dc16bb6837abff6607a50cddb1`，集成分支 `codex/lerna-implementation`。
- push run：[37145113569](https://github.com/ruipengliu/lerna/actions/runs/37145113569)，`completed / success`。
- `contracts` job `111267327816`：锁定工具安装、`make bootstrap`、`make check` 及清理全部 success。
- `postgres-admission` job `111267327696`：准确 PostgreSQL 镜像启动、`make test-integration`、真实集成 race 及清理全部 success。
- v1 fixture 的迁移、命令、真实 dump、writer 观察及 writer revision 五项校验和全部 OK。
- 日志中的真实套件结果：`conformance/recovery` 集成 `1.778s`，race `3.551s`；两次均未显示 `(cached)`。

该提交验证 PG 接纳、原固定回执、提交未知及 v1 来源输入范围。SQLite、Claim、调度、SIGKILL 和整片退出仍待后续票据，不将 CI 配置或 Git push 成功当作验收成功。

后续必需真实服务入口将增加 `-count=1`，以保证服务重新创建后重新执行集成行为，不复用 Go 测试结果缓存；本检查点日志已证实实际执行。

## PG 修订领取检查点

2026-10-03，通过实际 Actions API/job 日志核实提交 `e8e3384e4cce1f63187ff84f281283a023e07ab1`：push run [37146622433](https://github.com/ruipengliu/lerna/actions/runs/37146622433) 为 `completed / success`。`contracts` job `111271789953`、`postgres-admission` job `111271790111` 均 success。

真实29项PG套件集成 `2.534s`、race `4.866s`，未显示 cached；原五项 v1 artifact 校验和仍全部 OK。票03的代码 `18b80ce` 已整合并在此远端提交验证。范围包括一致领取快照、修订竞争、续租、到期／接替及受控数据库写入隔离；SQLite、调度、完整历史数据升级、SIGKILL与切片02整体仍未退出。

## SQLite 接纳与 PG 领取整合检查点

2026-10-03，准确提交 `930d3cae32e674e6f1ca856b6b60fa198e1b1b5c` 的 push run [37147496071](https://github.com/ruipengliu/lerna/actions/runs/37147496071) 为 `completed / success`。实际 `contracts` job `111274354950`、`durable-admission` job `111274355135` 均 success。

必需 PG+SQLite 集成和 race 都采用 `-count=1` 实际重新执行：`conformance/recovery` 集成 `2.868s`、race `8.739s`。PG／SQLite 两份真实 v1 artifact 的十项校验和全部 OK。配置缺失或服务不可用仍硬失败，基础 check 无外部服务。

此范围是 SQLite v1 接纳、重开、单写和 PG v2 领取；尚不包含 SQLite Claim 或后合入的09锁范围修复。09的本地并行复验另见票09Comments，新准确远端提交仍待实际核验；整片02未退出。

## PG 锁范围修复检查点

2026-10-03，准确提交 `3a7f1f85c1c63204083232ef07122d394d8173ac` 的 push run [37148346512](https://github.com/ruipengliu/lerna/actions/runs/37148346512) 已 `completed / success`；`contracts` job `111276804410` 与 `durable-admission` job `111276804200` 全部 success。

两库必需集成 `4.277s`、race `10.252s`，均以 `-count=1` 重新执行，原十项 v1 artifact 校验和全部 OK。包含09跨schema独立与同schema互斥回归；此前“新准确远端待核验”为历史检查点。当前受测提交尚不包含04 SQLite Claim；后合入04的准确CI另行核验，整片02仍未退出。

## SQLite 接纳本地检查点

票02 writer源码 `f4fb057` 已锁定；两库共享接纳套件、SQLite文件/配置/进程排除/Busy/取消/Close与真实v1恢复已本地通过。必需make集成及CI集成race已使用-count=1，并同时执行PG与SQLite；新版远端tip仍由root后续核实，不能引用上面的PG-only历史run当两库成功。详细本地命令/运行时/来源见[票02 Comments](issues/02-sqlite-durable-admission.md#comments)。

SQLite票02合并最新 `6ccdb6d` 后的本地准确检查：make check/test-race成功；两库make test-integration顺序执行成功（7.501s），随后两库integration-race成功（15.058s），均-count=1。一次同时运行普通/race遇到现有PG跨schema advisory锁域耦合的合法skip导致测试失败，已如实记入票02 Comments并交独立决策；随机schema仅证明数据及清理隔离，未声称锁域完全隔离。新两库远端CI仍待root实际核实。

## 双库修订领取检查点

2026-10-03，准确提交 `b1674b2d0252da73f3dfd753857484597dc0a080` 的 push run [37149178541](https://github.com/ruipengliu/lerna/actions/runs/37149178541) 已 `completed / success`。实际 `contracts` job `111279225885`、`durable-admission` job `111279226000` 均 success。

两库必需集成 `8.417s`、race `15.292s`，均 `-count=1` 重新执行，十项 v1 artifact 校验和全部 OK。包含04 SQLite Claim、PG/SQLite共享13项工作行为与09锁范围修复；05调度、07完整升级/清理及08进程故障仍由后续准确提交验证。04失败轮留下的无法确认归属scope限制保留，成功CI不追溯证明该轮已清理。整片02仍未退出。

## 双库进程故障检查点

2026-10-03，准确提交 `d89e78900f1a46b4b3ba6cfdc1bc5795feb20839` 的 push run [37151050492](https://github.com/ruipengliu/lerna/actions/runs/37151050492) 已 `completed / success`。实际 `contracts` job `111284851414`、`durable-admission` job `111284851550` 均 success。

必需真实两库集成 `9.718s`、race `26.419s`，均-count=1，十项v1校验和全部OK。包含08真实提交前/Host答复前SIGKILL、正常对照、跨进程原Claim接替及SQLite存储端口确认丢失；不声称SQLite原生Commit异常、断电或生产故障域。05严格Start/统一Clock整合和07当前新恢复工具仍待其各自准确远端提交验证。

之前纯文档 `e0f3894` 的 push run [37150721321](https://github.com/ruipengliu/lerna/actions/runs/37150721321) 也实际success，未将该文档检查点当成08代码测试。

## 正文清理与完整历史恢复检查点

2026-10-03，准确提交 `418415fa7ce11ee163607ced2c09714a4722544e` 的 push run [37152615344](https://github.com/ruipengliu/lerna/actions/runs/37152615344) 为 `completed / success`。`contracts` job `111289412432`、`durable-admission` job `111289412231` 均 success，实际新恢复客户端步骤全部success。

日志确认固定镜像client为psql18.6；显式必跑containerpsql生命周期race `2.378s`，完整双库必需集成 `11.242s`、race `28.948s`，全部-count=1，十项v1校验和全部OK。包含07完整v1来源恢复/真正v2+v3迁移/版本保存拒绝回滚重试、清理/墓碑，以及08进程故障。CID登记后取消清理已真实执行；未知CREATE未启动容器的历史限制保留。这一提交尚不包含随后05严格Start/等待迁移与新v2来源，05的准确新CI仍待核验。

## 持久等待与严格处理门禁检查点

2026-10-03，准确提交 `7fa7594ff213a23c1cf156187cf70295c9270db4` 的 push run [37153111359](https://github.com/ruipengliu/lerna/actions/runs/37153111359) 为 `completed / success`。`contracts` job `111290867825`、`durable-admission` job `111290867962` 均 success。

固定18.6工具生命周期race `1.567s`，完整PG/SQLite必需集成 `14.404s`、race `34.816s`，全部-count=1。四份真实v1/v2来源manifest共27项全部OK，包含共享v2 writer源校验；root全range Git diff也通过，原始dump字节及已发布001–004未变。此检查点包含05与07/08真实Start/Clock整合、legacy真实v2 Claim/work恢复、持久等待/退避/期限及固定回执；06配额/公平及整片退出仍待后续。

## 全部核心票整合检查点

2026-10-03，准确提交 `26c91003e7cf09dcc42e8d8c90d0db7b65a915a5` 的 push run [37156883508](https://github.com/ruipengliu/lerna/actions/runs/37156883508) 为 `completed / success`。实际 `contracts` job `111301974773`、`durable-admission` job `111301974555` 的全部步骤 success。

固定18.6恢复工具生命周期 race `1.569s`；完整PG/SQLite必需集成 `21.892s`、race `47.390s`，均以-count=1重新执行。四份真实v1/v2来源的27项manifest全部OK。包含06有限队列/共享配额、持久动态FIFO、三个独立lane、无执行额度维护及真实复制SQLite scope拒绝；此前05/07/08等既有路径一并执行。

该提交是整片审查前检查点。独立[两轴审查](code-review.md)仍发现错误原因丢失、陈旧说明、重复门禁和Run固定fallback问题，当前交单一分支修复。此CI成功不证明发现已关闭，也不替代修复后新CI或架构审查；整片02仍in-progress。

## 两轴修复整合检查点

2026-10-03，准确修复提交 `6783307ebe7d802f78f9aa7bdb1a1464ec5749e1` 的 push run [37158656088](https://github.com/ruipengliu/lerna/actions/runs/37158656088) 为 `completed / success`。实际 `contracts` job `111307253090`、`durable-admission` job `111307253299` 全部步骤success。

固定psql18.6工具生命周期race `1.560s`，完整PG/SQLite必需集成 `21.011s`、race `46.969s`，均-count=1/timeout120且实际重跑；原27来源manifest全部OK。包括all-members/current+claimed唤醒、真实Run重试窗口、零quota维护、PG取消/超时/driver cause与既有完整恢复行为。测试专用fault-holder有限10s，业务worker仍3s。两轴修复前失败及证据范围见[修复记录](code-review-fix-evidence.md)，未用新CI追溯抹去失败。

独立Standards/Spec复核同准确6783307均新增0项，原3/1项分别关闭。整片02仍待最终架构候选选择/实施及退出汇总；此CI是修复整合检查点，不宣称尚未实现的fixture架构优化已通过。
