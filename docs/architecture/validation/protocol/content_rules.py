"""Recorded content/Memory/Surface relations, without claiming real authorization or bytes."""
from copy import deepcopy
from datetime import datetime
from hashlib import sha256
import json


METHOD_NAMES = {
    'content.put', 'content.get', 'content.register_copy', 'content.release_copy', 'content.close',
    'memory.query', 'memory.list', 'memory.extract', 'memory.view.open', 'memory.view.pull',
    'memory.view.ack', 'memory.cleanup.get', 'interaction.surface_create',
    'interaction.surface_read', 'interaction.surface_update', 'interaction.surface_list',
    'interaction.present', 'interaction.application_event', 'interaction.request_read',
}


def instant(value):
    return datetime.fromisoformat(value.replace('Z', '+00:00'))


def digest(value):
    return 'sha256:' + sha256(json.dumps(value, sort_keys=True, separators=(',', ':'), ensure_ascii=False).encode()).hexdigest()


def successful(response):
    return response.get('stage') != 'rejected' and 'error' not in response and 'output' in response


def holder_identity(auth):
    # Both fields are authenticated fixture context, not caller-selected payload.
    return auth.get('sender_service_id', auth['actor_id'])


def snapshot_errors(snapshot):
    errors = []
    blocks = snapshot['blocks']
    if len({b['block_id'] for b in blocks}) != len(blocks):
        errors.append('surface_blocks: duplicate block identifiers')
    for block in blocks:
        if 'request_ref' in block and block['request_ref'] not in snapshot['request_refs']:
            errors.append('surface_request: block request is not in snapshot request set')
        if block['kind'] == 'table' and any(len(row) != len(block['columns']) for row in block['rows']):
            errors.append('surface_table: row width differs from declared columns')
        if block['kind'] == 'input':
            fields = block['input_schema']['fields']
            if len({f['name'] for f in fields}) != len(fields):
                errors.append('surface_fields: repeated input field name')
            for field in fields:
                kind = field['type']
                extra = set(field) - {'name', 'label', 'type', 'required'}
                allowed = {'text': {'max_length'}, 'integer': {'minimum', 'maximum'},
                           'boolean': set(), 'choice': {'options'}, 'choices': {'options', 'max_choices'}}[kind]
                if extra != allowed:
                    errors.append('surface_fields: field constraints differ from type')
                if kind == 'integer' and field.get('minimum', 0) > field.get('maximum', 0):
                    errors.append('surface_fields: reversed integer range')
                if 'options' in field:
                    options = field['options']
                    if len({o['id'] for o in options}) != len(options):
                        errors.append('surface_fields: duplicate choice identifiers')
                    if field.get('max_choices', 1) > len(options):
                        errors.append('surface_fields: selection maximum exceeds options')
    return errors


