"""Assemble independently reviewed edits into the canonical draft assets."""
from pathlib import Path
import json, copy, shutil

work=Path('.scratch/architecture-detail')
root=Path('docs/architecture/contracts')
schema=json.loads((work/'baseline/protocol.schema.json').read_text())
registry=json.loads((work/'baseline/methods.json').read_text())
cases=json.loads((work/'baseline/invalid-mutations.json').read_text())
claimed={}
for group in ('runtime','governance','content'):
    path=work/(group+'-protocol-patch.json')
    if not path.exists():
        print('Not yet available:',group)
        continue
    patch=json.loads(path.read_text())
    for name,value in patch['defs'].items():
        if name in claimed and claimed[name][1]!=value:
            raise AssertionError(f'definition conflict: {name}: {claimed[name][0]} / {group}')
        claimed[name]=(group,value)
        schema['$defs'][name]=copy.deepcopy(value)
    registry['methods'].update(patch['methods'])
    mutation=work/(group+'-mutations.json')
    if mutation.exists():cases.extend(json.loads(mutation.read_text()))
auth_extra=work/'governance-authcontext-properties.json'
if auth_extra.exists():
    schema['$defs']['AuthContext']['properties'].update(json.loads(auth_extra.read_text()))
schema['$id']='https://harness.invalid/schema/protocol-draft-2'
registry['profile']='full-harness-draft-2'
for kind,name in [('command','Command'),('query','Query')]:
    schema['$defs'][name]['properties']['method']['enum']=sorted(n for n,s in registry['methods'].items() if s['status']=='frozen-draft' and s['kind']==kind)
def safe_integers(v):
    if isinstance(v,dict):
        if v.get('type')=='integer':v['maximum']=min(v.get('maximum',9007199254740991),9007199254740991)
        for x in v.values():safe_integers(x)
    elif isinstance(v,list):
        for x in v:safe_integers(x)
safe_integers(schema)
for name,value in [('protocol.schema.json',schema),('methods.json',registry)]:
    (root/'schemas'/name).write_text(json.dumps(value,ensure_ascii=False,indent=2)+'\n')
assert len({x['name'] for x in cases})==len(cases),'duplicate mutation names'
for case in cases:
    if case['name']=='reserved method is not callable':
        case['name']='query method cannot use command envelope'
(root/'examples/protocol/invalid-mutations.json').write_text(json.dumps(cases,ensure_ascii=False,indent=2)+'\n')
for dirname in ('governance-migrated-fixtures', 'governance-v2-migrated-fixtures'):
    migration=work/dirname
    if migration.exists():
        for source in migration.glob('*.json'):shutil.copy2(source,root/'examples/protocol'/source.name)
print('Canonical asset:',len(schema['$defs']),'definitions;',sum(s['status']=='frozen-draft' for s in registry['methods'].values()),'methods;',len(cases),'mutations')
