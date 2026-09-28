#!/usr/bin/env python3
"""Render the review from computed fixtures plus authored explanations."""
from pathlib import Path
from collections import Counter
from copy import deepcopy as cp
import hashlib
import json

P=Path(__file__).resolve().parent
ROOT=P.parents[2]
DOC=P.parent/'task-scenarios-input-output-2026-09-28.md'
S={n:json.loads((P/n/'scenario.json').read_text()) for n in ['bluetooth-on','bluetooth-off','report']}
SH=json.loads((P/'shared.json').read_text())
DEFS=json.loads((ROOT/'docs/architecture/contracts/schemas/protocol.schema.json').read_text())['$defs']
IDS=json.loads((P/'identities.json').read_text())
STAT={k:v['statistics'] for k,v in S.items()}
SHORT={'bluetooth-on':'B+','bluetooth-off':'B−','report':'R'}
OUT=[]
def put(s=''):OUT.append(s.strip()+'\n')
def enc(v):return json.dumps(v,ensure_ascii=False,sort_keys=True,separators=(',',':')).encode()
def table(headers,rows):
    put('| '+' | '.join(headers)+' |\n| '+' | '.join('---' for _ in headers)+' |\n'+'\n'.join('| '+' | '.join(str(v).replace('|','\\|').replace('\n','<br>') for v in row)+' |' for row in rows))
def obj(n,key):return S[n]['objects'][key]['value']
def exchange(n,label):return next(x['exchange'] for x in S[n]['exchanges'] if x['label']==label)
def code(v):put('```json\n'+json.dumps(v,ensure_ascii=False,indent=2)+'\n```')
def pair(n,label,title):
    x=exchange(n,label);put('**'+title+'**（`'+SHORT[n]+'/exchanges[label='+label+']`）。完整请求与返回；认证上下文由适配器另行注入。')
    code(dict(request=x['request'],response=x['response']))

SYMBOLS={}
for n,b in S.items():
    for a,c in b['contents'].items():SYMBOLS[enc(c['ref'])]=SHORT[n]+'/'+a
for k,c in SH['components'].items():SYMBOLS[enc(c['ref'])]='K/'+k
for k,c in SH['capabilities'].items():SYMBOLS[enc(c['binding_ref'])]='Bind/'+k
def short(v):
    b=enc(v)
    if b in SYMBOLS:return '`'+SYMBOLS[b]+'`'
    if isinstance(v,dict):
        return '{'+', '.join(k+': '+short(x) for k,x in v.items())+'}'
    if isinstance(v,list):return '['+', '.join(short(x) for x in v)+']'
    return '`'+json.dumps(v,ensure_ascii=False)+'`'

put('''# 两个任务实例：逐字段输入输出与成本评估

日期：2026-09-28。本文延续同一实例评审，不修改正式方法或字段。蓝牙分“初始已开”和“初始关闭”两条独立轨迹；报告分“搜索、核实来源、综合、评估、保存、读回”。**所有轨迹都是合成的预期输入输出，没有运行 Harness、模型、官方网页、数据库或设备。**

先读第 1～4 节判断主线与关键成本，再按第 5 节追字段，第 6 节核对完整 JSON；第 7～9 节分别给出字节账本、异常边界和改进优先级。配套机器样例保存全部交接；正文包含字段来源、依赖和成本解释，不要求读者先读生成器。

## 1. 先回答简单任务是否过重

对“打开绑定手机蓝牙”，当前自然语言基线确实存在值得削减的固定开销：条件补全改变 `goal_revision` 后，原提案其余内容全部失效，必须新 `brain.decide`。因此，即使蓝牙已经开启，也需要两次串行模型生成后才读取一次状态；关闭分支再增加一次基于真实观察的规划。这个开销有契约依据，不能在算账时省略。

模块数量本身不能决定延迟。确定性字段、计划物化和最终汇总不需要模型，进程内 port 也不要求网络编码。真正可能拖累简单任务的是：将每份受控内容、每个用途、每次批准、每个原命令都做成独立远程往返；完整保存重复的请求、回执和来源清单；把可合并的本地提交拆成多次落库。下文将这些装配选择与不可省略的业务语义分开计量。
''')
table(['业务主链','Brain 物理生成','在线评估生成','Executor 操作','目标动作','确定性物化'],[
['B+：初始已开',2,0,1,'读状态 1',0],['B−：初始关闭',3,0,3,'读 2、设为 true 1',2],['R：报告正常',4,1,9,'搜索 2、获取 4、评估 1、写 1、读回 1',3]])
put('''报告需要 5 次模型生成，评估那一次包含在评估操作中，不能再当一轮 Brain。这里的“次数”是从脚本化轨迹逐项核对的预期次数，不是调用日志。首次查询即取得终结结果、无重试／分页／截断、一次综合同时生成报告与计划，都是负载假设；实际不满足时增加次数。

**判断：**本例不能证明目前实现已经慢或数据库已经过大，因为还没有运行实现和物理测量；但它证明自然语言简单任务至少要承受本基线的 2／3 次模型往返，而且一种完全展开的内容与权限装配会产生远多于业务主链的记录。应优先减少不必要的模型轮次与跨 owner 往返，不能用“字段多数是小整数”掩盖这两项成本。

### 1.1 固定装配与不成立时的行为

本样例把 H（Orchestrator）、B（Brain）、E（Executor）、C（内容 owner）、G（Grant owner）、V（批准 owner）作为逻辑边界；字节服务单独展示，H/B/E/界面各登记任务内临时副本，同一持有者复用准确版本缓存。逐条列出 `grant.use`／结算和 `evaluation.approval_check`，这是便于算账的**显式协议装配**，不是规范要求它们部署成六个服务。所有模型选择本地 profile；向该模型接收方交付材料仍有 disclose，模型计算另有 process。搜索／抓取外发的查询和 URL 使用 SEARCH_LOC／WEB_LOC，不拿调用端 LOC 冒充接收位置。生产默认分布式装配需实测哪些交接跨进程、库、可用区；本机可合并提交的路径另记网络增量为零。

权限预置的完整 GrantPolicy 见第 5 节：动作、目的、接收方、三个登记位置、精确资源集合和限额均明确列出。目标提交前的使用仅绑定已认证用户，尚不填不存在的 Task 身份。H 组装上下文、消费提案、计划物化、条件与结果归并各有有限 process；B 的校验/发布和 E 的确定性评估也分别登记。取得正文的 read 与交付给接收方的 disclose 分开核验。这里把本可同域完成的检查逐一序列化以便审计，不能把协议对数当成最低网络往返数。

两例均为一个用户、一个固定 Orchestrator、一个任务，无子任务／外部 Agent，无长期记忆需求、无 GUI。蓝牙使用有状态模拟设备的显式 API，**不代表真实手机平台已有支持**；报告使用虚构 Atlas/Boreal 1.0 和 `.example` 域名，四篇正文是本地 fixture，不是已核实的真实官方资料。选择虚构资料是为了审查处理链和字节关系，不能用报告内容做选型。

蓝牙 owner 提供原操作日志、状态版本和独占入口，样例中没有其他操作者；文件 owner 提供受控根、路径规范化、预期不存在、原写入身份及读回版本。缺少这些驱动合同、适用验证器、就绪实例、当前授权或缺陷门禁时，保持依赖缺口／拒绝相应新工作；不能猜 `verified`，也不能把未知 API 效果换成 GUI 重做。当前验收范围及这些依赖见[目标](../../docs/architecture/goals.md)、[执行](../../docs/architecture/execution/README.md)和[验证生命周期](../../docs/architecture/orchestrator/verification.md)。

### 1.2 事实、合成值与当前契约缺口
''')
table(['类别','本稿如何产生具体值','不能据此声称'],[
['协议字段','按当前 [protocol.schema.json](../../docs/architecture/contracts/schemas/protocol.schema.json)、[methods.json](../../docs/architecture/contracts/schemas/methods.json) 构造和校验','Schema 合法不证明服务已实现'],
['ID、版本、时间','首次用系统随机源分配 128 位 ID 并登记；版本来自夹具发布/状态提交顺序；时间来自固定虚拟时钟，每步 10 ms 只为排序','ID 不是模型猜测；时间差不是性能测量'],
['ContentRef / ComponentRef','对配套文件实际 UTF-8 字节计算 SHA-256 与长度；组件摘要指演示描述文件','组件描述文件不是已批准驱动二进制'],
['模型输出、驱动事实、判断','逐项脚本化预期值；生成阶段使用内部 `$local_ref`，保存后回填真实引用','没有真正模型推理、远端结果或质量/效果证据'],
['费用','演示 tariff：一项概念模型请求=1 `fixture_credit`；其他目标=0，纯用于验证去重与累计','fixture_credit 不是货币；没有真实 token、价格或服务费用'],
['权限／批准','假设受信数据库已有允许的 continuous Grant、准确安装锁与批准；每次使用回执是该前提下的期望输出','未伪造已完成的人类 Confirmation；这些值不能拿去授权真实服务'],
['尚未冻结的适配细节','内容 upload/download 准备、业务 intent_hash 投影、Requirement/plan 的预分配句柄、规则/缺陷/关闭证据登记均显式标为示例内部接口','不能从公开 Schema 反推出它们已存在或能互操作']])
put('''本文读取期间，另一项工作补入了[公共可靠工作框架](../../docs/architecture/reliable-work.md)。它复用接纳、Claim／Guard／Finish 与责任版本检查，不减少领域必需记录，也不要求只读调用全部异步化。下文 Job 仍是内部设计样例，不伪造 `job.*` RPC；本工作保留其他聊天对正式文档和 28 日评审的修改。

## 2. 共用身份、内容及每个值的来源

### 2.1 ID 和引用字典

下列完整值来自身份登记器。`B+`、`B−`、`R` 只是正文定位前缀，JSON 中均为完整 ID；两条蓝牙轨迹是独立的初始状态实验，不是同一设备状态在一次运行中互相矛盾。所有业务查询的 `target_id` 指原对象，租户来自认证上下文。
''')
table(['正文角色','具体 ID','来源与复用'],[[a, '`'+IDS[k]+'`',source] for a,k,source in [
('租户','tenant','认证适配器的 fixture 身份'),('H','orchestrator','受信放置目录，原提交后不换 owner'),('B','brain','已装配 Brain 服务'),('E','executor','已装配 Executor 服务'),('C','content-owner','准确内容 owner'),('G','grant-owner','原授权 owner'),('V','approval-owner','批准 owner'),('用户','user','受信用户会话'),('本地位置 LOC','local-host','已登记端点'),('SEARCH_LOC','search-provider-location','假设已获准的搜索接收位置'),('WEB_LOC','fetch-provider-location','假设已获准的网页获取接收位置'),('蓝牙目标','bound-simulator','会话绑定的模拟设备'),('文件根','managed-report-root','受信文件根登记')]])
table(['轨迹','Task ID','完成 Task.revision','goal/control revision'],[[SHORT[n],'`'+S[n]['task_id']+'`',obj(n,'Task-final')['revision'],'2 / 3'] for n in S])
put('''`goal_revision` 由 1→2 表示采用结构化条件；`control_revision` 同时 1→2，完成时 2→3 封闭新启动。`Task.revision` 是本夹具所选择的提交序列计数，不能当事务下界；每一步变化记录在各 `scenario.json.task_revision_log`。Decision/Operation 的 1→3 表示本样例接纳、发送阶段、结果阶段三份状态，墙上时间不参与跨对象排序。

以下 `K/...` 的版本均为配置给定的 `1.0.0`；digest 由实际描述字节计算，后续引用不得改成 latest。完整引用供字段表使用。
''')
table(['组件别名','id','digest','描述文件字节'],[[k,'`'+c['ref']['id']+'`','`'+c['ref']['digest']+'`',c['byte_length']] for k,c in SH['components'].items()])
put('''### 2.2 内容字典与真实正文

后续表中的 `B−/O1-output` 等指这里的唯一准确引用。所有引用共同的 `tenant_id`、`owner_id=C` 见上表，`version=1`；每行给出剩余字段和正文来源。`byte_length` 是原始正文 UTF-8 字节数，不含漂亮打印的换行，也不含引用 JSON。`application/json` 采用 ASCII 键、无浮点的紧凑排序编码；在本样例的数值/字符串范围内与 JCS 一致，不声称生成器是通用 JCS 库。

已安装目录、规则和策略在表中标“共享预置”，只读复用，不每任务重新发布。句柄内容是本次生成的内部示例：受信代码先分配 Requirement/plan ID，模型只能复制它们；当前标准并未冻结这项句柄注入协议，真实适配器落实前不能把随机猜到的 ID 当权威身份。
''')
for n,b in S.items():
    put('**'+SHORT[n]+' 内容**')
    table(['别名 / content_id','类型 / 字节','hash','产生方 / 来源（别名）'],[[a+'<br>`'+c['ref']['content_id']+'`',c['ref']['media_type']+' / '+str(c['ref']['byte_length']),'`'+c['ref']['hash']+'`',('共享预置' if c['preinstalled'] else c['producer'].split('_')[0])+'；'+(', '.join(SYMBOLS[enc(x)] for x in c['source_refs']) or '无派生来源；入口事实或配置')] for a,c in b['contents'].items()])
