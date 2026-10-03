package main_test

import (
	"context"
	"crypto/tls"
	"encoding/pem"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	rpcadapter "github.com/ruipengliu/lerna/adapters/grpc"
	"github.com/ruipengliu/lerna/adapters/wss"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/cmd/internal/bootstrap"
	"github.com/ruipengliu/lerna/internal/interaction"
	harness "github.com/ruipengliu/lerna/sdk/go"
)

var cliBuild struct {
	sync.Once
	path string
	err  error
}

func cliBinary(t *testing.T) string {
	t.Helper()
	cliBuild.Do(func() {
		dir, err := os.MkdirTemp("", "harness-cli-test-*")
		if err != nil {
			cliBuild.err = err
			return
		}
		cliBuild.path = filepath.Join(dir, "harness-cli")
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		cliBuild.err = exec.CommandContext(ctx, "go", "build", "-o", cliBuild.path, ".").Run()
	})
	if cliBuild.err != nil {
		t.Fatalf("build CLI executable: %v", cliBuild.err)
	}
	return cliBuild.path
}

func TestMain(m *testing.M) {
	code := m.Run()
	if cliBuild.path != "" {
		_ = os.RemoveAll(filepath.Dir(cliBuild.path))
	}
	os.Exit(code)
}

type cliFixture struct {
	root   string
	config bootstrap.Config
	app    *bootstrap.App
	server *httptest.Server
	ca     string
}

func fixture(t *testing.T, wrap ...func(http.Handler) http.Handler) cliFixture {
	t.Helper()
	ctx := context.Background()
	root := t.TempDir()
	c, err := bootstrap.InitializeConfig(ctx, filepath.Join(root, "config.json"), root, "sqlite")
	if err != nil {
		t.Fatal(err)
	}
	a, err := bootstrap.OpenApp(ctx, c, true)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := a.Close(); err != nil {
			t.Error(err)
		}
	})
	gateway, err := a.Gateway()
	if err != nil {
		t.Fatal(err)
	}
	var handler http.Handler = gateway.Handler()
	if len(wrap) != 0 {
		handler = wrap[0](handler)
	}
	s := httptest.NewTLSServer(handler)
	t.Cleanup(s.Close)
	ca := filepath.Join(root, "test-ca.pem")
	if err = os.WriteFile(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: s.Certificate().Raw}), 0600); err != nil {
		t.Fatal(err)
	}
	return cliFixture{root, c, a, s, ca}
}

