---------------------------- MODULE Evaluation ----------------------------
EXTENDS Naturals, FiniteSets, TLC
CONSTANTS BugStaleQualification, BugVersionBinding, BugTargetGate, Targets
VARIABLES unknownAtCrash, targetBlocked, targetExpired, commandVersion, activatedVersion, badTarget, candidate, telemetry, evidence, report, approved, withdrawn, authRev,
          stage, readRev, readCandidate, prepared, commands, activated, stopped,
          known, approvalHistory, reportHistory, invalidCommit, up, crashed, recovered
vars == <<unknownAtCrash, targetBlocked, targetExpired, commandVersion, activatedVersion, badTarget, candidate, telemetry, evidence, report, approved, withdrawn, authRev,
          stage, readRev, readCandidate, prepared, commands, activated, stopped,
          known, approvalHistory, reportHistory, invalidCommit, up, crashed, recovered>>
Init == /\ unknownAtCrash = {}
        /\ targetBlocked = {} /\ targetExpired = {}
        /\ commandVersion = [t \in Targets |-> 0] /\ activatedVersion = [t \in Targets |-> 0] /\ badTarget = FALSE
        /\ candidate = 1 /\ telemetry = FALSE /\ evidence = FALSE /\ report = 0
        /\ approved = FALSE /\ withdrawn = FALSE /\ authRev = 0
        /\ stage = "idle" /\ readRev = 0 /\ readCandidate = 0
        /\ prepared = {} /\ commands = {} /\ activated = {} /\ stopped = {} /\ known = {}
        /\ approvalHistory = {} /\ reportHistory = {}
        /\ invalidCommit = FALSE /\ up = TRUE /\ crashed = FALSE /\ recovered = FALSE
Telemetry == /\ up /\ ~telemetry /\ telemetry' = TRUE
             /\ UNCHANGED <<unknownAtCrash, targetBlocked, targetExpired, commandVersion, activatedVersion, badTarget, candidate, evidence, report, approved, withdrawn, authRev,
                 stage, readRev, readCandidate, prepared, commands, activated, stopped,
                 known, approvalHistory, reportHistory, invalidCommit, up, crashed, recovered>>
IndependentEvidence == /\ up /\ ~evidence /\ evidence' = TRUE
             /\ UNCHANGED <<unknownAtCrash, targetBlocked, targetExpired, commandVersion, activatedVersion, badTarget, candidate, telemetry, report, approved, withdrawn, authRev,
                 stage, readRev, readCandidate, prepared, commands, activated, stopped,
                 known, approvalHistory, reportHistory, invalidCommit, up, crashed, recovered>>
Report == /\ up /\ evidence /\ report = 0 /\ candidate = 1
          /\ report' = 1 /\ reportHistory' = {1}
          /\ UNCHANGED <<unknownAtCrash, targetBlocked, targetExpired, commandVersion, activatedVersion, badTarget, candidate, telemetry, evidence, approved, withdrawn, authRev,
              stage, readRev, readCandidate, prepared, commands, activated, stopped,
              known, approvalHistory, invalidCommit, up, crashed, recovered>>
Approve == /\ up /\ report = 1 /\ ~approved /\ ~withdrawn
           /\ approved' = TRUE /\ approvalHistory' = {1}
           /\ UNCHANGED <<unknownAtCrash, targetBlocked, targetExpired, commandVersion, activatedVersion, badTarget, candidate, telemetry, evidence, report, withdrawn, authRev,
               stage, readRev, readCandidate, prepared, commands, activated, stopped,
               known, reportHistory, invalidCommit, up, crashed, recovered>>
ChangeCandidate == /\ up /\ candidate = 1 /\ candidate' = 2
                   /\ UNCHANGED <<unknownAtCrash, targetBlocked, targetExpired, commandVersion, activatedVersion, badTarget, telemetry, evidence, report, approved, withdrawn, authRev,
                       stage, readRev, readCandidate, prepared, commands, activated, stopped,
                       known, approvalHistory, reportHistory, invalidCommit, up, crashed, recovered>>
