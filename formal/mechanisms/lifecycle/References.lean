import Std

/- General reference identities, unbounded management revisions and executions.
   Each constructor is one local commit; read/decide/register remain separate.
   Storage truth and the completeness of real-world holder discovery are premises
   of the refinement, not conclusions of this proof. -/
namespace ReferenceLifecycle
variable {Id : Type} [DecidableEq Id]
def put (f : Id → Bool) (i : Id) (v : Bool) : Id → Bool := fun j => if j = i then v else f j
structure State (Id : Type) where
  doneEvidence : Id → Bool
  held : Id → Bool
  running : Id → Bool
  accepted : Id → Bool
  finished : Id → Bool
  sealed : Id → Bool
  used : Id → Bool
  readAt : Id → Option Nat
  decided : Id → Bool
  gate : Bool
  revision : Nat
  disabled : Bool
  plan : Bool
  deleted : Bool
  up : Bool

def initial : State Id :=
  ⟨fun _ => false, fun _ => false, fun _ => false, fun _ => false, fun _ => false,
   fun _ => false, fun _ => false, fun _ => none, fun _ => false,
   true, 0, false, false, false, true⟩
def Inv (s : State Id) : Prop :=
  (∀ i, s.running i = true → s.held i = true) ∧
  (∀ i, s.running i = true → s.accepted i = true) ∧
  (∀ i, s.finished i = true → s.running i = false) ∧
  (∀ i, s.sealed i = true → s.accepted i = false) ∧
  (s.plan = true → s.gate = false ∧ ∀ i, s.held i = false) ∧
  (s.deleted = true → s.plan = true) ∧
  (∀ i, s.finished i = true → s.doneEvidence i = true)
inductive Step : State Id → State Id → Prop where
  | read (s : State Id) (i : Id) (h : s.up = true) (g : s.gate = true) :
    Step s { s with readAt := fun j => if j = i then some s.revision else s.readAt j }
  | decide (s : State Id) (i : Id) (h : s.up = true) (r : s.readAt i = some s.revision) :
    Step s { s with decided := put s.decided i true }
  | register (s : State Id) (i : Id) (h : s.up = true) (g : s.gate = true)
      (d : s.decided i = true) (r : s.readAt i = some s.revision) :
    Step s { s with held := put s.held i true }
  | use (s : State Id) (i : Id) (h : s.up = true) (d : s.disabled = false)
      (pin : s.held i = true) (f : s.finished i = false) (z : s.sealed i = false) :
    Step s { s with running := put s.running i true, accepted := put s.accepted i true, used := put s.used i true }
  | recordDone (s : State Id) (i : Id) (r : s.running i = true) :
    Step s { s with doneEvidence := put s.doneEvidence i true }
  | finish (s : State Id) (i : Id) (h : s.up = true) (r : s.running i = true)
      (evidence : s.doneEvidence i = true) :
    Step s { s with running := put s.running i false, finished := put s.finished i true }
  | sealRef (s : State Id) (i : Id) (h : s.up = true) (a : s.accepted i = false) :
    Step s { s with sealed := put s.sealed i true }
  | releaseDone (s : State Id) (i : Id) (h : s.up = true) (f : s.finished i = true) :
    Step s { s with held := put s.held i false }
  | releaseSealed (s : State Id) (i : Id) (h : s.up = true) (z : s.sealed i = true) :
    Step s { s with held := put s.held i false }
  | close (s : State Id) (h : s.up = true) :
    Step s { s with gate := false, revision := s.revision + 1 }
  | withdraw (s : State Id) (h : s.up = true) :
    Step s { s with disabled := true }
  | planReclaim (s : State Id) (h : s.up = true) (closed : s.gate = false)
      (empty : ∀ i, s.held i = false) :
    Step s { s with plan := true }
  | delete (s : State Id) (h : s.up = true) (planned : s.plan = true) :
    Step s { s with deleted := true }
  | crash (s : State Id) : Step s { s with up := false, readAt := fun _ => none, decided := fun _ => false }
  | restart (s : State Id) : Step s { s with up := true }
inductive Reachable : State Id → Prop where
  | init : Reachable initial
  | next {s t} : Reachable s → Step s t → Reachable t

omit [DecidableEq Id] in
theorem initial_safe : Inv (initial : State Id) := by simp [Inv, initial]

theorem step_safe {s t : State Id} (safe : Inv s) (step : Step s t) : Inv t := by
  rcases safe with ⟨pin, accepted, finished, sealed, plan, deleted, terminal⟩
  cases step <;> simp only [Inv, put]
  all_goals simp_all
  all_goals try constructor
  all_goals intro j hj heq
  all_goals subst j
  all_goals try simp_all
  all_goals have ha := accepted _ hj
  all_goals have hz := sealed _ (by assumption)
  all_goals simp_all

theorem reachable_safe {s : State Id} (h : Reachable s) : Inv s := by
  induction h with
  | init => exact initial_safe
  | next _ step ih => exact step_safe ih step

theorem no_use_without_retention {s : State Id} (h : Reachable s) (i : Id)
    (running : s.running i = true) : s.held i = true := (reachable_safe h).1 i running

theorem reclamation_has_no_holders {s : State Id} (h : Reachable s)
    (deleted : s.deleted = true) : ∀ i, s.held i = false := by
  have inv := reachable_safe h
  exact (inv.2.2.2.2.1 (inv.2.2.2.2.2.1 deleted)).2

theorem use_history_monotone {s t : State Id} (h : Step s t) (i : Id)
    (used : s.used i = true) : t.used i = true := by
  cases h <;> simp_all [put]

#print axioms reachable_safe
#print axioms no_use_without_retention
#print axioms reclamation_has_no_holders
#print axioms use_history_monotone
end ReferenceLifecycle
