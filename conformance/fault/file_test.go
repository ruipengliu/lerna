//go:build fault && darwin

package fault_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"syscall"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/cmd/assembly"
	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"github.com/ruipengliu/lerna/core/egress"
	"github.com/ruipengliu/lerna/defaults/scripted"
	"github.com/ruipengliu/lerna/infra/egressio"
	"github.com/ruipengliu/lerna/infra/sqlite"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

type nativeScenario struct {
	h                    *assembly.Harness
	ctx                  context.Context
	caller               *v1.Caller
	task, session, input *v1.Ref
	root, path           string
	scoped               bool
}

func nativeRootDirectory(t *testing.T) string {
	t.Helper()
	root, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	if e = os.Chmod(root, 0700); e != nil {
		t.Fatal(e)
	}
	for _, name := range []string{"objects", "commits", "locks"} {
		if e = os.Mkdir(filepath.Join(root, name), 0700); e != nil {
			t.Fatal(e)
		}
	}
	// 部署基线先真实同步子目录、根及父目录，之后模型才可视为既存布局。
	for _, path := range []string{filepath.Join(root, "objects"), filepath.Join(root, "commits"), filepath.Join(root, "locks"), root, filepath.Dir(root)} {
		f, e := os.Open(path)
		if e != nil {
			t.Fatal(e)
		}
		if e = syscall.Fsync(int(f.Fd())); e != nil {
			f.Close()
			t.Fatal(e)
		}
		_, _, errno := syscall.Syscall(syscall.SYS_FCNTL, f.Fd(), syscall.F_FULLFSYNC, 0)
		f.Close()
		if errno != 0 {
			t.Fatal(errno)
		}
	}
	return root
}
func newNativeScenario(t *testing.T) *nativeScenario {
	t.Helper()
	n := &nativeScenario{root: nativeRootDirectory(t), path: filepath.Join(t.TempDir(), "state.db"), ctx: context.Background(), caller: &v1.Caller{UserId: "u", IssuerId: "host"}}
	var e error
	n.h, e = assembly.OpenWithFiles(n.path, "u", "d", map[string]string{"documents": n.root})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = n.h.Close() })
	r, e := n.h.Sessions.SubmitGoal(n.ctx, n.caller, &v1.SubmitGoalCommand{Identity: admissionHeader("goal").Identity, ContractVersion: 1, SchemaId: "lerna.v1.SubmitGoal", FingerprintVersion: 1, Goal: "manage one file"})
	if e != nil {
		t.Fatal(e)
	}
	if e = n.h.Sessions.ProcessPending(n.ctx, n.caller); e != nil {
		t.Fatal(e)
	}
	q, e := n.h.Durable.QueryReceipt(n.ctx, n.caller, r.Identity)
	if e != nil {
		t.Fatal(e)
	}
	n.task = q.Receipt.TaskRef
	n.session = q.Receipt.SessionRef
	n.input = q.Receipt.InputRef
	r, e = n.h.Tasks.AcceptRequirements(n.ctx, n.caller, &v1.AcceptRequirementsCommand{Header: admissionHeader("requirements"), TaskRef: n.task, InputVersion: 1, Source: "TRUSTED_TEMPLATE", Conditions: []*v1.Requirement{{ConditionId: "file", DescriptionRef: n.input, Necessary: true, VerificationRule: "TARGET_RECORD", RuleVersion: 1}}})
	requireAccepted(t, r, e)
	for _, c := range []*v1.ConfigureBudgetCommand{{Header: admissionHeader("user-budget"), Unit: "USD_MICRO", Limit: 100}, {Header: admissionHeader("task-budget"), TaskId: n.task.Name, Unit: "USD_MICRO", Limit: 100}} {
		r, e = n.h.Budget.Configure(n.ctx, n.caller, c)
		requireAccepted(t, r, e)
	}
	return n
}
func (n *nativeScenario) parameters(t *testing.T, id, expected string, data []byte, cleanup ...*v1.Ref) *v1.Ref {
	t.Helper()
	p := &v1.FileParameters{ExpectedVersion: expected, Data: data}
	if len(cleanup) > 0 {
		p.CleanupResourcesRef = cleanup[0]
	}
	body, e := (protojson.MarshalOptions{UseProtoNames: true}).Marshal(p)
	if e != nil {
		t.Fatal(e)
	}
	h := admissionHeader("parameters:" + id)
	h.Identity.TargetDomainId = "d/content"
	r, e := n.h.Content.Register(n.ctx, n.caller, &v1.RegisterContentCommand{Header: h, Body: body, MediaType: "application/json", SourceDescriptor: &v1.ContentSourceDescriptor{Kind: "HOST_IMPORT", Locator: "file:" + id, AcquisitionMethod: "HOST_IMPORT", ProviderVersion: "native-test-v1"}})
	requireAccepted(t, r, e)
	if e = n.h.Content.ProcessRegistrations(n.ctx, n.caller); e != nil {
		t.Fatal(e)
	}
	return r.ResultRef
}
func (n *nativeScenario) capGrant(t *testing.T, id, action string, parameters *v1.Ref) (*v1.Ref, *v1.Ref) {
	t.Helper()
	right := "INVOKE"
	if action == "READ" || action == "QUERY" {
		right = "READ"
	}
	zero := int64(0)
	r, e := n.h.Tasks.ConfigureCapability(n.ctx, n.caller, &v1.ConfigureCapabilityCommand{Header: admissionHeader("cap:" + id), Capability: &v1.Capability{Action: action, Resource: "managed://documents/report", UseRight: right, ProcessingPurpose: "CURRENT_TASK", ExecutorEndpointId: "local-file", AdapterRef: &v1.Ref{Name: &v1.GlobalName{UserId: "u", AuthorityDomainId: "adapter", ObjectKind: "adapter", LocalId: "managed-file"}, Revision: 1, SchemaId: "lerna.v1.Adapter"}, Unit: "USD_MICRO", FeeCeiling: &zero, Nonbillable: true, RateBasisRef: parameters, MaxSends: 1}})
	requireAccepted(t, r, e)
	cap := r.ResultRef
	permissions := []*v1.PermissionClause{{Action: action, Resource: "managed://documents/report", UseRight: "READ", ProcessingPurpose: "CURRENT_TASK", ExecutorEndpointId: "local-file"}, {Action: action, Resource: "managed://documents/report", UseRight: "SAVE", ProcessingPurpose: "CURRENT_TASK", ExecutorEndpointId: "local-file"}}
	if right == "INVOKE" {
		permissions = append(permissions, &v1.PermissionClause{Action: action, Resource: "managed://documents/report", UseRight: "INVOKE", ProcessingPurpose: "CURRENT_TASK", ExecutorEndpointId: "local-file"})
	}
	r, e = n.h.Grants.Configure(n.ctx, n.caller, &v1.ConfigureGrantCommand{Header: admissionHeader("grant:" + id), Grant: &v1.Grant{Subject: n.task.Name, Permissions: permissions, ValidFromUnixMs: 1, ValidUntilUnixMs: 4000000000000, UseMode: "CONTINUOUS", UsePoolId: "pool:" + id}})
	requireAccepted(t, r, e)
	return cap, r.ResultRef
}
func (n *nativeScenario) prepare(t *testing.T, id, action, expected string, data []byte, cleanup ...*v1.Ref) (*v1.Admission, *v1.StartExecutionCommand) {
	t.Helper()
	parameters := n.parameters(t, id, expected, data, cleanup...)
	cap, grant := n.capGrant(t, id, action, parameters)
	if n.scoped {
		task, e := n.h.Tasks.QueryTask(n.ctx, n.caller, n.task.Name)
		if e != nil {
			t.Fatal(e)
		}
		r, e := n.h.Tasks.AcceptRequirements(n.ctx, n.caller, &v1.AcceptRequirementsCommand{Header: admissionHeader("scoped:" + id), TaskRef: &v1.Ref{Name: task.TaskId, Revision: task.Revision, SchemaId: "lerna.v1.Task"}, InputVersion: task.InputVersion, Source: "TRUSTED_TEMPLATE", Conditions: []*v1.Requirement{{ConditionId: "file", DescriptionRef: n.input, Necessary: true, VerificationRule: "TARGET_RECORD", RuleVersion: 1, TargetRecord: &v1.TargetRecordAssertion{CapabilityRef: cap, ParametersRef: parameters}}}})
		requireAccepted(t, r, e)
	}

	snap, e := n.h.Tasks.RequestProposal(n.ctx, n.caller, &v1.RequestProposalCommand{Header: admissionHeader("request:" + id), TaskId: n.task.Name})
	if e != nil {
		t.Fatal(e)
	}
	p, e := (&scripted.Reasoner{Step: &v1.ActionStep{StepId: id, ParametersRef: parameters, CapabilityRef: cap}}).Propose(n.ctx, snap)
	if e != nil {
		t.Fatal(e)
	}
	r, e := n.h.Tasks.ReceiveProposal(n.ctx, n.caller, &v1.ReceiveProposalCommand{Header: admissionHeader("proposal:" + id), Proposal: p})
	requireAccepted(t, r, e)
	r, e = n.h.Tasks.Admit(n.ctx, n.caller, &v1.AdmitCommand{Header: admissionHeader("admit:" + id), TaskId: n.task.Name, ProposalRef: r.ResultRef, GrantRef: grant})
	requireAccepted(t, r, e)
	a, e := n.h.Tasks.QueryAdmission(n.ctx, n.caller, r.ResultRef)
	if e != nil {
		t.Fatal(e)
	}
	if e = n.h.Tasks.ProcessHandoffs(n.ctx, n.caller); e != nil {
		t.Fatal(e)
	}
	claim, e := n.h.LedgerWork.ExecuteJob(n.ctx, n.caller, &v1.JobCommand{Identity: executionHeader("claim:" + id).Identity, ContractVersion: 1, Action: "CLAIM", AllowedTypes: []string{"EXECUTE_OPERATION"}, Limit: 100, LeaseMs: 30000, ProcessInstance: "native:" + id})
	requireAccepted(t, claim, e)
	var job *v1.Job
	for _, j := range claim.Jobs {
		if proto.Equal(j.SpecificationRef.Name, a.OperationId) {
			job = j
			break
		}
	}
	if job == nil {
		t.Fatal("no native execution claim")
	}
	r, e = n.h.Ledger.Prepare(n.ctx, n.caller, &v1.PrepareExecutionCommand{Header: executionHeader("prepare:" + id), OperationId: a.OperationId, ProcessInstance: job.ProcessInstance, Claim: job})
	requireAccepted(t, r, e)
	x, e := n.h.Ledger.QueryExecution(n.ctx, n.caller, a.OperationId)
	if e != nil {
		t.Fatal(e)
	}
	b := &v1.ExitCredentialBinding{UserId: "u", TaskId: a.TaskId, SubjectId: a.TaskId, OperationId: a.OperationId, AttemptId: x.Attempt.Ref.Name, SendSeq: x.Send.SendSeq, AdmissionRef: a.Ref, GrantUseRef: a.GrantUseRef, CallerIssuerId: "egress", Audience: "egress", ExecutorEndpointId: a.ExecutorEndpointId, ExecutorInstance: x.Send.ProcessInstance, DescriptorDigest: x.CallDescriptor.Digest, UseRight: a.CapabilitySnapshot.UseRight, ProcessingPurpose: a.CapabilitySnapshot.ProcessingPurpose, RequirementsVersion: a.RequirementsVersion, InputVersion: a.InputVersion, ControlGeneration: a.ControlGeneration, BudgetReservationRef: a.BudgetBasis.ReservationRef}
	r, e = n.h.Grants.IssueCredential(n.ctx, n.caller, &v1.IssueExitCredentialCommand{Header: admissionHeader("credential:" + id), AdmissionRef: a.Ref, Binding: b, ExpiresAtUnixMs: time.Now().Add(time.Minute).UnixMilli()})
	requireAccepted(t, r, e)
	h := admissionHeader("start:" + x.Send.Ref.Name.LocalId)
	h.Identity.IssuerId = "egress"
	return a, &v1.StartExecutionCommand{Header: h, AdmissionRef: a.Ref, CredentialRef: r.ResultRef, Binding: b, Claim: job, CallDescriptor: x.CallDescriptor}
}
func (n *nativeScenario) observation(t *testing.T, a *v1.Admission) *v1.RawObservation {
	t.Helper()
	x, e := n.h.Ledger.QueryExecution(n.ctx, n.caller, a.OperationId)
	if e != nil {
		t.Fatal(e)
	}
	o, e := n.h.Ledger.QueryObservation(n.ctx, n.caller, x.Send.ObservationRef)
	if e != nil {
		t.Fatal(e)
	}
	return o
}