ReadQualification == /\ up /\ stage = "idle" /\ approved /\ ~withdrawn
                     /\ stage' = "read" /\ readRev' = authRev /\ readCandidate' = candidate
                     /\ UNCHANGED <<unknownAtCrash, targetBlocked, targetExpired, commandVersion, activatedVersion, badTarget, candidate, telemetry, evidence, report, approved, withdrawn,
                         authRev, prepared, commands, activated, stopped, known, approvalHistory,
                         reportHistory, invalidCommit, up, crashed, recovered>>
Decide == /\ up /\ stage = "read" /\ stage' = "decided"
          /\ UNCHANGED <<unknownAtCrash, targetBlocked, targetExpired, commandVersion, activatedVersion, badTarget, candidate, telemetry, evidence, report, approved, withdrawn, authRev,
              readRev, readCandidate, prepared, commands, activated, stopped, known,
              approvalHistory, reportHistory, invalidCommit, up, crashed, recovered>>
CommitQualification == /\ up /\ stage = "decided"
                       /\ (BugStaleQualification \/ (~withdrawn /\ readRev = authRev))
                       /\ (BugVersionBinding \/ (readCandidate = report /\ candidate = readCandidate))
                       /\ commandVersion' = [t \in Targets |-> readCandidate]
                       /\ prepared' = Targets /\ stage' = "prepared"
                       /\ invalidCommit' = (invalidCommit \/ withdrawn \/ readRev # authRev
                                              \/ candidate # report \/ readCandidate # report)
                       /\ UNCHANGED <<unknownAtCrash, targetBlocked, targetExpired, activatedVersion, badTarget, candidate, telemetry, evidence, report, approved, withdrawn,
                           authRev, readRev, readCandidate, commands, activated, stopped, known,
                           approvalHistory, reportHistory, up, crashed, recovered>>
Withdraw == /\ up /\ ~withdrawn /\ withdrawn' = TRUE /\ authRev' = 1
            /\ UNCHANGED <<unknownAtCrash, targetBlocked, targetExpired, commandVersion, activatedVersion, badTarget, candidate, telemetry, evidence, report, approved, stage,
                readRev, readCandidate, prepared, commands, activated, stopped, known,
                approvalHistory, reportHistory, invalidCommit, up, crashed, recovered>>
Send(t) == /\ up /\ ~withdrawn /\ t \in prepared /\ t \notin commands
           /\ commands' = commands \cup {t}
           /\ UNCHANGED <<unknownAtCrash, targetBlocked, targetExpired, commandVersion, activatedVersion, badTarget, candidate, telemetry, evidence, report, approved, withdrawn, authRev,
               stage, readRev, readCandidate, prepared, activated, stopped, known,
               approvalHistory, reportHistory, invalidCommit, up, crashed, recovered>>
\* Remote approval view is deliberately not atomically revoked with the local record.
Activate(t) == /\ t \in commands /\ t \notin activated /\ t \notin stopped
               /\ (BugTargetGate \/ (t \notin targetBlocked /\ t \notin targetExpired /\ commandVersion[t] = 1))
               /\ activated' = activated \cup {t}
               /\ activatedVersion' = [activatedVersion EXCEPT ![t] = commandVersion[t]]
               /\ badTarget' = (badTarget \/ t \in targetBlocked \/ t \in targetExpired \/ commandVersion[t] # 1)
               /\ UNCHANGED <<unknownAtCrash, targetBlocked, targetExpired, commandVersion, candidate, telemetry, evidence, report, approved, withdrawn, authRev,
                   stage, readRev, readCandidate, prepared, commands, stopped, known,
                   approvalHistory, reportHistory, invalidCommit, up, crashed, recovered>>
