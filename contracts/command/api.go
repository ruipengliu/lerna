package command

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net"
	"net/url"
	"strconv"
	"unicode/utf8"

	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

// CompileAPIParameters 是参考协议唯一的确定性编码规则，不读取凭据或执行 I/O。
func CompileAPIParameters(input []byte) ([]byte, error) {
	if !utf8.Valid(input) || len(input) > 1<<20 {
		return nil, Fail("INVALID_API_PARAMETERS")
	}
	d := json.NewDecoder(bytes.NewReader(input))
	d.UseNumber()
	token, e := d.Token()
	if e != nil || token != json.Delim('{') {
		return nil, Fail("INVALID_API_PARAMETERS")
	}
	seen := map[string]bool{}
	value := ""
	quantity := int64(1)
	for d.More() {
		token, e = d.Token()
		key, ok := token.(string)
		if e != nil || !ok || seen[key] {
			return nil, Fail("INVALID_API_PARAMETERS")
		}
		seen[key] = true
		switch key {
		case "value":
			token, e = d.Token()
			var valid bool
			value, valid = token.(string)
			if e != nil || !valid {
				return nil, Fail("INVALID_API_PARAMETERS")
			}
		case "quantity":
			token, e = d.Token()
			number, valid := token.(json.Number)
			if e != nil || !valid {
				return nil, Fail("INVALID_API_PARAMETERS")
			}
			quantity, e = strconv.ParseInt(string(number), 10, 64)
			if e != nil || quantity < 0 || quantity > 1000000 {
				return nil, Fail("INVALID_API_PARAMETERS")
			}
		default:
			return nil, Fail("INVALID_API_PARAMETERS")
		}
	}
	token, e = d.Token()
	var trailing any
	if e != nil || token != json.Delim('}') || d.Decode(&trailing) != io.EOF || !seen["value"] {
		return nil, Fail("INVALID_API_PARAMETERS")
	}
	return json.Marshal(struct {
		Quantity int64  `json:"quantity"`
		Value    string `json:"value"`
	}{quantity, value})
}

func BytesDigest(body []byte) string { sum := sha256.Sum256(body); return hex.EncodeToString(sum[:]) }

// APIDescriptorDigest 把固定账户及凭据引用纳入身份，实际密钥不在这个对象中。
func APIDescriptorDigest(d *v1.ApiDescriptor) string {
	if d == nil {
		return ""
	}
	c := proto.Clone(d).(*v1.ApiDescriptor)
	c.Digest = ""
	return SemanticFingerprint("api-descriptor-v1", c)
}

func ValidateAPIDescriptor(d *v1.ApiDescriptor, user, target string) error {
	if d == nil || unknown(d.ProtoReflect()) || d.Provider != "lerna-reference" || d.Environment != "synthetic" || d.Version != "1" || d.ProtocolVersion != "lerna-reference-api-v1" || d.Serialization != "reference-json-v1" || d.MediaType != "application/json" || d.Authentication != "BEARER" || d.Digest != APIDescriptorDigest(d) {
		return Fail("UNSUPPORTED_API_DESCRIPTOR")
	}
	b := d.Binding
	if b == nil || b.UserId != user || b.Account == "" || len(b.Account) > 128 || b.Resource != target || b.CredentialRef == nil || b.CredentialRef.Name == nil || b.CredentialRef.Name.UserId != user || b.CredentialRef.Name.AuthorityDomainId != "platform-credentials" || b.CredentialRef.Name.ObjectKind != "api-credential" || b.CredentialRef.Name.LocalId == "" || b.CredentialRef.Revision != 1 || b.CredentialRef.SchemaId != "lerna.v1.ApiCredentialReference" {
		return Fail("API_BINDING_MISMATCH")
	}
	u, e := url.Parse(target)
	if e != nil || u.User != nil || u.Fragment != "" || u.RawQuery != "" || u.Opaque != "" || u.Hostname() == "" || (u.Scheme != "https" && u.Scheme != "http") || b.Origin != u.Scheme+"://"+u.Host || target != u.String() {
		return Fail("TARGET_SCOPE_MISMATCH")
	}
	if port := u.Port(); port != "" {
		number, e := strconv.Atoi(port)
		if e != nil || number < 1 || number > 65535 {
			return Fail("TARGET_SCOPE_MISMATCH")
		}
	} else if u.Host[len(u.Host)-1] == ':' {
		return Fail("TARGET_SCOPE_MISMATCH")
	}
	if u.Scheme == "http" {
		ip := net.ParseIP(u.Hostname())
		if ip == nil || !ip.IsLoopback() {
			return Fail("TARGET_SCOPE_MISMATCH")
		}
	}
	for _, text := range []string{b.Account, b.Origin, b.Resource, b.CredentialRef.Name.LocalId} {
		if !utf8.ValidString(text) || bytes.ContainsAny([]byte(text), "\r\n\x00") {
			return Fail("API_BINDING_MISMATCH")
		}
	}
	return nil
}

