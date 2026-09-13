package credentialcheck

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"lerna/adapters/credentialauth"
	"lerna/adapters/credentialhttp"
	"lerna/adapters/filekeys"
	"lerna/adapters/sqliteauth"
	"lerna/adapters/sqlitecredentials"
	"lerna/authorization"
	"lerna/credentials"
	wire "lerna/gen/harness/v1"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const fixtureSecret = "public-fixture-api-key-15"

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) Now() (time.Time, error) { c.mu.Lock(); defer c.mu.Unlock(); return c.t, nil }
func (c *clock) advance(d time.Duration) { c.mu.Lock(); defer c.mu.Unlock(); c.t = c.t.Add(d) }

type fixture struct {
	root, token string
	clock       *clock
	db          *sqliteauth.Store
	auth        *authorization.Service
	store       *sqlitecredentials.Store
	keys        *filekeys.Source
	broker      *credentials.Broker
	binding     credentials.Binding
	exit        *credentialhttp.Exit
	driver      *credentials.Driver
	target      *sql.DB
	server      *httptest.Server
	mu          sync.Mutex
	requests    int
	reflection  bool
}

func (f *fixture) close() {
	if f.server != nil {
		f.server.Close()
	}
	if f.exit != nil {
		f.exit.Close()
	}
	if f.target != nil {
		f.target.Close()
	}
	if f.store != nil {
		f.store.Close()
	}
	if f.keys != nil {
		f.keys.Close()
	}
	if f.db != nil {
		f.db.Close()
	}
	if f.root != "" {
		os.RemoveAll(f.root)
	}
}
func (f *fixture) setPolicy(ctx context.Context, actions []string) error {
	now, _ := f.clock.Now()
	scope := &wire.AuthorizationScope{Resources: &wire.ResourceSelector{Selection: &wire.ResourceSelector_Exact{Exact: "root"}}, Actions: actions, Purposes: []string{"task"}, Locations: []string{"local"}, ExpiresUnix: now.Add(time.Hour).Unix()}
	for _, grant := range []bool{false, true} {
		op, e := f.auth.NewOperation(ctx, f.token)
		if e != nil {
			return e
		}
		snap, e := f.db.Load(ctx)
		if e != nil {
			return e
		}
		cmd := &wire.AuthorizationCommand{ExpectedRevision: snap.State.Revision, Change: &wire.AuthorizationCommand_ReplacePolicy{ReplacePolicy: &wire.ReplacePolicy{Rules: []*wire.PolicyRule{{Id: "credentials", Scope: scope}}}}}
		if grant {
			cmd.Change = &wire.AuthorizationCommand_IssueGrant{IssueGrant: &wire.LocalGrant{Id: fmt.Sprintf("credential-%d", snap.State.Revision), Subject: "alice", Scope: scope, Mode: "continuous"}}
		}
		if _, e = f.auth.Execute(ctx, f.token, authorization.Mutation{Namespace: "local", OperationID: op, Command: cmd}); e != nil {
			return e
		}
	}
	return nil
}

