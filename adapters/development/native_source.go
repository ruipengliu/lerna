package development

import (
	"context"
	"crypto/rand"
	"os"
	"regexp"
	"strings"

	"github.com/ruipengliu/lerna/adapters/platform"
	"github.com/ruipengliu/lerna/adapters/providers"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/memory"
	"github.com/ruipengliu/lerna/runtime"
)

// ForeignConsumerConfig 是 Source 管理员批准的准确外来消费方；不是请求中的授权。
// 入站 peer 与 holder 分开。DatabaseID 由消费方原只读身份检查取得，并与 peer 一同冻结。
type ForeignConsumerConfig struct {
	TenantID           string                  `json:"tenant_id"`
	OwnerID            string                  `json:"owner_id"`
	DatabaseID         string                  `json:"database_id"`
	PeerSubjectRef     api.ObjectRef           `json:"peer_subject_ref"`
	InboundTokenEnvRef string                  `json:"inbound_token_env_ref"`
	Holders            []ForeignConsumerHolder `json:"holders"`
	Purposes           []string                `json:"purposes"`
	Locations          []string                `json:"locations"`
}
type ForeignConsumerHolder struct {
	SubjectRef api.ObjectRef `json:"subject_ref"`
	Roles      []string      `json:"roles"`
}
type foreignConsumerPair struct {
	Config        ForeignConsumerConfig `json:"config"`
	PeerTokenHash string                `json:"peer_token_hash"`
}

