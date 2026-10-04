#!/usr/bin/env python3
"""Small intake transition model, not an implementation or semantic NLP proof."""
from copy import deepcopy
from itertools import permutations
import json

def initial():return dict(status='active',control=1,goal=1,ready=False,requirements=(),adoption=None,consumed=set(),actions=0)
def adopt(s,key,base,clauses,complete=True,questions=False,compare_semantics=True):
    if key in s['consumed']:return 'original'
    s['consumed'].add(key)
    if s['status']!='active' or base!=(s['goal'],s['control']):return 'stale'
    if not complete or questions:return 'needs_input'
    normalized=tuple(sorted(set(s['requirements']).union(clauses)))
    if compare_semantics and normalized==s['requirements']:return 'unchanged'
    s.update(requirements=normalized,goal=s['goal']+1,control=s['control']+1,ready=False,adoption=key)
    return 'accepted'
def cover(s,goal,clauses,verdict='pass'):
    if s['status']=='active' and goal==s['goal'] and tuple(sorted(clauses))==s['requirements'] and s['requirements'] and verdict=='pass':s['ready']=True

def admit(s,purpose='goal_action',allowed=True,safe_check=True):
    ok=s['status']=='active' and allowed and (s['ready'] or purpose=='requirement_check' and safe_check)
    if ok:s['actions']+=1
    return bool(ok)

checks=0
s=initial();assert not admit(s);checks+=1
assert admit(s,'requirement_check') and not admit(s,'requirement_check',safe_check=False);checks+=1
s=initial();assert adopt(s,'d1',(1,1),['save','readback'],questions=True)=='needs_input' and not s['ready'];checks+=1
assert adopt(s,'d2',(1,1),['save'],complete=False)=='needs_input';checks+=1
assert adopt(s,'d3',(1,1),['save','readback'])=='accepted';checks+=1
assert not admit(s);cover(s,1,['save','readback']);assert not s['ready'];checks+=1
cover(s,2,['save']);assert not s['ready'];cover(s,2,['save','readback']);assert s['ready'];checks+=1
assert not admit(s,allowed=False) and admit(s);checks+=1
before=deepcopy(s);assert adopt(s,'d4',(2,2),['readback','save'])=='unchanged';assert (s['goal'],s['adoption'],s['requirements'])==(before['goal'],before['adoption'],before['requirements']);checks+=1
assert adopt(s,'d3',(2,2),['different'])=='original';assert s['requirements']==before['requirements'];checks+=1
# Every relative ordering of a user revision, original proposal, and cancellation.
traces=0
for order in permutations(('revise','adopt','cancel')):
    s=initial()
    for event in order:
        if event=='revise' and s['status']=='active':s.update(goal=s['goal']+1,control=s['control']+1,ready=False)
        elif event=='cancel':s.update(status='cancelled',control=s['control']+1,ready=False)
        elif event=='adopt':
            old=(s['goal'],s['control'],s['status']);result=adopt(s,'old',(1,1),['save','readback'])
            if old!=(1,1,'active'):assert result=='stale'
        assert not admit(s)
    assert s['status']=='cancelled' and not s['ready'];traces+=1
# Demonstrate that two deliberately weakened gates violate the declared properties.
s=initial();adopt(s,'a',(1,1),['save']);old_goal=s['goal'];adopt(s,'b',(2,2),['save'],compare_semantics=False)
assert s['goal']!=old_goal  # Bug witness: audit identity must not create a goal revision.
s=initial();assert s['status']=='active'  # Bug witness: active alone would permit empty-condition work.
assert not admit(s)
print(json.dumps({'checks':checks,'control_orderings':traces,'bug_witnesses':2,'result':'passed','scope':'bounded design model; no NLP, database, authentication or real tool execution'},ensure_ascii=False,indent=2))
