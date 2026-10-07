package admission_test

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"github.com/ruipengliu/lerna/core/ledger"
	"github.com/ruipengliu/lerna/infra/rules"
	"google.golang.org/protobuf/proto"
)

// 规则：G1、G3、G11、R6
func TestManagedFileHistoryNeedsConfiguredTrustedRulesBeforeRecovery(t *testing.T) {
	f := newFixture(t, 100, 80, false)
	configureFile(t, f, "CREATE", "managed://documents/report")
	a, _ := prepareStart(t, f)
	before, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
	if e != nil {
		t.Fatal(e)
	}
	for _, missing := range []ledger.EvidenceRules{nil, (*rules.File)(nil)} {
		f.h.Ledger.WithEvidenceRules(missing)
		if e = f.h.Ledger.CheckStartupCompatibility(f.ctx); e == nil || e.Error() != "missing required dependency: ledger.rules" {
			t.Fatalf("missing original FILE rules: %v", e)
		}
		after, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
		if e != nil || !proto.Equal(before, after) || f.calls.Load() != 0 {
			t.Fatal("rule configuration failure advanced original file responsibility")
		}
	}
	f.h.Ledger.WithEvidenceRules(rules.Fixed{})
	if e = f.h.Ledger.CheckStartupCompatibility(f.ctx); e != nil {
		t.Fatalf("supported original file rules: %v", e)
	}
}

func configureFile(t *testing.T, f *fixture, action, resource string) {
	t.Helper()
	ceiling := int64(0)
	useRight := "INVOKE"
	if action == "READ" || action == "QUERY" {
		useRight = "READ"
	}
	var permissions []*v1.PermissionClause
	for _, right := range []string{useRight, "READ", "SAVE"} {
		if right == "READ" && useRight == "READ" && len(permissions) > 0 {
			continue
		}
		permissions = append(permissions, &v1.PermissionClause{Action: action, Resource: resource, UseRight: right, ProcessingPurpose: "CURRENT_TASK", ExecutorEndpointId: "local-file"})
	}
	r, err := f.h.Tasks.ConfigureCapability(f.ctx, f.caller, &v1.ConfigureCapabilityCommand{Header: header("file-cap" + f.suffix), Capability: &v1.Capability{Action: action, Resource: resource, ExecutorEndpointId: "local-file", AdapterRef: &v1.Ref{Name: &v1.GlobalName{UserId: "u", AuthorityDomainId: "adapter", ObjectKind: "adapter", LocalId: "managed-file"}, Revision: 1, SchemaId: "lerna.v1.Adapter"}, UseRight: useRight, ProcessingPurpose: "CURRENT_TASK", Unit: "USD_MICRO", FeeCeiling: &ceiling, Nonbillable: true, RateBasisRef: f.parameters, MaxSends: 1}})
	accepted(t, r, err)
	f.capability = r.ResultRef
	r, err = f.h.Grants.Configure(f.ctx, f.caller, &v1.ConfigureGrantCommand{Header: header("file-grant" + f.suffix), Grant: &v1.Grant{Subject: f.task.Name, Permissions: permissions, ValidFromUnixMs: time.Now().Add(-time.Minute).UnixMilli(), ValidUntilUnixMs: time.Now().Add(time.Hour).UnixMilli(), UseMode: "CONTINUOUS", UsePoolId: "file-root"}})
	accepted(t, r, err)
	f.grant = r.ResultRef
}

// 规则：G1、G4、G5、G11
func TestManagedFilePreparationPersistsBoundedNonIdempotentDescriptor(t *testing.T) {
	f := newFixture(t, 100, 80, false)
	configureFile(t, f, "CREATE", "managed://documents/report")
	a, c := prepareStart(t, f)
	x, err := f.h.Ledger.QueryExecution(f.ctx, f.caller, a.OperationId)
	if err != nil {
		t.Fatal(err)
	}
	if c.CallDescriptor.Protocol != "FILE" || c.CallDescriptor.Method != "CREATE" || c.CallDescriptor.Target != "managed://documents/report" || x.Attempt.Capabilities.Idempotent || !x.Attempt.Capabilities.Queryable || f.calls.Load() != 0 {
		t.Fatalf("descriptor or guarantee: %v", x)
	}
}

