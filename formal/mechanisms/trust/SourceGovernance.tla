------------------------- MODULE SourceGovernance -------------------------
EXTENDS Naturals, FiniteSets
CONSTANT Mutant
Sources == {"root", "other"}
Artifacts == {"derived", "combined"}
Deps(a) == IF a = "derived" THEN {"root"} ELSE Sources
VARIABLE s
Init == s = [closed |-> {}, bytes |-> {}, retained |-> {}, registered |-> {},
             published |-> {}, required |-> {}, platformAck |-> {}, cleaned |-> {},
             unsafeUse |-> FALSE, badPublish |-> FALSE,
             falseComplete |-> FALSE, blockedUse |-> FALSE]
Affected(closed) == {a \in Artifacts : Deps(a) \cap closed # {}}
Acquire(a) == /\ a \in Artifacts /\ Deps(a) \cap s.closed = {}
              /\ s' = [s EXCEPT !.bytes = @ \cup {a}]
Retain(a) == /\ a \in s.bytes /\ Deps(a) \cap s.closed = {}
             /\ s' = [s EXCEPT !.retained = @ \cup {a}]
Register(a) == /\ a \in s.bytes /\ Deps(a) \cap s.closed = {}
               /\ s' = [s EXCEPT !.registered = @ \cup {a}]
Publish(a) == /\ a \in s.bytes /\ Deps(a) \cap s.closed = {}
              /\ LET ok == a \in s.retained /\ a \in s.registered
                     allow == ok \/ Mutant = "unregistered"
                 IN s' = [s EXCEPT !.published = IF allow THEN @ \cup {a} ELSE @,
                          !.badPublish = @ \/ (allow /\ ~ok)]
Close(src) == /\ src \in Sources \ s.closed
              /\ LET closed == s.closed \cup {src}
                 IN s' = [s EXCEPT !.closed = closed,
                          !.required = @ \cup (s.registered \cap Affected(closed))]
Use(a) == /\ a \in s.published /\ a \in s.bytes
          /\ LET valid == Deps(a) \cap s.closed = {}
                 allow == valid \/ Mutant = "staleSource"
             IN s' = [s EXCEPT !.unsafeUse = @ \/ (allow /\ ~valid),
                      !.blockedUse = @ \/ (~allow /\ ~valid)]
PlatformReports(a) == /\ a \in s.required /\ a \notin s.platformAck
                      /\ s' = [s EXCEPT !.platformAck = @ \cup {a}]
ApplyCleanup(a) == /\ a \in s.platformAck /\ a \notin s.cleaned
                   /\ s' = [s EXCEPT !.cleaned = @ \cup {a}, !.bytes = @ \ {a}]
ReportComplete == LET requiredDone == s.required \subseteq s.cleaned
                      report == IF Mutant = "logicalIsPhysical" THEN s.closed # {} ELSE requiredDone
                  IN s' = [s EXCEPT !.falseComplete = @ \/ (report /\ ~requiredDone)]
Next == \/ (\E a \in Artifacts : Acquire(a) \/ Retain(a) \/ Register(a) \/ Publish(a)
             \/ Use(a) \/ PlatformReports(a) \/ ApplyCleanup(a))
        \/ (\E src \in Sources : Close(src)) \/ ReportComplete
Spec == Init /\ [][Next]_s
NoUnsafeUse == ~s.unsafeUse
RegisteredBeforePublish == ~s.badPublish
ClosureCoverage == s.registered \cap Affected(s.closed) \subseteq s.required
PhysicalEvidence == s.cleaned \subseteq s.platformAck
NoFalseComplete == ~s.falseComplete
Safety == NoUnsafeUse /\ RegisteredBeforePublish /\ ClosureCoverage /\ PhysicalEvidence /\ NoFalseComplete
NoBlockedDerivedUse == ~s.blockedUse
NoCleaned == s.cleaned = {}
=============================================================================
