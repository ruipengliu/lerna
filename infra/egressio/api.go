package egressio

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"time"
	"unicode/utf8"

	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

type APICredentials interface {
	Resolve(context.Context, *v1.ApiTargetBinding) ([]byte, error)
}

// APIHTTP 的凭据接口仅在受信网络边界装配；Roots 是宿主固定的证书信任配置。
type APIHTTP struct {
	Credentials APICredentials
	Roots       *x509.CertPool
}

func (a APIHTTP) Preflight(ctx context.Context, d *v1.CallDescriptor) error {
	if d.GetApiDescriptor() == nil {
		return nil
	}
	secret, e := a.resolve(ctx, d.ApiDescriptor.Binding)
	clear(secret)
	return e
}

func (a APIHTTP) resolve(ctx context.Context, b *v1.ApiTargetBinding) ([]byte, error) {
	if a.Credentials == nil {
		return nil, command.Fail("CREDENTIAL_UNAVAILABLE")
	}
	secret, e := a.Credentials.Resolve(ctx, b)
	if e != nil || len(secret) < 16 || len(secret) > 16384 {
		clear(secret)
		return nil, command.Fail("CREDENTIAL_UNAVAILABLE")
	}
	for _, c := range secret {
		if c <= 32 || c >= 127 || c == '"' || c == '\\' {
			clear(secret)
			return nil, command.Fail("CREDENTIAL_UNAVAILABLE")
		}
	}
	return secret, nil
}

func (a APIHTTP) Perform(ctx context.Context, c *v1.PhysicalIORequest) (*v1.PhysicalIOResult, error) {
	d := c.GetCallDescriptor()
	if d == nil || d.Protocol != "HTTP" || c.GetSend().GetPhase() != "DISPATCH_POSSIBLE" || c.Attempt == nil || c.OperationId == nil || (d.Method != "POST" && (d.Method != "GET" || d.QuerySubject == nil)) || d.ExternalKey != c.Attempt.ExternalKey {
		return nil, command.Fail("INVALID_IO_INTENT")
	}
	if e := command.ValidateAPIDescriptor(d.ApiDescriptor, c.OperationId.UserId, d.Target); e != nil {
		return nil, e
	}
	sealed := proto.Clone(d).(*v1.CallDescriptor)
	sealed.Digest = ""
	if d.Digest != command.SemanticFingerprint("call-descriptor-v1", sealed) || d.BodyDigest != command.BytesDigest(c.Body) {
		return nil, command.Fail("PREPARATION_UNRECOVERABLE")
	}
	target, _ := url.Parse(d.Target)
	o := &v1.RawObservation{Ref: c.Send.ObservationRef, UserId: c.OperationId.UserId, TaskId: c.TaskId, OperationId: c.OperationId, AttemptId: c.Attempt.Ref.Name, SendRef: c.Send.Ref, SendSeq: c.Send.SendSeq, Target: d.Target, ExecutorEndpointId: c.ExecutorEndpointId, Protocol: "HTTP", StartedAtUnixMs: time.Now().UnixMilli(), ExternalKey: c.Attempt.ExternalKey, Source: "TRUSTED_IO", QuerySubject: d.QuerySubject}
	result := &v1.PhysicalIOResult{Observation: o}
	var secret []byte
	var responseFields []string
	metadataInvalid := false
	finish := func(code string) (*v1.PhysicalIOResult, error) {
		o.FinishedAtUnixMs = time.Now().UnixMilli()
		o.TransportError = code
		if metadataInvalid {
			o.ProviderRequestId, o.RateCategory, o.RetryAfter = "", "UNKNOWN", ""
			if code == "" {
				o.TransportError = "RESPONSE_METADATA_INVALID"
			}
		}
		if credentialEcho(secret, result.Body, append(responseFields, o.ProviderRequestId, o.RateCategory, o.RetryAfter)...) {
			result.Body = nil
			o.ProviderRequestId = ""
			o.RateCategory = "UNKNOWN"
			o.RetryAfter = ""
			o.Redacted = true
			o.TransportError = "CREDENTIAL_ECHO_REDACTED"
		}
		return result, nil
	}
	secret, e := a.resolve(ctx, d.ApiDescriptor.Binding)
	if e != nil {
		return finish("CREDENTIAL_UNAVAILABLE")
	}
	defer clear(secret)
	port := target.Port()
	if port == "" {
		port = "80"
		if target.Scheme == "https" {
			port = "443"
		}
	}
	conn, e := (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, "tcp", net.JoinHostPort(target.Hostname(), port))
	if e != nil {
		return finish("CONNECT_FAILED")
	}
	defer conn.Close()
	o.ActualAddress = conn.RemoteAddr().String()
	deadline := time.Now().Add(5 * time.Second)
	if until, ok := ctx.Deadline(); ok && until.Before(deadline) {
		deadline = until
	}
	if e = conn.SetDeadline(deadline); e != nil {
		return finish("CONNECT_FAILED")
	}
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	if target.Scheme == "https" {
		tlsConn := tls.Client(conn, &tls.Config{ServerName: target.Hostname(), RootCAs: a.Roots, MinVersion: tls.VersionTLS12})
		if e = tlsConn.HandshakeContext(ctx); e != nil {
			return finish("TLS_VERIFICATION_FAILED")
		}
		conn = tlsConn
	}
	request, e := http.NewRequestWithContext(ctx, d.Method, d.Target, bytes.NewReader(c.Body))
	if e != nil {
		return finish("REQUEST_INVALID")
	}
	request.Close = true
	request.Header.Set("Content-Type", d.ApiDescriptor.MediaType)
	request.Header.Set("Accept", d.ApiDescriptor.MediaType)
	request.Header.Set("Authorization", "Bearer "+string(secret))
	request.Header.Set("Idempotency-Key", d.ExternalKey)
	if d.KeyValidUntilUnixMs != nil {
		request.Header.Set("Lerna-Key-Valid-Until", strconv.FormatInt(d.GetKeyValidUntilUnixMs(), 10))
	}
	// 这些身份字段是已审查参考协议的一部分，不应用于其他供应商。
	request.Header.Set("Lerna-Account", d.ApiDescriptor.Binding.Account)
	request.Header.Set("Lerna-Attempt", c.Attempt.Ref.Name.LocalId)
	request.Header.Set("Lerna-Operation", c.OperationId.LocalId)
	request.Header.Set("Lerna-Send-Id", c.Send.Ref.Name.LocalId)
	request.Header.Set("Lerna-Send", strconv.FormatUint(uint64(c.Send.SendSeq), 10))
	if d.QuerySubject != nil {
		request.Header.Set("Lerna-Query-Operation", d.QuerySubject.OperationId.LocalId)
		request.Header.Set("Lerna-Query-Attempt", d.QuerySubject.AttemptId.LocalId)
		request.Header.Set("Lerna-Query-Key", d.QuerySubject.ExternalKey)
		request.Header.Set("Lerna-Query-Scope", d.QuerySubject.TargetScope)
	}
	if e = request.Write(conn); e != nil {
		return finish("WRITE_FAILED")
	}
	response, e := http.ReadResponse(bufio.NewReader(io.LimitReader(conn, (1<<20)+(64<<10))), request)
	if e != nil {
		return finish("READ_FAILED")
	}
	defer response.Body.Close()
	o.StatusCode = int32(response.StatusCode)
	for _, name := range []string{"X-Request-ID", "Lerna-Rate-Category", "Retry-After"} {
		responseFields = append(responseFields, response.Header.Values(name)...)
		maximum := 512
		if name == "Lerna-Rate-Category" {
			maximum = 32
		}
		if name == "Retry-After" {
			maximum = 128
		}
		for _, value := range response.Header.Values(name) {
			if len(value) > maximum || !utf8.ValidString(value) {
				metadataInvalid = true
			}
		}
	}
	o.ProviderRequestId = response.Header.Get("X-Request-ID")
	o.RateCategory = response.Header.Get("Lerna-Rate-Category")
	o.RetryAfter = response.Header.Get("Retry-After")
	if len(response.Header.Values("Lerna-Rate-Category")) != 1 {
		o.RateCategory = "UNKNOWN"
	}
	if len(response.Header.Values("Retry-After")) != 1 {
		o.RetryAfter = ""
	}
	result.Body, e = io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
	if len(result.Body) > 1<<20 {
		oversized, e := finish("RESPONSE_TOO_LARGE")
		oversized.Body = nil
		return oversized, e
	}
	if e != nil {
		return finish("READ_FAILED")
	}
	return finish("")
}

