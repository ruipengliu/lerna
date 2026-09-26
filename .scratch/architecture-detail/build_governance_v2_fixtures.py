import sys,json,copy,hashlib
from pathlib import Path
sys.path.insert(0,str(Path('docs/architecture/validation').resolve()))
from protocol.governance_rules import confirmation_ref,confirmation_intent_hash
ROOT=Path('docs/architecture/contracts/examples/protocol');SCR=Path('.scratch/architecture-detail');patch=json.loads((SCR/'governance-protocol-patch.json').read_text());methods=json.loads(Path('docs/architecture/contracts/schemas/methods.json').read_text())['methods'];methods.update(patch['methods']);newcases=[]
def I(s):return s.split('-')[0]+'_'+hashlib.sha256(s.encode()).hexdigest()[:32]
def H(s):return 'sha256:'+hashlib.sha256(s.encode()).hexdigest()
def A(x,u='calls'):return {'unit':u,'amount':str(x)}
def time(i):return f'2026-09-26T00:{i//60:02}:{i%60:02}Z'
def load(name):return json.loads((SCR/'governance-v2-migrated-fixtures'/name).read_text())
def blank(name):return {'name':name,'description':name+'; constructed records, no live authentication or durable commit proof','capabilities':[],'input_requests':[],'approvals':[],'confirmations':[],'events':[]}
def add(data,auth,name,target,payload,out,expected=None):
 idx=len(data['events']);spec=methods[name];q={'method':name,'target_id':target,'payload':copy.deepcopy(payload)}
 if spec['kind']=='command':
  q.update(command_id=I('command-'+data['name']+str(idx)),expires_at='2026-09-26T00:10:00Z')
  if spec['expected_revision']:q['expected_revision']=expected or 1
  r={'command_id':q['command_id'],'stage':'applied','decided_at':time(idx),'output':copy.deepcopy(out)}
 else:r={'output':copy.deepcopy(out),'observed_at':time(idx)}
 data['events'].append({'at':time(idx),'exchange':{'auth':copy.deepcopy(auth),'request':q,'response':r}});return idx

def case(file,name,idx,path,value,expect):newcases.append({'name':name,'fixture':file,'expect':expect,'edits':[{'op':'set','path':f'/events/{idx}/exchange/'+path,'value':value}]})
def save(file,data):(ROOT/file).write_text(json.dumps(data,ensure_ascii=False,indent=2)+'\n')
def authoritative_confirmation(ex):
 q=ex['request'];a=ex['auth'];ref=confirmation_ref(q)
 if q['method']=='grant.issue':
  q['payload']['intent_hash']=confirmation_intent_hash(a['logical_service_id'],q);ex['response']['output']['intent_hash']=q['payload']['intent_hash']
 return {'confirmation_id':ref['id'],'owner_id':a['logical_service_id'],'revision':2,'consumer_method':q['method'],'consumer_command_id':q['command_id'],'consumer_target_id':q['target_id'],'consumer_command':copy.deepcopy(q),'intent_hash':confirmation_intent_hash(a['logical_service_id'],q),'challenge':H(ref['id']),'expires_at':'2026-09-26T00:05:00Z','state':'approved','decided_at':time(0),'decided_by':a['actor_id'],'trusted_user_session_ref':{'owner_id':a['logical_service_id'],'id':I('session-'+ref['id']),'revision':1}}

