//go:build fault

package fault_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/ruipengliu/lerna/adapters/interaction"
	"github.com/ruipengliu/lerna/cmd/assembly"
	"github.com/ruipengliu/lerna/conformance/simulator"
	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"github.com/ruipengliu/lerna/infra/sqlite"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// 规则：G4、G8、G9
func TestConfirmationMetadataDoesNotBypassUnavailableBody(t *testing.T) {
	for _, kind := range []string{"ordinary", "model", "closure", "exact-grant", "any-grant"} {
		t.Run(kind, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "confirmation-governance.db")
			h, e := assembly.Open(path, "u", "d")
			if e != nil {
				t.Fatal(e)
			}
			defer func() { h.Close() }()
			ctx := context.Background()
			caller := &v1.Caller{UserId: "u", IssuerId: "host"}
			ref, parameters, assertCalls := prepareGovernedConfirmation(t, h, kind)
			original, e := h.Sessions.QueryConfirmation(ctx, caller, ref)
			if e != nil {
				t.Fatal(e)
			}
			originalSource := confirmationSource(t, h, ctx, caller, original)
			var metadata map[string]json.RawMessage
			if e = json.Unmarshal([]byte(original.Description), &metadata); e != nil || metadata["Parameters"] != nil {
				t.Fatalf("metadata persists parameter copy %v %v", original, e)
			}
			displayed, e := h.Sessions.ReadConfirmation(ctx, caller, ref)
			if e != nil {
				t.Fatal(e)
			}
			if parameters != nil {
				content, e := h.Content.Read(ctx, caller, parameters)
				if e != nil {
					t.Fatal(e)
				}
				var values struct{ Parameters json.RawMessage }
				if e = json.Unmarshal([]byte(displayed.Description), &values); e != nil {
					t.Fatal(e)
				}
				expected := command.DescribeParameters(content)
				if kind == "exact-grant" {
					var got []command.ParameterDescription
					if e = json.Unmarshal(values.Parameters, &got); e != nil || len(got) != 1 || got[0] != expected {
						t.Fatalf("wrong grant display %s %v", displayed.Description, e)
					}
				} else {
					var got command.ParameterDescription
					if e = json.Unmarshal(values.Parameters, &got); e != nil || got != expected {
						t.Fatalf("wrong action display %s %v", displayed.Description, e)
					}
				}
			}
			displayedProjection := proto.Clone(displayed).(*v1.Confirmation)
			displayedProjection.Description = original.Description
			if !proto.Equal(displayedProjection, original) {
				t.Fatal("display changed immutable approval matter")
			}
			other := &v1.Caller{UserId: "other", IssuerId: "host"}
			if _, e = h.Sessions.ReadConfirmation(ctx, other, ref); e == nil {
				t.Fatal("cross-user display")
			}
			unavailable := sqlite.WithContentReadUnavailable(ctx)
			if parameters != nil {
				if _, e = h.Content.Read(unavailable, caller, parameters); e == nil {
					t.Fatal("body fault missed")
				}
			}
			historical, e := h.Sessions.QueryConfirmation(unavailable, caller, ref)
			if e != nil || !proto.Equal(historical, original) {
				t.Fatalf("structural history unavailable or mutated %v %v", historical, e)
			}
			current, e := h.Sessions.QueryCurrentConfirmation(unavailable, caller, ref.Name)
			if e != nil || !proto.Equal(current, original) {
				t.Fatalf("structural current unavailable or mutated %v %v", current, e)
			}
			read, e := h.Sessions.ReadConfirmation(unavailable, caller, ref)
			if parameters != nil && (e == nil || read != nil) {
				t.Fatalf("unavailable historical display %v %v", read, e)
			}
			if parameters == nil && (e != nil || read == nil) {
				t.Fatalf("ANY grant acquired body dependency %v %v", read, e)
			}
			read, e = h.Sessions.ReadCurrentConfirmation(unavailable, caller, ref.Name)
			if parameters != nil && (e == nil || read != nil) {
				t.Fatalf("unavailable current display %v %v", read, e)
			}
			var output bytes.Buffer
			cli := interaction.CLI{Caller: caller, Domain: "d", Confirmations: h.Sessions}
			e = cli.Run(unavailable, []string{"confirmation", ref.Name.LocalId}, &output)
			if parameters != nil && (e == nil || output.Len() != 0) {
				t.Fatalf("CLI bypassed body read: %s %v", output.String(), e)
			}
			if parameters == nil && (e != nil || output.Len() == 0) {
				t.Fatalf("ANY CLI unavailable %v", e)
			}
			// 来源交接只使用原结构引用，不需要读取已经不可用的参数正文。
			if e = h.Trace.Collect(unavailable, caller); e != nil {
				t.Fatal(e)
			}
			if e = h.Trace.Index(unavailable, caller); e != nil {
				t.Fatal(e)
			}
			indexed, e := h.Trace.QueryEvent(unavailable, caller, originalSource.Command.Event.Ref)
			if e != nil || !proto.Equal(indexed, originalSource.Command.Event) {
				t.Fatalf("unavailable body changed original event %v %v", indexed, e)
			}
			actor := &v1.Caller{UserId: caller.UserId, IssuerId: originalSource.Command.Header.Identity.IssuerId}
			traceReceipt, e := h.Trace.QueryReceipt(unavailable, actor, originalSource.Command.Header.Identity)
			if e != nil {
				t.Fatal(e)
			}
			acknowledged := confirmationSource(t, h, unavailable, caller, original)
			if acknowledged.Receipt == nil || !proto.Equal(acknowledged.Receipt, traceReceipt.GetReceipt()) {
				t.Fatal("original source ACK missing")
			}
			view, e := h.Trace.Query(unavailable, caller, &v1.TraceQuery{TaskId: indexed.TaskId})
			if e != nil || !view.Complete {
				t.Fatalf("confirmation source coverage %v %v", view, e)
			}
			present := false
			for _, event := range view.Events {
				present = present || proto.Equal(event, originalSource.Command.Event)
			}
			if !present {
				t.Fatal("confirmation missing from task trace")
			}
			if kind == "ordinary" || kind == "model" || kind == "closure" {
				response, e := h.Sessions.RespondConfirmation(unavailable, caller, &v1.RespondConfirmationCommand{Header: admissionHeader("unavailable-approval"), ConfirmationRef: original.Ref, BindingDigest: original.BindingDigest, Decision: "APPROVE"})
				if e == nil && response.GetDecision() == v1.Decision_DECISION_ACCEPTED {
					t.Fatal("unavailable action parameters admitted approval")
				}
			}
			approved, e := h.Sessions.RespondConfirmation(ctx, caller, &v1.RespondConfirmationCommand{Header: admissionHeader("available-approval"), ConfirmationRef: original.Ref, BindingDigest: original.BindingDigest, Decision: "APPROVE"})
			requireAccepted(t, approved, e)
			withdrawn, e := h.Sessions.WithdrawConfirmation(ctx, caller, &v1.WithdrawConfirmationCommand{Header: admissionHeader("withdraw-displayed-confirmation"), ConfirmationRef: approved.ResultRef})
			requireAccepted(t, withdrawn, e)
			for _, changed := range []*v1.Ref{approved.ResultRef, withdrawn.ResultRef} {
				historical, e := h.Sessions.QueryConfirmation(ctx, caller, changed)
				if e != nil {
					t.Fatal(e)
				}
				confirmationSource(t, h, unavailable, caller, historical)
			}
			historicalDisplay, e := h.Sessions.ReadConfirmation(ctx, caller, original.Ref)
			if e != nil || !proto.Equal(historicalDisplay, displayed) {
				t.Fatalf("withdrawal erased historical display %v %v", historicalDisplay, e)
			}
			if e = h.Close(); e != nil {
				t.Fatal(e)
			}
			h, e = assembly.Open(path, "u", "d")
			if e != nil {
				t.Fatal(e)
			}
			again, e := h.Sessions.QueryConfirmation(ctx, caller, ref)
			if e != nil || !proto.Equal(again, original) {
				t.Fatalf("display persisted body or changed history %v %v", again, e)
			}
			recovered, e := h.Sessions.ReadConfirmation(ctx, caller, ref)
			if e != nil || !proto.Equal(recovered, displayed) {
				t.Fatalf("restart display changed %v %v", recovered, e)
			}
			restoredSource := confirmationSource(t, h, unavailable, caller, original)
			if !proto.Equal(originalSource.Command, restoredSource.Command) {
				t.Fatal("restart/current withdrawal relabeled original pending source")
			}
			assertCalls()
		})
	}
}

