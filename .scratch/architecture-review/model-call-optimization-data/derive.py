#!/usr/bin/env python3
"""Build bounded, synthetic call-layer projections. No model, owner or tool runs."""
from copy import deepcopy as cp
from datetime import datetime
import hashlib
import json
from pathlib import Path
import sys
from urllib.parse import urlparse

HERE = Path(__file__).resolve().parent
BASE = HERE.parent / 'task-scenarios-data'
ROOT = HERE.parents[2]
sys.path.insert(0, str(ROOT / 'docs/architecture/validation'))
from jsonschema import Draft202012Validator
from protocol.schema import validate

VARIANTS = {
    'bluetooth-on-1-call': ('bluetooth-on', ['D2']),
    'bluetooth-off-1-call': ('bluetooth-off', ['D2', 'D3']),
    'report-4-calls': ('report', ['D2']),
    'report-3-calls': ('report', ['D2', 'D3']),
}
DIMENSIONS = {'部署方式': 'deployment', '功能限制': 'limits', '维护成本': 'maintenance'}
_FIXED_POLICIES = {}


def encoded(value):
    return json.dumps(value, ensure_ascii=False, sort_keys=True, separators=(',', ':'), allow_nan=False).encode()


def digest(value):
    return 'sha256:' + hashlib.sha256(value).hexdigest()


def ident(kind, label):
    return kind + '_' + hashlib.sha256(('call-projection/1:' + label).encode()).hexdigest()[:32]


def read(path):
    return json.loads(path.read_text())


def write(path, value):
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(value, ensure_ascii=False, indent=2) + '\n')


def baseline_inventory():
    # Scripts and rendered Markdown may legitimately receive wording-only edits.
    return {str(p.relative_to(BASE)): digest(p.read_bytes()) for p in sorted(BASE.rglob('*'))
            if p.is_file() and (p.suffix == '.json' or 'bodies' in p.parts or 'components' in p.parts)}


def pointer_value(value, pointer):
    for part in pointer.strip('/').split('/'):
        part = part.replace('~1', '/').replace('~0', '~')
        value = value[int(part)] if isinstance(value, list) else value[part]
    return value


def source(slug, pointer):
    p = BASE / slug / 'scenario.json'
    return {'file': '../task-scenarios-data/' + slug + '/scenario.json', 'pointer': pointer,
            'value_digest': digest(encoded(pointer_value(read(p), pointer)))}


def v(bundle, key):
    return bundle['objects'][key]['value']


def content_by_ref(bundle, ref):
    return next(item for item in bundle['contents'].values() if item['ref'] == ref)


class NotEligible(Exception):
    def __init__(self, reason, route='new_model_decision'):
        self.reason, self.route = reason, route
        super().__init__(reason)


def require(test, reason, route='new_model_decision'):
    if not test:
        raise NotEligible(reason, route)


def inputs(bundle, phase):
    context = cp(v(bundle, phase + ':BrainContext'))
    goal = cp(content_by_ref(bundle, context['goal_ref'])['body'])
    facts = [cp(content_by_ref(bundle, fact['content_ref'])['body']) for fact in context['facts']]
    # A local rule test needs explicit current-state assumptions that the baseline
    # alone cannot prove. They are disclosed as constructed prerequisites.
    gate = {
        'goal_revision': 2,
        'control_revision': 2,
        'device_id': goal.get('device_id'),
        'capability_bindings': [{k: cp(c[k]) for k in ('capability_ref', 'binding_ref')} for c in context['capabilities']],
        'input_uses_allowed': True,
        'current_authorization_allowed': True,
        'budget_and_deadline_allow': True,
        'current_rule_policy_approved': True,
        'effect_reconciliation_required': False,
        'fact_max_age_seconds': 30,
        'evaluated_at': '2026-09-28T01:00:03.000Z',
    }
    slug = next(name for name in ('bluetooth-on', 'bluetooth-off', 'report')
                if read(BASE / name / 'scenario.json')['task_id'] == bundle['task_id'])
    return {'fixture_scenario': slug, 'phase': phase, 'context': context, 'goal': goal,
            'facts': facts, 'gate': gate, 'request_limits': cp(v(bundle, phase + ':DecisionRequest')['limits'])}