for once,num in [(False,50),(True,51)]:
 file=f'{num}-online-use-'+('once-zero' if once else 'settlement')+'.json';data=blank(file[:-5]);old=load('30-grant-lifecycle.json');first=copy.deepcopy(old['events'][0]);g=first['exchange']['response']['output'];q=first['exchange']['request'];g['policy']['mode']=q['payload']['policy']['mode']='once' if once else 'continuous';data['events']=[first];data['confirmations']=[authoritative_confirmation(first['exchange'])]
 auth=copy.deepcopy(first['exchange']['auth']);owner=auth['logical_service_id'];usage=I('usageowner-'+str(num));auth['sender_service_id']=usage;op=I('operation-'+str(num));use=I('use-'+str(num));refs=[{'owner_id':owner,'id':g['grant_id'],'revision':1}];grantpolicy=g['policy'];reserved=1 if once else 3
 p={'use_id':use,'operation_id':op,'usage_owner_id':usage,'intent_hash':H('operation-intent-'+str(num)),'grant_refs':refs,'source_refs':[],'subject':grantpolicy['subject'],'resource_scopes':grantpolicy['resources'],'action':'read','purpose':'research','recipient':grantpolicy['recipients'][0],'location':grantpolicy['locations'][0],'max_units':A(reserved),'max_cost':A(4,'USD')}
 receipt={'use_id':use,'owner_id':owner,'intent_hash':p['intent_hash'],'grant_revisions':refs,'decision':'allowed','reserved_units':A(reserved),'reserved_cost':A(4,'USD'),'start_before':'2026-09-26T00:00:30Z','decided_at':time(1)}
 add(data,auth,'grant.use',owner,p,receipt)
 state={'use_id':use,'owner_id':owner,'operation_id':op,'usage_owner_id':usage,'grant_refs':refs,'revision':1,'usage_revision':0,'state':'open','consumed_once':once,'reserved_units':A(reserved),'reserved_cost':A(4,'USD'),'spent_units':A(0),'spent_cost':A(0,'USD'),'held_units':A(reserved),'held_cost':A(4,'USD'),'released_units':A(0),'released_cost':A(0,'USD')}
 readidx=add(data,auth,'grant.use.settlement',use,{},state)
 if not once:
  psettle={'operation_id':op,'usage_owner_id':usage,'grant_refs':refs,'usage_revision':1,'cumulative_units':A(1),'cumulative_cost':A(1,'USD'),'final':False}
  state.update(revision=2,usage_revision=1,spent_units=A(1),spent_cost=A(1,'USD'),held_units=A(2),held_cost=A(3,'USD'));partial=add(data,auth,'grant.use.settle',use,psettle,state)
  case(file,'unknown open use retains all unspent reservation',partial,'response/output/released_cost/amount','1','use_settlement_unknown')
 spent=0 if once else 2;cost=0 if once else 2
 final={'operation_id':op,'usage_owner_id':usage,'grant_refs':refs,'usage_revision':1 if once else 2,'cumulative_units':A(spent),'cumulative_cost':A(cost,'USD'),'final':True,'closure_ref':{'owner_id':usage,'id':I('closure-'+str(num)),'revision':1}}
 prior=state['revision'];state.update(revision=prior+1,usage_revision=final['usage_revision'],state='final',spent_units=A(spent),spent_cost=A(cost,'USD'),held_units=A(0),held_cost=A(0,'USD'),released_units=A(reserved-spent),released_cost=A(4-cost,'USD'),closure_ref=final['closure_ref']);settleidx=add(data,auth,'grant.use.settle',use,final,state,prior)
 # Repeated original command is exact recovery and must not double charge.
 data['events'].append(copy.deepcopy(data['events'][settleidx]));data['events'][-1]['at']=time(len(data['events'])-1)
 add(data,auth,'grant.use.settlement',use,{},state);add(data,auth,'grant.use.get',use,{},receipt)
 if once:case(file,'final zero never refunds once identity',settleidx,'response/output/consumed_once',False,'once_not_refunded')
 else:
  case(file,'grant.use.settle rejects unknown field',settleidx,'request/payload/unknown',True,'schema');case(file,'grant.use.settlement rejects unknown field',readidx,'request/payload/unknown',True,'schema');case(file,'settlement requires authenticated usage owner',settleidx,'auth/sender_service_id',I('usageowner-other'),'usage_owner');case(file,'settlement preserves original operation',settleidx,'response/output/operation_id',I('operation-other'),'use_settlement_binding');case(file,'settlement preserves exact accounting unit',settleidx,'response/output/spent_cost/unit','EUR','use_settlement_unit')
 save(file,data)

