//go:build darwin && cgo

package admission_test

import (
	"bytes"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/cmd/assembly"
	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"github.com/ruipengliu/lerna/infra/egressio"
	"github.com/ruipengliu/lerna/infra/keys"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

func withAPIKeychain(t *testing.T, f *fixture, d *v1.ApiDescriptor, secret []byte) *keys.SyntheticKeychain {
	return withAPIKeychainOptions(t, f, d, secret, assembly.Options{})
}
func withAPIKeychainOptions(t *testing.T, f *fixture, d *v1.ApiDescriptor, secret []byte, options assembly.Options) *keys.SyntheticKeychain {
	t.Helper()
	keychain, e := keys.NewSyntheticKeychain()
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		if e := keychain.Close(); e != nil {
			t.Error(e)
		}
	})
	if e = keychain.Put(d.Binding, secret); e != nil {
		t.Fatal(e)
	}
	if e = f.h.Close(); e != nil {
		t.Fatal(e)
	}
	options.APIKeychainPath = keychain.Path()
	f.h, e = assembly.OpenWithOptions(f.path, "u", "d", options)
	if e != nil {
		t.Fatal(e)
	}
	return keychain
}

// 规则：G3、G4、G5、G7、G8、开始-4、开始-5
func TestAPITrustedNativeInjectionAndCanonicalSend(t *testing.T) {
	secret := []byte("synthetic-api-native-canary-197a2be1")
	type received struct {
		authorization string
		body          []byte
		key           string
	}
	requests := make(chan received, 2)
	target := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, e := io.ReadAll(r.Body)
		if e != nil {
			t.Error(e)
		}
		requests <- received{r.Header.Get("Authorization"), body, r.Header.Get("Idempotency-Key")}
		w.Header().Set("X-Request-ID", "synthetic-response")
		_, _ = w.Write([]byte(`{"observed":true}`))
	})
	f := newFixtureWithTarget(t, 100, 80, false, target)
	d := configureAPI(t, f)
	withAPIKeychain(t, f, d, secret)
	a, c := prepareStart(t, f)
	r, e := f.h.Egress.Invoke(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, c)
	accepted(t, r, e)
	got := <-requests
	if got.authorization != "Bearer "+string(secret) || !bytes.Equal(got.body, []byte(`{"quantity":1,"value":"hello"}`)) || got.key != c.CallDescriptor.ExternalKey || f.calls.Load() != 1 {
		t.Fatalf("target received invalid request")
	}
	x, e := f.h.Ledger.QueryExecution(f.ctx, f.caller, a.OperationId)
	if e != nil {
		t.Fatal(e)
	}
	raw, e := f.h.Ledger.QueryObservation(f.ctx, f.caller, x.Send.ObservationRef)
	if e != nil || raw.StatusCode != 200 || raw.TransportError != "" {
		t.Fatalf("observation %v %v", raw, e)
	}
	if bytes.Contains([]byte(c.String()), secret) || bytes.Contains([]byte(raw.String()), secret) {
		t.Fatal("secret escaped trusted injection")
	}
	r, e = f.h.Egress.Invoke(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, c)
	accepted(t, r, e)
	if f.calls.Load() != 1 {
		t.Fatal("replay sent twice")
	}
}

// 规则：G1、G5、G7、G8、G12
func TestAPICredentialEchoCannotBecomeContentTraceOrEvidence(t *testing.T) {
	for _, where := range []string{"body", "base64", "hex", "request-id", "rate-category", "retry-after", "escaped-json", "partial-body", "oversized-body"} {
		t.Run(where, func(t *testing.T) {
			secret := []byte("synthetic-native-echo-canary-a6aebd19")
			target := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch where {
				case "body":
					_, _ = w.Write(secret)
				case "base64":
					_, _ = w.Write([]byte(base64.StdEncoding.EncodeToString(secret)))
				case "hex":
					_, _ = w.Write([]byte(hex.EncodeToString(secret)))
				case "oversized-body":
					_, _ = w.Write(append(append([]byte(nil), secret...), bytes.Repeat([]byte("x"), 1<<20)...))
				case "request-id":
					w.Header().Set("X-Request-ID", string(secret))
					_, _ = w.Write([]byte(`{"safe":true}`))
				case "rate-category":
					w.Header().Set("Lerna-Rate-Category", string(secret))
					w.WriteHeader(429)
				case "retry-after":
					w.Header().Set("Retry-After", string(secret))
					w.WriteHeader(429)
				case "escaped-json":
					_, _ = fmt.Fprintf(w, `{"echo":"\u0073%s"}`, secret[1:])
				case "partial-body":
					w.Header().Set("Content-Length", "1000")
					_, _ = w.Write(secret)
				}
			})
			f := newFixtureWithTarget(t, 100, 80, false, target)
			d := configureAPI(t, f)
			withAPIKeychain(t, f, d, secret)
			a, c := prepareStart(t, f)
			r, e := f.h.Egress.Invoke(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, c)
			accepted(t, r, e)
			op, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
			if e != nil {
				t.Fatal(e)
			}
			raw, e := f.h.Ledger.QueryObservation(f.ctx, f.caller, op.Execution.Send.ObservationRef)
			if e != nil {
				t.Fatal(e)
			}
			body, e := f.h.Content.Read(f.ctx, f.caller, raw.BodyRef)
			if e != nil {
				t.Fatal(e)
			}
			reports, e := f.h.Ledger.QueryReports(f.ctx, f.caller, raw.Ref)
			if e != nil {
				t.Fatal(e)
			}
			if !raw.Redacted || raw.TransportError != "CREDENTIAL_ECHO_REDACTED" || len(body.RawBody) != 0 || body.Text != "" || op.Effect.Outcome != "UNKNOWN" || bytes.Contains([]byte(raw.String()+body.String()+reports.String()+op.String()), secret) || f.calls.Load() != 1 {
				t.Fatalf("unsafe echo escaped %s", where)
			}
			for _, path := range []string{f.path, f.path + "-wal", f.path + "-journal"} {
				data, e := os.ReadFile(path)
				if e == nil && bytes.Contains(data, secret) {
					t.Fatal("credential was persisted in state artifact")
				}
			}
			if e = f.h.Trace.Recover(f.ctx, f.caller); e != nil {
				t.Fatal(e)
			}
			assertAPITraceExcludes(t, f, string(secret), base64.StdEncoding.EncodeToString(secret), hex.EncodeToString(secret), d.Binding.Origin)
		})
	}
}

// 规则：G1、G3、G4、G5、G7、G8、开始-4
func TestAPIUnavailableNativeCredentialRefusesBeforeP5(t *testing.T) {
	for _, kind := range []string{"locked", "missing-item", "deleted-store", "no-store"} {
		t.Run(kind, func(t *testing.T) {
			f := newFixture(t, 100, 80, false)
			d := configureAPI(t, f)
			var keychain *keys.SyntheticKeychain
			if kind != "no-store" {
				keychain = withAPIKeychain(t, f, d, []byte("synthetic-unavailable-canary-c7d8ef6e"))
			}
			a, c := prepareStart(t, f)
			switch kind {
			case "locked":
				if e := keychain.Lock(); e != nil {
					t.Fatal(e)
				}
			case "missing-item":
				if e := keychain.Remove(d.Binding); e != nil {
					t.Fatal(e)
				}
			case "deleted-store":
				if e := keychain.Close(); e != nil {
					t.Fatal(e)
				}
			}
			_, e := f.h.Egress.Invoke(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, c)
			var failure *command.Failure
			if !errors.As(e, &failure) || failure.Detail.Code != "CREDENTIAL_UNAVAILABLE" || f.calls.Load() != 0 {
				t.Fatalf("credential %s: %v target=%d", kind, e, f.calls.Load())
			}
			x, e := f.h.Ledger.QueryExecution(f.ctx, f.caller, a.OperationId)
			if e != nil || x.Send.Phase != "REGISTERED" || x.Send.ObservationRef != nil {
				t.Fatalf("P5 opened: %v %v", x, e)
			}
		})
	}
}