var foreignConsumerEnv = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,127}$`)

func validateForeignConsumers(c Config) error {
	if len(c.ForeignConsumers) > 4 {
		return api.E("invalid_request", "foreign_consumer_configuration_bounds")
	}
	owners, subjects := map[string]bool{}, map[string]bool{c.SubjectID: true, c.OwnerID: true}
	for _, p := range c.ForeignConsumers {
		if p.TenantID != c.TenantID || !api.ValidID(p.OwnerID) || p.OwnerID == c.OwnerID || !api.ValidID(p.DatabaseID) || p.DatabaseID == c.DatabaseID || owners[p.OwnerID] || api.ValidateRecord("ObjectRef", p.PeerSubjectRef) != nil || p.PeerSubjectRef.OwnerID != c.OwnerID || p.PeerSubjectRef.TenantID != c.TenantID || subjects[p.PeerSubjectRef.ObjectID] || !foreignConsumerEnv.MatchString(p.InboundTokenEnvRef) || len(p.Holders) == 0 || len(p.Holders) > 8 || !boundedConsumerStrings(p.Purposes, 16, 128) || !boundedConsumerStrings(p.Locations, 4, 64) {
			return api.E("forbidden", "explicit_foreign_consumer_pair_required")
		}
		owners[p.OwnerID], subjects[p.PeerSubjectRef.ObjectID] = true, true
		for _, holder := range p.Holders {
			ref := holder.SubjectRef
			if api.ValidateRecord("ObjectRef", ref) != nil || ref.TenantID != c.TenantID || ref.OwnerID != p.OwnerID || subjects[ref.ObjectID] || len(holder.Roles) > 16 || len(holder.Roles) != 0 && !boundedConsumerStrings(holder.Roles, 16, 64) {
				return api.E("forbidden", "foreign_consumer_holder_unapproved")
			}
			subjects[ref.ObjectID] = true
		}
	}
	// 两种用途共享同一 Source 合同；同一个 owner 的 Authority 不能歧义。
	if c.RemoteAgent != nil {
		for _, p := range c.RemoteAgent.Peers {
			if owners[p.OwnerID] || subjects[p.InboundSubjectRef.ObjectID] {
				return api.E("forbidden", "foreign_consumer_authority_ambiguous")
			}
		}
		for _, ref := range c.RemoteAgent.SourceSubjectRefs {
			if subjects[ref.ObjectID] && ref.ObjectID != c.SubjectID && ref.ObjectID != c.OwnerID {
				return api.E("forbidden", "foreign_consumer_authority_ambiguous")
			}
		}
	}
	return nil
}
func boundedConsumerStrings(values []string, maxCount, maxBytes int) bool {
	if len(values) == 0 || len(values) > maxCount {
		return false
	}
	seen := map[string]bool{}
	for _, value := range values {
		if value == "" || len(value) > maxBytes || strings.TrimSpace(value) != value || seen[value] {
			return false
		}
		seen[value] = true
	}
	return true
}
func foreignConsumerPeer(p ForeignConsumerConfig) runtime.Auth {
	return runtime.Auth{TenantID: p.TenantID, SubjectID: p.PeerSubjectRef.ObjectID, CredentialGeneration: p.PeerSubjectRef.Revision, Roles: []string{"paired_content_consumer"}}
}
func foreignConsumerHolder(p ForeignConsumerConfig, holder ForeignConsumerHolder) runtime.Auth {
	return runtime.Auth{TenantID: p.TenantID, SubjectID: holder.SubjectRef.ObjectID, CredentialGeneration: holder.SubjectRef.Revision, Roles: append([]string{}, holder.Roles...)}
}
func foreignConsumerPrincipals(c Config) ([]platform.Principal, error) {
	if err := validateForeignConsumers(c); err != nil {
		return nil, err
	}
	var principals []platform.Principal
	for _, p := range c.ForeignConsumers {
		token := strings.TrimSpace(os.Getenv(p.InboundTokenEnvRef))
		if len(token) < 32 || len(token) > 4096 {
			return nil, api.E("unsupported", "foreign_consumer_inbound_credential_unavailable")
		}
		principals = append(principals, platform.Principal{Auth: foreignConsumerPeer(p), TokenHash: api.Hash([]byte(token))})
		for _, holder := range p.Holders {
			secret := make([]byte, 32)
			if _, err := rand.Read(secret); err != nil {
				return nil, err
			}
			// 准确 holder 身份可被 Source gate 解析，但没有可交付的 Source 登录 token。
			principals = append(principals, platform.Principal{Auth: foreignConsumerHolder(p, holder), TokenHash: api.Hash(secret)})
		}
	}
	return principals, nil
}
func (a *App) configureForeignConsumerSource(ctx context.Context, initialize bool) error {
	if len(a.Config.ForeignConsumers) == 0 {
		return nil
	}
	for _, p := range a.Config.ForeignConsumers {
		pair := foreignConsumerPair{p, api.Hash([]byte(strings.TrimSpace(os.Getenv(p.InboundTokenEnvRef))))}
		status, err := a.Store.Within(ctx, a.Scope, []string{"platform"}, func(tx runtime.Tx) error {
			var old foreignConsumerPair
			_, err := tx.Get(ctx, "platform.foreign_consumer_pairs", p.OwnerID, &old)
			if err == nil {
				if !api.Equal(old, pair) {
					return api.E("idempotency_conflict", "original_foreign_consumer_pair_changed")
				}
				return nil
			}
			if !api.IsCode(err, "not_found") {
				return err
			}
			if !initialize {
				return api.E("unsupported", "foreign_consumer_pair_not_initialized")
			}
			return tx.Create(ctx, "platform.foreign_consumer_pairs", p.OwnerID, "", pair)
		})
		if status == runtime.CommitUnknown {
			return runtime.ErrCommitUnknown
		}
		if err != nil {
			return err
		}
	}
	source, err := providers.NewForeignSource(providers.ForeignSourceConfig{Store: a.Store, Scope: a.Scope, Memory: a.Memory, Keys: a.Keys, SigningKeyID: "development-es256", Authority: foreignConsumerAuthority{a}, Participants: []string{"platform", "governance"}})
	if err == nil {
		a.foreignConsumerSource = source
	}
	return err
}

type foreignConsumerAuthority struct{ a *App }

func (g foreignConsumerAuthority) ResolveSourceSubjectTx(ctx context.Context, tx runtime.Tx, peer runtime.Auth, ref memory.ForeignReference, control bool) (runtime.Auth, error) {
	if tx.Scope() != g.a.Scope {
		return runtime.Auth{}, api.E("forbidden", "foreign_consumer_source_scope_mismatch")
	}
	for _, p := range g.a.Config.ForeignConsumers {
		if p.OwnerID != ref.HolderRef.OwnerID {
			continue
		}
		if !api.Equal(peer, foreignConsumerPeer(p)) || !containsString(p.Purposes, ref.Purpose) || !containsString(p.Locations, ref.Location) || ref.ReferenceIntentRef.OwnerID != p.OwnerID {
			return runtime.Auth{}, api.E("forbidden", "foreign_consumer_request_unpaired")
		}
		var pair foreignConsumerPair
		if _, err := tx.Get(ctx, "platform.foreign_consumer_pairs", p.OwnerID, &pair); err != nil {
			return runtime.Auth{}, err
		}
		if !api.Equal(pair.Config, p) {
			return runtime.Auth{}, api.E("forbidden", "original_foreign_consumer_pair_changed")
		}
		if err := exactForeignConsumerCredentialTx(ctx, tx, peer); err != nil {
			return runtime.Auth{}, err
		}
		for _, holder := range p.Holders {
			if holder.SubjectRef.ObjectID != ref.HolderRef.ObjectID {
				continue
			}
			if !control && holder.SubjectRef != ref.HolderRef || control && (holder.SubjectRef.TenantID != ref.HolderRef.TenantID || holder.SubjectRef.OwnerID != ref.HolderRef.OwnerID || holder.SubjectRef.Revision < ref.HolderRef.Revision) {
				return runtime.Auth{}, api.E("forbidden", "foreign_consumer_holder_generation_changed")
			}
			auth := foreignConsumerHolder(p, holder)
			return auth, exactForeignConsumerCredentialTx(ctx, tx, auth)
		}
		return runtime.Auth{}, api.E("forbidden", "foreign_consumer_holder_unapproved")
	}
	if g.a.remoteAgents != nil {
		return (remoteAgentAuthority{g.a}).ResolveSourceSubjectTx(ctx, tx, peer, ref, control)
	}
	return runtime.Auth{}, api.E("forbidden", "foreign_consumer_unpaired")
}
func exactForeignConsumerCredentialTx(ctx context.Context, tx runtime.Tx, auth runtime.Auth) error {
	if err := currentCredentialTx(ctx, tx, auth); err != nil {
		return err
	}
	var record currentCredential
	if _, err := tx.Get(ctx, "platform.credentials", auth.SubjectID, &record); err != nil {
		return err
	}
	if !api.Equal(record.Roles, auth.Roles) {
		return api.E("forbidden", "foreign_consumer_current_roles_changed")
	}
	return nil
}
