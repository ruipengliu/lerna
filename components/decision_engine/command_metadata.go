package decision_engine

import (
	"bytes"
	"encoding/json"
	v "github.com/ruipengliu/lerna/contract/v1_1"
	"io"
)

// DecodeCommandRecord accepts precisely the original01 decide metadata or the
// closed v2 branch. It never rewrites the old request, digest or receipt.
func DecodeCommandRecord(body []byte, ref v.CommandRef, digest string, receipt v.CommandReceipt) (*CommandRecord, error) {
	raw, err := v.ParseJSON(body)
	if err != nil {
		return nil, ErrUnavailable
	}
	shape, ok := raw.(map[string]any)
	if !ok {
		return nil, ErrUnavailable
	}
	_, versioned := shape["metadata_version"]
	_, hasMethod := shape["method"]
	_, hasDecide := shape["request"]
	_, hasCancel := shape["cancel_request"]
	if !versioned {
		if hasMethod || hasCancel || !hasDecide {
			return nil, ErrUnavailable
		}
	} else {
		if shape["metadata_version"] != "2" || !hasMethod {
			return nil, ErrUnavailable
		}
		switch shape["method"] {
		case "decide":
			if !hasDecide || hasCancel {
				return nil, ErrUnavailable
			}
		case "cancel":
			if hasDecide || !hasCancel {
				return nil, ErrUnavailable
			}
		default:
			return nil, ErrUnavailable
		}
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	var record CommandRecord
	if err := decoder.Decode(&record); err != nil {
		return nil, ErrUnavailable
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return nil, ErrUnavailable
	}
	if record.Digest != digest {
		return nil, ErrUnavailable
	}
	want, err := v.Encode(receipt)
	if err != nil {
		return nil, ErrUnavailable
	}
	actual, err := v.Encode(record.Receipt)
	if err != nil || !bytes.Equal(actual, want) {
		return nil, ErrUnavailable
	}
	if err = record.Validate(ref); err != nil {
		return nil, err
	}
	return &record, nil
}
func (r CommandRecord) Validate(ref v.CommandRef) error {
	var data []byte
	var err error
	var id v.ID
	var owner v.OwnerRef
	if r.MetadataVersion == "" {
		if r.Method != "" || r.Request == nil || r.CancelRequest != nil {
			return ErrUnavailable
		}
	} else if r.MetadataVersion != "2" {
		return ErrUnavailable
	}
	if r.Request != nil && r.CancelRequest == nil && (r.MetadataVersion == "" || r.Method == "decide") {
		data, err = v.Encode(*r.Request)
		if err == nil {
			_, err = v.DecodeDecide(data)
		}
		id = r.Request.CommandID
		owner = decisionOwner(r.Request.Target)
	} else if r.Request == nil && r.CancelRequest != nil && r.MetadataVersion == "2" && r.Method == "cancel" {
		data, err = v.Encode(*r.CancelRequest)
		if err == nil {
			_, err = v.DecodeCancel(data)
		}
		id = r.CancelRequest.CommandID
		owner = decisionOwner(r.CancelRequest.Target)
	} else {
		return ErrUnavailable
	}
	if err != nil || id != ref.CommandID || owner != ref.Owner || r.Subject.TenantID != ref.Owner.TenantID {
		return ErrUnavailable
	}
	principal, err := v.Encode(r.Subject)
	if err != nil {
		return ErrUnavailable
	}
	digest, err := v.CommandDigest(data, principal)
	if err != nil || digest != r.Digest {
		return ErrUnavailable
	}
	response := v.NewCommandGetResponseFound(v.CommandGetResponseFound{CommandRef: ref, Receipt: r.Receipt, Progress: v.NewCommandProgressNone(v.CommandProgressNone{})})
	_, err = v.EncodeCommandResponse(response, ref)
	return err
}
