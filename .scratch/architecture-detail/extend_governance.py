import json,copy
from pathlib import Path
P=Path('.scratch/architecture-detail/governance-protocol-patch.json');patch=json.loads(P.read_text());D=patch['defs'];M=patch['methods'];schema=json.loads(Path('docs/architecture/contracts/schemas/protocol.schema.json').read_text());base=schema['$defs'];registry=json.loads(Path('docs/architecture/contracts/schemas/methods.json').read_text())['methods']
def R(n):return {'$ref':'#/$defs/'+n}
ID=R('Id');REV=R('Revision');DG=R('Digest');TM={'type':'string','format':'date-time','pattern':'Z$'};TEXT={'type':'string','minLength':1,'maxLength':4096};BOOL={'type':'boolean'}
def enum(*x):return {'enum':list(x)}
def arr(t,minimum=0):return {'type':'array','items':t,'minItems':minimum,'maxItems':100}
def obj(p,optional=()):return {'type':'object','properties':p,'required':[k for k in p if k not in optional],'additionalProperties':False}
def put(n,p,optional=()):D[n]=obj(p,optional)
def method(name,inp,out,target,query=False,conditional=False,success=''):
 t=copy.deepcopy(M['grant.issue']);t.update(input=inp,output=out,target=target,kind='query' if query else 'command',expected_revision=conditional,stages=[] if query else ['applied','rejected'],success=success)
 for err in ('confirmation_required','confirmation_expired','confirmation_consumed','intent_conflict','use_unknown','settlement_conflict'):
  if err not in t['errors']:t['errors'].append(err)
  t['error_recovery'][err]=['after_change','query_original'] if err in ('intent_conflict','settlement_conflict') else ['after_change']
 M[name]=t
# Usage identity is fixed before admission, separate from immutable UseReceipt.
D['UseRequest']=copy.deepcopy(base['UseRequest']);D['UseRequest']['properties']['usage_owner_id']=ID
if 'usage_owner_id' not in D['UseRequest']['required']:D['UseRequest']['required'].append('usage_owner_id')
put('UseSettlementInput',{'operation_id':ID,'usage_owner_id':ID,'grant_refs':arr(R('ObjectRef'),1),'usage_revision':REV,'cumulative_units':R('Amount'),'cumulative_cost':R('Amount'),'final':BOOL,'closure_ref':R('ObjectRef')},('closure_ref',))
D['UseSettlementInput']['allOf']=[{'if':{'properties':{'final':{'const':True}}},'then':{'required':['closure_ref']}}]
put('UseSettlementRecord',{'use_id':ID,'owner_id':ID,'operation_id':ID,'usage_owner_id':ID,'grant_refs':arr(R('ObjectRef'),1),'revision':REV,'usage_revision':{'type':'integer','minimum':0,'maximum':9007199254740991},'state':enum('open','final'),'consumed_once':BOOL,'reserved_units':R('Amount'),'reserved_cost':R('Amount'),'spent_units':R('Amount'),'spent_cost':R('Amount'),'held_units':R('Amount'),'held_cost':R('Amount'),'released_units':R('Amount'),'released_cost':R('Amount'),'closure_ref':R('ObjectRef')},('closure_ref',))
D['UseSettlementRecord']['allOf']=[{'if':{'properties':{'state':{'const':'final'}}},'then':{'required':['closure_ref']}}]
put('UseSettlementQueryInput',{})
method('grant.use.settle','UseSettlementInput','UseSettlementRecord','use_id',conditional=True,success='原使用累计费用按差额入账；关闭有据才释放余量，单次资格不返还。')
method('grant.use.settlement','UseSettlementQueryInput','UseSettlementRecord','use_id',query=True,success='独立读取原使用结算修订，不改写原UseReceipt或续期。')
consumers=['grant.issue','evaluation.approve','task.accept_result','endpoint.pair.approve','grant.lease.allocate']
branches=[]
for name in consumers:
 spec=M.get(name,registry[name]);props={'command_id':ID,'method':{'const':name},'target_id':ID,'expires_at':TM,'payload':R(spec['input'])}
 if spec['expected_revision']:props['expected_revision']=REV
 branches.append(obj(props))
D['ConfirmationConsumerCommand']={'oneOf':branches}
put('ConfirmationRequestInput',{'confirmation_id':ID,'consumer_command':R('ConfirmationConsumerCommand'),'intent_hash':DG,'expires_at':TM})
put('ConfirmationRecord',{'confirmation_id':ID,'owner_id':ID,'revision':REV,'consumer_method':enum(*consumers),'consumer_command_id':ID,'consumer_target_id':ID,'consumer_command':R('ConfirmationConsumerCommand'),'intent_hash':DG,'challenge':DG,'expires_at':TM,'state':enum('pending','approved','denied','consumed','expired'),'decided_at':TM,'decided_by':ID,'trusted_user_session_ref':R('ObjectRef'),'consumed_by':ID,'consumed_at':TM},('decided_at','decided_by','trusted_user_session_ref','consumed_by','consumed_at'))
for state in ('approved','denied','consumed'):
 D['ConfirmationRecord'].setdefault('allOf',[]).append({'if':{'properties':{'state':{'const':state}}},'then':{'required':['decided_at','decided_by','trusted_user_session_ref']}})
D['ConfirmationRecord']['allOf'].append({'if':{'properties':{'state':{'const':'consumed'}}},'then':{'required':['consumed_by','consumed_at']}})
put('ConfirmationReadInput',{})
put('ConfirmationDecideInput',{'challenge':DG,'intent_hash':DG,'consumer_command_id':ID,'decision':enum('approve','deny')})
method('confirmation.request','ConfirmationRequestInput','ConfirmationRecord','consumer_owner_id',success='实际业务owner保存原命令和规范意图，创建待本人决定的有限挑战。')
method('confirmation.read','ConfirmationReadInput','ConfirmationRecord','confirmation_id',query=True,success='按当前本人或获准管理会话返回原规范意图和决定；不消费。')
method('confirmation.decide','ConfirmationDecideInput','ConfirmationRecord','confirmation_id',conditional=True,success='受信本人会话保存批准/拒绝及原回执；业务尚未执行，消费留在业务事务。')
D['AuthContext']=copy.deepcopy(base['AuthContext']);D['AuthContext']['properties'].update({'actor_kind':enum('user','endpoint','agent','job','maintainer'),'trusted_user_session_ref':R('ObjectRef')})
D['Scenario']=copy.deepcopy(base['Scenario']);D['Scenario']['properties']['confirmations']=arr(R('ConfirmationRecord'))
P.write_text(json.dumps(patch,ensure_ascii=False,indent=2)+'\n')
print(len(D),'definitions;',len(M),'method specs')
