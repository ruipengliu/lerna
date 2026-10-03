// Harness CLI 仅发送调用者已经冻结的公开请求；不生成业务身份或补截止时间。
package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/ruipengliu/lerna/api"
	harness "github.com/ruipengliu/lerna/sdk/go"
)

type options struct {
	endpoint, discovery, tokenFile, tokenEnv, caFile, journal, request, commandID string
	development                                                                   bool
	timeout                                                                       time.Duration
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Stdout); err != nil {
		var p *api.Error
		if errors.As(err, &p) {
			fmt.Fprintf(os.Stderr, "%s: %s (retry=%s)\n", p.Code, p.Reason, p.Retry)
		} else {
			fmt.Fprintln(os.Stderr, "dependency_unavailable: client_io_failed (retry=query_original)")
		}
		os.Exit(1)
	}
}

func parse(args []string) (options, string, error) {
	var o options
	f := flag.NewFlagSet("harness-cli", flag.ContinueOnError)
	f.SetOutput(io.Discard) // 无效参数也不将可能误传的凭据回显。
	f.StringVar(&o.endpoint, "endpoint", "", "HTTPS/WSS/grpcs endpoint")
	f.StringVar(&o.discovery, "discovery", "", "HTTPS discovery base for WSS/grpcs")
	f.StringVar(&o.tokenFile, "token-file", "", "private credential file")
	f.StringVar(&o.tokenEnv, "token-envref", "", "credential environment variable name")
	f.StringVar(&o.caFile, "ca-file", "", "additional trusted CA PEM file")
	f.StringVar(&o.journal, "journal", "", "private original-command journal directory")
	f.StringVar(&o.request, "request", "", "exact original JSON request file")
	f.StringVar(&o.commandID, "command-id", "", "original command ID for receipt")
	f.BoolVar(&o.development, "development-loopback", false, "explicit cleartext literal-loopback exception")
	f.DurationVar(&o.timeout, "timeout", 10*time.Second, "call timeout, never changes business deadlines")
	if f.Parse(args) != nil || len(f.Args()) != 1 || o.timeout < time.Second || o.timeout > 30*time.Second || o.endpoint == "" || (o.tokenFile == "") == (o.tokenEnv == "") {
		return o, "", api.E("invalid_request", "invalid_cli_arguments")
	}
	op := f.Args()[0]
	if op != "discover" && op != "command" && op != "query" && op != "receipt" && op != "recover" {
		return o, "", api.E("unsupported", "cli_operation_not_supported")
	}
	return o, op, nil
}

func readFile(path string, max int64, private bool) ([]byte, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || (private && info.Mode().Perm()&0077 != 0) {
		return nil, api.E("forbidden", "private_regular_file_required")
	}
	b, err := io.ReadAll(io.LimitReader(f, max+1))
	if err == nil && int64(len(b)) > max {
		err = api.E("invalid_request", "client_file_too_large")
	}
	return b, err
}