// 规则：G1、G2、G3、G4、G5、G11、R7
func TestNativeFileLostObservationRequiresOriginalQueryBeforeSuccessor(t *testing.T) {
	n := newNativeScenario(t)
	a, start := n.prepare(t, "A", "CREATE", "", []byte("A"))
	blob, e := proto.Marshal(start)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(n.path+".start", blob, 0600); e != nil {
		t.Fatal(e)
	}
	if e = n.h.Close(); e != nil {
		t.Fatal(e)
	}
	child := exec.Command(os.Args[0], "-test.run=^TestNativeFileCrashChild$")
	child.Env = append(os.Environ(), "LERNA_NATIVE_DB="+n.path, "LERNA_NATIVE_ROOT="+n.root, "LERNA_NATIVE_POINT=content.observation")
	output, e := child.CombinedOutput()
	var crashed *exec.ExitError
	if !errors.As(e, &crashed) || crashed.ExitCode() != sqlite.CrashExitCode {
		t.Fatalf("child did not stop after target publication: %v %s", e, output)
	}
	n.h, e = assembly.OpenWithFiles(n.path, "u", "d", map[string]string{"documents": n.root})
	if e != nil {
		t.Fatal(e)
	}
	pointer := readNativePointer(t, n.root)
	successor := n.additionalTask(t, "successor")
	b, bs := successor.prepare(t, "blocked-B", "REPLACE", pointer.Version, []byte("B"))
	r, e := n.h.Egress.Invoke(n.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, bs)
	requireAccepted(t, r, e)
	raw := n.observation(t, b)
	if raw.GetFileEvidence().GetErrorCode() != "FILE_PREDECESSOR_UNACCEPTED" || readNativePointer(t, n.root).Version != pointer.Version {
		t.Fatalf("overwrote unaccepted predecessor: %v", raw)
	}
	cap, grant := n.capGrant(t, "query-A", "QUERY", a.ParametersRef)
	r, e = n.h.Ledger.RequestReconciliation(n.ctx, n.caller, &v1.RequestReconciliationCommand{Header: executionHeader("query-A"), OperationId: a.OperationId, QueryCapabilityRef: cap, ParametersRef: a.ParametersRef, GrantRef: grant, PolicyVersion: 1, Limits: &v1.ReconciliationLimits{MaxChecks: 3, DeadlineUnixMs: time.Now().Add(time.Hour).UnixMilli(), MaxFee: 0, InitialDelayMs: 10, MaxDelayMs: 100}})
	requireAccepted(t, r, e)
	if e = n.h.Ledger.ProcessReconciliations(n.ctx, n.caller); e != nil {
		t.Fatal(e)
	}
	original, e := n.h.Ledger.QueryOperation(n.ctx, n.caller, a.OperationId)
	if e != nil || original.Effect.Outcome != "APPLIED" || original.Effect.LateEffect != "RULED_OUT" {
		t.Fatalf("original query unresolved: %v %v", original, e)
	}
	plan, e := n.h.Ledger.QueryReconciliation(n.ctx, n.caller, a.OperationId)
	if e != nil {
		t.Fatal(e)
	}
	relation, e := n.h.Ledger.QueryReconciliationQuery(n.ctx, n.caller, plan.QueryRefs[0])
	if e != nil {
		t.Fatal(e)
	}
	observed, e := n.h.Ledger.QueryObservation(n.ctx, n.caller, relation.ObservationRef)
	if e != nil || proto.Equal(observed.OperationId, a.OperationId) || !proto.Equal(observed.QuerySubject.OperationId, a.OperationId) || observed.FileEvidence.DurabilityConfirmed {
		t.Fatalf("query relabeled or invented durability: %v %v", observed, e)
	}
	b, bs = successor.prepare(t, "B", "REPLACE", pointer.Version, []byte("B"))
	r, e = n.h.Egress.Invoke(n.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, bs)
	requireAccepted(t, r, e)
	if o := n.observation(t, b); !o.FileEvidence.DurabilityConfirmed {
		t.Fatalf("successor blocked after handoff: %v", o)
	}
	if got := readNativePointer(t, n.root); got.Version == pointer.Version {
		t.Fatal("successor not published")
	}
	proof, e := n.h.Ledger.QueryFilePublication(n.ctx, &v1.Caller{UserId: "u", IssuerId: "egress-io"}, pointer)
	if e != nil || proof == nil || !proto.Equal(proof.Ref, observed.Ref) {
		t.Fatalf("A history lost after B: %v %v", proof, e)
	}
	bPointer := readNativePointer(t, n.root)
	third := n.additionalTask(t, "successor-C")
	c, cs := third.prepare(t, "C", "REPLACE", bPointer.Version, []byte("C"))
	r, e = n.h.Egress.Invoke(n.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, cs)
	requireAccepted(t, r, e)
	if !n.observation(t, c).FileEvidence.DurabilityConfirmed || readNativePointer(t, n.root).PreviousVersion != bPointer.Version {
		t.Fatal("C did not use B as immediate predecessor")
	}
	for _, ancestor := range []*v1.FileCommit{pointer, bPointer} {
		p, e := n.h.Ledger.QueryFilePublication(n.ctx, &v1.Caller{UserId: "u", IssuerId: "egress-io"}, ancestor)
		if e != nil || p == nil || !proto.Equal(p.FileEvidence.Commit.OperationId, ancestor.OperationId) {
			t.Fatalf("ancestor evidence lost after C: %v %v", p, e)
		}
	}
}
func readNativePointer(t *testing.T, root string) *v1.FileCommit {
	t.Helper()
	body, e := os.ReadFile(filepath.Join(root, "commits", "report"))
	if e != nil {
		t.Fatal(e)
	}
	c := new(v1.FileCommit)
	if e = protojson.Unmarshal(body, c); e != nil {
		t.Fatal(e)
	}
	return c
}