def fixed_policy(inp):
    """Exact fixture allowlist, not keyword interpretation of arbitrary NL goals."""
    name = inp['fixture_scenario']
    require(name in ('bluetooth-on', 'bluetooth-off', 'report'), 'unsupported_fixture')
    if name not in _FIXED_POLICIES:
        b = read(BASE / name / 'scenario.json')
        _FIXED_POLICIES[name] = {
            'goal': cp(b['contents']['goal']['body']),
            'requirements': cp(v(b, 'D1:Proposal')['requirements_proposal']['requirements']),
            'capabilities': cp(v(b, 'D2:BrainContext')['capabilities']),
        }
    return _FIXED_POLICIES[name]


def common_check(inp):
    c, g = inp['context'], inp['gate']
    require(not validate('BrainContext', c), 'context_incomplete', 'repair_context')
    require(c['goal_revision'] == g['goal_revision'] == 2, 'stale_goal', 'rebuild_current_snapshot')
    require(c['control_revision'] == g['control_revision'] and c['control'] == 'running', 'stale_control', 'hold_control')
    require(not c['gaps'], 'context_gap', 'repair_context')
    require(not c['unresolved_effects'] and not g['effect_reconciliation_required'], 'effect_unknown', 'reconcile_original_effect')
    require(g['input_uses_allowed'] and g['current_authorization_allowed'], 'authorization_unavailable', 'hold_authorization')
    require(g['budget_and_deadline_allow'], 'budget_or_deadline', 'hold_budget_or_deadline')
    require(g['current_rule_policy_approved'], 'rule_not_approved')
    current = g['capability_bindings']
    require([{k: x[k] for k in ('capability_ref', 'binding_ref')} for x in c['capabilities']] == current,
            'stale_capability_binding', 'rebuild_current_snapshot')
    require(c['requirements'] and all(r['required'] for r in c['requirements']), 'requirements_incomplete', 'repair_context')
    require(len(inp['facts']) == len(c['facts']), 'fact_missing', 'repair_context')
    # Values are never allowed to detach from the exact bytes named in context.
    for body, ref in [(inp['goal'], c['goal_ref'])] + list(zip(inp['facts'], [f['content_ref'] for f in c['facts']])):
        raw = encoded(body)
        require(digest(raw) == ref['hash'] and len(raw) == ref['byte_length'], 'content_digest_mismatch', 'repair_context')
        require(ref in c['input_manifest'], 'content_missing_from_manifest', 'repair_context')
    policy = fixed_policy(inp)
    goal = inp['goal']
    if inp['fixture_scenario'].startswith('bluetooth'):
        require(isinstance(goal.get('device_id'), str) and isinstance(goal.get('desired'), bool),
                'device_or_desired_missing', 'repair_context')
    else:
        products = goal.get('products')
        require(isinstance(products, list) and products and all(isinstance(p, dict) and
                all(isinstance(p.get(k), str) and p[k] for k in ('name', 'version', 'official_host')) for p in products)
                and isinstance(goal.get('dimensions'), list) and goal['dimensions'], 'report_parameters_incomplete', 'repair_context')
        require(isinstance(goal.get('root_id'), str) and isinstance(goal.get('relative_path'), str), 'report_destination_missing', 'repair_context')
    require(inp['goal'] == policy['goal'], 'unsupported_goal_shape')
    require(c['requirements'] == policy['requirements'], 'unsupported_requirements')
    require(c['capabilities'], 'capability_missing_or_ambiguous', 'repair_context')
    require(c['capabilities'] == policy['capabilities'], 'unsupported_capability_contract')
    require(isinstance(inp['request_limits'].get('max_actions'), int) and inp['request_limits']['max_actions'] >= 0,
            'request_action_limit_missing', 'repair_context')


def capability(inp, name):
    matches = [c for c in inp['context']['capabilities'] if c['semantic_operation_id'] == 'fixture.' + name]
    require(len(matches) == 1, 'capability_missing_or_ambiguous', 'repair_context')
    return matches[0]


def action(inp, name, key, arguments, evidence=()):
    cap = capability(inp, name)
    require(not list(Draft202012Validator(cap['input_schema']).iter_errors(arguments)), 'arguments_incomplete', 'repair_context')
    return {'action_key': key, 'type': 'invoke', 'purpose': 'task_execution',
            'requirement_refs': [r['requirement_id'] for r in inp['context']['requirements']],
            'evidence_refs': list(evidence), 'capability_ref': cp(cap['capability_ref']),
            'binding_ref': cp(cap['binding_ref']), 'arguments': arguments}


