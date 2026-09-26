----------------------------- MODULE Boundaries -----------------------------
EXTENDS Naturals
CONSTANTS Feature, Mutant
VARIABLE s
Init == s = [catalogVersion |-> 0, resolved |-> FALSE, resolvedVersion |-> 0,
  created |-> FALSE, boundVersion |-> 0, acceptedVersion |-> 0, boundIntent |-> 0,
  creates |-> 0, payload |-> FALSE, tombstone |-> FALSE, entryClosed |-> FALSE,
  gap |-> FALSE, rejectedConflict |-> FALSE,
  child |-> FALSE, parentPaused |-> FALSE, selfPaused |-> FALSE,
  projectedParent |-> FALSE, controlRevision |-> 0, selfErased |-> FALSE,
  world |-> 0, observed |-> 0, hasObservation |-> FALSE, clicked |-> FALSE,
  badObservation |-> FALSE, badControl |-> FALSE,
  parentResumed |-> FALSE, parentResumedWithSelf |-> FALSE, selfWasPaused |-> FALSE, controlledClick |-> FALSE,
  factRevision |-> 0, factValue |-> "unknown", conflict |-> FALSE,
  everConflict |-> FALSE, regressed |-> FALSE, lowSeen |-> FALSE,
  resultSaved |-> FALSE, resultValue |-> "none", originalResult |-> "none"]
Resolve == /\ Feature="catalog" /\ ~s.resolved
  /\ s' = [s EXCEPT !.resolved=TRUE, !.resolvedVersion=s.catalogVersion]
Accept == /\ Feature="catalog" /\ s.resolved /\ ~s.created /\ ~s.entryClosed
  /\ s.resolvedVersion=s.catalogVersion
  /\ s' = [s EXCEPT !.created=TRUE, !.boundVersion=s.resolvedVersion,
    !.acceptedVersion=s.resolvedVersion, !.boundIntent=0, !.creates=@+1,
    !.payload=TRUE, !.tombstone=TRUE]
Upgrade == /\ Feature="catalog" /\ s.catalogVersion=0
  /\ s' = [s EXCEPT !.catalogVersion=1,
    !.boundVersion=IF Mutant="switchVersion" /\ s.created THEN 1 ELSE @]
MismatchedReplay == /\ Feature="catalog" /\ s.created /\ ~s.rejectedConflict
  /\ s' = [s EXCEPT !.rejectedConflict=TRUE,
    !.boundIntent=IF Mutant="rebind" THEN 1 ELSE @]
ClearPayload == /\ Feature="catalog" /\ s.payload
  /\ s' = [s EXCEPT !.payload=FALSE,
    !.tombstone=IF Mutant="forgetTombstone" THEN FALSE ELSE @]
CloseEntry == /\ Feature="catalog" /\ s.created /\ ~s.entryClosed
  /\ s' = [s EXCEPT !.entryClosed=TRUE]
DeleteTombstone == /\ Feature="catalog" /\ ~s.payload /\ s.entryClosed /\ s.tombstone
  /\ s' = [s EXCEPT !.tombstone=FALSE]
RetryOld == /\ Feature="catalog" /\ s.created /\ ~s.payload /\ ~s.gap
  /\ s' = [s EXCEPT !.gap=TRUE,
    !.creates=IF ~s.tombstone /\ ~s.entryClosed THEN @+1 ELSE @]

CreateChild == /\ Feature="gui-control" /\ ~s.child
  /\ s' = [s EXCEPT !.child=TRUE, !.projectedParent=s.parentPaused]
PauseParent == /\ Feature="gui-control" /\ ~s.parentPaused /\ s.controlRevision<4
  /\ s' = [s EXCEPT !.parentPaused=TRUE, !.controlRevision=@+1]
PauseSelf == /\ Feature="gui-control" /\ s.child /\ ~s.selfPaused /\ s.controlRevision<4
  /\ s' = [s EXCEPT !.selfPaused=TRUE, !.selfWasPaused=TRUE, !.controlRevision=@+1]
ResumeParent == /\ Feature="gui-control" /\ s.parentPaused /\ s.controlRevision<4
  /\ s' = [s EXCEPT !.parentPaused=FALSE, !.parentResumed=TRUE,
    !.parentResumedWithSelf=@ \/ s.selfPaused,
    !.controlRevision=@+1,
    !.selfErased=@ \/ (Mutant="clearChildPause" /\ s.selfPaused),
    !.selfPaused=IF Mutant="clearChildPause" THEN FALSE ELSE @]
