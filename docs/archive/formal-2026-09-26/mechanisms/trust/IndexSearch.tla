-------------------------- MODULE IndexSearch --------------------------
EXTENDS Naturals, FiniteSets
CONSTANT Mutant
VARIABLE s
Ids == {1,2}
Versions(k) == [i \in Ids |-> IF i=1 THEN (IF k=0 \/ k=3 THEN 0 ELSE 1)
                                         ELSE (IF k<2 THEN 0 ELSE 2)]
ChangeId(k) == IF k=2 THEN 2 ELSE 1
Apply(values,k) == [values EXCEPT ![ChangeId(k)] = Versions(k)[ChangeId(k)]]
Eligible(values,allowed) == {i \in allowed : values[i] # 0}
Init == s = [seq |-> 0, iw |-> 0, index |-> Versions(0), allowed |-> Ids,
             phase |-> "idle", cut |-> 0, cursor |-> 0, candidates |-> Versions(0),
             result |-> {}, complete |-> FALSE, badRead |-> FALSE,
             falseComplete |-> FALSE, partial |-> FALSE]
Write == /\ s.seq < 3 /\ s' = [s EXCEPT !.seq = @ + 1]
IndexStep == /\ s.iw < s.seq
             /\ s' = [s EXCEPT !.iw = @ + 1, !.index = Apply(@,s.iw+1)]
Revoke(i) == /\ i \in s.allowed /\ s' = [s EXCEPT !.allowed = @ \ {i}]
Begin == /\ s.phase = "idle"
         /\ s' = [s EXCEPT !.phase = "scan", !.cut = s.seq,
                  !.cursor = s.iw, !.candidates = s.index]
TailStep == /\ s.phase = "scan" /\ s.cursor < s.cut
            /\ s' = [s EXCEPT !.cursor = @ + 1,
                     !.candidates = Apply(@,s.cursor+1)]
\* Exit revalidation is independent of earlier candidate collection.
\* This model fixes a literal match-all query, with no ranking/truncation.
Publish == /\ s.phase = "scan"
  /\ LET live == Versions(s.seq)
         candidates == {i \in Ids : s.candidates[i] # 0}
         result == IF Mutant = "cached" THEN candidates ELSE
                   {i \in candidates : i \in s.allowed /\ s.candidates[i] = live[i]}
         complete == (s.seq = s.cut /\ (s.cursor = s.cut \/ Mutant = "omitTail"))
     IN s' = [s EXCEPT !.phase = "done", !.result = result, !.complete = complete,
          !.partial = ~complete,
          !.badRead = \E i \in result : i \notin s.allowed \/ s.candidates[i] # live[i] \/ live[i]=0,
          !.falseComplete = complete /\ result # Eligible(live,s.allowed)]
Next == Write \/ IndexStep \/ Begin \/ TailStep \/ Publish
        \/ \E i \in Ids : Revoke(i)
Spec == Init /\ [][Next]_s
TypeOK == /\ s.seq \in 0..3 /\ s.iw \in 0..s.seq /\ s.cursor \in 0..s.cut
          /\ s.cut \in 0..s.seq /\ s.allowed \subseteq Ids
          /\ s.phase \in {"idle","scan","done"}
IndexAligned == s.index = Versions(s.iw)
NoStaleResult == ~s.badRead
NoFalseComplete == ~s.falseComplete
Safety == TypeOK /\ IndexAligned /\ NoStaleResult /\ NoFalseComplete
NoCompleteNonempty == ~(s.complete /\ s.result # {})
NoPartial == ~s.partial
=======================================================================