def freshness(inp, stamp):
    parse = lambda x: datetime.fromisoformat(x.replace('Z', '+00:00'))
    try:
        age = (parse(inp['gate']['evaluated_at']) - parse(stamp)).total_seconds()
    except (ValueError, AttributeError, TypeError):
        raise NotEligible('fact_timestamp_missing', 'repair_context')
    require(0 <= age <= inp['gate']['fact_max_age_seconds'], 'fact_expired', 'refresh_observation')


def proposal_base(inp, text):
    return {'kind': 'act', 'rationale': text, 'evidence_refs': [cp(inp['context']['goal_ref'])],
            'assumptions': ['限定规则及当前授权、绑定、事实新鲜度的门禁均为合成前提；未运行服务。'], 'actions': []}


def _rule_candidate(inp, rule, new_plan_id=None):
    """Construct outputs only from current inputs; never read a baseline Proposal."""
    common_check(inp)
    c, goal = inp['context'], inp['goal']
    if rule.startswith('bluetooth-'):
        require(goal.get('desired') is True and isinstance(goal.get('device_id'), str), 'device_or_desired_missing', 'repair_context')
        require(goal['device_id'] == inp['gate']['device_id'], 'stale_device_binding', 'rebuild_current_snapshot')
        require(len(c['requirements']) == 1 and c['requirements'][0]['kind'] == 'effect', 'unsupported_requirements')
        if rule == 'bluetooth-observe/1':
            p = proposal_base(inp, '按 g2 当前设备绑定和观察能力重新构造观察候选。')
            p['actions'] = [action(inp, 'observe', 'observe', {'device_id': goal['device_id']})]
            return p, None
        require(rule == 'bluetooth-enable-plan/1', 'unsupported_rule')
        require(c['plan_ref'] is None, 'existing_plan_requires_reconciliation')
        require(len(inp['facts']) == 1, 'observation_missing', 'repair_context')
        fact = inp['facts'][0]
        require(fact.get('device_id') == goal['device_id'], 'observation_device_mismatch', 'refresh_observation')
        require(fact.get('enabled') is False and isinstance(fact.get('state_version'), int) and fact['state_version'] >= 1,
                'observation_not_disabled', 'recheck_completion_or_observe')
        freshness(inp, fact.get('observed_at'))
        require(new_plan_id is not None, 'new_plan_handle_missing', 'repair_context')
        evidence = [cp(c['facts'][0]['content_ref'])]
        enable = action(inp, 'enable', 'enable', {'device_id': goal['device_id'], 'desired': True,
                                               'expected_state_version': fact['state_version']}, evidence)
        observe = action(inp, 'observe', 'observe_after', {'device_id': goal['device_id']})
        plan = {'schema_version': 'brain-plan/1', 'plan_id': new_plan_id, 'revision': 1,
                'task_ref': cp(c['task_ref']), 'goal_revision': c['goal_revision'],
                'steps': [{'step_id': 'enable', 'requirement_refs': enable['requirement_refs'], 'depends_on': [],
                           'instruction': '按新快照观察版本设置为开启；版本冲突须重新观察。', 'action_template': enable},
                          {'step_id': 'observe_after', 'requirement_refs': observe['requirement_refs'], 'depends_on': ['enable'],
                           'instruction': '原设置效果核清后重新观察；当前有效观察与固定条件核验通过才可完成。',
                           'action_template': observe}], 'source_refs': []}
        p = proposal_base(inp, '从 g2 新快照的关闭观察和准确绑定新建有限计划；安装后另行物化。')
        p['evidence_refs'] += evidence
        return p, plan
    require(rule in ('report-search/1', 'report-fetch-all/1'), 'semantic_generation_required')
    products, dims = goal.get('products'), goal.get('dimensions')
    require(isinstance(products, list) and len(products) == 2 and isinstance(dims, list) and dims == list(DIMENSIONS),
            'report_parameters_incomplete', 'repair_context')
    require(all(all(isinstance(p.get(k), str) and p[k] for k in ('name', 'version', 'official_host')) for p in products),
            'report_parameters_incomplete', 'repair_context')
    require(len({p['name'] for p in products}) == 2 and all('/' not in p['official_host'] for p in products), 'product_parameters_ambiguous')
    require(isinstance(goal.get('root_id'), str) and isinstance(goal.get('relative_path'), str), 'report_destination_missing', 'repair_context')
    require(len(c['requirements']) == 3 and [r['kind'] for r in c['requirements']] == ['quality', 'quality', 'effect'], 'unsupported_requirements')
    p = proposal_base(inp, '按已固定产品、版本、域名和维度模板重新构造搜索候选。' if rule == 'report-search/1' else
                      '对结构化搜索的有限同域命中逐项抓取；不作语义筛选或质量判定。')
    if rule == 'report-search/1':
        for prod in products:
            args = {'product': prod['name'], 'version': prod['version'], 'official_host': prod['official_host'],
                    'query': prod['name'] + ' ' + prod['version'] + ' ' + ' '.join(DIMENSIONS[d] for d in dims)}
            p['actions'].append(action(inp, 'search', 'search-' + prod['name'].lower(), args))
        return p, None
    require(len(inp['facts']) == len(products), 'search_materials_insufficient', 'repair_context')
    seen = set()
    for prod, result, fact_ref in zip(products, inp['facts'], c['facts']):
        require(result.get('product') == prod['name'] and result.get('version') == prod['version'], 'search_goal_mismatch', 'rebuild_current_snapshot')
        freshness(inp, result.get('retrieved_at'))
        hits = result.get('hits')
        require(isinstance(hits, list) and 1 <= len(hits) <= 2, 'search_materials_insufficient_or_overbound', 'repair_context')
        for hit in hits:
            try:
                url = urlparse(hit.get('url', ''))
                url_port, url_host = url.port, url.hostname
            except (ValueError, TypeError, AttributeError):
                raise NotEligible('search_url_invalid', 'repair_context')
            require(url.scheme == 'https' and url_host == prod['official_host'] and not url.username and not url.password and
                    not url_port and url.path.startswith('/' + prod['version'] + '/') and hit.get('official_host') == prod['official_host'],
                    'search_hit_outside_fixed_scope', 'repair_context')
            require(hit['url'] not in seen, 'duplicate_search_hit')
            seen.add(hit['url'])
            p['actions'].append(action(inp, 'fetch', 'fetch-' + str(len(p['actions']) + 1),
                                       {'url': hit['url'], 'official_host': prod['official_host']}, [cp(fact_ref['content_ref'])]))
    require(len(p['actions']) <= 4, 'action_bound_exceeded')
    p['evidence_refs'] += [cp(f['content_ref']) for f in c['facts']]
    return p, None


