package content_test

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"

	"github.com/ruipengliu/lerna/cmd/assembly"
	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

func header(id string) *v1.CommandHeader {
	return &v1.CommandHeader{Identity: &v1.CommandIdentity{UserId: "alice", IssuerId: "host", TargetDomainId: "local/content", CommandId: id}, ContractVersion: 1, FingerprintVersion: 1, SchemaId: "lerna.v1.AdmissionCommands"}
}
func original(id string, body []byte) *v1.RegisterContentCommand {
	return &v1.RegisterContentCommand{Header: header(id), Body: body, MediaType: "application/octet-stream", SourceDescriptor: &v1.ContentSourceDescriptor{Kind: "HOST_IMPORT", Locator: "fixture:" + id, AcquisitionMethod: "LOCAL_IMPORT", ProviderVersion: "fixture-v1"}}
}

// 规则：G3、G11、G12
func TestImmutableContentVersionsAndIndependentSources(t *testing.T) {
	ctx := context.Background()
	h, e := assembly.Open(filepath.Join(t.TempDir(), "content.db"), "alice", "local")
	if e != nil {
		t.Fatal(e)
	}
	defer h.Close()
	actor := &v1.Caller{UserId: "alice", IssuerId: "host"}
	c := original("a", []byte{0, 255, 65})
	r, e := h.Content.Register(ctx, actor, c)
	if e != nil || r.Decision != v1.Decision_DECISION_ACCEPTED {
		t.Fatalf("register %v %v", r, e)
	}
	if e = h.Content.ProcessRegistrations(ctx, actor); e != nil {
		t.Fatal(e)
	}
	a, e := h.Content.Read(ctx, actor, r.ResultRef)
	if e != nil || !bytes.Equal(command.ContentBytes(a), []byte{0, 255, 65}) {
		t.Fatalf("read %v %v", a, e)
	}
	if a.ContentVersion != 1 || a.ContentId == "" || a.AcquiredAtUnixMs <= 0 || a.LocationRef == nil || a.Digest == "" {
		t.Fatalf("missing provenance: %v", a)
	}
	repeat, e := h.Content.Register(ctx, actor, c)
	if e != nil || !proto.Equal(r, repeat) {
		t.Fatalf("repeat %v %v", repeat, e)
	}
	c.Body = []byte("changed")
	if _, e = h.Content.Register(ctx, actor, c); e == nil || e.Error() != "IDEMPOTENCY_CONFLICT" {
		t.Fatalf("conflict: %v", e)
	}
	update := original("update", []byte("new"))
	update.PreviousVersionRef = a.Ref
	u, e := h.Content.Register(ctx, actor, update)
	if e != nil {
		t.Fatal(e)
	}
	if e = h.Content.ProcessRegistrations(ctx, actor); e != nil {
		t.Fatal(e)
	}
	b, e := h.Content.Read(ctx, actor, u.ResultRef)
	if e != nil || b.ContentId != a.ContentId || b.ContentVersion != 2 || !proto.Equal(b.PreviousVersionRefs[0], a.Ref) {
		t.Fatalf("update %v %v", b, e)
	}
	old, e := h.Content.Read(ctx, actor, a.Ref)
	if e != nil || !proto.Equal(old, a) {
		t.Fatalf("mutated old %v %v", old, e)
	}
	independent, e := h.Content.Register(ctx, actor, original("independent", []byte{0, 255, 65}))
	if e != nil {
		t.Fatal(e)
	}
	if e = h.Content.ProcessRegistrations(ctx, actor); e != nil {
		t.Fatal(e)
	}
	d, e := h.Content.Read(ctx, actor, independent.ResultRef)
	if e != nil || d.ContentId == a.ContentId || d.Digest != a.Digest {
		t.Fatalf("source identity %v %v", d, e)
	}
}

