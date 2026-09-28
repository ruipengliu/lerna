#!/usr/bin/env python3
"""Static schema, concrete-byte and linkage checks. No services are executed."""
from pathlib import Path
from copy import deepcopy as cp
from collections import Counter
import hashlib
import json
import re
import subprocess
import sys
from urllib.parse import urlparse

HERE=Path(__file__).resolve().parent
ROOT=HERE.parents[2]
sys.path.insert(0,str(ROOT/'docs/architecture/validation'))
from jsonschema import Draft202012Validator
from protocol.schema import validate
from protocol.exchanges import validate_exchange
from protocol.runtime_rules import check_exchange as runtime_exchange
from protocol.runtime_rules import check_brain_plan, check_plan_installation, inspect_plan_materialization
from protocol.content_rules import check_exchange as content_exchange
from protocol.content_rules import check_trace_rules as content_trace
from protocol.governance_rules import check_exchange as governance_exchange
from validate_brain import resolve_generation
from check_documents import anchors


def enc(v):
    return json.dumps(v,ensure_ascii=False,sort_keys=True,separators=(',',':'),allow_nan=False).encode()


def dg(b):
    return 'sha256:'+hashlib.sha256(b).hexdigest()


def read(p):
    return json.loads(p.read_text())


def walk(v):
    if isinstance(v,dict):
        yield v
        for k,x in v.items():
            # These are JSON Schemas, not runtime tenant or content objects.
            if k not in ['input_schema','output_schema']:
                yield from walk(x)
    elif isinstance(v,list):
        for x in v:
            yield from walk(x)


def check(assertion, detail):
    if not assertion:
        raise AssertionError(detail)


def materialization_state(b):
    p=b['objects']['BrainPlan']['value'];t=b['objects']['Task-final']['value']
    state=dict(task_ref=p['task_ref'],goal_revision=2,plan_ref=dict(plan_id=p['plan_id'],revision=p['revision']),admissions=[],outputs=[],requirements=t['requirements'],condition_checks=[])
    for r in b['records'].values():
        v=r['value']
        if r['kind']=='PlanStepAdmission':
            state['admissions'].append(dict(task_ref=p['task_ref'],plan_id=p['plan_id'],plan_revision=p['revision'],step_id=v['step_id'],operation_id=v['operation_id']))
        if r['kind']=='Operation':
            out=next(x for x in b['contents'].values() if x['ref']==v['result_ref'])
            state['outputs'].append(dict(task_ref=p['task_ref'],operation_id=v['operation_id'],execution_state=v['execution_state'],effect=v['effect'],may_apply_later=v['may_apply_later'],verified=True,revision=v['revision'],content_ref=v['result_ref'],body=out['body']))
        if r['kind']=='ConditionCheck' and 'result' in v:
            state['condition_checks'].append(dict(task_ref=p['task_ref'],check_id=v['check_id'],rule_ref=v['rule_ref'],result=v['result'],selected=v['selected'],applicability=v['applicability']))
    return state


def official_chain_valid(bundle,shared):
    """Independent deterministic check; never trusts fixture pass booleans."""
    output=bundle['contents']['O7-output']['body']
    registry=shared['components']['official-source-registry']
    if output['official_registry_ref']!=registry['ref']:return False
    sources=output['source_refs'];fetches=output['fetch_evidence_refs']
    if len(sources)!=4 or len(fetches)!=len(sources):return False
    links=dict(re.findall(r'^\[([^\]]+)\]: (\S+)$',bundle['contents']['report']['body'],re.M))
    if len(links)!=len(sources):return False
    for source,fetchref in zip(sources,fetches):
        fetch=next((v['body'] for v in bundle['contents'].values() if v['ref']==fetchref),None)
        citation=next((x for x in output['citation_checks'] if x['source_ref']==source),None)
        if not fetch or not citation or 'redirect_chain' not in fetch or fetch.get('body_ref')!=source:return False
        registered=next((p for p in registry['body']['products'] if p['host']==urlparse(fetch['url']).hostname),None)
        if not registered:return False
        if citation['cited_url']!=fetch['url'] or links.get(citation['citation_key'])!=fetch['url']:return False
        for url in [fetch['url']]+fetch['redirect_chain']+[fetch['final_url']]:
            p=urlparse(url)
            if p.scheme!='https' or p.hostname!=registered['host'] or not p.path.startswith('/'+registered['version']+'/'):return False
    return True


