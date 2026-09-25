import Std

/-!
# Durable handoff: safety for arbitrary finite executions

This model has two independent durable stores and one logical operation.
The original intent is fixed. `reqGood` counts requests for that intent;
`reqBad` counts conflicting-intent probes after durable B acceptance.
Local durable updates are individual transitions; there is no A/B transaction.
Acceptance creates B's follow-up Work, not an external business effect.
-/

namespace DurableHandoff

inductive Phase where
  | idle
  | pending
  | released
  deriving DecidableEq, Repr

structure State where
  aPhase : Phase
  bAccepted : Bool
  bWork : Bool
  bCreates : Nat
  reqGood : Nat
  reqBad : Nat
  acks : Nat
  ackSeen : Bool
  aUp : Bool
  bUp : Bool
  bKnown : Bool
  aRemaining : Nat
  bRemaining : Nat
  gap : Bool
  stable : Bool
  deriving DecidableEq, Repr

def initial (aBudget bBudget : Nat) (initiallyStable : Bool) : State :=
  { aPhase := .idle
    bAccepted := false
    bWork := false
    bCreates := 0
    reqGood := 0
    reqBad := 0
    acks := 0
    ackSeen := false
    aUp := true
    bUp := true
    bKnown := false
    aRemaining := aBudget
    bRemaining := bBudget
    gap := false
    stable := initiallyStable }

def Init (aBudget bBudget : Nat) (initiallyStable : Bool) (s : State) : Prop :=
  s = initial aBudget bBudget initiallyStable

/- Every constructor is one allowed atomic action. The only atomic durable
   updates are within A or within B. Guards do not assume the safety theorem. -/
inductive Step (reqCap ackCap : Nat) : State → State → Prop where
  | saveA (s : State) (up : s.aUp = true) (idle : s.aPhase = .idle) :
      Step reqCap ackCap s { s with aPhase := .pending }
  | sendA (s : State) (up : s.aUp = true) (pending : s.aPhase = .pending)
      (budget : 0 < s.aRemaining) (room : s.reqGood + s.reqBad < reqCap) :
      Step reqCap ackCap s { s with reqGood := s.reqGood + 1, aRemaining := s.aRemaining - 1 }
  | duplicateReq (s : State) (present : 0 < s.reqGood)
      (room : s.reqGood + s.reqBad < reqCap) :
      Step reqCap ackCap s { s with reqGood := s.reqGood + 1 }
  | injectConflict (s : State) (accepted : s.bAccepted = true)
      (room : s.reqGood + s.reqBad < reqCap) :
      Step reqCap ackCap s { s with reqBad := s.reqBad + 1 }
  | commitB (s : State) (up : s.bUp = true) (present : 0 < s.reqGood)
      (fresh : s.bAccepted = false) :
      Step reqCap ackCap s
        { s with reqGood := s.reqGood - 1, bAccepted := true, bWork := true, bCreates := s.bCreates + 1 }
  | duplicateB (s : State) (up : s.bUp = true) (present : 0 < s.reqGood)
      (accepted : s.bAccepted = true) :
      Step reqCap ackCap s { s with reqGood := s.reqGood - 1 }
  | rejectConflict (s : State) (up : s.bUp = true) (present : 0 < s.reqBad)
      (accepted : s.bAccepted = true) :
      Step reqCap ackCap s { s with reqBad := s.reqBad - 1 }
  | readReceipt (s : State) (up : s.bUp = true) (accepted : s.bAccepted = true)
      (unknown : s.bKnown = false) :
      Step reqCap ackCap s { s with bKnown := true }
  | sendAck (s : State) (up : s.bUp = true) (known : s.bKnown = true)
      (budget : 0 < s.bRemaining) (room : s.acks < ackCap) :
      Step reqCap ackCap s { s with acks := s.acks + 1, bRemaining := s.bRemaining - 1 }
  | duplicateAck (s : State) (present : 0 < s.acks) (room : s.acks < ackCap) :
      Step reqCap ackCap s { s with acks := s.acks + 1 }
  | receiveAck (s : State) (up : s.aUp = true) (pending : s.aPhase = .pending)
      (present : 0 < s.acks) :
      Step reqCap ackCap s { s with acks := s.acks - 1, ackSeen := true }
  | finishA (s : State) (up : s.aUp = true) (pending : s.aPhase = .pending)
      (seen : s.ackSeen = true) :
      Step reqCap ackCap s { s with aPhase := .released, ackSeen := false, gap := false }
  | declareGap (s : State) (up : s.aUp = true) (pending : s.aPhase = .pending)
      (exhausted : s.aRemaining = 0) (unreported : s.gap = false) :
      Step reqCap ackCap s { s with gap := true }
  | dropGood (s : State) (unstable : s.stable = false) (present : 0 < s.reqGood) :
      Step reqCap ackCap s { s with reqGood := s.reqGood - 1 }
  | dropBad (s : State) (unstable : s.stable = false) (present : 0 < s.reqBad) :
      Step reqCap ackCap s { s with reqBad := s.reqBad - 1 }
  | dropAck (s : State) (unstable : s.stable = false) (present : 0 < s.acks) :
      Step reqCap ackCap s { s with acks := s.acks - 1 }
  | crashA (s : State) (unstable : s.stable = false) (up : s.aUp = true) :
      Step reqCap ackCap s { s with aUp := false, ackSeen := false }
  | crashB (s : State) (unstable : s.stable = false) (up : s.bUp = true) :
      Step reqCap ackCap s { s with bUp := false, bKnown := false }
  | recoverA (s : State) (down : s.aUp = false) :
      Step reqCap ackCap s { s with aUp := true }
  | recoverB (s : State) (down : s.bUp = false) :
      Step reqCap ackCap s { s with bUp := true }
  | stabilize (s : State) (unstable : s.stable = false) :
      Step reqCap ackCap s { s with stable := true }
  | stutter (s : State) : Step reqCap ackCap s s

