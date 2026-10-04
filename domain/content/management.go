package content

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/ruipengliu/lerna/contract"
	v "github.com/ruipengliu/lerna/contract/v1_2"
	"github.com/ruipengliu/lerna/runtime"
	"time"
)

type ManagementConflict struct{}

func (*ManagementConflict) Error() string { return "Content policy revision conflict" }

type PolicyChange struct {
	Key            string         `json:"key"`
	Policy         FixturePolicy  `json:"policy"`
	Previous       *FixturePolicy `json:"previous,omitempty"`
	Watermark      int64          `json:"watermark"`
	Cursor         string         `json:"cursor"`
	Deadline       time.Time      `json:"deadline"`
	Due            time.Time      `json:"due"`
	ExpiryDue      time.Time      `json:"expiry_due"`
	ExpiryDeadline time.Time      `json:"expiry_deadline"`
	State          string         `json:"state"`
	WorkRevision   int64          `json:"work_revision"`
	Reason         string         `json:"reason,omitempty"`
}
type CleanupResponsibility struct {
	ChangeKey     string           `json:"change_key"`
	Ref           v.ContentRef     `json:"content_ref"`
	Subject       v.SubjectBinding `json:"subject"`
	Purpose       string           `json:"purpose"`
	Actions       []string         `json:"actions"`
	Deadline      time.Time        `json:"deadline"`
	BodyCleanup   string           `json:"body_cleanup"`
	StagingHolder bool             `json:"staging_holder"`
	ObjectHolder  bool             `json:"object_holder"`
	AttemptKey    string           `json:"attempt_key"`
	Publication   string           `json:"publication"`
	Residual      string           `json:"residual"`
	Reason        string           `json:"reason,omitempty"`
}
type PropagationObservation struct {
	Change           PolicyChange            `json:"change"`
	Responsibilities []CleanupResponsibility `json:"responsibilities"`
	NextCursor       string                  `json:"next_cursor"`
}
type LegacySourcePage struct {
	Record       Record
	PolicyCursor string
}
type ManagementRepository interface {
	Repository
	LockPolicy(context.Context, runtime.Tx, FixturePolicy) (*FixturePolicy, error)
	SavePolicy(context.Context, runtime.Tx, FixturePolicy) error
	PoliciesForVersion(context.Context, runtime.Tx, v.ContentRef, string, int) ([]FixturePolicy, string, error)
	LockSourceIndex(context.Context, runtime.Tx) (int64, error)
	UnindexedVersions(context.Context, runtime.Tx, int) ([]LegacySourcePage, error)
	SaveBackfillProgress(context.Context, runtime.Tx, v.ContentRef, string, bool) error
	SaveSources(context.Context, runtime.Tx, v.ContentRef, []v.ContentRef) error
	SaveChange(context.Context, runtime.Tx, PolicyChange) error
	ReadChange(context.Context, runtime.Tx, string) (*PolicyChange, error)
	PendingChanges(context.Context, runtime.Tx, string) ([]PolicyChange, error)
	Descendants(context.Context, runtime.Tx, v.ContentRef, int64, string, int) ([]v.ContentRef, string, error)
	SaveResponsibility(context.Context, runtime.Tx, CleanupResponsibility) error
	Responsibilities(context.Context, runtime.Tx, string, string, int) ([]CleanupResponsibility, string, error)
	NextPolicyWork(context.Context, runtime.Tx, string) (int64, error)
	NextPolicyDue(context.Context, runtime.Tx, string) (time.Time, error)
}
type ManagementConfig struct {
	Owner          v.OwnerRef
	Store          ManagementRepository
	TrustedSubject v.SubjectBinding
	TrustedUntil   time.Time
	PageSize       int
	WorkBudget     time.Duration
}
type Manager struct{ config ManagementConfig }