// 规则：G1、G2、G3、G7、G11
func TestAPIOnlyFixedBoundTerminalEvidenceSettlesOperation(t *testing.T) {
	for _, kind := range []string{"applied", "not-applied", "pending", "202", "500", "401", "malformed", "wrong-account", "wrong-origin", "wrong-key", "tool-error", "duplicate"} {
		t.Run(kind, func(t *testing.T) {
			target := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if kind == "malformed" {
					_, _ = w.Write([]byte("{broken"))
					return
				}
				status := 200
				switch kind {
				case "202":
					status = 202
				case "500":
					status = 500
				case "401":
					status = 401
				}
				w.WriteHeader(status)
				account, origin, key := r.Header.Get("Lerna-Account"), "http://"+r.Host, r.Header.Get("Idempotency-Key")
				switch kind {
				case "wrong-account":
					account = "other"
				case "wrong-origin":
					origin = "https://other.invalid"
				case "wrong-key":
					key = "other"
				}
				if kind == "duplicate" {
					_, _ = fmt.Fprintf(w, `{"protocol":"lerna-reference-api-v1","external_key":%q,"attempt_id":%q,"account":%q,"origin":%q,"applied":true,"applied":false,"terminal":true}`, key, r.Header.Get("Lerna-Attempt"), account, origin)
					return
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"protocol": "lerna-reference-api-v1", "external_key": key, "attempt_id": r.Header.Get("Lerna-Attempt"), "account": account, "origin": origin, "applied": kind != "not-applied" && kind != "pending", "terminal": kind != "pending", "tool_error": kind == "tool-error"})
			})
			f := newFixtureWithTarget(t, 100, 80, false, target)
			d := configureAPI(t, f)
			withAPIKeychain(t, f, d, []byte("synthetic-terminal-canary-6ecbb89b"))
			a, c := prepareStart(t, f)
			r, e := f.h.Egress.Invoke(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, c)
			accepted(t, r, e)
			op, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
			if e != nil {
				t.Fatal(e)
			}
			outcome, late, lifecycle := "UNKNOWN", "MAY_OCCUR", "ACTIVE"
			switch kind {
			case "applied":
				outcome, late, lifecycle = "APPLIED", "RULED_OUT", "SETTLED"
			case "not-applied":
				outcome, late, lifecycle = "NOT_APPLIED", "RULED_OUT", "SETTLED"
			}
			if op.Effect.Outcome != outcome || op.Effect.LateEffect != late || op.Lifecycle != lifecycle || f.calls.Load() != 1 {
				t.Fatalf("%s: effect %v lifecycle %s sends%d", kind, op.Effect, op.Lifecycle, f.calls.Load())
			}
		})
	}
}

// 规则：G1、G3、G4、G5、G7、开始-4、开始-5
func TestAPIHTTPSRequiresTrustedCertificateAndHostname(t *testing.T) {
	for _, kind := range []string{"verified", "unknown-root", "wrong-hostname"} {
		t.Run(kind, func(t *testing.T) {
			var calls atomic.Int64
			target := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				_ = json.NewEncoder(w).Encode(map[string]any{"protocol": "lerna-reference-api-v1", "external_key": r.Header.Get("Idempotency-Key"), "attempt_id": r.Header.Get("Lerna-Attempt"), "account": r.Header.Get("Lerna-Account"), "origin": "https://" + r.Host, "applied": true, "terminal": true})
			}))
			t.Cleanup(target.Close)
			target.Config.ErrorLog = log.New(io.Discard, "", 0)
			endpoint := target.URL
			if kind == "wrong-hostname" {
				endpoint = strings.Replace(endpoint, "127.0.0.1", "localhost", 1)
			}
			f := newFixture(t, 100, 80, false)
			d := configureAPIAt(t, f, endpoint)
			options := assembly.Options{}
			if kind != "unknown-root" {
				roots := x509.NewCertPool()
				roots.AddCert(target.Certificate())
				options.APIRoots = roots
			}
			withAPIKeychainOptions(t, f, d, []byte("synthetic-https-native-canary-dc01b6ab"), options)
			a, c := prepareStart(t, f)
			r, e := f.h.Egress.Invoke(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, c)
			accepted(t, r, e)
			op, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
			if e != nil {
				t.Fatal(e)
			}
			raw, e := f.h.Ledger.QueryObservation(f.ctx, f.caller, op.Execution.Send.ObservationRef)
			if e != nil {
				t.Fatal(e)
			}
			if kind == "verified" {
				if calls.Load() != 1 || op.Effect.Outcome != "APPLIED" || op.Effect.LateEffect != "RULED_OUT" || raw.TransportError != "" {
					t.Fatalf("verified TLS: %v %v calls%d", op.Effect, raw, calls.Load())
				}
			} else if calls.Load() != 0 || op.Effect.Outcome != "UNKNOWN" || raw.TransportError != "TLS_VERIFICATION_FAILED" {
				t.Fatalf("invalid TLS: %v %v calls%d", op.Effect, raw, calls.Load())
			}
		})
	}
}

// 规则：G1、G5、G11、开始-5
func TestAPIRedirectAndAuthenticationFailureNeverCreateHiddenRequests(t *testing.T) {
	for _, status := range []int{307, 308, 401, 429, 500, 202} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			var redirected atomic.Int64
			destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { redirected.Add(1) }))
			t.Cleanup(destination.Close)
			f := newFixtureWithTarget(t, 100, 80, false, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Location", destination.URL)
				w.Header().Set("WWW-Authenticate", `Bearer realm="synthetic"`)
				w.WriteHeader(status)
			}))
			d := configureAPI(t, f)
			withAPIKeychain(t, f, d, []byte("synthetic-single-send-native-canary-d7d7f984"))
			a, c := prepareStart(t, f)
			r, e := f.h.Egress.Invoke(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, c)
			accepted(t, r, e)
			op, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
			if e != nil {
				t.Fatal(e)
			}
			if f.calls.Load() != 1 || redirected.Load() != 0 || op.Effect.Outcome != "UNKNOWN" || op.Effect.LateEffect != "MAY_OCCUR" {
				t.Fatalf("status%d: calls%d redirect%d effect%v", status, f.calls.Load(), redirected.Load(), op.Effect)
			}
		})
	}
}

// 规则：G1、G3、G4、G5、G11、开始-5
func TestAPI429PersistsClassifiedWaitAcrossRestartAndReplay(t *testing.T) {
	for _, category := range []string{"RATE", "CONCURRENCY", "RESOURCE_CONFLICT", "unknown-provider-category"} {
		t.Run(category, func(t *testing.T) {
			f := newFixtureWithTarget(t, 100, 80, false, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Lerna-Rate-Category", category)
				w.Header().Set("Retry-After", "3600")
				w.WriteHeader(429)
			}))
			d := configureAPI(t, f)
			configureResendLimit(t, f, 2)
			keychain := withAPIKeychain(t, f, d, []byte("synthetic-rate-native-canary-3c02cf31"))
			a, c := prepareStart(t, f)
			r, e := f.h.Egress.Invoke(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, c)
			accepted(t, r, e)
			op, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
			if e != nil {
				t.Fatal(e)
			}
			raw, e := f.h.Ledger.QueryObservation(f.ctx, f.caller, op.Execution.Send.ObservationRef)
			if e != nil {
				t.Fatal(e)
			}
			expected := category
			if category == "unknown-provider-category" {
				expected = "UNKNOWN"
			}
			if op.ApiWait == nil || op.ApiWait.Category != expected || op.ApiWait.ReadyAtUnixMs != raw.FinishedAtUnixMs+3600000 || op.ApiWait.ObservedAtUnixMs != raw.FinishedAtUnixMs || !proto.Equal(op.ApiWait.ObservationRef, raw.Ref) || op.Effect.Outcome != "UNKNOWN" || op.Effect.LateEffect != "MAY_OCCUR" || f.calls.Load() != 1 {
				t.Fatalf("wait: %v raw %v effect%v", op.ApiWait, raw, op.Effect)
			}
			saved := proto.Clone(op.ApiWait).(*v1.ApiWait)
			job, e := f.h.LedgerWork.QueryJob(f.ctx, f.caller, c.Claim.Ref.Name)
			if e != nil || job.State != "WAITING" || job.ReadyAtUnixMs != saved.ReadyAtUnixMs || job.WaitingReason != "API_429_"+expected {
				t.Fatalf("durable queue: %v %v", job, e)
			}
			claim, e := f.h.LedgerWork.ExecuteJob(f.ctx, f.caller, &v1.JobCommand{Identity: ledgerHeader("early-api-wake").Identity, ContractVersion: 1, Action: "CLAIM", AllowedTypes: []string{"EXECUTE_OPERATION"}, Limit: 1, LeaseMs: 30000, ProcessInstance: "wake"})
			accepted(t, claim, e)
			if len(claim.Jobs) != 0 {
				t.Fatal("wait was claimable before due")
			}
			if e = f.h.Ledger.ProcessInterpretations(f.ctx, f.caller); e != nil {
				t.Fatal(e)
			}
			if e = f.h.Close(); e != nil {
				t.Fatal(e)
			}
			f.h, e = assembly.OpenWithOptions(f.path, "u", "d", assembly.Options{APIKeychainPath: keychain.Path()})
			if e != nil {
				t.Fatal(e)
			}
			after, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
			if e != nil || !proto.Equal(after.ApiWait, saved) || f.calls.Load() != 1 {
				t.Fatalf("restart wait: %v %v", after, e)
			}
			assertAPIExecutionTrace(t, f, after, 1)
			assertAPITraceExcludes(t, f, "synthetic-rate-native-canary-3c02cf31", keychain.Path(), d.Binding.Origin, "unknown-provider-category")
		})
	}
}

