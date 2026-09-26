------------------------- MODULE LogRetention -------------------------
EXTENDS Naturals, FiniteSets
CONSTANT Mutant
VARIABLE s
Consumers == {"index","view","query","rebuild"}
Init == s = [seq |-> 0, floor |-> 0, active |-> {"index"},
             pos |-> [c \in Consumers |-> 0], invalid |-> FALSE,
             rebuildDue |-> FALSE, rebuilt |-> FALSE]
Append == /\ s.seq < 3 /\ s' = [s EXCEPT !.seq = @ + 1]
OpenView == /\ "view" \notin s.active
            /\ s' = [s EXCEPT !.active = @ \cup {"view"}, !.pos["view"] = s.seq]
OpenQuery == /\ "query" \notin s.active /\ "index" \in s.active
             /\ ~s.invalid
             /\ s' = [s EXCEPT !.active = @ \cup {"query"},
                      !.pos["query"] = s.pos["index"]]
Advance(c) == /\ c \in s.active /\ s.pos[c] < s.seq /\ s.pos[c] >= s.floor
              /\ s' = [s EXCEPT !.pos[c] = @ + 1]
Release(c) == /\ c \in {"view","query"} /\ c \in s.active
              /\ s' = [s EXCEPT !.active = @ \ {c}]
Trim(n) == /\ n \in (s.floor+1)..s.seq
           /\ \A c \in s.active : (Mutant = "ignoreIndex" /\ c="index") \/ s.pos[c] >= n
           /\ s' = [s EXCEPT !.floor = n]
\* Capacity pressure closes index admission and retains rebuild responsibility
\* before its old log claim is released. Other active readers remain pinned.
InvalidateIndex == /\ "index" \in s.active /\ ~s.rebuilt
                   /\ s' = [s EXCEPT !.active = @ \ {"index"}, !.invalid = TRUE,
                            !.rebuildDue = Mutant # "dropFirst"]
StartRebuild == /\ s.rebuildDue /\ "rebuild" \notin s.active
                /\ s' = [s EXCEPT !.active = @ \cup {"rebuild"}, !.pos["rebuild"] = s.seq]
Switch == /\ "rebuild" \in s.active /\ s.pos["rebuild"] = s.seq
          /\ s' = [s EXCEPT !.active = (@ \ {"rebuild"}) \cup {"index"},
                   !.pos["index"] = s.seq, !.invalid = FALSE,
                   !.rebuildDue = FALSE, !.rebuilt = TRUE]
Next == Append \/ OpenView \/ OpenQuery \/ InvalidateIndex \/ StartRebuild \/ Switch
        \/ (\E c \in Consumers : Advance(c) \/ Release(c))
        \/ \E n \in 1..3 : Trim(n)
Spec == Init /\ [][Next]_s
TypeOK == /\ s.seq \in 0..3 /\ s.floor \in 0..s.seq
          /\ s.active \subseteq Consumers /\ s.pos \in [Consumers -> 0..3]
ConsumerCoverage == \A c \in s.active : s.floor <= s.pos[c] /\ s.pos[c] <= s.seq
RebuildRetained == s.invalid => s.rebuildDue /\ "index" \notin s.active
Safety == TypeOK /\ ConsumerCoverage /\ RebuildRetained
NoPinnedTail == ~(s.seq > s.floor /\ "query" \in s.active /\ s.pos["query"] < s.seq)
NoRebuilt == ~s.rebuilt
======================================================================
