package integration_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/ruipengliu/lerna/internal/durable"
	pg "github.com/ruipengliu/lerna/internal/storage/postgres"
	filedb "github.com/ruipengliu/lerna/internal/storage/sqlite"
	"github.com/ruipengliu/lerna/internal/storage/sqlstore"
)

var scope = durable.Scope{TenantID: "tenant-test", OwnerID: "owner-test"}
var other = durable.Scope{TenantID: "tenant-other", OwnerID: "owner-test"}

const kind = "probe"

type store interface {
	durable.Backend
	Close() error
	Stats(context.Context, durable.Scope) (sqlstore.Stats, error)
}
type suite struct {
	driver, path, appURL, adminURL string
	raw                            *sql.DB
	store                          store
	e                              *durable.Engine
	p                              durable.Participant
}

func ctx() context.Context { return context.Background() }
func mustCommit(t *testing.T, r durable.Result) {
	t.Helper()
	if r.Outcome != durable.Committed || r.Err != nil {
		t.Fatalf("outcome=%s attempts=%d err=%v", r.Outcome, r.Attempts, r.Err)
	}
}
func newSuite(t *testing.T, driver string) *suite {
	t.Helper()
	s := &suite{driver: driver}
	if driver == "postgres" {
		admin := os.Getenv("LERNA_TEST_ADMIN_URL")
		app := os.Getenv("LERNA_TEST_APP_URL")
		if admin == "" || app == "" {
			t.Skip("PostgreSQL requires make durable-check")
		}
		root, err := sql.Open("pgx", admin)
		if err != nil {
			t.Fatal("invalid test admin configuration")
		}
		name := durable.NewID("testdurable")
		if _, err = root.ExecContext(ctx(), `CREATE DATABASE "`+name+`"`); err != nil {
			root.Close()
			t.Fatal("could not create isolated test database")
		}
		changeDB := func(v string) string {
			u, err := url.Parse(v)
			if err != nil {
				t.Fatal("invalid test URL")
			}
			u.Path = "/" + name
			return u.String()
		}
		s.appURL = changeDB(app)
		s.adminURL = changeDB(admin)
		t.Cleanup(func() {
			if s.store != nil {
				s.store.Close()
			}
			if s.raw != nil {
				s.raw.Close()
			}
			_, err := root.ExecContext(ctx(), `DROP DATABASE "`+name+`" WITH (FORCE)`)
			root.Close()
			if err != nil {
				t.Error("isolated test database cleanup failed")
			}
		})
		if err := pg.Migrate(ctx(), s.adminURL); err != nil {
			t.Fatal(err)
		}
		s.raw, _ = sql.Open("pgx", s.adminURL)
	} else {
		s.path = filepath.Join(t.TempDir(), "durable.db")
		if err := filedb.Migrate(ctx(), s.path); err != nil {
			t.Fatal(err)
		}
		s.raw, _ = sql.Open("sqlite", s.path)
		s.raw.SetMaxOpenConns(1)
		t.Cleanup(func() {
			if s.store != nil {
				s.store.Close()
			}
			s.raw.Close()
		})
	}
	_, err := s.raw.ExecContext(ctx(), `CREATE TABLE durable_probe(tenant_id TEXT NOT NULL,owner_id TEXT NOT NULL,source TEXT NOT NULL,revision BIGINT NOT NULL,result BIGINT NOT NULL,PRIMARY KEY(tenant_id,owner_id,source))`)
	if err != nil {
		t.Fatal(err)
	}
	if driver == "postgres" {
		if _, err = s.raw.ExecContext(ctx(), `REVOKE CREATE ON SCHEMA public FROM PUBLIC; GRANT USAGE ON SCHEMA public TO lerna_app; GRANT SELECT,INSERT,UPDATE,DELETE ON ALL TABLES IN SCHEMA public TO lerna_app; GRANT USAGE,SELECT ON ALL SEQUENCES IN SCHEMA public TO lerna_app`); err != nil {
			t.Fatal(err)
		}
	}
	s.reopen(t)
	return s
}
func (s *suite) reopen(t *testing.T) {
	t.Helper()
	if s.store != nil {
		s.store.Close()
	}
	var err error
	if s.driver == "postgres" {
		s.store, err = pg.Open(ctx(), s.appURL, []durable.Scope{scope, other, taskScope}, 4, false)
	} else {
		s.store, err = filedb.Open(ctx(), s.path, []durable.Scope{scope, other, taskScope})
	}
	if err != nil {
		t.Fatal(err)
	}
	s.e, err = durable.New(s.store, durable.Options{Kinds: []string{kind, "control"}, QueryRetention: 0})
	if err != nil {
		t.Fatal(err)
	}
	s.p, err = s.e.Register("probe")
	if err != nil {
		t.Fatal(err)
	}
}
func (s *suite) placeholders(q string) string {
	if s.driver == "postgres" {
		return q
	}
	for i := 1; i <= 12; i++ {
		q = strings.ReplaceAll(q, fmt.Sprintf("$%d", i), fmt.Sprintf("?%d", i))
	}
	return q
}
func (s *suite) lock(tx *durable.Tx, source string) error {
	return tx.Lock(s.p, source, func(h any) error {
		db := h.(sqlstore.DBTX)
		sc := tx.Scope()
		_, err := db.ExecContext(tx.Context(), s.placeholders(`INSERT INTO durable_probe(tenant_id,owner_id,source,revision,result) VALUES($1,$2,$3,0,0) ON CONFLICT(tenant_id,owner_id,source) DO NOTHING`), sc.TenantID, sc.OwnerID, source)
		if err != nil {
			return err
		}
		query := s.placeholders(`SELECT revision FROM durable_probe WHERE tenant_id=$1 AND owner_id=$2 AND source=$3`)
		if s.driver == "postgres" {
			query += " FOR UPDATE"
		}
		var v int64
		return db.QueryRowContext(tx.Context(), query, sc.TenantID, sc.OwnerID, source).Scan(&v)
	})
}
func (s *suite) update(tx *durable.Tx, source string, revision, result int64) error {
	return tx.Use(s.p, source, func(h any) error {
		sc := tx.Scope()
		_, err := h.(sqlstore.DBTX).ExecContext(tx.Context(), s.placeholders(`UPDATE durable_probe SET revision=revision+$4,result=result+$5 WHERE tenant_id=$1 AND owner_id=$2 AND source=$3`), sc.TenantID, sc.OwnerID, source, revision, result)
		return err
	})
}
func (s *suite) value(t *testing.T, source string) (int64, int64) {
	t.Helper()
	var r, v int64
	err := s.raw.QueryRowContext(ctx(), s.placeholders(`SELECT revision,result FROM durable_probe WHERE tenant_id=$1 AND owner_id=$2 AND source=$3`), scope.TenantID, scope.OwnerID, source).Scan(&r, &v)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, 0
	}
	if err != nil {
		t.Fatal(err)
	}
	return r, v
}
func (s *suite) job(t *testing.T, source string) durable.Job {
	t.Helper()
	var j durable.Job
	err := s.raw.QueryRowContext(ctx(), s.placeholders(`SELECT job_id,source_ref,state,due_at,work_revision,lease_epoch,lease_until,holder_id FROM durable_jobs WHERE tenant_id=$1 AND owner_id=$2 AND kind=$3 AND responsibility_key=$4`), scope.TenantID, scope.OwnerID, kind, source).Scan(&j.ID, &j.SourceRef, &j.State, &j.DueAt, &j.WorkRevision, &j.LeaseEpoch, &j.LeaseUntil, &j.HolderID)
	if errors.Is(err, sql.ErrNoRows) {
		return j
	}
	if err != nil {
		t.Fatal(err)
	}
	return j
}
func intent(t *testing.T, method string, expiry time.Time, payload string) durable.FixedIntent {
	t.Helper()
	v, err := durable.FixIntent(durable.Intent{CommandID: durable.NewID("cmd"), Method: method, TargetID: durable.NewID("orc"), ExpiresAt: expiry.UTC().Format(time.RFC3339Nano), Payload: json.RawMessage(payload)})
	if err != nil {
		t.Fatal(err)
	}
	return v
}
func (s *suite) admission(source string, prepare, accepted bool) durable.Admission {
	return durable.Admission{Check: func(context.Context, durable.Scope, durable.FixedIntent) error { return nil }, Disclose: func(_ context.Context, _ durable.Scope, r durable.CommandRecord) (durable.CommandRecord, error) {
		return r, nil
	}, NewAllowed: func() bool { return true }, Participants: []durable.Participant{s.p}, Apply: func(tx *durable.Tx, i durable.FixedIntent) (durable.Decision, error) {
		if err := s.lock(tx, source); err != nil {
			return durable.Decision{}, err
		}
		var revision int64
		if err := tx.Use(s.p, source, func(h any) error {
			sc := tx.Scope()
			return h.(sqlstore.DBTX).QueryRowContext(tx.Context(), s.placeholders(`SELECT revision FROM durable_probe WHERE tenant_id=$1 AND owner_id=$2 AND source=$3`), sc.TenantID, sc.OwnerID, source).Scan(&revision)
		}); err != nil {
			return durable.Decision{}, err
		}
		if revision == 0 {
			if err := s.update(tx, source, 1, 0); err != nil {
				return durable.Decision{}, err
			}
			if _, err := tx.Raise(durable.JobKey{Kind: kind, Responsibility: source}, source, time.Now()); err != nil {
				return durable.Decision{}, err
			}
		}
		decision := durable.Decision{}
		if prepare {
			decision.Preparation = &durable.Preparation{SourceRef: source, Job: durable.JobKey{Kind: kind, Responsibility: source}}
		}
		if !prepare || accepted {
			stage := "applied"
			if accepted {
				stage = "accepted"
			}
			decision.Receipt = &durable.Receipt{CommandID: i.CommandID(), Stage: stage, ResourceID: source}
		}
		return decision, nil
	}}
}
func (s *suite) submit(t *testing.T, source string) durable.FixedIntent {
	t.Helper()
	i := intent(t, "task.submit", time.Now().Add(time.Hour), `{}`)
	_, r := s.e.Admit(ctx(), scope, "service", i, s.admission(source, false, false))
	mustCommit(t, r)
	return i
}
func (s *suite) claim(t *testing.T, holder string, lease time.Duration) durable.Claim {
	t.Helper()
	claims, r := s.e.Claim(ctx(), scope, kind, holder, 1, lease)
	mustCommit(t, r)
	if len(claims) != 1 {
		t.Fatalf("claims=%d", len(claims))
	}
	return claims[0]
}
func (s *suite) advance(t *testing.T, source string, due time.Time) {
	t.Helper()
	mustCommit(t, s.e.Within(ctx(), scope, []durable.Participant{s.p}, func(tx *durable.Tx) error {
		if err := s.lock(tx, source); err != nil {
			return err
		}
		if err := s.update(tx, source, 1, 0); err != nil {
			return err
		}
		_, err := tx.Raise(durable.JobKey{Kind: kind, Responsibility: source}, source, due)
		return err
	}))
}
func (s *suite) finish(t *testing.T, c durable.Claim, d durable.Disposition, raise bool) durable.Result {
	t.Helper()
	return s.e.Within(ctx(), scope, []durable.Participant{s.p}, func(tx *durable.Tx) error {
		if err := s.lock(tx, c.SourceRef()); err != nil {
			return err
		}
		if err := s.update(tx, c.SourceRef(), 0, 1); err != nil {
			return err
		}
		if err := tx.Guard(c); err != nil {
			return err
		}
		if raise {
			if err := s.update(tx, c.SourceRef(), 1, 0); err != nil {
				return err
			}
			if _, err := tx.Raise(c.Key(), c.SourceRef(), time.Now().Add(-time.Second)); err != nil {
				return err
			}
		}
		_, err := tx.Finish(c, d)
		return err
	})
}

