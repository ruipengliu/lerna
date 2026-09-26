"""Recorded governance relations. No live identity, isolation or durable commit proof."""
from copy import deepcopy
from datetime import datetime
from decimal import Decimal
from .schema import METHODS, validate


def _time(value):
    return datetime.fromisoformat(value.replace('Z', '+00:00'))


def _walk(value):
    if isinstance(value, dict):
        yield value
        for child in value.values():
            yield from _walk(child)
    elif isinstance(value, list):
        for child in value:
            yield from _walk(child)


def _amount_le(a, b):
    return a['unit'] == b['unit'] and Decimal(a['amount']) <= Decimal(b['amount'])


def _policy_subset(child, parent):
    if any(not set(child[k]) <= set(parent[k]) for k in ('actions', 'purposes', 'recipients', 'locations')):
        return False
    if _time(child['valid_from']) < _time(parent['valid_from']) or _time(child['expires_at']) > _time(parent['expires_at']):
        return False
    if parent['mode']=='once' and child['mode']!='once':
        return False
    if child['max_offline_window_ms'] > parent['max_offline_window_ms']:
        return False
    for scope in child['resources']:
        candidates=[p for p in parent['resources'] if all(scope[k] == p[k] for k in ('resource_owner_id','resource_type','normalizer_version'))]
        if not any(set(scope['selector']['object_ids']) <= set(p['selector']['object_ids']) and ('versions' not in p['selector'] or ('versions' in scope['selector'] and set(scope['selector']['versions']) <= set(p['selector']['versions']))) for p in candidates):
            return False
    ceilings={a['unit']:Decimal(a['limit']) for a in parent['limits']}
    return all(x['unit'] in ceilings and Decimal(x['limit']) <= ceilings[x['unit']] for x in child['limits'])


def _pair_scope_subset(child, parent):
    return (set(child['capability_ids']) <= set(parent['capability_ids'])
            and all(x in parent['resource_scopes'] for x in child['resource_scopes'])
            and child['max_offline_window_ms'] <= parent['max_offline_window_ms'])