// 规则：G1、G3、G4、G5、G11、开始-5
func TestAPI429QueueControlCannotBypassDurableWait(t *testing.T) {
	f := newFixtureWithTarget(t, 100, 80, false, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Lerna-Rate-Category", "RATE")
		w.Header().Set("Retry-After", "3600")
		w.WriteHeader(429)
	}))
	d := configureAPI(t, f)
	configureResendLimit(t, f, 2)
	withAPIKeychain(t, f, d, []byte("synthetic-early-wake-canary-6dbf1743"))
	a, c := prepareStart(t, f)
	r, e := f.h.Egress.Invoke(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, c)
	accepted(t, r, e)
	job, e := f.h.LedgerWork.QueryJob(f.ctx, f.caller, c.Claim.Ref.Name)
	if e != nil {
		t.Fatal(e)
	}
	r, e = f.h.LedgerWork.ExecuteJob(f.ctx, f.caller, &v1.JobCommand{Identity: ledgerHeader("wake-queue-control").Identity, ContractVersion: 1, Action: "CONTROL", Module: "ledger", JobRef: job.Ref, NextState: "READY", ReadyAtUnixMs: 1})
	accepted(t, r, e)
	claim, e := f.h.LedgerWork.ExecuteJob(f.ctx, f.caller, &v1.JobCommand{Identity: ledgerHeader("claim-controlled-wake").Identity, ContractVersion: 1, Action: "CLAIM", AllowedTypes: []string{"EXECUTE_OPERATION"}, Limit: 1, LeaseMs: 30000, ProcessInstance: "wake"})
	accepted(t, claim, e)
	if len(claim.Jobs) != 1 {
		t.Fatal("test did not obtain early queue claim")
	}
	x, e := f.h.Ledger.QueryExecution(f.ctx, f.caller, a.OperationId)
	if e != nil {
		t.Fatal(e)
	}
	before := proto.Clone(x).(*v1.Execution)
	r, e = f.h.Ledger.PrepareResend(f.ctx, f.caller, &v1.PrepareResendCommand{Header: ledgerHeader("early-resend"), OperationId: a.OperationId, PreviousSendRef: x.Send.Ref, Claim: claim.Jobs[0]})
	if e != nil || r.GetError().GetCode() != "API_WAIT_NOT_DUE" {
		t.Fatalf("early resend %v %v", r, e)
	}
	x, e = f.h.Ledger.QueryExecution(f.ctx, f.caller, a.OperationId)
	if e != nil || !proto.Equal(x, before) || f.calls.Load() != 1 {
		t.Fatalf("early wake changed original execution %v %v", x, e)
	}
}

// 规则：G1、G3、G4、G5、G10、G11、开始-5
func TestAPI429DueWakeUsesOriginalAttemptKeyAndSeparateFeeSource(t *testing.T) {
	for _, category := range []string{"RATE", "CONCURRENCY", "RESOURCE_CONFLICT"} {
		t.Run(category, func(t *testing.T) {
			var calls atomic.Int64
			var keysSeen []string
			var targetMu sync.Mutex
			target := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				number := calls.Add(1)
				targetMu.Lock()
				keysSeen = append(keysSeen, r.Header.Get("Idempotency-Key"))
				targetMu.Unlock()
				if number == 1 {
					w.Header().Set("Lerna-Rate-Category", category)
					w.Header().Set("Retry-After", "0")
					w.WriteHeader(429)
					return
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"protocol": "lerna-reference-api-v1", "external_key": r.Header.Get("Idempotency-Key"), "attempt_id": r.Header.Get("Lerna-Attempt"), "account": r.Header.Get("Lerna-Account"), "origin": "http://" + r.Host, "applied": true, "terminal": true})
			})
			f := newFixtureWithTarget(t, 100, 80, false, target)
			d := configureAPI(t, f)
			configureResendLimit(t, f, 2)
			withAPIKeychain(t, f, d, []byte("synthetic-due-wake-canary-fc4d5194"))
			a, first := prepareStart(t, f)
			actor := &v1.Caller{UserId: "u", IssuerId: "egress"}
			r, e := f.h.Egress.Invoke(f.ctx, actor, first)
			accepted(t, r, e)
			before, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
			if e != nil {
				t.Fatal(e)
			}
			time.Sleep(max(0, time.Until(time.UnixMilli(before.ApiWait.ReadyAtUnixMs))) + 20*time.Millisecond)
			claim, e := f.h.LedgerWork.ExecuteJob(f.ctx, f.caller, &v1.JobCommand{Identity: ledgerHeader("due-wake-claim").Identity, ContractVersion: 1, Action: "CLAIM", AllowedTypes: []string{"EXECUTE_OPERATION"}, Limit: 1, LeaseMs: 30000, ProcessInstance: "wake"})
			accepted(t, claim, e)
			if len(claim.Jobs) != 1 {
				t.Fatal("due wait did not wake")
			}
			r, e = f.h.Ledger.PrepareResend(f.ctx, f.caller, &v1.PrepareResendCommand{Header: ledgerHeader("due-api-resend"), OperationId: a.OperationId, PreviousSendRef: before.Execution.Send.Ref, Claim: claim.Jobs[0]})
			accepted(t, r, e)
			x, e := f.h.Ledger.QueryExecution(f.ctx, f.caller, a.OperationId)
			if e != nil {
				t.Fatal(e)
			}
			second := resendStart(t, f, a, first, x, claim.Jobs[0])
			r, e = f.h.Egress.Invoke(f.ctx, actor, second)
			accepted(t, r, e)
			after, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
			if e != nil {
				t.Fatal(e)
			}
			targetMu.Lock()
			keysSeen = append([]string(nil), keysSeen...)
			targetMu.Unlock()
			if calls.Load() != 2 || len(keysSeen) != 2 || keysSeen[0] != keysSeen[1] || !proto.Equal(after.Execution.Attempt.Ref.Name, before.Execution.Attempt.Ref.Name) || !proto.Equal(after.Execution.CallDescriptor, before.Execution.CallDescriptor) || after.Execution.Send.SendSeq != 2 || len(after.Execution.PreviousSends) != 1 || after.Execution.PreviousSends[0].ApiWait.GetCategory() != category || after.Effect.Outcome != "APPLIED" || after.Effect.LateEffect != "RULED_OUT" {
				t.Fatalf("unsafe wake: before%v after%v", before.Execution, after)
			}
			for _, send := range []*v1.PhysicalSend{after.Execution.Send, after.Execution.PreviousSends[0]} {
				source, e := f.h.Budget.QueryBillingSource(f.ctx, f.caller, send.Ref)
				if e != nil || source == nil || source.Status != "PENDING" || source.Amount != nil {
					t.Fatalf("lost fee source%v %v", source, e)
				}
			}
			budget, e := f.h.Budget.QueryBudget(f.ctx, f.caller, a.TaskId)
			if e != nil || budget.Reserved != 60 {
				t.Fatalf("fees: %v %v", budget, e)
			}
			r, e = f.h.Egress.Invoke(f.ctx, actor, second)
			accepted(t, r, e)
			if calls.Load() != 2 {
				t.Fatal("wake replay sent again")
			}
			assertAPIExecutionTrace(t, f, after, 1)
		})
	}
}