func NewManager(config ManagementConfig) (*Manager, error) {
	if _, err := v.Encode(config.Owner); err != nil {
		return nil, err
	}
	if _, ok := trustedSubject(&config.TrustedSubject, config.Owner); !ok || config.TrustedUntil.IsZero() || config.Store == nil || config.PageSize < 1 || config.PageSize > 64 || config.WorkBudget <= 0 || config.WorkBudget > 24*time.Hour {
		return nil, errors.New("explicit finite trusted Content management capability required")
	}
	return &Manager{config: config}, nil
}
func (m *Manager) authorize(ctx context.Context, subject *v.SubjectBinding) error {
	principal, ok := trustedSubject(subject, m.config.Owner)
	if !ok {
		return refusal("forbidden")
	}
	a, _ := v.Encode(principal)
	b, _ := v.Encode(m.config.TrustedSubject)
	if string(a) != string(b) {
		return refusal("forbidden")
	}
	if !finite(ctx) {
		return ErrUnavailable
	}
	return nil
}
func (m *Manager) current(ctx context.Context, tx runtime.Tx) error {
	now, err := m.config.Store.Now(ctx, tx)
	if err != nil {
		return err
	}
	if !now.Before(m.config.TrustedUntil) {
		return refusal("forbidden")
	}
	return nil
}
func changeKey(policy FixturePolicy) string {
	raw, _ := json.Marshal(struct {
		Ref      v.ContentRef
		Subject  v.SubjectBinding
		Purpose  string
		Revision int64
	}{policy.Ref, policy.Subject, policy.Purpose, policy.Revision})
	sum := sha256.Sum256(raw)
	return "pc-" + hex.EncodeToString(sum[:])
}
func samePolicy(a, b FixturePolicy) bool {
	aa, _ := json.Marshal(a)
	bb, _ := json.Marshal(b)
	return string(aa) == string(bb)
}

// RebuildSources advances at most one finite page of legacy records. The old
// writer must be stopped before this upgrade; no mixed-writer guarantee exists.
func (m *Manager) RebuildSources(ctx context.Context, subject *v.SubjectBinding) (bool, error) {
	if err := m.authorize(ctx, subject); err != nil {
		return false, err
	}
	done := false
	err := m.config.Store.Within(ctx, owner(m.config.Owner), func(ctx context.Context, tx runtime.Tx) error {
		if err := m.current(ctx, tx); err != nil {
			return err
		}
		watermark, err := m.config.Store.LockSourceIndex(ctx, tx)
		if err != nil {
			return err
		}
		records, err := m.config.Store.UnindexedVersions(ctx, tx, m.config.PageSize)
		if err != nil {
			return err
		}
		service := &Service{config: Config{Owner: m.config.Owner, Store: m.config.Store}}
		complete := true
		for _, page := range records {
			record := page.Record
			refs, err := service.registeredClosure(ctx, tx, record.Ref, record.Sources, record.Subject, record.Purpose, nil)
			if err != nil {
				return err
			}
			if err = m.config.Store.SaveSources(ctx, tx, record.Ref, refs); err != nil {
				return err
			}
			policies, next, err := m.config.Store.PoliciesForVersion(ctx, tx, record.Ref, page.PolicyCursor, m.config.PageSize)
			if err != nil {
				return err
			}
			for _, policy := range policies {
				if err = m.restoreLegacyMaintenance(ctx, tx, record, policy, watermark); err != nil {
					return err
				}
			}
			if err = m.config.Store.SaveBackfillProgress(ctx, tx, record.Ref, next, next == ""); err != nil {
				return err
			}
			complete = complete && next == ""
		}
		done = complete && len(records) < m.config.PageSize
		return m.current(ctx, tx)
	})
	return done, err
}

