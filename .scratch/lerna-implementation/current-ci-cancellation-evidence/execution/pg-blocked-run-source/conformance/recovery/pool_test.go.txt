//go:build integration

package recovery_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/ruipengliu/lerna/contract"
	"github.com/ruipengliu/lerna/host/durablework"
	demo "github.com/ruipengliu/lerna/internal/durableworkdemo"
	"github.com/ruipengliu/lerna/runtime"
	"sort"
	"sync"
	"testing"
	"time"
)

func TestPGPoolBehaviors(t *testing.T)     { poolTracer(t, database(t).Store()) }
func TestSQLitePoolBehaviors(t *testing.T) { poolTracer(t, sqliteDatabase(t).Store()) }
func poolTracer(t *testing.T, store workStore) {
	ctx := contextFor(t)
	h := rawHostFor(store, owner, principal)
	h.PoolControl = true
	cfg := demo.DefaultPool("shared", []contract.OwnerRef{owner})
	cfg.Limits[0].Concurrent = 1
	cfg.Quotas[0].Concurrent = 1
	if err := h.InstallPool(ctx, cfg, 0); err != nil {
		t.Fatal(err)
	}
	p := demo.DefaultPolicy()
	p.Lane = "control"
	q := demo.DefaultPolicy()
	q.Lane = "reconciliation"
	var err error
	h.Policies, err = demo.NewPolicies([]demo.PolicyBinding{{Owner: owner, ObjectID: "control", Policy: p}, {Owner: owner, ObjectID: "reconcile", Policy: q}})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []contract.ID{"ordinary", "control", "reconcile"} {
		out, err := h.Record(ctx, command(string(id), string(id), "hello", nil, future()), &principal)
		assertReceived(t, out, err)
	}
	w := conformanceWorker(t, owner, store, store, store, store)
	first, err := w.Claim(ctx, "worker-a", 1, time.Minute)
	if err != nil || len(first) != 1 || first[0].Input.ID != "ordinary" {
		t.Fatalf("ordinary claim: %+v %v", first, err)
	}
	startWork(t, w, first[0])
	for _, id := range []contract.ID{"control", "reconcile"} {
		batch, err := w.Claim(ctx, "worker-b", 1, time.Minute)
		if err != nil || len(batch) != 1 || batch[0].Input.ID != id {
			t.Fatalf("reserved lane: %+v %v", batch, err)
		}
		if err = w.Process(ctx, batch[0]); err != nil {
			t.Fatal(err)
		}
		observed, err := h.Observe(ctx, id, &principal)
		if err != nil || observed.Projection == nil || observed.Projection.TextDigest != "sha256:2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824" {
			t.Fatalf("reserved actual progress: %+v %v", observed, err)
		}
	}
	if err = w.Complete(ctx, first[0].Claim, demo.Project(first[0])); err != nil {
		t.Fatal(err)
	}
}

func TestPGPoolAdmission(t *testing.T) {
	runPoolAdmission(t, func(t *testing.T) *ownedFixture { return database(t) })
}
func TestSQLitePoolAdmission(t *testing.T) {
	runPoolAdmission(t, func(t *testing.T) *ownedFixture { return sqliteDatabase(t) })
}
func runPoolAdmission(t *testing.T, newStore func(*testing.T) *ownedFixture) {
	t.Run("MissingConfigurationPreservesOriginalKeyAndNoNewFacts", func(t *testing.T) {
		fixture := newStore(t)
		store := fixture.Store()
		h := rawHostFor(store, owner, principal)
		ctx := contextFor(t)
		data := command("missing", "input", "hello", nil, future())
		if _, err := h.Record(ctx, data, &principal); !errors.Is(err, demo.ErrPoolMissing) {
			t.Fatalf("missing config: %v", err)
		}
		got, err := store.ReadCommand(ctx, contract.CommandRef{Owner: owner, CommandID: "missing"})
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := got.AsNotFound(); !ok {
			t.Fatalf("partial receipt: %+v", got)
		}
		w := conformanceWorker(t, owner, store, store, store, store)
		if _, err = w.Claim(ctx, "worker-a", 1, time.Minute); !errors.Is(err, demo.ErrPoolMissing) {
			t.Fatalf("missing claim configuration: %v", err)
		}
		h.PoolControl = true
		cfg := demo.DefaultPool("missing", []contract.OwnerRef{owner})
		if err = h.InstallPool(ctx, cfg, 0); err != nil {
			t.Fatal(err)
		}
		original := assertReceivedResult(t, h, ctx, data)
		cfg.Members = nil
		// The actual config may not remove unfinished responsibility. A normal
		// drain then removal preserves fixed original receipts and permits reads.
		batch, err := w.Claim(ctx, "worker-a", 1, time.Minute)
		if err != nil || len(batch) != 1 {
			t.Fatalf("normal claim: %+v %v", batch, err)
		}
		if err = w.Process(ctx, batch[0]); err != nil {
			t.Fatal(err)
		}
		changed := command("missing", "input", "changed", nil, future())
		_, err = h.Record(ctx, changed, &principal)
		assertReason(t, err, "idempotency_conflict")
		replay, err := h.Record(ctx, data, &principal)
		assertReceiptSame(t, original, assertReceived(t, replay, err))
	})
	t.Run("QueueBackpressureRollsBackAndReusesActiveJob", func(t *testing.T) {
		fixture := newStore(t)
		store := fixture.Store()
		h := rawHostFor(store, owner, principal)
		ctx := contextFor(t)
		h.PoolControl = true
		cfg := demo.DefaultPool("queue", []contract.OwnerRef{owner})
		cfg.Limits[0].Queue = 1
		if err := h.InstallPool(ctx, cfg, 0); err != nil {
			t.Fatal(err)
		}
		first := command("first", "input", "hello", nil, future())
		assertReceivedResult(t, h, ctx, first)
		blocked := command("blocked", "other", "hello", nil, future())
		if _, err := h.Record(ctx, blocked, &principal); !errors.Is(err, demo.ErrPoolCapacity) {
			t.Fatalf("capacity: %v", err)
		}
		got, err := store.ReadCommand(ctx, contract.CommandRef{Owner: owner, CommandID: "blocked"})
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := got.AsNotFound(); !ok {
			t.Fatalf("blocked saved receipt: %+v", got)
		}
		if _, err = h.Observe(ctx, "other", &principal); err == nil {
			t.Fatal("blocked input survived")
		}
		rev := contract.Revision("1")
		assertReceivedResult(t, h, ctx, command("new-revision", "input", "hello", &rev, future()))
		refused := assertReceivedResult(t, h, ctx, command("wrong-revision", "input", "hello", nil, future()))
		if _, ok := refused.AsRejected(); !ok {
			t.Fatal("capacity displaced precondition")
		}
		expired := assertReceivedResult(t, h, ctx, command("expired", "other", "hello", nil, "2000-01-01T00:00:00.000000Z"))
		r, ok := expired.AsRejected()
		if !ok || r.Reason != "expired" {
			t.Fatalf("capacity displaced expiry: %+v", expired)
		}
		w := conformanceWorker(t, owner, store, store, store, store)
		batch, err := w.Claim(ctx, "worker-a", 1, time.Minute)
		if err != nil || len(batch) != 1 {
			t.Fatalf("claim: %+v %v", batch, err)
		}
		if err = w.Process(ctx, batch[0]); err != nil {
			t.Fatal(err)
		}
		assertReceivedResult(t, h, ctx, blocked)
		rev = "2"
		if _, err = h.Record(ctx, command("retrigger", "input", "hello", &rev, future()), &principal); !errors.Is(err, demo.ErrPoolCapacity) {
			t.Fatalf("done retrigger ignored queue: %v", err)
		}
		observed, err := h.ObservePool(ctx)
		if err != nil || observed.Queued["ordinary"] != 1 {
			t.Fatalf("queue: %+v %v", observed, err)
		}
	})
}
func assertReceivedResult(t *testing.T, h *durablework.Host, ctx context.Context, data []byte) contract.CommandReceipt {
	t.Helper()
	out, err := h.Record(ctx, data, &principal)
	return assertReceived(t, out, err)
}