// 规则：G3、G5、G11
func TestNativeFileCrashChild(t *testing.T) {
	path := os.Getenv("LERNA_NATIVE_DB")
	if path == "" {
		t.Skip("subprocess only")
	}
	h, e := assembly.OpenWithFiles(path, "u", "d", map[string]string{"documents": os.Getenv("LERNA_NATIVE_ROOT")})
	if e != nil {
		t.Fatal(e)
	}
	data, e := os.ReadFile(path + ".start")
	if e != nil {
		t.Fatal(e)
	}
	start := new(v1.StartExecutionCommand)
	if e = proto.Unmarshal(data, start); e != nil {
		t.Fatal(e)
	}
	mode := sqlite.FaultMode(os.Getenv("LERNA_NATIVE_MODE"))
	if mode == "" {
		mode = sqlite.CrashBeforeCommit
	}
	ctx, e := sqlite.WithFault(context.Background(), os.Getenv("LERNA_NATIVE_POINT"), mode)
	if e != nil {
		t.Fatal(e)
	}
	if event := os.Getenv("LERNA_NATIVE_EVENT"); event != "" {
		ctx = egressio.WithNativeFileFault(ctx, &egressio.NativeFileFault{CrashAfter: event})
	}
	_, e = h.Egress.Invoke(ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, start)
	if mode == sqlite.LoseReceipt {
		if os.Getenv("LERNA_NATIVE_POINT") == "content.file_resources" {
			var failure *command.Failure
			if !errors.As(e, &failure) || failure.Detail.CommandAcceptance != v1.CommandAcceptance_COMMAND_ACCEPTANCE_UNKNOWN {
				t.Fatalf("resource receipt loss: %v", e)
			}
		} else if e != nil {
			t.Fatal(e)
		}
		os.Exit(0)
	}
	t.Fatalf("native crash boundary not reached: %v", e)
}

func (n *nativeScenario) additionalTask(t *testing.T, id string) *nativeScenario {
	t.Helper()
	m := &nativeScenario{h: n.h, ctx: n.ctx, caller: n.caller, root: n.root, path: n.path}
	r, e := m.h.Sessions.SubmitGoal(m.ctx, m.caller, &v1.SubmitGoalCommand{Identity: admissionHeader("goal:" + id).Identity, ContractVersion: 1, SchemaId: "lerna.v1.SubmitGoal", FingerprintVersion: 1, Goal: "another writer"})
	if e != nil {
		t.Fatal(e)
	}
	if e = m.h.Sessions.ProcessPending(m.ctx, m.caller); e != nil {
		t.Fatal(e)
	}
	q, e := m.h.Durable.QueryReceipt(m.ctx, m.caller, r.Identity)
	if e != nil {
		t.Fatal(e)
	}
	m.task = q.Receipt.TaskRef
	m.session = q.Receipt.SessionRef
	m.input = q.Receipt.InputRef
	r, e = m.h.Tasks.AcceptRequirements(m.ctx, m.caller, &v1.AcceptRequirementsCommand{Header: admissionHeader("requirements:" + id), TaskRef: m.task, InputVersion: 1, Source: "TRUSTED_TEMPLATE", Conditions: []*v1.Requirement{{ConditionId: "file", DescriptionRef: m.input, Necessary: true, VerificationRule: "TARGET_RECORD", RuleVersion: 1}}})
	requireAccepted(t, r, e)
	r, e = m.h.Budget.Configure(m.ctx, m.caller, &v1.ConfigureBudgetCommand{Header: admissionHeader("task-budget:" + id), TaskId: m.task.Name, Unit: "USD_MICRO", Limit: 100})
	requireAccepted(t, r, e)
	return m
}

// 规则：G1、G3、G4、G5、G9
func TestNativeFileFailedPreparationRetainsResourcesAndCleansOnlyAdmittedOrphan(t *testing.T) {
	n := newNativeScenario(t)
	a, start := n.prepare(t, "failed", "CREATE", "", []byte("temporary bytes"))
	ctx := egressio.WithNativeFileFault(n.ctx, &egressio.NativeFileFault{FailBefore: "object.sync.fullfsync", Error: syscall.ENOSPC})
	r, e := n.h.Egress.Invoke(ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, start)
	requireAccepted(t, r, e)
	raw := n.observation(t, a)
	if raw.FileEvidence.DurabilityConfirmed || raw.FileEvidence.Published || !raw.FileEvidence.NegativeProof {
		t.Fatalf("failed prepare evidence: %v", raw)
	}
	resources, e := n.h.Content.QueryFileResources(n.ctx, n.caller, raw.FileEvidence.ResourcesRef)
	if e != nil || resources == nil || resources.CleanupStatus != "RETAINED" {
		t.Fatalf("lost resources: %v %v", resources, e)
	}
	if _, e = os.Stat(filepath.Join(n.root, "objects", resources.ObjectName)); e != nil {
		t.Fatal(e)
	}
	cleanup, cs := n.prepare(t, "cleanup", "CLEANUP", "", nil, resources.Ref)
	r, e = n.h.Egress.Invoke(n.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, cs)
	requireAccepted(t, r, e)
	o := n.observation(t, cleanup)
	if o.FileEvidence.Stage != "CLEANED" || !o.FileEvidence.DurabilityConfirmed {
		t.Fatalf("cleanup did not complete: %v", o)
	}
	if _, e = os.Stat(filepath.Join(n.root, "objects", resources.ObjectName)); !errors.Is(e, os.ErrNotExist) {
		t.Fatalf("orphan still exists: %v", e)
	}
	resources, e = n.h.Content.QueryFileResources(n.ctx, n.caller, resources.Ref)
	if e != nil || resources.CleanupStatus != "CLEANED" || !proto.Equal(resources.CleanupObservationRef, o.Ref) {
		t.Fatalf("cleanup evidence ungoverned: %v %v", resources, e)
	}
}

