//go:build darwin && cgo

package admission_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"testing"

	"github.com/ruipengliu/lerna/cmd/assembly"
	"github.com/ruipengliu/lerna/conformance/protobuf"
	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"github.com/ruipengliu/lerna/core/egress"
	"github.com/ruipengliu/lerna/infra/egressio"
	"github.com/ruipengliu/lerna/infra/keys"
	"google.golang.org/protobuf/proto"
)

// 规则：H1、G1、G3、G4、G5、G7、G8、G11
func TestCaptureNativeAPIWaitAndRedactionPublicObjectsRoundTrip(t *testing.T) {
	var samples []protobuf.Sample
	captureNativeAPI(t, &samples)
	requireCapturedTypes(t, samples, []string{"lerna.v1.ApiTargetBinding", "lerna.v1.ApiDescriptor", "lerna.v1.ApiWait"})
	requireCapturedFields(t, samples, []string{"lerna.v1.Capability.api_descriptor", "lerna.v1.Operation.api_wait", "lerna.v1.CallDescriptor.api_descriptor", "lerna.v1.CallDescriptor.parameters_digest", "lerna.v1.PhysicalSend.api_wait", "lerna.v1.RawObservation.rate_category", "lerna.v1.RawObservation.retry_after", "lerna.v1.RawObservation.redacted"})
	roundTripClosingSamples(t, samples)
}

// recordingNativeAPIIO 委托正式凭据预检与原生 HTTP，只复制实际边界对象。
type recordingNativeAPIIO struct {
	egressio.APIHTTP
	t       *testing.T
	samples *[]protobuf.Sample
	prefix  string
}

func (r *recordingNativeAPIIO) Perform(ctx context.Context, request *v1.PhysicalIORequest) (*v1.PhysicalIOResult, error) {
	captureObject(r.t, r.samples, r.prefix+"-io-request", request, nil)
	result, e := r.APIHTTP.Perform(ctx, request)
	captureObject(r.t, r.samples, r.prefix+"-io-result", result, e)
	return result, e
}

