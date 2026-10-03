package development

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/ruipengliu/lerna/adapters/collaboration"
	"github.com/ruipengliu/lerna/adapters/platform"
	"github.com/ruipengliu/lerna/adapters/providers"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/memory"
	"github.com/ruipengliu/lerna/internal/task"
	"github.com/ruipengliu/lerna/runtime"
	harness "github.com/ruipengliu/lerna/sdk/go"
)

var remoteEnvRef = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,127}$`)
var remoteKeyDigest = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

// RemoteAgentConfig 固定显式配对；正文、SDK调用和恢复不能添加地址或身份。
// SourceSubjectRefs 只在管理初始化时批准有限消费主体，不能扩旧Content政策。
type RemoteAgentConfig struct {
	Profiles          []collaboration.RemoteAgentProfile `json:"profiles"`
	Peers             []RemoteAgentPeerConfig            `json:"peers"`
	SourceSubjectRefs []api.ObjectRef                    `json:"source_subject_refs"`
}
type RemoteAgentPeerConfig struct {
	TenantID               string        `json:"tenant_id"`
	OwnerID                string        `json:"owner_id"`
	DatabaseID             string        `json:"database_id"`
	Endpoint               string        `json:"endpoint"`
	CAFile                 string        `json:"ca_file"`
	SigningKeyID           string        `json:"signing_key_id"`
	SigningPublicKeyFile   string        `json:"signing_public_key_file"`
	SigningPublicKeyDigest string        `json:"signing_public_key_digest"`
	OutboundTokenEnvRef    string        `json:"outbound_token_env_ref"`
	InboundTokenEnvRef     string        `json:"inbound_token_env_ref"`
	InboundSubjectRef      api.ObjectRef `json:"inbound_subject_ref"`
}
type remoteAgentAssembly struct {
	peers      map[string]RemoteAgentPeerConfig
	journals   []*harness.FileJournal
	transports []*http.Transport
	source     *providers.ForeignSource
}

func validateRemoteAgent(c Config) error {
	p := c.RemoteAgent
	if p == nil {
		return nil
	}
	if len(p.Profiles) == 0 || len(p.Profiles) > 16 || len(p.Peers) == 0 || len(p.Peers) > 16 || len(p.SourceSubjectRefs) > 32 {
		return api.E("invalid_request", "remote_agent_configuration_bounds")
	}
	seen := map[string]bool{}
	for _, peer := range p.Peers {
		u, err := url.Parse(peer.Endpoint)
		if peer.TenantID != c.TenantID || !api.ValidID(peer.OwnerID) || peer.OwnerID == c.OwnerID || !api.ValidID(peer.DatabaseID) || seen[peer.OwnerID] || err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" && u.Path != "/" || !filepath.IsAbs(peer.CAFile) || !filepath.IsAbs(peer.SigningPublicKeyFile) || peer.SigningKeyID == "" || len(peer.SigningKeyID) > 128 || !remoteEnvRef.MatchString(peer.OutboundTokenEnvRef) || !remoteEnvRef.MatchString(peer.InboundTokenEnvRef) || !remoteKeyDigest.MatchString(peer.SigningPublicKeyDigest) || api.ValidateRecord("ObjectRef", peer.InboundSubjectRef) != nil || peer.InboundSubjectRef.OwnerID != c.OwnerID || peer.InboundSubjectRef.TenantID != c.TenantID || peer.InboundSubjectRef.ObjectID == c.SubjectID || peer.InboundSubjectRef.ObjectID == c.OwnerID {
			return api.E("forbidden", "explicit_remote_agent_pair_required")
		}
		seen[peer.OwnerID] = true
	}
	for _, profile := range p.Profiles {
		v := profile.Values
		other := v.ParentOwnerID
		if other == c.OwnerID {
			other = v.ReceiverID
		}
		if !seen[other] || v.ParentOwnerID != c.OwnerID && v.ReceiverID != c.OwnerID {
			return api.E("forbidden", "remote_agent_profile_unpaired")
		}
		if _, err := collaboration.NewRemoteAgentProfile(profile.ProfileRef.ComponentID, profile.ProfileRef.Version, v); err != nil {
			return err
		}
		digest, err := api.Digest(v)
		if err != nil {
			return err
		}
		if digest != profile.ProfileRef.Digest {
			return api.E("idempotency_conflict", "remote_agent_profile_digest_changed")
		}
	}
	seenSubjects := map[string]bool{}
	for _, ref := range p.SourceSubjectRefs {
		key, _ := api.Digest(ref)
		if api.ValidateRecord("ObjectRef", ref) != nil || ref.TenantID != c.TenantID || !seen[ref.OwnerID] || ref.ObjectID != ref.OwnerID && ref.ObjectID != c.SubjectID || seenSubjects[key] {
			return api.E("forbidden", "remote_source_subject_unpaired")
		}
		seenSubjects[key] = true
	}
	return nil
}
func remoteAgentPrincipals(c Config) ([]platform.Principal, error) {
	if err := validateRemoteAgent(c); err != nil {
		return nil, err
	}
	if c.RemoteAgent == nil {
		return nil, nil
	}
	principals := []platform.Principal{}
	for _, peer := range c.RemoteAgent.Peers {
		token := strings.TrimSpace(os.Getenv(peer.InboundTokenEnvRef))
		if token == "" || len(token) > 4096 {
			return nil, api.E("unsupported", "remote_inbound_credential_unavailable")
		}
		auth := runtime.Auth{TenantID: c.TenantID, SubjectID: peer.InboundSubjectRef.ObjectID, CredentialGeneration: peer.InboundSubjectRef.Revision, Roles: []string{"paired_agent"}}
		principals = append(principals, platform.Principal{Auth: auth, TokenHash: api.Hash([]byte(token))})
	}
	// 外来处理主体只取得本方数据政策中的身份，不创建其可登录凭据或角色。
	// 消费方自己的当前凭据仍在held_copy门禁另核；此处不是跨域登录系统。
	seen := map[string]bool{}
	for _, ref := range c.RemoteAgent.SourceSubjectRefs {
		if ref.ObjectID == c.SubjectID || seen[ref.ObjectID] {
			continue
		}
		secret := make([]byte, 32)
		if _, err := rand.Read(secret); err != nil {
			return nil, err
		}
		principals = append(principals, platform.Principal{Auth: runtime.Auth{TenantID: c.TenantID, SubjectID: ref.ObjectID, CredentialGeneration: ref.Revision, Roles: []string{}}, TokenHash: api.Hash(secret)})
		seen[ref.ObjectID] = true
	}
	return principals, nil
}
func readRemotePublicKey(c RemoteAgentPeerConfig) (*platform.Keyring, error) {
	info, err := os.Lstat(c.SigningPublicKeyFile)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > 16<<10 {
		return nil, api.E("forbidden", "remote_public_key_file_invalid")
	}
	b, err := os.ReadFile(c.SigningPublicKeyFile)
	if err != nil {
		return nil, err
	}
	block, rest := pem.Decode(b)
	if block == nil || block.Type != "PUBLIC KEY" || len(strings.TrimSpace(string(rest))) != 0 || api.Hash(block.Bytes) != c.SigningPublicKeyDigest {
		return nil, api.E("forbidden", "remote_public_key_digest_mismatch")
	}
	parsed, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, api.E("forbidden", "remote_public_key_invalid")
	}
	key, ok := parsed.(*ecdsa.PublicKey)
	if !ok || key.Curve != elliptic.P256() {
		return nil, api.E("forbidden", "remote_es256_key_required")
	}
	return &platform.Keyring{Keys: map[string]platform.RegisteredKey{c.SigningKeyID: {TenantID: c.TenantID, Issuer: c.OwnerID, Purposes: []string{"agent_allocation", "agent_state", "foreign_content"}, Public: key}}}, nil
}
func (a *App) configureRemoteAgent(local *collaboration.Adapter) (*collaboration.Remote, error) {
	if err := validateRemoteAgent(a.Config); err != nil {
		return nil, err
	}
	if a.Config.RemoteAgent == nil {
		return nil, nil
	}
	assembly := &remoteAgentAssembly{peers: map[string]RemoteAgentPeerConfig{}}
	a.remoteAgents = assembly
	peers := []collaboration.RemotePeer{}
	ports := map[string]memory.ForeignContentPort{}
	for _, cfg := range a.Config.RemoteAgent.Peers {
		keys, err := readRemotePublicKey(cfg)
		if err != nil {
			return nil, err
		}
		ca, err := os.ReadFile(cfg.CAFile)
		if err != nil {
			return nil, err
		}
		if len(ca) > 64<<10 {
			return nil, api.E("forbidden", "remote_tls_ca_bounds")
		}
		roots := x509.NewCertPool()
		if !roots.AppendCertsFromPEM(ca) {
			return nil, api.E("forbidden", "remote_tls_ca_invalid")
		}
		token := strings.TrimSpace(os.Getenv(cfg.OutboundTokenEnvRef))
		if token == "" || len(token) > 4096 {
			return nil, api.E("unsupported", "remote_outbound_credential_unavailable")
		}
		transport := &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: roots}, MaxConnsPerHost: 8, MaxIdleConnsPerHost: 4, MaxIdleConns: 16, IdleConnTimeout: 30 * time.Second, ResponseHeaderTimeout: 10 * time.Second}
		assembly.transports = append(assembly.transports, transport)
		scope := runtime.Scope{TenantID: cfg.TenantID, OwnerID: cfg.OwnerID, DatabaseID: cfg.DatabaseID}
		methods := append(collaboration.RemoteAgentContracts(), providers.ForeignSourceContracts()...)
		digest, err := api.DigestLimit(methods, 1<<20)
		if err != nil {
			return nil, err
		}
		discovery := harness.Discovery{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: scope.OwnerID, SchemaDigest: api.CoreDigest(), Methods: methods, MethodsDigest: digest, IdentityScope: scope.TenantID + "/" + scope.OwnerID + "/" + scope.DatabaseID, Limits: harness.Limits{MaxDomainBytes: api.MaxJSONBytes, MaxFrameBytes: 1 << 20, MaxPending: 4096}}
		journal, err := harness.OpenJournal(filepath.Join(a.Config.DataRoot, "remote-agent-journals", scope.OwnerID), discovery.IdentityScope)
		if err != nil {
			return nil, err
		}
		assembly.journals = append(assembly.journals, journal)
		client, err := harness.NewClient(&harness.HTTPTransport{BaseURL: cfg.Endpoint, Token: token, HTTP: &http.Client{Transport: transport, Timeout: 15 * time.Second}}, journal, discovery)
		if err != nil {
			return nil, err
		}
		peers = append(peers, collaboration.RemotePeer{Scope: scope, Client: client, Keys: keys})
		ports[scope.OwnerID] = &providers.ForeignSourceClient{SDK: client, Keys: keys, SourceScope: scope, ConsumerScope: a.Scope}
		assembly.peers[scope.OwnerID] = cfg
	}
	remote, err := collaboration.NewRemote(collaboration.RemoteConfig{Store: a.Store, Scope: a.Scope, Registry: a.Registry, Memory: a.Memory, ProofPolicy: a.ContentPolicy, Keys: a.Keys, SigningKeyID: "development-es256", Auth: a.ServiceAuth, Authority: remoteAgentAuthority{a}, Profiles: a.Config.RemoteAgent.Profiles, Peers: peers, Participants: []string{"platform", "governance"}, Local: local})
	if err != nil {
		return nil, err
	}
	previous := a.Memory.Foreign
	a.Memory.Foreign = &remoteSourceRoutes{ports: ports, fallback: previous}
	source, err := providers.NewForeignSource(providers.ForeignSourceConfig{Store: a.Store, Scope: a.Scope, Memory: a.Memory, Keys: a.Keys, SigningKeyID: "development-es256", Authority: remoteAgentAuthority{a}, Participants: []string{"platform", "governance"}})
	if err != nil {
		return nil, err
	}
	assembly.source = source
	return remote, nil
}
func (a *remoteAgentAssembly) close() error {
	if a == nil {
		return nil
	}
	for _, transport := range a.transports {
		transport.CloseIdleConnections()
	}
	var err error
	for _, journal := range a.journals {
		err = errors.Join(err, journal.Close())
	}
	return err
}

type remoteAgentAuthority struct{ a *App }

func (g remoteAgentAuthority) CheckPeerTx(ctx context.Context, tx runtime.Tx, peer runtime.Auth, owner string) error {
	if g.a.remoteAgents == nil {
		return api.E("unsupported", "remote_agent_authority_unconfigured")
	}
	c, ok := g.a.remoteAgents.peers[owner]
	if !ok || tx.Scope() != g.a.Scope || peer.TenantID != g.a.Scope.TenantID || peer.SubjectID != c.InboundSubjectRef.ObjectID || peer.CredentialGeneration != c.InboundSubjectRef.Revision || !api.Equal(peer.Roles, []string{"paired_agent"}) {
		return api.E("forbidden", "remote_agent_peer_unpaired")
	}
	return currentCredentialTx(ctx, tx, peer)
}
func (g remoteAgentAuthority) ResolveSubjectTx(ctx context.Context, tx runtime.Tx, ref api.ObjectRef, p collaboration.RemoteAgentProfile, control bool) (runtime.Auth, error) {
	auth := g.a.UserAuth
	if tx.Scope() != g.a.Scope || ref.OwnerID != p.Values.ParentOwnerID || ref.TenantID != auth.TenantID || ref.ObjectID != auth.SubjectID || !control && ref.Revision != auth.CredentialGeneration || control && ref.Revision > auth.CredentialGeneration {
		return runtime.Auth{}, api.E("forbidden", "remote_agent_subject_unpaired")
	}
	allowed := false
	for _, subject := range p.Values.SubjectRefs {
		allowed = allowed || subject == ref
	}
	if !allowed {
		return runtime.Auth{}, api.E("forbidden", "remote_agent_subject_scope_exceeded")
	}
	return auth, currentCredentialTx(ctx, tx, auth)
}
func (g remoteAgentAuthority) ResolveSourceSubjectTx(ctx context.Context, tx runtime.Tx, peer runtime.Auth, ref memory.ForeignReference, control bool) (runtime.Auth, error) {
	if err := g.CheckPeerTx(ctx, tx, peer, ref.HolderRef.OwnerID); err != nil {
		return runtime.Auth{}, err
	}
	if ref.HolderRef.ObjectID == g.a.UserAuth.SubjectID {
		auth := g.a.UserAuth
		if !control && ref.HolderRef.Revision != auth.CredentialGeneration || control && ref.HolderRef.Revision > auth.CredentialGeneration {
			return runtime.Auth{}, api.E("forbidden", "remote_source_subject_generation_changed")
		}
		return auth, currentCredentialTx(ctx, tx, auth)
	}
	for _, approved := range g.a.Config.RemoteAgent.SourceSubjectRefs {
		if approved.OwnerID == ref.HolderRef.OwnerID && approved.ObjectID == ref.HolderRef.ObjectID && (!control && approved.Revision == ref.HolderRef.Revision || control && approved.Revision >= ref.HolderRef.Revision) {
			auth := runtime.Auth{TenantID: g.a.Scope.TenantID, SubjectID: approved.ObjectID, CredentialGeneration: approved.Revision, Roles: []string{}}
			return auth, currentCredentialTx(ctx, tx, auth)
		}
	}
	return runtime.Auth{}, api.E("forbidden", "remote_source_holder_unapproved")
}

type remoteSourceRoutes struct {
	ports    map[string]memory.ForeignContentPort
	fallback memory.ForeignContentPort
}

func (r *remoteSourceRoutes) route(ref memory.ForeignReference) (memory.ForeignContentPort, error) {
	if port, ok := r.ports[ref.ContentRef.OwnerID]; ok {
		return port, nil
	}
	if r.fallback != nil {
		return r.fallback, nil
	}
	return nil, api.E("dependency_unavailable", "foreign_source_owner_unconfigured")
}
func (r *remoteSourceRoutes) RegisterCopy(ctx context.Context, s runtime.Scope, a runtime.Auth, ref memory.ForeignReference) (memory.ForeignProof, error) {
	p, e := r.route(ref)
	if e != nil {
		return memory.ForeignProof{}, e
	}
	return p.RegisterCopy(ctx, s, a, ref)
}
func (r *remoteSourceRoutes) Current(ctx context.Context, s runtime.Scope, a runtime.Auth, ref memory.ForeignReference) (memory.ForeignProof, error) {
	p, e := r.route(ref)
	if e != nil {
		return memory.ForeignProof{}, e
	}
	return p.Current(ctx, s, a, ref)
}
func (r *remoteSourceRoutes) Control(ctx context.Context, s runtime.Scope, a runtime.Auth, ref memory.ForeignReference) (memory.ForeignProof, error) {
	p, e := r.route(ref)
	if e != nil {
		return memory.ForeignProof{}, e
	}
	return p.Control(ctx, s, a, ref)
}
func (r *remoteSourceRoutes) Read(ctx context.Context, s runtime.Scope, a runtime.Auth, ref memory.ForeignReference, proof memory.ForeignProof) ([]byte, error) {
	p, e := r.route(ref)
	if e != nil {
		return nil, e
	}
	return p.Read(ctx, s, a, ref, proof)
}
func (r *remoteSourceRoutes) Release(ctx context.Context, s runtime.Scope, a runtime.Auth, ref memory.ForeignReference, in memory.ReleaseCopyInput) (memory.ForeignProof, error) {
	p, e := r.route(ref)
	if e != nil {
		return memory.ForeignProof{}, e
	}
	return p.Release(ctx, s, a, ref, in)
}
func (r *remoteSourceRoutes) VerifyTx(ctx context.Context, tx runtime.Tx, a runtime.Auth, ref memory.ForeignReference, proof memory.ForeignProof) error {
	p, e := r.route(ref)
	if e != nil {
		return e
	}
	return p.VerifyTx(ctx, tx, a, ref, proof)
}

// CurrentTaskGate 不读网络，nil配置也不能把既有外来Incoming当成本机任务。
func (g taskGate) CheckTaskCurrentTx(ctx context.Context, tx runtime.Tx, actual api.Task, requireRunning bool) error {
	if g.a.RemoteAgent != nil {
		return g.a.RemoteAgent.CheckTaskCurrentTx(ctx, tx, actual, requireRunning)
	}
	source, found, err := g.a.Task.ReadIncomingSourceTx(ctx, tx, g.a.ServiceAuth, actual.TaskID)
	if err != nil {
		return err
	}
	if found && source.OwnerID != tx.Scope().OwnerID {
		return api.E("unsupported", "remote_parent_authority_unconfigured")
	}
	return nil
}

var _ task.CurrentTaskGate = taskGate{}
