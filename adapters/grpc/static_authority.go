package grpc

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/ruipengliu/lerna/adapters/platform"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/runtime"
	"github.com/ruipengliu/lerna/sdk/go/grpcwire"
)

type DeliveryProofReader interface {
	ReadDeliveryProof(context.Context, api.ContentRef) ([]byte, error)
}
type DeliveryReplyReceiver interface {
	ReceiveDeliveryReply(context.Context, EndpointRegistration, grpcwire.Delivery, grpcwire.Reply) (bool, error)
}
type StaticEndpointPair struct {
	Registration EndpointRegistration
	// Methods 是原已配对recipient的固定准确合同，供回执/查询结果核验。
	Methods []api.MethodContract
}
type StaticEndpointAuthorityConfig struct {
	OwnerID           string
	GatewayIdentities []string
	Identity          platform.IdentityProvider
	Pairs             []StaticEndpointPair
	Keys              *platform.Keyring
	Proofs            DeliveryProofReader
	Replies           DeliveryReplyReceiver
}
type staticPair struct {
	registration    EndpointRegistration
	auth            *runtime.Auth
	inputs, outputs map[string]*api.Validator
	kinds           map[string]string
}
type StaticEndpointAuthority struct {
	cfg            StaticEndpointAuthorityConfig
	mu             sync.RWMutex
	pairs          map[string]staticPair
	errorValidator *api.Validator
}

