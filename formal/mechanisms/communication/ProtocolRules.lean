import Std
namespace ProtocolRules

/- Pure installed-extension dispatch rules. Known optional data is validated;
   unknown optional data may be ignored. These booleans are inputs whose
   real-world authenticity is not established by this proof. -/
def acceptExtension (known declarationRequires messageRequires validData : Bool) : Bool :=
  if known then
    if declarationRequires && !messageRequires then false else validData
  else !messageRequires

theorem known_payload_is_valid (d r v : Bool)
    (accepted : acceptExtension true d r v = true) : v = true := by
  cases d <;> cases r <;> cases v <;> simp_all [acceptExtension]

theorem minimum_required_cannot_be_lowered (r v : Bool)
    (accepted : acceptExtension true true r v = true) : r = true := by
  cases r <;> cases v <;> simp_all [acceptExtension]

theorem unknown_required_rejected (d v : Bool) :
    acceptExtension false d true v = false := by rfl

def selectPath (sourceResponse targetRequest gateway constraintRequired pathSupports : Bool) :=
  sourceResponse && targetRequest && gateway && (!constraintRequired || pathSupports)

theorem selected_path_has_both_endpoints (a b g r p : Bool)
    (selected : selectPath a b g r p = true) : a = true ∧ b = true := by
  cases a <;> cases b <;> cases g <;> cases r <;> cases p <;> simp_all [selectPath]

theorem selected_required_path_supports_constraint (a b g p : Bool)
    (selected : selectPath a b g true p = true) : p = true := by
  cases a <;> cases b <;> cases g <;> cases p <;> simp_all [selectPath]

def acceptFallback (typeSupported dataValid requiresInput completeInput textOnly : Bool) :=
  typeSupported && dataValid && (!requiresInput || (completeInput && !textOnly))

theorem required_input_not_lost (t v c text : Bool)
    (accepted : acceptFallback t v true c text = true) : c = true ∧ text = false := by
  cases t <;> cases v <;> cases c <;> cases text <;> simp_all [acceptFallback]

theorem optional_invalid_rejected : acceptExtension true false false false = false := by rfl
theorem supported_message_exists : selectPath true true true true true = true := by rfl
theorem optional_unknown_accepted : acceptExtension false false false false = true := by rfl

/- The same atomic bind-once primitive is used for the complete stream
   binding and the canonical original intent. Authentication/canonicalization
   must happen before entering this primitive and are outside this theorem. -/
def bindOnce {A : Type} [DecidableEq A] (stored : Option A) (incoming : A) : Option A :=
  match stored with
  | none => some incoming
  | some original => if original = incoming then some original else none

theorem accepted_existing_binding_unchanged {A : Type} [DecidableEq A]
    (old incoming returned : A) (accepted : bindOnce (some old) incoming = some returned) :
    returned = old := by
  unfold bindOnce at accepted
  split at accepted <;> simp_all

theorem conflicting_binding_rejected {A : Type} [DecidableEq A]
    (old incoming : A) (different : old ≠ incoming) :
    bindOnce (some old) incoming = none := by simp [bindOnce, different]

theorem original_binding_retry_idempotent {A : Type} [DecidableEq A] (old : A) :
    bindOnce (some old) old = some old := by simp [bindOnce]

#print axioms known_payload_is_valid
#print axioms minimum_required_cannot_be_lowered
#print axioms unknown_required_rejected
#print axioms selected_path_has_both_endpoints
#print axioms selected_required_path_supports_constraint
#print axioms required_input_not_lost
#print axioms optional_invalid_rejected
#print axioms supported_message_exists
#print axioms optional_unknown_accepted
#print axioms accepted_existing_binding_unchanged
#print axioms conflicting_binding_rejected
#print axioms original_binding_retry_idempotent
end ProtocolRules
