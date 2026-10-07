package content_test

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ruipengliu/lerna/cmd/assembly"
	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"github.com/ruipengliu/lerna/core/content"
	"github.com/ruipengliu/lerna/core/durable"
	"github.com/ruipengliu/lerna/core/ledger"
	"github.com/ruipengliu/lerna/core/tasks"
	"github.com/ruipengliu/lerna/infra/sqlite"
	"google.golang.org/protobuf/proto"
)

// 仅暴露消费方声明的 Store 方法，不提升 SQLite 的其他能力。
type declaredContentStore struct{ content.Store }

var (
	_ content.Store           = (*sqlite.Store)(nil)
	_ content.ObservationWork = (*durable.Service)(nil)
	_ durable.Store           = (*sqlite.ContentWork)(nil)
)

// 规则：G3、G11、R6、R7
func TestDeclaredContentAdapterPublishesOriginalBodyAndReceipt(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "content.db")
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
	work, err := durable.New(store.ContentWork(), "alice", "local/content")
	if err != nil {
		t.Fatal(err)
	}
	service, err := content.New(declaredContentStore{store}, work, "alice", "local/content")
	if err != nil {
		t.Fatal(err)
	}
	service.WithAssociations(h.Tasks).WithObservations(h.Ledger)
	if err = service.ValidateDependencies(); err != nil {
		t.Fatal(err)
	}
	actor := &v1.Caller{UserId: "alice", IssuerId: "host"}
	c := original("declared-adapter", []byte{0, 255, 65})
	receipt, err := service.Register(ctx, actor, c)
	if err != nil || receipt.GetDecision() != v1.Decision_DECISION_ACCEPTED {
		t.Fatalf("register: %v %v", receipt, err)
	}
	if err = service.ProcessRegistrations(ctx, actor); err != nil {
		t.Fatal(err)
	}
	got, err := service.Read(ctx, actor, receipt.ResultRef)
	if err != nil || !bytes.Equal(command.ContentBytes(got), []byte{0, 255, 65}) || !proto.Equal(got.Source, c.Header.Identity) {
		t.Fatalf("published original body: %v %v", got, err)
	}
	repeated, err := service.Register(ctx, actor, c)
	if err != nil || !proto.Equal(receipt, repeated) {
		t.Fatalf("original receipt: %v %v", repeated, err)
	}
}

// 规则：R6、G1
func TestContentCompletionRequiresEveryAssociationAndObservationLink(t *testing.T) {
	path := filepath.Join(t.TempDir(), "links.db")
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
	work, err := durable.New(store.ContentWork(), "alice", "local/content")
	if err != nil {
		t.Fatal(err)
	}
	service, err := content.New(store, work, "alice", "local/content")
	if err != nil {
		t.Fatal(err)
	}
	var nilFacts *tasks.Service
	var nilLedger *ledger.Service
	for _, test := range []struct {
		name   string
		facts  content.AssociationFacts
		ledger content.ObservationLedger
		want   string
	}{
		{name: "missing-associations", want: "content.associations"},
		{name: "typed-nil-associations", facts: nilFacts, ledger: h.Ledger, want: "content.associations"},
		{name: "missing-ledger", facts: h.Tasks, want: "content.ledger"},
		{name: "typed-nil-ledger", facts: h.Tasks, ledger: nilLedger, want: "content.ledger"},
		{name: "complete", facts: h.Tasks, ledger: h.Ledger},
	} {
		t.Run(test.name, func(t *testing.T) {
			service.WithAssociations(test.facts).WithObservations(test.ledger)
			err := service.ValidateDependencies()
			if test.want == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("completion: %v", err)
			}
		})
	}
}

// 规则：R6、G1
func TestContentConstructorRejectsMissingBaseDependencies(t *testing.T) {
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "dependencies.db"), "alice", "local")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	work, err := durable.New(store.ContentWork(), "alice", "local/content")
	if err != nil {
		t.Fatal(err)
	}
	var nilStore *sqlite.Store
	var nilWork *durable.Service
	for _, test := range []struct {
		name  string
		store content.Store
		work  content.ObservationWork
		want  string
	}{
		{name: "nil-store", work: work, want: "content.store"},
		{name: "typed-nil-store", store: nilStore, work: work, want: "content.store"},
		{name: "nil-work", store: store, want: "content.work"},
		{name: "typed-nil-work", store: store, work: nilWork, want: "content.work"},
	} {
		t.Run(test.name, func(t *testing.T) {
			service, err := content.New(test.store, test.work, "alice", "local/content")
			if service != nil || err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("constructor: %v %v", service, err)
			}
		})
	}
}