inductive Reachable (reqCap ackCap aBudget bBudget : Nat) (initiallyStable : Bool) : State → Prop where
  | init : Reachable reqCap ackCap aBudget bBudget initiallyStable
      (initial aBudget bBudget initiallyStable)
  | tail {s t : State} : Reachable reqCap ackCap aBudget bBudget initiallyStable s →
      Step reqCap ackCap s t → Reachable reqCap ackCap aBudget bBudget initiallyStable t

/-- The first seven fields expose the six requested safety properties. The fourth
    property has both the equivalence and exact create count. The final field
    additionally tracks the TLC model's durable-gap lifecycle invariant. -/
structure Safety (s : State) : Prop where
  released_owned : s.aPhase = .released → s.bAccepted = true ∧ s.bWork = true
  responsibility : s.aPhase ≠ .idle → s.aPhase = .pending ∨ s.bWork = true
  at_most_once : s.bCreates ≤ 1
  accepted_work : s.bAccepted = true ↔ s.bWork = true
  create_count : s.bCreates = if s.bAccepted then 1 else 0
  receipt_origin :
    (s.bKnown = true ∨ s.ackSeen = true ∨ 0 < s.acks) → s.bAccepted = true
  conflict_origin : 0 < s.reqBad → s.bAccepted = true
  gap_retained : s.gap = true → s.aPhase = .pending

theorem initial_safe (aBudget bBudget : Nat) (initiallyStable : Bool) :
    Safety (initial aBudget bBudget initiallyStable) := by
  constructor <;> simp [initial]

/-- Complete action-by-action preservation. `cases step` covers all 22 Step
    constructors, including loss, crash, recovery, and stutter. -/
theorem step_preserves_safety {reqCap ackCap : Nat} {s t : State}
    (safe : Safety s) (step : Step reqCap ackCap s t) : Safety t := by
  rcases safe with ⟨released, responsible, once, paired, count, receipt, conflict, gap⟩
  cases step <;> constructor <;> simp_all <;> try omega
  -- CrashA forgets one source of knowledge; it cannot add a new source.
  intro observed
  rcases observed with known | inFlight
  · exact receipt (Or.inl known)
  · exact receipt (Or.inr (Or.inr inFlight))

