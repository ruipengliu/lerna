# 核实外部协议 Go SDK 与 Skill 格式锁定依据

Type: research
Interaction: AFK
Labels: wayfinder:research
Status: resolved
Assignee: extension_sdk_research
Blocked by:
Parent: [Harness 技术实现方案决策地图](../map.md)

## Question

既定 MCP 2026-07-28、A2A 1.0 与 Agent Skills 内容兼容目标，能否通过官方 Go SDK 或固定 Schema 实现；有哪些可锁定的发布版本/提交及必须独立补齐的差异？

## Scope

只核对官方仓库、规范和发布记录，提供日期、版本/提交及兼容证据；若官方 SDK 尚未支持目标，明确差距，不重新选择目标或把最新版本自动当作兼容。固定 Skill 格式快照来源及引用方式，不要求实现或安装 SDK。

## Output

`docs/research/harness-extension-sdk-and-skill-snapshots.md`。

## Comments

由扩展生命周期票细化时产生的事实问题。具体运行器另由独立研究票核对；此票不决定用户权限或生命周期策略。

## Answer

已完成官方发布、源码和格式快照核对，报告：[外部协议 Go SDK 与 Skill 内容快照锁定依据](../../../docs/research/harness-extension-sdk-and-skill-snapshots.md)。研究分支 `research/harness-extension-sdk`，worktree `/tmp/lerna-wayfinder-extension-sdk`，报告提交 `955f019d8c56d372bf3c66efad17ba42af03653c`；已整合至主工作区，入口链接以票据标题显示。

- MCP Go SDK v1.7.0 已有 2026-07-28 的 stdio/Streamable HTTP 双角色证据；HTTP 需 Stateless 配置，严格版本选择及取消传播不能依赖默认行为，核心 SDK 的支持不代表独立 Tasks 扩展已支持。
- A2A Go SDK v2.5.0 有协议 1.0 HTTP+JSON 双角色实现；默认客户端优先其他绑定，需显式限定 REST。规范推荐媒体类型与 SDK 错误解析存在已证实差异，须定向适配并实测。
- 两套 SDK 均要求最低 Go 1.25.0；依赖模块不等于独立基础设施，具体引入仍须记录 go.mod/go.sum 及局部补丁。
- 已固定 MCP Schema、A2A proto/文字规范及 Agent Skills 格式的完整提交与原始文件摘要。Skill 格式快照不替代每个内容包的摘要，不定义运行授权、安装事务或完整脚本环境。

没有安装 SDK、生成接口或运行互操作测试；研究报告不构成已兼容承诺。版本与适配方案由 [确定插件、Skill 与外部 Agent 的接入生命周期](12-extension-lifecycle.md) 采纳，实际支持由相应一致性测试证明。
