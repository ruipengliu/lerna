#!/usr/bin/env python3
"""Design-asset checks. Does not exercise a Harness implementation."""
import copy, json, pathlib, re, sys, urllib.parse
from decimal import Decimal
try:
    import jsonschema
except ImportError:
    raise SystemExit('Missing existing test dependency: jsonschema. No dependency is installed by this script.')
ROOT = pathlib.Path(__file__).resolve().parents[1]
errors=[]
def require(ok, message):
    if not ok: errors.append(message)
def anchors(path):
    text=path.read_text(); result=set(re.findall(r'<a\s+id="([^"]+)"',text)); counts={}
    for line in text.splitlines():
        if re.match(r'^#{1,6} ',line):
            s=re.sub(r'^#+\s+','',line).strip().lower();s=re.sub(r'[^\w\-\s]','',s,flags=re.U).replace(' ','-')
            n=counts.get(s,0);counts[s]=n+1;result.add(s+(f'-{n}' if n else ''))
    return result
md=list(ROOT.rglob('*.md')); md=[p for p in md if '.draft' not in p.parts]
links=0; diagrams=0
for p in md:
    body=p.read_text()
    require('.draft' not in body and 'architecture-v2' not in body,f'{p}: forbidden historical dependency')
    fences=re.findall(r'^```.*$',body,re.M); require(len(fences)%2==0,f'{p}: unclosed code fence')
    diagrams+=body.count('```mermaid')
    for url in re.findall(r'\[[^\]]*\]\(([^\s)]+)(?:\s+"[^"]*")?\)',body):
        x=urllib.parse.urlsplit(url)
        if x.scheme: continue
        if not x.path and not x.fragment:continue
        dest=(p.parent/urllib.parse.unquote(x.path)).resolve() if x.path else p
        links+=1;require(dest.exists(),f'{p}: missing {url}')
        if dest.exists() and x.fragment and dest.suffix=='.md':
            require(urllib.parse.unquote(x.fragment) in anchors(dest),f'{p}: missing anchor {url}')
schema=json.loads((ROOT/'protocol/core.schema.json').read_text()); jsonschema.Draft202012Validator.check_schema(schema)
validator=jsonschema.Draft202012Validator(schema,format_checker=jsonschema.FormatChecker())
fixtures=json.loads((ROOT/'protocol/examples/core.json').read_text())
for e in validator.iter_errors(fixtures):errors.append('Schema: '+str(list(e.path))+' '+e.message)
tasks={f['record']['task_id']:f['record'] for f in fixtures if f['record_type']=='Task'}
def valid_result(v):
    task=tasks.get(v['task_id'])
    if not task or task['goal_revision']!=v['goal_revision']:return False
    reqs={x['requirement_id']:x for x in task['requirements']}
    if len(reqs)!=len(task['requirements']):return False
    selected={}
    for c in v['condition_results']:
        key=c['requirement_id']
        if key in selected or key not in reqs:return False
        if c['task_id']!=v['task_id'] or c['goal_revision']!=v['goal_revision']:return False
        if c['rule_ref']!=reqs[key]['rule_ref'] or c['artifact_ref'] not in v['artifact_refs']:return False
        if c['artifact_ref']['tenant_id']!=task['tenant_id']:return False
        selected[key]=c
    required=[key for key,x in reqs.items() if x['required']]
    if not required:return False
    if any(key not in selected or selected[key]['verdict']!='pass' or selected[key]['applicability']!='usable' for key in required):return False
    rank={'verified':0,'assessed':1,'user_accepted':2}
    return rank[v['completion_basis']]==max(rank[selected[key]['basis']] for key in required)
