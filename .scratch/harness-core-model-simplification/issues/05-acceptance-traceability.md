# 05：验收场景与规格追踪

Status: ready-for-agent
Progress: completed
Blocked by: 02, 03, 04

## Scope

在现有应用／SDK 工作流切面编写核心模型收敛的行为验收，复用已有 HAR、FW 和模块断点；建立规格用户故事到设计与验收的完整追踪。

Owned files: `docs/architecture/.draft/validation/core-model-scenarios.md`、`validation/README.md`；`.scratch/harness-core-model-simplification/traceability.md`。

## Acceptance

- [x] 54 条用户故事与 24 项实施决策均有设计承载／明确缺口、验收入口、预期外部行为和运行状态；可按语义组映射，不写 54 套重复测试。
- [x] 包含四类基础请求以及原提交恢复、分支、撤回范围、继续权、未知效果、父子激活、定时重复、环境停止、记忆／权限等场景。
- [x] 优先沿现有主入口观察行为与独立目标真值，复用既有正反例；不新增运行测试代码、内部字段镜像测试或未经同意的新测试专用 seam。
- [x] 区分实际静态检查、待运行机制检查、数据库／平台验证、模型质量与性能；不将设计覆盖勾为运行通过。
- [x] 规格变更与契约差异有明确检查条件，未触及机器资产时不重复声称运行验证。

## Comments

- 2026-10-01：文档提交 `21525022ccc4999b1947bed0affd259acb400a93` 完成[CM-01～17 场景及 CM-S1～S5 静态检查](../../../docs/architecture/.draft/validation/core-model-scenarios.md)、[US／ID 追踪](../traceability.md)和验收总入口。复用 HAR、FW 与模块场景；C 明确分为历史及原责任重建、恢复后继续，后段不混入历史研究的冷打开计数。
- 2026-10-01：实际静态验证通过：`python3 docs/architecture/.draft/validation/check_documents.py` 为 56 篇 Markdown／1749 本地链接／104 Mermaid／0 错误；同一检查器将 ROOT 指向本 feature 后为 9 篇／220 本地链接／0 错误。独立编号检查核对规格原编号，US-01～54、ID-01～24 各恰一行且含设计、验收、预期行为和未验证运行状态；CM-01～17 锚点齐全。暂存全部四个 owned 文件后 `git diff --cached --check` 通过。
- 2026-10-01：差异检查确认方法登记仍为 105 项；相对起点 `e35fa9b` 及固定 review base `1b647dc970317173f349296e47efd41a4cf3a23c`，contracts（含 Schema、方法、示例、proto）、ADR 和历史 sources.json 均无变更。完成前已合入最新集成分支检查，tip 为 `e35fa9b770f13a5ff321167a703ddf5a6c889294`，返回 Already up to date。
- 2026-10-01：本票据只交付设计与上述静态证据，没有运行内核、SDK、数据库、平台、上游测试、模型质量或性能实验，也没有 red／green 运行记录。G-01～05 的完整公共能力仍待合同及实现；G-06／07 按需启用。总览与工程导航由 06 整合，未计为本票据已经完成。
- 2026-10-01：在独立工作树从 `e35fa9b` 建立 `codex/core-model-05`，依赖 01～04 已完成；开始按真实设计入口建立行为场景和规格追踪，运行验证保持待实施。
- 2026-10-01：依赖实现内容合入后编写，便于逐条引用真实交付而非假想文件。
