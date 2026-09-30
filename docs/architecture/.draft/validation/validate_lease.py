#!/usr/bin/env python3
"""Check bounded offline billing corrections; referenced bills remain test premises."""
import copy
import json
from pathlib import Path
from protocol.traces import check_trace


def main():
    path = Path(__file__).resolve().parents[1] / 'contracts/examples/protocol/61-lease-final-correction.json'
    trace = json.loads(path.read_text())
    errors = check_trace(trace)
    if errors:
        raise AssertionError(errors)
    overrun = copy.deepcopy(trace)
    exchange = overrun['events'][5]['exchange']
    exchange['request']['payload']['uses'][0]['used_cost']['amount'] = '2.5'
    exchange['request']['payload']['cumulative_cost']['amount'] = '2.5'
    exchange['response']['output']['settled_cost']['amount'] = '2.5'
    errors = check_trace(overrun)
    if errors:
        raise AssertionError('verified original cost above allocation must remain recordable: ' + str(errors))

    def reject(label, rule, edit):
        bad = copy.deepcopy(trace)
        edit(bad['events'][5]['exchange'])
        errors = check_trace(bad)
        if not any(rule + ':' in error for error in errors):
            raise AssertionError(f'{label}: expected {rule}, got {errors}')

    reject('stale bill', 'lease_billing',
           lambda x: x['request']['payload']['uses'][0]['billing_ref'].update(revision=3))
    reject('different bill', 'lease_billing',
           lambda x: x['request']['payload']['uses'][0]['billing_ref'].update(id='bill_' + 'b' * 32))
    reject('different operation', 'lease_use_identity',
           lambda x: x['request']['payload']['uses'][0].update(operation_id='operation_' + 'b' * 32))
    reject('another use after final', 'lease_correction',
           lambda x: x['request']['payload']['uses'][0].update(use_id='use_' + 'b' * 32))
    reject('closed use reopened', 'lease_use_closed',
           lambda x: x['request']['payload']['uses'][0].update(closed=False))
    reject('changed closure', 'lease_correction',
           lambda x: x['request']['payload']['closure_ref'].update(id='closure_' + 'b' * 32))
    reject('replaced first settlement', 'lease_correction',
           lambda x: x['response']['output']['final_settlement_ref'].update(id='settlement_' + 'b' * 32))
    reject('repeated use in report', 'lease_use_identity',
           lambda x: x['request']['payload']['uses'].append(copy.deepcopy(x['request']['payload']['uses'][0])))
    reject('new units after final', 'lease_correction',
           lambda x: x['request']['payload']['cumulative_units'].update(amount='2'))
    reject('old report revision', 'lease_usage_revision',
           lambda x: x['request']['payload'].update(usage_revision=3))

    def unchanged_cost(x):
        x['request']['payload']['cumulative_cost']['amount'] = '0.3'
        x['request']['payload']['uses'][0]['used_cost']['amount'] = '0.3'
        x['response']['output']['settled_cost']['amount'] = '0.3'
    reject('same amount under new command', 'lease_correction', unchanged_cost)
    print('Offline lease corrections: 2 valid traces, 11 invalid mutations passed; bill authenticity and durable delivery are not exercised.')


if __name__ == '__main__':
    main()
