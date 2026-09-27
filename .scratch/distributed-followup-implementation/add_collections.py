import copy,json
from pathlib import Path
base=Path('docs/architecture/contracts');sp=base/'schemas/protocol.schema.json';s=json.loads(sp.read_text());d=s['$defs'];mp=base/'schemas/methods.json';m=json.loads(mp.read_text())
def ref(n):return {'$ref':'#/$defs/'+n}
def obj(props,required=None):return {'type':'object','properties':props,'required':required or list(props),'additionalProperties':False}
def arr(items,maxi=100):return {'type':'array','items':items,'minItems':0,'maxItems':maxi}
def string():return {'type':'string','minLength':1,'maxLength':4096}
def time():return {'type':'string','format':'date-time','pattern':'Z$'}
d['OwnerCollectionListInput']=obj({'query_id':ref('Id'),'limit':{'type':'integer','minimum':1,'maximum':100},'cursor':string()},['query_id','limit'])
for method,prefix,item,module in [('execution.list','Execution','Operation','execution'),('extensions.list','Extensions','Activation','extensions'),('grant.list','Grant','GrantRecord','security')]:
 d[prefix+'ListInput']=ref('OwnerCollectionListInput')
 props=copy.deepcopy(d['MemoryListOutput']['properties']);props['items']=arr(ref(item));props['snapshot_at']=time();props['expires_at']=time()
 d[prefix+'ListOutput']=obj(props,[k for k in props if k!='next_cursor'])
 spec=copy.deepcopy(m['methods']['memory.list']);spec.update(input=prefix+'ListInput',output=prefix+'ListOutput',source=module+'/README.md',success='冻结本 owner 当前获准成员 ID，有界分页返回当前可披露记录；权限范围变化使原分页失效，截断明确 partial/gaps。')
 m['methods'][method]=spec
 d['Query']['properties']['method']['enum'].append(method)
d['Query']['properties']['method']['enum'].sort();m['methods']=dict(sorted(m['methods'].items()))
d['OwnerCollectionFixture']=obj({'member_ids':dict(arr(ref('Id'),10000),uniqueItems=True),'readable_ids':dict(arr(ref('Id'),10000),uniqueItems=True),'authorization_scope':string(),'scan_end':{'type':'integer','minimum':0,'maximum':10000},'truncated':{'type':'boolean'}})
d['OwnerCollectionFixture']['description']='Test-only authority membership, current disclosure and scan observations; never accepted from a wire caller.'
d['Scenario']['properties']['events']['items']['properties']['collection']=ref('OwnerCollectionFixture')
sp.write_text(json.dumps(s,ensure_ascii=False,indent=2)+'\n');mp.write_text(json.dumps(m,ensure_ascii=False,indent=2)+'\n')
# Distinct owner sets exercise two-page traversal, current records, partial capacity and scope invalidation.
p=base/'examples/protocol';events=[]
def fixture_exchange(name,method):
 for e in json.loads((p/name).read_text())['events']:
  x=e.get('exchange',{})
  if x.get('request',{}).get('method')==method and 'output' in x['response']:return copy.deepcopy(x)
def idof(prefix,i):return prefix+'_'+format(i,'032x')
def event(x,method,q,items,members,scan_end,*,cursor=None,next_cursor=None,truncated=False,scope='scope-a',readable=None,at='2026-09-26T00:00:02Z',error=None):
 payload={'query_id':q,'limit':1}
 if cursor:payload['cursor']=cursor
 out={'query_id':q,'owner_id':x['auth']['logical_service_id'],'items':copy.deepcopy(items),'exhausted':next_cursor is None,'partial':truncated,'gaps':['membership_limit'] if truncated else [],'snapshot_at':'2026-09-26T00:00:01Z','expires_at':'2026-09-26T00:10:01Z'}
 if next_cursor:out['next_cursor']=next_cursor
 res={'error':{'code':error,'message':'Original collection cannot continue; restart within the current authorized scope.','retry':'after_change'}} if error else {'output':out,'observed_at':at}
 e={'at':at,'exchange':{'auth':copy.deepcopy(x['auth']),'request':{'method':method,'target_id':x['auth']['logical_service_id'],'payload':payload},'response':res},'collection':{'member_ids':members,'readable_ids':members if readable is None else readable,'authorization_scope':scope,'scan_end':scan_end,'truncated':truncated}}
 events.append(e);return e
samples=[('01-task-loop.json','execution.get','execution.list','operation_id'),('13-activation-online.json','extensions.read','extensions.list','activation_id'),('30-grant-lifecycle.json','grant.read','grant.list','grant_id')]
for i,(file,read,method,idkey) in enumerate(samples):
 x=fixture_exchange(file,read);a=copy.deepcopy(x['response']['output']);b=copy.deepcopy(a)
 a[idkey]=idof(idkey[:-3],100+i*10);b[idkey]=idof(idkey[:-3],101+i*10)
 if idkey=='activation_id':
  for r in (a,b):
   for key in ('startup_evidence','instance_readiness','approval_revision','activation_use_id'):r.pop(key,None)
   r.update(phase='prepared',ready_instance=None)
 members=[a[idkey],b[idkey]];q=idof('query',100+i)
 event(x,method,q,[a],members,1,next_cursor='page-'+str(i)+'-1')
 if idkey in ('operation_id','grant_id'):b['revision']+=1
 else:b['last_observed_at']='2026-09-26T00:00:03Z'
 event(x,method,q,[b],members,2,cursor='page-'+str(i)+'-1',at='2026-09-26T00:00:03Z')
 # Retry first page retains membership/time; current values need not be frozen.
 if i==0:
  a['revision']+=1
  event(x,method,q,[a],members,1,next_cursor='page-'+str(i)+'-1',at='2026-09-26T00:00:04Z')
  event(x,method,q,[],members,1,cursor='page-'+str(i)+'-1',scope='scope-b',readable=[b[idkey]],error='query_conflict',at='2026-09-26T00:00:05Z')
  event(x,method,idof('query',200),[b],[b[idkey]],1,scope='scope-b',at='2026-09-26T00:00:06Z')
  event(x,method,idof('query',201),[a],[a[idkey]],1,truncated=True,at='2026-09-26T00:00:07Z')
  event(x,method,q,[],members,1,cursor='page-'+str(i)+'-1',error='cursor_expired',at='2026-09-26T00:11:02Z')
trace={'name':'owner-collection-pages','description':'Recorded authority premises exercise owner-scoped Operation, Activation and Grant enumeration, fixed membership with current values, cursor continuation, authorization restart, capacity partial and expiry. This is not a database or authorization integration test.','capabilities':[],'input_requests':[],'approvals':[],'events':events}
(p/'56-owner-collection-pages.json').write_text(json.dumps(trace,ensure_ascii=False,indent=2)+'\n')