put('''每个新正文在 `content.put` 前已经存在准确字节，并固定 `content_id/version/upload_id/command_id`；owner 校验后才返回 ContentCommit。正文来自用户、驱动还是模型不会改变这个顺序。读取流程先登记副本，再拿限时 download 身份，实际字节不塞入 `content.get` 返回值。上传预留和下载流的完整绑定未冻结，本包只提供其输入/输出字节与已有领域方法，**不是可直接发往生产的完整传输录制**。

### 2.3 生成与存储成本代码

下面字段表复用这些成本代码。同一对象的一次查库、事务或模型调用按对象/交接记一次，不能按字段数重复相加；“复制”仍需要 CPU 和输出字节，只是不增加模型或外部取证。
''')
table(['代码','产生方、前序依赖和计算','产生代价','持久化与复用'],[
['I','身份/认证适配器：安全随机 ID；或复制已经认证的身份与已固定原 ID','新身份 O(1) 随机源；原 ID 为复制；分配随 owner 原事务登记','同一 operation/decision/use/command 恢复复用；不会每次重试再分配'],
['D','owner 的当前权威行：版本、状态、累计金额、选定证据','查当前对象/唯一键；版本在提交中单调推进；不是模型算数','版本化记录或当前行；旧修订不能覆写新投影'],
['C','受信配置/目录：类型、枚举、规则、Schema、上限、保留期','首次读配置/目录；精确制品可缓存，当前禁用/资格另查','共享配置不按任务重建；引用必须包含准确版本摘要'],
['H','字节处理：UTF-8、SHA-256、JCS 子集编码、签名','hash/复制 O(B)；签名每个 ControlSnapshot 一次；与模型无关','正文一份；同内容多个引用只累加引用 JSON，不再累加正文'],
['M','脚本化模型预期：动作/条件解释、短理由、比较文本、计划','本轮一次物理生成，所有这些字段共享 token/时延费用；本次实际为零模型运行','输出暂存/发布/Proposal 各有恢复责任；不保存隐含推理'],
['X','目标驱动：状态、搜索命中、HTTP 元信息、写入版本、读回字节','本次 O 的目标请求；字段解析/结构校验为本地 CPU','Operation 与准确输出/凭据关联；不能靠模型填这些事实'],
['V','受信确定性比较/评估归并：当前条件、准确成果、证据、门禁','已持有证据则 CPU/查库；需要新取证或模型时另计 Operation','ConditionCheck 固定输入与判断；当前 applicability 单独保存'],
['A','Grant/批准 owner：当前主体、资源、用途、窗口、额度及原 use','事务内锁当前资格并裁决；跨 owner 才增加 RPC；grant.check 不消费','UseReceipt 原决定不可变，结算投影独立；缓存不是新授权']])
put('''## 3. 蓝牙：每一步交接具体是什么

目标正文包含 `user_text="打开绑定手机蓝牙"`、真实 fixture 的 `device_id`、`desired=true` 和受信绑定来源。适配器选择“设为 true”，没有 toggle。观察输出的字段为 `device_id:string`、`enabled:boolean`、`state_version:integer≥1`、`observed_at:UTC string`，全部必填；设置输入另有 `desired:true`、`expected_state_version:integer≥1`，防止把过时观察直接当作当前状态。

```mermaid
sequenceDiagram
    participant U as 交互
    participant H as Orchestrator
    participant B as Brain
    participant E as Executor与模拟设备
    U->>H: goal_ref与原submit命令
    H->>B: D1：g1，requirements为空
    B-->>H: 条件 + 观察建议
    H->>H: 固定g2/c2，丢弃同份观察建议
    H->>B: D2：g2的固定快照
    B-->>H: 读取状态行动
    H->>E: O1 observe
    E-->>H: 准确状态正文与原Operation
    alt enabled=true
      H->>H: 确定性条件pass，固定Result
    else enabled=false
      H->>B: D3：g2与O1真实观察
      B-->>H: 两步计划；actions为空
      H->>H: 安装计划；下一decide零模型物化
      H->>E: O2 desired=true，expected_state_version=1
      E-->>H: 原设置已应用且不会迟到，state_version=2
      H->>E: O3 observe_after
      E-->>H: enabled=true，state_version=2
      H->>H: 条件pass，固定Result
    end
    H->>E: 终态控制c3，封闭新启动
    U->>H: task.read / task.result
```

图的箭头是业务交接；内容、授权和记录展开在第 5～7 节，每条 O 均先持久接纳、启动检查、保存实际事实，再由 H 查询归并。图不把 `execution.invoke` 的 applied 画成设备成功。
''')
table(['步骤 / 交接','真实输入值与前序字段','输出与谁产生','持久事实 / 成功含义'],[
['B0 交互→C→H','B−/goal；原 command；用户预算 fixture_credit=20、deadline=02:00Z','Task r1、g1/c1、requirements=[]；H 接纳事务','目标、Task、预算、原回执、首 decide；没有设备动作'],
['B1 H→B→H','D1-context.snapshot_revision=2、goal_revision=1；目录含 observe/enable','D1 Requirement(kind=effect,required=true,rule_ref=K/bluetooth-enabled-rule)；脚本化解释','消费 D1，Task r3 g2/c2；观察 action 被废弃'],
['B2 H→B→H','D2 新快照、同一规则；没有设备状态可猜','D2 action.arguments={device_id:绑定设备}；H 准入 O1','新 decision_id 与新快照；首 O 的来源必须是 D2'],
['B3 E→资源 owner/驱动','resource.acquire 得 control_epoch=1；O1 读原设备','B+ enabled=true / B− enabled=false；state_version=1；观察时间来自驱动','原 O1 r3 closed/applied/may_apply_later=false，H 按 r3 归并'],
['已开：H 确定性核验','同一 device_id、true、30 秒内观察、无未知效果、缺陷门禁有效','ConditionResult verdict=pass/basis=verified；Result 指 O1-output','零次额外模型；保证只到观察时点和规则新鲜度'],
['关闭：B 规划→H 安装','D3 的 O1-output.enabled=false/state_version=1','BrainPlan v1，enable→observe_after；actions=[]','Task 安装计划但不准入首步；计划是内容，不是授权'],
['关闭：H 物化→E','O2 arguments={device_id,desired:true,expected_state_version:1}','设置输出 state_version=2、enabled=true、closed=true，绑定原 operation_id','PlanStepAdmission(enable) 唯一；效果核清后才启动后项'],
['关闭：H 物化→E','O3 arguments={device_id}，depends_on enable','新观察 true/v2；非复用设置回执假装再观察','read-after 独立 operation，CPU 物化不新增 Brain'],
['结束 H→E/交互','当前一项必要条件 pass；原效果与迟到性均核清','Task succeeded，c3；执行端 ControlReceipt enforced=3；读 Task/Result','释放资源租约；副本停止使用但物理清理仍 pending，费用/清理可独立继续']])
put('''蓝牙两分支完整字段见各正文/对象字典。下面三份正文就是关闭分支的真实 fixture 字节内容，摘要与第 2 节对应；它们是驱动应交付的期望值，不是设备实测。
''')
for a in ['O1-output','O2-output','O3-output']:
    put('**B−/'+a+'**');code(S['bluetooth-off']['contents'][a]['body'])
put('''## 4. 报告：来源、候选、评估和文件怎样贯通

用户目标明确了产品版本、三个比较维度、受控根与相对路径。`.example` 只用于 fixture。本例由固定 `K/official-source-registry` 给出 Atlas/Boreal 1.0 的主机登记；它是受信来源配置的合成前提，不能证明真实官方身份。获取驱动为每份正文交付请求 URL、全部重定向、最终 URL、时间和准确 body_ref；搜索摘要只帮助选页。这里四篇合成正文分别为：
''')
for i in range(1,5):
    put('**R/source-'+str(i)+'**\n\n```text\n'+S['report']['contents']['source-'+str(i)]['body'].rstrip()+'\n```')
