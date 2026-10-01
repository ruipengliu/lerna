# 02：参考语义覆盖矩阵

Status: ready-for-agent
Progress: completed
Blocked by: None

## Scope

从五份既有固定源码调研建立简化核心模型的逐项语义覆盖矩阵。新增 `core-model-semantic-coverage.md` 并从研究总索引进入，保留历史基线与 sources.json。

Owned files: `docs/research/agent-harness-comparison/core-model-semantic-coverage.md`、该目录 `README.md`。

## Acceptance

- [x] 至少覆盖规格规定的 16 个语义族，每个项目的实际路径／配置、原身份、生命周期与恢复含义有可追溯的固定源码证据。
- [x] Pi 经典 CLI、AgentHarness、pi-durable 分开，其他项目的条件装配也明确；不能按同名 Task／Operation／Run 机械映射。
- [x] 映射到六组核心及必要子记录、配置或扩展，逐项标记已有设计、本轮补齐、按需扩展、本阶段不交付；运行证据单独记录。
- [x] 具体指出分支、排队／撤回、继续权、Schedule、环境、子 Agent、Memory 等未覆盖点和所需合同，避免“能够存 JSON 即覆盖”。
- [x] 固定来源路径与行号可核对；链接和结构检查通过，不改写历史研究快照，不宣称运行等价或性能改善。

## Comments

- 2026-10-01：源码在主工作区 `.reference` 中，子工作树可使用主工作区绝对路径读固定快照。此票仅更新研究映射，不修改公共协议或领域规则。

- 2026-10-01：完成[语义覆盖矩阵](../../../docs/research/agent-harness-comparison/core-model-semantic-coverage.md)及研究索引入口；16 个语义族分别覆盖 C、P-C、P-H、P-D、D、R、K，共 112 条实际路径对照，另列 7 项后续合同差异。
- 2026-10-01：特别保留既有 interaction.input_withdraw 的 queued／sending 竞争和 withdrawal_requested；未公开缺口限定为 Session 自由输入队列、steering／follow-up 与 Run 范围控制。未改变领域方法、Schema、历史 sources.json 或固定参考仓库。
- 2026-10-01：应用 tdd 技能的外部行为与既有切面原则，本票仅交付文档；复用文档检查器检查研究目录 9 个 Markdown、179 个本地链接、1 个既有 Mermaid 块，0 错误。一次性静态核对确认 16 × 7 条覆盖、209 个固定源码引用路径／行号有效、5 个固定源码快照干净；git diff --check 通过。运行场景保持待验证，未新增镜像测试或运行上游／数据库／性能试验。