/-- No bound is placed on the number of transitions, budgets, or channel cap. -/
theorem reachable_safe {reqCap ackCap aBudget bBudget : Nat} {initiallyStable : Bool} {s : State}
    (reachable : Reachable reqCap ackCap aBudget bBudget initiallyStable s) : Safety s := by
  induction reachable with
  | init => exact initial_safe _ _ _
  | tail previous step ih => exact step_preserves_safety ih step

theorem released_has_durable_owner
    {reqCap ackCap aBudget bBudget : Nat} {initiallyStable : Bool} {s : State}
    (reachable : Reachable reqCap ackCap aBudget bBudget initiallyStable s)
    (released : s.aPhase = .released) : s.bAccepted = true ∧ s.bWork = true :=
  (reachable_safe reachable).released_owned released

theorem responsibility_never_lost
    {reqCap ackCap aBudget bBudget : Nat} {initiallyStable : Bool} {s : State}
    (reachable : Reachable reqCap ackCap aBudget bBudget initiallyStable s)
    (started : s.aPhase ≠ .idle) : s.aPhase = .pending ∨ s.bWork = true :=
  (reachable_safe reachable).responsibility started

theorem work_created_at_most_once
    {reqCap ackCap aBudget bBudget : Nat} {initiallyStable : Bool} {s : State}
    (reachable : Reachable reqCap ackCap aBudget bBudget initiallyStable s) : s.bCreates ≤ 1 :=
  (reachable_safe reachable).at_most_once

theorem acceptance_and_work_agree
    {reqCap ackCap aBudget bBudget : Nat} {initiallyStable : Bool} {s : State}
    (reachable : Reachable reqCap ackCap aBudget bBudget initiallyStable s) :
    (s.bAccepted = true ↔ s.bWork = true) ∧
      s.bCreates = (if s.bAccepted then 1 else 0) :=
  ⟨(reachable_safe reachable).accepted_work, (reachable_safe reachable).create_count⟩

theorem acknowledgements_have_durable_origin
    {reqCap ackCap aBudget bBudget : Nat} {initiallyStable : Bool} {s : State}
    (reachable : Reachable reqCap ackCap aBudget bBudget initiallyStable s)
    (receipt : s.bKnown = true ∨ s.ackSeen = true ∨ 0 < s.acks) :
    s.bAccepted = true :=
  (reachable_safe reachable).receipt_origin receipt

theorem conflict_probes_follow_acceptance
    {reqCap ackCap aBudget bBudget : Nat} {initiallyStable : Bool} {s : State}
    (reachable : Reachable reqCap ackCap aBudget bBudget initiallyStable s)
    (probe : 0 < s.reqBad) : s.bAccepted = true :=
  (reachable_safe reachable).conflict_origin probe

theorem gap_is_pending
    {reqCap ackCap aBudget bBudget : Nat} {initiallyStable : Bool} {s : State}
    (reachable : Reachable reqCap ackCap aBudget bBudget initiallyStable s)
    (gap : s.gap = true) : s.aPhase = .pending :=
  (reachable_safe reachable).gap_retained gap

/-- Bool/Phase/Nat typing is intrinsic; these bounds cover the remaining
    baseline TLC TypeOK requirements (bCreates <= 2 follows from <= 1). -/
structure Bounds (reqCap ackCap aBudget bBudget : Nat) (s : State) : Prop where
  requests : s.reqGood + s.reqBad ≤ reqCap
  acknowledgements : s.acks ≤ ackCap
  a_budget : s.aRemaining ≤ aBudget
  b_budget : s.bRemaining ≤ bBudget

