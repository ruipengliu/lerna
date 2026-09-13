# 核实现行协议与 Skill 规范的适配边界

Type: research
Interaction: AFK
Labels: wayfinder:research
Status: resolved
Assignee: protocol_research
Blocked by:
Parent: [Harness 技术实现方案决策地图](../map.md)

## Question

现行 MCP、A2A 与 Agent Skills 规范能分别承载哪些任务、能力、上下文、取消、进展和授权语义，哪些本项目要求仍需内部契约或适配？

## Scope

核对官方规范的当前版本、稳定性、可选扩展与互操作限制；不能把 A2A 中的 skill 声明等同于用户 SKILL.md 包，也不能把外部协议直接当作内核任务状态。

## Expected evidence

提供有官方来源的能力矩阵、版本与访问日期、适配缺口及不确定项，区分事实与推荐。

## Output

计划关联文档：`docs/research/harness-protocol-contracts.md`。

## Comments

研究由 protocol_research 在独立 worktree 完成。研究分支：`research/harness-protocol-contracts`；提交：`5325b9353e9604e605a465ffda25a4e53cf937fd`。

## Answer

已核实现行 MCP、A2A 与 Agent Skills 的版本、生命周期、取消、授权及适配缺口。外部协议状态不能直接作为 Harness 任务所有权或动作效果的权威；Skill 内容格式也不提供本项目要求的运行保障。

详细事实、官方来源、版本冲突和未实测边界见 [MCP、A2A 与 Agent Skills 的适配边界](../../../docs/research/harness-protocol-contracts.md)。最终协议与 SDK 选择仍由“定义内部契约与外部协议映射”决定。
