//go:build fault

package fault_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/ruipengliu/lerna/cmd/assembly"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"github.com/ruipengliu/lerna/infra/sqlite"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// 规则：G3、G11、R7
func TestTraceSourceReceiverAcknowledgementAndIndexCrashRecovery(t *testing.T) {
	for _, point := range []string{"tasks.planning", "trace.accept", "trace.source_ack", "trace.index"} {
		for _, mode := range []sqlite.FaultMode{sqlite.CrashBeforeCommit, sqlite.CrashAfterCommit, sqlite.LoseReceipt} {
			t.Run(point+"/"+string(mode), func(t *testing.T) {
				ctx := context.Background()
				path := filepath.Join(t.TempDir(), "trace.db")
				h, err := assembly.Open(path, "u", "d")
				if err != nil {
					t.Fatal(err)
				}
				caller := &v1.Caller{UserId: "u", IssuerId: "host"}
				goal := &v1.SubmitGoalCommand{Identity: &v1.CommandIdentity{UserId: "u", IssuerId: "host", TargetDomainId: "d", CommandId: "trace-fault-goal"}, ContractVersion: 1, SchemaId: "lerna.v1.SubmitGoal", FingerprintVersion: 1, Goal: "synthetic goal"}
				if _, err = h.Sessions.SubmitGoal(ctx, caller, goal); err != nil {
					t.Fatal(err)
				}
				if err = h.Sessions.ProcessPending(ctx, caller); err != nil {
					t.Fatal(err)
				}
				q, err := h.Durable.QueryReceipt(ctx, caller, goal.Identity)
				if err != nil {
					t.Fatal(err)
				}
				c := &v1.AcceptRequirementsCommand{Header: &v1.CommandHeader{Identity: &v1.CommandIdentity{UserId: "u", IssuerId: "host", TargetDomainId: "d", CommandId: "trace-fault-requirements"}, ContractVersion: 1, FingerprintVersion: 1, SchemaId: "lerna.v1.AdmissionCommands"}, TaskRef: q.Receipt.TaskRef, InputVersion: 1, Source: "TRUSTED_TEMPLATE", Conditions: []*v1.Requirement{{ConditionId: "created", DescriptionRef: q.Receipt.InputRef, Necessary: true, VerificationRule: "TARGET_RECORD", RuleVersion: 1}}}
				b, err := protojson.Marshal(c)
				if err != nil {
					t.Fatal(err)
				}
				if err = os.WriteFile(path+".json", b, 0600); err != nil {
					t.Fatal(err)
				}
				if err = h.Trace.Recover(ctx, caller); err != nil {
					t.Fatal(err)
				}
				if err = h.Close(); err != nil {
					t.Fatal(err)
				}
				child := exec.Command(os.Args[0], "-test.run=^TestTraceCrashChild$")
				child.Env = append(os.Environ(), "LERNA_TRACE_DB="+path, "LERNA_TRACE_POINT="+point, "LERNA_TRACE_MODE="+string(mode))
				out, err := child.CombinedOutput()
				if mode == sqlite.LoseReceipt {
					if err != nil {
						t.Fatalf("child %v %s", err, out)
					}
				} else {
					var exit *exec.ExitError
					if !errors.As(err, &exit) || exit.ExitCode() != sqlite.CrashExitCode {
						t.Fatalf("missed boundary %v %s", err, out)
					}
				}
				h, err = assembly.Open(path, "u", "d")
				if err != nil {
					t.Fatal(err)
				}
				defer h.Close()
				r, err := h.Tasks.AcceptRequirements(ctx, caller, c)
				requireAccepted(t, r, err)
				if err = h.Trace.Recover(ctx, caller); err != nil {
					t.Fatal(err)
				}
				view, err := h.Trace.Query(ctx, caller, &v1.TraceQuery{TaskId: c.TaskRef.Name})
				if err != nil {
					t.Fatal(err)
				}
				count := 0
				for _, event := range view.Events {
					if event.EventType == "REQUIREMENTS_ACCEPTED" {
						count++
						if !proto.Equal(event.SourceRecordRef, r.ResultRef) {
							t.Fatal("source identity changed")
						}
					}
				}
				if count != 1 || !view.Complete || view.PendingReceipts != 0 {
					t.Fatalf("lost or duplicated handoff: count=%d view=%v", count, view)
				}
				sources, err := h.Trace.QuerySources(ctx, caller)
				if err != nil {
					t.Fatal(err)
				}
				for _, source := range sources {
					actor := &v1.Caller{UserId: "u", IssuerId: source.Command.Header.Identity.IssuerId}
					acceptance, err := h.Trace.QueryReceipt(ctx, actor, source.Command.Header.Identity)
					if err != nil || !proto.Equal(acceptance.Receipt, source.Receipt) {
						t.Fatalf("original receipt missing: %v %v", acceptance, err)
					}
				}
				if err = h.Trace.Recover(ctx, caller); err != nil {
					t.Fatal(err)
				}
				after, err := h.Trace.Query(ctx, caller, &v1.TraceQuery{TaskId: c.TaskRef.Name})
				if err != nil || !proto.Equal(after, view) {
					t.Fatal("repeated recovery changed original event/receipt/index")
				}
			})
		}
	}
}

// 规则：G3、R7
func TestTraceCrashChild(t *testing.T) {
	path := os.Getenv("LERNA_TRACE_DB")
	if path == "" {
		t.Skip("child only")
	}
	h, err := assembly.Open(path, "u", "d")
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	b, err := os.ReadFile(path + ".json")
	if err != nil {
		t.Fatal(err)
	}
	c := new(v1.AcceptRequirementsCommand)
	if err = protojson.Unmarshal(b, c); err != nil {
		t.Fatal(err)
	}
	point := os.Getenv("LERNA_TRACE_POINT")
	ctx, err := sqlite.WithFault(context.Background(), point, sqlite.FaultMode(os.Getenv("LERNA_TRACE_MODE")))
	if err != nil {
		t.Fatal(err)
	}
	caller := &v1.Caller{UserId: "u", IssuerId: "host"}
	if point == "tasks.planning" {
		_, err = h.Tasks.AcceptRequirements(ctx, caller, c)
	} else {
		r, e := h.Tasks.AcceptRequirements(context.Background(), caller, c)
		requireAccepted(t, r, e)
		err = h.Trace.Recover(ctx, caller)
	}
	if err == nil {
		t.Fatal("injected receipt loss not returned")
	}
}