func confirmationSource(t *testing.T, h *assembly.Harness, ctx context.Context, caller *v1.Caller, confirmation *v1.Confirmation) *v1.TraceSourceRecord {
	t.Helper()
	sources, e := h.Trace.QuerySources(ctx, caller)
	if e != nil {
		t.Fatal(e)
	}
	var result *v1.TraceSourceRecord
	for _, source := range sources {
		event := source.Command.Event
		if event.EventType != "CONFIRMATION_"+confirmation.State || !proto.Equal(event.SourceRecordRef, confirmation.Ref) {
			continue
		}
		if result != nil {
			t.Fatal("duplicate confirmation source")
		}
		result = source
	}
	if result == nil {
		t.Fatalf("missing original %s confirmation source", confirmation.State)
	}
	var expected []*v1.Ref
	if matter := confirmation.GetOperationAdmission(); matter != nil {
		expected = append([]*v1.Ref{matter.ProposalRef, matter.GrantRef, matter.Capability.Ref, matter.ParametersRef}, matter.ContentRefs...)
		if !proto.Equal(result.Command.Event.BodyRef, matter.ParametersRef) {
			t.Fatal("lost original operation parameter body ref")
		}
	}
	if matter := confirmation.GetGrantIssuance(); matter != nil {
		expected = append(expected, matter.IssuanceRef)
		for _, permission := range matter.Grant.Permissions {
			if permission.ParameterMode == "EXACT" {
				expected = append(expected, permission.ParametersRef)
			}
		}
	}
	for _, consumed := range []*v1.Ref{confirmation.GetConsumedAdmissionRef(), confirmation.GetConsumedGrantIssuanceRef(), confirmation.GetConsumedVerificationRef()} {
		if consumed != nil {
			expected = append(expected, consumed)
		}
	}
	for _, ref := range expected {
		found := false
		for _, recorded := range result.Command.Event.RelatedRefs {
			found = found || proto.Equal(recorded, ref)
		}
		if !found {
			t.Fatalf("confirmation source lost original ref %v", ref)
		}
	}
	encoded, e := protojson.Marshal(result)
	if e != nil || bytes.Contains(encoded, []byte("description")) || bytes.Contains(encoded, []byte("Parameters")) {
		t.Fatalf("confirmation rendering entered source: %v %s", e, encoded)
	}
	return result
}

