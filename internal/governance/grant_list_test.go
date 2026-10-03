package governance_test

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/adapters/platform"
	"github.com/ruipengliu/lerna/adapters/postgres"
	"github.com/ruipengliu/lerna/adapters/sqlite"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/governance"
	"github.com/ruipengliu/lerna/runtime"
)

// Current credential source is the real platform ledger initialized through its
// trusted management port. This consumer-side gate performs only same-Tx reads.
type listCredential struct {
	Revision   uint64   `json:"revision"`
	SubjectID  string   `json:"subject_id"`
	Generation uint64   `json:"generation"`
	State      string   `json:"state"`
	Roles      []string `json:"roles"`
}
type listMetadataAuthority struct{}

func (listMetadataAuthority) VisibilityTx(ctx context.Context, tx runtime.Tx, auth runtime.Auth) (string, error) {
	var c listCredential
	if _, err := tx.Get(ctx, "platform.credentials", auth.SubjectID, &c); err != nil {
		return "", err
	}
	actual := append([]string{}, c.Roles...)
	claimed := append([]string{}, auth.Roles...)
	sort.Strings(actual)
	sort.Strings(claimed)
	if c.SubjectID != auth.SubjectID || c.State != "active" || c.Generation != auth.CredentialGeneration || !api.Equal(actual, claimed) {
		return "", api.E("forbidden", "current_credential_changed")
	}
	c.Roles = actual
	return api.Digest(c)
}
func grantListFixture(t *testing.T) *fixture {
	t.Helper()
	f := environment(t, governance.Options{GrantMetadataGate: listMetadataAuthority{}, Participants: []string{"platform"}})
	i := platform.DevIdentity{Store: f.store, OwnerID: f.scope.OwnerID, SessionTTL: time.Hour, Principals: []platform.Principal{{Auth: f.auth, TokenHash: api.Hash([]byte("non-secret grant-list fixture identity"))}}}
	if err := i.Initialize(f.ctx); err != nil {
		t.Fatal(err)
	}
	return f
}
func setListCredential(t *testing.T, f *fixture, next runtime.Auth, state string) {
	t.Helper()
	status, err := f.store.Within(f.ctx, f.scope, []string{"platform"}, func(tx runtime.Tx) error {
		var c listCredential
		revision, err := tx.Get(f.ctx, "platform.credentials", next.SubjectID, &c)
		if err != nil {
			return err
		}
		return tx.Put(f.ctx, "platform.credentials", next.SubjectID, revision, listCredential{revision + 1, next.SubjectID, next.CredentialGeneration, state, next.Roles})
	})
	if err != nil || status != runtime.Committed {
		t.Fatalf("trusted current-identity change: %s %v", status, err)
	}
}

// 大集合使用公开受信 ProvisionGrantTx seam；它不冒充用户 Confirmation。
func provisionListGrant(t *testing.T, f *fixture, subject api.ObjectRef) api.Grant {
	t.Helper()
	g := api.Grant{GrantID: api.NewID("grant"), OwnerID: f.scope.OwnerID, Revision: 1, SubjectRef: subject,
		Resources: []string{"document"}, Actions: []string{"write"}, Purposes: []string{"save"}, Recipients: []string{"executor"}, Locations: []string{"device"},
		Mode: "continuous", State: "active", NotBefore: api.Time(time.Now().Add(-time.Minute)), ExpiresAt: api.Time(time.Now().Add(time.Hour)), Limits: []api.Amount{{Unit: "USD", Value: "10"}}}
	status, err := f.store.Within(f.ctx, f.scope, []string{governance.Namespace}, func(tx runtime.Tx) error {
		return f.svc.ProvisionGrantTx(f.ctx, tx, f.auth, g)
	})
	if err != nil || status != runtime.Committed {
		t.Fatalf("trusted initial grant: %s %v", status, err)
	}
	return g
}

