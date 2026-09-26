#!/usr/bin/env python3
"""Validate bounded transport documents, not network/authority implementations."""
import copy
from datetime import datetime
from functools import lru_cache
import hashlib
import json
from pathlib import Path
import subprocess

from jsonschema import Draft202012Validator, FormatChecker
from referencing import Registry, Resource
from protocol.schema import METHODS, SCHEMA

ROOT = Path(__file__).resolve().parents[1] / 'contracts'
TRANSPORT = json.loads((ROOT / 'schemas/transport.schema.json').read_text())
REGISTRY = Registry().with_resources([
    ('https://harness.invalid/schema/protocol.schema.json', Resource.from_contents(SCHEMA)),
    (TRANSPORT['$id'], Resource.from_contents(TRANSPORT)),
])
Draft202012Validator.check_schema(TRANSPORT)
NODE = Path(__file__).with_name('verify_transport_proofs.mjs')


def strict_pairs(pairs):
    result = {}
    for key, value in pairs:
        if key in result:
            raise ValueError('duplicate JSON key: ' + key)
        result[key] = value
    return result


def load(path):
    return json.loads(path.read_text(), object_pairs_hook=strict_pairs)


def instant(value):
    return datetime.fromisoformat(value.replace('Z', '+00:00'))


@lru_cache(maxsize=256)
def canonical_bytes(raw):
    # Node implements ECMAScript numbers and UTF-16 key ordering required by JCS.
    result = subprocess.run(['node', str(NODE), '--canonical'], input=raw,
                            text=True, capture_output=True, check=True)
    return result.stdout.encode('utf8')


def digest(value):
    raw = json.dumps(value, ensure_ascii=False, allow_nan=False)
    return 'sha256:' + hashlib.sha256(canonical_bytes(raw)).hexdigest()


def definition_errors(name, value):
    document = dict(TRANSPORT, **{'$ref': '#/$defs/' + name})
    validator = Draft202012Validator(document, registry=REGISTRY, format_checker=FormatChecker())
    return ['transport_schema: ' + e.message for e in validator.iter_errors(value)]


