package integration_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/internal/durable"
	pg "github.com/ruipengliu/lerna/internal/storage/postgres"
	filedb "github.com/ruipengliu/lerna/internal/storage/sqlite"
	"github.com/ruipengliu/lerna/internal/storage/sqlstore"
)

func TestDurableBoundaries(t *testing.T) {
	for _, driver := range []string{"sqlite", "postgres"} {
		t.Run(driver, func(t *testing.T) {
			t.Run("FW01_CommandNamespaceAndDisclosure", func(t *testing.T) {
				s := newSuite(t, driver)
				i := s.submit(t, "identity")
				raw := i.Value()
				raw.Payload = json.RawMessage(`{"different":true}`)
				different, err := durable.FixIntent(raw)
				if err != nil {
					t.Fatal(err)
				}
				denied := errors.New("current disclosure denied")
				a := s.admission("identity", false, false)
				a.Disclose = func(context.Context, durable.Scope, durable.CommandRecord) (durable.CommandRecord, error) {
					return durable.CommandRecord{}, denied
				}
				for _, value := range []durable.FixedIntent{i, different} {
					_, r := s.e.Admit(ctx(), scope, "service", value, a)
					if !errors.Is(r.Err, denied) {
						t.Fatal("cached result bypassed disclosure", r)
					}
				}
				owner2 := durable.Scope{TenantID: scope.TenantID, OwnerID: "second-owner"}
				s.store.Close()
				if driver == "postgres" {
					s.store, err = pg.Open(ctx(), s.appURL, []durable.Scope{scope, other, owner2}, 4, false)
				} else {
					s.store, err = filedb.Open(ctx(), s.path, []durable.Scope{scope, other, owner2})
				}
				if err != nil {
					t.Fatal(err)
				}
				s.e, err = durable.New(s.store, durable.Options{Kinds: []string{kind}})
				if err != nil {
					t.Fatal(err)
				}
				s.p, _ = s.e.Register("probe")
				_, r := s.e.Admit(ctx(), owner2, "service", i, s.admission("identity", false, false))
				if !errors.Is(r.Err, durable.ErrScope) {
					t.Fatal("owner change reused logical command", r)
				}
				_, r = s.e.Admit(ctx(), other, "service", i, s.admission("other-tenant", false, false))
				mustCommit(t, r)
				_, r = s.e.Admit(ctx(), scope, "other-service", i, s.admission("other-service", false, false))
				mustCommit(t, r)
				var count int
				if err = s.raw.QueryRowContext(ctx(), `SELECT count(*) FROM durable_commands`).Scan(&count); err != nil || count != 3 {
					t.Fatal("wrong idempotency namespace", count, err)
				}
			})
			t.Run("FW07_RetentionStartsAfterClosureAndExpiry", func(t *testing.T) {
				s := newSuite(t, driver)
				s.e, _ = durable.New(s.store, durable.Options{Kinds: []string{kind}, QueryRetention: 80 * time.Millisecond})
				s.p, _ = s.e.Register("probe")
				i := intent(t, "task.submit", time.Now().Add(30*time.Millisecond), `{}`)
				_, r := s.e.Admit(ctx(), scope, "service", i, s.admission("retention", false, false))
				mustCommit(t, r)
				time.Sleep(130 * time.Millisecond)
				c := s.claim(t, durable.NewID("boot"), time.Second)
				mustCommit(t, s.finish(t, c, durable.Done(), false))
				closed := time.Now()
				key := durable.CommandKey{ServiceID: "service", CommandID: i.CommandID()}
				r = s.e.Within(ctx(), scope, nil, func(tx *durable.Tx) error { return tx.Compact(key, closed) })
				if !errors.Is(r.Err, durable.ErrPrecondition) {
					t.Fatal("closed late lost query retention", r)
				}
				time.Sleep(100 * time.Millisecond)
				mustCommit(t, s.e.Within(ctx(), scope, nil, func(tx *durable.Tx) error { return tx.Compact(key, closed) }))
				denied := errors.New("gone disclosure denied")
				a := s.admission("retention", false, false)
				a.Disclose = func(context.Context, durable.Scope, durable.CommandRecord) (durable.CommandRecord, error) {
					return durable.CommandRecord{}, denied
				}
				_, r = s.e.Admit(ctx(), scope, "service", i, a)
				if !errors.Is(r.Err, denied) {
					t.Fatal("gone bypassed disclosure", r)
				}
				raw := i.Value()
				raw.Payload = json.RawMessage(`{"x":2}`)
				different, _ := durable.FixIntent(raw)
				_, r = s.e.Admit(ctx(), scope, "service", different, s.admission("retention", false, false))
				if !errors.Is(r.Err, durable.ErrConflict) {
					t.Fatal("gone replaced conflict", r)
				}
			})
			t.Run("FW03_GuardExpiresBeforeCommit", func(t *testing.T) {
				s := newSuite(t, driver)
				s.submit(t, "expires")
				c := s.claim(t, durable.NewID("boot"), 100*time.Millisecond)
				r := s.e.Within(ctx(), scope, []durable.Participant{s.p}, func(tx *durable.Tx) error {
					if err := s.lock(tx, "expires"); err != nil {
						return err
					}
					if err := tx.Guard(c); err != nil {
						return err
					}
					if err := s.update(tx, "expires", 0, 1); err != nil {
						return err
					}
					time.Sleep(130 * time.Millisecond)
					return nil
				})
				if !errors.Is(r.Err, durable.ErrClaim) {
					t.Fatal("guarded write crossed deadline", r)
				}
				if _, v := s.value(t, "expires"); v != 0 {
					t.Fatal("expired guarded facts committed")
				}
			})
			t.Run("FW08_TimeoutAndUndeclaredRepository", func(t *testing.T) {
				s := newSuite(t, driver)
				e, _ := durable.New(s.store, durable.Options{Kinds: []string{kind}, TxTimeout: 30 * time.Millisecond})
				p, _ := e.Register("probe")
				s.e = e
				s.p = p
				r := e.Within(ctx(), scope, nil, func(tx *durable.Tx) error { return s.lock(tx, "undeclared") })
				if !errors.Is(r.Err, durable.ErrTx) {
					t.Fatal("undeclared adapter accepted", r)
				}
				var escaped sqlstore.DBTX
				mustCommit(t, e.Within(ctx(), scope, []durable.Participant{p}, func(tx *durable.Tx) error {
					if err := tx.Lock(p, "handle", func(h any) error { escaped = h.(sqlstore.DBTX); return nil }); err != nil {
						return err
					}
					_, err := escaped.ExecContext(ctx(), `SELECT 1`)
					if !errors.Is(err, durable.ErrTx) {
						return errors.New("adapter handle escaped callback")
					}
					return nil
				}))
				r = e.Within(ctx(), scope, []durable.Participant{p}, func(tx *durable.Tx) error {
					if err := s.lock(tx, "timeout"); err != nil {
						return err
					}
					if err := s.update(tx, "timeout", 1, 1); err != nil {
						return err
					}
					<-tx.Context().Done()
					return tx.Context().Err()
				})
				if r.Outcome != durable.RolledBack || !errors.Is(r.Err, context.DeadlineExceeded) {
					t.Fatal("timeout result", r)
				}
				if _, v := s.value(t, "timeout"); v != 0 {
					t.Fatal("timeout facts committed")
				}
			})
			t.Run("FW08_DomainRevisionRejectsWithoutRetryOrJob", func(t *testing.T) {
				s := newSuite(t, driver)
				revision := int64(7)
				value := durable.Intent{CommandID: durable.NewID("cmd"), Method: "task.pause", TargetID: durable.NewID("tsk"), ExpiresAt: time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano), ExpectedRevision: &revision, Payload: json.RawMessage(`{}`)}
				i, err := durable.FixIntent(value)
				if err != nil {
					t.Fatal(err)
				}
				calls := 0
				a := s.admission("revision", false, false)
				a.Apply = func(tx *durable.Tx, i durable.FixedIntent) (durable.Decision, error) {
					calls++
					if err := s.lock(tx, "revision"); err != nil {
						return durable.Decision{}, err
					}
					if i.Value().ExpectedRevision == nil || *i.Value().ExpectedRevision != 7 {
						return durable.Decision{}, errors.New("changed original revision")
					}
					return durable.Decision{Receipt: &durable.Receipt{CommandID: i.CommandID(), Stage: "rejected", Error: json.RawMessage(`{"code":"precondition_failed","message":"revision conflict","retry":"after_change","current_revision":0}`)}}, nil
				}
				for n := 0; n < 2; n++ {
					record, r := s.e.Admit(ctx(), scope, "service", i, a)
					mustCommit(t, r)
					if r.Attempts != 1 || record.Receipt.Stage != "rejected" {
						t.Fatal("revision conflict became transient", r)
					}
				}
				if calls != 1 || s.job(t, "revision").ID != "" {
					t.Fatal("fixed synchronous refusal invoked again/created job")
				}
				value.ExpectedRevision = nil
				missing, _ := durable.FixIntent(value)
				_, r := s.e.Admit(ctx(), scope, "service", missing, a)
				if !errors.Is(r.Err, durable.ErrPrecondition) {
					t.Fatal("required revision missing", r)
				}
			})
			t.Run("FW05_UncertainPreparationCannotAuthorizeExternalStep", func(t *testing.T) {
				s := newSuite(t, driver)
				e := faultEngine(t, s, false, nil)
				p, _ := e.Register("probe")
				s.p = p
				i := intent(t, "brain.decide", time.Now().Add(time.Hour), `{}`)
				var sends atomic.Int64
				record, r := e.Admit(ctx(), scope, "service", i, s.admission("external", true, true))
				if r.Outcome == durable.Committed {
					sends.Add(1)
				}
				if r.Outcome != durable.CommitUnknown || record.Receipt != nil || sends.Load() != 0 {
					t.Fatal("unknown commit exposed preparation", r)
				}
				// Recovery follows the same command; the fixture is only a gate probe,
				// not Brain's attempt/use/publication persistence or a supplier protocol.
				record, r = e.Admit(ctx(), scope, "service", i, s.admission("external", true, true))
				mustCommit(t, r)
				if record.Receipt == nil || record.Receipt.Stage != "accepted" {
					t.Fatal("original prepared identity not recovered")
				}
				var calls int
				a := s.admission("no-job", true, true)
				a.Apply = func(*durable.Tx, durable.FixedIntent) (durable.Decision, error) {
					calls++
					return durable.Decision{Preparation: &durable.Preparation{SourceRef: "missing", Job: durable.JobKey{Kind: kind, Responsibility: "missing"}}}, nil
				}
				_, r = e.Admit(ctx(), scope, "service", intent(t, "brain.decide", time.Now().Add(time.Hour), `{}`), a)
				if !errors.Is(r.Err, durable.ErrInvariant) || calls != 1 {
					t.Fatal("preparation without responsibility accepted", r)
				}
			})
		})
	}
}