func TestPGPoolFairness(t *testing.T)     { poolFairness(t, database(t).Store()) }
func TestSQLitePoolFairness(t *testing.T) { poolFairness(t, sqliteDatabase(t).Store()) }
func poolFairness(t *testing.T, store workStore) {
	ctx := contextFor(t)
	h := rawHostFor(store, owner, principal)
	h.PoolControl = true
	other := contract.OwnerRef{TenantID: "tenant-two", OwnerID: "owner-two"}
	cfg := demo.DefaultPool("fair", []contract.OwnerRef{owner, other})
	cfg.Limits[0].Concurrent = 1
	for i := range cfg.Quotas {
		if cfg.Quotas[i].Lane == "ordinary" {
			cfg.Quotas[i].Concurrent = 1
		}
	}
	if err := h.InstallPool(ctx, cfg, 0); err != nil {
		t.Fatal(err)
	}
	hs := map[contract.OwnerRef]*durablework.Host{}
	workers := []*durablework.Worker{}
	for _, scope := range cfg.Members {
		subject := principal
		subject.TenantID = scope.TenantID
		hh := rawHostFor(store, scope, subject)
		hs[scope] = hh
		for i := 0; i < 3; i++ {
			id := fmt.Sprintf("job-%d", i)
			data := command(id, id, "hello", nil, future())
			var envelope contract.CommandEnvelope
			if err := json.Unmarshal(data, &envelope); err != nil {
				t.Fatal(err)
			}
			envelope.Target.TenantID = scope.TenantID
			envelope.Target.OwnerID = scope.OwnerID
			data, _ = json.Marshal(envelope)
			out, err := hh.Record(ctx, data, &subject)
			assertReceived(t, out, err)
		}
		workers = append(workers, conformanceWorker(t, scope, store, store, store, store))
	}
	pool, err := durablework.NewPoolWorker(h, workers)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 6; i++ {
		work, err := pool.ClaimLane(ctx, "ordinary", "worker-a", time.Minute)
		if err != nil || work == nil {
			t.Fatalf("fair opportunity %d: %+v %v", i, work, err)
		}
		expected := owner
		if i%2 == 1 {
			expected = other
		}
		if work.Worker.Owner != expected {
			t.Fatalf("N=2 exceeded at opportunity %d: got %v want %v", i, work.Worker.Owner, expected)
		}
		if err = work.Worker.Process(ctx, work.Work); err != nil {
			t.Fatal(err)
		}
	}
	observed, err := h.ObservePool(ctx)
	if err != nil || observed.State.Cursors["ordinary"].Sequence != 6 || observed.Queued["ordinary"] != 0 {
		t.Fatalf("durable fair state: %+v %v", observed, err)
	}
	for scope, hh := range hs {
		subject := principal
		subject.TenantID = scope.TenantID
		for i := 0; i < 3; i++ {
			got, err := hh.Observe(ctx, contract.ID(fmt.Sprintf("job-%d", i)), &subject)
			if err != nil || got.Projection == nil || got.Projection.TextDigest != "sha256:2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824" {
				t.Fatalf("healthy projection: %+v %v", got, err)
			}
		}
	}
}

func TestPGPoolZeroQuotaExpiry(t *testing.T)     { poolZeroQuotaExpiry(t, database(t).Store()) }
func TestSQLitePoolZeroQuotaExpiry(t *testing.T) { poolZeroQuotaExpiry(t, sqliteDatabase(t).Store()) }
func poolZeroQuotaExpiry(t *testing.T, store workStore) {
	ctx := contextFor(t)
	clock := &workClock{now: time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC)}
	h := rawHostFor(store, owner, principal)
	h.Clock = clock
	h.PoolControl = true
	cfg := demo.DefaultPool("zero", []contract.OwnerRef{owner})
	cfg.Quotas[0].Concurrent = 0
	if err := h.InstallPool(ctx, cfg, 0); err != nil {
		t.Fatal(err)
	}
	p := demo.DefaultPolicy()
	p.ExecutionLimit = time.Second
	var err error
	h.Policies, err = demo.NewPolicies([]demo.PolicyBinding{{Owner: owner, ObjectID: "input", Policy: p}})
	if err != nil {
		t.Fatal(err)
	}
	data := command("zero-source", "input", "hello", nil, future())
	original := assertReceivedResult(t, h, ctx, data)
	w := conformanceWorker(t, owner, store, store, store, clock)
	pool, err := durablework.NewPoolWorker(h, []*durablework.Worker{w})
	if err != nil {
		t.Fatal(err)
	}
	if batch, err := w.Claim(ctx, "worker-a", 1, time.Minute); err != nil || len(batch) != 0 {
		t.Fatalf("zero quota executed: %+v %v", batch, err)
	}
	if err = pool.Maintain(ctx); err != nil {
		t.Fatal(err)
	}
	before, err := h.ObserveSchedule(ctx, "input", 1, &principal)
	if err != nil || before.State.Attempts != 0 || before.State.Outcome != "" {
		t.Fatalf("before deadline: %+v %v", before, err)
	}
	clock.Advance(time.Second)
	if err = pool.Maintain(ctx); err != nil {
		t.Fatal(err)
	}
	after, err := h.ObserveSchedule(ctx, "input", 1, &principal)
	if err != nil || after.State.Attempts != 0 || after.State.Outcome != "expired" || after.Job.State != "done" {
		t.Fatalf("zero quota closure: %+v %v", after, err)
	}
	observation, err := h.Observe(ctx, "input", &principal)
	if err != nil || observation.Projection != nil {
		t.Fatalf("expiry invented hash: %+v %v", observation, err)
	}
	state, err := h.ObservePool(ctx)
	if err != nil || state.Queued["ordinary"] != 0 || state.State.Cursors["ordinary"].Sequence != 0 {
		t.Fatalf("expiry allocated execution: %+v %v", state, err)
	}
	replay, err := h.Record(ctx, data, &principal)
	assertReceiptSame(t, original, assertReceived(t, replay, err))
}