def valid_command(v):
    p=v['payload'];m=v['method']
    if m in ('task.cancel','task.billing_reconcile'):return v['target_id']==p['task_id']
    if m=='task.submit':
        if v['target_id']!=p['orchestrator_id'] or v['logical_service_id']!=p['orchestrator_id']:return False
        if 'delegation_context' not in p:return True
        c=p['delegation_context'];parent=c['parent_task_ref'];gate=c['parent_control_snapshot']
        return c['allocation_ref']['owner_id']==parent['owner_id']==gate['orchestrator_id'] and gate['task_id']==parent['object_id'] and gate['goal_revision']==c['parent_goal_revision'] and gate['status']=='active' and gate['control']=='running' and c['ancestor_task_refs'][-1]==parent and len({x['object_id'] for x in c['ancestor_task_refs']})==len(c['ancestor_task_refs'])
    if m=='execution.invoke':
        g=p['control_snapshot'];tr=p['task_ref']
        return g['orchestrator_id']==tr['owner_id']==p['reservation_ref']['owner_id'] and g['task_id']==tr['object_id'] and g['goal_revision']==p['goal_revision'] and g['control_revision']==p['control_revision'] and g['status']=='active' and g['control']=='running' and g['issued_at']<g['start_before']
    if m=='execution.cancel':return p['orchestrator_id']==p['task_ref']['owner_id']
    return False
for f in fixtures:
    v=f['record']; kind=f['record_type']
    if kind=='Task':
        c=v['open_effects'];require(c['unresolved_count']<=c['total_count'],f['case_id']+': impossible collection')
        if v['status']=='succeeded':require(c['complete'] and c['unresolved_count']==0,f['case_id']+': success with open effects')
    if kind=='Result':
        require(valid_result(v),f['case_id']+': missing/misbound necessary evidence or overstated basis')
    if kind=='Command':require(valid_command(v),f['case_id']+': command binding mismatch')
# These mutations test independent shape/semantic constraints, not real-world truth.
negative=[]
def bad(index, mutate, semantic=None):
    data=copy.deepcopy(fixtures); mutate(data[index]['record'])
    failed=not validator.is_valid(data)
    if semantic is not None: failed=failed or not semantic(data[index]['record'])
    negative.append(failed)
bad(0,lambda v:v.update(status='succeeded'))
bad(2,lambda v:v['open_effects'].update(unresolved_count=1))
bad(2,lambda v:v['open_effects'].update(complete=False))
bad(1,lambda v:v.update(effect='probably_applied'))
bad(4,lambda v:v.pop('consumed_by'))
bad(5,lambda v:v.update(spent=11.5))
bad(6,lambda v:v['payload'].update(extra='not allowed'))
bad(6,lambda v:v.pop('expected_revision'))
bad(8,lambda v:v.pop('holder_id'))
bad(8,lambda v:v.update(work_revision=9007199254740992))
bad(6,lambda v:v.update(target_id='task_'+'f'*32),lambda v:v['target_id']==v['payload']['task_id'])
bad(3,lambda v:v['condition_results'][0].update(verdict='fail'),valid_result)
bad(3,lambda v:v['condition_results'].pop(0),valid_result)
bad(3,lambda v:v['condition_results'][0]['artifact_ref'].update(hash='sha256:'+'f'*64),valid_result)
bad(3,lambda v:v.update(completion_basis='assessed'),valid_result)
require(valid_result(fixtures[3]['record']),'Optional failure must not invalidate necessary conditions or lower the result basis')
bad(10,lambda v:v['payload']['delegation_context']['allocation_ref'].update(owner_id='orch_'+'f'*32),valid_command)
bad(11,lambda v:v['payload']['control_snapshot'].update(control_revision=2),valid_command)
bad(12,lambda v:v['payload'].update(orchestrator_id='orch_'+'f'*32),valid_command)
require(all(negative),'A deliberately invalid fixture escaped checks')
# Spent may exceed a limit after a real correction; rejecting that is a bug.
require(validator.is_valid([fixtures[5]]),'Real overspend fixture must remain representable')
def strict_pairs(items):
    d={}
    for k,v in items:
        if k in d:raise ValueError('duplicate key')
        d[k]=v
    return d
try:json.loads('{"revision":1,"revision":2}',object_pairs_hook=strict_pairs);errors.append('duplicate-key pre-parser failed')
except ValueError:pass
if errors:
    print('\n'.join(errors));sys.exit(1)
print(json.dumps({'markdown_files':len(md),'relative_links':links,'mermaid_diagrams':diagrams,'schema_definitions':len(schema['$defs']),'positive_fixtures':len(fixtures),'negative_mutations':len(negative),'result':'passed','scope':'design assets only'},ensure_ascii=False,indent=2))
