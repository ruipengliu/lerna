package admission_test

import (
	"bytes"
	"testing"

	"github.com/ruipengliu/lerna/conformance/protobuf"
	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

func contentHeader(id string) *v1.CommandHeader {
	h := header(id)
	h.Identity.TargetDomainId = "d/content"
	return h
}

func captureContentGovernance(t *testing.T, samples *[]protobuf.Sample) {
	t.Helper()
	f := newFixture(t, 100, 80, false)
	register := &v1.RegisterContentCommand{Header: contentHeader("capture-content-register"), Body: []byte{0, 255, 65}, MediaType: "application/octet-stream", TaskId: f.task.Name, SourceDescriptor: &v1.ContentSourceDescriptor{Kind: "HOST_IMPORT", Locator: "fixture:protobuf-source", AcquisitionMethod: "LOCAL_IMPORT", ProviderVersion: "h1-v1", SourceTimeUnixMs: 1}}
	receipt, err := f.h.Content.Register(f.ctx, f.caller, register)
	accepted(t, receipt, err)
	captureObject(t, samples, "content-register-command", register, nil)
	ref := receipt.ResultRef
	staged, err := f.h.Content.QueryRegistration(f.ctx, f.caller, ref)
	captureObject(t, samples, "content-registration-staged", staged, err)
	if staged.State != "STAGED" || len(staged.StagedBody) != 0 {
		t.Fatal(staged)
	}
	if _, err = f.h.Content.Read(f.ctx, f.caller, ref); err == nil || err.Error() != "CONTENT_UNUSABLE" {
		t.Fatalf("staged content visible: %v", err)
	}
	if err = f.h.Content.ProcessRegistrations(f.ctx, f.caller); err != nil {
		t.Fatal(err)
	}
	published, err := f.h.Content.QueryRegistration(f.ctx, f.caller, ref)
	captureObject(t, samples, "content-registration-published", published, err)
	holder, err := f.h.Bodies.QueryReceipt(f.ctx, f.caller, published.Deposit)
	captureObject(t, samples, "content-body-receipt", holder, err)
	if published.State != "PUBLISHED" || !proto.Equal(holder, published.BodyReceipt) {
		t.Fatal("source and body-holder receipts disagree")
	}
	original, err := f.h.Content.Read(f.ctx, f.caller, ref)
	captureObject(t, samples, "content-version-one", original, err)
	if !bytes.Equal(command.ContentBytes(original), register.Body) || original.ContentVersion != 1 {
		t.Fatal(original)
	}
	update := proto.Clone(register).(*v1.RegisterContentCommand)
	update.Header = contentHeader("capture-content-update")
	update.PreviousVersionRef = ref
	update.Body = []byte("updated source")
	receipt, err = f.h.Content.Register(f.ctx, f.caller, update)
	accepted(t, receipt, err)
	captureObject(t, samples, "content-update-command", update, nil)
	if err = f.h.Content.ProcessRegistrations(f.ctx, f.caller); err != nil {
		t.Fatal(err)
	}
	versionTwo, err := f.h.Content.Read(f.ctx, f.caller, receipt.ResultRef)
	captureObject(t, samples, "content-version-two", versionTwo, err)
	historical, err := f.h.Content.Read(f.ctx, f.caller, ref)
	if err != nil || !proto.Equal(historical, original) || versionTwo.ContentId != original.ContentId || versionTwo.ContentVersion != 2 || !proto.Equal(versionTwo.PreviousVersionRefs[0], original.Ref) {
		t.Fatalf("version history: %v %v", versionTwo, err)
	}

	prepare := &v1.PrepareDerivationCommand{Header: contentHeader("capture-derive-prepare"), TaskId: f.task.Name, GeneratorVersion: "h1-copy-v1", OutputKind: "CONTEXT", MediaType: "application/octet-stream"}
	receipt, err = f.h.Content.PrepareDerivation(f.ctx, f.caller, prepare)
	accepted(t, receipt, err)
	captureObject(t, samples, "content-prepare-command", prepare, nil)
	derivation, err := f.h.Content.QueryDerivation(f.ctx, f.caller, receipt.ResultRef)
	captureObject(t, samples, "content-derivation-prepared", derivation, err)
	read := &v1.ReadDerivationInputCommand{Header: contentHeader("capture-derive-read"), DerivationRef: derivation.Ref, HostInstanceId: derivation.HostInstanceId, Generation: derivation.Generation, InputRef: original.Ref}
	input, err := f.h.Content.ReadDerivationInput(f.ctx, f.caller, read)
	captureObject(t, samples, "content-derivation-input", input, err)
	captureObject(t, samples, "content-read-input-command", read, nil)
	inputBytes := append([]byte{}, command.ContentBytes(input)...)
	takeover := &v1.TakeoverDerivationCommand{Header: contentHeader("capture-derive-takeover"), DerivationRef: derivation.Ref, ExpectedGeneration: derivation.Generation}
	receipt, err = f.h.Content.TakeoverDerivation(f.ctx, f.caller, takeover)
	accepted(t, receipt, err)
	captureObject(t, samples, "content-takeover-command", takeover, nil)
	successor, err := f.h.Content.QueryDerivation(f.ctx, f.caller, derivation.Ref)
	captureObject(t, samples, "content-derivation-taken-over", successor, err)
	if successor.Generation != 2 || successor.HostInstanceId == derivation.HostInstanceId || len(successor.ActualInputRefs) != 1 || !proto.Equal(successor.ActualInputRefs[0], original.Ref) {
		t.Fatal(successor)
	}
	if _, err = f.h.Content.ReadDerivationInput(f.ctx, f.caller, read); err == nil || err.Error() != "STALE_PRODUCER" {
		t.Fatalf("old producer admitted: %v", err)
	}
	// 新实例经当前围栏重新读取实际输入；封闭与输出只使用该读取的字节。
	readNew := proto.Clone(read).(*v1.ReadDerivationInputCommand)
	readNew.Header = contentHeader("capture-derive-read-new")
	readNew.HostInstanceId = successor.HostInstanceId
	readNew.Generation = successor.Generation
	input, err = f.h.Content.ReadDerivationInput(f.ctx, f.caller, readNew)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(inputBytes, command.ContentBytes(input)) {
		t.Fatal("takeover changed input version")
	}
	seal := &v1.SealDerivationCommand{Header: contentHeader("capture-derive-seal"), DerivationRef: successor.Ref, HostInstanceId: successor.HostInstanceId, Generation: successor.Generation, InputRefs: []*v1.Ref{original.Ref}}
	receipt, err = f.h.Content.SealDerivation(f.ctx, f.caller, seal)
	accepted(t, receipt, err)
	captureObject(t, samples, "content-seal-command", seal, nil)
	sealed, err := f.h.Content.QueryDerivation(f.ctx, f.caller, successor.Ref)
	captureObject(t, samples, "content-derivation-sealed", sealed, err)
	commit := &v1.CommitDerivationCommand{Header: contentHeader("capture-derive-commit"), DerivationRef: successor.Ref, HostInstanceId: successor.HostInstanceId, Generation: successor.Generation, Body: command.ContentBytes(input)}
	receipt, err = f.h.Content.CommitDerivation(f.ctx, f.caller, commit)
	accepted(t, receipt, err)
	captureObject(t, samples, "content-commit-command", commit, nil)
	stagedOutput, err := f.h.Content.QueryDerivation(f.ctx, f.caller, successor.Ref)
	captureObject(t, samples, "content-derivation-staged", stagedOutput, err)
	if _, err = f.h.Content.Read(f.ctx, f.caller, successor.OutputRef); err == nil {
		t.Fatal("staged derived body visible")
	}
	if err = f.h.Content.ProcessRegistrations(f.ctx, f.caller); err != nil {
		t.Fatal(err)
	}
	output, err := f.h.Content.Read(f.ctx, f.caller, successor.OutputRef)
	captureObject(t, samples, "content-derived-published", output, err)
	committed, err := f.h.Content.QueryDerivation(f.ctx, f.caller, successor.Ref)
	captureObject(t, samples, "content-derivation-committed", committed, err)
	if committed.State != "COMMITTED" || committed.BodyReceipt == nil || output.SourceDescriptor.Kind != "DERIVED" || len(output.DerivedFrom) != 1 || !proto.Equal(output.DerivedFrom[0], original.Ref) || !proto.Equal(output.ProducerRef, successor.Ref) || !bytes.Equal(command.ContentBytes(output), inputBytes) {
		t.Fatal(output)
	}
	deleteCommand := &v1.DeleteContentCommand{Header: contentHeader("capture-delete-unsupported"), ContentRef: output.Ref}
	if _, err = f.h.Content.Delete(f.ctx, f.caller, deleteCommand); err == nil || err.Error() != "UNSUPPORTED" {
		t.Fatalf("unexpected deletion: %v", err)
	}
	captureObject(t, samples, "content-delete-unsupported-command", deleteCommand, nil)
	after, err := f.h.Content.Read(f.ctx, f.caller, output.Ref)
	if err != nil || !proto.Equal(after, output) || f.calls.Load() != 0 {
		t.Fatal("content-only capture changed body or performed external call")
	}
}
