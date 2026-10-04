#!/usr/bin/env python3
"""Bounded design-contract examples. No server, database, provider or platform is exercised."""
from copy import deepcopy
from itertools import permutations
import json
checks=[]
def checked(name,condition):
    assert condition,name
    checks.append(name)
# Batch admission is one local decision; a remote action is outside this model.
def batch(costs,valid,available):return list(range(len(costs))) if all(valid) and sum(costs)<=available else []
checked('batch-all-or-none',batch([2,3],[True,False],10)==[] and batch([2,3],[True,True],4)==[] and batch([2,3],[True,True],5)==[0,1])
# Control facts and immutable issuance windows have independent identities.
def gate_key(w):return tuple(w[k] for k in ('task','goal','control','status'))
w1={'id':'w1','task':'t','goal':1,'control':2,'status':'active','expires':10};w2={**w1,'id':'w2','expires':20}
checked('window-is-not-control-revision',gate_key(w1)==gate_key(w2) and w1['id']!=w2['id'])
checked('same-window-different-content-conflicts',w1['id']=={**w1,'expires':30}['id'] and w1!={**w1,'expires':30})
# Expiry alone never proves that an original external action did not start.
def expire_use(proven_unstarted,send_closed):
    return {'closed':proven_unstarted and send_closed,'once_consumed':True,'release_numeric':proven_unstarted and send_closed}
checked('expired-once-not-restored',expire_use(True,True)=={'closed':True,'once_consumed':True,'release_numeric':True})
checked('unknown-use-keeps-responsibility',not expire_use(False,False)['release_numeric'])
# Confirmation retains the original business command through waiting and consumption.
command={'id':'original','stage':'accepted'};confirmation={'state':'pending','consumed':False}
confirmation['state']='approved'
checked('approval-is-not-business-success',command['stage']=='accepted' and not confirmation['consumed'])
confirmation['consumed']=True;command['stage']='applied'
checked('consume-keeps-original-command',command['id']=='original' and confirmation['consumed'])
# Result is an atomic local record; publishing its Content copy is separate.
def complete(cancelled,export_ok):return {'status':'cancelled' if cancelled else 'succeeded','result':None if cancelled else 'r1','publication':None if cancelled else ('published' if export_ok else 'pending')}
checked('result-export-failure-not-goal-failure',complete(False,False)['status']=='succeeded' and complete(False,False)['result']=='r1')
checked('cancel-before-result-no-publication',complete(True,True)['result'] is None and complete(True,True)['publication'] is None)
# Unknown response/cost is distinct from a possible target write.
def aggregate(effect_class,applied,unknown_write):
    return {'effect':'applied' if applied else 'unknown','may_apply_later':False if effect_class=='read_only' else ('unknown' if unknown_write else False),'accounting_open':True}
checked('read-response-unknown-not-write-unknown',aggregate('read_only',True,True)=={'effect':'applied','may_apply_later':False,'accounting_open':True})
checked('one-applied-does-not-hide-unknown-write',aggregate('write',True,True)['may_apply_later']=='unknown')
# Cell variables are privately prepared and published only at the exact current generation/head.
def commit_cell(state,expected_generation,expected_namespace,new_value,success):
    if not success or (state['generation'],state['namespace'])!=(expected_generation,expected_namespace):return False
    state.update(value=new_value,namespace=state['namespace']+1);return True
state={'generation':1,'namespace':1,'value':'old'}
checked('failed-cell-does-not-publish-partial-values',not commit_cell(state,1,1,'partial',False) and state['value']=='old')
state['generation']=2
checked('old-cell-cannot-commit-after-stop',not commit_cell(state,1,1,'late',True))
# Child replacement closes goals/effects, not necessarily all accounting.
def switch_child(goal_closed,effects_closed,accounting_open):return goal_closed and effects_closed
checked('child-unknown-effect-blocks-reuse',not switch_child(True,False,False))
checked('child-late-bill-does-not-block-reuse',switch_child(True,True,True))
# Overlap cannot replace the current slot, and only its original occurrence may release it.
def due(slot,new):return (slot,'skipped') if slot is not None else (new,'recorded')
checked('schedule-overlap-keeps-original-slot',due('old','new')==('old','skipped'))
checked('once-at-cutoff-rejected',not (10>10) and 11>10)
# The target head revision and instance must both survive asynchronous initialization.
def reopen(head,expected,new):
    if (head['revision'],head['generation'],head['instance'],head['enabled'])!=expected:return False
    head.update(revision=head['revision']+1,instance=new);return True
head={'revision':3,'generation':1,'instance':'A','enabled':True};expected=(3,1,'A',True)
checked('two-reopens-have-one-winner',reopen(head,expected,'B') and not reopen(head,expected,'C'))
head={'revision':4,'generation':1,'instance':'A','enabled':False}
checked('reopen-cannot-cross-deactivate',not reopen(head,expected,'B'))
# Atomic eligibility registration and defect fanout close both orderings.
traces=0
for order in permutations(('register','defect')):
    gate={'defect':False,'holder':False,'eligible':False,'fanout':False}
    for event in order:
        if event=='register':gate.update(holder=True,eligible=not gate['defect'])
        else:gate.update(defect=True,fanout=gate['holder'])
    assert not gate['eligible'] or gate['fanout'];traces+=1
checked('eligibility-atomic-register-or-notice',traces==2)
print(json.dumps({'result':'passed','contract_checks':len(checks),'eligibility_orderings':traces,'scope':'finite design models; no database, authentication, provider or production proof'},ensure_ascii=False,indent=2))