def check_exchange(exchange, capabilities):
    """Call after shared Schema validation; tolerate rejected/redacted projections."""
    req=exchange.get('request',{}); res=exchange.get('response',{}); name=req.get('method','')
    if not name.startswith(('grant.','endpoint.','extensions.','evaluation.')):
        return []
    spec=METHODS.get(name)
    if spec and (validate(spec.get('input','Id'),req.get('payload')) or ('output' in res and validate(spec.get('output','Id'),res['output']))):
        return []
    if 'output' not in res or res.get('stage') == 'rejected':
        return []
    p=req['payload']; out=res['output']; target=req['target_id']; errors=[]
    def fail(rule, detail): errors.append(rule+': '+detail)
    def same(a,b,rule='governance_binding'):
        if a != b: fail(rule,'request and recorded result differ')
    def revision(current):
        if 'expected_revision' in req and current != req['expected_revision']+1:
            fail('governance_revision','successful conditional mutation must advance exactly once')
    if name=='grant.issue':
        same(out['grant_id'],p['grant_id']);same(out['owner_id'],target)
        for k in ('policy','intent_hash','confirmation_ref'):same(out[k],p[k])
        if out['state']!='active' or out['revision']!=1:fail('grant_issue_state','new Grant must start active at revision one')
    if name in ('grant.read','grant.revoke'):
        same(out['grant_id'],target)
        if name.endswith('revoke'):
            revision(out['revision'])
            if out['state']!='revoked':fail('grant_revocation','revoke cannot return active')
    for node in _walk(out):
        if {'valid_from','expires_at','limits','resources','mode'} <= node.keys():
            if _time(node['expires_at'])<=_time(node['valid_from']):fail('grant_window','permission window must be positive')
            if len({x['unit'] for x in node['limits']})!=len(node['limits']):fail('grant_units','limits must use unique units')
        if {'required_revision','enforced_revision','endpoint_id','state'} <= node.keys():
            if node['enforced_revision']>node['required_revision'] or (node['state']=='enforced' and node['enforced_revision']!=node['required_revision']):fail('propagation_revision','enforced state must match required revision')
    if name=='grant.lease.allocate':
        same(out['owner_id'],target)
        for k in ('lease_id','grant_refs','endpoint_id','instance_id','scope','allocated_units','allocated_cost','expires_at'):same(out[k],p[k])
        if out['state']!='allocated' or out['revision']!=1:fail('lease_state','new lease must be allocated')
        if _time(out['expires_at'])<=_time(out['issued_at']) or _time(out['expires_at'])>_time(out['scope']['expires_at']) or (_time(out['expires_at'])-_time(out['issued_at'])).total_seconds()*1000>out['scope']['max_offline_window_ms']:fail('lease_window','lease must fit permission')
    if name=='grant.lease.settle':
        same(out['lease_id'],target);same(out['instance_id'],p['instance_id']);revision(out['revision'])
        same(out['settled_units'],p['cumulative_units'],'lease_total');same(out['settled_cost'],p['cumulative_cost'],'lease_total')
        if not _amount_le(out['settled_units'],out['allocated_units']) or not _amount_le(out['settled_cost'],out['allocated_cost']):fail('lease_bound','settlement exceeds allocation')
        if p['final'] and (out['state']!='reconciled' or 'final_settlement_ref' not in out or any(not u['closed'] for u in p['uses'])):fail('lease_final','final settlement needs closed uses and proof')
        if not p['final'] and out['state']=='reconciled':fail('lease_final','partial settlement cannot claim final release')
    if name=='endpoint.pair.begin':
        same(out['session']['pairing_id'],p['pairing_id'])
        for k in ('device_description','requested_scope'):same(out['session'][k],p[k])
        if out['session']['state']!='pending':fail('pairing_state','begin only creates pending session')
    if name=='endpoint.pair.approve':
        same(out['pairing_id'],target);revision(out['revision'])
        if out['state'] != ('approved' if p['decision']=='approve' else 'denied'):fail('pairing_decision','session state must reflect decision')
        if not _pair_scope_subset(p['approved_scope'],out['requested_scope']):fail('pairing_scope','approval expands requested scope')
    if name=='endpoint.pair.claim':
        same(out['session']['pairing_id'],target)
        if out['state']=='claimed':
            same(out['session']['state'],'claimed','pairing_state');same(out['endpoint']['endpoint_id'],out['session']['endpoint_id'])
            if out['endpoint']['state']!='active':fail('pairing_state','claim needs active endpoint')
            if _time(out['credential']['recovery_until'])>_time(out['credential']['expires_at']):fail('credential_window','recovery cannot outlast credential')
    if name=='endpoint.revoke':
        same(out['endpoint_id'],target);revision(out['credential_generation'])
        if out['state']!='revoked':fail('endpoint_revocation','revocation must close credential')
    if name=='extensions.prepare':
        for k in ('lock_id','manifest','config_ref','config_digest','platform','trust_evidence','conformance_report'):same(out[k],p[k])
        same(p['artifact_ref']['hash'],p['manifest']['digest'],'package_digest')
        same(p['config_ref']['hash'],p['config_digest'],'package_digest')
    if name=='extensions.deactivate':
        same(out['activation_id'],p['activation_id']);same(out['generation'],p['generation'])
        if not out['new_use_disabled']:fail('extension_stop','new uses must be disabled')
    if name=='extensions.dispose':
        same(out['lock_id'],target)
        if out['state']=='disposed' and out['blocking_refs']:fail('extension_references','referenced lock cannot be disposed')
        if out['state']=='blocked' and not out['blocking_refs']:fail('extension_references','blocked disposal must identify references')
    if name=='extensions.read':
        if p['kind']=='install_lock':same(out.get('lock_id'),target,'extension_projection')
        else:
            same(out.get('activation_id'),target,'extension_projection')
            if out.get('phase')=='active':
                if not {'startup_evidence','instance_readiness'} <= out.keys():fail('activation_readiness','active needs historical and current evidence')
                else:
                    initial=out['startup_evidence']; ready=out['instance_readiness']; current=ready['startup_evidence']
                    if initial['action_kind']!='activation' or initial['action_id']!=out['activation_id']:fail('activation_approval','historical evidence must name original activation')
                    if current['action_kind'] not in ('activation','reopen'):fail('activation_readiness','readiness cannot use a work receipt')
                    if current['action_kind']=='activation' and current != initial:fail('activation_readiness','initial readiness must use original activation evidence')
                    if not ready['ready'] or ready['instance_id']!=out['ready_instance'] or current['instance_id']!=ready['instance_id'] or out['new_use_disabled']:fail('activation_readiness','ready instance must match current evidence')
                    for evidence in (initial,current):
                        if any(evidence[k]!=out[v] for k,v in [('approval_id','approval_id'),('target_id','target_id'),('lock_id','new_lock_id')]):fail('activation_approval','startup evidence names wrong binding')
                        if evidence['kind']=='remote_use' and _time(evidence['checked_at'])>=_time(evidence['start_before']):fail('approval_window','recorded startup outside original window')
    if name=='evaluation.candidate_register':
        same(out,p['candidate'])
        if out['revision']!=1:fail('candidate_registration','new candidate must start at revision one')
    if name=='evaluation.partition_register':
        same(out,p['partition'])
        if out['revision']!=1 or out['state']!='available':fail('partition_registration','new partition starts available at revision one')
        samples=[s for group in out['source_groups'] for s in group['sample_ids']]
        if len(samples)!=len(set(samples)) or set(samples)!=set(out['sample_ids']):fail('partition_sources','source groups must partition every sample exactly once')
    if name=='evaluation.plan_create':
        same(out,p['plan'])
        if out['purpose']=='release_confirmation':
            if out['baseline_lock'] is None or Decimal(out['minimum_practical_gain']['amount'])<=0:fail('improvement_plan','formal improvement requires baseline and positive practical gain')
            if out['formal_attempt_index']>out['improvement_policy']['formal_attempt_limit']:fail('formal_attempt_limit','formal attempt exceeds immutable policy')
    if name=='evaluation.run':
        same(out['run_id'],p['run_id']);same(out['plan_id'],p['plan_id'])
    if name=='evaluation.cancel':
        same(out['run_id'],target);revision(out['revision'])
        if not out['cancel_requested']:fail('evaluation_cancel','cancel intention must remain recorded')
    if name=='evaluation.read':
        same(out['kind'],p['kind'],'evaluation_projection');same(out['record'][{'candidate':'candidate_id','partition':'partition_id','plan':'plan_id','run':'run_id','report':'report_id','approval':'approval_id'}[p['kind']]],target)
    if name=='evaluation.exposure_record':same(out['exposure'],p['exposure'])
    if name=='evaluation.feedback_open':
        same(out['report']['report_id'],target);same(out['report']['digest'],p['report_digest'],'report_binding');same(out['exposure']['exposure_id'],p['exposure_id'])
        same(out['exposure'].get('report_id'),target,'report_binding');same(out['exposure'].get('report_digest'),p['report_digest'],'report_binding')
        if not out['report']['sealed']:fail('feedback_sealed','feedback only after full report sealing')
    if name=='evaluation.approve':
        same(out,p['approval']);same(out['approved_by'],exchange['auth']['actor_id'],'approval_actor')
        if out['state']!='active' or out['revision']!=1:fail('approval_state','new approval starts active at revision one')
        batch_targets=[t for b in out['batches'] for t in b['targets']]
        if len(batch_targets)!=len(set(batch_targets)) or set(batch_targets)!=set(out['targets']):fail('approval_targets','batches must cover each approved target once')
    if name=='evaluation.revoke':
        same(out['approval_id'],target);revision(out['revision'])
        if out['state']!='revoked':fail('approval_revocation','revoked approval cannot remain active')
    if name=='evaluation.rollout_read':
        same(out['rollout_id'],target)
        if out['state']=='finished' and any(t['state']!='active' or not t['ready'] for t in out['targets']):fail('rollout_completion','finished requires all targets actually active and ready')
    return errors


