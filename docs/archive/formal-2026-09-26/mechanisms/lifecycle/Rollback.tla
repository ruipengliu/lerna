------------------------------ MODULE Rollback ------------------------------
EXTENDS Naturals, TLC
CONSTANTS BugStaleRestore, BugDisableAsRestore, BugRewindFacts
VARIABLES trusted, compatible, unknown, rev, stage, readRev, disabled, active,
          restored, disposition, complete, blockedHistory, originalFacts, badRestore,
          crashed, up, recovered
vars == <<trusted, compatible, unknown, rev, stage, readRev, disabled, active,
          restored, disposition, complete, blockedHistory, originalFacts, badRestore,
          crashed, up, recovered>>
Init == /\ trusted = TRUE /\ compatible = TRUE /\ unknown = FALSE /\ rev = 0
        /\ stage = "idle" /\ readRev = 0 /\ disabled = FALSE /\ active = 2
        /\ restored = FALSE /\ disposition = "restore" /\ complete = FALSE
        /\ blockedHistory = FALSE /\ originalFacts = TRUE /\ badRestore = FALSE
        /\ crashed = FALSE /\ up = TRUE /\ recovered = FALSE
ReadBasis == /\ up /\ stage = "idle" /\ trusted /\ compatible /\ ~unknown
             /\ stage' = "read" /\ readRev' = rev
             /\ UNCHANGED <<trusted, compatible, unknown, rev, disabled, active, restored,
                 disposition, complete, blockedHistory, originalFacts, badRestore, crashed, up, recovered>>
Decide == /\ up /\ stage = "read" /\ stage' = "decided"
          /\ UNCHANGED <<trusted, compatible, unknown, rev, readRev, disabled, active, restored,
              disposition, complete, blockedHistory, originalFacts, badRestore, crashed, up, recovered>>
Invalidate(k) == /\ rev = 0 /\ k \in {"trust", "format", "unknown"}
                 /\ trusted' = IF k = "trust" THEN FALSE ELSE trusted
                 /\ compatible' = IF k = "format" THEN FALSE ELSE compatible
                 /\ unknown' = IF k = "unknown" THEN TRUE ELSE unknown
                 /\ rev' = 1
                 /\ UNCHANGED <<stage, readRev, disabled, active, restored, disposition, complete,
                     blockedHistory, originalFacts, badRestore, crashed, up, recovered>>
Disable == /\ up /\ ~disabled /\ disabled' = TRUE
           /\ restored' = (restored \/ BugDisableAsRestore)
           /\ UNCHANGED <<trusted, compatible, unknown, rev, stage, readRev, active,
               disposition, complete, blockedHistory, originalFacts, badRestore, crashed, up, recovered>>
Restore == /\ up /\ stage = "decided" /\ disabled /\ ~restored
           /\ (BugStaleRestore \/ (readRev = rev /\ trusted /\ compatible /\ ~unknown))
           /\ restored' = TRUE /\ active' = 1 /\ stage' = "applied"
           /\ originalFacts' = ~BugRewindFacts
           /\ badRestore' = (badRestore \/ ~trusted \/ ~compatible \/ unknown \/ readRev # rev)
           /\ recovered' = (recovered \/ crashed)
           /\ UNCHANGED <<trusted, compatible, unknown, rev, readRev, disabled,
               disposition, complete, blockedHistory, crashed, up>>
MarkBlocked == /\ up /\ (~trusted \/ ~compatible \/ unknown) /\ ~blockedHistory
               /\ blockedHistory' = TRUE
               /\ UNCHANGED <<trusted, compatible, unknown, rev, stage, readRev, disabled, active,
                   restored, disposition, complete, originalFacts, badRestore, crashed, up, recovered>>
DecideStopOnly == /\ up /\ disabled /\ blockedHistory /\ disposition = "restore"
                  /\ disposition' = "disable"
                  /\ UNCHANGED <<trusted, compatible, unknown, rev, stage, readRev, disabled, active,
                      restored, complete, blockedHistory, originalFacts, badRestore, crashed, up, recovered>>
Finish == /\ up /\ ~complete
          /\ (IF disposition = "restore" THEN restored ELSE disabled)
          /\ complete' = TRUE
          /\ UNCHANGED <<trusted, compatible, unknown, rev, stage, readRev, disabled, active,
              restored, disposition, blockedHistory, originalFacts, badRestore, crashed, up, recovered>>
Crash == /\ up /\ up' = FALSE /\ crashed' = TRUE
         /\ stage' = IF stage \in {"read", "decided"} THEN "idle" ELSE stage
         /\ UNCHANGED <<trusted, compatible, unknown, rev, readRev, disabled, active, restored,
             disposition, complete, blockedHistory, originalFacts, badRestore, recovered>>
Restart == /\ ~up /\ up' = TRUE
           /\ UNCHANGED <<trusted, compatible, unknown, rev, stage, readRev, disabled, active,
               restored, disposition, complete, blockedHistory, originalFacts, badRestore, crashed, recovered>>
Next == ReadBasis \/ Decide \/ (\E k \in {"trust", "format", "unknown"}: Invalidate(k))
        \/ Disable \/ Restore \/ MarkBlocked \/ DecideStopOnly \/ Finish \/ Crash \/ Restart
Spec == Init /\ [][Next]_vars
CurrentRestoreBasis == ~badRestore
RestoredRequiresActivation == restored => active = 1
NoFactRewind == originalFacts
DispositionHonest == complete => IF disposition = "restore" THEN restored ELSE disabled
NoNormalWitness == ~(complete /\ restored)
NoBlockedWitness == ~(complete /\ disposition = "disable" /\ blockedHistory /\ ~restored)
NoRecoveryWitness == ~recovered
=============================================================================
