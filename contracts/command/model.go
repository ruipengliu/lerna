package command

import (
	"bytes"
	"encoding/json"
	"io"
	"net"
	"net/url"
	"unicode/utf8"

	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
)

// ReferenceModelOutput 只解释原始供应商协议，不采纳模型生成的授权、费用或任务效果。
func ReferenceModelOutput(raw *v1.RawObservation, body []byte, attempt *v1.ExecutionAttempt) (string, []byte, bool) {
	if raw == nil || attempt == nil || attempt.GetCapabilities().GetProtocolVersion() != "lerna-model-v1" || raw.Source != "TRUSTED_IO" || raw.Protocol != "HTTP" || raw.StatusCode != 200 || raw.TransportError != "" || !utf8.Valid(body) {
		return "UNKNOWN", nil, false
	}
	endpoint, e := url.Parse(raw.Target)
	if e != nil {
		return "UNKNOWN", nil, false
	}
	address, port, e := net.SplitHostPort(raw.ActualAddress)
	expected := endpoint.Port()
	if expected == "" {
		expected = "80"
	}
	if e != nil || port != expected || !net.ParseIP(address).Equal(net.ParseIP(endpoint.Hostname())) {
		return "UNKNOWN", nil, false
	}
	d := json.NewDecoder(bytes.NewReader(body))
	token, e := d.Token()
	if e != nil || token != json.Delim('{') {
		return "INVALID_OUTPUT", nil, false
	}
	values := map[string]json.RawMessage{}
	for d.More() {
		key, e := d.Token()
		if e != nil {
			return "INVALID_OUTPUT", nil, false
		}
		name, ok := key.(string)
		if !ok || values[name] != nil {
			return "INVALID_OUTPUT", nil, false
		}
		switch name {
		case "protocol", "external_key", "attempt_id", "applied", "terminal", "model", "billing":
		default:
			return "INVALID_OUTPUT", nil, false
		}
		var value json.RawMessage
		if e = d.Decode(&value); e != nil {
			return "INVALID_OUTPUT", nil, false
		}
		values[name] = value
	}
	token, e = d.Token()
	if e != nil || token != json.Delim('}') {
		return "INVALID_OUTPUT", nil, false
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return "INVALID_OUTPUT", nil, false
	}
	var protocol, key, attemptID string
	var applied, terminal bool
	if json.Unmarshal(values["protocol"], &protocol) != nil || json.Unmarshal(values["external_key"], &key) != nil || json.Unmarshal(values["attempt_id"], &attemptID) != nil || json.Unmarshal(values["applied"], &applied) != nil || json.Unmarshal(values["terminal"], &terminal) != nil || protocol != "lerna-model-v1" || key != attempt.ExternalKey || key != raw.ExternalKey || attemptID != attempt.Ref.Name.LocalId || !applied || !terminal {
		return "INVALID_OUTPUT", nil, false
	}
	var output struct {
		Status string `json:"status"`
		Output string `json:"output"`
	}
	md := json.NewDecoder(bytes.NewReader(values["model"]))
	md.DisallowUnknownFields()
	if md.Decode(&output) != nil {
		return "INVALID_OUTPUT", nil, true
	}
	switch output.Status {
	case "COMPLETED":
		if !json.Valid([]byte(output.Output)) {
			return "INVALID_OUTPUT", []byte(output.Output), true
		}
	case "REFUSED", "INCOMPLETE":
	default:
		return "INVALID_OUTPUT", []byte(output.Output), true
	}
	return output.Status, []byte(output.Output), true
}