// Backfill fixes the first durable maintenance identity without extending any
// accepted cap. Subsequent pages/reopens observe that identity, never reset it.
func (m *Manager) restoreLegacyMaintenance(ctx context.Context, tx runtime.Tx, record Record, policy FixturePolicy, watermark int64) error {
	key := changeKey(policy)
	existing, err := m.config.Store.ReadChange(ctx, tx, key)
	if err != nil || existing != nil {
		return err
	}
	now, err := m.config.Store.Now(ctx, tx)
	if err != nil {
		return err
	}
	due := earlier(policy.ValidUntil, policy.RetainUntil)
	a, _ := v.Encode(record.Subject)
	b, _ := v.Encode(policy.Subject)
	if string(a) == string(b) && record.Purpose == policy.Purpose {
		due = earlier(due, cutoff(record.CurrentRetainUntil))
	}
	change := PolicyChange{Key: key, Policy: policy, Watermark: watermark, Due: due, Deadline: due.Add(m.config.WorkBudget), ExpiryDue: due, ExpiryDeadline: due.Add(m.config.WorkBudget), State: "scheduled"}
	actions, cleanup := affected(change, record.Ref, now)
	if len(actions) > 0 || cleanup {
		// An already invalid saving basis requires immediate registration, even
		// when the original expiry-processing deadline is already past.
		if now.Before(due) {
			change.Due = now
			change.Deadline = now.Add(m.config.WorkBudget)
		}
		change.State = "pending"
		if !now.Before(change.Deadline) {
			change.State = "residual"
			change.Reason = "original_deadline_expired"
		}
		if err = m.register(ctx, tx, change, record, record.Ref, now); err != nil {
			return err
		}
	}
	change.WorkRevision, err = m.config.Store.NextPolicyWork(ctx, tx, record.ObjectID)
	if err != nil {
		return err
	}
	if err = m.config.Store.SaveChange(ctx, tx, change); err != nil {
		return err
	}
	if change.State == "residual" {
		return nil
	}
	_, err = m.config.Store.Trigger(ctx, tx, contract.ObjectRef{TenantID: contract.ID(record.Ref.Owner.TenantID), OwnerID: contract.ID(record.Ref.Owner.OwnerID), Kind: "content", ID: contract.ID(record.ObjectID)}, "policy_propagation", change.WorkRevision, change.Due)
	return err
}
func (m *Manager) InstallPolicy(ctx context.Context, subject *v.SubjectBinding, policy FixturePolicy, expected int64) (PolicyChange, error) {
	return m.install(ctx, subject, policy, expected, false)
}

// InstallFixturePolicy retains malformed full-ref negative fixtures at the
// explicit trusted fixture seam, with the same durable management transaction.
func (m *Manager) InstallFixturePolicy(ctx context.Context, subject *v.SubjectBinding, policy FixturePolicy, expected int64) (PolicyChange, error) {
	return m.install(ctx, subject, policy, expected, true)
}
func (m *Manager) install(ctx context.Context, subject *v.SubjectBinding, policy FixturePolicy, expected int64, legacy bool) (PolicyChange, error) {
	var result PolicyChange
	if err := m.authorize(ctx, subject); err != nil {
		return result, err
	}
	if _, err := v.Encode(policy.Ref); err != nil {
		return result, err
	}
	if _, ok := trustedSubject(&policy.Subject, m.config.Owner); !ok || policy.Ref.Owner != m.config.Owner || policy.Revision != expected+1 || expected < 0 || policy.ValidUntil.IsZero() || policy.RetainUntil.IsZero() || policy.Purpose == "" {
		return result, refusal("forbidden")
	}
	// Each backfill page has its own short transaction and durable progress.
	for {
		done, err := m.RebuildSources(ctx, subject)
		if err != nil {
			return result, err
		}
		if done {
			break
		}
	}
	err := m.config.Store.Within(ctx, owner(m.config.Owner), func(ctx context.Context, tx runtime.Tx) error {
		if err := m.current(ctx, tx); err != nil {
			return err
		}
		watermark, err := m.config.Store.LockSourceIndex(ctx, tx)
		if err != nil {
			return err
		}
		old, err := m.config.Store.LockPolicy(ctx, tx, policy)
		if err != nil {
			return err
		}
		key := changeKey(policy)
		prior, err := m.config.Store.ReadChange(ctx, tx, key)
		if err != nil {
			return err
		}
		if prior != nil {
			if !samePolicy(prior.Policy, policy) {
				return &ManagementConflict{}
			}
			result = *prior
			return m.current(ctx, tx)
		}
		revision := int64(0)
		if old != nil {
			revision = old.Revision
		}
		if revision != expected {
			return &ManagementConflict{}
		}
		record, err := m.config.Store.LockVersion(ctx, tx, policy.Ref)
		if err != nil {
			return err
		}
		if record != nil && record.Ref != policy.Ref && !legacy {
			return refusal("forbidden")
		}
		if err = m.config.Store.SavePolicy(ctx, tx, policy); err != nil {
			return err
		}
		now, err := m.config.Store.Now(ctx, tx)
		if err != nil {
			return err
		}
		deadline := now.Add(m.config.WorkBudget)
		expiry := earlier(policy.ValidUntil, policy.RetainUntil)
		result = PolicyChange{Key: key, Policy: policy, Previous: old, Watermark: watermark, Deadline: deadline, Due: now, ExpiryDue: expiry, ExpiryDeadline: expiry.Add(m.config.WorkBudget), State: "complete"}
		if record != nil {
			result.State = "pending"
			result.WorkRevision, err = m.config.Store.NextPolicyWork(ctx, tx, record.ObjectID)
			if err != nil {
				return err
			}
			if err = m.register(ctx, tx, result, *record, record.Ref, now); err != nil {
				return err
			}
			if _, err = m.config.Store.Trigger(ctx, tx, contract.ObjectRef{TenantID: contract.ID(policy.Ref.Owner.TenantID), OwnerID: contract.ID(policy.Ref.Owner.OwnerID), Kind: "content", ID: contract.ID(record.ObjectID)}, "policy_propagation", result.WorkRevision, now); err != nil {
				return err
			}
		}
		if err = m.config.Store.SaveChange(ctx, tx, result); err != nil {
			return err
		}
		return m.current(ctx, tx)
	})
	return result, err
}

