--------------------------- MODULE RecoveryView ---------------------------
EXTENDS Naturals, FiniteSets
CONSTANT Mutant
Value(r) == r < 2
VARIABLE s
Init == s = [rev |-> 0, round |-> 0, owner |-> 1, cuts |-> [r \in 1..2 |-> 0],
             r |-> 0, h |-> 0, cursor |-> 0, covered |-> {}, pages |-> {},
             targeted |-> FALSE, ready |-> FALSE, localValue |-> TRUE,
             queued |-> FALSE, pf |-> 0, pt |-> 0, pr |-> 0, po |-> 1,
             badApply |-> FALSE, badRead |-> FALSE, blockedRead |-> FALSE]
ChangeAuthority == /\ s.rev < 2 /\ s' = [s EXCEPT !.rev = @ + 1]
Open == /\ s.round < 2
        /\ s' = [s EXCEPT !.round = @ + 1, !.cuts[s.round + 1] = s.rev,
                 !.r = s.rev, !.h = s.rev, !.cursor = s.rev, !.covered = {},
                 !.pages = {}, !.targeted = FALSE, !.ready = FALSE,
                 !.localValue = Value(s.rev)]
Page(n) == /\ s.round > 0 /\ n \in {1,2} /\ s' = [s EXCEPT !.pages = @ \cup {n}]
FixTarget == /\ s.pages = {1,2} /\ ~s.targeted
             /\ s' = [s EXCEPT !.targeted = TRUE, !.h = s.rev]
QueuePage(r,f,t) == /\ ~s.queued /\ r \in 1..s.round
                    /\ f \in s.cuts[r]..s.rev /\ t \in (f+1)..s.rev
                    /\ s' = [s EXCEPT !.queued = TRUE, !.pf = f, !.pt = t,
                             !.pr = r, !.po = s.owner]
ApplyPage == /\ s.queued /\ s.targeted
             /\ LET binding == s.pr = s.round /\ s.po = s.owner
                    contiguous == s.pf = s.cursor
                    allow == binding /\ (contiguous \/ Mutant = "skip")
                 IN s' = [s EXCEPT !.queued = FALSE,
                          !.cursor = IF allow THEN s.pt ELSE @,
                          !.covered = IF allow THEN @ \cup ((s.pf+1)..s.pt) ELSE @,
                          !.localValue = IF allow THEN Value(s.pt) ELSE @,
                          !.badApply = @ \/ (allow /\ (~binding \/ ~contiguous))]
Publish == /\ s.round > 0
           /\ LET full == s.pages = {1,2} /\ s.targeted /\ s.cursor >= s.h
                  allow == full \/ Mutant = "early"
              IN s' = [s EXCEPT !.ready = IF allow THEN TRUE ELSE @]
NewOwner == /\ s.owner = 1
            /\ s' = [s EXCEPT !.owner = 2, !.ready = FALSE, !.targeted = FALSE, !.pages = {}]
Read == /\ s.ready
        /\ LET current == s.cursor = s.rev
               allow == current \/ Mutant = "stale"
           IN s' = [s EXCEPT !.badRead = @ \/ (allow /\ ~current),
                    !.blockedRead = @ \/ (~allow /\ ~current)]
Next == \/ ChangeAuthority \/ Open \/ (\E n \in {1,2}: Page(n)) \/ FixTarget
        \/ (\E r \in 1..s.round, f \in 0..s.rev, t \in 0..s.rev : QueuePage(r,f,t))
        \/ ApplyPage \/ Publish \/ NewOwner \/ Read
Spec == Init /\ [][Next]_s
ReadyComplete == s.ready => (s.pages = {1,2} /\ s.targeted /\ s.cursor >= s.h
                            /\ ((s.r+1)..s.h) \subseteq s.covered)
ValueBound == s.localValue = Value(s.cursor)
NoBadApply == ~s.badApply
NoStaleRead == ~s.badRead
Safety == ReadyComplete /\ ValueBound /\ NoBadApply /\ NoStaleRead
NoReady == ~s.ready
NoBlockedRead == ~s.blockedRead
=============================================================================
