---------------------------- MODULE Stream ----------------------------
EXTENDS Naturals, FiniteSets
CONSTANTS N, Mutant
ASSUME N > 0 /\ Mutant \in {"none", "jump", "relay", "erase", "rollback"}
VARIABLE s
Ids == 1..N
Prefix(n) == 1..n
Init == s = [sent |-> 0, source |-> {}, wire |-> {}, received |-> {},
             payload |-> {}, target |-> 0, ackWire |-> {}, ack |-> 0,
             business |-> 0, aGap |-> {}, bGap |-> {}, expired |-> FALSE,
             ackFloor |-> 0, reconcile |-> FALSE, relayReceipt |-> 0,
             acceptedForeignAck |-> FALSE]
Accept(i) == /\ ~s.expired /\ i = s.sent + 1 /\ i \in Ids
             /\ s' = [s EXCEPT !.sent = i, !.source = @ \cup {i}]
Send(i) == /\ ~s.expired /\ ~s.reconcile /\ i \in s.source
           /\ s' = [s EXCEPT !.wire = @ \cup {i}]
Receive(i) == /\ ~s.expired /\ i \in s.wire
              /\ s' = [s EXCEPT !.wire = @ \ {i}, !.received = @ \cup {i},
                       !.payload = @ \cup {i}]
Drop(i) == /\ i \in s.wire /\ s' = [s EXCEPT !.wire = @ \ {i}]
Advance == /\ s.target < N /\ s.target + 1 \in s.received
           /\ s' = [s EXCEPT !.target = @ + 1]
TargetAck == /\ s.target > 0
             /\ s' = [s EXCEPT !.ackWire = @ \cup {s.target}]
ApplyAck(n) == /\ n \in s.ackWire /\ n > s.ack /\ n <= s.sent
               /\ s' = [s EXCEPT !.ack = n, !.ackFloor = n, !.ackWire = @ \ {n},
                        !.aGap = @ \ Prefix(n)]
Handoff(i) == /\ ~s.expired /\ i = s.business + 1 /\ i <= s.target
              /\ i \in s.payload
              /\ s' = [s EXCEPT !.business = i]
CleanSource(i) == /\ i \in s.source /\ (i <= s.ack \/ i \in s.aGap)
                  /\ s' = [s EXCEPT !.source = @ \ {i}]
CleanTarget(i) == /\ i \in s.payload /\ (i <= s.business \/ i \in s.bGap)
                  /\ s' = [s EXCEPT !.payload = @ \ {i}]
Expire == /\ ~s.expired
          /\ s' = [s EXCEPT !.expired = TRUE,
                   !.aGap = Prefix(s.sent) \ Prefix(s.ack),
                   !.bGap = s.received \ Prefix(s.business)]
\* Deliberate mistakes: use highest seen as contiguous prefix; treat relay
\* storage as target proof; delete accepted payload without a durable gap.
Jump == /\ Mutant = "jump" /\ \E i \in s.received :
          /\ i > s.target /\ s' = [s EXCEPT !.target = i]
RelayStored == /\ s.sent > s.relayReceipt
               /\ s' = [s EXCEPT !.relayReceipt = s.sent]
RelayAck == /\ Mutant = "relay" /\ s.relayReceipt > 0 /\ ~s.acceptedForeignAck
            /\ s' = [s EXCEPT !.acceptedForeignAck = TRUE,
                     !.ack = IF s.relayReceipt > @ THEN s.relayReceipt ELSE @,
                     !.ackFloor = IF s.relayReceipt > @ THEN s.relayReceipt ELSE @]
Erase(i) == /\ Mutant = "erase" /\ i \in s.payload
            /\ s' = [s EXCEPT !.payload = @ \ {i}]
\* A stale restored peer reporting zero cannot lower a known durable ACK.
\* It closes automated resending until an external reconciliation resolves it.
ReportRollback == /\ s.ack > 0 /\ ~s.reconcile
                  /\ s' = [s EXCEPT !.reconcile = TRUE,
                           !.ack = IF Mutant = "rollback" THEN 0 ELSE @]
HealthyNext == \/ \E i \in Ids : Accept(i) \/ Send(i) \/ Receive(i)
                                \/ Handoff(i) \/ CleanSource(i) \/ CleanTarget(i)
               \/ Advance \/ TargetAck \/ \E n \in Ids : ApplyAck(n)
Next == \/ HealthyNext \/ Expire \/ Jump \/ RelayStored \/ RelayAck \/ ReportRollback
        \/ \E i \in Ids : Drop(i) \/ Erase(i)
Spec == Init /\ [][Next]_s
HealthySpec == Init /\ [][HealthyNext]_s
    /\ (\A i \in Ids : WF_s(Accept(i)) /\ WF_s(Send(i))
                       /\ WF_s(Receive(i)) /\ WF_s(Handoff(i)))
    /\ WF_s(Advance)
TypeOK == /\ s.sent \in 0..N /\ s.target \in 0..N /\ s.ack \in 0..N
          /\ s.business \in 0..N /\ s.source \subseteq Ids
          /\ s.wire \subseteq Ids /\ s.received \subseteq Ids
          /\ s.payload \subseteq Ids /\ s.ackWire \subseteq Ids
          /\ s.aGap \subseteq Ids /\ s.bGap \subseteq Ids
          /\ s.expired \in BOOLEAN
          /\ s.reconcile \in BOOLEAN /\ s.ackFloor \in 0..N
          /\ s.relayReceipt \in 0..N /\ s.acceptedForeignAck \in BOOLEAN
PrefixSafe == /\ Prefix(s.target) \subseteq s.received
              /\ Prefix(s.ack) \subseteq s.received
              /\ \A n \in s.ackWire : Prefix(n) \subseteq s.received
              /\ s.business <= s.target /\ s.target <= s.sent
DutyRetained == /\ Prefix(s.sent) \subseteq (s.source \cup s.received \cup s.aGap)
                /\ s.received \subseteq (s.payload \cup Prefix(s.business) \cup s.bGap)
AckOrigin == ~s.acceptedForeignAck
Safety == TypeOK /\ PrefixSafe /\ DutyRetained /\ s.ack = s.ackFloor /\ AckOrigin
Monotonic == [][s.ack' >= s.ack /\ s.target' >= s.target
               /\ s.sent' >= s.sent /\ s.business' >= s.business]_s
AllDelivered == <> (s.business = N)
NeverAllDelivered == s.business # N
NeverGap == s.aGap = {} /\ s.bGap = {}
=====================================================================
