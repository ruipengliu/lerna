package harness

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"strings"
	"time"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/sdk/go/grpcwire"
)

type endpointMethod struct {
	contract      api.MethodContract
	input, output *api.Validator
}
type endpointProofClaims struct {
	TenantID        string        `json:"tenant_id"`
	Issuer          string        `json:"issuer"`
	Audience        string        `json:"audience"`
	Purpose         string        `json:"purpose"`
	ObjectRef       api.ObjectRef `json:"object_ref"`
	Digest          string        `json:"digest"`
	ControlRevision uint64        `json:"control_revision"`
	WindowID        string        `json:"window_id"`
	IssuedAt        string        `json:"issued_at"`
	StartBefore     string        `json:"start_before"`
}

func newWSEndpoint(cfg EndpointConfig, expected Discovery) (*wsEndpoint, error) {
	if !api.ValidID(cfg.TenantID) || !api.ValidID(cfg.IssuerServiceID) || !api.ValidID(cfg.RecipientServiceID) || !api.ValidID(cfg.EndpointID) || !api.ValidID(cfg.InstanceID) || cfg.Generation == 0 || cfg.IdentityScope == "" || cfg.IdentityScope != expected.IdentityScope || cfg.IdentityRevision == 0 || cfg.IdentityRevision != expected.IdentityRevision || cfg.Profile != api.Profile || cfg.Profile != expected.Profile || cfg.SchemaDigest != api.CoreDigest() || cfg.SchemaDigest != expected.SchemaDigest || cfg.IssuerServiceID != expected.LogicalServiceID || cfg.Current == nil || cfg.Proofs == nil || cfg.Receiver == nil || cfg.Journal == nil || len(cfg.Keys) == 0 || len(cfg.Keys) > 16 || len(cfg.Methods) == 0 || len(cfg.Methods) > 256 {
		return nil, api.E("unsupported", "endpoint_receiver_not_configured")
	}
	j := cfg.Journal
	if j.scope != cfg.IdentityScope || j.endpoint != cfg.EndpointID || j.instance != cfg.InstanceID || j.generation != cfg.Generation {
		return nil, api.E("forbidden", "endpoint_journal_scope_mismatch")
	}
	e := &wsEndpoint{cfg: cfg, methods: map[string]endpointMethod{}, pending: map[string]grpcwire.Delivery{}, normal: make(chan grpcwire.Delivery, 28), control: make(chan grpcwire.Delivery, 4)}
	e.cfg.Keys = map[string]*ecdsa.PublicKey{}
	for id, key := range cfg.Keys {
		if id == "" || len(id) > 128 || key == nil || key.Curve != elliptic.P256() || key.X == nil || key.Y == nil || !key.Curve.IsOnCurve(key.X, key.Y) {
			return nil, api.E("forbidden", "endpoint_key_unregistered")
		}
		e.cfg.Keys[id] = &ecdsa.PublicKey{Curve: elliptic.P256(), X: new(big.Int).Set(key.X), Y: new(big.Int).Set(key.Y)}
	}
	for _, m := range cfg.Methods {
		digest, err := api.Digest([]any{m.InputSchema, m.OutputSchema})
		if err != nil || digest != m.SchemaDigest || m.Kind != "command" && m.Kind != "query" || e.methods[m.Name].contract.Name != "" || m.Name == "" {
			return nil, api.E("unsupported", "endpoint_method_schema_mismatch")
		}
		in, err := api.NewValidator(m.InputSchema)
		if err != nil {
			return nil, err
		}
		out, err := api.NewValidator(m.OutputSchema)
		if err != nil {
			return nil, err
		}
		e.methods[m.Name] = endpointMethod{m, in, out}
	}
	var err error
	e.errorValidator, err = api.NewValidator(api.SchemaFor[api.Error]())
	if err != nil {
		return nil, err
	}
	// 已知配置先验证旧责任；不对未知schema/profile另猜decoder或改原输入。
	entries, partial, err := j.Incomplete(context.Background(), 32)
	if err != nil {
		return nil, err
	}
	if partial {
		return nil, api.E("overloaded", "endpoint_recovery_page_limit")
	}
	for _, entry := range entries {
		if err = e.checkEntry(entry); err != nil {
			return nil, err
		}
	}
	e.recovered = entries
	return e, nil
}

