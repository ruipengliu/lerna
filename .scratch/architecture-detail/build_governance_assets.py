import json, copy, hashlib
from pathlib import Path
ROOT=Path('docs/architecture'); SCR=Path('.scratch/architecture-detail')
base=json.loads((ROOT/'contracts/schemas/protocol.schema.json').read_text()); oldm=json.loads((ROOT/'contracts/schemas/methods.json').read_text())['methods']
D={}; M={}
def ref(n):return {'$ref':'#/$defs/'+n}
ID=ref('Id'); DG=ref('Digest'); REV=ref('Revision'); TM={'type':'string','format':'date-time','pattern':'Z$'}; TEXT={'type':'string','minLength':1,'maxLength':4096}; BOOL={'type':'boolean'}
def integer(lo=0,hi=2147483647):return {'type':'integer','minimum':lo,'maximum':hi}
def enum(*v):return {'enum':list(v)}
def arr(t,lo=0,hi=100,unique=False):
 s={'type':'array','items':t,'minItems':lo,'maxItems':hi}
 if unique:s['uniqueItems']=True
 return s
def obj(p,optional=()):return {'type':'object','properties':p,'required':[k for k in p if k not in optional],'additionalProperties':False}
def nullable(t):return {'oneOf':[t,{'type':'null'}]}
def put(n,p,optional=()):D[n]=obj(p,optional);return ref(n)
def cond(n,key,value,required=(),forbidden=()):
 t={}
 if required:t['required']=list(required)
 if forbidden:t['not']={'anyOf':[{'required':[x]} for x in forbidden]}
 D[n].setdefault('allOf',[]).append({'if':{'properties':{key:{'const':value}},'required':[key]},'then':t})
def meth(name,inp,out,source,target,query=False,conditional=False,success='',extra=()):
 template=oldm['grant.use']; errors=list(dict.fromkeys(template['errors']+list(extra)))
 recovery=copy.deepcopy(template['error_recovery'])
 for e in extra:recovery.setdefault(e,['after_change','query_original'] if e.endswith(('conflict','unknown')) else ['after_change'])
 M[name]={'status':'frozen-draft','kind':'query' if query else 'command','input':inp,'output':out,'target':target,'expected_revision':conditional,'stages':[] if query else ['applied','rejected'],'errors':errors,'success':success,'source':source+'/README.md','error_recovery':{k:recovery.get(k,['after_change']) for k in errors}}
