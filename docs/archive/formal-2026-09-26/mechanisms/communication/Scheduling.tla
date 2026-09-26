------------------------- MODULE Scheduling -------------------------
EXTENDS Naturals, FiniteSets
CONSTANT Mutant
VARIABLE s
Jobs == 1..6
Users == {1,2}
Kinds == {"ordinary", "cleanup", "control"}
User(j) == IF j <= 4 THEN 1 ELSE 2
Kind(j) == IF j \in {1,2,5} THEN "ordinary" ELSE
           IF j \in {3,6} THEN "cleanup" ELSE "control"
Outstanding(u,k) == {j \in s.accepted \ s.done : User(j)=u /\ Kind(j)=k}
Init == s = [accepted |-> {}, queued |-> {}, done |-> {},
             connection |-> [j \in Jobs |-> 1]]
Admit(j,c) == /\ j \notin s.accepted
              /\ IF Mutant = "perConnection"
                    THEN Cardinality({q \in Outstanding(User(j),Kind(j)) : s.connection[q]=c}) < 1
                    ELSE Cardinality(Outstanding(User(j),Kind(j))) < 1
              /\ s' = [s EXCEPT !.accepted = @ \cup {j}, !.queued = @ \cup {j},
                       !.connection[j] = c]
Serve(j) == /\ j \in s.queued
            /\ s' = [s EXCEPT !.queued = @ \ {j}, !.done = @ \cup {j}]
Evict(j) == /\ Mutant = "evict" /\ j \in s.queued
            /\ s' = [s EXCEPT !.queued = @ \ {j}]
Next == \/ \E j \in Jobs, c \in {1,2} : Admit(j,c)
        \/ \E j \in Jobs : Serve(j) \/ Evict(j)
Spec == Init /\ [][Next]_s
FairSpec == Spec /\ \A j \in Jobs : WF_s(Serve(j))
Safety == /\ s.accepted \subseteq Jobs /\ s.queued \subseteq s.accepted
          /\ s.done \subseteq s.accepted /\ s.queued \cap s.done = {}
          /\ s.accepted = s.queued \cup s.done
          /\ \A u \in Users, k \in Kinds : Cardinality(Outstanding(u,k)) <= 1
EveryAcceptedFinishes == \A j \in Jobs : (j \in s.accepted) ~> (j \in s.done)
\* A full ordinary allocation does not consume cleanup capacity; another
\* user's progress can coexist with both. This is a reachable state check.
NeverReservedCoexistence == ~(1 \in s.queued /\ 3 \in s.queued /\ 5 \in s.done)
====================================================================
