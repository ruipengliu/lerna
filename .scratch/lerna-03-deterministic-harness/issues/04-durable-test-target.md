# 04: 独立目标与原键保证

**What to build:** 测试作者通过独立持久目标的写入、读取和原键查询观察真实测试效果，明确幂等窗口及无查询模式的保证范围。

**Blocked by:** [切片02完整退出](../../lerna-02-durable-work/exit-evidence.md)及[最终端口复核](../final-handoff.md)（均已满足；本片内无前置票）

**Status:** resolved

- [x] 目标与组件／未来Executor账本使用独立持久身份和存储；普通write／read／query及privileged observer有明确测试边界，不能从被测系统自报Effect推断目标状态。
- [x] 正常真实SQLite目标写入可独立读回准确字节与版本；窗口内原键同内容只产生原效果、异内容conflict，接收次数是目标公开事实而非内核私有调用数。
- [x] 窗口起止固定在首次耐久接收，重传／重开不续期；过期返回guarantee_expired而不假承诺安全，尚可能迟到的原责任不因过期被抹掉。
- [x] 有query模式仅报告当前原请求处理事实，not_found不等于永不发生；无query模式普通入口unsupported，privileged observer仍独立可见已写入事实，不能借它补造供应商查询保证。
- [x] 关闭重开保留原请求、窗口、实际值和接收观察；实际SQLite运行版本、WAL／FULL及相关迁移记录来自本票，不能继承02结果冒充新表恢复。
- [x] 测试目标只使用假材料／费用／保证，有闭合、有限的配置及I/O；缺依赖硬失败，正常与拒绝均可复演，不使用真实业务凭据或宣称供应商能力。


## Comments

2026-10-03，root依据授权Astra批准的粒度/真实edges及最终df2dbe5前置退出发布；本票验收尚未实现。已claimed，交独立工作树实施。


## Answer

独立测试目标已实现于 `conformance/internal/testkit/target`，详见[目标 README](../../../conformance/internal/testkit/target/README.md)。普通 Write/Read/原键 Query 与另行打开的只读 privileged Observer 分开；目标有独立文件、application_id、随机耐久 DatabaseID、固定配置 identity 及版本化新 owner 迁移。没有修改生产 adapter、Decision、Executor 账本、旧 1.0 合同或旧 host 迁移，也没有新增依赖。

受测代码 `cac8e9225b0394b5156bf3f83ad2f11b2b709855`（首实现 `51efe7d`，后续 `bd85008`/`6585184`/`cac8e92` 关闭审查问题）。10 项真实 SQLite 测试覆盖正常准确二进制字节/版本及独立观察、Close/Open 恢复、原键同内容重传与异内容 conflict、资源名和字节摘要绑定、固定窗口和准确到期、到期后保留原提交查询、无查询模式 unsupported、独立 DB identity/配置不变、实际 writer 排他、native SQLITE_BUSY 后正常控制、取消 context、拒绝外来数据库和有限配置，以及并发 8 次接收只提交原版本。测试时钟超出 UnixNano 的起止范围被拒绝；初始化失败且 Close 未确认时保留清理 handle 与错误，正常及拒绝测试都先登记 handle 再判断 error。

目标迁移 `0001_target.sql` SHA256 `83333adce6536f21154443a83c969b32ba14845e379df7341f91ea9cbd4bb8c7`，实际 writer Settings 观察 SQLite **3.53.4 / WAL / synchronous=2(FULL) / foreign_keys=1 / busy_timeout=10ms / migration version=1**。这是全新测试目标 owner 的初始化与同版本 reopen，没有虚构旧版目标业务数据升级。原请求/窗口/值/接收观察跨真实 reopen 保存；原 Query not_found 仅指当前处理事实未保存，不证明从未收到或永不会生效。无 query 的 Observer 仅用于 privileged 测试证据。当前 write 同步接收并提交；迟到耐久门闩/故障计划归 05，进程 SIGKILL 归 06。过期不撤销原已发生事实，也不取消潜在迟到原请求责任。本票未声称测到 native Commit 未知、断电耐久、供应商能力或 Executor Effect。