// 规则：G3、R7
func TestGoalStageUsesBodyHolderAndStableReceipt(t *testing.T) {
	ctx := context.Background()
	h, e := assembly.Open(filepath.Join(t.TempDir(), "stage.db"), "alice", "local")
	if e != nil {
		t.Fatal(e)
	}
	defer h.Close()
	actor := &v1.Caller{UserId: "alice", IssuerId: "local-cli"}
	c := &v1.SubmitGoalCommand{Identity: &v1.CommandIdentity{UserId: "alice", IssuerId: "local-cli", TargetDomainId: "local", CommandId: "goal"}, ContractVersion: 1, FingerprintVersion: 1, SchemaId: "lerna.v1.SubmitGoal", Goal: "hello\x00world"}
	ref, e := h.Content.Stage(ctx, actor, c)
	if e != nil {
		t.Fatal(e)
	}
	r, e := h.Content.QueryRegistration(ctx, actor, ref)
	if e != nil || r == nil || r.State != "PUBLISHED" || r.BodyReceipt == nil {
		t.Fatalf("untracked body %v %v", r, e)
	}
	got, e := h.Content.Read(ctx, actor, ref)
	if e != nil || got.Text != c.Goal || !proto.Equal(got.Source, c.Identity) || string(command.ContentBytes(got)) != c.Goal {
		t.Fatalf("goal %v %v", got, e)
	}
	repeated, e := h.Content.Stage(ctx, actor, c)
	if e != nil || !proto.Equal(ref, repeated) {
		t.Fatalf("replay %v %v", repeated, e)
	}
}

// 规则：G3、G6、R7
func TestHostDerivationCapturesEveryInputBeforePublishing(t *testing.T) {
	ctx := context.Background()
	h, e := assembly.Open(filepath.Join(t.TempDir(), "derive.db"), "alice", "local")
	if e != nil {
		t.Fatal(e)
	}
	defer h.Close()
	actor := &v1.Caller{UserId: "alice", IssuerId: "host"}
	refs := []*v1.Ref{}
	for _, id := range []string{"a", "b"} {
		r, e := h.Content.Register(ctx, actor, original(id, []byte(id)))
		if e != nil {
			t.Fatal(e)
		}
		refs = append(refs, r.ResultRef)
	}
	if e = h.Content.ProcessRegistrations(ctx, actor); e != nil {
		t.Fatal(e)
	}
	goal := &v1.SubmitGoalCommand{Identity: &v1.CommandIdentity{UserId: "alice", IssuerId: "host", TargetDomainId: "local", CommandId: "task"}, ContractVersion: 1, FingerprintVersion: 1, SchemaId: "lerna.v1.SubmitGoal", Goal: "derive"}
	if _, e = h.Sessions.SubmitGoal(ctx, actor, goal); e != nil {
		t.Fatal(e)
	}
	if e = h.Sessions.ProcessPending(ctx, actor); e != nil {
		t.Fatal(e)
	}
	q, e := h.Durable.QueryReceipt(ctx, actor, goal.Identity)
	if e != nil {
		t.Fatal(e)
	}
	r, e := h.Content.PrepareDerivation(ctx, actor, &v1.PrepareDerivationCommand{Header: header("prepare"), TaskId: q.Receipt.TaskRef.Name, GeneratorVersion: "test-v1", OutputKind: "CONTEXT", MediaType: "text/plain"})
	if e != nil {
		t.Fatal(e)
	}
	d, e := h.Content.QueryDerivation(ctx, actor, r.ResultRef)
	if e != nil || d == nil || d.HostInstanceId == "" || d.Generation != 1 || d.OutputRef == nil {
		t.Fatalf("responsibility %v %v", d, e)
	}
	for i, ref := range refs {
		body, e := h.Content.ReadDerivationInput(ctx, actor, &v1.ReadDerivationInputCommand{Header: header("read-" + []string{"a", "b"}[i]), DerivationRef: d.Ref, HostInstanceId: d.HostInstanceId, Generation: d.Generation, InputRef: ref})
		if e != nil || string(command.ContentBytes(body)) != []string{"a", "b"}[i] {
			t.Fatalf("input %v %v", body, e)
		}
	}
	seal := &v1.SealDerivationCommand{Header: header("omit"), DerivationRef: d.Ref, HostInstanceId: d.HostInstanceId, Generation: d.Generation, InputRefs: refs[:1]}
	rejected, e := h.Content.SealDerivation(ctx, actor, seal)
	if e != nil || rejected.GetError().GetCode() != "INCOMPLETE_INPUT_SET" {
		t.Fatalf("omission %v %v", rejected, e)
	}
	seal.Header = header("seal")
	seal.InputRefs = refs
	r, e = h.Content.SealDerivation(ctx, actor, seal)
	if e != nil || r.Decision != v1.Decision_DECISION_ACCEPTED {
		t.Fatalf("seal %v %v", r, e)
	}
	publish := &v1.CommitDerivationCommand{Header: header("output"), DerivationRef: d.Ref, HostInstanceId: d.HostInstanceId, Generation: d.Generation, Body: []byte("summary")}
	r, e = h.Content.CommitDerivation(ctx, actor, publish)
	if e != nil || r.Decision != v1.Decision_DECISION_ACCEPTED {
		t.Fatalf("commit %v %v", r, e)
	}
	if _, e = h.Content.Read(ctx, actor, d.OutputRef); e == nil {
		t.Fatal("staged output visible")
	}
	if e = h.Content.ProcessRegistrations(ctx, actor); e != nil {
		t.Fatal(e)
	}
	output, e := h.Content.Read(ctx, actor, d.OutputRef)
	if e != nil || output.SourceDescriptor.Kind != "DERIVED" || len(output.DerivedFrom) != 2 || !proto.Equal(output.DerivedFrom[0], refs[0]) || !proto.Equal(output.DerivedFrom[1], refs[1]) || !proto.Equal(output.ProducerRef, d.Ref) {
		t.Fatalf("derivation %v %v", output, e)
	}
	d, e = h.Content.QueryDerivation(ctx, actor, d.Ref)
	if e != nil || d.State != "COMMITTED" {
		t.Fatalf("published responsibility %v %v", d, e)
	}
}