func TestCLIOriginalCommandIsDurableBeforeIOAndRecoveredWithoutNewDeadline(t *testing.T) {
	journalPath := filepath.Join(t.TempDir(), "journal")
	var original api.Command
	var scope string
	observed := make(chan error, 1)
	var lose sync.Once
	f := fixture(t, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "POST" && r.URL.Path == "/api/call" {
				lost := false
				lose.Do(func() {
					lost = true
					j, err := harness.OpenJournal(journalPath, scope)
					if err == nil {
						entry, readErr := j.Read(r.Context(), original.CommandID)
						err = readErr
						if err == nil && (!api.Equal(entry.Command, original) || entry.Receipt != nil) {
							err = fmt.Errorf("original request was not durable before call")
						}
						_ = j.Close()
					}
					observed <- err
					recorder := httptest.NewRecorder()
					next.ServeHTTP(recorder, r) // 原决定已由真实业务事务保存。
					conn, _, err := w.(http.Hijacker).Hijack()
					if err == nil {
						_ = conn.Close()
					}
				})
				if lost {
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	})
	scope, _ = api.Digest([]string{f.config.TenantID, f.config.SubjectID})
	sessionID := api.NewID("session")
	original = api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: f.config.OwnerID, CommandID: api.NewID("command"), Method: "session.create", TargetID: f.config.OwnerID, ExpiresAt: api.Time(time.Now().Add(time.Minute)), Payload: api.Raw(interaction.CreateSessionInput{SessionID: sessionID, DefaultBranchID: api.NewID("branch"), ConfigRef: api.ComponentRef{ComponentID: api.NewID("component"), Version: "1.0.0", Digest: api.Hash([]byte("exact-cli-config"))}})}
	requestPath := filepath.Join(f.root, "command.json")
	if err := os.WriteFile(requestPath, api.Raw(original), 0600); err != nil {
		t.Fatal(err)
	}
	_, diagnostic, err := invoke(t, append(f.flags(), "--journal", journalPath, "--request", requestPath, "command")...)
	if err == nil {
		t.Fatal("lost response unexpectedly succeeded")
	}
	select {
	case err = <-observed:
		if err != nil {
			t.Fatal(err)
		}
	default:
		t.Fatalf("request did not reach durable-before-IO check: %s", diagnostic)
	}
	j, err := harness.OpenJournal(journalPath, scope)
	if err != nil {
		t.Fatal(err)
	}
	entry, err := j.Read(context.Background(), original.CommandID)
	_ = j.Close()
	if err != nil || !api.Equal(entry.Command, original) || entry.Receipt != nil {
		t.Fatalf("pending original entry: %+v %v", entry, err)
	}
	out, diagnostic, err := invoke(t, append(f.flags(), "--journal", journalPath, "recover")...)
	if err != nil {
		t.Fatalf("recover: %v %s", err, diagnostic)
	}
	var recovered struct {
		Receipts []api.Receipt `json:"receipts"`
		Partial  bool          `json:"partial"`
	}
	if err = api.Decode(out, &recovered); err != nil {
		t.Fatal(err)
	}
	if recovered.Partial || len(recovered.Receipts) != 1 || recovered.Receipts[0].Stage != "applied" || recovered.Receipts[0].CommandID != original.CommandID {
		t.Fatalf("original receipt: %+v", recovered)
	}
	out, diagnostic, err = invoke(t, append(f.flags(), "--journal", journalPath, "--command-id", original.CommandID, "receipt")...)
	if err != nil {
		t.Fatalf("receipt: %v %s", err, diagnostic)
	}
	var receipt api.Receipt
	if err = api.Decode(out, &receipt); err != nil || !api.Equal(receipt, recovered.Receipts[0]) {
		t.Fatalf("same decision was not retained: %v", err)
	}
	out, diagnostic, err = invoke(t, append(f.flags(), "--journal", journalPath, "--request", requestPath, "command")...)
	if err != nil {
		t.Fatalf("unchanged completed original replay: %v %s", err, diagnostic)
	}
	if err = api.Decode(out, &receipt); err != nil || !api.Equal(receipt, recovered.Receipts[0]) {
		t.Fatalf("replay did not retain the original decision: %v", err)
	}
	q := api.Query{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: f.config.OwnerID, QueryID: api.NewID("query"), Method: "session.read", TargetID: sessionID, Payload: api.Raw(interaction.ReadInput{Revision: 0})}
	queryPath := filepath.Join(f.root, "query.json")
	if err = os.WriteFile(queryPath, api.Raw(q), 0600); err != nil {
		t.Fatal(err)
	}
	out, diagnostic, err = invoke(t, append(f.flags(), "--request", queryPath, "query")...)
	if err != nil {
		t.Fatalf("query: %v %s", err, diagnostic)
	}
	var view interaction.SessionView
	if err = api.Decode(out, &view); err != nil || view.Session.SessionID != sessionID || len(view.Branches) != 1 {
		t.Fatalf("durable session view: %+v %v", view, err)
	}
	original.ExpiresAt = api.Time(time.Now().Add(time.Hour))
	if err = os.WriteFile(requestPath, api.Raw(original), 0600); err != nil {
		t.Fatal(err)
	}
	_, diagnostic, err = invoke(t, append(f.flags(), "--journal", journalPath, "--request", requestPath, "command")...)
	if err == nil || !strings.Contains(string(diagnostic), "idempotency_conflict") {
		t.Fatalf("changed deadline accepted: %v %s", err, diagnostic)
	}
}

func invoke(t *testing.T, args ...string) ([]byte, []byte, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, cliBinary(t), args...)
	var out, diagnostic strings.Builder
	cmd.Stdout, cmd.Stderr = &out, &diagnostic
	err := cmd.Run()
	return []byte(out.String()), []byte(diagnostic.String()), err
}

