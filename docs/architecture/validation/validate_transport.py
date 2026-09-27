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


CONTROL_METHODS = {
    'task.pause', 'task.cancel', 'task.billing_reconcile', 'execution.control', 'execution.cancel',
    'brain.cancel', 'evaluation.cancel', 'evaluation.revoke', 'grant.revoke',
    'endpoint.revoke', 'content.close', 'content.release_copy', 'resource.release',
    'budget.close', 'budget.settle', 'grant.lease.settle', 'grant.use.settle',
}


def query_result_binding_errors(request, result):
    if request.get('method') in {'execution.list', 'extensions.list', 'grant.list'} and 'output' in result:
        output = result['output']
        if output.get('query_id') != request['payload']['query_id'] or output.get('owner_id') != request['target_id']:
            return ['collection_result: result changed the original query or responsible owner']
    if request.get('method') == 'content.get' and 'output' in result:
        control_requested = request['payload'].get('mode', 'bytes') == 'control'
        if control_requested != (result['output'].get('mode') == 'control'):
            return ['content_get_mode: returned bytes/control branch differs from the original query']
    return []


def limits_errors(limits):
    if (limits['max_json_bytes'] >= limits['max_frame_bytes']
            or limits['control_reserve_bytes'] < limits['max_frame_bytes']
            or limits['control_reserve_bytes'] >= limits['max_queue_bytes']
            or limits['control_reserve_items'] >= min(limits['max_inflight_requests'], limits['max_pending_deliveries'])
            or limits['max_connections_per_identity_service'] > limits['max_connections_per_identity_total']
            or limits['heartbeat_timeout_ms'] <= limits['heartbeat_interval_ms']):
        return ['connection_limits: reserves, message bounds or heartbeat window are inconsistent']
    return []


