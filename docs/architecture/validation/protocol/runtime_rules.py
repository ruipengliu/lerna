"""Recorded invariants for task budgets, capability catalog and resource ownership.

These checks consume claimed authority records. They do not verify signatures,
run database transactions, execute devices, or prove external effects.
"""
from copy import deepcopy
from decimal import Decimal
from jsonschema import Draft202012Validator
from .schema import METHODS, validate, walk
from .exchanges import instant

RUNTIME_METHODS = {
    'budget.allocate', 'budget.settle', 'task.adjust_budget', 'task.list',
    'capability.search', 'capability.describe', 'resource.acquire',
    'resource.renew', 'resource.get', 'resource.release', 'resource.takeover',
    'resource.observe', 'budget.read', 'budget.close',
}


def values(items, field='amount'):
    return {x['unit']: Decimal(x[field]) for x in items}


def check_brain_plan(plan):
    """Static bounded-plan invariants; this does not execute a task or a tool."""
    errors = validate('BrainPlan', plan)
    if errors:
        return errors
    steps = {step['step_id']: step for step in plan['steps']}
    if len(steps) != len(plan['steps']):
        errors.append('plan_steps: step_id must be unique')
    visiting, ancestors = set(), {}

    def predecessors(step_id):
        if step_id in visiting:
            raise ValueError('cycle')
        if step_id not in steps:
            raise ValueError('missing predecessor')
        if step_id not in ancestors:
            visiting.add(step_id)
            deps = steps[step_id]['depends_on']
            if len(set(deps)) != len(deps):
                raise ValueError('duplicate predecessor')
            result = set(deps)
            for dep in deps:
                result.update(predecessors(dep))
            visiting.remove(step_id)
            ancestors[step_id] = result
        return ancestors[step_id]

    try:
        for step_id in steps:
            predecessors(step_id)
    except ValueError as error:
        return errors + ['plan_dependencies: ' + str(error)]
    for step in steps.values():
        targets = []
        for binding in step.get('argument_bindings', []):
            pointer = binding['target_pointer']
            targets.append(pointer)
            source = binding['source']
            if source['kind'] == 'step_output' and source['step_id'] not in ancestors[step['step_id']]:
                errors.append('plan_source: future output must belong to an actual predecessor')
        if targets and 'action_template' not in step:
            errors.append('plan_template: bindings require an action template')
        for index, pointer in enumerate(targets):
            if any(pointer == other or pointer.startswith(other + '/') or other.startswith(pointer + '/')
                   for other in targets[index + 1:]):
                errors.append('plan_targets: binding targets must not overlap')
    return errors


def check_plan_installation(proposal, next_plan, next_plan_ref, task_ref, goal_revision,
                            current_plan_ref=None, current_plan=None):
    """Compare a proposed plan with the caller's constructed current Task state."""
    errors = validate('Proposal', proposal) + check_brain_plan(next_plan)
    if errors:
        return errors
    delta = proposal.get('plan_delta')
    if delta is None or delta['base_plan_ref'] != current_plan_ref or delta['next_plan_ref'] != next_plan_ref:
        errors.append('plan_base: exact current plan reference must match')
    if next_plan['task_ref'] != task_ref or next_plan['goal_revision'] != goal_revision:
        errors.append('plan_goal: plan must belong to the current task and goal')
    if current_plan_ref is None:
        if current_plan is not None or next_plan['revision'] != 1:
            errors.append('plan_revision: initial plan starts at revision one')
    elif current_plan is None or next_plan['plan_id'] != current_plan['plan_id'] or next_plan['revision'] != current_plan['revision'] + 1:
        errors.append('plan_revision: replacement keeps plan identity and advances once')
    return errors


def check_plan_expansion(proposal, next_plan, next_plan_ref, task_ref, goal_revision,
                         current_plan_ref, current_plan, step_id):
    """Check a constructed instruction-only expansion against its fixed baseline.

    This checks the recorded relation, not a live Snapshot/Decision transaction.
    Other Proposal kinds are handled by ordinary Task branches and do not admit
    the old step; this helper covers only an expansion that installs action steps.
    """
    errors = check_plan_installation(proposal, next_plan, next_plan_ref, task_ref,
                                     goal_revision, current_plan_ref, current_plan)
    if proposal.get('kind') != 'act' or proposal.get('actions') != [] or 'plan_delta' not in proposal:
        errors.append('plan_expansion: action expansion must install a plan without direct actions')
    steps = [step for step in current_plan['steps'] if step['step_id'] == step_id]
    if len(steps) != 1 or 'action_template' in steps[0]:
        errors.append('plan_expansion: fixed original step must be instruction-only')
    return errors


