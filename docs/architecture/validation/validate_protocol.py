#!/usr/bin/env python3
"""Validate frozen draft messages and bounded documentation traces."""
import copy
import json
from pathlib import Path
from protocol.schema import METHODS, validate
from protocol.governance_rules import _exposure_affects_plan, confirmation_intent_hash
from protocol.traces import check_trace

ROOT=Path(__file__).resolve().parents[1] / 'contracts'


def mutate(value, edits):
    for edit in edits:
        target=value
        parts=edit['path'].strip('/').split('/')
        for key in parts[:-1]:target=target[int(key)] if isinstance(target,list) else target[key]
        key=int(parts[-1]) if isinstance(target,list) else parts[-1]
        if edit['op']=='remove':del target[key]
        elif edit['op']=='set':target[key]=edit['value']
        else:raise ValueError('unsupported fixture mutation')
    return value


def main():
    fixtures={}
    covered=set()
    for path in sorted((ROOT/'examples/protocol').glob('[0-9][0-9]-*.json')):
        value=json.loads(path.read_text());fixtures[path.name]=value
        errors=check_trace(value)
        if errors:raise AssertionError(f'{path.name}: '+ '\n'.join(errors))
        covered.update(e['exchange']['request']['method'] for e in value['events'] if 'exchange' in e)
    cases=json.loads((ROOT/'examples/protocol/invalid-mutations.json').read_text())
    for case in cases:
        errors=check_trace(mutate(copy.deepcopy(fixtures[case['fixture']]),case['edits']))
        if not any(case['expect']+':' in e for e in errors):
            raise AssertionError(f"{case['name']}: expected {case['expect']}, got {errors}")
    # A redundant read can remain unknown after independent evidence satisfies
    # all requirements. Its exact declared read-only class is essential: names
    # or missing declarations must retain the possible-business-effect gate.
    read_unknown=copy.deepcopy(fixtures['16-active-to-result.json'])
    cap=copy.deepcopy(read_unknown['capabilities'][0])
    cap['capability_ref']['id']='capability_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa'
    cap['capability_ref']['digest']='sha256:'+'a'*64
    cap['binding_ref']['binding_id']='binding_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa'
    cap['effect_class']='read_only'
    read_unknown['capabilities'].append(cap)
    invoke=copy.deepcopy(read_unknown['events'][3])
    call=invoke['exchange'];payload=call['request']['payload']
    call['request']['command_id']=call['response']['command_id']='execution_invoke_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa'
    payload['operation_id']=call['response']['output']['operation_id']='operation_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa'
    payload['capability_ref']=copy.deepcopy(cap['capability_ref'])
    payload['binding_ref']=copy.deepcopy(cap['binding_ref'])
    observed=copy.deepcopy(read_unknown['events'][4])
    observed['exchange']['request']['target_id']=payload['operation_id']
    original=observed['exchange']['response']['output']
    original['operation_id']=payload['operation_id'];original['effect']='unknown';original['may_apply_later']='unknown'
    read_unknown['events'][5:5]=[invoke,observed]
    errors=check_trace(read_unknown)
    if errors:raise AssertionError('redundant declared read-only unknown must preserve a proven result: '+str(errors))
    for classification in (None,'no_idempotency_guarantee','target_idempotent'):
        possible_write=copy.deepcopy(read_unknown)
        if classification is None:possible_write['capabilities'][-1].pop('effect_class')
        else:possible_write['capabilities'][-1]['effect_class']=classification
        errors=check_trace(possible_write)
        if not any('result_effect:' in error for error in errors):
            raise AssertionError('unknown possible business effect must prevent success')
    missing_evidence=copy.deepcopy(read_unknown)
    missing_evidence['events'][-1]['exchange']['response']['output']['result']['condition_results'][0]['requirement_id']='requirement_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa'
    errors=check_trace(missing_evidence)
    if not any('result_conditions:' in error for error in errors):
        raise AssertionError('read-only exception must not replace required evidence')
    reclassify=copy.deepcopy(fixtures['16-active-to-result.json'])
    reclassify['capabilities'].append(copy.deepcopy(cap))
    reclassify['events'][4]['exchange']['response']['output']['effect']='unknown'
    reclassify['events'][4]['exchange']['response']['output']['may_apply_later']='unknown'
    denied=copy.deepcopy(invoke)
    original_id=reclassify['events'][3]['exchange']['request']['payload']['operation_id']
    denied['exchange']['request']['payload']['operation_id']=original_id
    denied['exchange']['response'].pop('output')
    denied['exchange']['response']['stage']='rejected'
    denied['exchange']['response']['error']={'code':'idempotency_conflict','message':'Original write identity is fixed','retry':'query_original'}
    reclassify['events'].insert(5,denied)
    errors=check_trace(reclassify)
    if not any('result_effect:' in error for error in errors):
        raise AssertionError('a rejected read-only invoke must not reclassify an original unknown write')
    # An exposure may affect more than the former 100-ID response bound. Its receipt
    # names one durable scan, while current eligibility is checked from original facts.
    example=fixtures['35-improvement-exposure.json']['events']
    plan=example[2]['exchange']['response']['output']
    partition=example[1]['exchange']['response']['output']
    exposure_result=example[8]['exchange']['response']['output']
    if validate('ExposureRecordOutput',exposure_result):
        raise AssertionError('exposure receipt must name its single impact job')
    affected=0
    for index in range(101):
        candidate_plan=copy.deepcopy(plan);candidate_partition=copy.deepcopy(partition)
        candidate_plan['partition_id']=candidate_partition['partition_id']=f'partition_{index:032x}'
        if _exposure_affects_plan(exposure_result['exposure'],candidate_plan,candidate_partition,[]):affected+=1
    if affected!=101 or set(exposure_result)!={'exposure','impact_job_id'}:
        raise AssertionError('one impact job must cover more than 100 affected plans without truncating a receipt list')
    overrun=copy.deepcopy(fixtures['23-cross-orchestrator-budget.json'])
    for path in ((4,'response','output','receiver','final_usage'),(4,'response','output','receiver','closure','final_usage'),
                 (5,'request','payload','closure','final_usage'),(5,'response','output','final_usage'),
                 (5,'response','output','closure','final_usage')):
        event_index,side,*keys=path
        node=overrun['events'][event_index]['exchange'][side]
        for key in keys:node=node[key]
        node[0]['amount']='7'
    missing_cause=check_trace(overrun)
    if not any('allocation_incident:' in error for error in missing_cause):
        raise AssertionError('over-allocation bill must carry attributed incident or explicit pending cause')
    overrun['events'][5]['exchange']['response']['output']['incident_causes']=['provider_bound_breach']
    overrun['events'][5]['exchange']['response']['output']['incident_pending']=False
    overrun_errors=check_trace(overrun)
    if overrun_errors:raise AssertionError('constructed over-limit final bill must remain recordable: '+str(overrun_errors))
    receiver_breach=copy.deepcopy(overrun)
    receiver_breach['events'][5]['exchange']['response']['output']['incident_causes']=['receiver_allocation_breach']
    receiver_errors=check_trace(receiver_breach)
    if receiver_errors:raise AssertionError('multiple compliant charges can exceed receiver allocation without provider breach: '+str(receiver_errors))
    concurrent_causes=copy.deepcopy(overrun)
    concurrent_causes['events'][5]['exchange']['response']['output']['incident_causes']=['provider_bound_breach','receiver_allocation_breach']
    both_errors=check_trace(concurrent_causes)
    if both_errors:raise AssertionError('independent provider and receiver breaches may both hold: '+str(both_errors))
    pending_cause=copy.deepcopy(overrun)
    pending_cause['events'][5]['exchange']['response']['output']['incident_causes']=[]
    pending_cause['events'][5]['exchange']['response']['output']['incident_pending']=True
    pending_errors=check_trace(pending_cause)
    if pending_errors:raise AssertionError('trusted actual bill may be recorded while breach cause remains pending: '+str(pending_errors))
    partial_cause=copy.deepcopy(overrun)
    partial_cause['events'][5]['exchange']['response']['output']['incident_pending']=True
    partial_errors=check_trace(partial_cause)
    if partial_errors:raise AssertionError('one proven cause may coexist with another cause still under investigation: '+str(partial_errors))
    late=copy.deepcopy(fixtures['23-cross-orchestrator-budget.json'])
    receiver_event=copy.deepcopy(late['events'][4]);settle_event=copy.deepcopy(late['events'][5])
    receiver_event['at']=settle_event['at']='2026-09-26T00:00:01Z'
    receiver_event['exchange']['response']['observed_at']='2026-09-26T00:00:01Z'
    settle_event['exchange']['response']['decided_at']='2026-09-26T00:00:01Z'
    receiver=receiver_event['exchange']['response']['output']['receiver']
    receiver['revision']=4;receiver['final_usage'][0]['amount']='4'
    receiver['closure']['usage_revision']=2;receiver['closure']['final_usage'][0]['amount']='4'
    receiver['closure']['proof_ref']['content_id']='runtime_closure_ffffffffffffffffffffffffffffffff'
    receiver['closure']['proof_ref']['hash']='sha256:'+'f'*64
    # The correction is durable at the receiver. No lossy Change notification
    # is present: the parent recovery scan reads its settled record and the
    # receiver's current closure, then accepts the durable billing wakeup.
    owner_read=copy.deepcopy(late['events'][1])
    owner_read['at']=owner_read['exchange']['response']['observed_at']='2026-09-26T00:00:01Z'
    owner_read['exchange']['response']['output']['allocation']=copy.deepcopy(late['events'][5]['exchange']['response']['output'])
    settle=settle_event['exchange']
    settle['request']['command_id']=settle['response']['command_id']='budget_settle_ffffffffffffffffffffffffffffffff'
    settle['request']['expected_revision']=2
    settle['request']['payload']['closure']=copy.deepcopy(receiver['closure'])
    allocation=settle['response']['output']
    allocation['revision']=3;allocation['final_usage'][0]['amount']='4'
    allocation['closure']=copy.deepcopy(receiver['closure'])
    original=late['events'][0]['exchange']['response']['output']
    wakeup={'at':'2026-09-26T00:00:01Z','exchange':{
        'auth':{'tenant_id':late['events'][0]['exchange']['auth']['tenant_id'],
                'actor_id':late['events'][0]['exchange']['auth']['actor_id'],
                'logical_service_id':original['owner_id'],'sender_service_id':original['receiver_id']},
        'request':{'command_id':'task_billing_reconcile_cccccccccccccccccccccccccccccccc',
                   'method':'task.billing_reconcile','target_id':original['parent_task_id'],
                   'expires_at':'2026-09-26T00:10:00Z',
                   'payload':{'source_kind':'budget_allocation','source_id':original['allocation_id'],
                              'usage_revision':2,'usage_digest':'sha256:'+'c'*64}},
        'response':{'command_id':'task_billing_reconcile_cccccccccccccccccccccccccccccccc',
                    'stage':'applied','decided_at':'2026-09-26T00:00:01Z',
                    'output':{'job_id':'billing_job_cccccccccccccccccccccccccccccccc',
                              'resource_id':original['parent_task_id']}}}}
    late['events'].extend((owner_read,receiver_event,wakeup,settle_event))
    late_errors=check_trace(late)
    if late_errors:raise AssertionError('settled allocation must accept only a newer cumulative correction: '+str(late_errors))
    stale=copy.deepcopy(late)
    stale['events'][-1]['exchange']['request']['payload']['closure']['usage_revision']=1
    stale['events'][-1]['exchange']['response']['output']['closure']['usage_revision']=1
    stale_errors=check_trace(stale)
    if not any('allocation_correction:' in error for error in stale_errors):
        raise AssertionError('stale allocation correction must be rejected: '+str(stale_errors))
    replay=copy.deepcopy(late)
    original_receipt=copy.deepcopy(replay['events'][5]);original_receipt['at']='2026-09-26T00:00:02Z'
    replay['events'].append(original_receipt)
    replay_errors=check_trace(replay)
    if replay_errors:raise AssertionError('replaying the original settlement must retain its old receipt after later correction: '+str(replay_errors))
    listing=copy.deepcopy(fixtures['20-budget-allocation.json'])
    first=listing['events'][4]['exchange']['response']['output'];first_item=first['items'][0]
    tied=copy.deepcopy(first_item);tied['task']['task_id']='runtime_task_3f19394d42a7706fc18f2a1683b8745c'
    older=copy.deepcopy(first_item);older['created_at']='2026-09-25T23:59:59Z';older['task']['task_id']='runtime_task_4f19394d42a7706fc18f2a1683b8745c'
    first['items']=[first_item,tied,older]
    first['next_cursor']={'upper_bound':first['upper_bound'],'last_created_at':older['created_at'],'last_task_id':older['task']['task_id']}
    continuation=copy.deepcopy(listing['events'][4])
    continuation['exchange']['request']['payload']['cursor']=copy.deepcopy(first['next_cursor'])
    next_item=copy.deepcopy(first_item);next_item['created_at']='2026-09-25T23:59:58Z';next_item['task']['task_id']='runtime_task_5f19394d42a7706fc18f2a1683b8745c'
    continuation['exchange']['response']['output']['items']=[next_item]
    continuation['exchange']['response']['output'].pop('next_cursor',None)
    listing['events'].append(continuation)
    list_errors=check_trace(listing)
    if list_errors:raise AssertionError('descending task list must paginate after equal-time IDs and older timestamps: '+str(list_errors))
    ascending=copy.deepcopy(listing)
    ascending['events'][4]['exchange']['response']['output']['items'].reverse()
    ascending_errors=check_trace(ascending)
    if not any('task_list_order:' in error for error in ascending_errors):
        raise AssertionError('ascending task list must violate the documented descending order')
    out_of_order=copy.deepcopy(fixtures['60-task-billing-reconcile.json'])
    first_notice=out_of_order['events'][7]['exchange'];second_notice=out_of_order['events'][8]['exchange']
    # The original source has already published r2 when its r1 outbox first
    # reaches the Orchestrator; rereading the source may therefore settle r2.
    decision=out_of_order['events'][1]['exchange']
    latest=copy.deepcopy(decision['response']['output']);latest['revision']=4
    latest['model_call']['usage'][0]['amount']='30'
    source_read={'at':'2026-09-26T00:00:01Z','exchange':{
        'auth':copy.deepcopy(decision['auth']),
        'request':{'method':'brain.get','target_id':latest['decision_id'],'payload':{}},
        'response':{'output':latest,'observed_at':'2026-09-26T00:00:01Z'}}}
    out_of_order['events'].insert(7,source_read)
    first_notice['request']['payload']['usage_revision']=3
    second_notice['request']['payload']['usage_revision']=4
    second_notice['request']['payload']['usage_digest']='sha256:'+'b'*64
    old_notice=copy.deepcopy(out_of_order['events'][8]);old_notice['at']='2026-09-26T00:10:02Z'
    old_notice['exchange']['request']['command_id']='task_billing_reconcile_dddddddddddddddddddddddddddddddd'
    old_notice['exchange']['request']['expires_at']='2026-09-26T00:20:00Z'
    old_notice['exchange']['response']['command_id']=old_notice['exchange']['request']['command_id']
    old_notice['exchange']['response']['decided_at']=old_notice['at']
    out_of_order['events'].append(old_notice)
    order_errors=check_trace(out_of_order)
    if order_errors:raise AssertionError('newer billing notice must not make a valid older revision fail or create another job: '+str(order_errors))
    correction=copy.deepcopy(fixtures['50-online-use-settlement.json'])
    corrected=correction['events'][5]['exchange']
    corrected['request']['command_id']='command_ffffffffffffffffffffffffffffffff'
    corrected['response']['command_id']=corrected['request']['command_id']
    corrected['request']['expected_revision']=3
    corrected['request']['payload']['usage_revision']=3
    corrected['request']['payload']['cumulative_cost']['amount']='5'
    corrected['request']['payload']['closure_ref']['revision']=2
    current=corrected['response']['output']
    current['revision']=4;current['usage_revision']=3;current['spent_cost']['amount']='5'
    current['closure_ref']['revision']=2
    correction['events'][6]['exchange']['response']['output']=copy.deepcopy(current)
    correction_errors=check_trace(correction)
    if correction_errors:raise AssertionError('final bill correction must retain original use and released reserve: '+str(correction_errors))
    estimate=copy.deepcopy(fixtures['50-online-use-settlement.json'])
    def set_cost_mode(node):
        if isinstance(node,dict):
            if 'cost_bound' in node:node['cost_bound']='estimate'
            for value in node.values():set_cost_mode(value)
        elif isinstance(node,list):
            for value in node:set_cost_mode(value)
    for event in estimate['events']:
        if 'exchange' in event:set_cost_mode(event['exchange'])
    hard_errors=check_trace(estimate)
    if not any('estimate_hard_grant:' in error for error in hard_errors):
        raise AssertionError('estimate use must not consume a Grant with a hard cost limit')
    grant=estimate['events'][0]['exchange']
    grant['request']['payload']['policy']['limits']=[limit for limit in grant['request']['payload']['policy']['limits'] if limit['unit']!='USD']
    grant['response']['output']['policy']['limits']=copy.deepcopy(grant['request']['payload']['policy']['limits'])
    intent_hash=confirmation_intent_hash(grant['request']['target_id'],grant['request'])
    grant['request']['payload']['intent_hash']=grant['response']['output']['intent_hash']=intent_hash
    estimate['confirmations'][0]['consumer_command']=copy.deepcopy(grant['request'])
    estimate['confirmations'][0]['intent_hash']=intent_hash
    for index in (4,5,6):
        exchange=estimate['events'][index]['exchange']
        if index in (4,5):exchange['request']['payload']['cumulative_cost']['amount']='6'
        exchange['response']['output']['spent_cost']['amount']='6'
        exchange['response']['output']['released_cost']['amount']='0'
    estimate_errors=check_trace(estimate)
    if estimate_errors:raise AssertionError('constructed estimate overrun must remain recordable without a hard Grant cost limit: '+str(estimate_errors))
    frozen={name for name,spec in METHODS.items() if spec['status']=='frozen-draft'}
    if covered!=frozen:raise AssertionError(f'method coverage mismatch: {frozen-covered}, {covered-frozen}')
    print(f'PASS: {len(fixtures)} valid traces; {len(cases)} invalid mutations rejected for their stated rule; {len(covered)} frozen methods exercised')
    print('Read-only completion: one redundant unknown read with an exact declaration preserves proven success; missing/write declarations, absent required evidence and rejected reclassification cannot bypass the effect gate')
    print('Exposure boundary: one impact job covers a constructed 101-plan overlap; no bounded affected-ID response list')
    print('Budget boundary: a 7-unit actual bill against a 5-unit allocation remains recordable with provider, receiver, both, or pending incident cause; proof remains a runtime prerequisite')
    print('Budget correction: a parent recovery scan reads settled and closed records, receives one durable billing wakeup, and settles a newer bill; stale revision is rejected and original receipt replay is stable')
    print('Billing wakeup: source r2 is visible before r1 notice; r1, r2, then r1 retains one job, and same-revision digest conflict is rejected')
    print('Task listing: equal-time IDs and older timestamps paginate in documented descending-time order; reverse order is rejected')
    print('Use boundary: a constructed final-to-final cost correction keeps the original use and released reserve; bill authenticity remains a runtime prerequisite')
    print('Estimate boundary: a hard Grant cost limit rejects estimate; without it, constructed actual cost 6 exceeds reserved 4 without dropping the bill')
    print(f'Registry: {len(METHODS)} methods, {len(METHODS)-len(frozen)} reserved; protocol is an unpublished draft profile')
    print('Scope: structure and recorded semantic relations only; no authentication, service, concurrency, durability or interoperability runtime tested')


if __name__=='__main__':main()
