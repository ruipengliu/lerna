// Package simulator 提供独立目标的请求与效果记录；被测进程重启不重置这些记录。
package simulator

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
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
	mu              sync.Mutex
	mode            string
	behavior        string
	requests        []Request
	effects         []Effect
	pending         []Effect
	queryRetryAfter int64
	queryBehavior   string
	rejected        map[string]bool
	writeEntered    chan<- struct{}
	writeRelease    <-chan struct{}
	queryEntered    chan<- struct{}
	queryRelease    <-chan struct{}
}

func New(mode string) *Target { return &Target{mode: mode, rejected: map[string]bool{}} }
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
func (t *Target) SetQueryBehavior(b string) { t.mu.Lock(); defer t.mu.Unlock(); t.queryBehavior = b }
func (t *Target) SetQueryRetryAfter(ms int64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.queryRetryAfter = ms
}
func (t *Target) ReleasePending() {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, e := range t.pending {
		e.AppliedAtUnixNano = time.Now().UnixNano()
		t.effects = append(t.effects, e)
	}
	t.pending = nil
}
func (t *Target) SetWriteResponseGate(entered chan<- struct{}, release <-chan struct{}) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.writeEntered = entered
	t.writeRelease = release
}
func (t *Target) SetQueryGate(entered chan<- struct{}, release <-chan struct{}) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.queryEntered = entered
	t.queryRelease = release
}
func (t *Target) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		t.mu.Lock()
		entered, release := t.queryEntered, t.queryRelease
		t.mu.Unlock()
		if entered != nil {
			entered <- struct{}{}
		}
		if release != nil {
			<-release
		}
	}
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
	if r.Method == "GET" && (t.mode == "queryable" || t.mode == "idempotent-queryable") {
		if t.queryBehavior == "drop" {
			if h, ok := w.(http.Hijacker); ok {
				conn, _, e := h.Hijack()
				if e == nil {
					_ = conn.Close()
				}
			}
			return
		}
		if t.queryBehavior == "malformed" {
			_, _ = w.Write([]byte("{broken"))
			return
		}
		subject := r.Header.Get("Lerna-Query-Key")
		applied := false
		for _, effect := range t.effects {
			if effect.ExternalKey == subject {
				applied = true
			}
		}
		terminal := applied
		negative := false
		switch t.queryBehavior {
		case "weak":
			applied = false
			terminal = false
		case "absent-terminal-weak":
			applied = false
			terminal = true
		case "strong-negative":
			if !applied {
				t.rejected[subject] = true
				remaining := t.pending[:0]
				for _, pending := range t.pending {
					if pending.ExternalKey != subject {
						remaining = append(remaining, pending)
					}
				}
				t.pending = remaining
				terminal = true
				negative = true
			}
		}
		queryStatus := "AVAILABLE"
		if t.queryBehavior == "retention-expired" {
			queryStatus = "RETENTION_EXPIRED"
			applied = false
			terminal = false
		}
		if t.queryBehavior == "temporarily-unavailable" {
			queryStatus = "TEMPORARILY_UNAVAILABLE"
			applied = false
			terminal = false
		}
		readTerminal := t.queryBehavior != "no-read-terminal"
		if t.queryBehavior == "wrong-subject" {
			subject = "foreign-subject"
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"query_status": queryStatus, "protocol": "lerna-simulator-query-v1", "query_external_key": key, "query_attempt_id": r.Header.Get("Lerna-Attempt"), "query_operation_id": r.Header.Get("Lerna-Operation"), "read_terminal": readTerminal, "subject_external_key": subject, "subject_attempt_id": r.Header.Get("Lerna-Query-Attempt"), "subject_operation_id": r.Header.Get("Lerna-Query-Operation"), "subject_scope": r.Header.Get("Lerna-Query-Scope"), "applied": applied, "terminal": terminal, "negative_proof": negative, "retry_after_ms": t.queryRetryAfter})
		return
	}
	if r.Method != "POST" {
		http.Error(w, "unsupported", http.StatusMethodNotAllowed)
		return
	}
	if t.mode == "idempotent-expiring" {
		expiry, err := strconv.ParseInt(r.Header.Get("Lerna-Key-Valid-Until"), 10, 64)
		if err != nil || expiry <= 0 {
			http.Error(w, "missing immutable key expiry", 400)
			return
		}
		if time.Now().UnixMilli() >= expiry {
			http.Error(w, "key expired", http.StatusGone)
			return
		}
	}
	if t.behavior == "reject" || t.rejected[key] {
		_ = json.NewEncoder(w).Encode(map[string]any{"protocol": "lerna-simulator-v1", "external_key": key, "attempt_id": key, "applied": false, "terminal": true})
		return
	}
	for _, effect := range t.effects {
		if (t.mode == "idempotent" || t.mode == "idempotent-expiring" || t.mode == "idempotent-queryable") && effect.ExternalKey == key {
			if effect.BodyDigest != digest {
				http.Error(w, "key payload mismatch", http.StatusConflict)
				return
			}
			t.respond(w, key, effect.AppliedAtUnixNano)
			return
		}
	}
	if t.mode == "idempotent" || t.mode == "idempotent-expiring" || t.mode == "idempotent-queryable" {
		for _, pending := range t.pending {
			if pending.ExternalKey != key {
				continue
			}
			if pending.BodyDigest != digest {
				http.Error(w, "key payload mismatch", http.StatusConflict)
				return
			}
			// 已接受的原键仍在执行；重复请求不能排队第二个效果。
			_ = json.NewEncoder(w).Encode(map[string]any{"protocol": "lerna-simulator-v1", "external_key": key, "attempt_id": key, "applied": false, "terminal": false})
			return
		}
	}
	if t.behavior == "accept-and-delay" {
		t.pending = append(t.pending, Effect{ExternalKey: key, BodyDigest: digest})
		if hijack, ok := w.(http.Hijacker); ok {
			conn, _, e := hijack.Hijack()
			if e == nil {
				_ = conn.Close()
			}
		}
		return
	}
	effect := Effect{ExternalKey: key, BodyDigest: digest, AppliedAtUnixNano: time.Now().UnixNano()}
	t.effects = append(t.effects, effect)
	t.respond(w, key, effect.AppliedAtUnixNano)
}
func (t *Target) respond(w http.ResponseWriter, key string, at int64) {
	if t.writeEntered != nil || t.writeRelease != nil {
		entered, release := t.writeEntered, t.writeRelease
		t.mu.Unlock()
		if entered != nil {
			entered <- struct{}{}
		}
		if release != nil {
			<-release
		}
		t.mu.Lock()
	}
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