theorem initial_bounds (reqCap ackCap aBudget bBudget : Nat) (initiallyStable : Bool) :
    Bounds reqCap ackCap aBudget bBudget (initial aBudget bBudget initiallyStable) := by
  constructor <;> simp [initial]

theorem step_preserves_bounds {reqCap ackCap aBudget bBudget : Nat} {s t : State}
    (bounds : Bounds reqCap ackCap aBudget bBudget s) (step : Step reqCap ackCap s t) :
    Bounds reqCap ackCap aBudget bBudget t := by
  rcases bounds with ⟨requests, acknowledgements, aBudgetBound, bBudgetBound⟩
  cases step <;> constructor <;> simp_all <;> omega

theorem reachable_bounds
    {reqCap ackCap aBudget bBudget : Nat} {initiallyStable : Bool} {s : State}
    (reachable : Reachable reqCap ackCap aBudget bBudget initiallyStable s) :
    Bounds reqCap ackCap aBudget bBudget s := by
  induction reachable with
  | init => exact initial_bounds _ _ _ _ _
  | tail previous step ih => exact step_preserves_bounds ih step

/-- Lifecycle monotonicity is separate from the current-state responsibility
    predicate: a future reset-to-idle action must not make it vacuously true. -/
theorem step_never_returns_to_idle {reqCap ackCap : Nat} {s t : State}
    (step : Step reqCap ackCap s t) (started : s.aPhase ≠ .idle) :
    t.aPhase ≠ .idle := by
  cases step <;> simp_all

theorem step_keeps_released {reqCap ackCap : Nat} {s t : State}
    (step : Step reqCap ackCap s t) (released : s.aPhase = .released) :
    t.aPhase = .released := by
  cases step <;> simp_all

/-! A kernel-checked concrete execution: duplicate the request, durably accept,
read the receipt, crash B, recover B, consume the duplicate without creating
another Work, reread the receipt, send/receive the acknowledgement, release A.
This is an existence witness, not a universal progress claim. -/

def demo0 : State := initial 2 1 false
def demo1 : State := { demo0 with aPhase := .pending }
def demo2 : State := { demo1 with reqGood := 1, aRemaining := 1 }
def demo3 : State := { demo2 with reqGood := 2 }
def demo4 : State :=
  { demo3 with reqGood := 1, bAccepted := true, bWork := true, bCreates := 1 }
def demo5 : State := { demo4 with bKnown := true }
def demo6 : State := { demo5 with bUp := false, bKnown := false }
def demo7 : State := { demo6 with bUp := true }
def demo8 : State := { demo7 with reqGood := 0 }
def demo9 : State := { demo8 with bKnown := true }
def demo10 : State := { demo9 with acks := 1, bRemaining := 0 }
def demo11 : State := { demo10 with acks := 0, ackSeen := true }
def demo12 : State :=
  { demo11 with aPhase := .released, ackSeen := false, gap := false }

theorem before_restart_reachable : Reachable 2 2 2 1 false demo5 := by
  have r0 : Reachable 2 2 2 1 false demo0 := .init
  have r1 : Reachable 2 2 2 1 false demo1 :=
    .tail r0 (.saveA demo0 (by decide) (by decide))
  have r2 : Reachable 2 2 2 1 false demo2 :=
    .tail r1 (.sendA demo1 (by decide) (by decide) (by decide) (by decide))
  have r3 : Reachable 2 2 2 1 false demo3 :=
    .tail r2 (.duplicateReq demo2 (by decide) (by decide))
  have r4 : Reachable 2 2 2 1 false demo4 :=
    .tail r3 (.commitB demo3 (by decide) (by decide) (by decide))
  exact .tail r4 (.readReceipt demo4 (by decide) (by decide) (by decide))

theorem after_restart_duplicate_reachable : Reachable 2 2 2 1 false demo8 := by
  have r6 : Reachable 2 2 2 1 false demo6 :=
    .tail before_restart_reachable (.crashB demo5 (by decide) (by decide))
  have r7 : Reachable 2 2 2 1 false demo7 :=
    .tail r6 (.recoverB demo6 (by decide))
  exact .tail r7 (.duplicateB demo7 (by decide) (by decide) (by decide))

