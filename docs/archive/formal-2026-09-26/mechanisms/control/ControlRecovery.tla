------------------------- MODULE ControlRecovery -------------------------
EXTENDS Naturals
CONSTANTS MaxEpoch, QueryBudget, Mutant
VARIABLE s
Init == s = [cancelled |-> FALSE, cancelEarly |-> FALSE, claim |-> 0,
  tx |-> "none", txClaim |-> 0, known |-> "none", prepared |-> FALSE,
  missing |-> FALSE, sent |-> FALSE, accepted |-> FALSE, work |-> FALSE,
  blocked |-> FALSE, execEpoch |-> 0, execPrepared |-> FALSE, prepEpoch |-> 0,
  badPhysicalQualification |-> FALSE, lostSenderQueried |-> FALSE,
  senderLost |-> FALSE, driverSent |-> FALSE, sends |-> 0,
  effect |-> FALSE, observed |-> "unknown", closed |-> FALSE,
  lateEffect |-> FALSE, lateObservation |-> FALSE,
  queries |-> 0, retry |-> FALSE, badLease |-> FALSE, lateStart |-> FALSE]
RequestPrepare(w) == /\ s.tx = "none" /\ ~s.cancelled /\ w = s.claim
  /\ s' = [s EXCEPT !.tx = "pending", !.known = "unknown", !.txClaim = w]
CommitPrepare == /\ s.tx = "pending" /\ s.txClaim = s.claim
  /\ (~s.cancelled \/ Mutant = "cancelGate")
  /\ s' = [s EXCEPT !.tx = "committed", !.prepared = TRUE]
AbortPrepare == /\ s.tx = "pending" /\ (s.cancelled \/ s.txClaim # s.claim)
  /\ s' = [s EXCEPT !.tx = "aborted"]
LookupUnseen == /\ s.tx = "pending" /\ s.known = "unknown" /\ ~s.missing
  /\ s' = [s EXCEPT !.missing = TRUE]
ReadCommit == /\ s.tx = "committed" /\ s.known = "unknown"
  /\ s' = [s EXCEPT !.known = "committed"]
ReadReject == /\ s.tx = "aborted" /\ s.known = "unknown"
  /\ s' = [s EXCEPT !.known = "rejected"]
Cancel == /\ ~s.cancelled
  /\ s' = [s EXCEPT !.cancelled = TRUE, !.cancelEarly = ~s.prepared]
ExpireLease == /\ s.claim < MaxEpoch /\ s' = [s EXCEPT !.claim = @ + 1]
SendRequest == /\ s.known = "committed" /\ ~s.cancelled /\ ~s.sent
  /\ s' = [s EXCEPT !.sent = TRUE]
AcceptRemote == /\ s.sent /\ ~s.accepted
  /\ s' = [s EXCEPT !.accepted = TRUE, !.work = TRUE]
ApplyCancelRemote == /\ s.cancelled /\ ~s.blocked
  /\ s' = [s EXCEPT !.blocked = TRUE]
ExpireExecLease == /\ s.execEpoch < MaxEpoch
  /\ s' = [s EXCEPT !.execEpoch = @ + 1]
PrepareExec(w) == /\ s.accepted /\ ~s.blocked /\ ~s.execPrepared
  /\ (w = s.execEpoch \/ Mutant = "leaseGate")
  /\ s' = [s EXCEPT !.execPrepared = TRUE, !.prepEpoch = w,
                    !.badLease = w # s.execEpoch]
PhysicalSend == /\ s.execPrepared /\ ~s.driverSent
  /\ (~s.senderLost \/ Mutant="sendAfterLoss")
  /\ (s.prepEpoch = s.execEpoch \/ Mutant="sendStaleEpoch")
  /\ (~s.blocked \/ Mutant = "remoteGate")
  /\ s' = [s EXCEPT !.driverSent = TRUE, !.sends = @ + 1,
                    !.lateStart = s.blocked,
                    !.badPhysicalQualification=@ \/ s.senderLost \/ s.prepEpoch # s.execEpoch]
CrashSender == /\ s.execPrepared /\ ~s.senderLost
  /\ s' = [s EXCEPT !.senderLost = TRUE]
ExternalEffect == /\ s.driverSent /\ ~s.closed /\ ~s.effect
  /\ s' = [s EXCEPT !.effect = TRUE, !.lateEffect=s.cancelled /\ s.blocked]
QueryNotFound == /\ s.execPrepared /\ s.queries < QueryBudget
  /\ s' = [s EXCEPT !.queries = @ + 1,
                    !.lostSenderQueried=@ \/ (s.senderLost /\ ~s.driverSent),
                    !.observed = IF Mutant = "notFoundAbsent" THEN "absent" ELSE @]
