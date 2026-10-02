# 证据资格与受控改进

任务核验回答“这份成果是否满足当前条件”，实验评测回答“这个新版本是否比基线更好”。前者服务一次Task完成，后者服务组件发布；两者共享可追溯证据，不共享成功捷径。

默认普通开放质量允许assessed并说明未校准的限制；高影响自动判断和专项准确性保证必须有独立校准。评测通过不会授予行动或发布权限。

## 1 条件判断和适用性分开

ConditionCheck固定check_id、Task/goal_revision/requirement、准确artifact、rule/evaluator版本、配置/InstallLock、原评估Operation及报告。ConditionResult的原verdict=pass/fail/unknown不可变；当前applicability=usable/unknown/inapplicable另记。

规则定义目标谓词和证据范围，Evaluator实现检查。确定性验证、开放质量评估和用户验收分开；一个加权总分不能覆盖必要效果失败。组合判断保留有界无环的依赖check集合，任一子证据失效不能藏在汇总pass里。

普通退役、到期或升级只封新调用。受信维护者确认判断缺陷并命中准确rule/evaluator/scope，才使旧证据失去适用性。活动Task重新取证，已经成功的Result保持固定，附缺陷范围及补救说明。这保留[ADR 0005](../../adr/0005-evaluator-evidence-eligibility.md)。

## 2 本地缺陷与完成竞争

每个准确实现启用前建立evidence_gate。缺陷登记在同一原分片独占gate，提交缺陷原事实、gate_revision及分页影响Job；不锁全部Task。Task完成按Task→gate共享锁→当前checks→Job的顺序，直接查命中缺陷，不依赖异步影响投影。

缺陷先提交则旧pass不能完成；完成先提交则随后追加notice。影响扫描中断从原游标续行，包含GoalCoverage与组合检查。缺陷不可完整查询时保持unknown，不挑一个旧pass继续。

## 3 跨域当前资格的新增合同

单次启动批准不能证明旧证据现在没有缺陷。本系列新增 `evidence.eligibility.check`、`evidence.defect.register/read/changes`，补齐这个跨域缺口；由原验证器治理owner和消费方本地gate承担，不新增全局服务。

eligibility.check是写命令：它固定check_id、consumer_owner/task、准确evaluator/rule/report、适用scope、原缺陷修订、verdict与期限，并登记后续缺陷交回holder。返回签名EligibilityReceipt，绑定上述字段、authority_epoch、完整依赖摘要、checked_at/issued_at、expires_at和请求摘要。签名按受信owner密钥与用途核验，不由报告自带公钥建立信任。

消费方先保存原资格命令与Job，再请求原owner。收到后保存凭据、原缺陷游标和本地导入gate；丢答复查原命令。缺陷owner将新缺陷与holder交回outbox共同提交，消费方将导入事实、gate推进与影响责任共同提交后确认。序列缺口不能用后来的游标跳过。

TaskPolicy明确 `max_evidence_staleness`。最终完成同时检查本地gate、已知无缺口的导入水位、准确凭据绑定和最紧期限：

- 窗口为0：必须同一个权威事务范围直接查当前缺陷；远程调用后回本地提交不能声称零陈旧
- 窗口大于0：允许不超过策略规定的远端缺陷未知窗口，完成结果记该限制；实际截止不晚于min(原expires_at,checked_at+策略窗口)，查询原回执不续期；过期、时钟不可证、authority_epoch变化或交回链有缺口则等待并重建资格
- 高影响自动判断默认0，部署不能提供同域门禁时不开启该保证

该协议缩短并界定跨域陈旧，不声称撤销已完成的外部动作。旧Result后来的缺陷notice仍持续交回。

## 4 实验冻结什么

EvaluationPlan固定candidate/baseline准确InstallLock、任务与样本清单、分组与随机种子、开发/选择/正式分区、环境初态、模型/提示/工具、预算、尝试政策、指标、阈值、统计方法及停止规则。

ImprovementPolicy先固定候选谱系、formal_attempt_limit、停止及整体比较方法。正式Plan在同一事务绑定准确candidate；release_request_id与正式holdout partition_id各自具有唯一占用约束，永久占用formal_attempt_index；失败/取消不返次数或保留集，换ID不重置谱系。

一个Plan只产生一个逻辑EvaluationRun。每个 `(plan,sample_id,arm)` 只有一个SampleRun；恢复和安全重试是其Attempt，不改变分母。预先固定样本全集，排除规则在运行前固定；启动后基础设施失败、超时、未知和未跑均留在原分母并报告，不靠“排除原因”删难例。