# Actual confirmation flows reuse genuine consumer schemas and keep the consumer command fixed.
def flow(file,base,consumer_index):
 data=blank(file[:-5]);data['capabilities']=base['capabilities'];data['input_requests']=base['input_requests'];data['approvals']=base['approvals'];data['events']=copy.deepcopy(base['events'][:consumer_index]);ex=copy.deepcopy(base['events'][consumer_index]['exchange']);q=ex['request'];a=ex['auth'];ref=confirmation_ref(q);pending=authoritative_confirmation(ex);pending.update(revision=1,state='pending')
 for k in ('decided_at','decided_by','trusted_user_session_ref'):pending.pop(k,None)
 owner=a['logical_service_id'];ci=add(data,a,'confirmation.request',owner,{'confirmation_id':ref['id'],'consumer_command':q,'intent_hash':pending['intent_hash'],'expires_at':pending['expires_at']},pending)
 ri=add(data,a,'confirmation.read',ref['id'],{},pending)
 trusted=copy.deepcopy(a);trusted.update(actor_kind='user',trusted_user_session_ref={'owner_id':owner,'id':I('session-'+file),'revision':1});approved=copy.deepcopy(pending);approved.update(revision=2,state='approved',decided_at=time(len(data['events'])),decided_by=a['actor_id'],trusted_user_session_ref=trusted['trusted_user_session_ref'])
 di=add(data,trusted,'confirmation.decide',ref['id'],{'challenge':pending['challenge'],'intent_hash':pending['intent_hash'],'consumer_command_id':q['command_id'],'decision':'approve'},approved)
 add(data,a,'confirmation.read',ref['id'],{},approved)
 bi=len(data['events']);ex['response']['decided_at']=time(bi);data['events'].append({'at':time(bi),'exchange':ex})
 consumed=copy.deepcopy(approved);consumed.update(revision=3,state='consumed',consumed_by=q['command_id'],consumed_at=time(bi));last=add(data,a,'confirmation.read',ref['id'],{},consumed)
 case(file,'confirmation request rejects arbitrary intent payload '+str(ci),ci,'request/payload/consumer_command/payload/extra',True,'schema')
 case(file,'confirmation caller cannot switch consumer owner '+str(ci),ci,'request/target_id',I('owner-other'),'confirmation_owner')
 case(file,'model cannot certify trusted user approval '+str(di),di,'auth/actor_kind','agent','confirmation_actor')
 case(file,'confirmation cannot change original consumer command '+str(di),di,'request/payload/consumer_command_id',I('command-other'),'confirmation_decision')
 case(file,'business consumption must be visible at owner '+str(last),last,'response/output/state','approved','confirmation_observation')
 if file.startswith('52'):
  for name,idx in [('confirmation.request',ci),('confirmation.read',ri),('confirmation.decide',di)]:case(file,name+' rejects unknown payload',idx,'request/payload/extra',True,'schema')
  case(file,'confirmation hash must bind exact normalized intent',ci,'request/payload/intent_hash',H('bad'),'confirmation_intent')
 if file.startswith('54'):case(file,'accepted result cannot switch confirmed goal',bi,'request/payload/goal_revision',2,'confirmation_consumer')
 save(file,data)

flow('52-confirmation-grant.json',load('30-grant-lifecycle.json'),0)
f=load('34-compatibility-release.json');flow('53-confirmation-release.json',f,next(i for i,e in enumerate(f['events']) if e['exchange']['request']['method']=='evaluation.approve'))
flow('54-confirmation-acceptance.json',load('08-acceptance.json'),0)
p=SCR/'governance-mutations.json';cases=json.loads(p.read_text());cases=[c for c in cases if not c['fixture'].startswith(('50','51','52','53','54'))];cases.extend(newcases);p.write_text(json.dumps(cases,ensure_ascii=False,indent=2)+'\n');print('5 new fixtures;',len(newcases),'new mutations;',len(cases),'governance mutations total')
