-------------------------------- MODULE Budget --------------------------------
EXTENDS Naturals, FiniteSets
CONSTANTS Total, Cleanup, MaxDepth, MaxRevision, Mutant
Nodes == 0..2
RECURSIVE Sum(_)
Sum(f) == IF DOMAIN f = {} THEN 0
          ELSE LET i == CHOOSE j \in DOMAIN f : TRUE IN
            f[i] + Sum([j \in DOMAIN f \ {i} |-> f[j]])
VARIABLE s
Init == s = [active |-> {0}, parent |-> [n \in Nodes |-> 0],
  depth |-> [n \in Nodes |-> 0], free |-> [n \in Nodes |-> IF n=0 THEN Total ELSE 0],
  held |-> [n \in Nodes |-> 0], spent |-> [n \in Nodes |-> 0],
  allocation |-> [n \in Nodes |-> 0], used |-> [n \in Nodes |-> 0],
  revision |-> [n \in Nodes |-> 0], calls |-> {}, final |-> {}, sealed |-> {},
  done |-> {}, conflicts |-> {}, cancelled |-> FALSE,
  cleanupFree |-> Cleanup, cleanupSpent |-> 0, charged |-> 0,
  lateBill |-> FALSE, lateBillDuplicate |-> FALSE,
  duplicateSeen |-> FALSE, staleSeen |-> FALSE, nested |-> FALSE]
Delegate(p,n,a) == /\ p \in s.active /\ n \in Nodes \ s.active /\ p < n
  /\ a \in 1..s.free[p] /\ ~s.cancelled
  /\ (s.depth[p] < MaxDepth \/ Mutant = "depth")
  /\ s' = [s EXCEPT !.active = @ \cup {n}, !.parent[n] = p,
    !.depth[n] = s.depth[p]+1, !.free[p] = @-a, !.free[n] = a,
    !.nested = @ \/ p # 0]
Reserve(n,a) == /\ n \in s.active /\ n \notin s.calls /\ ~s.cancelled
  /\ a \in 1..s.free[n]
  /\ s' = [s EXCEPT !.calls = @ \cup {n}, !.free[n] = @-a,
    !.held[n] = a, !.allocation[n] = a]
Report(n,r,u) == /\ n \in s.calls /\ n \notin s.conflicts /\ n \notin s.final
  /\ r \in (s.revision[n]+1)..MaxRevision /\ u \in s.used[n]..s.allocation[n]
  /\ s' = [s EXCEPT !.revision[n] = r, !.used[n] = u,
    !.spent[n] = @ + (u-s.used[n]), !.held[n] = s.allocation[n]-u,
    !.charged = @ + (u-s.used[n]),
    !.lateBill = @ \/ (s.cancelled /\ u>s.used[n])]
Duplicate(n) == /\ n \in s.calls /\ s.revision[n]>0 /\ ~s.duplicateSeen
  /\ s' = [s EXCEPT !.duplicateSeen = TRUE, !.lateBillDuplicate=s.lateBill,
    !.spent[n] = IF Mutant="doubleBill" THEN @+s.used[n] ELSE @,
    !.charged = IF Mutant="doubleBill" THEN @+s.used[n] ELSE @]
Stale(n) == /\ n \in s.calls /\ s.revision[n]>0 /\ ~s.staleSeen
  /\ s' = [s EXCEPT !.staleSeen = TRUE]
Conflict(n) == /\ n \in s.calls /\ s.revision[n]>0 /\ n \notin s.conflicts
  /\ s' = [s EXCEPT !.conflicts = @ \cup {n}]
Done(n) == /\ n \in s.calls /\ n \notin s.done
  /\ s' = [s EXCEPT !.done = @ \cup {n}]
Seal(n) == /\ n \in s.calls /\ n \in s.done /\ n \notin s.sealed
  /\ s' = [s EXCEPT !.sealed = @ \cup {n}]
Finalize(n) == /\ n \in s.calls /\ n \in s.sealed /\ n \notin s.final
  /\ n \notin s.conflicts
  /\ s' = [s EXCEPT !.final = @ \cup {n}, !.free[n] = @+s.held[n], !.held[n] = 0]
PrematureRelease == /\ Mutant="earlyRelease"
  /\ \E n \in s.calls \ s.final :
      /\ n \in s.done /\ s.held[n]>0
      /\ s' = [s EXCEPT !.free[n] = @+s.held[n], !.held[n] = 0]
DoubleParent == /\ Mutant="doubleParent" /\ s.nested /\ s.charged=Sum(s.spent)
  /\ Sum(s.spent)>0 /\ s' = [s EXCEPT !.charged = @+Sum(s.spent)]
Cancel == /\ ~s.cancelled /\ s' = [s EXCEPT !.cancelled=TRUE]
CleanupUse == /\ s.cleanupFree>0
  /\ s' = [s EXCEPT !.cleanupFree=@-1, !.cleanupSpent=@+1]
Next == \/ (\E p,n \in Nodes, a \in 1..Total : Delegate(p,n,a))
  \/ (\E n \in Nodes, a \in 1..Total : Reserve(n,a))
  \/ (\E n \in Nodes, r \in 1..MaxRevision, u \in 0..Total : Report(n,r,u))
  \/ (\E n \in Nodes : Duplicate(n) \/ Stale(n) \/ Conflict(n) \/ Done(n) \/ Seal(n) \/ Finalize(n))
  \/ PrematureRelease \/ DoubleParent \/ Cancel \/ CleanupUse
Spec == Init /\ [][Next]_s
Conservation == Sum(s.free)+Sum(s.held)+Sum(s.spent)=Total
SingleChargePath == s.charged=Sum(s.spent)
CumulativeSettlement == \A n \in Nodes : s.spent[n]=s.used[n]
UnknownHeld == \A n \in s.calls \ s.final : s.held[n]+s.used[n]=s.allocation[n]
SealedRelease == s.final \subseteq s.sealed
Hierarchy == /\ (\A n \in s.active : s.depth[n]<=MaxDepth)
  /\ (\A n \in s.active \ {0} : s.parent[n] \in s.active /\ s.parent[n]<n
       /\ s.depth[n]=s.depth[s.parent[n]]+1)
SeparateCleanup == s.cleanupFree+s.cleanupSpent=Cleanup
TypeOK == /\ s.free \in [Nodes -> 0..Total] /\ s.held \in [Nodes -> 0..Total]
  /\ s.spent \in [Nodes -> 0..Total] /\ s.used \in [Nodes -> 0..Total]
  /\ s.cleanupFree \in 0..Cleanup /\ s.cleanupSpent \in 0..Cleanup
Safety == /\ TypeOK /\ Conservation /\ SingleChargePath /\ CumulativeSettlement
  /\ UnknownHeld /\ SealedRelease /\ Hierarchy /\ SeparateCleanup
NoNested == ~s.nested
NoCancelledLateBill == ~s.lateBillDuplicate
NoFinalRelease == s.final={}
=============================================================================
