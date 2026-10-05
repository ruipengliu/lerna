# 19: 内容治理：来源、版本与派生

**What to build:** 工具结果、上下文、模型输出和辅助产物从第一天起登记来源、版本和派生关系；缺少来源的持久产物不能发布；删除请求明确返回"不支持"。

**Blocked by:** 06（出口与效果：一次 API 调用经出口闸门）

**Status:** resolved

- [x] 内容身份：用户、内容和版本标识、来源、取得时间、关联任务和动作；更新产生新版本，旧引用稳定
- [x] 派生关系登记实际输入和输出；缺少来源的持久产物不得发布
- [x] 内容登记与派生产物按 R7 交接；回执丢失可恢复，重复登记结果一致，暂存不可见
- [x] 一个可信的参考正文后端
- [x] 删除返回"不支持"，不确认为已完成

## Comments

- 2026-10-05: ticket19 agent claimed on codex/m1-ticket19, base 6701126; public harness and fault seams already confirmed.

- Implementation WIP: structured source/acquisition metadata, immutable logical version history, trusted body receiver with R7 publication, Stage/raw observation migration, host-captured derivation lifecycle and producer fencing, explicit unsupported deletion. Public tests use assembly and independent body-holder receipts.
- Preliminary evidence: `go test -race ./...` passed; `make lint check-rules check-gen` passed; complete nonstorage fault suite passed in 178.099s, including new 9-case original registration and 21-case derivation crash/receipt matrices. Final integration merge and exclusive `make check` remain pending; this issue is not resolved yet.

## Answer

实现了不可变内容版本及结构化来源、实际取得时间、任务/动作/尝试关联、正文位置与完整性记录。原始内容和派生产物均经登记、可信正文持有方独立受理、查询原回执和原子发布；暂存内容不可读，旧引用稳定，同字节的独立来源保留独立身份。宿主在计算前固定派生责任和输出位置，在读取正文前登记实际输入，封存后才能提交；接管保留责任和输入集并隔离旧生产者。现有 Stage 和可信 raw observation 接入同一正文后端，保留原始 binary/empty/NUL 字节与受信出处。删除明确返回 UNSUPPORTED，不改变内容状态。

- 2026-10-05 final validation: merged integration base `47524c884ee12c5fbbf623d5fcdff07dfc9c6e93`; frozen tested tree `bda7ab54bfe6410f4888ddc53b3364498e22025a`. `make check-fmt lint test check-rules check-gen` passed; complete nonstorage fault preflight passed in 216.605s. Exclusive full `make check` passed, including the complete storage/crash matrix and fault suite in 2062.025s; no cuts or corruption policies were removed or deduplicated. Log: `/tmp/lerna-m1-ticket19-final-check.log`.
- Coverage includes immutable and branching versions, source distinction, source/task/operation validation, exact raw bytes, staged invisibility, body-holder receipt recovery, 9 original registration crash cases, 21 derivation crash cases, actual-input completeness, producer fencing, and explicit unsupported deletion. No M2 memory, purpose-policy engine or deletion propagation is claimed.
- This completion record is the only change after the frozen tested tree; integration is coordinated by the parent agent before the ticket is reported Done.