theorem restart_duplicate_witness :
    Reachable 2 2 2 1 false demo5 ∧
    Step 2 2 demo5 demo6 ∧ Step 2 2 demo6 demo7 ∧ Step 2 2 demo7 demo8 ∧
    demo5.bAccepted = true ∧ demo5.bKnown = true ∧
    demo6.bAccepted = true ∧ demo6.bKnown = false ∧
    demo8.bAccepted = true ∧ demo8.bWork = true ∧ demo8.bCreates = 1 := by
  refine ⟨before_restart_reachable,
    .crashB demo5 (by decide) (by decide),
    .recoverB demo6 (by decide),
    .duplicateB demo7 (by decide) (by decide) (by decide), ?_⟩
  decide

theorem completed_reachable : Reachable 2 2 2 1 false demo12 := by
  have r9 : Reachable 2 2 2 1 false demo9 :=
    .tail after_restart_duplicate_reachable
      (.readReceipt demo8 (by decide) (by decide) (by decide))
  have r10 : Reachable 2 2 2 1 false demo10 :=
    .tail r9 (.sendAck demo9 (by decide) (by decide) (by decide) (by decide))
  have r11 : Reachable 2 2 2 1 false demo11 :=
    .tail r10 (.receiveAck demo10 (by decide) (by decide) (by decide))
  exact .tail r11 (.finishA demo11 (by decide) (by decide) (by decide))

theorem completion_after_restart_witness :
    ∃ s, Reachable 2 2 2 1 false s ∧ s.aPhase = .released ∧
      s.bAccepted = true ∧ s.bWork = true ∧ s.bCreates = 1 := by
  exact ⟨demo12, completed_reachable, by decide, by decide, by decide, by decide⟩

/- A second execution family starts with one request and two ACK send
   opportunities. It witnesses both failure-free completion and completion
   after an ACK is lost and B restarts without volatile receipt knowledge. -/
def lost0 : State := initial 2 2 false
def lost1 : State := { lost0 with aPhase := .pending }
def lost2 : State := { lost1 with reqGood := 1, aRemaining := 1 }
def lost3 : State :=
  { lost2 with reqGood := 0, bAccepted := true, bWork := true, bCreates := 1 }
def lost4 : State := { lost3 with bKnown := true }
def lost5 : State := { lost4 with acks := 1, bRemaining := 1 }
def lost6 : State := { lost5 with acks := 0 }
def lost7 : State := { lost6 with bUp := false, bKnown := false }
def lost8 : State := { lost7 with bUp := true }
def lost9 : State := { lost8 with reqGood := 1, aRemaining := 0 }
def lost10 : State := { lost9 with reqGood := 0 }
def lost11 : State := { lost10 with bKnown := true }
def lost12 : State := { lost11 with acks := 1, bRemaining := 0 }
def lost13 : State := { lost12 with acks := 0, ackSeen := true }
def lost14 : State :=
  { lost13 with aPhase := .released, ackSeen := false, gap := false }
def healthy6 : State := { lost5 with acks := 0, ackSeen := true }
def healthy7 : State :=
  { healthy6 with aPhase := .released, ackSeen := false, gap := false }

theorem acknowledgement_ready_reachable : Reachable 1 1 2 2 false lost5 := by
  have r0 : Reachable 1 1 2 2 false lost0 := .init
  have r1 : Reachable 1 1 2 2 false lost1 :=
    .tail r0 (.saveA lost0 (by decide) (by decide))
  have r2 : Reachable 1 1 2 2 false lost2 :=
    .tail r1 (.sendA lost1 (by decide) (by decide) (by decide) (by decide))
  have r3 : Reachable 1 1 2 2 false lost3 :=
    .tail r2 (.commitB lost2 (by decide) (by decide) (by decide))
  have r4 : Reachable 1 1 2 2 false lost4 :=
    .tail r3 (.readReceipt lost3 (by decide) (by decide) (by decide))
  exact .tail r4 (.sendAck lost4 (by decide) (by decide) (by decide) (by decide))

