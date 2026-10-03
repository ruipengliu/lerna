package development

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"

	"github.com/ruipengliu/lerna/adapters/endpointchannel"
	rpc "github.com/ruipengliu/lerna/adapters/grpc"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/runtime"
	harness "github.com/ruipengliu/lerna/sdk/go"
)

type EndpointTLSFiles struct {
	CertificateFile string `json:"certificate_file"`
	KeyFile         string `json:"key_file"`
	CAFile          string `json:"ca_file,omitempty"`
}

// EndpointChannelConfig是受信静态配对配置；消息不能新增主体、地址或密钥。
// public App只开放原request/receipt；Delivery须另接原业务receiver/proof端口。
type EndpointChannelConfig struct {
	GatewayInstanceID     string                     `json:"gateway_instance_id"`
	ApplicationInstanceID string                     `json:"application_instance_id"`
	ApplicationAddresses  []string                   `json:"application_addresses"`
	GatewayIdentities     []string                   `json:"gateway_identities"`
	Registrations         []rpc.EndpointRegistration `json:"registrations"`
	GatewayTLS            EndpointTLSFiles           `json:"gateway_tls"`
	ApplicationTLS        EndpointTLSFiles           `json:"application_tls"`
	GatewayClientTLS      EndpointTLSFiles           `json:"gateway_client_tls"`
}

func validateEndpointChannels(c Config) error {
	p := c.EndpointChannels
	if p == nil {
		return nil
	}
	if c.Driver != "postgres" || !api.ValidID(p.GatewayInstanceID) || !api.ValidID(p.ApplicationInstanceID) || len(p.ApplicationAddresses) < 1 || len(p.ApplicationAddresses) > 2 || len(p.GatewayIdentities) < 1 || len(p.GatewayIdentities) > 2 || len(p.Registrations) < 1 || len(p.Registrations) > 2 {
		return api.E("unsupported", "static_endpoint_channel_configuration_required")
	}
	seen := map[string]bool{}
	for _, address := range p.ApplicationAddresses {
		u, err := url.Parse(address)
		if err != nil || u.Scheme != "grpcs" || u.Hostname() == "" || u.Port() == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || seen[address] {
			return api.E("forbidden", "static_endpoint_mtls_address_required")
		}
		seen[address] = true
	}
	seen = map[string]bool{}
	for _, identity := range p.GatewayIdentities {
		u, err := url.Parse(identity)
		if err != nil || u.Scheme != "spiffe" || u.Host == "" || u.Path == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || seen[identity] {
			return api.E("forbidden", "registered_gateway_identity_required")
		}
		seen[identity] = true
	}
	seen = map[string]bool{}
	for _, registration := range p.Registrations {
		if registration.TenantID != c.TenantID || registration.SubjectID != c.SubjectID || registration.CredentialGeneration != 1 || registration.RecipientServiceID != c.OwnerID || !api.ValidID(registration.EndpointID) || !api.ValidID(registration.InstanceID) || registration.Generation == 0 || seen[registration.SubjectID] {
			return api.E("forbidden", "original_endpoint_subject_pairing_required")
		}
		seen[registration.SubjectID] = true
	}
	return nil
}

func endpointTLS(files EndpointTLSFiles, server, mutual bool) (*tls.Config, error) {
	if !filepath.IsAbs(files.CertificateFile) || !filepath.IsAbs(files.KeyFile) || mutual && !filepath.IsAbs(files.CAFile) {
		return nil, api.E("forbidden", "explicit_endpoint_tls_files_required")
	}
	info, err := os.Stat(files.KeyFile)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return nil, api.E("forbidden", "private_endpoint_tls_key_required")
	}
	certificate, err := tls.LoadX509KeyPair(files.CertificateFile, files.KeyFile)
	if err != nil {
		return nil, api.E("forbidden", "endpoint_tls_certificate_unavailable")
	}
	config := &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{certificate}}
	if mutual {
		pem, err := os.ReadFile(files.CAFile)
		if err != nil {
			return nil, api.E("forbidden", "endpoint_tls_ca_unavailable")
		}
		roots := x509.NewCertPool()
		if !roots.AppendCertsFromPEM(pem) {
			return nil, api.E("forbidden", "endpoint_tls_ca_invalid")
		}
		if server {
			config.ClientCAs = roots
			config.ClientAuth = tls.RequireAndVerifyClientCert
		} else {
			config.RootCAs = roots
		}
	}
	return config, nil
}

