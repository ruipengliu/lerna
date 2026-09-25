import Std
namespace TrustRules

/- Authentication, current endpoint/owner/ledger association and installed
   message names are trusted inputs. Bootstrap never changes action grants. -/
inductive Message where
  | openRecovery | readRecovery | readChanges | applyRecovery | queryOwnRecovery
  | queryOtherCommand | act | readBody | delegate | confirm
  deriving DecidableEq

def recoveryMessage : Message → Bool
  | .openRecovery | .readRecovery | .readChanges | .applyRecovery | .queryOwnRecovery => true
  | _ => false

def bootstrapAccept (currentBinding hostOnly withinSubset fresh : Bool) (m : Message) :=
  currentBinding && hostOnly && withinSubset && fresh && recoveryMessage m

theorem bootstrap_never_authorizes_action (b h s f : Bool) :
    bootstrapAccept b h s f .act = false := by simp [bootstrapAccept, recoveryMessage]
theorem bootstrap_never_authorizes_body (b h s f : Bool) :
    bootstrapAccept b h s f .readBody = false := by simp [bootstrapAccept, recoveryMessage]
theorem bootstrap_never_authorizes_confirmation (b h s f : Bool) :
    bootstrapAccept b h s f .confirm = false := by simp [bootstrapAccept, recoveryMessage]
theorem bootstrap_never_authorizes_delegation (b h s f : Bool) :
    bootstrapAccept b h s f .delegate = false := by simp [bootstrapAccept, recoveryMessage]
theorem bootstrap_never_authorizes_foreign_query (b h s f : Bool) :
    bootstrapAccept b h s f .queryOtherCommand = false := by simp [bootstrapAccept, recoveryMessage]
theorem bootstrap_requires_current_binding (b h s f : Bool) (m : Message)
    (accepted : bootstrapAccept b h s f m = true) : b = true ∧ h = true ∧ s = true ∧ f = true := by
  simp only [bootstrapAccept, Bool.and_eq_true] at accepted
  exact ⟨accepted.1.1.1.1, accepted.1.1.1.2, accepted.1.1.2, accepted.1.2⟩
theorem bootstrap_query_witness : bootstrapAccept true true true true .readChanges = true := by rfl

/- The interval encloses real time only if the platform's time anchor is
   sound. No theorem here establishes real hardware rollback detection. -/
structure OfflineEvidence where
  signature : Bool
  currentOwnerLedger : Bool
  source : Bool
  taskControl : Bool
  budget : Bool
  localCapability : Bool
  timeAnchor : Bool
  rollbackExcluded : Bool
  knownRevoked : Bool
  lower : Nat
  upper : Nat
  notBefore : Nat
  grantEnd : Nat
  sourceEnd : Nat
  taskEnd : Nat

def offlineAllow (e : OfflineEvidence) : Prop :=
  e.signature = true ∧ e.currentOwnerLedger = true ∧ e.source = true ∧
  e.taskControl = true ∧ e.budget = true ∧ e.localCapability = true ∧
  e.timeAnchor = true ∧ e.rollbackExcluded = true ∧ e.knownRevoked = false ∧
  e.notBefore ≤ e.lower ∧ e.lower ≤ e.upper ∧
  e.upper < min e.grantEnd (min e.sourceEnd e.taskEnd)

theorem offline_interval_is_conservative (e : OfflineEvidence) (t : Nat)
    (allowed : offlineAllow e) (enclosed : e.lower ≤ t ∧ t ≤ e.upper) :
    e.notBefore ≤ t ∧ t < e.grantEnd ∧ t < e.sourceEnd ∧ t < e.taskEnd := by
  rcases allowed with ⟨_,_,_,_,_,_,_,_,_,start,_,finish⟩
  have g : min e.grantEnd (min e.sourceEnd e.taskEnd) ≤ e.grantEnd := Nat.min_le_left _ _
  have s : min e.grantEnd (min e.sourceEnd e.taskEnd) ≤ e.sourceEnd :=
    Nat.le_trans (Nat.min_le_right _ _) (Nat.min_le_left _ _)
  have k : min e.grantEnd (min e.sourceEnd e.taskEnd) ≤ e.taskEnd :=
    Nat.le_trans (Nat.min_le_right _ _) (Nat.min_le_right _ _)
  omega

theorem offline_no_rollback_evidence_blocks (e : OfflineEvidence)
    (missing : e.rollbackExcluded = false) : ¬ offlineAllow e := by
  intro h; rcases h with ⟨_,_,_,_,_,_,_,rollback,_⟩; simp_all
theorem offline_lost_anchor_blocks (e : OfflineEvidence)
    (missing : e.timeAnchor = false) : ¬ offlineAllow e := by
  intro h; rcases h with ⟨_,_,_,_,_,_,anchor,_⟩; simp_all
theorem offline_insufficient_budget_blocks (e : OfflineEvidence)
    (missing : e.budget = false) : ¬ offlineAllow e := by
  intro h; rcases h with ⟨_,_,_,_,budget,_⟩; simp_all