func newFixture(ctx context.Context) (f *fixture, err error) {
	f = &fixture{clock: &clock{t: time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC)}}
	defer func() {
		if err != nil {
			f.close()
		}
	}()
	f.root, err = os.MkdirTemp("", "credentials-profile-")
	if err != nil {
		return f, err
	}
	f.db, err = sqliteauth.Open(filepath.Join(f.root, "auth.db"))
	if err != nil {
		return f, err
	}
	f.auth, err = authorization.New(f.db, f.clock, authorization.Config{CredentialTTL: 24 * time.Hour, GrantTTL: time.Hour, WindowTTL: time.Minute, ReceiptRetention: time.Hour, MaxRules: 32, MaxResources: 128, MaxDepth: 16, MaxWork: 4096, EvaluationTimeout: time.Second})
	if err != nil {
		return f, err
	}
	f.token, err = f.auth.Bootstrap(ctx, "local", "alice")
	if err != nil {
		return f, err
	}
	if err = f.setPolicy(ctx, []string{"credential.manage", "credential.use"}); err != nil {
		return f, err
	}
	if err = os.Mkdir(filepath.Join(f.root, "keys"), 0700); err != nil {
		return f, err
	}
	f.keys, err = filekeys.Open(filepath.Join(f.root, "keys"), filekeys.MaxUses)
	if err != nil {
		return f, err
	}
	if _, err = f.keys.Generate(ctx); err != nil {
		return f, err
	}
	f.store, err = sqlitecredentials.Open(filepath.Join(f.root, "credentials.db"))
	if err != nil {
		return f, err
	}
	f.target, err = sql.Open("sqlite", filepath.Join(f.root, "target.db"))
	if err != nil {
		return f, err
	}
	f.target.SetMaxOpenConns(1)
	if _, err = f.target.Exec(`CREATE TABLE effects(op TEXT PRIMARY KEY,delta INTEGER NOT NULL)`); err != nil {
		return f, err
	}
	f.server = httptest.NewServer(http.HandlerFunc(f.serve))
	f.binding = credentials.Binding{Namespace: "local", Subject: "alice", Driver: "counter-v1", Service: f.server.URL, Account: "alice-account", Purpose: "task", Location: "local"}
	a, e := credentialauth.New(f.auth, "root")
	if e != nil {
		return f, e
	}
	f.broker, err = credentials.New(f.store, f.keys, a, f.clock, credentials.Config{Timeout: time.Second, MaxConcurrent: 4})
	if err != nil {
		return f, err
	}
	f.exit, err = credentialhttp.New(credentialhttp.Config{Binding: f.binding, Path: "/apply", Timeout: time.Second, AllowLoopbackHTTP: true})
	if err != nil {
		return f, err
	}
	f.driver, err = f.broker.Bind(f.binding, f.exit)
	if err != nil {
		return f, err
	}
	now, _ := f.clock.Now()
	_, err = f.broker.Put(ctx, f.token, "credential", f.binding, 0, now.Add(time.Hour).Unix(), []byte(fixtureSecret))
	return f, err
}
func (f *fixture) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.requests++
	reflect := f.reflection
	f.mu.Unlock()
	if reflect {
		http.Error(w, fixtureSecret, http.StatusBadGateway)
		return
	}
	if r.Method != "POST" || r.URL.Path != "/apply" || subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+base64.RawStdEncoding.EncodeToString([]byte(fixtureSecret)))) != 1 || r.Header.Get("X-Harness-Account") != "alice-account" || r.Header.Get("X-Harness-Driver") != "counter-v1" || r.Header.Get("X-Harness-Purpose") != "task" {
		w.WriteHeader(http.StatusForbidden)
		return
	}
	var in struct{ Delta int }
	d := json.NewDecoder(io.LimitReader(r.Body, 4097))
	d.DisallowUnknownFields()
	if d.Decode(&in) != nil || in.Delta < 1 || in.Delta > 9 {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	op := r.Header.Get("Idempotency-Key")
	if op == "" || len(op) > 256 {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	tx, e := f.target.BeginTx(r.Context(), nil)
	if e != nil {
		w.WriteHeader(500)
		return
	}
	defer tx.Rollback()
	var delta int
	e = tx.QueryRowContext(r.Context(), "SELECT delta FROM effects WHERE op=?", op).Scan(&delta)
	if e == nil && delta != in.Delta {
		w.WriteHeader(409)
		return
	}
	if e != nil && e != sql.ErrNoRows {
		w.WriteHeader(500)
		return
	}
	if e == sql.ErrNoRows {
		if _, e = tx.ExecContext(r.Context(), "INSERT INTO effects(op,delta) VALUES(?,?)", op, in.Delta); e != nil {
			w.WriteHeader(500)
			return
		}
	}
	if e = tx.Commit(); e != nil {
		w.WriteHeader(500)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
func (f *fixture) use(ctx context.Context, ref, op string) error {
	r, e := f.driver.Use(ctx, f.token, ref, credentials.Call{OperationID: op, Payload: []byte(`{"Delta":3}`)})
	if e != nil {
		return e
	}
	if !r.Accepted {
		return credentials.TargetUnavailable
	}
	return nil
}
func (f *fixture) value(ctx context.Context) (int, error) {
	var v int
	e := f.target.QueryRowContext(ctx, "SELECT coalesce(sum(delta),0) FROM effects").Scan(&v)
	return v, e
}
func (f *fixture) sent() int { f.mu.Lock(); defer f.mu.Unlock(); return f.requests }

func (f *fixture) rebind() error {
	a, e := credentialauth.New(f.auth, "root")
	if e != nil {
		return e
	}
	f.broker, e = credentials.New(f.store, f.keys, a, f.clock, credentials.Config{Timeout: time.Second, MaxConcurrent: 4})
	if e != nil {
		return e
	}
	f.driver, e = f.broker.Bind(f.binding, f.exit)
	return e
}
