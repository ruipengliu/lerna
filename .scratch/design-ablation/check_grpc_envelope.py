"""Check compiled Protobuf envelopes against recorded WSS vectors, not a server."""
import json
from pathlib import Path

import sys
sys.path.insert(0, str(Path(__file__).resolve().parent / "proto"))
import harness_pb2 as pb

ROOT = Path(__file__).resolve().parents[2]
VECTORS = ROOT / 'docs/architecture/contracts/examples/transport/vectors.json'
request_fields = {
    'command': 'command_json',
    'query': 'query_json',
    'receipt_lookup': 'receipt_lookup_json',
    'upload_reserve': 'upload_reserve_json',
    'upload_lookup': 'upload_lookup_json',
    'mirror_reserve': 'mirror_reserve_json',
    'mirror_lookup': 'mirror_lookup_json',
    'mirror_control': 'mirror_control_json',
}
result_fields = {
    'command': 'receipt_json', 'query': 'query_result_json',
    'receipt_lookup': 'receipt_json',
    'upload_reserve': 'upload_json', 'upload_lookup': 'upload_json',
    'mirror_reserve': 'mirror_json', 'mirror_lookup': 'mirror_json',
    'mirror_control': 'mirror_json',
}
assert set(request_fields.values()) == {
    f.name for f in pb.CallRequest.DESCRIPTOR.oneofs_by_name['request'].fields
}
assert set(result_fields.values()) | {'error_json'} == {
    f.name for f in pb.CallResponse.DESCRIPTOR.oneofs_by_name['result'].fields
}
service = pb.DESCRIPTOR.services_by_name['HarnessService']
assert set(service.methods_by_name) == {'Call', 'EndpointChannel'}
assert service.methods_by_name['EndpointChannel'].client_streaming
assert service.methods_by_name['EndpointChannel'].server_streaming
assert not service.methods_by_name['Call'].client_streaming
assert not service.methods_by_name['Call'].server_streaming


def encoded(value):
    return json.dumps(value, ensure_ascii=False, separators=(',', ':')).encode('utf8')


def roundtrip(message, field, original, oneof=None):
    restored = type(message).FromString(message.SerializeToString())
    if oneof:
        assert restored.WhichOneof(oneof) == field
    assert json.loads(getattr(restored, field)) == original


seen_requests, seen_responses = set(), set()
channels = calls = responses = bindings = 0
assert pb.ChannelFrame.DESCRIPTOR.fields_by_name["binding_id"].number == 2
for vector in json.loads(VECTORS.read_text()):
    if vector['definition'] == 'ChannelFrameRecord':
        record = vector['value']
        msg = pb.ChannelFrame(frame_json=encoded(record['frame']), binding_id=record['binding_id'])
        roundtrip(msg, 'frame_json', record['frame'])
        assert pb.ChannelFrame.FromString(msg.SerializeToString()).binding_id == record['binding_id']
        bindings += 1
        continue
    if vector['definition'] != 'Frame':
        continue
    frame = vector['value']
    msg = pb.ChannelFrame(frame_json=encoded(frame), binding_id='binding_' + '1' * 32)
    roundtrip(msg, 'frame_json', frame)
    assert pb.ChannelFrame.FromString(msg.SerializeToString()).binding_id == 'binding_' + '1' * 32
    channels += 1
    kind = frame.get('kind')
    if frame['type'] == 'request' and kind in request_fields:
        field = request_fields[kind]
        message = pb.CallRequest(logical_service_id='service_' + '1' * 32,
                                 **{field: encoded(frame['request'])})
        roundtrip(message, field, frame['request'], 'request')
        calls += 1
        seen_requests.add(kind)
    if frame['type'] == 'response' and kind in result_fields:
        # A rejected Receipt may itself contain error; it remains a Receipt.
        result = frame['result']
        is_error = set(result) == {'error'}
        field = 'error_json' if is_error else result_fields[kind]
        body = result['error'] if is_error else result
        roundtrip(pb.CallResponse(**{field: encoded(body)}), field, body, 'result')
        responses += 1
        seen_responses.add(field)

assert seen_requests == set(request_fields), seen_requests
assert seen_responses == set(result_fields.values()) | {'error_json'}, seen_responses
print(f'PASS: 2 RPC descriptors; {calls} Call requests; {responses} Call responses; '
      f'{channels} ChannelFrame JSON round trips; {bindings} binding frame round trips')
print('Scope: generated Protobuf bytes preserve recorded JSON; no service, '
      'metadata authentication, ingress rejection or fault recovery executed')