// 规则：G1、G3、G4、G5、G7、G8、G10、G11、开始-2、开始-4、开始-5
func TestAPI429DueWakeRechecksCurrentGates(t *testing.T) {
	for _, kind := range []string{"PAUSE", "CANCEL", "REVOKE", "BUDGET", "LOCKED", "PAYLOAD", "TARGET", "ACCOUNT", "CREDENTIAL_REF", "VERSION", "CAPABILITY_AFTER_P4", "KEY_EXPIRED"} {
		t.Run(kind, func(t *testing.T) {
			f := newFixtureWithTarget(t, 100, 80, false, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Lerna-Rate-Category", "RATE")
				w.Header().Set("Retry-After", "0")
				w.WriteHeader(429)
			}))
			d := configureAPI(t, f)
			configureResendLimit(t, f, 2)
			if kind == "KEY_EXPIRED" {
				cap, e := f.h.Tasks.QueryCapability(f.ctx, f.caller, f.capability)
				if e != nil {
					t.Fatal(e)
				}
				cap.Ref = nil
				cap.ApprovedBy = nil
				cap.IdempotencyRetentionMs = 1000
				r, e := f.h.Tasks.ConfigureCapability(f.ctx, f.caller, &v1.ConfigureCapabilityCommand{Header: header("api-expiring-cap"), Capability: cap})
				accepted(t, r, e)
				f.capability = r.ResultRef
			}
			keychain := withAPIKeychain(t, f, d, []byte("synthetic-current-gate-canary-19dd2ac2"))
			a, first := prepareStart(t, f)
			actor := &v1.Caller{UserId: "u", IssuerId: "egress"}
			r, e := f.h.Egress.Invoke(f.ctx, actor, first)
			accepted(t, r, e)
			before, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
			if e != nil {
				t.Fatal(e)
			}
			time.Sleep(max(0, time.Until(time.UnixMilli(before.ApiWait.ReadyAtUnixMs))) + 20*time.Millisecond)
			if kind == "KEY_EXPIRED" {
				claim, e := f.h.LedgerWork.ExecuteJob(f.ctx, f.caller, &v1.JobCommand{Identity: ledgerHeader("expired-api-wake").Identity, ContractVersion: 1, Action: "CLAIM", AllowedTypes: []string{"EXECUTE_OPERATION"}, Limit: 1, LeaseMs: 30000, ProcessInstance: "wake"})
				accepted(t, claim, e)
				if len(claim.Jobs) != 1 {
					t.Fatal("no expired wake claim")
				}
				r, e = f.h.Ledger.PrepareResend(f.ctx, f.caller, &v1.PrepareResendCommand{Header: ledgerHeader("expired-api-resend"), OperationId: a.OperationId, PreviousSendRef: before.Execution.Send.Ref, Claim: claim.Jobs[0]})
				if e != nil || r.GetError().GetCode() != "RESEND_KEY_EXPIRED" {
					t.Fatalf("expired identity permitted%v %v", r, e)
				}
			} else {
				second := prepareNextResend(t, f, a, first)
				switch kind {
				case "PAUSE", "CANCEL":
					resendTaskControl(t, f, kind, "api-wake-control")
				case "REVOKE":
					r, e = f.h.Grants.Revoke(f.ctx, f.caller, &v1.RevokeGrantCommand{Header: header("api-wake-revoke"), GrantId: f.grant.Name})
					accepted(t, r, e)
				case "BUDGET":
					b, e := f.h.Budget.QueryBudget(f.ctx, f.caller, a.TaskId)
					if e != nil {
						t.Fatal(e)
					}
					r, e = f.h.Budget.AdjustLimit(f.ctx, f.caller, &v1.AdjustBudgetLimitCommand{Header: header("api-wake-budget"), ExpectedRef: b.Ref, Limit: 40, Reason: "controlled test"})
					accepted(t, r, e)
				case "LOCKED":
					if e = keychain.Lock(); e != nil {
						t.Fatal(e)
					}
				case "PAYLOAD":
					second.CallDescriptor = proto.Clone(second.CallDescriptor).(*v1.CallDescriptor)
					second.CallDescriptor.ParametersDigest = "different"
				case "TARGET":
					second.CallDescriptor = proto.Clone(second.CallDescriptor).(*v1.CallDescriptor)
					second.CallDescriptor.Target += "/unreviewed"
				case "ACCOUNT":
					second.CallDescriptor = proto.Clone(second.CallDescriptor).(*v1.CallDescriptor)
					second.CallDescriptor.ApiDescriptor.Binding.Account = "other"
				case "CREDENTIAL_REF":
					second.CallDescriptor = proto.Clone(second.CallDescriptor).(*v1.CallDescriptor)
					second.CallDescriptor.ApiDescriptor.Binding.CredentialRef.Name.LocalId = "other"
				case "VERSION":
					second.CallDescriptor = proto.Clone(second.CallDescriptor).(*v1.CallDescriptor)
					second.CallDescriptor.ApiDescriptor.Version = "2"
				case "CAPABILITY_AFTER_P4":
					r, e = f.h.Tasks.StartExecution(f.ctx, actor, second)
					accepted(t, r, e)
					cap, e := f.h.Tasks.QueryCapability(f.ctx, f.caller, f.capability)
					if e != nil {
						t.Fatal(e)
					}
					original := cap.Ref
					cap.Ref = nil
					cap.ApprovedBy = nil
					cap.ApiDescriptor.Idempotent = false
					cap.ApiDescriptor.Digest = command.APIDescriptorDigest(cap.ApiDescriptor)
					r, e = f.h.Tasks.ConfigureCapability(f.ctx, f.caller, &v1.ConfigureCapabilityCommand{Header: header("weaken-api-after-p4"), Replaces: original, Capability: cap})
					accepted(t, r, e)
				}
				r, e = f.h.Egress.Invoke(f.ctx, actor, second)
				if e == nil && r.GetDecision() == v1.Decision_DECISION_ACCEPTED {
					t.Fatalf("%s reached P5", kind)
				}
			}
			after, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
			if e != nil || after.Effect.Outcome != "UNKNOWN" || after.Effect.LateEffect != "MAY_OCCUR" || !proto.Equal(after.Execution.Attempt.Ref.Name, before.Execution.Attempt.Ref.Name) || f.calls.Load() != 1 {
				t.Fatalf("gate %s lost original responsibility: %v %v count%d", kind, after, e, f.calls.Load())
			}
		})
	}
}

// 规则：G1、G3、G4、G5、G11
func TestAPI429RetryAfterIsBoundedAndAmbiguousClassStaysUnknown(t *testing.T) {
	for _, kind := range []string{"zero", "negative", "invalid", "huge", "past-date", "future-date", "ambiguous-class"} {
		t.Run(kind, func(t *testing.T) {
			retry := "0"
			future := time.Now().Add(time.Hour).UTC().Truncate(time.Second)
			switch kind {
			case "negative":
				retry = "-1"
			case "invalid":
				retry = "later"
			case "huge":
				retry = "999999999999999999999999"
			case "past-date":
				retry = "Mon, 01 Jan 2001 00:00:00 GMT"
			case "future-date":
				retry = future.Format(http.TimeFormat)
			}
			f := newFixtureWithTarget(t, 100, 80, false, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Lerna-Rate-Category", "RATE")
				if kind == "ambiguous-class" {
					w.Header().Add("Lerna-Rate-Category", "CONCURRENCY")
				}
				w.Header().Set("Retry-After", retry)
				w.WriteHeader(429)
			}))
			d := configureAPI(t, f)
			withAPIKeychain(t, f, d, []byte("synthetic-retry-after-canary-12dff373"))
			a, c := prepareStart(t, f)
			r, e := f.h.Egress.Invoke(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, c)
			accepted(t, r, e)
			op, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
			if e != nil {
				t.Fatal(e)
			}
			expected := op.ApiWait.ObservedAtUnixMs + 1000
			if kind == "huge" {
				expected = op.ApiWait.ObservedAtUnixMs + 86400000
			}
			if kind == "future-date" {
				expected = future.UnixMilli()
			}
			category := "RATE"
			if kind == "ambiguous-class" {
				category = "UNKNOWN"
			}
			if op.ApiWait.ReadyAtUnixMs != expected || op.ApiWait.Category != category || f.calls.Load() != 1 {
				t.Fatalf("%s: %v expected due%d category%s", kind, op.ApiWait, expected, category)
			}
		})
	}
}

