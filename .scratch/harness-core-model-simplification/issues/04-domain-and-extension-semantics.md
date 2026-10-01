# 04：领域规则与扩展生命周期

Status: ready-for-agent
Progress: completed
Blocked by: 01, 02

## Scope

把核心对象内聚与保留语义写回实际负责模块，补齐按需能力的归属、生命周期和启用约束。根据 02 矩阵解释覆盖与缺口，避免只在新总览中宣称收敛。

Owned files: `.draft` 中 orchestrator、brain、execution、security、collaboration、memory、extensions、evaluation 的相关 README／implementation 与既有专篇；可新增所属模块内必要短篇；`reliable-work.md`。不改交互模块、根总览、工程导航和研究文件。

## Acceptance

- [x] Task／条件／结果、Decision／输入／调用／提案、Operation／Intent／Attempt／Effect 各自统一管理但保留准确身份、原 owner 与终态后责任。
- [x] Grant／Use／UseSettlement 与 Confirmation 的职责准确；能力、绑定和安装配置保持必要性，受信输入不退化成聊天文本。
- [x] 持续目标与继续权、委派与稳定子会话／激活、Schedule 与 JobStore、环境与 Operation、Memory 与 Content 各自有明确区分。
- [x] 为尚未开放的调度、分支协作、环境复用等写最小语义合同、启用前提和缺口，不擅自增加公开 Schema／方法／状态。
- [x] 当前 ADR 的生产分布式、固定路由、共事务接纳、核验、条件修订、回退批准与预览规则均保留；普通记忆纠正不引入发布流程。
- [x] 避免九套重复规范或新通用状态机；既有正文是规则权威，核心目录负责导航。复用检查并记录运行证据缺失。

## Comments

- 2026-10-01：依赖 01 和 02 已合入。允许精简已有重复说明，避免只追加泛化宣言；勿把跨 owner 意图／接纳事实合并成一份伪原子记录。
- 2026-10-01：从集成分支 a2892b9 建立 codex/core-model-04。复用规格已确定的应用／SDK 工作流与既有静态、协议检查；本票不实现运行代码，不为文档补充私有结构镜像测试。
- 2026-10-01：规则已写回 Task、Brain、Executor、Grant、Memory、Collaboration、Extensions 和 Evaluation 的原说明。主要补齐[当前继续条件](../../../docs/architecture/.draft/orchestrator/implementation.md#bounded-progress)、[子会话与激活](../../../docs/architecture/.draft/collaboration/implementation.md#reusable-child)、[应用定时触发](../../../docs/architecture/.draft/orchestrator/scheduled-triggers.md)及[可复用环境](../../../docs/architecture/.draft/execution/programmatic-tools.md#reusable-environment)；现有一次委派、输入与固定路由合同不被扩展候选替换。
- 2026-10-01：复用文档检查通过 57 份架构 Markdown、1646 个本地链接、124 个 Mermaid 块；协议静态检查通过 55 个正例、371 个反例和 105 个固定方法；Brain 静态向量通过（5 项计划安装、11 项无效生成、15 项计划分支）。git diff --check 通过，机器契约及 ADR 未变。尚无新增运行系统、平台隔离、调度／子会话／环境运行验收或性能数据；本票完成仅指设计交付。
