#!/usr/bin/env python3
"""Check recorded rollback authority and single-plan execution, not live recovery."""
from copy import deepcopy
import json
from pathlib import Path

from protocol.governance_rules import confirmation_intent_hash
from protocol.schema import validate
from protocol.traces import check_trace

FIXTURES = Path(__file__).resolve().parents[3] / 'contracts/examples/protocol'


def refresh_confirmation(trace, command):
    for item in trace.get('confirmations', []):
        if item['consumer_command_id'] == command['command_id']:
            item['consumer_command'] = deepcopy(command)
            item['intent_hash'] = confirmation_intent_hash(item['owner_id'], command)


def main():
    rollback = json.loads((FIXTURES / '65-approved-rollback.json').read_text())
    run = json.loads((FIXTURES / '39-evaluation-cancel.json').read_text())
    checked = 0

    def expect(trace, rule=None):
        nonlocal checked
        errors = check_trace(trace)
        if rule is None and errors:
            raise AssertionError(errors)
        if rule is not None and not any(rule + ':' in error for error in errors):
            raise AssertionError(f'Expected {rule}, got {errors}')
        checked += 1

    expect(rollback)
    # Removing, forging or broadening the old approval must not be hidden by
    # valid evidence and confirmation for the new version.
    for field, value in [('id', 'approval_' + 'f' * 32),
                         ('revision', 99), ('owner_id', 'owner_' + 'f' * 32)]:
        case = deepcopy(rollback)
        event = case['events'][18]['exchange']
        event['request']['payload']['approval']['rollback_approval_ref'][field] = value
        event['response']['output']['rollback_approval_ref'][field] = value
        refresh_confirmation(case, event['request'])
        expect(case, 'rollback_approval')

    case = deepcopy(rollback)
    original = case['events'][7]['exchange']
    for approval in (original['request']['payload']['approval'], original['response']['output']):
        approval['expires_at'] = '2026-09-26T00:30:00Z'
    refresh_confirmation(case, original['request'])
    expect(case, 'rollback_approval')

    case = deepcopy(rollback)
    case['events'][6]['exchange']['response']['output']['report']['contract_gate'] = 'fail'
    expect(case, 'rollback_evidence')

    case = deepcopy(rollback)
    old_revoke = deepcopy(case['events'][22])
    request = old_revoke['exchange']['request']
    request['command_id'] = 'command_' + 'e' * 32
    request['target_id'] = case['events'][7]['exchange']['response']['output']['approval_id']
    response = old_revoke['exchange']['response']
    response['command_id'] = request['command_id']
    response['output'] = deepcopy(case['events'][7]['exchange']['response']['output'])
    response['output'].update(state='revoked', revision=2)
    old_revoke['at'] = response['decided_at'] = '2026-09-26T00:01:21Z'
    case['events'].insert(23, old_revoke)
    expect(case, 'activation_approval')

    case = deepcopy(rollback)
    case['events'][23]['exchange']['request']['payload']['approval_id'] = case['events'][18]['exchange']['response']['output']['approval_id']
    expect(case, 'activation_approval')

    approval = deepcopy(rollback['events'][18]['exchange']['response']['output'])
    del approval['rollback_approval_ref']
    if not validate('ReleaseApproval', approval):
        raise AssertionError('non-null rollback lock needs an independent approval reference')
    approval = deepcopy(rollback['events'][7]['exchange']['response']['output'])
    approval['rollback_approval_ref'] = deepcopy(rollback['events'][18]['exchange']['response']['output']['rollback_approval_ref'])
    if not validate('ReleaseApproval', approval):
        raise AssertionError('null rollback lock cannot carry a rollback approval reference')
    checked += 2

    duplicate = deepcopy(run['events'][3])
    request = duplicate['exchange']['request']
    request['command_id'] = 'command_' + 'f' * 32
    request['payload']['run_id'] = 'run_' + 'f' * 32
    response = duplicate['exchange']['response']
    response['command_id'] = request['command_id']
    response['output']['run_id'] = request['payload']['run_id']
    case = deepcopy(run)
    case['events'].insert(4, duplicate)
    expect(case, 'run_once')

    case = deepcopy(case)
    case['events'][4]['exchange']['response'] = {
        'command_id': request['command_id'], 'stage': 'rejected',
        'decided_at': duplicate['at'],
        'error': {'code': 'precondition_failed', 'message': 'Plan already has a logical run',
                  'retry': 'after_change', 'related_id': run['events'][3]['exchange']['response']['output']['run_id']}}
    expect(case)

    case = deepcopy(run)
    case['events'][3]['exchange']['response']['output']['total_samples'] = 2
    expect(case, 'run_denominator')
    print(f'PASS: {checked} targeted release/run cases; replay, rollback work and reopen use the independent old approval')
    print('Scope: recorded identities, evidence and refusal rules; no live UI, environment isolation or durable recovery tested')


if __name__ == '__main__':
    main()
