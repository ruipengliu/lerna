import json, hashlib, copy, runpy
from pathlib import Path
ns=runpy.run_path('.scratch/architecture-detail/build_governance_assets.py');M=ns['M'];D=ns['D']
ROOT=Path('docs/architecture');SCR=Path('.scratch/architecture-detail'); mutations=[];locations={}
def ID(s):return s.split('-')[0]+'_'+hashlib.sha256(s.encode()).hexdigest()[:32]
def H(s):return 'sha256:'+hashlib.sha256(s.encode()).hexdigest()
T='2026-09-26T00:00:00Z';EXP='2026-09-26T01:00:00Z'
tenant=ID('tenant-gov');owner=ID('owner-gov');actor=ID('actor-gov');instance=ID('instance-1');target=ID('target-gov')
def CR(s):return {'tenant_id':tenant,'owner_id':owner,'content_id':ID('content-'+s),'version':1,'hash':H(s),'media_type':'application/json','byte_length':128}
def OR(s,revision=1):return {'owner_id':owner,'id':ID(s),'revision':revision}
def AM(a,unit='calls'):return {'unit':unit,'amount':str(a)}
def component(s):return {'id':ID(s),'version':'1.0.0','digest':H(s)}
class Fixture:
 def __init__(self,n,name,description):self.file=f'{n:02}-{name}.json';self.data={'name':name,'description':description,'capabilities':[],'input_requests':[],'approvals':[],'events':[]};self.sec=0
 def event(self,name,p,out,target_id=None,expected=None,reject=None):
  spec=M.get(name) or ns['oldm'][name]; idx=len(self.data['events']);at=f'2026-09-26T00:{self.sec//60:02}:{self.sec%60:02}Z';self.sec+=1
  tar=target_id or owner;req={'method':name,'target_id':tar,'payload':copy.deepcopy(p)}
  if spec['kind']=='command':
   req.update(command_id=ID('command-'+self.file+str(idx)),expires_at=EXP)
   if spec['expected_revision']:req['expected_revision']=expected if expected is not None else 1
   res={'command_id':req['command_id'],'stage':'rejected' if reject else 'applied','decided_at':at}
   if reject:res['error']={'code':reject,'message':'Constructed expected rejection','retry':'after_change'}
   else:res['output']=copy.deepcopy(out)
  else:res={'output':copy.deepcopy(out),'observed_at':at}
  x={'auth':{'tenant_id':tenant,'logical_service_id':owner,'actor_id':actor},'request':req,'response':res};self.data['events'].append({'at':at,'exchange':x})
  if not reject:locations.setdefault(name,(self.file,idx))
  return idx
 def save(self):(ROOT/'contracts/examples/protocol'/self.file).write_text(json.dumps(self.data,ensure_ascii=False,indent=2)+'\n')
 def mutation(self,name,event,path,value,rule,op='set'):
  mutations.append({'name':name,'fixture':self.file,'expect':rule,'edits':[{'op':op,'path':f'/events/{event}/exchange/'+path,**({'value':value} if op=='set' else {})}]})
