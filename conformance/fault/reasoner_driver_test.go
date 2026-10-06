//go:build fault

package fault_test

import (
	"context"
	"errors"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/cmd/assembly"
	"github.com/ruipengliu/lerna/conformance/simulator"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"github.com/ruipengliu/lerna/infra/sqlite"
	"google.golang.org/protobuf/proto"
)

// 规则：G1、G3、G4、G5、G10、G11、开始-1、V4
func TestReasonerDriverResumesOriginalP4P5AndModelResultAfterProcessCrash(t *testing.T) {
	for _, point := range []string{"tasks.start", "ledger.dispatch", "tasks.model_result"} {
		t.Run(point, func(t *testing.T) {
			t.Parallel()
			provider := simulator.NewModelProvider()
			provider.Output = `{"kind":"QUESTION","question":{"question":"Choose destination"}}`
			server := httptest.NewServer(provider)
			defer server.Close()
			path := filepath.Join(t.TempDir(), "driver.db")
			h, e := assembly.Open(path, "u", "d")
			if e != nil {
				t.Fatal(e)
			}
			ctx := context.Background()
			caller := &v1.Caller{UserId: "u", IssuerId: "host"}
			run := prepareModelFault(t, h, server.URL)
			request, e := h.Tasks.QueryProposalRequest(ctx, caller, run.Preparation.RequestRef)
			if e != nil {
				t.Fatal(e)
			}
			claim := run.Preparation.Claim
			r, e := h.Durable.ExecuteJob(ctx, caller, &v1.JobCommand{Identity: admissionHeader("release-fixture-propose").Identity, ContractVersion: 1, Action: "PROGRESS", JobRef: claim.Ref, ProcessInstance: claim.ProcessInstance, ClaimEpoch: claim.ClaimEpoch, NextState: "WAITING"})
			requireAccepted(t, r, e)
			configuration := &v1.ConfigureReasonerDriverCommand{Header: admissionHeader("driver-disabled"), TaskId: request.TaskId, Policy: &v1.ReasonerDriverPolicy{Settings: run.Preparation.Settings, ModelCapabilityRef: run.Preparation.CapabilityRef, ModelGrantRef: run.GrantRef}, Disabled: true}
			r, e = h.Tasks.ConfigureReasonerDriver(ctx, caller, configuration)
			requireAccepted(t, r, e)
			configuration.Header = admissionHeader("driver-enabled")
			configuration.Replaces = r.ResultRef
			configuration.Disabled = false
			encoded, e := proto.Marshal(configuration)
			if e != nil {
				t.Fatal(e)
			}
			if e = os.WriteFile(path+".driver", encoded, 0600); e != nil {
				t.Fatal(e)
			}
			if e = h.Close(); e != nil {
				t.Fatal(e)
			}
			child := exec.Command(os.Args[0], "-test.run=^TestReasonerDriverCrashChild$")
			child.Env = append(os.Environ(), "LERNA_DRIVER_DB="+path, "LERNA_DRIVER_POINT="+point)
			output, e := child.CombinedOutput()
			var exit *exec.ExitError
			if !errors.As(e, &exit) || exit.ExitCode() != sqlite.CrashExitCode {
				t.Fatalf("fault missed: %v %s", e, output)
			}
			h, e = assembly.Open(path, "u", "d")
			if e != nil {
				t.Fatal(e)
			}
			defer h.Close()
			waiting, e := h.Tasks.QueryReasonerDriver(ctx, caller, request.TaskId)
			if e != nil || waiting.WaitingReason != "CLAIM_PENDING" || !proto.Equal(waiting.RequestRef, request.Ref) {
				t.Fatalf("claimed original recovery: %v %v", waiting, e)
			}
			before, e := h.Tasks.QueryModelCall(ctx, caller, request.Ref, 0)
			if e != nil || before == nil || before.AdmissionRef == nil {
				t.Fatalf("original call lost: %v %v", before, e)
			}
			admission, e := h.Tasks.QueryAdmission(ctx, caller, before.AdmissionRef)
			if e != nil {
				t.Fatal(e)
			}
			operation, e := h.Ledger.QueryOperation(ctx, caller, admission.OperationId)
			if e != nil || operation.Execution == nil {
				t.Fatalf("original execution lost: %v %v", operation, e)
			}
			originalSend := proto.Clone(operation.Execution.Send.Ref.Name).(*v1.GlobalName)
			expectedBefore := 0
			if point == "tasks.model_result" {
				expectedBefore = 1
			}
			if provider.Calls() != expectedBefore || len(provider.Bills()) != expectedBefore {
				t.Fatalf("restart sent while old lease live: %d %d", provider.Calls(), len(provider.Bills()))
			}
			job, e := h.Durable.QueryJob(ctx, caller, request.JobRef.Name)
			if e != nil {
				t.Fatal(e)
			}
			// 等待真实原租约到期；不改写数据库、复用死进程身份或制造领取。
			remaining := time.Until(time.UnixMilli(job.LeaseUntilUnixMs)) + 20*time.Millisecond
			if remaining > 0 {
				timer := time.NewTimer(remaining)
				defer timer.Stop()
				<-timer.C
			}
			recovered, e := h.Tasks.AdvanceReasonerTask(ctx, caller, &v1.AdvanceReasonerTaskRequest{TaskId: request.TaskId})
			if e != nil {
				t.Fatal(e)
			}
			expectedCalls := 1
			expectedReason := "SESSION_REQUIRED"
			if point == "ledger.dispatch" {
				expectedCalls = 0
				expectedReason = "UNKNOWN"
			}
			if recovered.WaitingReason != expectedReason || provider.Calls() != expectedCalls || len(provider.Bills()) != expectedCalls || !proto.Equal(recovered.RequestRef, request.Ref) {
				t.Fatalf("recovery: %v calls=%d bills=%d", recovered, provider.Calls(), len(provider.Bills()))
			}
			after, e := h.Tasks.QueryModelCall(ctx, caller, request.Ref, 0)
			if e != nil || !proto.Equal(after.Ref, before.Ref) || !proto.Equal(after.AdmissionRef, before.AdmissionRef) || !proto.Equal(after.InputRef, before.InputRef) {
				t.Fatalf("replaced original position: %v %v", after, e)
			}
			operation, e = h.Ledger.QueryOperation(ctx, caller, admission.OperationId)
			if e != nil || operation.Execution.Send.SendSeq != 1 || !proto.Equal(operation.Execution.Send.Ref.Name, originalSend) {
				t.Fatalf("replaced original send: %v %v", operation, e)
			}
			budget, e := h.Budget.QueryBudget(ctx, caller, nil)
			if e != nil {
				t.Fatal(e)
			}
			if point == "ledger.dispatch" {
				if budget.Reserved != 30 || budget.Settled != 0 {
					t.Fatalf("lost unknown hold: %v", budget)
				}
			} else if budget.Reserved != 0 || budget.Settled != 7 {
				t.Fatalf("duplicate/lost bill: %v", budget)
			}
			original, e := h.Tasks.QueryProposalRequest(ctx, caller, request.Ref)
			if e != nil || len(original.ModelOperationRefs) != 1 || len(original.OutcomeRefs) != 1 {
				t.Fatalf("duplicated responsibility: %v %v", original, e)
			}
			if e = h.Trace.Recover(ctx, caller); e != nil {
				t.Fatal(e)
			}
			sources, e := h.Trace.QuerySources(ctx, caller)
			if e != nil {
				t.Fatal(e)
			}
			for _, source := range sources {
				event := source.Command.Event
				if event.SourceRecordRef.GetSchemaId() != "lerna.v1.ReasonerDriver" {
					continue
				}
				version, e := h.Tasks.QueryReasonerDriverVersion(ctx, caller, event.SourceRecordRef)
				if e != nil || version == nil || !proto.Equal(version.Ref, event.SourceRecordRef) || source.Receipt == nil {
					t.Fatalf("orphan driver source: %v %v", source, e)
				}
			}
		})
	}
}

// 规则：G3、V4
func TestReasonerDriverCrashChild(t *testing.T) {
	path := os.Getenv("LERNA_DRIVER_DB")
	if path == "" {
		t.Skip("child only")
	}
	h, e := assembly.Open(path, "u", "d")
	if e != nil {
		t.Fatal(e)
	}
	defer h.Close()
	encoded, e := os.ReadFile(path + ".driver")
	if e != nil {
		t.Fatal(e)
	}
	configuration := new(v1.ConfigureReasonerDriverCommand)
	if e = proto.Unmarshal(encoded, configuration); e != nil {
		t.Fatal(e)
	}
	caller := &v1.Caller{UserId: "u", IssuerId: "host"}
	r, e := h.Tasks.ConfigureReasonerDriver(context.Background(), caller, configuration)
	requireAccepted(t, r, e)
	ctx, e := sqlite.WithFault(context.Background(), os.Getenv("LERNA_DRIVER_POINT"), sqlite.CrashAfterCommit)
	if e != nil {
		t.Fatal(e)
	}
	state, e := h.Tasks.AdvanceReasonerTask(ctx, caller, &v1.AdvanceReasonerTaskRequest{TaskId: configuration.TaskId})
	t.Fatalf("fault did not fire: %v %v", state, e)
}