def check_exchange(exchange, capabilities):
    """Call after the shared method input/output schema validation succeeds."""
    req = exchange['request']; name = req['method']; response = exchange['response']
    if name not in METHOD_NAMES:
        return []
    p = req['payload']; target = req['target_id']; errors = []
    def same(a, b, code='content_binding'):
        if a != b:
            errors.append(code + ': correlated fields differ')
    if name == 'content.close':
        if (p['mode'] == 'restrict') != ('policy' in p):
            errors.append('content_restriction: restrict requires policy; close does not replace policy')
    if name == 'memory.query':
        if target not in p['owner_ids']:
            errors.append('memory_owner: queried owner is outside declared owners')
        if not (p['text_terms'] or p['types'] or any(p['scope'].values())):
            errors.append('memory_query_bound: empty unbounded query')
    if name in ('interaction.surface_create', 'interaction.surface_update'):
        errors += snapshot_errors(p['snapshot'])
    if not successful(response):
        return errors
    o = response['output']
    control_get = name == 'content.get' and p.get('mode', 'bytes') == 'control'
    if name == 'content.get' and control_get != (o.get('mode') == 'control'):
        return errors + ['content_get_mode: result does not match requested bytes/control mode']
    if name.startswith('content.'):
        same(o['control']['content_ref'] if control_get else o['content_ref'], p['content_ref'])
        same(target, p['content_ref']['owner_id'], 'content_owner')
        if name == 'content.put':
            same(o['control_revision'], 1, 'content_initial_revision')
            same(o['policy_ref']['owner_id'], target, 'content_owner')
        if name == 'content.get':
            if control_get:
                same(o['copy']['content_ref'], p['content_ref'])
                same(o['copy']['copy_id'], p['copy_id'])
                same(o['copy']['holder_id'], holder_identity(exchange['auth']), 'copy_control_holder')
            else:
                same(o['copy_id'], p['copy_id'])
                if instant(o['expires_at']) <= instant(response['observed_at']):
                    errors.append('content_download_window: download locator already expired')
        if name in ('content.register_copy', 'content.release_copy'):
            for key in p:
                if key in o:
                    same(o[key], p[key], 'copy_binding')
            if name.endswith('register_copy') and (o['use_stopped'] or o['physical_state'] != 'pending'):
                errors.append('copy_registration: registration does not prove delivery or cleaning')
            if o['physical_state'] == 'complete' and (not o['use_stopped'] or not o['evidence_refs']):
                errors.append('copy_cleanup: complete needs use stopped and cleanup evidence')
            if o['physical_state'] in ('residual', 'unknown') and 'residual_reason' not in o:
                errors.append('copy_cleanup: unresolved physical state requires reason')
        if name == 'content.close':
            same(o['state'], 'closed' if p['mode'] == 'close' else 'restricted', 'content_closure')
            if o['control_revision'] <= req['expected_revision']:
                errors.append('content_revision: control did not advance')
            if 'cleanup_ref' not in o:
                errors.append('content_closure: closing must retain cleanup responsibility')
    if name in ('memory.query', 'memory.list', 'interaction.surface_list'):
        same(o['query_id'], p['query_id'], 'query_binding'); same(o['owner_id'], target, 'query_binding')
        if len(o['items']) > p['limit']:
            errors.append('query_limit: page exceeds requested limit')
        if ('next_cursor' in o) == o['exhausted']:
            errors.append('query_cursor: cursor presence differs from exhaustion')
        for item in o['items']:
            if item.get('owner_id', item.get('surface_owner_id')) != target:
                errors.append('query_binding: returned item belongs to another owner')
    if name == 'memory.query':
        if len(o['items']) + o['skipped_count'] != o['scanned_count']:
            errors.append('query_progress: traversed positions must equal returned plus skipped')
        if not o['exhausted'] and not o['scanned_count']:
            errors.append('query_progress: non-exhausted page made no progress')
        if o['skipped_count'] and not o['changed']:
            errors.append('query_changes: skipped changed records must be visible as changed')
        if any(item['state'] != 'active' for item in o['items']):
            errors.append('query_active: task query may only return active memories')
    if name == 'memory.list' and p['states'] and any(item['state'] not in p['states'] for item in o['items']):
        errors.append('memory_list_filter: control state outside management filter')
    if name == 'memory.extract':
        for key in ('extraction_id', 'home_id'):
            same(o[key], p[key], 'extraction_binding')
        same(o['owner_id'], target, 'extraction_binding')
        same(o['input_digest'], digest(p['input_refs']), 'extraction_binding')
    if name == 'memory.view.open':
        for key in p:
            same(o[key], p[key], 'view_binding')
        same(o['owner_id'], target, 'view_binding')
    if name == 'memory.view.pull':
        same(o['view_id'], p['view_id'], 'view_binding'); same(o['from_cursor'], p['cursor'], 'view_cursor')
        if len(o['items']) > p['limit']:
            errors.append('view_limit: page exceeds requested limit')
        if any(i['sequence'] > o['through_sequence'] for i in o['items']):
            errors.append('view_sequence: item lies after page watermark')
        if o['phase'] == 'changes' and [i['sequence'] for i in o['items']] != sorted({i['sequence'] for i in o['items']}):
            errors.append('view_sequence: changes must be unique and ordered')
    if name == 'memory.view.ack':
        for left, right in [('view_id','view_id'),('page_id','page_id'),('acked_sequence','through_sequence'),('acked_cursor','applied_cursor')]:
            same(o[left], p[right], 'view_ack')
    if name == 'memory.cleanup.get':
        same(o['object_ref'], p['object_ref'], 'cleanup_binding'); same(o['closure_revision'], p['closure_revision'], 'cleanup_binding')
        if o['physical_state'] == 'complete' and any(not h['use_stopped'] or h['physical_state'] != 'complete' for h in o['holders']):
            errors.append('cleanup_complete: unresolved holder forbids aggregate completion')
    if name in ('interaction.surface_create', 'interaction.surface_update'):
        same(o['snapshot'], p['snapshot'], 'surface_binding')
        errors += snapshot_errors(o['snapshot'])
        if ('task_ref' in o) != ('source_revision' in o['snapshot']):
            errors.append('surface_source: task projections require a source revision; independent surfaces do not')
        if name.endswith('create'):
            for key in ('surface_id', 'app_binding'):
                same(o[key], p[key], 'surface_binding')
            same(o.get('task_ref'), p.get('task_ref'), 'surface_binding')
            same(o['surface_owner_id'], target, 'surface_binding'); same(o['revision'], 1, 'surface_revision')
        else:
            same(o['surface_id'], target, 'surface_binding')
            if o['revision'] < req['expected_revision']:
                errors.append('surface_revision: surface revision regressed')
    if name == 'interaction.surface_read':
        if o['status'] == 'snapshot':
            same(o['surface']['surface_id'], target, 'surface_binding')
            errors += snapshot_errors(o['surface']['snapshot'])
        else:
            same(o['surface_id'], target, 'surface_binding')
            if p.get('known_revision') != o['revision'] or o['gaps']:
                errors.append('surface_not_modified: requires matching known revision and no new gaps')
    if name == 'interaction.present':
        same(o['surface_id'], target, 'presentation_binding')
        for key in ('endpoint_id','open','seen_revision'):
            same(o[key], p[key], 'presentation_binding')
        if o['intent_revision'] <= req['expected_revision']:
            errors.append('presentation_revision: intent revision did not advance')
    if name == 'interaction.request_read':
        request = o['request']; expected = p['request_ref']
        errors += snapshot_errors({'blocks':[{'kind':'input','block_id':'request','request_ref':expected,'input_schema':request['schema']}], 'request_refs':[expected]})
        same(request['owner_id'], expected['owner_id'], 'request_owner')
        same(request['owner_id'], exchange['auth']['logical_service_id'], 'request_owner')
        same(request['request_id'], expected['id'], 'request_identity')
        same(request['revision'], expected['revision'], 'request_revision')
        if (request['state'] == 'consumed') != ('consumed_by' in request):
            errors.append('request_consumption: consumed state and saved consumer must agree')
        if (request['kind'] == 'application') == ('task_ref' in request):
            errors.append('request_kind: task requests require task binding; independent applications do not')
        acceptance = {'goal_revision', 'candidate_ref', 'candidate_hash'}
        if request['kind'] == 'acceptance':
            if not acceptance <= request.keys():
                errors.append('request_acceptance: candidate and goal binding are required')
            elif request['candidate_ref']['hash'] != request['candidate_hash']:
                errors.append('request_acceptance: candidate digest differs from exact content')
        elif acceptance & request.keys():
            errors.append('request_kind: ordinary request cannot carry acceptance binding')
    if name == 'interaction.application_event':
        for key in p:
            same(o[key], p[key], 'application_binding')
        if o['state'] in ('applied','rejected') and 'receipt_ref' not in o:
            errors.append('application_receipt: final consumption state needs original business receipt')
    return errors