func TestPGPoolCompetition(t *testing.T) {
	runPoolCompetition(t, func(t *testing.T) *ownedFixture { return database(t) })
}
func TestSQLitePoolCompetition(t *testing.T) {
	runPoolCompetition(t, func(t *testing.T) *ownedFixture { return sqliteDatabase(t) })
}
func runPoolCompetition(t *testing.T, newStore func(*testing.T) *ownedFixture) {
	t.Run("ConcurrentLastQueueSlotHasOneReceiptAndOriginalRetry", func(t *testing.T) {
		fixture := newStore(t)
		store := fixture.Store()
		ctx := contextFor(t)
		h := rawHostFor(store, owner, principal)
		h.PoolControl = true
		cfg := demo.DefaultPool("last-slot", []contract.OwnerRef{owner})
		cfg.Limits[0].Queue = 1
		if err := h.InstallPool(ctx, cfg, 0); err != nil {
			t.Fatal(err)
		}
		type response struct {
			index int
			out   contract.TransportOutcome
			err   error
		}
		answers := make(chan response, 2)
		begin := make(chan struct{})
		data := [][]byte{command("a", "a", "hello", nil, future()), command("b", "b", "hello", nil, future())}
		for i := range data {
			go func(i int) { <-begin; out, err := h.Record(ctx, data[i], &principal); answers <- response{i, out, err} }(i)
		}
		close(begin)
		winner, loser := -1, -1
		for range 2 {
			r := <-answers
			if r.err == nil {
				receipt := assertReceived(t, r.out, r.err)
				if _, ok := receipt.AsApplied(); !ok || winner != -1 {
					t.Fatal("more than one last-slot winner")
				}
				winner = r.index
			} else {
				if !errors.Is(r.err, demo.ErrPoolCapacity) {
					t.Fatal(r.err)
				}
				loser = r.index
			}
		}
		if winner < 0 || loser < 0 {
			t.Fatalf("queue competition: %d %d", winner, loser)
		}
		missing, err := store.ReadCommand(ctx, contract.CommandRef{Owner: owner, CommandID: contract.ID([]string{"a", "b"}[loser])})
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := missing.AsNotFound(); !ok {
			t.Fatal("loser fixed a receipt")
		}
		w := conformanceWorker(t, owner, store, store, store, store)
		batch, err := w.Claim(ctx, "worker-a", 1, time.Minute)
		if err != nil || len(batch) != 1 {
			t.Fatalf("winner claim: %+v %v", batch, err)
		}
		if err = w.Process(ctx, batch[0]); err != nil {
			t.Fatal(err)
		}
		assertReceivedResult(t, h, ctx, data[loser])
	})
	t.Run("ConcurrentWorkersAndOwnersShareOneTenantReservation", func(t *testing.T) {
		fixture := newStore(t)
		store := fixture.Store()
		ctx := contextFor(t)
		second := contract.OwnerRef{TenantID: owner.TenantID, OwnerID: "second-owner"}
		h := rawHostFor(store, owner, principal)
		h.PoolControl = true
		cfg := demo.DefaultPool("tenant", []contract.OwnerRef{owner, second})
		cfg.Limits[0].Concurrent = 2
		cfg.Quotas[0].Concurrent = 1
		if err := h.InstallPool(ctx, cfg, 0); err != nil {
			t.Fatal(err)
		}
		workers := []*durablework.Worker{}
		for _, scope := range cfg.Members {
			hh := rawHostFor(store, scope, principal)
			wire := command(string(scope.OwnerID), "input", "hello", nil, future())
			var e contract.CommandEnvelope
			_ = json.Unmarshal(wire, &e)
			e.Target.OwnerID = scope.OwnerID
			wire, _ = json.Marshal(e)
			out, err := hh.Record(ctx, wire, &principal)
			assertReceived(t, out, err)
			workers = append(workers, conformanceWorker(t, scope, store, store, store, store))
		}
		pool, err := durablework.NewPoolWorker(h, workers)
		if err != nil {
			t.Fatal(err)
		}
		type result struct {
			dispatch *durablework.Dispatch
			err      error
		}
		responses := make(chan result, 8)
		begin := make(chan struct{})
		for range 8 {
			go func() {
				<-begin
				d, e := pool.ClaimLane(ctx, "ordinary", "worker-a", time.Minute)
				responses <- result{d, e}
			}()
		}
		close(begin)
		var winner *durablework.Dispatch
		for range 8 {
			r := <-responses
			if r.err != nil {
				t.Fatal(r.err)
			}
			if r.dispatch != nil {
				if winner != nil {
					t.Fatal("tenant reservation exceeded across owners")
				}
				winner = r.dispatch
			}
		}
		if winner == nil {
			t.Fatal("no healthy quota winner")
		}
		if err = winner.Worker.Process(ctx, winner.Work); err != nil {
			t.Fatal(err)
		}
		next, err := pool.ClaimLane(ctx, "ordinary", "worker-b", time.Minute)
		if err != nil || next == nil {
			t.Fatalf("released tenant slot: %+v %v", next, err)
		}
		if err = next.Worker.Process(ctx, next.Work); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("PoolReopenRetainsCursorAndConfiguration", func(t *testing.T) {
		fixture := newStore(t)
		store := fixture.Store()
		ctx := contextFor(t)
		h := rawHostFor(store, owner, principal)
		h.PoolControl = true
		cfg := demo.DefaultPool("reopen", []contract.OwnerRef{owner})
		if err := h.InstallPool(ctx, cfg, 0); err != nil {
			t.Fatal(err)
		}
		assertReceivedResult(t, h, ctx, command("source", "input", "hello", nil, future()))
		w := conformanceWorker(t, owner, store, store, store, store)
		batch, err := w.Claim(ctx, "worker-a", 1, time.Minute)
		if err != nil || len(batch) != 1 {
			t.Fatalf("claim: %+v %v", batch, err)
		}
		startWork(t, w, batch[0])
		next := fixture.Replace(t)
		hh := rawHostFor(next, owner, principal)
		hh.PoolControl = true
		observed, err := hh.ObservePool(ctx)
		if err != nil || observed.State.Config.Revision != 1 || observed.State.Cursors["ordinary"].Sequence != 1 || observed.Active["ordinary"] != 1 {
			t.Fatalf("reopened pool state: %+v %v", observed, err)
		}
		ww := conformanceWorker(t, owner, next, next, next, next)
		if err = ww.Complete(ctx, batch[0].Claim, demo.Project(batch[0])); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("ConfigurationRevisionDrainAndQueueOverhang", func(t *testing.T) {
		fixture := newStore(t)
		store := fixture.Store()
		ctx := contextFor(t)
		h := rawHostFor(store, owner, principal)
		h.PoolControl = true
		cfg := demo.DefaultPool("config", []contract.OwnerRef{owner})
		cfg.Limits[0].Concurrent = 2
		cfg.Quotas[0].Concurrent = 2
		if err := h.InstallPool(ctx, cfg, 0); err != nil {
			t.Fatal(err)
		}
		originalBytes := map[string][]byte{}
		for _, id := range []string{"a", "b", "c"} {
			originalBytes[id] = command(id, id, "hello", nil, future())
			assertReceivedResult(t, h, ctx, originalBytes[id])
		}
		w := conformanceWorker(t, owner, store, store, store, store)
		batch, err := w.Claim(ctx, "worker-a", 2, time.Minute)
		if err != nil || len(batch) != 2 {
			t.Fatalf("two claims: %+v %v", batch, err)
		}
		lowered := cfg
		lowered.Limits = append([]demo.LaneLimit{}, cfg.Limits...)
		lowered.Quotas = append([]demo.TenantQuota{}, cfg.Quotas...)
		lowered.Limits[0].Concurrent = 1
		lowered.Quotas[0].Concurrent = 1
		if err = h.InstallPool(ctx, lowered, 1); !errors.Is(err, demo.ErrPoolCapacity) {
			t.Fatalf("unsafe downsize: %v", err)
		}
		shrunk := cfg
		shrunk.Limits = append([]demo.LaneLimit{}, cfg.Limits...)
		shrunk.Limits[0].Queue = 1
		if err = h.InstallPool(ctx, shrunk, 1); err != nil {
			t.Fatal(err)
		}
		if err = h.InstallPool(ctx, cfg, 1); !errors.Is(err, demo.ErrPoolConfig) {
			t.Fatalf("stale config revision: %v", err)
		}
		removed := demo.DefaultPool("config", []contract.OwnerRef{{TenantID: owner.TenantID, OwnerID: "replacement"}})
		if err = h.InstallPool(ctx, removed, 2); !errors.Is(err, demo.ErrPoolCapacity) {
			t.Fatalf("undrained member removed: %v", err)
		}
		observed, err := h.ObservePool(ctx)
		if err != nil || observed.Queued["ordinary"] != 3 || observed.Active["ordinary"] != 2 {
			t.Fatalf("downsize discarded responsibility: %+v %v", observed, err)
		}
		if _, err = h.Record(ctx, command("overhang", "other", "hello", nil, future()), &principal); !errors.Is(err, demo.ErrPoolCapacity) {
			t.Fatal(err)
		}
		for _, work := range batch {
			if err = w.Process(ctx, work); err != nil {
				t.Fatal(err)
			}
		}
		batch, err = w.Claim(ctx, "worker-b", 1, time.Minute)
		if err != nil || len(batch) != 1 {
			t.Fatalf("retained third responsibility: %+v %v", batch, err)
		}
		if err = w.Process(ctx, batch[0]); err != nil {
			t.Fatal(err)
		}
		if err = h.InstallPool(ctx, removed, 2); err != nil {
			t.Fatal(err)
		}
		// No configured owner remains at this consumer; original-key replay and
		// authorized reads are still independent of fresh admission configuration.
		replay, err := h.Record(ctx, originalBytes["a"], &principal)
		replayed := assertReceived(t, replay, err)
		if _, ok := replayed.AsApplied(); !ok {
			t.Fatal("missing configuration displaced original replay")
		}
		var changed contract.CommandEnvelope
		_ = json.Unmarshal(originalBytes["a"], &changed)
		changed.Payload, _ = json.Marshal(map[string]string{"text": "changed"})
		changedWire, _ := json.Marshal(changed)
		_, err = h.Record(ctx, changedWire, &principal)
		assertReason(t, err, "idempotency_conflict")
		originalFound, err := store.ReadCommand(ctx, contract.CommandRef{Owner: owner, CommandID: "a"})
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := originalFound.AsFound(); !ok {
			t.Fatal("unregistered read lost original")
		}
		if _, err = h.Record(ctx, command("missing-after-drain", "new", "hello", nil, "2000-01-01T00:00:00.000000Z"), &principal); !errors.Is(err, demo.ErrPoolMissing) {
			t.Fatalf("missing config saved expired decision: %v", err)
		}
	})
}

func TestPGPoolMaintenance(t *testing.T) {
	runPoolMaintenance(t, func(t *testing.T) *ownedFixture { return database(t) })
}
func TestSQLitePoolMaintenance(t *testing.T) {
	runPoolMaintenance(t, func(t *testing.T) *ownedFixture { return sqliteDatabase(t) })
}
func runPoolMaintenance(t *testing.T, newStore func(*testing.T) *ownedFixture) {
	t.Run("ExpiredStartedClaimIsFencedWithoutErasingNewRevision", func(t *testing.T) {
		fixture := newStore(t)
		store := fixture.Store()
		ctx := contextFor(t)
		clock := &workClock{now: time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC)}
		h := rawHostFor(store, owner, principal)
		h.Clock = clock
		h.PoolControl = true
		cfg := demo.DefaultPool("expiry", []contract.OwnerRef{owner})
		cfg.Limits[0].Concurrent = 1
		cfg.Quotas[0].Concurrent = 1
		if err := h.InstallPool(ctx, cfg, 0); err != nil {
			t.Fatal(err)
		}
		policy := demo.DefaultPolicy()
		policy.ExecutionLimit = time.Second
		h.Policies, _ = demo.NewPolicies([]demo.PolicyBinding{{Owner: owner, ObjectID: "input", Policy: policy}})
		assertReceivedResult(t, h, ctx, command("old", "input", "hello", nil, future()))
		w := conformanceWorker(t, owner, store, store, store, clock)
		batch, err := w.Claim(ctx, "worker-a", 1, time.Minute)
		if err != nil || len(batch) != 1 {
			t.Fatalf("old claim: %+v %v", batch, err)
		}
		startWork(t, w, batch[0])
		old := batch[0]
		h.Policies = nil
		rev := contract.Revision("1")
		assertReceivedResult(t, h, ctx, command("new", "input", "next", &rev, future()))
		clock.Advance(time.Second)
		pool, err := durablework.NewPoolWorker(h, []*durablework.Worker{w})
		if err != nil {
			t.Fatal(err)
		}
		if err = pool.Maintain(ctx); err != nil {
			t.Fatal(err)
		}
		oldState, err := h.ObserveSchedule(ctx, "input", 1, &principal)
		if err != nil || oldState.State.Outcome != "expired" || oldState.State.Attempts != 1 || oldState.Job.State != "ready" || oldState.Job.WorkRevision != 2 || oldState.Job.CompletedRevision != 1 {
			t.Fatalf("exact expiry lost new work: %+v %v", oldState, err)
		}
		if err = w.Complete(ctx, old.Claim, demo.Project(old)); !errors.Is(err, runtime.ErrClaim) {
			t.Fatalf("late expired finish: %v", err)
		}
		if _, err = w.Renew(ctx, old.Claim, time.Minute); !errors.Is(err, runtime.ErrClaim) {
			t.Fatalf("late expired renew: %v", err)
		}
		current, err := w.Claim(ctx, "worker-b", 1, time.Minute)
		if err != nil || len(current) != 1 || current[0].Claim.JobID != old.Claim.JobID {
			t.Fatalf("preserved new responsibility: %+v %v", current, err)
		}
		if err = w.Complete(ctx, old.Claim, demo.Project(old)); !errors.Is(err, runtime.ErrClaim) {
			t.Fatal(err)
		}
		reservations, err := h.ObservePool(ctx)
		if err != nil || reservations.Active["ordinary"] != 1 {
			t.Fatalf("late message released new slot: %+v %v", reservations, err)
		}
		if err = w.Process(ctx, current[0]); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("LatestExpiryPreservesDifferentRevisionValidClaim", func(t *testing.T) {
		fixture := newStore(t)
		store := fixture.Store()
		ctx := contextFor(t)
		clock := &workClock{now: time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC)}
		h := rawHostFor(store, owner, principal)
		h.Clock = clock
		h.PoolControl = true
		if err := h.InstallPool(ctx, demo.DefaultPool("different", []contract.OwnerRef{owner}), 0); err != nil {
			t.Fatal(err)
		}
		assertReceivedResult(t, h, ctx, command("old", "input", "hello", nil, future()))
		w := conformanceWorker(t, owner, store, store, store, clock)
		batch, err := w.Claim(ctx, "worker-a", 1, time.Minute)
		if err != nil || len(batch) != 1 {
			t.Fatalf("claim: %+v %v", batch, err)
		}
		startWork(t, w, batch[0])
		policy := demo.DefaultPolicy()
		policy.ExecutionLimit = time.Second
		h.Policies, _ = demo.NewPolicies([]demo.PolicyBinding{{Owner: owner, ObjectID: "input", Policy: policy}})
		rev := contract.Revision("1")
		assertReceivedResult(t, h, ctx, command("latest", "input", "next", &rev, future()))
		clock.Advance(time.Second)
		pool, err := durablework.NewPoolWorker(h, []*durablework.Worker{w})
		if err != nil {
			t.Fatal(err)
		}
		if err = pool.Maintain(ctx); err != nil {
			t.Fatal(err)
		}
		observed, err := h.ObserveSchedule(ctx, "input", 2, &principal)
		if err != nil || observed.State.Outcome != "expired" || observed.State.Attempts != 0 || observed.Job.State != "leased" || observed.Job.CompletedRevision != 0 {
			t.Fatalf("different revision claim stolen: %+v %v", observed, err)
		}
		if err = w.Complete(ctx, batch[0].Claim, demo.Project(batch[0])); err != nil {
			t.Fatal(err)
		}
		if err = pool.Maintain(ctx); err != nil {
			t.Fatal(err)
		}
		got, err := h.Observe(ctx, "input", &principal)
		if err != nil || got.Job.State != "done" || got.Job.CompletedRevision != 2 || got.Projection == nil || got.Projection.InputRevision != 1 || got.Projection.TextDigest != "sha256:2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824" {
			t.Fatalf("latest closure or old success lost: %+v %v", got, err)
		}
	})
	t.Run("ZeroQuotaMaintenancePagesAndReopensPast64", func(t *testing.T) {
		fixture := newStore(t)
		store := fixture.Store()
		ctx := contextFor(t)
		clock := &workClock{now: time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC)}
		h := rawHostFor(store, owner, principal)
		h.Clock = clock
		h.PoolControl = true
		cfg := demo.DefaultPool("pages", []contract.OwnerRef{owner})
		cfg.Limits[0].Queue = 128
		cfg.Quotas[0].Concurrent = 0
		if err := h.InstallPool(ctx, cfg, 0); err != nil {
			t.Fatal(err)
		}
		bindings := []demo.PolicyBinding{}
		policy := demo.DefaultPolicy()
		policy.ExecutionLimit = time.Second
		for i := 0; i < 64; i++ {
			bindings = append(bindings, demo.PolicyBinding{Owner: owner, ObjectID: contract.ID(fmt.Sprintf("expired-%02d", i)), Policy: policy})
		}
		h.Policies, _ = demo.NewPolicies(bindings)
		for i := 0; i < 65; i++ {
			id := fmt.Sprintf("expired-%02d", i)
			assertReceivedResult(t, h, ctx, command(id, id, "hello", nil, future()))
		}
		// The 65th normal-policy responsibility is a healthy negative control:
		// quota zero preserves it without inventing an expiry/hash.
		clock.Advance(time.Second)
		w := conformanceWorker(t, owner, store, store, store, clock)
		pool, _ := durablework.NewPoolWorker(h, []*durablework.Worker{w})
		if err := pool.Maintain(ctx); err != nil {
			t.Fatal(err)
		}
		observation, err := h.ObservePool(ctx)
		if err != nil || observation.Queued["ordinary"] < 1 || observation.State.Cursors["maintenance:"+string(owner.TenantID)+"/"+string(owner.OwnerID)].After == "" {
			t.Fatalf("maintenance page bound: %+v %v", observation, err)
		}
		next := fixture.Replace(t)
		hh := rawHostFor(next, owner, principal)
		hh.Clock = clock
		hh.PoolControl = true
		ww := conformanceWorker(t, owner, next, next, next, clock)
		continued, _ := durablework.NewPoolWorker(hh, []*durablework.Worker{ww})
		if err = continued.Maintain(ctx); err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 64; i++ {
			id := contract.ID(fmt.Sprintf("expired-%02d", i))
			observed, err := hh.ObserveSchedule(ctx, id, 1, &principal)
			if err != nil || observed.State.Outcome != "expired" || observed.State.Attempts != 0 || observed.Job.State != "done" {
				t.Fatalf("paged exact expiry %s: %+v %v", id, observed, err)
			}
		}
		control, err := hh.Observe(ctx, "expired-64", &principal)
		if err != nil || control.Job.State != "ready" || control.Projection != nil {
			t.Fatalf("normal deadline falsely expired: %+v %v", control, err)
		}
		cfg.Quotas[0].Concurrent = 4
		if err = hh.InstallPool(ctx, cfg, 1); err != nil {
			t.Fatal(err)
		}
		processed, err := continued.StepLane(ctx, "ordinary", "worker-a", time.Minute)
		if err != nil || !processed {
			t.Fatalf("positive quota normal control: %v %v", processed, err)
		}
	})
}

func TestPGPoolRawClaimCannotAcquireReservationOnReconfigure(t *testing.T) {
	poolRawClaim(t, database(t).Store())
}
func TestSQLitePoolRawClaimCannotAcquireReservationOnReconfigure(t *testing.T) {
	poolRawClaim(t, sqliteDatabase(t).Store())
}
func poolRawClaim(t *testing.T, store workStore) {
	ctx := contextFor(t)
	h := rawHostFor(store, owner, principal)
	h.PoolControl = true
	cfg := demo.DefaultPool("raw", []contract.OwnerRef{owner})
	if err := h.InstallPool(ctx, cfg, 0); err != nil {
		t.Fatal(err)
	}
	assertReceivedResult(t, h, ctx, command("source", "input", "hello", nil, future()))
	var work durablework.Work
	err := store.Within(ctx, owner, func(ctx context.Context, tx runtime.Tx) error {
		input, err := store.LockInput(ctx, tx, owner, "input")
		if err != nil {
			return err
		}
		now, err := store.Now(ctx, tx)
		if err != nil {
			return err
		}
		jobs, err := store.Scan(ctx, tx, now, 1)
		if err != nil || len(jobs) != 1 {
			return fmt.Errorf("raw mechanism scan: %v", err)
		}
		claim, err := store.Claim(ctx, tx, jobs[0], "worker-a", now, now.Add(time.Minute))
		if err != nil || claim == nil {
			return fmt.Errorf("raw mechanism claim: %v", err)
		}
		work = durablework.Work{Claim: *claim, Input: *input}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	w := conformanceWorker(t, owner, store, store, store, store)
	if _, _, err = w.Start(ctx, work); !errors.Is(err, runtime.ErrClaim) {
		t.Fatalf("raw claim bypassed reservation: %v", err)
	}
	if err = h.InstallPool(ctx, cfg, 1); err != nil {
		t.Fatal(err)
	}
	if _, _, err = w.Start(ctx, work); !errors.Is(err, runtime.ErrClaim) {
		t.Fatalf("reconfigure laundered raw claim reservation: %v", err)
	}
}

func TestPGPoolEligibilityReturnsAtTail(t *testing.T) { poolEligibilityTail(t, database(t).Store()) }
func TestSQLitePoolEligibilityReturnsAtTail(t *testing.T) {
	poolEligibilityTail(t, sqliteDatabase(t).Store())
}
func poolEligibilityTail(t *testing.T, store workStore) {
	ctx := contextFor(t)
	clock := &workClock{now: time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC)}
	members := []contract.OwnerRef{owner, {TenantID: "tenant-two", OwnerID: "two"}, {TenantID: "tenant-three", OwnerID: "three"}}
	h := rawHostFor(store, owner, principal)
	h.PoolControl = true
	h.Clock = clock
	cfg := demo.DefaultPool("tail", members)
	cfg.Limits[0].Concurrent = 1
	for i := range cfg.Quotas {
		if cfg.Quotas[i].Lane == "ordinary" {
			cfg.Quotas[i].Concurrent = 1
		}
	}
	if err := h.InstallPool(ctx, cfg, 0); err != nil {
		t.Fatal(err)
	}
	workers := []*durablework.Worker{}
	for _, scope := range members {
		subject := principal
		subject.TenantID = scope.TenantID
		hh := rawHostFor(store, scope, subject)
		hh.Clock = clock
		for i := 0; i < 3; i++ {
			id := fmt.Sprintf("job-%d", i)
			var e contract.CommandEnvelope
			_ = json.Unmarshal(command(id, id, "hello", nil, future()), &e)
			e.Target.TenantID = scope.TenantID
			e.Target.OwnerID = scope.OwnerID
			wire, _ := json.Marshal(e)
			out, err := hh.Record(ctx, wire, &subject)
			assertReceived(t, out, err)
		}
		workers = append(workers, conformanceWorker(t, scope, store, store, store, clock))
	}
	pool, _ := durablework.NewPoolWorker(h, workers)
	consume := func(expected contract.OwnerRef) {
		t.Helper()
		dispatch, err := pool.ClaimLane(ctx, "ordinary", "worker-a", time.Minute)
		if err != nil || dispatch == nil || dispatch.Worker.Owner != expected {
			t.Fatalf("FIFO owner: %+v expected %v err %v", dispatch, expected, err)
		}
		if err = dispatch.Worker.Process(ctx, dispatch.Work); err != nil {
			t.Fatal(err)
		}
	}
	consume(members[0])
	cfg.Quotas[0].Concurrent = 0
	if err := h.InstallPool(ctx, cfg, 1); err != nil {
		t.Fatal(err)
	}
	consume(members[2]) // stable identity tie orders tenant-three before tenant-two
	cfg.Quotas[0].Concurrent = 1
	if err := h.InstallPool(ctx, cfg, 2); err != nil {
		t.Fatal(err)
	}
	consume(members[1])
	consume(members[2])
	consume(members[0])
}

func TestPGPoolRunReservedLaneProgress(t *testing.T) { poolRunReservedLanes(t, database(t).Store()) }
func TestSQLitePoolRunReservedLaneProgress(t *testing.T) {
	poolRunReservedLanes(t, sqliteDatabase(t).Store())
}
func poolRunReservedLanes(t *testing.T, store workStore) {
	ctx := contextFor(t)
	h := rawHostFor(store, owner, principal)
	h.PoolControl = true
	cfg := demo.DefaultPool("run", []contract.OwnerRef{owner})
	cfg.Limits[0].Concurrent = 1
	cfg.Quotas[0].Concurrent = 1
	if err := h.InstallPool(ctx, cfg, 0); err != nil {
		t.Fatal(err)
	}
	control := demo.DefaultPolicy()
	control.Lane = "control"
	reconcile := demo.DefaultPolicy()
	reconcile.Lane = "reconciliation"
	h.Policies, _ = demo.NewPolicies([]demo.PolicyBinding{{Owner: owner, ObjectID: "control", Policy: control}, {Owner: owner, ObjectID: "reconcile", Policy: reconcile}})
	for _, id := range []string{"ordinary", "control", "reconcile"} {
		assertReceivedResult(t, h, ctx, command(id, id, "hello", nil, future()))
	}
	w := conformanceWorker(t, owner, store, store, store, store)
	batch, err := w.Claim(ctx, "worker-a", 1, time.Minute)
	if err != nil || len(batch) != 1 {
		t.Fatalf("ordinary saturation: %+v %v", batch, err)
	}
	startWork(t, w, batch[0])
	pool, err := durablework.NewPoolWorker(h, []*durablework.Worker{w})
	if err != nil {
		t.Fatal(err)
	}
	running, cancel := context.WithCancel(ctx)
	defer cancel()
	finished := make(chan error, 1)
	go func() { finished <- pool.Run(running, "worker-b", time.Minute, 10*time.Millisecond) }()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		complete := true
		for _, id := range []contract.ID{"control", "reconcile"} {
			got, err := h.Observe(ctx, id, &principal)
			if err != nil {
				t.Fatal(err)
			}
			if got.Projection == nil {
				complete = false
			} else if got.Projection.TextDigest != "sha256:2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824" {
				t.Fatal("Run persisted incorrect hash")
			}
		}
		if complete {
			break
		}
		select {
		case err := <-finished:
			t.Fatalf("Run stopped before reserved progress: %v", err)
		case <-ticker.C:
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	cancel()
	if err = <-finished; !errors.Is(err, context.Canceled) {
		t.Fatalf("owned Run lifecycle: %v", err)
	}
	observed, err := h.ObservePool(ctx)
	if err != nil || observed.Active["ordinary"] != 1 || observed.Active["control"] != 0 || observed.Active["reconciliation"] != 0 {
		t.Fatalf("reserved actual service: %+v %v", observed, err)
	}
	if err = w.Complete(ctx, batch[0].Claim, demo.Project(batch[0])); err != nil {
		t.Fatal(err)
	}
}

func TestPGPoolLockedPagePreservesFairHeadAndMaintenanceCursor(t *testing.T) {
	fixture := database(t)
	store := fixture.PG()
	ctx := contextFor(t)
	clock := &workClock{now: time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC)}
	h := rawHostFor(store, owner, principal)
	h.Clock = clock
	h.PoolControl = true
	cfg := demo.DefaultPool("locked-page", []contract.OwnerRef{owner})
	cfg.Limits[0].Queue = 128
	if err := h.InstallPool(ctx, cfg, 0); err != nil {
		t.Fatal(err)
	}
	type item struct {
		id  contract.ID
		job contract.ID
	}
	items := []item{}
	for i := 0; i < 65; i++ {
		id := fmt.Sprintf("prefix-%02d", i)
		assertReceivedResult(t, h, ctx, command(id, id, "hello", nil, future()))
		got, err := h.Observe(ctx, contract.ID(id), &principal)
		if err != nil {
			t.Fatal(err)
		}
		items = append(items, item{contract.ID(id), got.Job.ID})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].job < items[j].job })
	// The fault holder must stay live throughout the assertions. Its larger,
	// still finite transaction limit is test infrastructure only; the business
	// Store keeps its normal 3s transaction limit and execution policies.
	holder := fixture.PGPeer(t, 0, 10*time.Second)
	held := fixture.holdPGTransaction(t, holder, func(ctx context.Context, tx runtime.Tx) error {
		for _, item := range items[:64] {
			if _, err := holder.LockInput(ctx, tx, owner, item.id); err != nil {
				return err
			}
		}
		return nil
	})
	checkHeld := func() { held.Check(t) }
	checkHeld()
	w := conformanceWorker(t, owner, store, store, store, clock)
	pool, _ := durablework.NewPoolWorker(h, []*durablework.Worker{w})
	blocked, err := pool.ClaimLane(ctx, "ordinary", "worker-a", time.Minute)
	checkHeld()
	if err != nil || blocked != nil {
		t.Fatalf("bounded locked prefix: %+v %v", blocked, err)
	}
	state, err := h.ObservePool(ctx)
	if err != nil || state.State.Cursors["ordinary"].Sequence != 0 || state.State.Cursors["ordinary"].After == "" {
		t.Fatalf("unresolved head discarded: %+v %v", state, err)
	}
	healthy, err := pool.ClaimLane(ctx, "ordinary", "worker-a", time.Minute)
	checkHeld()
	if err != nil || healthy == nil || healthy.Work.Input.ID != items[64].id {
		t.Fatalf("healthy suffix starved: %+v %v", healthy, err)
	}
	if err = healthy.Worker.Process(ctx, healthy.Work); err != nil {
		t.Fatal(err)
	}
	// Restore the highest-key Job as new, unstarted responsibility. Its old
	// success remains a historical projection; quota zero maintenance must find
	// and close the new revision behind the still genuinely locked prefix.
	rev := contract.Revision("1")
	assertReceivedResult(t, h, ctx, command("suffix-new", string(items[64].id), "hello", &rev, future()))
	cfg.Quotas[0].Concurrent = 0
	if err = h.InstallPool(ctx, cfg, 1); err != nil {
		t.Fatal(err)
	}
	clock.Advance(5 * time.Minute)
	if err = pool.Maintain(ctx); err != nil {
		t.Fatal(err)
	}
	if err = pool.Maintain(ctx); err != nil {
		t.Fatal(err)
	}
	checkHeld()
	expired, err := h.ObserveSchedule(ctx, items[64].id, 2, &principal)
	if err != nil || expired.State.Outcome != "expired" || expired.State.Attempts != 0 || expired.Job.CompletedRevision != 2 {
		t.Fatalf("maintenance suffix behind real locks: %+v %v", expired, err)
	}
	checkHeld()
	err = held.ReleaseAndJoin()
	if err != nil {
		t.Fatal(err)
	}
	// One real SQL input-lock prefix is PG-only. SQLite's single BEGIN IMMEDIATE
	// writer cannot hold this prefix while allowing an independent writer; its
	// shared 65-item maintenance/reopen test verifies its actual page mechanism.
}

func TestPGPoolRejectsForeignStorageDispatch(t *testing.T) {
	poolForeignDispatch(t, func(t *testing.T) *ownedFixture { return database(t) })
}
func TestSQLitePoolRejectsForeignStorageDispatch(t *testing.T) {
	poolForeignDispatch(t, func(t *testing.T) *ownedFixture { return sqliteDatabase(t) })
}
func poolForeignDispatch(t *testing.T, newStore func(*testing.T) *ownedFixture) {
	ctx := contextFor(t)
	first, second := newStore(t).Store(), newStore(t).Store()
	h := rawHostFor(first, owner, principal)
	foreign := rawHostFor(second, owner, principal)
	h.PoolControl = true
	foreign.PoolControl = true
	cfg := demo.DefaultPool("same-id", []contract.OwnerRef{owner})
	for _, hh := range []*durablework.Host{h, foreign} {
		if err := hh.InstallPool(ctx, cfg, 0); err != nil {
			t.Fatal(err)
		}
		assertReceivedResult(t, hh, ctx, command("original", "input", "hello", nil, future()))
	}
	worker := conformanceWorker(t, owner, second, second, second, second)
	pool, _ := durablework.NewPoolWorker(h, []*durablework.Worker{worker})
	if dispatch, err := pool.ClaimLane(ctx, "ordinary", "worker-a", time.Minute); !errors.Is(err, runtime.ErrScope) || dispatch != nil {
		t.Fatalf("cross storage pool dispatch: %+v %v", dispatch, err)
	}
	if err := pool.Maintain(ctx); !errors.Is(err, runtime.ErrScope) {
		t.Fatalf("cross storage maintenance: %v", err)
	}
	for _, hh := range []*durablework.Host{h, foreign} {
		observed, err := hh.ObservePool(ctx)
		if err != nil || observed.Active["ordinary"] != 0 || observed.Queued["ordinary"] != 1 {
			t.Fatalf("cross scope changed responsibility: %+v %v", observed, err)
		}
	}
}

func TestPGPoolNewTenantJoinsExistingTailAfterReopen(t *testing.T) {
	poolNewTenantTail(t, func(t *testing.T) *ownedFixture { return database(t) })
}
func TestSQLitePoolNewTenantJoinsExistingTailAfterReopen(t *testing.T) {
	poolNewTenantTail(t, func(t *testing.T) *ownedFixture { return sqliteDatabase(t) })
}
func poolNewTenantTail(t *testing.T, newStore func(*testing.T) *ownedFixture) {
	fixture := newStore(t)
	store := fixture.Store()
	ctx := contextFor(t)
	clock := &workClock{now: time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC)}
	members := []contract.OwnerRef{owner, {TenantID: "tenant-two", OwnerID: "two"}, {TenantID: "tenant-three", OwnerID: "three"}}
	h := rawHostFor(store, owner, principal)
	h.Clock = clock
	h.PoolControl = true
	cfg := demo.DefaultPool("new-tail", members)
	cfg.Limits[0].Concurrent = 1
	for i := range cfg.Quotas {
		if cfg.Quotas[i].Lane == "ordinary" {
			cfg.Quotas[i].Concurrent = 1
		}
	}
	if err := h.InstallPool(ctx, cfg, 0); err != nil {
		t.Fatal(err)
	}
	record := func(scope contract.OwnerRef, id string) {
		t.Helper()
		subject := principal
		subject.TenantID = scope.TenantID
		hh := rawHostFor(store, scope, subject)
		hh.Clock = clock
		var e contract.CommandEnvelope
		_ = json.Unmarshal(command(id, id, "hello", nil, future()), &e)
		e.Target.TenantID = scope.TenantID
		e.Target.OwnerID = scope.OwnerID
		wire, _ := json.Marshal(e)
		out, err := hh.Record(ctx, wire, &subject)
		assertReceived(t, out, err)
	}
	for _, scope := range []contract.OwnerRef{members[0], members[2]} {
		record(scope, "first")
		record(scope, "second")
	}
	assemble := func(current workStore, anchor *durablework.Host) *durablework.PoolWorker {
		t.Helper()
		workers := []*durablework.Worker{}
		for _, scope := range members {
			workers = append(workers, conformanceWorker(t, scope, current, current, current, clock))
		}
		pool, err := durablework.NewPoolWorker(anchor, workers)
		if err != nil {
			t.Fatal(err)
		}
		return pool
	}
	pool := assemble(store, h)
	consume := func(pool *durablework.PoolWorker, expected contract.OwnerRef) {
		t.Helper()
		d, err := pool.ClaimLane(ctx, "ordinary", "worker-a", time.Minute)
		if err != nil || d == nil || d.Worker.Owner != expected {
			t.Fatalf("new tenant tail: %+v expected %v err %v", d, expected, err)
		}
		if err = d.Worker.Process(ctx, d.Work); err != nil {
			t.Fatal(err)
		}
	}
	consume(pool, members[0])
	record(members[1], "late")
	// Existing C then A retain their order. New B joins behind both, even
	// though B's lexical identity would be reached earlier in a static ring.
	consume(pool, members[2])
	store = fixture.Replace(t)
	h = rawHostFor(store, owner, principal)
	h.Clock = clock
	h.PoolControl = true
	pool = assemble(store, h)
	observed, err := h.ObservePool(ctx)
	if err != nil || len(observed.State.Cursors["ordinary"].Order) != 3 || observed.State.Cursors["ordinary"].Order[0] != members[0].TenantID || observed.State.Cursors["ordinary"].Waiting[members[1].TenantID].IsZero() {
		t.Fatalf("persisted FIFO/waiting: %+v %v", observed, err)
	}
	consume(pool, members[0])
	consume(pool, members[1])
}

func TestPGPoolIndependentStoresShareCapacityAndSchemasKeepLockScope(t *testing.T) {
	t.Run("SameScopeIndependentStores", func(t *testing.T) {
		fixture := database(t)
		first := fixture.PG()
		second := fixture.PGPeer(t, 0, 0)
		ctx := contextFor(t)
		h := rawHostFor(first, owner, principal)
		h.PoolControl = true
		cfg := demo.DefaultPool("stores", []contract.OwnerRef{owner})
		cfg.Limits[0].Concurrent = 1
		cfg.Quotas[0].Concurrent = 1
		if err := h.InstallPool(ctx, cfg, 0); err != nil {
			t.Fatal(err)
		}
		for _, id := range []string{"a", "b"} {
			assertReceivedResult(t, h, ctx, command(id, id, "hello", nil, future()))
		}
		workers := []*durablework.Worker{conformanceWorker(t, owner, first, first, first, first), conformanceWorker(t, owner, second, second, second, second)}
		anchors := []*durablework.Host{h, rawHostFor(second, owner, principal)}
		type result struct {
			d *durablework.Dispatch
			e error
		}
		responses := make(chan result, 2)
		begin := make(chan struct{})
		for i := range workers {
			p, err := durablework.NewPoolWorker(anchors[i], []*durablework.Worker{workers[i]})
			if err != nil {
				t.Fatal(err)
			}
			go func() {
				<-begin
				d, e := p.ClaimLane(ctx, "ordinary", "worker-a", time.Minute)
				responses <- result{d, e}
			}()
		}
		close(begin)
		var winner *durablework.Dispatch
		for range 2 {
			r := <-responses
			if r.e != nil {
				t.Fatal(r.e)
			}
			if r.d != nil {
				if winner != nil {
					t.Fatal("independent Stores exceeded shared capacity")
				}
				winner = r.d
			}
		}
		if winner == nil {
			t.Fatal("no independent Store winner")
		}
		if err := winner.Worker.Process(ctx, winner.Work); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("DifferentSchemasDoNotShareRegistryLock", func(t *testing.T) {
		first, second := database(t).PG(), database(t).PG()
		ctx := contextFor(t)
		h := rawHostFor(first, owner, principal)
		other := rawHostFor(second, owner, principal)
		h.PoolControl = true
		other.PoolControl = true
		cfg := demo.DefaultPool("same-id", []contract.OwnerRef{owner})
		for _, hh := range []*durablework.Host{h, other} {
			if err := hh.InstallPool(ctx, cfg, 0); err != nil {
				t.Fatal(err)
			}
		}
		ready, release := make(chan struct{}), make(chan struct{})
		done := make(chan error, 1)
		var once sync.Once
		unblock := func() { once.Do(func() { close(release) }) }
		defer unblock()
		go func() {
			done <- first.Within(ctx, owner, func(ctx context.Context, tx runtime.Tx) error {
				if _, err := first.LockPool(ctx, tx); err != nil {
					return err
				}
				close(ready)
				select {
				case <-release:
					return nil
				case <-ctx.Done():
					return ctx.Err()
				}
			})
		}()
		awaitStage(t, ctx, ready)
		assertReceivedResult(t, other, ctx, command("source", "input", "hello", nil, future()))
		w := conformanceWorker(t, owner, second, second, second, second)
		batch, err := w.Claim(ctx, "worker-a", 1, time.Minute)
		if err != nil || len(batch) != 1 {
			t.Fatalf("different schema pool blocked: %+v %v", batch, err)
		}
		if err = w.Process(ctx, batch[0]); err != nil {
			t.Fatal(err)
		}
		unblock()
		if err = <-done; err != nil {
			t.Fatal(err)
		}
	})
}

func TestSQLitePoolRejectsIdenticalDatabaseCopiedToAnotherFile(t *testing.T) {
	fixture := sqliteDatabase(t)
	original := fixture.Store()
	ctx := contextFor(t)
	h := rawHostFor(original, owner, principal)
	h.PoolControl = true
	if err := h.InstallPool(ctx, demo.DefaultPool("copy", []contract.OwnerRef{owner}), 0); err != nil {
		t.Fatal(err)
	}
	assertReceivedResult(t, h, ctx, command("original", "input", "hello", nil, future()))
	copied := fixture.CopySQLite(t).Store()
	restored := fixture.Replace(t)
	var err error
	anchor := rawHostFor(restored, owner, principal)
	anchor.PoolControl = true
	worker := conformanceWorker(t, owner, copied, copied, copied, copied)
	pool, _ := durablework.NewPoolWorker(anchor, []*durablework.Worker{worker})
	if dispatch, err := pool.ClaimLane(ctx, "ordinary", "worker-a", time.Minute); !errors.Is(err, runtime.ErrScope) || dispatch != nil {
		t.Fatalf("copied nonce bypassed actual file binding: %+v %v", dispatch, err)
	}
	if err = pool.Maintain(ctx); !errors.Is(err, runtime.ErrScope) {
		t.Fatalf("copied nonce maintenance: %v", err)
	}
	correct := conformanceWorker(t, owner, restored, restored, restored, restored)
	normal, _ := durablework.NewPoolWorker(anchor, []*durablework.Worker{correct})
	dispatch, err := normal.ClaimLane(ctx, "ordinary", "worker-a", time.Minute)
	if err != nil || dispatch == nil {
		t.Fatalf("same-file normal dispatch: %+v %v", dispatch, err)
	}
	if err = dispatch.Worker.Process(ctx, dispatch.Work); err != nil {
		t.Fatal(err)
	}
}

func TestPGPoolLastStartExhaustionRequiresNoNewQuota(t *testing.T) {
	poolLastStartExhaustion(t, database(t).Store())
}
func TestSQLitePoolLastStartExhaustionRequiresNoNewQuota(t *testing.T) {
	poolLastStartExhaustion(t, sqliteDatabase(t).Store())
}
func poolLastStartExhaustion(t *testing.T, store workStore) {
	ctx := contextFor(t)
	clock := &workClock{now: time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC)}
	h := rawHostFor(store, owner, principal)
	h.Clock = clock
	h.PoolControl = true
	cfg := demo.DefaultPool("last-start", []contract.OwnerRef{owner})
	if err := h.InstallPool(ctx, cfg, 0); err != nil {
		t.Fatal(err)
	}
	p := demo.DefaultPolicy()
	p.MaxAttempts = 1
	h.Policies, _ = demo.NewPolicies([]demo.PolicyBinding{{Owner: owner, ObjectID: "input", Policy: p}})
	assertReceivedResult(t, h, ctx, command("source", "input", "hello", nil, future()))
	w := conformanceWorker(t, owner, store, store, store, clock)
	batch, err := w.Claim(ctx, "worker-a", 1, time.Second)
	if err != nil || len(batch) != 1 {
		t.Fatalf("last start claim: %+v %v", batch, err)
	}
	startWork(t, w, batch[0])
	pool, _ := durablework.NewPoolWorker(h, []*durablework.Worker{w})
	if err = pool.Maintain(ctx); err != nil {
		t.Fatal(err)
	}
	before, err := h.ObserveSchedule(ctx, "input", 1, &principal)
	if err != nil || before.State.Outcome != "running" || !before.ActiveClaim {
		t.Fatalf("valid last start prematurely failed: %+v %v", before, err)
	}
	clock.Advance(time.Second)
	cfg.Quotas[0].Concurrent = 0
	if err = h.InstallPool(ctx, cfg, 1); err != nil {
		t.Fatal(err)
	}
	if err = pool.Maintain(ctx); err != nil {
		t.Fatal(err)
	}
	after, err := h.ObserveSchedule(ctx, "input", 1, &principal)
	if err != nil || after.State.Outcome != "permanent" || after.State.Reason != "attempts_exhausted" || after.State.Attempts != 1 || after.Job.State != "done" {
		t.Fatalf("finite last start closure: %+v %v", after, err)
	}
	if err = w.Complete(ctx, batch[0].Claim, demo.Project(batch[0])); !errors.Is(err, runtime.ErrClaim) {
		t.Fatalf("lost old start completed: %v", err)
	}
}
