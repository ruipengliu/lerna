from pathlib import Path
import re

ROOT = Path('/Volumes/Data/proj/lerna-docs')
BASE = ROOT / 'docs/architecture/memory'
BEFORE = ROOT / '.scratch/memory-docs/before'
readme = (BEFORE / 'README.md').read_text()
implementation = (BEFORE / 'implementation.md').read_text()
optimization = (BEFORE / 'optimization-plan.md').read_text()


def between(text, start, end=None):
    assert text.count(start) == 1, start
    after = text.split(start, 1)[1]
    if end is not None:
        assert end in after, end
        after = after.split(end, 1)[0]
    return after.strip()


def section(number):
    match = re.search(rf'^## {number}\. .*\n', optimization, re.M)
    assert match, number
    end = re.search(r'^## \d+\. ', optimization[match.end():], re.M)
    body = optimization[match.end():match.end() + end.start()] if end else optimization[match.end():]
    return re.sub(r'\n<a id="proposals"></a>\s*$', '', body).strip()


intro = '''# 记忆、内容与来源

[整体设计](../README.md) · [目标](../goals.md) · [授权规则](../security/README.md) · [大脑](../brain/README.md) · [模块 UML](../uml-models.md#memory)

本模块让任务跨任务复用获准的个人信息与经验，并让用户能够纠正、限制和删除它们。Memory owner 保存记忆修订和检索依据，内容 owner 保存准确正文、来源及副本清理责任；Orchestrator 选择当前任务所需材料并组织使用。覆盖 C2、C3、C7、C9 及三系统独立替换要求。

本页与实现设计构成现行设计基线，参考实现和运行验收仍待交付。owner、来源、许可、修订、分页与清理语义是替换实现必须保持的契约；字面召回和初始配额是默认实现选择。中文词法、混合检索、候选质量检查和经验整理属于待评测策略，文档整理不使它们自动成为默认。

## 阅读路径与规则归属

先用本页理解保存、查询和关闭的完整行为，再按实现职责进入后续文档。完整规则在下表所列位置维护，研究证据和检查记录不另行定义运行契约。

| 顺序 | 文档 | 负责定义 |
| --- | --- | --- |
| 1 | 本页 | 模块边界、关键取舍、写入与查询行为、内容治理、集中字段及方法语义 |
| 2 | [实现设计](implementation.md) | 软件组件、持久对象、事务与锁序、索引水位、候选发布、内容交付及恢复 |
| 3 | [质量优化策略](optimization-plan.md) | 写入保真、B0／B1／B2 检索、显式关联、本地嵌入和读时整理的候选机制及采用边界 |
| 4 | [容量、验收与建设顺序](validation.md) | 配额起点、行为与故障用例、质量对照、指标、启用与回退门槛、阶段退出证据 |

精确线字段以[机器契约](../contracts/schemas/protocol.schema.json)和[方法登记](../contracts/schemas/methods.json)为准；领域含义在本页[集中定义](#memory-contracts)。外部依据见[近半年论文](../../research/agent-memory-papers-2026-09-28.md)与[开源实现](../../research/agent-memory-libraries-2026-09-28.md)。研究结论不表示本项目已实现相应能力。

'''
part12 = '## 1. 边界与选择\n\n' + between(readme, '## 1. 边界与选择', '## 3. 检索、修改与提取')
write = between(readme, '### 3.4 写入和并发', '### 3.5 端侧挖掘与提取')
extract = between(readme, '### 3.5 端侧挖掘与提取', '## 4. 隐私、视图与清理')
temporal = between(readme, '### 时间、冲突与去重边界', '<a id="memory-pages"></a>')
query = between(readme, '### 3.1 查询的授权边界', '<a id="default-matching"></a>')
matching = between(readme, '### 3.2 默认匹配与索引补齐', '<a id="temporal-conflicts"></a>')
pages = between(readme, '### 3.3 稳定分页与管理读取', '### 3.4 写入和并发')
privacy = between(readme, '## 4. 隐私、视图与清理', '<a id="memory-contracts"></a>')
privacy = re.sub(r'^### 4\.', '### 5.', privacy, flags=re.M)
fields = between(readme, '## 5. 集中字段与接口', '<a id="memory-capacity"></a>')
new_readme = intro + part12 + f'''

## 3. 保存、提取与修订

### 3.1 写入和并发

{write}

### 3.2 端侧挖掘与提取

{extract}

候选的持久化与确认见[实现设计](implementation.md#4-端侧提取与候选发布)；如何减少遗漏、无依据概括和错误经验，见[候选质量策略](optimization-plan.md#write-quality)。两者分别规定发布合同和待评测的提取方法。

<a id="temporal-conflicts"></a>
### 3.3 时间、冲突与去重

{temporal}

## 4. 查询与使用

### 4.1 查询的授权边界

{query}

<a id="default-matching"></a>
### 4.2 默认匹配与索引补齐

{matching}

中文与语义召回的替代配置见[检索策略](optimization-plan.md#retrieval)；其启用不改变处理前授权、权威补扫和逐页复核的含义。

<a id="memory-pages"></a>
### 4.3 稳定分页与管理读取

{pages}

## 5. 内容、视图与清理

{privacy}

<a id="memory-contracts"></a>
## 6. 集中字段与接口

{fields}

<a id="memory-capacity"></a>
## 7. 容量与验收入口

查询、提取、同步、索引和清理按用户分别计费与限流，清理及撤权传播保留服务份额。完整配额集中在[容量配置](validation.md#capacity)；故障恢复不能通过取消扫描上限、丢弃关闭责任或跳过清理换取吞吐。

[验收设计](validation.md)先覆盖用户可见行为和 MI-01～27 故障，再按 E0～E5 比较候选策略。静态协议和文档检查不证明召回质量、隐私出口、数据库竞争或跨端恢复；真实能力以对应阶段的运行证据为准。
'''

