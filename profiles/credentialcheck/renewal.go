package credentialcheck

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	credentialauth "lerna/adapters/credentials/auth"
	credentialhttp "lerna/adapters/credentials/http"
	sqlitecredentials "lerna/adapters/credentials/sqlite"
	"lerna/credentials"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"time"
)

// The provider owns its token and operation truth in a separate SQLite
// database. It never reads the broker's records or the lifecycle journal.
func renewalServer(f *fixture, mode string) (*httptest.Server, error) {
	_, e := f.target.Exec("CREATE TABLE provider_tokens(current BLOB); CREATE TABLE provider_ops(op TEXT PRIMARY KEY, old BLOB, next BLOB); CREATE TABLE provider_attempts(op TEXT); CREATE TABLE provider_queries(op TEXT); CREATE TABLE provider_effects(op TEXT PRIMARY KEY)")
	if e != nil {
		return nil, e
	}
	if _, e = f.target.Exec("INSERT INTO provider_tokens VALUES(?)", []byte(fixtureSecret)); e != nil {
		return nil, e
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, e := base64.RawStdEncoding.DecodeString(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
		if e != nil || len(raw) == 0 || r.Header.Get("X-Harness-Account") != f.binding.Account || r.Header.Get("X-Harness-Driver") != f.binding.Driver || r.Header.Get("X-Harness-Purpose") != f.binding.Purpose {
			w.WriteHeader(403)
			return
		}
		defer clear(raw)
		op := r.Header.Get("Idempotency-Key")
		if len(op) == 0 || len(op) > 256 {
			w.WriteHeader(400)
			return
		}
		switch r.URL.Path {
		case "/renew":
			if r.Method != "POST" {
				w.WriteHeader(405)
				return
			}
			tx, e := f.target.BeginTx(r.Context(), nil)
			if e != nil {
				w.WriteHeader(500)
				return
			}
			defer tx.Rollback()
			if _, e = tx.Exec("INSERT INTO provider_attempts VALUES(?)", op); e != nil {
				w.WriteHeader(500)
				return
			}
			var old []byte
			e = tx.QueryRow("SELECT old FROM provider_ops WHERE op=?", op).Scan(&old)
			if e == sql.ErrNoRows {
				if tx.QueryRow("SELECT current FROM provider_tokens").Scan(&old) != nil || !bytes.Equal(old, raw) {
					w.WriteHeader(403)
					return
				}
				if mode != "not-occurred" && mode != "unsafe" {
					next := make([]byte, 32)
					if _, e = rand.Read(next); e != nil {
						w.WriteHeader(500)
						return
					}
					defer clear(next)
					if _, e = tx.Exec("INSERT INTO provider_ops VALUES(?,?,?)", op, raw, next); e != nil {
						w.WriteHeader(500)
						return
					}
					if _, e = tx.Exec("UPDATE provider_tokens SET current=?", next); e != nil {
						w.WriteHeader(500)
						return
					}
				}
			} else if e != nil || !bytes.Equal(old, raw) {
				w.WriteHeader(403)
				return
			}
			if tx.Commit() != nil {
				w.WriteHeader(500)
				return
			}
			if mode == "not-occurred" || mode == "unsafe" {
				w.WriteHeader(503)
				return
			}
			conn, _, e := w.(http.Hijacker).Hijack()
			if e == nil {
				conn.Close()
			}
		case "/inspect":
			if r.Method != "GET" {
				w.WriteHeader(405)
				return
			}
			var old, next []byte
			e = f.target.QueryRow("SELECT old,next FROM provider_ops WHERE op=?", op).Scan(&old, &next)
			defer clear(next)
			if e == sql.ErrNoRows {
				if f.target.QueryRow("SELECT current FROM provider_tokens").Scan(&old) != nil {
					w.WriteHeader(500)
					return
				}
			} else if e != nil {
				w.WriteHeader(500)
				return
			}
			if !bytes.Equal(old, raw) {
				w.WriteHeader(403)
				return
			}
			if _, e = f.target.Exec("INSERT INTO provider_queries VALUES(?)", op); e != nil {
				w.WriteHeader(500)
				return
			}
			if mode == "unknown" {
				json.NewEncoder(w).Encode(map[string]any{"state": "unknown"})
			} else if next == nil {
				json.NewEncoder(w).Encode(map[string]any{"state": "not_occurred"})
			} else {
				now, _ := f.clock.Now()
				json.NewEncoder(w).Encode(map[string]any{"state": "applied", "secret": next, "expires_unix": now.Add(2 * time.Hour).Unix()})
			}
		case "/use":
			if r.Method != "POST" {
				w.WriteHeader(405)
				return
			}
			var current []byte
			if f.target.QueryRow("SELECT current FROM provider_tokens").Scan(&current) != nil || !bytes.Equal(current, raw) {
				w.WriteHeader(403)
				return
			}
			if _, e = f.target.Exec("INSERT OR IGNORE INTO provider_effects VALUES(?)", op); e != nil {
				w.WriteHeader(500)
				return
			}
			w.WriteHeader(204)
		default:
			w.WriteHeader(404)
		}
	})), nil
}

