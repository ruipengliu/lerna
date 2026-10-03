package bootstrap

import (
	"context"
	"github.com/ruipengliu/lerna/adapters/platform"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/brain"
	"github.com/ruipengliu/lerna/internal/governance"
	"github.com/ruipengliu/lerna/runtime"
	"time"
)

type taskGate struct{ a *App }

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
		return api.E("forbidden", "identity_authority_unavailable")
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
	if e := currentCredentialTx(ctx, tx, auth); e != nil {
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
		if encoding.Receiver != "builtin-rule-engine" || encoding.Location != "cloud" {
			return api.E("forbidden", "model_recipient_not_configured")
		}
	}
	return nil
}

type usageVerifier struct{ a *App }

func (v usageVerifier) Verify(ctx context.Context, s runtime.Scope, ref api.ObjectRef, u api.UsageSnapshot) error {
	var actual api.UsageSnapshot
	var e error
	actual, e = v.a.Brain.Usage(ctx, v.a.Store, s, ref)
	if api.IsCode(e, "not_found") {
		actual, e = (executionBridge{v.a}).Usage(ctx, s, ref)
	}
	if e != nil {
		return e
	}
	if !api.Equal(actual, u) {
		return api.E("forbidden", "usage_source_mismatch")
	}
	return nil
}