# Preserve the baseline mechanisms; move tests and strategy experiments out.
new_implementation = implementation.split('## 8. 上限与故障实验', 1)[0].rstrip() + '\n'
new_implementation = new_implementation.replace(
    '[模块主线](README.md) · [安全](../security/README.md) · [Brain 输入](../brain/implementation.md) · [交互实现](../interaction/implementation.md)',
    '[模块主线](README.md) · [质量优化策略](optimization-plan.md) · [容量与验收](validation.md) · [安全](../security/README.md) · [Brain 输入](../brain/implementation.md) · [交互实现](../interaction/implementation.md)')
read_time = between(implementation, '<a id="read-time-curation"></a>', '### 4.2 逐条确认与预授权分支')
new_implementation = new_implementation.replace(read_time, '''### 读时整理的接入位置

默认查询返回准确记忆版本，不自动为每次查询增加摘要调用。按当前任务／进展整理原材料的候选机制集中在[读时整理与经验复用](optimization-plan.md#read-time-curation)：Orchestrator 组织有界处理，Memory 保存获准候选及来源；整理产出进入上下文不等于取得长期保存许可。对照与启用条件见[质量实验](validation.md#experiments)。''')
closure_tests = between(implementation, '## 6. 关闭、清理与恢复\n\n', '\n\n原文按保留策略到期')
new_implementation = new_implementation.replace(closure_tests, '本节定义关闭与恢复机制；摘要、经验、索引、模型输入／输出、UI 缓存及跨端副本的组合验收统一见[来源关闭与故障用例](validation.md#faults)。')
new_implementation = new_implementation.replace(
    '第 8 节数值仍是待测初值；生产以带来源过滤、索引落后、设备失联和单可用区退出的组合实验确定配额。',
    '配额起点及生产组合实验见[容量与验收](validation.md#capacity)。实施时先验证本页机制，再按质量策略的独立实验决定是否启用替代算法；索引、内容和权限恢复始终沿原责任。')
new_implementation = new_implementation.replace(
    '以下表结构和算法为设计规格；没有实际隐私隔离或存储恢复实验结果。',
    '以下表结构和算法为待实现的设计规格。阅读顺序为[组件与工作接入](#module-shape) → [对象与来源交接](#data-flow) → [发布及索引时序](#key-sequence) → 查询与候选处理 → 内容交付与关闭 → [生产约束](#production)。配额和故障实验在独立[验收页](validation.md)维护。')

