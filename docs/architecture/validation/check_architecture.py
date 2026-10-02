#!/usr/bin/env python3
"""Design-asset checks. Does not exercise a Harness implementation."""
import copy, hashlib, json, pathlib, re, subprocess, sys, urllib.parse
from datetime import datetime
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
coverages={(f['record']['coverage_id'],f['record']['revision']):f['record'] for f in fixtures if f['record_type']=='GoalCoverage'}
def moment(value): return datetime.fromisoformat(value.replace('Z','+00:00'))
def digest(value):
    # These fixtures use simple strings/integers, for which this is JCS-equivalent.
    # This is not a general RFC8785 implementation or a cryptographic provenance test.
    return 'sha256:'+hashlib.sha256(json.dumps(value,ensure_ascii=False,sort_keys=True,separators=(',',':')).encode()).hexdigest()
def same_scope(value,tenant):
    if isinstance(value,dict):
        if 'tenant_id' in value and value['tenant_id']!=tenant:return False
        return all(same_scope(v,tenant) for v in value.values())
    if isinstance(value,list):return all(same_scope(v,tenant) for v in value)
    return True
def unique(items,key):return len({v[key] for v in items})==len(items)
def valid_coverage_ref(ref,task):
    coverage=coverages.get((ref['object_id'],ref['revision']))
    return bool(coverage and ref['tenant_id']==task['tenant_id'] and ref['owner_id']==task['orchestrator_id']
        and coverage['task_ref']['tenant_id']==task['tenant_id'] and coverage['task_ref']['owner_id']==task['orchestrator_id']
        and coverage['task_ref']['object_id']==task['task_id'] and coverage['task_ref']['revision']<=task['revision']
        and coverage['goal_revision']==task['goal_revision'] and coverage['goal_ref']==task['goal_ref']
        and coverage['requirements_digest']==task['requirements_digest'] and coverage['verdict']=='pass'
        and coverage['applicability']=='usable' and same_scope(coverage,task['tenant_id']))
def valid_task(task):
    reqs=task['requirements'];c=task['open_effects']
    if not unique(reqs,'requirement_id') or not unique(task['budget'],'unit'):return False
    if c['unresolved_count']>c['total_count'] or not same_scope(task,task['tenant_id']):return False
    if task['requirements_digest']!=digest(sorted(reqs,key=lambda r:r['requirement_id'])):return False
    if task['requirements_state']=='ready':
        if not reqs or not any(r['required'] for r in reqs):return False
        if not valid_coverage_ref(task.get('current_coverage_ref',{}),task):return False
    if task['status']=='succeeded':
        if task['requirements_state']!='ready' or not c['complete'] or c['unresolved_count']!=0:return False
    return True
def valid_result(v):
    task=tasks.get(v['task_id'])
    if not task or task['status']!='succeeded' or task['goal_revision']!=v['goal_revision']:return False
    if not valid_task(task) or not same_scope(v,task['tenant_id']):return False
    rr=task['result_ref']
    if rr['owner_id']!=task['orchestrator_id'] or rr['object_id']!=v['result_id'] or rr['revision']!=v['revision']:return False
    if not valid_coverage_ref(v['coverage_ref'],task) or v['coverage_ref']!=task['current_coverage_ref']:return False
    if moment(v['completed_at'])>moment(task['deadline']):return False
    reqs={x['requirement_id']:x for x in task['requirements']}
    if len(reqs)!=len(task['requirements']):return False
    selected={}
    for c in v['condition_results']:
        key=c['requirement_id']
        if key in selected or key not in reqs:return False
        if c['task_id']!=v['task_id'] or c['goal_revision']!=v['goal_revision']:return False
        if c['requirement_revision']!=reqs[key]['revision']:return False
        if c['rule_ref']!=reqs[key]['rule_ref'] or c['artifact_ref'] not in v['artifact_refs']:return False
        if c['artifact_ref']['tenant_id']!=task['tenant_id']:return False
        if reqs[key]['required'] and reqs[key]['kind']=='effect' and c['basis']!='verified':return False
        if moment(c['checked_at'])>moment(v['completed_at']):return False
        if 'observed_at' in c and moment(c['observed_at'])>moment(c['checked_at']):return False
        selected[key]=c
    required=[key for key,x in reqs.items() if x['required']]
    if not required:return False
    if any(key not in selected or selected[key]['verdict']!='pass' or selected[key]['applicability']!='usable' for key in required):return False
    rank={'verified':0,'assessed':1,'user_accepted':2}
    return rank[v['completion_basis']]==max(rank[selected[key]['basis']] for key in required)
