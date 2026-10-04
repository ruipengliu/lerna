# 05: 迟到效果与耐久故障计划

**What to build:** 测试作者能有限、可重复地制造提交边界断开、丢响应、迟到、重复和乱序，并用目标原事实区分答复与实际效果。

**Blocked by:** 04 独立目标与原键保证

**Status:** resolved

- [x] 私有环境fault plan保存准确输入、seed、step／event身份、有限次数、阶段、deadline和耐久cursor；故障选择不进入生产业务payload，不建立无限通用工作流。
- [x] 提交前断开配正常对照且无部分目标效果；提交后丢响应时独立query／observer证明已经写入，不能把通道断开当未执行。
- [x] 耐久门闩暂停已接收尚未生效原请求，先query not_found再受控释放，observer证明迟到写入；窗口过期或无query时不能夹具判定未发生或安全换键。
- [x] 重复、乱序和延迟事件保留原身份，有有限推进和准确预期，不靠任意长sleep或内部调用次数；合法正常事件仍能完成。
- [x] 同一持久scenario重启从原cursor继续，不从seed再发已完成写入；新隔离scenario用相同seed／input可复演，事件与目标本地阶段在声明事务边界内保存。
- [x] 所有故障及无故障对照都有截止、清理与记录；冻结可重演输入及实际观察，仅证明测试目标和本地故障范围，不当作外部供应商／生产证明。


## Comments

2026-10-03，root依据授权Astra批准的粒度/真实edges及最终df2dbe5前置退出发布；本票验收尚未实现。等待上述直接前置resolved，不能以spec ready替代实现依赖。

2026-10-04：直接前置04已resolved，最终受测代码cac8e9225b0394b5156bf3f83ad2f11b2b709855／clean tip792ee859424800a67501ea6c8b2291cde9c696cc经独立merger整合为f11135b24b67057a6cb87e68d87b848cc5ddd230；6AC、真实SQLite normal/race、基础检查和独立评审均有[前置票证据](04-durable-test-target.md)。本票现claimed，在新独立工作树实施，当前6AC仍未验收。实际接口交接/tmp/lerna-03-target-handoff.md，实施上下文/tmp/lerna-03-ticket-05-implementation-handoff.md；冻结0001，新增实际需要的0002和耐久阶段，不继承04通过冒充故障计划完成。


## Answer

有限耐久故障计划随独立目标实现在 `conformance/internal/testkit/target`，详见[目标与故障计划 README](../../../conformance/internal/testkit/target/README.md)。私有环境 InstallPlan/RunEvent 固定 scenario、准确 uint64 seed、输入、闭合 Kind、唯一稳定 event ID、1–64 步、总材料 4 MiB、期限和 cursor；普通 Request 不携带故障选择，没有新增生产方法或通用 workflow。已完成 event 返回原结果/错误，不重新发请求；未来 event 返回 out_of_order；相同 scenario 配置变更 conflict。新隔离文件使用相同 seed/input/steps 复演，DatabaseID 保持独立。Seed 记录作者选择来源，执行器读取耐久明确步骤，不从 seed 重生成已完成步骤。

ReceiveOnly 同事务保存原请求、首次固定窗口、准确 pending 字节、received 观察与 event/cursor；普通 Query 可当前 not_found，privileged Observer 独立显示 Pending。ApplyReceived 核对原键与原摘要，显式释放同一原请求，实际提交字节/版本、删除 pending 与 event/cursor 同事务完成，窗口过期或无 query 不取消原责任。普通 Write 对 pending 返回 pending/conflict/guarantee_expired 并保留目标接收观察，不续期、不偷偷 apply；计划到期拒绝新步骤且保留 pending。Observer.Plan 与 Observer.Observe 都是独立只读测试事实，不可补造普通 provider 查询保证或 Executor Effect。

DisconnectBeforeCommit 实际准备目标 SQL 后 Rollback，独立 query/read/observer 均无此次部分目标事实；确认回滚后，另一个短事务保存 rolled_back event/cursor，再返回注入的断开错误。两次事务之间的进程故障可重做该回滚步骤，不能声称回滚及失败记录原子。DropResponse 在目标事实、event/cursor 实际同事务 Commit 后返回 response_lost，query/observer 证明已写；reopen/重复同 event 恢复固定原错误，不再发送。SQLite/context 错误保留真实原因，未提交步骤不假消费 cursor；未测 native Commit 未知故障。

冻结已发布 04 `0001_target.sql`（SHA256 `83333adce6536f21154443a83c969b32ba14845e379df7341f91ea9cbd4bb8c7`）。新增实际 `0002_plans.sql`（SHA256 `61e7e6927f22c3046db20ca8ed2c42c19ad806cc64bef431776472e15364c3f0`），明确升级 v1-only 迁移 ledger/receive CHECK 并保留原行与序列，再增加 pending/plan/event。真实历史 writer 使用[冻结 04 源](../../../conformance/fixtures/deterministic-target/v1/README.md)（准确原代码 cac8e92，三个 hash 验证），只机械改包名与 SQL 内嵌，在根唯一 module 编译并实际 normal write/replay/observe/Close、确认 child exit，再由 05 打开同一 overlay 文件升级。原 DatabaseID、窗口、摘要、字节、版本、接收观察及 0001 校验保持，升级后新 receive/apply 正常增加版本。自己的实际 SQLite 为 **3.53.4 / WAL / FULL(2) / FK(1) / 10ms busy timeout / migrations 1+2**，不借 04 的通过冒充新迁移结果。