func TestGrantListExposesCurrentOwnMetadataAndScopedAuthority(t *testing.T) {
	f := grantListFixture(t)
	own := provisionListGrant(t, f, f.auth.Ref(f.scope.OwnerID))
	other := f.auth.Ref(f.scope.OwnerID)
	other.ObjectID = api.NewID("user")
	provisionListGrant(t, f, other)
	old := f.auth.Ref(f.scope.OwnerID)
	old.Revision++
	provisionListGrant(t, f, old)
	page := query[api.Page[governance.GrantRecord]](t, f, "grant.list", api.ListInput{Limit: 100})
	if len(page.Items) != 3 || !page.Exhausted || page.Partial || len(page.Gaps) != 0 || page.CollectionRevision != 7 {
		t.Fatalf("authority metadata page: %+v", page)
	}
	f.auth.Roles = []string{}
	setListCredential(t, f, f.auth, "active")
	page = query[api.Page[governance.GrantRecord]](t, f, "grant.list", api.ListInput{Limit: 100})
	if len(page.Items) != 1 || page.Items[0].Grant.GrantID != own.GrantID || len(page.Items[0].Reserved) != 0 || len(page.Items[0].Spent) != 0 || page.Items[0].OnceConsumed {
		t.Fatalf("ordinary metadata leaked foreign grant: %+v", page)
	}
}

func listQuery(f *fixture, in api.ListInput) api.Query {
	return api.Query{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: f.scope.OwnerID, QueryID: api.NewID("query"), Method: "grant.list", TargetID: f.scope.OwnerID, Payload: api.Raw(in)}
}
func readListPage(t *testing.T, f *fixture, q api.Query) api.Page[governance.GrantRecord] {
	t.Helper()
	raw, err := f.dispatcher.Query(f.ctx, f.auth, api.Raw(q))
	if err != nil {
		t.Fatal(err)
	}
	var page api.Page[governance.GrantRecord]
	if err = api.Decode(raw, &page); err != nil {
		t.Fatal(err)
	}
	return page
}
func requireListFailure(t *testing.T, f *fixture, q api.Query, code string) {
	t.Helper()
	raw, err := f.dispatcher.Query(f.ctx, f.auth, api.Raw(q))
	if len(raw) != 0 || !api.IsCode(err, code) {
		t.Fatalf("grant list failure %s: reply=%s error=%v", code, raw, err)
	}
}

func TestGrantListPagesKeepOriginalIdentityAndRejectCursorScopeChanges(t *testing.T) {
	f := grantListFixture(t)
	expected := map[string]bool{}
	// A true >100 collection remains below the independent metadata byte bound.
	for i := 0; i < 205; i++ {
		g := provisionListGrant(t, f, f.auth.Ref(f.scope.OwnerID))
		expected[g.GrantID] = true
	}
	firstQuery := listQuery(f, api.ListInput{Limit: 100})
	first := readListPage(t, f, firstQuery)
	if len(first.Items) != 100 || first.Exhausted || first.NextCursor == "" || first.CollectionRevision != 411 {
		t.Fatalf("first bounded page %+v", first)
	}
	if repeated := readListPage(t, f, firstQuery); !api.Equal(first, repeated) {
		t.Fatal("original query changed its cursor or snapshot")
	}
	seen := map[string]bool{}
	page := first
	for pass := 0; pass < 3; pass++ {
		for _, item := range page.Items {
			id := item.Grant.GrantID
			if seen[id] || !expected[id] {
				t.Fatal("duplicate or foreign grant in exact pages")
			}
			seen[id] = true
		}
		if page.Exhausted {
			break
		}
		page = readListPage(t, f, listQuery(f, api.ListInput{Limit: 100, Cursor: page.NextCursor}))
		if page.CollectionRevision != first.CollectionRevision {
			t.Fatal("cursor moved to another snapshot")
		}
	}
	if !page.Exhausted || page.NextCursor != "" || len(seen) != len(expected) {
		t.Fatal("bounded paging lost the original full collection")
	}
	requireListFailure(t, f, listQuery(f, api.ListInput{Limit: 99, Cursor: first.NextCursor}), "cursor_expired")
	requireListFailure(t, f, listQuery(f, api.ListInput{Limit: 100, Cursor: first.NextCursor + "x"}), "cursor_expired")
	original := f.auth
	other := original
	other.SubjectID = api.NewID("user")
	i := platform.DevIdentity{Store: f.store, OwnerID: f.scope.OwnerID, SessionTTL: time.Hour, Principals: []platform.Principal{{Auth: other, TokenHash: api.Hash([]byte("non-secret other authenticated subject"))}}}
	if err := i.Initialize(f.ctx); err != nil {
		t.Fatal(err)
	}
	f.auth = other
	requireListFailure(t, f, listQuery(f, api.ListInput{Limit: 100, Cursor: first.NextCursor}), "cursor_expired")
	f.auth = original
	f.auth.TenantID = api.NewID("tenant")
	q := listQuery(f, api.ListInput{Limit: 100, Cursor: first.NextCursor})
	if raw, err := f.dispatcher.Query(f.ctx, f.auth, api.Raw(q)); err == nil || len(raw) != 0 {
		t.Fatal("old cursor disclosed metadata across tenants")
	}
	f.auth = original
	q = listQuery(f, api.ListInput{Limit: 100, Cursor: first.NextCursor})
	q.LogicalServiceID = api.NewID("owner")
	requireListFailure(t, f, q, "invalid_request")
	q = listQuery(f, api.ListInput{Limit: 100, Cursor: first.NextCursor})
	q.TargetID = api.NewID("grant")
	requireListFailure(t, f, q, "invalid_request")
}

