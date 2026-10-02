#!/usr/bin/env python3
"""Generate the core field dictionary from the same unpublished strict Schema."""
import json, pathlib, sys
ROOT=pathlib.Path(__file__).resolve().parents[1]
s=json.loads((ROOT/'protocol/core.schema.json').read_text());defs=s['$defs']
GROUPS=[
 ('公共值与引用','无独立生命周期；外层记录提供原作用域。',['Id','Revision','Count','Digest','Time','Decimal','Amount','ObjectRef','ComponentRef','ContentRef','CollectionSummary']),
 ('原始输入与请求','Session/Branch/Message/Submission 归应用；InputRequest 归实际业务消费 owner。',['Session','Branch','Message','Submission','InputRequest','SourceEvidence','ScheduleSpec']),
 ('目标与条件','以下存储记录归原 Orchestrator。候选和引用是嵌入值，不能脱离 Task/原来源独立寻址。',['GoalDocument','RequirementCandidate','RequirementDelta','RequirementRef','Requirement','RequirementMapping','GoalRevision','RequirementAdoption','GoalCoverage','RuleDefinition','WaitReason','Task','ConditionResult','Result']),
 ('决策与执行','DispatchIntent 归 Orchestrator；DecisionRecord 归 Brain；Operation 归 Executor；ControlSnapshot 是原控制的传输值。',['DecisionDispatchIntent','DecisionRecord','Operation','ControlSnapshot']),
 ('授权预算和交接','Grant 归授权 owner，Confirmation 归原业务 owner；BudgetBalance 是 Task 每单位投影；UsageSnapshot 归实际计费源；Closure 和 DelegationContext 保持原双方身份。',['Grant','Confirmation','BudgetBalance','UsageSnapshot','AllocationClosure','DelegationContext']),
 ('持久责任与命令','Job 在每个业务 owner 本地保存；Claim 是固定领取凭据；Command 是跨网络不变的原请求。',['Job','Claim','Command'])]

def typ(v):
 if '$ref' in v:return v['$ref'].split('/')[-1]
 if 'const' in v:return str(v['const'])
 if 'enum' in v:return ' / '.join(str(x).lower() if isinstance(x,bool) else str(x) for x in v['enum'])
 if 'oneOf' in v:return ' 或 '.join(typ(x) for x in v['oneOf'])
 t=v.get('type','条件结构')
 if isinstance(t,list):t=' / '.join(t)
 if t=='array':return typ(v['items'])+'[] '+str(v.get('minItems',0))+'..'+str(v.get('maxItems','有界'))
 if t=='string':return 'string'+(' ('+v['format']+')' if 'format' in v else '')
 if t=='integer':return 'integer '+str(v.get('minimum',0))+'..'+str(v.get('maximum','按策略'))
 return t

def esc(x):return str(x).replace('|','&#124;').replace('\n',' ')

def table(v):
 out=['| 字段 | 类型 | 必填 | 语义 |','| --- | --- | --- | --- |']
 conditional=set()
 def scan(x):
  if isinstance(x,dict):
   for key,val in x.items():
    if key=='required':conditional.update(val)
    else:scan(val)
  elif isinstance(x,list):
   for a in x:scan(a)
 scan(v.get('allOf',[]))
 for k,x in v.get('properties',{}).items():
  need='是' if k in v.get('required',[]) else '条件' if k in conditional else '否'
  out.append('| '+ ' | '.join(esc(z) for z in (k,typ(x),need,x.get('description','准确字段语义由本类型约束确定'))) +' |')
 return out