def valid_adoption(v):
    if not unique(v['mappings'],'candidate_key') or not unique(v['mappings'],'requirement_id'):return False
    if v['outcome']=='accepted':return bool(v['mappings']) and v['result_goal_revision']==v['base_goal_revision']+1
    if v['result_goal_revision']!=v['base_goal_revision']:return False
    return v['outcome']!='rejected' or not v['mappings']
def valid_delta(v):
    return unique(v['candidates'],'candidate_key')
def valid_command(v):
    p=v['payload'];m=v['method']
    if m in ('task.cancel','task.billing_reconcile'):return v['target_id']==p['task_id']
    if m=='task.submit':
        if v['target_id']!=p['orchestrator_id'] or v['logical_service_id']!=p['orchestrator_id']:return False
        if not unique(p.get('requirement_candidates',[]),'candidate_key') or not unique(p['budget'],'unit'):return False
        if not same_scope(p,p['goal_ref']['tenant_id']):return False
        if 'delegation_context' not in p:return True
        c=p['delegation_context'];parent=c['parent_task_ref'];gate=c['parent_control_snapshot']
        return c['allocation_ref']['owner_id']==parent['owner_id']==gate['orchestrator_id'] and gate['task_id']==parent['object_id'] and gate['goal_revision']==c['parent_goal_revision'] and gate['status']=='active' and gate['control']=='running' and c['ancestor_task_refs'][-1]==parent and len({x['object_id'] for x in c['ancestor_task_refs']})==len(c['ancestor_task_refs'])
    if m=='execution.invoke':
        g=p['control_snapshot'];tr=p['task_ref']
        return v['target_id']==v['logical_service_id']==p['binding_ref']['owner_id'] and same_scope(p,tr['tenant_id']) and g['orchestrator_id']==tr['owner_id']==p['reservation_ref']['owner_id'] and g['task_id']==tr['object_id'] and g['goal_revision']==p['goal_revision'] and g['control_revision']==p['control_revision'] and g['status']=='active' and g['control']=='running' and moment(g['issued_at'])<moment(g['start_before'])
    if m=='execution.cancel':return v['target_id']==v['logical_service_id'] and p['orchestrator_id']==p['task_ref']['owner_id'] and same_scope(p,p['task_ref']['tenant_id'])
    return False
for f in fixtures:
    v=f['record']; kind=f['record_type']
    if kind=='Task': require(valid_task(v),f['case_id']+': task scope/condition/coverage mismatch')
    if kind=='RequirementAdoption':require(valid_adoption(v),f['case_id']+': invalid adoption identity/version')
    if kind=='RequirementDelta':require(valid_delta(v),f['case_id']+': duplicate candidate key')
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
# Data profile mutations: shape checks plus separate semantic guards.
by_case={f['case_id']:i for i,f in enumerate(fixtures)}
def bad_case(case,mutate,semantic=None):bad(by_case[case],mutate,semantic)
def swap_effect_to_acceptance(v):
    key=next(r['requirement_id'] for r in tasks[v['task_id']]['requirements'] if r['required'] and r['kind']=='effect')
    next(c for c in v['condition_results'] if c['requirement_id']==key)['basis']='user_accepted'
    v['completion_basis']='user_accepted'
