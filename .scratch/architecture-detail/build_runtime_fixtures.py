import copy, hashlib, json
from pathlib import Path
root=Path('docs/architecture/contracts/examples/protocol')
def uid(label):return label.replace('-','_')+'_'+hashlib.sha256(label.encode()).hexdigest()[:32]
def digest(label):return 'sha256:'+hashlib.sha256(label.encode()).hexdigest()
def comp(label):return {'id':uid(label),'version':'1.0.0','digest':digest(label)}
T='2026-09-26T00:00:00Z';END='2026-09-26T00:10:00Z';tenant=uid('runtime_tenant');actor=uid('runtime_actor');home=uid('runtime_home');owner=uid('runtime_executor');resource=uid('runtime_phone');taskid=uid('runtime_task')
def content(label):return {'tenant_id':tenant,'owner_id':owner,'content_id':uid(label),'version':1,'hash':digest(label),'media_type':'application/json','byte_length':16}
def exchange(name,target,payload,output,service,expected=None,at=T):
 req={'method':name,'target_id':target,'payload':copy.deepcopy(payload)}
 from_registry=json.loads(Path('.scratch/architecture-detail/runtime-protocol-patch.json').read_text())['methods']
 command=from_registry.get(name,{'kind':'command'})['kind']=='command'
 if command:
  req.update(command_id=uid(name.replace('.','_')+'_'+str(len(events))),expires_at=END)
  if expected is not None:req['expected_revision']=expected
  res={'command_id':req['command_id'],'stage':'applied','decided_at':at,'output':copy.deepcopy(output)}
 else:res={'output':copy.deepcopy(output),'observed_at':at}
 return {'at':at,'exchange':{'auth':{'tenant_id':tenant,'actor_id':actor,'logical_service_id':service},'request':req,'response':res}}
def fixture(name,description,caps=[]):
 data={'name':name,'description':description,'capabilities':caps,'input_requests':[],'approvals':[],'events':copy.deepcopy(events)}
 (root/(name+'.json')).write_text(json.dumps(data,ensure_ascii=False,indent=2)+'\n')
 return data
mutations=[]
def bad(file,event,name,expect,path,value):
 mutations.append({'name':name,'fixture':file+'.json','expect':expect,'edits':[{'op':'set','path':f'/events/{event}/exchange/'+path,'value':value}]})