def frame_errors(frame, context):
    """Check recorded connection premises; this does not establish real authentication."""
    errors = []
    kind = frame['type']
    client_types = {'request', 'reply', 'ping', 'pong'}
    server_types = {'ready', 'response', 'delivery', 'reply_ack', 'mirror_ticket',
                    'change', 'snapshot_required', 'ping', 'pong'}
    allowed = {'client_to_server': client_types, 'server_to_client': server_types}
    if kind not in allowed.get(context.get('direction'), set()):
        errors.append('frame_direction: message is not allowed in this direction')
    if frame['connection_id'] != context.get('connection_id'):
        errors.append('frame_connection: message belongs to another socket')
    if not context.get('authenticated') or context.get('credential_revoked'):
        errors.append('frame_auth: current connection identity is not authorized')
    if kind != 'ready' and not context.get('ready_received'):
        errors.append('frame_ready: business messages cannot precede Ready')
    if kind == 'ready' and context.get('ready_received') and not context.get('internal_binding'):
        errors.append('frame_ready: an external socket receives Ready only once')
    limits = frame['limits'] if kind == 'ready' else context.get('limits')
    if not limits:
        return errors + ['connection_limits: negotiated limits are missing']
    errors.extend(limits_errors(limits))
    encoded = json.dumps(frame, ensure_ascii=False, separators=(',', ':')).encode('utf8')
    if len(encoded) > limits['max_frame_bytes']:
        errors.append('frame_capacity: complete frame exceeds byte limit')
    control = kind in {'ready', 'response', 'reply', 'reply_ack', 'snapshot_required', 'ping', 'pong'}
    request = frame.get('request', {})
    if kind == 'delivery':
        request = frame['envelope']['delivery']['request']
    if request.get('method') in CONTROL_METHODS:
        control = True
    if kind == 'request' and frame['kind'] == 'mirror_control':
        control = True
    # collaboration.control branches are admitted to the reserve only after domain authorization.
    if request.get('method') == 'collaboration.control' and context.get('closing_control_authorized'):
        control = True
    queue_limit = limits['max_queue_bytes'] - (0 if control else limits['control_reserve_bytes'])
    if context.get('queued_bytes', 0) + len(encoded) > queue_limit:
        errors.append('frame_capacity: send queue would consume unavailable capacity')
    if kind == 'ready':
        if frame['logical_service_id'] != context.get('logical_service_id'):
            errors.append('frame_service: Ready changed the discovered logical service')
        if not context.get('internal_binding'):
            if context.get('existing_connections', 0) >= limits['max_connections_per_identity_service']:
                errors.append('frame_capacity: identity and service connection limit reached')
            if context.get('existing_total_connections', 0) >= limits['max_connections_per_identity_total']:
                errors.append('frame_capacity: identity total connection limit reached')
    if kind == 'request':
        replay = context.get('internal_request_replay')
        before = context.get('request_seq_before', 0)
        after = context.get('request_seq_after')
        if replay and not context.get('internal_binding'):
            errors.append('frame_recovery_identity: only gateway-held internal recovery bypasses new ingress')
        if replay and frame != context.get('original_request_frame'):
            errors.append('frame_recovery_identity: internal rebind cannot change an outstanding request')
        if replay and frame['kind'] not in {'query', 'receipt_lookup', 'subscribe', 'upload_lookup', 'mirror_lookup'}:
            errors.append('frame_recovery_identity: uncertain writes must be looked up before returning their outcome')
        if not replay and frame['request_seq'] <= before:
            errors.append('frame_sequence: repeated or lower request sequence closes the socket without a response')
        if not replay and after != max(before, frame['request_seq']):
            errors.append('frame_sequence: recognized new requests consume their sequence even when later rejected')
        if replay and (after != before or frame['request_seq'] > before):
            errors.append('frame_recovery_sequence: recovery preserves the existing high-watermark and original sequence')
        cap = limits['max_inflight_requests'] - (0 if control else limits['control_reserve_items'])
        if not replay and context.get('inflight_requests', 0) >= cap:
            errors.append('frame_capacity: request slots reserved or exhausted')
        if frame['kind'] in ('command', 'query'):
            spec = METHODS.get(request['method'])
            if spec is None or spec['kind'] != frame['kind']:
                errors.append('frame_method: method does not match request kind')
            else:
                from protocol.schema import validate
                errors.extend('transport_schema: ' + e for e in validate(spec['input'], request['payload']))
            if len(json.dumps(request, ensure_ascii=False, separators=(',', ':')).encode('utf8')) > limits['max_json_bytes']:
                errors.append('frame_capacity: original request exceeds JSON limit')
        if (frame['kind'] == 'subscribe' and not context.get('replaces_subscription')
                and context.get('subscription_count', 0) >= limits['max_subscriptions']):
            errors.append('frame_capacity: subscription limit reached')
        management = frame['kind']
        if management in ('upload_reserve', 'upload_lookup'):
            if context.get('upload_owner_id') != context.get('authenticated_subject') or not context.get('authenticated_subject'):
                errors.append('upload_owner: upload metadata belongs to another authenticated subject')
            existing = context.get('existing_upload')
            if existing and (request['upload_id'] != existing['upload_id'] or
                    (management == 'upload_reserve' and any(request[k] != existing[k] for k in request))):
                errors.append('upload_identity: reservation or lookup changed the original upload')
        if management == 'mirror_reserve':
            registered = context.get('registered_copy', {})
            if (not context.get('reservation_authorized')
                    or request['receiver_service_id'] != context.get('receiver_service_id')
                    or request['content_ref'] != registered.get('content_ref')
                    or request['copy_id'] != registered.get('copy_id')
                    or request['receiver_service_id'] != registered.get('holder_id')
                    or request['sender_endpoint_id'] != context.get('paired_sender_endpoint')
                    or request['sender_instance_id'] != context.get('paired_sender_instance')
                    or registered.get('use_stopped')
                    or instant(request['expires_at']) > instant(registered['retention_until'])):
                errors.append('mirror_reserve: reservation lacks current receiver authority or original copy binding')
            if context.get('existing_mirror') and request != context['existing_mirror']['ticket']:
                errors.append('mirror_identity: reservation changed the original ticket')
        if management in ('mirror_lookup', 'mirror_control'):
            mirror = context.get('mirror', {})
            if not context.get('metadata_access_allowed') or request['ticket_id'] != mirror.get('ticket', {}).get('ticket_id'):
                errors.append('mirror_identity: metadata access does not bind an authorized original ticket')
        if management == 'mirror_control':
            previous = context.get('original_control')
            if previous is not None:
                if request != previous:
                    errors.append('control_identity: reused control id changed its original request')
                if context.get('authenticated_owner_id') != request['content_ref']['owner_id']:
                    errors.append('mirror_control: replay must still authenticate the original owner')
            elif context.get('mirror'):
                errors.extend(check({'definition': 'MirrorControl', 'value': request, 'context': context}))
    if kind == 'response':
        original = context.get('request_frame', {})
        if (original.get('connection_id') != frame['connection_id']
                or original.get('request_seq') != frame['request_seq']
                or original.get('kind') != frame['kind']):
            return errors + ['frame_response: response does not match this socket request']
        result = frame['result']
        original_request = original['request']
        if context.get('disclosure_allowed') is False and 'output' in result:
            errors.append('frame_disclosure: response cannot disclose content after permission closes')
        if frame['kind'] in ('command', 'receipt_lookup') and 'command_id' in result:
            if result['command_id'] != original['request']['command_id']:
                errors.append('frame_command: receipt belongs to another command')
        if frame['kind'] in ('command', 'query') and 'output' in result:
            from protocol.schema import validate
            spec = METHODS.get(original['request']['method'])
            if spec is None:
                errors.append('frame_method: original request method is unknown')
            else:
                errors.extend('transport_schema: ' + e for e in validate(spec['output'], result['output']))
                errors.extend(query_result_binding_errors(original_request, result))
        if 'error' not in result and frame['kind'] in ('upload_reserve', 'upload_lookup'):
            if result['upload_id'] != original_request['upload_id'] or (frame['kind'] == 'upload_reserve' and
                    any(result[k] != original_request[k] for k in original_request)):
                errors.append('upload_identity: response changed the original upload or reserved metadata')
            errors.extend(check({'definition': 'Upload', 'value': result, 'context': context}))
        if 'error' not in result and frame['kind'] in ('mirror_reserve', 'mirror_lookup', 'mirror_control'):
            if result['ticket']['ticket_id'] != original_request['ticket_id']:
                errors.append('mirror_identity: response changed the original mirror ticket')
            if frame['kind'] == 'mirror_reserve' and result['ticket'] != original_request:
                errors.append('mirror_identity: reserve response changed the immutable ticket')
            if frame['kind'] == 'mirror_control' and (
                    result['ticket']['content_ref'] != original_request['content_ref']
                    or result['ticket']['copy_id'] != original_request['copy_id']
                    or result['control_revision'] < original_request['control_revision']
                    or result['state'] != 'closed'):
                errors.append('control_identity: control response must confirm the original copy is closed at this revision')
        if (context.get('disclosure_allowed') is False and 'error' not in result
                and frame['kind'] in ('upload_reserve', 'upload_lookup', 'mirror_reserve', 'mirror_lookup', 'mirror_control')):
            errors.append('frame_disclosure: transfer metadata response cannot bypass current disclosure permission')
    if kind == 'delivery':
        delivery = frame['envelope']['delivery']
        cap = limits['max_pending_deliveries'] - (0 if control else limits['control_reserve_items'])
        if context.get('pending_deliveries', 0) >= cap:
            errors.append('frame_capacity: delivery slots reserved or exhausted')
        if (delivery['recipient_endpoint_id'] != context.get('authenticated_endpoint')
                or delivery['recipient_instance_id'] != context.get('authenticated_instance')):
            errors.append('frame_endpoint: delivery does not bind the authenticated endpoint instance')
        if context.get('disclosure_allowed') is False:
            errors.append('frame_disclosure: delivery cannot cross a closed disclosure gate')
        if len(json.dumps(delivery['request'], ensure_ascii=False, separators=(',', ':')).encode('utf8')) > limits['max_json_bytes']:
            errors.append('frame_capacity: delivered request exceeds JSON limit')
        errors.extend(check({'definition': 'Delivery', 'value': delivery, 'context': context}))
    if kind in ('reply', 'reply_ack', 'mirror_ticket'):
        definition, field = {'reply': ('Reply', 'reply'), 'reply_ack': ('ReplyAck', 'ack'),
                             'mirror_ticket': ('MirrorTicket', 'ticket')}[kind]
        errors.extend(check({'definition': definition, 'value': frame[field], 'context': context}))
        if kind == 'reply':
            delivery = context.get('delivery', {})
            if (delivery.get('recipient_endpoint_id') != context.get('authenticated_endpoint')
                    or delivery.get('recipient_instance_id') != context.get('authenticated_instance')):
                errors.append('frame_endpoint: reply does not come from the expected endpoint instance')
        if kind == 'mirror_ticket' and context.get('disclosure_allowed') is False:
            errors.append('frame_disclosure: mirror ticket cannot cross a closed disclosure gate')
    if kind in ('change', 'snapshot_required'):
        subscription = context.get('subscription', {})
        if frame['subscription_id'] != subscription.get('subscription_id'):
            errors.append('frame_subscription: message belongs to another subscription')
        if kind == 'snapshot_required' and subscription.get('authorization_changed') and frame['reason'] != 'authorization_changed':
            errors.append('frame_subscription: changed authorization scope requires an explicit new snapshot')
        if kind == 'change':
            if subscription.get('paused_for_gap') or subscription.get('authorization_changed') or frame['change']['object_type'] not in subscription.get('object_types', []):
                errors.append('frame_subscription: Change cannot pass a paused or different filter')
            if context.get('disclosure_allowed') is False:
                errors.append('frame_disclosure: current permission forbids this object hint')
    if kind == 'pong' and frame['nonce'] != context.get('pending_ping_nonce'):
        errors.append('frame_heartbeat: pong does not match the outstanding ping')
    if kind == 'ping' and context.get('pending_ping_nonce'):
        errors.append('frame_heartbeat: only one outstanding ping is allowed per direction')
    return errors