// APIObservationMatches 只接纳固定目标的受信观察，网络或工具错误不提供终局证明。
func APIObservationMatches(raw *v1.RawObservation, d *v1.CallDescriptor, attempt *v1.ExecutionAttempt) bool {
	if raw == nil || d == nil || attempt == nil || d.ApiDescriptor == nil || raw.Source != "TRUSTED_IO" || raw.Protocol != "HTTP" || raw.Redacted || raw.TransportError != "" || raw.Target != d.Target || raw.ExternalKey != attempt.ExternalKey || !proto.Equal(raw.AttemptId, attempt.Ref.Name) || ValidateAPIDescriptor(d.ApiDescriptor, raw.UserId, d.Target) != nil {
		return false
	}
	u, e := url.Parse(raw.Target)
	if e != nil {
		return false
	}
	address, port, e := net.SplitHostPort(raw.ActualAddress)
	if e != nil || net.ParseIP(address) == nil {
		return false
	}
	expected := u.Port()
	if expected == "" {
		expected = "80"
		if u.Scheme == "https" {
			expected = "443"
		}
	}
	if port != expected {
		return false
	}
	if u.Scheme == "http" && !net.ParseIP(address).Equal(net.ParseIP(u.Hostname())) {
		return false
	}
	return true
}

// StrictJSONObject 固定协议拒绝重复字段、尾随文档和未知字段；矛盾字段保留证据冲突。
func StrictJSONObject(body []byte, allowed ...string) (map[string]json.RawMessage, bool) {
	if !utf8.Valid(body) {
		return nil, false
	}
	d := json.NewDecoder(bytes.NewReader(body))
	token, e := d.Token()
	if e != nil || token != json.Delim('{') {
		return nil, false
	}
	keys := map[string]bool{}
	for _, key := range allowed {
		keys[key] = true
	}
	values := map[string]json.RawMessage{}
	for d.More() {
		token, e = d.Token()
		key, ok := token.(string)
		if e != nil || !ok || !keys[key] {
			return nil, false
		}
		if _, exists := values[key]; exists {
			return nil, true
		}
		var value json.RawMessage
		if d.Decode(&value) != nil {
			return nil, false
		}
		values[key] = value
	}
	token, e = d.Token()
	var extra any
	if e != nil || token != json.Delim('}') || d.Decode(&extra) != io.EOF {
		return nil, false
	}
	return values, false
}

func ReferenceAPIResponse(raw *v1.RawObservation, body []byte, d *v1.CallDescriptor, attempt *v1.ExecutionAttempt) (applied, terminal, valid, conflict bool) {
	if !APIObservationMatches(raw, d, attempt) || raw.StatusCode != 200 || attempt.GetCapabilities().GetProtocolVersion() != "lerna-reference-api-v1" || attempt.GetCapabilities().GetVerificationBasis() != "reference-api-v1" {
		return
	}
	values, duplicate := StrictJSONObject(body, "protocol", "external_key", "attempt_id", "account", "origin", "applied", "terminal", "tool_error", "applied_at_unix_nano", "billing")
	if values == nil {
		conflict = duplicate
		return
	}
	var response struct {
		Protocol    string `json:"protocol"`
		ExternalKey string `json:"external_key"`
		AttemptID   string `json:"attempt_id"`
		Account     string `json:"account"`
		Origin      string `json:"origin"`
		Applied     *bool  `json:"applied"`
		Terminal    *bool  `json:"terminal"`
		ToolError   bool   `json:"tool_error"`
	}
	if json.Unmarshal(body, &response) != nil || response.Protocol != d.ApiDescriptor.ProtocolVersion || response.ExternalKey != attempt.ExternalKey || response.AttemptID != attempt.Ref.Name.LocalId || response.Account != d.ApiDescriptor.Binding.Account || response.Origin != d.ApiDescriptor.Binding.Origin || response.Applied == nil || response.Terminal == nil || response.ToolError {
		return
	}
	return *response.Applied, *response.Terminal, true, false
}