func TestRenewBlockedAtOldDeadline(t *testing.T) {
	s := newSuite(t, "postgres")
	s.submit(t, "blocked-renew")
	started := make(chan *durable.Work, 1)
	cancelled := make(chan time.Time, 1)
	release := make(chan struct{})
	var invalidStep atomic.Bool
	h := durable.HandlerFunc(func(c context.Context, w *durable.Work) error {
		started <- w
		<-c.Done()
		cancelled <- time.Now()
		if err := w.Step(context.Background(), func() error { invalidStep.Store(true); return nil }); err == nil {
			invalidStep.Store(true)
		}
		<-release
		return nil
	})
	opt := runnerOptions()
	opt.Lease = 300 * time.Millisecond
	opt.Renew = 100 * time.Millisecond
	opt.WorkTimeout = 2 * time.Second
	r, err := durable.NewRunner(s.e, []durable.Binding{{Kind: kind, Handler: h}}, opt)
	if err != nil {
		t.Fatal(err)
	}
	life, stop := context.WithCancel(ctx())
	defer stop()
	run := make(chan error, 1)
	go func() { run <- r.Run(life) }()
	w := <-started
	locked, err := s.raw.BeginTx(ctx(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer locked.Rollback()
	var id string
	if err = locked.QueryRowContext(ctx(), `SELECT job_id FROM durable_jobs WHERE job_id=$1 FOR UPDATE`, w.Claim().JobID()).Scan(&id); err != nil {
		t.Fatal(err)
	}
	select {
	case at := <-cancelled:
		if at.After(w.Claim().Until().Add(50 * time.Millisecond)) {
			t.Fatal("blocked renew postponed old deadline")
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("blocked renew prevented cancellation")
	}
	if invalidStep.Load() || r.Active() != 1 {
		t.Fatal("stopped work reused Step/capacity")
	}
	stop()
	locked.Rollback()
	close(release)
	if err = <-run; err != nil {
		t.Fatal(err)
	}
}

func TestWorkNestedEntryWithOriginalContext(t *testing.T) {
	s := newSuite(t, "sqlite")
	s.submit(t, "nested")
	done := make(chan error, 1)
	h := durable.HandlerFunc(func(c context.Context, w *durable.Work) error {
		r := w.Within(c, []durable.Participant{s.p}, func(tx *durable.Tx) error {
			if err := s.lock(tx, "nested"); err != nil {
				return err
			}
			if err := s.update(tx, "nested", 0, 1); err != nil {
				return err
			}
			return w.Within(c, nil, func(*durable.Tx) error { return nil }).Err
		})
		if !errors.Is(r.Err, durable.ErrNested) {
			done <- errors.New("original context bypassed Work boundary")
		} else {
			done <- nil
		}
		return nil
	})
	opt := runnerOptions()
	r, _ := durable.NewRunner(s.e, []durable.Binding{{Kind: kind, Handler: h}}, opt)
	life, stop := context.WithCancel(ctx())
	run := make(chan error, 1)
	go func() { run <- r.Run(life) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("work did not run")
	}
	stop()
	if err := <-run; err != nil {
		t.Fatal(err)
	}
	if _, v := s.value(t, "nested"); v != 0 {
		t.Fatal("nested transaction retained facts")
	}
}
