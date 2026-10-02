#!/usr/bin/env python3
"""Check recorded input bytes and bindings, not live authority or atomic consumption.

Run from any directory with Python/jsonschema and Node available. The Node JCS
implementation and confirmation intent digest are shared with transport proofs.
"""
from copy import deepcopy
from hashlib import sha256
import json
import math
from pathlib import Path
import subprocess

from jsonschema import Draft202012Validator, FormatChecker
from protocol.content_rules import instant, request_schema_errors
from protocol.governance_rules import confirmation_intent_hash
from protocol.schema import METHODS, SCHEMA, validate
from validate_transport import canonical_bytes, strict_pairs

ROOT = Path(__file__).resolve().parents[3] / 'contracts'
FIXTURES = ROOT / 'examples/input-answers'
BODY_SCHEMA = json.loads((ROOT / 'schemas/input-answer.schema.json').read_text())
MAX_BYTES = 1048576
MAX_INTEGER = 9007199254740991
BODY_VALIDATOR = Draft202012Validator(BODY_SCHEMA, format_checker=FormatChecker())


def canonical(value):
    return canonical_bytes(json.dumps(value, ensure_ascii=False, allow_nan=False))


def ref_for_bytes(reference, raw):
    return dict(reference, hash='sha256:' + sha256(raw).hexdigest(), byte_length=len(raw))


def request_ref(request):
    return {'owner_id': request['owner_id'], 'id': request['request_id'],
            'revision': request['revision']}


def scalar_errors(value):
    if isinstance(value, str):
        if any(0xD800 <= ord(char) <= 0xDFFF for char in value):
            return ['answer_unicode: lone surrogate is not a Unicode scalar value']
    elif isinstance(value, (int, float)) and not isinstance(value, bool):
        if ((isinstance(value, int) and abs(value) > MAX_INTEGER)
                or (isinstance(value, float) and not math.isfinite(value))):
            return ['answer_number: non-finite or unsafe integer']
    elif isinstance(value, dict):
        return [error for key, item in value.items() for member in (key, item)
                for error in scalar_errors(member)]
    elif isinstance(value, list):
        return [error for item in value for error in scalar_errors(item)]
    return []


def parse_answer(raw, reference):
    if len(raw) > MAX_BYTES:
        return None, ['answer_size: body exceeds 1048576 bytes']
    errors = validate('InputAnswerContentRef', reference)
    if errors:
        return None, ['answer_ref_schema: ' + error for error in errors]
    if reference != ref_for_bytes(reference, raw):
        return None, ['answer_bytes: hash or byte_length differs from exact stored bytes']
    try:
        text = raw.decode('utf8', errors='strict')
        if text.startswith('\ufeff'):
            return None, ['answer_encoding: UTF-8 BOM is forbidden']
    except UnicodeDecodeError:
        return None, ['answer_encoding: body is not strict UTF-8']
    def reject_constant(value):
        raise ValueError('non-JSON constant: ' + value)
    try:
        body = json.loads(text, object_pairs_hook=strict_pairs, parse_constant=reject_constant)
    except (ValueError, RecursionError) as error:
        return None, ['answer_json: ' + str(error)]
    errors = scalar_errors(body)
    if errors:
        return None, errors
    errors = ['answer_schema: ' + error.message for error in BODY_VALIDATOR.iter_errors(body)]
    if errors:
        return None, errors
    try:
        if canonical_bytes(text) != raw:
            return None, ['answer_canonical: bytes are not the JCS encoding of the body']
    except subprocess.CalledProcessError:
        return None, ['answer_canonical: body cannot be encoded by the shared JCS profile']
    return body, []


