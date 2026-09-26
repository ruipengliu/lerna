import copy, hashlib, json
from pathlib import Path
ROOT=Path('docs/architecture/contracts')
old=json.loads((ROOT/'schemas/protocol.schema.json').read_text())['$defs']
registry=json.loads((ROOT/'schemas/methods.json').read_text())['methods']
def ref(n):return {'$ref':'#/$defs/'+n}
def obj(props,required=None):return {'type':'object','properties':props,'required':list(props) if required is None else required,'additionalProperties':False}
def arr(item,mi=0,ma=100):return {'type':'array','items':item,'minItems':mi,'maxItems':ma}
def string(maxlen=4096):return {'type':'string','minLength':1,'maxLength':maxlen}
def integer(mi=1,ma=2147483647):return {'type':'integer','minimum':mi,'maximum':ma}
def enum(*xs):return {'enum':list(xs)}
ID=ref('Id');REV=ref('Revision');TIME={'type':'string','format':'date-time','pattern':'Z$'};BOOL={'type':'boolean'};AMOUNTS=arr(ref('Amount'),1);AUTHS=arr(ref('AuthorizationRef'),1)
d={}
d['RuntimeBudgetClosure']=obj({'allocation_id':ID,'receiver_id':ID,'usage_revision':REV,'final_usage':AMOUNTS,'spending_closed':{'const':True},'closed_at':TIME,'proof_ref':ref('ContentRef')})
d['RuntimeBudgetAllocation']=obj({'allocation_id':ID,'parent_task_id':ID,'owner_id':ID,'receiver_id':ID,'revision':REV,'limits':arr(ref('BudgetLimit'),1),'expires_at':TIME,'state':enum('allocated','settled'),'final_usage':arr(ref('Amount')),'closure':ref('RuntimeBudgetClosure')},['allocation_id','parent_task_id','owner_id','receiver_id','revision','limits','expires_at','state','final_usage'])
d['BudgetAllocateInput']=obj({'allocation_id':ID,'parent_task_id':ID,'receiver_id':ID,'limits':arr(ref('BudgetLimit'),1),'expires_at':TIME})
d['BudgetAllocateOutput']=ref('RuntimeBudgetAllocation')
d['BudgetSettleInput']=obj({'allocation_id':ID,'closure':ref('RuntimeBudgetClosure')})
d['BudgetSettleOutput']=ref('RuntimeBudgetAllocation')
d['TaskAdjustBudgetInput']=obj({'limits':arr(ref('BudgetLimit'),1)})
d['TaskAdjustBudgetOutput']=ref('Task')
d['RuntimeTaskListCursor']=obj({'upper_bound':TIME,'last_created_at':TIME,'last_task_id':ID})
d['TaskListInput']=obj({'limit':integer(1,100),'statuses':arr(enum('active','succeeded','failed','cancelled'),1,4),'cursor':ref('RuntimeTaskListCursor')},['limit'])
d['TaskListOutput']=obj({'home_id':ID,'upper_bound':TIME,'items':arr(obj({'created_at':TIME,'task':ref('Task')})),'next_cursor':ref('RuntimeTaskListCursor'),'gaps':arr(string())},['home_id','upper_bound','items','gaps'])
d['RuntimeCapabilityRetry']=obj({'max_attempts':integer(1,3),'initial_backoff_ms':integer(1,60000),'max_backoff_ms':integer(1,3600000),'reconciliation_timeout_ms':integer(1,86400000),'key_scope':string(),'key_retention_ms':integer(),'replay_guarantee_ref':ref('ComponentRef')},['max_attempts','initial_backoff_ms','max_backoff_ms','reconciliation_timeout_ms'])
d['RuntimeCapabilityVerification']=obj({'predicate_ref':ref('ComponentRef'),'evidence_kinds':arr(enum('target_receipt','query_result','resource_observation'),1,3),'query_supported':BOOL,'cancel_supported':BOOL,'not_applied_rule_ref':ref('ComponentRef')},['predicate_ref','evidence_kinds','query_supported','cancel_supported'])
d['RuntimeCapabilityAuthorization']=obj({'resource_scopes':arr(ref('ResourceScope'),1),'actions':arr(enum('read','process','store','sync','disclose','act','manage'),1,7),'purposes':arr(string(),1),'requires_lease':BOOL,'requires_confirmation':BOOL})
d['RuntimeCapabilityLimits']=obj({'max_duration_ms':integer(),'max_input_bytes':integer(),'max_output_bytes':integer(),'max_physical_requests':integer(1,1000),'cost_bound':enum('strict','estimate'),'max_cost':AMOUNTS,'mutex_domains':arr(ID)})
d['Capability']=obj({'capability_id':ID,'version':old['ComponentRef']['properties']['version'],'digest':ref('Digest'),'description':string(),'input_schema':{'type':'object'},'output_schema':{'type':'object'},'effect_class':enum('read_only','target_idempotent','non_repeatable'),'verification':ref('RuntimeCapabilityVerification'),'retry':ref('RuntimeCapabilityRetry'),'authorization':ref('RuntimeCapabilityAuthorization'),'limits':ref('RuntimeCapabilityLimits')})
d['Binding']=obj({'binding_id':ID,'revision':REV,'capability_ref':ref('ComponentRef'),'executor_id':ID,'target_ref':ref('ObjectRef'),'driver_ref':ref('ComponentRef'),'configuration_ref':ref('ComponentRef'),'availability':enum('ready','offline','disabled')})
d['RuntimeCapabilityCandidate']=obj({'capability_ref':ref('ComponentRef'),'binding_ref':ref('BindingRef'),'summary':string()})
d['CapabilitySearchInput']=obj({'query':string(1024),'resource_scope':ref('ResourceScope'),'cursor':string(),'limit':integer(1,100)},['query','limit'])
d['CapabilitySearchOutput']=obj({'candidates':arr(ref('RuntimeCapabilityCandidate')),'next_cursor':string(),'gaps':arr(string())},['candidates','gaps'])
d['CapabilityDescribeInput']=obj({'capability_ref':ref('ComponentRef'),'binding_ref':ref('BindingRef')})
d['CapabilityDescribeOutput']=obj({'capability':ref('Capability'),'binding':ref('Binding')})
d['ResourceLease']=obj({'lease_id':ID,'revision':REV,'resource_owner_id':ID,'resource_id':ID,'holder_id':ID,'instance_id':ID,'control_epoch':REV,'expires_at':TIME,'state':enum('active','released','revoked')})
d['RuntimeResourceState']=obj({'resource_owner_id':ID,'resource_id':ID,'control_epoch':REV,'user_control':BOOL,'lease':ref('ResourceLease'),'inflight_operation_ids':arr(ID)},['resource_owner_id','resource_id','control_epoch','user_control','inflight_operation_ids'])
d['ResourceAcquireInput']=obj({'holder_id':ID,'instance_id':ID,'expires_at':TIME,'authorization_refs':AUTHS})
d['ResourceAcquireOutput']=ref('ResourceLease')
d['ResourceRenewInput']=obj({'lease_id':ID,'expires_at':TIME,'authorization_refs':AUTHS})
d['ResourceRenewOutput']=ref('ResourceLease')
d['ResourceGetInput']=obj({})
d['ResourceGetOutput']=ref('RuntimeResourceState')
d['ResourceReleaseInput']=obj({'expected_control_epoch':REV,'authorization_refs':AUTHS})
d['ResourceReleaseOutput']=ref('RuntimeResourceState')
d['ResourceTakeoverInput']=obj({'reason':string(),'authorization_refs':AUTHS})
d['ResourceTakeoverOutput']=ref('RuntimeResourceState')
d['RuntimeViewport']=obj({'width':integer(1,32768),'height':integer(1,32768),'scale':{'type':'number','exclusiveMinimum':0,'maximum':16},'orientation':enum('portrait','landscape'),'coordinate_unit':{'const':'logical_pixel'}})
d['Observation']=obj({'observation_id':ID,'resource_id':ID,'control_epoch':REV,'captured_at':TIME,'expires_at':TIME,'ui_revision':REV,'screenshot_ref':ref('ContentRef'),'structure_ref':ref('ContentRef'),'focus':string(),'viewport':ref('RuntimeViewport'),'precondition_strength':enum('atomic','best_effort')},['observation_id','resource_id','control_epoch','captured_at','expires_at','ui_revision','screenshot_ref','focus','viewport','precondition_strength'])
d['ResourceObserveInput']=obj({'resource_id':ID,'invoke':ref('Invoke')})
d['ResourceObserveOutput']=obj({'operation':ref('Operation'),'observation':ref('Observation')},['operation'])
methods={}
def method(name,kind,input_,output,target,condition=False,success=''):
 s=copy.deepcopy(registry['execution.get' if kind=='query' else 'execution.invoke'])
 s.update(kind=kind,input=input_,output=output,target=target,expected_revision=condition,stages=[] if kind=='query' else ['applied','rejected'],source=('task-runtime/README.md' if name.startswith(('budget.','task.')) else 'execution/README.md'),success=success)
 # A query may be retried after transient transport failure; no same_command action exists for it.
 if kind=='query':
  s['error_recovery']={k:[x for x in v if x!='same_command'] for k,v in s['error_recovery'].items()}
 methods[name]=s
