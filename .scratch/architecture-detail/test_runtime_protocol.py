import sys,json,copy
from pathlib import Path
sys.path.insert(0,str(Path('docs/architecture/validation').resolve()))
from protocol import schema
patch=json.loads(Path('.scratch/architecture-detail/runtime-protocol-patch.json').read_text())
schema.SCHEMA['$defs'].update(patch['defs']);schema.METHODS.update(patch['methods'])
for typ,kind in [('Command','command'),('Query','query')]:
 schema.SCHEMA['$defs'][typ]['properties']['method']['enum']=[n for n,s in schema.METHODS.items() if s['status']=='frozen-draft' and s['kind']==kind]
from protocol.traces import check_trace
from protocol.runtime_rules import check_exchange,check_trace_rules
from validate_protocol import mutate
root=Path('docs/architecture/contracts/examples/protocol')
def check(t):
 return check_trace(t)+[e for event in t['events'] if 'exchange'in event for e in check_exchange(event['exchange'],t['capabilities'])]+check_trace_rules(t)
fixtures={}
for path in root.glob('2[0-3]-*.json'):
 t=json.loads(path.read_text());fixtures[path.name]=t;errors=check(t)
 if errors:raise AssertionError(path.name+'\n'+'\n'.join(errors))
for case in json.loads(Path('.scratch/architecture-detail/runtime-mutations.json').read_text()):
 errors=check(mutate(copy.deepcopy(fixtures[case['fixture']]),case['edits']))
 if not any(case['expect']+':' in e for e in errors):raise AssertionError(case['name']+str(errors))
print('PASS: 4 new traces; 37 directed mutations; 14 methods; no runtime/service guarantee')
