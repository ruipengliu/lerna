#!/usr/bin/env python3
"""Validate frozen draft messages and bounded documentation traces."""
import copy
import json
from pathlib import Path
from protocol.schema import METHODS
from protocol.traces import check_trace

ROOT=Path(__file__).resolve().parents[1] / 'contracts'


def mutate(value, edits):
    for edit in edits:
        target=value
        parts=edit['path'].strip('/').split('/')
        for key in parts[:-1]:target=target[int(key)] if isinstance(target,list) else target[key]
        key=int(parts[-1]) if isinstance(target,list) else parts[-1]
        if edit['op']=='remove':del target[key]
        elif edit['op']=='set':target[key]=edit['value']
        else:raise ValueError('unsupported fixture mutation')
    return value


def main():
    fixtures={}
    covered=set()
    for path in sorted((ROOT/'examples/protocol').glob('[0-9][0-9]-*.json')):
        value=json.loads(path.read_text());fixtures[path.name]=value
        errors=check_trace(value)
        if errors:raise AssertionError(f'{path.name}: '+ '\n'.join(errors))
        covered.update(e['exchange']['request']['method'] for e in value['events'] if 'exchange' in e)
    cases=json.loads((ROOT/'examples/protocol/invalid-mutations.json').read_text())
    for case in cases:
        errors=check_trace(mutate(copy.deepcopy(fixtures[case['fixture']]),case['edits']))
        if not any(case['expect']+':' in e for e in errors):
            raise AssertionError(f"{case['name']}: expected {case['expect']}, got {errors}")
    frozen={name for name,spec in METHODS.items() if spec['status']=='frozen-draft'}
    if covered!=frozen:raise AssertionError(f'method coverage mismatch: {frozen-covered}, {covered-frozen}')
    print(f'PASS: {len(fixtures)} valid traces; {len(cases)} invalid mutations rejected for their stated rule; {len(covered)} frozen methods exercised')
    print(f'Registry: {len(METHODS)} methods, {len(METHODS)-len(frozen)} reserved; protocol is an unpublished draft profile')
    print('Scope: structure and recorded semantic relations only; no authentication, service, concurrency, durability or interoperability runtime tested')


if __name__=='__main__':main()
