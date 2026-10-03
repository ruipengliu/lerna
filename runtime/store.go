// Package runtime 共享持久责任原语；业务成功、外部效果及重试由领域裁决。
package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/ruipengliu/lerna/api"
)

type CommitStatus string

const (
	Committed     CommitStatus = "committed"
	RolledBack    CommitStatus = "rolled_back"
	CommitUnknown CommitStatus = "commit_unknown"
)

var (
	ErrNotFound      = api.E("not_found", "object_not_found")
	ErrConflict      = api.E("revision_conflict", "revision_changed")
	ErrClaimLost     = errors.New("claim is no longer valid")
	ErrCommitUnknown = errors.New("commit result unknown; query original identity")
)

type Scope struct {
	TenantID   string
	OwnerID    string
	DatabaseID string
}

func (s Scope) Ref(id string, revision uint64) api.ObjectRef {
	return api.ObjectRef{TenantID: s.TenantID, OwnerID: s.OwnerID, ObjectID: id, Revision: revision}
}

type Auth struct {
	TenantID             string
	SubjectID            string
	CredentialGeneration uint64
	Roles                []string
}

func (a Auth) HasRole(role string) bool {
	for _, r := range a.Roles {
		if r == role {
			return true
		}
	}
	return false
}
func (a Auth) Ref(owner string) api.ObjectRef {
	return api.ObjectRef{TenantID: a.TenantID, OwnerID: owner, ObjectID: a.SubjectID, Revision: a.CredentialGeneration}
}

type Record struct {
	ID       string
	Revision uint64
	ParentID string
	Data     json.RawMessage
}

func (r Record) Decode(v any) error { return json.Unmarshal(r.Data, v) }

type SemanticKey struct {
	ObjectID string
	Digest   string
}
type StoredCommand struct {
	Command     api.Command
	PrincipalID string
	Digest      string
	Receipt     api.Receipt
	Tombstone   bool
}
type Work struct {
	Job   api.Job
	Claim api.Claim
}
type Disposition struct {
	State string
	DueAt time.Time
}

func Done() Disposition                { return Disposition{State: "done"} }
func Waiting(at time.Time) Disposition { return Disposition{State: "waiting", DueAt: at} }
func Ready(at time.Time) Disposition   { return Disposition{State: "ready", DueAt: at} }

// Tx 不可跨 tenant/owner/database，participants 是受信宿主声明的 namespace 根。
type Tx interface {
	Scope() Scope
	Now(context.Context) (time.Time, error)
	Get(context.Context, string, string, any) (uint64, error)
	GetVersion(context.Context, string, string, uint64, any) error
	Create(context.Context, string, string, string, any) error
	Put(context.Context, string, string, uint64, any) error
	List(context.Context, string, string, string, int) ([]Record, error)
	Bind(context.Context, string, string, string, string) error
	LookupKey(context.Context, string, string) (SemanticKey, error)
	Savepoint(context.Context, func(Tx) error) error
	LoadCommand(context.Context, string) (StoredCommand, error)
	SaveCommand(context.Context, StoredCommand) error
	Raise(context.Context, string, string, api.ObjectRef, time.Time) (api.Job, error)
	Hint(context.Context, string, time.Time) error
	Guard(context.Context, api.Claim) error
	Finish(context.Context, api.Claim, Disposition) error
}
type Store interface {
	ID() string
	Within(context.Context, Scope, []string, func(Tx) error) (CommitStatus, error)
	Read(context.Context, Scope, string, string, uint64, any) (uint64, error)
	List(context.Context, Scope, string, string, string, int) ([]Record, error)
	LookupCommand(context.Context, Scope, string) (StoredCommand, error)
	Claim(context.Context, Scope, string, []string, int, time.Duration) ([]Work, CommitStatus, error)
	Renew(context.Context, Scope, api.Claim, time.Duration) (api.Claim, CommitStatus, error)
	CheckClaim(context.Context, Scope, api.Claim) error
	Close() error
}

func Finish(ctx context.Context, store Store, scope Scope, participants []string, work Work, disposition Disposition, fn func(Tx) error) error {
	status, err := store.Within(ctx, scope, participants, func(tx Tx) error {
		if fn != nil {
			if err := fn(tx); err != nil {
				return err
			}
		}
		if err := tx.Guard(ctx, work.Claim); err != nil {
			return err
		}
		return tx.Finish(ctx, work.Claim, disposition)
	})
	if status == CommitUnknown {
		return ErrCommitUnknown
	}
	return err
}

// CheckRef 验证准确引用；不把引用本身当读取或行动许可。
func CheckRef(scope Scope, ref api.ObjectRef) error {
	if ref.TenantID != scope.TenantID || !api.ValidID(ref.OwnerID) || !api.ValidID(ref.ObjectID) || ref.Revision == 0 {
		return api.E("forbidden", "reference_scope_mismatch")
	}
	return nil
}
