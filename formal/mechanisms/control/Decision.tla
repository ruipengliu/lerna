------------------------------- MODULE Decision -------------------------------
EXTENDS Naturals, FiniteSets
CONSTANTS MaxRounds, MaxAttempts, MaxRevision, MaxEpoch, InitiallyFixed, Mutant
CallIds == {<<r,a>> : r \in 1..MaxRounds, a \in 1..MaxAttempts}
VARIABLE s
Init == s = [round |-> 0, status |-> "idle", semantic |-> 0, base |-> 0,
  epoch |-> 0, roundEpoch |-> 0, paused |-> FALSE, cancelled |-> FALSE,
  fixed |-> InitiallyFixed, ready |-> FALSE, proposal |-> FALSE,
  valid |-> FALSE, selfPass |-> FALSE, attempt |-> 0, sent |-> {},
  responses |-> [k \in CallIds |-> "none"], badCorrelation |-> FALSE,
  oldReturnWaitingKey |-> <<0,0>>, oldReturnRecovered |-> FALSE,
  outcome |-> "none", ended |-> FALSE, usageKnown |-> FALSE,
  saved |-> FALSE, admissions |-> 0, calls |-> 0, bad |-> FALSE,
  billed |-> FALSE, billDuring |-> FALSE, admittedAfterBill |-> FALSE]
Current == s.epoch = s.roundEpoch /\ s.base = s.semantic /\ ~s.cancelled /\ ~s.paused
Begin == /\ s.status \in {"idle", "closed", "admitted"} /\ s.round < MaxRounds
  /\ ~s.cancelled /\ ~s.paused
  /\ s' = [s EXCEPT !.round = @ + 1, !.status = "open", !.base = s.semantic,
    !.roundEpoch = s.epoch, !.proposal = FALSE, !.valid = FALSE, !.selfPass = FALSE,
    !.attempt = 1, !.outcome = "none", !.ended = FALSE,
    !.usageKnown = FALSE, !.saved = FALSE, !.billDuring = FALSE]
Generate == /\ s.status = "open" /\ Current /\ s.outcome = "none"
  /\ <<s.round,s.attempt>> \notin s.sent
  /\ s' = [s EXCEPT !.sent = @ \cup {<<s.round,s.attempt>>}, !.calls = @ + 1,
                    !.outcome = "unknown"]
\* Each producer response names its persisted (round,attempt) call key.
\* Saving late facts and applying a response to the current round are distinct actions.
Return(k,v,u) == /\ k \in s.sent /\ s.responses[k]="none"
  /\ s' = [s EXCEPT
    !.responses[k]=IF v THEN (IF u THEN "validKnown" ELSE "validUnknown")
                            ELSE (IF u THEN "invalidKnown" ELSE "invalidUnknown"),
    !.oldReturnWaitingKey=IF k[1]<s.round /\ s.status="open" /\ s.outcome="unknown"
                          THEN <<s.round,s.attempt>> ELSE @]
LoadReturn(k) == /\ s.status="open" /\ s.outcome="unknown"
  /\ k \in s.sent /\ s.responses[k] # "none"
  /\ (k = <<s.round,s.attempt>> \/ Mutant="wrongResponseKey")
  /\ LET valid == s.responses[k] \in {"validKnown","validUnknown"}
         known == s.responses[k] \in {"validKnown","invalidKnown"}
     IN s' = [s EXCEPT !.outcome=IF valid THEN "valid" ELSE "invalid",
       !.proposal=valid, !.valid=valid, !.selfPass=TRUE,
       !.ended=TRUE, !.usageKnown=known, !.saved=TRUE,
       !.badCorrelation=@ \/ k # <<s.round,s.attempt>>,
       !.oldReturnRecovered=@ \/ (k = <<s.round,s.attempt>> /\ k=s.oldReturnWaitingKey)]
RuleProposal(v) == /\ s.status = "open" /\ s.outcome = "none" /\ ~s.proposal
  /\ s' = [s EXCEPT !.proposal = TRUE, !.valid = v, !.selfPass = TRUE]
