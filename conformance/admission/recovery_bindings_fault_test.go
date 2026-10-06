//go:build fault

package admission_test

import (
	"encoding/hex"
	"testing"

	"github.com/ruipengliu/lerna/cmd/assembly"
	"github.com/ruipengliu/lerna/conformance/simulator"
	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

// 规则：G1、G3、G4、G11、开始-2、V4
func TestStartupChecksOriginalSimulatorAndModelDeclarationAndBindings(t *testing.T) {
	cases := []struct {
		name   string
		model  bool
		mutate func(*v1.Operation)
	}{
		{"effect", false, func(o *v1.Operation) { o.Execution.Attempt.Capabilities.Effect = "READ" }},
		{"idempotency", false, func(o *v1.Operation) { o.Execution.Attempt.Capabilities.Idempotent = false }},
		{"queryability", false, func(o *v1.Operation) { o.Execution.Attempt.Capabilities.Queryable = true }},
		{"concurrency", false, func(o *v1.Operation) { o.Execution.Attempt.Capabilities.ConcurrencyGuarantee = "GLOBAL" }},
		{"retention", false, func(o *v1.Operation) { o.Execution.Attempt.Capabilities.RetentionMs = 1 }},
		{"account", false, func(o *v1.Operation) { o.Execution.Attempt.Capabilities.AccountScope = "another-account" }},
		{"parameter-binding", false, func(o *v1.Operation) { o.Execution.Attempt.Capabilities.ParameterBinding = "NONE" }},
		{"target", false, func(o *v1.Operation) { o.Execution.CallDescriptor.Target += "/another" }},
		{"parameters", false, func(o *v1.Operation) { o.Execution.CallDescriptor.ParametersRef.Revision++ }},
		{"capability", false, func(o *v1.Operation) { o.Execution.CallDescriptor.CapabilityRef.Revision++ }},
		{"key", false, func(o *v1.Operation) { o.Execution.CallDescriptor.ExternalKey += "-another" }},
		{"attempt-operation", false, func(o *v1.Operation) { o.Execution.Attempt.OperationId.LocalId += "-another" }},
		{"send-attempt", false, func(o *v1.Operation) { o.Execution.Send.AttemptId.LocalId += "-another" }},
		{"model-idempotency", true, func(o *v1.Operation) { o.Execution.Attempt.Capabilities.Idempotent = true }},
		{"model-body", true, func(o *v1.Operation) { o.Execution.CallDescriptor.BodyDigest = "another-body" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			target := simulator.NewBillingTarget(25)
			f := newFixtureWithTarget(t, 100, 80, false, target)
			var op *v1.Operation
			provider := simulator.NewModelProvider()
			if tc.model {
				f = modelFixtureTarget(t, provider)
				run := modelRunCommand(t, f, 30000)
				if _, e := f.h.Tasks.RunModelCall(f.ctx, f.caller, run); e != nil {
					t.Fatal(e)
				}
				call, e := f.h.Tasks.QueryModelCall(f.ctx, f.caller, run.Preparation.RequestRef, 0)
				if e != nil {
					t.Fatal(e)
				}
				admission, e := f.h.Tasks.QueryAdmission(f.ctx, f.caller, call.AdmissionRef)
				if e != nil {
					t.Fatal(e)
				}
				op, e = f.h.Ledger.QueryOperation(f.ctx, f.caller, admission.OperationId)
				if e != nil {
					t.Fatal(e)
				}
			} else {
				a, _ := prepareStart(t, f)
				var e error
				op, e = f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
				if e != nil {
					t.Fatal(e)
				}
			}
			changed := proto.Clone(op).(*v1.Operation)
			tc.mutate(changed)
			changed.Execution.CallDescriptor.Digest = ""
			changed.Execution.CallDescriptor.Digest = command.SemanticFingerprint("call-descriptor-v1", changed.Execution.CallDescriptor)
			// 故障仅替换实际历史的声明或绑定；还原这些元数据后必须逐字段等于原记录。
			restored := proto.Clone(changed).(*v1.Operation)
			restored.Execution.Attempt.Capabilities = proto.Clone(op.Execution.Attempt.Capabilities).(*v1.ExecutionCapabilities)
			restored.Execution.Attempt.OperationId = proto.Clone(op.Execution.Attempt.OperationId).(*v1.GlobalName)
			restored.Execution.Send.AttemptId = proto.Clone(op.Execution.Send.AttemptId).(*v1.GlobalName)
			restored.Execution.CallDescriptor = proto.Clone(op.Execution.CallDescriptor).(*v1.CallDescriptor)
			if !proto.Equal(restored, op) {
				t.Fatal("fault changed business facts")
			}
			wire, e := proto.Marshal(changed)
			if e != nil {
				t.Fatal(e)
			}
			if e = f.h.StorageFaultSQL("UPDATE operations SET record=X'" + hex.EncodeToString(wire) + "' WHERE user_id='u' AND domain_id='d/ledger' AND id='" + op.Ref.Name.LocalId + "'"); e != nil {
				t.Fatal(e)
			}
			goal := &v1.SubmitGoalCommand{Identity: header("binding-other-owner").Identity, ContractVersion: 1, SchemaId: "lerna.v1.SubmitGoal", FingerprintVersion: 1, Goal: "preserve original pending owner"}
			receipt, e := f.h.Sessions.SubmitGoal(f.ctx, f.caller, goal)
			if e != nil {
				t.Fatal(e)
			}
			job, e := f.h.Durable.QueryJob(f.ctx, f.caller, receipt.JobRef.Name)
			if e != nil {
				t.Fatal(e)
			}
			budget, e := f.h.Budget.QueryBudget(f.ctx, f.caller, nil)
			if e != nil {
				t.Fatal(e)
			}
			candidate, openErr := assembly.Open(f.path, "u", "d")
			if candidate != nil {
				if e = candidate.Close(); e != nil {
					t.Fatal(e)
				}
			}
			if openErr == nil || openErr.Error() != "PREPARATION_UNRECOVERABLE" {
				t.Errorf("unsupported original declaration/binding must fail before recovery: %v", openErr)
			}
			after, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, op.Ref.Name)
			if e != nil || !proto.Equal(changed, after) {
				t.Fatalf("original record changed: %v %v", after, e)
			}
			q, e := f.h.Durable.QueryReceipt(f.ctx, f.caller, goal.Identity)
			if e != nil || !proto.Equal(q.Receipt, receipt) {
				t.Errorf("other owner advanced: %v %v", q, e)
			}
			j, e := f.h.Durable.QueryJob(f.ctx, f.caller, job.Ref.Name)
			if e != nil || !proto.Equal(j, job) || j.State != "READY" || j.ClaimEpoch != 0 {
				t.Errorf("other job advanced: %v %v", j, e)
			}
			b, e := f.h.Budget.QueryBudget(f.ctx, f.caller, nil)
			if e != nil || !proto.Equal(b, budget) {
				t.Errorf("budget changed: %v %v", b, e)
			}
			calls, effects := target.Target.Snapshot()
			if len(calls) != 0 || len(effects) != 0 || len(target.Bills()) != 0 || tc.model && (provider.Calls() != 1 || len(provider.Bills()) != 1) {
				t.Fatal("recovery changed actual calls/effects/fees")
			}
		})
	}
}
