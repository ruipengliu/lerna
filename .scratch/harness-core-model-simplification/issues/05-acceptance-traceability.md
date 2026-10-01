# 05：验收场景与规格追踪

Status: ready-for-agent
Progress: in-progress
Blocked by: 02, 03, 04

## Scope

在现有应用／SDK 工作流切面编写核心模型收敛的行为验收，复用已有 HAR、FW 和模块断点；建立规格用户故事到设计与验收的完整追踪。

Owned files: `docs/architecture/.draft/validation/core-model-scenarios.md`、`validation/README.md`；`.scratch/harness-core-model-simplification/traceability.md`。

## Acceptance

- [ ] 54 条用户故事与 24 项实施决策均有设计承载／明确缺口、验收入口、预期外部行为和运行状态；可按语义组映射，不写 54 套重复测试。
- [ ] 包含四类基础请求以及原提交恢复、分支、撤回范围、继续权、未知效果、父子激活、定时重复、环境停止、记忆／权限等场景。
- [ ] 优先沿现有主入口观察行为与独立目标真值，复用既有正反例；不新增运行测试代码、内部字段镜像测试或未经同意的新测试专用 seam。
- [ ] 区分实际静态检查、待运行机制检查、数据库／平台验证、模型质量与性能；不将设计覆盖勾为运行通过。
- [ ] 规格变更与契约差异有明确检查条件，未触及机器资产时不重复声称运行验证。

## Comments

- 2026-10-01：在独立工作树从 `e35fa9b` 建立 `codex/core-model-05`，依赖 01～04 已完成；开始按真实设计入口建立行为场景和规格追踪，运行验证保持待实施。
- 2026-10-01：依赖实现内容合入后编写，便于逐条引用真实交付而非假想文件。
