------------------------------- MODULE Preview -------------------------------
EXTENDS Naturals, FiniteSets, TLC
CONSTANTS BugMissingPreview, BugStalePreview
Previews == {1,2}
VARIABLES packetAcquiredAt, retained, obtained, acquiredAt, revision, openSource, packet, packetRev,
          pending, transferred, badPreview, actualBytes
vars == <<packetAcquiredAt, retained, obtained, acquiredAt, revision, openSource, packet, packetRev,
          pending, transferred, badPreview, actualBytes>>
Init == /\ packetAcquiredAt = [p \in Previews |-> 2]
        /\ retained = {} /\ obtained = {} /\ acquiredAt = [p \in Previews |-> 2]
        /\ revision = 0 /\ openSource = TRUE /\ packet = {} /\ packetRev = 0
        /\ pending = FALSE /\ transferred = FALSE /\ badPreview = FALSE /\ actualBytes = {}
Fetch(p) == /\ openSource /\ p \notin actualBytes /\ actualBytes' = actualBytes \cup {p}
            /\ UNCHANGED <<packetAcquiredAt, retained, obtained, acquiredAt, revision, openSource, packet,
                 packetRev, pending, transferred, badPreview>>
Retain(p) == /\ p \in actualBytes /\ p \notin retained /\ retained' = retained \cup {p}
             /\ UNCHANGED <<packetAcquiredAt, obtained, acquiredAt, revision, openSource, packet, packetRev,
                  pending, transferred, badPreview, actualBytes>>
Acquire(p) == /\ openSource /\ p \in retained /\ p \in actualBytes
              /\ acquiredAt[p] # revision
              /\ acquiredAt' = [acquiredAt EXCEPT ![p] = revision] /\ obtained' = obtained \cup {p}
              /\ UNCHANGED <<packetAcquiredAt, retained, revision, openSource, packet, packetRev,
                   pending, transferred, badPreview, actualBytes>>
\* Candidate payloads may be incomplete or stale. Consumer checks evidence, not the caller's claim.
Propose(ps) == /\ ~pending /\ ~transferred /\ ps \subseteq obtained
               /\ packetAcquiredAt' = acquiredAt
               /\ packet' = ps /\ packetRev' = revision /\ pending' = TRUE
               /\ UNCHANGED << retained, obtained, acquiredAt, revision, openSource,
                    transferred, badPreview, actualBytes>>
ChangeProjection == /\ revision = 0 /\ revision' = 1
                    /\ UNCHANGED <<packetAcquiredAt, retained, obtained, acquiredAt, openSource, packet,
                         packetRev, pending, transferred, badPreview, actualBytes>>
CloseSource == /\ openSource /\ openSource' = FALSE
               /\ UNCHANGED <<packetAcquiredAt, retained, obtained, acquiredAt, revision, packet,
                    packetRev, pending, transferred, badPreview, actualBytes>>
Commit == /\ pending /\ ~transferred
          /\ (BugMissingPreview \/ packet = Previews)
          /\ (BugStalePreview \/ (openSource /\ packetRev = revision
                                      /\ (\A p \in packet: packetAcquiredAt[p] = revision)))
          /\ transferred' = TRUE
          /\ badPreview' = (badPreview \/ packet # Previews \/ ~openSource \/ packetRev # revision
                                \/ (\E p \in packet: packetAcquiredAt[p] # revision))
          /\ UNCHANGED <<packetAcquiredAt, retained, obtained, acquiredAt, revision, openSource, packet,
               packetRev, pending, actualBytes>>
Next == (\E p \in Previews: Fetch(p) \/ Retain(p) \/ Acquire(p))
        \/ (\E ps \in SUBSET Previews: Propose(ps)) \/ ChangeProjection \/ CloseSource \/ Commit
Spec == Init /\ [][Next]_vars
RetainedBeforeAcquisition == obtained \subseteq retained
CompleteCurrentPreview == ~badPreview
NoNormalWitness == ~transferred
NoClosedOldPacketWitness == ~(pending /\ ~openSource /\ ~transferred /\ packet = Previews)
=============================================================================
