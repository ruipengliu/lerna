// Package endpointchannel binds an original external WSS connection to a
// bounded, statically paired application channel without changing its owner.
package endpointchannel

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"net/url"
	"sync"
	"time"

	transport "github.com/ruipengliu/lerna/adapters/grpc"
	"github.com/ruipengliu/lerna/adapters/platform"
	"github.com/ruipengliu/lerna/adapters/wss"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/sdk/go/grpcwire"
)

type Application struct {
	Address   string
	ClientTLS *tls.Config
}
type DeliveryVerifier interface {
	VerifyDelivery(context.Context, transport.EndpointRegistration, grpcwire.Delivery) error
}
type Config struct {
	OwnerID           string
	GatewayInstanceID string
	MethodsDigest     string
	Applications      []Application
	Registrations     []transport.EndpointRegistration
	Identity          platform.IdentityProvider
	Credentials       transport.CredentialSource
	Fallback          wss.Processor
	DeliveryProofs    DeliveryVerifier
}
type Router struct {
	cfg            Config
	mu             sync.Mutex
	closed         bool
	connections    map[string]*Connection
	identityCounts map[string]int
	tenantBytes    map[string]int
	queuedBytes    int
}
type State struct {
	ConnectionID        string `json:"connection_id"`
	EndpointID          string `json:"endpoint_id"`
	EndpointInstanceID  string `json:"endpoint_instance_id"`
	EndpointGeneration  uint64 `json:"endpoint_generation"`
	GatewayInstanceID   string `json:"gateway_instance_id"`
	MethodsDigest       string `json:"methods_digest"`
	BindingID           string `json:"binding_id"`
	BindingRevision     uint64 `json:"binding_revision"`
	LastRequestSeq      uint64 `json:"last_request_seq"`
	Connected           bool   `json:"connected"`
	Pending             uint64 `json:"pending"`
	DiscardedOldOutputs uint64 `json:"discarded_old_outputs"`
}

func NewRouter(cfg Config) (*Router, error) {
	if !api.ValidID(cfg.OwnerID) || !api.ValidID(cfg.GatewayInstanceID) || len(cfg.MethodsDigest) != 71 || cfg.Identity == nil || cfg.Credentials == nil || len(cfg.Applications) < 1 || len(cfg.Applications) > 2 || len(cfg.Registrations) < 1 || len(cfg.Registrations) > 16 {
		return nil, api.E("unsupported", "endpoint_channel_route_unconfigured")
	}
	apps := make([]Application, len(cfg.Applications))
	for i, app := range cfg.Applications {
		u, err := url.Parse(app.Address)
		if err != nil || u.Scheme != "grpcs" || u.Host == "" || u.Port() == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || app.ClientTLS == nil || app.ClientTLS.InsecureSkipVerify || app.ClientTLS.RootCAs == nil || len(app.ClientTLS.Certificates) == 0 {
			return nil, api.E("forbidden", "endpoint_channel_mtls_required")
		}
		apps[i] = Application{Address: app.Address, ClientTLS: app.ClientTLS.Clone()}
		apps[i].ClientTLS.MinVersion = tls.VersionTLS13
	}
	cfg.Applications = apps
	cfg.Registrations = append([]transport.EndpointRegistration{}, cfg.Registrations...)
	seen := map[string]bool{}
	for _, r := range cfg.Registrations {
		key := r.TenantID + "/" + r.SubjectID
		if !api.ValidID(r.TenantID) || !api.ValidID(r.SubjectID) || !api.ValidID(r.EndpointID) || !api.ValidID(r.InstanceID) || !api.ValidID(r.RecipientServiceID) || r.Generation == 0 || r.CredentialGeneration == 0 || seen[key] {
			return nil, api.E("invalid_request", "invalid_endpoint_channel_registration")
		}
		seen[key] = true
	}
	return &Router{cfg: cfg, connections: map[string]*Connection{}, identityCounts: map[string]int{}, tenantBytes: map[string]int{}}, nil
}
func (r *Router) Call(ctx context.Context, a runtimeAuth, kind string, raw json.RawMessage) (string, json.RawMessage, error) {
	if r.cfg.Fallback == nil {
		return "", nil, api.E("unsupported", "external_connection_required")
	}
	return r.cfg.Fallback.Call(ctx, a, kind, raw)
}
func (r *Router) Open(ctx context.Context, a runtimeAuth, info wss.ConnectionInfo) (wss.Connection, error) {
	if info.OwnerID != r.cfg.OwnerID || info.MethodsDigest != r.cfg.MethodsDigest || !api.ValidID(info.ConnectionID) || info.Emit == nil || info.EmitChecked == nil || info.Stop == nil {
		return nil, api.E("unsupported", "external_connection_contract_mismatch")
	}
	var registration transport.EndpointRegistration
	for _, candidate := range r.cfg.Registrations {
		if candidate.TenantID == a.TenantID && candidate.SubjectID == a.SubjectID && candidate.CredentialGeneration == a.CredentialGeneration {
			registration = candidate
			break
		}
	}
	if registration.EndpointID == "" {
		return nil, api.E("forbidden", "endpoint_pairing_required")
	}
	if err := r.cfg.Identity.CheckCurrent(ctx, a); err != nil {
		return nil, err
	}
	key := a.TenantID + "/" + a.SubjectID
	r.mu.Lock()
	if r.closed || len(r.connections) >= 16 || r.identityCounts[key] >= 2 {
		r.mu.Unlock()
		return nil, api.E("overloaded", "endpoint_channel_connection_limit")
	}
	if r.connections[info.ConnectionID] != nil {
		r.mu.Unlock()
		return nil, api.E("idempotency_conflict", "external_connection_already_bound")
	}
	c := newConnection(r, ctx, a, registration, info)
	r.connections[info.ConnectionID] = c
	r.identityCounts[key]++
	r.mu.Unlock()
	setup, stop := context.WithTimeout(ctx, 5*time.Second)
	err := c.connect(setup)
	stop()
	if err != nil {
		return nil, errors.Join(err, c.Close())
	}
	c.workers.Add(1)
	go c.rebindLoop()
	return c, nil
}
func (r *Router) reserveBytes(tenant string, n int) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed || r.queuedBytes+n > 64<<20 || r.tenantBytes[tenant]+n > 32<<20 {
		return false
	}
	r.queuedBytes += n
	r.tenantBytes[tenant] += n
	return true
}
func (r *Router) releaseBytes(tenant string, n int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.queuedBytes -= n
	r.tenantBytes[tenant] -= n
	if r.tenantBytes[tenant] == 0 {
		delete(r.tenantBytes, tenant)
	}
}
func (r *Router) States() []State {
	r.mu.Lock()
	connections := make([]*Connection, 0, len(r.connections))
	for _, c := range r.connections {
		connections = append(connections, c)
	}
	r.mu.Unlock()
	states := make([]State, 0, len(connections))
	for _, c := range connections {
		states = append(states, c.State())
	}
	return states
}
func (r *Router) Close() error {
	r.mu.Lock()
	r.closed = true
	connections := make([]*Connection, 0, len(r.connections))
	for _, c := range r.connections {
		connections = append(connections, c)
	}
	r.mu.Unlock()
	var err error
	for _, c := range connections {
		c.info.Stop()
		err = errors.Join(err, c.Close())
	}
	return err
}