func credential(o options) (string, error) {
	var value string
	if o.tokenFile != "" {
		b, err := readFile(o.tokenFile, 4096, true)
		if err != nil {
			return "", err
		}
		value = strings.TrimSpace(string(b))
	} else {
		if !regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,127}$`).MatchString(o.tokenEnv) {
			return "", api.E("invalid_request", "invalid_credential_reference")
		}
		value = os.Getenv(o.tokenEnv)
	}
	if len(value) == 0 || len(value) > 4089 {
		return "", api.E("forbidden", "credential_unavailable")
	}
	for _, ch := range []byte(value) {
		if ch < 0x21 || ch > 0x7e {
			return "", api.E("forbidden", "invalid_credential_format")
		}
	}
	return value, nil
}

func trustedHTTP(o options) (*http.Client, *tls.Config, error) {
	cfg := &tls.Config{MinVersion: tls.VersionTLS13}
	if o.caFile != "" {
		b, err := readFile(o.caFile, 1<<20, false)
		if err != nil {
			return nil, nil, err
		}
		pool, err := x509.SystemCertPool()
		if err != nil {
			pool = x509.NewCertPool()
		}
		if !pool.AppendCertsFromPEM(b) {
			return nil, nil, api.E("invalid_request", "invalid_ca_file")
		}
		cfg.RootCAs = pool
	}
	dialer := &net.Dialer{Timeout: o.timeout}
	transport := &http.Transport{TLSClientConfig: cfg, DisableKeepAlives: true, MaxConnsPerHost: 1, ResponseHeaderTimeout: o.timeout}
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		conn, err := dialer.DialContext(ctx, network, address)
		if err != nil {
			return nil, err
		}
		if o.development {
			remote, ok := conn.RemoteAddr().(*net.TCPAddr)
			if !ok || !remote.IP.IsLoopback() {
				conn.Close()
				return nil, api.E("forbidden", "development_requires_loopback")
			}
		}
		return conn, nil
	}
	return &http.Client{Transport: transport, Timeout: o.timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, cfg, nil
}

func endpoint(value string, development bool) (*url.URL, error) {
	u, err := url.Parse(value)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, api.E("invalid_request", "invalid_endpoint")
	}
	secure := u.Scheme == "https" || u.Scheme == "wss" || u.Scheme == "grpcs"
	if !secure {
		ip := net.ParseIP(u.Hostname())
		if !development || ip == nil || !ip.IsLoopback() || (u.Scheme != "http" && u.Scheme != "ws" && u.Scheme != "grpc") {
			return nil, api.E("forbidden", "tls_required")
		}
	}
	return u, nil
}

func run(ctx context.Context, args []string, out io.Writer) error {
	o, operation, err := parse(args)
	if err != nil {
		return err
	}
	u, err := endpoint(o.endpoint, o.development)
	if err != nil {
		return err
	}
	token, err := credential(o)
	if err != nil {
		return err
	}
	httpClient, tlsConfig, err := trustedHTTP(o)
	if err != nil {
		return err
	}
	defer httpClient.CloseIdleConnections()
	base := o.endpoint
	if u.Scheme != "https" && u.Scheme != "http" {
		base = o.discovery
	}
	discoveryURL, err := endpoint(base, o.development)
	if err != nil {
		return err
	}
	if discoveryURL.Scheme != "https" && discoveryURL.Scheme != "http" {
		return api.E("invalid_request", "https_discovery_required")
	}
	httpTransport := &harness.HTTPTransport{BaseURL: base, Token: token, HTTP: httpClient, AllowInsecureLoopback: o.development}
	ctx, cancel := context.WithTimeout(ctx, o.timeout)
	defer cancel()
	d, err := httpTransport.Discover(ctx)
	if err != nil {
		return err
	}
	if operation == "discover" {
		return writeJSON(out, api.Raw(d))
	}
	var transport harness.Transport = httpTransport
	switch u.Scheme {
	case "wss", "ws":
		ws, e := harness.DialWebSocketWithHTTP(ctx, o.endpoint, token, d, o.development, httpClient)
		if e != nil {
			return e
		}
		defer ws.Close()
		transport = ws
	case "grpcs", "grpc":
		grpc, e := harness.DialGRPC(ctx, o.endpoint, token, d, tlsConfig, o.development)
		if e != nil {
			return e
		}
		defer grpc.Close()
		transport = grpc
	}
	if operation == "query" {
		b, e := readFile(o.request, api.MaxJSONBytes, false)
		if e != nil {
			return e
		}
		var q api.Query
		if e = api.Decode(b, &q); e != nil {
			return e
		}
		if e = validateQuery(q, d); e != nil {
			return e
		}
		c, e := harness.NewClient(transport, readOnlyJournal{}, d)
		if e != nil {
			return e
		}
		result, e := c.Query(ctx, q)
		if e != nil {
			return e
		}
		return writeJSON(out, result)
	}
	j, release, err := commandJournal(o.journal, d.IdentityScope)
	if err != nil {
		return err
	}
	defer release()
	c, err := harness.NewClient(transport, j, d)
	if err != nil {
		return err
	}
	switch operation {
	case "command":
		b, e := readFile(o.request, api.MaxJSONBytes, false)
		if e != nil {
			return e
		}
		var command api.Command
		if e = api.Decode(b, &command); e != nil {
			return e
		}
		if e = validateCommand(command, b, d); e != nil {
			return e
		}
		receipt, e := c.Send(ctx, command)
		if e != nil {
			return e
		}
		if e = writeJSON(out, api.Raw(receipt)); e != nil {
			return e
		}
		if receipt.Stage == "rejected" {
			return api.E("invalid_state", "original_command_rejected")
		}
		return nil
	case "receipt":
		if !api.ValidID(o.commandID) {
			return api.E("invalid_request", "original_command_id_required")
		}
		receipt, e := c.Receipt(ctx, o.commandID)
		if e != nil {
			return e
		}
		return writeJSON(out, api.Raw(receipt))
	case "recover":
		pending, _, e := j.Pending(ctx, 128)
		if e != nil {
			return e
		}
		for _, entry := range pending {
			if entry.SchemaDigest != d.SchemaDigest || entry.Command.Protocol != d.Protocol || entry.Command.Profile != d.Profile {
				return api.E("unsupported", "original_decoder_unavailable")
			}
		}
		receipts, partial, e := c.Recover(ctx)
		if e != nil {
			return e
		}
		return writeJSON(out, api.Raw(struct {
			Receipts []api.Receipt `json:"receipts"`
			Partial  bool          `json:"partial"`
		}{receipts, partial}))
	}
	return api.E("unsupported", "cli_operation_not_supported")
}

func writeJSON(out io.Writer, b json.RawMessage) error {
	_, err := out.Write(append(b, '\n'))
	return err
}

func validateEnvelope(protocol, profile, owner, id, target string, d harness.Discovery) error {
	if protocol != d.Protocol || profile != d.Profile {
		return api.E("unsupported", "profile_not_supported")
	}
	if owner != d.LogicalServiceID {
		return api.E("invalid_request", "wrong_logical_service")
	}
	if !api.ValidID(owner) || !api.ValidID(id) || !api.ValidID(target) {
		return api.E("invalid_request", "invalid_identity")
	}
	return nil
}
func methodKind(d harness.Discovery, method, kind string) error {
	for _, m := range d.Methods {
		if m.Name == method {
			if m.Kind != kind {
				return api.E("invalid_request", "method_kind_mismatch")
			}
			return nil
		}
	}
	return api.E("unsupported", "method_not_supported")
}
func validateQuery(q api.Query, d harness.Discovery) error {
	if err := validateEnvelope(q.Protocol, q.Profile, q.LogicalServiceID, q.QueryID, q.TargetID, d); err != nil {
		return err
	}
	return methodKind(d, q.Method, "query")
}
func validateCommand(c api.Command, raw []byte, d harness.Discovery) error {
	if err := validateEnvelope(c.Protocol, c.Profile, c.LogicalServiceID, c.CommandID, c.TargetID, d); err != nil {
		return err
	}
	if _, err := api.ParseTime(c.ExpiresAt); err != nil {
		return api.E("invalid_request", "invalid_expiry")
	}
	if err := methodKind(d, c.Method, "command"); err != nil {
		return err
	}
	value, err := api.ParseJSON(raw)
	if err != nil {
		return err
	}
	if field, present := value.(map[string]any)["expected_revision"]; present && field == nil {
		return api.E("invalid_request", "invalid_revision")
	}
	if c.ExpectedRevision != nil && (*c.ExpectedRevision == 0 || *c.ExpectedRevision > api.MaxSafeInteger) {
		return api.E("invalid_request", "invalid_revision")
	}
	return nil
}

// readOnlyJournal 只满足客户端查询构造约束；查询不建立命令责任。
type readOnlyJournal struct{}

func (readOnlyJournal) Save(context.Context, harness.Entry) error {
	return api.E("unsupported", "query_journal_read_only")
}
func (readOnlyJournal) Read(context.Context, string) (harness.Entry, error) {
	return harness.Entry{}, api.E("unsupported", "query_journal_read_only")
}
func (readOnlyJournal) Pending(context.Context, int) ([]harness.Entry, bool, error) {
	return nil, false, api.E("unsupported", "query_journal_read_only")
}

func commandJournal(path, scope string) (*harness.FileJournal, func(), error) {
	if !filepath.IsAbs(path) {
		return nil, nil, api.E("invalid_request", "absolute_journal_directory_required")
	}
	if err := os.MkdirAll(path, 0700); err != nil {
		return nil, nil, err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, nil, err
	}
	if !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return nil, nil, api.E("forbidden", "private_journal_directory_required")
	}
	root, err := os.OpenRoot(path)
	if err != nil {
		return nil, nil, err
	}
	lock, err := root.OpenFile(".cli.lock", os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		root.Close()
		return nil, nil, err
	}
	if err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		lock.Close()
		root.Close()
		return nil, nil, api.E("overloaded", "cli_journal_in_use")
	}
	j, err := harness.OpenJournal(path, scope)
	if err != nil {
		lock.Close()
		root.Close()
		return nil, nil, err
	}
	return j, func() { _ = j.Close(); _ = lock.Close(); _ = root.Close() }, nil
}
