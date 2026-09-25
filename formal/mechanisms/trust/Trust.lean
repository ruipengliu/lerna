import Std

/- General safety lemmas for the trust mechanisms. This is an independent
formalization, not a machine-checked refinement of the three TLA+ modules.
Boolean authentication/permission observations and durable atomic transitions
are explicit interfaces; no cryptography or physical deletion is proved. -/
namespace Trust

structure ScopeState (Right : Type) where
  rights : Right → Bool
  revoked : Bool

inductive ScopeStep {Right : Type} : ScopeState Right → ScopeState Right → Prop where
  | delegate (s : ScopeState Right) (child : Right → Bool)
      (narrow : ∀ r, child r = true → s.rights r = true)
      (active : s.revoked = false) : ScopeStep s ⟨child, false⟩
  | revoke (s : ScopeState Right) : ScopeStep s {s with revoked := true}
  | stutter (s : ScopeState Right) : ScopeStep s s

inductive ScopeReach {Right : Type} (root : Right → Bool) : ScopeState Right → Prop where
  | init : ScopeReach root ⟨root, false⟩
  | next {s t} : ScopeReach root s → ScopeStep s t → ScopeReach root t

def Attenuates {Right : Type} (root : Right → Bool) (s : ScopeState Right) :=
  ∀ r, s.rights r = true → root r = true

theorem scope_step_preserves {Right : Type} {root : Right → Bool} {s t}
    (safe : Attenuates root s) (step : ScopeStep s t) : Attenuates root t := by
  cases step with
  | delegate child narrow active => exact fun r h => safe r (narrow r h)
  | revoke => exact safe
  | stutter => exact safe

theorem scope_reachable_attenuates {Right : Type} {root : Right → Bool} {s}
    (h : ScopeReach root s) : Attenuates root s := by
  induction h with
  | init => exact fun _ h => h
  | next _ step ih => exact scope_step_preserves ih step

structure UseEvent where
  epoch : Nat
  cacheEpoch : Nat
  revoked : Bool
  closed : Bool
  owner : Bool
  granted : Bool
  deriving DecidableEq, Repr

structure GateState where
  epoch : Nat
  revoked : Bool
  closed : Bool
  cacheEpoch : Nat
  cacheAllow : Bool
  cacheOwner : Bool
  hasCache : Bool
  events : List UseEvent
  facts : Nat
  deriving DecidableEq, Repr

def initial : GateState := ⟨0, false, false, 0, false, false, false, [], 0⟩
def eligible (s : GateState) : Bool :=
  s.hasCache && s.cacheAllow && s.cacheOwner && !s.revoked && !s.closed && (s.cacheEpoch == s.epoch)
def observation (s : GateState) : UseEvent :=
  ⟨s.epoch, s.cacheEpoch, s.revoked, s.closed, s.cacheOwner, s.cacheAllow⟩
def consume (s : GateState) : GateState :=
  if eligible s then {s with events := observation s :: s.events} else s

def EventGood (e : UseEvent) : Prop :=
  e.epoch = e.cacheEpoch ∧ e.revoked = false ∧ e.closed = false ∧ e.owner = true ∧ e.granted = true
def GateSafe (s : GateState) : Prop := ∀ e ∈ s.events, EventGood e

inductive GateStep : GateState → GateState → Prop where
  | evaluate (s : GateState) (allow owner : Bool) :
      GateStep s {s with cacheEpoch := s.epoch, cacheAllow := (allow && !s.revoked && !s.closed), cacheOwner := owner, hasCache := true}
  | revise (s : GateState) : GateStep s {s with epoch := s.epoch + 1}
  | revoke (s : GateState) : GateStep s {s with revoked := true}
  | close (s : GateState) : GateStep s {s with closed := true}
  | use (s : GateState) : GateStep s (consume s)
  | fact (s : GateState) : GateStep s {s with facts := s.facts + 1}
  | crash (s : GateState) : GateStep s {s with hasCache := false}
  | stutter (s : GateState) : GateStep s s

inductive GateReach : GateState → Prop where
  | init : GateReach initial
  | next {s t} : GateReach s → GateStep s t → GateReach t

theorem eligible_event_good (s : GateState) (h : eligible s = true) : EventGood (observation s) := by
  simp [eligible] at h
  simp [EventGood, observation]
  exact ⟨h.2.symm, h.1.1.2, h.1.2, h.1.1.1.2, h.1.1.1.1.2⟩

theorem consume_preserves {s : GateState} (safe : GateSafe s) : GateSafe (consume s) := by
  unfold consume
  split
  next h =>
    intro e he
    simp only [List.mem_cons] at he
    rcases he with rfl | he
    · exact eligible_event_good s h
    · exact safe e he
  next => exact safe

theorem gate_step_preserves {s t} (safe : GateSafe s) (step : GateStep s t) : GateSafe t := by
  cases step with
  | use => exact consume_preserves safe
  | evaluate => exact safe
  | revise => exact safe
  | revoke => exact safe
  | close => exact safe
  | fact => exact safe
  | crash => exact safe
  | stutter => exact safe

theorem gate_reachable_safe {s} (h : GateReach s) : GateSafe s := by
  induction h with
  | init => simp [GateSafe, initial]
  | next _ step ih => exact gate_step_preserves ih step

theorem revoked_cannot_add_use (s : GateState) (h : s.revoked = true) : consume s = s := by
  simp [consume, eligible, h]
theorem closed_cannot_add_use (s : GateState) (h : s.closed = true) : consume s = s := by
  simp [consume, eligible, h]
theorem stale_cache_cannot_add_use (s : GateState) (h : s.cacheEpoch ≠ s.epoch) : consume s = s := by
  simp [consume, eligible, h]

/- Historical facts are deliberately independent of new-action eligibility. -/
def cached : GateState := {initial with cacheAllow := true, cacheOwner := true, hasCache := true}
def revoked : GateState := {cached with revoked := true}
def afterFact : GateState := {revoked with facts := 1}
theorem late_fact_without_new_use_witness :
    GateReach afterFact ∧ afterFact.facts = 1 ∧ consume afterFact = afterFact := by
  have h1 : GateReach cached := .next .init (.evaluate initial true true)
  have h2 : GateReach revoked := .next h1 (.revoke cached)
  have h3 : GateReach afterFact := .next h2 (.fact revoked)
  exact ⟨h3, rfl, revoked_cannot_add_use afterFact rfl⟩

/- All dependencies must remain applicable; dropping one invalid dependency
is not a permitted way to reuse an already-derived artifact. -/
def AllOpen {Source : Type} (deps closed : Source → Prop) : Prop := ∀ x, deps x → ¬ closed x

theorem closed_dependency_blocks {Source : Type} {deps closed : Source → Prop}
    {x : Source} (needed : deps x) (invalid : closed x) : ¬ AllOpen deps closed := by
  intro h
  exact h x needed invalid

#print axioms scope_reachable_attenuates
#print axioms gate_reachable_safe
#print axioms revoked_cannot_add_use
#print axioms closed_cannot_add_use
#print axioms stale_cache_cannot_add_use
#print axioms late_fact_without_new_use_witness
#print axioms closed_dependency_blocks
end Trust