# A real parent Task snapshot precedes adjustment and allocation.
events=[]
base=json.loads((root/'01-task-loop.json').read_text())['events'][0]
base=copy.deepcopy(base)
x=base['exchange'];oldtask=x['response']['output'];oldtask['tenant_id']=tenant;oldtask['home_id']=home;oldtask['task_id']=taskid
oldtask['goal_ref']=content('runtime_goal');oldtask['requirements']=[];oldtask['deadline']=END
x['auth']={'tenant_id':tenant,'actor_id':actor,'logical_service_id':home};x['request']['target_id']=home
x['request']['payload']['home_id']=home;x['request']['payload']['goal_ref']=oldtask['goal_ref'];x['request']['payload']['deadline']=END
x['request']['payload']['constraints']=[]
x['request']['payload']['budget']=[{'unit':'request','limit':'10'}]
oldtask['budget']=[{'unit':'request','limit':{'unit':'request','amount':'10'},'spent':{'unit':'request','amount':'0'},'reserved':{'unit':'request','amount':'0'}}]
events.append(base)
updated=copy.deepcopy(oldtask);updated['revision']=2;updated['budget'][0]['limit']['amount']='20'
events.append(exchange('task.adjust_budget',taskid,{'limits':[{'unit':'request','limit':'20'}]},updated,home,1))
allocation={'allocation_id':uid('runtime_allocation'),'parent_task_id':taskid,'owner_id':home,'receiver_id':uid('runtime_child_home'),'revision':1,'limits':[{'unit':'request','limit':'5'}],'expires_at':END,'state':'allocated','final_usage':[]}
events.append(exchange('budget.allocate',taskid,{k:allocation[k] for k in ('allocation_id','parent_task_id','receiver_id','limits','expires_at')},allocation,home))
closure={'allocation_id':allocation['allocation_id'],'receiver_id':allocation['receiver_id'],'usage_revision':1,'final_usage':[{'unit':'request','amount':'3'}],'spending_closed':True,'closed_at':T,'proof_ref':content('runtime_closure')}
settled=copy.deepcopy(allocation);settled.update(revision=2,state='settled',final_usage=closure['final_usage'],closure=closure)
events.append(exchange('budget.settle',allocation['allocation_id'],{'allocation_id':allocation['allocation_id'],'closure':closure},settled,home,1))
listed=copy.deepcopy(updated);listed['revision']=4;listed['budget'][0]['spent']['amount']='3'
events.append(exchange('task.list',home,{'limit':10,'statuses':['active']},{'home_id':home,'upper_bound':T,'items':[{'created_at':T,'task':listed}],'gaps':[]},home))
fixture('20-budget-allocation','父任务调额、预留固定子额度和凭原接收方封账结算；列表使用本Home固定上界。')
bad('20-budget-allocation',1,'budget adjustment cannot resume task','budget_adjust_scope','response/output/control','paused')
bad('20-budget-allocation',2,'allocation cannot transfer beyond parent obligation bound','allocation_budget','response/output/limits/0/limit','25')
# Keep request and output correlated so the above reaches ledger validation.
mutations[-1]['edits'].append({'op':'set','path':'/events/2/exchange/request/payload/limits/0/limit','value':'25'})
bad('20-budget-allocation',3,'settlement cannot change original receiver','allocation_identity','response/output/receiver_id',uid('wrong_receiver'))
mutations[-1]['edits'] += [{'op':'set','path':path,'value':uid('wrong_receiver')} for path in ['/events/3/exchange/request/payload/closure/receiver_id','/events/3/exchange/response/output/closure/receiver_id']]
bad('20-budget-allocation',4,'task list cannot include another Home','task_list_home','response/output/items/0/task/home_id',uid('wrong_home'))
# Catalog carries full exact, closed input and output schemas.
events=[]
capref=comp('runtime_observe');bindref={'binding_id':uid('runtime_binding'),'revision':1}
input_schema={'$schema':'https://json-schema.org/draft/2020-12/schema','type':'object','properties':{'resource_id':{'type':'string','pattern':'^[a-z][a-z0-9_]*_[0-9a-f]{32}$'}},'required':['resource_id'],'additionalProperties':False}
capfixture={'capability_ref':capref,'binding_ref':bindref,'input_schema':input_schema,'semantic_operation_id':'harness.gui.observe','effect_class':'read_only'}
cap={'semantic_operation_id':'harness.gui.observe','capability_id':capref['id'],'version':capref['version'],'digest':capref['digest'],'description':'gui.observe：读取指定设备新观察，不发送GUI动作','input_schema':input_schema,'output_schema':{'type':'object','properties':{'observation_id':{'type':'string'}},'required':['observation_id'],'additionalProperties':False},'effect_class':'read_only','verification':{'predicate_ref':comp('runtime_observe_verifier'),'evidence_kinds':['resource_observation'],'query_supported':True,'cancel_supported':False},'retry':{'max_attempts':3,'initial_backoff_ms':100,'max_backoff_ms':1000,'reconciliation_timeout_ms':30000},'authorization':{'resource_scopes':[{'resource_owner_id':owner,'resource_type':'device','selector':{'object_ids':[resource]},'normalizer_version':'1.0.0'}],'actions':['read'],'purposes':['gui_observation'],'requires_lease':False,'requires_confirmation':False},'limits':{'max_duration_ms':5000,'max_input_bytes':4096,'max_output_bytes':1000000,'max_physical_requests':1,'cost_bound':'strict','max_cost':[{'unit':'request','amount':'1'}],'mutex_domains':[resource]}}
binding={'binding_id':bindref['binding_id'],'revision':1,'capability_ref':capref,'executor_id':owner,'target_ref':{'owner_id':owner,'id':resource,'revision':1},'driver_ref':comp('runtime_simulator'),'configuration_ref':comp('runtime_simulator_config'),'availability':'ready'}
events.append(exchange('capability.search',owner,{'query':'观察手机','limit':2},{'candidates':[{'capability_ref':capref,'binding_ref':bindref,'summary':'gui.observe'}],'gaps':[]},owner))
events.append(exchange('capability.describe',owner,{'capability_ref':capref,'binding_ref':bindref},{'capability':cap,'binding':binding},owner))
fixture('21-capability-catalog','目录搜索只给候选；描述固定准确版本、可访问目标、参数Schema与效果核对合同。',[capfixture])
bad('21-capability-catalog',0,'catalog cannot invent a capability version','catalog_reference','response/output/candidates/0/capability_ref/version','2.0.0')
bad('21-capability-catalog',1,'describe cannot switch pinned binding executor','catalog_binding','response/output/binding/executor_id',uid('other_executor'))
# Lease lifecycle, real observation, takeover and handback.
events=[]
auths=[{'kind':'grant','owner_id':home,'id':uid('runtime_grant'),'revision':1}]
lease={'lease_id':uid('runtime_lease'),'revision':1,'resource_owner_id':owner,'resource_id':resource,'holder_id':actor,'instance_id':uid('runtime_instance'),'control_epoch':1,'expires_at':'2026-09-26T00:05:00Z','state':'active'}
events.append(exchange('resource.acquire',resource,{'holder_id':actor,'instance_id':lease['instance_id'],'expires_at':lease['expires_at'],'authorization_refs':auths},lease,owner))
renewed=copy.deepcopy(lease);renewed.update(revision=2,expires_at=END)
events.append(exchange('resource.renew',resource,{'lease_id':lease['lease_id'],'expires_at':END,'authorization_refs':auths},renewed,owner,1))
state={'resource_owner_id':owner,'resource_id':resource,'control_epoch':1,'user_control':False,'lease':renewed,'inflight_operation_ids':[]}
events.append(exchange('resource.get',resource,{},state,owner))
inv={'operation_id':uid('runtime_observation_operation'),'task_id':taskid,'home_id':home,'goal_revision':1,'control_snapshot':{'gate':{'home_id':home,'task_id':taskid,'control_revision':1,'goal_revision':1,'status':'active','control':'running'},'executor_id':owner,'issued_at':T,'start_before':'2026-09-26T00:00:30Z','home_proof':'fixture: authenticated Home snapshot'},'capability_ref':capref,'binding_ref':bindref,'arguments':{'resource_id':resource},'intent_hash':digest('runtime_observe_intent'),'authorization_refs':auths,'reservation_ref':{'owner_id':home,'id':uid('runtime_observation_reservation'),'revision':1},'deadline':END}
img=content('runtime_screen');img['media_type']='image/png'
obs={'observation_id':uid('runtime_observation'),'resource_id':resource,'control_epoch':1,'captured_at':T,'expires_at':'2026-09-26T00:00:10Z','ui_revision':1,'screenshot_ref':img,'focus':'reminders','viewport':{'width':390,'height':844,'scale':1,'orientation':'portrait','coordinate_unit':'logical_pixel'},'precondition_strength':'atomic'}
op={'operation_id':inv['operation_id'],'revision':2,'execution_state':'closed','effect':'applied','may_apply_later':False,'attempts':[],'evidence_refs':[img],'usage':[{'unit':'request','amount':'1'}],'usage_final':True,'next_action':'none'}
events.append(exchange('resource.observe',resource,{'resource_id':resource,'invoke':inv},{'operation':op,'observation':obs},owner))
taken=copy.deepcopy(state);taken['control_epoch']=2;taken['user_control']=True;taken['lease']['state']='revoked';taken['lease']['revision']=3
events.append(exchange('resource.takeover',resource,{'reason':'本人接管','authorization_refs':auths},taken,owner))
released=copy.deepcopy(taken);released['user_control']=False;released['lease']['state']='released';released['lease']['revision']=4
events.append(exchange('resource.release',resource,{'expected_control_epoch':2,'authorization_refs':auths},released,owner))
fixture('22-resource-lifecycle','设备唯一占用、条件续期、实际观察、本人接管和交还分别产生持久事实。',[capfixture])
bad('22-resource-lifecycle',0,'resource acquire cannot change holder','lease_holder','response/output/holder_id',uid('other_holder'))
bad('22-resource-lifecycle',1,'resource renew cannot change original instance','lease_renewal','response/output/instance_id',uid('other_instance'))
bad('22-resource-lifecycle',2,'resource get cannot attach a foreign lease','resource_binding','response/output/lease/resource_id',uid('other_device'))
bad('22-resource-lifecycle',3,'observation cannot use stale control generation','observation_epoch','response/output/observation/control_epoch',2)
bad('22-resource-lifecycle',4,'takeover must advance current resource epoch','resource_epoch','response/output/control_epoch',1)
bad('22-resource-lifecycle',5,'release cannot erase newer holder generation','resource_epoch','request/payload/expected_control_epoch',1)
mutations[-1]['edits'].append({'op':'set','path':'/events/5/exchange/response/output/control_epoch','value':1})
# Cross-Home receiving has a current allocation read and a permanent close gate.
events=[]
receiver_home=uid('runtime_receiver_home');delegation_id=uid('runtime_cross_delegation')
cross=copy.deepcopy(allocation);cross['allocation_id']=uid('runtime_cross_allocation');cross['receiver_id']=receiver_home
allocated_event=exchange('budget.allocate',taskid,{k:cross[k] for k in ('allocation_id','parent_task_id','receiver_id','limits','expires_at')},cross,home)
events.append(allocated_event)
events.append(exchange('budget.read',cross['allocation_id'],{'role':'owner'},{'role':'owner','allocation':cross},home))
ctx={'sender_home_id':home,'parent_delegation_id':delegation_id,'allocation_ref':{'owner_id':home,'id':cross['allocation_id'],'revision':1},'allocation_command_id':allocated_event['exchange']['request']['command_id'],'permission_refs':auths,'ancestor_ids':[taskid]}
child_event=copy.deepcopy(base);cx=child_event['exchange'];child_id=uid('runtime_cross_child')
cx['auth']={'tenant_id':tenant,'actor_id':actor,'logical_service_id':receiver_home,'sender_service_id':home}
cx['request']['command_id']=uid('runtime_child_submit');cx['request']['target_id']=receiver_home
cx['request']['payload'].update(home_id=receiver_home,budget=cross['limits'],delegation_context=ctx)
cx['response']['command_id']=cx['request']['command_id'];child=cx['response']['output'];child.update(home_id=receiver_home,task_id=child_id,submit_command_id=cx['request']['command_id'])
child['budget'][0]['limit']['amount']='5'
events.append(child_event)
closing={'allocation_id':cross['allocation_id'],'parent_owner_id':home,'receiver_id':receiver_home,'parent_delegation_id':delegation_id,'revision':2,'state':'closing','task_id':child_id,'final_usage':[]}
close_input={k:ctx[k] for k in ('sender_home_id','parent_delegation_id','allocation_ref','allocation_command_id')};close_input['reason']='父任务取消，关闭原接收额度'
close_event=exchange('budget.close',cross['allocation_id'],close_input,closing,receiver_home);close_event['exchange']['auth']['sender_service_id']=home
events.append(close_event)
cross_closure=copy.deepcopy(closure);cross_closure.update(allocation_id=cross['allocation_id'],receiver_id=receiver_home);cross_closure['proof_ref']['owner_id']=receiver_home
closed=copy.deepcopy(closing);closed.update(revision=3,state='closed',final_usage=cross_closure['final_usage'],closure=cross_closure)
events.append(exchange('budget.read',cross['allocation_id'],{'role':'receiver'},{'role':'receiver','receiver':closed},receiver_home))
cross_settled=copy.deepcopy(cross);cross_settled.update(revision=2,state='settled',final_usage=cross_closure['final_usage'],closure=cross_closure)
events.append(exchange('budget.settle',cross['allocation_id'],{'allocation_id':cross['allocation_id'],'closure':cross_closure},cross_settled,home,1))
fixture('23-cross-home-budget','另一Home校验原额度命令及当前分配，唯一子接纳与额度关闭竞争；原接收方最终封账才允许父结算。')
bad('23-cross-home-budget',1,'budget.read cannot substitute receiver view','budget_read_role','request/payload/role','receiver')
bad('23-cross-home-budget',2,'delegated task cannot invent authenticated sender','delegation_sender','request/payload/delegation_context/sender_home_id',uid('wrong_sender'))
bad('23-cross-home-budget',2,'delegated task must match current allocation revision','receiver_admission','request/payload/delegation_context/allocation_ref/revision',2)
bad('23-cross-home-budget',3,'budget.close must seal receiver gate','receiver_closure','response/output/state','open')
bad('23-cross-home-budget',4,'closed receiver cannot omit final proof association','receiver_closure','response/output/receiver/closure/receiver_id',uid('wrong_receiver'))
# Add a late creation under a new command after the receiver was closed.
late=copy.deepcopy(child_event);late['exchange']['request']['command_id']=uid('runtime_late_child_submit');late['exchange']['response']['command_id']=late['exchange']['request']['command_id'];late['exchange']['request']['payload']['delegation_context']['parent_delegation_id']=uid('runtime_other_delegation');late['exchange']['response']['output']['task_id']=uid('runtime_other_child');late['exchange']['response']['output']['submit_command_id']=late['exchange']['request']['command_id']
mutations.append({'name':'closed receiver refuses late new child under same allocation','fixture':'23-cross-home-budget.json','expect':'receiver_reopen','edits':[{'op':'set','path':'/events/5','value':late}]})
# Focused bypass and closure ordering cases beyond each method's base mutation.
mutations.append({'name':'observation convenience cannot run a write capability','fixture':'22-resource-lifecycle.json','expect':'observation_capability','edits':[{'op':'set','path':'/capabilities/0/effect_class','value':'non_repeatable'}]})
r22=json.loads((root/'22-resource-lifecycle.json').read_text());oinv=r22['events'][3]['exchange']['request']['payload']['invoke']
cancel=exchange('execution.cancel',owner,{'home_id':home,'task_id':taskid,'operation_id':oinv['operation_id'],'reason':'先取消原观察'},{'home_id':home,'task_id':taskid,'operation_id':oinv['operation_id'],'cancel_command_id':uid('execution_cancel_'+str(len(events))),'cancelled_at':T},owner)
mutations.append({'name':'observation convenience honors original cancellation tombstone','fixture':'22-resource-lifecycle.json','expect':'observation_start','edits':[{'op':'set','path':'/events/2','value':cancel}]})
no_child_close=copy.deepcopy(close_event);no_child_close['exchange']['response']['output'].pop('task_id')
mutations.append({'name':'receiver closure before first child wins permanently','fixture':'23-cross-home-budget.json','expect':'receiver_reopen','edits':[{'op':'set','path':'/events/2','value':no_child_close},{'op':'set','path':'/events/3','value':child_event}]})
bad('23-cross-home-budget',3,'receiver close validates original allocation revision','receiver_allocation_original','request/payload/allocation_ref/revision',2)
mutations.append({'name':'parent cannot settle while known receiver still closing','fixture':'23-cross-home-budget.json','expect':'allocation_closure','edits':[{'op':'set','path':'/events/4/exchange/response/output/receiver','value':closing}]})
# One unknown payload field mutation for each newly frozen method.

seen_methods=set()
for file in ['20-budget-allocation','21-capability-catalog','22-resource-lifecycle','23-cross-home-budget']:
 data=json.loads((root/(file+'.json')).read_text())
 for i,e in enumerate(data['events']):
  name=e['exchange']['request']['method']
  if name=='task.submit' or name in seen_methods:continue
  seen_methods.add(name)
  bad(file,i,name+' rejects undeclared payload field','schema','request/payload/undeclared',True)
Path('.scratch/architecture-detail/runtime-mutations.json').write_text(json.dumps(mutations,ensure_ascii=False,indent=2)+'\n')
print('4 fixtures;',len(mutations),'mutations')
