import Std

namespace ControlMechanisms

/- One allocation is the account component of Budget.tla. Natural-number
   budgets and cumulative reports are unbounded; there is no fixed trace bound.
   The TLC tree-composition experiment is deliberately not claimed as a Lean
   proof of arbitrary delegation graphs. -/
structure Ledger where
  free : Nat
  held : Nat
  spent : Nat
  allocation : Nat
  used : Nat
  revision : Nat
  creates : Nat
  active : Bool
  finalized : Bool
  sealed : Bool
  done : Bool
  conflict : Bool
  cancelled : Bool
  cleanupFree : Nat
  cleanupSpent : Nat
  deriving DecidableEq, Repr

def initialLedger (total cleanup : Nat) : Ledger :=
  ⟨total, 0, 0, 0, 0, 0, 0, false, false, false, false, false, false, cleanup, 0⟩

inductive BudgetStep : Ledger → Ledger → Prop where
  | reserve (s : Ledger) (a : Nat) (inactive : s.active = false)
      (allowed : s.cancelled = false) (positive : 0 < a) (room : a ≤ s.free) :
      BudgetStep s { s with free := s.free - a, held := a, allocation := a, creates := s.creates + 1, active := true }
  | report (s : Ledger) (r u : Nat) (active : s.active = true)
      (unfinalized : s.finalized = false) (consistent : s.conflict = false)
      (newer : s.revision < r) (monotone : s.used ≤ u) (limit : u ≤ s.allocation) :
      BudgetStep s { s with revision := r, used := u, spent := s.spent + (u - s.used), held := s.allocation - u }
  | duplicate (s : Ledger) : BudgetStep s s
  | stale (s : Ledger) : BudgetStep s s
  | conflict (s : Ledger) : BudgetStep s { s with conflict := true }
  | done (s : Ledger) (active : s.active = true) : BudgetStep s { s with done := true }
  | close_sources (s : Ledger) (active : s.active = true) (doneProof : s.done = true) :
      BudgetStep s { s with sealed := true }
  | finalize (s : Ledger) (active : s.active = true) (unfinalized : s.finalized = false)
      (sealed : s.sealed = true) (consistent : s.conflict = false) :
      BudgetStep s { s with free := s.free + s.held, held := 0, finalized := true }
  | cancel (s : Ledger) : BudgetStep s { s with cancelled := true }
  | cleanup (s : Ledger) (room : 0 < s.cleanupFree) :
      BudgetStep s { s with cleanupFree := s.cleanupFree - 1, cleanupSpent := s.cleanupSpent + 1 }

structure BudgetSafety (total cleanup : Nat) (s : Ledger) : Prop where
  conservation : s.free + s.held + s.spent = total
  separate_cleanup : s.cleanupFree + s.cleanupSpent = cleanup
  exact_cumulative : s.spent = s.used
  cumulative_bound : s.used ≤ s.allocation
  unknown_reserved : s.active = true → s.finalized = false → s.held + s.used = s.allocation
  inactive_zero : s.active = false → s.allocation = 0 ∧ s.used = 0 ∧ s.held = 0
  final_sealed : s.finalized = true → s.sealed = true ∧ s.active = true
  create_count : s.creates = if s.active then 1 else 0

theorem initial_budget_safe (total cleanup : Nat) :
    BudgetSafety total cleanup (initialLedger total cleanup) := by
  constructor <;> simp [initialLedger]

theorem budget_step_preserves {total cleanup : Nat} {s t : Ledger}
    (safe : BudgetSafety total cleanup s) (step : BudgetStep s t) :
    BudgetSafety total cleanup t := by
  rcases safe with ⟨conservation, separate, exact, bound, unknown, inactive, final, count⟩
  cases step <;> constructor <;> simp_all <;> omega

inductive BudgetReachable (total cleanup : Nat) : Ledger → Prop where
  | init : BudgetReachable total cleanup (initialLedger total cleanup)
  | step {s t} : BudgetReachable total cleanup s → BudgetStep s t → BudgetReachable total cleanup t

theorem reachable_budget_safe {total cleanup : Nat} {s : Ledger}
    (h : BudgetReachable total cleanup s) : BudgetSafety total cleanup s := by
  induction h with
  | init => exact initial_budget_safe _ _
  | step _ transition ih => exact budget_step_preserves ih transition

theorem cumulative_charged_once {total cleanup : Nat} {s : Ledger}
    (h : BudgetReachable total cleanup s) : s.spent = s.used :=
  (reachable_budget_safe h).exact_cumulative

theorem unknown_allocation_not_released {total cleanup : Nat} {s : Ledger}
    (h : BudgetReachable total cleanup s) (active : s.active = true)
    (unfinished : s.finalized = false) : s.held + s.used = s.allocation :=
  (reachable_budget_safe h).unknown_reserved active unfinished

theorem cancellation_does_not_create_allocation {s t : Ledger}
    (h : BudgetStep s t) (cancelled : s.cancelled = true) : t.creates = s.creates := by
  cases h <;> simp_all