// 规则：G3、G6、G11
func TestStagedContentHasNoReadableBodyAndDeletionIsUnsupported(t *testing.T) {
	ctx := context.Background()
	h, e := assembly.Open(filepath.Join(t.TempDir(), "staged.db"), "alice", "local")
	if e != nil {
		t.Fatal(e)
	}
	defer h.Close()
	actor := &v1.Caller{UserId: "alice", IssuerId: "host"}
	c := original("staged", []byte{})
	c.SourceDescriptor.SourceTimeUnixMs = 1
	r, e := h.Content.Register(ctx, actor, c)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = h.Content.Read(ctx, actor, r.ResultRef); e == nil || e.Error() != "CONTENT_UNUSABLE" {
		t.Fatalf("staged read: %v", e)
	}
	if e = h.Content.CheckUsable(ctx, actor, r.ResultRef); e == nil {
		t.Fatal("staged is usable")
	}
	reg, e := h.Content.QueryRegistration(ctx, actor, r.ResultRef)
	if e != nil || reg.State != "STAGED" || len(reg.StagedBody) != 0 || reg.Content.AcquiredAtUnixMs <= 1 {
		t.Fatalf("stage %v %v", reg, e)
	}
	if e = h.Content.ProcessRegistrations(ctx, actor); e != nil {
		t.Fatal(e)
	}
	body, e := h.Content.Read(ctx, actor, r.ResultRef)
	if e != nil || len(command.ContentBytes(body)) != 0 || body.SourceDescriptor.SourceTimeUnixMs != 1 {
		t.Fatalf("empty %v %v", body, e)
	}
	if _, e = h.Content.Delete(ctx, actor, &v1.DeleteContentCommand{Header: header("delete"), ContentRef: r.ResultRef}); e == nil || e.Error() != "UNSUPPORTED" {
		t.Fatalf("delete: %v", e)
	}
	after, e := h.Content.Read(ctx, actor, r.ResultRef)
	if e != nil || !proto.Equal(body, after) {
		t.Fatalf("delete changed content %v %v", after, e)
	}
	forged := original("model", []byte("I am raw"))
	forged.SourceDescriptor.Kind = "TRUSTED_IO"
	rr, e := h.Content.Register(ctx, actor, forged)
	if e != nil || rr.GetError().GetCode() != "INVALID_CONTENT_SOURCE" {
		t.Fatalf("spoofed raw %v %v", rr, e)
	}
}

