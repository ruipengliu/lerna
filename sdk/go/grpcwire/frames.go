package grpcwire

import (
	"encoding/json"
	"sync"

	"github.com/ruipengliu/lerna/api"
)

const EndpointProfile = "harness-grpc-endpoint/1"

type Bind struct {
	Type               string `json:"type"`
	LogicalServiceID   string `json:"logical_service_id"`
	ConnectionID       string `json:"connection_id"`
	GatewayInstanceID  string `json:"gateway_instance_id"`
	EndpointID         string `json:"endpoint_id"`
	InstanceID         string `json:"instance_id"`
	EndpointGeneration uint64 `json:"endpoint_generation"`
	Protocol           string `json:"protocol"`
	Profile            string `json:"profile"`
	TransportProfile   string `json:"transport_profile"`
	MethodsDigest      string `json:"methods_digest"`
}
type ChannelLimits struct {
	MaxDomainBytes         uint64 `json:"max_domain_bytes"`
	MaxFrameBytes          uint64 `json:"max_frame_bytes"`
	MaxPending             uint64 `json:"max_pending"`
	MaxQueuedBytes         uint64 `json:"max_queued_bytes"`
	ControlReservedBytes   uint64 `json:"control_reserved_bytes"`
	ControlReservedItems   uint64 `json:"control_reserved_items"`
	MaxConnections         uint64 `json:"max_connections"`
	MaxIdentityConnections uint64 `json:"max_identity_connections"`
	MaxTenantQueuedBytes   uint64 `json:"max_tenant_queued_bytes"`
	MaxGlobalQueuedBytes   uint64 `json:"max_global_queued_bytes"`
}

func Limits() ChannelLimits {
	return ChannelLimits{api.MaxJSONBytes, MaxFrameBytes, 32, 4 << 20, 1 << 20, 4, 16, 2, 32 << 20, 64 << 20}
}

type Ready struct {
	Bind
	Limits ChannelLimits `json:"limits"`
}
type Request struct {
	Type       string          `json:"type"`
	RequestSeq uint64          `json:"request_seq"`
	Kind       string          `json:"kind"`
	Payload    json.RawMessage `json:"payload"`
}
type Response struct {
	Type       string          `json:"type"`
	RequestSeq uint64          `json:"request_seq"`
	ResultKind string          `json:"result_kind"`
	Payload    json.RawMessage `json:"payload"`
}
type Delivery struct {
	Type                string          `json:"type"`
	DeliveryID          string          `json:"delivery_id"`
	SenderServiceID     string          `json:"sender_service_id"`
	RecipientEndpointID string          `json:"recipient_endpoint_id"`
	RecipientInstanceID string          `json:"recipient_instance_id"`
	RequestDigest       string          `json:"request_digest"`
	Kind                string          `json:"kind"`
	Request             json.RawMessage `json:"request"`
	DeliverBefore       string          `json:"deliver_before"`
	ProofRef            api.ContentRef  `json:"proof_ref"`
}
type Reply struct {
	Type          string          `json:"type"`
	DeliveryID    string          `json:"delivery_id"`
	RequestDigest string          `json:"request_digest"`
	ResultKind    string          `json:"result_kind"`
	Payload       json.RawMessage `json:"payload"`
}
type ReplyAck struct {
	Type          string `json:"type"`
	DeliveryID    string `json:"delivery_id"`
	RequestDigest string `json:"request_digest"`
	Stored        bool   `json:"stored"`
}
type Heartbeat struct {
	Type  string `json:"type"`
	Nonce string `json:"nonce"`
}

var validators sync.Map

func frameValidator(kind string) (*api.Validator, error) {
	if v, ok := validators.Load(kind); ok {
		return v.(*api.Validator), nil
	}
	var schema api.Schema
	switch kind {
	case "bind":
		schema = api.SchemaFor[Bind]()
	case "ready":
		schema = api.SchemaFor[Ready]()
	case "request":
		schema = api.SchemaFor[Request]()
	case "response":
		schema = api.SchemaFor[Response]()
	case "delivery":
		schema = api.SchemaFor[Delivery]()
	case "reply":
		schema = api.SchemaFor[Reply]()
	case "reply_ack":
		schema = api.SchemaFor[ReplyAck]()
	case "ping", "pong":
		schema = api.SchemaFor[Heartbeat]()
	default:
		return nil, api.E("unsupported", "channel_frame_not_supported")
	}
	schema["properties"].(map[string]any)["type"] = api.Schema{"const": kind}
	v, e := api.NewValidator(schema)
	if e != nil {
		return nil, e
	}
	actual, _ := validators.LoadOrStore(kind, v)
	return actual.(*api.Validator), nil
}