def field_errors(body, request):
    errors = []
    definitions = {field['name']: field for field in request['schema']['fields']}
    values = body['fields']
    if body['action_id'] not in request['allowed_actions']:
        errors.append('answer_action: action_id is not currently allowed')
    if set(values) - set(definitions):
        errors.append('answer_fields: unknown input field')
    for name, field in definitions.items():
        if name not in values:
            if field['required']:
                errors.append('answer_required: required field is absent: ' + name)
            continue
        value = values[name]
        kind = field['type']
        if kind == 'text':
            if not isinstance(value, str):
                errors.append('answer_type: text requires a string')
            elif len(value) > field['max_length']:
                errors.append('answer_text_length: text exceeds Unicode code point limit')
        elif kind == 'integer':
            if not isinstance(value, int) or isinstance(value, bool):
                errors.append('answer_type: integer requires a safe integer, not a boolean')
            elif not field['minimum'] <= value <= field['maximum']:
                errors.append('answer_integer_range: integer is outside the declared range')
        elif kind == 'boolean':
            if not isinstance(value, bool):
                errors.append('answer_type: boolean requires true or false')
        else:
            option_ids = [option['id'] for option in field['options']]
            if kind == 'choice':
                if not isinstance(value, str):
                    errors.append('answer_type: choice requires one option id')
                elif value not in option_ids:
                    errors.append('answer_option: choice must use a declared option id')
            elif not isinstance(value, list) or any(not isinstance(item, str) for item in value):
                errors.append('answer_type: choices requires an array of option ids')
            elif any(item not in option_ids for item in value):
                errors.append('answer_option: choices contains an undeclared option id')
            elif len(value) != len(set(value)):
                errors.append('answer_choices: choices contains duplicate option ids')
            elif len(value) > field['max_choices']:
                errors.append('answer_choices: choices exceeds max_choices')
            elif value != [option_id for option_id in option_ids if option_id in value]:
                errors.append('answer_choices_order: choices differs from declaration order')
    return errors


def command_errors(command):
    errors = validate('Command', command)
    if errors:
        return errors
    return validate(METHODS[command['method']]['input'], command['payload'])


def acceptance_errors(body, case):
    request = case['request']
    command = body.get('consumer_command')
    if command is None:
        return ['answer_consumer: acceptance requires the exact approved consumer command']
    errors = []
    payload = command['payload']
    if (command['target_id'] != request['task_ref']['task_id']
            or payload['request_id'] != request['request_id']
            or payload['request_revision'] != request['revision']
            or payload['candidate_hash'] != request['candidate_hash']
            or payload['goal_revision'] != request['goal_revision']):
        errors.append('answer_consumer_binding: consumer differs from this task/request/candidate/goal')
    if instant(command['expires_at']) <= instant(case['at']):
        errors.append('answer_consumer_window: consumer admission has expired')
    confirmation = case.get('confirmation')
    if confirmation is None:
        return errors + ['answer_confirmation: approved confirmation observation is missing']
    shape = validate('ConfirmationRecord', confirmation)
    if shape:
        return errors + ['answer_confirmation_schema: ' + error for error in shape]
    expected_ref = {'owner_id': confirmation['owner_id'], 'id': confirmation['confirmation_id'],
                    'revision': confirmation['revision']}
    if (confirmation['state'] != 'approved' or confirmation['revision'] != 2
            or payload['confirmation_ref'] != expected_ref
            or confirmation['owner_id'] != request['owner_id']):
        errors.append('answer_confirmation: consumer must name this owner\'s exact approved revision')
    if command != confirmation['consumer_command']:
        errors.append('answer_consumer_identity: consumer command differs from approved original')
    if (confirmation['consumer_method'] != command['method']
            or confirmation['consumer_target_id'] != command['target_id']
            or confirmation['consumer_command_id'] != command['command_id']):
        errors.append('answer_confirmation_binding: recorded consumer metadata differs')
    if confirmation['intent_hash'] != confirmation_intent_hash(confirmation['owner_id'], confirmation['consumer_command']):
        errors.append('answer_confirmation_intent: canonical approved intent hash differs')
    if (instant(confirmation['expires_at']) <= instant(case['at'])
            or instant(confirmation['expires_at']) > instant(command['expires_at'])
            or ('decided_at' in confirmation and instant(confirmation['decided_at']) > instant(case['at']))):
        errors.append('answer_confirmation_window: approval is not usable at the recorded observation')
    return errors


def handoff_errors(body, case):
    errors = []
    request = case['request']
    expected = {'request_id': request['request_id'], 'request_revision': request['revision'],
                'answer_ref': case['answer_ref']}
    def input_payload(payload):
        result = []
        if any(payload.get(key) != value for key, value in expected.items()):
            result.append('answer_handoff: input changed exact request or answer reference')
        if any(ref not in payload['preview_refs'] for ref in request['required_content_refs']):
            result.append('answer_preview: input omits a required current preview reference')
        return result
    delivery = case.get('delivery_command')
    if delivery is not None:
        shape = command_errors(delivery)
        if shape:
            return ['answer_handoff_schema: ' + error for error in shape]
        if request['kind'] == 'acceptance':
            if delivery != body.get('consumer_command'):
                errors.append('answer_handoff: delivery rebuilt or replaced the fixed consumer command')
        elif request['kind'] == 'clarification':
            if delivery['method'] != 'task.input' or delivery['target_id'] != request['task_ref']['task_id']:
                errors.append('answer_handoff: clarification targets another consumer')
            else:
                errors += input_payload(delivery['payload'])
    queue = case.get('queue')
    if queue is not None:
        queued = queue['request']
        submission = queue['submission']
        shape = command_errors(queued) + validate('InputSubmission', submission)
        if shape:
            return errors + ['answer_handoff_schema: ' + error for error in shape]
        if queued['method'] != 'interaction.input':
            return errors + ['answer_handoff: queue ingress must use interaction.input']
        payload = queued['payload']
        errors += input_payload(payload) + input_payload(submission)
        if (queued['target_id'] != payload['surface_id']
                or any(submission[key] != payload[key] for key in payload)):
            errors.append('answer_handoff: persisted submission changed the queue ingress')
        consumer = body.get('consumer_command', delivery)
        if consumer is None or submission['target_command_id'] != consumer['command_id']:
            errors.append('answer_handoff: queue replaced the fixed target_command_id')
    return errors


