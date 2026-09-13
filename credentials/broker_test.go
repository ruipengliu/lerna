package credentials_test

import (
	"context"
	"encoding/binary"
	"lerna/credentials"
	"sync"
	"testing"
	"time"
)

type store struct {
	mu      sync.Mutex
	records map[string]credentials.Record
}

func (s *store) Get(_ context.Context, ref string) (credentials.Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.records[ref]
	if !ok {
		return r, credentials.Missing
	}
	r.Nonce = append([]byte(nil), r.Nonce...)
	r.Ciphertext = append([]byte(nil), r.Ciphertext...)
	return r, nil
}
func (s *store) Swap(_ context.Context, v uint64, r credentials.Record) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.records[r.Ref].Revision != v {
		return credentials.Conflict
	}
	s.records[r.Ref] = r
	return nil
}
func (s *store) List(_ context.Context) ([]credentials.Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []credentials.Record
	for _, r := range s.records {
		out = append(out, r)
	}
	return out, nil
}

type keys struct {
	mu   sync.Mutex
	used uint64
	key  []byte
}

func (k *keys) Reserve(context.Context) (string, []byte, []byte, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.used++
	nonce := make([]byte, 12)
	binary.BigEndian.PutUint64(nonce[4:], k.used)
	return "key1", append([]byte(nil), k.key...), nonce, nil
}
func (k *keys) Read(context.Context, string) ([]byte, error) {
	return append([]byte(nil), k.key...), nil
}

type auth struct{}

func (auth) Check(_ context.Context, token string, b credentials.Binding, verb string) error {
	if token != "trusted" || b.Subject != "alice" {
		return credentials.Denied
	}
	return nil
}

type clock struct{ now time.Time }

func (c *clock) Now() (time.Time, error) { return c.now, nil }

type exit struct {
	target   credentials.Binding
	calls    int
	received string
}

func (e *exit) Send(_ context.Context, _ credentials.Call, secret []byte) (credentials.Result, error) {
	e.calls++
	e.received = string(secret)
	return credentials.Result{Accepted: true}, nil
}
func TestCredentialOnlyReachesBoundDriver(t *testing.T) {
	ctx := context.Background()
	s := &store{records: map[string]credentials.Record{}}
	k := &keys{key: make([]byte, 32)}
	c := &clock{time.Unix(1800000000, 0)}
	b, err := credentials.New(s, k, auth{}, c, credentials.Config{Timeout: time.Second, MaxConcurrent: 4})
	if err != nil {
		t.Fatal(err)
	}
	target := credentials.Binding{Namespace: "local", Subject: "alice", Driver: "orders-v1", Service: "orders", Account: "alice-account", Purpose: "task", Location: "local"}
	if _, err = b.Put(ctx, "trusted", "cred-1", target, 0, c.now.Add(time.Hour).Unix(), []byte("fixture-secret")); err != nil {
		t.Fatal(err)
	}
	e := &exit{target: target}
	d, err := b.Bind(target, e)
	if err != nil {
		t.Fatal(err)
	}
	result, err := d.Use(ctx, "trusted", "cred-1", credentials.Call{OperationID: "op-1", Payload: []byte(`{"amount":3}`)})
	if err != nil || !result.Accepted || e.calls != 1 || e.received != "fixture-secret" {
		t.Fatal("bound exit did not receive credential", err)
	}
	target.Account = "other"
	wrongExit := &exit{target: target}
	wrong, _ := b.Bind(target, wrongExit)
	if _, err = wrong.Use(ctx, "trusted", "cred-1", credentials.Call{OperationID: "op-2"}); err != credentials.Denied || e.calls != 1 {
		t.Fatal("wrong account reached exit", err)
	}
}

func TestCredentialRejectsTamperingExpiryAndSecretErrors(t *testing.T) {
	ctx := context.Background()
	s := &store{records: map[string]credentials.Record{}}
	k := &keys{key: make([]byte, 32)}
	c := &clock{time.Unix(1800000000, 0)}
	b, _ := credentials.New(s, k, auth{}, c, credentials.Config{Timeout: time.Second, MaxConcurrent: 4})
	target := credentials.Binding{Namespace: "local", Subject: "alice", Driver: "orders-v1", Service: "orders", Account: "alice-account", Purpose: "task", Location: "local"}
	if _, e := b.Put(ctx, "trusted", "cred", target, 0, c.now.Add(time.Hour).Unix(), []byte("secret-echo")); e != nil {
		t.Fatal(e)
	}
	e := &exit{target: target}
	d, _ := b.Bind(target, e)
	original, _ := s.Get(ctx, "cred")
	corrupt := original
	corrupt.ExpiresUnix++
	s.records["cred"] = corrupt
	if _, err := d.Use(ctx, "trusted", "cred", credentials.Call{OperationID: "op"}); err != credentials.Denied || e.calls != 0 {
		t.Fatal("metadata tamper", err)
	}
	s.records["cred"] = original
	c.now = c.now.Add(time.Hour)
	if _, err := d.Use(ctx, "trusted", "cred", credentials.Call{OperationID: "op"}); err != credentials.Expired || e.calls != 0 {
		t.Fatal("expired secret used", err)
	}
	c.now = c.now.Add(-time.Hour)
	bad, _ := b.Bind(target, reflectingExit{target: target})
	if _, err := bad.Use(ctx, "trusted", "cred", credentials.Call{OperationID: "op"}); err != credentials.TargetUnavailable {
		t.Fatal("raw error escaped", err)
	}
}

type reflectingExit struct{ target credentials.Binding }

func (reflectingExit) Send(_ context.Context, _ credentials.Call, s []byte) (credentials.Result, error) {
	return credentials.Result{}, credentials.Error(string(s))
}

func (e *exit) Target() credentials.Binding          { return e.target }
func (e reflectingExit) Target() credentials.Binding { return e.target }
