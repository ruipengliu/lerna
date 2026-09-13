package credentialhttp_test

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"lerna/adapters/credentialhttp"
	"lerna/credentials"
	_ "modernc.org/sqlite"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"
)

func TestRenewalChecksCommittedEffectAfterLostHTTPReply(t *testing.T) {
	db, e := sql.Open("sqlite", filepath.Join(t.TempDir(), "provider.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if _, e = db.Exec("CREATE TABLE tokens(current TEXT); INSERT INTO tokens VALUES('old-token'); CREATE TABLE renewals(op TEXT PRIMARY KEY, old TEXT, next TEXT)"); e != nil {
		t.Fatal(e)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, e := base64.RawStdEncoding.DecodeString(r.Header.Get("Authorization")[7:])
		if e != nil || r.Header.Get("X-Harness-Account") != "alice" {
			w.WriteHeader(403)
			return
		}
		op := r.Header.Get("Idempotency-Key")
		switch r.URL.Path {
		case "/renew":
			tx, e := db.BeginTx(r.Context(), nil)
			if e != nil {
				w.WriteHeader(500)
				return
			}
			defer tx.Rollback()
			var prior string
			e = tx.QueryRow("SELECT old FROM renewals WHERE op=?", op).Scan(&prior)
			if e == sql.ErrNoRows {
				var current string
				if tx.QueryRow("SELECT current FROM tokens").Scan(&current) != nil || string(raw) != current {
					w.WriteHeader(403)
					return
				}
				if _, e = tx.Exec("INSERT INTO renewals VALUES(?,?,?); UPDATE tokens SET current='new-token'", op, string(raw), "new-token"); e != nil {
					w.WriteHeader(500)
					return
				}
			} else if e != nil || prior != string(raw) {
				w.WriteHeader(403)
				return
			}
			if tx.Commit() != nil {
				w.WriteHeader(500)
				return
			}
			conn, _, e := w.(http.Hijacker).Hijack()
			if e == nil {
				conn.Close()
			}
		case "/inspect":
			var old, next string
			if db.QueryRow("SELECT old,next FROM renewals WHERE op=?", op).Scan(&old, &next) != nil || old != string(raw) {
				w.WriteHeader(403)
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"state": "applied", "secret": []byte(next), "expires_unix": 1800007200})
		case "/use":
			var current string
			if db.QueryRow("SELECT current FROM tokens").Scan(&current) != nil || current != string(raw) {
				w.WriteHeader(403)
				return
			}
			w.WriteHeader(204)
		default:
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	binding := credentials.Binding{Namespace: "local", Subject: "alice", Account: "alice", Driver: "api-v1", Purpose: "task", Location: "local", Service: server.URL}
	p, e := credentialhttp.NewRenewal(credentialhttp.RenewalConfig{Binding: binding, ProviderID: "test-provider", RenewPath: "/renew", InspectPath: "/inspect", Timeout: time.Second, AllowLoopbackHTTP: true, ReplaySafe: true})
	if e != nil {
		t.Fatal(e)
	}
	defer p.Close()
	ctx := context.Background()
	if e = p.Renew(ctx, "renew-op", []byte("old-token")); e == nil {
		t.Fatal("reply was not lost")
	}
	observed, e := p.Inspect(ctx, "renew-op", []byte("old-token"))
	if e != nil || observed.State != "applied" {
		t.Fatal("committed operation not found", e)
	}
	defer clear(observed.Secret)
	exit, e := credentialhttp.New(credentialhttp.Config{Binding: binding, Path: "/use", Timeout: time.Second, AllowLoopbackHTTP: true})
	if e != nil {
		t.Fatal(e)
	}
	defer exit.Close()
	if _, e = exit.Send(ctx, credentials.Call{OperationID: "use-new"}, observed.Secret); e != nil {
		t.Fatal("new token unusable", e)
	}
	if _, e = exit.Send(ctx, credentials.Call{OperationID: "use-old"}, []byte("old-token")); e == nil {
		t.Fatal("old token remained valid")
	}
	var effects int
	if e = db.QueryRow("SELECT count(*) FROM renewals").Scan(&effects); e != nil || effects != 1 {
		t.Fatal("wrong renewal effect count", e)
	}
}
