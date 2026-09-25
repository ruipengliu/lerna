---------------------------- MODULE CoreContracts ----------------------------
EXTENDS Naturals, FiniteSets
CONSTANTS Feature, Mutant
VARIABLE s
Users == {1,2}
Tasks == {1,2}
Ops == {1,2}
Authorities == {1,2}
Owner(u) == u
Init == s = [events |-> {}, wrongRoute |-> FALSE, rejected |-> FALSE,
  requestVersion |-> 0, consumed |-> FALSE, winningOp |-> 0, consumes |-> 0,
  consumedAfterRefresh |-> FALSE, expired |-> FALSE, uiRevision |-> 0, invalidConsume |-> FALSE,
  proposed |-> FALSE, valid1 |-> FALSE, valid2 |-> FALSE, ready2 |-> FALSE,
  decision |-> "none", operations |-> {}, reservation |-> 0,
  limit |-> 3, used |-> 0, held |-> 1, management |-> "none",
  originalDelta |-> 0, applied |-> 0, budgetRevision |-> 0, duplicateSeen |-> FALSE,
  externalLimit |-> 3, internalParent |-> 2, internalChild |-> 1]
HasKey(u,o,t) == \E e \in s.events : e.user=u /\ e.op=o /\
  (Mutant # "taskScopedKey" \/ e.task=t)
Bind(u,o,t,k,i,a) == /\ Feature="identity" /\ Cardinality(s.events)<2
  /\ (a=Owner(u) \/ Mutant="wrongAuthority") /\ ~HasKey(u,o,t)
  /\ s' = [s EXCEPT !.events = @ \cup {[user |-> u, op |-> o, task |-> t,
    kind |-> k, intent |-> i, authority |-> a]}, !.wrongRoute = @ \/ a # Owner(u)]
ReplayConflict == /\ Feature="identity" /\ s.events # {} /\ ~s.rejected
  /\ s' = [s EXCEPT !.rejected=TRUE]
ReplaceInput == /\ Feature="input" /\ s.requestVersion=0 /\ ~s.consumed
  /\ s' = [s EXCEPT !.requestVersion=1]
RefreshText == /\ Feature="input" /\ s.uiRevision=0
  /\ s' = [s EXCEPT !.uiRevision=1]
ExpireInput == /\ Feature="input" /\ ~s.expired
  /\ s' = [s EXCEPT !.expired=TRUE]
Consume(o,v) == /\ Feature="input" /\ o \in Ops /\ v \in 0..1
  /\ (~s.consumed \/ Mutant="twoWinners") /\ s.consumes<2
  /\ (v=s.requestVersion \/ Mutant="oldInput") /\ ~s.expired
  /\ s' = [s EXCEPT !.consumed=TRUE, !.consumedAfterRefresh=s.uiRevision=1, !.winningOp=o, !.consumes=@+1,
    !.invalidConsume=@ \/ v # s.requestVersion]
ReplayInput == /\ Feature="input" /\ s.consumed /\ ~s.duplicateSeen
  /\ s' = [s EXCEPT !.duplicateSeen=TRUE]
Propose(v1,v2,r) == /\ Feature="batch" /\ ~s.proposed
  /\ s' = [s EXCEPT !.proposed=TRUE, !.valid1=v1, !.valid2=v2, !.ready2=r]
AdmitBatch == /\ Feature="batch" /\ s.proposed /\ s.decision="none"
  /\ s.valid1 /\ ((s.valid2 /\ s.ready2) \/ Mutant="partialBatch")
  /\ s' = [s EXCEPT !.decision="accepted", !.operations={1,2}, !.reservation=2]
RejectBatch == /\ Feature="batch" /\ s.proposed /\ s.decision="none"
  /\ ~(s.valid1 /\ s.valid2 /\ s.ready2)
  /\ s' = [s EXCEPT !.decision="rejected",
    !.operations=IF Mutant="partialReject" /\ s.valid1 THEN {1} ELSE {},
    !.reservation=IF Mutant="partialReject" /\ s.valid1 THEN 1 ELSE 0]
SetLimit(n) == /\ Feature="adjust" /\ s.management="none" /\ n \in 0..4
  /\ (n>=s.used+s.held \/ Mutant="belowReserved")
  /\ s' = [s EXCEPT !.limit=n, !.management="limit", !.applied=1,
    !.budgetRevision=@+1]
Increase == /\ Feature="adjust" /\ s.management="none"
  /\ s' = [s EXCEPT !.limit=@+1, !.originalDelta=1, !.management="increase",
    !.applied=1, !.budgetRevision=@+1]
RepeatIncrease == /\ Feature="adjust" /\ s.management="increase" /\ ~s.duplicateSeen
  /\ s' = [s EXCEPT !.duplicateSeen=TRUE,
    !.applied=IF Mutant="repeatIncrease" THEN @+1 ELSE @,
    !.limit=IF Mutant="repeatIncrease" THEN @+s.originalDelta ELSE @]
AdjustInternal == /\ Feature="adjust" /\ s.internalParent=2
  /\ s' = [s EXCEPT !.internalParent=@-1, !.internalChild=@+1]
AttemptExternalExpansion == /\ Feature="adjust" /\ s.externalLimit=3
  /\ s' = [s EXCEPT !.externalLimit=IF Mutant="expandExternal" THEN 4 ELSE @]
Next == \/ (\E u \in Users,o \in Ops,t \in Tasks,k \in {"input","cancel"},i \in 0..1,a \in Authorities : Bind(u,o,t,k,i,a))
  \/ ReplayConflict \/ ReplaceInput \/ RefreshText \/ ExpireInput
  \/ (\E o \in Ops,v \in 0..1 : Consume(o,v)) \/ ReplayInput
  \/ (\E v1,v2,r \in BOOLEAN : Propose(v1,v2,r)) \/ AdmitBatch \/ RejectBatch
  \/ (\E n \in 0..4 : SetLimit(n)) \/ Increase \/ RepeatIncrease \/ AdjustInternal \/ AttemptExternalExpansion
Spec == Init /\ [][Next]_s
GlobalUserKey == \A e,f \in s.events : e.user=f.user /\ e.op=f.op => e=f
FixedAuthority == ~s.wrongRoute
OneInputConsumer == s.consumes<=1
InputVersion == ~s.invalidConsume
WholeBatch == /\ (s.decision="accepted" => s.valid1 /\ s.valid2 /\ s.ready2 /\ s.operations={1,2} /\ s.reservation=2)
  /\ (s.decision="rejected" => s.operations={} /\ s.reservation=0)
AdjustmentFloor == s.limit>=s.used+s.held
ManagementOnce == s.applied<=1
InternalShare == s.internalParent+s.internalChild=3
ExternalContractFixed == s.externalLimit=3
Safety == /\ GlobalUserKey /\ FixedAuthority /\ OneInputConsumer /\ InputVersion /\ WholeBatch
  /\ AdjustmentFloor /\ ManagementOnce /\ InternalShare /\ ExternalContractFixed
NoTwoUsersSameOp == ~(\E e,f \in s.events : e.user # f.user /\ e.op=f.op)
NoInputAfterTextRefresh == ~(s.consumedAfterRefresh /\ s.duplicateSeen)
NoValidBatch == ~(s.decision="accepted" /\ s.operations={1,2})
NoManagedTransfer == ~(s.duplicateSeen /\ s.applied=1 /\ s.internalChild=2)
=============================================================================
