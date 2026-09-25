----------------------------- MODULE References -----------------------------
EXTENDS Naturals, FiniteSets, TLC
CONSTANTS Refs, BugStaleRegister, BugEarlyRelease
VARIABLES doneEvidence, phase, readEpoch, retained, active, used, gate, epoch, disabled,
          plan, deleted, up, crashedWithPlan, recoveredDelete, badRegistration
vars == <<doneEvidence, phase, readEpoch, retained, active, used, gate, epoch, disabled,
          plan, deleted, up, crashedWithPlan, recoveredDelete, badRegistration>>
Init == /\ doneEvidence = {}
        /\ phase = [r \in Refs |-> "idle"]
        /\ readEpoch = [r \in Refs |-> 0]
        /\ retained = {} /\ active = {} /\ used = {}
        /\ gate = TRUE /\ epoch = 0 /\ disabled = FALSE
        /\ plan = FALSE /\ deleted = FALSE /\ up = TRUE
        /\ crashedWithPlan = FALSE /\ recoveredDelete = FALSE
        /\ badRegistration = FALSE
ReadGate(r) == /\ up /\ phase[r] = "idle" /\ gate
               /\ phase' = [phase EXCEPT ![r] = "read"]
               /\ readEpoch' = [readEpoch EXCEPT ![r] = epoch]
               /\ UNCHANGED <<doneEvidence, retained, active, used, gate, epoch, disabled, plan,
                              deleted, up, crashedWithPlan, recoveredDelete, badRegistration>>
Decide(r) == /\ up /\ phase[r] = "read"
             /\ phase' = [phase EXCEPT ![r] = "decided"]
             /\ UNCHANGED <<doneEvidence, readEpoch, retained, active, used, gate, epoch, disabled, plan,
                            deleted, up, crashedWithPlan, recoveredDelete, badRegistration>>
Register(r) == /\ up /\ phase[r] = "decided"
               /\ (BugStaleRegister \/ (gate /\ readEpoch[r] = epoch))
               /\ phase' = [phase EXCEPT ![r] = "pinned"]
               /\ retained' = retained \cup {r}
               /\ badRegistration' = (badRegistration \/ ~gate \/ readEpoch[r] # epoch)
               /\ UNCHANGED <<doneEvidence, readEpoch, active, used, gate, epoch, disabled, plan,
                              deleted, up, crashedWithPlan, recoveredDelete>>
Use(r) == /\ up /\ phase[r] = "pinned" /\ ~disabled
          /\ phase' = [phase EXCEPT ![r] = "using"]
          /\ active' = active \cup {r} /\ used' = used \cup {r}
          /\ UNCHANGED <<doneEvidence, readEpoch, retained, gate, epoch, disabled, plan,
                         deleted, up, crashedWithPlan, recoveredDelete, badRegistration>>
RecordDone(r) == /\ r \in active /\ r \notin doneEvidence
                 /\ doneEvidence' = doneEvidence \cup {r}
                 /\ UNCHANGED <<phase, readEpoch, retained, active, used, gate, epoch, disabled, plan,
                      deleted, up, crashedWithPlan, recoveredDelete, badRegistration>>
Finish(r) == /\ up /\ phase[r] = "using" /\ r \in doneEvidence
             /\ phase' = [phase EXCEPT ![r] = "done"]
             /\ active' = active \ {r}
             /\ UNCHANGED <<doneEvidence, readEpoch, retained, used, gate, epoch, disabled, plan,
                            deleted, up, crashedWithPlan, recoveredDelete, badRegistration>>
Seal(r) == /\ up /\ phase[r] = "pinned"
           /\ phase' = [phase EXCEPT ![r] = "sealed"]
           /\ UNCHANGED <<doneEvidence, readEpoch, retained, active, used, gate, epoch, disabled, plan,
                          deleted, up, crashedWithPlan, recoveredDelete, badRegistration>>
Release(r) == /\ up /\ (phase[r] \in {"done", "sealed"} \/
                                      (BugEarlyRelease /\ phase[r] = "using"))
              /\ phase' = [phase EXCEPT ![r] = "released"]
              /\ retained' = retained \ {r}
              /\ UNCHANGED <<doneEvidence, readEpoch, active, used, gate, epoch, disabled, plan,
                             deleted, up, crashedWithPlan, recoveredDelete, badRegistration>>
Close == /\ up /\ gate /\ gate' = FALSE /\ epoch' = 1
         /\ UNCHANGED <<doneEvidence, phase, readEpoch, retained, active, used, disabled, plan,
                        deleted, up, crashedWithPlan, recoveredDelete, badRegistration>>
Withdraw == /\ up /\ ~disabled /\ disabled' = TRUE
            /\ UNCHANGED <<doneEvidence, phase, readEpoch, retained, active, used, gate, epoch, plan,
                           deleted, up, crashedWithPlan, recoveredDelete, badRegistration>>
PlanReclaim == /\ up /\ ~gate /\ retained = {} /\ ~plan
               /\ plan' = TRUE
               /\ UNCHANGED <<doneEvidence, phase, readEpoch, retained, active, used, gate, epoch, disabled,
                              deleted, up, crashedWithPlan, recoveredDelete, badRegistration>>
Delete == /\ up /\ plan /\ ~deleted /\ deleted' = TRUE
          /\ recoveredDelete' = crashedWithPlan
          /\ UNCHANGED <<doneEvidence, phase, readEpoch, retained, active, used, gate, epoch, disabled,
                         plan, up, crashedWithPlan, badRegistration>>
Crash == /\ up /\ up' = FALSE
         /\ phase' = [r \in Refs |-> IF phase[r] \in {"read", "decided"} THEN "idle" ELSE phase[r]]
         /\ crashedWithPlan' = (crashedWithPlan \/ (plan /\ ~deleted))
         /\ UNCHANGED <<doneEvidence, readEpoch, retained, active, used, gate, epoch, disabled,
                        plan, deleted, recoveredDelete, badRegistration>>
Restart == /\ ~up /\ up' = TRUE
           /\ UNCHANGED <<doneEvidence, phase, readEpoch, retained, active, used, gate, epoch, disabled,
                          plan, deleted, crashedWithPlan, recoveredDelete, badRegistration>>
Next == (\E r \in Refs: ReadGate(r) \/ Decide(r) \/ Register(r) \/ Use(r) \/ RecordDone(r) \/ Finish(r) \/ Seal(r) \/ Release(r))
        \/ Close \/ Withdraw \/ PlanReclaim \/ Delete \/ Crash \/ Restart
Spec == Init /\ [][Next]_vars
FairSpec == Spec /\ <>[]up /\ WF_vars(Close) /\ WF_vars(PlanReclaim) /\ WF_vars(Delete)
            /\ (\A r \in Refs: WF_vars(RecordDone(r)) /\ WF_vars(Finish(r)) /\ WF_vars(Seal(r)) /\ WF_vars(Release(r)))
EventualReclaim == <>deleted
TypeOK == /\ retained \subseteq Refs /\ active \subseteq Refs /\ used \subseteq Refs
          /\ epoch \in 0..1 /\ up \in BOOLEAN /\ gate \in BOOLEAN
UseHasPin == active \subseteq retained
NoPrematureReclaim == (plan \/ deleted) => retained = {}
FreshRegistration == ~badRegistration
NoNormalWitness == ~(deleted /\ used # {})
NoRecoveryWitness == ~(recoveredDelete /\ used # {})
=============================================================================