def inspect_plan_materialization(plan, step_id, state, capabilities):
    """Check constructed authority snapshots, never infer that effects really ran.

    state supplies the current Task, exact admitted predecessor mappings, original
    output bodies and selected ConditionResults with their current applicability.
    Return a diagnostic branch and, only when ready, the resolved candidate.
    """
    errors = check_brain_plan(plan)
    if errors:
        return {'status': 'invalid', 'errors': errors}
    if state['task_ref'] != plan['task_ref'] or state['goal_revision'] != plan['goal_revision'] or state['plan_ref'] != {'plan_id': plan['plan_id'], 'revision': plan['revision']}:
        return {'status': 'stale'}
    steps = {step['step_id']: step for step in plan['steps']}
    step = steps[step_id]
    resolved_inputs = []

    def admission(source_step):
        matches = [item for item in state['admissions']
                   if item['task_ref'] == plan['task_ref'] and item['plan_id'] == plan['plan_id']
                   and item['plan_revision'] == plan['revision'] and item['step_id'] == source_step]
        if len(matches) != 1:
            raise LookupError('missing or ambiguous predecessor admission')
        return matches[0]

    def output(operation_id, expected_ref=None):
        matches = [item for item in state['outputs'] if item['operation_id'] == operation_id
                   and item['task_ref'] == plan['task_ref']]
        if len(matches) != 1:
            raise LookupError('missing or ambiguous original output')
        item = matches[0]
        if item['execution_state'] != 'closed' or item['effect'] != 'applied' or item['may_apply_later'] is not False or not item['verified']:
            raise LookupError('predecessor has no final verified output')
        if expected_ref is not None and item['content_ref'] != expected_ref:
            raise ValueError('fixed output version differs')
        return item

    try:
        for dependency in step['depends_on']:
            output(admission(dependency)['operation_id'])
    except (LookupError, ValueError) as error:
        return {'status': 'waiting', 'reason': str(error)}
    checks, gap = [], False
    for condition in step.get('pass_conditions', []):
        requirements = [item for item in state['requirements'] if item['requirement_id'] == condition['requirement_id']]
        if len(requirements) != 1 or requirements[0]['rule_ref'] != condition['rule_ref']:
            gap = True
            continue
        selected = [item for item in state['condition_checks'] if item['selected']
                    and item['task_ref'] == plan['task_ref'] and item['rule_ref'] == condition['rule_ref']
                    and item['result']['requirement_id'] == condition['requirement_id']
                    and item['result']['goal_revision'] == plan['goal_revision']
                    and item['result']['artifact_ref'] == condition['artifact_ref']]
        if len(selected) != 1 or selected[0]['applicability'] != 'usable' or validate('ConditionResult', selected[0]['result']):
            gap = True
            continue
        checks.append(selected[0])
    if any(item['result']['verdict'] == 'fail' for item in checks):
        return {'status': 'redecide', 'reason': 'current condition failed'}
    if gap or any(item['result']['verdict'] != 'pass' for item in checks):
        return {'status': 'waiting', 'reason': 'current applicable condition evidence missing'}
    if 'action_template' not in step:
        return {'status': 'redecide', 'reason': 'step has no action template'}
    candidate = deepcopy(step['action_template'])

    def tokens(pointer):
        return [part.replace('~1', '/').replace('~0', '~') for part in pointer.split('/')[1:]]

    def descend(node, parts):
        for part in parts:
            if isinstance(node, list):
                if not part.isdigit() or str(int(part)) != part:
                    raise ValueError('invalid array index')
                node = node[int(part)]
            else:
                node = node[part]
        return node

    try:
        for binding in step.get('argument_bindings', []):
            source = binding['source']
            operation_id = source['operation_id'] if source['kind'] == 'operation_output' else admission(source['step_id'])['operation_id']
            item = output(operation_id, source.get('evidence_ref'))
            value = deepcopy(descend(item['body'], tokens(source['source_pointer'])))
            path = tokens(binding['target_pointer'])
            target = descend(candidate, path[:-1])
            if isinstance(target, list):
                if not path[-1].isdigit() or str(int(path[-1])) != path[-1] or int(path[-1]) >= len(target):
                    raise ValueError('target array slot must already exist')
                target[int(path[-1])] = value
            elif isinstance(target, dict):
                target[path[-1]] = value
            else:
                raise ValueError('binding parent is not an object or array')
            resolved_inputs.append({'operation_id': operation_id, 'content_ref': item['content_ref'],
                                    'revision': item['revision'], 'source_pointer': source['source_pointer']})
    except (KeyError, IndexError, TypeError, LookupError, ValueError) as error:
        return {'status': 'redecide', 'reason': str(error)}
    errors = validate('ActionInvoke' if candidate['type'] == 'invoke' else 'ActionDelegate', candidate)
    if candidate['type'] == 'invoke':
        matches = [cap for cap in capabilities if cap['capability_ref'] == candidate['capability_ref'] and cap['binding_ref'] == candidate['binding_ref']]
        if len(matches) != 1:
            errors.append('plan_capability: exact declared capability missing')
        else:
            errors.extend('plan_arguments: ' + error.message for error in Draft202012Validator(matches[0]['input_schema']).iter_errors(candidate['arguments']))
    if errors:
        return {'status': 'redecide', 'errors': errors}
    return {'status': 'ready', 'candidate': candidate, 'resolved_inputs': resolved_inputs,
            'condition_check_ids': [item['check_id'] for item in checks]}