func (a *App) configureEndpointChannels() error {
	if err := validateEndpointChannels(a.Config); err != nil {
		return err
	}
	p := a.Config.EndpointChannels
	if p == nil || a.Role != "gateway" && a.Role != "application" {
		return nil
	}
	methods := a.Registry.Contracts()
	digest, err := api.DigestLimit(methods, 1<<20)
	if err != nil {
		return err
	}
	pairs := make([]rpc.StaticEndpointPair, len(p.Registrations))
	for i, registration := range p.Registrations {
		pairs[i] = rpc.StaticEndpointPair{Registration: registration, Methods: methods}
	}
	// 没有原业务Delivery receiver/proof时保持关闭，不造第二份业务决定。
	a.endpointAuthority, err = rpc.NewStaticEndpointAuthority(rpc.StaticEndpointAuthorityConfig{OwnerID: a.Scope.OwnerID, GatewayIdentities: append([]string{}, p.GatewayIdentities...), Identity: a.Identity, Pairs: pairs})
	if err != nil {
		return err
	}
	if a.Role == "application" {
		a.endpointServerTLS, err = endpointTLS(p.ApplicationTLS, true, true)
		return err
	}
	a.endpointServerTLS, err = endpointTLS(p.GatewayTLS, true, false)
	if err != nil {
		return err
	}
	clientTLS, err := endpointTLS(p.GatewayClientTLS, false, true)
	if err != nil {
		return err
	}
	applications := make([]endpointchannel.Application, len(p.ApplicationAddresses))
	for i, address := range p.ApplicationAddresses {
		applications[i] = endpointchannel.Application{Address: address, ClientTLS: clientTLS}
	}
	fallback := &endpointUnaryForward{a: a, application: applications[0], methods: methods, digest: digest}
	a.endpointRouter, err = endpointchannel.NewRouter(endpointchannel.Config{OwnerID: a.Scope.OwnerID, GatewayInstanceID: p.GatewayInstanceID, MethodsDigest: digest, Applications: applications, Registrations: p.Registrations, Identity: a.Identity, Credentials: a.forwardCredential, Fallback: fallback})
	return err
}

type endpointUnaryForward struct {
	a           *App
	application endpointchannel.Application
	methods     []api.MethodContract
	digest      string
}

func (p *endpointUnaryForward) Call(ctx context.Context, auth runtime.Auth, kind string, raw json.RawMessage) (string, json.RawMessage, error) {
	resultKind := "query_result"
	switch kind {
	case "command", "receipt_lookup":
		resultKind = "receipt"
	case "query":
	default:
		return "", nil, api.E("unsupported", "frame_kind_not_supported")
	}
	token, err := p.a.forwardCredential(ctx, auth)
	if err != nil {
		return "", nil, err
	}
	scope, err := api.Digest(auth)
	if err != nil {
		return "", nil, err
	}
	manifest := harness.Discovery{Protocol: api.Protocol, Profile: api.Profile, SchemaDigest: api.CoreDigest(), LogicalServiceID: p.a.Scope.OwnerID, IdentityScope: scope, IdentityRevision: auth.CredentialGeneration, Methods: p.methods, MethodsDigest: p.digest}
	transport, err := harness.DialGRPC(ctx, p.application.Address, token, manifest, p.application.ClientTLS, false)
	if err != nil {
		return "", nil, err
	}
	body, err := transport.Call(ctx, kind, raw)
	return resultKind, body, errors.Join(err, transport.Close())
}