func prepareGovernedConfirmation(t *testing.T, h *assembly.Harness, kind string) (*v1.Ref, *v1.Ref, func()) {
	t.Helper()
	ctx := context.Background()
	caller := &v1.Caller{UserId: "u", IssuerId: "host"}
	if kind == "model" {
		provider := simulator.NewModelProvider()
		server := httptest.NewServer(provider)
		t.Cleanup(server.Close)
		run := prepareModelFault(t, h, server.URL)
		call, e := h.Tasks.PrepareModelCall(ctx, caller, run.Preparation)
		if e != nil {
			t.Fatal(e)
		}
		request, e := h.Tasks.QueryProposalRequest(ctx, caller, run.Preparation.RequestRef)
		if e != nil {
			t.Fatal(e)
		}
		requested, e := h.Tasks.RequestAdmissionConfirmation(ctx, caller, &v1.RequestAdmissionConfirmationCommand{Header: admissionHeader("governed-model-confirmation"), TaskId: request.TaskId, ProposalRef: call.Ref, GrantRef: run.GrantRef})
		requireAccepted(t, requested, e)
		return requested.ResultRef, call.InputRef, func() {
			if provider.Calls() != 0 {
				t.Fatal("display called model")
			}
		}
	}
	if kind == "closure" {
		target := simulator.New("queryable")
		target.SetBehavior("drop-after-apply")
		server := httptest.NewServer(target)
		t.Cleanup(server.Close)
		query := prepareQueryFault(t, h, server.URL)
		grant, e := h.Grants.QueryGrant(ctx, caller, query.GrantRef)
		if e != nil {
			t.Fatal(e)
		}
		grant.Ref = nil
		grant.Issuer = nil
		grant.Status = ""
		grant.SemanticVersion = 0
		grant.ConfirmationRequired = true
		configured, e := h.Grants.Configure(ctx, caller, &v1.ConfigureGrantCommand{Header: admissionHeader("governed-closure-grant"), Grant: grant})
		requireAccepted(t, configured, e)
		query.GrantRef = configured.ResultRef
		requested, e := h.Ledger.RequestReconciliation(ctx, caller, query)
		requireAccepted(t, requested, e)
		if e = h.Ledger.ProcessReconciliations(ctx, caller); e != nil {
			t.Fatal(e)
		}
		plan, e := h.Ledger.QueryReconciliation(ctx, caller, query.OperationId)
		if e != nil || plan.PauseReason != "CONFIRMATION_REQUIRED" {
			t.Fatalf("query not awaiting confirmation %v %v", plan, e)
		}
		work, e := h.Ledger.QueryReconciliationQuery(ctx, caller, plan.ActiveQueryRef)
		if e != nil {
			t.Fatal(e)
		}
		confirmation, e := h.Tasks.RequestClosureConfirmation(ctx, caller, &v1.RequestClosureConfirmationCommand{Header: admissionHeader("governed-closure-confirmation"), WorkRef: work.Work.Ref})
		requireAccepted(t, confirmation, e)
		return confirmation.ResultRef, work.Work.ParametersRef, func() {
			requests, effects := target.Snapshot()
			if len(requests) != 1 || len(effects) != 1 {
				t.Fatal("display changed target calls")
			}
		}
	}
	target := simulator.New("idempotent")
	server := httptest.NewServer(target)
	t.Cleanup(server.Close)
	noCalls := func() {
		requests, effects := target.Snapshot()
		if len(requests) != 0 || len(effects) != 0 {
			t.Fatal("display called target")
		}
	}
	action := prepareAdmission(t, h, server.URL)
	proposal, e := h.Tasks.QueryProposal(ctx, caller, action.ProposalRef)
	if e != nil {
		t.Fatal(e)
	}
	if kind == "ordinary" {
		requested, e := h.Tasks.RequestAdmissionConfirmation(ctx, caller, &v1.RequestAdmissionConfirmationCommand{Header: admissionHeader("governed-confirmation"), TaskId: action.TaskId, ProposalRef: action.ProposalRef, GrantRef: action.GrantRef})
		requireAccepted(t, requested, e)
		return requested.ResultRef, proposal.Step.ParametersRef, noCalls
	}
	grant, e := h.Grants.QueryGrant(ctx, caller, action.GrantRef)
	if e != nil {
		t.Fatal(e)
	}
	grant.Ref = nil
	grant.Issuer = nil
	grant.Status = ""
	grant.UsePoolId = ""
	var parameters *v1.Ref
	if kind == "exact-grant" {
		parameters = proposal.Step.ParametersRef
		grant.Permissions[0].ParameterMode = "EXACT"
		grant.Permissions[0].ParametersRef = parameters
	} else {
		grant.Permissions[0].ParameterMode = "ANY"
		grant.Permissions[0].ParametersRef = nil
	}
	requested, e := h.Grants.RequestGrantConfirmation(ctx, caller, &v1.RequestGrantConfirmationCommand{Header: admissionHeader("governed-grant-confirmation"), Grant: grant})
	requireAccepted(t, requested, e)
	return requested.ResultRef, parameters, noCalls
}