// 规则：G3、G4、G5、G10
func TestNativeFileReadRejectsCrossTargetCommit(t *testing.T) {
	n := newNativeScenario(t)
	_, start := n.prepare(t, "create", "CREATE", "", []byte("private payload"))
	r, e := n.h.Egress.Invoke(n.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, start)
	requireAccepted(t, r, e)
	pointer := readNativePointer(t, n.root)
	pointer.Target = "managed://documents/other"
	body, e := protojson.Marshal(pointer)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(n.root, "commits", "report"), body, 0600); e != nil {
		t.Fatal(e)
	}
	a, read := n.prepare(t, "read", "READ", "", nil)
	r, e = n.h.Egress.Invoke(n.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, read)
	requireAccepted(t, r, e)
	raw := n.observation(t, a)
	if raw.FileEvidence.ErrorCode != "FILE_COMMIT_INVALID" || raw.FileEvidence.ReadbackVerified {
		t.Fatalf("cross-target bytes accepted: %v", raw)
	}
}

// 规则：G2、G3、G4、G5、G8
func TestNativeFileRevocationAtRenamePreventsPublication(t *testing.T) {
	n := newNativeScenario(t)
	a, start := n.prepare(t, "revoked", "CREATE", "", []byte("must remain an orphan"))
	recorder := egressio.NewNativeFileRecorder(nil, nil)
	fired := false
	ctx := egressio.WithNativeFileFault(n.ctx, &egressio.NativeFileFault{Recorder: recorder, Boundary: func(stage string) {
		if stage != "publish.rename" || fired {
			return
		}
		fired = true
		r, e := n.h.Grants.Revoke(n.ctx, n.caller, &v1.RevokeGrantCommand{Header: admissionHeader("revoke-native"), GrantId: a.GrantRefs[0].Name})
		requireAccepted(t, r, e)
	}})
	r, e := n.h.Egress.Invoke(ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, start)
	requireAccepted(t, r, e)
	for _, event := range recorder.Events() {
		if event.Kind == "rename" {
			t.Fatal("revoked writer published")
		}
	}
	if !fired || n.observation(t, a).FileEvidence.Published {
		t.Fatal("publication boundary not fenced")
	}
}

// 规则：G2、G3、G5、G11
func TestNativeFilePrimitiveFailuresNeverClaimDurableSuccess(t *testing.T) {
	for _, stage := range []string{"object.write", "object.sync.fullfsync", "objects.sync.fullfsync", "pointer.sync.fullfsync", "publish.rename", "commits.sync.fullfsync", "readback.object"} {
		t.Run(stage, func(t *testing.T) {
			n := newNativeScenario(t)
			a, start := n.prepare(t, "failure", "CREATE", "", []byte("payload"))
			recorder := egressio.NewNativeFileRecorder(nil, nil)
			fault := syscall.EIO
			if stage == "publish.rename" {
				fault = syscall.EXDEV
			}
			ctx := egressio.WithNativeFileFault(n.ctx, &egressio.NativeFileFault{FailBefore: stage, Error: fault, Recorder: recorder})
			r, e := n.h.Egress.Invoke(ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, start)
			requireAccepted(t, r, e)
			raw := n.observation(t, a)
			op, e := n.h.Ledger.QueryOperation(n.ctx, n.caller, a.OperationId)
			if e != nil {
				t.Fatal(e)
			}
			if raw.FileEvidence.ErrorCode == "" || op.Effect.Outcome == "APPLIED" {
				t.Fatalf("failure declared success: %v", raw)
			}
			count := 0
			for _, event := range recorder.Events() {
				if event.Kind == "rename" {
					count++
				}
			}
			expected := 0
			if stage == "commits.sync.fullfsync" || stage == "readback.object" {
				expected = 1
			}
			if count != expected || raw.FileEvidence.Published != (expected == 1) {
				t.Fatalf("actual publication=%d evidence=%v", count, raw.FileEvidence)
			}
			if stage == "commits.sync.fullfsync" && raw.FileEvidence.DurabilityConfirmed {
				t.Fatal("failed directory barrier marked durable")
			}
		})
	}
}

// 规则：G2、G3、G5、G11
func TestNativeFileSameVersionWritersAndReadUseActualTarget(t *testing.T) {
	n := newNativeScenario(t)
	_, start := n.prepare(t, "A", "CREATE", "", []byte("A"))
	r, e := n.h.Egress.Invoke(n.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, start)
	requireAccepted(t, r, e)
	initial := readNativePointer(t, n.root)
	b := n.additionalTask(t, "B")
	c := n.additionalTask(t, "C")
	ba, bs := b.prepare(t, "B", "REPLACE", initial.Version, []byte("B"))
	ca, cs := c.prepare(t, "C", "REPLACE", initial.Version, []byte("C"))
	recorder := egressio.NewNativeFileRecorder(nil, nil)
	ctx := egressio.WithNativeFileFault(n.ctx, &egressio.NativeFileFault{Recorder: recorder, MaxWrite: 3})
	type answer struct {
		r *v1.CommandReceipt
		e error
	}
	results := make(chan answer, 2)
	for _, send := range []*v1.StartExecutionCommand{bs, cs} {
		go func(s *v1.StartExecutionCommand) {
			r, e := n.h.Egress.Invoke(ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, s)
			results <- answer{r, e}
		}(send)
	}
	for i := 0; i < 2; i++ {
		answer := <-results
		requireAccepted(t, answer.r, answer.e)
	}
	winner, loser := b.observation(t, ba), c.observation(t, ca)
	if !winner.FileEvidence.Published {
		winner, loser = loser, winner
	}
	if !winner.FileEvidence.DurabilityConfirmed || loser.FileEvidence.ErrorCode != "FILE_VERSION_CONFLICT" {
		t.Fatalf("writers: %v %v", winner, loser)
	}
	renames := 0
	for _, event := range recorder.Events() {
		if event.Kind == "rename" {
			renames++
		}
	}
	if renames != 1 {
		t.Fatalf("duplicate actual publications=%d", renames)
	}
	current := readNativePointer(t, n.root)
	if !proto.Equal(current, winner.FileEvidence.Commit) {
		t.Fatal("current pointer differs from winner")
	}
	reader := n.additionalTask(t, "reader")
	ra, rs := reader.prepare(t, "read", "READ", current.Version, nil)
	r, e = n.h.Egress.Invoke(n.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, rs)
	requireAccepted(t, r, e)
	raw := reader.observation(t, ra)
	content, e := n.h.Content.Read(n.ctx, n.caller, raw.BodyRef)
	if e != nil {
		t.Fatal(e)
	}
	if len(content.DerivedFrom) != 1 || !proto.Equal(content.DerivedFrom[0], current.ContentRef) {
		t.Fatalf("file read lost exact source lineage: %v", content)
	}
	actual, e := os.ReadFile(filepath.Join(n.root, "objects", current.ObjectName))
	if e != nil {
		t.Fatal(e)
	}
	if !raw.FileEvidence.ReadbackVerified || string(command.ContentBytes(content)) != string(actual) {
		t.Fatalf("readback differs: %v", raw)
	}
}

// 规则：G3、G4、G5、G9
func TestNativeFileCleanupCannotRemoveCurrentPublication(t *testing.T) {
	n := newNativeScenario(t)
	a, start := n.prepare(t, "A", "CREATE", "", []byte("retained"))
	r, e := n.h.Egress.Invoke(n.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, start)
	requireAccepted(t, r, e)
	original := n.observation(t, a)
	cleaner := n.additionalTask(t, "cleaner")
	ca, cs := cleaner.prepare(t, "cleanup-current", "CLEANUP", "", nil, original.FileEvidence.ResourcesRef)
	recorder := egressio.NewNativeFileRecorder(nil, nil)
	ctx := egressio.WithNativeFileFault(n.ctx, &egressio.NativeFileFault{Recorder: recorder})
	r, e = n.h.Egress.Invoke(ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, cs)
	requireAccepted(t, r, e)
	if cleaner.observation(t, ca).FileEvidence.ErrorCode != "FILE_RESOURCE_IN_USE" {
		t.Fatal("current resource not protected")
	}
	for _, event := range recorder.Events() {
		if event.Kind == "unlink" {
			t.Fatal("current resource removed")
		}
	}
	if !proto.Equal(readNativePointer(t, n.root), original.FileEvidence.Commit) {
		t.Fatal("current publication changed")
	}
}

