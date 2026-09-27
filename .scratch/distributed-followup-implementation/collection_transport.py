import json,copy,subprocess
from pathlib import Path
base=Path('docs/architecture/contracts');p=base/'schemas/methods.json';m=json.loads(p.read_text());old=json.loads(subprocess.check_output(['git','show','HEAD:'+str(p)]));m['methods']={k:m['methods'][k] for k in list(old['methods'])+[k for k in m['methods'] if k not in old['methods']]};p.write_text(json.dumps(m,ensure_ascii=False,indent=2)+'\n')
p=base/'schemas/transport.schema.json';s=json.loads(p.read_text())
def walk(v):
 if isinstance(v,dict):
  if isinstance(v.get('enum'),list) and 'backend_rebind' in v['enum']:v['enum'].append('authorization_changed')
  for x in v.values():walk(x)
 elif isinstance(v,list):
  for x in v:walk(x)
walk(s);p.write_text(json.dumps(s,ensure_ascii=False,indent=2)+'\n')
p=base/'examples/transport/vectors.json';v=json.loads(p.read_text());by={x['name']:x for x in v}
x=copy.deepcopy(by['wss-gap-cursor_expired']);x['name']='wss-gap-authorization-changed';x['value']['reason']='authorization_changed';x['context']['subscription']['authorization_changed']=True;v.append(x)
t=json.loads((base/'examples/protocol/56-owner-collection-pages.json').read_text())
for method in ['execution.list','extensions.list','grant.list']:
 e=next(e['exchange'] for e in t['events'] if e['exchange']['request']['method']==method)
 x=copy.deepcopy(by['wss-query-request']);x['name']='wss-'+method+'-request';x['value']['request']=copy.deepcopy(e['request']);v.append(x)
 y=copy.deepcopy(by['wss-query-response']);y['name']='wss-'+method+'-response';y['value']['result']=copy.deepcopy(e['response']);y['context']['request_frame']=copy.deepcopy(x['value']);v.append(y)
x=copy.deepcopy(by['wss-change']);x['name']='wss-activation-change';x['value']['change'].update(object_type='activation',object_id=t['events'][-1]['activation_view']['activation_id'],revision=2);x['context']['subscription']['object_types']=['activation'];v.append(x)
p.write_text(json.dumps(v,ensure_ascii=False,indent=2)+'\n')
p=base/'examples/transport/invalid-mutations.json';mut=json.loads(p.read_text())
def add(name,vec,path,val,expect):mut.append({'name':name,'vector':vec,'edits':[{'op':'set','path':path,'value':val}],'expect':expect})
add('authorization-changed subscription cannot emit Change','wss-change','/context/subscription/authorization_changed',True,'frame_subscription')
add('authorization-changed gap retains explicit reason','wss-gap-authorization-changed','/value/reason','queue_overflow','frame_subscription')
for method in ['execution.list','extensions.list','grant.list']:
 add(method+' rejects owner rebound','wss-'+method+'-response','/value/result/output/owner_id','owner_'+('f'*32),'collection_result')
 add(method+' rejects query rebound','wss-'+method+'-response','/value/result/output/query_id','query_'+('f'*32),'collection_result')
 add(method+' rejects unbounded page','wss-'+method+'-request','/value/request/payload/limit',101,'transport_schema')
p.write_text(json.dumps(mut,ensure_ascii=False,indent=2)+'\n')