def check_trace_rules(trace):
    """Facts absent from the trace remain unproven; no fabricated auth or content lookup."""
    errors=[]; copies={}; contents={}; views={}; pages={}; acked={}; surfaces={}; presentations={}; apps={}; queries={}; uploads={}; extractions={}; commands=set(); memory_versions={}; saved_candidates={}
    control_history={}; copy_history={}; holder_gates={}; holder_copies={}
    for index,event in enumerate(trace['events']):
        if 'exchange' not in event:
            continue
        x=event['exchange']; r=x['request']; p=r['payload']; o=x['response'].get('output'); name=r['method']; target=r['target_id']; a=x['auth']
        if not successful(x['response']):
            continue
        from .schema import METHODS, validate
        spec = METHODS.get(name)
        if not spec or validate(spec['input'], p) or validate(spec['output'], o):
            continue
        key=(a['tenant_id'],a['logical_service_id'],r.get('command_id'))
        if 'command_id' in r and key in commands:
            continue
        if 'command_id' in r:
            commands.add(key)
        def err(code,detail):errors.append(f'event {index}: {code}: {detail}')
        control_get = name == 'content.get' and p.get('mode', 'bytes') == 'control'
        if name == 'content.get' and control_get != (o.get('mode') == 'control'):
            continue  # The exchange-level rule reports the mode mismatch.
        if 'holder_gate' in event and not control_get:
            err('copy_gate_context', 'holder observation requires a successful control query')
        if name=='content.put':
            cref=tuple(p['content_ref'][k] for k in ('owner_id','content_id','version'))
            if cref in contents and contents[cref]['ref']!=p['content_ref']:
                err('content_immutable','immutable content version changed bytes')
            if p['upload_id'] in uploads and uploads[p['upload_id']]!=p['content_ref']:
                err('upload_binding','same upload adopted as another content version')
            contents[cref]={'ref':deepcopy(p['content_ref']),'policy':deepcopy(p['policy']),'state':'active','revision':o['control_revision']}
            control_history.setdefault(cref, []).append((instant(event['at']), deepcopy(o)))
            uploads[p['upload_id']]=deepcopy(p['content_ref'])
        if name in ('content.register_copy','content.get','content.close'):
            cref=tuple(p['content_ref'][k] for k in ('owner_id','content_id','version')); old=contents.get(cref)
            if old and old['state']=='closed' and not control_get:err('content_closed','closed content cannot be delivered or reopened')
            if name=='content.register_copy':
                cid=p['copy_id']; prev=copies.get(cid)
                if prev and any(prev[k]!=p[k] for k in p):err('copy_immutable','copy registration identity rebound')
                if old and instant(p['retention_until'])>instant(old['policy']['retention_until']):err('copy_retention','copy exceeds original content retention')
                copies[cid]=deepcopy(o)
                copy_history.setdefault(cid, []).append((instant(event['at']), deepcopy(o)))
            if control_get:
                observed=instant(x['response']['observed_at'])
                known_copies=[v for at,v in copy_history.get(p['copy_id'], []) if at<=observed]
                registered=known_copies[-1] if known_copies else None
                if not registered or registered['content_ref']!=p['content_ref'] or registered['holder_id']!=holder_identity(a):
                    err('copy_control_holder','control requires this authenticated holder and original registered copy')
                elif any(o['copy'][k]!=registered[k] for k in o['copy']):
                    err('copy_control_record','control projection differs from original copy at observation time')
                known_controls=[v for at,v in control_history.get(cref, []) if at<=observed]
                if known_controls and o['control']!=known_controls[-1]:
                    err('copy_control_current','control differs from known authority at observation time')
                incoming=o['control']; previous=holder_gates.get(p['copy_id'])
                if previous and incoming['control_revision']==previous['control_revision'] and incoming!=previous:
                    err('copy_control_conflict','same control revision changed its fact')
                if not previous or incoming['control_revision']>previous['control_revision']:
                    if previous and previous['state']=='closed' and incoming['state']!='closed':
                        err('copy_gate_reopen','closed holder cannot reopen on a later revision')
                    else:
                        holder_gates[p['copy_id']]=deepcopy(incoming)
                current=holder_gates[p['copy_id']]
                incoming_copy=o['copy']; previous_copy=holder_copies.get(p['copy_id'])
                if previous_copy and incoming_copy['revision']==previous_copy['revision'] and incoming_copy!=previous_copy:
                    err('copy_control_conflict','same copy revision changed its fact')
                if not previous_copy or incoming_copy['revision']>previous_copy['revision']:
                    if previous_copy and previous_copy['use_stopped'] and not incoming_copy['use_stopped']:
                        err('copy_gate_reopen','stopped holder copy cannot reopen on a later revision')
                    else:
                        holder_copies[p['copy_id']]=deepcopy(incoming_copy)
                current_copy=holder_copies[p['copy_id']]
                if 'holder_gate' in event:
                    expected={'copy_id':p['copy_id'],'control_revision':current['control_revision'],'copy_revision':current_copy['revision'],'state':'closed' if current['state']=='closed' or current_copy['use_stopped'] else 'open'}
                    if event['holder_gate']!=expected:
                        err('copy_gate_monotonic','persisted holder gate regressed or missed current control')
            elif name=='content.get':
                copy=copies.get(p['copy_id'])
                if not copy or copy['use_stopped'] or any(copy[k]!=p[k] for k in ('content_ref','purpose','recipient_id')):
                    err('copy_access','download requires matching live copy registration')
                if copy and instant(o['expires_at'])>instant(copy['retention_until']):err('copy_retention','download exceeds copy retention')
            if name=='content.close' and old:
                if r['expected_revision']!=old['revision']:err('content_expected_revision','close ignored current control revision')
                if p['mode']=='restrict':
                    policy=p['policy']; previous=old['policy']
                    if any(not set(policy[k])<=set(previous[k]) for k in ('allowed_locations','allowed_recipients','allowed_purposes')) or instant(policy['retention_until'])>instant(previous['retention_until']) or (policy['offline_allowed'] and not previous['offline_allowed']):
                        err('content_policy_widening','restriction expands a known policy')
                    old['policy']=deepcopy(policy)
                old['state']=o['state'];old['revision']=o['control_revision']
                control_history.setdefault(cref, []).append((instant(event['at']), deepcopy(o)))
        if name=='content.release_copy':
            prev=copies.get(p['copy_id'])
            if not prev:err('copy_access','release requires original registered copy')
            elif any(o[k]!=prev[k] for k in ('content_ref','holder_id','purpose','recipient_id','retention_until')):
                err('copy_immutable','cleanup rebound copy ownership or scope')
            elif prev['use_stopped'] and not o['use_stopped']:
                err('copy_reopen','stopped copy cannot restart use')
            copies[p['copy_id']]=deepcopy(o)
            copy_history.setdefault(p['copy_id'], []).append((instant(event['at']), deepcopy(o)))
        if name=='memory.extract':
            old=extractions.get(p['extraction_id'])
            if old and old!=o:err('extraction_immutable','same extraction changed original task mapping')
            extractions[p['extraction_id']]=deepcopy(o)
        if name in ('memory.create','memory.replace','memory.delete','memory.restrict','memory.read','memory.inspect'):
            mid=o['memory_id'];memory_versions[mid]=deepcopy(o)
            candidate=p.get('extraction_candidate_ref')
            if candidate:
                ckey=(candidate['owner_id'],candidate['id'],candidate['revision'])
                if name!='memory.create':err('candidate_create_only','candidate publication only uses create')
                if ckey in saved_candidates and saved_candidates[ckey]!=mid:err('candidate_once','one candidate created multiple memories')
                saved_candidates[ckey]=mid
        if name=='memory.query':
            for item in o['items']:
                known=memory_versions.get(item['memory_id'])
                if known and (known['revision']!=item['revision'] or known['state']!='active'):
                    err('query_current','query returned a known stale or closed memory')
            qkey=(a['tenant_id'],a['actor_id'],target,p['query_id']);frozen={k:v for k,v in p.items() if k not in ('cursor','limit')};old=queries.get(qkey)
            if old and old['query']!=frozen:err('query_immutable','query id reused with different selection or recipient')
            if old and p.get('cursor')==old['next_cursor'] and o['position']!=old['position']+old['scanned_count']:
                err('query_continuity','next page skipped or repeated frozen positions')
            queries[qkey]={'query':deepcopy(frozen),'next_cursor':o.get('next_cursor'),'position':o['position'],'scanned_count':o['scanned_count']}
        if name=='memory.view.open':
            old=views.get(p['view_id'])
            if old and old!=o:err('view_immutable','view id rebound to another scope or recipient')
            views[p['view_id']]=deepcopy(o)
        if name=='memory.view.pull':
            view=views.get(p['view_id'])
            if not view:err('view_context','page lacks an opened view')
            elif instant(view['expires_at'])<=instant(event['at']):err('view_expiry','expired view returned a new page')
            elif view['projection']=='metadata' and any(i['kind']=='upsert' and 'content_ref' in i['record'] for i in o['items']):err('view_projection','metadata-only view disclosed content record')
            if o['page_id'] in pages and pages[o['page_id']]!=o:err('view_page_immutable','page identity changed bytes or range')
            pages[o['page_id']]=deepcopy(o)
        if name=='memory.view.ack':
            page=pages.get(p['page_id']);old=acked.get(p['view_id'])
            if not page or page['view_id']!=p['view_id'] or page['through_sequence']!=p['through_sequence'] or page['next_cursor']!=p['applied_cursor']:
                err('view_ack_page','ack does not describe a delivered original page')
            elif old and old['page_id']!=p['page_id'] and page['from_cursor']!=old['acked_cursor']:
                err('view_ack_gap','ack skipped an unapplied range')
            acked[p['view_id']]=deepcopy(o)
        if name in ('interaction.surface_create','interaction.surface_update'):
            sid=o['surface_id'];old=surfaces.get(sid)
            if old:
                if any(o.get(k)!=old.get(k) for k in ('surface_owner_id','app_binding','task_ref')):err('surface_immutable_binding','surface changed its registered handler or task')
                if name.endswith('update') and r['expected_revision']!=old['revision']:err('surface_expected_revision','update ignored current surface revision')
                if o['revision']<old['revision'] or (o['revision']==old['revision'] and o!=old):err('surface_revision','same revision changed snapshot or revision regressed')
                source=old['snapshot'].get('source_revision');incoming=o['snapshot'].get('source_revision')
                if source is not None and (incoming<source or (incoming==source and o['snapshot']!=old['snapshot'])):err('surface_source_order','older or conflicting same-revision task projection')
            surfaces[sid]=deepcopy(o)
        if name=='interaction.surface_read' and o['status']=='snapshot':
            known=surfaces.get(target)
            if known and o['surface']['revision']<known['revision']:err('surface_revision','read regressed known revision')
        if name=='interaction.present':
            pk=(target,p['endpoint_id']);old=presentations.get(pk);surface=surfaces.get(target)
            if old and r['expected_revision']!=old['intent_revision']:err('presentation_expected_revision','intent update ignored concurrent user intent')
            if surface and p['seen_revision']>surface['revision']:err('presentation_seen','cannot have seen a future surface revision')
            presentations[pk]=deepcopy(o)
        if name=='interaction.application_event':
            surface=surfaces.get(p['surface_id'])
            if surface and ('task_ref' in surface or surface['app_binding']!=p['app_binding'] or surface['revision']!=p['surface_revision']):err('application_surface','event does not match current independent surface handler/version')
            old=apps.get(p['event_id'])
            if old and any(o[k]!=old[k] for k in ('surface_id','surface_revision','app_binding','event_type','payload_ref','preview_refs','target_service_id','target_command_id')):err('application_immutable','event changed its fixed payload or delivery target')
            apps[p['event_id']]=deepcopy(o)
    return errors