// 规则：G4、G5、G7、G12
func TestNativeFileRejectsUnsafePathObjects(t *testing.T) {
	for _, kind := range []string{"symlink-directory", "symlink-pointer", "hardlink-pointer", "fifo-pointer", "moved-root", "moved-object-create"} {
		t.Run(kind, func(t *testing.T) {
			n := newNativeScenario(t)
			a, start := n.prepare(t, "unsafe", "CREATE", "", []byte("protected"))
			outside := filepath.Join(t.TempDir(), "outside")
			if e := os.WriteFile(outside, []byte("untouched"), 0600); e != nil {
				t.Fatal(e)
			}
			pointer := filepath.Join(n.root, "commits", "report")
			switch kind {
			case "symlink-directory":
				if e := os.Remove(filepath.Join(n.root, "objects")); e != nil {
					t.Fatal(e)
				}
				if e := os.Symlink(filepath.Dir(outside), filepath.Join(n.root, "objects")); e != nil {
					t.Fatal(e)
				}
			case "symlink-pointer":
				if e := os.Symlink(outside, pointer); e != nil {
					t.Fatal(e)
				}
			case "hardlink-pointer":
				if e := os.Link(outside, pointer); e != nil {
					t.Fatal(e)
				}
			case "fifo-pointer":
				if e := syscall.Mkfifo(pointer, 0600); e != nil {
					t.Fatal(e)
				}
			}
			recorder := egressio.NewNativeFileRecorder(nil, nil)
			moved := false
			fault := &egressio.NativeFileFault{Recorder: recorder, Boundary: func(stage string) {
				if ((kind == "moved-root" && stage == "publish.rename") || (kind == "moved-object-create" && stage == "object.create")) && !moved {
					if e := os.Rename(n.root, n.root+".moved"); e != nil {
						t.Fatal(e)
					}
					moved = true
				}
			}}
			defer func() {
				if moved {
					if e := os.Rename(n.root+".moved", n.root); e != nil {
						t.Fatal(e)
					}
				}
			}()
			ctx := egressio.WithNativeFileFault(n.ctx, fault)
			r, e := n.h.Egress.Invoke(ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, start)
			requireAccepted(t, r, e)
			raw := n.observation(t, a)
			if raw.FileEvidence.ErrorCode == "" || raw.FileEvidence.Published {
				t.Fatalf("unsafe target accepted: %v", raw)
			}
			for _, event := range recorder.Events() {
				if event.Kind == "rename" {
					t.Fatal("unsafe target published")
				}
				if kind == "moved-object-create" && event.Kind == "create" && event.Directory == "objects" {
					t.Fatal("created resource under moved root")
				}

			}
			body, e := os.ReadFile(outside)
			if e != nil || string(body) != "untouched" {
				t.Fatalf("outside changed: %q %v", body, e)
			}
		})
	}
}

func requestNativeQuery(t *testing.T, n *nativeScenario, id string, a *v1.Admission) *v1.Reconciliation {
	t.Helper()
	cap, grant := n.capGrant(t, id, "QUERY", a.ParametersRef)
	r, e := n.h.Ledger.RequestReconciliation(n.ctx, n.caller, &v1.RequestReconciliationCommand{Header: executionHeader(id), OperationId: a.OperationId, QueryCapabilityRef: cap, ParametersRef: a.ParametersRef, GrantRef: grant, PolicyVersion: 1, Limits: &v1.ReconciliationLimits{MaxChecks: 3, DeadlineUnixMs: time.Now().Add(time.Hour).UnixMilli(), MaxFee: 0, InitialDelayMs: 10, MaxDelayMs: 100}})
	requireAccepted(t, r, e)
	if e = n.h.Ledger.ProcessReconciliations(n.ctx, n.caller); e != nil {
		t.Fatal(e)
	}
	plan, e := n.h.Ledger.QueryReconciliation(n.ctx, n.caller, a.OperationId)
	if e != nil {
		t.Fatal(e)
	}
	return plan
}
func crashNativeInvocation(t *testing.T, n *nativeScenario, start *v1.StartExecutionCommand, event string) {
	t.Helper()
	blob, e := proto.Marshal(start)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(n.path+".start", blob, 0600); e != nil {
		t.Fatal(e)
	}
	if e = n.h.Close(); e != nil {
		t.Fatal(e)
	}
	child := exec.Command(os.Args[0], "-test.run=^TestNativeFileCrashChild$")
	child.Env = append(os.Environ(), "LERNA_NATIVE_DB="+n.path, "LERNA_NATIVE_ROOT="+n.root, "LERNA_NATIVE_POINT=content.observation", "LERNA_NATIVE_EVENT="+event)
	out, e := child.CombinedOutput()
	var exit *exec.ExitError
	if !errors.As(e, &exit) || exit.ExitCode() != sqlite.CrashExitCode {
		t.Fatalf("native child did not crash: %v %s", e, out)
	}
	n.h, e = assembly.OpenWithFiles(n.path, "u", "d", map[string]string{"documents": n.root})
	if e != nil {
		t.Fatal(e)
	}
}

// 规则：G1、G2、G3、G5、G6
func TestNativeFileVisibleQueryDoesNotCompleteWithoutDurability(t *testing.T) {
	for _, stage := range []string{"commits.sync.fullfsync", "readback.object"} {
		t.Run(stage, func(t *testing.T) {
			n := newNativeScenario(t)
			n.scoped = true
			a, start := n.prepare(t, "A", "CREATE", "", []byte("visible but not durable"))
			ctx := egressio.WithNativeFileFault(n.ctx, &egressio.NativeFileFault{FailBefore: stage, Error: syscall.EIO})
			r, e := n.h.Egress.Invoke(ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, start)
			requireAccepted(t, r, e)
			requestNativeQuery(t, n, "query-uncertain", a)
			op, e := n.h.Ledger.QueryOperation(n.ctx, n.caller, a.OperationId)
			if e != nil || op.Effect.Outcome != "APPLIED" {
				t.Fatalf("visible query effect: %v %v", op, e)
			}
			snap, e := n.h.Tasks.RequestProposal(n.ctx, n.caller, &v1.RequestProposalCommand{Header: admissionHeader("complete-request"), TaskId: n.task.Name})
			if e != nil {
				t.Fatal(e)
			}
			proposal, e := (&scripted.Reasoner{CompletionEvidence: []*v1.CompletionEvidence{{ConditionId: "file", OperationId: a.OperationId}}}).Propose(n.ctx, snap)
			if e != nil {
				t.Fatal(e)
			}
			r, e = n.h.Tasks.ReceiveProposal(n.ctx, n.caller, &v1.ReceiveProposalCommand{Header: admissionHeader("complete-proposal"), Proposal: proposal})
			requireAccepted(t, r, e)
			r, e = n.h.Tasks.BeginCompletion(n.ctx, n.caller, &v1.BeginCompletionCommand{Header: admissionHeader("complete"), TaskId: n.task.Name, ProposalRef: r.ResultRef})
			requireAccepted(t, r, e)
			if e = n.h.Tasks.ProcessCompletions(n.ctx, n.caller); e != nil {
				t.Fatal(e)
			}
			result, e := n.h.Tasks.QueryResult(n.ctx, n.caller, n.task.Name)
			if e != nil {
				t.Fatal(e)
			}
			if stage == "commits.sync.fullfsync" && result != nil {
				t.Fatalf("visible-only query completed task: %v", result)
			}
			if stage == "readback.object" && result.GetOutcome() != "SUCCEEDED" {
				t.Fatalf("durable original plus verified query did not complete: %v", result)
			}
		})
	}
}