def check_trace_rules(trace):
    """Check authority records supplied in this bounded sequence, never infer missing facts."""
    errors=[]; grants={}; confirms={}; leases={}; lease_uses={}; lease_rev={}; pairs={}; pair_secrets={}; pair_scopes={}; claims={}; endpoints={}; candidates={}; partitions={}; plans={}; reservations={}; attempts={}; policies={}; reports={}; exposures=[]; approvals={}; uses={}; use_times={}; activations={}; intentions={}; immutable={}
    for i,event in enumerate(trace['events']):
        if 'exchange' not in event:continue
        x=event['exchange'];req=x['request'];res=x['response'];name=req['method'];p=req['payload'];out=res.get('output');target=req['target_id']; rejected=res.get('stage')=='rejected' or ('error' in res and 'stage' not in res)
        def fail(rule,detail):errors.append(f'event {i}: {rule}: {detail}')
        spec=METHODS.get(name)
        if spec and (validate(spec.get('input','Id'),p) or (out is not None and validate(spec.get('output','Id'),out))):continue
        if name.startswith(('evaluation.approval_','extensions.')) and name=='evaluation.approval_check':
            known=approvals.get(p['approval_id'])
            if known and not rejected and p['use_id'] not in uses:
                if known['state']!='active' or _time(known['expires_at'])<=_time(event['at']):fail('approval_current','revoked or expired approval cannot admit new startup')
                if p['target_id'] not in known['targets'] or p['lock_id']!=known['lock_id']:fail('approval_current','startup outside approved target/lock')
        if rejected or out is None:continue
        # Original command replay is checked by shared validator; do not charge twice here.
        if 'command_id' in req:
            key=(x['auth']['tenant_id'],x['auth']['logical_service_id'],req['command_id'])
            if key in immutable:continue
            immutable[key]=deepcopy(req)
        if name=='grant.issue':
            cid=p['confirmation_ref']['id']
            if cid in confirms and confirms[cid]!=p['grant_id']:fail('confirmation_once','same confirmation created another grant')
            confirms[cid]=p['grant_id'];parent=p['policy'].get('parent_grant_ref')
            if parent and parent['id'] in grants and not _policy_subset(p['policy'],grants[parent['id']]['policy']):fail('grant_subset','child policy exceeds recorded parent')
            grants[out['grant_id']]=deepcopy(out)
        if name in ('grant.read','grant.revoke'):
            old=grants.get(out['grant_id'])
            if old and old['state']=='revoked' and out['state']!='revoked':fail('grant_reopen','revocation cannot be undone')
            if old and any(out[k]!=old[k] for k in ('owner_id','policy','intent_hash','confirmation_ref','issued_at')):fail('grant_immutable','grant identity and granted scope cannot change')
            grants[out['grant_id']]=deepcopy(out)
        if name=='grant.use' and out['decision']=='allowed':
            for g in p['grant_refs']:
                known=grants.get(g['id'])
                if known and (known['state']!='active' or _time(known['policy']['expires_at'])<=_time(event['at'])):fail('grant_current','recorded revoked/expired permission cannot be used')
        if name=='grant.lease.allocate':
            if out['lease_id'] in leases and leases[out['lease_id']]!=out:fail('lease_identity','allocation cannot change under original lease identity')
            for g in p['grant_refs']:
                known=grants.get(g['id'])
                if known and (known['state']!='active' or not _policy_subset(p['scope'],known['policy'])):fail('lease_scope','lease exceeds or uses closed permission')
            leases[out['lease_id']]=deepcopy(out)
        if name=='grant.lease.settle':
            old=leases.get(target)
            if old:
                if old['instance_id']!=p['instance_id']:fail('lease_instance','settlement changes consuming instance')
                if old['state']=='reconciled':fail('lease_reopen','final settlement is closed to new commands')
                if not _amount_le(old['settled_units'],p['cumulative_units']) or not _amount_le(old['settled_cost'],p['cumulative_cost']):fail('lease_monotonic','cumulative settlement cannot decrease')
            if p['usage_revision']<=lease_rev.get(target,0):fail('lease_usage_revision','new settlement must advance usage revision')
            lease_rev[target]=p['usage_revision']
            for u in p['uses']:
                key=(target,u['use_id']); prior=lease_uses.get(key)
                if prior and (prior['intent_hash']!=u['intent_hash'] or not _amount_le(prior['used_cost'],u['used_cost'])):fail('lease_use_identity','use identity or cumulative cost changed')
                lease_uses[key]=deepcopy(u)
            leases[target]=deepcopy(out)
        if name=='endpoint.pair.begin':
            pairs[p['pairing_id']]=deepcopy(out['session']);pair_secrets[p['pairing_id']]={'device_code':out['device_code'],'client_nonce':p['client_nonce'],'user_code':out['user_code']}
        if name=='endpoint.pair.approve':
            old=pairs.get(target)
            if old and old['state']!='pending':fail('pairing_terminal','only pending pairing may be decided')
            if target in pair_secrets and p['user_code']!=pair_secrets[target]['user_code']:fail('pairing_identity','approval code does not name original pairing')
            pair_scopes[target]=deepcopy(p['approved_scope']);pairs[target]=deepcopy(out)
        if name=='endpoint.pair.claim' and out['state']=='claimed':
            old=pairs.get(target)
            if target in pair_secrets and any(p[k]!=pair_secrets[target][k] for k in ('device_code','client_nonce')):fail('pairing_identity','claim changes original device secret or nonce')
            if target in pair_scopes and out['endpoint']['approved_scope']!=pair_scopes[target]:fail('pairing_scope','credential exceeds the scope approved by user')
            if old and old['state'] not in ('approved','claimed'):fail('pairing_authority','credential requires previous user approval')
            if target in claims and claims[target]!=out:fail('pairing_claim_once','original pairing cannot issue another credential')
            claims[target]=deepcopy(out);pairs[target]=deepcopy(out['session']);endpoints[out['endpoint']['endpoint_id']]=deepcopy(out['endpoint'])
        if name=='endpoint.revoke':endpoints[target]=deepcopy(out)
        if name=='evaluation.candidate_register':
            if out['candidate_id'] in candidates and candidates[out['candidate_id']]!=out:fail('candidate_immutable','registered candidate identity changed')
            candidates[out['candidate_id']]=deepcopy(out)
        if name=='evaluation.partition_register':
            if out['partition_id'] in partitions and partitions[out['partition_id']]!=out:fail('partition_immutable','registered partition identity changed')
            partitions[out['partition_id']]=deepcopy(out)
        if name=='evaluation.plan_create':
            candidate=candidates.get(out['candidate_id']);part=partitions.get(out['partition_id'])
            if candidate and candidate['candidate_lock']!=out['candidate_lock']:fail('plan_candidate','plan does not use registered candidate lock')
            if part and not set(out['sample_ids'])<=set(part['sample_ids']):fail('plan_samples','plan samples outside registered partition')
            if out['purpose']=='release_confirmation':
                if candidate and candidate['release_kind']!='improvement':fail('plan_kind','formal improvement needs improvement candidate')
                if part and part['split']!='holdout':fail('holdout_required','formal confirmation requires holdout')
                if out['partition_id'] in reservations:fail('holdout_once','holdout partition already permanently reserved')
                reservations[out['partition_id']]=out['plan_id']
                policy=out['improvement_policy'];pid=policy['improvement_id'];key=(pid,out['formal_attempt_index'])
                if pid in policies and policies[pid]!=policy:fail('improvement_policy','policy cannot change after formal attempt')
                if key in attempts:fail('formal_attempt_once','attempt index reused across plans')
                expected_index=1+max([k[1] for k in attempts if k[0]==pid],default=0)
                if out['formal_attempt_index']!=expected_index:fail('formal_attempt_sequence','formal attempt must use next permanent ordinal')
                if out['candidate_id'] not in policy['candidate_scope']:fail('improvement_scope','candidate outside approved process')
                policies[pid]=deepcopy(policy);attempts[key]=out['plan_id']
                if any(e['partition_id']==out['partition_id'] for e in exposures):fail('holdout_exposure','exposed partition cannot become new formal holdout')
            plans[out['plan_id']]=deepcopy(out)
        if name=='evaluation.run':
            plan=plans.get(p['plan_id'])
            if plan and p['plan_digest']!=plan['digest']:fail('run_plan','run must use exact frozen plan')
        if name=='evaluation.feedback_open':
            report=out['report'];plan=plans.get(report['plan_id'])
            if plan and report['plan_digest']!=plan['digest']:fail('report_plan','report does not match frozen plan')
            if plan and report['evidence_class']=='formal' and ('paired_counts' not in report or sum(report['paired_counts'].values())!=len(plan['sample_ids'])):fail('report_denominator','paired counts must include every frozen sample')
            if report['report_id'] in reports and reports[report['report_id']]['digest']!=report['digest']:fail('report_immutable','sealed report digest cannot change')
            reports[report['report_id']]=deepcopy(report);exposures.append(deepcopy(out['exposure']))
        if name=='evaluation.exposure_record':
            exposure=out['exposure'];exposures.append(deepcopy(exposure));affected=[]
            for pid,plan in plans.items():
                if plan['purpose']!='release_confirmation':continue
                part=partitions.get(plan['partition_id'])
                overlapping=part and bool(set(exposure['source_group_ids']) & {g['source_group_id'] for g in part['source_groups']})
                if plan['partition_id']!=exposure['partition_id'] and not overlapping:continue
                related=[r for r in reports.values() if r['plan_id']==pid]
                before=not related or exposure['occurred_at'] is None or any('sealed_at' not in r or _time(exposure['occurred_at'])<=_time(r['sealed_at']) for r in related)
                if before:
                    affected.append(pid)
                    for r in related:r['eligibility']='ineligible'
            if not set(affected)<=set(out['invalidated_plan_ids']):fail('exposure_invalidation','pre-sealing exposure must invalidate related formal plans')
            if affected and any(a['report_id'] in reports and reports[a['report_id']]['plan_id'] in affected and a['state']=='active' for a in approvals.values()) and not out['revocation_job_ids']:fail('exposure_revocation','affected live approvals need durable revocation jobs')
        if name=='evaluation.approve':
            report=reports.get(out['report_id']);candidate=candidates.get(out['candidate_id'])
            if candidate and any(out[k]!=candidate[v] for k,v in [('candidate_digest','digest'),('lock_id','candidate_lock'),('release_kind','release_kind')]):fail('approval_candidate','approval must bind registered candidate')
            if report:
                if report['digest']!=out['report_digest'] or report['candidate_id']!=out['candidate_id'] or report['candidate_digest']!=out['candidate_digest']:fail('approval_report','approval names another report/candidate')
                if not report['sealed'] or report['eligibility']!='eligible' or not report['coverage_complete'] or report['contract_gate']!='pass':fail('approval_evidence','approval requires complete eligible sealed contract evidence')
                if out['release_kind']=='improvement' and (report['evidence_class']!='formal' or any(not report[k]['applicable'] or report[k]['result']!='pass' for k in ('target_attainment','statistical_gate','improvement_gate')) or any(not c['within_limit'] for c in report['category_changes'])):fail('improvement_approval','improvement approval requires all formal gates and categories')
                if out['release_kind']=='compatibility' and report['evidence_class']!='conformance':fail('compatibility_approval','compatibility path requires explicit conformance report')
            else:fail('approval_report','sequence must provide report through recorded trusted feedback')
            approvals[out['approval_id']]=deepcopy(out)
        if name=='evaluation.revoke':approvals[target]=deepcopy(out)
        if name=='evaluation.approval_check':
            uses[out['use_id']]=deepcopy(out);use_times.setdefault(out['use_id'],event['at'])
        if name=='extensions.activate':intentions[p['activation_id']]=deepcopy(p)
        if name=='evaluation.rollout_read' and out['state']=='finished':
            approval=approvals.get(out['approval_id'])
            if approval:
                for target_state in out['targets']:
                    batch=next((b for b in approval['batches'] if target_state['target_id'] in b['targets']),None)
                    if not batch or target_state['observation_elapsed_ms']<batch['observation_window_ms'] or target_state['samples_observed']<batch['minimum_samples']:fail('rollout_observation','finished requires approved observation window and samples')
        if name=='extensions.read' and p['kind']=='activation' and out['phase']=='active':
            initial=out['startup_evidence'];current=out['instance_readiness']['startup_evidence'];old=activations.get(out['activation_id'])
            intended=intentions.get(out['activation_id'])
            if intended and (any(out[k]!=intended[k] for k in ('target_id','old_lock_id','new_lock_id','approval_id')) or out['generation']!=intended['expected_generation']+1):fail('activation_intent','active binding differs from admitted switch')
            if _time(current['checked_at'])>_time(event['at']):fail('activation_readiness','cannot observe readiness before startup check')
            if old and old['startup_evidence']!=initial:fail('activation_history','restart cannot replace original activation evidence')
            if old and old['ready_instance']!=out['ready_instance'] and current['action_kind']!='reopen':fail('activation_restart','new instance must use fresh reopen evidence')
            known=approvals.get(out['approval_id'])
            if known and (known['state']!='active' or _time(known['expires_at'])<=_time(current['checked_at'])):fail('activation_current','current readiness cannot bypass revoked approval')
            for evidence in (initial,current):
                if evidence['kind']=='remote_use':
                    use=uses.get(evidence['use_id'])
                    if use and _time(evidence['checked_at'])<_time(use_times[evidence['use_id']]):fail('approval_window','startup check predates recorded approval use')
                    if not use or any(use[k]!=evidence[k] for k in ('approval_id','approval_revision','target_id','lock_id','instance_id','action_kind','action_id','start_before')):fail('activation_approval','remote startup must match a recorded fixed use')
            activations[out['activation_id']]=deepcopy(out)
    return errors