func (f cliFixture) flags() []string {
	return []string{"--endpoint", f.server.URL, "--ca-file", f.ca, "--token-file", f.config.TokenFile}
}

func TestCLIDiscoveryRequiresAuthenticatedTLS(t *testing.T) {
	f := fixture(t)
	out, diagnostic, err := invoke(t, append(f.flags(), "discover")...)
	if err != nil {
		t.Fatalf("discovery: %v %s", err, diagnostic)
	}
	var d harness.Discovery
	if err = api.DecodeLimit(out, &d, 1<<20); err != nil {
		t.Fatal(err)
	}
	if d.LogicalServiceID != f.config.OwnerID || d.SchemaDigest != api.CoreDigest() || len(d.Methods) < 10 {
		t.Fatalf("discovery did not verify exact owner/schema: %+v", d)
	}
	_, diagnostic, err = invoke(t, "--endpoint", f.server.URL, "--token-file", f.config.TokenFile, "discover")
	if err == nil || !strings.Contains(string(diagnostic), "dependency_unavailable") {
		t.Fatalf("untrusted TLS: %v %s", err, diagnostic)
	}
	bad := filepath.Join(f.root, "bad-token")
	if err = os.WriteFile(bad, []byte("private-invalid-test-token"), 0600); err != nil {
		t.Fatal(err)
	}
	_, diagnostic, err = invoke(t, "--endpoint", f.server.URL, "--ca-file", f.ca, "--token-file", bad, "discover")
	if err == nil || !strings.Contains(string(diagnostic), "forbidden") || strings.Contains(string(diagnostic), "private-invalid-test-token") {
		t.Fatalf("credential failure was not redacted: %v %s", err, diagnostic)
	}
}

func TestCLIWSSAndGRPCUseAuthenticatedDiscoveryAndClosedQuery(t *testing.T) {
	f := fixture(t)
	server, err := rpcadapter.New(rpcadapter.Config{OwnerID: f.config.OwnerID, Identity: f.app.Identity, Processor: wss.LocalProcessor{Dispatcher: f.app.Dispatcher}})
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- server.Serve(ctx, listener, &tls.Config{MinVersion: tls.VersionTLS13, Certificates: f.server.TLS.Certificates})
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(6 * time.Second):
			t.Error("gRPC server did not actually exit")
		}
	})
	q := api.Query{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: f.config.OwnerID, QueryID: api.NewID("query"), Method: "session.list", TargetID: f.config.OwnerID, Payload: api.Raw(api.ListInput{Limit: 1})}
	path := filepath.Join(f.root, "query.json")
	if err = os.WriteFile(path, api.Raw(q), 0600); err != nil {
		t.Fatal(err)
	}
	addresses := []string{strings.Replace(f.server.URL, "https://", "wss://", 1) + "/connect", "grpcs://" + listener.Addr().String()}
	for _, address := range addresses {
		t.Run(strings.Split(address, ":")[0], func(t *testing.T) {
			flags := []string{"--endpoint", address, "--discovery", f.server.URL, "--ca-file", f.ca, "--token-file", f.config.TokenFile, "--request", path}
			out, diagnostic, err := invoke(t, append(flags, "query")...)
			if err != nil {
				t.Fatalf("secure query: %v %s", err, diagnostic)
			}
			var page api.Page[api.Session]
			if err = api.Decode(out, &page); err != nil || len(page.Items) != 1 || page.Items[0].OwnerID != f.config.OwnerID || page.Items[0].State != "open" || !page.Exhausted || page.Partial {
				t.Fatalf("query view: %s %v", out, err)
			}
			bad := strings.TrimSuffix(string(api.Raw(q)), "}") + `,"extra":"not-in-contract"}`
			if err = os.WriteFile(path, []byte(bad), 0600); err != nil {
				t.Fatal(err)
			}
			_, diagnostic, err = invoke(t, append(flags, "query")...)
			if err == nil || !strings.Contains(string(diagnostic), "invalid_request") {
				t.Fatalf("unknown query field accepted: %v %s", err, diagnostic)
			}
			if err = os.WriteFile(path, api.Raw(q), 0600); err != nil {
				t.Fatal(err)
			}
		})
	}
}
