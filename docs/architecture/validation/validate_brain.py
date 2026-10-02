#!/usr/bin/env python3
"""Static Brain generation/plan vectors; no model, database or tool is executed.

JSON fixture bodies use only ASCII object keys and integer numbers, for which
the compact sorted encoder below agrees with JCS. This is not a JCS runtime.
"""
import copy
import hashlib
import json
from pathlib import Path

from jsonschema import Draft202012Validator
from protocol.schema import validate
from protocol.runtime_rules import check_brain_plan, check_plan_installation, check_plan_expansion, inspect_plan_materialization

ROOT = Path(__file__).resolve().parents[1] / 'contracts'
GENERATION = json.loads((ROOT / 'schemas/brain-generation.schema.json').read_text())
Draft202012Validator.check_schema(GENERATION)


def encoded(value):
    return json.dumps(value, sort_keys=True, ensure_ascii=False, separators=(',', ':'), allow_nan=False).encode('utf-8')


def local_refs(value):
    if isinstance(value, dict):
        if '$local_ref' in value:
            yield value['$local_ref']
        else:
            for item in value.values():
                yield from local_refs(item)
    elif isinstance(value, list):
        for item in value:
            yield from local_refs(item)


def depth(value):
    children = value.values() if isinstance(value, dict) else value if isinstance(value, list) else []
    return 1 + max((depth(item) for item in children), default=0)


def content_refs(value):
    if isinstance(value, dict):
        if not validate('ContentRef', value):
            yield value
        else:
            for item in value.values():
                yield from content_refs(item)
    elif isinstance(value, list):
        for item in value:
            yield from content_refs(item)


def resolve_generation(fixture):
    generation = fixture['generation']
    errors = ['generation_schema: ' + error.message for error in Draft202012Validator(GENERATION).iter_errors(generation)]
    if errors:
        return None, errors
    if len(encoded(generation)) > 1048576 or depth(generation) > 32 or len(encoded(generation['proposal'])) > 131072:
        return None, ['generation_bound: aggregate, proposal or depth limit exceeded']
    contents = {item['local_id']: item for item in generation['contents']}
    if len(contents) != len(generation['contents']):
        return None, ['generation_identity: duplicate local_id']
    if any(ref not in contents for ref in local_refs(generation)):
        return None, ['generation_reference: missing local content']
    prepared = fixture['publication']
    if set(prepared) != set(contents):
        return None, ['publication_identity: exact prepared content set required']
    for field in ('content_id', 'upload_id', 'command_id'):
        if len({item[field] for item in prepared.values()}) != len(prepared):
            return None, ['publication_identity: distinct original save identities required']
    visiting, refs, records, bodies = set(), {}, {}, {}

    def resolve(value):
        if isinstance(value, dict):
            if '$local_ref' in value:
                return publish(value['$local_ref'])
            return {key: resolve(item) for key, item in value.items()}
        if isinstance(value, list):
            return [resolve(item) for item in value]
        return value

    def publish(local_id):
        if local_id in visiting:
            raise ValueError('generation_cycle: local content references form a cycle')
        if local_id in refs:
            return copy.deepcopy(refs[local_id])
        visiting.add(local_id)
        content = contents[local_id]
        body = resolve(content['body'])
        sources = copy.deepcopy(fixture['input_manifest'])
        for ref in local_refs(content['body']):
            if refs[ref] not in sources:
                sources.append(refs[ref])
        if isinstance(body, dict) and body.get('schema_version') == 'brain-plan/1':
            body['source_refs'] = sources
            plan_errors = check_brain_plan(body)
            if plan_errors:
                raise ValueError('generation_plan: ' + '; '.join(plan_errors))
        raw = encoded(body) if content['media_type'] == 'application/json' else body.encode('utf-8')
        if len(raw) > 131072:
            raise ValueError('generation_bound: content UTF-8 byte limit exceeded')
        identity = prepared[local_id]
        ref = {key: identity[key] for key in ('tenant_id', 'owner_id', 'content_id', 'version')}
        ref.update(hash='sha256:' + hashlib.sha256(raw).hexdigest(), media_type=content['media_type'], byte_length=len(raw))
        if validate('ContentRef', ref):
            raise ValueError('publication_identity: invalid prepared content identity')
        if identity.get('stored_ref', ref) != ref:
            raise ValueError('publication_conflict: original stored content differs')
        refs[local_id] = ref
        bodies[local_id] = body
        records[local_id] = {'content_ref': ref, 'source_refs': sources,
                             'command_id': identity['command_id'], 'upload_id': identity['upload_id']}
        visiting.remove(local_id)
        return copy.deepcopy(ref)

    try:
        for local_id in contents:
            publish(local_id)
        proposal = resolve(generation['proposal'])
    except ValueError as error:
        return None, [str(error)]
    errors = validate('Proposal', proposal)
    if len(encoded(proposal)) > 131072:
        errors.append('generation_bound: resolved proposal too large')
    allowed = fixture['visible_refs'] + list(refs.values())
    if any(ref not in allowed for value in [proposal, *bodies.values()] for ref in content_refs(value)):
        errors.append('generation_reference: reference is neither visible nor an exact new output')
    return {'proposal': proposal, 'refs': refs, 'records': records, 'bodies': bodies}, errors