def check(vector):
    name, value = vector['definition'], vector['value']
    errors = definition_errors(name, value)
    if errors:
        return errors
    context = vector.get('context', {})
    if name == 'Discovery':
        errors.extend(limits_errors(value['limits']))
        if len(value['methods']) != len(set(value['methods'])) or any(m not in METHODS or METHODS[m]['status'] != 'frozen-draft' for m in value['methods']):
            errors.append('discovery_method: method not registered in exact profile')
    if name == 'ConnectionLimits':
        errors.extend(limits_errors(value))
    if name == 'Frame':
        errors.extend(frame_errors(value, context))
    if name == 'ChannelBinding':
        errors.extend(limits_errors(value['limits']))
        if (value['logical_service_id'] != context.get('logical_service_id')
                or value['connection_id'] != context.get('connection_id')):
            errors.append('channel_binding: metadata changed the gateway external service or connection')
        if value['limits_digest'] != digest(value['limits']):
            errors.append('channel_limits: metadata digest does not bind the complete effective limits')
        if not context.get('authenticated_gateway') or not context.get('current_identity_valid'):
            errors.append('channel_auth: internal binding lacks current gateway or original identity authority')
        previous = context.get('previous_binding')
        if previous:
            if value['binding_id'] == previous['binding_id']:
                errors.append('channel_binding: each internal replacement needs a fresh binding id')
            if value['binding_revision'] != previous['binding_revision'] + 1:
                errors.append('channel_revision: each newly allocated candidate advances exactly one revision')
            if any(value[k] != previous[k] for k in ('logical_service_id', 'connection_id', 'limits', 'limits_digest')):
                errors.append('channel_binding: rebind changed the external service, connection or limits')
            if (context.get('external_slots_before') != context.get('external_slots_after')
                    or context.get('request_seq_before') != context.get('request_seq_after')):
                errors.append('channel_quota: rebind must preserve external slots and request high-watermark')
        elif value['binding_revision'] != 1:
            errors.append('channel_revision: the first candidate starts at revision one')
        # This is a constructed atomic-registration observation, not a database test.
        # stored_binding may be newer than this request after an unknown earlier commit.
        stored = context.get('stored_binding')
        if stored:
            if value['binding_revision'] < stored['binding_revision']:
                errors.append('channel_revision: a delayed lower revision cannot replace the stored binding')
            elif value['binding_revision'] == stored['binding_revision'] and value != stored:
                errors.append('channel_revision: the same revision only permits the identical binding retry')
    if name == 'ChannelFrameRecord':
        frame = value['frame']
        # A candidate may only supply Ready; business output belongs to the active binding.
        binding = (context.get('candidate_binding', context.get('active_binding', {}))
                   if frame['type'] == 'ready' else context.get('active_binding', {}))
        if value['binding_id'] != binding.get('binding_id'):
            return errors + ['channel_binding: stale or unrelated internal stream output must be discarded']
        if frame['connection_id'] != binding.get('connection_id'):
            errors.append('channel_binding: frame changed the external connection identity')
        if frame['type'] == 'ready':
            if (frame['logical_service_id'] != binding.get('logical_service_id')
                    or frame['limits'] != binding.get('limits')):
                errors.append('channel_limits: internal Ready must echo the same service and exact external limits')
            if context.get('external_ready_sent') and context.get('forward_to_client'):
                errors.append('channel_ready: replacement Ready must not be forwarded as a second external Ready')
        errors.extend(frame_errors(frame, dict(context, internal_binding=True)))
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
            errors.extend(query_result_binding_errors(delivery['request'], value['result']))
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
        if (value['ticket_id'] != ticket['ticket_id']
                or value['content_ref'] != ticket['content_ref']
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


def sequence_trace_errors(trace):
    """Check recorded queue/ingress observations, not a running socket implementation."""
    sockets = {}
    errors = []
    maximum = 9007199254740991
    for event in trace['events']:
        action, connection = event['action'], event['connection_id']
        if action == 'open':
            if connection in sockets:
                errors.append('sequence_connection: a new socket needs a new connection identity')
                continue
            initial = event.get('initial_high_water', 0)
            sockets[connection] = dict(high_water=initial, last_sent=initial,
                                       binding=event['binding_id'], pending={}, queued=set(), closed=False)
            continue
        state = sockets[connection]
        if state['closed']:
            errors.append('sequence_closed: no events may resume a closed external socket')
            continue
        if action == 'enqueue':
            if 'request_seq' in event:
                errors.append('sequence_send_order: sequence allocation must occur at actual write, not enqueue')
            state['queued'].add(event['call'])
        elif action == 'write':
            seq = event['request_seq']
            if (event['call'] not in state['queued'] or not isinstance(seq, int)
                    or isinstance(seq, bool) or not state['last_sent'] < seq <= maximum):
                errors.append('sequence_send_order: actual serialized request order must strictly increase')
            state['queued'].discard(event['call'])
            state['last_sent'] = seq
        elif action == 'ingress':
            seq = event['request_seq']
            if (not isinstance(seq, int) or isinstance(seq, bool)
                    or not state['high_water'] < seq <= maximum):
                outcome = 'close'
                state['closed'] = True
            else:
                state['high_water'] = seq
                # Rejection is a recorded current-authority/capacity premise, not inferred here.
                outcome = 'error' if event.get('rejection') else 'pending'
                if outcome == 'pending':
                    state['pending'][seq] = event['call']
            if event['outcome'] != outcome or event['high_water_after'] != state['high_water']:
                errors.append('sequence_ingress: old sequences close; every recognized new sequence is consumed')
        elif action == 'rebind':
            if event['high_water_after'] != state['high_water']:
                errors.append('sequence_rebind: replacing an internal binding cannot reset or increment the high-watermark')
            state['binding'] = event['binding_id']
        elif action == 'recover':
            if (state['pending'].get(event['request_seq']) != event['call']
                    or event['high_water_after'] != state['high_water']):
                errors.append('sequence_recovery: only an original pending request can recover without new admission')
        elif action == 'response':
            seq = event['request_seq']
            deliver = (event['frame_connection_id'] == connection
                       and event['binding_id'] == state['binding'] and seq in state['pending'])
            if event['outcome'] != ('delivered' if deliver else 'discarded'):
                errors.append('sequence_response: match current connection, binding and pending sequence')
            if deliver:
                del state['pending'][seq]
        elif action == 'exhaustion_close':
            if state['high_water'] != maximum or not 0 <= event['drain_seconds'] <= 5:
                errors.append('sequence_exhaustion: close after safe-integer exhaustion with bounded draining')
            state['closed'] = True
        else:
            errors.append('sequence_event: unknown recorded event')
    return errors


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
    sequences = load(ROOT / 'examples/transport/request-sequences.json')
    traces = {v['name']: v for v in sequences['traces']}
    for trace in traces.values():
        errors = sequence_trace_errors(trace)
        if errors:
            raise AssertionError(trace['name'] + ': ' + '\n'.join(errors))
    for case in sequences['invalid_mutations']:
        errors = sequence_trace_errors(edit(copy.deepcopy(traces[case['trace']]), case['edits']))
        if not any(e.startswith(case['expect'] + ':') for e in errors):
            raise AssertionError(case['name'] + ': expected ' + case['expect'] + ', got ' + str(errors))
    print(f'PASS: {len(vectors)} transport/closure vectors; {len(cases)} directed invalid mutations')
    print(f'PASS: {len(traces)} request-sequence traces; {len(sequences["invalid_mutations"])} directed invalid mutations')
    print('Scope: bounded recorded structures and bindings; no network, storage, authentication or byte transfer executed')
    subprocess.run(['node', str(NODE)], check=True)


if __name__ == '__main__':
    main()
