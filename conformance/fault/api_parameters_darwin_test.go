//go:build fault && darwin && cgo

package fault_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/ruipengliu/lerna/cmd/assembly"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"github.com/ruipengliu/lerna/defaults/scripted"
	"github.com/ruipengliu/lerna/infra/sqlite"
	"google.golang.org/protobuf/proto"
)

// 规则：G1、G3、G4、G5、G7、G8、G11、R7、开始-4、开始-5
func TestAPIParameterSourceCrashRecoveryPublishesOriginalBodyBeforeSend(t *testing.T) {
	for _, point := range []string{"content.register", "body.accept", "content.publish", "trace.source_ack", "trace.index"} {
		for _, mode := range []sqlite.FaultMode{sqlite.CrashBeforeCommit, sqlite.CrashAfterCommit, sqlite.LoseReceipt} {
			t.Run(point+"/"+string(mode), func(t *testing.T) {
				var calls atomic.Int64
				target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					calls.Add(1)
					body, err := io.ReadAll(r.Body)
					if err != nil || string(body) != `{"quantity":9,"value":"api-source-body-canary-6719c096"}` {
						t.Error("target did not receive original published parameters")
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"protocol": "lerna-reference-api-v1", "external_key": r.Header.Get("Idempotency-Key"), "attempt_id": r.Header.Get("Lerna-Attempt"), "account": r.Header.Get("Lerna-Account"), "origin": "http://" + r.Host, "applied": true, "terminal": true})
				}))
				defer target.Close()
				ctx, actor := context.Background(), &v1.Caller{UserId: "u", IssuerId: "host"}
				path := filepath.Join(t.TempDir(), "api-parameters.db")
				h, err := assembly.Open(path, "u", "d")
				if err != nil {
					t.Fatal(err)
				}
				admit := prepareAdmissionAdapter(t, h, target.URL, "api-reference-v1", 2)
				state, err := h.Tasks.QueryPlanning(ctx, actor, admit.TaskId)
				if err != nil {
					t.Fatal(err)
				}
				capability := state.Proposal.Step.CapabilityRef
				hdr := admissionHeader("api-parameter-source")
				hdr.Identity.TargetDomainId = "d/content"
				original := &v1.RegisterContentCommand{Header: hdr, TaskId: admit.TaskId, Body: []byte(` {"value":"api-source-body-canary-6719c096","quantity":9} `), MediaType: "application/json", SourceDescriptor: &v1.ContentSourceDescriptor{Kind: "HOST_IMPORT", Locator: "https://provider-private-source.invalid/local-path-canary", AcquisitionMethod: "LOCAL_IMPORT", ProviderVersion: "v1"}}
				encoded, err := proto.Marshal(original)
				if err != nil {
					t.Fatal(err)
				}
				if err = os.WriteFile(path+".parameters", encoded, 0600); err != nil {
					t.Fatal(err)
				}
				if err = h.Trace.Recover(ctx, actor); err != nil {
					t.Fatal(err)
				}
				if err = h.Close(); err != nil {
					t.Fatal(err)
				}
				child := exec.Command(os.Args[0], "-test.run=^TestAPIParameterSourceCrashChild$")
				child.Env = append(os.Environ(), "LERNA_API_PARAMETERS_DB="+path, "LERNA_API_PARAMETERS_POINT="+point, "LERNA_API_PARAMETERS_MODE="+string(mode))
				out, err := child.CombinedOutput()
				if mode == sqlite.LoseReceipt {
					if err != nil {
						t.Fatalf("child%v %s", err, out)
					}
				} else {
					var exit *exec.ExitError
					if !errors.As(err, &exit) || exit.ExitCode() != sqlite.CrashExitCode {
						t.Fatalf("fault not reached%v %s", err, out)
					}
				}
				if calls.Load() != 0 {
					t.Fatal("parameter source fault sent before P4/P5")
				}
				h, err = assembly.Open(path, "u", "d")
				if err != nil {
					t.Fatal(err)
				}
				r, err := h.Content.Register(ctx, actor, original)
				requireAccepted(t, r, err)
				if err = h.Content.ProcessRegistrations(ctx, actor); err != nil {
					t.Fatal(err)
				}
				body, err := h.Content.Read(ctx, actor, r.ResultRef)
				if err != nil || body.Status != "AVAILABLE" || string(body.RawBody) != string(original.Body) {
					t.Fatal("original parameter body changed during recovery", err)
				}
				snapshot, err := h.Tasks.RequestProposal(ctx, actor, &v1.RequestProposalCommand{Header: admissionHeader("imported-parameters-request"), TaskId: admit.TaskId})
				if err != nil {
					t.Fatal(err)
				}
				proposal, err := (&scripted.Reasoner{Step: &v1.ActionStep{StepId: "one", ParametersRef: r.ResultRef, CapabilityRef: capability}}).Propose(ctx, snapshot)
				if err != nil {
					t.Fatal(err)
				}
				r, err = h.Tasks.ReceiveProposal(ctx, actor, &v1.ReceiveProposalCommand{Header: admissionHeader("imported-parameters-proposal"), Proposal: proposal})
				requireAccepted(t, r, err)
				admit.ProposalRef = r.ResultRef
				h, keychain, a, start := prepareAPIStartFromAdmissionFault(t, h, path, admit)
				defer h.Close()
				r, err = h.Egress.Invoke(ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, start)
				requireAccepted(t, r, err)
				op, err := h.Ledger.QueryOperation(ctx, actor, a.OperationId)
				if err != nil || op.Effect.Outcome != "APPLIED" || op.Effect.LateEffect != "RULED_OUT" || calls.Load() != 1 {
					t.Fatal("published parameter execution lost", err)
				}
				assertAPIRecoveredSources(t, h, op, 0, keychain.Path(), target.URL, "synthetic-api-fault-secret-f17e4038", "api-source-body-canary-6719c096", original.SourceDescriptor.Locator)
				sources, err := h.Trace.QuerySources(ctx, actor)
				if err != nil {
					t.Fatal(err)
				}
				staged, published := 0, 0
				for _, source := range sources {
					event := source.Command.Event
					if proto.Equal(event.SourceRecordRef, body.Ref) {
						if !proto.Equal(event.OriginCommand, original.Header.Identity) || !proto.Equal(event.BodyRef, body.Ref) || !proto.Equal(event.TaskId, admit.TaskId) {
							t.Fatal("parameter source identity changed")
						}
						if event.EventType == "CONTENT_STAGED" {
							staged++
						}
						if event.EventType == "CONTENT_PUBLISHED" {
							published++
						}
					}
				}
				if staged != 1 || published != 1 {
					t.Fatal("original parameter sources duplicated or omitted")
				}
			})
		}
	}
}

// 规则：G1、G3、G4、G5、G11、R7
func TestAPIParameterSourceCrashChild(t *testing.T) {
	path := os.Getenv("LERNA_API_PARAMETERS_DB")
	if path == "" {
		t.Skip("subprocess only")
	}
	h, err := assembly.Open(path, "u", "d")
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	encoded, err := os.ReadFile(path + ".parameters")
	if err != nil {
		t.Fatal(err)
	}
	original := new(v1.RegisterContentCommand)
	if err = proto.Unmarshal(encoded, original); err != nil {
		t.Fatal(err)
	}
	ctx, err := sqlite.WithFault(context.Background(), os.Getenv("LERNA_API_PARAMETERS_POINT"), sqlite.FaultMode(os.Getenv("LERNA_API_PARAMETERS_MODE")))
	if err != nil {
		t.Fatal(err)
	}
	actor := &v1.Caller{UserId: "u", IssuerId: "host"}
	_, err = h.Content.Register(ctx, actor, original)
	if err == nil {
		err = h.Content.ProcessRegistrations(ctx, actor)
	}
	if err == nil {
		err = h.Trace.Recover(ctx, actor)
	}
	if err == nil {
		t.Fatal("fault not reached")
	}
}