def main():
    shared=read(HERE/'shared.json')
    ids=read(HERE/'identities.json')
    caps=[dict(capability_ref=c['ref'],binding_ref=c['binding_ref'],input_schema=c['capability']['input_schema'],semantic_operation_id=c['capability']['semantic_operation_id'],effect_class=c['capability']['effect_class']) for c in shared['capabilities'].values()]
    expected={'bluetooth-on':(2,0,1,2),'bluetooth-off':(3,0,3,3),'report':(4,1,9,5)}
    report=dict(scope='STATIC ONLY; no Harness, model, database, remote sources, authorization or devices executed.',scenarios={},known_upstream_checker_limitations=[],negative_checks=[])
    all_refs=[];all_proofs=[];schemas=0;exchanges=0;bytefiles=0
    check(len(set(ids.values()))==len(ids),'fixture ID collisions')
    check(all(not validate('Id',v) for v in ids.values()),'ID format')
    check(not validate('GrantPolicy',shared['grant_policy']),'assumed GrantPolicy schema')
    for k,c in shared['components'].items():
        raw=(HERE/c['body_file']).read_bytes()
        check(dg(raw)==c['ref']['digest'] and len(raw)==c['byte_length'],('component bytes',k))
    for k,c in shared['capabilities'].items():
        check(not validate('Capability',c['capability']),('capability',k))
        check(not validate('Binding',c['binding']),('binding',k))
        for f in ['input_schema','output_schema']:
            Draft202012Validator.check_schema(c['capability'][f])
    for name,exp in expected.items():
        b=read(HERE/name/'scenario.json')
        submitted=False
        grant=shared['grant_policy'];limits={x['unit']:int(x['limit']) for x in grant['limits']}
        consumed=Counter()
        for ex in b['exchanges']:
            request=ex['exchange']['request']
            if request['method']=='task.submit':submitted=True
            if request['method']!='grant.use':continue
            q=request['payload']
            check(('task_id' in q['subject'])==submitted,(name,'no task scope before task exists',ex['label']))
            for one,many in [('action','actions'),('purpose','purposes'),('recipient','recipients'),('location','locations')]:
                check(q[one] in grant[many],(name,'use outside assumed grant',one,ex['label']))
            check(all(r in grant['resources'] for r in q['resource_scopes']),(name,'use outside resource scope',ex['label']))
            for one in ['max_units','max_cost']:
                v=q[one];check(v['unit'] in limits,(name,'unbounded unit',v));consumed[v['unit']]+=int(v['amount'])
            wanted_location={ids['search-provider']:ids['search-provider-location'],ids['fetch-provider']:ids['fetch-provider-location']}.get(q['recipient'],ids['local-host'])
            check(q['location']==wanted_location,(name,'receiver location',ex['label']))
        check(all(v<=limits[u] for u,v in consumed.items()),(name,'assumed grant quota exceeded',consumed))
        check(not any(x['label'].startswith('read:result:user') for x in b['exchanges']),(name,'duplicate Result download'))
        for alias,c in b['contents'].items():
            raw=(HERE/c['body_file']).read_bytes();r=c['ref']
            check(dg(raw)==r['hash'] and len(raw)==r['byte_length'],('content bytes',name,alias))
            check(raw==(enc(c['body']) if r['media_type']=='application/json' else c['body'].encode()),('body serialized differently',alias))
            for src in c['source_refs']:
                check(any(src==x['ref'] for x in b['contents'].values()),('missing exact source',name,alias))
            all_refs.append(r);bytefiles+=1
        for alias,o in b['objects'].items():
            check(o['json_bytes']==len(enc(o['value'])),('object size',name,alias))
            if o['schema']!='INTERNAL-DESIGN-EXAMPLE':
                e=validate(o['schema'],o['value']);check(not e,(name,alias,e));schemas+=1
        for ex in b['exchanges']:
            x=ex['exchange'];method=x['request']['method']
            errs=validate_exchange(x,caps)
            # Existing walker traverses JSON Schema's properties.tenant_id and
            # treats the schema object as an actual authenticated tenant ID.
            # Keep that raw outcome visible and run an independent data-only
            # check; never relax actual runtime tenant comparisons.
            if errs and method=='capability.describe' and all(e=='tenant_binding: response reference belongs to another tenant' for e in errs):
                report['known_upstream_checker_limitations'].append(dict(scenario=name,label=ex['label'],messages=errs,reason='upstream recursive tenant walker enters capability JSON Schema; data-only check performed separately'))
                errs=[]
            check(not errs,(name,ex['label'],errs))
            for val in [x['request'],x['response']]:
                check(all(v['tenant_id']==x['auth']['tenant_id'] for v in walk(val) if 'tenant_id' in v),('runtime tenant mismatch',name,ex['label']))
            for checker in [runtime_exchange,content_exchange,governance_exchange]:
                e=checker(x,caps);check(not e,(name,ex['label'],e))
            exchanges+=1
        # Content copy registration must precede retrieval; original put and
        # clear/stop semantics are checked by the repo's content trace rules.
        e=content_trace(dict(events=[dict(at=x['exchange']['response'].get('observed_at',x['exchange']['response'].get('decided_at',x['exchange']['response'].get('accepted_at'))),exchange=x['exchange']) for x in b['exchanges']]))
        check(not e,(name,'content trace',e))
        for g in b['generations']:
            publication={}
            for local,cr in g['resolved_contents'].items():
                publication[local]={k:cr[k] for k in ['tenant_id','owner_id','content_id','version']}
                publication[local].update(upload_id=ids[name+':upload:'+local],command_id=ids[name+':put:'+local+':command'],stored_ref=cr)
            fixture=dict(generation=g['generation'],publication=publication,input_manifest=g['actual_input_manifest'],visible_refs=g['actual_input_manifest'])
            resolved,e=resolve_generation(fixture)
            check(not e,(name,g['label'],'generation',e))
            check(resolved['proposal']==g['resolved_proposal'],(name,'generation proposal differs'))
            check(resolved['refs']==g['resolved_contents'],(name,'generation publication differs'))
        if 'BrainPlan' in b['objects']:
            plan=b['objects']['BrainPlan']['value']
            check(not check_brain_plan(plan),(name,'plan'))
            prop=b['objects']['D'+str(b['statistics']['brain_calls'])+':Proposal']['value']
            check(not check_plan_installation(prop,plan,b['contents']['plan']['ref'],plan['task_ref'],2),(name,'plan installation'))
            state=materialization_state(b)
            for step in plan['steps']:
                materialized=inspect_plan_materialization(plan,step['step_id'],state,caps)
                check(materialized['status']=='ready',(name,step['step_id'],materialized))
                admission=next(v['value'] for v in b['records'].values() if v['kind']=='PlanStepAdmission' and v['value']['step_id']==step['step_id'])
                check(materialized['candidate']['arguments']==admission['arguments'],(name,'materialized arguments drift'))
        # Exact operation and output adapter contracts; no symbolic future data.
        for i in range(1,b['statistics']['operations']+1):
            inv=b['objects'][f'O{i}:Invoke']['value'];op=b['objects'][f'O{i}:Operation']['value'];out=b['contents'][f'O{i}-output']['body']
            cap=next(c['capability'] for c in shared['capabilities'].values() if c['ref']==inv['capability_ref'])
            check(not list(Draft202012Validator(cap['input_schema']).iter_errors(inv['arguments'])),(name,'adapter input',i))
            check(not list(Draft202012Validator(cap['output_schema']).iter_errors(out)),(name,'adapter output',i))
            check(inv['operation_id']==op['operation_id'] and inv['goal_revision']==2,(name,'operation linkage'))
            check(op['result_ref']==b['contents'][f'O{i}-output']['ref'],(name,'operation result linkage'))
            check(op['execution_state']=='closed' and op['effect']=='applied' and op['may_apply_later'] is False,(name,'successful dependencies'))
            attempt=op['attempts'][0]
            check(attempt['prepared_at']<attempt['sent_at'],(name,'attempt order',i))
            for timestamp in ['observed_at','retrieved_at']:
                if timestamp in out:check(attempt['sent_at']<out[timestamp],(name,'observation before send',i))
        check([x['revision'] for x in b['task_revision_log']]==list(range(1,len(b['task_revision_log'])+1)),(name,'Task revision sequence'))
        first_intent=b['objects']['O1:OperationIntent']['value']
        check(first_intent['source']['decision_id']==ids[name+':D2'],(name,'D1 stale action must not be admitted'))
        check(b['objects']['D1:DecisionRequest']['value']['snapshot_revision']!=b['objects']['D2:DecisionRequest']['value']['snapshot_revision'],(name,'new decide snapshot'))
        result=b['objects']['Result']['value'];task=b['objects']['Task-final']['value']
        check(task['result_ref']==b['contents']['result']['ref'] and b['contents']['result']['body']==result,(name,'final result reference'))
        check({r['requirement_id'] for r in task['requirements']}=={r['requirement_id'] for r in result['condition_results']},(name,'missing required condition'))
        check(all(r['goal_revision']==task['goal_revision'] and r['verdict']=='pass' for r in result['condition_results']),(name,'condition suitability'))
        # All ContentRefs must identify bytes in this scenario. Exclude the
        # embedded JSON schemas (their field definitions are not real refs).
        known={enc(x['ref']) for x in b['contents'].values()}
        for v in walk(dict(objects=b['objects'],exchanges=b['exchanges'])):
            if set(v)=={'tenant_id','owner_id','content_id','version','hash','media_type','byte_length'}:
                check(enc(v) in known,(name,'unresolved exact ContentRef',v))
        s=b['statistics'];m=Counter(x['exchange']['request']['method'] for x in b['exchanges'])
        check((s['brain_calls'],s['assessment_model_calls'],s['operations'],s['fixture_credit'])==exp,(name,'counts'))
        check(m['brain.decide']==m['brain.get']==s['brain_calls'],(name,'decision query count'))
        check(m['execution.invoke']==m['execution.get']==s['operations'],(name,'operation query count'))
        check(m['grant.use']==m['grant.use.settle']==s['use_count'],(name,'use closures'))
        check(sum(x['json_bytes'] for x in b['records'].values())==s['retained_record_json_bytes'],(name,'record sum'))
        check(sum(len(enc(x['exchange']['request']))+len(enc(x['exchange']['response'])) for x in b['exchanges'])==s['request_response_json_bytes'],(name,'wire sum'))
        check(len(b['records'])==s['retained_record_count'],(name,'record count'))
        check(len(b['minimum_closures'])==s['minimum_closure_count'],(name,'closure count'))
        check(sum(len(enc(x)) for x in b['minimum_closures'])==s['minimum_closure_json_bytes'],(name,'closure sum'))
        reservations=[r['value'] for r in b['records'].values() if r['kind']=='BudgetReservation']
        check(sum(int(x['spent']) for x in reservations)==int(task['budget'][0]['spent']['amount'])==s['fixture_credit'],(name,'unique cost sources'))
        check(len({(x['source_owner'],x['source_kind'],x['source_id']) for x in reservations})==len(reservations),(name,'duplicate fee source'))
        # A grant amount mirrors the same charge; deliberately not added above.
        for r in b['records'].values():
            check(r['json_bytes']==len(enc(r['value'])),(name,'record bytes'))
        all_proofs+=b['control_proofs']
        report['scenarios'][name]=dict(schema_objects=sum(o['schema']!='INTERNAL-DESIGN-EXAMPLE' for o in b['objects'].values()),exchanges=len(b['exchanges']),body_files=len(b['contents']),generations=len(b['generations']),counts_and_bytes='passed',plan_and_reference_links='passed',grant_containment_and_units='passed on assumed policy; not live authorization',event_order='passed')

    r=read(HERE/'report/scenario.json');plan=r['objects']['BrainPlan']['value'];state=materialization_state(r)
    check(official_chain_valid(r,shared),'official fixture chain')
    assessment=r['contents']['O7-output']['body']
    judgments={x['requirement_id']:x['verdict'] for x in assessment['judgments']}
    citation=ids['report:requirement:citation'];quality=ids['report:requirement:quality']
    for key in ['citation','quality']:
        internal=r['records']['check:'+key]['value']
        check(internal['assessment_judgment']==dict(report_ref=r['contents']['O7-output']['ref'],requirement_id=ids['report:requirement:'+key]),'exact assessment judgment binding')
    check(ids['report:check:quality'] not in r['records']['check:citation']['value']['dependency_check_ids'],'quality is independent of citation semantics')
    changed=cp(r)
    next(x for x in changed['contents']['O7-output']['body']['judgments'] if x['requirement_id']==citation)['verdict']='fail'
    quality_judgment=next(x for x in changed['contents']['O7-output']['body']['judgments'] if x['requirement_id']==quality)
    check(official_chain_valid(changed,shared) and quality_judgment['verdict']=='pass','valid identity and quality do not override failed semantic citation')
    st=cp(state)
    next(x for x in st['condition_checks'] if x['result']['requirement_id']==citation)['result']['verdict']='fail'
    check(inspect_plan_materialization(plan,'write',st,caps)['status']=='redecide','quality pass citation fail blocks write')
    report['negative_checks'].append('quality pass and citation semantic fail blocks write')
    for label,mutate in [
        ('unregistered final host blocks citation and write',lambda b:b['contents']['O3-output']['body'].update(final_url='https://untrusted.example/1.0/deployment')),
        ('missing fetch evidence blocks citation and write',lambda b:b['contents']['O7-output']['body']['fetch_evidence_refs'].pop()),
        ('missing redirect chain blocks citation and write',lambda b:b['contents']['O3-output']['body'].pop('redirect_chain')),
        ('wrong report link blocks citation and write',lambda b:b['contents']['report'].update(body=b['contents']['report']['body'].replace('[A1]: https://atlas.example','[A1]: https://untrusted.example')))
    ]:
        changed=cp(r);mutate(changed);check(not official_chain_valid(changed,shared),label)
        st=cp(state)
        next(x for x in st['condition_checks'] if x['result']['requirement_id']==ids['report:requirement:citation'])['result']['verdict']='fail'
        check(inspect_plan_materialization(plan,'write',st,caps)['status']=='redecide',label+' propagation')
        report['negative_checks'].append(label)
    for label,mutate,step,want in [
        ('valid assessment fail blocks write',lambda s:s['condition_checks'][0]['result'].update(verdict='fail'),'write','redecide'),
        ('unknown effect blocks dependent read',lambda s:next(x for x in s['outputs'] if x['operation_id']==ids['report:O8']).update(effect='unknown',may_apply_later='unknown'),'readback','waiting'),
        ('missing output version cannot be guessed',lambda s:next(x for x in s['outputs'] if x['operation_id']==ids['report:O8'])['body'].pop('file_version'),'readback','redecide'),
        ('changed goal invalidates plan',lambda s:s.update(goal_revision=3),'write','stale'),
        ('inapplicable pass cannot authorize write',lambda s:s['condition_checks'][0].update(applicability='unknown'),'write','waiting')
    ]:
        st=cp(state);mutate(st)
        check(inspect_plan_materialization(plan,step,st,caps)['status']==want,label)
        report['negative_checks'].append(label)
    p=cp(r['objects']['D4:Proposal']['value']);p['actions']=[r['objects']['D2:Proposal']['value']['actions'][0]]
    check(bool(validate('Proposal',p)),'plan and actions must be exclusive');report['negative_checks'].append('plan plus direct actions rejected')
    e=cp(next(x['exchange'] for x in r['exchanges'] if x['label']=='task-read'));e['request']['payload']={'task_id':r['task_id']}
    check(bool(validate_exchange(e,caps)),'task.read ID belongs in target');report['negative_checks'].append('query payload identity rejected')
    candidate=r['contents']['report']['ref'];rb=r['contents']['readback-bytes']['ref']
    check(candidate['hash']==rb['hash'] and candidate['byte_length']==rb['byte_length'] and candidate['content_id']!=rb['content_id'],'equal bytes with independent readback provenance')
    for checkrow in r['contents']['O7-output']['body']['citation_checks']:
        c=next(x for x in r['contents'].values() if x['ref']==checkrow['source_ref'])
        raw=(HERE/c['body_file']).read_bytes()
        check(raw[checkrow['byte_start']:checkrow['byte_end']].decode()==checkrow['quote'],'citation byte range')
    report['negative_checks'].append('changed candidate hash would fail equality check')
    check(dg((HERE/r['contents']['report']['body_file']).read_bytes()+b'\n')!=candidate['hash'],'modified body hash')
    signed=subprocess.run(['node',str(HERE/'proofs.mjs')],input=json.dumps(dict(mode='verify',items=all_proofs)),text=True,capture_output=True,check=True)
    report['control_signatures']=json.loads(signed.stdout)
    # Negative binding check: correct signature cannot authorize another gate.
    wrong=cp(all_proofs);wrong[0]['payload']['gate']['goal_revision']+=1
    tampered=subprocess.run(['node',str(HERE/'proofs.mjs')],input=json.dumps(dict(mode='verify',items=wrong)),text=True,capture_output=True)
    check(tampered.returncode!=0,'changed control binding must fail');report['negative_checks'].append('control proof binding changed')
    doc=HERE.parent/'task-scenarios-input-output-2026-09-28.md'
    text=doc.read_text();plain=re.sub(r'^```.*?^```\s*$', '', text, flags=re.M|re.S)
    stats=read(HERE/'statistics.json')
    for label,key in [('Brain 生成','brain_calls'),('评估模型生成','assessment_model_calls'),('Operation','operations'),('领域协议对','protocol_pairs'),('请求+返回 JSON B','request_response_json_bytes'),('本包序列化的保留记录数','retained_record_count'),('这些记录 JSON B','retained_record_json_bytes'),('新正文数','new_body_count'),('新正文唯一身份字节 B','new_body_bytes'),('下载字节 B（本轮各持有者冷读）','download_bytes'),('用途使用/结算各次数','use_count'),('选定最小关闭记录数','minimum_closure_count'),('其独立 JSON B','minimum_closure_json_bytes'),('Brain 输入材料字节累计（非token）','model_input_body_bytes'),('O7 评估输入材料字节（非token）','assessment_input_body_bytes')]:
        row='| '+' | '.join([label]+[str(stats[n][key]) for n in expected])+' |'
        check(row in plain,('document statistics stale',label))
    links=[]
    for target in re.findall(r'\[[^\]]*\]\(([^)]+)\)',plain):
        if re.match(r'^[a-zA-Z][\w+.-]*:',target):continue
        f,_,frag=target.partition('#');dest=(doc.parent/f).resolve() if f else doc
        check(dest.exists(),('local link missing',target))
        if frag and dest.suffix=='.md':check(frag in anchors(dest.read_text()),('anchor missing',target))
        links.append(target)
    in_fence=False;width=None;tables=0
    for line in text.splitlines():
        if line.startswith('```'):in_fence=not in_fence;width=None;continue
        if in_fence:continue
        if line.startswith('|'):
            current=len(re.findall(r'(?<!\\)\|',line))-1
            if width is None:tables+=1
            check(width is None or width==current,('table width',line))
            width=current
        else:width=None
    check(not in_fence,'unclosed fence')
    report['markdown']=dict(local_links=len(links),tables=tables,status='passed',rendering='separate')
    # Capture current basis hashes without treating concurrent chats' edits as ours.
    paths=['docs/architecture/contracts/schemas/protocol.schema.json','docs/architecture/contracts/schemas/methods.json','docs/architecture/contracts/schemas/brain-generation.schema.json']
    report['contract_digests']={p:dg((ROOT/p).read_bytes()) for p in paths}
    baseline=read(HERE/'protected-baseline.json')
    report['preexisting_review_preservation']={p:hashlib.sha256((ROOT/p).read_bytes()).hexdigest()==h for p,h in baseline.items() if p.startswith('.scratch/architecture-review/')}
    report['shared_workspace_note']='This task writes only the main scenario document and task-scenarios-data/. Other chats may update formal docs and the September 28 review; changed hashes are reported, not overwritten.'
    check(all(ok for p,ok in report['preexisting_review_preservation'].items() if p.endswith('2026-09-27.md')),'September 27 review changed')
    report.update(schema_objects=schemas,exchanges=exchanges,body_files=bytefiles,status='passed_with_documented_upstream_checker_limitation',runtime='not_tested')
    (HERE/'validation-results.json').write_text(json.dumps(report,ensure_ascii=False,indent=2)+'\n')
    print(json.dumps({k:report[k] for k in ['status','schema_objects','exchanges','body_files','control_signatures','markdown','runtime']},ensure_ascii=False,indent=2))
    print('Negative checks:',len(report['negative_checks']),'upstream JSON-Schema walker exceptions:',len(report['known_upstream_checker_limitations']))


if __name__=='__main__':main()
