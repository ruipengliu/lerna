#!/usr/bin/env python3
"""Static schema/provenance and bounded-rule counterexamples; no runtime proof."""
from copy import deepcopy as cp
import json
from pathlib import Path
import sys
sys.dont_write_bytecode = True
from derive import (BASE, HERE, ROOT, VARIANTS, NotEligible, baseline_inventory, content_by_ref,
                    digest, encoded, ident, inputs, pointer_value, read, rule_candidate, v, write)
from protocol.schema import validate
from protocol.runtime_rules import check_plan_installation, inspect_plan_materialization

checks = []


def check(condition, label):
    if not condition:
        raise AssertionError(label)
    checks.append(label)


def walk(value):
    if isinstance(value, dict):
        yield value
        for child in value.values():
            yield from walk(child)
    elif isinstance(value, list):
        for child in value:
            yield from walk(child)


def schema(name, value, label):
    errors = validate(name, value)
    check(not errors, label + ': ' + '; '.join(errors))


def rebind_changed_bodies(inp):
    """Model newly supplied synthetic bytes for semantic rejection cases.

    A separate integrity case deliberately keeps the old reference. These new
    references are only in-memory counterexample inputs, not owner commits.
    """
    c = inp['context']
    pairs = [(inp['goal'], c['goal_ref'])] + list(zip(inp['facts'], [f['content_ref'] for f in c['facts']]))
    for body, ref in pairs:
        raw = encoded(body)
        if digest(raw) == ref['hash'] and len(raw) == ref['byte_length']:
            continue
        old = cp(ref)
        new = {**old, 'content_id': ident('content', digest(raw)), 'version': 1,
               'hash': digest(raw), 'byte_length': len(raw)}
        if c['goal_ref'] == old:
            c['goal_ref'] = cp(new)
        for fact in c['facts']:
            if fact['content_ref'] == old:
                fact['content_ref'] = cp(new)
        c['input_manifest'] = [cp(new) if x == old else x for x in c['input_manifest']]


def counterexample(name, inp, rule, mutate, expected_reason, expected_route, plan_id=None, rebind=True):
    changed = cp(inp)
    mutate(changed)
    if rebind:
        rebind_changed_bodies(changed)
    try:
        rule_candidate(changed, rule, plan_id)
    except NotEligible as error:
        check((error.reason, error.route) == (expected_reason, expected_route), name)
        return {'case': name, 'expected_reason': expected_reason, 'expected_route': expected_route,
                'actual_reason': error.reason, 'actual_route': error.route, 'passed': True}
    raise AssertionError(name + ': unsafe rule accepted counterexample')