def check_exchange(exchange, capabilities):
    req = exchange.get('request', {})
    name = req.get('method')
    delegated_submit = name == 'task.submit' and 'delegation_context' in req.get('payload', {})
    if name not in RUNTIME_METHODS and not delegated_submit:
        return []
    spec = METHODS[name]
    p = req.get('payload', {})
    errors = validate('Exchange', exchange) + validate(spec['input'], p)
    if spec['kind'] == 'command' and ('expected_revision' in req) != spec['expected_revision']:
        errors.append('expected_revision: method conditional-update requirement differs')
    res = exchange.get('response', {})
    output = res.get('output')
    if errors or output is None or res.get('stage') == 'rejected':
        return errors
    errors += validate(spec['output'], output)
    if errors:
        return errors
    target = req['target_id']
    owner = exchange['auth']['logical_service_id']

    def require(test, code, detail):
        if not test:
            errors.append(code + ': ' + detail)

    def unique_units(items, field='amount'):
        require(len(items) == len(values(items, field)), 'budget_units', 'unit may occur only once')

    def check_allocation_incident(record):
        limits=values(record['limits'],'limit');spent=values(record['final_usage'])
        if record['state']=='settled' and set(limits)==set(spent) and any(spent[u]>limits[u] for u in limits):
            require(bool(record.get('incident_causes')) or record.get('incident_pending') is True,
                    'allocation_incident', 'over-allocation bill needs an attributed cause or explicit pending investigation')

    if delegated_submit:
        ctx = p['delegation_context']
        require(exchange['auth'].get('sender_service_id') == ctx['sender_orchestrator_id'] == ctx['allocation_ref']['owner_id'],
                'delegation_sender', 'sender Orchestrator must come from authenticated peer identity')
        require(owner == target == p['orchestrator_id'] == output['orchestrator_id'],
                'receiver_admission', 'delegated task must be created at the fixed receiver Orchestrator')
        require(len(ctx['ancestor_ids']) == len(set(ctx['ancestor_ids'])) and output['task_id'] not in ctx['ancestor_ids'],
                'delegation_ancestry', 'delegated ancestry must be bounded and acyclic')
    if name == 'budget.read':
        require(output['role'] == p['role'], 'budget_read_role', 'owner and receiver views cannot be interchanged')
        record = output.get('allocation', output.get('receiver'))
        require(record['allocation_id'] == target, 'allocation_binding', 'query returned another allocation')
        authority = record['owner_id'] if output['role'] == 'owner' else record['receiver_id']
        require(authority == owner, 'budget_owner', 'query role must be served by its fixed authority')
        if output['role']=='owner':check_allocation_incident(record)
    if name == 'budget.close':
        require(exchange['auth'].get('sender_service_id') == p['sender_orchestrator_id'] == p['allocation_ref']['owner_id'],
                'delegation_sender', 'only authenticated original allocation owner can request closure')
        require(output['allocation_id'] == target == p['allocation_ref']['id'] and output['receiver_id'] == owner and output['parent_owner_id'] == p['sender_orchestrator_id'] and output['parent_delegation_id'] == p['parent_delegation_id'],
                'receiver_binding', 'close must preserve original allocation, sender, receiver and delegation')
        require(output['state'] in ('closing', 'closed'), 'receiver_closure', 'close cannot leave new admission open')
    if name == 'budget.close' or name == 'budget.read' and output['role'] == 'receiver':
        receiver = output if name == 'budget.close' else output['receiver']
        if receiver['state'] == 'closed':
            closure = receiver.get('closure', {})
            require(closure.get('allocation_id') == receiver['allocation_id'] and closure.get('receiver_id') == receiver['receiver_id'] and closure.get('final_usage') == receiver['final_usage'],
                    'receiver_closure', 'closed receiver requires exact final spending proof')
        else:
            require('closure' not in receiver, 'receiver_closure', 'unfinished closure cannot publish final spending proof')

    if name in ('budget.allocate', 'budget.settle'):
        for key in ('limits', 'final_usage'):
            unique_units(output[key], 'limit' if key == 'limits' else 'amount')
        require(output['owner_id'] == owner, 'budget_owner', 'allocation belongs to responding budget owner')
        require(output['allocation_id'] == p['allocation_id'], 'allocation_binding', 'original allocation identity differs')
        check_allocation_incident(output)
        if name == 'budget.allocate':
            for key in ('parent_task_id', 'receiver_id', 'limits', 'expires_at'):
                require(output[key] == p[key], 'allocation_binding', 'fixed allocation intent differs')
            require(target == p['parent_task_id'], 'allocation_binding', 'allocation targets parent task')
            require(output['state'] == 'allocated' and not output['final_usage'] and 'closure' not in output,
                    'allocation_state', 'new allocation cannot be returned as settled')
        else:
            closure = p['closure']
            require(target == p['allocation_id'] and closure['allocation_id'] == p['allocation_id'],
                    'allocation_binding', 'closure must concern original allocation')
            require(output['state'] == 'settled' and output.get('closure') == closure,
                    'allocation_closure', 'settlement requires exact verified closure')
            require(output['receiver_id'] == closure['receiver_id'] and output['final_usage'] == closure['final_usage'],
                    'allocation_closure', 'receiver and final cumulative use differ')
            require(output['revision'] == req['expected_revision'] + 1, 'budget_revision', 'settlement advances compared revision once')
            limits = values(output['limits'], 'limit'); spent = values(closure['final_usage'])
            require(set(limits) == set(spent), 'allocation_amount', 'settled amounts must have the original units')
            # A verified final bill can exceed allocation after a provider's
            # single-call bound breach, a receiver admission breach, or both.
            # This trace checks explicit attribution/pending status, not proof
            # bytes; the parent owner must classify from original evidence.
    if name == 'task.adjust_budget':
        require(output['task_id'] == target and output['orchestrator_id'] == owner,
                'budget_owner', 'adjustment must target the original Orchestrator task')
        require(output['status'] == 'active' and output['revision'] == req['expected_revision'] + 1,
                'budget_revision', 'only active task advances compared revision once')
        unique_units(p['limits'], 'limit')
        requested = values(p['limits'], 'limit')
        actual = {b['unit']: Decimal(b['limit']['amount']) for b in output['budget']}
        require(requested == actual, 'budget_limits', 'output has exactly the requested unit limits')
        for b in output['budget']:
            require(all(b[k]['unit'] == b['unit'] for k in ('spent', 'reserved', 'limit')),
                    'budget_units', 'balance mixes billing units')
            require(Decimal(b['spent']['amount']) + Decimal(b['reserved']['amount']) <= Decimal(b['limit']['amount']),
                    'budget_obligations', 'new limit is below spent plus reserved')
    if name == 'task.list':
        require(output['orchestrator_id'] == target == owner, 'task_list_orchestrator', 'a page belongs to one authoritative Orchestrator')
        require(len(output['items']) <= p['limit'], 'page_bound', 'page exceeds requested count')
        keys = [(x['created_at'], x['task']['task_id']) for x in output['items']]
        def after(current, previous):
            return instant(current[0]) < instant(previous[0]) or (instant(current[0]) == instant(previous[0]) and current[1] > previous[1])
        require(all(after(current, previous) for previous, current in zip(keys, keys[1:])),
                'task_list_order', 'page must use created_at descending and task_id ascending at equal time')
        require(all(x['task']['orchestrator_id'] == target for x in output['items']), 'task_list_orchestrator', 'foreign Orchestrator task in page')
        require(all(instant(x['created_at']) <= instant(output['upper_bound']) for x in output['items']),
                'task_list_cut', 'newer task crossed fixed listing upper bound')
        if 'statuses' in p:
            require(all(x['task']['status'] in p['statuses'] for x in output['items']), 'task_list_filter', 'page violates status filter')
        if 'cursor' in p:
            c = p['cursor']
            require(output['upper_bound'] == c['upper_bound'] and all(after(k, (c['last_created_at'], c['last_task_id'])) for k in keys),
                    'task_list_cursor', 'page must continue after the supplied key at the same upper bound')
        if 'next_cursor' in output:
            c = output['next_cursor']
            require(c['upper_bound'] == output['upper_bound'], 'task_list_cursor', 'continuation changed upper bound')
            require(instant(c['last_created_at']) <= instant(output['upper_bound']),
                    'task_list_cursor', 'continuation crossed fixed listing upper bound')
            floor = keys[-1] if keys else (p['cursor']['last_created_at'], p['cursor']['last_task_id']) if 'cursor' in p else None
            if floor:
                next_key = (c['last_created_at'], c['last_task_id'])
                require(next_key == floor or after(next_key, floor),
                        'task_list_cursor', 'continuation went backwards over returned or scanned rows')
    if name == 'capability.search':
        require(len(output['candidates']) <= p['limit'], 'page_bound', 'catalog page exceeds requested count')
        pairs = [(c['capability_ref']['id'], c['capability_ref']['version'], c['capability_ref']['digest'], c['binding_ref']['binding_id'], c['binding_ref']['revision']) for c in output['candidates']]
        require(len(set(pairs)) == len(pairs), 'capability_duplicate', 'same exact capability and binding repeated')
        for c in output['candidates']:
            require(any(f['capability_ref'] == c['capability_ref'] and f['binding_ref'] == c['binding_ref'] for f in capabilities),
                    'catalog_reference', 'candidate not in fixed authority catalog fixture')
    if name == 'capability.describe':
        cap, binding = output['capability'], output['binding']
        cr = {'id': cap['capability_id'], 'version': cap['version'], 'digest': cap['digest']}
        require(cr == p['capability_ref'] == binding['capability_ref'], 'catalog_reference', 'describe silently changed exact capability')
        require({'binding_id': binding['binding_id'], 'revision': binding['revision']} == p['binding_ref'] and binding['executor_id'] == target,
                'catalog_binding', 'describe changed exact binding or executor')
        for key in ('input_schema', 'output_schema'):
            schema = cap[key]
            try:
                Draft202012Validator.check_schema(schema)
                valid = schema.get('type') == 'object' and schema.get('additionalProperties') is False
                valid &= not any(k in node and not node[k].startswith('#/') for node in walk(schema) for k in ('$ref', '$dynamicRef'))
            except Exception:
                valid = False
            require(valid, 'catalog_schema', 'installed schemas must be valid closed objects with only resolved local references')
        match = [f for f in capabilities if f['capability_ref'] == cr and f['binding_ref'] == p['binding_ref']]
        require(len(match) == 1 and match[0]['input_schema'] == cap['input_schema'], 'catalog_schema', 'description differs from installed fixed input schema')
        if match:
            require(all(k not in match[0] or match[0][k] == cap[k] for k in ('semantic_operation_id', 'effect_class')),
                    'catalog_reference', 'declared business operation or effect class differs from installed fixture')
        retry = cap['retry']
        require(retry['max_backoff_ms'] >= retry['initial_backoff_ms'], 'capability_retry', 'backoff limits reversed')
        if cap['effect_class'] == 'target_idempotent':
            require(all(k in retry for k in ('key_scope', 'key_retention_ms', 'replay_guarantee_ref')),
                    'capability_retry', 'idempotent target requires key scope, retention and replay contract')
    if name in ('resource.acquire', 'resource.renew'):
        require(output['resource_id'] == target and output['resource_owner_id'] == owner,
                'resource_binding', 'lease is for another resource or owner')
        require(output['state'] == 'active' and instant(output['expires_at']) <= instant(p['expires_at']),
                'lease_window', 'successful lease must be active and no longer than requested')
        if name == 'resource.acquire':
            require(all(output[k] == p[k] for k in ('holder_id', 'instance_id')),
                    'lease_holder', 'acquire changed requested holder or instance')
        else:
            require(output['lease_id'] == p['lease_id'] and output['revision'] == req['expected_revision'] + 1,
                    'lease_revision', 'renew must advance the original compared lease revision')
    if name in ('resource.get', 'resource.release', 'resource.takeover'):
        require(output['resource_id'] == target and output['resource_owner_id'] == owner,
                'resource_binding', 'resource observation comes from another owner')
        lease = output.get('lease')
        if lease:
            require(lease['resource_id'] == target and lease['resource_owner_id'] == owner,
                    'resource_binding', 'nested lease belongs to another resource')
            require(lease['state'] != 'active' or lease['control_epoch'] == output['control_epoch'],
                    'resource_epoch', 'active lease uses stale control epoch')
        if name == 'resource.release':
            require(output['control_epoch'] == p['expected_control_epoch'] and not output['user_control'] and (not lease or lease['state'] != 'active'),
                    'release_state', 'release must close current holder without changing or overriding epoch')
        if name == 'resource.takeover':
            require(output['user_control'] and (not lease or lease['state'] != 'active'),
                    'takeover_state', 'takeover cannot retain an active automatic lease')
    if name == 'resource.observe':
        inv = p['invoke']; op = output['operation']; obs = output.get('observation')
        caps = [c for c in capabilities if c['capability_ref'] == inv['capability_ref'] and c['binding_ref'] == inv['binding_ref']]
        require(len(caps) == 1 and caps[0].get('semantic_operation_id') == 'harness.gui.observe' and caps[0].get('effect_class') == 'read_only',
                'observation_capability', 'resource.observe must use a pinned declared read-only GUI observation capability')
        require(target == p['resource_id'] and op['operation_id'] == inv['operation_id'], 'observation_binding', 'observation operation identity changed')
        require(inv['arguments'].get('resource_id') == target, 'observation_binding', 'gui.observe must target the same resource')
        gate = inv['control_snapshot']['gate']
        require(all(inv[k] == gate[k] for k in ('orchestrator_id', 'task_id', 'goal_revision')), 'observation_binding', 'embedded Invoke differs from Orchestrator gate')
        require(bool(obs) == (op['effect'] == 'applied'), 'observation_effect', 'image only accompanies a successful declared read')
        if obs:
            require(obs['resource_id'] == target and instant(obs['expires_at']) > instant(obs['captured_at']),
                    'observation_binding', 'observation target or validity interval differs')
            require(obs['screenshot_ref'] in op['evidence_refs'], 'observation_evidence', 'image must be one of the original operation evidence refs')
    return errors