// Faults act at a real database commit boundary. No in-memory store substitutes
// for persistence; after-commit faults still commit the real SQL transaction.
type faulty struct {
	durable.Backend
	before    bool
	after     func()
	remaining atomic.Int64
}
type faultySession struct {
	durable.Session
	f *faulty
}

var unknown = errors.New("test: lost commit acknowledgement")

func (f *faulty) Begin(c context.Context, s durable.Scope) (durable.Session, error) {
	x, err := f.Backend.Begin(c, s)
	if err != nil {
		return nil, err
	}
	return &faultySession{x, f}, nil
}
func (x *faultySession) Commit(c context.Context) error {
	if x.f.remaining.Add(-1) < 0 {
		return x.Session.Commit(c)
	}
	if x.f.before {
		if err := x.Session.Rollback(ctx()); err != nil {
			return err
		}
		return unknown
	}
	if err := x.Session.Commit(c); err != nil {
		return err
	}
	if x.f.after != nil {
		x.f.after()
	}
	return unknown
}
func faultEngine(t *testing.T, s *suite, before bool, after func()) *durable.Engine {
	f := &faulty{Backend: s.store, before: before, after: after}
	f.remaining.Store(1)
	e, err := durable.New(f, durable.Options{Kinds: []string{kind}})
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func TestDurableConformance(t *testing.T) {
	for _, driver := range []string{"sqlite", "postgres"} {
		t.Run(driver, func(t *testing.T) {
			t.Run("FW01_AtomicAdmissionAndOriginalIdentity", func(t *testing.T) {
				s := newSuite(t, driver)
				i := intent(t, "task.submit", time.Now().Add(time.Hour), `{"a":1}`)
				var wg sync.WaitGroup
				for n := 0; n < 12; n++ {
					wg.Add(1)
					go func() {
						defer wg.Done()
						record, r := s.e.Admit(ctx(), scope, "service", i, s.admission("one", false, false))
						if r.Outcome != durable.Committed || record.Receipt == nil || record.Receipt.Stage != "applied" {
							t.Errorf("concurrent admission: %s %v", r.Outcome, r.Err)
						}
					}()
				}
				wg.Wait()
				if r, v := s.value(t, "one"); r != 1 || v != 0 || s.job(t, "one").WorkRevision != 1 {
					t.Fatal("original responsibility duplicated")
				}
				j := intent(t, "task.submit", time.Now().Add(time.Hour), `{}`)
				_, r := s.e.Admit(ctx(), scope, "service", j, s.admission("one", false, false))
				mustCommit(t, r)
				if s.job(t, "one").WorkRevision != 1 {
					t.Fatal("business identity duplicated by new command")
				}
				changed, err := durable.FixIntent(durable.Intent{CommandID: i.CommandID(), Method: i.Method(), TargetID: i.TargetID(), ExpiresAt: time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano), Payload: json.RawMessage(`{"a":2}`)})
				if err != nil {
					t.Fatal(err)
				}
				_, r = s.e.Admit(ctx(), scope, "service", changed, s.admission("one", false, false))
				if !errors.Is(r.Err, durable.ErrConflict) {
					t.Fatal(r)
				}
				a := s.admission("one", false, false)
				a.NewAllowed = func() bool { return false }
				_, r = s.e.Admit(ctx(), scope, "service", i, a)
				mustCommit(t, r)
				a.Check = func(context.Context, durable.Scope, durable.FixedIntent) error { return durable.ErrPrecondition }
				_, r = s.e.Admit(ctx(), scope, "service", i, a)
				if r.Err == nil {
					t.Fatal("cached receipt bypassed current authorization")
				}
				s.reopen(t)
				record, r := s.e.Admit(ctx(), scope, "service", i, s.admission("one", false, false))
				mustCommit(t, r)
				if record.Receipt == nil || s.job(t, "one").WorkRevision != 1 {
					t.Fatal("restart lost original")
				}
			})
			t.Run("FW01_CommitUnknownAndRollback", func(t *testing.T) {
				s := newSuite(t, driver)
				for _, before := range []bool{true, false} {
					e := faultEngine(t, s, before, nil)
					p, err := e.Register("probe")
					if err != nil {
						t.Fatal(err)
					}
					oldE, oldP := s.e, s.p
					s.e, s.p = e, p
					source := fmt.Sprintf("fault-%t", before)
					i := intent(t, "task.submit", time.Now().Add(time.Hour), `{}`)
					record, r := e.Admit(ctx(), scope, "service", i, s.admission(source, false, false))
					if r.Outcome != durable.CommitUnknown || record.Receipt != nil {
						t.Fatal("unknown commit exposed success", r)
					}
					s.e, s.p = oldE, oldP
					if revision, _ := s.value(t, source); (before && revision != 0) || (!before && revision != 1) {
						t.Fatal("commit-boundary persistence mismatch")
					}
					_, r = s.e.Admit(ctx(), scope, "service", i, s.admission(source, false, false))
					mustCommit(t, r)
					if revision, _ := s.value(t, source); revision != 1 {
						t.Fatal("recovery repeated domain action")
					}
				}
			})
			t.Run("FW02_FinalOnlyPreparation", func(t *testing.T) {
				s := newSuite(t, driver)
				i := intent(t, "memory.create", time.Now().Add(time.Hour), `{}`)
				r, result := s.e.Admit(ctx(), scope, "service", i, s.admission("prepared", true, false))
				mustCommit(t, result)
				if r.State != "prepared" || r.Receipt != nil {
					t.Fatal("preparation exposed a receipt")
				}
				wait, cancel := context.WithTimeout(ctx(), 60*time.Millisecond)
				_, err := s.e.Await(wait, scope, durable.CommandKey{ServiceID: "service", CommandID: i.CommandID()}, s.admission("prepared", true, false).Disclose)
				cancel()
				if !errors.Is(err, durable.ErrPreparation) || !errors.Is(err, context.DeadlineExceeded) {
					t.Fatal(err)
				}
				_, result = s.e.Admit(ctx(), scope, "service", i, s.admission("prepared", true, false))
				mustCommit(t, result)
				if s.job(t, "prepared").WorkRevision != 1 {
					t.Fatal("replay reinitialized preparation")
				}
				c := s.claim(t, durable.NewID("boot"), time.Second)
				mustCommit(t, s.e.Within(ctx(), scope, []durable.Participant{s.p}, func(tx *durable.Tx) error {
					key := durable.CommandKey{ServiceID: "service", CommandID: i.CommandID()}
					if _, err := tx.Lookup(key); err != nil {
						return err
					}
					if err := s.lock(tx, "prepared"); err != nil {
						return err
					}
					if err := tx.Guard(c); err != nil {
						return err
					}
					if err := tx.Decide(key, durable.Receipt{CommandID: i.CommandID(), Stage: "applied", ResourceID: "prepared"}); err != nil {
						return err
					}
					_, err := tx.Finish(c, durable.Done())
					return err
				}))
				final, err := s.e.Await(ctx(), scope, durable.CommandKey{ServiceID: "service", CommandID: i.CommandID()}, s.admission("prepared", true, false).Disclose)
				if err != nil || final.Receipt.Stage != "applied" {
					t.Fatal(err)
				}
				bad := intent(t, "memory.create", time.Now().Add(time.Hour), `{}`)
				_, result = s.e.Admit(ctx(), scope, "service", bad, s.admission("bad", true, true))
				if result.Outcome != durable.RolledBack || s.job(t, "bad").ID != "" {
					t.Fatal("final-only method admitted accepted")
				}
				ok := intent(t, "brain.decide", time.Now().Add(time.Hour), `{}`)
				_, result = s.e.Admit(ctx(), scope, "service", ok, s.admission("accepted", true, true))
				mustCommit(t, result)
			})
			t.Run("FW03_ExpiredClaimAndRenewal", func(t *testing.T) {
				s := newSuite(t, driver)
				s.submit(t, "lease")
				c1 := s.claim(t, durable.NewID("boot"), 120*time.Millisecond)
				time.Sleep(time.Until(c1.Until()) + 20*time.Millisecond)
				c2 := s.claim(t, durable.NewID("boot"), time.Second)
				if c2.Epoch() != c1.Epoch()+1 {
					t.Fatal("epoch did not advance")
				}
				if r := s.finish(t, c1, durable.Done(), false); r.Outcome != durable.RolledBack || !errors.Is(r.Err, durable.ErrClaim) {
					t.Fatal(r)
				}
				if _, v := s.value(t, "lease"); v != 0 {
					t.Fatal("old protected write escaped rollback")
				}
				_, r := s.e.Renew(ctx(), c1, time.Second)
				if !errors.Is(r.Err, durable.ErrClaim) {
					t.Fatal("expired claim revived")
				}
				until, r := s.e.Renew(ctx(), c2, 2*time.Second)
				mustCommit(t, r)
				if !until.After(c2.Until()) || c2.ObservedRevision() != 1 {
					t.Fatal("renew changed observation")
				}
				mustCommit(t, s.finish(t, c2, durable.Done(), false))
			})
			t.Run("FW03_UnknownClaimKeepsOriginalObservation", func(t *testing.T) {
				s := newSuite(t, driver)
				s.submit(t, "claim")
				e := faultEngine(t, s, false, func() { s.advance(t, "claim", time.Now().Add(-time.Second)) })
				claims, result := e.Claim(ctx(), scope, kind, durable.NewID("boot"), 1, time.Second)
				if result.Outcome != durable.CommitUnknown || len(claims) != 1 || claims[0].ObservedRevision() != 1 || s.job(t, "claim").WorkRevision != 2 {
					t.Fatal("candidate snapshot changed", result)
				}
				mustCommit(t, s.finish(t, claims[0], durable.Done(), false))
				if s.job(t, "claim").State != "ready" {
					t.Fatal("unknown confirmation covered new responsibility")
				}
				before := faultEngine(t, s, true, nil)
				claims, result = before.Claim(ctx(), scope, kind, durable.NewID("boot"), 1, time.Second)
				if result.Outcome != durable.CommitUnknown || len(claims) != 0 {
					t.Fatal("unconfirmed claim executed")
				}
			})
			t.Run("FW04_AllMergeOrdersAndHints", func(t *testing.T) {
				for _, order := range []string{"new-done", "done-new", "new-waiting", "waiting-new", "same-tx"} {
					t.Run(order, func(t *testing.T) {
						s := newSuite(t, driver)
						s.submit(t, "merge")
						c := s.claim(t, durable.NewID("boot"), time.Second)
						early := time.Now().Add(-time.Second)
						d := durable.Done()
						if strings.Contains(order, "waiting") {
							d = durable.Waiting(time.Now().Add(time.Minute), "dependency")
						}
						if strings.HasPrefix(order, "new-") {
							s.advance(t, "merge", early)
							if s.job(t, "merge").HolderID != c.HolderID() {
								t.Fatal("new responsibility preempted claim")
							}
						}
						mustCommit(t, s.finish(t, c, d, order == "same-tx"))
						if strings.HasSuffix(order, "-new") {
							s.advance(t, "merge", early)
						}
						j := s.job(t, "merge")
						if j.State != "ready" || j.WorkRevision != 2 || j.DueAt > time.Now().UnixMilli() || c.ObservedRevision() != 1 {
							t.Fatalf("lost newer work: %+v", j)
						}
						mustCommit(t, s.e.Within(ctx(), scope, nil, func(tx *durable.Tx) error { return tx.Hint(c.Key(), time.Now().Add(-2*time.Second)) }))
						if s.job(t, "merge").WorkRevision != 2 {
							t.Fatal("hint raised work")
						}
					})
				}
			})
			t.Run("FW04_ElapsedWaitReturnsToReady", func(t *testing.T) {
				s := newSuite(t, driver)
				s.submit(t, "elapsed-wait")
				claim := s.claim(t, durable.NewID("boot"), time.Second)
				mustCommit(t, s.finish(t, claim, durable.Waiting(time.Now().Add(-time.Second), "dependency"), false))
				job := s.job(t, "elapsed-wait")
				if job.State != "ready" || job.DueAt > time.Now().UnixMilli() || job.WaitReason != "" {
					t.Fatalf("elapsed wait was not made ready: %+v", job)
				}
			})
			t.Run("FW07_RetainedIdentityAndJobRebuild", func(t *testing.T) {
				s := newSuite(t, driver)
				i := intent(t, "task.submit", time.Now().Add(100*time.Millisecond), `{}`)
				_, r := s.e.Admit(ctx(), scope, "service", i, s.admission("retained", false, false))
				mustCommit(t, r)
				c := s.claim(t, durable.NewID("boot"), time.Second)
				mustCommit(t, s.finish(t, c, durable.Done(), false))
				mustCommit(t, s.e.Within(ctx(), scope, nil, func(tx *durable.Tx) error { return tx.Hint(c.Key(), time.Now().Add(-time.Hour)) }))
				if s.job(t, "retained").State != "done" {
					t.Fatal("hint reopened done")
				}
				mustCommit(t, s.e.Within(ctx(), scope, nil, func(tx *durable.Tx) error { return tx.DeleteDone(c.Key(), true) }))
				s.advance(t, "retained", time.Now())
				j := s.job(t, "retained")
				if j.ID == c.JobID() || j.WorkRevision != 1 {
					t.Fatal("rebuilt job reused ID")
				}
				if r = s.finish(t, c, durable.Done(), false); !errors.Is(r.Err, durable.ErrClaim) {
					t.Fatal("old claim hit rebuilt row")
				}
				if r = s.e.Within(ctx(), scope, nil, func(tx *durable.Tx) error {
					return tx.Compact(durable.CommandKey{ServiceID: "service", CommandID: i.CommandID()}, time.Time{})
				}); !errors.Is(r.Err, durable.ErrPrecondition) {
					t.Fatal("missing closure proof accepted")
				}
				current := s.claim(t, durable.NewID("boot"), time.Second)
				mustCommit(t, s.finish(t, current, durable.Done(), false))
				closedAt := time.Now()
				time.Sleep(120 * time.Millisecond)
				mustCommit(t, s.e.Within(ctx(), scope, nil, func(tx *durable.Tx) error {
					return tx.Compact(durable.CommandKey{ServiceID: "service", CommandID: i.CommandID()}, closedAt)
				}))
				_, r = s.e.Admit(ctx(), scope, "service", i, s.admission("retained", false, false))
				if !errors.Is(r.Err, durable.ErrGone) {
					t.Fatal(r)
				}
				var target, decision string
				if err := s.raw.QueryRowContext(ctx(), s.placeholders(`SELECT target_id,decided_type FROM durable_commands WHERE tenant_id=$1 AND owner_id=$2 AND service_id=$3 AND command_id=$4`), scope.TenantID, scope.OwnerID, "service", i.CommandID()).Scan(&target, &decision); err != nil || target != i.TargetID() || decision != "applied" {
					t.Fatal("minimum identity lost")
				}
			})
			t.Run("FW08_ScopeLifetimeLocksAndRetry", func(t *testing.T) {
				s := newSuite(t, driver)
				s.submit(t, "tx")
				c := s.claim(t, durable.NewID("boot"), time.Second)
				if r := s.e.Within(ctx(), other, nil, func(tx *durable.Tx) error { return tx.Guard(c) }); !errors.Is(r.Err, durable.ErrClaim) {
					t.Fatal("cross tenant claim")
				}
				if r := s.e.Within(ctx(), durable.Scope{TenantID: "unauthorized", OwnerID: "owner-test"}, nil, func(tx *durable.Tx) error { return nil }); !errors.Is(r.Err, durable.ErrScope) {
					t.Fatal("foreign scope accepted")
				}
				var escaped *durable.Tx
				mustCommit(t, s.e.Within(ctx(), scope, nil, func(tx *durable.Tx) error { escaped = tx; return nil }))
				if _, err := escaped.Lookup(durable.CommandKey{ServiceID: "service", CommandID: durable.NewID("cmd")}); !errors.Is(err, durable.ErrTx) {
					t.Fatal("expired Tx accepted")
				}
				r := s.e.Within(ctx(), scope, []durable.Participant{s.p}, func(tx *durable.Tx) error {
					if err := s.lock(tx, "tx"); err != nil {
						return err
					}
					if err := s.update(tx, "tx", 0, 1); err != nil {
						return err
					}
					return s.e.Within(tx.Context(), scope, nil, func(*durable.Tx) error { return nil }).Err
				})
				if !errors.Is(r.Err, durable.ErrNested) {
					t.Fatal(r)
				}
				r = s.e.Within(ctx(), scope, []durable.Participant{s.p}, func(tx *durable.Tx) error {
					if err := tx.Guard(c); err != nil {
						return err
					}
					return s.lock(tx, "late")
				})
				if !errors.Is(r.Err, durable.ErrLockOrder) {
					t.Fatal(r)
				}
				if _, value := s.value(t, "tx"); value != 0 {
					t.Fatal("rejected transaction wrote business")
				}
				var count int
				r = s.e.Within(ctx(), scope, []durable.Participant{s.p}, func(tx *durable.Tx) error {
					count++
					if count == 1 {
						return &pgconn.PgError{Code: "40001"}
					}
					return s.lock(tx, "retry")
				})
				mustCommit(t, r)
				if count != 2 {
					t.Fatal("confirmed rollback did not retry within budget")
				}
			})
		})
	}
}