func TestGrantListCurrentCredentialGatesFirstAndSubsequentPages(t *testing.T) {
	for _, change := range []string{"revoked", "generation", "role_removed", "authority_revision"} {
		t.Run(change, func(t *testing.T) {
			f := grantListFixture(t)
			own := provisionListGrant(t, f, f.auth.Ref(f.scope.OwnerID))
			foreign := f.auth.Ref(f.scope.OwnerID)
			foreign.ObjectID = api.NewID("user")
			other := provisionListGrant(t, f, foreign)
			first := readListPage(t, f, listQuery(f, api.ListInput{Limit: 1}))
			current := f.auth
			state := "active"
			switch change {
			case "revoked":
				state = "revoked"
			case "generation":
				current.CredentialGeneration++
			case "role_removed":
				current.Roles = []string{"maintainer"}
			}
			setListCredential(t, f, current, state)
			if change != "authority_revision" {
				requireListFailure(t, f, listQuery(f, api.ListInput{Limit: 1}), "forbidden")
				requireListFailure(t, f, listQuery(f, api.ListInput{Limit: 1, Cursor: first.NextCursor}), "forbidden")
			}
			if state == "revoked" {
				return
			}
			f.auth = current
			requireListFailure(t, f, listQuery(f, api.ListInput{Limit: 1, Cursor: first.NextCursor}), "cursor_expired")
			freshQuery := listQuery(f, api.ListInput{Limit: 100})
			fresh := readListPage(t, f, freshQuery)
			if change == "role_removed" && (len(fresh.Items) != 1 || fresh.Items[0].Grant.GrantID != own.GrantID || strings.Contains(string(api.Raw(fresh)), other.GrantID)) {
				t.Fatal("maintainer regained another subject's metadata")
			}
		})
	}
}

func TestGrantListOriginalPagesInvalidateAfterPublicIssueRevokeAndUsage(t *testing.T) {
	f := grantListFixture(t)
	grant := issue(t, f, "continuous")
	provisionListGrant(t, f, f.auth.Ref(f.scope.OwnerID))
	beforeIssue := readListPage(t, f, listQuery(f, api.ListInput{Limit: 1}))
	issued := issue(t, f, "continuous")
	requireListFailure(t, f, listQuery(f, api.ListInput{Limit: 1, Cursor: beforeIssue.NextCursor}), "snapshot_required")
	beforeUse := readListPage(t, f, listQuery(f, api.ListInput{Limit: 1}))
	use := useRequest(f, grant)
	_, receipt := command(t, f, "grant.use", use.UseID, use, nil)
	if receipt.Stage != "applied" {
		t.Fatalf("original public use: %+v", receipt)
	}
	requireListFailure(t, f, listQuery(f, api.ListInput{Limit: 1, Cursor: beforeUse.NextCursor}), "snapshot_required")
	used := readListPage(t, f, listQuery(f, api.ListInput{Limit: 100}))
	if used.CollectionRevision <= beforeUse.CollectionRevision {
		t.Fatal("usage did not monotonically advance the collection")
	}
	found := false
	for _, item := range used.Items {
		if item.Grant.GrantID == grant.GrantID {
			found = len(item.Reserved) == 1 && item.Reserved[0].Value == "2"
		}
	}
	if !found {
		t.Fatal("list omitted current original reservation")
	}
	beforeRevoke := readListPage(t, f, listQuery(f, api.ListInput{Limit: 1}))
	revoke, accepted := command(t, f, "grant.revoke", issued.GrantID, governance.GrantRevoke{GrantRef: f.scope.Ref(issued.GrantID, issued.Revision), PreviewRefs: []api.ContentRef{ref(t, f, "preview")}, ConfirmationExpiresAt: api.Time(time.Now().Add(time.Minute))}, &issued.Revision)
	if accepted.Stage != "accepted" {
		t.Fatalf("public revoke admission: %+v", accepted)
	}
	approveOriginal(t, f, revoke, accepted)
	// An accepted confirmation request is not a completed revocation. The
	// collection changes when the original confirmed Grant write commits.
	requireListFailure(t, f, listQuery(f, api.ListInput{Limit: 1, Cursor: beforeRevoke.NextCursor}), "snapshot_required")
	closed := readListPage(t, f, listQuery(f, api.ListInput{Limit: 100}))
	found = false
	for _, item := range closed.Items {
		if item.Grant.GrantID == issued.GrantID {
			found = item.Grant.State == "revoked"
		}
	}
	if !found || closed.CollectionRevision <= used.CollectionRevision {
		t.Fatal("current revoked fact lost from metadata view")
	}
}