scope={'resource_owner_id':owner,'resource_type':'content','selector':{'object_ids':[ID('resource-one')]},'normalizer_version':'1.0.0'}
policy={'subject':{'tenant_id':tenant,'actor_id':actor,'actor_kind':'user'},'resources':[scope],'actions':['read'],'purposes':['research'],'recipients':[actor],'locations':[owner],'mode':'continuous','valid_from':T,'expires_at':EXP,'limits':[{'unit':'calls','limit':'10'},{'unit':'USD','limit':'5'}],'max_offline_window_ms':600000}
grant={'grant_id':ID('grant-gov'),'owner_id':owner,'revision':1,'policy':policy,'intent_hash':H('intent'),'confirmation_ref':OR('confirmation-grant'),'state':'active','issued_at':T,'propagation':[]}
issue={k:grant[k] for k in ('grant_id','policy','intent_hash','confirmation_ref')}
f=Fixture(30,'grant-lifecycle','一次确认签发、当前读取、撤销后状态不可复活。')
a=f.event('grant.issue',issue,grant);b=f.event('grant.read',{},grant,grant['grant_id']);rev=copy.deepcopy(grant);rev.update(revision=2,state='revoked');c=f.event('grant.revoke',{'reason':'user revoked'},rev,grant['grant_id']);d=f.event('grant.read',{},rev,grant['grant_id'])
f.mutation('grant issue binds original intent',a,'response/output/intent_hash',H('different'),'governance_binding');f.mutation('grant read binds target',b,'response/output/grant_id',ID('grant-other'),'governance_binding');f.mutation('grant revoke closes policy',c,'response/output/state','active','grant_revocation');f.mutation('grant cannot reopen after revocation',d,'response/output/state','active','grant_reopen');f.save()
# Pairing credentials here are synthetic documentation data.
pscope={'capability_ids':[ID('capability-one')],'resource_scopes':[scope],'max_offline_window_ms':0}
session={'pairing_id':ID('pairing-gov'),'revision':1,'requested_scope':pscope,'device_description':'Constructed laptop endpoint','expires_at':'2026-09-26T00:05:00Z','poll_interval_ms':5000,'state':'pending'}
begin={'pairing_id':session['pairing_id'],'device_description':session['device_description'],'requested_scope':pscope,'client_nonce':H('nonce')}
device='synthetic-device-code-for-documentation-0001'
f=Fixture(31,'pairing-lifecycle','预认证创建、本人批准、设备一次领取及凭据撤销；token只是构造数据。')
a=f.event('endpoint.pair.begin',begin,{'session':session,'device_code':device,'user_code':'ABCD-EFGH','verification_uri':'https://example.invalid/pair'})
f.event('endpoint.pair.claim',{'device_code':device,'client_nonce':H('nonce')},{'state':'pending','session':session},session['pairing_id'])
approved=copy.deepcopy(session);approved.update(revision=2,state='approved');b=f.event('endpoint.pair.approve',{'user_code':'ABCD-EFGH','decision':'approve','approved_scope':pscope,'confirmation_ref':OR('confirmation-pair')},approved,session['pairing_id'])
endpoint={'endpoint_id':ID('endpoint-gov'),'tenant_id':tenant,'instance_id':instance,'credential_generation':1,'state':'active','approved_scope':pscope,'registered_at':T,'propagation':[]}
claimed=copy.deepcopy(approved);claimed.update(revision=3,state='claimed',endpoint_id=endpoint['endpoint_id'])
claimout={'state':'claimed','session':claimed,'endpoint':endpoint,'credential':{'token_type':'Bearer','access_token':'synthetic-bearer-token-never-valid-00000001','expires_at':EXP,'recovery_until':'2026-09-26T00:05:00Z'}}
c=f.event('endpoint.pair.claim',{'device_code':device,'client_nonce':H('nonce')},claimout,session['pairing_id']);erev=copy.deepcopy(endpoint);erev.update(credential_generation=2,state='revoked');d=f.event('endpoint.revoke',{'reason':'lost device'},erev,endpoint['endpoint_id'])
f.mutation('pair begin stays pending',a,'response/output/session/state','approved','pairing_state');f.mutation('pair approve matches decision',b,'response/output/state','denied','pairing_decision');f.mutation('claim endpoint mapping fixed',c,'response/output/session/endpoint_id',ID('endpoint-other'),'governance_binding');f.mutation('endpoint revoke closes credential',d,'response/output/state','active','endpoint_revocation');f.save()
f=Fixture(32,'lease-settlement','离线租约固定一个实例；原累计用量结算到最终关闭。')
f.event('grant.issue',issue,grant)
allocate={'lease_id':ID('lease-gov'),'grant_refs':[OR('grant-gov')],'endpoint_id':endpoint['endpoint_id'],'instance_id':instance,'scope':policy,'allocated_units':AM(5),'allocated_cost':AM(2,'USD'),'expires_at':'2026-09-26T00:10:00Z','confirmation_ref':OR('confirmation-lease')}
lease={k:v for k,v in allocate.items() if k!='confirmation_ref'};lease.update(owner_id=owner,revision=1,issued_at=T,owner_revision=1,state='allocated',settled_units=AM(0),settled_cost=AM(0,'USD'))
a=f.event('grant.lease.allocate',allocate,lease)
settle={'instance_id':instance,'usage_revision':1,'uses':[{'use_id':ID('use-lease'),'intent_hash':H('lease-use'),'used_units':AM(1),'used_cost':AM('0.1','USD'),'started_at':T,'closed':True}],'cumulative_units':AM(1),'cumulative_cost':AM('0.1','USD'),'final':True,'closure_ref':OR('closure-lease')}
closed=copy.deepcopy(lease);closed.update(revision=2,state='reconciled',settled_units=AM(1),settled_cost=AM('0.1','USD'),final_settlement_ref=OR('settlement-lease'))
b=f.event('grant.lease.settle',settle,closed,lease['lease_id'])
f.mutation('allocation cannot switch instance',a,'response/output/instance_id',ID('instance-other'),'governance_binding');f.mutation('final settlement requires closed use',b,'request/payload/uses/0/closed',False,'lease_final');f.save()
artifact=CR('plugin-artifact');config=CR('config');manifest={'package_id':ID('package-gov'),'version':'1.0.0','digest':artifact['hash'],'kind':'plugin','entrypoints':['dist/main.js'],'ports':['brain'],'contract_versions':['harness/1'],'dependencies':[],'requested_permissions':[],'trust_requirement':'trusted_native','state_formats':[]}
prepare={'lock_id':ID('lock-gov'),'artifact_ref':artifact,'manifest':manifest,'config_ref':config,'config_digest':config['hash'],'platform':'linux-x86_64','trust_evidence':[CR('review')],'conformance_report':CR('contract-report')}
lock={k:v for k,v in prepare.items() if k!='artifact_ref'};lock.update(manifest_digest=H('manifest'),resolved_dependencies=[],state_compatibility=[],prepared_at=T,reference_revision=1)
f=Fixture(33,'installation-lifecycle','准备完整安装锁、按kind读取、停用入口与无引用清理。')
a=f.event('extensions.prepare',prepare,lock);b=f.event('extensions.read',{'kind':'install_lock'},lock,lock['lock_id'])
c=f.event('extensions.deactivate',{'activation_id':ID('activation-gov'),'generation':1,'reason':'maintenance','management_ref':OR('management')},{'activation_id':ID('activation-gov'),'generation':1,'new_use_disabled':True,'previous_version_ready':False,'residual_work':[],'decided_at':T},target)
d=f.event('extensions.dispose',{'reference_revision':1},{'lock_id':lock['lock_id'],'state':'disposed','reference_revision':1,'blocking_refs':[],'closed_index_ref':OR('closed-lock')},lock['lock_id'])
f.mutation('prepare digest matches artifact',a,'request/payload/artifact_ref/hash',H('wrong-artifact'),'package_digest');f.mutation('lock query cannot change identity',b,'response/output/lock_id',ID('lock-other'),'extension_projection');f.mutation('deactivate names original activation',c,'response/output/activation_id',ID('activation-other'),'governance_binding');f.mutation('dispose cannot drop live references',d,'response/output/blocking_refs',[OR('task-live')],'extension_references');f.save()
metric={'name':'contract coverage','direction':'minimum','threshold':AM(1,'ratio'),'required':True}
def eval_objects(label,formal=False):
 cand={'candidate_id':ID('candidate-'+label),'revision':1,'release_kind':'improvement' if formal else 'compatibility','parent_candidate_ids':[],'kind':'plugin','artifact_ref':artifact,'source_refs':[],'candidate_lock':lock['lock_id'],'baseline_lock':ID('lock-baseline') if formal else None,'digest':artifact['hash']}
 if formal:cand['improvement_id']=ID('improvement-'+label)
 sample=ID('sample-'+label);group=ID('group-'+label)
 part={'partition_id':ID('partition-'+label),'revision':1,'dataset_ref':CR('dataset-'+label),'split':'holdout' if formal else 'development','sample_ids':[sample],'source_groups':[{'source_group_id':group,'sample_ids':[sample]}],'content_digest':H('dataset-'+label),'permission_refs':[OR('grant-data')],'state':'available'}
 plan={'plan_id':ID('plan-'+label),'digest':H('plan-'+label),'purpose':'release_confirmation' if formal else 'compatibility_check','candidate_id':cand['candidate_id'],'candidate_lock':cand['candidate_lock'],'baseline_lock':cand['baseline_lock'],'partition_id':part['partition_id'],'sample_ids':[sample],'seed':'fixed-seed-1','environment_binding':component('environment-adapter'),'judge_binding':component('judge-adapter'),'metrics':[metric],'budget':[{'unit':'USD','limit':'10'}],'retry_limit':2,'stop_rule':'one frozen sample set only','invalid_run_policy':'whole pair preflight failure is invalid','sampling':{'frame':'constructed single documentation sample','sample_unit':'independent task','sample_size':1,'source_group_ids':[group],'method':'independent' if formal else 'fixed_set','analysis_method':'predeclared independent binary analysis' if formal else 'contract coverage only','predeclared_stopping':'fixed sample count'}}
 if formal:plan.update(release_request_id=ID('release-'+label),minimum_practical_gain=AM('0.01','ratio'),regression_limits=[metric],improvement_policy={'improvement_id':cand['improvement_id'],'policy_digest':H('policy-'+label),'candidate_scope':[cand['candidate_id']],'formal_attempt_limit':1,'stop_rule':'one formal confirmation','inference_method':'predeclared Wilson rule for real sample set','comparison_method':'paired practical improvement and guards','related_improvement_ids':[],'approved_by':actor,'confirmation_ref':OR('confirmation-policy')},formal_attempt_index=1,reservation_id=ID('reservation-'+label))
 run={'run_id':ID('run-'+label),'plan_id':plan['plan_id'],'revision':1,'state':'queued','cancel_requested':False,'completed_samples':0,'total_samples':1,'environment_refs':[],'environment_sealed':False,'cleanup_state':'pending'}
 gate={'applicable':formal,'result':'pass' if formal else 'inconclusive','reason':'constructed recorded gate outcome; no runtime claim' if formal else 'not applicable to compatibility claim'}
 report={'report_id':ID('report-'+label),'plan_id':plan['plan_id'],'plan_digest':plan['digest'],'candidate_id':cand['candidate_id'],'candidate_digest':cand['digest'],'digest':H('report-'+label),'evidence_class':'formal' if formal else 'conformance','sealed':True,'eligibility':'eligible','run_refs':[OR('run-'+label)],'metrics':[],'coverage_complete':True,'gaps':[],'contract_gate':'pass','target_attainment':gate,'statistical_gate':gate,'improvement_gate':gate,'category_changes':[],'sealed_at':T}
 if formal:report['paired_counts']={'a':0,'b':1,'c':0,'d':0}
 exposure={'exposure_id':ID('exposure-'+label),'partition_id':part['partition_id'],'source_group_ids':[group],'scope':'full_report','recipient':actor,'occurred_at':'2026-09-26T00:00:05Z','recorded_at':'2026-09-26T00:00:05Z','evidence_refs':[],'reason':'controlled feedback after sealing','report_id':report['report_id'],'report_digest':report['digest']}
 approval={'approval_id':ID('approval-'+label),'revision':1,'release_kind':cand['release_kind'],'candidate_id':cand['candidate_id'],'candidate_digest':cand['digest'],'lock_id':lock['lock_id'],'report_id':report['report_id'],'report_digest':report['digest'],'targets':[target],'approved_by':actor,'confirmation_ref':OR('confirmation-release'),'batches':[{'batch_id':ID('batch-'+label),'targets':[target],'observation_window_ms':1000,'minimum_samples':1,'stop_rules':[metric]}],'expires_at':EXP,'max_offline_window_ms':0,'rollback_lock':cand['baseline_lock'],'state':'active','propagation':[]}
 if formal:
  samples=[ID('sample-'+label+'-'+str(i)) for i in range(100)]
  groups=[{'source_group_id':ID('group-'+label+'-'+str(i)),'sample_ids':[sample]} for i,sample in enumerate(samples)]
  part.update(sample_ids=samples,source_groups=groups)
  plan['sample_ids']=samples;plan['sampling'].update(sample_size=100,source_group_ids=[g['source_group_id'] for g in groups])
  plan['metrics'] += [{'name':'first_call_correct','direction':'minimum','threshold':AM('0.90','ratio'),'required':True},{'name':'task_success','direction':'minimum','threshold':AM('0.95','ratio'),'required':True}]
  run['total_samples']=100
  report['paired_counts']={'a':0,'b':100,'c':0,'d':0}
  report['metrics']=[{'name':name,'value':AM(1,'ratio'),'numerator':100,'denominator':100} for name in ('first_call_correct','task_success')]
  exposure['source_group_ids']=[g['source_group_id'] for g in groups]
 return cand,part,plan,run,report,exposure,approval

