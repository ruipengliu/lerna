# 公共持久框架运行证据

2026-10-02；范围按[用户确认的规格](spec.md)。代码为本仓库工作树；默认五进程维持诊断，不开放领域业务。参考[公共实现](../../internal/durable/README.md)与[套件映射](../../tests/integration/README.md)。

## 公共约束

| 验收 | 公共结果与观察 | 尚缺的领域证明 |
| --- | --- | --- |
| FW-01 | pass：两方言同键并发、业务键归并、原路由/服务隔离、当前披露；提交前/后真实子进程崩溃；PG COMMIT 已发生后网络确认截断，沿原键核对一次 | 真实 Task/预算等事实映射 |
| FW-02 | pass：最终阶段命令保存内部准备，重投/等待不冒充 accepted 或 not_found；最终决定固定 | Memory 远端 copy/发布/清理 inconclusive |
| FW-03 | pass：过期 W1 回写整笔回滚、W2 新 epoch；提交前复核过期；领取未知独立确认保持原观察，未提交候选不处理 | 真实迟到效果/账单的独立核验入口 inconclusive |
| FW-04 | pass：新责任与 done/waiting 各两种次序、同事务 Raise；保留新版本 ready/较早 due，Hint 不加版本 | 各领域的原事实/累计预算映射 |
| FW-05 | pass：真实 DB 准备提交未知不向调用者暴露发送资格，沿原命令恢复 | Brain/Executor 不可撤回入口、费用与供应商真值 inconclusive |
| FW-06 | pass：全丢通知仍扫描；请求独立；阻塞续租不推迟旧截止；停止后 Step/Within 不恢复；实际退出前保留容量；控制独立池、有限页修复、PG LISTEN 重连 | 远端物理并发/费用与生产时钟资格 inconclusive |
| FW-07 | pass：done 不被 Hint 重开、合法重建新 job_id、旧 Claim 拒绝；关闭后完整查询保留期与最小身份 | 真实效果、费用、传播、清理的关闭证明 inconclusive |
| FW-08 | pass：真实 scope 拒绝、过期/逃逸 Tx、回调 SQL handle 生命周期、锁序、Work 原 ctx 嵌套、超时回滚；领域修订固定 rejected 无重试/无多余 job | 所有领域隐式锁/权限和公开严格 codec inconclusive |
| FW-09 | pass：固定小负载均排空且事实无重复；实际计量见下节，生产容量 inconclusive | 物理字节/行/锁等待、外部请求/费用、生产恢复净余量 |

领域门禁由可信接入负责：公共 scope 不是任意 SQL 沙箱，Engine 不交给 domain port；上下文 marker 的宿主诊断与 Work 的固定嵌套入口分别报告。临时故障只作用于当前用例的独立数据库、连接和子进程。唯一直接删除作业的断点是明确的迁移遗漏，修复读取原未结事实及有限页，不借原始 job 修改制造成功。

## 小负载计量

固定每场景 32 项责任，并发 2、批次 2、扫描 10ms、租约 1s、续约 200ms；全部关闭本地和 PG 通知。正常、32 次 COMMIT 确认丢失、32 项过期接替（前置 100ms 租约/等待 130ms）、256 条 done 历史分别比较。历史准备不计入测量窗口，最终结果要求 32 个真实事实增量且 open=0。

数据源为实际 Begin→Commit/Rollback 计时、Engine 三态/领取计数、应用 SQL 计数、database/sql 连接等待和 10ms 采样的最老到期责任；采样本身的查询计入 SQL，事务控制、迁移、监听及独立观察 SQL 不计入该 SQL 数。PG WAL 用整个测试集群 insert LSN 增量，含后台写；SQLite 是 WAL 文件大小增量，受分配/检查点影响，零增量不代表零写入。SQLite 单写队列的等待并不计入 Pool WaitCount。

2026-10-02 最终完整 race 套件通过，Go 1.27.1 darwin/arm64；PG 18.6 容器，SQLite 3.53.4。原始记录见 [cost.jsonl](cost.jsonl)。每场景观测 open 峰值为 32、最终为 0；无新到达负载，排空率只计算 runner 排空窗口。

| 适配器 | 场景 | 已建立 Tx / C·R·U 尝试结果 | SQL | Tx p95/p99 ms | 连接等待 次/ms | 最老责任 ms | 排空 项/s | WAL 观测增量 bytes |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| sqlite | 正常/全丢通知 | 81 / 81·0·0 | 785 | 5.95/6.10 | 45/21.53 | 307 | 103.5 | 1685080 |
| sqlite | 提交确认丢失 | 81 / 49·0·32 | 785 | 5.86/6.03 | 45/20.50 | 306 | 103.6 | 1685080 |
| sqlite | 过期接替 | 82 / 82·0·0 | 849 | 7.43/42.42 | 43/32.08 | 468 | 109.1 | 1639760 |
| sqlite | 256 done 历史 | 81 / 81·1·0 | 786 | 7.12/7.33 | 46/79.21 | 311 | 102.6 | 0 |
| postgres | 正常/全丢通知 | 80 / 80·0·0 | 770 | 3.62/11.64 | 0/0.00 | 166 | 193.7 | 99312 |
| postgres | 提交确认丢失 | 80 / 48·0·32 | 770 | 3.26/17.50 | 0/0.00 | 165 | 193.7 | 99312 |
| postgres | 过期接替 | 82 / 82·0·0 | 837 | 5.52/24.64 | 0/0.00 | 328 | 173.5 | 126024 |
| postgres | 256 done 历史 | 81 / 81·0·0 | 771 | 3.38/17.51 | 0/0.00 | 167 | 193.5 | 99792 |

已建立 Tx、Commit/Rollback 调用按实际成功 Begin 与驱动调用计数；C/R/U 是调用方尝试结果，开始前失败也可能记为 R，unknown 可对应已提交，不把它们简单当成物理 COMMIT 数。32 次丢确认负载由真实 COMMIT 后包装层返回 unknown 注入，未包含网络重连或客户端查原回执的成本；真实 PG 网络截断另由 commit_proxy_test.go 验证。这里未预设生产性能阈值，行/物理字节/锁等待及生产负载尚未完整采集。

## 开发与验证

- `make build`：四项 Go 命令与 TS SDK/Web 通过。
- `make check`：Go race/vet、TS、SQL 可复现、全部静态契约/Proto 通过；91 份 Markdown、2202 个本地链接零错误。普通 Go 检查的 PG skip 不作为 PG 通过。
- `make durable-check`：最终完整真实 PG/SQLite race 套件通过（20.056s）；含两个同时存活的独立 PG worker 分领取、崩溃接替、真实 COMMIT 网络确认丢失、旧观察与取消边界。测试库均按本次身份清理。
- `go vet ./...` 与 `git diff --check`：通过。
- 入口/实现/证据文档补充检查：13 份文件、60 个本地链接与代码围栏/引用锚点通过；根 AGENTS.md 保持 99 行。
- `make migrate-postgres` / `make migrate-sqlite`：对原开发数据库显式升级及重复幂等升级通过，均为版本 1、两张公共表；PG 受限应用身份可读取表且 CREATE/superuser 仍为 false。
- 最终状态：隔离测试库残留 0；五角色 live/startup 全部 200、ready 全部 503；Vite 为 200，Docker PG 保持 healthy。

五个默认角色继续运行；领域接入及业务 ready 保持 503，未声明完整阶段 1.2 或生产容量退出。Linux/公司平台、真实供应商及业务恢复未运行。