out=['# 核心对象字段字典','','本文件由 core.schema.json 生成，保持字段、类型、必填性和枚举同源。它定义逻辑值与读视图，不等同于数据库 DDL。模块内部完整记录另见[模块字段附录](module-records.md)，包括 Snapshot、Intent、Attempt、记忆、预算使用、协作、安装、评测和生产目录。对象归责及关联见[整体数据设计](README.md)。','','所有持久记录的物理封装必须有受信 tenant/owner、记录时间与格式版本；可变头有业务 revision。未在读视图中重复的作用域由受信外层继承，不能从正文任意指定。跨域 ObjectRef 明确带租户。Requirement、BudgetBalance 等嵌入样例不是无作用域的独立资源。','','“条件”须结合下方状态规则；未出现字段表示未知或不适用，不能默认为已通过/零费用。额外字段拒绝，新增合同要换 profile。数组有上限；可增长集合使用关系表与分页，不靠截断满足 Schema。','','ID 格式为类型前缀加32位小写十六进制；业务修订从1开始。金额保留十进制字符串与单位；时间是UTC RFC3339。摘要使用准确字节或协议声明的JCS输入，不能拿未校验摘要证明内容存在或用户授权。','']
seen=[]
for num,(title,description,names) in enumerate(GROUPS,1):
 out += ['## '+str(num)+' '+title,'',description,'']
 for name in names:
  v=defs[name];seen.append(name);out+=['### '+name,'',v.get('description','') or '字段语义如下。','']
  if 'properties' in v:out+=table(v)
  elif 'oneOf' in v and all('properties' in x for x in v['oneOf']):
   for variant in v['oneOf']:
    tag=variant['properties']['type']['const'];out+=['#### '+tag,'']+table(variant)+['']
  else:out+=['值类型：'+typ(v)+'。'+ ('格式：`'+v['pattern']+'`。' if 'pattern' in v else '')]
  out+=['']
assert set(seen)==set(defs), (set(defs)-set(seen),set(seen)-set(defs))
out += ['## 7 状态和跨字段约束','','- Task.succeeded 必须有指向原owner权威Result的ObjectRef result_ref、ready、完整且未结为零的效果集合；非成功状态没有成功 result_ref。ready 至少一条条件，且 current_coverage_ref 对应当前 pass/usable 的覆盖记录','- GoalCoverage 的 verdict、目标与报告不改写；当前 applicability 变化推进其读视图 revision。GoalRevision 保留原集合，旧引用不静默解析为较新版本','- ConditionResult 的 observed_at 仅在观察已知时保存；pass 且 usable 必须存在。必要 effect 条件的 basis 必须 verified，不能由 user_accepted 或质量打分替代','- Submission.steer 固定Task和 expected_goal_revision；input 固定 request_ref，仅Task型回答要求目标与目标修订，独立应用回答可以没有Task；enqueue_goal_after 固定 predecessor_task_ref。sending/applied 必须有原 dispatch；已应用的Task型输入保留Task映射','- InputRequest.answered 必须有 answer_ref、consumed_by 和 answered_at；accept_quality 还固定 candidate_ref、limitations_ref 和 goal_revision，预览必须覆盖这些准确内容','- Confirmation.consumed 必须保存原 consumed_by/at；原本人决定和命令必须绑定。Use、金额、权限仍须业务端重新核验','- Job.leased 才有 holder_id/lease_until。Claim.observed_work_revision 在本次领取中固定；当前 Job.work_revision 可以更高','- 同一目标快照中 requirement_id、同一候选来源中 candidate_key、同一最终 Result 中所选必要条件不得重复。Schema 的 uniqueItems 不能替代按业务键唯一','- tenant/owner/task、条件版本、内容摘要、期限、真实来源、当前门禁及原授权的关系由语义校验和实际 owner 事务检查，不能仅靠 JSON 结构证明','', '## 8 五条核心命令载荷','','外层 Command 的身份、profile、首次接纳期限、原 CAS 和摘要不可在重传时刷新。以下只列已形式化的五个方法；其他模块端口必须单独发布闭合合同，不能用任意 payload 绕过版本管理。','']
for branch in defs['Command']['allOf']:
 method=branch['if']['properties']['method']['const'];payload=branch['then']['properties']['payload'];out+=['### '+method,'']+table(payload)+['']
out+=['task.submit 可不带 requirement_candidates。它们只作候选，权威条件由 Task owner 接纳。task.cancel 必须提供原 expected_revision；账单唤醒按原计费源修订归并，不要求发送者猜 Task 当前修订。','','重新生成：运行 `python docs/architecture/validation/build_field_reference.py`；只检查漂移时加 `--check`。这个脚本不连接数据库或外部服务。','']
body='\n'.join(out);target=ROOT/'data/field-reference.md'
if '--check' in sys.argv:
 if not target.exists() or target.read_text()!=body:raise SystemExit('field-reference.md differs from core.schema.json; regenerate it')
 print('field dictionary matches strict schema')
else:target.write_text(body);print('generated',target)
