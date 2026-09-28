from pathlib import Path
p=Path('docs/architecture/brain/decision-paths.md');s=p.read_text()
s=s.replace('# Brain 决策路径：规则 shortcuts 与 System One 模型','# Brain 决策路径：规则、类型化推理与升级')
start=s.index('[模块主线]');end=s.index('## 1. 三条路径如何分工')
s=s[:start]+'''[模块主线与双系统分工](README.md#dual-system) · [实现与恢复](implementation.md) · [外部模型调研](../../research/system-one-models-2026-09-28.md)

本文定义单轮选择、规则覆盖、类型化答案映射及跨轮升级的完整行为。S1 由规则 shortcuts 和类型化模型两条路径组成，S2 使用通用模型；总体职责和任务交接见模块主线。规则命中不创建 ModelCall，任一模型路径每个 Decision 至多一次物理推理。

类型化路径为可选设计，按第 6 节条件完成实现、适配与评测后才启用。首个候选是规则无法处理的报告选页，选 Jev 作为托管试验后端；当前无本项目模型质量、费用或延迟实测，供应方性能证据集中在调研笔记。

'''+s[end:]
a=s.index('Orchestrator 先固定任务快照');b=s.index('下图表示一个 Decision 内互斥的产出路径。')
s=s[:a]+'''Orchestrator 已固定输入快照和 model_profile_ref。Brain 先检查控制、未知效果、必要事实及用途资格，再尝试规则；完整命中则构造提案，未覆盖才调用已选模型，规则冲突则保存失败。未知阶段与初次开放目标的 profile 选择按[整体协作](README.md#dual-system)处理，不在 Brain 内新增一次分类调用。

'''+s[b:]
s=s.replace('S[System One 一次推理<br/>校验并接纳有限答案]','S[S1 类型化模型一次推理<br/>校验并接纳有限答案]').replace('G[通用模型一次推理]','G[S2 通用模型一次推理]')
s=s.replace('选择三种产出方式只扩展已有策略与适配器，不增加路由服务、长期 Agent 或新的执行权限。用一个便宜模型给所有任务分类，会让原本零模型的工作增加调用，也会让复杂任务先支付一次分类成本；因此不作为默认架构。只有同任务评测证明额外分类的净收益，才考虑这种竞争方案。\n\n','')
s=s.replace('给它另加 System One 没有必要','给它另加类型化模型没有必要')
rule='规则无覆盖与规则错误须区分：前者才允许按本轮固定模型配置推理；冲突规则产生不同效果、或规则产物校验失败时，保存 `failed / invalid_output`、`retry=after_change` 并省略 ModelCall。Orchestrator 保存策略依赖缺口，待负责方修复并启用新版本后以新 Decision 继续；不能用模型隐藏规则缺陷。等待超过任务期限或策略允许范围时按现有任务失败／终止规则收尾。'
assert rule in s
s=s.replace(rule,'规则未覆盖和规则错误分别处理：前者可走本轮指定模型，冲突或无效规则产物须先修复策略。错误记录及后续责任集中在[异常与升级](#43-拒判错误和升级分别处理)。')
s=s.replace('## 3. 首个 System One 场景：报告候选选页','## 3. 类型化判断：以报告候选选页为例')
s=s.replace('System One 可以给候选评分','类型化模型可以给候选评分').replace('搜索摘要和 System One 分数','搜索摘要和模型分数')
a=s.index('### 3.2 哪些位置暂不替换');b=s.index('### 3.3 选型与概率接纳')
s=s[:a]+'''### 3.2 适用边界

本路径只选择下一批读取内容。D1 保留完整自然语言理解，D4 保留正文与计划生成；任意设备指代、时间或附加约束不能压成固定标签。后续若试验有限意图识别，须已有受信入口或可定位的参数候选，并独立验证“未覆盖／需澄清”判断。准确设备布尔状态、状态版本复制和计划后继继续用规则。

O7 的质量与引用语义仍是独立评估操作；将来替换限定子判断须另测整套评估器漏判及替换契约，不能从选页准确率推断。授权、效果核对与完成裁决继续归原负责方，概率分数不改变其保证。

'''+s[b:]
s=s.replace('## 4. 在现有 Brain 中怎样实现','## 4. 固定配置与异常处理')
s=s.replace('公共 `brain.decide / get / cancel`、DecisionRequest、DecisionRecord 和 Proposal 形状保持。ModelProfile 的新增可选编码 `typed_decision` 及 `decision_spec_ref` 在[契约查阅](README.md#brain-contracts)定义；仅供本路径使用。', 'ModelProfile 的 `typed_decision` 编码及 `decision_spec_ref` 字段见[对外契约](README.md#brain-contracts)。本节定义其决策规格内容，不增加公共方法或 Proposal 类型。')
s=s.replace('DecisionPolicy 接收固定领域值，检查覆盖、候选与答案接纳条件，并构造提案；不访问模型或数据库。ModelAdapter 只编码请求、校验供应方原始响应并提供调用事实。RecoveryWorker 复用既有准备、用途门禁、发送和终态流程，DecisionStore 沿原记录保存版本、候选映射摘要、调用与获准诊断。无需新增数据表；禁止保存的原文或分数不因调试需要绕过保留策略。', '规格覆盖、候选和答案接纳由 DecisionPolicy 判断，请求编码及供应方原始响应检查由 ModelAdapter 完成；两者职责按[软件分工](implementation.md#module-shape)装配。DecisionStore 沿原记录保存规格版本、候选映射摘要及获准诊断，调用继续复用发送和恢复流程，不新增数据表。禁止保存的原文或分数不因调试需要落库。')
s=s.replace('现有估算模式还要求用户已接受估算、费用 Grant owner 与原 Task 同受信提交域且在线、无 allocation 等完整前提；本方案不新增估算授权，也不把牌价乘输入上限视为硬上界。', '估算模式须满足[资源边界](README.md#model-recovery)中的全部既有前提，不新增估算授权，也不把牌价乘输入上限视为硬上界。')
s=s.replace('### 4.3 拒判、错误和升级分别处理\n\n','### 4.3 拒判、错误和升级分别处理\n\n'+rule+'\n\n')
s=s.replace('通用模型仍失败时使用原有限修复／失败机制，不循环回 System One。','升级后的 S2 读取当前问题、准确候选及获准拒判诊断，原分数保留模型派生性质。S2 应给出行动、补证、澄清或失败，不将原问题改名后退回 S1；仍失败时使用原有限修复／失败机制。只有本链关闭并推进到新的业务阶段后，才重新判断能否使用 S1。')
s=s.replace('participant S as System One','participant S as S1 类型化模型').replace('participant G as 通用模型','participant G as S2 通用模型')
s=s.replace('规则能解决时 System One 增加成本','规则能解决时类型化模型增加成本').replace('将 D3 换为 System One','将 D3 换为类型化模型').replace('System One 为 `C_s`','类型化模型为 `C_s`')
s=s.replace('规则加 System One、规则加受限输出通用模型','规则加类型化 S1、规则加受限输出通用模型')
s=s.replace('| 全任务质量 |', '| 跨轮协作 | 条件修订、新事实与计划冲突、无进展往返、同原因误归为权限或语义问题 | 旧目标行动／计划失效；按实际问题交原负责方；候选改名不重置升级次数，任务总限额累计 |\n| 全任务质量 |')
s=s.replace('本轮交付为应用设计与资料调研，没有运行 Harness、设备或模型请求；没有本项目准确率、费用或延迟实测结论。','以上均为待实施验证。逐轮 S2 复核若作为单独实验，记录其全部调用及增量价值；不能将双跑实验的质量与不复核路径的成本拼接为同一结论。通用子问题委派和经验进入 S1 的范围见[模块演进边界](README.md#6-验收与演进边界)。')
p.write_text(s)