// DecodeFrame 先检查原 JSON，再按唯一 type 的闭合结构检查，保留 payload 准确字节。
func DecodeFrame(b []byte) (any, error) {
	v, e := api.ParseJSONLimit(b, MaxFrameBytes)
	if e != nil {
		return nil, e
	}
	o, ok := v.(map[string]any)
	if !ok {
		return nil, api.E("invalid_request", "invalid_channel_frame")
	}
	kind, _ := o["type"].(string)
	validator, e := frameValidator(kind)
	if e != nil {
		return nil, e
	}
	// 帧头闭合验证与领域 payload 的256KiB检查分开，允许准确领域JSON加帧头。
	header := make(map[string]any, len(o))
	for k, value := range o {
		header[k] = value
	}
	for _, k := range []string{"payload", "request"} {
		if _, present := header[k]; present {
			header[k] = map[string]any{}
		}
	}
	if e = validator.Validate(api.Raw(header)); e != nil {
		return nil, e
	}
	var frame any
	switch kind {
	case "bind":
		frame = &Bind{}
	case "ready":
		frame = &Ready{}
	case "request":
		frame = &Request{}
	case "response":
		frame = &Response{}
	case "delivery":
		frame = &Delivery{}
	case "reply":
		frame = &Reply{}
	case "reply_ack":
		frame = &ReplyAck{}
	case "ping", "pong":
		frame = &Heartbeat{}
	}
	if e = api.DecodeLimit(b, frame, MaxFrameBytes); e != nil {
		return nil, e
	}
	switch f := frame.(type) {
	case *Bind:
		e = checkBind(*f)
	case *Ready:
		e = checkBind(f.Bind)
		if e == nil && !api.Equal(f.Limits, Limits()) {
			e = api.E("unsupported", "channel_limits_mismatch")
		}
	case *Request:
		if f.RequestSeq == 0 || !validKind(f.Kind) {
			e = api.E("invalid_request", "invalid_channel_request")
		}
		if e == nil {
			_, e = api.ParseJSON(f.Payload)
		}
	case *Response:
		if f.RequestSeq == 0 || !validResult(f.ResultKind) {
			e = api.E("invalid_request", "invalid_channel_response")
		}
		if e == nil {
			_, e = api.ParseJSON(f.Payload)
		}
	case *Delivery:
		if !api.ValidID(f.DeliveryID) || !api.ValidID(f.SenderServiceID) || !api.ValidID(f.RecipientEndpointID) || !api.ValidID(f.RecipientInstanceID) || !validKind(f.Kind) {
			e = api.E("invalid_request", "invalid_delivery")
		}
		if e == nil {
			canonical, err := api.Canonical(f.Request)
			e = err
			if e == nil && api.Hash(canonical) != f.RequestDigest {
				e = api.E("idempotency_conflict", "delivery_digest_mismatch")
			}
		}
		if e == nil {
			_, e = api.ParseTime(f.DeliverBefore)
		}
	case *Reply:
		if !api.ValidID(f.DeliveryID) || len(f.RequestDigest) != 71 || !validResult(f.ResultKind) {
			e = api.E("invalid_request", "invalid_delivery_reply")
		}
		if e == nil {
			_, e = api.ParseJSON(f.Payload)
		}
	case *ReplyAck:
		if !api.ValidID(f.DeliveryID) || len(f.RequestDigest) != 71 || !f.Stored {
			e = api.E("invalid_request", "invalid_reply_ack")
		}
	case *Heartbeat:
		if !api.ValidID(f.Nonce) {
			e = api.E("invalid_request", "invalid_channel_heartbeat")
		}
	}
	return frame, e
}
func validKind(kind string) bool {
	return kind == "command" || kind == "query" || kind == "receipt_lookup"
}
func validResult(kind string) bool {
	return kind == "receipt" || kind == "query_result" || kind == "error"
}
func checkBind(f Bind) error {
	if !api.ValidID(f.LogicalServiceID) || !api.ValidID(f.ConnectionID) || !api.ValidID(f.GatewayInstanceID) || !api.ValidID(f.EndpointID) || !api.ValidID(f.InstanceID) || f.EndpointGeneration == 0 || len(f.MethodsDigest) != 71 {
		return api.E("invalid_request", "invalid_channel_binding")
	}
	if f.Protocol != api.Protocol || f.Profile != api.Profile || f.TransportProfile != EndpointProfile {
		return api.E("unsupported", "channel_profile_mismatch")
	}
	return nil
}
