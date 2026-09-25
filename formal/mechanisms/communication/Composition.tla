-------------------------- MODULE Composition --------------------------
EXTENDS Naturals
CONSTANTS Domains, Mutant
ASSUME Domains # {} /\ Mutant \in {"none", "stale", "noVersion", "retry"}
VARIABLE s
\* rev/enabled are evidence and barriers ALREADY APPLIED at ONE resource
\* admission authority. AuthorityInvalidate and ApplyInvalidation are
\* separate: remote revocation is not atomically visible here. The model
\* assumes local barrier application serializes with local admission; it
\* does not establish a globally fresh distributed snapshot or a delay bound.
Init == s = [rev |-> [d \in Domains |-> 0], enabled |-> Domains,
             authorityEnabled |-> Domains,
             stamp |-> [d \in Domains |-> 0], prepared |-> FALSE,
             issued |-> FALSE, command |-> FALSE, effects |-> 0,
             unknown |-> FALSE, fact |-> FALSE, held |-> FALSE,
             illegal |-> FALSE, completed |-> FALSE, lateFact |-> FALSE,
             issueDuringPropagation |-> FALSE]
Prepare == /\ ~s.prepared /\ s.enabled = Domains
           /\ s' = [s EXCEPT !.prepared = TRUE, !.stamp = s.rev, !.held = TRUE]
AuthorityInvalidate(d) == /\ d \in s.authorityEnabled
                         /\ s' = [s EXCEPT !.authorityEnabled = @ \ {d}]
ApplyInvalidation(d) == /\ d \notin s.authorityEnabled /\ d \in s.enabled
                 /\ s' = [s EXCEPT !.enabled = @ \ {d},
                          !.rev[d] = IF Mutant = "noVersion" THEN @ ELSE @ + 1]
CommitIssue == /\ s.prepared /\ ~s.issued
               /\ (s.stamp = s.rev \/ Mutant = "stale")
               /\ s' = [s EXCEPT !.issued = TRUE, !.command = TRUE,
                        !.illegal = @ \/ s.enabled # Domains,
                        !.issueDuringPropagation = s.authorityEnabled # Domains /\ s.enabled = Domains]
\* The effect may occur after any domain has closed new admission. No
\* cancellation action claims a command already issued has not happened.
Effect == /\ s.command /\ s.effects < 2
          /\ s' = [s EXCEPT !.command = FALSE, !.effects = @ + 1]
Timeout == /\ s.issued /\ ~s.fact /\ ~s.unknown
           /\ s' = [s EXCEPT !.unknown = TRUE]
ReadFact == /\ s.effects > 0 /\ ~s.fact
            /\ s' = [s EXCEPT !.fact = TRUE, !.unknown = FALSE,
                     !.lateFact = s.enabled # Domains]
Settle == /\ s.fact /\ s.held /\ s' = [s EXCEPT !.held = FALSE]
Complete == /\ s.fact /\ s.enabled = Domains /\ ~s.completed
            /\ s' = [s EXCEPT !.completed = TRUE]
RetryUnknown == /\ Mutant = "retry" /\ s.unknown /\ ~s.command
                /\ s.effects < 2
                /\ s' = [s EXCEPT !.command = TRUE]
Next == \/ Prepare \/ CommitIssue \/ Effect \/ Timeout \/ ReadFact
        \/ Settle \/ Complete \/ RetryUnknown
        \/ \E d \in Domains : AuthorityInvalidate(d) \/ ApplyInvalidation(d)
Spec == Init /\ [][Next]_s
TypeOK == /\ s.rev \in [Domains -> 0..1] /\ s.stamp \in [Domains -> 0..1]
          /\ s.enabled \subseteq Domains /\ s.authorityEnabled \subseteq s.enabled
          /\ s.effects \in 0..2
          /\ \A f \in {"prepared","issued","command","unknown","fact","held","illegal","completed","lateFact","issueDuringPropagation"} : s[f] \in BOOLEAN
Safety == /\ TypeOK /\ ~s.illegal /\ s.effects <= 1
          /\ (s.issued /\ ~s.fact => s.held)
          /\ (s.fact => s.effects > 0)
CompletionBoundary == [][(~s.completed /\ s.completed') => s.enabled = Domains]_s
NeverCompleted == ~s.completed
NeverLateFact == ~s.lateFact
NeverPropagationWindow == ~s.issueDuringPropagation
=======================================================================
