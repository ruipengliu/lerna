-------------------------- MODULE ExtractionPolicy ------------------------
EXTENDS Naturals
CONSTANT Mutant
VARIABLE s
Init == s = [task |-> "running", optin |-> FALSE, permission |-> TRUE,
             observed |-> FALSE, cachedEligible |-> FALSE, trigger |-> FALSE,
             creates |-> 0, ownBudget |-> 1, parentBudget |-> 1,
             unsafe |-> FALSE, blocked |-> FALSE]
OptIn == /\ ~s.optin /\ s' = [s EXCEPT !.optin = TRUE]
OptOut == /\ s.optin /\ s' = [s EXCEPT !.optin = FALSE]
Finish(k) == /\ s.task = "running" /\ k \in {"completed","failed","cancelled"}
             /\ s' = [s EXCEPT !.task = k]
Observe == /\ s.task # "running"
           /\ s' = [s EXCEPT !.observed = TRUE,
                    !.cachedEligible = (s.task = "completed" /\ s.optin /\ s.permission)]
Revoke == /\ s.permission /\ s' = [s EXCEPT !.permission = FALSE]
Trigger == /\ s.observed /\ ~s.trigger
           /\ LET current == s.task = "completed" /\ s.optin /\ s.permission /\ s.ownBudget > 0
                  allow == IF Mutant = "cached" THEN s.cachedEligible ELSE current
              IN s' = [s EXCEPT !.trigger = allow, !.creates = IF allow THEN @ + 1 ELSE @,
                       !.ownBudget = IF allow /\ @ > 0 THEN @ - 1 ELSE @,
                       !.unsafe = @ \/ (allow /\ ~current),
                       !.blocked = @ \/ (s.cachedEligible /\ ~current /\ ~allow)]
RepeatedFinal == /\ s.trigger /\ UNCHANGED s
Next == OptIn \/ OptOut \/ (\E k \in {"completed","failed","cancelled"} : Finish(k))
        \/ Observe \/ Revoke \/ Trigger \/ RepeatedFinal
Spec == Init /\ [][Next]_s
NoUnsafeTrigger == ~s.unsafe
OnceTrigger == s.creates <= 1
SeparateBudget == s.parentBudget = 1
Safety == NoUnsafeTrigger /\ OnceTrigger /\ SeparateBudget
NoTrigger == ~s.trigger
NoBlocked == ~s.blocked
=============================================================================
