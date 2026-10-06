//go:build fault && darwin

package fault_test

import (
	"errors"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"

	"github.com/ruipengliu/lerna/cmd/assembly"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"github.com/ruipengliu/lerna/infra/egressio"
	"github.com/ruipengliu/lerna/infra/sqlite"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// 规则：G3、G5、G9、G11、R7
func TestNativeFileTracePreservesOriginalOwnerSourcesWithoutNativeData(t *testing.T) {
	n := newNativeScenario(t)
	a, start := n.prepare(t, "file-source", "CREATE", "", []byte("FILE-BODY-SECRET-17"))
	r, e := n.h.Egress.Invoke(n.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, start)
	requireAccepted(t, r, e)
	root, e := n.h.Content.QueryManagedFileRoot(n.ctx, n.caller, "documents")
	if e != nil || root == nil {
		t.Fatalf("root binding: %v %v", root, e)
	}
	x, e := n.h.Ledger.QueryExecution(n.ctx, n.caller, a.OperationId)
	if e != nil {
		t.Fatal(e)
	}
	resources, e := n.h.Content.QueryFileResourcesForSend(n.ctx, n.caller, x.Send.Ref)
	if e != nil || resources == nil {
		t.Fatalf("file resources: %v %v", resources, e)
	}
	sources, e := n.h.Trace.QuerySources(n.ctx, n.caller)
	if e != nil {
		t.Fatal(e)
	}
	want := map[string]*v1.Ref{"FILE_ROOT_BOUND": root.Ref, "FILE_RESOURCES_REGISTERED": resources.Ref, "FILE_RESOURCES_OBSERVED": resources.Ref}
	captured := map[string]*v1.TraceSourceRecord{}
	for _, source := range sources {
		event := source.Command.Event
		if ref := want[event.EventType]; ref != nil {
			if event.Producer != "content" || !proto.Equal(event.SourceRecordRef, ref) || !proto.Equal(event.TaskId, a.TaskId) || !proto.Equal(event.OperationId, a.OperationId) || !proto.Equal(event.AttemptId, x.Attempt.Ref.Name) || !proto.Equal(event.SendRef.Name, x.Send.Ref.Name) || event.OriginCommand == nil || event.OriginCommand.TargetDomainId != "d/content" || source.Receipt != nil {
				t.Fatalf("file source relabeled or receiver committed inside owner: %v", source)
			}
			captured[event.Ref.Name.LocalId] = source
			delete(want, event.EventType)
		}
	}
	if len(want) != 0 {
		t.Fatalf("missing original file source events: %v", want)
	}
	assertPrivate := func(message proto.Message) {
		body, e := protojson.Marshal(message)
		if e != nil {
			t.Fatal(e)
		}
		for _, secret := range []string{n.root, root.NativeIdentity, "FILE-BODY-SECRET-17", "managed://documents/report", "local-file"} {
			if strings.Contains(string(body), secret) {
				t.Fatalf("native data entered trace: %s", secret)
			}
		}
	}
	for _, source := range captured {
		assertPrivate(source)
	}
	if e = n.h.Trace.Collect(n.ctx, n.caller); e != nil {
		t.Fatal(e)
	}
	sources, e = n.h.Trace.QuerySources(n.ctx, n.caller)
	if e != nil {
		t.Fatal(e)
	}
	for _, source := range sources {
		original := captured[source.Command.Event.Ref.Name.LocalId]
		if original == nil {
			continue
		}
		accepted, e := n.h.Trace.QueryEvent(n.ctx, n.caller, original.Command.Event.Ref)
		if e != nil || !proto.Equal(accepted, original.Command.Event) || source.Receipt == nil || !proto.Equal(source.Receipt.Identity, original.Command.Header.Identity) || source.Receipt.Decision != v1.Decision_DECISION_ACCEPTED {
			t.Fatalf("independent accept/source ACK changed event identity: %v %v", source, e)
		}
	}
	if e = n.h.Trace.Index(n.ctx, n.caller); e != nil {
		t.Fatal(e)
	}
	view, e := n.h.Trace.Query(n.ctx, n.caller, &v1.TraceQuery{OperationId: a.OperationId})
	if e != nil || !view.Complete {
		t.Fatalf("file source index: %v %v", view, e)
	}
	indexed := 0
	for _, event := range view.Events {
		if original := captured[event.Ref.Name.LocalId]; original != nil {
			if !proto.Equal(event, original.Command.Event) {
				t.Fatal("index changed event")
			}
			indexed++
		}
	}
	if indexed != len(captured) {
		t.Fatal("index omitted file source")
	}
	assertPrivate(view)
	before := nativeManifest(t, n.root)
	_, e = n.h.Egress.Invoke(n.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, start)
	if e != nil {
		t.Fatal(e)
	}
	if e = n.h.Trace.Recover(n.ctx, n.caller); e != nil {
		t.Fatal(e)
	}
	after, e := n.h.Trace.Query(n.ctx, n.caller, &v1.TraceQuery{OperationId: a.OperationId})
	if e != nil || len(after.Events) != len(view.Events) || !proto.Equal(readNativePointer(t, n.root), n.observation(t, a).FileEvidence.Commit) {
		t.Fatal("replay duplicated source or publication")
	}
	now := nativeManifest(t, n.root)
	for name, body := range before {
		if now[name] != body {
			t.Fatal("trace replay rewrote target")
		}
	}
}

func readNativeExecutionSend(t *testing.T, n *nativeScenario, a *v1.Admission) *v1.Ref {
	t.Helper()
	x, e := n.h.Ledger.QueryExecution(n.ctx, n.caller, a.OperationId)
	if e != nil {
		t.Fatal(e)
	}
	return x.Send.Ref
}

// 规则：G3、G5、G9、G11、R7
func TestNativeFileResourceSourceCrashAtomicity(t *testing.T) {
	for _, mode := range []sqlite.FaultMode{sqlite.CrashBeforeCommit, sqlite.CrashAfterCommit, sqlite.LoseReceipt} {
		t.Run(string(mode), func(t *testing.T) {
			n := newNativeScenario(t)
			a, start := n.prepare(t, "resource-source", "CREATE", "", []byte("unpublished resource"))
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
			child.Env = append(os.Environ(), "LERNA_NATIVE_DB="+n.path, "LERNA_NATIVE_ROOT="+n.root, "LERNA_NATIVE_POINT=content.file_resources", "LERNA_NATIVE_MODE="+string(mode))
			output, e := child.CombinedOutput()
			if mode == sqlite.LoseReceipt {
				if e != nil {
					t.Fatalf("lost resource receipt child: %v %s", e, output)
				}
			} else {
				var exited *exec.ExitError
				if !errors.As(e, &exited) || exited.ExitCode() != sqlite.CrashExitCode {
					t.Fatalf("resource source boundary: %v %s", e, output)
				}
			}
			n.h, e = assembly.OpenWithFiles(n.path, "u", "d", map[string]string{"documents": n.root})
			if e != nil {
				t.Fatal(e)
			}
			send := readNativeExecutionSend(t, n, a)
			resources, e := n.h.Content.QueryFileResourcesForSend(n.ctx, n.caller, send)
			if e != nil {
				t.Fatal(e)
			}
			if (resources != nil) != (mode != sqlite.CrashBeforeCommit) {
				t.Fatalf("partial resource record: %v", resources)
			}
			sources, e := n.h.Trace.QuerySources(n.ctx, n.caller)
			if e != nil {
				t.Fatal(e)
			}
			count := 0
			for _, source := range sources {
				event := source.Command.Event
				if event.EventType != "FILE_RESOURCES_REGISTERED" || !proto.Equal(event.OperationId, a.OperationId) {
					continue
				}
				count++
				if resources == nil || !proto.Equal(event.SourceRecordRef, resources.Ref) || !proto.Equal(event.SendRef, resources.SendRef) || !proto.Equal(event.BodyRef, resources.ContentRef) || source.Receipt == nil || !proto.Equal(source.Receipt.Identity, source.Command.Header.Identity) {
					t.Fatalf("resource source/ACK not atomic or attributable: %v", source)
				}
				accepted, e := n.h.Trace.QueryEvent(n.ctx, n.caller, event.Ref)
				if e != nil || !proto.Equal(event, accepted) {
					t.Fatalf("resource event changed during recovery: %v %v", accepted, e)
				}
			}
			want := 0
			if resources != nil {
				want = 1
			}
			if count != want {
				t.Fatalf("source count=%d want=%d", count, want)
			}
			if len(nativeManifest(t, n.root)) != 0 {
				t.Fatal("resource registration produced native effects")
			}
			r, e := n.h.Egress.Invoke(n.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, start)
			requireAccepted(t, r, e)
			if len(nativeManifest(t, n.root)) != 0 {
				t.Fatal("resource receipt replay republished")
			}
		})
	}
}

// 规则：G3、G5、G9、G11、R7
func TestNativeFileCleanupSourceKeepsOriginalResourceAndIndependentObservation(t *testing.T) {
	n := newNativeScenario(t)
	a, start := n.prepare(t, "orphan-source", "CREATE", "", []byte("orphan"))
	ctx := egressio.WithNativeFileFault(n.ctx, &egressio.NativeFileFault{FailBefore: "object.sync.fullfsync", Error: syscall.ENOSPC})
	r, e := n.h.Egress.Invoke(ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, start)
	requireAccepted(t, r, e)
	raw := n.observation(t, a)
	original, e := n.h.Content.QueryFileResources(n.ctx, n.caller, raw.FileEvidence.ResourcesRef)
	if e != nil {
		t.Fatal(e)
	}
	cleanup, send := n.prepare(t, "cleanup-source", "CLEANUP", "", nil, original.Ref)
	r, e = n.h.Egress.Invoke(n.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, send)
	requireAccepted(t, r, e)
	observed := n.observation(t, cleanup)
	if e = n.h.Trace.Recover(n.ctx, n.caller); e != nil {
		t.Fatal(e)
	}
	view, e := n.h.Trace.Query(n.ctx, n.caller, &v1.TraceQuery{OperationId: a.OperationId})
	if e != nil || !view.Complete {
		t.Fatalf("cleanup source view: %v %v", view, e)
	}
	found := false
	for _, event := range view.Events {
		if event.EventType != "FILE_RESOURCES_CLEANED" {
			continue
		}
		found = true
		if !proto.Equal(event.SourceRecordRef, original.Ref) || !proto.Equal(event.OperationId, a.OperationId) || !proto.Equal(event.AttemptId, original.AttemptId) || !proto.Equal(event.SendRef, original.SendRef) || event.OriginCommand.GetCommandId() != "ack:"+observed.Ref.Name.LocalId {
			t.Fatalf("cleanup relabeled original resource: %v", event)
		}
		linked := false
		for _, ref := range event.RelatedRefs {
			linked = linked || proto.Equal(ref, observed.Ref)
		}
		if !linked || proto.Equal(observed.OperationId, a.OperationId) {
			t.Fatal("cleanup observation lost independent identity")
		}
	}
	if !found {
		t.Fatal("cleanup responsible transaction omitted source")
	}
}

// 规则：G3、G4、G5、G11
func TestNativeFileUseCheckPersistenceBoundaryKeepsTargetUnchanged(t *testing.T) {
	for _, mode := range []sqlite.FaultMode{sqlite.CrashBeforeCommit, sqlite.CrashAfterCommit, sqlite.LoseReceipt} {
		t.Run(string(mode), func(t *testing.T) {
			n := newNativeScenario(t)
			a, start := n.prepare(t, "file-use-boundary", "CREATE", "", []byte("no publish after uncertain authority"))
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
			child.Env = append(os.Environ(), "LERNA_NATIVE_DB="+n.path, "LERNA_NATIVE_ROOT="+n.root, "LERNA_NATIVE_POINT=tasks.file_use", "LERNA_NATIVE_MODE="+string(mode))
			output, e := child.CombinedOutput()
			if mode == sqlite.LoseReceipt {
				if e != nil {
					t.Fatalf("lost use-check child: %v %s", e, output)
				}
			} else {
				var exited *exec.ExitError
				if !errors.As(e, &exited) || exited.ExitCode() != sqlite.CrashExitCode {
					t.Fatalf("use-check boundary: %v %s", e, output)
				}
			}
			n.h, e = assembly.OpenWithFiles(n.path, "u", "d", map[string]string{"documents": n.root})
			if e != nil {
				t.Fatal(e)
			}
			r, e := n.h.Egress.Invoke(n.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, start)
			requireAccepted(t, r, e)
			for name := range nativeManifest(t, n.root) {
				if !strings.HasPrefix(name, "locks/") {
					t.Fatalf("uncertain use check/replay mutated target: %s", name)
				}
			}
			op, e := n.h.Ledger.QueryOperation(n.ctx, n.caller, a.OperationId)
			if e != nil || op.Effect.Outcome == "APPLIED" || op.Execution.Send.SendSeq != 1 {
				t.Fatalf("use check failure lost original unknown/negative responsibility: %v %v", op, e)
			}
		})
	}
}

// 规则：G3、G5、G9、R7
func TestNativeFileTraceDoesNotCopyNativeErrorText(t *testing.T) {
	n := newNativeScenario(t)
	a, start := n.prepare(t, "native-error-source", "CREATE", "", []byte("FILE-ERROR-BODY-SECRET-17"))
	secret := n.root + "/FILE-NATIVE-PATH-SECRET-17"
	ctx := egressio.WithNativeFileFault(n.ctx, &egressio.NativeFileFault{FailBefore: "object.write", Error: &os.PathError{Op: "write", Path: secret, Err: syscall.ENOSPC}})
	r, e := n.h.Egress.Invoke(ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, start)
	requireAccepted(t, r, e)
	if e = n.h.Trace.Recover(n.ctx, n.caller); e != nil {
		t.Fatal(e)
	}
	view, e := n.h.Trace.Query(n.ctx, n.caller, &v1.TraceQuery{OperationId: a.OperationId})
	if e != nil {
		t.Fatal(e)
	}
	body, e := protojson.Marshal(view)
	if e != nil {
		t.Fatal(e)
	}
	for _, marker := range []string{n.root, "FILE-NATIVE-PATH-SECRET-17", "FILE-ERROR-BODY-SECRET-17"} {
		if strings.Contains(string(body), marker) {
			t.Fatalf("native error text entered owner trace: %s", marker)
		}
	}
	raw := n.observation(t, a)
	if raw.FileEvidence.ErrorCode != "FILE_NO_SPACE" {
		t.Fatalf("native failure lost controlled classification: %v", raw)
	}
}