func derivationFixture(t *testing.T) (*assembly.Harness, *v1.Caller, *v1.ContentDerivation, *v1.Ref) {
	t.Helper()
	ctx := context.Background()
	h, e := assembly.Open(filepath.Join(t.TempDir(), "fence.db"), "alice", "local")
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { h.Close() })
	actor := &v1.Caller{UserId: "alice", IssuerId: "host"}
	goal := &v1.SubmitGoalCommand{Identity: &v1.CommandIdentity{UserId: "alice", IssuerId: "host", TargetDomainId: "local", CommandId: "task"}, ContractVersion: 1, FingerprintVersion: 1, SchemaId: "lerna.v1.SubmitGoal", Goal: "input"}
	if _, e = h.Sessions.SubmitGoal(ctx, actor, goal); e != nil {
		t.Fatal(e)
	}
	if e = h.Sessions.ProcessPending(ctx, actor); e != nil {
		t.Fatal(e)
	}
	q, e := h.Durable.QueryReceipt(ctx, actor, goal.Identity)
	if e != nil {
		t.Fatal(e)
	}
	task, e := h.Tasks.QueryTask(ctx, actor, q.Receipt.TaskRef.Name)
	if e != nil {
		t.Fatal(e)
	}
	r, e := h.Content.PrepareDerivation(ctx, actor, &v1.PrepareDerivationCommand{Header: header("prepare"), TaskId: q.Receipt.TaskRef.Name, GeneratorVersion: "test-v1", OutputKind: "MODEL_OUTPUT", MediaType: "application/octet-stream"})
	if e != nil || r.Decision != v1.Decision_DECISION_ACCEPTED {
		t.Fatalf("prepare %v %v", r, e)
	}
	d, e := h.Content.QueryDerivation(ctx, actor, r.ResultRef)
	if e != nil {
		t.Fatal(e)
	}
	return h, actor, d, task.GoalRef
}

// 规则：G3、G6、G11、G12
func TestProducerTakeoverFencesOldInstanceAndKeepsActualInputs(t *testing.T) {
	ctx := context.Background()
	h, actor, d, input := derivationFixture(t)
	read := &v1.ReadDerivationInputCommand{Header: header("read"), DerivationRef: d.Ref, HostInstanceId: d.HostInstanceId, Generation: d.Generation, InputRef: input}
	if _, e := h.Content.ReadDerivationInput(ctx, actor, read); e != nil {
		t.Fatal(e)
	}
	r, e := h.Content.TakeoverDerivation(ctx, actor, &v1.TakeoverDerivationCommand{Header: header("takeover"), DerivationRef: d.Ref, ExpectedGeneration: 1})
	if e != nil || r.Decision != v1.Decision_DECISION_ACCEPTED {
		t.Fatalf("takeover %v %v", r, e)
	}
	next, e := h.Content.QueryDerivation(ctx, actor, d.Ref)
	if e != nil || next.Generation != 2 || next.HostInstanceId == d.HostInstanceId || len(next.ActualInputRefs) != 1 {
		t.Fatalf("next %v %v", next, e)
	}
	if _, e = h.Content.ReadDerivationInput(ctx, actor, read); e == nil || e.Error() != "STALE_PRODUCER" {
		t.Fatalf("old read %v", e)
	}
	seal := &v1.SealDerivationCommand{Header: header("old-seal"), DerivationRef: d.Ref, HostInstanceId: d.HostInstanceId, Generation: 1, InputRefs: []*v1.Ref{input}}
	r, e = h.Content.SealDerivation(ctx, actor, seal)
	if e != nil || r.GetError().GetCode() != "STALE_PRODUCER" {
		t.Fatalf("old seal %v %v", r, e)
	}
	seal.Header = header("new-seal")
	seal.HostInstanceId = next.HostInstanceId
	seal.Generation = 2
	r, e = h.Content.SealDerivation(ctx, actor, seal)
	if e != nil || r.Decision != v1.Decision_DECISION_ACCEPTED {
		t.Fatalf("new seal %v %v", r, e)
	}
	commit := &v1.CommitDerivationCommand{Header: header("new-output"), DerivationRef: d.Ref, HostInstanceId: next.HostInstanceId, Generation: 2, Body: []byte{}}
	r, e = h.Content.CommitDerivation(ctx, actor, commit)
	if e != nil || r.Decision != v1.Decision_DECISION_ACCEPTED {
		t.Fatalf("new output %v %v", r, e)
	}
	if e = h.Content.ProcessRegistrations(ctx, actor); e != nil {
		t.Fatal(e)
	}
	output, e := h.Content.Read(ctx, actor, next.OutputRef)
	if e != nil || output.RawBody == nil || len(output.RawBody) != 0 || len(output.DerivedFrom) != 1 {
		t.Fatalf("empty derived %v %v", output, e)
	}
}