// 规则：G1、G2、G3、G4、G5、G10、G11、R7、开始-4、开始-5
func TestAPIOriginalAttemptQueryPreservesWeakEvidenceAndRestoresLateEffect(t *testing.T) {
	var visible atomic.Bool
	var writes, queries, effects atomic.Int64
	var queryIdentityMu sync.Mutex
	var subjects []string
	target := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			writes.Add(1)
			effects.Add(1)
			conn, _, e := w.(http.Hijacker).Hijack()
			if e != nil {
				t.Error(e)
				return
			}
			_ = conn.Close()
			return
		}
		if r.Method != "GET" {
			t.Error("unexpected request method")
			w.WriteHeader(405)
			return
		}
		queries.Add(1)
		body, e := io.ReadAll(r.Body)
		if e != nil || len(body) != 0 {
			t.Error("query sent a business payload")
		}
		queryIdentityMu.Lock()
		subjects = append(subjects, r.Header.Get("Lerna-Query-Operation")+"/"+r.Header.Get("Lerna-Query-Attempt")+"/"+r.Header.Get("Lerna-Query-Key")+"/"+r.Header.Get("Lerna-Query-Scope"))
		queryIdentityMu.Unlock()
		applied := visible.Load()
		_ = json.NewEncoder(w).Encode(map[string]any{"protocol": "lerna-reference-api-query-v1", "account": r.Header.Get("Lerna-Account"), "origin": "http://" + r.Host, "query_status": "AVAILABLE", "query_external_key": r.Header.Get("Idempotency-Key"), "query_attempt_id": r.Header.Get("Lerna-Attempt"), "query_operation_id": r.Header.Get("Lerna-Operation"), "read_terminal": true, "subject_external_key": r.Header.Get("Lerna-Query-Key"), "subject_attempt_id": r.Header.Get("Lerna-Query-Attempt"), "subject_operation_id": r.Header.Get("Lerna-Query-Operation"), "subject_scope": r.Header.Get("Lerna-Query-Scope"), "applied": applied, "terminal": applied, "negative_proof": false, "retry_after_ms": 1000})
	})
	f := newFixtureWithTarget(t, 200, 200, false, target)
	d, queryCap, queryGrant := configureAPIQueryable(t, f, false)
	keychain := withAPIKeychain(t, f, d, []byte("synthetic-query-native-canary-03f8e024"))
	scopeRequirement(t, f)
	a, start := prepareStart(t, f)
	r, e := f.h.Egress.Invoke(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, start)
	accepted(t, r, e)
	before, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
	if e != nil || before.Effect.Outcome != "UNKNOWN" {
		t.Fatalf("lost receipt %v %v", before, e)
	}
	r, e = f.h.Ledger.RequestReconciliation(f.ctx, f.caller, reconciliationCommand(f, a, queryCap, queryGrant))
	accepted(t, r, e)
	if e = f.h.Ledger.ProcessReconciliations(f.ctx, f.caller); e != nil {
		t.Fatal(e)
	}
	plan, e := f.h.Ledger.QueryReconciliation(f.ctx, f.caller, a.OperationId)
	if e != nil || plan.State != "WAITING" || plan.CheckCount != 1 {
		t.Fatalf("weak query schedule %v %v", plan, e)
	}
	original, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
	if e != nil || original.Effect.Outcome != "UNKNOWN" || original.Effect.LateEffect != "MAY_OCCUR" || writes.Load() != 1 || queries.Load() != 1 || effects.Load() != 1 {
		t.Fatalf("weak absence changed original effect%v %v", original, e)
	}
	if e = f.h.Close(); e != nil {
		t.Fatal(e)
	}
	f.h, e = assembly.OpenWithOptions(f.path, "u", "d", assembly.Options{APIKeychainPath: keychain.Path()})
	if e != nil {
		t.Fatal(e)
	}
	restored, e := f.h.Ledger.QueryReconciliation(f.ctx, f.caller, a.OperationId)
	if e != nil || restored.NextReconcileAtUnixMs != plan.NextReconcileAtUnixMs || queries.Load() != 1 {
		t.Fatalf("query restoration %v %v", restored, e)
	}
	visible.Store(true)
	time.Sleep(max(0, time.Until(time.UnixMilli(plan.NextReconcileAtUnixMs))) + 20*time.Millisecond)
	if e = f.h.Ledger.ProcessReconciliations(f.ctx, f.caller); e != nil {
		t.Fatal(e)
	}
	plan, e = f.h.Ledger.QueryReconciliation(f.ctx, f.caller, a.OperationId)
	if e != nil || plan.State != "COMPLETED" || plan.CheckCount != 2 {
		t.Fatalf("late query proof %v %v", plan, e)
	}
	after, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
	if e != nil || after.Effect.Outcome != "APPLIED" || after.Effect.LateEffect != "RULED_OUT" || !proto.Equal(after.Execution, before.Execution) || writes.Load() != 1 || queries.Load() != 2 || effects.Load() != 1 {
		t.Fatalf("late original effect %v %v", after, e)
	}
	queryIdentityMu.Lock()
	defer queryIdentityMu.Unlock()
	expected := a.OperationId.LocalId + "/" + before.Execution.Attempt.Ref.Name.LocalId + "/" + before.Execution.Attempt.ExternalKey + "/" + d.Binding.Resource
	if len(subjects) != 2 || subjects[0] != expected || subjects[1] != expected {
		t.Fatal("query identity drifted")
	}
	budget, e := f.h.Budget.QueryBudget(f.ctx, f.caller, a.TaskId)
	if e != nil || budget.Reserved != 40 {
		t.Fatalf("query fees lost: %v %v", budget, e)
	}
	p := completeProposal(t, f, []*v1.CompletionEvidence{{ConditionId: "created", OperationId: a.OperationId}})
	r, e = f.h.Tasks.BeginCompletion(f.ctx, f.caller, &v1.BeginCompletionCommand{Header: header("api-query-completion"), TaskId: f.task.Name, ProposalRef: p})
	accepted(t, r, e)
	if e = f.h.Tasks.ProcessCompletions(f.ctx, f.caller); e != nil {
		t.Fatal(e)
	}
	result, e := f.h.Tasks.QueryResult(f.ctx, f.caller, f.task.Name)
	if e != nil || result == nil || result.Outcome != "SUCCEEDED" || writes.Load() != 1 || queries.Load() != 2 {
		t.Fatalf("query completion evidence%v %v", result, e)
	}
	assertAPIQueryTrace(t, f, plan)
	assertAPITraceExcludes(t, f, "synthetic-query-native-canary", keychain.Path(), d.Binding.Origin)
}

// 规则：G1、G3、G5、G10、G11
func TestAPIBillingBindsReviewedAccountAndActualSend(t *testing.T) {
	var calls atomic.Int64
	target := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		amount := int64(5)
		if n > 1 {
			amount = 7
		}
		bill := map[string]any{"rule": "reference-billing-v1", "namespace": "lerna-reference", "account": r.Header.Get("Lerna-Account"), "native_instance": r.Header.Get("Lerna-Send-Id"), "component": "call", "send_id": r.Header.Get("Lerna-Send-Id"), "external_key": r.Header.Get("Idempotency-Key"), "source_version": 1, "unit": "USD_MICRO", "amount": amount, "final": true, "price_version": "reference-price-v1"}
		if n == 1 {
			w.Header().Set("Lerna-Rate-Category", "RATE")
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(429)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"protocol": "lerna-reference-api-v1", "external_key": r.Header.Get("Idempotency-Key"), "attempt_id": r.Header.Get("Lerna-Attempt"), "account": r.Header.Get("Lerna-Account"), "origin": "http://" + r.Host, "applied": n > 1, "terminal": n > 1, "billing": bill})
	})
	f := newFixtureWithTarget(t, 100, 80, false, target)
	d := configureAPI(t, f)
	configureResendLimit(t, f, 2)
	withAPIKeychain(t, f, d, []byte("synthetic-billing-canary-87a60fef"))
	a, first := prepareStart(t, f)
	actor := &v1.Caller{UserId: "u", IssuerId: "egress"}
	r, e := f.h.Egress.Invoke(f.ctx, actor, first)
	accepted(t, r, e)
	op, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
	if e != nil {
		t.Fatal(e)
	}
	b, e := f.h.Budget.QueryBudget(f.ctx, f.caller, a.TaskId)
	if e != nil || b.Settled != 5 || b.Reserved != 0 || op.Effect.Outcome != "UNKNOWN" {
		t.Fatalf("first source%v %v effect%v", b, e, op.Effect)
	}
	time.Sleep(max(0, time.Until(time.UnixMilli(op.ApiWait.ReadyAtUnixMs))) + 20*time.Millisecond)
	second := prepareNextResend(t, f, a, first)
	r, e = f.h.Egress.Invoke(f.ctx, actor, second)
	accepted(t, r, e)
	after, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
	if e != nil {
		t.Fatal(e)
	}
	for index, send := range []*v1.PhysicalSend{after.Execution.PreviousSends[0], after.Execution.Send} {
		source, e := f.h.Budget.QueryBillingSource(f.ctx, f.caller, send.Ref)
		expected := int64(5)
		if index == 1 {
			expected = 7
		}
		if e != nil || source.Amount == nil || *source.Amount != expected || source.Identity.Account != d.Binding.Account || source.Identity.NativeInstance != send.Ref.Name.LocalId {
			t.Fatalf("source%v %v", source, e)
		}
	}
	if e = f.h.Ledger.ProcessReports(f.ctx, f.caller); e != nil {
		t.Fatal(e)
	}
	r, e = f.h.Egress.Invoke(f.ctx, actor, second)
	accepted(t, r, e)
	b, e = f.h.Budget.QueryBudget(f.ctx, f.caller, a.TaskId)
	if e != nil || b.Settled != 12 || b.Reserved != 0 || calls.Load() != 2 {
		t.Fatalf("duplicate/missing fee%v %v calls%d", b, e, calls.Load())
	}
}