put('''```mermaid
flowchart TB
    G[原目标与三个条件] --> D2[D2 搜索决策]
    D2 --> S[O1/O2 两份搜索输出]
    S --> D3[D3 从命中复制四个URL]
    D3 --> F[O3到O6 获取正文与来源]
    F --> D4[D4 同轮生成报告与计划]
    D4 --> P[保存正文后回填引用并安装计划]
    P --> A[O7 官方来源与引用定位核对<br/>一次模型评估质量与语义支撑]
    A --> Q{两个当前条件均pass}
    Q -->|否：fail或unknown| W[修订或补证责任]
    Q -->|是| O8[O8 写入受控根]
    O8 --> O9[O9 绑定原file_version读回]
    O9 --> V[确定性字节核对与最终汇总]
```

图中评估通过与写入效果是不同条件；一条箭头表达一个依赖，不把四篇来源当作四个独立事实真值。计划发布不执行首步，三次物化均零模型。
''')
table(['交接','具体输入（来源）','输出与下游','模型/目标增量'],[
['R0→R1 接纳/理解','R/goal 的 Atlas/Boreal 1.0、维度与路径','D1 提出 quality、citation、file 三项条件；g2/c2，搜索建议失效','Brain +1'],
['R2 搜索决策','D2 的 g2、规则和准确 search 声明','两个 Action；query 为 Atlas/Boreal 1.0 deployment limits maintenance','Brain +1'],
['O1/O2 搜索','product/version/official_host/query 全部字符串','各 2 个 hits：url/title/snippet/official_host；retrieved_at 来自驱动','搜索各1，共2'],
['D3 选页','两份原搜索输出，来源不靠模型记忆','4 个 fetch Action；URL 从 hits 精确复制','Brain +1'],
['O3～O6 获取','url、official_host；独立已准入 operation','url/final_url/redirect_chain/retrieved_at/http_status=200/body_ref；body_ref 指 source-1～4','正文请求各1，共4'],
['D4 综合/计划','g2、四篇正文、来源封套、规则、准确评估/文件合同','内部 contents[report,plan]；C 固定 report 引用，再回填 plan 和 Proposal','Brain +1，无额外摘要生成'],
['O7 评估','task_id,g2,report准确ref,两条件ID,两rule_ref,四source_ref、四fetch_evidence_ref、官方登记ref','judgments[quality,citation]；origin_checks[4]、citation_checks[4]；固定 model/prompt/evaluator；limitations','评估模型 +1；来源/定位为CPU，无新网页'],
['H 条件归并','原 O7/result_ref 和全组成依据','quality pass/assessed；citation pass/assessed，依赖官方来源、定位与语义检查','零模型；缺陷门禁覆盖全部组成实现'],
['O8 写入','root_id、reports/comparison.md、expected_absent=true、同 report ref','file_version=1；content_hash/byte_length 与候选一致；原 operation_id、closed=true','文件写动作1；底层I/O不等于1'],
['O9 读回','expected_file_version 从原 write 输出 /file_version 解析','file_version=1，独立 readback-bytes ref；hash与候选相等，content_id不同','文件读动作1'],
['H 完成/交互','三个必要条件对同一 report；无未知或可能迟到效果','file pass/verified；整体 Result assessed；Task/Result查询及报告获准读取','零汇总模型']])
put('''### 4.1 为什么不能让模型直接填报告引用和文件版本

D4 返回的是 `brain-generation/1` 的内部结构。模型可以生成报告文本、计划模板和 `$local_ref:"report"`，无法预知 owner 会分配的 ContentRef。Brain 先持久准备发布身份，再按依赖顺序保存 report、把准确引用回填 plan、保存 plan，最后形成公共 Proposal。报告、plan 的 SHA-256 和字节数都由实际产物计算；“同一轮生成两个正文”不增加第二次模型调用，但增加两次内容发布及来源元数据。

readback 模板在发布时故意不填 `expected_file_version`。它使用 `argument_bindings.target_pointer=/arguments/expected_file_version`，`source.kind=step_output`、`step_id=write`、`source_pointer=/file_version`。H 从当前计划版本的唯一准入映射找到原 O8，再从已核实输出取 1。未来版本不存在于 D4 输入，任何预填“1”都不能被当作模型知道了未来。

### 4.2 评估和保存的边界

O7 的 `effect=applied` 只表示评估报告已经形成，`judgments[].verdict` 可以是 fail。write 的两个 pass_conditions 同时绑定当前 g2 的两个 Requirement、固定 rule_ref 和同一 report 引用；只有都 usable/pass 才能准入。

引用条件 R-C 由三项同时成立：官方来源身份、准确定位、语义支撑。O7 从受信组件目录解析固定来源登记，再以 `fetch_evidence_refs` 对照每份正文的 `body_ref`，逐项核对请求、全部重定向和最终 URL 的 HTTPS 主机及版本路径。确定性定位保存 source_ref、UTF-8 起止字节、quote、matched，并将报告实际 citation_key/cited_url 与原抓取请求绑定；只验证摘录存在而不验证报告链接不能通过。语义支撑仍来自一次固定评估模型。
H 导入 O7 的组成证据，`check:citation` 直接读取 `O7.judgments[requirement_id=R-C]` 的语义判断，同时依赖 official-origin、citation-location 两项确定性检查；quality 是 write 的另一项独立 pass_condition，不能替代引用语义判断。任一组成 fail 则 citation fail，均 pass 才 pass，其余为 unknown；不再发起模型或网页请求。
登记缺失、封套缺失、跨域跳转、错误链接、错字节或任一组成结果未知都不能形成可用于写入的 citation pass。生产若要允许官方 CDN/不同版本路径，须修改准确登记与规则制品；不能临时把任意跳转目标视为官方。

文件输出中的 `content_hash` 从已取得候选字节计算，`file_version` 由文件 owner 的提交日志分配，`closed` 来自原写入不再发送/不迟到的证据。读回内容是新观察，具有不同 ContentRef 身份与来源；即便存储层按 hash 去重物理正文，也不能合并写入与读回的事实身份。R-F 检查路径、版本、原写入关闭和准确字节，最终保证仍为 assessed，因为质量两条件依赖评估。

## 5. 字段查阅：类型、必填性、具体值、来源和代价

表中“必填”以精确 Schema 为准，附加业务条件在来源列说明；枚举只展示本例选值，其余合法值见正式 Schema。公共对象复用第 2 节字典；子对象/数组按其专表展开。每行 B 为 `"字段名":值` 的紧凑 UTF-8 字节，不含外层 `{}` 和字段间逗号；**只在同一对象同一层相加**才等于对象大小，嵌套表不能再加一次。

`I/D/C/H/M/X/V/A` 成本含义见 2.3。字段既有直接产生者，也有读取/复制它的下游；下列表头给定主交接方向，来源列说明它实际依赖哪项前序事实。`DecisionRecord.proposal`、actions、plan_delta、steps 和评估 judgments 的首次产生计入所属模型调用；下游复制不抹去首次成本，也不重复收费。`model_call` 是适配器汇总的持久事实，子字段才分别来自身份、供应商和计量。相同字段名按对象区分，例如 ContentControl 与 Task 的 control_revision 互不替代。完整实例均在对应 `scenario.json`，原请求/响应没有使用正文别名。
''')

# Field dictionaries: explicit exceptions first, then mechanism-based fallback.
SPECIFIC={
 'proposal':('M','本 Decision 的模型/规则输出，经 Validator 校验和 publication 局部引用解析后写入；本例每 D 一次模型生成，各字段共用该次成本'),
 'model_call':('D','Brain 模型适配器汇总已持久的准备、发送、供应商回执和计量事实；子字段分别追身份、驱动和账本，不是固定配置'),
 'actions':('M','本 Decision 生成的行动建议，经 Validator 校验；plan_delta 分支必须为空；所有行动共享本 D 一次模型成本'),
 'plan_delta':('M','本轮提出计划替换，publication 将局部引用解析成已保存 next_plan_ref；安装不同时准入行动'),
 'steps':('M','本轮生成的有限步骤及依赖，Validator 核对 DAG/模板，publication 回填准确引用；和报告共享 D4'),
 'action_key':('M','模型在本份提案内选定的局部键，Validator 检查唯一；不是受信 operation_id'),
 'target_pointer':('M','模型按能力输入 Schema 给出确定性复制落点，Validator 检查 JSON Pointer 与目标类型'),
 'source_pointer':('M','模型按前项输出 Schema 给出取值路径；真实值只能由后续物化读取'),
 'source':('M','计划生成的前项输出绑定声明；H 按同 plan/step 唯一映射解析，不能执行表达式'),
 'attempts':('D','Executor 从原操作持久的准备/发送记录汇集 Attempt；不由模型报告执行经历'),
 'target_key':('I','从原 operation_id 确定性复制到目标幂等键；重试必须复用原键'),
 'control_snapshot':('D','H 读取当前 TaskGate，绑定执行端与窗口后签名；不是模型产物或静态配置'),
 'gate':('D','TaskGate 当前状态的准确投影，H 裁决、E 单调应用；与内容门禁独立'),
 'entrances':('D','E 从实际发送入口逐项汇总已落实的控制修订；缺失入口保留 gap'),
 'user_control':('X','资源 owner 当前用户接管事实；不是 Harness 根据愿望写 false'),
 'lease':('D','资源 owner 当前唯一占用租约记录；查询仅复制原事实'),
 'approval_revision':('A','批准 owner 当前已核验的批准行修订，回执记录本次实际使用版本'),
 'redirect_chain':('X','抓取驱动按跳转顺序记录完整中间 URL；空数组仅表示本次无跳转'),
 'fetch_evidence_refs':('H','从 O3～O6 原结果复制准确抓取封套引用，与 source_refs 一一绑定'),
 'official_registry_ref':('C','固定受信来源登记制品；E 从组件目录解析正文，不能信页面自称官方'),
 'origin_checks':('V','O7 确定性核对登记主机、版本路径、全部跳转及封套 body_ref；无新模型/抓取'),
 'requested_url':('X','原抓取封套 url；从原搜索命中到原请求的精确链'),
 'registered_host_match':('V','对请求/每跳/最终 URL 解析 HTTPS 主机，与固定来源登记逐项相等'),
 'redirect_chain_checked':('V','原封套具有完整 redirect_chain 且 body_ref 等于被引用正文；缺失就不通过'),
 'version_match':('V','全部来源 URL 路径匹配登记版本；本例规则为 /1.0/，其他版本规则需固定制品'),
 'tenant_id':('I','认证会话；不是请求正文指定租户'),
 'goal_ref':('H','用户准确 goal 字节发布后的 ContentRef'),
 'goal_revision':('D','H 当前条件版本；D1条件变更后为2'),
 'snapshot_revision':('D','H 固定输入时的 Task.revision；后续只能复制'),
 'control_revision':('D','H 当前门禁；条件变化/终态分别递增'),
 'revision':('D','所属 owner 的提交序列；不同对象不比较'),
 'version':('C','Component为安装版本；Content为owner发布版本'),
 'hash':('H','准确原始正文 SHA-256'), 'digest':('H','准确描述制品字节 SHA-256'),
 'byte_length':('H','已编码 UTF-8 字节长度，非字符数'),
 'provider_request_id':('X','供应商回执；本例 fixture-provider/model_call_id'),
 'usage':('X','原计量owner/fixture tariff；非模型自报金额'),
 'usage_final':('X','原计量方可信最终账单/无收费依据；本例脚本化'),
 'intent_hash':('H','fixture-intent-v1准确投影；生产投影合同尚未冻结'),
 'orchestrator_proof':('H','原H用fixture P-256密钥签准确gate/受众/窗口；不是占位字串'),
 'requirements':('M','D1复制用户约束，规则来自固定配置，ID从已分配句柄复制；H审查接纳'),
 'requirements_proposal':('M','基于base_goal_revision=1；若采用改变条件，整份其余提案失效'),
 'requirement_refs':('M','从当前requirements复制，模型不能增授权'),
 'rule_ref':('C','从TaskPolicy/安装锁允许的准确规则集合取得'),
 'evaluator_ref':('C','按固定候选顺序选择实现；当前资格另查门禁'),
 'artifact_ref':('H','已发布的准确候选/观察；不得靠模型猜hash/版本'),
 'rationale':('M','本轮短理由；非隐含推理'), 'instruction':('M','本轮计划步骤说明'),
 'arguments':('M','用户参数/真实观察/精确声明→提议；未来值由H确定性物化'),
 'action_template':('M','完整固定模板；缺未来值仅由声明的argument_bindings补齐'),
 'argument_bindings':('M','JSON Pointer复制映射，不允许表达式或外部动作'),
 'depends_on':('M','有限计划DAG；发布时无未来operation_id'),
 'pass_conditions':('M','本轮模型按既有 Requirement/rule 提出门禁，Validator 核对；H 物化再检查当前准确条件与成果'),
 'source_refs':('H','实际处理清单/前序来源闭包；由可信适配器继承，不许模型删减'),
 'sources':('H','source_ref+关系+原观察时间+policy_ref；沿已发布来源回填'),
 'input_manifest':('H','本轮真实使用的全部准确资料清单；CPU组装/去重，不新生成摘要'),
 'facts':('D','原owner可查的对象修订和准确输出；不是模型陈述'),
 'materials':('H','获准正文及来源；能力完整输入/效果合同作为材料补齐'),
 'gaps':('D','组装器实际缺项；本例[]是假设依赖全部齐备'),
 'assumptions':('M','解释中声明的前提；不当正式证据'),
 'unresolved_effects':('D','所有已准入原操作的权威当前集合；不可截断后填空'),
 'open_effects':('D','H权威未结效果投影，本例仅展示无未知的最终状态'),
 'result_ref':('H','已提交的原输出/Result字节引用；效果成立须另核验'),
 'evidence_refs':('H','从准确已存在输出/目标收据关联；本身不证明真实性'),
 'target_receipt_ref':('X','目标可核对的原操作凭据；本例等于设置/写入输出引用'),
 'may_apply_later':('X','驱动原操作停止/完成事实，不从超时推断false'),
 'effect':('X','目标/原操作凭据经固定效果谓词解释'),
 'execution_state':('D','Executor发送过程投影，独立于效果'),
 'verdict':('V','固定条件、准确成果、完整证据及当前资格；评估判断不冒充确定性'),
 'basis':('V','确定性效果verified；开放质量assessed'),
 'completion_basis':('V','按全部必要条件中最弱依据归类'),
 'condition_results':('V','当前选定、适用的全部必要条件结果'),
 'limitations':('V','固定方法局限与本例证据范围；本例明确非运行'),
 'policy':('A','受信保存策略与来源限制交集；模型无权放宽'),
 'policy_ref':('C','Task为固定ComponentRef；内容为owner保存的ObjectRef'),
 'decision':('A','Grant锁内allowed/denied；不等于实际行动'),
 'grant_revisions':('A','实际锁内核验的原Grant版本'),
 'authorization_refs':('A','现有Grant/Use依据；启动前重新核验'),
 'usage_authorization_refs':('A','本轮处理/读取用途的依据；Decision接纳时可给Grant，实际使用另消费'),
 'grant_refs':('A','受信预授权owner/ID/revision，不接受模型给许可'),
 'resource_scopes':('A','类型规范化器验证资源/集合成员，不按模型字符串前缀放行'),
 'recipient':('A','受信接收方登记；来源许可必须覆盖'),
 'location':('A','实际接收/处理端点登记；本地用 LOC，查询/URL 外发分别用 SEARCH_LOC/WEB_LOC，不能用调用端冒充目的地'),
 'cost_bound':('C','原能力/profile的strict声明；fixture额度非真实货币'),
 'max_cost':('C','可信fixture tariff最大值，不由模型估算'),
 'max_units':('C','动作1或内容准确字节长度，使用身份固定后不增大'),
 'spent_cost':('D','原账累计差额；Task只选一个费用权威'),
 'cumulative_cost':('X','计量owner原累计账；不能重复叠加历史值'),
 'held_cost':('D','未结原预留；未知不能因超时释放'),
 'released_cost':('D','原使用最终关闭后的未支出额；不重开once身份'),
 'closure_ref':('D','原使用端封闭及最终用量的内部可核验证据；接口未冻结'),
 'use_stopped':('D','持有者已停止新使用；独立于物理删除'),
 'physical_state':('D','本例pending，未提供删除证据就不写complete'),
 'upload_id':('I','受信上传准备先分配，入口未冻结；模型不能生成'),
 'download_id':('I','内容owner限时下载准备；字节通道另传'),
 'start_before':('A','按当前策略/许可/控制窗口取最小值；原回执重放不续期'),
 'expires_at':('C','命令首次接纳截止、下载/租约有效期分别配置'),
 'deadline':('C','用户/策略确定绝对业务期限；不能用命令过期替代'),
 'expected_revision':('D','调用方从原owner当前记录取；冲突先读再决定'),
 'control_epoch':('D','资源owner控制代次；不是Task.control_revision'),
 'expected_control_epoch':('D','原资源占用代次；释放不得覆盖用户接管'),
 'expected_file_version':('X','原写入输出/file_version，由H按step_output复制'),
 'expected_state_version':('X','D3从O1准确观察state_version=1复制，目标启动时比较'),
 'enabled':('X','模拟设备当前状态寄存器；本次是脚本化true/false期望值'),
 'desired':('M','由用户“打开”确定为true，不能改成toggle'),
 'state_version':('X','目标owner的状态提交日志单调版本'),
 'previous_state_version':('X','目标owner原设置操作提交前读取到的版本'),
 'file_version':('X','文件owner原写入/读回日志；模型不知道未来值'),
 'content_hash':('H','文件实际候选/读回字节SHA-256，驱动核对后报告'),
 'closed':('X','原目标操作不再发送且不可能迟到的凭据；非超时推断'),
 'product':('I','原用户目标明确的比较对象；复制，不另猜产品'),
 'official_host':('C','受信产品来源登记；本次.example为虚构fixture，未核实真实官方性'),
 'query':('M','本轮用原产品/版本与比较维度构造搜索文本'),
 'hits':('X','实际搜索提供方返回的有限候选；本次脚本化'),
 'url':('X','搜索命中复制至获取输入；不能当已获取正文'),
 'final_url':('X','获取驱动实际重定向完成地址；本例无重定向'),
 'http_status':('X','HTTP驱动状态；200不证明内容质量'),
 'retrieved_at':('X','搜索/获取驱动的受信时钟；本次虚拟时间'),
 'title':('X','搜索提供方候选标题'), 'snippet':('X','搜索提供方摘要；仅供定位'),
 'relative_path':('I','受信入口从用户目标取得，文件规范化器再次检查'),
 'expected_absent':('C','示例文件合同：预期不存在；目标owner启动时核验'),
 'judgments':('M','O7 固定评估模型对质量/语义支撑的输出，适配器绑定规则与准确输入后保存；共享一次评估生成，不是配置复制'),
 'citation_checks':('V','确定性定位结果，须与语义支撑组成记录共同使用'),
 'quote':('H','原来源UTF-8区间解码得到，不能从搜索摘要替代'),
 'byte_start':('H','在准确来源字节中确定起始偏移'),
 'byte_end':('H','起始偏移+摘录UTF-8长度，半开区间'),
 'matched':('V','原始字节切片与quote逐字比较'),
 'reason':('V','条件判断的明确依据/缺口；本次不是实际模型结论'),
}