// 规则：G3、G11
func TestUpdatesFromSameHistoricalVersionGetDistinctVersionNumbers(t *testing.T) {
	ctx := context.Background()
	h, e := assembly.Open(filepath.Join(t.TempDir(), "versions.db"), "alice", "local")
	if e != nil {
		t.Fatal(e)
	}
	defer h.Close()
	actor := &v1.Caller{UserId: "alice", IssuerId: "host"}
	r, e := h.Content.Register(ctx, actor, original("base", []byte("base")))
	if e != nil {
		t.Fatal(e)
	}
	if e = h.Content.ProcessRegistrations(ctx, actor); e != nil {
		t.Fatal(e)
	}
	refs := []*v1.Ref{}
	for _, id := range []string{"update-one", "update-two"} {
		c := original(id, []byte(id))
		c.PreviousVersionRef = r.ResultRef
		u, e := h.Content.Register(ctx, actor, c)
		if e != nil {
			t.Fatal(e)
		}
		refs = append(refs, u.ResultRef)
	}
	if e = h.Content.ProcessRegistrations(ctx, actor); e != nil {
		t.Fatal(e)
	}
	a, e := h.Content.Read(ctx, actor, refs[0])
	if e != nil {
		t.Fatal(e)
	}
	b, e := h.Content.Read(ctx, actor, refs[1])
	if e != nil {
		t.Fatal(e)
	}
	if a.ContentId != b.ContentId || a.ContentVersion != 2 || b.ContentVersion != 3 {
		t.Fatalf("version identity collision %v %v", a, b)
	}
}

