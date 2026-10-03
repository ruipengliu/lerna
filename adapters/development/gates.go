package development

import (
	"context"
	"github.com/ruipengliu/lerna/adapters/platform"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/brain"
	"github.com/ruipengliu/lerna/internal/governance"
	"github.com/ruipengliu/lerna/internal/task"
	"github.com/ruipengliu/lerna/runtime"
	"time"
)

type taskGate struct{ a *App }

func (g taskGate) CheckSubjectTx(ctx context.Context, tx runtime.Tx, auth runtime.Auth) error {
	return currentCredentialTx(ctx, tx, auth)
}

func (g taskGate) Authorize(ctx context.Context, tx runtime.Tx, auth runtime.Auth, purpose string, contents []api.ContentRef, objects []api.ObjectRef) error {
	if e := currentCredentialTx(ctx, tx, auth); e != nil {
		return e
	}
	for _, r := range contents {
		if _, e := g.a.Memory.CheckContentTx(ctx, tx, auth, r, purpose, "cloud", true); e != nil {
			return e
		}
	}
	for _, r := range objects {
		if e := runtime.CheckRef(tx.Scope(), r); e != nil {
			return e
		}
	}
	return nil
}
func (g taskGate) Evidence(ctx context.Context, tx runtime.Tx, t api.Task, checks []api.ObjectRef, _ []api.ComponentRef) error {
	refs := []governance.CheckReference{}
	for _, r := range checks {
		refs = append(refs, governance.CheckReference{CheckRef: r})
	}
	_, e := g.a.Governance.CheckEvidenceTx(ctx, tx, governance.EvidenceCompletion{ConsumerTaskRef: tx.Scope().Ref(t.TaskID, t.Revision), Checks: refs, MaxStalenessSeconds: 0})
	return e
}

type currentCredential struct {
	Revision   uint64   `json:"revision"`
	SubjectID  string   `json:"subject_id"`
	Generation uint64   `json:"generation"`
	State      string   `json:"state"`
	Roles      []string `json:"roles"`
}

func currentCredentialTx(ctx context.Context, tx runtime.Tx, a runtime.Auth) error {
	var c currentCredential
	if _, e := tx.Get(ctx, "platform.credentials", a.SubjectID, &c); e != nil {
		if api.IsCode(e, "not_found") {
			return api.E("forbidden", "identity_authority_unavailable")
		}
		return e
	}
	if c.State != "active" || c.Generation != a.CredentialGeneration {
		return api.E("forbidden", "credential_revoked")
	}
	for _, role := range a.Roles {
		found := false
		for _, actual := range c.Roles {
			found = found || actual == role
		}
		if !found {
			return api.E("forbidden", "role_changed")
		}
	}
	return nil
}

type proofBridge struct{ a *App }

func proofClaims(p governance.ProofStatement) platform.ProofClaims {
	return platform.ProofClaims{TenantID: p.TenantID, Issuer: p.IssuerID, Audience: p.AudienceID, Purpose: p.Purpose, ObjectRef: p.ObjectRef, Digest: p.Digest, WindowID: p.ObjectRef.ObjectID, IssuedAt: p.IssuedAt, StartBefore: p.StartBefore}
}
func (p proofBridge) SignLocal(s governance.ProofStatement) (string, error) {
	return p.a.Keys.Sign("development-es256", proofClaims(s))
}
func (p proofBridge) VerifyLocal(jws string, s governance.ProofStatement, now time.Time) error {
	_, e := p.a.Keys.Verify(jws, proofClaims(s), now)
	return e
}

type brainGate struct{ a *App }

func (g brainGate) CheckTx(ctx context.Context, tx runtime.Tx, auth runtime.Auth, in brain.DecideInput, encoding *brain.Encoding) error {
	if !auth.HasRole("service") {
		return api.E("forbidden", "trusted_orchestrator_required")
	}
	if e := g.a.Task.CheckDecisionTx(ctx, tx, auth, in.DecisionID); e != nil {
		return e
	}
	if e := currentCredentialTx(ctx, tx, auth); e != nil {
		return e
	}
	if e := g.a.authorizeModelTx(ctx, tx, auth, in, encoding); e != nil {
		return e
	}
	if _, e := g.a.Memory.CheckContentTx(ctx, tx, auth, in.SnapshotRef, "brain.input", "cloud", true); e != nil {
		return e
	}
	if encoding != nil {
		for _, r := range encoding.ProcessedSources {
			if _, e := g.a.Memory.CheckContentTx(ctx, tx, auth, r, "brain.input", "cloud", true); e != nil {
				return e
			}
		}
		if g.a.Model == nil && (encoding.Receiver != "builtin-rule-engine" || encoding.Location != "cloud") {
			return api.E("forbidden", "model_recipient_not_configured")
		}
	}
	return nil
}