func captureNativeAPI(t *testing.T, samples *[]protobuf.Sample) {
	t.Helper()
	first := len(*samples)
	for _, category := range []string{"RATE", "CONCURRENCY", "RESOURCE_CONFLICT", "UNKNOWN", "REDACTED"} {
		captureNativeAPIBoundary(t, samples, category)
	}
	t.Logf("actual native API: four distinct 429 waits and one secret-redacted response, POST5/effects0; %d public objects", len(*samples)-first)
}
func captureNativeAPIBoundary(t *testing.T, samples *[]protobuf.Sample, category string) {
	t.Helper()
	prefix := "api-native-" + category
	secret := "synthetic-h1-native-api-secret-609842"
	target := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, e := io.ReadAll(r.Body)
		if e != nil || r.Method != "POST" || string(body) != `{"quantity":1,"value":"hello"}` || r.Header.Get("Authorization") != "Bearer "+secret || r.Header.Get("Lerna-Account") != "synthetic-account" || r.Header.Get("Lerna-Attempt") == "" || r.Header.Get("Idempotency-Key") == "" {
			t.Error("native request lost fixed parameters/credential/account/attempt")
		}
		if category == "REDACTED" {
			_, _ = io.WriteString(w, secret)
			return
		}
		w.Header().Set("Lerna-Rate-Category", category)
		w.Header().Set("Retry-After", "2")
		w.WriteHeader(http.StatusTooManyRequests)
	})
	f := newFixtureWithTarget(t, 100, 80, false, target)
	d, _, _ := configureAPIQueryable(t, f, true)
	keychain := withAPIKeychain(t, f, d, []byte(secret))
	secure, e := keys.OpenFileBased(keychain.Path())
	if e != nil {
		t.Fatal(e)
	}
	native := &recordingNativeAPIIO{APIHTTP: egressio.APIHTTP{Credentials: secure}, t: t, samples: samples, prefix: prefix}
	lock, e := egressio.NewFileLock(f.path)
	if e != nil {
		t.Fatal(e)
	}
	gateway := egress.New(f.h.Tasks, f.h.Ledger, f.h.Content, native, lock)
	a, start := prepareStart(t, f)
	actor := &v1.Caller{UserId: "u", IssuerId: "egress"}
	receipt, e := gateway.Invoke(f.ctx, actor, start)
	accepted(t, receipt, e)
	add := func(name string, m proto.Message, e error) {
		t.Helper()
		captureObject(t, samples, prefix+"-"+name, m, e)
	}
	add("start", start, nil)
	add("admission", a, nil)
	add("receipt", receipt, nil)
	cap, e := f.h.Tasks.QueryCapability(f.ctx, f.caller, f.capability)
	add("capability", cap, e)
	add("descriptor", cap.ApiDescriptor, nil)
	add("binding", cap.ApiDescriptor.Binding, nil)
	op, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
	add("operation", op, e)
	add("call-descriptor", op.Execution.CallDescriptor, nil)
	add("send", op.Execution.Send, nil)
	raw, e := f.h.Ledger.QueryObservation(f.ctx, f.caller, op.Execution.Send.ObservationRef)
	add("observation", raw, e)
	source, e := f.h.Budget.QueryBillingSource(f.ctx, f.caller, op.Execution.Send.Ref)
	add("billing-source", source, e)
	body, e := f.h.Content.Read(f.ctx, f.caller, f.parameters)
	add("parameters", body, e)
	if !proto.Equal(cap.ApiDescriptor, d) || !proto.Equal(op.Execution.CallDescriptor.ApiDescriptor, d) || op.Execution.CallDescriptor.ParametersDigest != command.BytesDigest(command.ContentBytes(body)) || !proto.Equal(op.Execution.CallDescriptor.ParametersRef, a.ParametersRef) || !proto.Equal(raw.OperationId, a.OperationId) || !proto.Equal(raw.SendRef.Name, op.Execution.Send.Ref.Name) || raw.ExternalKey != op.Execution.Attempt.ExternalKey || source.Amount != nil || source.Status != "PENDING" || op.Effect.Outcome != "UNKNOWN" || op.Effect.LateEffect != "MAY_OCCUR" || f.calls.Load() != 1 {
		t.Fatal("actual native descriptor, input, unknown responsibility or source drift")
	}
	if category == "REDACTED" {
		if !raw.Redacted || raw.TransportError != "CREDENTIAL_ECHO_REDACTED" || op.ApiWait != nil {
			t.Fatalf("native redaction absent: %v", raw)
		}
	} else {
		if raw.StatusCode != 429 || raw.RateCategory != category || raw.RetryAfter != "2" || op.ApiWait == nil || op.ApiWait.Category != category || op.ApiWait.ReadyAtUnixMs-op.ApiWait.ObservedAtUnixMs != 2000 || !proto.Equal(op.ApiWait, op.Execution.Send.ApiWait) || !proto.Equal(op.ApiWait.ObservationRef, raw.Ref) || !proto.Equal(op.ApiWait.SendRef.Name, op.Execution.Send.Ref.Name) {
			t.Fatalf("real classified wait missing: %v %v", op.ApiWait, raw)
		}
		add("wait", op.ApiWait, nil)
	}
	if e = f.h.Trace.Recover(f.ctx, f.caller); e != nil {
		t.Fatal(e)
	}
	sources, e := f.h.Trace.QuerySources(f.ctx, f.caller)
	if e != nil {
		t.Fatal(e)
	}
	for _, s := range sources {
		if proto.Equal(s.Command.Event.OperationId, a.OperationId) && s.Command.Event.EventType == "API_WAIT_RECORDED" {
			add("wait-trace-source", s, nil)
			event, e := f.h.Trace.QueryEvent(f.ctx, f.caller, s.Command.Event.Ref)
			add("wait-trace-event", event, e)
			if !proto.Equal(event, s.Command.Event) || s.Receipt == nil {
				t.Fatal("wait source not acknowledged unchanged")
			}
		}
	}
	replay, e := gateway.Invoke(f.ctx, actor, start)
	accepted(t, replay, e)
	if !proto.Equal(replay, receipt) || f.calls.Load() != 1 {
		t.Fatal("capture replay caused another native request")
	}
	if e = f.h.Close(); e != nil {
		t.Fatal(e)
	}
	f.h, e = assembly.OpenWithOptions(f.path, "u", "d", assembly.Options{APIKeychainPath: keychain.Path()})
	if e != nil {
		t.Fatal(e)
	}
	restored, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
	if e != nil || !proto.Equal(restored, op) || f.calls.Load() != 1 {
		t.Fatal("restart changed captured original native operation", e)
	}
	for _, sample := range *samples {
		wire, e := proto.Marshal(sample.Message)
		if e != nil || bytes.Contains(wire, []byte(secret)) || bytes.Contains(wire, []byte(keychain.Path())) {
			t.Fatalf("sample leaked native secret or store: %s", sample.Name)
		}
	}
}
