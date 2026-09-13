# 核实能力评测的统计与比较方法

Type: research
Interaction: AFK
Labels: wayfinder:research
Status: resolved
Assignee: evaluation_statistics_research
Blocked by:
Parent: [Harness 技术实现方案决策地图](../map.md)

## Question

千级 API 的二值首次正确率及有界任务成功率，如何区分逐 API 覆盖、总体比例、统计区间与版本改进证据；重复样本、成组任务和反复使用评测集会使哪些常见判定失效？

## Scope

从一手统计资料和原始研究核对二项比例区间的条件、按 API 分组时的聚合/相关性、基线与候选配对比较及评测集污染。给出适用于本项目的候选方法与局限，不替用户决定采样规模、发布门槛或模型预算，不实施评测系统。

## Output

`docs/research/harness-evaluation-statistics-and-comparison.md`。

## Comments

由观测评测与自进化决策产生的独立事实问题，研究在独立 worktree 的 research 分支保存。根会话继续处理观测、评测与发布的职责及数据生命周期。

## Answer

已完成一手资料核对，报告：[能力评测的统计与版本比较](../../../docs/research/harness-evaluation-statistics-and-comparison.md)。研究分支 `research/harness-evaluation-statistics`，worktree `/tmp/lerna-wayfinder-evaluation-statistics`，报告提交 `431c52035229a7418e02617168a13dce406402b0`；已整合至主工作区。

- 二项比例区间依赖适用的独立采样假设；同源改写、重跑与共享状态不能直接算作独立样本，使用精确算法也不能修复错误假设。
- 逐 API 覆盖、宏平均、微平均及跨目录泛化是不同问题；固定目录应保持所有 API 与预先选定权重，组合任务不复制为多个独立任务结果。
- 以模板族等真实相关单元重采样与以 API 整簇重采样估计的对象不同；独立簇不足或交叉关系未处理时须表达统计推断不适用。
- 候选与基线保留逐例配对；达标、改善、非劣及其统计证据不能互相替代。多候选筛选与保留集反馈会引入适应性偏差，换种子或摘要不能恢复未使用状态。

未选择新门槛、样本常数或统计实现库；未运行模型评测、统计仿真或样本量计算。具体采样、比较与发布政策由 [确定观测评测与自进化的发布闭环](13-observation-evolution.md) 决定。