# Candidate strategy mechanisms remain prospective, with research separate.
strategy_intro = '''# 记忆质量优化：候选提取、检索与经验复用

[模块主线](README.md) · [实现机制](implementation.md) · [对照与建设顺序](validation.md#experiments) · [论文依据](../../research/agent-memory-papers-2026-09-28.md) · [开源实现](../../research/agent-memory-libraries-2026-09-28.md)

本页细化 [MEM-01～03、X-02](../validation/optimization-evidence.md) 的建设方向，集中定义待评测的提取、检索与经验策略。现行字面匹配仍是默认；B1 中文词法、B2 混合检索、显式关联扩展和模型质量检查都须分别取得证据后启用。公共 Schema、候选发布、来源关闭与稳定分页沿[模块契约](README.md)，本文不新增线方法或改变成功含义。

推荐先补齐运行基线和中文／写入质量，再评估混合召回与按需经验整理。质量实验须有获准数据、隔离环境和独立参考；模型处理须有当前位置、接收方、资源预算及清理依据。权威与关闭机制未运行时只能做离线算法实验；不具备合法处理能力时停用相应分支，继续已获准基线或报告既有依赖缺口。阶段安排与验收统一见[建设顺序](validation.md#roadmap)。

## 1. 优化目标与取舍

'''
choice = section(1)
choice = choice.replace('以上对应 [MEM-01～03、X-02](../validation/optimization-evidence.md)，本方案细化其算法候选和建设顺序，不另造优化总账。\n\n', '')
evidence = '''

### 1.1 研究支持的范围

[论文调研](../../research/agent-memory-papers-2026-09-28.md)按首次提交日期核对近半年工作，集中保存阅读版本、实验设置和限制。StateMem 支持显式替代及依赖检查，TRUSTMEM 支持把覆盖、保留、支撑分开审核；MemoryCPT 支持比较建设与在线整理的总成本，AMD 支持按工作流、子任务和函数粒度组织经验。它们没有证明本项目需要逐轮建图、训练专用模型或采用固定的性能目标。

来源撤销与派生残留论文用于扩展失败用例：文本里声明撤销、停止再次输出敏感词和物理清理是不同证据。相应反例集中在[策略验收](validation.md#strategy-cases)，事实与许可仍由原 owner 裁决。

开源实现优先作为算法参考或隔离对照：LangMem 的纯提取接口适合候选流程，Graphiti 的时间与检索配方适合关系密集任务比较，Mem0 的轻量事实流程可作对照；Letta、memU 和 OpenViking 的材料组织与后台整理不直接替代 Memory owner。具体提交、许可证、流程漂移及治理差距在[开源库调研](../../research/agent-memory-libraries-2026-09-28.md)集中定义。首选仍是现有 Go 组件和存储上的小型策略实现，尚未选定生产第三方依赖。
'''
write_quality = re.sub(r'^### 4\.', '### 2.', section(4), flags=re.M)
retrieval = re.sub(r'^### 5\.', '### 3.', section(5), flags=re.M)
retrieval = retrieval.replace('初始实验取 `k=60`、两路等权。', '常数与权重按[实验配额](validation.md#capacity)固定。')
retrieval = retrieval.replace('冻结最多 200 个准确版本与排序依据。', '按[总集合上限](validation.md#capacity)冻结准确版本与排序依据。')
retrieval = retrieval.replace('每路候选建议从 80 条开始；两路合并、权威补扫及受限关联共享最多 200 条的总集合预算。', '两路合并、权威补扫及受限关联共享同一总集合预算；各路候选上限见[容量配置](validation.md#capacity)。')
experience = section(6)
records = section(8)
records = records.split('参数起点：', 1)[0].rstrip() + '\n\n模型、扫描、字节／token、CPU／内存、超时及重建批次等配置按[容量与实验要求](validation.md#capacity)固定；未确定资源上界不启动相应实验。'
proposals = section(11)
proposals = proposals.replace('不把它们当本轮已采用合同。', '不把它们当已采用合同。')
proposals = proposals.replace('本轮只使用现有任务证据做隔离评测', '先使用现有任务证据做隔离评测')
proposals = proposals.split('全文方案不依赖用户先选择某个开源品牌才能继续。',1)[0].rstrip()
new_optimization = strategy_intro + choice + evidence + f'''

<a id="write-quality"></a>
## 2. 写入：候选内容与质量检查

{write_quality}

<a id="retrieval"></a>
## 3. 检索：候选生成、融合与关联

{retrieval}

<a id="read-time-curation"></a>
## 4. 经验复用与读时整理

{experience}

## 5. 内部记录与兼容边界

{records}

<a id="proposals"></a>
## 6. 后续跨模块提案

{proposals}

采用决策以[同口径质量与成本对照](validation.md#experiments)为依据；缺收益证据时维持原配置，未决提案只限制依赖它的能力。
'''