// 规则：G3、G6、G12
func TestDerivationRejectsMissingForeignAndUnpublishedInputs(t *testing.T) {
	ctx := context.Background()
	h, actor, d, input := derivationFixture(t)
	seal := &v1.SealDerivationCommand{Header: header("no-actual"), DerivationRef: d.Ref, HostInstanceId: d.HostInstanceId, Generation: 1, InputRefs: []*v1.Ref{input}}
	r, e := h.Content.SealDerivation(ctx, actor, seal)
	if e != nil || r.GetError().GetCode() != "MISSING_ACTUAL_INPUT" {
		t.Fatalf("invented input %v %v", r, e)
	}
	staged, e := h.Content.Register(ctx, actor, original("pending", []byte("pending")))
	if e != nil {
		t.Fatal(e)
	}
	foreign := proto.Clone(input).(*v1.Ref)
	foreign.Name.UserId = "other"
	missing := proto.Clone(input).(*v1.Ref)
	missing.Name.LocalId = "does-not-exist"
	for i, ref := range []*v1.Ref{foreign, missing, staged.ResultRef, d.OutputRef} {
		_, e = h.Content.ReadDerivationInput(ctx, actor, &v1.ReadDerivationInputCommand{Header: header("bad-read-" + []string{"foreign", "missing", "staged", "self"}[i]), DerivationRef: d.Ref, HostInstanceId: d.HostInstanceId, Generation: 1, InputRef: ref})
		if e == nil {
			t.Fatalf("invalid input accepted: %v", ref)
		}
	}
	check, e := h.Content.QueryDerivation(ctx, actor, d.Ref)
	if e != nil || len(check.ActualInputRefs) != 0 || check.State != "PREPARED" {
		t.Fatalf("failed reads added provenance %v %v", check, e)
	}
	untrusted := &v1.Caller{UserId: "alice", IssuerId: "model"}
	c := &v1.ReadDerivationInputCommand{Header: header("model-read"), DerivationRef: d.Ref, HostInstanceId: d.HostInstanceId, Generation: 1, InputRef: input}
	c.Header.Identity.IssuerId = "model"
	if _, e = h.Content.ReadDerivationInput(ctx, untrusted, c); e == nil || e.Error() != "PERMISSION_DENIED" {
		t.Fatalf("model captured input: %v", e)
	}
	bad := &v1.PrepareDerivationCommand{Header: header("fake-task"), TaskId: &v1.GlobalName{UserId: "alice", AuthorityDomainId: "local", ObjectKind: "task", LocalId: "does-not-exist"}, GeneratorVersion: "v1", OutputKind: "SUMMARY", MediaType: "text/plain"}
	r, e = h.Content.PrepareDerivation(ctx, actor, bad)
	if e != nil || r.Decision != v1.Decision_DECISION_REJECTED {
		t.Fatalf("nonexistent task %v %v", r, e)
	}
}

// 规则：G3、G6
func TestDeclaredMediaMustMatchActualBytes(t *testing.T) {
	ctx := context.Background()
	h, e := assembly.Open(filepath.Join(t.TempDir(), "media.db"), "alice", "local")
	if e != nil {
		t.Fatal(e)
	}
	defer h.Close()
	actor := &v1.Caller{UserId: "alice", IssuerId: "host"}
	c := original("invalid-text", []byte{255})
	c.MediaType = "text/plain"
	r, e := h.Content.Register(ctx, actor, c)
	if e != nil || r.GetError().GetCode() != "CONTENT_TYPE_MISMATCH" {
		t.Fatalf("invalid text %v %v", r, e)
	}
	c = original("invalid-json", []byte("not JSON"))
	c.MediaType = "application/json"
	r, e = h.Content.Register(ctx, actor, c)
	if e != nil || r.GetError().GetCode() != "CONTENT_TYPE_MISMATCH" {
		t.Fatalf("invalid json %v %v", r, e)
	}
}

// 规则：G3、R7
func TestEmptyByteCommandRetainsIdentityAcrossProtobufRoundTrip(t *testing.T) {
	ctx := context.Background()
	h, e := assembly.Open(filepath.Join(t.TempDir(), "empty.db"), "alice", "local")
	if e != nil {
		t.Fatal(e)
	}
	defer h.Close()
	actor := &v1.Caller{UserId: "alice", IssuerId: "host"}
	c := original("empty", []byte{})
	r, e := h.Content.Register(ctx, actor, c)
	if e != nil {
		t.Fatal(e)
	}
	wire, e := proto.Marshal(c)
	if e != nil {
		t.Fatal(e)
	}
	decoded := new(v1.RegisterContentCommand)
	if e = proto.Unmarshal(wire, decoded); e != nil {
		t.Fatal(e)
	}
	repeat, e := h.Content.Register(ctx, actor, decoded)
	if e != nil || !proto.Equal(r, repeat) {
		t.Fatalf("empty changed after wire roundtrip: %v %v", repeat, e)
	}
}

