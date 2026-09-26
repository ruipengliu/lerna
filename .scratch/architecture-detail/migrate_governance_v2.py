"""Prepare old fixture copies with authoritative confirmations and usage-owner bindings."""
import sys,json,copy,hashlib
from datetime import datetime,timedelta
from pathlib import Path
sys.path.insert(0,str(Path('docs/architecture/validation').resolve()))
from protocol.governance_rules import confirmation_ref,confirmation_intent_hash,_CONFIRMATION_METHODS
src=Path('docs/architecture/contracts/examples/protocol');dst=Path('.scratch/architecture-detail/governance-v2-migrated-fixtures');dst.mkdir(exist_ok=True)
def ident(kind,value):return kind+'_'+hashlib.sha256(value.encode()).hexdigest()[:32]
def walk(v):
 if isinstance(v,dict):
  yield v
  for x in v.values():yield from walk(x)
 elif isinstance(v,list):
  for x in v:yield from walk(x)
files=[]
for path in sorted(src.glob('[0-4][0-9]-*.json')):
 data=json.loads(path.read_text());changed=False;consumers={}
 # Use original declaration consistently in check/use requests, preserved receipts untouched.
 for e in data['events']:
  if 'exchange' not in e:continue
  x=e['exchange'];q=x['request'];p=q['payload']
  if q['method'] in ('grant.check','grant.use'):
   p.setdefault('usage_owner_id',x['auth'].get('sender_service_id',ident('usageowner',data['name'])))
   if q['method']=='grant.use':x['auth']['sender_service_id']=p['usage_owner_id']
   changed=True
  if q['method'] in _CONFIRMATION_METHODS and x['response'].get('stage')!='rejected' and 'output' in x['response']:
   cref=confirmation_ref(q);consumers.setdefault(cref['id'],(q,x['auth'],e['at']));changed=True
 for node in walk(data):
  if 'confirmation_ref' in node and isinstance(node['confirmation_ref'],dict) and node['confirmation_ref']['id'] in consumers:
   q,a,at=consumers[node['confirmation_ref']['id']];node['confirmation_ref'].update(owner_id=a['logical_service_id'],revision=2)
 # Grant's own normalized intent field is derived, hence excluded from intent hashing.
 for q,a,at in consumers.values():
  if q['method']=='grant.issue':
   old=q['payload']['intent_hash'];new=confirmation_intent_hash(a['logical_service_id'],q)
   for node in walk(data):
    if node.get('intent_hash')==old:node['intent_hash']=new
 if consumers:
  data['confirmations']=[]
  for cid,(q,a,at) in consumers.items():
   expires=min(datetime.fromisoformat(q['expires_at'].replace('Z','+00:00')),datetime.fromisoformat(at.replace('Z','+00:00'))+timedelta(minutes=5)).isoformat().replace('+00:00','Z')
   data['confirmations'].append({'confirmation_id':cid,'owner_id':a['logical_service_id'],'revision':2,'consumer_method':q['method'],'consumer_command_id':q['command_id'],'consumer_target_id':q['target_id'],'consumer_command':copy.deepcopy(q),'intent_hash':confirmation_intent_hash(a['logical_service_id'],q),'challenge':'sha256:'+hashlib.sha256(cid.encode()).hexdigest(),'expires_at':expires,'state':'approved','decided_at':at,'decided_by':a['actor_id'],'trusted_user_session_ref':{'owner_id':a['logical_service_id'],'id':ident('session',cid),'revision':1}})
 if changed:
  (dst/path.name).write_text(json.dumps(data,ensure_ascii=False,indent=2)+'\n');files.append(path.name)
Path('.scratch/architecture-detail/governance-v2-migration-files.json').write_text(json.dumps(files,indent=2)+'\n')
print('prepared',len(files),'migrated fixture copies')
