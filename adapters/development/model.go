package development

import (
	"context"
	"os"
	"regexp"
	"time"

	"github.com/ruipengliu/lerna/adapters/providers"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/brain"
	"github.com/ruipengliu/lerna/internal/governance"
	"github.com/ruipengliu/lerna/runtime"
)

// ModelConfig 只保存受信出口和凭据引用。价格和计数合同没有隐式默认。
type ModelConfig struct {
	ProfileRef               api.ComponentRef `json:"profile_ref"`
	Endpoint                 string           `json:"endpoint"`
	Model                    string           `json:"model"`
	Receiver                 string           `json:"receiver"`
	Location                 string           `json:"location"`
	CredentialEnv            string           `json:"credential_env"`
	CredentialID             string           `json:"credential_id"`
	TokenizerContract        string           `json:"tokenizer_contract"`
	InputUSDPerMillion       string           `json:"input_usd_per_million"`
	CachedInputUSDPerMillion string           `json:"cached_input_usd_per_million"`
	OutputUSDPerMillion      string           `json:"output_usd_per_million"`
	BillingFinal             bool             `json:"billing_final"`
	ContextLimit             uint64           `json:"context_limit"`
	MaxInputTokens           uint64           `json:"max_input_tokens"`
	MaxOutputTokens          uint64           `json:"max_output_tokens"`
	SafetyMargin             uint64           `json:"safety_margin"`
	MaxInputBytes            uint64           `json:"max_input_bytes"`
	RequestTimeoutSeconds    uint64           `json:"request_timeout_seconds"`
	MaxResponseBytes         uint64           `json:"max_response_bytes"`
	MaxConcurrent            int              `json:"max_concurrent"`
	Guidance                 string           `json:"guidance"`
	AllowHTTPForLoopback     bool             `json:"allow_http_for_loopback"`
}

type modelMaterials struct{ a *App }

func (m modelMaterials) ReadMaterial(ctx context.Context, ref api.ContentRef) ([]byte, error) {
	return m.a.ReadContent(ctx, m.a.Scope, m.a.ServiceAuth, ref, "brain.input")
}