func (g taskGate) RegisterCoverage(ctx context.Context, tx runtime.Tx, t api.Task, c api.GoalCoverage) error {
	return (evidenceBridge{g.a}).RegisterCoverage(ctx, tx, t, c)
}
func (g taskGate) RegisterCheck(ctx context.Context, tx runtime.Tx, t api.Task, c api.ConditionResult) error {
	return (evidenceBridge{g.a}).RegisterCheck(ctx, tx, t, c)
}
func (g taskGate) BindResult(ctx context.Context, tx runtime.Tx, t api.Task, r api.Result, checks []api.ObjectRef) error {
	return (evidenceBridge{g.a}).BindResult(ctx, tx, t, r, checks)
}

type contentAuthority struct{ a *App }

func (g contentAuthority) Check(ctx context.Context, tx runtime.Tx, auth runtime.Auth, _ api.ComponentRef, _, _ string, _ bool) (uint64, error) {
	if e := currentCredentialTx(ctx, tx, auth); e != nil {
		return 0, e
	}
	var c currentCredential
	_, e := tx.Get(ctx, "platform.credentials", auth.SubjectID, &c)
	return c.Revision, e
}
func (g contentAuthority) Visibility(ctx context.Context, tx runtime.Tx, auth runtime.Auth) (string, error) {
	if e := currentCredentialTx(ctx, tx, auth); e != nil {
		return "", e
	}
	var c currentCredential
	if _, e := tx.Get(ctx, "platform.credentials", auth.SubjectID, &c); e != nil {
		return "", e
	}
	return api.Digest(c)
}

type previewGate struct{ a *App }

func (g previewGate) CheckTx(ctx context.Context, tx runtime.Tx, auth runtime.Auth, refs []api.ContentRef) error {
	return (taskGate{g.a}).Authorize(ctx, tx, auth, "confirmation.preview", refs, nil)
}

type scheduleGate struct{ a *App }

func (g scheduleGate) CheckTx(ctx context.Context, tx runtime.Tx, auth runtime.Auth, policy, installLock api.ComponentRef, budget []api.Amount) error {
	if e := currentCredentialTx(ctx, tx, auth); e != nil {
		return e
	}
	if !api.Equal(policy, g.a.TaskPolicy.PolicyRef) || !api.Equal(installLock, g.a.InstallLock) {
		return api.E("unsupported", "schedule_profiles_not_configured")
	}
	if e := api.ValidateAmounts(budget); e != nil {
		return e
	}
	for _, b := range budget {
		if b.Unit != "USD" {
			return api.E("unsupported", "budget_unit_not_configured")
		}
		cmp, e := api.CompareDecimal(b.Value, "100")
		if e != nil {
			return e
		}
		if cmp > 0 {
			return api.E("invalid_request", "budget_limit_exceeded")
		}
	}
	return nil
}

var _ task.EvidenceRegistration = taskGate{}

type usageVerifier struct{ a *App }

type resultNoticeBridge struct{ a *App }

func (b resultNoticeBridge) RecordNoticeTx(ctx context.Context, tx runtime.Tx, notice governance.ResultNotice) error {
	return b.a.Task.RecordResultNoticeTx(ctx, tx, b.a.ServiceAuth, task.ResultNotice{NoticeRef: tx.Scope().Ref(notice.NoticeID, 1), ConsumerTaskRef: notice.ConsumerTaskRef, ResultRef: notice.ResultRef, HolderRef: notice.HolderRef, DefectRef: notice.DefectRef, Reason: notice.Reason, RegisteredAt: notice.RegisteredAt})
}

func (v usageVerifier) Verify(ctx context.Context, s runtime.Scope, ref api.ObjectRef, u api.UsageSnapshot) error {
	var actual api.UsageSnapshot
	var e error
	actual, e = v.a.Brain.Usage(ctx, v.a.Store, s, ref)
	if api.IsCode(e, "not_found") {
		actual, e = (executionBridge{v.a}).rawUsage(ctx, s, ref)
	}
	if e != nil {
		return e
	}
	if !api.Equal(actual, u) {
		return api.E("forbidden", "usage_source_mismatch")
	}
	return nil
}
