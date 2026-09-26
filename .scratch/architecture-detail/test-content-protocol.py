import sys,json,copy
from pathlib import Path
sys.path.insert(0,str(Path('docs/architecture/validation').resolve()))
from protocol.schema import SCHEMA,METHODS,validate
from protocol.exchanges import validate_exchange
from protocol.traces import check_trace
from protocol.content_rules import check_exchange,check_trace_rules
from validate_protocol import mutate
p=json.load(open('.scratch/architecture-detail/content-protocol-patch.json'));SCHEMA['$defs'].update(p['defs']);METHODS.update(p['methods'])
for kind,name in [('command','Command'),('query','Query')]:SCHEMA['$defs'][name]['properties']['method']['enum']=[k for k,v in METHODS.items() if v['status']=='frozen-draft' and v['kind']==kind]
def check(t):
 errors=check_trace(t)
 for i,e in enumerate(t['events']):
  if 'exchange' in e:
   x=e['exchange'];n=x['request']['method'];spec=METHODS[n]
   if not validate(spec['input'],x['request']['payload']) and (not x['response'].get('output') or not validate(spec['output'],x['response']['output'])):
    errors += [f'event {i}: '+a for a in check_exchange(x,t['capabilities'])]
 if not validate('Scenario',t):errors+=check_trace_rules(t)
 return errors
fixtures={p.name:json.loads(p.read_text()) for p in Path('docs/architecture/contracts/examples/protocol').glob('4[0-9]-*.json')}
for fn,t in fixtures.items():
 es=check(t)
 if es:print(fn,*es,sep='\n');raise SystemExit(1)
cases=json.load(open('.scratch/architecture-detail/content-mutations.json'))
for c in cases:
 es=check(mutate(copy.deepcopy(fixtures[c['fixture']]),c['edits']))
 if not any(c['expect']+':' in e for e in es):print(c['name'],c['expect'],es);raise SystemExit(1)
print('PASS',len(fixtures),'positive traces;',len(cases),'targeted negative mutations')
