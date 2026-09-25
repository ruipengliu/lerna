import Std

namespace TransportPrefix

structure State where
  received : Nat → Prop
  target : Nat
  ack : Nat
  business : Nat

def initial : State := ⟨fun _ => False, 0, 0, 0⟩
def Prefix (n : Nat) (received : Nat → Prop) :=
  ∀ i, 0 < i → i ≤ n → received i

inductive Step : State → State → Prop where
  | receive (s : State) (i : Nat) :
      Step s { s with received := fun j => s.received j ∨ j = i }
  | advance (s : State) (next : s.received (s.target + 1)) :
      Step s { s with target := s.target + 1 }
  | ack (s : State) (n : Nat) (bound : n ≤ s.target) (forward : s.ack ≤ n) :
      Step s { s with ack := n }
  | handoff (s : State) (next : s.business + 1 ≤ s.target) :
      Step s { s with business := s.business + 1 }
  | stutter (s : State) : Step s s

structure Safe (s : State) : Prop where
  durable_prefix : Prefix s.target s.received
  ack_bound : s.ack ≤ s.target
  business_bound : s.business ≤ s.target

theorem initial_safe : Safe initial := by
  refine ⟨?_, by decide, by decide⟩
  intro i positive bounded
  simp [initial] at bounded
  omega

theorem step_safe {s t : State} (safe : Safe s) (step : Step s t) : Safe t := by
  cases step with
  | receive i =>
    refine ⟨?_, safe.ack_bound, safe.business_bound⟩
    intro j positive bounded
    exact Or.inl (safe.durable_prefix j positive bounded)
  | advance next =>
    refine ⟨?_, by have := safe.ack_bound; dsimp; omega,
                by have := safe.business_bound; dsimp; omega⟩
    intro i positive bounded
    by_cases old : i ≤ s.target
    · exact safe.durable_prefix i positive old
    · have eq : i = s.target + 1 := by dsimp at bounded; omega
      simpa [eq] using next
  | ack n bound forward => exact ⟨safe.durable_prefix, bound, safe.business_bound⟩
  | handoff next => exact ⟨safe.durable_prefix, safe.ack_bound, next⟩
  | stutter => exact safe

inductive Reachable : State → Prop where
  | init : Reachable initial
  | tail {s t} : Reachable s → Step s t → Reachable t

theorem reachable_safe {s : State} (r : Reachable s) : Safe s := by
  induction r with
  | init => exact initial_safe
  | tail _ step ih => exact step_safe ih step

theorem acknowledged_prefix_is_durable {s : State} (r : Reachable s) :
    Prefix s.ack s.received := by
  intro i positive bounded
  exact (reachable_safe r).durable_prefix i positive (Nat.le_trans bounded (reachable_safe r).ack_bound)

theorem handed_off_prefix_is_durable {s : State} (r : Reachable s) :
    Prefix s.business s.received := by
  intro i positive bounded
  exact (reachable_safe r).durable_prefix i positive (Nat.le_trans bounded (reachable_safe r).business_bound)

#print axioms reachable_safe
#print axioms acknowledged_prefix_is_durable
#print axioms handed_off_prefix_is_durable
end TransportPrefix

namespace AdmissionVector

/- Each domain has its own locally APPLIED revision and eligibility. A cached vector
   can admit work only while every component is unchanged. History records
   eligibility at admission, not the permission of an already issued effect.
   This theorem requires local barrier application and admission to serialize.
   It establishes neither current remote authority state nor propagation time. -/
structure State (I : Type) where
  current : I → Nat
  snapshot : I → Nat
  enabled : I → Bool
  prepared : Bool
  history : List (I → Bool)

def initial (I : Type) : State I :=
  ⟨fun _ => 0, fun _ => 0, fun _ => true, false, []⟩

inductive Step {I : Type} [DecidableEq I] : State I → State I → Prop where
  | prepare (s : State I) (fresh : s.prepared = false) (ready : ∀ i, s.enabled i = true) :
      Step s { s with snapshot := s.current, prepared := true }
  | invalidate (s : State I) (i : I) :
      Step s { s with current := fun j => if j = i then s.current j + 1 else s.current j,
                      enabled := fun j => if j = i then false else s.enabled j }
  | issue (s : State I) (prepared : s.prepared = true) (same : s.snapshot = s.current) :
      Step s { s with history := s.enabled :: s.history }
  | stutter (s : State I) : Step s s

structure Safe {I : Type} (s : State I) : Prop where
  bounded : ∀ i, s.snapshot i ≤ s.current i
  current_ready : s.prepared = true → ∀ i, s.snapshot i = s.current i → s.enabled i = true
  history_ready : ∀ h, h ∈ s.history → ∀ i, h i = true

theorem initial_safe (I : Type) : Safe (initial I) := by
  constructor <;> simp [initial]

theorem step_safe {I : Type} [DecidableEq I] {s t : State I}
    (safe : Safe s) (step : Step s t) : Safe t := by
  cases step with
  | prepare fresh ready =>
    refine ⟨?_, ?_, safe.history_ready⟩
    · intro i; exact Nat.le_refl _
    · intro _ i _; exact ready i
  | invalidate i =>
    refine ⟨?_, ?_, safe.history_ready⟩
    · intro j
      have old := safe.bounded j
      dsimp
      split <;> omega
    · intro prepared j same
      by_cases equal : j = i
      · have old := safe.bounded j
        simp [equal] at same old
        omega
      · have previous : s.snapshot j = s.current j := by simpa [equal] using same
        simpa [equal] using safe.current_ready prepared j previous
  | issue prepared same =>
    refine ⟨safe.bounded, safe.current_ready, ?_⟩
    intro h member i
    simp only [List.mem_cons] at member
    rcases member with rfl | old
    · exact safe.current_ready prepared i (congrFun same i)
    · exact safe.history_ready h old i
  | stutter => exact safe

inductive Reachable {I : Type} [DecidableEq I] : State I → Prop where
  | init : Reachable (initial I)
  | tail {s t} : Reachable s → Step s t → Reachable t

theorem reachable_safe {I : Type} [DecidableEq I] {s : State I}
    (r : Reachable s) : Safe s := by
  induction r with
  | init => exact initial_safe _
  | tail _ step ih => exact step_safe ih step

theorem all_admissions_were_eligible {I : Type} [DecidableEq I] {s : State I}
    (r : Reachable s) (h : I → Bool) (member : h ∈ s.history) :
    ∀ i, h i = true := (reachable_safe r).history_ready h member

#print axioms reachable_safe
#print axioms all_admissions_were_eligible
end AdmissionVector