// 规则：G3、G6、G11
func TestSealedInputsAllowSupplementAndOutputCannotBeOverwritten(t *testing.T) {
	ctx := context.Background()
	h, actor, d, input := derivationFixture(t)
	extra, e := h.Content.Register(ctx, actor, original("supplement", []byte("extra")))
	if e != nil {
		t.Fatal(e)
	}
	if e = h.Content.ProcessRegistrations(ctx, actor); e != nil {
		t.Fatal(e)
	}
	read := &v1.ReadDerivationInputCommand{Header: header("capture"), DerivationRef: d.Ref, HostInstanceId: d.HostInstanceId, Generation: 1, InputRef: input}
	if _, e = h.Content.ReadDerivationInput(ctx, actor, read); e != nil {
		t.Fatal(e)
	}
	r, e := h.Content.SealDerivation(ctx, actor, &v1.SealDerivationCommand{Header: header("seal"), DerivationRef: d.Ref, HostInstanceId: d.HostInstanceId, Generation: 1, InputRefs: []*v1.Ref{input, extra.ResultRef}})
	if e != nil || r.Decision != v1.Decision_DECISION_ACCEPTED {
		t.Fatalf("supplement %v %v", r, e)
	}
	read.Header = header("sealed-read")
	read.InputRef = extra.ResultRef
	if _, e = h.Content.ReadDerivationInput(ctx, actor, read); e == nil || e.Error() != "INPUT_SET_SEALED" {
		t.Fatalf("read after seal %v", e)
	}
	c := &v1.CommitDerivationCommand{Header: header("commit"), DerivationRef: d.Ref, HostInstanceId: d.HostInstanceId, Generation: 1, Body: []byte("output")}
	r, e = h.Content.CommitDerivation(ctx, actor, c)
	if e != nil || r.Decision != v1.Decision_DECISION_ACCEPTED {
		t.Fatalf("commit %v %v", r, e)
	}
	if e = h.Content.ProcessRegistrations(ctx, actor); e != nil {
		t.Fatal(e)
	}
	output, e := h.Content.Read(ctx, actor, d.OutputRef)
	if e != nil || len(output.DerivedFrom) != 2 {
		t.Fatalf("lost supplement %v %v", output, e)
	}
	duplicate, e := h.Content.CommitDerivation(ctx, actor, c)
	if e != nil || !proto.Equal(r, duplicate) {
		t.Fatalf("repeat %v %v", duplicate, e)
	}
	c.Body = []byte("changed")
	if _, e = h.Content.CommitDerivation(ctx, actor, c); e == nil || e.Error() != "IDEMPOTENCY_CONFLICT" {
		t.Fatalf("changed same command %v", e)
	}
	c.Header = header("overwrite")
	reject, e := h.Content.CommitDerivation(ctx, actor, c)
	if e != nil || reject.Decision != v1.Decision_DECISION_REJECTED {
		t.Fatalf("overwrite %v %v", reject, e)
	}
	same, e := h.Content.Read(ctx, actor, d.OutputRef)
	if e != nil || !proto.Equal(output, same) {
		t.Fatalf("output mutated %v %v", same, e)
	}
}

// 规则：G12、R7
func TestHolderQueryRejectsForeignIdentityInsideDeposit(t *testing.T) {
	ctx := context.Background()
	h, e := assembly.Open(filepath.Join(t.TempDir(), "holder.db"), "alice", "local")
	if e != nil {
		t.Fatal(e)
	}
	defer h.Close()
	actor := &v1.Caller{UserId: "alice", IssuerId: "host"}
	r, e := h.Content.Register(ctx, actor, original("body", []byte("body")))
	if e != nil {
		t.Fatal(e)
	}
	if e = h.Content.ProcessRegistrations(ctx, actor); e != nil {
		t.Fatal(e)
	}
	reg, e := h.Content.QueryRegistration(ctx, actor, r.ResultRef)
	if e != nil {
		t.Fatal(e)
	}
	forged := proto.Clone(reg.Deposit).(*v1.BodyDeposit)
	forged.Identity.UserId = "other"
	if _, e = h.Bodies.QueryReceipt(ctx, actor, forged); e == nil || e.Error() != "PERMISSION_DENIED" {
		t.Fatalf("foreign holder identity: %v", e)
	}
}
