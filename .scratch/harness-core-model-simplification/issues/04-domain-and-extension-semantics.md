# 04：领域规则与扩展生命周期

Status: ready-for-agent
Progress: pending
Blocked by: 01, 02

## Scope

把核心对象内聚与保留语义写回实际负责模块，补齐按需能力的归属、生命周期和启用约束。根据 02 矩阵解释覆盖与缺口，避免只在新总览中宣称收敛。

Owned files: `.draft` 中 orchestrator、brain、execution、security、collaboration、memory、extensions、evaluation 的相关 README／implementation 与既有专篇；可新增所属模块内必要短篇；`reliable-work.md`。不改交互模块、根总览、工程导航和研究文件。

## Acceptance

- [ ] Task／条件／结果、Decision／输入／调用／提案、Operation／Intent／Attempt／Effect 各自统一管理但保留准确身份、原 owner 与终态后责任。
- [ ] Grant／Use／UseSettlement 与 Confirmation 的职责准确；能力、绑定和安装配置保持必要性，受信输入不退化成聊天文本。
- [ ] 持续目标与继续权、委派与稳定子会话／激活、Schedule 与 JobStore、环境与 Operation、Memory 与 Content 各自有明确区分。
- [ ] 为尚未开放的调度、分支协作、环境复用等写最小语义合同、启用前提和缺口，不擅自增加公开 Schema／方法／状态。
- [ ] 当前 ADR 的生产分布式、固定路由、共事务接纳、核验、条件修订、回退批准与预览规则均保留；普通记忆纠正不引入发布流程。
- [ ] 避免九套重复规范或新通用状态机；既有正文是规则权威，核心目录负责导航。复用检查并记录运行证据缺失。

## Comments

- 2026-10-01：依赖 01 和 02 已合入。允许精简已有重复说明，避免只追加泛化宣言；勿把跨 owner 意图／接纳事实合并成一份伪原子记录。
