# 07: 合同 runner 生命周期与有限故障处理

本任务是原六项合同实现之后的架构优化，不替代原六项验收条件。

**What to build:** 开发者继续使用真实 Go / TypeScript 双向合同往返和独立夹具，运行器复用只读初始化；单例拒绝不会中断后续正常夹具，崩溃或超时则明确失败。

**Blocked by:** 01、02、03、04、05、06；另须两轴代码审查的正确性修复验收完成。

**Status:** resolved

- [x] 正确性修复后的固定提交作为基线；同环境、同夹具完整测量前后耗时、实际启动数及结果，不使用粗估代替实测。
- [x] 每语言一个持续 runner、单一在途请求；私有有界帧用 base64 保留原始字节，两端仍进入准确 typed 公开 codec。
- [x] 正例保留两个方向的真实解码／编码和准确独立值比较；负例保留公开拒绝分类，拒绝后正常值仍可处理。
- [x] 每次调用最多 10 秒；错身份、非法／超限帧、stdout 污染、崩溃、缺答复和超时均是基础设施失败，不冒充合法负例、不静默重放。
- [x] 成功、断言失败、超时、取消及退出均有限清理两个进程及临时文件，观察迟到拒绝并限制 stderr 内存。
- [x] 同进程重复值、拒绝后正常值与共享夹具反序结果一致；保留必须首次编译的新进程 Schema 不可变回归。
- [x] 全部原套件及正确性回归继续通过；只增加新生命周期可观察故障测试，不增加产品 interface 或修改方法支持清单。

决定和约束见同目录上层的 architecture-decisions.md；若复杂度扩大或无实测收益，可保留正确性修复并回退本独立优化。

## Comments

2026-10-03，在独立 `codex/contract-runner-lifecycle` 工作树实施，固定正确性基线 `bb9241f9dbf82fa954f0ba15fbe03e49bb5b7fd6`。产品紧凑编码 / Schema 冻结修复保留在先前独立提交，本票据只优化测试工具的 lifecycle。

私有 `scripts/contract-runner.mjs` 集中 start / run / close、一个在途请求、唯一 string id、8 MiB 帧、64 KiB stderr 和每调用 10 秒截止。每个脚本只启动一个真实 Go 和一个真实 TS runner。第一条请求的等待覆盖剩余初始化；没有批次级 10 秒或静默重启。正常 EOF 等待 500 ms，不合作进程 SIGKILL 后最多等待 1.5 秒；成功、断言失败、超时、AbortSignal / SIGINT / SIGTERM 及退出均进入有限清理和临时文件 finally。

Go / TS 增加私有 base64 bytes NDJSON batch mode，保留原单次 CLI；两个工具模式各自共用唯一 typed roundtrip。产品 Schema 和 wire 正文 / 深度限制不变。一次预期公开拒绝可继续，崩溃、超时、错 id、非法响应及协议污染均抛出 infrastructure failure。私有 Go frame decoder 的 missing wire_base64、非法 UTF-8 和大小写别名 ID 三个错误接纳在测试中依次取得 red，随后改为 hardfail；这不把 IPC 规则混入产品合同。

可观察测试：私有 lifecycle 缺失先 red，正常 bytes / classified refusal / repeated bytes 实现后 green；真实 typed batch mode 缺失先因 10 秒截止 red，实施后两语言准确 Revision / 非法 Revision / 非法 UTF-8 正文 / 重复准确值均 green。15 个 lifecycle tests 配正常控制，覆盖超时、崩溃、错 id、坏 JSON / shape / base64、stdout 污染、帧 / stderr 越界、取消、单在途、断言失败 cleanup 和 stdin 不合作；通过已退出 pid 的独立 OS 观察验证没有残留测试进程。原 158 项、68 个正例保留真实双向 typed codec 和原始独立期待值；make test-contract 同时验证完整反序 corpus。Schema 初次编译不可变回归仍由新的 Node test 进程运行。

同环境实际测量（Go 1.27.1、Node 24.19.0、pnpm 12.8.1 / TS 7.0.2）：

| 工作量 | 完整 wall time | 实际 Go / TS fixture runner 启动 | 结果 |
| --- | ---: | ---: | --- |
| 基线前向：158 项 / 68 正例及既有摘要、读取、协商 suites | 119.217268 s | 226 / 226 | passed |
| 本实现相同前向工作量 | 5.903860 s | 1 / 1 | passed |
| 额外完整反序工作量及同样 suites | 5.470951 s | 1 / 1 | passed |
| 新增 15 项 lifecycle tests（含真实工具 build 与协议 probes） | 6.294658 s | 另行测试设施，不混入配对启动数 | passed |

相同前向工作量本次实测约 20.19 倍，减少 95.05% wall time。额外反序与生命周期测试约 11.765609 s，不隐藏新增测试成本，也不把相加的分项测量冒充 make check 的整次 wall time。一次本机配对测量包含 Go build 与原有辅助 suites；不是跨机器、CI 或生产容量保证。启动数由 Node preload 对实际 spawnSync pid / spawn 事件计数，仅匹配真实 typed fixture runner，不把 Go build 或辅助 go test / node --test 当 fixture runner。可重复命令为 node scripts/test-contract.mjs，额外反序为同命令 --reverse，生命周期为 node --test scripts/contract-runner.test.mjs；保留原单次 CLI供未来启动测量。

验证：make bootstrap、最终 make check、go test -race ./... 与 git diff --check 全部通过。check 包含 15 lifecycle / 34 TS tests、13 generator 拒绝 / 8 Schema 变更 goldens、生成零差异、全部 Go tests、158 共同夹具前向与反向双语言往返、46 命令 digest / 28 受信 query / 15 协商案例及两个 Schema goldens，以及双语言构建。未增加产品方法或 public SDK interface，未新增 ADR；整片 01 退出仍由主任务统一核对。
