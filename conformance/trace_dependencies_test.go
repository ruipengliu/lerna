package conformance_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ruipengliu/lerna/cmd/assembly"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"github.com/ruipengliu/lerna/core/durable"
	"github.com/ruipengliu/lerna/core/ledger"
	"github.com/ruipengliu/lerna/core/trace"
	"github.com/ruipengliu/lerna/infra/sqlite"
	"google.golang.org/protobuf/proto"
)

// 仅通过运行记录 Store 的声明方法接入生产存储。
type declaredTraceStore struct{ trace.Store }

var (
	_ trace.Store   = (*sqlite.Store)(nil)
	_ trace.Work    = (*durable.Service)(nil)
	_ trace.Source  = (*ledger.Service)(nil)
	_ durable.Store = (*sqlite.TraceWork)(nil)
)

// 规则：G3、G11、R3、R6、R7
func TestDeclaredTraceAdapterCollectsOriginalSourcesAndIndependentIndex(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "trace.db")
	h, err := assembly.Open(path, "alice", "local")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = h.Close() })
	caller := &v1.Caller{UserId: "alice", IssuerId: "cli"}
	goal := &v1.SubmitGoalCommand{Identity: &v1.CommandIdentity{UserId: "alice", IssuerId: "cli", TargetDomainId: "local", CommandId: "declared-trace-goal"}, ContractVersion: 1, SchemaId: "lerna.v1.SubmitGoal", FingerprintVersion: 1, Goal: "synthetic goal"}
	if _, err = h.Sessions.SubmitGoal(ctx, caller, goal); err != nil {
		t.Fatal(err)
	}
	if err = h.Sessions.ProcessPending(ctx, caller); err != nil {
		t.Fatal(err)
	}
	receipt, err := h.Durable.QueryReceipt(ctx, caller, goal.Identity)
	if err != nil {
		t.Fatal(err)
	}
	task, err := h.Tasks.QueryTask(ctx, caller, receipt.Receipt.TaskRef.Name)
	if err != nil {
		t.Fatal(err)
	}
	store, err := sqlite.Open(path, "alice", "local")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	service, err := trace.New(declaredTraceStore{store}, durable.New(store.TraceWork(), "alice", "local/trace"), h.Ledger, "alice", "local/trace")
	if err != nil {
		t.Fatal(err)
	}
	if err = service.ValidateDependencies(); err != nil {
		t.Fatal(err)
	}
	query := &v1.TraceQuery{TaskId: receipt.Receipt.TaskRef.Name}
	before, err := service.Query(ctx, caller, query)
	if err != nil || before.Complete || before.Backlog == 0 {
		t.Fatalf("original source backlog: %v %v", before, err)
	}
	if err = service.Collect(ctx, caller); err != nil {
		t.Fatal(err)
	}
	sources, err := service.QuerySources(ctx, caller)
	if err != nil || len(sources) == 0 {
		t.Fatalf("original sources: %v %v", sources, err)
	}
	source := sources[0]
	actor := &v1.Caller{UserId: "alice", IssuerId: source.Command.Header.Identity.IssuerId}
	acceptance, err := service.QueryReceipt(ctx, actor, source.Command.Header.Identity)
	if err != nil || source.Receipt == nil || !proto.Equal(acceptance.Receipt, source.Receipt) {
		t.Fatalf("original source acknowledgement: %v %v", acceptance, err)
	}
	event, err := service.QueryEvent(ctx, caller, source.Command.Event.Ref)
	if err != nil || source.Command.Event.SourceSeq == 0 || !proto.Equal(event, source.Command.Event) {
		t.Fatalf("original event: %v %v", event, err)
	}
	collected, err := service.QueryMetrics(ctx, caller)
	if err != nil || collected.Accepted != uint64(len(sources)) || collected.Indexed != 0 || collected.PendingAcknowledgements != 0 || collected.IndexBacklog != uint64(len(sources)) || collected.CoverageComplete {
		t.Fatalf("collection is independent of index: %v %v", collected, err)
	}
	if err = service.Index(ctx, caller); err != nil {
		t.Fatal(err)
	}
	indexed, err := service.Query(ctx, caller, query)
	if err != nil || !indexed.Complete || indexed.Backlog != 0 || indexed.IndexBacklog != 0 {
		t.Fatalf("indexed source cutoff: %v %v", indexed, err)
	}
	if err = service.Recover(ctx, caller); err != nil {
		t.Fatal(err)
	}
	repeated, err := service.Query(ctx, caller, query)
	if err != nil || !proto.Equal(repeated, indexed) {
		t.Fatalf("original recovery: %v %v", repeated, err)
	}
	after, err := h.Tasks.QueryTask(ctx, caller, receipt.Receipt.TaskRef.Name)
	if err != nil || !proto.Equal(after, task) {
		t.Fatalf("trace changed owner facts: %v %v", after, err)
	}
}

// 规则：G7、R7
func TestTraceCannotAcceptUnversionedExtensionPayload(t *testing.T) {
	h, err := assembly.Open(filepath.Join(t.TempDir(), "extension.db"), "u", "d")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = h.Close() })
	if _, err = h.Trace.Accept(context.Background(), &v1.Caller{UserId: "u", IssuerId: "extension"}, &v1.AcceptTraceCommand{}); err == nil {
		t.Fatal("unsupported trace accepted")
	}
}

// 规则：R6、G1
func TestTraceConstructorRejectsMissingStoreWorkAndSource(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dependencies.db")
	h, err := assembly.Open(path, "alice", "local")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = h.Close() })
	store, err := sqlite.Open(path, "alice", "local")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	work := durable.New(store.TraceWork(), "alice", "local/trace")
	var nilStore *sqlite.Store
	var nilWork *durable.Service
	var nilSource *ledger.Service
	for _, test := range []struct {
		name   string
		store  trace.Store
		work   trace.Work
		source trace.Source
		want   string
	}{
		{name: "nil-store", work: work, source: h.Ledger, want: "trace.store"},
		{name: "typed-nil-store", store: nilStore, work: work, source: h.Ledger, want: "trace.store"},
		{name: "nil-work", store: store, source: h.Ledger, want: "trace.work"},
		{name: "typed-nil-work", store: store, work: nilWork, source: h.Ledger, want: "trace.work"},
		{name: "nil-source", store: store, work: work, want: "trace.source"},
		{name: "typed-nil-source", store: store, work: work, source: nilSource, want: "trace.source"},
	} {
		t.Run(test.name, func(t *testing.T) {
			service, err := trace.New(test.store, test.work, test.source, "alice", "local/trace")
			if service != nil || err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("constructor: %v %v", service, err)
			}
		})
	}
}
