----------------------------- MODULE Handoff -----------------------------
EXTENDS Naturals

\* Two durable domains, one fixed operation. A owns the initial duty; B
\* durably accepts it and creates a continuation, not an external effect.
\* reqGood/reqBad and acks are bounded bags, not ordered reliable queues.
CONSTANTS ReqCap, AckCap, ABudget, BBudget, InitiallyStable, Mutant
ASSUME /\ ReqCap > 0 /\ AckCap > 0
       /\ ABudget \in Nat /\ BBudget \in Nat
       /\ InitiallyStable \in BOOLEAN
       /\ Mutant \in {"none", "earlyAck", "duplicate"}

VARIABLE s

Init == s = [aPhase |-> "idle", bAccepted |-> FALSE, bWork |-> FALSE,
             bCreates |-> 0, reqGood |-> 0, reqBad |-> 0, acks |-> 0,
             ackSeen |-> FALSE, aUp |-> TRUE, bUp |-> TRUE,
             bKnown |-> FALSE, aRemaining |-> ABudget,
             bRemaining |-> BBudget, gap |-> FALSE,
             stable |-> InitiallyStable]

SaveA == /\ s.aPhase = "idle" /\ s.aUp
         /\ s' = [s EXCEPT !.aPhase = "pending"]

SendA == /\ s.aPhase = "pending" /\ s.aUp /\ s.aRemaining > 0
         /\ s.reqGood + s.reqBad < ReqCap
         /\ s' = [s EXCEPT !.reqGood = @ + 1, !.aRemaining = @ - 1]

DuplicateReq == /\ s.reqGood > 0 /\ s.reqGood + s.reqBad < ReqCap
                /\ s' = [s EXCEPT !.reqGood = @ + 1]

\* A conflicting same-key intent may be probed only after the first
\* acceptance. Authentication, binding before first acceptance, and
\* message corruption/forgery are outside this model.
InjectConflict == /\ s.bAccepted /\ s.reqGood + s.reqBad < ReqCap
                  /\ s' = [s EXCEPT !.reqBad = @ + 1]

\* The only first-accept transaction: receipt, work, and creation counter
\* commit together. The caller may still not know that it committed.
CommitB == /\ s.bUp /\ s.reqGood > 0 /\ ~s.bAccepted
           /\ s' = [s EXCEPT !.reqGood = @ - 1,
                    !.bAccepted = TRUE, !.bWork = TRUE,
                    !.bCreates = @ + 1]

DuplicateB == /\ s.bUp /\ s.reqGood > 0 /\ s.bAccepted
              /\ (Mutant # "duplicate" \/ s.bCreates < 2)
              /\ s' = [s EXCEPT !.reqGood = @ - 1,
                       !.bCreates = IF Mutant = "duplicate" THEN @ + 1 ELSE @]

RejectConflict == /\ s.bUp /\ s.reqBad > 0 /\ s.bAccepted
                  /\ s' = [s EXCEPT !.reqBad = @ - 1]

ReadReceipt == /\ s.bUp /\ s.bAccepted /\ ~s.bKnown
               /\ s' = [s EXCEPT !.bKnown = TRUE]

SendAck == /\ s.bUp /\ s.bKnown /\ s.bRemaining > 0
           /\ s.acks < AckCap
           /\ s' = [s EXCEPT !.acks = @ + 1, !.bRemaining = @ - 1]

DuplicateAck == /\ s.acks > 0 /\ s.acks < AckCap
                /\ s' = [s EXCEPT !.acks = @ + 1]

ReceiveAck == /\ s.aUp /\ s.aPhase = "pending" /\ s.acks > 0
              /\ s' = [s EXCEPT !.acks = @ - 1, !.ackSeen = TRUE]

\* A only uses its local receipt knowledge. In particular this guard
\* does NOT inspect B's durable state; receipt provenance must be proved.
FinishA == /\ s.aUp /\ s.aPhase = "pending" /\ s.ackSeen
           /\ s' = [s EXCEPT !.aPhase = "released",
                    !.ackSeen = FALSE, !.gap = FALSE]

\* Exhaustion is a durable gap, never a claim that B did not accept.
\* Late valid confirmation can still resolve it through FinishA.
DeclareGap == /\ s.aUp /\ s.aPhase = "pending"
              /\ s.aRemaining = 0 /\ ~s.gap
              /\ s' = [s EXCEPT !.gap = TRUE]

DropGood == /\ ~s.stable /\ s.reqGood > 0
            /\ s' = [s EXCEPT !.reqGood = @ - 1]
DropBad == /\ ~s.stable /\ s.reqBad > 0
           /\ s' = [s EXCEPT !.reqBad = @ - 1]
DropAck == /\ ~s.stable /\ s.acks > 0
           /\ s' = [s EXCEPT !.acks = @ - 1]

CrashA == /\ ~s.stable /\ s.aUp
          /\ s' = [s EXCEPT !.aUp = FALSE, !.ackSeen = FALSE]
