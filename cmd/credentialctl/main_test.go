package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	sqliteauth "lerna/adapters/authorization/sqlite"
	"lerna/adapters/credentials/filekeys"
	sqlitecredentials "lerna/adapters/credentials/sqlite"
	"lerna/authorization"
	"lerna/credentials"
	wire "lerna/gen/harness/v1"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestManagementStoresAndRewrapsWithoutSecretOutput(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	dir := filepath.Join(root, "keys")
	var output bytes.Buffer
	if e := run(ctx, []string{"init-key", "-key-dir", dir}, strings.NewReader(""), &output); e != nil {
		t.Fatal(e)
	}
	db, e := sqliteauth.Open(filepath.Join(root, "auth.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	a, e := authorization.New(db, authorization.SystemClock{}, authorization.Config{CredentialTTL: 24 * time.Hour, GrantTTL: time.Hour, WindowTTL: time.Minute, ReceiptRetention: time.Hour, MaxRules: 16, MaxResources: 16, MaxDepth: 4, MaxWork: 256, EvaluationTimeout: time.Second})
	if e != nil {
		t.Fatal(e)
	}
	token, e := a.Bootstrap(ctx, "local", "alice")
	if e != nil {
		t.Fatal(e)
	}
	scope := &wire.AuthorizationScope{Resources: &wire.ResourceSelector{Selection: &wire.ResourceSelector_Exact{Exact: "root"}}, Actions: []string{"credential.manage", "credential.use"}, Purposes: []string{"task"}, Locations: []string{"local"}, ExpiresUnix: time.Now().Add(time.Hour).Unix()}
	for i, cmd := range []*wire.AuthorizationCommand{{Change: &wire.AuthorizationCommand_ReplacePolicy{ReplacePolicy: &wire.ReplacePolicy{Rules: []*wire.PolicyRule{{Id: "allow", Scope: scope}}}}}, {Change: &wire.AuthorizationCommand_IssueGrant{IssueGrant: &wire.LocalGrant{Id: "manager", Subject: "alice", Scope: scope, Mode: "continuous"}}}} {
		op, e := a.NewOperation(ctx, token)
		if e != nil {
			t.Fatal(e)
		}
		cmd.ExpectedRevision = uint64(i)
		if _, e = a.Execute(ctx, token, authorization.Mutation{Namespace: "local", OperationID: op, Command: cmd}); e != nil {
			t.Fatal(e)
		}
	}
	tokenFile := filepath.Join(root, "token")
	if e = os.WriteFile(tokenFile, []byte(token), 0600); e != nil {
		t.Fatal(e)
	}
	var providerMu sync.Mutex
	var providerOp string
	providerSends := 0
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		providerMu.Lock()
		defer providerMu.Unlock()
		if r.Header.Get("Authorization") != "Bearer "+base64.RawStdEncoding.EncodeToString([]byte("cli-fixture-secret")) {
			w.WriteHeader(403)
			return
		}
		switch r.URL.Path {
		case "/renew":
			providerSends++
			providerOp = r.Header.Get("Idempotency-Key")
			w.WriteHeader(204)
		case "/inspect":
			if providerOp == "" || r.Header.Get("Idempotency-Key") != providerOp {
				w.WriteHeader(403)
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"state": "applied", "secret": []byte("cli-renewed-secret"), "expires_unix": time.Now().Add(2 * time.Hour).Unix()})
		default:
			w.WriteHeader(404)
		}
	}))
	defer provider.Close()
	cfg := configuration{AuthDB: filepath.Join(root, "auth.db"), TokenFile: tokenFile, CredentialDB: filepath.Join(root, "credentials.db"), KeyDirectory: dir, Resource: "root", Binding: credentials.Binding{Namespace: "local", Subject: "alice", Driver: "http-v1", Service: provider.URL, Account: "alice", Purpose: "task", Location: "local"}}
	cfg.BackupDirectory = filepath.Join(root, "backups")
	cfg.ArchiveID = "cli-archive"
	cfg.Renewal = &renewalConfiguration{ProviderID: "cli-provider", RenewPath: "/renew", InspectPath: "/inspect", AllowLoopbackHTTP: true, ReplaySafe: true}
	configPath := filepath.Join(root, "config.json")
	raw, _ := json.Marshal(cfg)
	os.WriteFile(configPath, raw, 0600)
	expires := time.Now().Add(time.Hour).Unix()
	output.Reset()
	e = run(ctx, []string{"put", "-config", configPath, "-ref", "credential", "-expires", strconv.FormatInt(expires, 10)}, strings.NewReader("cli-fixture-secret"), &output)
	if e != nil {
		t.Fatal(e)
	}
	if bytes.Contains(output.Bytes(), []byte("cli-fixture-secret")) {
		t.Fatal("secret in CLI output")
	}
	var before credentials.Record
	if json.Unmarshal(output.Bytes(), &before) != nil {
		t.Fatal("invalid initial receipt")
	}
	output.Reset()
	if e = run(ctx, []string{"backup", "-config", configPath, "-operation", "cli-backup"}, strings.NewReader(""), &output); e != nil {
		t.Fatal(e)
	}
	output.Reset()
	if e = run(ctx, []string{"rotate-key", "-config", configPath, "-operation", "cli-rotation"}, strings.NewReader(""), &output); e != nil {
		t.Fatal(e)
	}
	output.Reset()
	if e = run(ctx, []string{"rotation-step", "-config", configPath, "-operation", "cli-rotation"}, strings.NewReader(""), &output); e != nil {
		t.Fatal(e)
	}
	output.Reset()
	if e = run(ctx, []string{"rotation-step", "-config", configPath, "-operation", "cli-rotation"}, strings.NewReader(""), &output); e != nil {
		t.Fatal(e)
	}
	var rotated credentials.Rotation
	if json.Unmarshal(output.Bytes(), &rotated) != nil || rotated.Phase != "completed" {
		t.Fatal("CLI rotation not completed")
	}
	output.Reset()
	if e = run(ctx, []string{"rewrap", "-config", configPath, "-ref", "credential", "-expected", "2"}, strings.NewReader(""), &output); e != nil {
		t.Fatal(e)
	}
	var r credentials.Record
	if json.Unmarshal(output.Bytes(), &r) != nil || r.Revision != 3 || len(r.Ciphertext) != 0 {
		t.Fatal("unsafe management receipt")
	}

	output.Reset()
	if e = run(ctx, []string{"retire-key", "-config", configPath, "-key-version", before.KeyVersion}, strings.NewReader(""), &output); e != credentials.Denied {
		t.Fatal("CLI removed referenced key", e)
	}
	if e = run(ctx, []string{"dispose-backup", "-config", configPath, "-operation", "cli-backup"}, strings.NewReader(""), &output); e != nil {
		t.Fatal(e)
	}
	output.Reset()
	if e = run(ctx, []string{"retire-key", "-config", configPath, "-key-version", before.KeyVersion}, strings.NewReader(""), &output); e != nil {
		t.Fatal(e)
	}
	output.Reset()
	if e = run(ctx, []string{"renew", "-config", configPath, "-operation", "cli-renew", "-ref", "credential"}, strings.NewReader(""), &output); e != nil {
		t.Fatal(e)
	}
	providerMu.Lock()
	sends := providerSends
	providerMu.Unlock()
	if sends != 0 {
		t.Fatal("renew intent started external request")
	}
	for i := 0; i < 2; i++ {
		output.Reset()
		if e = run(ctx, []string{"renewal-step", "-config", configPath, "-operation", "cli-renew"}, strings.NewReader(""), &output); e != nil {
			t.Fatal(e)
		}
	}
	var renewed credentials.Renewal
	if json.Unmarshal(output.Bytes(), &renewed) != nil || renewed.Phase != "completed" || bytes.Contains(output.Bytes(), []byte("cli-renewed-secret")) {
		t.Fatal("CLI renewal incomplete or disclosed secret")
	}
	providerMu.Lock()
	sends = providerSends
	providerMu.Unlock()
	if sends != 1 {
		t.Fatal("CLI resent renewal")
	}

	t.Run("stalled stdin is bounded without a write", func(t *testing.T) {
		reader, writer, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		defer reader.Close()
		defer writer.Close()
		bounded, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		cmd := exec.CommandContext(bounded, os.Args[0], "-test.run=^TestStdinProcessProbe$")
		cmd.Env = append(os.Environ(), "HARNESS_STDIN_PROBE="+configPath)
		cmd.Stdin = reader
		if err := cmd.Run(); err != nil {
			t.Fatalf("stalled input did not return bounded error: %v", err)
		}
		store, err := sqlitecredentials.Open(cfg.CredentialDB)
		if err != nil {
			t.Fatal(err)
		}
		defer store.Close()
		if _, err = store.Get(ctx, "stalled"); err != credentials.Missing {
			t.Fatal("stalled request changed store", err)
		}
	})
	k, e := filekeys.Open(dir, filekeys.MaxUses)
	if e != nil {
		t.Fatal(e)
	}
	defer k.Close()
	key, e := k.Read(ctx, r.KeyVersion)
	clear(key)
	if e != nil {
		t.Fatal(e)
	}
}

func TestStdinProcessProbe(t *testing.T) {
	path := os.Getenv("HARNESS_STDIN_PROBE")
	if path == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	e := run(ctx, []string{"put", "-config", path, "-ref", "stalled", "-expires", strconv.FormatInt(time.Now().Add(time.Hour).Unix(), 10)}, os.Stdin, &bytes.Buffer{})
	if e != credentials.Unavailable {
		os.Exit(2)
	}
	os.Exit(0)
}