// 规则：G1、G3、G5、G9、G11
func TestNativeFileOrphanQueryNeverClaimsPublication(t *testing.T) {
	n := newNativeScenario(t)
	a, start := n.prepare(t, "orphan", "CREATE", "", []byte("prepared only"))
	crashNativeInvocation(t, n, start, "objects.sync")
	files, e := os.ReadDir(filepath.Join(n.root, "objects"))
	if e != nil || len(files) != 1 {
		t.Fatalf("real prepared object missing: %v %v", files, e)
	}
	recorder := egressio.NewNativeFileRecorder(nil, nil)
	n.ctx = egressio.WithNativeFileFault(n.ctx, &egressio.NativeFileFault{Recorder: recorder})
	plan := requestNativeQuery(t, n, "query-orphan", a)
	op, e := n.h.Ledger.QueryOperation(n.ctx, n.caller, a.OperationId)
	if e != nil || op.Effect.Outcome != "UNKNOWN" {
		t.Fatalf("orphan treated as publication: %v %v", op, e)
	}
	if len(plan.QueryRefs) != 1 {
		t.Fatalf("query not performed: %v", plan)
	}
	for _, event := range recorder.Events() {
		if event.Kind == "write" || event.Kind == "rename" {
			t.Fatal("orphan query republished")
		}
	}
}

// 规则：G1、G3、G5、G11
func TestNativeFileOriginalQueryUsesAcceptedHistoryAfterSuccessor(t *testing.T) {
	n := newNativeScenario(t)
	a, start := n.prepare(t, "A", "CREATE", "", []byte("A"))
	ctx := egressio.WithNativeFileFault(n.ctx, &egressio.NativeFileFault{FailBefore: "readback.object", Error: syscall.EIO})
	r, e := n.h.Egress.Invoke(ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, start)
	requireAccepted(t, r, e)
	first := readNativePointer(t, n.root)
	successor := n.additionalTask(t, "B")
	_, bs := successor.prepare(t, "B", "REPLACE", first.Version, []byte("B"))
	r, e = n.h.Egress.Invoke(n.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, bs)
	requireAccepted(t, r, e)
	second := readNativePointer(t, n.root)
	if second.Version == first.Version {
		t.Fatal("successor did not publish after durable predecessor acceptance")
	}
	recorder := egressio.NewNativeFileRecorder(nil, nil)
	n.ctx = egressio.WithNativeFileFault(n.ctx, &egressio.NativeFileFault{Recorder: recorder})
	before, e := n.h.Ledger.QueryOperation(n.ctx, n.caller, a.OperationId)
	if e != nil || before.Lifecycle != "SETTLED" || before.Dispatch != "SEALED" || before.Effect.Outcome != "UNKNOWN" || before.Effect.LateEffect != "RULED_OUT" {
		t.Fatalf("unknown terminal history state: %v %v", before, e)
	}
	plan := requestNativeQuery(t, n, "query-history", a)
	after, e := n.h.Ledger.QueryOperation(n.ctx, n.caller, a.OperationId)
	if e != nil || after.Dispatch != "SEALED" || after.Execution.Send.SendSeq != 1 || after.Effect.Outcome != "APPLIED" {
		t.Fatalf("history query reopened/resubmitted original: %v %v", after, e)
	}
	if len(plan.QueryRefs) != 1 {
		t.Fatalf("historical query not performed: %v", plan)
	}
	q, e := n.h.Ledger.QueryReconciliationQuery(n.ctx, n.caller, plan.QueryRefs[0])
	if e != nil {
		t.Fatal(e)
	}
	raw, e := n.h.Ledger.QueryObservation(n.ctx, n.caller, q.ObservationRef)
	if e != nil || raw.FileEvidence.Stage != "ACCEPTED_HISTORY" || !raw.FileEvidence.ReadbackVerified || !proto.Equal(raw.FileEvidence.Commit, first) {
		t.Fatalf("history evidence: %v %v", raw, e)
	}
	if !proto.Equal(second, readNativePointer(t, n.root)) {
		t.Fatal("original query restored A over B")
	}
	for _, event := range recorder.Events() {
		if event.Kind == "write" || event.Kind == "rename" {
			t.Fatal("historical query wrote target")
		}
	}
}

// 规则：G1、G4、G5、G11
func TestNativeFileCancellationAndClaimLossAtPublicationBoundary(t *testing.T) {
	for _, kind := range []string{"cancel", "claim"} {
		t.Run(kind, func(t *testing.T) {
			n := newNativeScenario(t)
			a, start := n.prepare(t, kind, "CREATE", "", []byte("must not publish"))
			fired := false
			recorder := egressio.NewNativeFileRecorder(nil, nil)
			ctx := egressio.WithNativeFileFault(n.ctx, &egressio.NativeFileFault{Recorder: recorder, Boundary: func(stage string) {
				if stage != "publish.rename" || fired {
					return
				}
				fired = true
				if kind == "cancel" {
					task, e := n.h.Tasks.QueryTask(n.ctx, n.caller, n.task.Name)
					if e != nil {
						t.Fatal(e)
					}
					r, e := n.h.Sessions.SubmitInput(n.ctx, n.caller, &v1.SubmitInputCommand{Header: admissionHeader("cancel-at-publish"), SessionId: n.session.Name, TaskId: n.task.Name, InputKind: "CONTROL", Control: "CANCEL", ExpectedControlGeneration: task.ControlGeneration})
					requireAccepted(t, r, e)
				} else {
					r, e := n.h.LedgerWork.ExecuteJob(n.ctx, n.caller, &v1.JobCommand{Identity: executionHeader("yield-claim-at-publish").Identity, ContractVersion: 1, Action: "PROGRESS", JobRef: start.Claim.Ref, ClaimEpoch: start.Claim.ClaimEpoch, ProcessInstance: start.Claim.ProcessInstance, NextState: "WAITING", WaitingReason: "qualification changed"})
					requireAccepted(t, r, e)
				}
			}})
			r, e := n.h.Egress.Invoke(ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, start)
			requireAccepted(t, r, e)
			raw := n.observation(t, a)
			if !fired || raw.FileEvidence.Published || raw.FileEvidence.ErrorCode == "" {
				t.Fatalf("stale publisher escaped: %v", raw)
			}
			for _, event := range recorder.Events() {
				if event.Kind == "rename" {
					t.Fatal("stale publisher performed rename")
				}
			}
		})
	}
}

// 规则：G1、G3、G4、G5、G9
func TestNativeFileCleanupRefusesUnknownOrChangedResources(t *testing.T) {
	for _, kind := range []string{"unknown", "identity", "sync-error"} {
		t.Run(kind, func(t *testing.T) {
			n := newNativeScenario(t)
			a, start := n.prepare(t, "orphan", "CREATE", "", []byte("retained orphan"))
			if kind == "unknown" {
				crashNativeInvocation(t, n, start, "objects.sync")
			} else {
				ctx := egressio.WithNativeFileFault(n.ctx, &egressio.NativeFileFault{FailBefore: "object.sync.fullfsync", Error: syscall.ENOSPC})
				r, e := n.h.Egress.Invoke(ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, start)
				requireAccepted(t, r, e)
			}
			x, e := n.h.Ledger.QueryExecution(n.ctx, n.caller, a.OperationId)
			if e != nil {
				t.Fatal(e)
			}
			resources, e := n.h.Content.QueryFileResourcesForSend(n.ctx, n.caller, x.Send.Ref)
			if e != nil || resources == nil {
				t.Fatalf("resource registration lost: %v %v", resources, e)
			}
			path := filepath.Join(n.root, "objects", resources.ObjectName)
			if kind == "identity" {
				if e = os.Rename(path, path+".retained"); e != nil {
					t.Fatal(e)
				}
				if e = os.WriteFile(path, []byte("replacement inode"), 0600); e != nil {
					t.Fatal(e)
				}
			}
			cleaner := n.additionalTask(t, "cleaner")
			ca, cs := cleaner.prepare(t, "cleanup", "CLEANUP", "", nil, resources.Ref)
			fault := &egressio.NativeFileFault{Recorder: egressio.NewNativeFileRecorder(nil, nil)}
			if kind == "sync-error" {
				fault.FailBefore = "cleanup.objects.sync.fullfsync"
				fault.Error = syscall.EIO
			}
			ctx := egressio.WithNativeFileFault(n.ctx, fault)
			r, e := n.h.Egress.Invoke(ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, cs)
			requireAccepted(t, r, e)
			raw := cleaner.observation(t, ca)
			if raw.FileEvidence.ErrorCode == "" || raw.FileEvidence.DurabilityConfirmed || raw.FileEvidence.Stage == "CLEANED" {
				t.Fatalf("unsafe cleanup accepted: %v", raw)
			}
			resources, e = n.h.Content.QueryFileResources(n.ctx, n.caller, resources.Ref)
			if e != nil || resources.CleanupStatus != "RETAINED" {
				t.Fatalf("unverified cleanup lost responsibility: %v %v", resources, e)
			}
			if kind != "sync-error" {
				for _, event := range fault.Recorder.Events() {
					if event.Kind == "unlink" {
						t.Fatal("unsafe cleanup unlinked resource")
					}
				}
				if _, e = os.Stat(path); e != nil {
					t.Fatal(e)
				}
			} else {
				retryTask := n.additionalTask(t, "retry-cleaner")
				retry, send := retryTask.prepare(t, "cleanup-absence", "CLEANUP", "", nil, resources.Ref)
				r, e = n.h.Egress.Invoke(n.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, send)
				requireAccepted(t, r, e)
				if retryTask.observation(t, retry).FileEvidence.Stage != "CLEANED" {
					t.Fatal("admitted absence+barriers did not finish cleanup")
				}
			}
		})
	}
}