def answer_errors(raw, case):
    body, errors = parse_answer(raw, case['answer_ref'])
    if errors:
        return errors
    request = case['request']
    shape = validate('InputRequestView', request) + validate('ObjectRef', case['request_ref'])
    if shape:
        return ['answer_request_schema: ' + error for error in shape]
    errors += request_schema_errors(request['schema'])
    if request_ref(request) != case['request_ref']:
        errors.append('answer_request_binding: request owner/id/revision differs from requested view')
    if 'task_ref' in request and request['task_ref']['orchestrator_id'] != request['owner_id']:
        errors.append('answer_request_binding: task and input request have different owners')
    if request['state'] != 'open' or 'consumed_by' in request:
        errors.append('answer_request_state: only a current open request accepts an answer')
    if instant(request['deadline']) <= instant(case['at']):
        errors.append('answer_request_window: input request has expired')
    if case.get('request_gaps'):
        errors.append('answer_disclosure: required request information is not fully disclosed')
    if request['kind'] == 'acceptance':
        if request['candidate_hash'] != request['candidate_ref']['hash']:
            errors.append('answer_candidate: candidate reference hash differs from request binding')
        errors += acceptance_errors(body, case)
    elif 'consumer_command' in body:
        errors.append('answer_consumer: clarification/application cannot carry a consumer command')
    errors += field_errors(body, request)
    errors += handoff_errors(body, case)
    return errors


def wait_binding_errors(task, requests):
    errors = []
    for wait in task['wait_reasons']:
        if wait['kind'] != 'input':
            continue
        matches = [request for request in requests if wait.get('object_ref') == request_ref(request)]
        if not matches:
            errors.append('input_wait_binding: wait does not identify an exact current request')
            continue
        request = matches[0]
        target = request.get('task_ref', {'orchestrator_id': request['owner_id'],
                                          'task_id': request.get('task_id')})
        if (request['owner_id'] != task['orchestrator_id'] or target != {
                'orchestrator_id': task['orchestrator_id'], 'task_id': task['task_id']}
                or request.get('state', 'open') != 'open'):
            errors.append('input_wait_binding: request is not a current open input of this task')
    return errors


def discovery_errors(case):
    """Check a recorded task.read projection against its recorded current requests."""
    query = case['query_result']
    shape = validate('QueryResult', query) + validate('Task', query['output'])
    for request in case['requests']:
        shape += validate('InputRequestView', request)
    if shape:
        return ['input_wait_schema: ' + error for error in shape]
    task = query['output']
    references = [request_ref(request) for request in case['requests']]
    visible = [wait['object_ref'] for wait in task['wait_reasons'] if wait['kind'] == 'input']
    errors = wait_binding_errors(task, case['requests'])
    if visible != references:
        errors.append('input_wait_binding: wait must identify exactly the current disclosed request')
    withheld = case.get('withheld_request_refs', [])
    if withheld and (not query.get('gaps') or any(ref in visible for ref in withheld)):
        errors.append('input_disclosure: withheld request uses gaps without an incomplete input item')
    if case['answerable_request_refs'] != references:
        errors.append('input_disclosure: answering requires a complete exact current request view')
    return errors


def edit(value, edits):
    for change in edits:
        parts = [part.replace('~1', '/').replace('~0', '~') for part in change['path'].split('/')[1:]]
        current = value
        for part in parts[:-1]:
            current = current[int(part)] if isinstance(current, list) else current[part]
        key = int(parts[-1]) if isinstance(current, list) else parts[-1]
        if change['op'] == 'remove':
            del current[key]
        else:
            current[key] = deepcopy(change['value'])


