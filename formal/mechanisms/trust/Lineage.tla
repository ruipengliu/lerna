------------------------------ MODULE Lineage ------------------------------
EXTENDS Naturals, FiniteSets
CONSTANT Mutant
Inputs == {"private", "public", "M1", "N1"}
Closure(x) == CASE x = "M1" -> {"M1", "private"}
                  [] x = "N1" -> {"N1", "public"}
                  [] OTHER -> {x}
Full(xs) == UNION {Closure(x) : x \in xs}
VARIABLE s
Init == s = [prepared |-> FALSE, target |-> "M1", actual |-> {}, claimed |-> {},
             saved |-> FALSE, rejected |-> FALSE, missing |-> FALSE, selfDependent |-> FALSE]
Prepare(xs,claim,target) == /\ ~s.prepared /\ xs \subseteq Inputs /\ xs # {}
                           /\ claim \subseteq Full(xs) /\ target \in {"M1","N1"}
                           /\ s' = [s EXCEPT !.prepared = TRUE, !.target = target,
                                    !.actual = Full(xs), !.claimed = claim]
Save == /\ s.prepared /\ ~s.saved /\ ~s.rejected
        /\ LET complete == s.claimed = s.actual
               independent == s.target \notin s.actual
               accepted == (complete \/ Mutant = "trustClaim") /\ (independent \/ Mutant = "self")
           IN s' = [s EXCEPT !.saved = accepted, !.rejected = ~accepted,
                    !.missing = accepted /\ ~complete,
                    !.selfDependent = accepted /\ ~independent]
Next == (\E xs \in SUBSET Inputs, claim \in SUBSET Inputs, t \in {"M1","N1"} : Prepare(xs,claim,t)) \/ Save
Spec == Init /\ [][Next]_s
CompleteClosure == ~s.missing
NoSelfDependency == ~s.selfDependent
Safety == CompleteClosure /\ NoSelfDependency
NoSaved == ~s.saved
NoRejected == ~s.rejected
=============================================================================