func endpointPairKey(r EndpointRegistration) string {
	return r.TenantID + "/" + r.EndpointID + "/" + r.InstanceID
}
func NewStaticEndpointAuthority(cfg StaticEndpointAuthorityConfig) (*StaticEndpointAuthority, error) {
	if !api.ValidID(cfg.OwnerID) || cfg.Identity == nil || len(cfg.GatewayIdentities) == 0 || len(cfg.GatewayIdentities) > 16 || len(cfg.Pairs) == 0 || len(cfg.Pairs) > 16 {
		return nil, api.E("unsupported", "static_endpoint_authority_unconfigured")
	}
	s := &StaticEndpointAuthority{cfg: cfg, pairs: map[string]staticPair{}}
	var err error
	s.errorValidator, err = api.NewValidator(api.SchemaFor[api.Error]())
	if err != nil {
		return nil, err
	}
	for _, pair := range cfg.Pairs {
		r := pair.Registration
		if !api.ValidID(r.TenantID) || !api.ValidID(r.SubjectID) || !api.ValidID(r.EndpointID) || !api.ValidID(r.InstanceID) || !api.ValidID(r.RecipientServiceID) || r.CredentialGeneration == 0 || r.Generation == 0 {
			return nil, api.E("invalid_request", "invalid_static_endpoint_pair")
		}
		p := staticPair{registration: r, inputs: map[string]*api.Validator{}, outputs: map[string]*api.Validator{}, kinds: map[string]string{}}
		if len(pair.Methods) > 256 {
			return nil, api.E("overloaded", "endpoint_method_contract_limit")
		}
		for _, method := range pair.Methods {
			digest, err := api.Digest([]any{method.InputSchema, method.OutputSchema})
			if err != nil || digest != method.SchemaDigest || p.kinds[method.Name] != "" || method.Kind != "command" && method.Kind != "query" {
				return nil, api.E("unsupported", "endpoint_method_contract_mismatch")
			}
			p.inputs[method.Name], err = api.NewValidator(method.InputSchema)
			if err != nil {
				return nil, err
			}
			p.outputs[method.Name], err = api.NewValidator(method.OutputSchema)
			if err != nil {
				return nil, err
			}
			p.kinds[method.Name] = method.Kind
		}
		key := endpointPairKey(r)
		if _, exists := s.pairs[key]; exists {
			return nil, api.E("idempotency_conflict", "duplicate_static_endpoint_pair")
		}
		s.pairs[key] = p
	}
	return s, nil
}
func (s *StaticEndpointAuthority) pair(r EndpointRegistration) (staticPair, error) {
	s.mu.RLock()
	p, ok := s.pairs[endpointPairKey(r)]
	s.mu.RUnlock()
	if !ok || !api.Equal(p.registration, r) {
		return p, api.E("forbidden", "endpoint_pairing_changed")
	}
	return p, nil
}
func (s *StaticEndpointAuthority) Bind(ctx context.Context, gateway string, a runtime.Auth, b grpcwire.Bind) (EndpointRegistration, error) {
	allowed := false
	for _, identity := range s.cfg.GatewayIdentities {
		if identity == gateway {
			allowed = true
		}
	}
	if !allowed || b.LogicalServiceID != s.cfg.OwnerID {
		return EndpointRegistration{}, api.E("forbidden", "gateway_pairing_mismatch")
	}
	if err := s.cfg.Identity.CheckCurrent(ctx, a); err != nil {
		return EndpointRegistration{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, p := range s.pairs {
		r := p.registration
		if r.TenantID == a.TenantID && r.SubjectID == a.SubjectID && r.CredentialGeneration == a.CredentialGeneration && r.EndpointID == b.EndpointID && r.InstanceID == b.InstanceID && r.Generation == b.EndpointGeneration {
			if p.auth != nil && !api.Equal(*p.auth, a) {
				return EndpointRegistration{}, api.E("forbidden", "endpoint_original_auth_changed")
			}
			copy := a
			copy.Roles = append([]string{}, a.Roles...)
			p.auth = &copy
			s.pairs[key] = p
			return r, nil
		}
	}
	return EndpointRegistration{}, api.E("forbidden", "endpoint_pairing_mismatch")
}
func (s *StaticEndpointAuthority) Check(ctx context.Context, r EndpointRegistration) error {
	p, err := s.pair(r)
	if err != nil {
		return err
	}
	if p.auth == nil {
		return api.E("forbidden", "endpoint_original_auth_not_bound")
	}
	return s.cfg.Identity.CheckCurrent(ctx, *p.auth)
}

// RevokePair 是受信管理端口；普通Bearer/Channel帧没有此入口。
func (s *StaticEndpointAuthority) RevokePair(r EndpointRegistration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.pairs[endpointPairKey(r)]
	if !ok || !api.Equal(p.registration, r) {
		return api.E("revision_conflict", "endpoint_pairing_changed")
	}
	delete(s.pairs, endpointPairKey(r))
	return nil
}
func DeliveryIntentDigest(d grpcwire.Delivery) (string, error) {
	d.ProofRef = api.ContentRef{}
	return api.Digest(d)
}
func DeliveryProofClaims(r EndpointRegistration, d grpcwire.Delivery, digest, issuedAt string) platform.ProofClaims {
	return platform.ProofClaims{TenantID: r.TenantID, Issuer: d.SenderServiceID, Audience: r.EndpointID, Purpose: "delivery", ObjectRef: api.ObjectRef{TenantID: r.TenantID, OwnerID: d.SenderServiceID, ObjectID: d.DeliveryID, Revision: 1}, Digest: digest, ControlRevision: r.Generation, WindowID: d.DeliveryID, IssuedAt: issuedAt, StartBefore: d.DeliverBefore}
}
func (s *StaticEndpointAuthority) VerifyDelivery(ctx context.Context, r EndpointRegistration, d grpcwire.Delivery) error {
	if err := s.Check(ctx, r); err != nil {
		return err
	}
	p, err := s.pair(r)
	if err != nil {
		return err
	}
	if s.cfg.Keys == nil || s.cfg.Proofs == nil || s.cfg.Replies == nil || len(p.kinds) == 0 {
		return api.E("unsupported", "endpoint_delivery_unconfigured")
	}
	if _, err = grpcwire.DecodeFrame(api.Raw(d)); err != nil {
		return err
	}
	if d.SenderServiceID != s.cfg.OwnerID || d.RecipientEndpointID != r.EndpointID || d.RecipientInstanceID != r.InstanceID || d.ProofRef.TenantID != r.TenantID || d.ProofRef.OwnerID != d.SenderServiceID || d.ProofRef.MediaType != "application/jose" || d.ProofRef.ByteLength > 16384 {
		return api.E("forbidden", "delivery_scope_mismatch")
	}
	if err = transportPayload(r.RecipientServiceID, d.Kind, d.Request); err != nil {
		return err
	}
	if d.Kind != "receipt_lookup" {
		var header struct {
			Method  string          `json:"method"`
			Payload json.RawMessage `json:"payload"`
		}
		v, e := api.ParseJSON(d.Request)
		if e != nil {
			return e
		}
		o := v.(map[string]any)
		header.Method, _ = o["method"].(string)
		header.Payload = api.Raw(o["payload"])
		if p.kinds[header.Method] != d.Kind {
			return api.E("unsupported", "endpoint_delivery_method_unconfigured")
		}
		if err = p.inputs[header.Method].Validate(header.Payload); err != nil {
			return err
		}
	}
	bytes, err := s.cfg.Proofs.ReadDeliveryProof(ctx, d.ProofRef)
	if err != nil {
		return err
	}
	if api.Hash(bytes) != d.ProofRef.Hash || uint64(len(bytes)) != d.ProofRef.ByteLength {
		return api.E("forbidden", "delivery_proof_bytes_mismatch")
	}
	digest, err := DeliveryIntentDigest(d)
	if err != nil {
		return err
	}
	_, err = s.cfg.Keys.Verify(string(bytes), DeliveryProofClaims(r, d, digest, ""), time.Now())
	return err
}
func (s *StaticEndpointAuthority) ReceiveReply(ctx context.Context, r EndpointRegistration, d grpcwire.Delivery, reply grpcwire.Reply) (bool, error) {
	if err := s.Check(ctx, r); err != nil {
		return false, err
	}
	if err := grpcwire.ValidateReply(d, reply); err != nil {
		return false, err
	}
	p, err := s.pair(r)
	if err != nil {
		return false, err
	}
	if s.cfg.Replies == nil {
		return false, api.E("unsupported", "endpoint_reply_owner_unconfigured")
	}
	if reply.ResultKind == "error" {
		if err = s.errorValidator.Validate(reply.Payload); err != nil {
			return false, err
		}
	} else if d.Kind != "receipt_lookup" {
		v, e := api.ParseJSON(d.Request)
		if e != nil {
			return false, e
		}
		o := v.(map[string]any)
		method, _ := o["method"].(string)
		output, ok := p.outputs[method]
		if !ok {
			return false, api.E("unsupported", "endpoint_reply_decoder_unconfigured")
		}
		body := reply.Payload
		if reply.ResultKind == "receipt" {
			var receipt api.Receipt
			if err = api.Decode(body, &receipt); err != nil {
				return false, err
			}
			if receipt.Stage == "rejected" {
				return s.cfg.Replies.ReceiveDeliveryReply(ctx, r, d, reply)
			}
			body = receipt.Output
		}
		if err = output.Validate(body); err != nil {
			return false, err
		}
	}
	return s.cfg.Replies.ReceiveDeliveryReply(ctx, r, d, reply)
}