```mermaid
flowchart LR
    D[开发材料与失败分析] --> C[固定候选]
    C --> S[选择集比较与参数选择]
    S --> F[冻结正式计划和未曝光分区]
    F --> R[成对运行基线与候选]
    R --> A[独立判断、全部样本与成本]
    A --> P[受信发布批准]
    P --> M[有限上线监测与独立旧版回退]
```

每臂独立环境键为(run_id,sample_id,arm)，两臂预检均通过才启动任一臂。prepare和每次Attempt在真实入口核签有限启动许可，创建未知查原environment_key，不另造环境；seal确认目标调用不能再启动且在途已收束后才destroy。candidate不能读取答案或修改目标真值/judge，真值来自独立只读通道。

正式数据只在隔离runner内按最小用途开放。已授权隔离runner的正常输入/真值读取不是向开发或选择过程的Exposure；后者的接触、调试输出或泄漏才按曝光规则处理。反馈查看、样本正文读取、人工调试和候选选择接触都登记Exposure；不能把“没有训练”当没有泄漏。正常feedback_open只在报告封存后，先耐久登记Exposure再返回，保留自身已封报告资格，但使尚未封存且依赖同source_group的计划失去正式资格。提前或时点不明的泄露使相关原formal证据失效。门禁依据曝光类型/时点/血缘裁决，不能一读报告就使它自身失效。

## 5 曝光与大规模取消

Exposure原事实、source group关联、门禁修订及唯一影响Job同事务提交。formal plan、report封存、批准及后续启动同步查原曝光gate；异步扫描只负责更新投影，不能让扫描尚未到达的run继续拿正式资格。

样本清单用不可变manifest及摘要，运行环境/attempt/取消目标使用完整关联索引和分页，不在EvaluationRun响应嵌入最多100个environment却声称覆盖上千样本。新增sample或environment的本域启动许可与run gate串行；远端已签有限窗口仍按在途处理。

run.cancel原子封闭新样本、重试和环境启动，再保存分页seal/停止/费用核对责任。回执表示取消决定已存；报告另列已停止、在途、未知效果、未知费用及残留环境。原本已签有限启动窗口和已开始动作继续核对，不靠异步“取消广播”保证即时停止。

## 6 统计口径与优化门禁

对照采用同任务成对结果，分别报告：目标真值成功、模型完成声明、系统完成接纳、误放行和误挡。终态验证器拒绝错误答案有价值，但不能撤销前面已发生的错误退款或写动作。

结论分别记录target_attainment（是否达目标）、statistical_gate（统计证据）、improvement_gate（是否达到实用改进门槛），并标formal/exploratory/conformance用途。实用收益须为正，逐类质量、费用和延迟的退化界限预先冻结。质量目标按预先选择的置信区间/检验解释，并报告样本数、缺失及适用任务域。成本包含失败尝试、辅助摘要/标题/评估、发现、重规划、未知账单保留、环境和维护；延迟报分布及超时，不只报成功均值。

优化试验固定一个主要因素，并记录无法分离的协同变更。更多工具、typed_decision、有限计划、程序化cell、记忆检索、压缩和更强模型分别试验；若工具、提示、文件状态和诊断一起改变，不能把差值归给“工具数量”。

项目验收的1000+API、首次正确率≥90%、有限重试成功率≥95%都是目标。API按语义不同操作计数，别名/账号/版本包装不重复。首调正确同时检查能力、参数、资源、权限及目标效果；最终成功分母包含失败和unknown，不用项目总体成功率冒充某个Evaluator准确率。

## 7 自进化的权限

常规Memory更新可在原授权内生效，但“提高效果”仍需证据。Skill/执行策略/Agent配置经评测后有限启用。插件与内核代码生成可审查补丁，由维护者确认发布。候选不能自己调低阈值、打开正式数据或给自己批准。

首装只验证兼容和可运行，不要求虚构旧版本收益。改善声明必须对应准确基线与正式证据，发布系统不能因试验失败将其改为compatibility继续上线。

## 8 仍需运行建立的证据

测试须注入缺陷登记与完成同时发生、远端资格过期、缺陷流缺口、报告封存与曝光竞争、取消上千样本、runner退出但环境未停、重试不增分母和同任务错误效果早于验证器拒绝。

模型、协议构造向量和图可以检查局部不变量，无法证明实际事务、身份、平台隔离或目标效果。任何形式化结论必须绑定本版模型、性质、假设与哈希；历史模型检查计数不能迁移为本架构通过。