def seed_eval(f,label,formal=False):
 c,p,plan,r,report,exposure,a=eval_objects(label,formal)
 indexes=[]
 indexes.append(f.event('evaluation.candidate_register',{'candidate':c},c));indexes.append(f.event('evaluation.partition_register',{'partition':p},p));indexes.append(f.event('evaluation.plan_create',{'plan':plan},plan));indexes.append(f.event('evaluation.run',{'run_id':r['run_id'],'plan_id':plan['plan_id'],'plan_digest':plan['digest']},r,plan['plan_id']));finished=copy.deepcopy(r);finished.update(revision=2,state='finished',completed_samples=r['total_samples'],environment_sealed=True,cleanup_state='cleaned',report_id=report['report_id']);f.event('evaluation.read',{'kind':'run'},{'kind':'run','record':finished},r['run_id']);indexes.append(f.event('evaluation.read',{'kind':'plan'},{'kind':'plan','record':plan},plan['plan_id']));indexes.append(f.event('evaluation.feedback_open',{'exposure_id':exposure['exposure_id'],'report_digest':report['digest'],'scope':'full_report'},{'exposure':exposure,'report':report},report['report_id']));indexes.append(f.event('evaluation.approve',{'approval':a},a))
 return (c,p,plan,r,report,exposure,a),indexes
