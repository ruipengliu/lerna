# 02：参考语义覆盖矩阵

Status: ready-for-agent
Progress: pending
Blocked by: None

## Scope

从五份既有固定源码调研建立简化核心模型的逐项语义覆盖矩阵。新增 `core-model-semantic-coverage.md` 并从研究总索引进入，保留历史基线与 sources.json。

Owned files: `docs/research/agent-harness-comparison/core-model-semantic-coverage.md`、该目录 `README.md`。

## Acceptance

- [ ] 至少覆盖规格规定的 16 个语义族，每个项目的实际路径／配置、原身份、生命周期与恢复含义有可追溯的固定源码证据。
- [ ] Pi 经典 CLI、AgentHarness、pi-durable 分开，其他项目的条件装配也明确；不能按同名 Task／Operation／Run 机械映射。
- [ ] 映射到六组核心及必要子记录、配置或扩展，逐项标记已有设计、本轮补齐、按需扩展、本阶段不交付；运行证据单独记录。
- [ ] 具体指出分支、排队／撤回、继续权、Schedule、环境、子 Agent、Memory 等未覆盖点和所需合同，避免“能够存 JSON 即覆盖”。
- [ ] 固定来源路径与行号可核对；链接和结构检查通过，不改写历史研究快照，不宣称运行等价或性能改善。

## Comments

- 2026-10-01：源码在主工作区 `.reference` 中，子工作树可使用主工作区绝对路径读固定快照。此票仅更新研究映射，不修改公共协议或领域规则。