def main():
    checks.clear()
    saved_inventory = read(HERE / 'baseline-digests.json')['files']
    check(saved_inventory == baseline_inventory(), 'protected baseline data digests unchanged')
    summaries, zero_ids = [], set()
    for variant, (slug, zero_phases) in VARIANTS.items():
        p = read(HERE / (variant + '.json'))
        b = read(BASE / slug / 'scenario.json')
        stats = p['statistics']
        refs = {encoded(item['ref']): item for item in b['contents'].values()}
        # Verify every baseline body, not just the selected scalar byte totals.
        for item in b['contents'].values():
            raw = (BASE / item['body_file']).read_bytes()
            check(digest(raw) == item['ref']['hash'] and len(raw) == item['ref']['byte_length'], variant + ': baseline body ' + item['body_file'])
        for node in walk(p):
            if set(('file', 'pointer', 'value_digest')) <= set(node):
                selected = pointer_value(read((HERE / node['file']).resolve()), node['pointer'])
                check(digest(encoded(selected)) == node['value_digest'], variant + ': source ' + node['pointer'])
        d1 = v(b, 'D1:DecisionRecord')
        consumed = v(b, 'D1:consumption')
        transition = b['task_revision_log'][2]
        check(consumed['decision_id'] == d1['decision_id'] and transition['goal_revision'] == 2 and
              d1['proposal']['requirements_proposal']['base_goal_revision'] == 1,
              variant + ': D1 consumed before g2')
        expected_requirements = d1['proposal']['requirements_proposal']['requirements']
        model_ids, model_generations, model_bytes = [], 0, 0
        for entry in p['entries']:
            phase = entry['phase']
            model_generations += entry['model_generation_count']
            model_bytes += entry['model_input_body_bytes']
            if entry['mode'] != 'deterministic':
                model_ids.append(entry['model_call_id'])
                original_call = v(b, phase + ':ModelCall')
                check(entry['model_call_id'] == original_call['model_call_id'], variant + ': retained physical call ' + phase)
                continue
            req, rec = entry['decision_request'], entry['decision_record']
            check(phase in zero_phases, variant + ': only selected phases replaced')
            check(req['decision_id'] == rec['decision_id'] and req['decision_id'] not in zero_ids and
                  req['decision_id'] not in [v(b, gen['label'] + ':DecisionRecord')['decision_id'] for gen in b['generations']],
                  variant + ': independent zero-model Decision identity ' + phase)
            zero_ids.add(req['decision_id'])
            check('model_call' not in rec and entry['model_calls'] == [] and entry['model_usage'] == [] and
                  entry['model_generation_count'] == entry['model_input_body_bytes'] == entry['model_fee']['amount'] == 0,
                  variant + ': no ModelCall or model fee ' + phase)
            schema('DecisionRequest', req, variant + ': request schema ' + phase)
            schema('DecisionRecord', rec, variant + ': record schema ' + phase)
            schema('Proposal', rec['proposal'], variant + ': proposal schema ' + phase)
            check(entry['decision_input']['input_digest'] == digest(encoded(req)), variant + ': fixed Decision input ' + phase)
            inp = inputs(b, phase)
            plan, plan_ref = None, None
            for body in entry['new_bodies']:
                raw = (HERE / body['body_file']).read_bytes()
                check(digest(raw) == body['ref']['hash'] and len(raw) == body['ref']['byte_length'], variant + ': new body bytes ' + body['body_file'])
                refs[encoded(body['ref'])] = body
                body_value = json.loads(raw)
                if body_value.get('schema_version') == 'brain-context/1':
                    inp['context'] = body_value
                if body_value.get('schema_version') == 'brain-plan/1':
                    plan, plan_ref = body_value, body['ref']
            c = inp['context']
            check(c['goal_revision'] == 2 and c['snapshot_revision'] == req['snapshot_revision'] == rec['snapshot_revision'] and
                  c['snapshot_revision'] > consumed['snapshot_revision'] and c['requirements'] == expected_requirements,
                  variant + ': fresh g2 snapshot with accepted requirements ' + phase)
            schema('BrainContext', c, variant + ': context schema ' + phase)
            check(c['task_ref']['task_id'] == req['task_id'] == p['baseline_task_id'] and
                  c['goal_ref'] == b['contents']['goal']['ref'], variant + ': correct task and goal binding ' + phase)
            check(entry['decision_input']['actual_processing_manifest'] == [req['context_ref']] + c['input_manifest'],
                  variant + ': full rule input source closure ' + phase)
            rebuilt, rebuilt_plan = rule_candidate(inp, entry['rule'], plan['plan_id'] if plan else None)
            if plan:
                rebuilt_plan['source_refs'] = entry['decision_input']['actual_processing_manifest']
                rebuilt['plan_delta'] = {'base_plan_ref': None, 'next_plan_ref': plan_ref}
                check(rebuilt_plan == plan, variant + ': plan reconstructed from fresh facts')
                check(plan['plan_id'] != b['contents']['plan']['body']['plan_id'] and plan['revision'] == 1,
                      variant + ': new plan identity and initial revision')
                check(not check_plan_installation(rec['proposal'], plan, plan_ref, c['task_ref'], 2),
                      variant + ': plan installation contract')
                handles_ref = next(body['ref'] for body in entry['new_bodies'] if 'allocated-handles' in body['body_file'])
                handle_body = json.loads((HERE / refs[encoded(handles_ref)]['body_file']).read_text())
                check(handle_body['plan_id'] == plan['plan_id'] and handles_ref in c['input_manifest'],
                      variant + ': new allocated plan handle is in current context')
            check(rebuilt == rec['proposal'], variant + ': Proposal is rule output ' + phase)
            check('requirements_proposal' not in rec['proposal'], variant + ': no repeated goal completion ' + phase)
            # Contract-level equivalence is independently compared with the baseline,
            # ignoring rationale, evidence strengthening and new object identities.
            candidate_actions = [s['action_template'] for s in plan['steps']] if plan else rec['proposal']['actions']
            original_actions = [s['action_template'] for s in b['contents']['plan']['body']['steps']] if plan else v(b, phase + ':Proposal')['actions']
            fields = ('type', 'purpose', 'requirement_refs', 'capability_ref', 'binding_ref', 'arguments')
            projection = lambda aa: [{k: a[k] for k in fields} for a in aa]
            check(projection(candidate_actions) == projection(original_actions), variant + ': equivalent bounded action arguments ' + phase)
            for item in entry['decision_input']['actual_processing_manifest']:
                check(encoded(item) in refs, variant + ': rule source resolves ' + phase)
        check(model_generations == len(model_ids) == len(set(model_ids)) == stats['candidate_model_generations'],
              variant + ': unique physical ModelCall counting')
        check(model_bytes == stats['candidate_model_input_body_bytes'], variant + ': retained input byte sum')
        check(stats['baseline_model_generations'] == len(b['generations']) + int(slug == 'report'), variant + ': baseline physical count')
        check(stats['decision_count'] == len(b['generations']), variant + ': Decision count distinct from ModelCall count')
        check(sum(row['baseline_model_input_body_bytes'] for row in stats['phases']) == stats['baseline_model_input_body_bytes'] ==
              b['statistics']['model_input_body_bytes'] + b['statistics']['assessment_input_body_bytes'], variant + ': byte metric matches original statistics')
        check(stats['eliminated_model_input_body_bytes'] == stats['baseline_model_input_body_bytes'] - model_bytes,
              variant + ': eliminated byte arithmetic')
        if slug == 'report':
            check([(e['phase'], e['model_generation_count']) for e in p['entries'] if e['phase'] in ('D4', 'O7')] == [('D4', 1), ('O7', 1)],
                  variant + ': synthesis and shared assessment retained once each')
        summaries.append({'variant': variant, 'model_generations': stats['candidate_model_generations'],
                          'decision_count': stats['decision_count'], 'model_input_body_bytes': model_bytes})

    # Meaningful rejection cases execute the same bounded selector over modified
    # inputs; none is a live authorization or live provider experiment.
    bt = read(BASE / 'bluetooth-off/scenario.json')
    report = read(BASE / 'report/scenario.json')
    observe, enable = inputs(bt, 'D2'), inputs(bt, 'D3')
    search, fetch = inputs(report, 'D2'), inputs(report, 'D3')
    plan_id = ident('plan', 'counterexample-only')
    bad = []
    bad.append(counterexample('missing device field', observe, 'bluetooth-observe/1', lambda i: i['goal'].pop('device_id'), 'device_or_desired_missing', 'repair_context'))
    bad.append(counterexample('old g1 context', observe, 'bluetooth-observe/1', lambda i: i['context'].update(goal_revision=1), 'stale_goal', 'rebuild_current_snapshot'))
    bad.append(counterexample('changed device binding', observe, 'bluetooth-observe/1', lambda i: i['gate'].update(device_id=ident('device', 'other')), 'stale_device_binding', 'rebuild_current_snapshot'))
    bad.append(counterexample('changed capability binding', observe, 'bluetooth-observe/1', lambda i: i['gate']['capability_bindings'][0]['binding_ref'].update(revision=2), 'stale_capability_binding', 'rebuild_current_snapshot'))
    bad.append(counterexample('missing capability', observe, 'bluetooth-observe/1', lambda i: (i['context'].update(capabilities=[]), i['gate'].update(capability_bindings=[])), 'capability_missing_or_ambiguous', 'repair_context'))
    bad.append(counterexample('stale observation', enable, 'bluetooth-enable-plan/1', lambda i: i['facts'][0].update(observed_at='2026-09-28T00:00:00Z'), 'fact_expired', 'refresh_observation', plan_id))
    bad.append(counterexample('observation from another device', enable, 'bluetooth-enable-plan/1', lambda i: i['facts'][0].update(device_id=ident('device', 'other')), 'observation_device_mismatch', 'refresh_observation', plan_id))
    bad.append(counterexample('unknown original effect', enable, 'bluetooth-enable-plan/1', lambda i: i['gate'].update(effect_reconciliation_required=True), 'effect_unknown', 'reconcile_original_effect', plan_id))
    bad.append(counterexample('missing processing authorization', search, 'report-search/1', lambda i: i['gate'].update(input_uses_allowed=False), 'authorization_unavailable', 'hold_authorization'))
    bad.append(counterexample('missing report product version', search, 'report-search/1', lambda i: i['goal']['products'][0].pop('version'), 'report_parameters_incomplete', 'repair_context'))
    bad.append(counterexample('unsupported dimension needs reasoning', search, 'report-search/1', lambda i: i['goal']['dimensions'].append('可靠性'), 'unsupported_goal_shape', 'new_model_decision'))
    bad.append(counterexample('missing report destination', search, 'report-search/1', lambda i: i['goal'].pop('relative_path'), 'report_destination_missing', 'repair_context'))
    bad.append(counterexample('search materials insufficient', fetch, 'report-fetch-all/1', lambda i: i['facts'][0].update(hits=[]), 'search_materials_insufficient_or_overbound', 'repair_context'))
    bad.append(counterexample('too many hits for bounded rule', fetch, 'report-fetch-all/1', lambda i: i['facts'][0]['hits'].append(cp(i['facts'][0]['hits'][0])), 'search_materials_insufficient_or_overbound', 'repair_context'))
    bad.append(counterexample('unapproved source host', fetch, 'report-fetch-all/1', lambda i: i['facts'][0]['hits'][0].update(url='https://other.example/1.0/a'), 'search_hit_outside_fixed_scope', 'repair_context'))
    bad.append(counterexample('old report product version', fetch, 'report-fetch-all/1', lambda i: i['facts'][0].update(version='0.9'), 'search_goal_mismatch', 'rebuild_current_snapshot'))
    bad.append(counterexample('stale search facts', fetch, 'report-fetch-all/1', lambda i: i['facts'][0].update(retrieved_at='2026-09-28T00:00:00Z'), 'fact_expired', 'refresh_observation'))
    bad.append(counterexample('cannot replace synthesis', inputs(report, 'D4'), 'report-synthesis/1', lambda i: None, 'semantic_generation_required', 'new_model_decision'))
    bad.append(counterexample('cannot replace semantic assessment', inputs(report, 'D4'), 'report-semantic-assessment/1', lambda i: None, 'semantic_generation_required', 'new_model_decision'))
    bad.append(counterexample('same kind but changed rule', observe, 'bluetooth-observe/1', lambda i: i['context']['requirements'][0]['rule_ref'].update(version='2.0.0'), 'unsupported_requirements', 'new_model_decision'))
    bad.append(counterexample('same kind but changed source', observe, 'bluetooth-observe/1', lambda i: i['context']['requirements'][0]['source_ref'].update(version=2), 'unsupported_requirements', 'new_model_decision'))
    bad.append(counterexample('additional connection preservation constraint', observe, 'bluetooth-observe/1', lambda i: i['goal'].update(user_text=i['goal']['user_text'] + '，不影响当前连接'), 'unsupported_goal_shape', 'new_model_decision'))
    bad.append(counterexample('changed capability version with updated current binding', observe, 'bluetooth-observe/1', lambda i: (i['context']['capabilities'][0]['capability_ref'].update(version='2.0.0'), i['gate']['capability_bindings'][0]['capability_ref'].update(version='2.0.0')), 'unsupported_capability_contract', 'new_model_decision'))
    bad.append(counterexample('changed capability semantics with same name', observe, 'bluetooth-observe/1', lambda i: i['context']['capabilities'][0].update(effect_class='target_idempotent'), 'unsupported_capability_contract', 'new_model_decision'))
    bad.append(counterexample('two searches exceed request max_actions=1', search, 'report-search/1', lambda i: i['request_limits'].update(max_actions=1), 'action_bound_exceeded', 'repair_context'))
    bad.append(counterexample('malformed URL port', fetch, 'report-fetch-all/1', lambda i: i['facts'][0]['hits'][0].update(url='https://atlas.example:invalid/1.0/deployment'), 'search_url_invalid', 'repair_context'))
    bad.append(counterexample('changed body with original ContentRef', enable, 'bluetooth-enable-plan/1', lambda i: i['facts'][0].update(state_version=9), 'content_digest_mismatch', 'repair_context', plan_id, rebind=False))

    # Poison old proposals to prove that rebuilt candidates do not read their
    # invalidated output. The original fixture files remain untouched.
    poisoned = cp(bt)
    for phase in ('D1', 'D2', 'D3'):
        poisoned['objects'][phase + ':Proposal']['value'] = {'do_not_reuse': True}
        poisoned['objects'][phase + ':DecisionRecord']['value']['proposal'] = {'do_not_reuse': True}
    check(rule_candidate(inputs(poisoned, 'D2'), 'bluetooth-observe/1') == rule_candidate(observe, 'bluetooth-observe/1'), 'invalidated D1/D2 proposal poisoning has no effect')
    check(rule_candidate(inputs(poisoned, 'D3'), 'bluetooth-enable-plan/1', plan_id) == rule_candidate(enable, 'bluetooth-enable-plan/1', plan_id), 'old D3 proposal poisoning has no effect')

    # Plan dependency checks use the repository's existing contract inspector.
    pp = read(HERE / 'bluetooth-off-1-call.json')
    ee = next(e for e in pp['entries'] if e['phase'] == 'D3')
    plan = read(HERE / next(body['body_file'] for body in ee['new_bodies'] if body['body_file'].endswith('-plan.json')))
    state = {'task_ref': plan['task_ref'], 'goal_revision': 2,
             'plan_ref': {'plan_id': plan['plan_id'], 'revision': 1}, 'admissions': [], 'outputs': [],
             'requirements': enable['context']['requirements'], 'condition_checks': []}
    check(inspect_plan_materialization(plan, 'observe_after', state, enable['context']['capabilities'])['status'] == 'waiting', 'observe_after waits for original enable closure')
    op = v(bt, 'O2:Operation')
    state['admissions'] = [{'task_ref': plan['task_ref'], 'plan_id': plan['plan_id'], 'plan_revision': 1, 'step_id': 'enable', 'operation_id': op['operation_id']}]
    state['outputs'] = [{'task_ref': plan['task_ref'], 'operation_id': op['operation_id'], 'execution_state': 'closed',
                         'effect': 'unknown', 'may_apply_later': True, 'verified': False}]
    check(inspect_plan_materialization(plan, 'observe_after', state, enable['context']['capabilities'])['status'] == 'waiting', 'unknown enable effect cannot advance plan')
    state['outputs'][0].update(effect='applied', may_apply_later=False, verified=True)
    check(inspect_plan_materialization(plan, 'observe_after', state, enable['context']['capabilities'])['status'] == 'ready', 'closed verified enable permits independent observation')
    state['goal_revision'] = 1
    check(inspect_plan_materialization(plan, 'enable', state, enable['context']['capabilities'])['status'] == 'stale', 'old-goal plan cannot materialize')
    null_record = cp(ee['decision_record'])
    null_record['model_call'] = None
    check(bool(validate('DecisionRecord', null_record)), 'model_call:null is rejected; zero-model record must omit field')
    failures = []
    for reason in ('rule_conflict', 'rule_output_invalid'):
        failed = {'decision_id': ident('decision', reason), 'revision': 2,
                  'snapshot_revision': ee['decision_record']['snapshot_revision'], 'status': 'failed',
                  'error': {'code': 'invalid_output', 'message': reason, 'retry': 'after_change'}}
        schema('DecisionRecord', failed, reason + ': expected failed record contract')
        check('model_call' not in failed and 'proposal' not in failed, reason + ': no hidden model fallback')
        failures.append({'case': reason, 'expected_record': failed,
                         'continuation': 'hold until exact rule policy is repaired; then take a new snapshot and a new Decision',
                         'scope': 'contract expectation only; complete policy dispatch/recovery was not executed'})
    check(baseline_inventory() == saved_inventory, 'baseline data unchanged after counterexamples')
    results = {'schema': 'call-projection-validation/1', 'status': 'passed', 'static_checks_passed': len(checks),
               'counterexamples_passed': len(bad), 'protected_baseline_file_count': len(saved_inventory),
               'variant_summaries': summaries, 'counterexamples': bad,
               'rule_failure_expectations': failures,
               'static_check_labels': checks,
               'runtime_validation': {'performed': False, 'model_calls': 0,
                                      'reason': 'Python exercised synthetic rule fixtures and schema/linkage checks only; no Harness, provider, content owner, target or database ran'},
               'measurement_limits': {'token_counts': None, 'real_currency_cost': None, 'model_latency_ms': None,
                                      'quality_equivalence': None, 'task_success_rate': None}}
    write(HERE / 'validation-results.json', results)
    print(json.dumps({k: results[k] for k in ('status', 'static_checks_passed', 'counterexamples_passed', 'protected_baseline_file_count')}, ensure_ascii=False))


if __name__ == '__main__':
    main()
