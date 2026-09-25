import Std
namespace EvaluationRules
inductive Verdict where
  | pass | fail | unknown
  deriving DecidableEq, Repr
-- Required groups are three-valued: a failure dominates unknown, which dominates pass.
def combine : List Verdict → Verdict
  | [] => .pass
  | .fail :: _ => .fail
  | .pass :: xs => combine xs
  | .unknown :: xs => if combine xs = .fail then .fail else .unknown

theorem pass_iff_all (xs : List Verdict) : combine xs = .pass ↔ ∀ x ∈ xs, x = .pass := by
  induction xs with
  | nil => simp [combine]
  | cons x xs ih =>
    cases x <;> simp_all [combine]
    split <;> simp_all

def successes : List Verdict → Nat
  | [] => 0
  | .pass :: xs => 1 + successes xs
  | _ :: xs => successes xs

theorem successes_bounded (xs : List Verdict) : successes xs ≤ xs.length := by
  induction xs with
  | nil => simp [successes]
  | cons x xs ih => cases x <;> simp_all [successes] <;> omega

theorem unknown_is_not_success (xs : List Verdict) :
    successes (.unknown :: xs) = successes xs ∧ (.unknown :: xs).length = xs.length + 1 := by
  simp [successes]

structure Run (n : Nat) where
  score : Fin n → Option Verdict
  planVersion : Nat
  closing : Bool
  dispatched : Nat
  remaining : Nat
  reported : Bool
  pendingFees : Nat
  knownFees : Nat
  up : Bool

def initial (n version budget : Nat) : Run n :=
  ⟨fun _ => none, version, false, 0, budget, false, 0, 0, true⟩

def writeScore {n} (f : Fin n → Option Verdict) (i : Fin n) (v : Verdict) :=
  fun j => if j = i then some v else f j
inductive Step {n : Nat} : Run n → Run n → Prop where
  | dispatch (s : Run n) (notClosed : s.closing = false) (up : s.up = true)
      (budget : 0 < s.remaining) :
      Step s { s with dispatched := s.dispatched + 1, remaining := s.remaining - 1, pendingFees := s.pendingFees + 1 }
  | close (s : Run n) : Step s { s with closing := true }
  | scoreOnce (s : Run n) (i : Fin n) (v : Verdict) (up : s.up = true)
      (missing : s.score i = none) : Step s { s with score := writeScore s.score i v }
  | report (s : Run n) (closed : s.closing = true) (up : s.up = true) :
      Step s { s with reported := true }
  | settle (s : Run n) (pending : 0 < s.pendingFees) (amount : Nat) :
      Step s { s with pendingFees := s.pendingFees - 1, knownFees := s.knownFees + amount }
  | crash (s : Run n) : Step s { s with up := false }
  | restart (s : Run n) : Step s { s with up := true }
  | duplicateOrConflict (s : Run n) : Step s s
inductive Reachable (n version budget : Nat) : Run n → Prop where
  | init : Reachable n version budget (initial n version budget)
  | next {s t} : Reachable n version budget s → Step s t → Reachable n version budget t

def Inv {n} (version budget : Nat) (s : Run n) :=
  s.planVersion = version ∧ s.dispatched + s.remaining = budget ∧
  s.pendingFees ≤ s.dispatched ∧ (s.reported = true → s.closing = true)

theorem step_inv {n version budget} {s t : Run n} (safe : Inv version budget s)
    (step : Step s t) : Inv version budget t := by
  cases step <;> simp_all [Inv] <;> omega

theorem reachable_inv {n version budget} {s : Run n} (h : Reachable n version budget s) :
    Inv version budget s := by
  induction h with
  | init => simp [Inv, initial]
  | next _ step ih => exact step_inv ih step

-- All n frozen sample positions remain in the total score function, including none/unknown.
theorem frozen_denominator {n : Nat} (_s : Run n) : (List.finRange n).length = n := by simp

theorem original_score_preserved {n} {s t : Run n} (step : Step s t)
    (i : Fin n) (v : Verdict) (saved : s.score i = some v) : t.score i = some v := by
  cases step <;> simp_all [writeScore]
  intro eq
  subst i
  simp_all

theorem closing_monotone {n} {s t : Run n} (step : Step s t)
    (closed : s.closing = true) : t.closing = true := by
  cases step <;> simp_all

theorem old_report_survives_late_cost {n} {s t : Run n} (step : Step s t)
    (reported : s.reported = true) : t.reported = true := by
  cases step <;> simp_all

#print axioms pass_iff_all
#print axioms unknown_is_not_success
#print axioms frozen_denominator
#print axioms successes_bounded
#print axioms reachable_inv
#print axioms original_score_preserved
#print axioms closing_monotone
#print axioms old_report_survives_late_cost
end EvaluationRules