// 规则：G2、G3、G4、G5、完成-2、完成-4、完成-6、完成-7
func TestAPITrustedTerminalEvidenceCanSatisfyScopedCompletion(t *testing.T) {
	var effects atomic.Int64
	target := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		effects.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{"protocol": "lerna-reference-api-v1", "external_key": r.Header.Get("Idempotency-Key"), "attempt_id": r.Header.Get("Lerna-Attempt"), "account": r.Header.Get("Lerna-Account"), "origin": "http://" + r.Host, "applied": true, "terminal": true})
	})
	f := newFixtureWithTarget(t, 100, 80, false, target)
	d := configureAPI(t, f)
	withAPIKeychain(t, f, d, []byte("synthetic-completion-native-canary-e89f015b"))
	scopeRequirement(t, f)
	a, start := prepareStart(t, f)
	r, e := f.h.Egress.Invoke(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, start)
	accepted(t, r, e)
	p := completeProposal(t, f, []*v1.CompletionEvidence{{ConditionId: "created", OperationId: a.OperationId}})
	r, e = f.h.Tasks.BeginCompletion(f.ctx, f.caller, &v1.BeginCompletionCommand{Header: header("api-completion"), TaskId: f.task.Name, ProposalRef: p})
	accepted(t, r, e)
	if e = f.h.Tasks.ProcessCompletions(f.ctx, f.caller); e != nil {
		t.Fatal(e)
	}
	result, e := f.h.Tasks.QueryResult(f.ctx, f.caller, f.task.Name)
	if e != nil || result == nil || result.Outcome != "SUCCEEDED" || effects.Load() != 1 || f.calls.Load() != 1 {
		t.Fatalf("verified API completion %v %v", result, e)
	}
}

func apiQueryResponse(r *http.Request) map[string]any {
	return map[string]any{"protocol": "lerna-reference-api-query-v1", "account": r.Header.Get("Lerna-Account"), "origin": "http://" + r.Host, "query_status": "AVAILABLE", "query_external_key": r.Header.Get("Idempotency-Key"), "query_attempt_id": r.Header.Get("Lerna-Attempt"), "query_operation_id": r.Header.Get("Lerna-Operation"), "read_terminal": true, "subject_external_key": r.Header.Get("Lerna-Query-Key"), "subject_attempt_id": r.Header.Get("Lerna-Query-Attempt"), "subject_operation_id": r.Header.Get("Lerna-Query-Operation"), "subject_scope": r.Header.Get("Lerna-Query-Scope"), "applied": true, "terminal": true, "negative_proof": false, "retry_after_ms": 1000}
}

func performAPIWithoutObservation(t *testing.T, f *fixture, a *v1.Admission, c *v1.StartExecutionCommand, store *keys.FileBased) *v1.PhysicalIOResult {
	t.Helper()
	actor := &v1.Caller{UserId: "u", IssuerId: "egress"}
	start, e := f.h.Tasks.StartExecution(f.ctx, actor, c)
	accepted(t, start, e)
	x, e := f.h.Ledger.QueryExecution(f.ctx, f.caller, a.OperationId)
	if e != nil {
		t.Fatal(e)
	}
	dh := ledgerHeader("dispatch:" + x.Send.Ref.Name.LocalId)
	dh.Identity.IssuerId = "egress"
	r, fresh, e := f.h.Ledger.RecordDispatch(f.ctx, actor, &v1.DispatchCommand{Header: dh, OperationId: a.OperationId, StartReceipt: start, Claim: c.Claim})
	accepted(t, r, e)
	if !fresh {
		t.Fatal("no physical send right")
	}
	x, e = f.h.Ledger.QueryExecution(f.ctx, f.caller, a.OperationId)
	if e != nil {
		t.Fatal(e)
	}
	content, e := f.h.Content.Read(f.ctx, actor, x.CallDescriptor.ParametersRef)
	if e != nil {
		t.Fatal(e)
	}
	body, e := command.CompileAPIParameters(command.ContentBytes(content))
	if e != nil {
		t.Fatal(e)
	}
	result, e := (egressio.APIHTTP{Credentials: store}).Perform(f.ctx, &v1.PhysicalIORequest{TaskId: a.TaskId, OperationId: a.OperationId, ExecutorEndpointId: a.ExecutorEndpointId, Attempt: x.Attempt, Send: x.Send, CallDescriptor: x.CallDescriptor, Body: body})
	if e != nil {
		t.Fatal(e)
	}
	return result
}

// 规则：G1、G3、G5、G10、G11
func TestAPILateRawEvidenceRetainsEverySendAndCurrentWait(t *testing.T) {
	for _, scenario := range []string{"old-429", "old-applied", "old-unknown-current-negative", "old-unknown-current-applied"} {
		t.Run(scenario, func(t *testing.T) {
			var calls, effects atomic.Int64
			target := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				n := calls.Add(1)
				if scenario == "old-429" || scenario == "old-applied" && n == 2 {
					w.Header().Set("Lerna-Rate-Category", "RESOURCE_CONFLICT")
					w.Header().Set("Retry-After", "1")
					if n == 1 {
						w.Header().Set("Lerna-Rate-Category", "RATE")
						w.Header().Set("Retry-After", "60")
					}
					w.WriteHeader(429)
					return
				}
				if n == 1 && scenario != "old-applied" {
					_, _ = w.Write([]byte(`{"observed":true}`))
					return
				}
				applied := scenario != "old-unknown-current-negative"
				if applied {
					effects.CompareAndSwap(0, 1)
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"protocol": "lerna-reference-api-v1", "external_key": r.Header.Get("Idempotency-Key"), "attempt_id": r.Header.Get("Lerna-Attempt"), "account": r.Header.Get("Lerna-Account"), "origin": "http://" + r.Host, "applied": applied, "terminal": true})
			})
			f := newFixtureWithTarget(t, 100, 80, false, target)
			d := configureAPI(t, f)
			configureResendLimit(t, f, 2)
			keychain := withAPIKeychain(t, f, d, []byte("synthetic-history-native-canary-bc71236a"))
			a, first := prepareStart(t, f)
			old := performAPIWithoutObservation(t, f, a, first, keychain.Store())
			oldState, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
			if e != nil {
				t.Fatal(e)
			}
			prepared, e := f.h.Ledger.PrepareResend(f.ctx, f.caller, &v1.PrepareResendCommand{Header: ledgerHeader("late-api-resend"), OperationId: a.OperationId, PreviousSendRef: oldState.Execution.Send.Ref, Claim: first.Claim})
			accepted(t, prepared, e)
			next, e := f.h.Ledger.QueryExecution(f.ctx, f.caller, a.OperationId)
			if e != nil {
				t.Fatal(e)
			}
			second := resendStart(t, f, a, first, next, first.Claim)
			r, e := f.h.Egress.Invoke(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, second)
			accepted(t, r, e)
			current, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
			if e != nil {
				t.Fatal(e)
			}
			savePhysicalObservation(t, f, old)
			after, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
			if e != nil || len(after.Execution.PreviousSends) != 1 || after.Execution.PreviousSends[0].Phase != "OBSERVED" || len(after.Effect.EvidenceRefs) != 2 || calls.Load() != 2 || !proto.Equal(after.Execution.Send.Ref, current.Execution.Send.Ref) {
				t.Fatalf("late source lost%v %v", after, e)
			}
			switch scenario {
			case "old-429":
				if !proto.Equal(after.ApiWait, current.ApiWait) || after.ApiWait.Category != "RESOURCE_CONFLICT" || after.Execution.PreviousSends[0].ApiWait.GetCategory() != "RATE" || after.Execution.PreviousSends[0].ApiWait.ReadyAtUnixMs <= after.ApiWait.ReadyAtUnixMs || after.Effect.Outcome != "UNKNOWN" || after.Effect.LateEffect != "MAY_OCCUR" || effects.Load() != 0 {
					t.Fatalf("old wait overwrote current%v", after)
				}
			case "old-unknown-current-negative":
				if after.Effect.Outcome != "UNKNOWN" || after.Effect.LateEffect != "MAY_OCCUR" || effects.Load() != 0 {
					t.Fatalf("single negative covered unresolved send%v", after.Effect)
				}
			default:
				if after.Effect.Outcome != "APPLIED" || after.Effect.LateEffect != "RULED_OUT" || effects.Load() != 1 {
					t.Fatalf("strong proof lost%v", after.Effect)
				}
			}
			historical, e := f.h.Ledger.QueryOperationVersion(f.ctx, f.caller, oldState.Ref)
			if e != nil || !proto.Equal(historical, oldState) {
				t.Fatalf("old operation rewritten%v %v", historical, e)
			}
			send, e := f.h.Ledger.QuerySend(f.ctx, f.caller, oldState.Execution.Send.Ref)
			if e != nil || send.Phase != "DISPATCH_POSSIBLE" || send.ApiWait != nil {
				t.Fatalf("old send rewritten%v %v", send, e)
			}
			savePhysicalObservation(t, f, old)
			replay, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
			if e != nil || !proto.Equal(replay, after) || calls.Load() != 2 {
				t.Fatal("late source replay changed records or sent")
			}
			budget, e := f.h.Budget.QueryBudget(f.ctx, f.caller, a.TaskId)
			if e != nil || budget.Reserved != 60 {
				t.Fatalf("history fee lost%v %v", budget, e)
			}
			wantWaits := 0
			switch scenario {
			case "old-429":
				wantWaits = 2
			case "old-applied":
				wantWaits = 1
			}
			assertAPIExecutionTrace(t, f, after, wantWaits)
			assertLateSendTrace(t, f, after, old.Observation.SendRef)
			assertAPITraceExcludes(t, f, "synthetic-history-native-canary-bc71236a", keychain.Path(), d.Binding.Origin)
		})
	}
}