func (a *App) configureModel() error {
	c := a.Config.Model
	if c == nil {
		return nil
	}
	if c.Location != "cloud" {
		return api.E("unsupported", "model_location_not_configured")
	}
	if c.TokenizerContract != "utf8-byte-upper-bound-v1" || c.RequestTimeoutSeconds == 0 || c.RequestTimeoutSeconds > 120 || c.InputUSDPerMillion == "" || c.CachedInputUSDPerMillion == "" || c.OutputUSDPerMillion == "" || !regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,127}$`).MatchString(c.CredentialEnv) {
		return api.E("unsupported", "explicit_model_tariff_counter_and_credential_reference_required")
	}
	p := brain.Profile{Ref: c.ProfileRef, ContextLimit: c.ContextLimit, MaxInputTokens: c.MaxInputTokens, MaxOutputTokens: c.MaxOutputTokens, SafetyMargin: c.SafetyMargin, MaxInputBytes: c.MaxInputBytes, RequestTimeout: time.Duration(c.RequestTimeoutSeconds) * time.Second}
	model, err := providers.NewOpenAI(providers.OpenAIConfig{Store: a.Store, Scope: a.Scope, Profile: p, Endpoint: c.Endpoint, Model: c.Model, Receiver: c.Receiver, Location: c.Location, APIKey: os.Getenv(c.CredentialEnv), CredentialID: c.CredentialID, Tokenizer: providers.UTF8UpperBound{}, MaterialResolver: modelMaterials{a}, InputUSDPerMillion: c.InputUSDPerMillion, CachedInputUSDPerMillion: c.CachedInputUSDPerMillion, OutputUSDPerMillion: c.OutputUSDPerMillion, BillingFinal: c.BillingFinal, Guidance: c.Guidance, MaxResponseBytes: c.MaxResponseBytes, MaxConcurrent: c.MaxConcurrent, AllowHTTPForLoopback: a.Config.Development && c.AllowHTTPForLoopback})
	if err != nil {
		return err
	}
	a.Model, a.Engine, a.Profile, a.TokenizerRef = model, model, model.Profile(), model.TokenizerRef()
	return nil
}

func (a *App) decisionCost(snap api.Snapshot) ([]api.Amount, error) {
	if a.Model == nil {
		return []api.Amount{{Unit: "USD", Value: "0"}}, nil
	}
	if snap.ModelProfileRef != a.Profile.Ref {
		return nil, api.E("unsupported", "original_model_profile_not_loaded")
	}
	return a.Model.CostBound(snap.InputTokens, snap.ReservedOutputTokens)
}

func modelUseID(decisionID string) string { return stableID("use", "model/"+decisionID) }
func (a *App) modelGrantID() string       { return stableID("grant", "model/"+a.Profile.Ref.Digest) }

func (a *App) provisionModelGrantTx(ctx context.Context, tx runtime.Tx) error {
	if a.Model == nil {
		return nil
	}
	exists, err := a.Governance.GrantExistsTx(ctx, tx, a.ServiceAuth, a.modelGrantID())
	if err != nil || exists {
		return err
	}
	now, err := tx.Now(ctx)
	if err != nil {
		return err
	}
	return a.Governance.ProvisionGrantTx(ctx, tx, a.ServiceAuth, api.Grant{GrantID: a.modelGrantID(), OwnerID: a.Scope.OwnerID, Revision: 1, SubjectRef: a.ServiceAuth.Ref(a.Scope.OwnerID), Resources: []string{"model-input"}, Actions: []string{"model.request"}, Purposes: []string{"decision"}, Recipients: []string{a.Config.Model.Receiver}, Locations: []string{a.Config.Model.Location}, Mode: "continuous", State: "active", NotBefore: api.Time(now), ExpiresAt: a.Config.PolicyExpiresAt, Limits: []api.Amount{{Unit: "USD", Value: "100"}}})
}

// 原Decision准确许可在同库Brain接纳和每次出站门禁中核验；模型没有准入权。
func (a *App) authorizeModelTx(ctx context.Context, tx runtime.Tx, auth runtime.Auth, in brain.DecideInput, encoding *brain.Encoding) error {
	if a.Model == nil {
		return nil
	}
	if in.ModelProfileRef != a.Profile.Ref || len(in.UseRefs) != 1 || in.UseRefs[0] != tx.Scope().Ref(modelUseID(in.DecisionID), 1) {
		return api.E("forbidden", "original_model_authorization_required")
	}
	source, supported := any(a.Task).(interface {
		DecisionSnapshotTx(context.Context, runtime.Tx, runtime.Auth, string) (api.Snapshot, error)
		DecisionCostBoundTx(context.Context, runtime.Tx, runtime.Auth, string) ([]api.Amount, error)
	})
	if !supported {
		return api.E("unsupported", "original_decision_snapshot_port_required")
	}
	snap, err := source.DecisionSnapshotTx(ctx, tx, auth, in.DecisionID)
	if err != nil {
		return err
	}
	bound, err := a.decisionCost(snap)
	if err != nil {
		return err
	}
	originalBound, err := source.DecisionCostBoundTx(ctx, tx, auth, in.DecisionID)
	if err != nil {
		return err
	}
	if !api.Equal(bound, in.Limits) || !api.Equal(originalBound, in.Limits) || snap.TaskRef != in.TaskRef || snap.Revision != in.SnapshotRevision {
		return api.E("forbidden", "original_model_reservation_changed")
	}
	if encoding != nil {
		if encoding.InputTokens != snap.InputTokens || encoding.Digest != snap.EncodedDigest || encoding.Receiver != a.Config.Model.Receiver || encoding.Location != a.Config.Model.Location {
			return api.E("forbidden", "model_recipient_changed")
		}
	}
	digest, err := api.Digest(in)
	if err != nil {
		return err
	}
	use, err := a.Governance.UseTx(ctx, tx, auth, governance.UseRequest{UseID: in.UseRefs[0].ObjectID, SubjectRef: auth.Ref(tx.Scope().OwnerID), TargetRef: tx.Scope().Ref(in.DecisionID, 1), TargetKind: "decision", IntentHash: digest, GrantRefs: []api.ObjectRef{tx.Scope().Ref(a.modelGrantID(), 1)}, RequestedUnits: in.Limits, Resources: []string{"model-input"}, Actions: []string{"model.request"}, Recipient: a.Config.Model.Receiver, Location: a.Config.Model.Location, Purposes: []string{"decision"}, StartBefore: in.Deadline})
	if err != nil {
		return err
	}
	if use.Decision != "allowed" {
		return api.E("forbidden", "model_grant_denied")
	}
	now, err := tx.Now(ctx)
	if err != nil {
		return err
	}
	return a.Governance.CheckUseTx(ctx, tx, auth, in.UseRefs[0], tx.Scope().Ref(in.DecisionID, 1), digest, now)
}