func RenewalCheck(ctx context.Context, mode string) error {
	f, e := newFixture(ctx)
	if e != nil {
		return e
	}
	defer f.close()
	server, e := renewalServer(f, mode)
	if e != nil {
		return credentials.Unavailable
	}
	defer server.Close()
	target := f.binding
	target.Service = server.URL
	now, _ := f.clock.Now()
	if _, e = f.broker.Put(ctx, f.token, "renewable", target, 0, now.Add(time.Hour).Unix(), []byte(fixtureSecret)); e != nil {
		return e
	}
	p, e := credentialhttp.NewRenewal(credentialhttp.RenewalConfig{Binding: target, ProviderID: "stateful-provider", RenewPath: "/renew", InspectPath: "/inspect", Timeout: time.Second, AllowLoopbackHTTP: true, ReplaySafe: mode != "unsafe"})
	if e != nil {
		return e
	}
	defer p.Close()
	a, e := credentialauth.New(f.auth, "root")
	if e != nil {
		return e
	}
	create := func() (*credentials.Lifecycle, error) {
		l, e := credentials.NewLifecycle(f.store, f.keys, a, f.clock, credentials.Config{Timeout: time.Second, MaxConcurrent: 4})
		if e != nil {
			return nil, e
		}
		return l.WithProvider(p)
	}
	manager, e := create()
	if e != nil {
		return e
	}
	if mode == "target-mismatch" {
		wrong := target
		wrong.Account = "other"
		if _, e = manager.StartRenewal(ctx, f.token, "renew", "renewable", wrong); e != credentials.Denied {
			return credentials.Invalid
		}
	} else {
		if _, e = manager.StartRenewal(ctx, f.token, "renew", "renewable", target); e != nil {
			return e
		}
		if mode == "provider-change" {
			changed, e := credentialhttp.NewRenewal(credentialhttp.RenewalConfig{Binding: target, ProviderID: "stateful-provider", RenewPath: "/renew", InspectPath: "/changed-inspect", Timeout: time.Second, AllowLoopbackHTTP: true, ReplaySafe: true})
			if e != nil {
				return e
			}
			defer changed.Close()
			manager, e = manager.WithProvider(changed)
			if e != nil {
				return e
			}
			if _, e = manager.StepRenewal(ctx, f.token, "renew", target); e != credentials.Denied {
				return credentials.Invalid
			}
		} else if mode == "policy-revoked" {
			if e = f.setPolicy(ctx, []string{"credential.manage"}); e != nil {
				return e
			}
			if _, e = manager.StepRenewal(ctx, f.token, "renew", target); e != credentials.Denied {
				return credentials.Invalid
			}
		} else {
			first, e := manager.StepRenewal(ctx, f.token, "renew", target)
			if e != nil || first.Phase != "checking" || first.Attempts != 1 {
				return credentials.Invalid
			}
			f.store.Close()
			f.store, e = sqlitecredentials.Open(filepath.Join(f.root, "credentials.db"))
			if e != nil {
				return e
			}
			if e = f.rebind(); e != nil {
				return e
			}
			manager, e = create()
			if e != nil {
				return e
			}
			if mode == "during-rotation" {
				if _, e = manager.StartRotation(ctx, f.token, "rotate-renewable", target); e != nil {
					return e
				}
				if _, e = manager.StepRotation(ctx, f.token, "rotate-renewable", target); e != nil {
					return e
				}
			}
			var result credentials.Renewal
			for i := 0; i < 10; i++ {
				result, e = manager.StepRenewal(ctx, f.token, "renew", target)
				if e != nil {
					return e
				}
			}
			switch mode {
			case "unknown", "unsafe":
				checks := uint32(8)
				if mode == "unsafe" {
					checks = 1
				}
				if result.Phase != "needs_reconciliation" || result.Attempts != 1 || result.Checks != checks {
					return credentials.Invalid
				}
				if _, e = manager.StartRenewal(ctx, f.token, "new-identity", "renewable", target); e != credentials.Conflict {
					return credentials.Invalid
				}
				if e = f.use(ctx, "credential", "independent-original-service"); e != nil {
					return e
				}
			case "not-occurred":
				if _, e = manager.StartRenewal(ctx, f.token, "retry-new-identity", "renewable", target); e != credentials.Conflict {
					return credentials.Invalid
				}
				if result.Phase != "needs_reconciliation" || result.Attempts != 3 || result.Checks != 3 {
					return credentials.Invalid
				}
			default:
				if result.Phase != "completed" || result.Attempts != 1 || result.Checks != 1 {
					return credentials.Invalid
				}
				exit, e := credentialhttp.New(credentialhttp.Config{Binding: target, Path: "/use", Timeout: time.Second, AllowLoopbackHTTP: true})
				if e != nil {
					return e
				}
				defer exit.Close()
				driver, e := f.broker.Bind(target, exit)
				if e != nil {
					return e
				}
				if _, e = driver.Use(ctx, f.token, "renewable", credentials.Call{OperationID: "actual-use"}); e != nil {
					return e
				}
				if _, e = exit.Send(ctx, credentials.Call{OperationID: "old-token-use"}, []byte(fixtureSecret)); e == nil {
					return credentials.Invalid
				}
				if mode == "during-rotation" {
					rotated, e := manager.StepRotation(ctx, f.token, "rotate-renewable", target)
					if e != nil || rotated.Phase != "completed" {
						return credentials.Invalid
					}
				}
			}
		}
	}
	expectedAttempts, expectedQueries, expectedRenewals, expectedUses := 1, 1, 1, 1
	switch mode {
	case "target-mismatch", "policy-revoked", "provider-change":
		expectedAttempts, expectedQueries, expectedRenewals, expectedUses = 0, 0, 0, 0
	case "unknown":
		expectedQueries, expectedUses = 8, 0
	case "unsafe":
		expectedRenewals, expectedUses = 0, 0
	case "not-occurred":
		expectedAttempts, expectedQueries, expectedRenewals, expectedUses = 3, 3, 0, 0
	}
	for _, check := range []struct {
		table    string
		expected int
	}{{"provider_attempts", expectedAttempts}, {"provider_queries", expectedQueries}, {"provider_ops", expectedRenewals}, {"provider_effects", expectedUses}} {
		var actual int
		if e = f.target.QueryRow("SELECT count(*) FROM " + check.table).Scan(&actual); e != nil || actual != check.expected {
			return credentials.Invalid
		}
	}
	return nil
}