CrashB == /\ ~s.stable /\ s.bUp
          /\ s' = [s EXCEPT !.bUp = FALSE, !.bKnown = FALSE]
RecoverA == /\ ~s.aUp /\ s' = [s EXCEPT !.aUp = TRUE]
RecoverB == /\ ~s.bUp /\ s' = [s EXCEPT !.bUp = TRUE]

\* An environment event: after it, crashes and loss cease. It never
\* refills retry budgets or fabricates delivery/commit success.
Stabilize == /\ ~s.stable /\ s' = [s EXCEPT !.stable = TRUE]

\* Negative control, not part of the proposed protocol. This permits
\* acknowledgement knowledge before the receiver's durable commit.
EarlyAckBug == /\ Mutant = "earlyAck" /\ s.bUp
               /\ s.reqGood > 0 /\ ~s.bAccepted /\ ~s.bKnown
               /\ s' = [s EXCEPT !.bKnown = TRUE]

Next == \/ SaveA \/ SendA \/ DuplicateReq \/ InjectConflict
        \/ CommitB \/ DuplicateB \/ RejectConflict \/ ReadReceipt
        \/ SendAck \/ DuplicateAck \/ ReceiveAck \/ FinishA \/ DeclareGap
        \/ DropGood \/ DropBad \/ DropAck
        \/ CrashA \/ CrashB \/ RecoverA \/ RecoverB \/ Stabilize
        \/ EarlyAckBug

Spec == Init /\ [][Next]_s

\* SendA uses strong fairness: conflicting probes may repeatedly fill
\* the bounded request bag, offering only intermittent send capacity.
Fairness == /\ WF_s(SaveA) /\ SF_s(SendA)
            /\ WF_s(CommitB) /\ WF_s(DuplicateB) /\ WF_s(RejectConflict)
            /\ WF_s(ReadReceipt) /\ WF_s(SendAck) /\ WF_s(ReceiveAck)
            /\ WF_s(FinishA) /\ WF_s(DeclareGap)
            /\ WF_s(RecoverA) /\ WF_s(RecoverB) /\ WF_s(Stabilize)
LiveSpec == Spec /\ Fairness

TypeOK == s \in [aPhase : {"idle", "pending", "released"},
                  bAccepted : BOOLEAN, bWork : BOOLEAN, bCreates : 0..2,
                  reqGood : 0..ReqCap, reqBad : 0..ReqCap, acks : 0..AckCap,
                  ackSeen : BOOLEAN, aUp : BOOLEAN, bUp : BOOLEAN,
                  bKnown : BOOLEAN, aRemaining : 0..ABudget,
                  bRemaining : 0..BBudget, gap : BOOLEAN, stable : BOOLEAN]
          /\ s.reqGood + s.reqBad <= ReqCap

NoEarlyRelease == s.aPhase = "released" => (s.bAccepted /\ s.bWork)
Responsibility == s.aPhase # "idle" => (s.aPhase = "pending" \/ s.bWork)
NoDuplicate == s.bCreates <= 1
ReceiptAndWork == /\ (s.bAccepted <=> s.bWork)
                  /\ s.bCreates = (IF s.bAccepted THEN 1 ELSE 0)
AckProvenance == (s.bKnown \/ s.ackSeen \/ s.acks > 0) => s.bAccepted
ConflictAfterBinding == s.reqBad > 0 => s.bAccepted
GapRetained == s.gap => s.aPhase = "pending"
Safety == /\ TypeOK /\ NoEarlyRelease /\ Responsibility /\ NoDuplicate
          /\ ReceiptAndWork /\ AckProvenance /\ ConflictAfterBinding
          /\ GapRetained

\* Once a duty exists it cannot be forgotten by resetting to idle;
\* release is terminal for this single handoff. This is checked even
\* without fairness, independently of the current-state responsibility test.
PhaseMonotonic == [][/\ (s.aPhase # "idle" => s'.aPhase # "idle")
                     /\ (s.aPhase = "released" => s'.aPhase = "released")]_s

\* Liveness is checked only with explicit fairness and stability.
\* Positive budgets AT a stable pending state are sufficient, not
\* necessary. A smaller budget may already have left a deliverable ACK.
CompletionWhenResourcesRemain ==
    (s.stable /\ s.aPhase = "pending" /\ s.aRemaining > 0 /\ s.bRemaining > 0)
      ~> (s.aPhase = "released")
SettledOrGap == (s.aPhase = "pending") ~> (s.aPhase = "released" \/ s.gap)
HealthyCompletion == <> (s.aPhase = "released")

\* Expected counterexample checks establish reachability, not bugs.
NotReleased == s.aPhase # "released"
NoUncommittedGap == ~(s.gap /\ ~s.bAccepted)
NoStablePendingBudget == ~(s.stable /\ s.aPhase = "pending"
                           /\ s.aRemaining > 0 /\ s.bRemaining > 0)
=============================================================================