func (e *wsEndpoint) invocation(d grpcwire.Delivery) (EndpointInvocation, error) {
	if _, err := grpcwire.DecodeFrame(api.Raw(d)); err != nil {
		return EndpointInvocation{}, err
	}
	if d.SenderServiceID != e.cfg.IssuerServiceID || d.RecipientEndpointID != e.cfg.EndpointID || d.RecipientInstanceID != e.cfg.InstanceID || d.ProofRef.TenantID != e.cfg.TenantID || d.ProofRef.OwnerID != e.cfg.IssuerServiceID || d.ProofRef.MediaType != "application/jose" || d.ProofRef.ByteLength == 0 || d.ProofRef.ByteLength > 16384 {
		return EndpointInvocation{}, api.E("forbidden", "endpoint_delivery_scope_mismatch")
	}
	var method string
	var payload json.RawMessage
	var owner, profile, protocol string
	switch d.Kind {
	case "command":
		var c api.Command
		if err := api.Decode(d.Request, &c); err != nil {
			return EndpointInvocation{}, err
		}
		if !api.ValidID(c.CommandID) || !api.ValidID(c.TargetID) {
			return EndpointInvocation{}, api.E("invalid_request", "endpoint_original_command_invalid")
		}
		if _, err := api.ParseTime(c.ExpiresAt); err != nil {
			return EndpointInvocation{}, err
		}
		method, payload, owner, profile, protocol = c.Method, c.Payload, c.LogicalServiceID, c.Profile, c.Protocol
	case "query":
		var q api.Query
		if err := api.Decode(d.Request, &q); err != nil {
			return EndpointInvocation{}, err
		}
		if !api.ValidID(q.QueryID) || !api.ValidID(q.TargetID) {
			return EndpointInvocation{}, api.E("invalid_request", "endpoint_original_query_invalid")
		}
		method, payload, owner, profile, protocol = q.Method, q.Payload, q.LogicalServiceID, q.Profile, q.Protocol
	case "receipt_lookup":
		var lookup api.ReceiptLookup
		if err := api.Decode(d.Request, &lookup); err != nil {
			return EndpointInvocation{}, err
		}
		if e.cfg.ReceiptDecoder == nil || !api.ValidID(lookup.CommandID) || lookup.LogicalServiceID != e.cfg.RecipientServiceID {
			return EndpointInvocation{}, api.E("unsupported", "endpoint_original_receipt_decoder_required")
		}
		return e.metadata(api.CoreDigest()), nil
	}
	if owner != e.cfg.RecipientServiceID || profile != e.cfg.Profile || protocol != api.Protocol {
		return EndpointInvocation{}, api.E("forbidden", "endpoint_recipient_profile_mismatch")
	}
	m, ok := e.methods[method]
	if !ok || m.contract.Kind != d.Kind {
		return EndpointInvocation{}, api.E("unsupported", "endpoint_method_not_registered")
	}
	if err := m.input.Validate(payload); err != nil {
		return EndpointInvocation{}, err
	}
	return e.metadata(m.contract.SchemaDigest), nil
}
func (e *wsEndpoint) metadata(methodDigest string) EndpointInvocation {
	return EndpointInvocation{IssuerServiceID: e.cfg.IssuerServiceID, RecipientServiceID: e.cfg.RecipientServiceID, Protocol: api.Protocol, Profile: e.cfg.Profile, SchemaDigest: e.cfg.SchemaDigest, MethodSchemaDigest: methodDigest, IdentityRevision: e.cfg.IdentityRevision}
}
func (e *wsEndpoint) checkEntry(entry ReplyEntry) error {
	meta, err := e.invocation(entry.Delivery)
	if err != nil {
		return err
	}
	if entry.Invocation != nil {
		meta.Sequence, meta.Phase = entry.Invocation.Sequence, entry.Invocation.Phase
		if !api.Equal(meta, *entry.Invocation) {
			return api.E("unsupported", "endpoint_original_decoder_binding_changed")
		}
	} else if entry.ReplyDigest == "" {
		return api.E("invalid_state", "endpoint_original_invocation_missing")
	}
	if entry.ReplyDigest != "" {
		return e.validateReply(context.Background(), entry.Delivery, entry.Reply)
	}
	return nil
}
func (e *wsEndpoint) verifyDelivery(ctx context.Context, d grpcwire.Delivery) error {
	if err := e.cfg.Current(ctx); err != nil {
		return err
	}
	if _, err := e.invocation(d); err != nil {
		return err
	}
	body, err := e.cfg.Proofs.ReadDeliveryProof(ctx, d.ProofRef)
	if err != nil {
		return err
	}
	if len(body) > 16384 || uint64(len(body)) != d.ProofRef.ByteLength || api.Hash(body) != d.ProofRef.Hash {
		return api.E("forbidden", "endpoint_proof_bytes_mismatch")
	}
	parts := strings.Split(string(body), ".")
	if len(parts) != 3 {
		return api.E("forbidden", "endpoint_jws_invalid")
	}
	header, err := base64.RawURLEncoding.DecodeString(parts[0])
	var h struct {
		Algorithm string `json:"alg"`
		KeyID     string `json:"kid"`
		Type      string `json:"typ"`
	}
	if err != nil || api.Decode(header, &h) != nil || h.Algorithm != "ES256" || h.Type != "JWS" {
		return api.E("forbidden", "endpoint_jws_algorithm_invalid")
	}
	key := e.cfg.Keys[h.KeyID]
	if key == nil {
		return api.E("forbidden", "endpoint_key_unregistered")
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || len(sig) != 64 {
		return api.E("forbidden", "endpoint_jws_signature_invalid")
	}
	hash := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if !ecdsa.Verify(key, hash[:], new(big.Int).SetBytes(sig[:32]), new(big.Int).SetBytes(sig[32:])) {
		return api.E("forbidden", "endpoint_jws_signature_invalid")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	var claims endpointProofClaims
	if err != nil || api.Decode(payload, &claims) != nil {
		return api.E("forbidden", "endpoint_jws_claims_invalid")
	}
	digestInput := d
	digestInput.ProofRef = api.ContentRef{}
	digest, err := api.Digest(digestInput)
	if err != nil {
		return err
	}
	ref := api.ObjectRef{TenantID: e.cfg.TenantID, OwnerID: e.cfg.IssuerServiceID, ObjectID: d.DeliveryID, Revision: 1}
	if claims.TenantID != e.cfg.TenantID || claims.Issuer != e.cfg.IssuerServiceID || claims.Audience != e.cfg.EndpointID || claims.Purpose != "delivery" || !api.Equal(claims.ObjectRef, ref) || claims.Digest != digest || claims.ControlRevision != e.cfg.Generation || claims.WindowID != d.DeliveryID || claims.StartBefore != d.DeliverBefore {
		return api.E("forbidden", "endpoint_proof_binding_mismatch")
	}
	issued, err := api.ParseTime(claims.IssuedAt)
	if err != nil {
		return err
	}
	until, err := api.ParseTime(claims.StartBefore)
	now := time.Now()
	if err != nil || !issued.Before(until) || issued.After(now) || !now.Before(until) {
		return api.E("expired", "endpoint_delivery_window_expired")
	}
	return nil
}
func (e *wsEndpoint) validateReply(ctx context.Context, d grpcwire.Delivery, reply grpcwire.Reply) error {
	if _, err := grpcwire.DecodeFrame(api.Raw(reply)); err != nil {
		return err
	}
	if err := grpcwire.ValidateReply(d, reply); err != nil {
		return err
	}
	if reply.ResultKind == "error" {
		return e.errorValidator.Validate(reply.Payload)
	}
	if d.Kind == "receipt_lookup" {
		var lookup api.ReceiptLookup
		var receipt api.Receipt
		if err := api.Decode(d.Request, &lookup); err != nil {
			return err
		}
		if err := api.Decode(reply.Payload, &receipt); err != nil {
			return err
		}
		return e.cfg.ReceiptDecoder(ctx, lookup, receipt)
	}
	v, err := api.ParseJSON(d.Request)
	if err != nil {
		return err
	}
	o := v.(map[string]any)
	method, _ := o["method"].(string)
	body := reply.Payload
	if reply.ResultKind == "receipt" {
		var receipt api.Receipt
		if err := api.Decode(body, &receipt); err != nil {
			return err
		}
		if receipt.Stage == "rejected" {
			return e.errorValidator.Validate(api.Raw(receipt.Error))
		}
		body = receipt.Output
	}
	return e.methods[method].output.Validate(body)
}
