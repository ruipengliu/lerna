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

    if delegated_submit:
        ctx = p['delegation_context']
        require(exchange['auth'].get('sender_service_id') == ctx['sender_home_id'] == ctx['allocation_ref']['owner_id'],
                'delegation_sender', 'sender Home must come from authenticated peer identity')
        require(owner == target == p['home_id'] == output['home_id'],
                'receiver_admission', 'delegated task must be created at the fixed receiver Home')
        require(len(ctx['ancestor_ids']) == len(set(ctx['ancestor_ids'])) and output['task_id'] not in ctx['ancestor_ids'],
                'delegation_ancestry', 'delegated ancestry must be bounded and acyclic')
    if name == 'budget.read':
        require(output['role'] == p['role'], 'budget_read_role', 'owner and receiver views cannot be interchanged')
        record = output.get('allocation', output.get('receiver'))
        require(record['allocation_id'] == target, 'allocation_binding', 'query returned another allocation')
        authority = record['owner_id'] if output['role'] == 'owner' else record['receiver_id']
        require(authority == owner, 'budget_owner', 'query role must be served by its fixed authority')
    if name == 'budget.close':
        require(exchange['auth'].get('sender_service_id') == p['sender_home_id'] == p['allocation_ref']['owner_id'],
                'delegation_sender', 'only authenticated original allocation owner can request closure')
        require(output['allocation_id'] == target == p['allocation_ref']['id'] and output['receiver_id'] == owner and output['parent_owner_id'] == p['sender_home_id'] and output['parent_delegation_id'] == p['parent_delegation_id'],
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
            require(set(limits) == set(spent) and all(spent[u] <= limits[u] for u in spent),
                    'allocation_amount', 'settled amounts must have exact units and fit transferred bounds')
    if name == 'task.adjust_budget':
        require(output['task_id'] == target and output['home_id'] == owner,
                'budget_owner', 'adjustment must target the original Home task')
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
        require(output['home_id'] == target == owner, 'task_list_home', 'a page belongs to one authoritative Home')
        require(len(output['items']) <= p['limit'], 'page_bound', 'page exceeds requested count')
        keys = [(x['created_at'], x['task']['task_id']) for x in output['items']]
        require(keys == sorted(set(keys)), 'task_list_order', 'page must contain unique ascending stable keys')
        require(all(x['task']['home_id'] == target for x in output['items']), 'task_list_home', 'foreign Home task in page')
        require(all(instant(x['created_at']) <= instant(output['upper_bound']) for x in output['items']),
                'task_list_cut', 'newer task crossed fixed listing upper bound')
        if 'statuses' in p:
            require(all(x['task']['status'] in p['statuses'] for x in output['items']), 'task_list_filter', 'page violates status filter')
        if 'cursor' in p:
            c = p['cursor']
            require(output['upper_bound'] == c['upper_bound'] and all(k > (c['last_created_at'], c['last_task_id']) for k in keys),
                    'task_list_cursor', 'page must continue after the supplied key at the same upper bound')
        if 'next_cursor' in output:
            c = output['next_cursor']
            require(c['upper_bound'] == output['upper_bound'], 'task_list_cursor', 'continuation changed upper bound')
            floor = keys[-1] if keys else ((p.get('cursor') or {}).get('last_created_at', ''), (p.get('cursor') or {}).get('last_task_id', ''))
            require((c['last_created_at'], c['last_task_id']) >= floor,
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
        require(all(inv[k] == gate[k] for k in ('home_id', 'task_id', 'goal_revision')), 'observation_binding', 'embedded Invoke differs from Home gate')
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
            gate=out['gate'];gates[(tenant,gate['home_id'],gate['task_id'],owner)]=deepcopy(gate)
        if name == 'execution.cancel':
            cancelled.add((tenant,p['home_id'],p['task_id'],p['operation_id']))
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
            receivers[key]=deepcopy(r)
        if name == 'task.submit' and 'delegation_context' in p:
            ctx=p['delegation_context'];aref=ctx['allocation_ref'];akey=(tenant,ctx['sender_home_id'],aref['id']);rkey=(*akey,owner)
            a=allocations.get(akey);original=original_allocations.get(akey);receiver=receivers.get(rkey)
            dkey=(tenant,ctx['sender_home_id'],ctx['parent_delegation_id'])
            previous=incoming_delegations.get(dkey)
            if previous:
                require(previous==(out['task_id'],p), 'receiver_reopen', 'original delegation may only return the same child for the same intent')
            else:
                require(original is not None and original[0]==ctx['allocation_command_id'], 'receiver_allocation_original', 'receiver must verify original parent allocation command')
                require(a is not None and a['revision']==aref['revision'] and a['state']=='allocated' and a['receiver_id']==owner and a['limits']==p['budget'] and instant(a['expires_at'])>instant(event['at']) and instant(p['deadline'])<=instant(a['expires_at']),
                        'receiver_admission', 'current exact allocation must still fund this receiver within its bounds')
                require(receiver is None, 'receiver_reopen', 'one allocation may admit only one original child; closed gates cannot reopen')
                incoming_delegations[dkey]=(out['task_id'],deepcopy(p))
                receivers[rkey]={'allocation_id':aref['id'],'parent_owner_id':ctx['sender_home_id'],'receiver_id':owner,'parent_delegation_id':ctx['parent_delegation_id'],'revision':1,'state':'open','task_id':out['task_id'],'final_usage':[]}
        if name in ('task.submit', 'task.read', 'task.revise', 'task.adjust_budget') and 'task_id' in out:
            key = (tenant, out['home_id'], out['task_id']); old = tasks.get(key)
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
                require(old['state'] == 'allocated' and req['expected_revision'] == old['revision'], 'allocation_terminal', 'settled allocation cannot be consumed or settled again by another command')
                require(all(out[k] == old[k] for k in ('parent_task_id', 'receiver_id', 'limits', 'expires_at', 'owner_id')),
                        'allocation_identity', 'settlement changed fixed allocation intent')
                require(instant(p['closure']['closed_at']) <= instant(event['at']), 'allocation_closure', 'closure claims a future fact')
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
            gate=gates.get((tenant,inv['home_id'],inv['task_id'],inv['control_snapshot']['executor_id']),gate)
            require(gate['status']=='active' and gate['control']=='running' and instant(event['at'])<instant(inv['deadline']) and instant(event['at'])<instant(inv['control_snapshot']['start_before']),
                    'observation_start', 'successful new observation requires current eligible bounded startup')
            require((tenant,inv['home_id'],inv['task_id'],inv['operation_id']) not in cancelled and inv['goal_revision']==gate['goal_revision'],
                    'observation_start', 'observation convenience method cannot bypass cancellation or revised goal')
            known=resources.get((tenant,owner,p['resource_id']))
            if known:
                require(obs['control_epoch']==known['control_epoch'], 'observation_epoch', 'new observation must bind current owner epoch')
    return errors
