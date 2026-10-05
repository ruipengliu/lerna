package admission_test

import (
	"bytes"
	"io"
	"net/http"
	"testing"
	"time"

	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

// 规则：G4、G5、G9
func TestRawObservationCanBeReusedWithoutChangingItsBytes(t *testing.T) {
	for _, tc := range []struct {
		name    string
		payload []byte
	}{
		{"binary", []byte{0, 255, 128, 'x', 0}},
		{"empty", []byte{}},
		{"utf8", []byte("观察\x00正文")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			received := make(chan []byte, 2)
			target := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				b, e := io.ReadAll(r.Body)
				if e != nil {
					t.Error(e)
				}
				received <- b
				_, _ = w.Write(tc.payload)
			})
			f := newFixtureWithTarget(t, 100, 80, false, target)
			a, c := prepareStart(t, f)
			r, e := f.h.Egress.Invoke(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, c)
			accepted(t, r, e)
			<-received
			x, e := f.h.Ledger.QueryExecution(f.ctx, f.caller, a.OperationId)
			if e != nil {
				t.Fatal(e)
			}
			raw, e := f.h.Ledger.QueryObservation(f.ctx, f.caller, x.Send.ObservationRef)
			if e != nil {
				t.Fatal(e)
			}
			content, e := f.h.Content.Read(f.ctx, f.caller, raw.BodyRef)
			if e != nil || !bytes.Equal(content.RawBody, tc.payload) {
				t.Fatalf("stored bytes %v %v", content, e)
			}
			r, e = f.h.Sessions.SubmitGoal(f.ctx, f.caller, &v1.SubmitGoalCommand{Identity: header("raw-goal").Identity, ContractVersion: 1, SchemaId: "lerna.v1.SubmitGoal", FingerprintVersion: 1, Goal: "reuse observed bytes"})
			if e != nil {
				t.Fatal(e)
			}
			if e = f.h.Sessions.ProcessPending(f.ctx, f.caller); e != nil {
				t.Fatal(e)
			}
			q, e := f.h.Durable.QueryReceipt(f.ctx, f.caller, r.Identity)
			if e != nil {
				t.Fatal(e)
			}
			f.task = q.Receipt.TaskRef
			r, e = f.h.Tasks.AcceptRequirements(f.ctx, f.caller, &v1.AcceptRequirementsCommand{Header: header("raw-requirements"), TaskRef: f.task, InputVersion: 1, Conditions: []*v1.Requirement{{ConditionId: "created", DescriptionRef: q.Receipt.InputRef, Necessary: true, VerificationRule: "TARGET_RECORD", RuleVersion: 1}}, Source: "TRUSTED_TEMPLATE"})
			accepted(t, r, e)
			r, e = f.h.Budget.Configure(f.ctx, f.caller, &v1.ConfigureBudgetCommand{Header: header("raw-budget"), TaskId: f.task.Name, Unit: "USD_MICRO", Limit: 80})
			accepted(t, r, e)
			original, e := f.h.Grants.QueryGrant(f.ctx, f.caller, f.grant)
			if e != nil {
				t.Fatal(e)
			}
			g := proto.Clone(original).(*v1.Grant)
			g.Ref = nil
			g.Issuer = nil
			g.Status = ""
			g.Subject = f.task.Name
			g.UsePoolId = "raw-second"
			g.ValidFromUnixMs = time.Now().Add(-time.Minute).UnixMilli()
			r, e = f.h.Grants.Configure(f.ctx, f.caller, &v1.ConfigureGrantCommand{Header: header("raw-grant"), Grant: g})
			accepted(t, r, e)
			f.grant = r.ResultRef
			f.parameters = raw.BodyRef
			f.suffix = "-raw"
			_, second := prepareStart(t, f)
			r, e = f.h.Egress.Invoke(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, second)
			accepted(t, r, e)
			if actual := <-received; !bytes.Equal(actual, tc.payload) {
				t.Fatalf("sent bytes %x, want %x", actual, tc.payload)
			}
			if f.calls.Load() != 2 {
				t.Fatalf("received %d requests", f.calls.Load())
			}
		})
	}
}
