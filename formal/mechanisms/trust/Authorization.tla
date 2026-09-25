--------------------------- MODULE Authorization ---------------------------
EXTENDS Naturals, FiniteSets
CONSTANT Mutant
Rights == {"read", "act", "share"}
RootRights == {"read", "act"}
Contexts == {"owner", "otherUser", "otherActor", "otherProcessor"}
Ops == {1, 2}
VARIABLE s
Init == s = [issued |-> FALSE, rights |-> {}, revoked |-> FALSE,
             sourceRev |-> 1, now |-> 0, cached |-> FALSE, context |-> "owner",
             purpose |-> "act", cacheRev |-> 1, cacheAllow |-> FALSE,
             pending |-> FALSE, onceOp |-> 0, receipts |-> {},
             unsafeUse |-> FALSE, unsafeDisclosure |-> FALSE,
             lateFact |-> FALSE, staleRejected |-> FALSE]
Current == s.issued /\ ~s.revoked /\ s.now < 2
           /\ s.context = "owner" /\ s.purpose \in s.rights
           /\ s.cacheRev = s.sourceRev
Issue(r) == /\ ~s.issued /\ r \subseteq Rights
            /\ (r \subseteq RootRights \/ Mutant = "expand")
            /\ s' = [s EXCEPT !.issued = TRUE, !.rights = r]
Evaluate(c,p) == /\ s.issued /\ c \in Contexts /\ p \in Rights
                 /\ s' = [s EXCEPT !.cached = TRUE, !.pending = TRUE,
                          !.context = c, !.purpose = p, !.cacheRev = s.sourceRev,
                          !.cacheAllow = (~s.revoked /\ s.now < 2 /\ c = "owner" /\ p \in s.rights)]
BeginUse(o) == /\ s.pending /\ o \in Ops
               /\ LET allow == IF Mutant = "stale" THEN s.cacheAllow ELSE Current
                      consume == allow /\ (s.onceOp = 0 \/ s.onceOp = o)
                  IN s' = [s EXCEPT !.pending = FALSE,
                     !.onceOp = IF consume THEN o ELSE @,
                     !.receipts = IF consume THEN @ \cup {o} ELSE @,
                     !.unsafeUse = @ \/ (consume /\ ~Current),
                     !.staleRejected = @ \/ (s.cacheAllow /\ ~Current /\ ~allow)]
Disclose == /\ s.cached
            /\ LET allow == IF Mutant = "discloseCache" THEN s.cacheAllow ELSE Current
               IN s' = [s EXCEPT !.unsafeDisclosure = @ \/ (allow /\ ~Current)]
Revoke == /\ s.issued /\ ~s.revoked /\ s' = [s EXCEPT !.revoked = TRUE]
ChangeSource == /\ s.sourceRev = 1 /\ s' = [s EXCEPT !.sourceRev = 2]
Tick == /\ s.now < 2 /\ s' = [s EXCEPT !.now = @ + 1]
ReceiveHistoricalFact == /\ s.revoked /\ s.receipts # {} /\ ~s.lateFact
                         /\ s' = [s EXCEPT !.lateFact = TRUE]
Next == \/ (\E r \in SUBSET Rights : Issue(r))
        \/ (\E c \in Contexts, p \in Rights : Evaluate(c,p))
        \/ (\E o \in Ops : BeginUse(o))
        \/ Disclose \/ Revoke \/ ChangeSource \/ Tick \/ ReceiveHistoricalFact
Spec == Init /\ [][Next]_s
Attenuation == s.rights \subseteq RootRights
NoUnsafeUse == ~s.unsafeUse
NoUnsafeDisclosure == ~s.unsafeDisclosure
OnceUnique == Cardinality(s.receipts) <= 1
ReceiptBinding == s.receipts \subseteq {s.onceOp}
Safety == Attenuation /\ NoUnsafeUse /\ NoUnsafeDisclosure /\ OnceUnique /\ ReceiptBinding
NoStaleRejection == ~s.staleRejected
NoLateFact == ~s.lateFact
=============================================================================