// InstallFixturePolicy preserves the explicitly trusted old test installer,
// including its malformed-binding negative fixtures. Real management is strict.
func InstallFixturePolicy(ctx context.Context, store ManagementRepository, policy FixturePolicy, expected int64) error {
	m, err := NewManager(ManagementConfig{Owner: policy.Ref.Owner, Store: store, TrustedSubject: policy.Subject, TrustedUntil: time.Now().Add(time.Hour), PageSize: 64, WorkBudget: time.Minute})
	if err != nil {
		return err
	}
	_, err = m.install(ctx, &policy.Subject, policy, expected, true)
	return err
}
func affected(change PolicyChange, source v.ContentRef, now time.Time) ([]string, bool) {
	p := change.Policy
	if p.Ref != source {
		return []string{"read", "process", "save", "sync", "disclose"}, true
	}
	old := change.Previous
	out := []string{}
	values := []bool{p.Read, p.Process, p.Save, p.Sync, p.Disclose}
	names := []string{"read", "process", "save", "sync", "disclose"}
	before := []bool{true, true, true, true, true}
	if old != nil {
		before = []bool{old.Read, old.Process, old.Save, old.Sync, old.Disclose}
	}
	for i, name := range names {
		if before[i] && !values[i] || !now.Before(p.ValidUntil) || !now.Before(p.RetainUntil) {
			out = append(out, name)
		}
	}
	cleanup := !p.Save || !now.Before(p.ValidUntil) || !now.Before(p.RetainUntil)
	return out, cleanup
}
func (m *Manager) register(ctx context.Context, tx runtime.Tx, change PolicyChange, record Record, source v.ContentRef, now time.Time) error {
	actions, cleanup := affected(change, source, now)
	reason := ""
	if change.Policy.Ref != source {
		reason = "source_binding_mismatch"
	}
	// A separate subject's policy never authorizes deletion of this writer's body.
	a, _ := v.Encode(record.Subject)
	b, _ := v.Encode(change.Policy.Subject)
	applies := string(a) == string(b) && record.Purpose == change.Policy.Purpose
	state := "not_required"
	residual := "use_review"
	if applies {
		if !now.Before(cutoff(record.CurrentRetainUntil)) {
			actions = []string{"read", "process", "save", "sync", "disclose"}
			cleanup = true
			if reason == "" {
				reason = "accepted_retention_expired"
			}
		}
		cap := earlier(cutoff(record.CurrentRetainUntil), change.Policy.RetainUntil)
		if cap.Before(cutoff(record.CurrentRetainUntil)) {
			record.CurrentRetainUntil = wireTime(cap)
			record.Revision++
			if err := m.config.Store.SaveVersion(ctx, tx, record); err != nil {
				return err
			}
		}
		if cleanup {
			state = "pending"
			residual = "holder_unconfirmed"
		}
	}
	return m.config.Store.SaveResponsibility(ctx, tx, CleanupResponsibility{ChangeKey: change.Key, Ref: record.Ref, Subject: change.Policy.Subject, Purpose: change.Policy.Purpose, Actions: actions, Deadline: change.Deadline, BodyCleanup: state, StagingHolder: record.StagingHolder, ObjectHolder: record.ObjectHolder, AttemptKey: record.AttemptKey, Publication: record.Publication, Residual: residual, Reason: reason})
}
func (m *Manager) ObserveChange(ctx context.Context, subject *v.SubjectBinding, key, cursor string, limit int) (PropagationObservation, error) {
	out := PropagationObservation{Responsibilities: []CleanupResponsibility{}}
	if err := m.authorize(ctx, subject); err != nil {
		return out, err
	}
	if limit < 1 || limit > 64 || len(key) > 128 || len(cursor) > 128 {
		return out, refusal("input_over_limit")
	}
	err := m.config.Store.Within(ctx, owner(m.config.Owner), func(ctx context.Context, tx runtime.Tx) error {
		if err := m.current(ctx, tx); err != nil {
			return err
		}
		change, err := m.config.Store.ReadChange(ctx, tx, key)
		if err != nil {
			return err
		}
		if change == nil {
			return refusal("source_unavailable")
		}
		out.Change = *change
		out.Responsibilities, out.NextCursor, err = m.config.Store.Responsibilities(ctx, tx, key, cursor, limit)
		if err != nil {
			return err
		}
		return m.current(ctx, tx)
	})
	return out, err
}