def check_trace_rules(trace):
    errors = []
    allocations = {}; resources = {}; tasks = {}; commands = set()
    receivers = {}; original_allocations = {}; incoming_delegations = {}; gates = {}; cancelled = set()
    for index, event in enumerate(trace.get('events', [])):
        if 'exchange' not in event:
            continue
        x = event['exchange']; req = x['request']; res = x['response']
        name = req['method']; p = req['payload']; out = res.get('output')
        spec=METHODS.get(name)
        if validate('Exchange', x) or spec and (validate(spec['input'],p) or out is not None and validate(spec['output'],out)):
            continue
        if (name in RUNTIME_METHODS or name == 'task.submit' and 'delegation_context' in p) and check_exchange(x, trace.get('capabilities', [])):
            continue
        if res.get('stage') == 'rejected' or out is None:
            continue
        cmd = req.get('command_id')
        cmdkey = (x['auth']['tenant_id'], x['auth']['logical_service_id'], cmd)
        if cmd and cmdkey in commands:
            continue  # Original Receipt replay never applies the ledger transition twice.
        if cmd:
            commands.add(cmdkey)
        def require(test, code, detail):
            if not test:
                errors.append(f'event {index}: {code}: {detail}')
        tenant = x['auth']['tenant_id']; owner = x['auth']['logical_service_id']
        if name == 'execution.control':
            gate=out['gate'];gates[(tenant,gate['orchestrator_id'],gate['task_id'],owner)]=deepcopy(gate)
        if name == 'execution.cancel':
            cancelled.add((tenant,p['orchestrator_id'],p['task_id'],p['operation_id']))
        if name == 'budget.read' and out['role'] == 'owner':
            a=out['allocation'];key=(tenant,a['owner_id'],a['allocation_id']);old=allocations.get(key)
            if old:
                require(a['revision']>=old['revision'] and not(old['state']=='settled' and a['state']!='settled'), 'allocation_terminal', 'current owner query cannot regress settled allocation')
                require(all(a[k]==old[k] for k in ('parent_task_id','receiver_id','limits','expires_at')), 'allocation_identity', 'owner query changed original allocation')
            allocations[key]=deepcopy(a)
        if name == 'budget.close' or name == 'budget.read' and out['role'] == 'receiver':
            r=out if name=='budget.close' else out['receiver'];key=(tenant,r['parent_owner_id'],r['allocation_id'],r['receiver_id']);old=receivers.get(key)
            if name == 'budget.close':
                original=original_allocations.get((tenant,r['parent_owner_id'],r['allocation_id']))
                if original:
                    require(original[0]==p['allocation_command_id'] and original[1]['revision']==p['allocation_ref']['revision'] and original[1]['receiver_id']==owner,
                            'receiver_allocation_original', 'closure request must bind exact original allocation and receiver')
                else:
                    require(r['state']=='closing', 'receiver_closure', 'unverified unknown allocation can be sealed but not finally settled')
            if old:
                require(old['parent_delegation_id']==r['parent_delegation_id'] and old.get('task_id')==r.get('task_id'), 'receiver_binding', 'receiver changed original child or delegation mapping')
                require(not(old['state'] in ('closing','closed') and r['state']=='open') and not(old['state']=='closed' and r['state']!='closed'), 'receiver_reopen', 'receiver closure cannot reopen admission')
                if old['state']=='closed' and r['state']=='closed':
                    previous=old['closure'];current=r['closure']
                    earlier=values(previous['final_usage']);later=values(current['final_usage'])
                    require(r['revision']>old['revision'] and current['usage_revision']>previous['usage_revision'],
                            'receiver_correction', 'closed receiver can only publish a newer billing revision')
                    require(all(current[k]==previous[k] for k in ('allocation_id','receiver_id','spending_closed','closed_at')) and
                            set(earlier)==set(later) and all(later[u]>=earlier[u] for u in earlier),
                            'receiver_correction', 'billing correction cannot reopen spending, change identity or reduce cumulative use')
            receivers[key]=deepcopy(r)
        if name == 'task.submit' and 'delegation_context' in p:
            ctx=p['delegation_context'];aref=ctx['allocation_ref'];akey=(tenant,ctx['sender_orchestrator_id'],aref['id']);rkey=(*akey,owner)
            a=allocations.get(akey);original=original_allocations.get(akey);receiver=receivers.get(rkey)
            dkey=(tenant,ctx['sender_orchestrator_id'],ctx['parent_delegation_id'])
            previous=incoming_delegations.get(dkey)
            if previous:
                require(previous==(out['task_id'],p), 'receiver_reopen', 'original delegation may only return the same child for the same intent')
            else:
                require(original is not None and original[0]==ctx['allocation_command_id'], 'receiver_allocation_original', 'receiver must verify original parent allocation command')
                require(a is not None and a['revision']==aref['revision'] and a['state']=='allocated' and a['receiver_id']==owner and a['limits']==p['budget'] and instant(a['expires_at'])>instant(event['at']) and instant(p['deadline'])<=instant(a['expires_at']),
                        'receiver_admission', 'current exact allocation must still fund this receiver within its bounds')
                require(receiver is None, 'receiver_reopen', 'one allocation may admit only one original child; closed gates cannot reopen')
                incoming_delegations[dkey]=(out['task_id'],deepcopy(p))
                receivers[rkey]={'allocation_id':aref['id'],'parent_owner_id':ctx['sender_orchestrator_id'],'receiver_id':owner,'parent_delegation_id':ctx['parent_delegation_id'],'revision':1,'state':'open','task_id':out['task_id'],'final_usage':[]}
        if name in ('task.submit', 'task.read', 'task.revise', 'task.adjust_budget') and 'task_id' in out:
            key = (tenant, out['orchestrator_id'], out['task_id']); old = tasks.get(key)
            if name == 'task.adjust_budget' and old:
                require(req['expected_revision'] == old['revision'], 'budget_revision', 'adjustment ignored current revision')
                require(all(out[k] == old[k] for k in ('deadline', 'control', 'control_revision', 'goal_revision', 'requirements', 'submit_command_id')),
                        'budget_adjust_scope', 'budget adjustment changed control, goal or lifetime')
                old_bal = {b['unit']: b for b in old['budget']}
                require(set(old_bal) == {b['unit'] for b in out['budget']} and all(old_bal[b['unit']]['spent'] == b['spent'] and old_bal[b['unit']]['reserved'] == b['reserved'] for b in out['budget'] if b['unit'] in old_bal),
                        'budget_adjust_scope', 'adjustment changed incurred or reserved obligations')
            tasks[key] = deepcopy(out)
        if name == 'budget.allocate':
            key = (tenant, owner, out['allocation_id'])
            require(key not in allocations, 'allocation_identity', 'another command cannot create the same allocation again')
            allocations[key] = deepcopy(out)
            original_allocations[key] = (cmd, deepcopy(out))
            require(instant(out['expires_at']) > instant(event['at']), 'allocation_window', 'cannot allocate already expired spending rights')
            parent = tasks.get((tenant, owner, out['parent_task_id']))
            if parent:
                limits = values(out['limits'], 'limit'); balances = {b['unit']: b for b in parent['budget']}
                require(set(limits) <= set(balances) and all(Decimal(balances[u]['spent']['amount']) + Decimal(balances[u]['reserved']['amount']) + amount <= Decimal(balances[u]['limit']['amount']) for u, amount in limits.items() if u in balances),
                        'allocation_budget', 'allocation exceeds parent uncommitted budget')
                require(instant(out['expires_at']) <= instant(parent['deadline']), 'allocation_window', 'child spending deadline exceeds parent')
                for u, amount in limits.items():
                    if u in balances:
                        balances[u]['reserved']['amount'] = str(Decimal(balances[u]['reserved']['amount']) + amount)
                parent['revision'] += 1
        if name == 'budget.settle':
            key = (tenant, owner, out['allocation_id']); old = allocations.get(key)
            require(old is not None, 'allocation_original', 'settlement needs original allocation record')
            if old:
                require(req['expected_revision'] == old['revision'], 'allocation_revision', 'settlement must compare current allocation revision')
                require(all(out[k] == old[k] for k in ('parent_task_id', 'receiver_id', 'limits', 'expires_at', 'owner_id')),
                        'allocation_identity', 'settlement changed fixed allocation intent')
                require(instant(p['closure']['closed_at']) <= instant(event['at']), 'allocation_closure', 'closure claims a future fact')
                if old['state']=='settled':
                    previous=old['closure'];current=p['closure']
                    earlier=values(previous['final_usage']);later=values(current['final_usage'])
                    require(current['usage_revision']>previous['usage_revision'] and
                            all(current[k]==previous[k] for k in ('allocation_id','receiver_id','spending_closed','closed_at')) and
                            set(earlier)==set(later) and all(later[u]>=earlier[u] for u in earlier),
                            'allocation_correction', 'settled allocation accepts only newer cumulative bill for the same closed receiver')
                receiver=receivers.get((tenant,owner,out['allocation_id'],out['receiver_id']))
                if receiver:
                    require(receiver['state']=='closed' and receiver.get('closure')==p['closure'], 'allocation_closure', 'known receiver has not produced this final closure')
            allocations[key] = deepcopy(out)
        if name.startswith('resource.') and name != 'resource.observe':
            key = (tenant, owner, req['target_id']); old = resources.get(key)
            lease = out if name in ('resource.acquire', 'resource.renew') else out.get('lease')
            epoch = out['control_epoch']
            if name in ('resource.acquire', 'resource.renew'):
                require(instant(lease['expires_at']) > instant(event['at']), 'lease_window', 'returned lease has already expired')
            if old:
                require(epoch >= old['control_epoch'], 'resource_epoch', 'resource control epoch regressed')
                if name == 'resource.acquire':
                    previous = old.get('lease')
                    blocked = old['user_control'] or old['inflight_operation_ids'] or previous and previous['state'] == 'active' and instant(previous['expires_at']) > instant(event['at'])
                    require(not blocked, 'resource_exclusion', 'cannot acquire while a holder or possible late action remains')
                    require(epoch == old['control_epoch'] + 1, 'resource_epoch', 'new acquisition establishes a fresh control epoch')
                if name == 'resource.renew':
                    previous = old.get('lease')
                    require(previous is not None and previous['state'] == 'active' and instant(previous['expires_at']) > instant(event['at']) and not old['user_control'],
                            'lease_renewal', 'cannot renew expired, revoked or manually taken-over lease')
                    if previous:
                        require(all(lease[k] == previous[k] for k in ('lease_id', 'holder_id', 'instance_id', 'control_epoch')) and req['expected_revision'] == previous['revision'],
                                'lease_renewal', 'renew changed original lease identity or ignored revision')
                        require(instant(lease['expires_at'])>=instant(previous['expires_at']), 'lease_renewal', 'renew cannot shorten the original lease window')
                if name == 'resource.takeover':
                    require(epoch == old['control_epoch'] + 1, 'resource_epoch', 'takeover must advance control epoch exactly once')
                    require(set(old['inflight_operation_ids']) <= set(out['inflight_operation_ids']),
                            'takeover_inflight', 'takeover cannot erase known in-flight effects')
                if name == 'resource.release':
                    require(req['payload']['expected_control_epoch'] == old['control_epoch'], 'resource_epoch', 'stale holder cannot release current occupant')
                    require(set(old['inflight_operation_ids']) <= set(out['inflight_operation_ids']),
                            'release_inflight', 'release cannot erase in-flight effects')
            if name in ('resource.acquire', 'resource.renew'):
                resources[key] = {'resource_id': lease['resource_id'], 'resource_owner_id': owner, 'control_epoch': epoch,
                                  'lease': deepcopy(lease), 'user_control': False, 'inflight_operation_ids': deepcopy(old['inflight_operation_ids']) if old else []}
            else:
                resources[key] = deepcopy(out)
        if name == 'resource.observe' and 'observation' in out:
            inv=p['invoke'];gate=inv['control_snapshot']['gate'];obs=out['observation']
            gate=gates.get((tenant,inv['orchestrator_id'],inv['task_id'],inv['control_snapshot']['executor_id']),gate)
            require(gate['status']=='active' and gate['control']=='running' and instant(event['at'])<instant(inv['deadline']) and instant(event['at'])<instant(inv['control_snapshot']['start_before']),
                    'observation_start', 'successful new observation requires current eligible bounded startup')
            require((tenant,inv['orchestrator_id'],inv['task_id'],inv['operation_id']) not in cancelled and inv['goal_revision']==gate['goal_revision'],
                    'observation_start', 'observation convenience method cannot bypass cancellation or revised goal')
            known=resources.get((tenant,owner,p['resource_id']))
            if known:
                require(obs['control_epoch']==known['control_epoch'], 'observation_epoch', 'new observation must bind current owner epoch')
    return errors