bad(3,swap_effect_to_acceptance,valid_result)
bad(3,lambda v:v['coverage_ref'].update(tenant_id='tenant_'+'f'*32),valid_result)
bad(3,lambda v:v['coverage_ref'].update(owner_id='orch_'+'f'*32),valid_result)
bad(3,lambda v:v['coverage_ref'].update(revision=2),valid_result)
bad(3,lambda v:v.update(completed_at='2026-10-03T00:00:00Z'),valid_result)
bad(3,lambda v:v['condition_results'][0].update(requirement_revision=2),valid_result)
bad(3,lambda v:v['condition_results'][0].update(observed_at='2026-10-03T00:00:00Z'),valid_result)
bad(11,lambda v:v.update(target_id='executor_'+'f'*32),valid_command)
bad(11,lambda v:v['payload']['binding_ref'].update(owner_id='executor_'+'f'*32),valid_command)
bad(12,lambda v:v.update(target_id='executor_'+'f'*32),valid_command)
bad(9,lambda v:v['payload']['requirement_candidates'].append(copy.deepcopy(v['payload']['requirement_candidates'][0])),valid_command)
bad(9,lambda v:v['payload'].update(requirements=[]),valid_command)
bad_case('ready-after-intake',lambda v:v.update(requirements=[]),valid_task)
bad_case('ready-after-intake',lambda v:v['current_coverage_ref'].update(object_id='coverage_'+'f'*32),valid_task)
bad_case('ready-after-intake',lambda v:v.update(requirements_digest='sha256:'+'0'*64),valid_task)
bad_case('ready-after-intake',lambda v:v['requirements'].append(copy.deepcopy(v['requirements'][0])),valid_task)
bad_case('candidate-conditions',lambda v:v['candidates'].append(copy.deepcopy(v['candidates'][0])),valid_delta)
bad_case('accepted-original-decision',lambda v:v.update(result_goal_revision=1),valid_adoption)
bad_case('accepted-original-decision',lambda v:v.update(outcome='rejected'),valid_adoption)
bad_case('accepted-original-decision',lambda v:v['mappings'].append(copy.deepcopy(v['mappings'][0])),valid_adoption)
bad_case('claim-fixed-observed-work',lambda v:v.pop('observed_work_revision'))
bad_case('clarification-pending',lambda v:v.update(state='answered'))
bad_case('clarification-pending',lambda v:v.update(purpose='accept_quality'))
# The initial raw natural-language submit and cancelled unresolved Task remain representable.
require(validator.is_valid([fixtures[by_case['raw-submit-without-conditions']]]),'raw submit must not require authoritative requirements')
require(valid_command(fixtures[by_case['raw-submit-without-conditions']]['record']),'raw submit must have valid owner binding')
unknown_check=copy.deepcopy(fixtures[3]['record']['condition_results'][0]);unknown_check.update(verdict='unknown',applicability='unknown');unknown_check.pop('observed_at')
check_validator=jsonschema.Draft202012Validator({'$ref':'#/$defs/ConditionResult','$defs':schema['$defs']},format_checker=jsonschema.FormatChecker())
require(check_validator.is_valid(unknown_check),'unknown evidence must not require an invented observation time')
independent_input=copy.deepcopy(fixtures[by_case['original-submission']]['record'])
independent_request=copy.deepcopy(fixtures[by_case['clarification-pending']]['record']['target_ref']);independent_request.update(owner_id='app_'+'0'*31+'1',object_id='input_'+'0'*31+'2')
independent_input.update(kind='input',request_ref=independent_request);independent_input.pop('task_ref')
submission_validator=jsonschema.Draft202012Validator({'$ref':'#/$defs/Submission','$defs':schema['$defs']},format_checker=jsonschema.FormatChecker())
require(submission_validator.is_valid(independent_input),'independent app input must not require a fictitious Task')
# Explicit current and historical views of the intake chain are linked by owner, tenant and version.
initial=fixtures[by_case['initial-empty-goal']]['record'];adopted=fixtures[by_case['adopted-goal']]['record'];ready=fixtures[by_case['ready-after-intake']]['record'];adoption=fixtures[by_case['accepted-original-decision']]['record']
require(initial['requirements']==[] and initial['goal_revision']==1,'intake starts with a preserved empty version')
require(adopted['task_ref']['object_id']==ready['task_id'] and adopted['goal_revision']==ready['goal_revision']==adoption['result_goal_revision'],'intake version links differ')
require(adopted['requirements']==[{'requirement_id':r['requirement_id'],'revision':r['revision']} for r in ready['requirements']],'adopted goal must select exact requirement versions')
require(adopted['requirements_digest']==ready['requirements_digest'],'intake digest differs')
require(all(r['adoption_id']==adoption['adoption_id'] for r in ready['requirements']),'requirement loses original adoption')
bad(3,lambda v:v.update(result_id='result_'+'f'*32),valid_result)
bad_case('registered-effect-rule',lambda v:v.update(allowed_basis=['user_accepted']))
bad_case('registered-effect-rule',lambda v:v.update(allow_user_acceptance=True))
bad_case('registered-effect-rule',lambda v:v.pop('max_observation_age_seconds'))
bad_case('schedule-interval',lambda v:v.update(every_seconds=0))
bad_case('schedule-daily',lambda v:v.update(local_time='24:00:00'))
bad_case('schedule-weekly',lambda v:v.update(weekdays=[1,1]))
bad_case('schedule-monthly',lambda v:v.update(monthdays=[32]))
bad_case('schedule-daily',lambda v:v.update(cron='* * * * *'))
bad_case('ordered-goal-document',lambda v:v.update(format_version=2))
bad(11,lambda v:v['payload']['control_snapshot'].pop('window_id'))
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
dictionary_check=subprocess.run([sys.executable,str(ROOT/'validation/build_field_reference.py'),'--check'],capture_output=True,text=True)
require(dictionary_check.returncode==0,dictionary_check.stdout+dictionary_check.stderr)
if errors:
    print('\n'.join(errors));sys.exit(1)
print(json.dumps({'markdown_files':len(md),'relative_links':links,'mermaid_diagrams':diagrams,'schema_definitions':len(schema['$defs']),'positive_fixtures':len(fixtures),'negative_mutations':len(negative),'result':'passed','scope':'design assets only'},ensure_ascii=False,indent=2))