// ObservePolicyChange finds the original revision's management observation.
// It is read-only, including for legacy policies whose backfill is incomplete.
func (m *Manager) ObservePolicyChange(ctx context.Context, subject *v.SubjectBinding, policy FixturePolicy) (PropagationObservation, error) {
	if policy.Ref.Owner != m.config.Owner {
		return PropagationObservation{}, refusal("forbidden")
	}
	return m.ObserveChange(ctx, subject, changeKey(policy), "", 64)
}

func (m *Manager) scheduleAdmission(ctx context.Context, tx runtime.Tx, policy *FixturePolicy, record Record) error {
	change, err := m.config.Store.ReadChange(ctx, tx, changeKey(*policy))
	if err != nil {
		return err
	}
	if change == nil {
		return ErrUnavailable
	}
	if change.State != "complete" {
		return nil
	}
	watermark, err := m.config.Store.LockSourceIndex(ctx, tx)
	if err != nil {
		return err
	}
	change.Watermark = watermark
	change.State = "scheduled"
	change.Due = earlier(earlier(policy.ValidUntil, policy.RetainUntil), cutoff(record.CurrentRetainUntil))
	budget := change.ExpiryDeadline.Sub(change.ExpiryDue)
	if budget <= 0 || budget > 24*time.Hour {
		return ErrUnavailable
	}
	change.ExpiryDue = change.Due
	change.ExpiryDeadline = change.Due.Add(budget)
	change.Deadline = change.ExpiryDeadline
	change.WorkRevision, err = m.config.Store.NextPolicyWork(ctx, tx, record.ObjectID)
	if err != nil {
		return err
	}
	if err = m.config.Store.SaveChange(ctx, tx, *change); err != nil {
		return err
	}
	_, err = m.config.Store.Trigger(ctx, tx, contract.ObjectRef{TenantID: contract.ID(record.Ref.Owner.TenantID), OwnerID: contract.ID(record.Ref.Owner.OwnerID), Kind: "content", ID: contract.ID(record.ObjectID)}, "policy_propagation", change.WorkRevision, change.Due)
	return err
}