def mutate(value, edit):
    keys = edit['path'].strip('/').split('/')
    node = value
    for key in keys[:-1]:
        node = node[int(key)] if isinstance(node, list) else node[key]
    key = int(keys[-1]) if isinstance(node, list) else keys[-1]
    if edit['op'] == 'remove':
        del node[key]
    else:
        node[key] = edit['value']


def main():
    fixture = json.loads((ROOT / 'examples/brain/generation-and-plan.json').read_text())
    resolved, errors = resolve_generation(fixture)
    assert not errors, errors
    # A lost save reply is represented by the same prepared identity plus the
    # owner's now-queryable exact stored ref, not by a second generated result.
    recovered = copy.deepcopy(fixture)
    for local_id, ref in resolved['refs'].items():
        recovered['publication'][local_id]['stored_ref'] = ref
    after_restart, errors = resolve_generation(recovered)
    assert not errors and after_restart == resolved, errors
    assert not list(local_refs(resolved['proposal']))
    assert all(ref in resolved['records']['plan']['source_refs'] for ref in fixture['input_manifest'])
    assert resolved['refs']['report'] in resolved['records']['plan']['source_refs']
    plan = resolved['bodies']['plan']
    assert not check_plan_installation(resolved['proposal'], plan, resolved['refs']['plan'], plan['task_ref'], plan['goal_revision'])
    assert any(error.startswith('plan_goal:') for error in check_plan_installation(resolved['proposal'], plan, resolved['refs']['plan'], plan['task_ref'], plan['goal_revision'] + 1))
    revised_plan = copy.deepcopy(plan)
    revised_plan['revision'] = 2
    revised_proposal = copy.deepcopy(resolved['proposal'])
    revised_proposal['plan_delta']['base_plan_ref'] = resolved['refs']['plan']
    revised_ref = copy.deepcopy(resolved['refs']['plan'])
    revised_ref['version'] = 2
    revised_proposal['plan_delta']['next_plan_ref'] = revised_ref
    assert not check_plan_installation(revised_proposal, revised_plan, revised_ref, plan['task_ref'], plan['goal_revision'], resolved['refs']['plan'], plan)
    assert any(error.startswith('plan_base:') for error in check_plan_installation(resolved['proposal'], plan, resolved['refs']['plan'], plan['task_ref'], plan['goal_revision'], resolved['refs']['plan'], plan))
    assert any(error.startswith('plan_revision:') for error in check_plan_installation(resolved['proposal'], revised_plan, resolved['refs']['plan'], plan['task_ref'], plan['goal_revision']))
    negatives = json.loads((ROOT / 'examples/brain/invalid-generation.json').read_text())
    for case in negatives:
        bad = copy.deepcopy(fixture)
        for edit in case['edits']:
            mutate(bad, edit)
        _, errors = resolve_generation(bad)
        assert any(case['expect'] + ':' in error for error in errors), (case['name'], errors)
    plan_fixture = json.loads((ROOT / 'examples/brain/plan-materialization.json').read_text())
    instruction_plan=copy.deepcopy(plan_fixture['plan'])
    instruction_plan['steps'][1].pop('action_template')
    expanded_plan=copy.deepcopy(plan_fixture['plan']);expanded_plan['revision']=2
    old_ref=copy.deepcopy(resolved['refs']['plan'])
    old_bytes=encoded(instruction_plan);old_ref['hash']='sha256:'+hashlib.sha256(old_bytes).hexdigest();old_ref['byte_length']=len(old_bytes)
    new_ref=copy.deepcopy(old_ref);new_ref['version']=2
    new_bytes=encoded(expanded_plan);new_ref['hash']='sha256:'+hashlib.sha256(new_bytes).hexdigest();new_ref['byte_length']=len(new_bytes)
    expansion=copy.deepcopy(resolved['proposal'])
    expansion['actions']=[];expansion['plan_delta']={'base_plan_ref':old_ref,'next_plan_ref':new_ref}
    args=(expanded_plan,new_ref,expanded_plan['task_ref'],expanded_plan['goal_revision'],old_ref,instruction_plan,'write')
    assert not check_plan_expansion(expansion,*args)
    direct=copy.deepcopy(expansion);direct['actions']=[copy.deepcopy(expanded_plan['steps'][1]['action_template'])]
    assert any(error.startswith('plan_expansion:') for error in check_plan_expansion(direct,*args))
    stale=copy.deepcopy(expansion);stale['plan_delta']['base_plan_ref']['version']=3
    assert any(error.startswith('plan_base:') for error in check_plan_expansion(stale,*args))
    assert any(error.startswith('plan_expansion:') for error in check_plan_expansion(expansion,*args[:-1],'absent'))
    assert any(error.startswith('plan_expansion:') for error in check_plan_expansion(expansion,*args[:-1],'assess'))
    # The successor's step_output is scoped to the new plan revision: old
    # admitted mappings cannot accidentally complete the expanded predecessor.
    expansion_state=copy.deepcopy(plan_fixture['state']);expansion_state['plan_ref']['revision']=2
    blocked=inspect_plan_materialization(expanded_plan,'read',expansion_state,plan_fixture['capabilities'])
    assert blocked['status']=='waiting',blocked
    for admitted in expansion_state['admissions']:admitted['plan_revision']=2
    ready=inspect_plan_materialization(expanded_plan,'read',expansion_state,plan_fixture['capabilities'])
    assert ready['status']=='ready',ready
    for case in plan_fixture['cases']:
        state = copy.deepcopy(plan_fixture['state'])
        plan = copy.deepcopy(plan_fixture['plan'])
        for edit in case.get('state_edits', []):
            mutate(state, edit)
        for edit in case.get('plan_edits', []):
            mutate(plan, edit)
        result = inspect_plan_materialization(plan, case['step_id'], state, plan_fixture['capabilities'])
        assert result['status'] == case['expect'], (case['name'], result)
        if 'arguments' in case:
            assert result['candidate']['arguments'] == case['arguments'], (case['name'], result)
        if result['status'] == 'ready':
            assert result == inspect_plan_materialization(plan, case['step_id'], state, plan_fixture['capabilities'])
    print(f"Brain static vectors passed: generation/recovery, 5 plan-install checks, {len(negatives)} rejected generations, {len(plan_fixture['cases'])} plan branches. No service or fault experiment executed.")
    print('Instruction-only expansion: full successor plan accepted; direct actions, stale baseline, absent/template steps and cross-revision predecessor reuse rejected; current revision output resolves')


if __name__ == '__main__':
    main()