for key in ['media_type','schema_version','description','input_schema','output_schema','effect_class','verification','retry','authorization','limits','evidence_kinds','query_supported','cancel_supported','max_attempts','initial_backoff_ms','max_backoff_ms','reconciliation_timeout_ms','key_scope','key_retention_ms','requires_lease','requires_confirmation','max_duration_ms','max_input_bytes','max_output_bytes','max_physical_requests','mutex_domains','max_output_tokens','max_actions','max_context_requests','range_supported','normalizer_version','max_offline_window_ms','offline_allowed']:
    SPECIFIC[key]=('C','由该对象所属协议版本、准确 capability/profile/规范化器或存储实现声明提供；按安装锁读取，本次没有模型生成')
for key in ['subject','resources','recipients','locations','purposes','classification','allowed_locations','allowed_recipients','allowed_purposes','valid_from']:
    SPECIFIC[key]=('A','由受信授权/内容策略制定者按认证主体、规范资源、目的地与来源限制确定；本例为明确预置假设，模型不得签发或放宽')
for key in ['kind','type','required']:
    SPECIFIC[key]=('C','按本对象 Schema 分支或固定规则分类；模型输出对象的分类来源另按类型覆盖')
SPECIFIC.update({
 'citation_key':('H','从准确候选的引用定义解析 A1/A2/B1/B2，和本条来源证据关联'),
 'cited_url':('X','原抓取请求 url；确定性检查与报告对应引用定义逐字一致'),
 'report_link_matches':('V','候选引用键实际解析的 URL 与原抓取请求一致；缺失或改链时引用条件不通过'),
 'fetch_evidence_ref':('H','固定 O3～O6 中绑定本 source_ref 的原抓取封套；不靠模型补正文身份'),
 'unit':('C','原能力/计量合同给定；fixture_credit、byte、invocation 为不同单位，禁止混加'),
 'amount':('D','原计量方/账本给定精确十进制量；本字段复制既有金额，不从模型估价'),
 'actor_kind':('I','认证适配器的主体分类；本例 user'),
 'cursor':('D','查询 owner 对固定查询状态编码；本例未分页，无生成成本'),
 'constraints':('I','从原用户提交复制明确约束；本例空数组，不隐含新模型解释'),
 'budget':('D','Submit 从受信用户上限生成；Task 从原账投影 limit/spent/reserved，各数有唯一来源'),
 'delegation_context':('I','受信委派入口提供父任务与分配绑定；本例未委派，不产生此对象'),
 'capabilities':('C','H 从准确能力/绑定目录组装模型可见清单；完整目录为输入材料，不由模型自报可用工具'),
 'availability':('D','目录/执行宿主对当前准确绑定的可用性事实；不是能力静态存在即 ready'),
 'purpose':('A','调用方为本次有限使用选择目的，G 核对授权覆盖；来自原动作而非模型扩大许可'),
 'action':('A','由实际 read/process/store/act/disclose/manage 工作确定，G 分别裁决，不互相隐含'),
 'gui_precondition':('X','最近获准 GUI 观察及目标前提经受信适配器绑定；本例纯 API，不生成'),
 'resource_type':('C','资源 owner 登记的类型与规范化器合同，不能把任意路径字符串当范围'),
 'selector':('A','受信规范化器确认 object_ids/versions 的实际归属与范围，再供 Grant 匹配'),
 'action_kind':('C','批准接口的工作类别；本例 work，原 action_id 另绑定具体工作'),
 'relation':('H','发布适配器根据真实派生/观察关系写 SourceBinding；不是模型自由删改来源'),
 'mode':('C','方法/策略选择的模式；Content 为 bytes，Grant 为预置 continuous，含义按所属类型'),
 'residual_reason':('X','持有者实际清理失败/残留原因；本例 pending，无虚构删除凭据'),
 'gap':('D','发送入口尚未落实当前控制的实际缺口；本例无缺口是合成前提'),
 'base_goal_revision':('D','复制本 D 输入的 g1，H 采用前比较当前目标修订'),
 'role':('H','H 按实际输入用途分类；本例材料为 evidence，不改变来源权限'),
 'versions':('A','规范化器确认的精确资源版本筛选；本例 selector 只用 object_ids'),
})
for key in ['source_ref','context_ref','content_ref','body_ref','artifact_refs','next_plan_ref']:
    SPECIFIC[key]=('H','准确正文已发布后从其 ContentRef 复制；首次字节编码/hash 在原发布计，当前字段仅复制，不能靠模型预造引用')
for key in ['plan_ref','base_plan_ref','object_ref','reservation_ref','cost_reservation_ref']:
    SPECIFIC[key]=('D','从当前已保存的计划/前序事实/原预算预留复制准确版本；该引用不新增模型或目标调用')
for key in ['capability_refs','capability_ref','binding_ref','model_profile_ref','driver_ref','configuration_ref','predicate_ref','not_applied_rule_ref','replay_guarantee_ref','rule_refs','prompt_ref']:
    SPECIFIC[key]=('C','受信安装/能力目录按准确版本取得；模型可选择允许项，不能发明已安装实现；配置冷读按目录整体计一次')
SPECIFIC.update(task_ref=('I','固定原 orchestrator_id/task_id 的二元身份；复制接纳绑定，不重新选 owner'),target_ref=('C','准确 Binding 中的受信目标 owner/id/revision，由绑定目录提供'),trusted_user_session_ref=('I','认证适配器的受信用户会话证明；本例未用此可选字段'),parent_grant_ref=('A','受信签发的父许可引用；当前父链逐次核验，本例未委派'),delegation_ref=('A','受信委派许可/分配绑定；本例没有此项，不以模型请求替代'))
TYPE_SOURCE={
 ('ContentRef','version'):('D','C 为准确 content_id 分配的不可变发布版本；独立于组件安装版本'),
 ('ContentCommit','control_revision'):('D','C 对此准确内容版本保存的 ContentControl 修订；本例 1，与 H 的 Task c2/c3 独立'),
 ('ContentBytesGetOutput','control_revision'):('D','C 在当前下载资格检查时读取的 ContentControl 修订；不是 Task.control_revision'),
 ('DecisionRecord','proposal'):SPECIFIC['proposal'],
 ('DecisionRecord','model_call'):SPECIFIC['model_call'],
 ('Task','requirements'):('D','H 接纳 D1 提出的条件后保存，后续 Task 仅复制；解释成本计 D1 一次，不随每次投影重复'),
 ('BrainContext','requirements'):('D','H 从当前 Task 条件集合复制；首次解释成本属于 D1，本轮组装不再次生成条件'),
 ('BrainContext','assumptions'):('D','H 根据已知部署前提和缺口写入上下文；不是另一次模型假设生成'),
 ('Invoke','arguments'):('H','H 从获准 Action/计划模板复制，step_output 按原 Operation 输出确定性补齐；不新增模型'),
 ('Requirement','kind'):('M','D1 按用户目标与既有规则分类，由 H 校验接纳；共享 D1 生成'),
 ('Requirement','required'):('M','D1 对原目标提出必要条件，H 确认；不能把用户必需条件降为可选'),
 ('Proposal','kind'):('M','本轮模型/规则选择 act 分支，经 Validator 校验；不是静态配置'),
 ('ActionInvoke','type'):('M','本轮 Action 选择 invoke 类型；H 再按 Schema 校验'),
 ('BrainOutputSource','kind'):('M','D4 选择 step_output 绑定语法，实际未来值由 H 物化复制'),
 ('RuntimeCapabilityAuthorization','actions'):('C','能力提供方声明实际动作所需权限种类；不是 Proposal.actions，也不能替用户授权'),
 ('GrantPolicy','actions'):('A','受信签发者预置允许动作集合；本包是授权假设，不是本轮用户签发记录'),
 ('GrantPolicy','limits'):('A','受信签发者给定各单位总限额；Grant 逐项检查当前余额，本包仅验证样例包含关系'),
 ('AssessmentJudgment','verdict'):('M','O7 评估模型按准确候选/规则/来源输出；通过不等于确定性正确，共享一次评估'),
 ('AssessmentJudgment','reason'):('M','同一 O7 评估输出的短依据；本例为脚本化期望，不另开一次模型'),
 ('AssessmentJudgment','basis'):('C','评估适配器声明 assessed，模型无权提升为 verified'),
 ('SourceBinding','observed_at'):('H','发布适配器记录本次来源关联时刻；正文原 observed_at/retrieved_at 仍从驱动保留'),
}

