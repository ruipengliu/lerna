import sys,json,copy
from pathlib import Path
sys.path.insert(0,str(Path('docs/architecture/validation').resolve()))
import protocol.schema as s
patch=json.loads(Path('.scratch/architecture-detail/governance-protocol-patch.json').read_text());s.SCHEMA['$defs'].update(patch['defs']);s.METHODS.update(patch['methods']);s.SCHEMA['$defs']['AuthContext']['properties'].update(json.loads(Path('.scratch/architecture-detail/governance-authcontext-properties.json').read_text()))
for kind,name in [('command','Command'),('query','Query')]:
 s.SCHEMA['$defs'][name]['properties']['method']['enum']=[k for k,v in s.METHODS.items() if v['status']=='frozen-draft' and v['kind']==kind]
import protocol.exchanges as e
source=Path('docs/architecture/validation/protocol/exchanges.py').read_text()
if "    if name=='extensions.read':" in source:
 start=source.index("    if name=='extensions.read':");end=source.index("    if name.startswith('execution.')",start);source=source[:start]+source[end:]
exec(compile(source,'<merged-exchanges>','exec'),e.__dict__)
from protocol.governance_rules import check_exchange,check_trace_rules
original=e.validate_exchange
def wrapped(x,c):
 errors=original(x,c)
 return errors+(check_exchange(x,c) if not any(z.startswith('schema:') for z in errors) else [])
e.validate_exchange=wrapped
import protocol.traces as t
source=Path('docs/architecture/validation/protocol/traces.py').read_text()
if "        if name=='extensions.read' and output['phase']=='active':" in source:
 start=source.index("        if name=='extensions.read' and output['phase']=='active':");end=source.index("        if name in ('grant.use','grant.use.get'):",start);source=source[:start]+source[end:]
source=source.replace("any(p[k]!=approval[k] for k in ('target_id','lock_id','instance_id'))", "any(p[k]!=approval[k] for k in ('target_id','lock_id')) or p['instance_id'] not in approval.get('eligible_instance_ids',[approval['instance_id']])")
exec(compile(source,'<merged-traces>','exec'),t.__dict__)
def check(trace):
 errors=t.check_trace(trace)
 return errors+(check_trace_rules(trace) if not any('schema:' in z for z in errors) else [])
from validate_protocol import mutate
fixtures={p.name:json.loads(p.read_text()) for p in Path('docs/architecture/contracts/examples/protocol').glob('[35][0-9]-*.json')}
for path in Path('.scratch/architecture-detail/governance-migrated-fixtures').glob('*.json'):
 fixtures[path.name]=json.loads(path.read_text())
for path in Path('.scratch/architecture-detail/governance-v2-migrated-fixtures').glob('*.json'):
 fixtures[path.name]=json.loads(path.read_text())
for name,trace in fixtures.items():
 errors=check(trace)
 if errors:raise AssertionError(name+':\n'+'\n'.join(errors))
mutations=json.loads(Path('.scratch/architecture-detail/governance-mutations.json').read_text())
for case in mutations:
 errors=check(mutate(copy.deepcopy(fixtures[case['fixture']]),case['edits']))
 if not any(case['expect']+':' in error for error in errors):raise AssertionError(case['name']+': expected '+case['expect']+'; got '+repr(errors))
print('PASS:',len(fixtures),'governance traces;',len(mutations),'targeted mutations; merged draft Schema/method registry in memory only')
