package main_test

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/interaction"
	harness "github.com/ruipengliu/lerna/sdk/go"
)

func TestCLIFullJournalRejectsNewCommandBeforeActualHTTPCall(t *testing.T) {
	var sends atomic.Int32
	f := fixture(t, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "POST" && r.URL.Path == "/api/call" {
				sends.Add(1)
			}
			next.ServeHTTP(w, r)
		})
	})
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	token, err := os.ReadFile(f.config.TokenFile)
	if err != nil {
		t.Fatal(err)
	}
	discovery, err := (&harness.HTTPTransport{BaseURL: f.server.URL, Token: strings.TrimSpace(string(token)), HTTP: f.server.Client()}).Discover(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var method api.MethodContract
	for _, m := range discovery.Methods {
		if m.Name == "session.create" {
			method = m
		}
	}
	original := api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: f.config.OwnerID, CommandID: api.NewID("command"), Method: "session.create", TargetID: f.config.OwnerID, ExpiresAt: api.Time(time.Now().Add(time.Minute)), Payload: api.Raw(interaction.CreateSessionInput{SessionID: api.NewID("session"), DefaultBranchID: api.NewID("branch"), ConfigRef: f.app.TaskPolicy.PolicyRef})}
	root := t.TempDir()
	for range 4095 {
		command := original
		command.CommandID = api.NewID("command")
		digest, err := api.Digest(command)
		if err != nil {
			t.Fatal(err)
		}
		// 旧已拒绝命令墓碑不再需要出站，也必须占据保留容量。
		receipt := &api.Receipt{CommandID: command.CommandID, RequestDigest: digest, Stage: "rejected", DecidedAt: api.Time(time.Now()), Error: api.E("unsupported", "retained_historical_method")}
		entry := harness.Entry{IdentityScope: discovery.IdentityScope, SchemaDigest: discovery.SchemaDigest, MethodSchemaDigest: method.SchemaDigest, Command: command, Digest: digest, Receipt: receipt}
		if err = os.WriteFile(filepath.Join(root, command.CommandID+".json"), api.Raw(entry), 0600); err != nil {
			t.Fatal(err)
		}
	}
	request := filepath.Join(f.root, "full-journal-command.json")
	if err = os.WriteFile(request, api.Raw(original), 0600); err != nil {
		t.Fatal(err)
	}
	_, diagnostic, err := invoke(t, append(f.flags(), "--journal", root, "--request", request, "command")...)
	if err == nil || !strings.Contains(string(diagnostic), "journal_capacity") || sends.Load() != 0 {
		t.Fatalf("CLI physically sent an unrecoverable identity: sends=%d err=%v diagnostic=%s", sends.Load(), err, diagnostic)
	}
	if _, err = os.Stat(filepath.Join(root, original.CommandID+".json")); !os.IsNotExist(err) {
		t.Fatalf("CLI rejected command became a persisted responsibility: %v", err)
	}
}
