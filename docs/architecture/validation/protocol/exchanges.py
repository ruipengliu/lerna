"""Stateless field and cross-field rules for one authenticated exchange."""
from datetime import datetime
from decimal import Decimal
from jsonschema import Draft202012Validator
from .schema import METHODS, validate, walk


def instant(value):
    return datetime.fromisoformat(value.replace('Z', '+00:00'))


def check_capability(value, capabilities):
    errors = []
    for node in walk(value):
        if not {'capability_ref', 'binding_ref', 'arguments'} <= node.keys():
            continue
        match = [c for c in capabilities
                 if c['capability_ref'] == node['capability_ref']
                 and c['binding_ref'] == node['binding_ref']]
        if len(match) != 1:
            errors.append('capability_binding: exact capability and binding unavailable')
        elif list(Draft202012Validator(match[0]['input_schema']).iter_errors(node['arguments'])):
            errors.append('capability_arguments: arguments violate pinned input schema')
    return errors


def validate_exchange(exchange, capabilities):
    req = exchange.get('request', {})
    name = req.get('method')
    spec = METHODS.get(name)
    if not spec or spec['status'] != 'frozen-draft':
        return ['unsupported_method: method is unknown or reserved']
    errors = validate('Exchange', exchange)
    if errors:
        return errors
    command = 'command_id' in req
    if command != (spec['kind'] == 'command'):
        return ['method_kind: command and query cannot be interchanged']
    errors += validate(spec['input'], req['payload'])
    if command and ('expected_revision' in req) != spec['expected_revision']:
        errors.append('expected_revision: method conditional-update requirement differs')
    if spec['target'].startswith('payload.'):
        target = req['payload']
        for key in spec['target'].split('.')[1:]:
            target = target.get(key) if isinstance(target, dict) else None
        if req['target_id'] != target:
            errors.append('target_binding: envelope target differs from payload')
    if errors:
        return errors
    errors += check_capability(req['payload'], capabilities)
    if name == 'execution.invoke':
        p = req['payload']; gate = p['control_snapshot']['gate']
        if any(p[k] != gate[k] for k in ('home_id','task_id','goal_revision')):
            errors.append('gate_binding: Invoke and authenticated snapshot differ')
        if instant(p['control_snapshot']['start_before']) <= instant(p['control_snapshot']['issued_at']):
            errors.append('control_window: empty or reversed startup window')
    response = exchange['response']
    # Authentication metadata is test harness context, never a caller-selected wire tenant.
    for node in walk(exchange['request']):
        if 'tenant_id' in node and node['tenant_id'] != exchange['auth']['tenant_id']:
            errors.append('tenant_binding: request reference belongs to another tenant')
    if 'error' in response and 'stage' not in response:
        if command:
            errors.append('command_response: durable business decision requires Receipt')
        return errors + error_rules(response['error'], spec)
    if command:
        if 'stage' not in response:
            return errors + ['command_response: expected Receipt']
        if response['command_id'] != req['command_id']:
            errors.append('command_binding: Receipt belongs to another command')
        if response['stage'] not in spec['stages']:
            errors.append('receipt_stage: stage unsupported by method')
        if response['stage'] == 'rejected':
            return errors + error_rules(response['error'], spec)
        if response.get('redacted'):
            if 'output' in response:
                errors.append('redaction: draft profile redaction omits entire output')
            return errors
    elif 'stage' in response or 'output' not in response:
        return errors + ['query_response: expected QueryResult']
    output = response.get('output')
    if output is None:
        return errors  # accepted may omit output until available.
    errors += validate(spec['output'], output)
    if errors:
        return errors
    for node in walk(output):
        if 'error' in node:
            errors += error_rules(node['error'], spec)
        if 'tenant_id' in node and node['tenant_id'] != exchange['auth']['tenant_id']:
            errors.append('tenant_binding: response reference belongs to another tenant')
    errors += check_capability(req['payload'], capabilities)
    errors += check_capability(output, capabilities)
    if 'resource_revision' in response and 'revision' in output and response['resource_revision'] != output['revision']:
        errors.append('response_revision: query metadata differs from object')
    p, target = req['payload'], req['target_id']
    def same(a, b, rule='identity_binding'):
        if a != b:
            errors.append(rule + ': correlated fields differ')
    if name == 'task.submit':
        same(output['home_id'], target)
        same(output['submit_command_id'], req['command_id'])
        same(output['goal_ref'], p['goal_ref'])
    if name in ('task.read','task.revise'):
        same(output['task_id'], target)
        if name == 'task.revise':
            same(output['goal_ref'], p['goal_ref'])
            if output['revision'] <= req['expected_revision']:
                errors.append('revision_progress: conditional write did not advance revision')
    if name in ('task.pause','task.resume','task.cancel'):
        same(output['task_id'], target)
        if output['revision'] <= req['expected_revision']:
            errors.append('revision_progress: control write did not advance revision')
        expected = {'pause':('control','paused'), 'resume':('control','running'), 'cancel':('status','cancelled')}[name.split('.')[1]]
        same(output[expected[0]], expected[1], 'control_decision')
    if name == 'task.result' and output['status'] == 'succeeded':
        same(output['result']['task_id'], target)
        errors += result_rules(output['result'])
    if name in ('task.input','task.accept_result'):
        same(output['task_id'], target)
        same(output['request_id'], p['request_id'])
        same(output['request_revision'], p['request_revision'])
        same(output['consumed_by'], req['command_id'])
        if name == 'task.accept_result' and 'verification_job_id' not in output:
            errors.append('acceptance_job: user acceptance requires subsequent verification')
    if name == 'brain.decide':
        same(output['decision_id'], p['decision_id'])
        same(output['snapshot_revision'], p['snapshot_revision'])
        if response['stage'] == 'applied' and output['status'] not in ('completed','failed','cancelled'):
            errors.append('brain_terminal: applied decide requires terminal DecisionRecord')
        if output['status'] == 'completed':
            proposal = output['proposal']
            if len(proposal.get('actions', [])) > p['limits']['max_actions'] or len(proposal.get('requests', [])) > p['limits']['max_context_requests']:
                errors.append('brain_bound: proposal exceeds admitted limits')
            available = [(c['capability_ref'],c['binding_ref']) for c in p['capability_refs']]
            for action in proposal.get('actions', []):
                if action['type'] == 'invoke' and (action['capability_ref'], action['binding_ref']) not in available:
                    errors.append('brain_visible_capability: proposal invents unseen version')
    if name in ('brain.get','brain.cancel'):
        same(output['decision_id'], target)
    if name == 'execution.invoke':
        same(output['operation_id'], p['operation_id'])
        gate = p['control_snapshot']['gate']
        for key in ('task_id','home_id','goal_revision'):
            same(p[key], gate[key], 'gate_binding')
        if instant(p['control_snapshot']['start_before']) <= instant(p['control_snapshot']['issued_at']):
            errors.append('control_window: empty or reversed startup window')
    if name in ('execution.get','execution.reconcile'):
        same(output['operation_id'], target)
    if name == 'execution.cancel':
        same(output['operation_id'], p['operation_id'])
        if 'cancel_command_id' in output:
            same(output['cancel_command_id'], req['command_id'])
            same(output['home_id'], p['home_id']); same(output['task_id'], p['task_id'])
    if name in ('execution.control','execution.control.get'):
        gate=output['gate']; source=p['gate'] if name=='execution.control' else p
        same(gate['home_id'],source['home_id']);same(gate['task_id'],source['task_id'])
        minimum=min(x['enforced_control_revision'] for x in output['entrances'])
        same(output['enforced_control_revision'],minimum,'control_enforcement')
        if minimum > gate['control_revision'] or any(e['enforced_control_revision']>gate['control_revision'] for e in output['entrances']):
            errors.append('control_enforcement: entrance cannot enforce an unknown revision')
        if name=='execution.control' and response['stage']=='applied' and minimum!=gate['control_revision']:
            errors.append('control_enforcement: applied requires all entrances enforced')
    if name=='grant.use':
        same(output['owner_id'],target,'grant_owner')
        same(output['grant_revisions'],p['grant_refs'],'grant_version')
        same(output['use_id'],p['use_id']);same(output['intent_hash'],p['intent_hash'])
        if output['decision']=='allowed':
            for maximum,reserved in [('max_units','reserved_units'),('max_cost','reserved_cost')]:
                same(p[maximum]['unit'],output[reserved]['unit'],'usage_unit')
                if Decimal(output[reserved]['amount'])>Decimal(p[maximum]['amount']):
                    errors.append('usage_bound: reservation exceeds request maximum')
        elif any(Decimal(output[k]['amount'])!=0 for k in ('reserved_units','reserved_cost')):
            errors.append('denied_consumption: denied use consumes no quota')
    if name=='grant.use.get':same(output['use_id'],target)
    if name in ('memory.create','memory.replace','memory.restrict','memory.delete','memory.read','memory.inspect'):
        if name!='memory.create':same(output['memory_id'],target)
        if name=='memory.create':same(output['owner_id'],target)
        if name in ('memory.replace','memory.delete','memory.restrict') and output['revision']<=req['expected_revision']:
            errors.append('revision_progress: memory write did not advance revision')
        if name in ('memory.create','memory.replace'):same(output['content_ref'],p['content_ref'])
        if name=='memory.read':same(output['revision'],p['revision'],'exact_revision')
        if name=='memory.delete':same(output['state'],'deleted','deletion_state')
    if name=='interaction.input':
        for key in p:same(output[key],p[key],'input_binding')
    if name in ('interaction.input_read','interaction.input_withdraw'):same(output['input_id'],target)
    if name.startswith('collaboration.') and 'delegation_id' in output:
        same(output['delegation_id'], p['delegation_id'] if name=='collaboration.delegate' else target)
        if name=='collaboration.delegate':
            for key in p:same(output[key],p[key],'delegation_binding')
        if output['phase']=='active' and not ('child_task_id' in output or 'remote_binding' in output):
            errors.append('delegation_mapping: active requires one fixed child mapping')
        if output['phase']=='closed' and (output['effects_pending'] or 'settlement_ref' not in output):
            errors.append('delegation_closure: effects and final settlement must be closed')
    if name=='evaluation.approval_check':
        same(p['approval_id'],target)
        for key in p:same(output[key],p[key],'approval_binding')
    if name=='evaluation.approval_lease':
        for key in ('approval_id','target_id','lock_id','instance_id'):same(output[key],p[key],'approval_binding')
    if name=='extensions.activate':same(output['activation_id'],p['activation_id'])
    if name.startswith('execution.') and 'effect' in output:
        if output['execution_state']=='accepted' and output['effect']!='not_started':
            errors.append('operation_state: accepted operation has not crossed startup boundary')
        if output['effect']=='not_started' and output['attempts'] and any('sent_at' in a for a in output['attempts']):
            errors.append('operation_effect: sent attempt cannot be proved never started')
        if output['effect']=='not_applied' and output['may_apply_later'] is not False:
            errors.append('operation_effect: not_applied requires no later effect')
    from .runtime_rules import check_exchange as runtime_exchange
    from .governance_rules import check_exchange as governance_exchange
    from .content_rules import check_exchange as content_exchange
    errors += runtime_exchange(exchange, capabilities)
    errors += governance_exchange(exchange, capabilities)
    errors += content_exchange(exchange, capabilities)
    from .collection_rules import check_exchange as collection_exchange
    errors += collection_exchange(exchange, capabilities)
    return errors


def error_rules(error, spec):
    found=[]
    if error['code'] not in spec['errors']:
        found.append('error_code: undeclared domain error')
    if error['code'] in spec['error_recovery'] and error['retry'] not in spec['error_recovery'][error['code']]:
        found.append('error_recovery: unsafe action for this error')
    return found


def result_rules(result):
    found=[]
    for condition in result['condition_results']:
        if condition['goal_revision']!=result['goal_revision'] or condition['artifact_ref'] not in result['artifact_refs']:
            found.append('result_version: evidence and artifact revisions differ')
    levels={'verified':0,'assessed':1,'user_accepted':2}
    return found