f=Fixture(34,'compatibility-release','契约报告支持首装兼容批准；不宣称统计改善，逐目标完成后可撤回。');objects,ix=seed_eval(f,'compat');c,p,plan,r,report,exposure,approval=objects
roll={'rollout_id':ID('rollout-compat'),'approval_id':approval['approval_id'],'revision':1,'state':'finished','current_batch':0,'targets':[{'target_id':target,'activation_id':ID('activation-gov'),'state':'active','ready':True,'observation_elapsed_ms':1000,'samples_observed':1,'residual_work':[]}]}
a=f.event('evaluation.rollout_read',{},roll,roll['rollout_id']);rev=copy.deepcopy(approval);rev.update(revision=2,state='revoked');b=f.event('evaluation.revoke',{'reason':'withdraw release'},rev,approval['approval_id'])
f.mutation('candidate output stays exact',ix[0],'response/output/digest',H('wrong'),'governance_binding');f.mutation('partition groups cover samples',ix[1],'response/output/source_groups/0/sample_ids',[ID('sample-other')],'partition_sources');f.mutation('plan output cannot change seed',ix[2],'response/output/seed','changed','governance_binding');f.mutation('run uses frozen plan digest',ix[3],'request/payload/plan_digest',H('wrong-plan'),'run_plan');f.mutation('evaluation query projection matches kind',ix[4],'response/output/record/plan_id',ID('plan-other'),'governance_binding');f.mutation('feedback report must be sealed',ix[5],'response/output/report/sealed',False,'feedback_sealed');f.mutation('approval batches cover targets',ix[6],'response/output/batches/0/targets',[ID('target-other')],'approval_targets');f.mutation('rollout finished needs ready',a,'response/output/targets/0/ready',False,'rollout_completion');f.mutation('approval revoke cannot remain active',b,'response/output/state','active','approval_revocation');f.save()
f=Fixture(35,'improvement-exposure','正式改善记录先有过程策略和保留占用；批准后发现早期泄露必须失效并撤回。');objects,ix=seed_eval(f,'improve',True);c,p,plan,r,report,exposure,approval=objects
leak={'exposure_id':ID('exposure-leak'),'partition_id':p['partition_id'],'source_group_ids':p['source_groups'][0:1][0]['sample_ids'] if False else [p['source_groups'][0]['source_group_id']],'scope':'answers','recipient':actor,'occurred_at':None,'recorded_at':'2026-09-26T00:00:07Z','evidence_refs':[CR('leak-proof')],'reason':'unknown-time leak recorded conservatively'}
a=f.event('evaluation.exposure_record',{'exposure':leak},{'exposure':leak,'invalidated_plan_ids':[plan['plan_id']],'revocation_job_ids':[ID('job-revoke')]})
rev=copy.deepcopy(approval);rev.update(revision=2,state='revoked');f.event('evaluation.revoke',{'reason':'holdout leaked before sealing'},rev,approval['approval_id'])
f.mutation('exposure must invalidate related formal plan',a,'response/output/invalidated_plan_ids',[],'exposure_invalidation');f.mutation('formal plan needs positive gain',ix[2],'response/output/minimum_practical_gain/amount','0','improvement_plan');f.mutation('formal approval cannot accept failed improvement gate',ix[5],'response/output/report/improvement_gate/result','fail','improvement_approval');f.save()
# Startup evidence: original activation and per-instance evidence are independent.
def startup(approval,inst,kind='local_transaction',action='activation',aid=None):
 e={'kind':kind,'approval_id':approval['approval_id'],'approval_revision':approval['revision'],'target_id':target,'lock_id':lock['lock_id'],'instance_id':inst,'action_kind':action,'action_id':aid or ID('activation-gov'),'checked_at':'2026-09-26T00:00:10Z'}
 if kind=='local_transaction':e['commit_id']=ID('commit-'+inst+action)
 else:e.update(use_id=ID('use-'+inst+action),start_before='2026-09-26T00:00:30Z')
 return e