Repair == /\ s.status = "open" /\ Current /\ s.outcome = "invalid"
  /\ s.ended /\ s.saved /\ (s.usageKnown \/ Mutant = "unknownRepair")
  /\ s.attempt < MaxAttempts
  /\ s' = [s EXCEPT !.attempt = @ + 1, !.outcome = "none", !.ended = FALSE,
    !.usageKnown = FALSE, !.saved = FALSE,
    !.bad = @ \/ ~s.usageKnown]
Admit == /\ s.status = "open" /\ s.proposal /\ ~s.cancelled /\ ~s.paused
  /\ s.epoch = s.roundEpoch /\ (s.base = s.semantic \/ Mutant = "stale")
  /\ (s.fixed \/ (Mutant = "selfPass" /\ s.selfPass)) /\ s.valid /\ s.ready
  /\ s' = [s EXCEPT !.status = "admitted", !.admissions = @ + 1,
    !.bad = @ \/ s.base # s.semantic \/ ~s.fixed,
    !.admittedAfterBill = @ \/ s.billDuring]
Close == /\ s.status = "open" /\ s' = [s EXCEPT !.status = "closed"]
SemanticChange == /\ s.semantic < MaxRevision
  /\ s' = [s EXCEPT !.semantic = @ + 1]
FixConditions == /\ ~s.fixed /\ s.semantic < MaxRevision
  /\ s' = [s EXCEPT !.fixed = TRUE, !.semantic = @ + 1]
DependencyFact == /\ ~s.ready /\ s' = [s EXCEPT !.ready = TRUE]
BillOnly == /\ ~s.billed
  /\ s' = [s EXCEPT !.billed = TRUE, !.billDuring = s.status = "open"]
Expire == /\ s.epoch < MaxEpoch /\ s' = [s EXCEPT !.epoch = @ + 1]
Pause == /\ ~s.paused /\ ~s.cancelled /\ s.semantic < MaxRevision
  /\ s' = [s EXCEPT !.paused = TRUE, !.semantic = @ + 1]
Resume == /\ s.paused /\ ~s.cancelled /\ s.semantic < MaxRevision
  /\ s' = [s EXCEPT !.paused = FALSE, !.semantic = @ + 1]
Cancel == /\ ~s.cancelled
  /\ s' = [s EXCEPT !.cancelled = TRUE,
    !.status = IF s.status = "open" THEN "closed" ELSE @]
Next == \/ Begin \/ Generate \/ (\E k \in CallIds, v,u \in BOOLEAN : Return(k,v,u))
  \/ (\E k \in CallIds : LoadReturn(k))
  \/ (\E v \in BOOLEAN : RuleProposal(v)) \/ Repair \/ Admit \/ Close
  \/ SemanticChange \/ FixConditions \/ DependencyFact \/ BillOnly
  \/ Expire \/ Pause \/ Resume \/ Cancel
Spec == Init /\ [][Next]_s
TypeOK == /\ s.round \in 0..MaxRounds /\ s.attempt \in 0..MaxAttempts
  /\ s.semantic \in 0..MaxRevision /\ s.epoch \in 0..MaxEpoch
  /\ s.status \in {"idle","open","closed","admitted"}
  /\ s.outcome \in {"none","unknown","valid","invalid"}
AdmissionAndRepair == ~s.bad
ResponseCorrelation == ~s.badCorrelation
ResponseOrigin == \A k \in CallIds : s.responses[k] # "none" => k \in s.sent
OneSendPerCall == s.calls = Cardinality(s.sent)
FiniteConsumption == /\ s.calls <= MaxRounds * MaxAttempts /\ s.admissions <= s.round
Safety == TypeOK /\ AdmissionAndRepair /\ OneSendPerCall /\ FiniteConsumption
  /\ ResponseCorrelation /\ ResponseOrigin
NoAdmission == s.admissions = 0
NoBillReuse == ~s.admittedAfterBill
NoRepair == s.attempt < 2
NoOldReturnRecovery == ~s.oldReturnRecovered
=============================================================================