// 规则：G2、G3、G4、G5、G9、G10
func TestManagedFileCreatePublishesBytesAndRetainsGovernedEvidence(t *testing.T) {
	root, rootErr := filepath.EvalSymlinks(t.TempDir())
	if rootErr != nil {
		t.Fatal(rootErr)
	}
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"objects", "commits", "locks"} {
		if err := os.Mkdir(filepath.Join(root, name), 0700); err != nil {
			t.Fatal(err)
		}
	}
	f := newFixtureWithTargetAndFiles(t, 100, 80, false, nil, map[string]string{"documents": root})
	fileParameters(t, f, "", []byte("create a record"))
	configureFile(t, f, "CREATE", "managed://documents/report")
	task, err := f.h.Tasks.QueryTask(f.ctx, f.caller, f.task.Name)
	if err != nil {
		t.Fatal(err)
	}
	r, err := f.h.Tasks.AcceptRequirements(f.ctx, f.caller, &v1.AcceptRequirementsCommand{Header: header("file-requirements"), TaskRef: &v1.Ref{Name: task.TaskId, Revision: task.Revision, SchemaId: "lerna.v1.Task"}, InputVersion: task.InputVersion, Source: "TRUSTED_TEMPLATE", Conditions: []*v1.Requirement{{ConditionId: "created", DescriptionRef: f.parameters, Necessary: true, VerificationRule: "TARGET_RECORD", RuleVersion: 1, TargetRecord: &v1.TargetRecordAssertion{CapabilityRef: f.capability, ParametersRef: f.parameters}}}})
	accepted(t, r, err)
	a, c := prepareStart(t, f)
	r, err = f.h.Egress.Invoke(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, c)
	accepted(t, r, err)
	data, err := os.ReadFile(filepath.Join(root, "commits", "report"))
	if err != nil {
		x, _ := f.h.Ledger.QueryExecution(f.ctx, f.caller, a.OperationId)
		o, _ := f.h.Ledger.QueryObservation(f.ctx, f.caller, x.Send.ObservationRef)
		t.Fatalf("%v observation=%v", err, o)
	}
	var pointer struct {
		ObjectName string `json:"object_name"`
		Digest     string `json:"digest"`
		Version    string `json:"version"`
	}
	if err = json.Unmarshal(data, &pointer); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(root, "objects", pointer.ObjectName))
	if err != nil || string(body) != "create a record" || pointer.Version == "" {
		t.Fatalf("published body %q: %v", body, err)
	}
	op, err := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
	if err != nil || op.Effect.Outcome != "APPLIED" || op.Lifecycle != "SETTLED" {
		t.Fatalf("effect %v: %v", op, err)
	}
	assertFixedExecutionDeclaration(t, op.Execution.Attempt.Capabilities, "lerna-managed-file-v1", "managed-file-v1")
	assertOriginalProofRule(t, f, op, "managed-file-v1")
	admission, err := f.h.Tasks.QueryAdmission(f.ctx, f.caller, op.AdmissionRef)
	if err != nil || !op.CapabilitySnapshot.Nonbillable || op.CapabilitySnapshot.FeeCeiling == nil || *op.CapabilitySnapshot.FeeCeiling != 0 || admission.BudgetBasis.Ceiling != 0 {
		t.Fatalf("file declaration lost original nonbillable basis: %v %v", admission, err)
	}
	source, err := f.h.Budget.QueryBillingSource(f.ctx, f.caller, op.Execution.Send.Ref)
	if err != nil || source.Status != "SETTLED" {
		t.Fatalf("file billing %v: %v", source, err)
	}
	proposal := completeProposal(t, f, []*v1.CompletionEvidence{{ConditionId: "created", OperationId: a.OperationId}})
	r, err = f.h.Tasks.BeginCompletion(f.ctx, f.caller, &v1.BeginCompletionCommand{Header: header("file-complete"), TaskId: f.task.Name, ProposalRef: proposal})
	accepted(t, r, err)
	if err = f.h.Tasks.ProcessCompletions(f.ctx, f.caller); err != nil {
		t.Fatal(err)
	}
	result, err := f.h.Tasks.QueryResult(f.ctx, f.caller, f.task.Name)
	if err != nil || result.GetOutcome() != "SUCCEEDED" {
		t.Fatalf("durable file completion: %v %v", result, err)
	}

}

func fileParameters(t *testing.T, f *fixture, expected string, data []byte) {
	t.Helper()
	body, err := json.Marshal(map[string]string{"expected_version": expected, "data": base64.StdEncoding.EncodeToString(data)})
	if err != nil {
		t.Fatal(err)
	}
	h := header("file-parameters" + f.suffix)
	h.Identity.TargetDomainId = "d/content"
	r, err := f.h.Content.Register(f.ctx, f.caller, &v1.RegisterContentCommand{Header: h, Body: body, MediaType: "application/json", SourceDescriptor: &v1.ContentSourceDescriptor{Kind: "HOST_IMPORT", Locator: "file-parameters", AcquisitionMethod: "HOST_IMPORT", ProviderVersion: "test-v1"}})
	accepted(t, r, err)
	if err = f.h.Content.ProcessRegistrations(f.ctx, f.caller); err != nil {
		t.Fatal(err)
	}
	f.parameters = r.ResultRef
}