def fallback(key,name):
    if (name,key) in TYPE_SOURCE:return TYPE_SOURCE[(name,key)]
    if key in SPECIFIC:return SPECIFIC[key]
    if key.endswith('_id') or key.endswith('_ids') or key in ['id','use_id','target_id']:
        return ('I','受信分配或从前序固定身份复制；恢复保持原ID')
    if key.endswith('_ref') or key.endswith('_refs'):
        return ('C','从已查询/发布的准确对象或配置复制；对象是否当前可用仍核验')
    if key.endswith('_at') or key.endswith('_until') or key in ['issued_at','sent_at','prepared_at']:
        return ('D','对应owner受信时钟；本例虚拟时间只表示顺序')
    if key in ['status','state','next_action','wait_reasons','accounting_open','control','enforced_control_revision']:
        return ('D','领域当前事实或未结责任派生；不把回执阶段当任务成功')
    if key in ['limit','spent','reserved','reserved_units','reserved_cost','cumulative_units','spent_units','held_units','released_units','usage_revision','consumed_once','final']:
        return ('D','配置上限和原累计账确定性运算；在所属账本事务保存')
    if key in ['method','stage','output','payload','request','response','error','redacted','observed_at','resource_revision']:
        return ('D','按methods登记和原领域记录编码/投影；不产生新业务事实')
    raise ValueError('Unreviewed field provenance: '+name+'.'+key)

def typename(s):
    if '$ref' in s:return s['$ref'].split('/')[-1]
    if 'const' in s:return 'const '+json.dumps(s['const'],ensure_ascii=False)
    if 'enum' in s:return 'enum'
    if 'oneOf' in s:return ' / '.join(typename(x) for x in s['oneOf'])
    t=s.get('type','object')
    if t=='array':return 'array<'+typename(s.get('items',{}))+'>'
    return t

def fieldtable(name,value,where,direction,schema=None):
    schema=cp(schema or DEFS[name])
    while '$ref' in schema:schema=cp(DEFS[schema['$ref'].split('/')[-1]])
    if 'oneOf' in schema:
        schema=next(x for x in schema['oneOf'] if x.get('properties',{}).get('kind',{}).get('const')==value.get('kind'))
    put('### '+name+'\n\n'+direction+'。示例定位：`'+where+'`；所示完整对象编码 '+str(len(enc(value)))+' B。')
    rows=[]
    for k,s in schema.get('properties',{}).items():
        required='是' if k in schema.get('required',[]) else '否/按分支'
        cost,source=fallback(k,name)
        if k in value:
            v=value[k]
            # Inline object/array structures have their own typed field tables.
            if isinstance(v,dict) and enc(v) not in SYMBOLS:
                shown=short(v) if len(enc(v))<360 else '对象（字段 '+', '.join(v)+ '）；下方子表或第 6 节完整 JSON'
            elif isinstance(v,list) and len(enc(v))>450:
                shown=short(v) if all(enc(x) in SYMBOLS for x in v) else str(len(v))+' 项；见 '+typename(s)+' 子表与 `'+where+'.'+k+'`'
            else:shown=short(v)
            size=len(enc(k))+1+len(enc(v))
        else:shown='本例不出现';size=0
        rows.append(['`'+k+'`',typename(s),required,shown,source,cost,size])
    table(['字段','类型','必填','本例具体值/子对象','产生方式及前序依赖','成本','B'],rows)

def val(n,key):return obj(n,key)
bt='bluetooth-off';rp='report'
common=[
 ('ContentRef',S[bt]['contents']['goal']['ref'],'B−/goal/ref','C→各消费者，准确正文身份'),
 ('ComponentRef',SH['components']['task-policy']['ref'],'shared/components/task-policy/ref','安装目录→H/B/E，准确制品身份'),
 ('ObjectRef',SH['grant_ref'],'shared/grant_ref','原owner→使用者，版本化业务对象'),
 ('BindingRef',SH['capabilities']['observe']['binding_ref'],'shared/capabilities/observe/binding_ref','目录→H/B/E，准确目标绑定'),
 ('AuthorizationRef',dict(kind='grant',**SH['grant_ref']),'Invoke.authorization_refs[0]','G→H/B/E，许可引用不是自证授权'),
 ('Amount',dict(unit='fixture_credit',amount='1'),'D1/ModelCall.usage[0]','原计量owner→账本，精确单位金额'),
 ('BudgetLimit',exchange(bt,'submit')['request']['payload']['budget'][0],'B−/submit.payload.budget[0]','受信入口→H，上限'),
 ('BudgetBalance',val(bt,'Task-final')['budget'][0],'B−/Task-final.budget[0]','H账本→Task展示'),
 ('AuthContext',exchange(bt,'O2:invoke')['auth'],'B−/O2:invoke.auth','认证适配器→接收端；不属于调用者可填正文'),
 ('Command',exchange(bt,'submit')['request'],'B−/submit.request','调用者→方法owner，原命令固定'),
 ('Receipt',exchange(bt,'O2:invoke')['response'],'B−/O2:invoke.response','方法owner→原调用者；此applied只接纳Operation'),
 ('Query',exchange(bt,'O2:get')['request'],'B−/O2:get.request','H→E；目标身份在target_id，payload={}'),
 ('QueryResult',exchange(bt,'O2:get')['response'],'B−/O2:get.response','E→H；output是查询时原事实'),
 ('TaskSubmitInput',exchange(bt,'submit')['request']['payload'],'B−/submit.payload','交互→H'),
 ('Task',val(bt,'Task-final'),'B−/Task-final','H→交互/快照/完成核验；initial和各revision见第3节'),
 ('TaskRef',val(bt,'D2:BrainContext')['task_ref'],'B−/D2:BrainContext.task_ref','H→B/计划，固定owner与任务'),
 ('Requirement',val(bt,'Task-final')['requirements'][0],'B−/Task-final.requirements[0]','B提出→H裁决保存→所有检查'),
 ('DecisionRequest',val(bt,'D2:DecisionRequest'),'B−/D2:DecisionRequest','H→B，准备/预留完成后的固定请求'),
 ('BrainContext',val(bt,'D3:BrainContext'),'B−/D3:BrainContext','H组装→C保存→B读取'),
 ('DecisionRecord',val(bt,'D3:DecisionRecord'),'B−/D3:DecisionRecord','B→H，原决策状态和一次生成结果'),
 ('ModelCall',val(bt,'D3:ModelCall'),'B−/D3:ModelCall','B/模型适配器→原账务恢复；R/O7:ModelCall 使用同型，固定其内部模型身份并向 O7 投影费用'),
 ('Proposal',val(bt,'D1:Proposal'),'B−/D1:Proposal','B→H；字段表展示act分支，D1条件变化后行动失效'),
 ('ActionInvoke',val(bt,'D2:Proposal')['actions'][0],'B−/D2:Proposal.actions[0]','B/物化器→H准入；没有operation_id或权限签发能力'),
 ('BrainPlan',val(rp,'BrainPlan'),'R/BrainPlan','B发布→H安装/物化'),
 ('BrainPlanStep',val(rp,'BrainPlan')['steps'][1],'R/BrainPlan.steps[write]','计划→H；write同时有依赖和质量门禁'),
 ('BrainArgumentBinding',val(rp,'BrainPlan')['steps'][2]['argument_bindings'][0],'R/BrainPlan.steps[readback].argument_bindings[0]','计划→H复制字段'),
 ('BrainOutputSource',val(rp,'BrainPlan')['steps'][2]['argument_bindings'][0]['source'],'R/BrainPlan.readback.source','H从同计划前项实际输出解析'),
 ('BrainPassCondition',val(rp,'BrainPlan')['steps'][1]['pass_conditions'][0],'R/BrainPlan.write.pass_conditions[0]','H当前核验集合→行动门禁'),
 ('Capability',SH['capabilities']['enable']['capability'],'shared/capabilities/enable/capability','目录→B/H/E，示例适配器合同'),
 ('Binding',SH['capabilities']['enable']['binding'],'shared/capabilities/enable/binding','目录→H/E，准确目标和驱动'),
 ('RuntimeCapabilityVerification',SH['capabilities']['enable']['capability']['verification'],'Capability.verification','驱动提供方→Executor效果核对'),
 ('RuntimeCapabilityRetry',SH['capabilities']['enable']['capability']['retry'],'Capability.retry','驱动提供方→原操作恢复'),
 ('RuntimeCapabilityAuthorization',SH['capabilities']['enable']['capability']['authorization'],'Capability.authorization','目录→准入和实际启动门禁'),
 ('RuntimeCapabilityLimits',SH['capabilities']['enable']['capability']['limits'],'Capability.limits','固定配置→预算/请求有界执行'),
 ('Invoke',val(bt,'O2:Invoke'),'B−/O2:Invoke','H→E；不是模型输出原样透传'),
 ('ControlSnapshot',val(bt,'O2:Invoke')['control_snapshot'],'B−/O2:Invoke.control_snapshot','H签发→E验证'),
 ('TaskGate',val(bt,'O2:Invoke')['control_snapshot']['gate'],'B−/O2:Invoke.control_snapshot.gate','原H→E持久门禁'),
 ('Operation',val(bt,'O2:Operation'),'B−/O2:Operation','E→H；实际发送过程、效果、费用分开'),
 ('Attempt',val(bt,'O2:Operation')['attempts'][0],'B−/O2:Operation.attempts[0]','E准备/发送→原恢复路径'),
 ('ResourceAcquireInput',exchange(bt,'resource-acquire')['request']['payload'],'B−/resource-acquire.payload','E→资源owner'),
 ('ResourceLease',val(bt,'ResourceLease'),'B−/ResourceLease','资源owner→E，原占用和代次'),
 ('ResourceReleaseInput',exchange(bt,'resource-release')['request']['payload'],'B−/resource-release.payload','E→原资源owner，固定代次释放'),
 ('RuntimeResourceState',exchange(bt,'resource-release')['response']['output'],'B−/resource-release.output','资源owner→E，释放不意味着Task成功'),
 ('UseRequest',val(bt,'O2:target:UseRequest'),'B−/O2:target:UseRequest','实际使用端→G；用途、范围、单位和费用分别固定'),
 ('GrantPolicy',SH['grant_policy'],'shared/grant_policy','G中假设存在的受信预授权范围；只给完整政策值，不伪造GrantRecord/Confirmation'),
 ('Subject',val(bt,'O2:target:UseRequest')['subject'],'B−/O2:target:UseRequest.subject','认证映射→G'),
 ('ResourceScope',val(bt,'O2:target:UseRequest')['resource_scopes'][0],'B−/O2:target:UseRequest.resource_scopes[0]','受信规范化器→G'),
 ('UseReceipt',val(bt,'O2:target:UseReceipt'),'B−/O2:target:UseReceipt','G→E；原窗口与消费决定不可变'),
 ('UseSettlementInput',exchange(bt,'O2:target:settle')['request']['payload'],'B−/O2:target:settle.payload','原计量owner→G；使用关闭后累计结算'),
 ('UseSettlementRecord',val(bt,'O2:target:Settlement'),'B−/O2:target:Settlement','G→使用端/核对方；Task不再重复计此投影'),
 ('ApprovalRequest',exchange(bt,'O2:work:approval')['request']['payload'],'B−/O2:work:approval.payload','工作端→V，准确安装/实例/动作'),
 ('ApprovalUse',val(bt,'O2:work:ApprovalUse'),'B−/O2:work:ApprovalUse','V→工作端；启动批准不是缺陷资格或用户权限'),
 ('ContentPutInput',exchange(bt,'put:goal')['request']['payload'],'B−/put:goal.payload','发布端→C；实际字节已准备'),
 ('SourceBinding',exchange(bt,'put:D2-context')['request']['payload']['sources'][0],'B−/put:D2-context.payload.sources[0]','发布适配器→C；来源闭包/限制继承'),
 ('ContentPolicy',exchange(bt,'put:goal')['request']['payload']['policy'],'B−/put:goal.payload.policy','受信来源/策略→C'),
 ('ContentCommit',exchange(bt,'put:goal')['response']['output'],'B−/put:goal.output','C→发布端，准确字节已提交'),
 ('ContentBytesGetInput',exchange(bt,'read:O1-output:orchestrator:task_processing:get')['request']['payload'],'B−/read:O1-output:orchestrator:task_processing:get.payload','持有者→C，副本先登记'),
 ('ContentBytesGetOutput',exchange(bt,'read:O1-output:orchestrator:task_processing:get')['response']['output'],'B−/read:O1-output:orchestrator:task_processing:get.output','C→持有者；下载身份不含正文'),
 ('ContentRegister_CopyInput',exchange(bt,'read:O1-output:orchestrator:task_processing:register')['request']['payload'],'B−/read:O1-output:orchestrator:task_processing:register.payload','持有者→C，读取前建立清理责任'),
 ('ContentCopy',val(bt,'read:O1-output:orchestrator:task_processing:Copy'),'B−/read:O1-output:orchestrator:task_processing:Copy','C/持有者→清理恢复；use_stopped与physical_state独立'),
 ('ContentRelease_CopyInput',exchange(bt,'read:O1-output:orchestrator:task_processing:release')['request']['payload'],'B−/read:O1-output:orchestrator:task_processing:release.payload','持有者→C；本例只停止使用，清理pending'),
 ('ConditionResult',val(bt,'ConditionResult-bt'),'B−/ConditionResult-bt','H受信检查→当前条件/Result'),
 ('Result',val(rp,'Result'),'R/Result','H最终事务→交互；不复制另一个可变Task'),
 ('ControlReceipt',val(bt,'ControlReceipt'),'B−/ControlReceipt','E→H；已执行控制修订与在途集合'),
 ('Entrance',val(bt,'ControlReceipt')['entrances'][0],'B−/ControlReceipt.entrances[0]','实际发送入口→E/H，缺口不能冒充已封闭')
]
for name,value,where,direction in common:fieldtable(name,value,where,direction)