def rule_candidate(inp, rule, new_plan_id=None):
    proposal, plan = _rule_candidate(inp, rule, new_plan_id)
    # max_actions is the caller's per-round direct-action bound. A plan is
    # separately bounded by BrainPlan and later step admissions.
    require(len(proposal['actions']) <= inp['request_limits']['max_actions'], 'action_bound_exceeded', 'repair_context')
    return proposal, plan


def new_body(variant, label, body, base_ref, source_refs):
    path = HERE / 'bodies' / variant / (label + '.json')
    raw = encoded(body)
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_bytes(raw)
    ref = {**cp(base_ref), 'content_id': ident('content', variant + ':' + label), 'version': 1,
           'hash': digest(raw), 'byte_length': len(raw), 'media_type': 'application/json'}
    return {'body_file': str(path.relative_to(HERE)), 'ref': ref, 'source_refs': cp(source_refs),
            'publication_status': 'local_synthetic_bytes_only; owner commit not executed'}


def make_zero_entry(variant, slug, phase, bundle):
    inp = inputs(bundle, phase)
    rule = ('bluetooth-observe/1' if phase == 'D2' else 'bluetooth-enable-plan/1') if slug.startswith('bluetooth') else (
        'report-search/1' if phase == 'D2' else 'report-fetch-all/1')
    request = cp(v(bundle, phase + ':DecisionRequest'))
    request['decision_id'] = ident('decision', variant + ':' + phase)
    request['limits']['cost_reservation_ref']['id'] = ident('reservation', variant + ':' + phase)
    bodies = []
    plan_id = None
    if rule == 'bluetooth-enable-plan/1':
        plan_id = ident('plan', variant + ':' + phase)
        old_handles = bundle['contents']['allocated-handles']
        handles = cp(old_handles['body'])
        handles['plan_id'] = plan_id
        hb = new_body(variant, phase + '-allocated-handles', handles, old_handles['ref'], [old_handles['ref']])
        bodies.append(hb)
        c = inp['context']
        for material in c['materials']:
            if material['content_ref'] == old_handles['ref']:
                material['content_ref'] = cp(hb['ref'])
                material['source_refs'] = [cp(old_handles['ref'])]
        c['input_manifest'] = [cp(hb['ref']) if ref == old_handles['ref'] else ref for ref in c['input_manifest']]
        cb = new_body(variant, phase + '-context', c, request['context_ref'],
                      [request['context_ref']] + c['input_manifest'])
        bodies.append(cb)
        request['context_ref'] = cp(cb['ref'])
    proposal, plan = rule_candidate(inp, rule, plan_id)
    manifest = [cp(request['context_ref'])] + cp(inp['context']['input_manifest'])
    if plan:
        plan['source_refs'] = cp(manifest)
        pb = new_body(variant, phase + '-plan', plan, request['context_ref'], manifest)
        bodies.append(pb)
        proposal['plan_delta'] = {'base_plan_ref': None, 'next_plan_ref': cp(pb['ref'])}
    record = {'decision_id': request['decision_id'], 'revision': 2,
              'snapshot_revision': request['snapshot_revision'], 'status': 'completed', 'proposal': proposal}
    sources = {name: source(slug, '/objects/' + phase + ':' + name + '/value')
               for name in ('DecisionRequest', 'BrainContext')}
    sources['goal_body'] = source(slug, '/contents/goal/body')
    sources['D1_consumption'] = source(slug, '/objects/D1:consumption/value')
    return {'phase': phase, 'mode': 'deterministic', 'rule': rule, 'source_baseline': sources,
            'constructed_current_gate': inp['gate'],
            'rule_scope': 'exact frozen fixture goal, accepted Requirement values and complete capability declarations; arbitrary natural-language constraint coverage is unverified',
            'decision_request': request,
            'decision_input': {'input_digest': digest(encoded(request)), 'context_ref': request['context_ref'],
                               'actual_processing_manifest': manifest},
            'decision_record': record, 'new_bodies': bodies,
            'model_calls': [], 'model_generation_count': 0,
            'model_input_body_bytes': 0, 'model_usage': [],
            'model_fee': {'amount': 0, 'reason': 'no physical model generation'},
            'total_task_cost': None, 'model_tokens': None, 'model_latency_ms': None,
            'retained_responsibilities': ['Decision admission/job and fixed input', 'local input use and source closure',
                                         'Proposal validation and durable terminal record', 'Orchestrator current admission and consumption',
                                         'new content publication/recovery when present', 'unused model reservation release'],
            'persistence_evidence': 'JSON expected record is persisted in this package; real DecisionStore transactions, grants, reservation and owner publication are not executed'}


