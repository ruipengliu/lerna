"""Check bounded recorded traces. This checks claimed observations, not a live service."""
from copy import deepcopy
from jsonschema import Draft202012Validator
from .schema import validate, walk
from .exchanges import instant, validate_exchange


def same_receipt(original, view):
    # Whole-output redaction is a disclosure projection; durable decision is unchanged.
    metadata=lambda x:{k:v for k,v in x.items() if k not in ('output','redacted')}
    return metadata(original)==metadata(view) and (view.get('redacted') is True and 'output' not in view or original.get('output')==view.get('output'))


def check_trace(trace):
    errors=validate('Scenario',trace)
    if errors:return errors
    for cap in trace['capabilities']:
        if any(k in n and not n[k].startswith('#/') for n in walk(cap['input_schema']) for k in ('$ref','$dynamicRef')):
            return ['capability_schema: external schema references are not allowed in resolved fixtures']
        try:Draft202012Validator.check_schema(cap['input_schema'])
        except Exception:return ['capability_schema: invalid pinned input schema']
        if cap['input_schema'].get('type')!='object' or cap['input_schema'].get('additionalProperties') is not False:
            return ['capability_schema: fixture root parameters must be a closed object']
    commands={}; gates={}; tombstones=set(); uses={}; approvals={}; consumed={}; decisions={}; operations={}; operation_tasks={}; operation_orchestrators={}; delegations={}; inputs={}; tasks={}; activations={}; billing_sources={}; grant_billing={}; billing_jobs={}; billing_digests={}
    request_index={x['request_id']:x for x in trace['input_requests']}
    approval_index={x['approval_id']:x for x in trace['approvals']}
    for index,event in enumerate(trace['events']):
        local=[]
        def err(code,detail):local.append(code+': '+detail)
        if 'recovery' in event:
            r=event['recovery'];a=r['auth'];key=(a['tenant_id'],a['logical_service_id'],r['lookup']['command_id'])
            if r['lookup']['command_id']!=r['original_command_id'] or r['response']['command_id']!=r['original_command_id']:
                err('recovery_binding','lookup and response must use original command')
            if key not in commands or not same_receipt(commands[key][1],r['response']):
                err('recovery_original','no matching original durable receipt at this owner/tenant')
            errors.extend(f'event {index}: '+x for x in local)
            continue
        x=event['exchange'];req=x['request'];res=x['response'];p=req['payload'];name=req['method'];a=x['auth'];target=req['target_id']
        local+=validate_exchange(x,trace['capabilities'])
        if local:
            errors.extend(f'event {index}: '+e for e in local);continue
        is_command='command_id' in req
        rejected=res.get('stage')=='rejected' or ('error' in res and 'stage' not in res)
        rejection=res.get('error',{}).get('code')
        output=res.get('output')
        if is_command:
            key=(a['tenant_id'],a['logical_service_id'],req['command_id'])
            if key in commands:
                old_request,old_receipt=commands[key]
                if old_request!=req:
                    if rejection!='idempotency_conflict':err('idempotency_conflict','changed request must be rejected without overwriting original')
                    errors.extend(f'event {index}: '+e for e in local);continue
                if old_receipt['stage'] in ('applied','rejected'):
                    if not same_receipt(old_receipt,res):err('receipt_immutable','terminal original command receipt changed')
                    errors.extend(f'event {index}: '+e for e in local);continue
            elif instant(req['expires_at'])<=instant(event['at']) and not rejected:
                err('admission_expiry','new expired command cannot be accepted')
            commands[key]=(deepcopy(req),deepcopy(res))
        if name in ('task.input','task.accept_result','interaction.input'):
            original=request_index.get(p['request_id'])
            if not original:
                err('request_context','no authoritative request fixture')
            else:
                invalid=p['request_revision']!=original['revision']
                if name in ('task.input','task.accept_result') and (target!=original['task_id'] or a['logical_service_id']!=original['owner_id']):
                    invalid=True
                if name=='task.accept_result':
                    invalid|=original['kind']!='acceptance' or p['candidate_hash']!=original.get('candidate_hash') or p['goal_revision']!=original.get('goal_revision') or p['confirmation_ref']!=original.get('confirmation_ref')
                elif name=='task.input':invalid|=original['kind']!='clarification'
                if name!='task.accept_result':
                    invalid|=any(c not in p['preview_refs'] for c in original['required_content_refs'])
                if invalid and not rejected:err('input_version','request kind/revision/preview or trusted candidate confirmation differs')
                if name in ('task.input','task.accept_result') and p['request_id'] in consumed and not rejected:
                    err('input_consumption','different commands consumed same request')
                if name in ('task.input','task.accept_result') and not rejected:
                    consumed[p['request_id']]=req['command_id']
        if name in ('evaluation.approval_check','evaluation.approval_lease'):
            approval=approval_index.get(p['approval_id'])
            if approval is None:
                err('approval_context','missing authority snapshot')
            else:
                replayed_use=name.endswith('check') and p['use_id'] in approvals
                invalid=approval['state']!='active' or instant(approval['expires_at'])<=instant(event['at']) or any(p[k]!=approval[k] for k in ('target_id','lock_id')) or p['instance_id'] not in approval.get('eligible_instance_ids',[approval['instance_id']])
                if name=='evaluation.approval_lease':invalid|=approval['max_offline_window_ms']==0 or not approval['already_active']
                if invalid and not rejected and not replayed_use:err('approval_authority','current approval does not cover requested use')
                if not rejected and output:
                    deadline=output['start_before'] if name.endswith('check') else output['continue_until']
                    replayed_use=name.endswith('check') and p['use_id'] in approvals
                    if not replayed_use and (output['approval_revision']!=approval['revision'] or instant(deadline)>instant(approval['expires_at']) or instant(deadline)<=instant(event['at'])):
                        err('approval_window','new use must bind current revision and finite valid window')
                    if name.endswith('lease'):
                        millis=(instant(deadline)-instant(event['at'])).total_seconds()*1000
                        if millis>min(p['requested_window_ms'],approval['max_offline_window_ms']):err('approval_window','offline window exceeds granted maximum')
        if name=='execution.invoke':
            operation_tasks[p['operation_id']]=p['task_id']
            operation_orchestrators[p['operation_id']]=p['orchestrator_id']
            gate=p['control_snapshot']['gate'];gkey=(a['tenant_id'],gate['orchestrator_id'],gate['task_id'],target)
            known=gates.get(gkey,gate)
            blocked=(a['tenant_id'],p['orchestrator_id'],p['task_id'],p['operation_id']) in tombstones or known['status']!='active' or p['goal_revision']!=known['goal_revision']
            if blocked and not rejected:err('cancel_or_goal_gate','known cancellation or revised goal must prevent delayed invocation')
            if not rejected and output and output['execution_state']=='started':
                if known['control']!='running' or instant(event['at'])>=instant(p['control_snapshot']['start_before']) or instant(event['at'])>=instant(p['deadline']):err('startup_gate','paused or expired action cannot start')
            if not rejected:
                if gate['control_revision']>=known['control_revision']:gates[gkey]=deepcopy(gate)
        if name=='execution.control' and not rejected and output:
            incoming=p['gate'];gkey=(a['tenant_id'],incoming['orchestrator_id'],incoming['task_id'],target);known=gates.get(gkey)
            if known:
                if incoming['control_revision']==known['control_revision'] and incoming!=known:err('control_conflict','same revision cannot change gate')
                if incoming['control_revision']>known['control_revision'] and incoming['goal_revision']<known['goal_revision']:
                    err('goal_revision','new gate cannot decrease goal revision')
                chosen=incoming if incoming['control_revision']>known['control_revision'] else known
                if known['status']!='active' and incoming['control_revision']>known['control_revision'] and incoming['status']!=known['status']:
                    err('terminal_gate','terminal task gate cannot reopen or change terminal state')
            else:chosen=incoming
            if output['gate']!=chosen:err('control_order','receiver must preserve highest known effective gate')
            gates[gkey]=deepcopy(chosen)
        if name=='execution.cancel' and not rejected:
            tombstones.add((a['tenant_id'],p['orchestrator_id'],p['task_id'],p['operation_id']))
        if rejected or output is None:errors.extend(f'event {index}: '+e for e in local);continue
        if name=='brain.decide':
            billing_sources[(a['tenant_id'],'brain_decision',p['decision_id'])]=(p['task_id'],p['orchestrator_id'],a['logical_service_id'])
        if name=='execution.invoke':
            billing_sources[(a['tenant_id'],'execution_operation',p['operation_id'])]=(p['task_id'],p['orchestrator_id'],a['logical_service_id'])
        if name=='grant.use' and output['decision']=='allowed':
            grant_billing[(a['tenant_id'],'grant_use',output['use_id'])]=(p['operation_id'],a['logical_service_id'])
        if name=='budget.allocate':
            billing_sources[(a['tenant_id'],'budget_allocation',output['allocation_id'])]=(output['parent_task_id'],output['owner_id'],output['receiver_id'])
        if name=='task.billing_reconcile':
            source_key=(a['tenant_id'],p['source_kind'],p['source_id'])
            source=billing_sources.get(source_key)
            if p['source_kind']=='grant_use':
                grant=grant_billing.get(source_key)
                if grant:
                    operation_id,source_owner=grant
                    if operation_id in operation_tasks:
                        source=(operation_tasks[operation_id],operation_orchestrators[operation_id],source_owner)
            if source is None:
                err('billing_source','bounded trace lacks the original trusted source/task binding')
            elif source!=(target,a['logical_service_id'],a.get('sender_service_id')):
                err('billing_source','authenticated source, original Orchestrator and Task binding differ')
            if output['resource_id']!=target:err('billing_task','wakeup job belongs to another Task')
            job=billing_jobs.get(source_key)
            if job and job!=output['job_id']:err('billing_job','same source must retain one reconcile responsibility slot')
            billing_jobs[source_key]=output['job_id']
            rev_key=(*source_key,p['usage_revision']);digest=billing_digests.get(rev_key)
            if digest and digest!=p['usage_digest']:err('billing_revision_conflict','same source usage revision claims another bill digest')
            billing_digests[rev_key]=p['usage_digest']
        if name=='extensions.activate':activations[p['activation_id']]=deepcopy(p)
        if name in ('grant.use','grant.use.get'):
            uid=output['use_id']
            if uid in uses and uses[uid]!=output:err('use_immutable','query or new command cannot change original use/deadline')
            uses[uid]=deepcopy(output)
        if name=='evaluation.approval_check':
            uid=output['use_id']
            if uid in approvals and approvals[uid]!=output:err('approval_use_immutable','same use cannot change binding/deadline')
            approvals[uid]=deepcopy(output)
        if name in ('brain.decide','brain.get','brain.cancel'):
            did=output['decision_id'];old=decisions.get(did)
            if old:
                if output['snapshot_revision']!=old['snapshot_revision']:err('decision_snapshot','decision rebound to another snapshot')
                if old['status'] in ('completed','failed','cancelled') and output['status']!=old['status']:err('decision_terminal','terminal decision reopened')
                if old.get('model_call') and output.get('model_call',{}).get('model_call_id')!=old['model_call']['model_call_id']:err('decision_physical_call','same decision launched another model call')
            decisions[did]=deepcopy(output)
        if name.startswith('execution.') and 'effect' in output:
            oid=output['operation_id'];old=operations.get(oid)
            if old:
                if output['revision']<old['revision']:err('operation_revision','operation observation regressed')
                if old['execution_state']=='closed' and output['execution_state']!='closed':err('operation_reopen','closed operation reopened sends')
                if old['effect']=='applied' and output['effect']!='applied':err('effect_fact','confirmed historical effect cannot disappear')
            operations[oid]=deepcopy(output)
        if name.startswith('collaboration.') and 'delegation_id' in output:
            did=output['delegation_id'];old=delegations.get(did)
            facts=event.get('delegation_facts')
            if facts:
                # Supplied authority observations, not a second domain state machine.
                has_gap=(facts['creation'] in ('unknown','not_admitted') or facts['query_gap'] or facts['input_pending']
                         or output['control_pending'] or output['effects_pending'] or facts['closure_pending']
                         or facts['ending'] and (not facts['settlement_final'] or not facts['closure_saved']))
                expected=('closed' if facts['closure_saved'] else 'reconciling' if has_gap
                          else 'active' if facts['creation']=='mapped' else 'preparing')
                if output['phase']!=expected:err('delegation_projection','phase differs from the ordered projection of original responsibility records')
                if facts['creation']=='mapped' and not ('child_task_id' in output or 'remote_binding' in output):err('delegation_mapping','recorded mapping requires its fixed child identity')
                if facts['closure_saved'] and (not facts['settlement_final'] or facts['closure_pending'] or facts['input_pending']):err('delegation_closure','durable closure cannot leave required input or settlement responsibility unfinished')
            if old:
                for k in ('child_task_id','remote_binding'):
                    if k in old and output.get(k)!=old[k]:err('delegation_identity','original child mapping changed')
                if old['revision']==output['revision'] and old!=output:err('delegation_revision','same revision cannot change its recorded projection')
                # closed is evidence of a durable Closure; it is not an inferred enum edge.
                if old['phase']=='closed' and output['phase']!='closed':err('delegation_terminal','closed responsibility reopened')
            delegations[did]=deepcopy(output)
        if name.startswith('interaction.input'):
            iid=output['input_id'];old=inputs.get(iid)
            if old:
                for k in ('surface_id','request_id','request_revision','answer_ref','preview_refs','target_command_id'):
                    if old[k]!=output[k]:err('input_identity','forwarded input identity or answer changed')
                if old['state']=='sending' and output['state']=='withdrawn':err('withdrawal_race','sending cannot promise unconsumed withdrawal')
            inputs[iid]=deepcopy(output)
        if name in ('task.pause','task.resume','task.cancel'):
            old=tasks.get(target)
            if old:
                if old['status']!='active':err('task_terminal','terminal task cannot receive new control success')
                if req['expected_revision']!=old['revision']:err('expected_revision','successful control did not compare known revision')
                if output['control_revision']<=old['control_revision']:err('task_control_revision','control did not advance its revision')
                updated=deepcopy(old)
                for k in ('revision','control_revision','status','control'):updated[k]=output[k]
                tasks[target]=updated
        if name in ('task.submit','task.read','task.revise'):
            tid=output['task_id'];old=tasks.get(tid)
            if old:
                for k in ('orchestrator_id','submit_command_id'):
                    if output[k]!=old[k]:err('task_identity','fixed Orchestrator or original submit changed')
                if old['status']!=output['status'] and output['control_revision']<=old['control_revision']:err('task_control_revision','terminal transition must advance control revision')
                if name=='task.revise' and output['control_revision']<=old['control_revision']:err('task_control_revision','goal revision must advance control revision')
                if old['status']!='active' and output['status']!=old['status']:err('task_terminal','terminal task reopened')
                if name=='task.revise' and output['goal_revision']!=old['goal_revision']+1:err('goal_revision','revised goal did not increment exactly once')
            tasks[tid]=deepcopy(output)
        if name=='task.result' and output['status']=='succeeded':
            result=output['result'];task=tasks.get(result['task_id'])
            if task:
                if task['status'] in ('failed','cancelled'):err('result_status','known task is terminal without success')
                if result['goal_revision']!=task['goal_revision']:err('result_version','result is stale against current goal')
                conditions={c['requirement_id']:c for c in result['condition_results']}
                condition_ids=set(conditions)
                required=[r['requirement_id'] for r in task['requirements'] if r['required']]
                if len(conditions)!=len(result['condition_results']):err('result_conditions','duplicate condition result')
                if any(conditions[k]['verdict']!='pass' for k in required if k in conditions):err('result_conditions','required condition has not passed')
                if required and all(k in conditions for k in required):
                    levels={'verified':0,'assessed':1,'user_accepted':2}
                    if levels[result['completion_basis']]!=max(levels[conditions[k]['basis']] for k in required):err('result_basis','basis differs from weakest required evidence')
                if any(r['required'] and r['requirement_id'] not in condition_ids for r in task['requirements']):err('result_conditions','required task condition missing')
            if any((o['effect']=='unknown' or o['may_apply_later'] is not False) and (operation_tasks.get(oid)==result['task_id'] or task and oid in task['open_effects']) for oid,o in operations.items()):
                err('result_effect','trace still has unresolved possible external effects')
        errors.extend(f'event {index}: '+e for e in local)
    if not any('schema:' in error for error in errors):
        from .runtime_rules import check_trace_rules as runtime_trace
        from .governance_rules import check_trace_rules as governance_trace
        from .content_rules import check_trace_rules as content_trace
        errors += runtime_trace(trace)
        errors += governance_trace(trace)
        errors += content_trace(trace)
        from .collection_rules import check_trace_rules as collection_trace
        errors += collection_trace(trace)
    return errors