## Comments（实施证据）

2026-10-04：独立工作树 `/tmp/lerna-worktrees/deterministic-harness-04`、分支 `codex/deterministic-harness-ticket-04` 自 clean 基线 `d3f5bc4f93c82d84ed3f775547a6e3b1528c82ed` 开始。已读 AGENTS/CONTEXT/ADR0004/0005、03 spec/decisions/final-handoff/ticket-review、implement-spec/TDD/tests/mocking 及 issue tracker；seams 沿已授权普通目标与 privileged Observer，不另造 production 合同。

垂直 red→green：首次 normal tracer 缺实现编译失败，随后真实写/read/observer/reopen 通过；原键重传的实际 red 为 `UNIQUE constraint failed: target_requests.original_key`，修复后原效果、原窗口及 conflict 接收观察通过；到期 red 错误得到 nil，修复后准确 Deadline 返回 guarantee_expired；no-query red 错误得到 nil，修复后 unsupported 且 Observer 可见实际提交；Settings red 空配置，修复后观察新目标自身真实版本/耐久/迁移；资源名/字节分隔歧义的 red 错误复用了原成功，改为长度前缀摘要后 conflict；错误测试时钟 red 接受 year0001，修复后拒绝超范围起止且正常时间精确恢复。上述真实行为 red 不以缺符号编译失败代替。native Busy、取消、拒绝配置和并发作为已实现行为的后续验核，没有伪造新的 red。

工具：Go **1.27.1**、Node **24.19.0**、pnpm **12.8.1**、Linux、CGO_ENABLED=1、固定 go-sqlite3 **v1.14.52**。受测文件位于 `/workspace` overlay；本环境 `/tmp` 为 tmpfs，仅放 worktree、日志和审查报告，不作 SQLite 耐久证据。`TMPDIR=/workspace/lerna-target-04-tmp`，每个成功测试 scope 即时 fsync 精确路径登记至 `/workspace/lerna-target-04-scopes.log`，没有按前缀或时间猜删。当前 worker 累计 101 个登记 scope 均实际 absent；每个 final normal/race 创建 12 个独立 scope，先确认 observer/外部锁连接/writer Close 后删除自身路径。未操作或声称清除了前置历史未知 PG scope。

按 root 排定的独占非 PG 测试槽，顺序执行（均 exit 0）：

- `go test -count=1 -v -timeout=30s ./conformance/internal/testkit/target`：10 项，**0.356s**。
- `go test -race -count=1 -v -timeout=45s ./conformance/internal/testkit/target`：10 项，**1.483s**。
- `make bootstrap`：锁定工具链及依赖安装；`make check`：格式/vet、生成一致性、全部无 PG tag Go/TS 测试、双方 158 合同夹具正反序与真实往返、构建通过。
- `go mod verify`：全部 modules verified；四组历史 durable-work SHA256 清单 **27 项**全部 OK。

独立 Standards/Spec/Architecture 审查实际执行。Standards 初次 2 P2（时钟起止编码、observer 初始化失败 Close/清理责任），包括后续拒绝路径 handle 登记与错误保留均由原 reviewer 在 `cac8e92` 复核关闭，最终 0 open；Spec 0、Architecture 0。报告 `/tmp/lerna-03-ticket-04-standards-review.md`、`/tmp/lerna-03-ticket-04-spec-review.md`、`/tmp/lerna-03-ticket-04-architecture-review.md`。实现/环境/命令/限制详细证据 `/tmp/lerna-03-ticket-04-evidence.md`；实际签名、迁移和 05 接法 `/tmp/lerna-03-target-handoff.md`。全部 exec/test sessions 已结束。结束前已 merge 最新 root integration `d3f5bc4`，结果 Already up to date；本票只关闭自身 6 AC，整合/push/整个切片 03 最终验收由 root 负责。