// 规则：G1、G3、G4、G5、G7、G8、开始-4、开始-5
func TestAPICredentialRotationUsesCurrentNativeSecretWithFixedIdentity(t *testing.T) {
	oldSecret, newSecret := []byte("synthetic-rotation-first-canary-c8392140"), []byte("synthetic-rotation-second-canary-a5423eb6")
	var calls atomic.Int64
	var valid atomic.Bool
	target := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		expected := oldSecret
		if n == 2 {
			expected = newSecret
		}
		if r.Header.Get("Authorization") != "Bearer "+string(expected) {
			valid.Store(false)
		}
		if n == 1 {
			w.Header().Set("Retry-After", "0")
			w.Header().Set("Lerna-Rate-Category", "RATE")
			w.WriteHeader(429)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"protocol": "lerna-reference-api-v1", "external_key": r.Header.Get("Idempotency-Key"), "attempt_id": r.Header.Get("Lerna-Attempt"), "account": r.Header.Get("Lerna-Account"), "origin": "http://" + r.Host, "applied": true, "terminal": true})
	})
	valid.Store(true)
	f := newFixtureWithTarget(t, 100, 80, false, target)
	d := configureAPI(t, f)
	configureResendLimit(t, f, 2)
	keychain := withAPIKeychain(t, f, d, oldSecret)
	a, first := prepareStart(t, f)
	r, e := f.h.Egress.Invoke(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, first)
	accepted(t, r, e)
	before, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
	if e != nil {
		t.Fatal(e)
	}
	if e = keychain.Put(d.Binding, newSecret); e != nil {
		t.Fatal(e)
	}
	time.Sleep(max(0, time.Until(time.UnixMilli(before.ApiWait.ReadyAtUnixMs))) + 20*time.Millisecond)
	second := prepareNextResend(t, f, a, first)
	r, e = f.h.Egress.Invoke(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, second)
	accepted(t, r, e)
	after, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
	if e != nil || !valid.Load() || calls.Load() != 2 || !proto.Equal(before.Execution.CallDescriptor, after.Execution.CallDescriptor) || !proto.Equal(before.Execution.Attempt.Ref.Name, after.Execution.Attempt.Ref.Name) || before.Execution.Attempt.ExternalKey != after.Execution.Attempt.ExternalKey || after.Effect.Outcome != "APPLIED" {
		t.Fatal("native rotation changed fixed attempt or used stale secret")
	}
	if bytes.Contains([]byte(after.String()), oldSecret) || bytes.Contains([]byte(after.String()), newSecret) {
		t.Fatal("rotation leaked secret to records")
	}
}

// 规则：G1、G3、G5、G7、G11
func TestAPIResponseMetadataCannotBreakDurableObservation(t *testing.T) {
	for _, scenario := range []string{"invalid-utf8", "oversized-id", "oversized-category", "oversized-retry"} {
		t.Run(scenario, func(t *testing.T) {
			target := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch scenario {
				case "invalid-utf8":
					w.Header().Set("X-Request-ID", string([]byte{0xff}))
				case "oversized-id":
					w.Header().Set("X-Request-ID", strings.Repeat("x", 1024))
				case "oversized-category":
					w.Header().Set("Lerna-Rate-Category", strings.Repeat("x", 128))
				case "oversized-retry":
					w.Header().Set("Retry-After", strings.Repeat("1", 256))
				}
				w.WriteHeader(429)
				_ = json.NewEncoder(w).Encode(map[string]any{"protocol": "lerna-reference-api-v1", "external_key": r.Header.Get("Idempotency-Key"), "attempt_id": r.Header.Get("Lerna-Attempt"), "account": r.Header.Get("Lerna-Account"), "origin": "http://" + r.Host, "applied": true, "terminal": true})
			})
			f := newFixtureWithTarget(t, 100, 80, false, target)
			d := configureAPI(t, f)
			withAPIKeychain(t, f, d, []byte("synthetic-metadata-canary-32bf6514"))
			a, start := prepareStart(t, f)
			r, e := f.h.Egress.Invoke(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, start)
			accepted(t, r, e)
			op, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
			if e != nil || op.Effect.Outcome != "UNKNOWN" || op.Effect.LateEffect != "MAY_OCCUR" || op.ApiWait == nil || op.ApiWait.Category != "UNKNOWN" || op.ApiWait.ReadyAtUnixMs-op.ApiWait.ObservedAtUnixMs != 1000 || f.calls.Load() != 1 {
				t.Fatal("invalid metadata lost durable 429 wait or promoted effect")
			}
			raw, e := f.h.Ledger.QueryObservation(f.ctx, f.caller, op.Execution.Send.ObservationRef)
			if e != nil || raw.TransportError != "RESPONSE_METADATA_INVALID" || raw.ProviderRequestId != "" || raw.RateCategory != "UNKNOWN" || raw.RetryAfter != "" {
				t.Fatalf("durable metadata%v %v", raw, e)
			}
		})
	}
}