put('''### 内嵌字段与动态适配器正文

上表中的类型对象继续按下表展开。`Proposal.plan_delta` 与非空 actions 互斥；`BrainContext.capabilities` 使用 CapabilityFixture，其缺少的完整输出、重复、授权合同放在 `catalog-*` 材料内，不能只给模型函数名称。
''')
inline=[
 ('DecisionRequest.limits',val(bt,'D2:DecisionRequest')['limits'],DEFS['DecisionRequest']['properties']['limits']),
 ('Proposal.requirements_proposal',val(bt,'D1:Proposal')['requirements_proposal'],DEFS['Proposal']['oneOf'][0]['properties']['requirements_proposal']),
 ('Proposal.plan_delta',val(bt,'D3:Proposal')['plan_delta'],DEFS['Proposal']['oneOf'][0]['properties']['plan_delta']),
 ('BrainContext.facts[]',val(bt,'D3:BrainContext')['facts'][0],DEFS['BrainContext']['properties']['facts']['items']),
 ('BrainContext.materials[]',val(bt,'D3:BrainContext')['materials'][0],DEFS['BrainContext']['properties']['materials']['items']),
 ('CapabilityFixture',val(bt,'D3:BrainContext')['capabilities'][0],DEFS['CapabilityFixture']),
 ('ResourceScope.selector',val(bt,'O2:target:UseRequest')['resource_scopes'][0]['selector'],DEFS['ResourceScope']['properties']['selector'])
]
for name,value,schema in inline:fieldtable(name,value,name,'内嵌对象，不是新的RPC',schema)

for capname,opnum,n in [('observe',1,bt),('enable',2,bt),('search',1,rp),('fetch',3,rp),('assess',7,rp),('write',8,rp),('readback',9,rp)]:
    inp=val(n,'O'+str(opnum)+':Invoke')['arguments'];out=S[n]['contents']['O'+str(opnum)+'-output']['body']
    fieldtable('示例 '+capname+' 输入',inp,SHORT[n]+'/O'+str(opnum)+':Invoke.arguments','H按冻结示例Schema校验→E/驱动；该业务参数Schema是本评审假设',SH['capabilities'][capname]['capability']['input_schema'])
    fieldtable('示例 '+capname+' 输出',out,SHORT[n]+'/O'+str(opnum)+'-output','驱动→C保存→E.result_ref→H归并；输出每项由实际驱动取得，本次为脚本化',SH['capabilities'][capname]['capability']['output_schema'])
for name,value,schema in [
 ('SearchHit',S[rp]['contents']['O1-output']['body']['hits'][0],SH['capabilities']['search']['capability']['output_schema']['properties']['hits']['items']),
 ('AssessmentJudgment',S[rp]['contents']['O7-output']['body']['judgments'][0],SH['capabilities']['assess']['capability']['output_schema']['properties']['judgments']['items']),
 ('CitationCheck',S[rp]['contents']['O7-output']['body']['citation_checks'][0],SH['capabilities']['assess']['capability']['output_schema']['properties']['citation_checks']['items']),
 ('OriginCheck',S[rp]['contents']['O7-output']['body']['origin_checks'][0],SH['capabilities']['assess']['capability']['output_schema']['properties']['origin_checks']['items'])
]:fieldtable(name,value,name,'示例输出子对象，非新增正式领域类型',schema)