// 规则：G4、G5、G11、G12
func TestNativeFileRootReplacementAcrossRestartIsNotSameTarget(t *testing.T) {
	n := newNativeScenario(t)
	_, start := n.prepare(t, "A", "CREATE", "", []byte("original root"))
	r, e := n.h.Egress.Invoke(n.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, start)
	requireAccepted(t, r, e)
	readNativePointer(t, n.root)
	old := n.root + ".original"
	if e = os.Rename(n.root, old); e != nil {
		t.Fatal(e)
	}
	defer func() {
		_ = os.RemoveAll(n.root)
		if e := os.Rename(old, n.root); e != nil {
			t.Fatal(e)
		}
	}()
	for _, path := range []string{n.root, filepath.Join(n.root, "objects"), filepath.Join(n.root, "commits"), filepath.Join(n.root, "locks")} {
		if e = os.Mkdir(path, 0700); e != nil {
			t.Fatal(e)
		}
	}
	if e = n.h.Close(); e != nil {
		t.Fatal(e)
	}
	n.h, e = assembly.OpenWithFiles(n.path, "u", "d", map[string]string{"documents": n.root})
	if e != nil {
		t.Fatal(e)
	}
	other := n.additionalTask(t, "new-root")
	a, send := other.prepare(t, "B", "CREATE", "", []byte("wrong root"))
	recorder := egressio.NewNativeFileRecorder(nil, nil)
	ctx := egressio.WithNativeFileFault(n.ctx, &egressio.NativeFileFault{Recorder: recorder})
	r, e = n.h.Egress.Invoke(ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, send)
	requireAccepted(t, r, e)
	raw := other.observation(t, a)
	if raw.FileEvidence.ErrorCode != "FILE_ROOT_CHANGED" || raw.FileEvidence.Published {
		t.Fatalf("replacement root accepted: %v", raw)
	}
	for _, event := range recorder.Events() {
		if event.Kind == "create" || event.Kind == "write" || event.Kind == "rename" {
			t.Fatal("replacement root received mutation")
		}
	}
}

// 规则：G3、G5、G11、G12
func TestNativeFileRootBindingCrashDoesNotPublishFile(t *testing.T) {
	for _, mode := range []sqlite.FaultMode{sqlite.CrashBeforeCommit, sqlite.CrashAfterCommit, sqlite.LoseReceipt} {
		t.Run(string(mode), func(t *testing.T) {
			n := newNativeScenario(t)
			a, start := n.prepare(t, "root-bind", "CREATE", "", []byte("not yet published"))
			blob, e := proto.Marshal(start)
			if e != nil {
				t.Fatal(e)
			}
			if e = os.WriteFile(n.path+".start", blob, 0600); e != nil {
				t.Fatal(e)
			}
			if e = n.h.Close(); e != nil {
				t.Fatal(e)
			}
			child := exec.Command(os.Args[0], "-test.run=^TestNativeFileCrashChild$")
			child.Env = append(os.Environ(), "LERNA_NATIVE_DB="+n.path, "LERNA_NATIVE_ROOT="+n.root, "LERNA_NATIVE_POINT=content.file_root", "LERNA_NATIVE_MODE="+string(mode))
			out, e := child.CombinedOutput()
			if mode == sqlite.LoseReceipt {
				if e != nil {
					t.Fatalf("receipt-loss child: %v %s", e, out)
				}
			} else {
				var exit *exec.ExitError
				if !errors.As(e, &exit) || exit.ExitCode() != sqlite.CrashExitCode {
					t.Fatalf("root boundary not hit: %v %s", e, out)
				}
			}
			n.h, e = assembly.OpenWithFiles(n.path, "u", "d", map[string]string{"documents": n.root})
			if e != nil {
				t.Fatal(e)
			}
			root, e := n.h.Content.QueryManagedFileRoot(n.ctx, n.caller, "documents")
			if e != nil {
				t.Fatal(e)
			}
			if root != nil {
				t.Logf("persisted governed root: %s", protojson.Format(root))
			}
			if (root != nil) != (mode != sqlite.CrashBeforeCommit) {
				t.Fatalf("root binding lost/partially published: %v", root)
			}
			sources, err := n.h.Trace.QuerySources(n.ctx, n.caller)
			if err != nil {
				t.Fatal(err)
			}
			rootSources := 0
			for _, source := range sources {
				event := source.Command.Event
				if event.EventType != "FILE_ROOT_BOUND" || !proto.Equal(event.OperationId, a.OperationId) {
					continue
				}
				rootSources++
				if root == nil || !proto.Equal(event.SourceRecordRef, root.Ref) || !proto.Equal(event.SendRef.Name, readNativeExecutionSend(t, n, a).Name) || source.Receipt == nil {
					t.Fatalf("root source not atomic/original: %v", source)
				}
			}
			wantSources := 0
			if root != nil {
				wantSources = 1
			}
			if rootSources != wantSources {
				t.Fatalf("root source count=%d want=%d", rootSources, wantSources)
			}
			identity := &v1.CommandIdentity{UserId: "u", IssuerId: "egress-io", TargetDomainId: "d/content", CommandId: "file-root:" + start.Binding.OperationId.LocalId}
			x, e := n.h.Ledger.QueryExecution(n.ctx, n.caller, a.OperationId)
			if e != nil {
				t.Fatal(e)
			}
			identity.CommandId = "file-root:" + x.Send.Ref.Name.LocalId
			q, e := n.h.Content.QueryReceipt(n.ctx, &v1.Caller{UserId: "u", IssuerId: "egress-io"}, identity)
			if e != nil {
				t.Fatal(e)
			}
			if root != nil && (q.State != v1.ReceiptQueryState_RECEIPT_QUERY_STATE_DECIDED || !proto.Equal(q.Receipt.ResultRef, root.Ref)) {
				t.Fatalf("root original receipt missing: %v", q)
			}
			r, e := n.h.Egress.Invoke(n.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, start)
			requireAccepted(t, r, e)
			for _, name := range []string{"objects", "commits", "locks"} {
				entries, e := os.ReadDir(filepath.Join(n.root, name))
				if e != nil || len(entries) != 0 {
					t.Fatalf("root binding/replay mutated %s: %v %v", name, entries, e)
				}
			}
			op, e := n.h.Ledger.QueryOperation(n.ctx, n.caller, a.OperationId)
			if e != nil || op.Effect.Outcome == "APPLIED" {
				t.Fatalf("root binding became file effect: %v %v", op, e)
			}
		})
	}
}

// unavailableFileLedger 只注入读取失败，不生成前驱证明或成功观察。
type unavailableFileLedger struct {
	egressio.FileLedger
	code  string
	calls int
}

func (l *unavailableFileLedger) QueryFilePublication(context.Context, *v1.Caller, *v1.FileCommit) (*v1.RawObservation, error) {
	l.calls++
	return nil, command.Fail(l.code)
}