# Consumer ownership and online settlement extend, rather than replace, the rules above.
_CONFIRMATION_METHODS={'grant.issue','evaluation.approve','task.accept_result','endpoint.pair.approve','grant.lease.allocate'}


def confirmation_ref(command):
    if command['method']=='evaluation.approve':return command['payload']['approval']['confirmation_ref']
    return command['payload']['confirmation_ref']


def confirmation_intent_hash(owner_id, command):
    from validate_transport import digest
    value=deepcopy(command)
    # grant.issue carries the displayed intent hash itself; remove that one derived field.
    if value['method']=='grant.issue':value['payload'].pop('intent_hash',None)
    return digest({'owner_id':owner_id,'consumer_command':value})


def _safe_exchange(exchange):
    req=exchange.get('request',{});res=exchange.get('response',{});spec=METHODS.get(req.get('method'))
    return (spec is not None and not validate(spec['input'],req.get('payload'))
            and ('output' not in res or not validate(spec['output'],res['output'])))


def _confirmation_settlement_exchange(exchange):
    req=exchange['request'];name=req['method'];p=req['payload'];res=exchange['response'];a=exchange['auth'];out=res.get('output');errors=[]
    if not _safe_exchange(exchange):return errors
    def fail(rule,detail):errors.append(rule+': '+detail)
    if name=='confirmation.request':
        command=p['consumer_command'];cref=confirmation_ref(command)
        if cref['owner_id']!=req['target_id'] or cref['id']!=p['confirmation_id'] or cref['revision']!=2:fail('confirmation_owner','original command must bind this owner and anticipated approved revision two')
        if p['intent_hash']!=confirmation_intent_hash(req['target_id'],command):fail('confirmation_intent','intent does not match canonical exact consumer command')
        if command['method']=='grant.issue' and command['payload']['intent_hash']!=p['intent_hash']:fail('confirmation_intent','grant issue carries another normalized intent')
        if _time(p['expires_at'])>_time(command['expires_at']):fail('confirmation_window','confirmation cannot outlast consumer admission')
        decided=res.get('decided_at')
        if decided and not 0<(_time(p['expires_at'])-_time(decided)).total_seconds()<=300:fail('confirmation_window','new confirmation has a finite maximum five-minute window')
    if not out or res.get('stage')=='rejected':return errors
    if name.startswith('confirmation.'):
        command=out['consumer_command'];cref=confirmation_ref(command)
        if (out['owner_id']!=a['logical_service_id'] or out['confirmation_id']!=cref['id'] or cref['owner_id']!=out['owner_id']
                or out['consumer_method']!=command['method'] or out['consumer_command_id']!=command['command_id'] or out['consumer_target_id']!=command['target_id']):fail('confirmation_binding','confirmation must belong to actual consumer owner and exact command')
        if out['intent_hash']!=confirmation_intent_hash(out['owner_id'],command):fail('confirmation_intent','recorded display differs from canonical consumer intent')
        if name=='confirmation.request':
            if out['confirmation_id']!=p['confirmation_id'] or out['consumer_command']!=p['consumer_command'] or out['intent_hash']!=p['intent_hash'] or out['expires_at']!=p['expires_at'] or out['state']!='pending' or out['revision']!=1:fail('confirmation_request','new request must fix pending revision one and original command')
        else:
            if out['confirmation_id']!=req['target_id']:fail('confirmation_binding','query/decision targets another confirmation')
        if name=='confirmation.decide':
            if a.get('actor_kind') not in ('user','maintainer') or 'trusted_user_session_ref' not in a:fail('confirmation_actor','decision requires server-verified trusted user session')
            if out.get('decided_by')!=a['actor_id'] or out.get('trusted_user_session_ref')!=a.get('trusted_user_session_ref'):fail('confirmation_actor','decision must record authenticated user/session')
            if any(p[k]!=out[k] for k in ('challenge','intent_hash','consumer_command_id')):fail('confirmation_decision','challenge and original intent/consumer must match')
            if out['state']!=('approved' if p['decision']=='approve' else 'denied') or out['revision']!=req['expected_revision']+1 or req['expected_revision']!=1:fail('confirmation_decision','pending revision one has exactly one immutable decision')
    if name in _CONFIRMATION_METHODS:
        cref=confirmation_ref(req)
        if cref['owner_id']!=a['logical_service_id'] or cref['revision']!=2:fail('confirmation_owner','business owner must consume its own approved revision')
    if name=='grant.use' and out['decision']=='allowed':
        if a.get('sender_service_id') is not None and p['usage_owner_id']!=a['sender_service_id']:fail('usage_owner','admission must bind authenticated usage owner')
    if name in ('grant.use.settle','grant.use.settlement'):
        if out['use_id']!=req['target_id'] or out['owner_id']!=a['logical_service_id']:fail('use_settlement_binding','settlement belongs to another use/owner')
        if name=='grant.use.settle':
            if a.get('sender_service_id')!=p['usage_owner_id']:fail('usage_owner','only authenticated recorded usage owner may submit settlement')
            for field in ('operation_id','usage_owner_id','grant_refs','usage_revision'):
                if out[field]!=p[field]:fail('use_settlement_binding','settlement changed original identity or usage revision')
            if out['spent_units']!=p['cumulative_units'] or out['spent_cost']!=p['cumulative_cost']:fail('use_settlement_total','spent must equal reported cumulative usage')
            if out['revision']!=req['expected_revision']+1:fail('use_settlement_revision','new settlement must advance record revision once')
            if (out['state']=='final')!=p['final'] or (p['final'] and out.get('closure_ref')!=p['closure_ref']):fail('use_settlement_final','final result must preserve exact closure proof')
            if p['final'] and p['closure_ref']['owner_id']!=p['usage_owner_id']:fail('use_settlement_closure','closure proof must belong to original usage owner')
        for suffix in ('units','cost'):
            values=[out[k+'_'+suffix] for k in ('reserved','spent','held','released')]
            if len({v['unit'] for v in values})!=1:fail('use_settlement_unit','reservation, spent, held and released units must match')
            reserved,spent,held,released=[Decimal(v['amount']) for v in values]
            if reserved!=spent+held+released:fail('use_settlement_conservation','spent plus held plus released must equal original reservation')
            if out['state']=='open' and released!=0:fail('use_settlement_unknown','open/unknown use cannot release unspent reservation')
            if out['state']=='final' and held!=0:fail('use_settlement_final','closed use must settle all held amounts')
    return errors