def sync_answer_refs(value, old, new):
    if isinstance(value, dict):
        for key, item in value.items():
            if key == 'answer_ref' and item == old:
                value[key] = deepcopy(new)
            else:
                sync_answer_refs(item, old, new)
    elif isinstance(value, list):
        for item in value:
            sync_answer_refs(item, old, new)


def schema_bundle_errors():
    Draft202012Validator.check_schema(BODY_SCHEMA)
    errors = []
    shared = BODY_SCHEMA['$defs']
    for name, definition in shared.items():
        if name != 'InputAnswer' and definition != SCHEMA['$defs'].get(name):
            errors.append('schema_bundle: copied shared definition differs: ' + name)
    def references(value):
        if isinstance(value, dict):
            for key, item in value.items():
                if key == '$ref' and (not item.startswith('#/$defs/') or item[8:] not in shared):
                    errors.append('schema_bundle: non-local or missing definition: ' + item)
                references(item)
        elif isinstance(value, list):
            for item in value:
                references(item)
    references(BODY_SCHEMA)
    return errors


def main():
    manifest = json.loads((FIXTURES / 'cases.json').read_text(), object_pairs_hook=strict_pairs)
    errors = schema_bundle_errors()
    if errors:
        print('\n'.join(errors))
        raise SystemExit(1)
    cases = {case['name']: case for case in manifest['valid_answers']}
    bodies = {}
    for name, case in cases.items():
        raw = (FIXTURES / case['body_path']).read_bytes()
        bodies[name] = raw
        errors += [name + ': ' + error for error in answer_errors(raw, case)]
    for negative in manifest['invalid_answers']:
        case = deepcopy(cases[negative['base']])
        body = json.loads(bodies[case['name']])
        edit(body, negative.get('body_edits', []))
        if 'raw_utf8' in negative:
            raw = negative['raw_utf8'].encode('utf8')
        elif 'raw_hex' in negative:
            raw = bytes.fromhex(negative['raw_hex'])
        elif negative.get('oversize'):
            raw = b' ' * (MAX_BYTES + 1)
        else:
            raw = canonical(body)
        # Rebind ordinary mutations so a field/consumer case reaches its named rule.
        old = case['answer_ref']
        sync_answer_refs(case, old, ref_for_bytes(old, raw))
        edit(case, negative.get('context_edits', []))
        found = answer_errors(raw, case)
        if not any(error.startswith(negative['expect'] + ':') for error in found):
            errors.append(negative['name'] + ': expected ' + negative['expect'] + ', got ' + repr(found))
    discoveries = {case['name']: case for case in manifest['valid_discoveries']}
    for name, case in discoveries.items():
        errors += [name + ': ' + error for error in discovery_errors(case)]
    for negative in manifest['invalid_discoveries']:
        case = deepcopy(discoveries[negative['base']])
        edit(case, negative['edits'])
        found = discovery_errors(case)
        if not any(error.startswith(negative['expect'] + ':') for error in found):
            errors.append(negative['name'] + ': expected ' + negative['expect'] + ', got ' + repr(found))
    bindings = 0
    wait_bindings = 0
    known_refs = [case['answer_ref'] for case in cases.values()]
    for path in sorted((ROOT / 'examples/protocol').glob('[0-9]*.json')):
        document = json.loads(path.read_text())
        def check_refs(value):
            nonlocal bindings
            if isinstance(value, dict):
                for key, item in value.items():
                    if key == 'answer_ref':
                        bindings += 1
                        if item not in known_refs:
                            errors.append(str(path.name) + ': answer_ref has no verified byte fixture')
                    check_refs(item)
            elif isinstance(value, list):
                for item in value:
                    check_refs(item)
        check_refs(document)
        for event in document['events']:
            output = event.get('exchange', {}).get('response', {}).get('output', {})
            if 'wait_reasons' in output:
                wait_bindings += sum(wait['kind'] == 'input' for wait in output['wait_reasons'])
                errors += [path.name + ': ' + error for error in
                           wait_binding_errors(output, document['input_requests'])]
    if errors:
        print('\n'.join(errors))
        raise SystemExit(1)
    print(f'PASS: {len(cases)} real input-answer byte fixtures, '
          f'{len(manifest["invalid_answers"])} rejected answer mutations, '
          f'{len(discoveries)} input discovery projections, '
          f'{len(manifest["invalid_discoveries"])} rejected discovery mutations, '
          f'{bindings} protocol answer_ref byte bindings, {wait_bindings} protocol input wait bindings; '
          'local schema/JCS/recorded relations only, no live authority or commit proof')


if __name__ == '__main__':
    main()
