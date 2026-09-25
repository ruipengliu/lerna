-------------------------------- MODULE UI --------------------------------
EXTENDS Naturals, TLC
CONSTANTS BugStaleReply, BugDeltaBase
VARIABLES pendingCrashGen, generation, intent, pending, sentGen, sentIntent, sentSession, session,
          confirmedGen, confirmedIntent, visible, cacheRev, textLength, full,
          result, resultEver, up, crashed, recovered, everShown
vars == <<pendingCrashGen, generation, intent, pending, sentGen, sentIntent, sentSession, session,
          confirmedGen, confirmedIntent, visible, cacheRev, textLength, full,
          result, resultEver, up, crashed, recovered, everShown>>
Init == /\ pendingCrashGen = 3
        /\ generation = 0 /\ intent = "closed" /\ pending = FALSE
        /\ sentGen = 0 /\ sentIntent = "closed" /\ sentSession = 0 /\ session = 0
        /\ confirmedGen = 0 /\ confirmedIntent = "closed" /\ visible = FALSE
        /\ cacheRev = 0 /\ textLength = 0 /\ full = FALSE
        /\ result = "none" /\ resultEver = FALSE
        /\ up = TRUE /\ crashed = FALSE /\ recovered = FALSE /\ everShown = FALSE
Intent(i) == /\ up /\ generation < 2 /\ i \in {"open", "closed"}
             /\ generation' = generation + 1 /\ intent' = i /\ visible' = FALSE
             /\ UNCHANGED <<pendingCrashGen, pending, sentGen, sentIntent, sentSession, session,
                confirmedGen, confirmedIntent, cacheRev, textLength, full,
                result, resultEver, up, crashed, recovered, everShown>>
Send == /\ up /\ ~pending /\ generation > confirmedGen
        /\ pending' = TRUE /\ sentGen' = generation /\ sentIntent' = intent
        /\ sentSession' = session /\ visible' = FALSE
        /\ UNCHANGED <<pendingCrashGen, generation, intent, session, confirmedGen, confirmedIntent,
             cacheRev, textLength, full, result, resultEver, up, crashed, recovered, everShown>>
Reply == /\ up /\ pending
         /\ pending' = FALSE
         /\ confirmedGen' = sentGen /\ confirmedIntent' = sentIntent
         /\ visible' = (full /\ sentIntent = "open" /\ sentSession = session
                        /\ (BugStaleReply \/ sentGen = generation))
         /\ everShown' = (everShown \/ visible')
         /\ recovered' = (recovered \/ (pendingCrashGen = sentGen /\ visible'))
         /\ UNCHANGED <<pendingCrashGen, generation, intent, sentGen, sentIntent, sentSession, session,
              cacheRev, textLength, full, result, resultEver, up, crashed>>
Snapshot(v) == /\ up /\ v \in 1..2 /\ v >= cacheRev
               /\ cacheRev' = v /\ textLength' = v /\ full' = TRUE
               /\ UNCHANGED <<pendingCrashGen, generation, intent, pending, sentGen, sentIntent, sentSession,
                 session, confirmedGen, confirmedIntent, visible, result, resultEver,
                 up, crashed, recovered, everShown>>
Delta == /\ up /\ cacheRev < 2
         /\ (BugDeltaBase \/ (full /\ cacheRev = 1))
         /\ cacheRev' = 2 /\ textLength' = textLength + 1
         /\ UNCHANGED <<pendingCrashGen, generation, intent, pending, sentGen, sentIntent, sentSession,
              session, confirmedGen, confirmedIntent, visible, full, result, resultEver,
              up, crashed, recovered, everShown>>
Conflict == /\ up /\ full /\ full' = FALSE /\ visible' = FALSE
            /\ UNCHANGED <<pendingCrashGen, generation, intent, pending, sentGen, sentIntent, sentSession,
              session, confirmedGen, confirmedIntent, cacheRev, textLength, result, resultEver,
              up, crashed, recovered, everShown>>
SwitchSession == /\ up /\ session = 0 /\ session' = 1 /\ visible' = FALSE
                 /\ full' = FALSE /\ intent' = "closed" /\ generation' = 2
                 /\ UNCHANGED <<pendingCrashGen, pending, sentGen, sentIntent, sentSession, confirmedGen,
                   confirmedIntent, cacheRev, textLength, result, resultEver, up,
                   crashed, recovered, everShown>>
ReceiveResult == /\ up /\ result = "none" /\ result' = "original-result" /\ resultEver' = TRUE
                 /\ UNCHANGED <<pendingCrashGen, generation, intent, pending, sentGen, sentIntent, sentSession,
                   session, confirmedGen, confirmedIntent, visible, cacheRev, textLength, full,
                   up, crashed, recovered, everShown>>
Crash == /\ up /\ up' = FALSE /\ visible' = FALSE /\ full' = FALSE /\ crashed' = TRUE
         /\ pendingCrashGen' = IF pending THEN sentGen ELSE pendingCrashGen
         /\ UNCHANGED << generation, intent, pending, sentGen, sentIntent, sentSession, session,
           confirmedGen, confirmedIntent, cacheRev, textLength, result, resultEver, recovered, everShown>>
Restart == /\ ~up /\ up' = TRUE
           /\ UNCHANGED <<pendingCrashGen, generation, intent, pending, sentGen, sentIntent, sentSession, session,
             confirmedGen, confirmedIntent, visible, cacheRev, textLength, full,
             result, resultEver, crashed, recovered, everShown>>
Next == (\E i \in {"open", "closed"}: Intent(i)) \/ Send \/ Reply
        \/ (\E v \in 1..2: Snapshot(v)) \/ Delta \/ Conflict \/ SwitchSession
        \/ ReceiveResult \/ Crash \/ Restart
Spec == Init /\ [][Next]_vars
TypeOK == /\ generation \in 0..2 /\ cacheRev \in 0..2 /\ textLength \in 0..2
          /\ session \in 0..1 /\ intent \in {"open", "closed"}
DisplayAuthority == visible => (intent = "open" /\ ~pending /\ full /\ up
                          /\ confirmedGen = generation /\ sentSession = session)
DeltaContinuity == cacheRev = textLength
ResultPersistence == resultEver => result = "original-result"
NoNormalWitness == ~(everShown /\ intent = "closed" /\ resultEver /\ ~visible)
NoRecoveryWitness == ~recovered
=============================================================================
