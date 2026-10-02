#!/usr/bin/env python3
"""Executable finite DESIGN models, not a Harness implementation or liveness proof."""
from itertools import permutations, product
from decimal import Decimal
from copy import deepcopy
import json
counts={};counterexamples=[]

def finish_job(row, claim, now, *, ignore_epoch=False, ignore_work=False):
    out=deepcopy(row)
    if out['state']!='leased' or out['holder']!=claim['holder'] or now>=out['until'] or (not ignore_epoch and out['epoch']!=claim['epoch']):
        return out,False
    out['facts']+=1
    out['state']='ready' if not ignore_work and out['work']>claim['observed'] else 'done'
    return out,True

def job_trace(order, broken=False):
    row={'state':'leased','holder':'boot-a','epoch':7,'until':10,'work':1,'facts':0}
    claim={'holder':'boot-a','epoch':7,'observed':1}
    for event in order:
        if event=='raise':
            row['work']+=1
            if row['state']!='leased':row['state']='ready'
        else:row,_=finish_job(row,claim,9,ignore_work=broken)
    return row
for order in permutations(('raise','finish')):
    assert job_trace(order)['state']=='ready'
    counts['job_interleavings']=counts.get('job_interleavings',0)+1
broken=job_trace(('raise','finish'),True)
assert broken['state']=='done';counterexamples.append({'mutant':'ignore_work_revision','trace':['claim work=1','raise work=2','old finish'],'bad_state':broken})
for epoch,claim_epoch,now in product((7,8),(7,8),(9,10,11)):
    row={'state':'leased','holder':'boot-a','epoch':epoch,'until':10,'work':1,'facts':0}
    claim={'holder':'boot-a','epoch':claim_epoch,'observed':1}
    result,committed=finish_job(row,claim,now)
    if epoch!=claim_epoch or now>=10:assert not committed and result['facts']==0
    else:assert committed and result['facts']==1
    counts['claim_cases']=counts.get('claim_cases',0)+1
row={'state':'leased','holder':'boot-a','epoch':8,'until':20,'work':1,'facts':0}
bad,committed=finish_job(row,{'holder':'boot-a','epoch':7,'observed':1},11,ignore_epoch=True)
assert committed;counterexamples.append({'mutant':'ignore_claim_epoch','trace':['claim epoch=7','replacement epoch=8','old finish'],'bad_state':bad})

def terminate(state,event):
    return ('succeeded' if event=='complete' else 'cancelled') if state=='active' else state
for order in permutations(('complete','cancel')):
    state='active'
    for event in order:state=terminate(state,event)
    assert state==('succeeded' if order[0]=='complete' else 'cancelled')
    counts['terminal_interleavings']=counts.get('terminal_interleavings',0)+1

def try_complete(s,ignore_open_send=False):
    out=deepcopy(s)
    if s['status']=='active' and all(s[k] for k in ('deadline_ok','coverage','checks','effect_known','no_late','index_complete')) and (ignore_open_send or s['send_closed']):out['status']='succeeded'
    return out
for flags in product((False,True),repeat=7):
    s=dict(zip(('deadline_ok','coverage','checks','send_closed','effect_known','no_late','index_complete'),flags));s['status']='active'
    after=try_complete(s)
    if after['status']=='succeeded':assert all(flags)
    counts['completion_states']=counts.get('completion_states',0)+1
s={k:True for k in ('deadline_ok','coverage','checks','send_closed','effect_known','no_late','index_complete')};s.update(status='active',send_closed=False)
bad=try_complete(s,True);assert bad['status']=='succeeded'
counterexamples.append({'mutant':'omit_send_closure','trace':['operation accepted but not started','complete from empty effect view','executor may still start'],'bad_state':bad})

def recover_unknown(state,unsafe=False):
    out=deepcopy(state)
    if out['visible']=='unknown' and out['send_started']:
        if unsafe:out['target_effects']+=1;out['identities'].append('op2');out['last_action']='new_operation'
        else:out['last_action']='query_original'
    return out
for effects in (0,1):
    initial={'send_started':True,'visible':'unknown','target_effects':effects,'identities':['op1']}
    safe=recover_unknown(initial);assert safe['target_effects']==effects and safe['identities']==['op1']
    counts['unknown_histories']=counts.get('unknown_histories',0)+1
bad=recover_unknown({'send_started':True,'visible':'unknown','target_effects':1,'identities':['op1']},True)
assert bad['target_effects']==2;counterexamples.append({'mutant':'retry_unknown_with_new_identity','trace':['op1 applied','reply lost','new op2 retry'],'bad_state':bad})

def merge_bill(state,event,add_cumulative=False):
    out=deepcopy(state);rev,value,final=event
    if rev<=out['revision']:return out
    out['spent']=out['spent']+value if add_cumulative else value
    out['revision']=rev;out['final']=out['final'] or final
    out['reserved']=Decimal('0') if out['final'] else max(Decimal('10')-out['spent'],Decimal('0'))
    return out
updates=[(1,Decimal('6'),False),(2,Decimal('8'),True),(3,Decimal('9'),True)]
initial={'revision':0,'spent':Decimal('0'),'reserved':Decimal('10'),'final':False,'once_consumed':True}
for order in permutations(updates):
    state=deepcopy(initial)
    for event in order:state=merge_bill(state,event)
    assert state['spent']==9 and state['reserved']==0 and state['once_consumed']
    counts['billing_orders']=counts.get('billing_orders',0)+1
bad=deepcopy(initial)
for event in updates:bad=merge_bill(bad,event,True)
assert bad['spent']==23;counterexamples.append({'mutant':'sum_cumulative_bills','trace':['cumulative 6','cumulative 8','cumulative 9'],'bad_state':{k:str(v) for k,v in bad.items()}})
for order in permutations(('cancel','invoke')):
    closed=False;accepted=False
    for event in order:
        if event=='cancel':closed=True
        elif not closed:accepted=True
    assert closed
    if order[0]=='cancel':assert not accepted
    counts['cancel_invoke_orders']=counts.get('cancel_invoke_orders',0)+1
assert 290*4*8==9280 and 290*20==5800 and 100000*2*1==200000
assert Decimal(36000)/(Decimal(1800)*Decimal(2)/Decimal(3)-Decimal(1000))==180
print(json.dumps({'result':'passed','cases':counts,'counterexamples':counterexamples,'capacity_examples':'passed','scope':'finite atomic design models; no implementation or liveness proof'},ensure_ascii=False,indent=2))
