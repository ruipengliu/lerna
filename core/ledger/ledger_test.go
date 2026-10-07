package ledger

import (
	"context"
	"testing"

	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
)

type factsStore struct {
	Store
	operation *v1.Operation
}

func (s *factsStore) LoadOperation(context.Context, *v1.GlobalName) (*v1.Operation, error) {
	return s.operation, nil
}

// 规则：G1、准入-5
func TestUnknownAndPreviouslyStartedUnsettledOperationsBlockReplacement(t *testing.T) {
	ref := &v1.Ref{Name: &v1.GlobalName{UserId: "u", AuthorityDomainId: "d/ledger", ObjectKind: "operation", LocalId: "o"}, Revision: 1, SchemaId: "lerna.v1.Operation"}
	for _, tc := range []struct {
		name      string
		operation *v1.Operation
		previous  bool
		blocked   bool
	}{
		{"unknown", &v1.Operation{Lifecycle: "ACTIVE", Effect: &v1.Effect{Outcome: "UNKNOWN"}}, false, true},
		{"late-possible", &v1.Operation{Lifecycle: "ACTIVE", Effect: &v1.Effect{Outcome: "NOT_APPLIED", LateEffect: "MAY_OCCUR"}}, false, true},
		{"conflict", &v1.Operation{Lifecycle: "ACTIVE", Effect: &v1.Effect{Outcome: "APPLIED", EvidenceConflict: true}}, false, true},
		{"rejected-round-started", &v1.Operation{Lifecycle: "ACTIVE", StartReceiptObtained: true, Effect: &v1.Effect{Outcome: "APPLIED"}}, true, true},
		{"settled", &v1.Operation{Lifecycle: "SETTLED", StartReceiptObtained: true, Effect: &v1.Effect{Outcome: "APPLIED"}}, true, false},
		{"accepted-no-dispatch", &v1.Operation{Lifecycle: "ACCEPTED", Effect: &v1.Effect{Outcome: "NOT_APPLIED"}}, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, e := New(&factsStore{operation: tc.operation}, "u", "d/ledger", "d")
			if e != nil {
				t.Fatal(e)
			}
			var previous []*v1.Ref
			if tc.previous {
				previous = []*v1.Ref{ref}
			}
			blocked, e := s.BlocksAdmission(context.Background(), &v1.Caller{UserId: "u", IssuerId: "host"}, []*v1.Ref{ref}, previous)
			if e != nil || blocked != tc.blocked {
				t.Fatalf("blocked=%v err=%v", blocked, e)
			}
		})
	}
}