// 规则：G4、G5、G8
func TestManagedFileWriteRequiresExplicitReadAndSavePermissions(t *testing.T) {
	f := newFixture(t, 100, 80, false)
	configureFile(t, f, "CREATE", "managed://documents/report")
	for _, missing := range []string{"READ", "SAVE"} {
		g, err := f.h.Grants.QueryGrant(f.ctx, f.caller, f.grant)
		if err != nil {
			t.Fatal(err)
		}
		var clauses []*v1.PermissionClause
		for _, right := range []string{"INVOKE", "READ", "SAVE"} {
			if right != missing {
				clauses = append(clauses, &v1.PermissionClause{Action: "CREATE", Resource: "managed://documents/report", UseRight: right, ProcessingPurpose: "CURRENT_TASK", ExecutorEndpointId: "local-file"})
			}
		}
		g.Ref = nil
		g.Issuer = nil
		g.Status = ""
		g.Permissions = clauses
		g.UsePoolId = "missing-" + missing
		r, err := f.h.Grants.Configure(f.ctx, f.caller, &v1.ConfigureGrantCommand{Header: header("missing-grant-" + missing), Grant: g})
		accepted(t, r, err)
		f.suffix = "-missing-" + missing
		p := f.propose(t, nil)
		r, err = f.h.Tasks.Admit(f.ctx, f.caller, &v1.AdmitCommand{Header: header("missing-admit-" + missing), TaskId: f.task.Name, ProposalRef: p, GrantRef: r.ResultRef})
		if err != nil || r.GetError().GetCode() != "GRANT_SCOPE_MISMATCH" {
			t.Fatalf("missing %s accepted: %v %v", missing, r, err)
		}
	}
}

// 规则：G1、G5、G9
func TestManagedFileCleanupDoesNotClaimAtomicOrQueryableEffect(t *testing.T) {
	f := newFixture(t, 100, 80, false)
	configureFile(t, f, "CLEANUP", "managed://documents/report")
	a, _ := prepareStart(t, f)
	x, e := f.h.Ledger.QueryExecution(f.ctx, f.caller, a.OperationId)
	if e != nil || x.Attempt.Capabilities.Queryable || x.Attempt.Capabilities.Idempotent || x.Attempt.Capabilities.Effect != "PARTIAL_WRITE" {
		t.Fatalf("cleanup overclaimed guarantee: %v %v", x, e)
	}
}

// 规则：G1、G5、G8
func TestManagedFilePreparationRejectsReservedNamespace(t *testing.T) {
	for _, resource := range []string{"managed://documents/.pending", "managed://.internal/report"} {
		t.Run(resource, func(t *testing.T) {
			f := newFixture(t, 100, 80, false)
			configureFile(t, f, "CREATE", resource)
			p := f.propose(t, nil)
			r, e := f.h.Tasks.Admit(f.ctx, f.caller, &v1.AdmitCommand{Header: header("reserved-admit"), TaskId: f.task.Name, ProposalRef: p, GrantRef: f.grant})
			accepted(t, r, e)
			a, e := f.h.Tasks.QueryAdmission(f.ctx, f.caller, r.ResultRef)
			if e != nil {
				t.Fatal(e)
			}
			if e = f.h.Tasks.ProcessHandoffs(f.ctx, f.caller); e != nil {
				t.Fatal(e)
			}
			claim, e := f.h.LedgerWork.ExecuteJob(f.ctx, f.caller, &v1.JobCommand{Identity: ledgerHeader("reserved-claim").Identity, ContractVersion: 1, Action: "CLAIM", AllowedTypes: []string{"EXECUTE_OPERATION"}, Limit: 1, LeaseMs: 30000, ProcessInstance: "worker"})
			accepted(t, claim, e)
			if len(claim.Jobs) != 1 {
				t.Fatal("missing execution claim")
			}
			r, e = f.h.Ledger.Prepare(f.ctx, f.caller, &v1.PrepareExecutionCommand{Header: ledgerHeader("reserved-prepare"), OperationId: a.OperationId, ProcessInstance: "worker", Claim: claim.Jobs[0]})
			if e != nil || r.GetError().GetCode() != "TARGET_SCOPE_MISMATCH" || f.calls.Load() != 0 {
				t.Fatalf("reserved namespace accepted: %v %v", r, e)
			}
		})
	}
}