put('''### 内部记录不冒充公共 RPC

下表字段均是参考实现描述或本包选定的内部序列化形状。其大小可计算，不等于实际物理表设计已经冻结。特别是 `check_id` 不属于公开 ConditionResult，`current plan_ref` 也不能任意加进公开 Task。
''')
internaltypes=['Job','OperationIntent','DecisionConsumption','PlanStepAdmission','ReceivedFact','BudgetReservation','ConditionCheck','ModelPreparation','ModelSend','UsageClosure','CleanupResponsibility','TaskExecutorBinding']
table(['内部对象','本包具体字段和值','产生、依赖与消费','代价/保留'],[[t,short(next(r['value'] for r in S[rp if t!='Job' else bt]['records'].values() if r['kind']==t)),
{'Job':'领域Raise保存责任→宿主Claim→领域Guard/Finish；work_revision与lease_epoch各自比较','OperationIntent':'H准入原候选→固定Invoke/原command→dispatch，不重拼当前Task','DecisionConsumption':'H锁Task消费原decision一次，D1条件变化也消费','PlanStepAdmission':'H固定plan版本/step唯一映射→原operation；后续不再消费D4','ReceivedFact':'H按owner/object/revision去重→当前投影及下一责任','BudgetReservation':'H固定唯一计费来源→累计差额入spent','ConditionCheck':'H固定rule/evaluator/artifact→不可变判断＋当前适用性；组合依赖可追溯','ModelPreparation':'B保存原model_call_id和上界，先于使用消费','ModelSend':'B发送门禁保存实际manifest/use/接收方；最终provider编码仍是实现缺口','UsageClosure':'原使用端保存封闭事实→G核验后释放held；本例为内部fixture','CleanupResponsibility':'C保存停止使用但尚未有删除证据的副本→清理器继续','TaskExecutorBinding':'H首次准入端→终态时枚举控制接收者'}[t],
'D/H；一次原子提交可包含多记录；原责任未结不清理，关闭后按第7节压缩'] for t in internaltypes])
put('''内部 Job 值给出了一个无重领的串行调度示例，**不能把其 `attempt_count` 合计当成真实工作者执行次数或事务次数**。本包仅序列化 H 的责任槽，Brain/Executor/内容/Grant 的实际 JobStore 映射、原子 outbox、索引与计数明细仍须按[公共框架接入](../../docs/architecture/reliable-work.md#integration)落实测量，字节账本会单列这些未覆盖的物理存储项。

## 6. 关键完整请求与响应

以下 JSON 可直接按正式方法输入/输出 Schema 校验；它们仍是合成条件下的样例，不具备真实会话、Grant 或发布批准。`AuthContext` 不出现在请求正文，WSS/gRPC 外壳与 upload/download 流不是这里的伪 RPC。其余所有逐次交接在 [B+ 机器样例](task-scenarios-data/bluetooth-on/scenario.json)、[B− 机器样例](task-scenarios-data/bluetooth-off/scenario.json)、[R 机器样例](task-scenarios-data/report/scenario.json) 的 `exchanges[]` 中，按本页步骤标签定位。
''')
for n,label,title in [(bt,'submit','6.1 task.submit 接纳'),(bt,'D2:decide','6.2 brain.decide 固定请求及 accepted'),(bt,'D1:get','6.3 brain.get 取得条件补全；其同行动必须丢弃'),(bt,'O2:invoke','6.4 execution.invoke 只接纳设置责任'),(bt,'O2:get','6.5 execution.get 取得原设置效果'),(bt,'put:goal','6.6 content.put 发布已存在字节'),(bt,'read:O1-output:orchestrator:task_processing:get','6.7 content.get 只返回下载身份'),(bt,'O2:target:use','6.8 grant.use 消費准确用途'),(bt,'O2:target:settle','6.9 grant.use.settle 原使用结算'),(bt,'O2:work:approval','6.10 evaluation.approval_check 当前启动批准')]:pair(n,label,title)
put('**6.11 R 完整有限计划正文。** 写入门禁和未来文件版本都可在这份 JSON 中定位。');code(val(rp,'BrainPlan'))
put('**6.12 R 评估输出正文。** `execution.get(O7).output.result_ref` 指这份准确字节；它不是离线 evaluation.run/report。');code(S[rp]['contents']['O7-output']['body'])
put('**O7 内部模型事实。** 调用身份在发送前分配，process/disclose 绑定此 model_call_id；E 的确定性评估工作仍绑定原 operation_id。费用由此调用向原 O7 usage 投影，Task 只按 Executor 的 O7 累计账扣一次。');code(val(rp,'O7:ModelCall'))
put('**6.13 R 最终 Result。** 三项条件绑定同一准确报告；读回证据保留独立身份。');code(val(rp,'Result'))
pair(bt,'task-read','6.14 task.read 查询终态；target_id承载任务身份')
put('''## 7. 把每个对象换算成生成与存储成本

### 7.1 每条交接的产生代价

假设未发生重试，B 的每份 Decision 仅调用一次模型，E 的评估操作包含一次模型，其余字段由已列来源提供。CPU 与读写不会因零模型而免费，但没有硬件、存储实现和负载实测时不给毫秒或货币数字。
''')
table(['对象/交接','CPU/生成','模型/目标请求','查库/事务与重复生成'],[
['目标、Task、Requirement','目标UTF-8及hash；ID分配；条件结构校验','条件解释属于D1，不额外记一次','接纳事务查原命令/容量/策略，Task、预算、首job及Receipt共同保存；重投复用'],
['BrainContext / input_manifest','查当前修订与事实；去重、序列化/hash；读取必要正文','不隐含模型摘要；本样例两/三/四份快照','内容保存和固定snapshot/decision/reservation；准确材料缓存可复用，完整上下文随修订新建'],
['DecisionRecord / Proposal / ModelCall','Schema/引用/边界校验；实际input清单和输出摘要','每D一次模型，单次返回多个字段不按字段收费','B接纳、发送准备、发送门禁、结果/发布恢复分别有持久点；H另消费原决策'],
['报告与plan发布','局部引用拓扑解析；候选字节、plan字节各hash一次','同D4生成，零次额外规划模型','先固定publication原身份，content.put恢复不二次生成；模型原输出暂存另占空间'],
['Invoke / Operation / Attempt','参数校验、资源规范化、意图hash、控制ES256签名验证、驱动解析','每O一次声明目标请求；重取/核对另计','H原意图/预留/dispatch；E接纳、发送准备、结果及核对责任；网络不在事务内'],
['ConditionCheck / Result','已有证据谓词、字节范围/摘要比较、当前门禁和未结集合','最终verify零模型；新评估只能普通O','rule/evaluator/check/依赖当前行；成功事务固定Result/终态/控制；不扫无限历史'],
['Grant Use / Settlement','规范意图hash、策略匹配、十进制加减','零模型；跨owner时请求/应答另计','G锁父链/范围/额度，allowed与open账共同保存；结算用原累计差额，once身份不恢复'],
['Content / Copy','每正文hash与长度、来源闭包和策略交集、缓存校验','零模型；下载为实际字节通道','发布元数据、来源、保留/副本登记与清理责任；同holder同准确ref复用副本，本包没有每次重新下载'],
['批准/缺陷/目录','精确ref查配置；当前instance和批准/缺陷门禁','零模型；目录搜索本例0，仅describe','装配共享；每工作当前资格不能缓存成永久许可；批准与缺陷是两种门禁']])
put('''### 7.2 精确计数与序列化字节

下表由样例逐项计算，字节用十进制 B。`协议对`是一份领域请求及返回，**不等于网络请求数或数据库事务数**。传输帧、TLS、压缩、数据库行头、索引/WAL/副本不在 JSON 字节内；查询响应、记录重复保存和内容下载分别计量，不把它们相加伪装成物理占用。
''')
metrics=[('Brain 生成','brain_calls'),('评估模型生成','assessment_model_calls'),('Operation','operations'),('领域协议对','protocol_pairs'),('请求+返回 JSON B','request_response_json_bytes'),('本包序列化的保留记录数','retained_record_count'),('这些记录 JSON B','retained_record_json_bytes'),('新正文数','new_body_count'),('新正文唯一身份字节 B','new_body_bytes'),('共享预置正文 B（不入本次新写）','preinstalled_body_bytes'),('临时副本数','copy_count'),('下载字节 B（本轮各持有者冷读）','download_bytes'),('用途使用/结算各次数','use_count'),('选定最小关闭记录数','minimum_closure_count'),('其独立 JSON B','minimum_closure_json_bytes'),('Brain 输入材料字节累计（非token）','model_input_body_bytes'),('O7 评估输入材料字节（非token）','assessment_input_body_bytes')]
table(['口径','B+已开','B−关闭','R报告'],[[title]+[STAT[n][k] for n in S] for title,k in metrics])
put('''这些字段产生量是真实文件算出的**样例量**，不是 Harness 性能。完整请求和回执是本包选择的恢复序列化布局：其中嵌套的 Invoke、Task、ContentRef 可能重复出现，记录表按实际重复字节计；同一个原正文只在“新正文”栏计一次。Result 作为内容字节与 task_results 中嵌入的逻辑值分别列出，这是显式冗余布局；若实现只存引用，减去嵌入值，不保留本表的重复量。`IntentProjection` 等内部对象可以嵌入原记录，不必成为单独表或独立事务。

最小关闭记录是在详情到期后可能保留的另一阶段，**不与完整记录 JSON B 相加**。本包列出 task、decision、operation、command、use 的关闭序列化示例；它不是所有 owner 已冻结的最终关闭 Schema，不含未实现的目录/副本/上传身份压缩组织。因此即使最终清理后也不能据此给出精确物理总量或完整总行数。

下面按方法拆解外围开销，可以看到“每个小字段创建一次调用”并不是这里的计数方式。
''')
methods=sorted(set(k for s in STAT.values() for k in s['methods']))
table(['方法','B+','B−','R','此调用为什么存在'],[[m]+[STAT[n]['methods'].get(m,0) for n in S]+[{'brain.decide':'固定一次决策责任','brain.get':'首次查询即已终结的假设','execution.invoke':'每项原操作接纳','execution.get':'每项原操作持久查询基线','capability.describe':'少量工具跳过search，读取完整准确声明','content.put':'每个新正文一次发布；上传字节另计','content.get':'每holder/准确正文/用途首次冷读','content.register_copy':'跨内容持有边界先登记','content.release_copy':'停止使用，物理删除仍pending','grant.use':'每有限store/read/process/act/disclose/manage使用','grant.use.settle':'每原使用最终累计结算；同笔钱不再扣Task','evaluation.approval_check':'本装配对Brain、操作和本地检查显式取一次工作批准','execution.control':'终态传播；条件首次变化时尚无已绑定E','resource.acquire':'模拟设备的任务内占用','resource.release':'原占用释放','task.submit':'一次任务接纳','task.read':'一次最终状态展示','task.result':'一次固定Result展示'}[m]] for m in methods])
put('''本例 `content.get` 按精确版本缓存，缓存命中只重核当前用途；这种重核在同域是查库/门禁，不伪造为新的公开 RPC。统计没有把每个字段的复制乘成外部调用。对于公共且可共享的目录/策略，运行装配可以使用安装锁下的本地不可变制品与当前门禁，取消此处“每任务、每持有者受控内容副本”的可选代价；同时保留真实私有目标、观察和报告的用途约束。

最终界面直接使用 `task.result` 返回的完整 Result，没有再次下载同一 Result 正文；`Task.result_ref` 与权威存储仍保留。报告正文是另一份内容，仍执行 read/disclose/副本登记/下载和收尾。`task.read`、`task.result` 返回前各做当前披露检查，本包将其也展开成有限 disclose 使用；正式实现可由同域资格适配器完成，不能把它误解成必须增加公开 RPC。任何当前权限或来源状态无法核验时停止披露，原任务成功事实仍保存。

### 7.3 选定逻辑记录分布与正文分布

这是本包实际序列化的记录，不是物理表数；同事务可以写多类记录，同一物理行也可保存多个逻辑值。B+ 的记录主要花在原命令/用途/内容恢复，并非只有一个 Task 和一个 Operation。
''')
kinds=sorted(set(k for s in STAT.values() for k in s['retained_record_kinds']))
table(['记录类别','B+ 数/JSON B','B− 数/JSON B','R 数/JSON B'],[[k]+[str(STAT[n]['retained_record_kinds'].get(k,0))+' / '+str(sum(r['json_bytes'] for r in S[n]['records'].values() if r['kind']==k)) for n in S] for k in kinds])
table(['新增正文','B+ B','B− B','R B'],[[title]+[sum(c['ref']['byte_length'] for alias,c in S[n]['contents'].items() if not c['preinstalled'] and matcher(alias)) for n in S] for title,matcher in [
('用户目标',lambda a:a=='goal'),('本次预分配句柄',lambda a:a=='allocated-handles'),('Brain上下文',lambda a:a.endswith('-context')),('驱动输出封套',lambda a:a.startswith('O') and a.endswith('-output')),('来源正文',lambda a:a.startswith('source-')),('报告候选',lambda a:a=='report'),('计划',lambda a:a=='plan'),('独立读回正文',lambda a:a=='readback-bytes'),('最终Result',lambda a:a=='result')]])
put('''`new_body_bytes` 按内容身份求和：报告和读回字节相同仍有两个独立事实身份；存储后端是否按 hash 去重物理 blob 未定。物理 blob 去重节省正文，不自动省掉两份来源、策略和引用元数据。已安装规则、能力和模型 profile 不应每任务重新生成；本包为自包含审查复制的组件文件不进入任务正文总数。

### 7.4 尚不能给出的物理总量、token 和事务数

实际数据库占用应在每个提交域测量：`业务堆/键值记录 + 原命令与关闭索引 + 二级索引 + 活跃WAL/重做日志 + 副本 + 备份 + 临时/历史膨胀`。对象存储另计正文、版本、来源元数据、临时副本和清理残留。WAL 是写入量/保留量指标，不能把每次 WAL 字节都永久加到当前堆占用；副本/备份也按实际策略单列。

本包没有冻结并序列化全部物理表：`grant_use_items` 的许可明细、owner 工作槽、upload/download 暂存、模型原输出publication暂存、最终供应商请求编码、账务交回outbox、门禁/集合成员索引和数据库元数据均未给出物理布局。它们不应任意取“零”。记录 JSON 加新正文只能叫本包选定序列化布局小计，不能冠名“总DB占用”。

每次 Brain 的 `model_input_body_bytes` 只加本轮实际输入清单和上下文原字节，跨轮重复发送再次计；没有把它除以4当token。真实 token 需确定模型、tokenizer、最终提示编码、工具Schema编码、缓存计费和输出，再记录 provider usage。模型输出、评估模型输入/输出、隐式SDK重试也要单列。现金成本公式为 `Σ实际独立计费请求的可信费用 + 目标服务费 + 计算/存储/网络分摊`；若评估服务报价已含模型，不再加内部模型费。

数据库按 `T_H + T_B + T_E + T_C + T_G + T_V` 记录实际提交；同域合并事务只计一次。至少有“接纳责任”“发送前准备”“结果/后续责任”这些持久点，但不把字段数、记录数或job数直接当事务数。每类采集 SQL/键值请求、扫描/锁定行数、提交次数、WAL、锁等待、租户分布、p50/p95/p99 和故障后积压。当前源文给出逻辑接口，尚不足以计算这些物理数。

### 7.5 固定开销、关键路径与保留

设 m 为模型生成，x 为目标动作完整执行，h 为非重叠的内容/权限/批准依赖耗时，p 为持久查询调度等待，a 为编排/提交/完成耗时。只有独立操作且容量充足时才取 max：

- B+：`L = m1 + m2 + x_observe + h + p + a`。
- B−：`L = m1 + m2 + x_observe + m3 + x_enable + x_observe_after + h + p + a`。
- R：`L = m1 + m2 + max(searchA,searchB) + m3 + max(fetch1..4) + m4 + x_assess + x_write + x_readback + h + p + a`，x_assess 已包含评估模型。

授权/内容准备若并行，h 取关键路径而非所有跨度相加。两例都不需要最终 complete 模型；终态控制、账务和清理可继续，但用户获知目标成功前，条件和未知效果必须核清。按业务关键路径比较优化，不能用整个fixture脚本耗时代表任务延迟。
''')
table(['对象','本例保留值/清理条件','不可删除的最小依据','成本承担方'],[
['命令/Task/Decision/Operation详细记录','完整回执至少至首次接纳截止、责任全清后再加部署查询期；本例查询期未冻结，不能给具体TTL','租户、原owner、不可复用身份、原请求摘要、原决定/终态及必要修订；长期，无固定TTL','所属owner的持久库/备份；高任务量累积'],
['目标/观察/来源/报告/上下文正文','示例 ContentPolicy.retention_until=2026-10-28T00:00:00Z；实际依许可/当前引用与未决恢复判断','Content身份/关闭与必要来源约束；最后一份恢复依据未交接不能清掉','内容owner；用户只在获准保留范围承担'],
['临时副本','登记retention_until=2026-09-28T02:00:00Z；本包use_stopped=true、physical_state=pending','原copy与清理责任，拿到准确删除证据才能complete','持有者执行清理，C保留跟进；本统计不含日后最终清理回复'],
['Grant use/结算','用量最终、发送关闭、查询/更正窗口结束后才能压缩；连续许可也不能使旧use复用','use原意图、allowed/denied、绑定操作、消费及封闭；once身份不可复活','G与原usage_owner；不按Task结束直接删'],
['计划/条件检查','当前/历史版本按来源许可与诊断保留；所有依赖和未知责任释放后才清理','成功Result绑定的准确版本、选定check与必要关闭依据','H；历史fail不能被新pass覆盖删除'],
['预算预留/原账务','未知费用继续held；可信最终账或不收费依据才释放，迟到上调仍有原责任','唯一费用来源和已记累计值/修订，避免重复扣款','H与原费用owner；更正窗口需由供应商合同给出'],
['组件/规则/安装锁','跨任务共享；至少保留旧操作可读可恢复所需版本','精确版本/digest/兼容与停用依据','宿主/提供方摊销，不每Task重装']])
put('''## 8. 正常之外，哪些故障最可能推翻方案

| 场景 | 谁保存原事实/谁继续 | 什么时候能确认成功 | 成本和禁止的捷径 |
| --- | --- | --- | --- |
| D1 条件改变却夹带观察/搜索行动 | H 消费 D1，原Decision可查，g2/c2和新decide共同保存 | 必须用g2新快照形成候选再准入 | 已计一次额外模型；不能将D1旧行动免费重绑新修订 |
| enable 已发送，答复丢失 | E保存原attempt/可能已发送；H原command查询与O2 poll；目标owner原日志核对 | 原O2应用/不再迟到，加当前新观察满足规则 | +q_cmd/q_op/q_target和等待；仅看到true不证明旧发送者被隔离；不换operation重设 |
| 用户接管或状态版本在观察后变化 | 资源owner变更control_epoch；E启动检查，H保留旧观察和具体缺口 | 重新获得合法控制及当前证据后判断 | 原state_version=1不能当永久前提；不得靠模型忽略冲突 |
| O7执行成功，但quality/citation有效fail | E保留原评估报告和费用；H当前fail拦住write，Brain按策略修订 | 新候选v2、新检查通过，再独立写/读；旧fail保留 | 若无需补源且一次修订通过，+1 Brain/+1评估，即模型共7，操作共10；这不是故障上界 |
| O7缺报告或组成证据冲突 | H保存unknown、原poll/verify；E继续原责任 | 必需证据齐备且适用，不从历史报告挑pass | 查询/补证另计；不能“评估调用没报错”就pass |
| report v2却引用v1通过记录 | H检查artifact_ref、goal、rule及当前选定check | 仅准确同一候选版本的依据可用 | 新内容即使只改一句也不能免费继承旧质量分数 |
| 文件写入生效，原答复丢失 | 文件owner原操作日志/版本/摘要；E核对O8；H继续poll | 原写入核清且不会迟到，读回同字节 | 不能创建新写入或换GUI保存；读到相同字节不单独证明旧发送者已封闭 |
| 本轮处理后新增责任，旧job想done | 领域先Raise增加work_revision；Guard/Finish锁原槽核对观察版本和lease_epoch | 后续责任仍可被新领取处理；不以handler返回判Task成功 | 旧worker不能结束或延后新责任；重领次数/事务另测 |
| 本轮模型输出已生成，content.put失答复 | B保留publication和原保存命令；C返回原ContentCommit | 所有局部引用已解析且准确正文可核验才交Proposal | 增原内容命令查询，不重新生成报告/计划 |
| 授权/批准窗口过期，尚未发送 | 原使用和Gate保持，当前依赖不可用则等待/拒绝新启动 | 取得合法新依据后按领域规则继续；closed不能复活 | 重放UseReceipt/ApprovalUse不续期；grant.check不能替代use |
| Task已成功但费用上调或清理pending | 原Brain/E/G保留原账及可靠交回，H原计费槽结算；C跟进副本 | 成功结果不重开；费用和清理独立可查 | 原来源累计差额只扣一次；不能删旧身份省存储 |

负例校验覆盖官方主机/跳转封套/报告链接错误、quality pass 而 citation fail、stale goal、有效fail阻写、unknown前项、缺失file_version、inapplicable pass、计划/行动互斥、查询身份位置和控制证明绑定。它们验证构造数据的行为约束，不证明数据库并发、目标隔离或实际授权有效。

### 8.1 当前不能用假字段补掉的缺口

| 缺口 | 当前能做的事 | 缺失时行为 / 后续证据 |
| --- | --- | --- |
| 业务 intent_hash 的统一投影 | 样例固定 `fixture-intent-v1`，真实SHA可重算；Command摘要按完整请求JCS子集计算 | 正式跨实现需冻结意图域、排除自引用及资源/费用绑定；没有一致投影不能声称互操作去重 |
| 生成条件/计划ID的适配约定 | 样例H预分配句柄，模型复制；没有额外模型调用 | 当前公共格式要求合法ID，未规定此注入机制；真实实现必须落实或拒绝无法解析身份，不能随机猜权威ID |
| upload/download/计量关闭证据的内部port | 提供真实字节、准确refs、现有content/use方法 | 未冻结的准备/流传输/closure验证不能凭JSON成功补成已实现接口；没有可核验依据不发布/不最终结算 |
| 当前Grant、批准、实例和验证器缺陷资格 | 样例只注入受信fixture前提，并逐使用展示交接 | 不能据组件hash推出已安装/批准；真实依赖缺失时不启动相应用途，不发布verified |
| provider最终编码与token/费用 | 数清逻辑物理调用边界、字节和fixture信用计数 | 需实际适配器/模型/价目/账单，未知不能填0；strict若无可信上界不启动 |
| 物理存储/清理/故障恢复 | 序列化数精确，逻辑责任可追溯 | 需真实事务、索引、WAL、副本、持久发送/目标隔离测试，不能用静态通过替代L2 |

## 9. 优先改进建议与不变边界

| 优先级 | 建议 / 当前还是待决 | 解决的具体成本 | 代价、边界及何时改选 |
| --- | --- | --- | --- |
| 1 | 在真实装配中合并同域接纳/资格/责任提交，复用准确配置和内容缓存；保持领域owner与当前门禁（现有契约允许） | 消除不必要RPC、重复SQL、重复目录正文/副本；简单任务主要外围放大源 | 宿主需提供共同事务及当前资格，跨库不能伪合并；测出共域锁热点时按owner拆分，不扩大控制保证 |
| 2 | 按输入依赖复用材料和Schema，仅新建本轮快照与实际来源记录；删除不需要单独存在的中间投影行（布局候选） | 降低长ContentRef、请求/回执和上下文的重复序列化 | H/B/E各自必须保留可恢复原身份；不能把恢复所需原Invoke改成读取当前Task重建；精确缓存失效检查仍有成本 |
| 3 | 为“已绑定设备+明确开启目标”的受信入口建立结构化条件路径（待决） | 可能省去仅用于条件补全的D1及随后的失效产物 | 入口需证明完整保持用户约束、规则与设备绑定；开放自然语言仍走当前基线。不能仅因省钱跳过g1→g2后的新decide |
| 4 | 验证一个有界、目标owner可核对的 `ensure_enabled` 适配器（待决） | 目标内读/必要设置/后验核对可减少跨Harness往返；是否再省规划需测试 | 要承担原操作关联、用户接管与状态新鲜度；不能把不可控长GUI脚本包装成一次原子动作。当前样例继续使用3个O，不预支收益 |
| 5 | 简单任务是否需要发布独立plan/句柄内容，按实测决定；确定性续行保留（待决布局） | 减少plan/元数据发布与原回执 | 不把复用计划当预授权；若去plan反而增加一次模型，不值得。条件、版本、来源和唯一准入必须仍能恢复 |
| 6 | 公共工作框架复用实现和故障套件（已在相邻工作落实设计，运行待测） | 降低各模块重复实现接纳/领取/回写成本 | 不减少物理必要提交、不自动解决外部效果未知，不新增一个独立通用工作流服务；两实例测正常与故障放大 |

最先需要的证据是同一模型、同一业务输入下的两种装配对照：显式跨owner路径与同宿主共享事务/缓存路径。记录端到端p95、模型时间占比、真实SQL/提交、内容bytes、权限/批准往返、长期关闭索引增长，再决定是否修改简单任务入口或能力合同。当前有依据先优化实现布局；尚无依据删掉恢复、权限、费用或效果核对。

## 10. 模块覆盖与交付验证
''')
table(['模块','实际参与','未参与/边界'],[
['交互','保存目标、提交原Task、最终read/result及内容披露','没有输入澄清/GUI Surface；不用interaction.input假装用户确认'],
['Orchestrator','固定快照、条件/提案消费、计划/准入、账本、事实归并、检查/Result、jobs/终态控制','TaskPolicy内部接受/资源门禁仍需真实宿主'],
['Brain','每D固定上下文、内部生成格式、新正文发布、原Decision/ModelCall/用量','无嵌套工具调用、无隐藏摘要模型、无最终汇总模型'],
['Executor','准确能力/绑定、Invoke/Operation/Attempt、模拟资源/文件、在线评估','API路径；GUI观察令牌/点击/接管流程未计，真实手机不承诺'],
['Memory/内容','content.put/get、来源、策略、副本和清理责任','没有memory.query/extract/长期记忆；不为模块覆盖而额外检索'],
['权限','read/store/process/act/disclose/manage使用及原结算','既有continuous授权为假设；不新签Grant、不伪造Confirmation；无离线lease'],
['扩展/宿主','准确配置/能力/安装锁/实例批准；公共工作职责','安装发布是共享前提，不每Task执行extensions.prepare/activate'],
['观测/评测/改进','关联ID、字节/调用/账务量、启动批准与验证器依据','报告质量评估是O7；不新建离线evaluation计划/候选发布/优化任务'],
['Agent协作','两例无必要参与','没有collaboration.delegate、子Task或allocation；不产生委派成本']])
put('''静态检查入口是 [validate.py](task-scenarios-data/validate.py)，结果是 [validation-results.json](task-scenarios-data/validation-results.json)，统计是 [statistics.json](task-scenarios-data/statistics.json)。本包逐一核对正式Schema、方法kind/target/回执、适配器输入输出、真实字节/hash、所有ContentRef解析、生成局部引用回填、计划准入/门禁、Task修订、原费用身份、调用数和JSON字节。

仓库原逐消息校验器会把 `Capability.input_schema/output_schema.properties.tenant_id` 的“字段类型定义”误当运行值，4条 describe 出现同类误报。本次保留原校验结果，在配套校验器中只对这4条已定位情况另作排除Schema节点的真实租户核验，其他错误仍失败；**不宣称未修改的原校验器对本包全部通过**，也未修改正式校验代码。其余跨字段、内容状态和计划校验复用仓库检查器。

独立读者能够复述 H/B/E/C/G/V 的分工、蓝牙 2/3 次及报告 4+1 次模型路径，以及样例 JSON 与未知物理占用的区别。审查指出并已修正官方来源证据链、用途覆盖、内容控制修订归属、观察时序、重复 Result 下载，以及质量/引用语义的独立判断；主线程指出的模型产物来源误分类也已按对象修正。结果留在 [review-results.json](task-scenarios-data/review-results.json)。

静态检查：1324 个类型对象、1096 份协议对、54 份正文的 Schema/引用/实际字节、16 份控制签名及 14 个负例均通过，保留上述 4 条已定位的原检查器限制。正文统计另与机器账本逐行比对。仓库既有 Brain 静态向量、协议的 55 条有效轨迹/371 个无效变体/105 个方法，以及任务结果的 5 个有效/9 个无效样例均通过；这些是静态检查，不是服务调用。

渲染检查：两张 Mermaid 均成功渲染并目视检查，报告图调整为纵向以避免横向压缩；正文链接、锚点、112 张表的列数和代码围栏检查通过。渲染产物及源摘要见 [render-results.json](task-scenarios-data/render-results.json)。没有对整篇长文逐屏做浏览器截图。

运行实现、模型质量、授权真实性、数据库事务/并发、目标效果、真实token/费用/延迟、清理和长期容量均未验证。原27日评审不改，28日及正式设计其他会话的修改原样保留；本工作不切分支、不建worktree、不提交。
''')
DOC.write_text('\n'.join(OUT))
print('Wrote',DOC,'lines',len(DOC.read_text().splitlines()),'bytes',DOC.stat().st_size)