// 规则：G1、G3、G4、G5、G7、G8、开始-4、开始-5
func TestAPICommandLineHostUsesExplicitNativeKeychain(t *testing.T) {
	executable := filepath.Join(t.TempDir(), "lerna")
	if e := exec.Command("go", "build", "-o", executable, "../../cmd/lerna").Run(); e != nil {
		t.Fatal(e)
	}
	var effects atomic.Int64
	secret := []byte("synthetic-cli-native-canary-840ac215")
	target := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+string(secret) {
			t.Error("CLI host used invalid authentication")
		}
		effects.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{"protocol": "lerna-reference-api-v1", "external_key": r.Header.Get("Idempotency-Key"), "attempt_id": r.Header.Get("Lerna-Attempt"), "account": r.Header.Get("Lerna-Account"), "origin": "http://" + r.Host, "applied": true, "terminal": true})
	})
	f := newFixtureWithTarget(t, 100, 80, false, target)
	d := configureAPI(t, f)
	keychain := withAPIKeychain(t, f, d, secret)
	a, start := prepareStart(t, f)
	data, e := protojson.Marshal(start)
	if e != nil {
		t.Fatal(e)
	}
	path := filepath.Join(t.TempDir(), "start.json")
	if e = os.WriteFile(path, data, 0600); e != nil {
		t.Fatal(e)
	}
	blocked := exec.Command(executable, "--db", f.path, "--user", "u", "--issuer", "egress", "--domain", "d", "--api-keychain", keychain.Path(), "execute", path)
	blockedOutput, blockedError := blocked.CombinedOutput()
	if blockedError == nil || !bytes.Contains(blockedOutput, []byte("CREDENTIAL_UNAVAILABLE")) || bytes.Contains(blockedOutput, secret) || f.calls.Load() != 0 {
		t.Fatal("creator-only native item allowed another executable or leaked a secret")
	}
	before, e := f.h.Ledger.QueryExecution(f.ctx, f.caller, a.OperationId)
	if e != nil || before.Send.Phase != "REGISTERED" {
		t.Fatal("native ACL rejection opened P5")
	}
	if e := keychain.Remove(d.Binding); e != nil {
		t.Fatal(e)
	}
	if e := keychain.PutForExecutable(d.Binding, secret, executable); e != nil {
		t.Fatal(e)
	}
	child := exec.Command(executable, "--db", f.path, "--user", "u", "--issuer", "egress", "--domain", "d", "--api-keychain", keychain.Path(), "execute", path)
	out, e := child.CombinedOutput()
	if bytes.Contains(out, secret) {
		t.Fatal("CLI output included secret")
	}
	if e != nil {
		t.Fatalf("CLI native request failed: %v", e)
	}
	op, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
	if e != nil || op.Effect.Outcome != "APPLIED" || op.Effect.LateEffect != "RULED_OUT" || effects.Load() != 1 || f.calls.Load() != 1 {
		t.Fatal("CLI host lost effect or bypassed single request gate")
	}
	budget, e := f.h.Budget.QueryReservations(f.ctx, f.caller, a.TaskId)
	if e != nil || len(budget) != 1 || budget[0].ConsumedSends != 1 {
		t.Fatal("CLI ACL retry consumed another send")
	}
}

// 规则：G1、G2、G3、G4、G10、G11、完成-4
func TestAPIQueryFailuresKeepOriginalResponsibilityAndNeverResend(t *testing.T) {
	for _, scenario := range []string{"drop", "malformed", "duplicate", "tool-error", "wrong-protocol", "wrong-account", "wrong-origin", "wrong-query-operation", "wrong-query-attempt", "wrong-query-key", "wrong-original-operation", "wrong-original-attempt", "wrong-original-key", "wrong-scope", "no-read-terminal", "retention-expired", "temporarily-unavailable", "weak-terminal-absence", "strong-negative"} {
		t.Run(scenario, func(t *testing.T) {
			var writes, queries, effects atomic.Int64
			target := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == "POST" {
					writes.Add(1)
					if scenario != "strong-negative" {
						effects.Add(1)
					}
					conn, _, e := w.(http.Hijacker).Hijack()
					if e != nil {
						t.Error(e)
						return
					}
					_ = conn.Close()
					return
				}
				queries.Add(1)
				response := apiQueryResponse(r)
				switch scenario {
				case "drop":
					conn, _, e := w.(http.Hijacker).Hijack()
					if e != nil {
						t.Error(e)
						return
					}
					_ = conn.Close()
					return
				case "malformed":
					_, _ = w.Write([]byte(`{"protocol":`))
					return
				case "duplicate":
					_, _ = w.Write([]byte(`{"protocol":"lerna-reference-api-query-v1","protocol":"lerna-reference-api-query-v1"}`))
					return
				case "tool-error":
					response["tool_error"] = true
				case "wrong-protocol":
					response["protocol"] = "unreviewed-query-v1"
				case "wrong-account":
					response["account"] = "another-account"
				case "wrong-origin":
					response["origin"] = "https://another.invalid"
				case "wrong-query-operation":
					response["query_operation_id"] = "another-query"
				case "wrong-query-attempt":
					response["query_attempt_id"] = "another-attempt"
				case "wrong-query-key":
					response["query_external_key"] = "another-key"
				case "wrong-original-operation":
					response["subject_operation_id"] = "another-original"
				case "wrong-original-attempt":
					response["subject_attempt_id"] = "another-original-attempt"
				case "wrong-original-key":
					response["subject_external_key"] = "another-original-key"
				case "wrong-scope":
					response["subject_scope"] = "https://another.invalid"
				case "no-read-terminal":
					response["read_terminal"] = false
				case "retention-expired":
					response["query_status"] = "RETENTION_EXPIRED"
				case "temporarily-unavailable":
					response["query_status"] = "TEMPORARILY_UNAVAILABLE"
				case "weak-terminal-absence":
					response["applied"] = false
				case "strong-negative":
					response["applied"] = false
					response["negative_proof"] = true
				}
				_ = json.NewEncoder(w).Encode(response)
			})
			f := newFixtureWithTarget(t, 200, 200, false, target)
			d, cap, grant := configureAPIQueryable(t, f, false)
			keychain := withAPIKeychain(t, f, d, []byte("synthetic-query-failures-canary-184b269a"))
			a, start := prepareStart(t, f)
			r, e := f.h.Egress.Invoke(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, start)
			accepted(t, r, e)
			r, e = f.h.Ledger.RequestReconciliation(f.ctx, f.caller, reconciliationCommand(f, a, cap, grant))
			accepted(t, r, e)
			if e = f.h.Ledger.ProcessReconciliations(f.ctx, f.caller); e != nil {
				t.Fatal(e)
			}
			plan, e := f.h.Ledger.QueryReconciliation(f.ctx, f.caller, a.OperationId)
			if e != nil || plan.CheckCount != 1 || writes.Load() != 1 || queries.Load() != 1 {
				t.Fatalf("query responsibility %v %v", plan, e)
			}
			original, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
			if e != nil {
				t.Fatal(e)
			}
			switch scenario {
			case "strong-negative":
				if plan.State != "COMPLETED" || original.Effect.Outcome != "NOT_APPLIED" || original.Effect.LateEffect != "RULED_OUT" || effects.Load() != 0 {
					t.Fatalf("negative proof %v %v", plan, original.Effect)
				}
			case "weak-terminal-absence", "temporarily-unavailable":
				if plan.State != "WAITING" || original.Effect.Outcome != "UNKNOWN" || original.Effect.LateEffect != "MAY_OCCUR" || effects.Load() != 1 {
					t.Fatalf("weak evidence %v %v", plan, original.Effect)
				}
			default:
				expectedReason := "QUERY_RESULT_UNKNOWN"
				if scenario == "retention-expired" {
					expectedReason = "QUERY_RETENTION_EXPIRED"
				}
				if plan.State != "PAUSED" || plan.PauseReason != expectedReason || original.Effect.Outcome != "UNKNOWN" || original.Effect.LateEffect != "MAY_OCCUR" || effects.Load() != 1 {
					t.Fatalf("unproved query %v %v", plan, original.Effect)
				}
			}
			relation, e := f.h.Ledger.QueryReconciliationQuery(f.ctx, f.caller, plan.QueryRefs[0])
			if e != nil {
				t.Fatal(e)
			}
			query, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, relation.QueryOperationRef.Name)
			if e != nil {
				t.Fatal(e)
			}
			queryAdmission, e := f.h.Tasks.QueryAdmission(f.ctx, f.caller, query.AdmissionRef)
			if e != nil {
				t.Fatal(e)
			}
			hold, e := f.h.Budget.QueryReservation(f.ctx, f.caller, queryAdmission.BudgetBasis.ReservationRef)
			if e != nil || hold.Ceiling != 5 {
				t.Fatalf("lost query fee%v %v", hold, e)
			}
			if e = f.h.Close(); e != nil {
				t.Fatal(e)
			}
			f.h, e = assembly.OpenWithOptions(f.path, "u", "d", assembly.Options{APIKeychainPath: keychain.Path()})
			if e != nil {
				t.Fatal(e)
			}
			if e = f.h.Ledger.ProcessReconciliations(f.ctx, f.caller); e != nil {
				t.Fatal(e)
			}
			if writes.Load() != 1 || queries.Load() != 1 {
				t.Fatal("query restart created implicit request")
			}
		})
	}
}