# Collect each test once, keeping established MI identifiers and exact rows.
faults = between(implementation, '| 实验 | 输入及注入 | 可观察结果 |', '\n\n上述槽映射')
fault_rows = [line for line in faults.splitlines() if line.startswith('| MI-')]
assert len(fault_rows) == 27
fault_rows.sort(key=lambda line:int(re.search(r'MI-(\d+)',line).group(1)))
fault_tail = implementation.split('上述槽映射须分别运行',1)[1].strip()
scenario = section(7)
scenario_text = scenario.split('| 反例 |',1)[0].strip()
scenario_rows = [line for line in scenario.splitlines() if line.startswith('| ') and not line.startswith('| 反例') and not line.startswith('| ---') and 'create 已提交但答复丢失' not in line]
assert len(scenario_rows)==8
scenario_rows.append('| 新 B 反向声明与旧 A 冲突，但只有 A 初始命中 | IndexWorker 保存双向关系投影及独立水位；QueryService 逐项鉴权并有界补扫 | 启用关联扩展时能找出获准的 B；覆盖不足保留缺口，受限端点不披露；以 E3a 独立验证 |')
evaluation = section(9)
roadmap = section(10).replace('论文复述、代码阅读和本次文档检查', '论文复述、代码阅读和静态文档检查')
roadmap = roadmap.replace('评估关系扩展、生成式重排', '评估超过一跳的关系扩展、生成式重排')
new_validation = '''# Memory 容量、验收与建设顺序

[模块主线](README.md) · [实现机制](implementation.md) · [质量优化策略](optimization-plan.md) · [系统验收](../validation/README.md) · [评测与发布](../evaluation/README.md)

本页集中定义默认实现的配额与故障验收，以及候选策略的质量对照和启用条件。先证明写入、查询、关闭与恢复符合契约，再判断记忆是否改善实际任务；静态检查、算法实验和服务运行分别取证。以下均为待运行的验收要求，不是已取得的性能或质量结果。

<a id="capacity"></a>
## 1. 容量起点与过载边界

数值是初始待测配置，部署可下调；提高前须验证热写、跨端、来源过滤、索引落后、设备失联、清理积压及单可用区退出。生产容量与恢复目标仍沿[系统部署](../deployment-production.md#capacity)，不能从论文吞吐推算本项目 SLO。

| 范围 | 初始配置 | 必须另外限定或核验 |
| --- | --- | --- |
| 查询 | 每页 20 项、总集合最多 200 项、集合保留 5 分钟；每用户 4 个活动查询 | 真实扫描行数、来源核验次数、字节、耗时与任务累计预算；分页上限不等于底层工作量 |
| 提取 | 每用户 1 个任务；每次最多 100 个准确输入引用、100 个候选 | 单次字节／token、模型费用、候选保留期、检查点与取消后继续责任 |
| 视图 | 每页最多 100 个变更；每用户最多 8 个活动视图 | 变化日志受最慢有效 ACK 与磁盘水位约束；不扩大离线使用窗口 |
| B2 候选与融合 | 词法、语义每路先取最多 80 项；RRF 常数 60，两路等权 | 两路、权威补扫与一跳关联共享查询总集合上限；截断保留 partial，不因此声称语义穷尽 |
| 本地嵌入与重建 | 模型、tokenizer、维度及索引代次固定；CPU／内存、超时、并发、扫描和批次在实验前确定 | 没有本地能力或有界资源装配时 B2 保持关闭；未知使用不计为零，费用／资源沿原责任结算 |

查询、提取、同步、索引和清理按用户分别计费与限流，关闭索引、撤权传播和清理保留份额。过载先限制新提取、上传和视图，再收紧新查询；空间不足拒绝新保存，不丢弃已经接纳的清理责任。完整生产机制与指标见[实现设计](implementation.md#production)。长期关闭索引、source_edges、WAL、备份及重建空间均进入容量预算，不能按普通缓存 TTL 删除最后的关闭依据。

<a id="behavior"></a>
## 2. 默认行为与故障验收

### 2.1 用户效果及端云边界

| 场景 | 前置与刺激 | 可观察结果／目标 |
| --- | --- | --- |
| 偏好实际生效 | 保存限定技术评审的偏好，执行评审与非评审两个任务 | 仅适用任务采用输出格式，记录准确来源；C2、V1 |
| 并发纠正 | 两端以同一 expected_revision 替换 | 一次成功、一次 revision_conflict；派生旧记忆停止参与查询；C2、A4 |
| 私密提取缺本地能力 | local_only 来源，只有云模型可用 | 提取等待或不支持；正文、摘要、向量均不外发；C2、C7 |
| 离线副本撤权 | 副本取得有限租约后离线，owner 删除 | owner 已禁用；副本不晚于原租约截止停止，物理状态仍 pending；C5、C7 |
| 视图开放后的外部扩权 | view.open 时旧 Memory 不可披露；另一 Grant owner 后来签发许可，Memory owner 无对象变更 | 旧 view.pull 不伪造 upsert；exhausted 只针对原快照与 Memory 变化。新 view.open 枚举当前获准旧对象；撤权后旧页仍逐项复核；C2、C7 |

写答复丢失、热写分页、索引滞后和旧备份恢复分别由下表 MI-01、03、02、09 验证，不另设重复用例。跨模块配对效果和恢复复用 [SYS-08、11、12、28](../validation/README.md#scenarios)及 [OPT-06／07](../validation/optimization-evidence.md#scenarios)。

<a id="faults"></a>
### 2.2 事务、索引与持有者故障

''' + closure_tests + '\n\n| 实验 | 输入及注入 | 可观察结果 |\n| --- | --- | --- |\n' + '\n'.join(fault_rows) + '\n\n上述槽映射须分别运行' + fault_tail + '''

<a id="strategy-cases"></a>
## 3. 候选策略的贯穿场景与反例

以下用例对所启用的策略运行；B1／B2、候选质量检查及显式关联的定义见[优化策略](optimization-plan.md)。既有权限、来源及关闭用例同样适用于所有实验臂，不能为了测提速而取消。

''' + scenario_text + '\n\n| 反例 | 谁保存事实、谁继续 | 预期可观察行为 |\n| --- | --- | --- |\n' + '\n'.join(scenario_rows) + '''

<a id="experiments"></a>
## 4. 对照、指标与启用条件

''' + evaluation + '''

<a id="roadmap"></a>
## 5. 建设顺序与退出证据

''' + roadmap + '''

## 6. 证据状态与交付记录

当前已有设计规格、一手文献与源码核对，以及文档／构造序列的静态检查；没有本模块的真实存储恢复、隐私出口、召回质量或成本收益报告。不得把文献结果、Mermaid 渲染或 Schema 合法性记作本页运行用例通过。

实现交付须逐项记录配置和环境、冻结输入与真值、原责任身份、实际结果、失败后的继续记录、未知与残留。一次报告分别列语义审查、静态检查、图示渲染和运行验证；通用要求见[系统验收](../validation/README.md)，文档检查历史见[架构审查记录](../review.md)。
'''

for name, value in [('README.md',new_readme),('implementation.md',new_implementation),('optimization-plan.md',new_optimization),('validation.md',new_validation)]:
    assert value.count('```') % 2 == 0, name
    (BASE/name).write_text(value.rstrip()+'\n')
    print(f'{name}: {len(value.splitlines())} lines, {len(value)} chars')

# Keep dated delivery evidence outside the enduring design text.
old_review = section(12)
(ROOT/'.scratch/memory-docs/research-delivery-review.md').write_text('# 2026-09-28 Memory 调研交付检查\n\n以下记录对应整理前的调研方案，不作为当前文件数量或运行能力声明。\n\n'+old_review+'\n')