theorem cumulative_repeat_has_zero_delta (s : Ledger) :
    s.spent + (s.used - s.used) = s.spent := by omega

/- A fresh child share is transferred from existing free budget, not minted.
   This two-account lemma has arbitrary natural parameters; conservation of
   an arbitrary tree still needs a composition argument beyond this lemma. -/
theorem share_transfer_preserves_total (parent child amount held spent : Nat)
    (available : amount ≤ parent) :
    (parent - amount) + (child + amount) + held + spent = parent + child + held + spent := by
  omega

inductive Tx where | none | pending | committed | aborted
  deriving DecidableEq, Repr
inductive Knowledge where | none | unknown | committed | rejected
  deriving DecidableEq, Repr
structure Core where
  cancelled : Bool
  cancelEarly : Bool
  epoch : Nat
  requestEpoch : Nat
  tx : Tx
  known : Knowledge
  prepared : Bool
  sent : Bool
  deriving DecidableEq, Repr
def initialCore : Core := ⟨false, false, 0, 0, .none, .none, false, false⟩

inductive CoreStep : Core → Core → Prop where
  | request (s : Core) (token : Nat) (fresh : s.tx = .none)
      (allowed : s.cancelled = false) (current : token = s.epoch) :
      CoreStep s { s with tx := .pending, known := .unknown, requestEpoch := token }
  | commit (s : Core) (pending : s.tx = .pending) (allowed : s.cancelled = false)
      (current : s.requestEpoch = s.epoch) :
      CoreStep s { s with tx := .committed, prepared := true }
  | abort (s : Core) (pending : s.tx = .pending)
      (obsolete : s.cancelled = true ∨ s.requestEpoch ≠ s.epoch) :
      CoreStep s { s with tx := .aborted }
  | lookup_unseen (s : Core) : CoreStep s s
  | read_commit (s : Core) (committed : s.tx = .committed) (unknown : s.known = .unknown) :
      CoreStep s { s with known := .committed }
  | read_reject (s : Core) (aborted : s.tx = .aborted) (unknown : s.known = .unknown) :
      CoreStep s { s with known := .rejected }
  | cancel (s : Core) (fresh : s.cancelled = false) :
      CoreStep s { s with cancelled := true, cancelEarly := !s.prepared }
  | expire (s : Core) : CoreStep s { s with epoch := s.epoch + 1 }
  | send (s : Core) (committed : s.known = .committed) (allowed : s.cancelled = false) :
      CoreStep s { s with sent := true }

structure CoreSafety (s : Core) : Prop where
  prepared_commit : s.prepared = true ↔ s.tx = .committed
  commit_knowledge : s.known = .committed → s.prepared = true
  rejection_knowledge : s.known = .rejected → s.tx = .aborted
  sent_known : s.sent = true → s.known = .committed ∧ s.prepared = true
  early_cancel : s.cancelEarly = true → s.cancelled = true ∧ s.prepared = false

theorem initial_core_safe : CoreSafety initialCore := by
  constructor <;> simp [initialCore]

theorem core_step_preserves {s t : Core} (safe : CoreSafety s) (step : CoreStep s t) :
    CoreSafety t := by
  rcases safe with ⟨prepared, known, rejected, sent, early⟩
  cases step <;> constructor <;> simp_all

inductive CoreReachable : Core → Prop where
  | init : CoreReachable initialCore
  | step {s t} : CoreReachable s → CoreStep s t → CoreReachable t

theorem reachable_core_safe {s : Core} (h : CoreReachable s) : CoreSafety s := by
  induction h with
  | init => exact initial_core_safe
  | step _ transition ih => exact core_step_preserves ih transition

theorem cancel_blocks_new_prepare {s t : Core} (step : CoreStep s t)
    (cancelled : s.cancelled = true) : t.prepared = s.prepared := by
  cases step <;> simp_all

theorem stale_lease_blocks_new_prepare {s t : Core} (step : CoreStep s t)
    (stale : s.requestEpoch ≠ s.epoch) : t.prepared = s.prepared := by
  cases step <;> simp_all

theorem submission_unknown_never_dispatched {s : Core} (h : CoreReachable s)
    (unknown : s.known = .unknown) : s.sent = false := by
  have known := (reachable_core_safe h).sent_known
  cases hs : s.sent <;> simp_all

theorem cancellation_is_monotone {s t : Core} (step : CoreStep s t)
    (cancelled : s.cancelled = true) : t.cancelled = true := by
  cases step <;> simp_all

#print axioms budget_step_preserves
#print axioms reachable_budget_safe
#print axioms cumulative_charged_once
#print axioms unknown_allocation_not_released
#print axioms cancellation_does_not_create_allocation
#print axioms cumulative_repeat_has_zero_delta
#print axioms share_transfer_preserves_total
#print axioms core_step_preserves
#print axioms reachable_core_safe
#print axioms cancel_blocks_new_prepare
#print axioms stale_lease_blocks_new_prepare
#print axioms submission_unknown_never_dispatched
#print axioms cancellation_is_monotone

end ControlMechanisms
