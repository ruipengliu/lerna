# 03 · 规则决策与可查询故障目标

Status: ready-for-agent
Phase: A
Implementation: in-progress
Depends on: [01](../lerna-01-command-contracts/spec.md)、[02](../lerna-02-durable-work/spec.md)

## Problem Statement

后续实现需要可重复制造丢回执、迟到效果和错误证据的目标，直接依赖真实模型与供应商会让恢复测试不稳定。

## Solution

提供符合 Component 合同的规则决策引擎和可查询模拟目标，能按固定输入产生结构化提案、记录目标实际状态并注入故障，作为所有业务切片的确定性基线。

## User Stories

1. As a framework developer, I want deterministic proposals, so that recovery failures are reproducible.
2. As a test author, I want observable target state, so that a receipt cannot masquerade as an effect.
3. As a test author, I want a write followed by a lost response, so that reconciliation is exercised.
4. As a test author, I want delayed and duplicate messages, so that ordering assumptions are exposed.
5. As a component author, I want malformed proposal fixtures, so that invalid combinations are rejected.
6. As an operator, I want restartable test services, so that persistent state can be checked across crashes.
7. As a tester, I want queryable and unqueryable targets, so that unknown effects are not hidden.
8. As a reviewer, I want bounded recorded scenarios, so that simulations are not mistaken for production evidence.

## Implementation Decisions

- 实现 decision_engine.decide、get、cancel 的规则基线，固定 Decision、Snapshot、版本、限额与截止；不调用真实模型。
- Proposal 带准确 decision_ref、snapshot_ref、目标与控制修订、processed_source_refs；互斥候选采用闭合 Schema。条件实质变化优先于同提案其他候选。
- 模拟目标支持读取、写入、按原键查询、幂等窗口，以及无查询保证的模式；自身记录实际效果与接收次数供测试独立观察。
- 故障计划可选择提交前后断开、丢答复、延迟、重复、乱序和进程终止，保留可重演的输入及种子。
- 模拟目标独立于 Executor 的事实存储，不能让测试根据被测系统的 Effect 自报值判断是否真的写入。
- 模拟服务只用于测试环境，不持有真实业务凭据；基线声明的能力与费用为测试夹具，不转化为供应商保证。

## Testing Decisions

通过公开 Component 合同驱动规则引擎；通过独立模拟目标的观察接口核验结果。复用 01 的 Schema 套件和 02 的进程恢复设施；故障注入属于环境设施，不增加生产业务方法。

测试只断言公开行为、持久恢复结果和独立目标状态；不依赖私有表布局或内部调用次数。每个故障或拒绝案例必须配允许正常完成的对照。

验收条件：

1. 相同固定输入按规则得到可预测 Proposal，原 Decision 重传不重复接纳；完成提案与必要内容可恢复查询。
2. 一个提案同时要求行动、输入和完成时被拒绝；有依赖的行动不能伪装为四项独立行动。
3. 目标实际写入但丢响应时，独立查询证明已写；延迟生效与未找到原请求不能被夹具当成未发生。
4. 幂等窗口内重传只保留原效果；窗口过期或无查询模式明确暴露保证不足。
5. 模拟目标和规则组件重启后保留原身份与记录，故障计划可重复执行。
6. 无故障对照能正常返回；每个故障运行都有明确结束条件，不依赖无限等待。

## Out of Scope

- 真实搜索、供应商请求、真实用户文件写入。
- 用模拟结果证明质量、生产容量或供应商幂等能力。

## Further Notes

2026-10-03，前置01及02均完整退出；02受测源码f56d930、准确整合/CI5548744、退出文档df2dbe5。按授权代理批准的六票42AC与[最终接法](final-handoff.md)采用[决定](decisions.md)、[存储](storage-handoff.md)、[容量](capacity-handoff.md)，root发布独立issues并启动01与04；真实图whole02→01/04、01→02/03、04→05、01+05→06。1.1.0/Decision/独立目标尚未验收；对外完整范围仍为原1.0.0 command.get。完整profile广告/整片审查及最终CI由root在六票退出后核实，不是06隐藏业务依赖。

A 阶段在 01–03 完成后退出：共同身份、两种数据库耐久工作与确定性故障设施均可运行，不开放真实副作用。

依赖项表示实现先决条件；`ready-for-agent` 表示规格已明确，不表示依赖已完成或能力已开放。全部验收通过并附准确版本、环境、命令、结果和限制后，才可将本切片记为完成。共同执行与证据规则见[切片索引](../lerna-implementation/README.md)。

依据：[validation](../../docs/architecture/validation.md#工程切片与退出条件)、[validation](../../docs/architecture/validation.md#故障与并发反例)、[capabilities](../../docs/architecture/capabilities.md#决策引擎只形成下一步提案)、[contracts](../../docs/architecture/contracts.md#能力模块方法)、[ADR-0002](../../docs/adr/0002-stable-kernel-replaceable-strategies.md)、[ADR-0004](../../docs/adr/0004-transactional-durable-work.md)、[ADR-0005](../../docs/adr/0005-explicit-effect-uncertainty.md)。
