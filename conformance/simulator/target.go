// Package simulator 提供独立目标的请求与效果记录；被测进程重启不重置这些记录。
package simulator

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

type Request struct {
	Method, ExternalKey, Attempt, Send, User, Operation, BodyDigest string
	ReceivedAtUnixNano                                              int64
}
type Effect struct {
	ExternalKey, BodyDigest string
	AppliedAtUnixNano       int64
}
type Target struct {
	mu       sync.Mutex
	mode     string
	behavior string
	requests []Request
	effects  []Effect
}

func New(mode string) *Target { return &Target{mode: mode} }
func (t *Target) SetBehavior(behavior string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.behavior = behavior
}
func (t *Target) Snapshot() ([]Request, []Effect) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]Request(nil), t.requests...), append([]Effect(nil), t.effects...)
}
func (t *Target) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, e := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if e != nil {
		http.Error(w, "body unavailable", 400)
		return
	}
	sum := sha256.Sum256(body)
	digest := hex.EncodeToString(sum[:])
	key := r.Header.Get("Idempotency-Key")
	t.mu.Lock()
	defer t.mu.Unlock()
	t.requests = append(t.requests, Request{Method: r.Method, ExternalKey: key, Attempt: r.Header.Get("Lerna-Attempt"), Send: r.Header.Get("Lerna-Send"), User: r.Header.Get("Lerna-User"), Operation: r.Header.Get("Lerna-Operation"), BodyDigest: digest, ReceivedAtUnixNano: time.Now().UnixNano()})
	if r.Method == "GET" && t.mode == "queryable" {
		for _, effect := range t.effects {
			if effect.ExternalKey == key {
				t.respond(w, key, effect.AppliedAtUnixNano)
				return
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"protocol": "lerna-simulator-v1", "external_key": key, "attempt_id": key, "applied": false, "terminal": false})
		return
	}
	if r.Method != "POST" {
		http.Error(w, "unsupported", http.StatusMethodNotAllowed)
		return
	}
	if t.behavior == "reject" {
		_ = json.NewEncoder(w).Encode(map[string]any{"protocol": "lerna-simulator-v1", "external_key": key, "attempt_id": key, "applied": false, "terminal": true})
		return
	}
	for _, effect := range t.effects {
		if t.mode == "idempotent" && effect.ExternalKey == key {
			if effect.BodyDigest != digest {
				http.Error(w, "key payload mismatch", http.StatusConflict)
				return
			}
			t.respond(w, key, effect.AppliedAtUnixNano)
			return
		}
	}
	effect := Effect{ExternalKey: key, BodyDigest: digest, AppliedAtUnixNano: time.Now().UnixNano()}
	t.effects = append(t.effects, effect)
	t.respond(w, key, effect.AppliedAtUnixNano)
}
func (t *Target) respond(w http.ResponseWriter, key string, at int64) {
	switch t.behavior {
	case "drop-after-apply":
		if hijack, ok := w.(http.Hijacker); ok {
			conn, _, e := hijack.Hijack()
			if e == nil {
				_ = conn.Close()
			}
		}
		return
	case "empty-success":
		w.WriteHeader(200)
		return
	case "contradictory":
		_, _ = fmt.Fprintf(w, `{"protocol":"lerna-simulator-v1","external_key":%q,"attempt_id":%q,"applied":true,"applied":false,"terminal":true}`, key, key)
		return
	case "malformed":
		_, _ = w.Write([]byte("{broken"))
		return
	case "wrong-key":
		key = "not-the-original-key"
	case "redirect":
		w.Header().Set("Location", "/redirected")
		w.WriteHeader(307)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"protocol": "lerna-simulator-v1", "external_key": key, "attempt_id": key, "applied": true, "terminal": t.behavior != "applied-not-terminal", "applied_at_unix_nano": at})
}