def check(vector):
    name, value = vector['definition'], vector['value']
    errors = definition_errors(name, value)
    if errors:
        return errors
    context = vector.get('context', {})
    if name == 'Discovery':
        if len(value['methods']) != len(set(value['methods'])) or any(m not in METHODS or METHODS[m]['status'] != 'frozen-draft' for m in value['methods']):
            errors.append('discovery_method: method not registered in exact profile')
    if name == 'Delivery':
        if value['request_digest'] != digest(value['request']):
            errors.append('request_digest: immutable request hash differs')
        request = value['request']
        if value['kind'] != 'receipt_lookup':
            spec = METHODS.get(request['method'])
            if spec is None or spec['kind'] != value['kind']:
                errors.append('delivery_method: unsupported method/kind')
            else:
                from protocol.schema import validate
                errors.extend('transport_schema: ' + e for e in validate(spec['input'], request['payload']))
    if name == 'Reply':
        if (context.get('disclosure_allowed') is False or context.get('disclosure_closed')) and not value.get('withheld'):
            errors.append('reply_disclosure: cached content cannot cross a closed disclosure gate')
        delivery = context.get('delivery')
        if not delivery:
            return errors + ['delivery_context: original delivery missing']
        if any(value[k] != delivery[k] for k in ('delivery_id', 'kind', 'request_digest')):
            errors.append('delivery_binding: reply changed immutable delivery')
        if value['kind'] in ('command', 'receipt_lookup') and 'command_id' in value['result']:
            if value['result']['command_id'] != delivery['request']['command_id']:
                errors.append('delivery_command: reply belongs to another command')
        if value['kind'] != 'receipt_lookup' and 'output' in value['result']:
            from protocol.schema import validate
            method = delivery['request']['method']
            errors.extend('transport_schema: ' + e for e in validate(METHODS[method]['output'], value['result']['output']))
    if name == 'ReplyAck':
        reply = context.get('reply')
        if not reply or value['delivery_id'] != reply['delivery_id'] or value['result_digest'] != digest(reply['result']):
            errors.append('reply_digest: durable ack differs from original reply')
    if name == 'Upload' and value['state'] == 'committed':
        if any(value[k] != value['content_ref'][k] for k in ('hash', 'byte_length', 'media_type')):
            errors.append('content_upload: published reference does not match uploaded bytes')
    if name in ('MirrorTicket', 'Mirror'):
        ticket = value if name == 'MirrorTicket' else value['ticket']
        registered = context.get('registered_copy', {})
        if (ticket['content_ref'] != registered.get('content_ref')
                or ticket['copy_id'] != registered.get('copy_id')
                or ticket['receiver_service_id'] != registered.get('holder_id')
                or registered.get('use_stopped')
                or ticket['sender_endpoint_id'] != context.get('authenticated_sender')
                or ticket['sender_instance_id'] != context.get('authenticated_instance')
                or ticket['receiver_service_id'] != context.get('receiver_service_id')
                or instant(ticket['expires_at']) > instant(registered['retention_until'])):
            errors.append('mirror_binding: original reference, copy or authenticated route differs')
    if name == 'MirrorRead':
        mirror, download = value['mirror'], value['source_download']
        ticket = mirror['ticket']
        permitted = (mirror['state'] == 'ready' and value['source_online']
                     and instant(value['observed_at']) < instant(download['expires_at'])
                     and instant(value['observed_at']) < instant(mirror['retention_until'])
                     and download['content_ref'] == ticket['content_ref']
                     and download['copy_id'] == ticket['copy_id']
                     and download['control_revision'] == mirror['control_revision'])
        if value['allowed'] != permitted:
            errors.append('mirror_read: upload ticket does not grant or extend read permission')
    if name == 'MirrorControl':
        mirror = context['mirror']
        ticket = mirror['ticket']
        if (value['content_ref'] != ticket['content_ref']
                or value['copy_id'] != ticket['copy_id']
                or context['authenticated_owner_id'] != ticket['content_ref']['owner_id']
                or value['control_revision'] <= mirror['control_revision']):
            errors.append('mirror_control: new control must bind original owner and advance its revision')
    if name == 'ClosedIdentityLookup':
        record = value['record']
        expected = 'precondition_failed'
        if record['kind'] == 'command':
            expected = 'gone'
            if value.get('request_digest', record.get('request_digest')) != record.get('request_digest'):
                expected = 'idempotency_conflict'
        if value['lookup_id'] != record['object_id'] or record['payload_retained'] or value['response_code'] != expected:
            errors.append('closed_identity: purging content cannot erase original decision or reopen identity')
    return errors


def edit(value, operations):
    for operation in operations:
        cursor = value
        parts = operation['path'].strip('/').split('/')
        for part in parts[:-1]:
            cursor = cursor[int(part)] if isinstance(cursor, list) else cursor[part]
        key = int(parts[-1]) if isinstance(cursor, list) else parts[-1]
        if operation['op'] == 'remove':
            del cursor[key]
        else:
            cursor[key] = operation['value']
    return value


def main():
    vectors = load(ROOT / 'examples/transport/vectors.json')
    cases = load(ROOT / 'examples/transport/invalid-mutations.json')
    index = {v['name']: v for v in vectors}
    if len(index) != len(vectors):
        raise AssertionError('duplicate vector names')
    for vector in vectors:
        errors = check(vector)
        if errors:
            raise AssertionError(vector['name'] + ': ' + '\n'.join(errors))
    for case in cases:
        errors = check(edit(copy.deepcopy(index[case['vector']]), case['edits']))
        if not any(e.startswith(case['expect'] + ':') for e in errors):
            raise AssertionError(case['name'] + ': expected ' + case['expect'] + ', got ' + str(errors))
    print(f'PASS: {len(vectors)} transport/closure vectors; {len(cases)} directed invalid mutations')
    print('Scope: bounded recorded structures and bindings; no network, storage, authentication or byte transfer executed')
    subprocess.run(['node', str(NODE)], check=True)


if __name__ == '__main__':
    main()