func TestGrantListOriginalBindingExpiresWithoutPageRenewal(t *testing.T) {
	f := grantListFixture(t)
	for i := 0; i < 3; i++ {
		provisionListGrant(t, f, f.auth.Ref(f.scope.OwnerID))
	}
	firstQuery := listQuery(f, api.ListInput{Limit: 1})
	roles := append([]string{}, f.auth.Roles...)
	sort.Strings(roles)
	rolesDigest, _ := api.Digest(roles)
	digest, _ := api.Digest(firstQuery)
	bindings := f.store.(runtime.QueryBindingStore)
	original, status, err := bindings.BindQuery(f.ctx, f.scope, runtime.QueryBindingInput{QueryID: firstQuery.QueryID, PrincipalID: f.auth.SubjectID, CredentialGeneration: f.auth.CredentialGeneration, RolesDigest: rolesDigest, QueryDigest: digest, TTL: 2 * time.Second})
	if err != nil || status != runtime.Committed {
		t.Fatal(err)
	}
	first := readListPage(t, f, firstQuery)
	second := readListPage(t, f, listQuery(f, api.ListInput{Limit: 1, Cursor: first.NextCursor}))
	if second.NextCursor == "" || second.Exhausted {
		t.Fatal("fixture did not reach a continuation deadline")
	}
	until, _ := api.ParseTime(original.ExpiresAt)
	if wait := time.Until(until) + 5*time.Millisecond; wait > 0 {
		time.Sleep(wait)
	}
	requireListFailure(t, f, listQuery(f, api.ListInput{Limit: 1, Cursor: second.NextCursor}), "cursor_expired")
	// An expired runtime identity can start a new lifecycle, but the old token
	// never acquires that lifecycle's key or later deadline.
	fresh := readListPage(t, f, firstQuery)
	if fresh.NextCursor == first.NextCursor || fresh.CollectionRevision != first.CollectionRevision {
		t.Fatal("expired original slot was renewed under its old cursor")
	}
	requireListFailure(t, f, listQuery(f, api.ListInput{Limit: 1, Cursor: first.NextCursor}), "cursor_expired")
}

func TestGrantListNaturalExpiryAndNotBeforeInvalidateOriginalPages(t *testing.T) {
	for _, boundary := range []string{"expires_at", "not_before"} {
		t.Run(boundary, func(t *testing.T) {
			f := grantListFixture(t)
			g := provisionListGrant(t, f, f.auth.Ref(f.scope.OwnerID))
			provisionListGrant(t, f, f.auth.Ref(f.scope.OwnerID))
			deadline := time.Now().Add(2 * time.Second)
			status, err := f.store.Within(f.ctx, f.scope, []string{governance.Namespace}, func(tx runtime.Tx) error {
				if boundary == "expires_at" {
					g.ExpiresAt = api.Time(deadline)
				} else {
					g.NotBefore = api.Time(deadline)
				}
				g.Revision++
				// Trusted initial temporal precondition, before any query cursor.
				return tx.Put(f.ctx, "governance/grants", g.GrantID, 1, g)
			})
			if err != nil || status != runtime.Committed {
				t.Fatal(err)
			}
			first := readListPage(t, f, listQuery(f, api.ListInput{Limit: 1}))
			if wait := time.Until(deadline) + 5*time.Millisecond; wait > 0 {
				time.Sleep(wait)
			}
			requireListFailure(t, f, listQuery(f, api.ListInput{Limit: 1, Cursor: first.NextCursor}), "cursor_expired")
			fresh := readListPage(t, f, listQuery(f, api.ListInput{Limit: 100}))
			if len(fresh.Items) != 2 || fresh.CollectionRevision != first.CollectionRevision {
				t.Fatal("time boundary deleted metadata or invented a record revision")
			}
		})
	}
}