theorem offline_expired_blocks (e : OfflineEvidence)
    (expired : e.grantEnd ≤ e.upper) : ¬ offlineAllow e := by
  intro h; rcases h with ⟨_,_,_,_,_,_,_,_,_,_,_,finish⟩
  have g : min e.grantEnd (min e.sourceEnd e.taskEnd) ≤ e.grantEnd := Nat.min_le_left _ _
  omega

theorem offline_witness : offlineAllow
    ⟨true,true,true,true,true,true,true,true,false,10,12,9,20,18,15⟩ := by
  unfold offlineAllow
  decide

/- B is the ENTIRE confirmation binding (user, confirmation, surface,
   input request, scope/display hashes, host, nonce, deadline, approval).
   Equality is structural equality after trusted normalization; cryptography
   and physical user presence are assumptions of the caller. -/
structure Confirmation (B : Type) where
  shown : B
  consumed : Bool
  issued : List B

def confirmationInit (shown : B) : Confirmation B := ⟨shown,false,[]⟩
def confirm [DecidableEq B] (s : Confirmation B) (proofBinding : B)
    (authentic approved fresh : Bool) : Confirmation B :=
  if !s.consumed && authentic && approved && fresh && decide (proofBinding = s.shown)
  then { s with consumed := true, issued := [s.shown] }
  else s

inductive ConfirmStep [DecidableEq B] : Confirmation B → Confirmation B → Prop where
  | attempt (s : Confirmation B) (p : B) (a yes fresh : Bool) :
      ConfirmStep s (confirm s p a yes fresh)
  | query (s : Confirmation B) : ConfirmStep s s

def ConfirmationSafe (s : Confirmation B) :=
  (s.consumed = false ∧ s.issued = []) ∨ (s.consumed = true ∧ s.issued = [s.shown])

theorem confirmation_step_safe [DecidableEq B] {s t : Confirmation B}
    (safe : ConfirmationSafe s) (step : ConfirmStep s t) : ConfirmationSafe t := by
  cases step with
  | query => exact safe
  | attempt p a yes fresh =>
    unfold confirm
    split
    · exact Or.inr ⟨rfl,rfl⟩
    · exact safe

inductive ConfirmReach [DecidableEq B] (shown : B) : Confirmation B → Prop where
  | init : ConfirmReach shown (confirmationInit shown)
  | tail {s t} : ConfirmReach shown s → ConfirmStep s t → ConfirmReach shown t

theorem confirmation_reachable_safe [DecidableEq B] {shown : B} {s : Confirmation B}
    (r : ConfirmReach shown s) : ConfirmationSafe s := by
  induction r with
  | init => exact Or.inl ⟨rfl,rfl⟩
  | tail _ step ih => exact confirmation_step_safe ih step

theorem confirmation_at_most_once [DecidableEq B] {shown : B} {s : Confirmation B}
    (r : ConfirmReach shown s) : s.issued.length ≤ 1 := by
  rcases confirmation_reachable_safe r with ⟨_,empty⟩ | ⟨_,single⟩
  · simp [empty]
  · simp [single]

theorem confirmation_binding_fixed [DecidableEq B] {shown : B} {s : Confirmation B}
    (r : ConfirmReach shown s) : s.shown = shown := by
  induction r with
  | init => rfl
  | tail _ step ih =>
    cases step with
    | query => exact ih
    | attempt p a yes fresh =>
      unfold confirm
      split <;> exact ih

theorem changed_confirmation_binding_cannot_issue [DecidableEq B]
    (s : Confirmation B) (p : B) (a yes fresh : Bool) (changed : p ≠ s.shown) :
    confirm s p a yes fresh = s := by simp [confirm, changed]
theorem consumed_confirmation_cannot_reissue [DecidableEq B]
    (s : Confirmation B) (p : B) (a yes fresh : Bool) (used : s.consumed = true) :
    confirm s p a yes fresh = s := by simp [confirm, used]
theorem rejected_confirmation_cannot_issue [DecidableEq B]
    (s : Confirmation B) (p : B) (a fresh : Bool) :
    confirm s p a false fresh = s := by simp [confirm]
theorem confirmation_witness :
    (confirm (confirmationInit (7 : Nat)) 7 true true true).issued = [7] := by decide

#print axioms bootstrap_requires_current_binding
#print axioms bootstrap_never_authorizes_action
#print axioms bootstrap_never_authorizes_body
#print axioms bootstrap_never_authorizes_confirmation
#print axioms bootstrap_never_authorizes_delegation
#print axioms bootstrap_never_authorizes_foreign_query
#print axioms bootstrap_query_witness
#print axioms offline_interval_is_conservative
#print axioms offline_no_rollback_evidence_blocks
#print axioms offline_lost_anchor_blocks
#print axioms offline_insufficient_budget_blocks
#print axioms offline_expired_blocks
#print axioms offline_witness
#print axioms confirmation_reachable_safe
#print axioms confirmation_at_most_once
#print axioms confirmation_binding_fixed
#print axioms changed_confirmation_binding_cannot_issue
#print axioms consumed_confirmation_cannot_reissue
#print axioms rejected_confirmation_cannot_issue
#print axioms confirmation_witness
end TrustRules