ObservePositive == /\ s.effect /\ s.observed # "confirmed"
  /\ s' = [s EXCEPT !.observed = "confirmed", !.lateObservation=s.lateEffect /\ s.cancelled]
CloseOriginal == /\ s.execPrepared /\ ~s.closed
  /\ s' = [s EXCEPT !.closed = TRUE]
ObserveNegative == /\ s.closed /\ ~s.effect /\ s.observed = "unknown"
  /\ s' = [s EXCEPT !.observed = "absent"]
NewAttempt == /\ s.closed /\ s.observed = "absent" /\ ~s.cancelled /\ ~s.retry
  /\ s' = [s EXCEPT !.retry = TRUE]
Next == \/ (\E w \in 0..MaxEpoch : RequestPrepare(w) \/ PrepareExec(w))
  \/ CommitPrepare \/ AbortPrepare \/ LookupUnseen \/ ReadCommit \/ ReadReject
  \/ Cancel \/ ExpireLease \/ SendRequest \/ AcceptRemote \/ ApplyCancelRemote
  \/ ExpireExecLease \/ PhysicalSend \/ CrashSender \/ ExternalEffect
  \/ QueryNotFound \/ ObservePositive \/ CloseOriginal \/ ObserveNegative \/ NewAttempt
Spec == Init /\ [][Next]_s
TypeOK == /\ s.claim \in 0..MaxEpoch /\ s.execEpoch \in 0..MaxEpoch
  /\ s.tx \in {"none", "pending", "committed", "aborted"}
  /\ s.known \in {"none", "unknown", "committed", "rejected"}
  /\ s.observed \in {"unknown", "absent", "confirmed"}
  /\ s.queries \in 0..QueryBudget /\ s.sends \in 0..1
CancelWins == s.cancelEarly => ~s.prepared
CommitKnowledge == /\ (s.prepared => s.tx = "committed")
  /\ (s.known = "committed" => s.prepared)
  /\ (s.known = "rejected" => s.tx = "aborted" /\ ~s.prepared)
NoUnknownDispatch == s.sent => s.prepared /\ s.known = "committed"
DurableExecution == /\ (s.accepted <=> s.work) /\ (s.accepted => s.sent)
  /\ (s.driverSent => s.execPrepared /\ s.work) /\ (s.effect => s.driverSent)
EvidenceNotGuess == s.observed = "absent" => s.closed /\ ~s.effect
SafeNewAttempt == s.retry => s.closed /\ s.observed = "absent" /\ ~s.effect
CurrentLease == ~s.badLease
PhysicalSendQualified == ~s.badPhysicalQualification
LocalControlGate == ~s.lateStart
Safety == /\ TypeOK /\ CancelWins /\ CommitKnowledge /\ NoUnknownDispatch
  /\ DurableExecution /\ EvidenceNotGuess /\ SafeNewAttempt
  /\ CurrentLease /\ LocalControlGate /\ PhysicalSendQualified
NoLateCancelledEffect == ~s.lateObservation
NoUnseenThenCommit == ~(s.missing /\ s.prepared /\ s.known = "committed")
NoSafeRetry == ~s.retry
NoLostSenderQuery == ~s.lostSenderQueried
=============================================================================