theorem normal_completion_witness :
    ∃ s, Reachable 1 1 2 2 false s ∧ s.aPhase = .released ∧
      s.bAccepted = true ∧ s.bWork = true ∧ s.bCreates = 1 := by
  have r6 : Reachable 1 1 2 2 false healthy6 :=
    .tail acknowledgement_ready_reachable
      (.receiveAck lost5 (by decide) (by decide) (by decide))
  have r7 : Reachable 1 1 2 2 false healthy7 :=
    .tail r6 (.finishA healthy6 (by decide) (by decide) (by decide))
  exact ⟨healthy7, r7, by decide, by decide, by decide, by decide⟩

theorem lost_ack_recovery_reachable : Reachable 1 1 2 2 false lost14 := by
  have r6 : Reachable 1 1 2 2 false lost6 :=
    .tail acknowledgement_ready_reachable (.dropAck lost5 (by decide) (by decide))
  have r7 : Reachable 1 1 2 2 false lost7 :=
    .tail r6 (.crashB lost6 (by decide) (by decide))
  have r8 : Reachable 1 1 2 2 false lost8 :=
    .tail r7 (.recoverB lost7 (by decide))
  have r9 : Reachable 1 1 2 2 false lost9 :=
    .tail r8 (.sendA lost8 (by decide) (by decide) (by decide) (by decide))
  have r10 : Reachable 1 1 2 2 false lost10 :=
    .tail r9 (.duplicateB lost9 (by decide) (by decide) (by decide))
  have r11 : Reachable 1 1 2 2 false lost11 :=
    .tail r10 (.readReceipt lost10 (by decide) (by decide) (by decide))
  have r12 : Reachable 1 1 2 2 false lost12 :=
    .tail r11 (.sendAck lost11 (by decide) (by decide) (by decide) (by decide))
  have r13 : Reachable 1 1 2 2 false lost13 :=
    .tail r12 (.receiveAck lost12 (by decide) (by decide) (by decide))
  exact .tail r13 (.finishA lost13 (by decide) (by decide) (by decide))

theorem lost_ack_recovery_witness :
    Reachable 1 1 2 2 false lost5 ∧ Step 1 1 lost5 lost6 ∧
    Step 1 1 lost6 lost7 ∧ Step 1 1 lost7 lost8 ∧
    Step 1 1 lost8 lost9 ∧ Step 1 1 lost9 lost10 ∧
    lost5.acks = 1 ∧ lost6.acks = 0 ∧ lost7.bKnown = false ∧
    Reachable 1 1 2 2 false lost14 ∧ lost14.aPhase = .released ∧
    lost14.bWork = true ∧ lost14.bCreates = 1 := by
  exact ⟨acknowledgement_ready_reachable,
    .dropAck lost5 (by decide) (by decide),
    .crashB lost6 (by decide) (by decide),
    .recoverB lost7 (by decide),
    .sendA lost8 (by decide) (by decide) (by decide) (by decide),
    .duplicateB lost9 (by decide) (by decide) (by decide),
    by decide, by decide, by decide,
    lost_ack_recovery_reachable, by decide, by decide, by decide⟩

#print axioms initial_safe
#print axioms step_preserves_safety
#print axioms reachable_safe
#print axioms released_has_durable_owner
#print axioms responsibility_never_lost
#print axioms work_created_at_most_once
#print axioms acceptance_and_work_agree
#print axioms acknowledgements_have_durable_origin
#print axioms conflict_probes_follow_acceptance
#print axioms gap_is_pending
#print axioms reachable_bounds
#print axioms step_never_returns_to_idle
#print axioms step_keeps_released
#print axioms restart_duplicate_witness
#print axioms completion_after_restart_witness
#print axioms normal_completion_witness
#print axioms lost_ack_recovery_witness

end DurableHandoff
