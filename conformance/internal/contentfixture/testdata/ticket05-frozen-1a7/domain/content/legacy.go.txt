package content

import (
	"context"
	"encoding/json"
	old "github.com/ruipengliu/lerna/contract"
	middle "github.com/ruipengliu/lerna/contract/v1_1"
	v "github.com/ruipengliu/lerna/contract/v1_2"
)

// legacyCommand observes the same original owner through the current durable
// reader policy. Changing only the read envelope version is safe; original
// fixed receipts and progress must still pass the requested closed codec.
func (s *Service) legacyCommand(ctx context.Context, raw, principal []byte) ([]byte, error) {
	var request map[string]json.RawMessage
	if err := json.Unmarshal(raw, &request); err != nil {
		return nil, err
	}
	request["contract_version"] = json.RawMessage(`"1.2.0"`)
	wire, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}
	var subject *v.SubjectBinding
	if principal != nil {
		frozen, err := v.Decode[v.SubjectBinding](principal)
		if err != nil {
			return nil, err
		}
		subject = &frozen
	}
	response, err := s.GetCommand(ctx, wire, subject)
	if err != nil {
		return nil, err
	}
	return v.Encode(response)
}

// GetCommand10 preserves 1.0 exactly. A 1.2 accepted receipt cannot express its
// mandatory original ContentRef and retention in 1.0, so remains unavailable.
func (s *Service) GetCommand10(ctx context.Context, raw []byte, subject *old.SubjectBinding) (old.CommandGetResponse, error) {
	request, err := old.DecodeCommand(raw)
	if err != nil {
		return old.CommandGetResponse{}, err
	}
	unavailable := old.NewCommandGetResponseUnavailable(old.CommandGetResponseUnavailable{CommandRef: request.Payload.CommandRef, Reason: "dependency_unavailable"})
	var principal []byte
	if subject != nil {
		principal, err = old.Encode(*subject)
		if err != nil {
			return old.NewCommandGetResponseRejected(old.CommandGetResponseRejected{Reason: "forbidden"}), nil
		}
	}
	wire, err := s.legacyCommand(ctx, raw, principal)
	if err != nil {
		return unavailable, nil
	}
	result, err := old.DecodeCommandResponse(wire, request.Payload.CommandRef)
	if err != nil {
		return unavailable, nil
	}
	return result, nil
}

// GetCommand11 applies the same lossless closed-codec requirement to 1.1.
func (s *Service) GetCommand11(ctx context.Context, raw []byte, subject *middle.SubjectBinding) (middle.CommandGetResponse, error) {
	request, err := middle.DecodeCommand(raw)
	if err != nil {
		return middle.CommandGetResponse{}, err
	}
	unavailable := middle.NewCommandGetResponseUnavailable(middle.CommandGetResponseUnavailable{CommandRef: request.Payload.CommandRef, Reason: "dependency_unavailable"})
	var principal []byte
	if subject != nil {
		principal, err = middle.Encode(*subject)
		if err != nil {
			return middle.NewCommandGetResponseRejected(middle.CommandGetResponseRejected{Reason: "forbidden"}), nil
		}
	}
	wire, err := s.legacyCommand(ctx, raw, principal)
	if err != nil {
		return unavailable, nil
	}
	result, err := middle.DecodeCommandResponse(wire, request.Payload.CommandRef)
	if err != nil {
		return unavailable, nil
	}
	return result, nil
}