## Comments（实施与审查证据）

2026-10-04：新独立 worktree `/tmp/lerna-worktrees/deterministic-harness-05`、branch `codex/deterministic-harness-ticket-05` 从 root 正式前置 checkpoint `270ae4ae8abe2c5245328da2e5c4cdfe514ecf40` 开始，初始 clean；旧 04 worktree/branch 未修改或删除。实际受测源码 **`a0198ed70af535c1402f2f95d04de6ae8fe44fe3`**（实现 ccf5628、补验核 2c986ac、修复 a0198ed）。最终 merge 最新 root `c7d46c30deafde548616de474054ddb92f6c3284` 得到 `b34d92d`；该 root 增量只含 progress 文档 2 行，产品/测试代码与准确受测 pin 相同，不重跑未变化 suite。整个 03 仍 in-progress，06 仍需 01+05 真正退出；本票只关闭自身 6 AC。

垂直真实 red→green：初始 ReceiveOnly 暂用 normal Write，实际 Query 错误返回成功（nil error），实现持久 pending 后 Query not_found、observer pending、reopen cursor 和到期后的原键迟到 apply 全通过；before-commit 暂按 normal 写而错误返回 nil，改为实际 SQL Rollback + 独立失败 cursor 事务后无部分事实并正常下一步提交；drop-response 暂 normal 成功错误返回 nil，改为实际 Commit 后固定错误，独立 query/observer、reopen/重复 event 保留原效果通过。审查 UTF-8 red 实际接受 `a\xff` 原键并静默替换身份，修复后拒绝非法 UTF-8，合法中文/NUL 资源和任意内容字节准确保存。以上均实际可运行行为失败，不用缺 package/符号编译失败冒充故障 red。重复/乱序、有限拒绝、并发 event、无 query/截止和真实已发布升级用于验证已实现保证，不伪造新 red。

最终顺序检查均 exit 0，使用 root 授予的独占非 PG 槽（没有与其他 broad/race/PG 测试竞争）：

- `go test -count=1 -v -timeout=60s ./conformance/internal/testkit/target`：**19 tests / 23 scopes / 2.264s**。
- `go test -race -count=1 -v -timeout=90s ./conformance/internal/testkit/target`：**19 tests / 23 scopes / 3.615s**。
- `make bootstrap`、`make check`：锁定安装，格式/vet、生成一致性、全部非 integration-tag Go/TS 测试、158 共同夹具正反序和实际双方往返、构建均通过。
- `go mod verify`：全部 modules verified；历史四组 durable-work 27 hash 与新增冻结 04 writer 三项 hash 均通过。当前 0001 hash 未变。

Go **1.27.1**、Node **24.19.0**、pnpm **12.8.1**、Linux、CGO_ENABLED=1、go-sqlite3 **v1.14.52**；没有新增依赖/PG 服务或真实凭据。SQLite 路径为 `/workspace` overlay，`TMPDIR=/workspace/lerna-target-05-tmp`；`/tmp` tmpfs 只存代码工作树、日志和审查文档。每个 scope 即时 fsync 精确登记到 `/workspace/lerna-target-05-scopes.log`，本 worker 累计 **120 个 distinct 登记 scope 均实际 absent**。每个 final normal/race 各 23，未猜删前缀/时间/未知 PG scope。正常 cleanup 先确认 observer/writer/历史 writer child exit；尝试过的历史 Go build 若失败/超时，保留准确 scope，因为直接 Go ProcessState/WaitDelay 不证明编译后代全部结束。成功构建才确认编译收束；没有声称实际注入该构建超时或 native Close 失败。所有 exec/test sessions 已结束；完整槽已释放给 01，最后只写证据，无未变化代码重复测试。

独立 Standards 与 Spec 审查实际执行：Standards 在 2c986ac 发现两个 P2（JSON 改变非法 UTF-8 身份；历史 build 取消时后代退出未确认）；原 reviewer 在准确 **a0198ed** 复核两项关闭、最终 0 open。Spec 2c986ac 为 0，a0198ed fix recheck 为 0；Architecture a0198ed 为 0。第一次 spec followup 因 agent thread limit 失败，未冒称已 review；standards 完成后重试成功，实际串行完成。报告 `/tmp/lerna-03-ticket-05-standards-review.md`、`/tmp/lerna-03-ticket-05-spec-review.md`、`/tmp/lerna-03-ticket-05-architecture-review.md`。详细证据 `/tmp/lerna-03-ticket-05-evidence.md`；06 实际阶段/0002/原键/cursor/API 交接 `/tmp/lerna-03-fault-plan-handoff.md`。

此处只是本地测试环境注入的断开/丢响应/迟到与正常历史 writer 的真实重开和升级。未做 06 SIGKILL、断电/跨区恢复或外部供应商验证，没有拿 plan 结果替代目标事实，没有 production/Task/Executor 权威、profile 广告或全片退出声明。root 负责最终 merge/push 与后续 06，worker 不 push/PR/删除 worktree。
