# 02: 长任务上下文与最终模型请求的来源核验

**What to build:** 设计长任务经过多次压缩和目标修订后，仍能使用准确事实与获准材料形成一次可追溯的模型请求，并解释缺口、费用与结果来源。

**Blocked by:** None (can start immediately).

**Status:** ready-for-agent

**Progress:** completed

- [x] 分清机械重建的硬约束、获准正文、近期事实、候选摘要和最终供应商编码；摘要不能修改目标或效果事实。
- [x] 将最终请求的全部实际字段纳入来源、接收方、用途和尺寸核验，覆盖附加日志、元数据及插件字段，明确禁止保存材料的最小记录边界。
- [x] 记录准确输入版本、策略与编码配置、保留和排除依据，投影落后、来源关闭和必要材料超窗均有明确分支。
- [x] 覆盖三次压缩、目标修订、冲突、原操作无结果、local_only 资料及附加日志外发；合法本地处理和获准披露均有正例。
- [x] 新摘要处理另行准入并计入完整成本；保持单 Decision 至多一次物理模型请求。


## Comments

2026-10-01：设计文档完成。Brain implementation 的 `snapshot-reconstruction` / `final-request-provenance` 集中定义机械重建、内部组装依据、最终全部字段／接收方／尺寸核验、不可追加编码和临时材料分支；记录复用 Snapshot／Decision／ModelCall，不增公共字段或独立权威。两层 Orchestrator 与 Brain README 已导读，公开说明新增 `fixed-decision-input`。新增运行验收向量 BI-17～20，含三次压缩与目标修订、投影／资格／超窗、local_only 与日志／插件字段、禁止保存输入恢复的正反例。

局部自检：11 个新增锚点链接均可定位，受限文件 `git diff --check` 通过；未运行共享验证、数据库／模型／平台实验，运行能力仍未证实。主链摘要、辅助处理、原未知效果及单 Decision 0/1 物理模型请求约束保留。
