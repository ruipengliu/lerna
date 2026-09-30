"""Check bounded collection observations; no live database, ACL or subscription is exercised."""
from copy import deepcopy
from .exchanges import instant

LISTS = {
    'execution.list': ('operation_id', 'execution.get', {}),
    'extensions.list': ('activation_id', 'extensions.read', {'kind': 'activation'}),
    'grant.list': ('grant_id', 'grant.read', {}),
}


def check_exchange(exchange, capabilities):
    req=exchange['request']; name=req['method']; out=exchange['response'].get('output')
    if name not in LISTS or out is None:return []
    p=req['payload']; auth=exchange['auth']; field,read,payload=LISTS[name]; errors=[]
    def fail(code,msg):errors.append(code+': '+msg)
    if out['query_id']!=p['query_id'] or out['owner_id']!=req['target_id'] or out['owner_id']!=auth['logical_service_id']:
        fail('collection_binding','query and owner must match the authenticated responsible service')
    if name=='grant.list' and any(item['owner_id']!=out['owner_id'] for item in out['items']):
        fail('collection_binding','every GrantRecord must belong to the enumerated authenticated owner')
    ids=[item[field] for item in out['items']]
    if ids!=sorted(set(ids)) or len(ids)>p['limit']:fail('collection_page','ordered unique IDs and page limit are required')
    if out['exhausted']==('next_cursor' in out):fail('collection_page','only nonterminal pages carry a cursor')
    if out['partial']!=bool(out['gaps']):fail('collection_gap','partial and nonempty gaps must agree')
    duration=(instant(out['expires_at'])-instant(out['snapshot_at'])).total_seconds()
    if not 0<duration<=600:fail('collection_expiry','snapshot lifetime is positive and at most ten minutes')
    # Reuse the existing read contract for every current projection.
    from .exchanges import validate_exchange
    for item in out['items']:
        errors+=validate_exchange({'auth':auth,'request':{'method':read,'target_id':item[field],'payload':payload},'response':{'output':item,'observed_at':exchange['response']['observed_at']}},capabilities)
    return errors


def check_trace_rules(trace):
    snapshots={}; cursors={}; errors=[]; activation_views={}
    for index,event in enumerate(trace['events']):
        x=event.get('exchange',{});req=x.get('request',{});name=req.get('method');out=x.get('response',{}).get('output')
        def fail(code,msg):errors.append(f'event {index}: {code}: {msg}')
        if name in LISTS:
            premise=event.get('collection')
            if premise is None:
                fail('collection_context','enumeration needs explicit authority and scan premises');continue
            a=x['auth'];p=req['payload'];key=(a['tenant_id'],a['actor_id'],a.get('sender_service_id'),req['target_id'],name,p['query_id'])
            if premise.get('state_reclaimed'):snapshots.pop(key,None)
            old=snapshots.get(key); cursor=p.get('cursor'); required=None;start=0
            if cursor:
                saved=cursors.get(cursor)
                if saved is None or saved[0]!=key:required='query_conflict'
                elif instant(event['at'])>=instant(saved[2]):required='cursor_expired'
                elif old and saved[3]!=old['snapshot_at']:required='query_conflict'
                else:start=saved[1]
            if old:
                if instant(event['at'])>=instant(old['expires_at']):required='cursor_expired'
                elif old.get('invalidated') or old['scope']!=premise['authorization_scope']:
                    old['invalidated']=True
                    required='query_conflict'
                elif old['limit']!=p['limit']:required='query_conflict'
            elif cursor and required is None:required='cursor_expired'
            if required:
                if x['response'].get('error',{}).get('code')!=required:fail('collection_context','expired or rebound scope/cursor must return '+required)
                continue
            if out is None:continue
            if old:
                if (out['snapshot_at'],out['expires_at'],premise['member_ids'],premise['truncated'])!=(old['snapshot_at'],old['expires_at'],old['members'],old['truncated']):
                    fail('collection_snapshot','original member set, truncation and lifetime cannot change between pages or retries')
            else:
                if cursor:fail('collection_context','new snapshots cannot start from a foreign cursor')
                if instant(out['snapshot_at'])>instant(event['at']) or instant(event['at'])>=instant(out['expires_at']):fail('collection_expiry','first response must be within the stated lifetime')
                snapshots[key]={'snapshot_at':out['snapshot_at'],'expires_at':out['expires_at'],'members':deepcopy(premise['member_ids']),'truncated':premise['truncated'],'scope':premise['authorization_scope'],'limit':p['limit']}
            members=premise['member_ids'];end=premise['scan_end'];field=LISTS[name][0];ids=[i[field] for i in out['items']]
            if members!=sorted(set(members)):fail('collection_snapshot','membership is stable and ordered')
            if not start<=end<=min(len(members),start+1000) or (not out['exhausted'] and end<=start):fail('collection_progress','bounded nonterminal scans must advance')
            expected=[i for i in members[start:end] if i in premise['readable_ids']]
            if ids!=expected:fail('collection_membership','page must contain exactly the currently readable scanned members')
            if out['exhausted']!=(end==len(members)):fail('collection_progress','exhausted means the fixed member set has ended')
            if premise['truncated'] and (not out['partial'] or 'membership_limit' not in out['gaps']):fail('collection_gap','capacity truncation cannot be presented as a complete collection')
            if end-start>len(expected) and not out['partial']:fail('collection_gap','an unavailable member needs a non-identifying gap')
            if 'next_cursor' in out:
                val=(key,end,out['expires_at'],out['snapshot_at'])
                if out['next_cursor'] in cursors and cursors[out['next_cursor']]!=val:fail('collection_cursor','cursor cannot be rebound or fail to advance')
                cursors[out['next_cursor']]=val
        # Test-only client projection observation checks delayed reads, not wire authority.
        records=[]
        if name=='extensions.list' and out:records=out['items']
        elif name in ('extensions.read','extensions.activate','extensions.disable') and out and 'activation_id' in out and 'revision' in out:records=[out]
        for record in records:
            key=(x['auth']['tenant_id'],x['auth']['logical_service_id'],record['activation_id']);old=activation_views.get(key)
            if old and record['revision']==old['revision'] and record!=old:fail('activation_revision','one revision cannot describe different persistent projections')
            if old is None or record['revision']>old['revision']:activation_views[key]=deepcopy(record)
        view=event.get('activation_view')
        if view:
            key=(x['auth']['tenant_id'],x['auth']['logical_service_id'],view['activation_id']);current=activation_views.get(key)
            if not current or any(view[k]!=current[k] for k in ('revision','phase')):fail('activation_revision','late older reads cannot replace the highest observed activation projection')
    return errors