method('budget.allocate','command','BudgetAllocateInput','BudgetAllocateOutput','payload.parent_task_id',success='父预留、固定 allocation 及额度交接责任共同提交；不证明接收方已可消费。')
method('budget.settle','command','BudgetSettleInput','BudgetSettleOutput','payload.allocation_id',True,'核验原接收方最终封账、累计费用及版本；调整父预留并保存固定结算。')
method('task.adjust_budget','command','TaskAdjustBudgetInput','TaskAdjustBudgetOutput','task_id',True,'在原义务以上修改精确单位上限；不恢复暂停、不改变期限。')
method('task.list','query','TaskListInput','TaskListOutput','home_id',success='返回本 Home 有界、按当前权限过滤的任务页；稳定上界与缺口可查。')
method('capability.search','query','CapabilitySearchInput','CapabilitySearchOutput','executor_id',success='当前获准候选的有限页；候选不授予执行权限。')
method('capability.describe','query','CapabilityDescribeInput','CapabilityDescribeOutput','executor_id',success='返回准确版本与绑定修订的完整声明；停用状态不被隐去。')
method('resource.acquire','command','ResourceAcquireInput','ResourceAcquireOutput','resource_id',success='资源 owner 原子保存唯一有效占用；可能迟到旧动作未核清时拒绝。')
method('resource.renew','command','ResourceRenewInput','ResourceRenewOutput','resource_id',True,'同一未过期占用比较修订后延长；不改变持有者或控制代次。')
method('resource.get','query','ResourceGetInput','ResourceGetOutput','resource_id',success='返回 owner 当前控制代次、占用及在途操作；读取不新建占用。')
method('resource.release','command','ResourceReleaseInput','ResourceReleaseOutput','resource_id',success='比较控制代次后交还占用或人工控制；在途效果仍保留。')
method('resource.takeover','command','ResourceTakeoverInput','ResourceTakeoverOutput','resource_id',success='持久增加代次并封闭旧自动入口后确认人工接管；列出仍可能生效动作。')
method('resource.observe','command','ResourceObserveInput','ResourceObserveOutput','payload.resource_id',success='按固定 gui.observe Invoke 接纳读取责任；实际图像按原 operation 查询。')
d['Capability']['properties']['semantic_operation_id']=string();d['Capability']['required'].append('semantic_operation_id')
d['CapabilityFixture']=copy.deepcopy(old['CapabilityFixture']);d['CapabilityFixture']['properties'].update(semantic_operation_id=string(),effect_class=enum('read_only','target_idempotent','non_repeatable'))
d['RuntimeBudgetAllocation']['allOf']=[{'if':{'properties':{'state':{'const':'settled'}}},'then':{'required':['closure']},'else':{'properties':{'final_usage':{'maxItems':0}},'not':{'required':['closure']}}}]
# Cross-Home admission requires current owner facts and receiver closure recovery.

