-------------------------- MODULE MemoryCommand --------------------------
EXTENDS Naturals, FiniteSets
CONSTANT Mutant
Commands == {1,2}
VARIABLE s
Init == s = [revision |-> 1, tombstone |-> FALSE,
             phase |-> [c \in Commands |-> "idle"],
             expected |-> [c \in Commands |-> 1],
             deleting |-> [c \in Commands |-> FALSE],
             committed |-> {}, closed |-> {}, creates |-> [c \in Commands |-> 0],
             badCAS |-> FALSE, resurrected |-> FALSE, conflict |-> FALSE]
Prepare(c,del) == /\ c \in Commands /\ s.phase[c] = "idle"
                  /\ s' = [s EXCEPT !.phase[c] = "prepared",
                           !.expected[c] = s.revision, !.deleting[c] = del]
Commit(c) == /\ c \in Commands
             /\ (s.phase[c] = "prepared" \/ (Mutant = "late" /\ s.phase[c] = "closed"))
             /\ s.revision < 3
             /\ LET current == s.expected[c] = s.revision
                    possible == ~s.tombstone /\ (current \/ Mutant = "blindCAS")
                 IN s' = [s EXCEPT
                   !.phase[c] = IF possible THEN "committed" ELSE "rejected",
                   !.revision = IF possible THEN @ + 1 ELSE @,
                   !.tombstone = IF possible THEN s.deleting[c] ELSE @,
                   !.committed = IF possible THEN @ \cup {c} ELSE @,
                   !.creates[c] = IF possible THEN @ + 1 ELSE @,
                   !.badCAS = @ \/ (possible /\ ~current),
                   !.resurrected = @ \/ (possible /\ s.tombstone),
                   !.conflict = @ \/ (~possible /\ ~current)]
Close(c) == /\ c \in Commands /\ s.phase[c] \in {"idle","prepared"}
            /\ s' = [s EXCEPT !.phase[c] = "closed", !.closed = @ \cup {c}]
Replay(c) == /\ c \in Commands /\ s.phase[c] # "idle" /\ UNCHANGED s
Next == \/ (\E c \in Commands, del \in BOOLEAN : Prepare(c,del))
        \/ (\E c \in Commands : Commit(c) \/ Close(c) \/ Replay(c))
Spec == Init /\ [][Next]_s
ClosedNeverCommits == s.closed \cap s.committed = {}
CASRespected == ~s.badCAS
NoResurrection == ~s.resurrected
OncePerCommand == \A c \in Commands : s.creates[c] <= 1
OutcomeFixed == \A c \in s.committed : s.phase[c] = "committed"
Safety == ClosedNeverCommits /\ CASRespected /\ NoResurrection /\ OncePerCommand /\ OutcomeFixed
NoConflict == ~s.conflict
NoDelete == ~s.tombstone
=============================================================================