// Step advances one bounded page, retaining the frozen watermark and deadlines.
func (m *Manager) Step(ctx context.Context, subject *v.SubjectBinding) (bool, error) {
	if err := m.authorize(ctx, subject); err != nil {
		return false, err
	}
	worked := false
	err := m.config.Store.Within(ctx, owner(m.config.Owner), func(ctx context.Context, tx runtime.Tx) error {
		if err := m.current(ctx, tx); err != nil {
			return err
		}
		now, err := m.config.Store.Now(ctx, tx)
		if err != nil {
			return err
		}
		jobs, err := m.config.Store.Scan(ctx, tx, now, 64)
		if err != nil {
			return err
		}
		for _, job := range jobs {
			if job.Phase != "policy_propagation" {
				continue
			}
			record, err := m.config.Store.LockObject(ctx, tx, string(job.Object.ID))
			if err != nil {
				return err
			}
			if record == nil {
				return runtime.ErrScope
			}
			worked, err = m.advanceJob(ctx, tx, job, *record, "content-policy-management", time.Minute)
			if err != nil || worked {
				if err != nil {
					return err
				}
				return m.current(ctx, tx)
			}
		}
		return m.current(ctx, tx)
	})
	return worked, err
}
func (m *Manager) advanceJob(ctx context.Context, tx runtime.Tx, job runtime.Job, source Record, worker string, lease time.Duration) (bool, error) {
	// A legacy watermark cannot be complete while any original version or
	// policy page remains unregistered. Queries still qualify real sources.
	if _, err := m.config.Store.LockSourceIndex(ctx, tx); err != nil {
		return false, err
	}
	pending, err := m.config.Store.UnindexedVersions(ctx, tx, 1)
	if err != nil || len(pending) > 0 {
		return false, err
	}
	now, err := m.config.Store.Now(ctx, tx)
	if err != nil {
		return false, err
	}
	claim, err := m.config.Store.Claim(ctx, tx, job, worker, now, now.Add(lease))
	if err != nil || claim == nil {
		return false, err
	}
	changes, err := m.config.Store.PendingChanges(ctx, tx, source.ObjectID)
	if err != nil {
		return false, err
	}
	nextDue := time.Time{}
	for _, change := range changes {
		if change.Due.After(now) {
			if nextDue.IsZero() || change.Due.Before(nextDue) {
				nextDue = change.Due
			}
			continue
		}
		if !now.Before(change.Deadline) {
			// The processing budget bounds descendant traversal, not ownership
			// of the already known source holder. Preserve that finite fact even
			// when the remaining frozen watermark becomes residual.
			if err = m.register(ctx, tx, change, source, source.Ref, now); err != nil {
				return false, err
			}
			change.State = "residual"
			change.Reason = "original_deadline_expired"
			if err = m.config.Store.SaveChange(ctx, tx, change); err != nil {
				return false, err
			}
			continue
		}
		if err = m.register(ctx, tx, change, source, source.Ref, now); err != nil {
			return false, err
		}
		refs, next, err := m.config.Store.Descendants(ctx, tx, change.Policy.Ref, change.Watermark, change.Cursor, m.config.PageSize)
		if err != nil {
			return false, err
		}
		for _, ref := range refs {
			record, err := m.config.Store.LockVersion(ctx, tx, ref)
			if err != nil {
				return false, err
			}
			if record == nil || record.Ref != ref {
				return false, runtime.ErrScope
			}
			if err = m.register(ctx, tx, change, *record, source.Ref, now); err != nil {
				return false, err
			}
		}
		change.Cursor = next
		if next == "" {
			change.State = "complete"
			if now.Before(change.ExpiryDue) {
				change.State = "scheduled"
				change.Cursor = ""
				change.Due = change.ExpiryDue
				change.Deadline = change.ExpiryDeadline
			}
		}
		if change.State == "pending" || change.State == "scheduled" {
			if nextDue.IsZero() || change.Due.Before(nextDue) {
				nextDue = change.Due
			}
		}
		if err = m.config.Store.SaveChange(ctx, tx, change); err != nil {
			return false, err
		}
	}
	nextDue, err = m.config.Store.NextPolicyDue(ctx, tx, source.ObjectID)
	if err != nil {
		return false, err
	}
	now, err = m.config.Store.Now(ctx, tx)
	if err != nil {
		return false, err
	}
	if err = m.config.Store.ValidateClaim(ctx, tx, *claim, now); err != nil {
		return false, err
	}
	if !nextDue.IsZero() {
		if !nextDue.After(now) {
			nextDue = now.Add(time.Microsecond)
		}
		return true, m.config.Store.DeferClaim(ctx, tx, *claim, now, nextDue)
	}
	return true, m.config.Store.Complete(ctx, tx, *claim, now)
}

// ScheduleRetention and AdvancePolicyJob keep the domain rules behind narrow
// storage forwarding ports so Repository decorators retain required behavior.
func ScheduleRetention(ctx context.Context, tx runtime.Tx, store ManagementRepository, policy *FixturePolicy, record Record, budget time.Duration) error {
	m := &Manager{config: ManagementConfig{Owner: record.Ref.Owner, Store: store, PageSize: 64, WorkBudget: budget}}
	return m.scheduleAdmission(ctx, tx, policy, record)
}
func AdvancePolicyJob(ctx context.Context, tx runtime.Tx, store ManagementRepository, job runtime.Job, record Record, worker string, lease, budget time.Duration) (bool, error) {
	m := &Manager{config: ManagementConfig{Owner: record.Ref.Owner, Store: store, PageSize: 64, WorkBudget: budget}}
	return m.advanceJob(ctx, tx, job, record, worker, lease)
}