d['RuntimeDelegationContext']=obj({'sender_home_id':ID,'parent_delegation_id':ID,'allocation_ref':ref('ObjectRef'),'allocation_command_id':ID,'permission_refs':AUTHS,'ancestor_ids':arr(ID,1,5)})
d['TaskSubmitInput']=copy.deepcopy(old['TaskSubmitInput']);d['TaskSubmitInput']['properties']['delegation_context']=ref('RuntimeDelegationContext')
d['AuthContext']=copy.deepcopy(old['AuthContext']);d['AuthContext']['properties']['sender_service_id']=ID
d['RuntimeBudgetReceiver']=obj({'allocation_id':ID,'parent_owner_id':ID,'receiver_id':ID,'parent_delegation_id':ID,'revision':REV,'state':enum('open','closing','closed'),'task_id':ID,'final_usage':arr(ref('Amount')),'closure':ref('RuntimeBudgetClosure')},['allocation_id','parent_owner_id','receiver_id','parent_delegation_id','revision','state','final_usage'])
d['BudgetReadInput']=obj({'role':enum('owner','receiver')})
d['BudgetReadOutput']={'oneOf':[obj({'role':{'const':'owner'},'allocation':ref('RuntimeBudgetAllocation')}),obj({'role':{'const':'receiver'},'receiver':ref('RuntimeBudgetReceiver')})]}
d['BudgetCloseInput']=obj({'sender_home_id':ID,'parent_delegation_id':ID,'allocation_ref':ref('ObjectRef'),'allocation_command_id':ID,'reason':string()})
d['BudgetCloseOutput']=ref('RuntimeBudgetReceiver')
method('budget.read','query','BudgetReadInput','BudgetReadOutput','allocation_id',success='读取原分配owner当前记录或receiver唯一接纳/关闭投影；role不能改变服务身份。')
method('budget.close','command','BudgetCloseInput','BudgetCloseOutput','payload.allocation_ref.id',success='原receiver关闭allocation接纳与新增消费门禁并保存收尾责任；最终封账证明另查，未知子创建也先保存禁止索引。')
Path('.scratch/architecture-detail/runtime-protocol-patch.json').write_text(json.dumps({'defs':d,'methods':methods},ensure_ascii=False,indent=2)+'\n')
print(len(d),'definitions;',len(methods),'methods')