def active_record(approval,initial,current=None):
 current=current or initial
 out={'activation_id':ID('activation-gov'),'target_id':target,'old_lock_id':None,'new_lock_id':lock['lock_id'],'approval_id':approval['approval_id'],'phase':'active','generation':1,'ready_instance':current['instance_id'],'last_observed_at':current['checked_at'],'approval_revision':approval['revision'],'new_use_disabled':False,'previous_version_ready':False,'residual_work':[],'startup_evidence':initial,'instance_readiness':{'instance_id':current['instance_id'],'ready':True,'observed_at':current['checked_at'],'startup_evidence':current}}
 if initial['kind']=='remote_use':out['activation_use_id']=initial['use_id']
 return out

def activate(f,approval):return f.event('extensions.activate',{'activation_id':ID('activation-gov'),'target_id':target,'old_lock_id':None,'new_lock_id':lock['lock_id'],'approval_id':approval['approval_id'],'expected_generation':0},{'activation_id':ID('activation-gov')},target)
f=Fixture(36,'local-install-restart','本地共库首装与重启，无在线ApprovalUse；重启保留原激活证据、当前实例另取reopen事务依据。');objects,ix=seed_eval(f,'local');approval=objects[-1];activate(f,approval);initial=startup(approval,instance);initial['checked_at']=f'2026-09-26T00:00:{f.sec:02}Z';a=f.event('extensions.read',{'kind':'activation'},active_record(approval,initial),ID('activation-gov'));reopen=startup(approval,ID('instance-2'),action='reopen',aid=ID('reopen-local'));reopen['checked_at']=f'2026-09-26T00:00:{f.sec:02}Z';b=f.event('extensions.read',{'kind':'activation'},active_record(approval,initial,reopen),ID('activation-gov'))
f.mutation('restart preserves original activation evidence',b,'response/output/startup_evidence/commit_id',ID('commit-replaced'),'activation_history');f.mutation('current readiness binds actual instance',b,'response/output/instance_readiness/instance_id',instance,'activation_readiness');f.save()
f=Fixture(37,'remote-instance-reopen','远端新实例取得独立reopen回执；原激活身份与代际不变，旧实例回执不可复用。');objects,ix=seed_eval(f,'remote');approval=objects[-1];second=ID('instance-2');f.data['approvals']=[{'approval_id':approval['approval_id'],'revision':1,'state':'active','target_id':target,'lock_id':lock['lock_id'],'instance_id':instance,'eligible_instance_ids':[instance,second],'max_offline_window_ms':0,'expires_at':EXP,'already_active':True}]
activate(f,approval)
def use_event(f,e):
 u={k:e[k] for k in ('use_id','approval_id','approval_revision','target_id','lock_id','instance_id','action_kind','action_id','start_before')};p={k:v for k,v in u.items() if k not in ('approval_revision','start_before')};return f.event('evaluation.approval_check',p,u,approval['approval_id'])