func TestGrantListDurableQuerySlotsDoNotEvictLiveOriginalQueries(t *testing.T) {
	f := grantListFixture(t)
	provisionListGrant(t, f, f.auth.Ref(f.scope.OwnerID))
	provisionListGrant(t, f, f.auth.Ref(f.scope.OwnerID))
	firstQuery := listQuery(f, api.ListInput{Limit: 1})
	first := readListPage(t, f, firstQuery)
	for i := 1; i < 100; i++ {
		readListPage(t, f, listQuery(f, api.ListInput{Limit: 1}))
	}
	requireListFailure(t, f, listQuery(f, api.ListInput{Limit: 1}), "overloaded")
	if again := readListPage(t, f, firstQuery); !api.Equal(again, first) {
		t.Fatal("quota evicted or refreshed a live original query")
	}
	second := readListPage(t, f, listQuery(f, api.ListInput{Limit: 1, Cursor: first.NextCursor}))
	if !second.Exhausted || len(second.Items) != 1 {
		t.Fatal("live continuation was blocked by the root query slot quota")
	}
}

func TestGrantListOriginalCursorAndQuerySurviveRealDatabaseReopen(t *testing.T) {
	ctx := context.Background()
	dsn := os.Getenv("HARNESS_GOVERNANCE_POSTGRES_DSN")
	path := filepath.Join(t.TempDir(), "list-recovery.db")
	var store runtime.Store
	var err error
	if dsn == "" {
		s, e := sqlite.Open(path)
		if e == nil {
			e = s.Migrate(ctx)
		}
		store, err = s, e
	} else {
		s, e := postgres.Open(ctx, dsn)
		if e == nil {
			e = s.Migrate(ctx)
		}
		store, err = s, e
	}
	if err != nil {
		t.Fatal(err)
	}
	f := &fixture{ctx: ctx, store: store, scope: runtime.Scope{TenantID: api.NewID("tenant"), OwnerID: api.NewID("owner"), DatabaseID: store.ID()}}
	f.auth = runtime.Auth{TenantID: f.scope.TenantID, SubjectID: api.NewID("user"), CredentialGeneration: 1, Roles: []string{"grant_authority"}}
	configure := func() {
		f.registry = runtime.NewRegistry()
		f.svc = governance.New(f.store, governance.Options{GrantMetadataGate: listMetadataAuthority{}, Participants: []string{"platform"}})
		if err := f.svc.Register(f.registry); err != nil {
			t.Fatal(err)
		}
		f.dispatcher = &runtime.Dispatcher{Store: f.store, OwnerID: f.scope.OwnerID, Registry: f.registry}
	}
	configure()
	identity := platform.DevIdentity{Store: f.store, OwnerID: f.scope.OwnerID, SessionTTL: time.Hour, Principals: []platform.Principal{{Auth: f.auth, TokenHash: api.Hash([]byte("non-secret original recovery principal"))}}}
	if err = identity.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := f.store.Close(); err != nil {
			t.Error(err)
		}
	})
	for i := 0; i < 4; i++ {
		provisionListGrant(t, f, f.auth.Ref(f.scope.OwnerID))
	}
	original := listQuery(f, api.ListInput{Limit: 2})
	first := readListPage(t, f, original)
	continuation := listQuery(f, api.ListInput{Limit: 2, Cursor: first.NextCursor})
	second := readListPage(t, f, continuation)
	if err = f.store.Close(); err != nil {
		t.Fatal(err)
	}
	if dsn == "" {
		f.store, err = sqlite.Open(path, sqlite.WithExpectedDatabaseID(f.scope.DatabaseID))
	} else {
		f.store, err = postgres.Open(ctx, dsn, postgres.WithExpectedDatabaseID(f.scope.DatabaseID))
	}
	if err != nil {
		t.Fatal(err)
	}
	configure()
	if got := readListPage(t, f, original); !api.Equal(got, first) {
		t.Fatal("database reopen replaced original query or cursor identity")
	}
	if got := readListPage(t, f, continuation); !api.Equal(got, second) {
		t.Fatal("database reopen changed the original subsequent page")
	}
	original.Payload = api.Raw(api.ListInput{Limit: 1})
	requireListFailure(t, f, original, "idempotency_conflict")
	t.Logf("GRANT_LIST_RECOVERY_EVIDENCE %s", api.Raw(struct {
		Scope       runtime.Scope `json:"scope"`
		QueryID     string        `json:"query_id"`
		NextQueryID string        `json:"next_query_id"`
		Revision    uint64        `json:"collection_revision"`
	}{f.scope, original.QueryID, continuation.QueryID, first.CollectionRevision}))
}

