# 0002 授权区分操作权利与处理目的

| 日期 | 修订说明 |
| --- | --- |
| 2026-10-04 | 初版，来自[架构评审处理记录](../review/archive/round-1/README.md) RV9。 |
| 2026-10-06 | 按[第五轮评审处理记录](../review/disposition.md)修订后果：区分 M1 保留字段、M2 完整检查、M5 验收（A2-01）。决定不变。 |

- 状态：已采纳
- 影响：[项目目标 G8 与术语](../architecture/project-goals.md#3-核心术语)、[核心契约](../architecture/core/contracts/README.md)、[授权](../architecture/core/grants/README.md)、[内容治理](../architecture/core/content/README.md)、[运行记录](../architecture/core/trace/README.md)

## 背景

授权原有的 `purposes[]` 只有读取、保存、同步、行动四值。运行记录和评测却要求"诊断授权不能充当评测授权"。同一主体读取同一份记录，一次用于诊断、一次用于生成 Skill，四值集合无法区分。

## 决定

- 原"用途"改称**操作权利**，字段为 `use_rights[]`，取值不变。
- 新增**处理目的**，字段为 `processing_purposes[]`。首批取值：`CURRENT_TASK`（当前任务）、`PERSONALIZATION`（个性化）、`DIAGNOSIS`（诊断）、`EVALUATION`（评测）、`IMPROVEMENT`（生成改进）。集合带语义版本。
- 缺省不等于全部；新增目的不自动包含在旧授权中。
- 委派只能缩小目的集合；派生内容的目的上限取全部输入的交集，交集为空时不发布。
- 处理目的由受信的任务编排或平台工作流绑定，在读取、模型调用、派生发布和导出时核验。扩展和推理不能靠改参数获得新目的。

## 后果

- 授权的可判定集合 D(g) 多一个维度；委派包含判断、内容用途上限和凭据请求绑定都要带上处理目的。
- M1 在契约中保留 `use_rights[]`、`processing_purposes[]` 两个字段，未知值一律拒绝，处理目的只用"当前任务"；M2 实现完整的检查；M5 在真实运行记录的评测数据流上验收。

## 考虑过的备选

**把目的编进动作名**，例如 `trace.read_for_diagnosis`。每增加一种目的，就要复制全部读取、保存、同步动作，而且目的无法与资源操作自由组合。放弃。