ApplyTargetRevoke(t) == /\ withdrawn /\ t \notin targetBlocked
                       /\ targetBlocked' = targetBlocked \cup {t}
                       /\ UNCHANGED <<unknownAtCrash, targetExpired, commandVersion, activatedVersion, badTarget, candidate, telemetry, evidence, report, approved, withdrawn, authRev,
                           stage, readRev, readCandidate, prepared, commands, activated, stopped, known, approvalHistory, reportHistory, invalidCommit, up, crashed, recovered>>
ExpireTarget(t) == /\ t \notin targetExpired /\ targetExpired' = targetExpired \cup {t}
                  /\ UNCHANGED <<unknownAtCrash, targetBlocked, commandVersion, activatedVersion, badTarget, candidate, telemetry, evidence, report, approved, withdrawn, authRev,
                      stage, readRev, readCandidate, prepared, commands, activated, stopped, known, approvalHistory, reportHistory, invalidCommit, up, crashed, recovered>>

ReadOriginal(t) == /\ up /\ t \in activated /\ t \notin known
                  /\ known' = known \cup {t} /\ recovered' = (recovered \/ t \in unknownAtCrash)
                  /\ UNCHANGED <<unknownAtCrash, targetBlocked, targetExpired, commandVersion, activatedVersion, badTarget, candidate, telemetry, evidence, report, approved, withdrawn,
                      authRev, stage, readRev, readCandidate, prepared, commands, activated, stopped,
                      approvalHistory, reportHistory, invalidCommit, up, crashed>>
Stop(t) == /\ up /\ withdrawn /\ t \in known /\ t \notin stopped
           /\ stopped' = stopped \cup {t}
           /\ UNCHANGED <<unknownAtCrash, targetBlocked, targetExpired, commandVersion, activatedVersion, badTarget, candidate, telemetry, evidence, report, approved, withdrawn, authRev,
               stage, readRev, readCandidate, prepared, commands, activated, known,
               approvalHistory, reportHistory, invalidCommit, up, crashed, recovered>>
Crash == /\ up /\ up' = FALSE /\ crashed' = TRUE
         /\ unknownAtCrash' = unknownAtCrash \cup (activated \ known)
         /\ stage' = IF stage \in {"read", "decided"} THEN "idle" ELSE stage
         /\ UNCHANGED << targetBlocked, targetExpired, commandVersion, activatedVersion, badTarget, candidate, telemetry, evidence, report, approved, withdrawn, authRev,
             readRev, readCandidate, prepared, commands, activated, stopped, known,
             approvalHistory, reportHistory, invalidCommit, recovered>>
Restart == /\ ~up /\ up' = TRUE
           /\ UNCHANGED <<unknownAtCrash, targetBlocked, targetExpired, commandVersion, activatedVersion, badTarget, candidate, telemetry, evidence, report, approved, withdrawn, authRev,
               stage, readRev, readCandidate, prepared, commands, activated, stopped, known,
               approvalHistory, reportHistory, invalidCommit, crashed, recovered>>
Next == Telemetry \/ IndependentEvidence \/ Report \/ Approve \/ ChangeCandidate
        \/ ReadQualification \/ Decide \/ CommitQualification \/ Withdraw
        \/ (\E t \in Targets: Send(t) \/ Activate(t) \/ ReadOriginal(t) \/ Stop(t) \/ ApplyTargetRevoke(t) \/ ExpireTarget(t)) \/ Crash \/ Restart
Spec == Init /\ [][Next]_vars
TargetQualification == ~badTarget /\ (\A t \in activated: activatedVersion[t] = 1)
EvidenceSeparation == report = 1 => evidence
ExactFreshQualification == ~invalidCommit
DurableResponsibility == /\ activated \subseteq commands /\ commands \subseteq prepared
                         /\ known \subseteq activated /\ stopped \subseteq known
HistoryPreserved == /\ (approved => approvalHistory = {1})
                    /\ (report = 1 => reportHistory = {1})
NoNormalWitness == ~(known = Targets /\ approved)
NoRecoveryWitness == ~(recovered /\ withdrawn /\ stopped # {})
NoPartialWitness == ~(known # {} /\ known # Targets /\ withdrawn)
=============================================================================
