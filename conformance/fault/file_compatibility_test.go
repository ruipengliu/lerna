//go:build fault && darwin

package fault_test

import (
	"encoding/hex"
	"testing"

	"github.com/ruipengliu/lerna/cmd/assembly"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

// 规则：G1、G3、G11、R7、V4
func TestStartupRefusesOriginalUnsupportedFileVersionsBeforeAnyOwnerRecovery(t *testing.T) {
	for _, field := range []string{"adapter", "protocol", "declaration", "verification"} {
		t.Run(field, func(t *testing.T) {
			n := newNativeScenario(t)
			a, _ := n.prepare(t, "original-version", "CREATE", "", []byte("unpublished original"))
			original, err := n.h.Ledger.QueryOperation(n.ctx, n.caller, a.OperationId)
			if err != nil {
				t.Fatal(err)
			}
			changed := proto.Clone(original).(*v1.Operation)
			switch field {
			case "adapter":
				changed.AdapterRef.Revision = 2
				changed.CapabilitySnapshot.AdapterRef.Revision = 2
			case "protocol":
				changed.Execution.Attempt.Capabilities.ProtocolVersion = "lerna-managed-file-v2"
			case "declaration":
				changed.Execution.Attempt.Capabilities.DeclarationVersion = "2"
			case "verification":
				changed.Execution.Attempt.Capabilities.VerificationBasis = "managed-file-v2"
			}
			wire, err := proto.Marshal(changed)
			if err != nil {
				t.Fatal(err)
			}
			if err = n.h.StorageFaultSQL("UPDATE operations SET record=X'" + hex.EncodeToString(wire) + "' WHERE user_id='u' AND domain_id='d/ledger' AND id='" + a.OperationId.LocalId + "'"); err != nil {
				t.Fatal(err)
			}
			goal := &v1.SubmitGoalCommand{Identity: admissionHeader("other-owner-original").Identity, ContractVersion: 1, SchemaId: "lerna.v1.SubmitGoal", FingerprintVersion: 1, Goal: "keep original pending responsibility"}
			receipt, err := n.h.Sessions.SubmitGoal(n.ctx, n.caller, goal)
			if err != nil {
				t.Fatal(err)
			}
			job, err := n.h.Durable.QueryJob(n.ctx, n.caller, receipt.JobRef.Name)
			if err != nil {
				t.Fatal(err)
			}
			namespace := snapshotBackupFileNamespace(t, n.root)
			candidate, openErr := assembly.OpenWithFiles(n.path, "u", "d", map[string]string{"documents": n.root})
			if candidate != nil {
				if err = candidate.Close(); err != nil {
					t.Fatal(err)
				}
			}
			if openErr == nil {
				t.Error("unsupported original FILE implementation accepted")
			}
			got, err := n.h.Ledger.QueryOperation(n.ctx, n.caller, a.OperationId)
			if err != nil || !proto.Equal(got, changed) {
				t.Fatalf("original FILE responsibility changed %v %v", got, err)
			}
			gotReceipt, err := n.h.Durable.QueryReceipt(n.ctx, n.caller, goal.Identity)
			if err != nil || !proto.Equal(gotReceipt.Receipt, receipt) {
				t.Fatalf("startup advanced other owner %v %v", gotReceipt, err)
			}
			gotJob, err := n.h.Durable.QueryJob(n.ctx, n.caller, receipt.JobRef.Name)
			if err != nil || !proto.Equal(gotJob, job) || gotJob.State != "READY" || gotJob.ClaimEpoch != 0 {
				t.Fatalf("startup claimed original job %v %v", gotJob, err)
			}
			assertBackupFileNamespace(t, n.root, namespace)
		})
	}
}