ResumeSelf == /\ Feature="gui-control" /\ s.selfPaused /\ s.controlRevision<4
  /\ s' = [s EXCEPT !.selfPaused=FALSE, !.controlRevision=@+1]
ProjectParent == /\ Feature="gui-control" /\ s.projectedParent # s.parentPaused
  /\ s' = [s EXCEPT !.projectedParent=s.parentPaused]
Observe == /\ Feature="gui-control" /\ (~s.hasObservation \/ s.observed # s.world)
  /\ s' = [s EXCEPT !.hasObservation=TRUE, !.observed=s.world]
EnvironmentChange == /\ Feature="gui-control" /\ s.world=0
  /\ s' = [s EXCEPT !.world=1]
Click == /\ Feature="gui-control" /\ s.child /\ s.hasObservation /\ ~s.clicked
  /\ ~s.selfPaused
  /\ (IF Mutant="staleProjection" THEN ~s.projectedParent ELSE ~s.parentPaused)
  /\ (s.observed=s.world \/ Mutant="staleObservation")
  /\ s' = [s EXCEPT !.clicked=TRUE, !.badObservation=s.observed # s.world,
    !.badControl=s.parentPaused \/ s.selfPaused,
    !.controlledClick=s.parentResumedWithSelf /\ ~s.selfPaused]

Opposite(a,b) == (a="confirmed" /\ b="absent") \/ (a="absent" /\ b="confirmed")
Receive(r,v) == /\ Feature="facts" /\ r \in 0..2
  /\ v \in {"running","confirmed","absent"}
  /\ LET mismatch == (r=s.factRevision /\ v # s.factValue)
         contradicted == r>=s.factRevision /\ Opposite(s.factValue,v)
         newConflict == mismatch \/ contradicted
         accept == r>s.factRevision /\ ~contradicted
     IN s' = [s EXCEPT
       !.factRevision=IF accept \/ Mutant="lastArrival" THEN r ELSE @,
       !.factValue=IF accept \/ Mutant="lastArrival" THEN v ELSE @,
       !.lowSeen=@ \/ r<s.factRevision,
       !.regressed=@ \/ (Mutant="lastArrival" /\ r<s.factRevision),
       !.everConflict=@ \/ newConflict,
       !.conflict=IF Mutant="eraseConflict" THEN newConflict ELSE @ \/ newConflict,
       !.resultValue=IF Mutant="rewriteResult" /\ s.resultSaved /\ accept THEN v ELSE @]
Publish == /\ Feature="facts" /\ s.factRevision>0 /\ ~s.conflict /\ ~s.resultSaved
  /\ s' = [s EXCEPT !.resultSaved=TRUE, !.resultValue=s.factValue,
                    !.originalResult=s.factValue]
Next == \/ Resolve \/ Accept \/ Upgrade \/ MismatchedReplay \/ ClearPayload
  \/ CloseEntry \/ DeleteTombstone \/ RetryOld \/ CreateChild
  \/ PauseParent \/ PauseSelf \/ ResumeParent \/ ResumeSelf \/ ProjectParent
  \/ Observe \/ EnvironmentChange \/ Click
  \/ (\E r \in 0..2, v \in {"running","confirmed","absent"} : Receive(r,v)) \/ Publish
Spec == Init /\ [][Next]_s
FixedBinding == s.created => s.boundVersion=s.acceptedVersion /\ s.boundIntent=0
NoIdentityResurrection == s.creates<=1
RecoverableIdentity == s.created /\ ~s.entryClosed => s.tombstone
PauseReasonsIndependent == ~s.selfErased
ActionBoundary == ~s.badObservation /\ ~s.badControl
FactRevisionDoesNotRegress == ~s.regressed
ConflictRetained == s.everConflict => s.conflict
FixedHistoricalResult == s.resultSaved => s.resultValue=s.originalResult
Safety == /\ FixedBinding /\ NoIdentityResurrection /\ RecoverableIdentity
  /\ PauseReasonsIndependent /\ ActionBoundary /\ FactRevisionDoesNotRegress
  /\ ConflictRetained /\ FixedHistoricalResult
NoOldBindingAfterUpgrade == ~(s.created /\ s.catalogVersion=1 /\ s.boundVersion=0)
NoControlledClick == ~s.controlledClick
NoFixedResultAndNewFact == ~(s.resultSaved /\ s.factValue # s.resultValue /\ ~s.conflict)
=============================================================================
