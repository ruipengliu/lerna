------------------------------- MODULE Input -------------------------------
EXTENDS Naturals, FiniteSets, TLC
CONSTANTS BugSendBeforeMapping, BugSecondConsumer, BugOldMeaning, SplitMapping
Parents == {1,2}
VARIABLES duty, child, sent, outcome, phase, frozenMeaning, meaning, contentRev,
          consumer, consumes, usedMeaning, badMeaning, up, crashed, recovered, appliedEver
vars == <<duty, child, sent, outcome, phase, frozenMeaning, meaning, contentRev,
          consumer, consumes, usedMeaning, badMeaning, up, crashed, recovered, appliedEver>>
Init == /\ duty = {} /\ child = [p \in Parents |-> 0] /\ sent = {}
        /\ outcome = [p \in Parents |-> "none"] /\ phase = [p \in Parents |-> "none"]
        /\ frozenMeaning = [p \in Parents |-> 0] /\ meaning = 0 /\ contentRev = 0
        /\ consumer = [g \in 0..1 |-> 0] /\ consumes = [g \in 0..1 |-> 0] /\ usedMeaning = 0 /\ badMeaning = FALSE
        /\ up = TRUE /\ crashed = FALSE /\ recovered = FALSE /\ appliedEver = {}
Accept(p) == /\ up /\ p \notin duty
             /\ duty' = duty \cup {p} /\ phase' = [phase EXCEPT ![p] = "accepted"]
             /\ child' = IF SplitMapping THEN child ELSE [child EXCEPT ![p] = p]
             /\ frozenMeaning' = [frozenMeaning EXCEPT ![p] = meaning]
             /\ UNCHANGED <<sent, outcome, meaning, contentRev, consumer, consumes,
                  usedMeaning, badMeaning, up, crashed, recovered, appliedEver>>
SaveMapping(p) == /\ up /\ p \in duty /\ child[p] = 0
                  /\ child' = [child EXCEPT ![p] = p]
                  /\ UNCHANGED <<duty, sent, outcome, phase, frozenMeaning, meaning, contentRev,
                       consumer, consumes, usedMeaning, badMeaning, up, crashed, recovered, appliedEver>>
Forward(p) == /\ up /\ p \in duty /\ p \notin sent
              /\ (BugSendBeforeMapping \/ child[p] = p)
              /\ sent' = sent \cup {p}
              /\ UNCHANGED <<duty, child, outcome, phase, frozenMeaning, meaning, contentRev,
                   consumer, consumes, usedMeaning, badMeaning, up, crashed, recovered, appliedEver>>
Consume(p) == /\ p \in sent /\ outcome[p] = "none"
              /\ (BugSecondConsumer \/ consumer[frozenMeaning[p]] = 0)
              /\ (BugOldMeaning \/ frozenMeaning[p] = meaning)
              /\ consumer' = [consumer EXCEPT ![frozenMeaning[p]] = p]
              /\ consumes' = [consumes EXCEPT ![frozenMeaning[p]] = @ + 1] /\ usedMeaning' = frozenMeaning[p]
              /\ badMeaning' = (badMeaning \/ frozenMeaning[p] # meaning)
              /\ outcome' = [outcome EXCEPT ![p] = "consumed"]
              /\ UNCHANGED <<duty, child, sent, phase, frozenMeaning, meaning, contentRev,
                   up, crashed, recovered, appliedEver>>
Conflict(p) == /\ p \in sent /\ outcome[p] = "none"
               /\ (consumer[frozenMeaning[p]] # 0 \/ frozenMeaning[p] # meaning)
               /\ outcome' = [outcome EXCEPT ![p] = "conflict"]
               /\ UNCHANGED <<duty, child, sent, phase, frozenMeaning, meaning, contentRev,
                    consumer, consumes, usedMeaning, badMeaning, up, crashed, recovered, appliedEver>>
ReadOriginal(p) == /\ up /\ outcome[p] # "none" /\ phase[p] # "applied"
                  /\ phase' = [phase EXCEPT ![p] = "applied"]
                  /\ appliedEver' = appliedEver \cup {p}
                  /\ recovered' = (recovered \/ crashed)
                  /\ UNCHANGED <<duty, child, sent, outcome, frozenMeaning, meaning, contentRev,
                       consumer, consumes, usedMeaning, badMeaning, up, crashed>>
LateAccepted(p) == /\ up /\ p \in duty
                   /\ UNCHANGED vars
Progress == /\ contentRev = 0 /\ contentRev' = 1
            /\ UNCHANGED <<duty, child, sent, outcome, phase, frozenMeaning, meaning,
                 consumer, consumes, usedMeaning, badMeaning, up, crashed, recovered, appliedEver>>
ReplaceMeaning == /\ meaning = 0 /\ meaning' = 1
                  /\ UNCHANGED <<duty, child, sent, outcome, phase, frozenMeaning, contentRev,
                       consumer, consumes, usedMeaning, badMeaning, up, crashed, recovered, appliedEver>>
Crash == /\ up /\ up' = FALSE /\ crashed' = TRUE
         /\ UNCHANGED <<duty, child, sent, outcome, phase, frozenMeaning, meaning, contentRev,
              consumer, consumes, usedMeaning, badMeaning, recovered, appliedEver>>
Restart == /\ ~up /\ up' = TRUE
           /\ UNCHANGED <<duty, child, sent, outcome, phase, frozenMeaning, meaning, contentRev,
                consumer, consumes, usedMeaning, badMeaning, crashed, recovered, appliedEver>>
Next == (\E p \in Parents: Accept(p) \/ SaveMapping(p) \/ Forward(p) \/ Consume(p)
                            \/ Conflict(p) \/ ReadOriginal(p) \/ LateAccepted(p))
        \/ Progress \/ ReplaceMeaning \/ Crash \/ Restart
Spec == Init /\ [][Next]_vars
MappingBeforeForward == \A p \in sent: p \in duty /\ child[p] = p
OneConsumer == \A g \in 0..1: consumes[g] <= 1
MeaningBound == ~badMeaning
NoParentRegression == \A p \in appliedEver: phase[p] = "applied"
NoNormalWitness == ~(consumes[0] + consumes[1] > 0 /\ appliedEver = Parents /\ contentRev = 1)
NoRecoveryWitness == ~(recovered /\ consumes[0] + consumes[1] > 0)
=============================================================================