def generate():
    before = baseline_inventory()
    lock = HERE / 'baseline-digests.json'
    if lock.exists():
        assert read(lock)['files'] == before, 'protected baseline data changed since first derivation'
    else:
        write(lock, {'schema': 'baseline-data-digests/1', 'files': before,
                     'scope': 'all JSON, bodies and components; scripts/rendered prose excluded'})
    all_stats = []
    for variant, (slug, zeros) in VARIANTS.items():
        bundle = read(BASE / slug / 'scenario.json')
        rows, entries = [], []
        for index, generation in enumerate(bundle['generations']):
            phase = generation['label']
            raw_bytes = sum(r['byte_length'] for r in generation['actual_input_manifest'])
            retained = phase not in zeros
            rows.append({'phase': phase, 'owner': 'Brain', 'baseline_model_generations': 1,
                         'candidate_model_generations': int(retained), 'baseline_model_input_body_bytes': raw_bytes,
                         'candidate_model_input_body_bytes': raw_bytes if retained else 0,
                         'baseline_manifest': source(slug, '/generations/' + str(index) + '/actual_input_manifest')})
            if not retained:
                entries.append(make_zero_entry(variant, slug, phase, bundle))
            else:
                call = v(bundle, phase + ':ModelCall')
                entries.append({'phase': phase, 'mode': 'retained_baseline_model_projection',
                                'source': source(slug, '/objects/' + phase + ':DecisionRecord/value'),
                                'decision_id': v(bundle, phase + ':DecisionRecord')['decision_id'],
                                'model_call_id': call['model_call_id'], 'model_generation_count': 1,
                                'model_input_body_bytes': raw_bytes, 'model_tokens': None, 'real_cost': None,
                                'model_latency_ms': None})
        if slug == 'report':
            call = v(bundle, 'O7:ModelCall')
            raw_bytes = sum(r['byte_length'] for r in v(bundle, 'O7:model-send')['input_manifest'])
            rows.append({'phase': 'O7', 'owner': 'Executor assessment', 'baseline_model_generations': 1,
                         'candidate_model_generations': 1, 'baseline_model_input_body_bytes': raw_bytes,
                         'candidate_model_input_body_bytes': raw_bytes,
                         'baseline_manifest': source(slug, '/objects/O7:model-send/value/input_manifest')})
            entries.append({'phase': 'O7', 'mode': 'retained_baseline_model_projection',
                            'source': source(slug, '/objects/O7:ModelCall/value'),
                            'model_call_id': call['model_call_id'], 'model_generation_count': 1,
                            'model_input_body_bytes': raw_bytes,
                            'evaluation_scope': ['quality', 'citation_semantics'],
                            'count_rule': 'one physical generation shared by both checks',
                            'model_tokens': None, 'real_cost': None, 'model_latency_ms': None})
        stats = {'variant': variant, 'baseline_scenario': slug, 'phases': rows,
                 'decision_count': len(bundle['generations']), 'zero_model_decision_count': len(zeros),
                 'baseline_model_generations': sum(r['baseline_model_generations'] for r in rows),
                 'candidate_model_generations': sum(r['candidate_model_generations'] for r in rows),
                 'baseline_model_input_body_bytes': sum(r['baseline_model_input_body_bytes'] for r in rows),
                 'candidate_model_input_body_bytes': sum(r['candidate_model_input_body_bytes'] for r in rows),
                 'baseline_brain_model_input_body_bytes': bundle['statistics']['model_input_body_bytes'],
                 'baseline_assessment_input_body_bytes': bundle['statistics']['assessment_input_body_bytes'],
                 'model_tokens': None, 'real_model_cost': None, 'end_to_end_latency_ms': None}
        stats['eliminated_model_input_body_bytes'] = stats['baseline_model_input_body_bytes'] - stats['candidate_model_input_body_bytes']
        all_stats.append(stats)
        projection = {'schema': 'synthetic-call-layer-projection/1', 'variant': variant,
                      'scope': 'conditional alternative call-layer expectations, not a full replayable protocol package',
                      'warning': 'No new model output, runtime task, authorization, target operation, semantic evaluation or real bill was produced.',
                      'baseline_task_id': bundle['task_id'],
                      'preserved_goal_transition': {'source': source(slug, '/task_revision_log/2'),
                                                   'D1_consumption': source(slug, '/objects/D1:consumption/value'),
                                                   'old_goal_revision': 1, 'new_goal_revision': 2,
                                                   'discard_D1_remaining_proposal': True, 'new_decision_responsibility_required': True},
                      'entries': entries, 'statistics': stats,
                      'unreplayed_dependencies': ['new DecisionRequest reservation and authorization binding',
                                                 'current gate lookup and local processing use',
                                                 'new content owner publication and source policy inheritance',
                                                 'transactional Decision terminal/Orchestrator consumption and successor jobs',
                                                 'operation materialization, effects, condition checks, bills and cleanup']}
        write(HERE / (variant + '.json'), projection)
    write(HERE / 'statistics.json', {'schema': 'call-projection-statistics/1',
                                   'metric': 'sum exact manifest ContentRef.byte_length per projected physical generation; repeated bodies in different calls counted again',
                                   'limitation': 'fixed synthetic body bytes, not provider encoding, tokens, measured time or real charges; rule input reads are not model input',
                                   'variants': all_stats})
    assert baseline_inventory() == before, 'generator changed baseline data'
    return all_stats


if __name__ == '__main__':
    for row in generate():
        print(row['variant'], str(row['baseline_model_generations']) + ' -> ' + str(row['candidate_model_generations']),
              'body bytes', row['baseline_model_input_body_bytes'], '->', row['candidate_model_input_body_bytes'])
