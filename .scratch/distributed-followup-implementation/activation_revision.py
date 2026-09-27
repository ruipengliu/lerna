import json,copy
from pathlib import Path
base=Path('docs/architecture/contracts');p=base/'schemas/protocol.schema.json';s=json.loads(p.read_text());d=s['$defs'];d['Activation']['properties']['revision']={'$ref':'#/$defs/Revision'};d['Activation']['required'].append('revision')
d['Scenario']['properties']['events']['items']['properties']['activation_view']={'description':'Test-only client highest-revision activation projection; never a wire field.','type':'object','properties':{'activation_id':{'$ref':'#/$defs/Id'},'revision':{'$ref':'#/$defs/Revision'},'phase':copy.deepcopy(d['Activation']['properties']['phase'])},'required':['activation_id','revision','phase'],'additionalProperties':False}
p.write_text(json.dumps(s,ensure_ascii=False,indent=2)+'\n')
for p in sorted((base/'examples/protocol').glob('[0-9][0-9]-*.json')):
 t=json.loads(p.read_text());seen={};changed=False
 def walk(v):
  global changed
  if isinstance(v,dict):
   if {'activation_id','phase','generation','last_observed_at','residual_work'}<=v.keys():
    key=v['activation_id'];record=json.dumps({k:x for k,x in v.items() if k!='revision'},sort_keys=True);versions=seen.setdefault(key,{})
    if record not in versions:versions[record]=len(versions)+1
    v['revision']=versions[record];changed=True
   for c in v.values():walk(c)
  elif isinstance(v,list):
   for c in v:walk(c)
 walk(t)
 if changed:p.write_text(json.dumps(t,ensure_ascii=False,indent=2)+'\n')
# New fixture: reread updated activation then a delayed old read, preserving the newest client view.
p=base/'examples/protocol/56-owner-collection-pages.json';t=json.loads(p.read_text());source=next(e for e in t['events'] if e['exchange']['request']['method']=='extensions.list');r=copy.deepcopy(source['exchange']['response']['output']['items'][0]);auth=copy.deepcopy(source['exchange']['auth'])
for revision,phase in [(2,'blocked'),(1,'prepared')]:
 record=copy.deepcopy(r);record.update(revision=revision,phase=phase)
 t['events'].append({'at':'2026-09-26T00:00:09Z','exchange':{'auth':auth,'request':{'method':'extensions.read','target_id':r['activation_id'],'payload':{'kind':'activation'}},'response':{'output':record,'observed_at':'2026-09-26T00:00:08Z','resource_revision':revision}},'activation_view':{'activation_id':r['activation_id'],'revision':2,'phase':'blocked'}})
p.write_text(json.dumps(t,ensure_ascii=False,indent=2)+'\n')
