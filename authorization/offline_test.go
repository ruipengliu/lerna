package authorization_test

import (
	"context"
	"lerna/adapters/sqliteauth"
	"lerna/authorization"
	wire "lerna/gen/harness/v1"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestOfflineReceiptIsNotApplication(t *testing.T) {
	f := newGrantFixture(t)
	ctx := context.Background()
	receipt, err := f.g.Mutate(ctx, f.token, f.request(t, "ISSUE", 1))
	if err != nil {
		t.Fatal(err)
	}
	op, err := f.s.NewOperation(ctx, f.token)
	if err != nil {
		t.Fatal(err)
	}
	p := authorization.GrantPresentation{Namespace: "local", Subject: "admin", Audience: "receiver", Presenter: "node", CertificateSHA256: f.spec.CertificateSha256, OperationID: op, SemanticSHA256: strings.Repeat("a", 64)}
	action := &wire.AuthorizationAction{Resource: "root", Action: "read", Purpose: "task", Location: "local"}
	cfg := authorization.OfflineConfig{Window: time.Minute, Skew: time.Second, IOTimeout: time.Second, Retention: time.Minute, Records: 16, PageSize: 1, Bytes: 65536}
	source, err := f.g.OfflineView("test-view", p, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err = source.Allocate(ctx, receipt.Material, p, action, true); err != nil {
		t.Fatal(err)
	}
	receiver, err := authorization.NewOfflineReplica(f.s, "test-view", p, cfg, f.crypto, authorization.SystemOfflineClock{}, publicOfflineFixture)
	if err != nil {
		t.Fatal(err)
	}
	query, err := receiver.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	page, err := source.Read(ctx, query)
	if err != nil {
		t.Fatal(err)
	}
	if err = receiver.Receive(ctx, page); err != nil {
		t.Fatal(err)
	}
	if err = receiver.Check(ctx, p, action); err == nil {
		t.Fatal("receipt authorized execution before application")
	}
	if err = receiver.Apply(ctx); err != nil {
		t.Fatal(err)
	}
	if err = receiver.Check(ctx, p, action); err != nil {
		t.Fatal(err)
	}
	reopened, err := authorization.NewOfflineReplica(f.s, "test-view", p, cfg, f.crypto, authorization.SystemOfflineClock{}, publicOfflineFixture)
	if err != nil {
		t.Fatal(err)
	}
	if err = reopened.Check(ctx, p, action); err == nil {
		t.Fatal("offline restart reused persisted time")
	}
}

type offlineClock struct {
	wall    time.Time
	elapsed time.Duration
}

func (c *offlineClock) Sample() (time.Time, time.Duration, error) { return c.wall, c.elapsed, nil }
func (c *offlineClock) advance(d time.Duration)                   { c.wall = c.wall.Add(d); c.elapsed += d }
func TestOfflineRevocationTimeAndPagination(t *testing.T) {
	f := newGrantFixture(t)
	ctx := context.Background()
	receipt, e := f.g.Mutate(ctx, f.token, f.request(t, "ISSUE", 1))
	if e != nil {
		t.Fatal(e)
	}
	cfg := authorization.OfflineConfig{Window: time.Minute, Skew: time.Second, IOTimeout: time.Second, Retention: time.Minute, Records: 16, PageSize: 1, Bytes: 65536}
	p := authorization.GrantPresentation{Namespace: "local", Subject: "admin", Audience: "receiver", Presenter: "node", CertificateSHA256: f.spec.CertificateSha256, SemanticSHA256: strings.Repeat("b", 64)}
	a := &wire.AuthorizationAction{Resource: "root", Action: "read", Purpose: "task", Location: "local"}
	source, e := f.g.OfflineView("test-view", p, cfg)
	if e != nil {
		t.Fatal(e)
	}
	var first authorization.GrantPresentation
	for i := 0; i < 2; i++ {
		p.OperationID, e = f.s.NewOperation(ctx, f.token)
		if e != nil {
			t.Fatal(e)
		}
		if i == 0 {
			first = p
		}
		if e = source.Allocate(ctx, receipt.Material, p, a, true); e != nil {
			t.Fatal(e)
		}
	}
	clock := &offlineClock{wall: f.clock.now}
	replica, e := authorization.NewOfflineReplica(f.s, "test-view", p, cfg, f.crypto, clock, publicOfflineFixture)
	if e != nil {
		t.Fatal(e)
	}
	expiredPage := false
	read := func(ctx context.Context, q authorization.OfflineQuery) (string, error) {
		if q.Offset > 0 && !expiredPage {
			expiredPage = true
			return "", &authorization.Error{Code: authorization.Expired}
		}
		return source.Read(ctx, q)
	}
	if e = replica.Synchronize(ctx, read); e != nil {
		t.Fatal(e)
	}
	if e = replica.Check(ctx, first, a); e != nil {
		t.Fatal(e)
	}
	clock.wall = clock.wall.Add(-2 * time.Second)
	if e = replica.Check(ctx, first, a); !authorization.Is(e, authorization.TimeUntrusted) {
		t.Fatalf("rollback accepted: %v", e)
	}
	clock.wall = clock.wall.Add(2 * time.Second)
	if e = replica.Check(ctx, first, a); e == nil {
		t.Fatal("clock recovery silently renewed anchor")
	}
	if e = replica.Synchronize(ctx, source.Read); e != nil {
		t.Fatal(e)
	}
	clock.wall = clock.wall.Add(time.Hour)
	if e = replica.Check(ctx, first, a); e == nil {
		t.Fatal("suspend-like wall/elapsed divergence accepted")
	}
	clock.wall = clock.wall.Add(-time.Hour)
	if e = replica.Synchronize(ctx, source.Read); e != nil {
		t.Fatal(e)
	}
	clock.advance(time.Minute)
	if e = replica.Check(ctx, first, a); e == nil {
		t.Fatal("expired offline window accepted")
	}
	if e = replica.Synchronize(ctx, source.Read); e != nil {
		t.Fatal(e)
	}
	q, e := replica.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	page, e := source.Read(ctx, q)
	if e != nil {
		t.Fatal(e)
	}
	if e = replica.Receive(ctx, page); e != nil {
		t.Fatal(e)
	}
	op, e := f.s.NewOperation(ctx, f.token)
	if e != nil {
		t.Fatal(e)
	}
	_, e = f.g.Mutate(ctx, f.token, &wire.GrantMutation{OperationId: op, Kind: "REVOKE", GrantId: receipt.GrantId, ExpectedRevision: 2, ExpectedGrantRevision: 2})
	if e != nil {
		t.Fatal(e)
	}
	q, e = replica.Next(ctx)
	if e != nil {
		t.Fatal(e)
	}
	page, e = source.Read(ctx, q)
	if e != nil {
		t.Fatal(e)
	}
	if e = replica.Receive(ctx, page); e != nil {
		t.Fatal(e)
	}
	if e = replica.Apply(ctx); e != nil {
		t.Fatal(e)
	}
	if e = replica.Check(ctx, first, a); e == nil {
		t.Fatal("snapshot behind known authority revision authorized work")
	}
	if e = replica.Synchronize(ctx, source.Read); e != nil {
		t.Fatal(e)
	}
	if e = replica.Check(ctx, first, a); e == nil {
		t.Fatal("known revoked grant accepted")
	}
}

func TestOfflineReceiveSurvivesRestartWithoutTimeAuthority(t *testing.T) {
	f := newGrantFixture(t)
	ctx := context.Background()
	receipt, e := f.g.Mutate(ctx, f.token, f.request(t, "ISSUE", 1))
	if e != nil {
		t.Fatal(e)
	}
	cfg := authorization.OfflineConfig{Window: time.Minute, Skew: time.Second, IOTimeout: time.Second, Retention: time.Minute, Records: 4, PageSize: 1, Bytes: 65536}
	op, e := f.s.NewOperation(ctx, f.token)
	if e != nil {
		t.Fatal(e)
	}
	p := authorization.GrantPresentation{Namespace: "local", Subject: "admin", Audience: "receiver", Presenter: "node", CertificateSHA256: f.spec.CertificateSha256, OperationID: op, SemanticSHA256: strings.Repeat("c", 64)}
	a := &wire.AuthorizationAction{Resource: "root", Action: "read", Purpose: "task", Location: "local"}
	source, e := f.g.OfflineView("test-view", p, cfg)
	if e != nil {
		t.Fatal(e)
	}
	if e = source.Allocate(ctx, receipt.Material, p, a, true); e != nil {
		t.Fatal(e)
	}
	replica, e := authorization.NewOfflineReplica(f.s, "test-view", p, cfg, f.crypto, authorization.SystemOfflineClock{}, publicOfflineFixture)
	if e != nil {
		t.Fatal(e)
	}
	q, e := replica.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	page, e := source.Read(ctx, q)
	if e != nil {
		t.Fatal(e)
	}
	if e = replica.Receive(ctx, page); e != nil {
		t.Fatal(e)
	}
	progress, e := replica.Progress(ctx)
	if e != nil || progress.Received == 0 || progress.Applied != 0 || !progress.Pending {
		t.Fatal(progress, e)
	}
	reopened, e := authorization.NewOfflineReplica(f.s, "test-view", p, cfg, f.crypto, authorization.SystemOfflineClock{}, publicOfflineFixture)
	if e != nil {
		t.Fatal(e)
	}
	if e = reopened.Apply(ctx); e != nil {
		t.Fatal(e)
	}
	progress, e = reopened.Progress(ctx)
	if e != nil || progress.Applied != progress.Received || progress.Pending {
		t.Fatal(progress, e)
	}
	if e = reopened.Check(ctx, p, a); e == nil {
		t.Fatal("recovered application renewed time")
	}
	if e = replica.Check(ctx, p, a); e == nil {
		t.Fatal("old host session survived restart")
	}
	if e = reopened.Synchronize(ctx, source.Read); e != nil {
		t.Fatal(e)
	}
	if e = reopened.Check(ctx, p, a); e != nil {
		t.Fatal(e)
	}
	backup, e := f.db.Load(ctx)
	if e != nil {
		t.Fatal(e)
	}
	clone, e := sqliteauth.Open(filepath.Join(t.TempDir(), "old-backup.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer clone.Close()
	if e = clone.Commit(ctx, 0, backup.State); e != nil {
		t.Fatal(e)
	}
	clonedService, e := authorization.New(clone, f.clock, config())
	if e != nil {
		t.Fatal(e)
	}
	clonedReplica, e := authorization.NewOfflineReplica(clonedService, "test-view", p, cfg, f.crypto, authorization.SystemOfflineClock{}, publicOfflineFixture)
	if e != nil {
		t.Fatal(e)
	}
	if e = clonedReplica.Check(ctx, p, a); e == nil {
		t.Fatal("cloned persisted authorization resumed offline")
	}

}

type offlineFailStore struct {
	authorization.Store
	fail bool
}

func (s *offlineFailStore) Commit(ctx context.Context, v uint64, state authorization.State) error {
	if s.fail {
		return &authorization.Error{Code: authorization.Unavailable}
	}
	return s.Store.Commit(ctx, v, state)
}
func TestOfflineAtomicApplyFailure(t *testing.T) {
	f := newGrantFixture(t)
	ctx := context.Background()
	receipt, e := f.g.Mutate(ctx, f.token, f.request(t, "ISSUE", 1))
	if e != nil {
		t.Fatal(e)
	}
	cfg := authorization.OfflineConfig{Window: time.Minute, Skew: time.Second, IOTimeout: time.Second, Retention: time.Minute, Records: 4, PageSize: 1, Bytes: 65536}
	op, e := f.s.NewOperation(ctx, f.token)
	if e != nil {
		t.Fatal(e)
	}
	p := authorization.GrantPresentation{Namespace: "local", Subject: "admin", Audience: "receiver", Presenter: "node", CertificateSHA256: f.spec.CertificateSha256, OperationID: op, SemanticSHA256: strings.Repeat("d", 64)}
	a := &wire.AuthorizationAction{Resource: "root", Action: "read", Purpose: "task", Location: "local"}
	source, e := f.g.OfflineView("test-view", p, cfg)
	if e != nil {
		t.Fatal(e)
	}
	if e = source.Allocate(ctx, receipt.Material, p, a, true); e != nil {
		t.Fatal(e)
	}
	store := &offlineFailStore{Store: f.db}
	service, e := authorization.New(store, f.clock, config())
	if e != nil {
		t.Fatal(e)
	}
	replica, e := authorization.NewOfflineReplica(service, "test-view", p, cfg, f.crypto, authorization.SystemOfflineClock{}, publicOfflineFixture)
	if e != nil {
		t.Fatal(e)
	}
	q, e := replica.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	page, e := source.Read(ctx, q)
	if e != nil {
		t.Fatal(e)
	}
	store.fail = true
	if e = replica.Receive(ctx, page); e == nil {
		t.Fatal("failed persistence reported received")
	}
	store.fail = false
	progress, e := replica.Progress(ctx)
	if e != nil || progress.Received != 0 || progress.Applied != 0 {
		t.Fatal(progress, e)
	}
	if e = replica.Receive(ctx, page); e != nil {
		t.Fatal(e)
	}
	store.fail = true
	if e = replica.Apply(ctx); e == nil {
		t.Fatal("failed apply reported success")
	}
	store.fail = false
	progress, e = replica.Progress(ctx)
	if e != nil || progress.Applied != 0 || progress.Received == 0 {
		t.Fatal(progress, e)
	}
	if e = replica.Check(ctx, p, a); e == nil {
		t.Fatal("failed apply installed authorization")
	}
	if e = replica.Apply(ctx); e != nil {
		t.Fatal(e)
	}
	pending, e := source.Progress(ctx)
	if e != nil || !pending.Pending {
		t.Fatal(pending, e)
	}
	if e = replica.Confirm(ctx, source.Read); e != nil {
		t.Fatal(e)
	}
	pending, e = source.Progress(ctx)
	if e != nil || pending.Pending {
		t.Fatal(pending, e)
	}
	op, e = f.s.NewOperation(ctx, f.token)
	if e != nil {
		t.Fatal(e)
	}
	_, e = f.g.Mutate(ctx, f.token, &wire.GrantMutation{OperationId: op, Kind: "REVOKE", GrantId: receipt.GrantId, ExpectedRevision: 2, ExpectedGrantRevision: 2})
	if e != nil {
		t.Fatal(e)
	}
	pending, e = source.Progress(ctx)
	if e != nil || !pending.Pending {
		t.Fatal("undelivered revocation not pending", pending, e)
	}
	p.Audience = "different-receiver"
	if e = replica.Check(ctx, p, a); e == nil {
		t.Fatal("cross receiver replay allowed")
	}
}

// This fixture contains synthetic public authorization metadata.
func publicOfflineFixture(context.Context) error { return nil }

func TestOfflineExpiredCursorAndReservedCapacity(t *testing.T) {
	f := newGrantFixture(t)
	ctx := context.Background()
	receipt, e := f.g.Mutate(ctx, f.token, f.request(t, "ISSUE", 1))
	if e != nil {
		t.Fatal(e)
	}
	cfg := authorization.OfflineConfig{Window: time.Minute, Skew: time.Second, IOTimeout: time.Second, Retention: time.Second, Records: 1, PageSize: 1, Bytes: 65536}
	p := authorization.GrantPresentation{Namespace: "local", Subject: "admin", Audience: "receiver", Presenter: "node", CertificateSHA256: f.spec.CertificateSha256, SemanticSHA256: strings.Repeat("e", 64)}
	p.OperationID, e = f.s.NewOperation(ctx, f.token)
	if e != nil {
		t.Fatal(e)
	}
	a := &wire.AuthorizationAction{Resource: "root", Action: "read", Purpose: "task", Location: "local"}
	source, e := f.g.OfflineView("test-view", p, cfg)
	if e != nil {
		t.Fatal(e)
	}
	if e = source.Allocate(ctx, receipt.Material, p, a, true); e != nil {
		t.Fatal(e)
	}
	replica, e := authorization.NewOfflineReplica(f.s, "test-view", p, cfg, f.crypto, authorization.SystemOfflineClock{}, publicOfflineFixture)
	if e != nil {
		t.Fatal(e)
	}
	if e = replica.Synchronize(ctx, source.Read); e != nil {
		t.Fatal(e)
	}
	// A second allocation cannot fit; its uncertain delivery is still reserved.
	second := p
	second.OperationID, e = f.s.NewOperation(ctx, f.token)
	if e != nil {
		t.Fatal(e)
	}
	if e = source.Allocate(ctx, receipt.Material, second, a, true); !authorization.Is(e, authorization.Unavailable) {
		t.Fatal(e)
	}
	if _, e = f.g.LookupUse(ctx, second); e != nil {
		t.Fatal("failed delivery refunded reservation", e)
	}
	if e = source.Allocate(ctx, receipt.Material, p, a, true); e != nil {
		t.Fatal(e)
	}
	record, e := f.g.Get(ctx, f.token, receipt.GrantId)
	if e != nil || record.Allocated != 2 {
		t.Fatal(record, e)
	}
	op, e := f.s.NewOperation(ctx, f.token)
	if e != nil {
		t.Fatal(e)
	}
	_, e = f.g.Mutate(ctx, f.token, &wire.GrantMutation{OperationId: op, Kind: "REVOKE", GrantId: receipt.GrantId, ExpectedRevision: 2, ExpectedGrantRevision: 2})
	if e != nil {
		t.Fatal(e)
	}
	f.clock.now = f.clock.now.Add(2 * time.Second)
	// Retention discards the old source cursor, requiring a fresh snapshot.
	if e = replica.Synchronize(ctx, source.Read); e != nil {
		t.Fatal(e)
	}
	if e = replica.Check(ctx, p, a); e == nil {
		t.Fatal("expired cursor reset removed revocation")
	}
}

func TestSystemOfflineWallSample(t *testing.T) {
	wall, _, e := (authorization.SystemOfflineClock{}).Sample()
	if e != nil {
		t.Fatal(e)
	}
	if wall != wall.Round(0) {
		t.Fatal("wall sample retains monotonic component, hiding wall/elapsed divergence")
	}
}

func TestOfflineRecipientsShareFiniteParentQuota(t *testing.T) {
	f := newGrantFixture(t)
	ctx := context.Background()
	f.spec.Units = 2
	f.spec.DelegateTargets = []*wire.GrantDelegateTarget{{Subject: "admin", Audience: "receiver-two", Presenter: "node", CertificateSha256: f.spec.CertificateSha256}}
	root, e := f.g.Mutate(ctx, f.token, f.request(t, "ISSUE", 1))
	if e != nil {
		t.Fatal(e)
	}
	cfg := authorization.OfflineConfig{Window: time.Minute, Skew: time.Second, IOTimeout: time.Second, Retention: time.Minute, Records: 4, PageSize: 1, Bytes: 65536}
	a := &wire.AuthorizationAction{Resource: "root", Action: "read", Purpose: "task", Location: "local"}
	var replicas []*authorization.OfflineReplica
	var peers []authorization.GrantPresentation
	var views []*authorization.OfflineView
	for i, audience := range []string{"receiver", "receiver-two"} {
		child := f.request(t, "DERIVE", uint64(2+i))
		child.GrantId = root.GrantId
		child.ExpectedGrantRevision = 2
		child.Spec.Units = 1
		child.Spec.DelegationDepth = 0
		child.Spec.Audience = audience
		child.Spec.DelegateTargets = nil
		issued, e := f.g.Mutate(ctx, f.token, child)
		if e != nil {
			t.Fatal(e)
		}
		op, e := f.s.NewOperation(ctx, f.token)
		if e != nil {
			t.Fatal(e)
		}
		p := authorization.GrantPresentation{Namespace: "local", Subject: "admin", Audience: audience, Presenter: "node", CertificateSHA256: f.spec.CertificateSha256, OperationID: op, SemanticSHA256: strings.Repeat("f", 64)}
		view, e := f.g.OfflineView(audience, p, cfg)
		if e != nil {
			t.Fatal(e)
		}
		if e = view.Allocate(ctx, issued.Material, p, a, true); e != nil {
			t.Fatal(e)
		}
		replica, e := authorization.NewOfflineReplica(f.s, audience, p, cfg, f.crypto, authorization.SystemOfflineClock{}, publicOfflineFixture)
		if e != nil {
			t.Fatal(e)
		}
		if e = replica.Synchronize(ctx, view.Read); e != nil {
			t.Fatal(e)
		}
		replicas = append(replicas, replica)
		peers = append(peers, p)
		views = append(views, view)
	}
	parent, e := f.g.Get(ctx, f.token, root.GrantId)
	if e != nil || parent.Allocated != 2 {
		t.Fatal(parent, e)
	}
	if e = replicas[0].Check(ctx, peers[1], a); e == nil {
		t.Fatal("sibling recipient quota reused")
	}
	over := f.request(t, "DERIVE", 4)
	over.GrantId = root.GrantId
	over.ExpectedGrantRevision = 2
	if _, e = f.g.Mutate(ctx, f.token, over); !authorization.Is(e, authorization.Denied) {
		t.Fatal("parent quota expanded", e)
	}
	revoke := f.request(t, "REVOKE", 4)
	revoke.Spec = nil
	revoke.GrantId = root.GrantId
	revoke.ExpectedGrantRevision = 2
	if _, e = f.g.Mutate(ctx, f.token, revoke); e != nil {
		t.Fatal(e)
	}
	for i, replica := range replicas {
		if e = replica.Synchronize(ctx, views[i].Read); e != nil {
			t.Fatal(e)
		}
		if e = replica.Check(ctx, peers[i], a); e == nil {
			t.Fatal("ancestor revocation bypassed")
		}
	}
}