// 规则：G3、G5、G11
func TestNativeFilePredecessorLedgerFailureDoesNotPublishSuccessor(t *testing.T) {
	for _, code := range []string{"LEDGER_UNAVAILABLE", "RECEIPT_UNKNOWN"} {
		t.Run(code, func(t *testing.T) {
			n := newNativeScenario(t)
			a, start := n.prepare(t, "first", "CREATE", "", []byte("A"))
			r, e := n.h.Egress.Invoke(n.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, start)
			requireAccepted(t, r, e)
			if !n.observation(t, a).FileEvidence.DurabilityConfirmed {
				t.Fatal("first publication not durable")
			}
			pointer := readNativePointer(t, n.root)
			successor := n.additionalTask(t, "blocked-successor")
			b, bs := successor.prepare(t, "successor", "REPLACE", pointer.Version, []byte("B"))
			broken := &unavailableFileLedger{FileLedger: n.h.Ledger, code: code}
			lock, e := egressio.NewFileLock(n.path)
			if e != nil {
				t.Fatal(e)
			}
			service, e := egress.New(n.h.Tasks, n.h.Ledger, n.h.Content, egressio.NewFiles(map[string]string{"documents": n.root}, broken, n.h.Content), lock)
			if e != nil {
				t.Fatal(e)
			}
			recorder := egressio.NewNativeFileRecorder(nil, nil)
			ctx := egressio.WithNativeFileFault(n.ctx, &egressio.NativeFileFault{Recorder: recorder})
			r, e = service.Invoke(ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, bs)
			requireAccepted(t, r, e)
			raw := n.observation(t, b)
			if raw.FileEvidence.ErrorCode != code || broken.calls != 1 || !proto.Equal(readNativePointer(t, n.root), pointer) {
				t.Fatalf("predecessor failure bypassed: %v", raw)
			}
			for _, event := range recorder.Events() {
				if event.Kind == "rename" || event.Stage == "object.create" {
					t.Fatalf("successor mutated namespace after %s: %v", code, event)
				}
			}
		})
	}
}

// 规则：G1、G3、G5、G10、G11
func TestNativeFileSettledUnknownWeakQueryKeepsOriginalSealed(t *testing.T) {
	n := newNativeScenario(t)
	a, start := n.prepare(t, "original", "CREATE", "", []byte("original"))
	ctx := egressio.WithNativeFileFault(n.ctx, &egressio.NativeFileFault{FailBefore: "readback.object", Error: syscall.EIO})
	r, e := n.h.Egress.Invoke(ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, start)
	requireAccepted(t, r, e)
	before, e := n.h.Ledger.QueryOperation(n.ctx, n.caller, a.OperationId)
	if e != nil {
		t.Fatal(e)
	}
	recorder := egressio.NewNativeFileRecorder(nil, nil)
	n.ctx = egressio.WithNativeFileFault(n.ctx, &egressio.NativeFileFault{FailBefore: "query.object", Error: syscall.EIO, Recorder: recorder})
	plan := requestNativeQuery(t, n, "weak-history", a)
	after, e := n.h.Ledger.QueryOperation(n.ctx, n.caller, a.OperationId)
	if e != nil || after.Lifecycle != "SETTLED" || after.Dispatch != "SEALED" || after.Effect.Outcome != "UNKNOWN" || after.Effect.LateEffect != "RULED_OUT" || !proto.Equal(before.Execution, after.Execution) {
		t.Fatalf("weak query changed original responsibility: %v %v", after, e)
	}
	if plan.State != "WAITING" || len(plan.QueryRefs) != 1 {
		t.Fatalf("unknown historical query incorrectly completed: %v", plan)
	}
	relation, e := n.h.Ledger.QueryReconciliationQuery(n.ctx, n.caller, plan.QueryRefs[0])
	if e != nil {
		t.Fatal(e)
	}
	query, e := n.h.Ledger.QueryOperation(n.ctx, n.caller, relation.QueryOperationRef.Name)
	if e != nil {
		t.Fatal(e)
	}
	for _, op := range []*v1.Operation{after, query} {
		source, e := n.h.Budget.QueryBillingSource(n.ctx, n.caller, op.Execution.Send.Ref)
		if e != nil || source.Status != "SETTLED" || !proto.Equal(source.OperationId, op.Ref.Name) || !proto.Equal(source.SendRef.Name, op.Execution.Send.Ref.Name) {
			t.Fatalf("query/original billing relabeled: %v %v", source, e)
		}
	}
	if len(recorder.Events()) == 0 {
		t.Fatal("admitted query never read actual target")
	}
	for _, event := range recorder.Events() {
		if event.Kind != "read" && event.Kind != "error" {
			t.Fatalf("failed query performed mutation: %v", event)
		}
	}
}

// 规则：G1、G5、G11
func TestNativeFileKnownPublicationRejectsRedundantQuery(t *testing.T) {
	n := newNativeScenario(t)
	a, start := n.prepare(t, "known", "CREATE", "", []byte("known"))
	r, e := n.h.Egress.Invoke(n.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, start)
	requireAccepted(t, r, e)
	cap, grant := n.capGrant(t, "known-query", "QUERY", a.ParametersRef)
	r, e = n.h.Ledger.RequestReconciliation(n.ctx, n.caller, &v1.RequestReconciliationCommand{Header: executionHeader("known-query"), OperationId: a.OperationId, QueryCapabilityRef: cap, ParametersRef: a.ParametersRef, GrantRef: grant, PolicyVersion: 1, Limits: &v1.ReconciliationLimits{MaxChecks: 1, DeadlineUnixMs: time.Now().Add(time.Hour).UnixMilli(), InitialDelayMs: 10, MaxDelayMs: 100}})
	if e != nil || r.GetError().GetCode() != "ALREADY_SETTLED" {
		t.Fatalf("known publication reopened query: %v %v", r, e)
	}
}

// 规则：G1、G5、G10、G11
func TestNativeFileRejectsCrossAdapterReconciliation(t *testing.T) {
	n := newNativeScenario(t)
	a, start := n.prepare(t, "original", "CREATE", "", []byte("original"))
	ctx := egressio.WithNativeFileFault(n.ctx, &egressio.NativeFileFault{FailBefore: "readback.object", Error: syscall.EIO})
	r, err := n.h.Egress.Invoke(ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, start)
	requireAccepted(t, r, err)
	before := nativeManifest(t, n.root)
	capRef, grant := n.capGrant(t, "cross-adapter", "QUERY", a.ParametersRef)
	cap, err := n.h.Tasks.QueryCapability(n.ctx, n.caller, capRef)
	if err != nil {
		t.Fatal(err)
	}
	cap.Ref, cap.ApprovedBy = nil, nil
	cap.AdapterRef.Name.LocalId = "simulator-queryable"
	r, err = n.h.Tasks.ConfigureCapability(n.ctx, n.caller, &v1.ConfigureCapabilityCommand{Header: admissionHeader("cross-adapter-capability"), Capability: cap})
	requireAccepted(t, r, err)
	recorder := egressio.NewNativeFileRecorder(nil, nil)
	ctx = egressio.WithNativeFileFault(n.ctx, &egressio.NativeFileFault{Recorder: recorder})
	r, err = n.h.Ledger.RequestReconciliation(ctx, n.caller, &v1.RequestReconciliationCommand{Header: executionHeader("cross-adapter-query"), OperationId: a.OperationId, QueryCapabilityRef: r.ResultRef, ParametersRef: a.ParametersRef, GrantRef: grant, PolicyVersion: 1, Limits: &v1.ReconciliationLimits{MaxChecks: 1, DeadlineUnixMs: time.Now().Add(time.Hour).UnixMilli(), InitialDelayMs: 10, MaxDelayMs: 100}})
	if err != nil || r.GetError().GetCode() != "CAPABILITY_INVALID" {
		t.Fatalf("cross-adapter query accepted: %v %v", r, err)
	}
	if err = n.h.Ledger.ProcessReconciliations(ctx, n.caller); err != nil {
		t.Fatal(err)
	}
	plan, err := n.h.Ledger.QueryReconciliation(n.ctx, n.caller, a.OperationId)
	if err != nil || plan != nil || len(recorder.Events()) != 0 || !reflect.DeepEqual(before, nativeManifest(t, n.root)) {
		t.Fatalf("rejected query created work or native I/O: %v %v", plan, err)
	}
}