initial=startup(approval,instance,'remote_use');initial['checked_at']=f'2026-09-26T00:00:{f.sec+1:02}Z';use_event(f,initial);a=f.event('extensions.read',{'kind':'activation'},active_record(approval,initial),ID('activation-gov'));reopen=startup(approval,second,'remote_use','reopen',ID('reopen-remote'));reopen['checked_at']=f'2026-09-26T00:00:{f.sec+1:02}Z';use_event(f,reopen);b=f.event('extensions.read',{'kind':'activation'},active_record(approval,initial,reopen),ID('activation-gov'))
f.mutation('remote reopen cannot use prior instance receipt',b,'response/output/instance_readiness/startup_evidence/use_id',initial['use_id'],'activation_approval');f.save()
f=Fixture(38,'revoked-approval-startup','批准撤回后保留历史激活，可查询disabled；新的在线启动明确拒绝。');objects,ix=seed_eval(f,'revoke');approval=objects[-1];f.data['approvals']=[{'approval_id':approval['approval_id'],'revision':1,'state':'active','target_id':target,'lock_id':lock['lock_id'],'instance_id':instance,'max_offline_window_ms':0,'expires_at':EXP,'already_active':True}];activate(f,approval);initial=startup(approval,instance);initial['checked_at']=f'2026-09-26T00:00:{f.sec:02}Z';f.event('extensions.read',{'kind':'activation'},active_record(approval,initial),ID('activation-gov'));rev=copy.deepcopy(approval);rev.update(revision=2,state='revoked');f.event('evaluation.revoke',{'reason':'stop all new use'},rev,approval['approval_id']);disabled=active_record(approval,initial);disabled.update(phase='disabled',new_use_disabled=True);disabled['instance_readiness']['ready']=False;f.event('extensions.read',{'kind':'activation'},disabled,ID('activation-gov'))
p={'use_id':ID('use-revoked'),'approval_id':approval['approval_id'],'target_id':target,'lock_id':lock['lock_id'],'instance_id':instance,'action_kind':'reopen','action_id':ID('reopen-revoked')};idx=f.event('evaluation.approval_check',p,None,approval['approval_id'],reject='approval_inactive')
# A semantically legal response shape that illegally admits after observed revocation.
res={'command_id':f.data['events'][idx]['exchange']['request']['command_id'],'stage':'applied','decided_at':f.data['events'][idx]['at'],'output':{**p,'approval_revision':1,'start_before':'2026-09-26T00:00:30Z'}}
f.mutation('new startup cannot bypass observed revocation',idx,'response',res,'approval_current');f.save()
f=Fixture(39,'evaluation-cancel','实验取消保存封闭与清理责任，运行终态不冒充环境已销毁。');c,p,plan,r,report,exposure,approval=eval_objects('cancel');f.event('evaluation.candidate_register',{'candidate':c},c);f.event('evaluation.partition_register',{'partition':p},p);f.event('evaluation.plan_create',{'plan':plan},plan);f.event('evaluation.run',{'run_id':r['run_id'],'plan_id':plan['plan_id'],'plan_digest':plan['digest']},r,plan['plan_id']);cancelled=copy.deepcopy(r);cancelled.update(revision=2,cancel_requested=True,state='blocked',reason='environment creation result unknown',environment_refs=[OR('environment-unknown')],cleanup_state='pending');idx=f.event('evaluation.cancel',{'reason':'user stopped experiment'},cancelled,r['run_id']);f.event('evaluation.read',{'kind':'run'},{'kind':'run','record':cancelled},r['run_id']);f.mutation('cancel preserves intention and original run',idx,'response/output/cancel_requested',False,'evaluation_cancel');f.save()
# Every newly frozen method gets a deliberate unknown input field regression.
for name,spec in M.items():
 if ns['oldm'][name]['status']!='reserved':continue
 file,idx=locations[name];mutations.append({'name':name+' rejects unknown payload field','fixture':file,'expect':'schema','edits':[{'op':'set','path':f'/events/{idx}/exchange/request/payload/unexpected_field','value':True}]})
(SCR/'governance-mutations.json').write_text(json.dumps(mutations,ensure_ascii=False,indent=2)+'\n')
print('10 governance fixtures;',len(mutations),'mutations;',len(locations),'methods represented')