func TestGrantListClosedLimitsAndUnconfiguredAuthority(t *testing.T) {
	f := grantListFixture(t)
	for _, payload := range []any{api.ListInput{Limit: 0}, api.ListInput{Limit: 101}, map[string]any{"limit": 1, "subject_id": api.NewID("user")}} {
		q := listQuery(f, api.ListInput{Limit: 1})
		q.Payload = api.Raw(payload)
		requireListFailure(t, f, q, "invalid_request")
	}
	f.svc.Ports.GrantMetadataGate = nil
	requireListFailure(t, f, listQuery(f, api.ListInput{Limit: 1}), "unsupported")
}

func TestGrantListRejectsOversizedWholeViewBeforeReturningAnyPage(t *testing.T) {
	for _, bound := range []string{"record_count", "metadata_bytes"} {
		t.Run(bound, func(t *testing.T) {
			f := grantListFixture(t)
			count := 1000
			if bound == "metadata_bytes" {
				count = 300
			}
			status, err := f.store.Within(f.ctx, f.scope, []string{governance.Namespace}, func(tx runtime.Tx) error {
				for i := 0; i < count; i++ {
					g := api.Grant{GrantID: api.NewID("grant"), OwnerID: f.scope.OwnerID, Revision: 1, SubjectRef: f.auth.Ref(f.scope.OwnerID), Resources: []string{"document"}, Actions: []string{"write"}, Purposes: []string{"save"}, Recipients: []string{"executor"}, Locations: []string{"device"}, Mode: "continuous", State: "active", NotBefore: api.Time(time.Now().Add(-time.Minute)), ExpiresAt: api.Time(time.Now().Add(time.Hour)), Limits: []api.Amount{}}
					if bound == "metadata_bytes" {
						g.Resources = []string{strings.Repeat("r", 2000)}
					}
					if err := f.svc.ProvisionGrantTx(f.ctx, tx, f.auth, g); err != nil {
						return err
					}
				}
				return nil
			})
			if err != nil || status != runtime.Committed {
				t.Fatal(err)
			}
			requireListFailure(t, f, listQuery(f, api.ListInput{Limit: 1}), "overloaded")
		})
	}
}

func TestGrantListConcurrentUsagePreservesCurrentFactsWithoutLockInversion(t *testing.T) {
	f := grantListFixture(t)
	g := provisionListGrant(t, f, f.auth.Ref(f.scope.OwnerID))
	provisionListGrant(t, f, f.auth.Ref(f.scope.OwnerID))
	ctx, cancel := context.WithTimeout(f.ctx, 30*time.Second)
	defer cancel()
	var wg sync.WaitGroup
	errors := make(chan error, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 8; i++ {
			use := useRequest(f, g)
			use.RequestedUnits[0].Value = "0.1"
			c := api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: f.scope.OwnerID, CommandID: api.NewID("command"), Method: "grant.use", TargetID: use.UseID, ExpiresAt: api.Time(time.Now().Add(time.Minute)), Payload: api.Raw(use)}
			out, err := f.dispatcher.Command(ctx, f.auth, api.Raw(c))
			if err != nil {
				errors <- err
				return
			}
			if out.Stage != "applied" {
				errors <- api.E("invalid_state", "concurrent_use_not_applied")
				return
			}
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 12; i++ {
			_, err := f.dispatcher.Query(ctx, f.auth, api.Raw(listQuery(f, api.ListInput{Limit: 1})))
			if err != nil && !api.IsCode(err, "snapshot_required") {
				errors <- err
				return
			}
		}
	}()
	wg.Wait()
	close(errors)
	for err := range errors {
		t.Fatal(err)
	}
	page := readListPage(t, f, listQuery(f, api.ListInput{Limit: 100}))
	if page.CollectionRevision != 13 {
		t.Fatalf("eight original usage revisions lost: %+v", page)
	}
	found := false
	for _, item := range page.Items {
		if item.Grant.GrantID == g.GrantID {
			found = len(item.Reserved) == 1 && item.Reserved[0].Value == "0.8"
		}
	}
	if !found {
		t.Fatal("concurrent metadata read altered original usage")
	}
}