def _confirmation_settlement_trace(trace):
    errors=[];confirmations={c['confirmation_id']:deepcopy(c) for c in trace.get('confirmations',[])};uses={};settlements={};grant_modes={};commands=set()
    for i,event in enumerate(trace['events']):
        if 'exchange' not in event:continue
        x=event['exchange'];req=x['request'];p=req['payload'];name=req['method'];res=x['response'];out=res.get('output');a=x['auth'];target=req['target_id'];rejected=res.get('stage')=='rejected' or ('error' in res and 'stage' not in res)
        if not _safe_exchange(x):continue
        def fail(rule,detail):errors.append(f'event {i}: {rule}: {detail}')
        if rejected or out is None:continue
        if 'command_id' in req:
            key=(a['tenant_id'],a['logical_service_id'],req['command_id'])
            if key in commands:continue
            commands.add(key)
        if name=='confirmation.request':
            prior=confirmations.get(out['confirmation_id'])
            if prior and (prior['consumer_command']!=out['consumer_command'] or prior['intent_hash']!=out['intent_hash']):fail('confirmation_immutable','cannot replace original confirmation intent')
            confirmations[out['confirmation_id']]=deepcopy(out)
        if name=='confirmation.decide':
            prior=confirmations.get(target)
            if not prior or prior['state']!='pending':fail('confirmation_once','only original pending request may be decided')
            elif any(prior[k]!=out[k] for k in ('owner_id','consumer_command','intent_hash','challenge','expires_at')):fail('confirmation_immutable','decision cannot change displayed intent or challenge')
            if _time(out['expires_at'])<=_time(event['at']):fail('confirmation_expiry','expired confirmation cannot be approved')
            confirmations[target]=deepcopy(out)
        if name in _CONFIRMATION_METHODS:
            cref=confirmation_ref(req);prior=confirmations.get(cref['id'])
            if not prior or prior['state']!='approved':fail('confirmation_consume','business transaction requires an unconsumed approved confirmation')
            else:
                if prior['owner_id']!=a['logical_service_id'] or prior['revision']!=cref['revision'] or prior['consumer_command']!=req or prior['consumer_command_id']!=req['command_id']:fail('confirmation_consumer','approved confirmation must bind this exact original command and owner')
                if prior['intent_hash']!=confirmation_intent_hash(a['logical_service_id'],req):fail('confirmation_intent','business command differs from confirmed canonical intent')
                if _time(prior['expires_at'])<=_time(event['at']):fail('confirmation_expiry','expired confirmation cannot be consumed')
                prior.update(state='consumed',revision=3,consumed_by=req['command_id'],consumed_at=event['at'])
        if name=='confirmation.read':
            prior=confirmations.get(target)
            if prior:
                expected=deepcopy(prior)
                if expected['state'] in ('pending','approved') and _time(event['at'])>=_time(expected['expires_at']):expected['state']='expired'
                if out!=expected:fail('confirmation_observation','read must preserve current decision and atomic consumption')
        if name=='grant.issue':grant_modes[out['grant_id']]=out['policy']['mode']
        if name=='grant.use' and out['decision']=='allowed':uses[out['use_id']]={'request':deepcopy(p),'receipt':deepcopy(out),'owner_id':target}
        if name in ('grant.use.settle','grant.use.settlement'):
            origin=uses.get(target);prior=settlements.get(target)
            if not origin:fail('use_settlement_origin','settlement requires recorded original allowed UseReceipt')
            else:
                q=origin['request'];r=origin['receipt']
                if any(out[k]!=q[v] for k,v in [('operation_id','operation_id'),('usage_owner_id','usage_owner_id'),('grant_refs','grant_refs')]) or out['owner_id']!=origin['owner_id']:fail('use_settlement_binding','settlement changed original usage identity')
                if out['reserved_units']!=r['reserved_units'] or out['reserved_cost']!=r['reserved_cost']:fail('use_settlement_reservation','settlement cannot resize original reservation')
                once=any(grant_modes.get(g['id'])=='once' for g in q['grant_refs'])
                if once and not out['consumed_once']:fail('once_not_refunded','zero final usage cannot return consumed once identity')
            if prior:
                for field in ('operation_id','usage_owner_id','grant_refs','reserved_units','reserved_cost','consumed_once'):
                    if out[field]!=prior[field]:fail('use_settlement_immutable','fixed settlement identity changed')
                if prior['state']=='final' and out!=prior:fail('use_settlement_closed','final settlement cannot reopen or add usage')
                if name=='grant.use.settle':
                    if p['usage_revision']<=prior['usage_revision'] or req['expected_revision']!=prior['revision']:fail('use_settlement_revision','new usage revision and expected record revision must advance')
                    if not _amount_le(prior['spent_units'],out['spent_units']) or not _amount_le(prior['spent_cost'],out['spent_cost']):fail('use_settlement_monotonic','cumulative spent cannot decrease')
            elif name=='grant.use.settle' and req['expected_revision']!=1:fail('use_settlement_revision','initial settlement record starts at revision one')
            settlements[target]=deepcopy(out)
    return errors


_previous_check_exchange=check_exchange
_previous_check_trace_rules=check_trace_rules


def check_exchange(exchange, capabilities):
    return _previous_check_exchange(exchange, capabilities)+_confirmation_settlement_exchange(exchange)


def check_trace_rules(trace):
    return _previous_check_trace_rules(trace)+_confirmation_settlement_trace(trace)
