# 实现进度

集成分支：`codex/lerna-implementation`。实现前基线：`1c042ec`。

2026-10-03 用户授权按依赖实现全部切片，逐片架构审查、提交并推送；需要决策时由 `gpt-6-astra`、`high` 智能体代为分析决定。后续 tickets 的粒度和依赖决策沿用此授权，不重复要求用户确认。测试沿用各规格已确定的 Application／Component／Host 公开验证范围。

## 当前状态

- 切片 01：6 个 tickets 已确认。任务 01–05 已 resolved 并合入集成分支（`45b90ad`、`c0e12f7`、`3c156a6`、`a96f49d`、`b414092`）；任务 06 正在实现。其他切片仍为 not-started。
- 任务 01 证据：锁定依赖干净重装、`make check`、`make test-race` 通过；41 项共同正反夹具及合法值的真实 Go→TS／TS→Go 往返通过。详细记录见[任务 01](../lerna-01-command-contracts/issues/01-exact-contract-roundtrip.md#comments)。这些只证明公共值合同范围。
- 任务 02 证据：`make bootstrap`、`make check`、`make test-race` 通过；共同语料共 109 项（41 值、68 命令），两端错误分类及合法输入真实往返通过；6 项生成器拒绝探针通过。详细记录见[任务 02](../lerna-01-command-contracts/issues/02-strict-command-validation.md#comments)。已按决策消除 TypeScript 内部模块反向导入公共 facade 的循环依赖。
- 任务 03 证据：`make check` 通过；46 项共同摘要案例（31 独立预期、15 拒绝），覆盖 UTF-16 键排序、业务／主体变化、重传及 1 MiB 边界。详细记录见[任务 03](../lerna-01-command-contracts/issues/03-canonical-command-digest.md#comments)。
- 任务 04 证据：合并后 `make check`、`make test-race`，最终别名修复后 `make lint test build` 与生成一致性通过。共同语料为 153 项值／命令／响应往返夹具，加 46 项摘要案例；最终 21 个 TS 测试。详细记录见[任务 04](../lerna-01-command-contracts/issues/04-receipt-and-readonly-query.md#comments)。只读接口返回独立观察，并保留独立的原输入引用，避免调用方或回调修改绑定及源事实；受信入口仍待任务 05。
- 任务 05 证据：`make check`、`make test-race` 通过；补充拒绝案例后 Go / TS 受信查询套件与 lint 通过。28 项共同权限夹具覆盖租户、委托、固定 owner、目录接替、原身份与截止；额外验证只读观察、回调隔离及取消。详细记录见[任务 05](../lerna-01-command-contracts/issues/05-authenticated-owner-isolation.md#comments)。Go 协作者须遵守有限 context；TS 单一总计时器限制整个查询，不能强制终止不合作的外部回调。这些不构成真实网络认证或持久事实查询证据。
- 切片完成必须有实际验收证据；规格明确、任务发布和代码合并均不等于验收完成。
- 真实外部接入、独立组件、生产故障域、评测样本与容量结论按验收矩阵分别记录，不用本地模拟结果代替。

## 过程约定

- 每个实现任务使用独立 worktree／分支，先核对集成分支基线，采用 TDD 后合并集成分支最新提交再报告。
- 任务依赖以实际完成记录为准；集成后运行相关验证及两轴代码审查，修复后关闭任务。
- 每片结束执行架构审查，按用户授权由决策智能体选择需要立即解决的问题，保留审查报告与决定，不增加尚无使用者的抽象。
- 记录命令、环境、准确版本、结果和限制后更新切片及验收索引，提交并推送。