// credentialEcho 在内容交接前丢弃秘密副本；被遮蔽的正文不能继续作为终局证据。
func credentialEcho(secret, body []byte, headers ...string) bool {
	if len(secret) == 0 {
		return false
	}
	variants := [][]byte{secret, []byte(base64.StdEncoding.EncodeToString(secret)), []byte(base64.RawStdEncoding.EncodeToString(secret)), []byte(base64.URLEncoding.EncodeToString(secret)), []byte(hex.EncodeToString(secret)), []byte(url.QueryEscape(string(secret)))}
	has := func(data []byte) bool {
		for _, v := range variants {
			if bytes.Contains(data, v) {
				return true
			}
		}
		return false
	}
	if has(body) {
		return true
	}
	for _, h := range headers {
		if has([]byte(h)) {
			return true
		}
	}
	d := json.NewDecoder(bytes.NewReader(body))
	for {
		token, e := d.Token()
		if e != nil {
			break
		}
		if value, ok := token.(string); ok && has([]byte(value)) {
			return true
		}
	}
	return false
}

// Router 只按已固定的描述选择受信出口，不接受额外 URL 或认证选项。
type Router struct{ API APIHTTP }

func (r Router) Preflight(ctx context.Context, d *v1.CallDescriptor) error {
	return r.API.Preflight(ctx, d)
}
func (r Router) Perform(ctx context.Context, c *v1.PhysicalIORequest) (*v1.PhysicalIOResult, error) {
	if c.GetCallDescriptor().GetApiDescriptor() != nil {
		return r.API.Perform(ctx, c)
	}
	return (HTTP{}).Perform(ctx, c)
}