# Security: precise policy; Confirmation itself is owned by the trusted interaction surface.
put('GrantPolicy',{'subject':ref('Subject'),'resources':arr(ref('ResourceScope'),1),'actions':arr(enum('read','process','store','sync','disclose','act','manage'),1,7,True),'purposes':arr(TEXT,1,100,True),'recipients':arr(ID,1,100,True),'locations':arr(ID,1,100,True),'mode':enum('once','continuous'),'valid_from':TM,'expires_at':TM,'limits':arr(ref('BudgetLimit'),1),'max_offline_window_ms':integer(0,86400000),'parent_grant_ref':ref('ObjectRef')},('parent_grant_ref',))
put('PropagationTarget',{'endpoint_id':ID,'required_revision':REV,'enforced_revision':integer(),'state':enum('pending','enforced','unreachable'),'last_observed_at':TM})
put('GrantRecord',{'grant_id':ID,'owner_id':ID,'revision':REV,'policy':ref('GrantPolicy'),'intent_hash':DG,'confirmation_ref':ref('ObjectRef'),'state':enum('active','revoked'),'issued_at':TM,'propagation':arr(ref('PropagationTarget'))})
put('GrantIssueInput',{'grant_id':ID,'policy':ref('GrantPolicy'),'intent_hash':DG,'confirmation_ref':ref('ObjectRef')})
D['GrantIssueOutput']=ref('GrantRecord');put('GrantReadInput',{});D['GrantReadOutput']=ref('GrantRecord')
put('GrantRevokeInput',{'reason':TEXT});D['GrantRevokeOutput']=ref('GrantRecord')
meth('grant.issue','GrantIssueInput','GrantIssueOutput','security','owner_id',success='原许可、一次确认消费、原回执同事务保存。')
meth('grant.read','GrantReadInput','GrantReadOutput','security','grant_id',True,success='当前许可与逐端传播范围；过期独立按时间判定。')
meth('grant.revoke','GrantRevokeInput','GrantRevokeOutput','security','grant_id',conditional=True,success='撤销修订和传播责任已保存；未承诺全端立即停止。')
put('LeaseAllocationInput',{'lease_id':ID,'grant_refs':arr(ref('ObjectRef'),1),'endpoint_id':ID,'instance_id':ID,'scope':ref('GrantPolicy'),'allocated_units':ref('Amount'),'allocated_cost':ref('Amount'),'expires_at':TM,'confirmation_ref':ref('ObjectRef')})
put('OfflineLeaseRecord',{'lease_id':ID,'owner_id':ID,'revision':REV,'grant_refs':arr(ref('ObjectRef'),1),'endpoint_id':ID,'instance_id':ID,'scope':ref('GrantPolicy'),'allocated_units':ref('Amount'),'allocated_cost':ref('Amount'),'issued_at':TM,'expires_at':TM,'owner_revision':REV,'state':enum('allocated','in_use','closed','reconciled'),'settled_units':ref('Amount'),'settled_cost':ref('Amount'),'final_settlement_ref':ref('ObjectRef')},('final_settlement_ref',))
put('LeaseUsageItem',{'use_id':ID,'intent_hash':DG,'used_units':ref('Amount'),'used_cost':ref('Amount'),'started_at':TM,'closed':BOOL})
put('LeaseSettlementInput',{'instance_id':ID,'usage_revision':REV,'uses':arr(ref('LeaseUsageItem')),'cumulative_units':ref('Amount'),'cumulative_cost':ref('Amount'),'final':BOOL,'closure_ref':ref('ObjectRef')},('closure_ref',));cond('LeaseSettlementInput','final',True,['closure_ref'])
D['LeaseAllocationOutput']=ref('OfflineLeaseRecord');D['LeaseSettlementOutput']=ref('OfflineLeaseRecord')
meth('grant.lease.allocate','LeaseAllocationInput','LeaseAllocationOutput','security','owner_id',success='唯一端点实例租约及预算预留同事务提交；失回执不再分配。')
meth('grant.lease.settle','LeaseSettlementInput','LeaseSettlementOutput','security','lease_id',conditional=True,success='原使用累计值去重结算；最终关闭有据才释放余额。')
put('PairingScope',{'capability_ids':arr(ID,0,100,True),'resource_scopes':arr(ref('ResourceScope')),'max_offline_window_ms':integer(0,86400000)})
put('PairingSessionRecord',{'pairing_id':ID,'revision':REV,'requested_scope':ref('PairingScope'),'device_description':TEXT,'expires_at':TM,'poll_interval_ms':integer(1000,30000),'state':enum('pending','approved','denied','claimed','expired'),'endpoint_id':ID},('endpoint_id',))
put('EndpointRecord',{'endpoint_id':ID,'tenant_id':ID,'instance_id':ID,'credential_generation':REV,'state':enum('active','revoked'),'approved_scope':ref('PairingScope'),'registered_at':TM,'propagation':arr(ref('PropagationTarget'))})
put('PairBeginInput',{'pairing_id':ID,'device_description':TEXT,'requested_scope':ref('PairingScope'),'client_nonce':DG})
put('PairBeginOutput',{'session':ref('PairingSessionRecord'),'device_code':{'type':'string','minLength':32,'maxLength':256},'user_code':{'type':'string','pattern':'^[A-Z0-9-]{8,16}$'},'verification_uri':{'type':'string','format':'uri','maxLength':2048}})
put('PairApproveInput',{'user_code':{'type':'string','pattern':'^[A-Z0-9-]{8,16}$'},'decision':enum('approve','deny'),'approved_scope':ref('PairingScope'),'confirmation_ref':ref('ObjectRef')});D['PairApproveOutput']=ref('PairingSessionRecord')
put('PairClaimInput',{'device_code':{'type':'string','minLength':32,'maxLength':256},'client_nonce':DG})
put('PairClaimOutput',{'state':enum('pending','claimed'),'session':ref('PairingSessionRecord'),'endpoint':ref('EndpointRecord'),'credential':obj({'token_type':{'const':'Bearer'},'access_token':{'type':'string','minLength':32,'maxLength':8192},'expires_at':TM,'recovery_until':TM})},('endpoint','credential'))
cond('PairClaimOutput','state','claimed',['endpoint','credential']);cond('PairClaimOutput','state','pending',forbidden=['endpoint','credential'])
put('EndpointRevokeInput',{'reason':TEXT});D['EndpointRevokeOutput']=ref('EndpointRecord')
meth('endpoint.pair.begin','PairBeginInput','PairBeginOutput','security','pairing_service_id',success='预认证限流入口保存有限会话；未赋予tenant或业务权限。',extra=('pairing_expired','slow_down'))
meth('endpoint.pair.approve','PairApproveInput','PairApproveOutput','security','pairing_id',conditional=True,success='本人受信确认与批准或拒绝决定持久保存。',extra=('pairing_expired','pairing_conflict'))
meth('endpoint.pair.claim','PairClaimInput','PairClaimOutput','security','pairing_id',success='原会话的一次凭据领取；pending仅指待批准。',extra=('pairing_expired','authorization_pending','slow_down'))
meth('endpoint.revoke','EndpointRevokeInput','EndpointRevokeOutput','security','endpoint_id',conditional=True,success='端点撤销、凭据代次递增及传播责任同事务提交。')
# Extensions: immutable verified installation and independent activation / instance readiness.
put('PackageDependency',{'package_id':ID,'version_range':TEXT})
put('StateFormat',{'domain':TEXT,'read_versions':arr(TEXT,1),'write_version':TEXT,'migration_entrypoint':TEXT},('migration_entrypoint',))
put('PackageManifest',{'package_id':ID,'version':TEXT,'digest':DG,'kind':enum('plugin','skill','agent_config'),'entrypoints':arr(TEXT),'ports':arr(TEXT,1),'contract_versions':arr(TEXT,1),'dependencies':arr(ref('PackageDependency')),'requested_permissions':arr(ref('ResourceScope')),'trust_requirement':enum('trusted_native','isolated_native','data_only'),'state_formats':arr(ref('StateFormat'))})
put('ResolvedPackage',{'package_id':ID,'version':TEXT,'digest':DG,'artifact_ref':ref('ContentRef')})
put('InstallLock',{'lock_id':ID,'manifest':ref('PackageManifest'),'manifest_digest':DG,'resolved_dependencies':arr(ref('ResolvedPackage')),'config_ref':ref('ContentRef'),'config_digest':DG,'platform':TEXT,'trust_evidence':arr(ref('ContentRef'),1),'conformance_report':ref('ContentRef'),'state_compatibility':arr(ref('StateFormat')),'prepared_at':TM,'reference_revision':REV})
put('ExtensionsPrepareInput',{'lock_id':ID,'artifact_ref':ref('ContentRef'),'manifest':ref('PackageManifest'),'config_ref':ref('ContentRef'),'config_digest':DG,'platform':TEXT,'trust_evidence':arr(ref('ContentRef'),1),'conformance_report':ref('ContentRef')});D['ExtensionsPrepareOutput']=ref('InstallLock')
put('ExtensionsDeactivateInput',{'activation_id':ID,'generation':REV,'reason':TEXT,'management_ref':ref('ObjectRef')})
put('DeactivationResult',{'activation_id':ID,'generation':REV,'new_use_disabled':{'const':True},'previous_version_ready':BOOL,'residual_work':arr(ID),'decided_at':TM})
put('ExtensionsDisposeInput',{'reference_revision':REV});put('ExtensionsDisposeOutput',{'lock_id':ID,'state':enum('disposed','blocked'),'reference_revision':REV,'blocking_refs':arr(ref('ObjectRef')),'closed_index_ref':ref('ObjectRef')},('closed_index_ref',));cond('ExtensionsDisposeOutput','state','disposed',['closed_index_ref'])
meth('extensions.prepare','ExtensionsPrepareInput','ExtensionsPrepareOutput','extensions','extension_manager_id',success='完整制品、检查证据和不可变安装锁已提交；不执行包内安装脚本。',extra=('package_invalid','dependency_conflict','contract_unsupported','isolation_unavailable'))
meth('extensions.deactivate','ExtensionsDeactivateInput','DeactivationResult','extensions','target_id',conditional=True,success='关闭新入口与停用事实持久保存；残留责任单列。',extra=('generation_conflict','approval_inactive'))
meth('extensions.dispose','ExtensionsDisposeInput','ExtensionsDisposeOutput','extensions','lock_id',conditional=True,success='无引用则清理制品并保留关闭索引；否则返回具体blocked引用。',extra=('generation_conflict','drain_blocked'))
D['ApprovalRequest']=copy.deepcopy(base['$defs']['ApprovalRequest']);D['ApprovalRequest']['properties']['action_kind']=enum('activation','reopen','work')
D['ApprovalUse']=copy.deepcopy(base['$defs']['ApprovalUse']);D['ApprovalUse']['properties']['action_kind']=enum('activation','reopen','work')
D['ApprovalFixture']=copy.deepcopy(base['$defs']['ApprovalFixture']);D['ApprovalFixture']['properties']['eligible_instance_ids']=arr(ID,1,100,True)
common={'approval_id':ID,'approval_revision':REV,'target_id':ID,'lock_id':ID,'instance_id':ID,'action_kind':enum('activation','reopen'),'action_id':ID,'checked_at':TM}
put('LocalStartupEvidence',{'kind':{'const':'local_transaction'},**common,'commit_id':ID})
put('RemoteStartupEvidence',{'kind':{'const':'remote_use'},**common,'use_id':ID,'start_before':TM})
D['StartupEvidence']={'oneOf':[ref('LocalStartupEvidence'),ref('RemoteStartupEvidence')]}
put('InstanceReadiness',{'instance_id':ID,'ready':BOOL,'observed_at':TM,'startup_evidence':ref('StartupEvidence')})
D['Activation']=copy.deepcopy(base['$defs']['Activation']);D['Activation']['properties'].update({'startup_evidence':ref('StartupEvidence'),'instance_readiness':ref('InstanceReadiness')})
cond('Activation','phase','active',['startup_evidence','instance_readiness'])
# Draft assets are migrated together; no missing-evidence compatibility branch.
put('ExtensionsReadInput',{'kind':enum('activation','install_lock')})
D['ExtensionsReadOutput']={'oneOf':[ref('Activation'),ref('InstallLock')]}
M['extensions.read']=copy.deepcopy(oldm['extensions.read']);M['extensions.read']['success']='查询准确激活及当前实例，或不可变安装锁；不把活动指针当就绪。'
# Evaluation: contract-only paths never fabricate improvement evidence.
put('EvaluationCandidate',{'candidate_id':ID,'revision':REV,'release_kind':enum('compatibility','improvement'),'parent_candidate_ids':arr(ID,0,100,True),'kind':enum('memory_proposal','skill','execution_policy','agent_config','plugin','kernel'),'artifact_ref':ref('ContentRef'),'source_refs':arr(ref('ContentRef')),'candidate_lock':ID,'baseline_lock':nullable(ID),'digest':DG,'improvement_id':ID},('improvement_id',));cond('EvaluationCandidate','release_kind','improvement',['improvement_id'])
put('CandidateRegisterInput',{'candidate':ref('EvaluationCandidate')});D['CandidateRegisterOutput']=ref('EvaluationCandidate')
put('SourceGroup',{'source_group_id':ID,'sample_ids':arr(ID,1,1000,True)})
put('DatasetPartition',{'partition_id':ID,'revision':REV,'dataset_ref':ref('ContentRef'),'split':enum('development','selection','holdout'),'sample_ids':arr(ID,1,1000,True),'source_groups':arr(ref('SourceGroup'),1,1000),'content_digest':DG,'permission_refs':arr(ref('ObjectRef'),1),'state':enum('available','reserved','closed')})
put('PartitionRegisterInput',{'partition':ref('DatasetPartition')});D['PartitionRegisterOutput']=ref('DatasetPartition')
put('ImprovementPolicy',{'improvement_id':ID,'policy_digest':DG,'candidate_scope':arr(ID,1,100,True),'formal_attempt_limit':integer(1,100),'stop_rule':TEXT,'inference_method':TEXT,'comparison_method':TEXT,'related_improvement_ids':arr(ID,0,100,True),'approved_by':ID,'confirmation_ref':ref('ObjectRef')})
put('SamplingPlan',{'frame':TEXT,'sample_unit':TEXT,'sample_size':integer(1,1000000),'source_group_ids':arr(ID,1,1000,True),'method':enum('independent','fixed_set','cluster','weighted'),'analysis_method':TEXT,'predeclared_stopping':TEXT})
put('EvaluationMetric',{'name':TEXT,'direction':enum('minimum','maximum'),'threshold':ref('Amount'),'required':BOOL})
put('EvaluationPlan',{'plan_id':ID,'digest':DG,'purpose':enum('development','selection','compatibility_check','release_confirmation'),'candidate_id':ID,'candidate_lock':ID,'baseline_lock':nullable(ID),'partition_id':ID,'sample_ids':arr(ID,1,1000,True),'seed':TEXT,'release_request_id':ID,'environment_binding':ref('ComponentRef'),'judge_binding':ref('ComponentRef'),'metrics':arr(ref('EvaluationMetric'),1),'budget':arr(ref('BudgetLimit'),1),'retry_limit':integer(0,2),'stop_rule':TEXT,'invalid_run_policy':TEXT,'sampling':ref('SamplingPlan'),'minimum_practical_gain':ref('Amount'),'regression_limits':arr(ref('EvaluationMetric')),'improvement_policy':ref('ImprovementPolicy'),'formal_attempt_index':integer(1,100),'reservation_id':ID},('release_request_id','minimum_practical_gain','regression_limits','improvement_policy','formal_attempt_index','reservation_id'))
cond('EvaluationPlan','purpose','release_confirmation',['release_request_id','minimum_practical_gain','regression_limits','improvement_policy','formal_attempt_index','reservation_id'])
put('PlanCreateInput',{'plan':ref('EvaluationPlan')});D['PlanCreateOutput']=ref('EvaluationPlan')
put('EvaluationRunRecord',{'run_id':ID,'plan_id':ID,'revision':REV,'state':enum('queued','running','scoring','finished','blocked'),'cancel_requested':BOOL,'completed_samples':integer(),'total_samples':integer(1),'environment_refs':arr(ref('ObjectRef')),'environment_sealed':BOOL,'cleanup_state':enum('pending','cleaned','residual'),'report_id':ID,'reason':TEXT},('report_id','reason'))
put('EvaluationRunInput',{'run_id':ID,'plan_id':ID,'plan_digest':DG});D['EvaluationRunOutput']=ref('EvaluationRunRecord')
put('EvaluationCancelInput',{'reason':TEXT});D['EvaluationCancelOutput']=ref('EvaluationRunRecord')
put('GateResult',{'applicable':BOOL,'result':enum('pass','fail','inconclusive'),'reason':TEXT})
put('MetricObservation',{'name':TEXT,'value':ref('Amount'),'numerator':integer(),'denominator':integer(1)},('numerator','denominator'))
put('CategoryChange',{'category':TEXT,'baseline':ref('Amount'),'candidate':ref('Amount'),'within_limit':BOOL})
put('EvaluationReport',{'report_id':ID,'plan_id':ID,'plan_digest':DG,'candidate_id':ID,'candidate_digest':DG,'digest':DG,'evidence_class':enum('conformance','formal','exploratory'),'sealed':BOOL,'eligibility':enum('eligible','ineligible'),'run_refs':arr(ref('ObjectRef'),1),'metrics':arr(ref('MetricObservation')),'coverage_complete':BOOL,'gaps':arr(TEXT),'contract_gate':enum('pass','fail','inconclusive'),'target_attainment':ref('GateResult'),'statistical_gate':ref('GateResult'),'improvement_gate':ref('GateResult'),'paired_counts':obj({'a':integer(),'b':integer(),'c':integer(),'d':integer()}),'category_changes':arr(ref('CategoryChange')),'sealed_at':TM},('paired_counts','sealed_at'))
put('ReportStatus',{'report_id':ID,'plan_id':ID,'sealed':BOOL,'eligibility':enum('eligible','ineligible'),'preparation_gaps':arr(TEXT)})
put('FeedbackExposure',{'exposure_id':ID,'partition_id':ID,'source_group_ids':arr(ID,1,1000,True),'scope':enum('samples','answers','scores','full_report'),'recipient':ID,'occurred_at':nullable(TM),'recorded_at':TM,'evidence_refs':arr(ref('ContentRef')),'reason':TEXT,'report_id':ID,'report_digest':DG},('report_id','report_digest'))
put('ExposureRecordInput',{'exposure':ref('FeedbackExposure')})
put('ExposureRecordOutput',{'exposure':ref('FeedbackExposure'),'invalidated_plan_ids':arr(ID,0,100,True),'revocation_job_ids':arr(ID,0,100,True)})
put('FeedbackOpenInput',{'exposure_id':ID,'report_digest':DG,'scope':enum('scores','full_report')})
put('FeedbackOpenOutput',{'exposure':ref('FeedbackExposure'),'report':ref('EvaluationReport')})
put('ApprovalBatch',{'batch_id':ID,'targets':arr(ID,1,100,True),'observation_window_ms':integer(1,604800000),'minimum_samples':integer(1),'stop_rules':arr(ref('EvaluationMetric'),1)})
put('ReleaseApproval',{'approval_id':ID,'revision':REV,'release_kind':enum('compatibility','improvement'),'candidate_id':ID,'candidate_digest':DG,'lock_id':ID,'report_id':ID,'report_digest':DG,'targets':arr(ID,1,100,True),'approved_by':ID,'confirmation_ref':ref('ObjectRef'),'batches':arr(ref('ApprovalBatch'),1),'expires_at':TM,'max_offline_window_ms':integer(0,86400000),'rollback_lock':nullable(ID),'state':enum('active','revoked','expired'),'propagation':arr(ref('PropagationTarget'))})
put('EvaluationApproveInput',{'approval':ref('ReleaseApproval')});D['EvaluationApproveOutput']=ref('ReleaseApproval')
put('EvaluationRevokeInput',{'reason':TEXT});D['EvaluationRevokeOutput']=ref('ReleaseApproval')
put('RolloutTarget',{'target_id':ID,'activation_id':ID,'state':enum('pending','active','disabled','blocked'),'ready':BOOL,'observation_elapsed_ms':integer(),'samples_observed':integer(),'residual_work':arr(ID)})
put('RolloutRecord',{'rollout_id':ID,'approval_id':ID,'revision':REV,'state':enum('running','waiting','stopped','finished'),'current_batch':integer(),'targets':arr(ref('RolloutTarget'),1),'reason':TEXT},('reason',))
put('RolloutReadInput',{});D['RolloutReadOutput']=ref('RolloutRecord')
readmap={'candidate':'EvaluationCandidate','partition':'DatasetPartition','plan':'EvaluationPlan','run':'EvaluationRunRecord','report':'ReportStatus','approval':'ReleaseApproval'}
put('EvaluationReadInput',{'kind':enum(*readmap)})
D['EvaluationReadOutput']={'oneOf':[obj({'kind':{'const':k},'record':ref(v)}) for k,v in readmap.items()]}
for name,ins,outs,target,query,condi,succ in [
 ('candidate_register','CandidateRegisterInput','CandidateRegisterOutput','evaluation_service_id',False,False,'候选、内容谱系与原注册回执固定保存。'),
 ('partition_register','PartitionRegisterInput','PartitionRegisterOutput','evaluation_service_id',False,False,'受信分区与来源组登记；不授予内容使用权限。'),
 ('plan_create','PlanCreateInput','PlanCreateOutput','evaluation_service_id',False,False,'固定计划；正式确认同时占用保留分区和永久尝试序号。'),
 ('run','EvaluationRunInput','EvaluationRunOutput','payload.plan_id',False,False,'运行及首批环境准备责任已保存，不代表评分完成。'),
 ('cancel','EvaluationCancelInput','EvaluationCancelOutput','run_id',False,True,'取消意图、全部环境封闭及清理责任已保存。'),
 ('read','EvaluationReadInput','EvaluationReadOutput','object_id',True,False,'受控投影；未开放的报告不含成绩和逐例反馈。'),
 ('exposure_record','ExposureRecordInput','ExposureRecordOutput','evaluation_service_id',False,False,'暴露、受影响资格失效与撤回责任一起提交。'),
 ('feedback_open','FeedbackOpenInput','FeedbackOpenOutput','report_id',False,False,'先记录不可撤销暴露，再返回已封存报告。'),
 ('approve','EvaluationApproveInput','EvaluationApproveOutput','evaluation_service_id',False,False,'按发布目的核对证据，批准和逐目标发布责任共同提交。'),
 ('revoke','EvaluationRevokeInput','EvaluationRevokeOutput','approval_id',False,True,'批准撤回与传播任务持久；原批准不可复活。'),
 ('rollout_read','RolloutReadInput','RolloutReadOutput','rollout_id',True,False,'原批次、逐目标就绪、观察与残留事实。')]:
 meth('evaluation.'+name,ins,outs,'evaluation',target,query,condi,succ,('plan_conflict','holdout_unavailable','feedback_not_ready','source_unavailable','environment_unavailable','evidence_incomplete','approval_inactive','rollback_unavailable'))
(SCR/'governance-protocol-patch.json').write_text(json.dumps({'defs':D,'methods':M},ensure_ascii=False,indent=2)+'\n')
print(f'{len(D)} definitions; {len(M)} methods ({sum(k in oldm and oldm[k]["status"]=="reserved" for k in M)} newly frozen)')
