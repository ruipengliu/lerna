package grants

import (
	"context"
	"testing"

	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
)

type grantStore struct {
	Store
	grant *v1.Grant
	uses  []*v1.GrantUse
}

func (s *grantStore) LoadGrant(context.Context, *v1.Ref) (*v1.Grant, error) { return s.grant, nil }
func (s *grantStore) Position(context.Context) (uint64, int64, error)       { return 1, 100, nil }
func (s *grantStore) SaveGrantUse(_ context.Context, u *v1.GrantUse) error {
	s.uses = append(s.uses, u)
	return nil
}

// 规则：G7、G8、准入-7
func TestRevokedOrReadOnlyGrantCannotAuthorizeInvocation(t *testing.T) {
	for _, tc := range []struct{ status, right string }{{"REVOKED", "INVOKE"}, {"ACTIVE", "READ"}} {
		t.Run(tc.status+tc.right, func(t *testing.T) {
			ref := &v1.Ref{Name: &v1.GlobalName{UserId: "u", AuthorityDomainId: "d", ObjectKind: "grant", LocalId: "g"}, Revision: 1, SchemaId: "lerna.v1.Grant"}
			task := &v1.GlobalName{UserId: "u", AuthorityDomainId: "d", ObjectKind: "task", LocalId: "t"}
			store := &grantStore{grant: &v1.Grant{Ref: ref, Subject: task, Status: tc.status, SemanticVersion: 1, UseMode: "CONTINUOUS", UsePoolId: "root", Issuer: &v1.CommandIdentity{IssuerId: "host"}, ValidFromUnixMs: 1, ValidUntilUnixMs: 200, Permissions: []*v1.PermissionClause{{Action: "CREATE", Resource: "r", UseRight: tc.right, ProcessingPurpose: "CURRENT_TASK", ExecutorEndpointId: "e"}}}}
			use, _, e := New(store, nil, "u", "d", "host").OccupyInTransaction(context.Background(), ref, task, &v1.GlobalName{LocalId: "o"}, nil, &v1.Capability{Action: "CREATE", Resource: "r", UseRight: "INVOKE", ProcessingPurpose: "CURRENT_TASK", ExecutorEndpointId: "e"}, nil)
			if e == nil || use != nil || len(store.uses) != 0 {
				t.Fatalf("authorization escaped: %v %v %v", use, e, store.uses)
			}
		})
	}
}
