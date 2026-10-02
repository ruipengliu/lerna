package integration_test

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/internal/durable"
	pg "github.com/ruipengliu/lerna/internal/storage/postgres"
	"github.com/ruipengliu/lerna/internal/storage/sqlstore"
)

type probeReconciler struct {
	s       *suite
	pages   atomic.Int64
	largest atomic.Int64
}

func (p *probeReconciler) Reconcile(c context.Context, cursor string, limit int) (string, error) {
	p.pages.Add(1)
	rows, err := p.s.raw.QueryContext(c, p.s.placeholders(`SELECT source FROM durable_probe WHERE tenant_id=$1 AND owner_id=$2 AND revision>result AND source>$3 ORDER BY source LIMIT $4`), scope.TenantID, scope.OwnerID, cursor, limit)
	if err != nil {
		return cursor, err
	}
	var sources []string
	for rows.Next() {
		var source string
		if err = rows.Scan(&source); err != nil {
			rows.Close()
			return cursor, err
		}
		sources = append(sources, source)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return cursor, err
	}
	p.largest.Store(max(p.largest.Load(), int64(len(sources))))
	for _, source := range sources {
		r := p.s.e.Within(c, scope, []durable.Participant{p.s.p}, func(tx *durable.Tx) error {
			if err := p.s.lock(tx, source); err != nil {
				return err
			}
			var needs bool
			if err := tx.Use(p.s.p, source, func(h any) error {
				return h.(sqlstore.DBTX).QueryRowContext(tx.Context(), p.s.placeholders(`SELECT revision>result AND NOT EXISTS(SELECT 1 FROM durable_jobs WHERE tenant_id=$1 AND owner_id=$2 AND kind=$4 AND responsibility_key=$3) FROM durable_probe WHERE tenant_id=$1 AND owner_id=$2 AND source=$3`), scope.TenantID, scope.OwnerID, source, kind).Scan(&needs)
			}); err != nil {
				return err
			}
			if !needs {
				return nil
			}
			_, err := tx.Raise(durable.JobKey{Kind: kind, Responsibility: source}, source, time.Now())
			return err
		})
		if r.Outcome != durable.Committed {
			return cursor, r.Err
		}
	}
	if len(sources) < limit {
		return "", nil
	}
	return sources[len(sources)-1], nil
}
func TestBoundedDomainReconcile(t *testing.T) {
	for _, driver := range []string{"sqlite", "postgres"} {
		t.Run(driver, func(t *testing.T) {
			s := newSuite(t, driver)
			if _, err := s.raw.ExecContext(ctx(), `CREATE INDEX durable_probe_unfinished ON durable_probe(tenant_id,owner_id,source) WHERE revision>result`); err != nil {
				t.Fatal(err)
			}
			for n := 0; n < 6; n++ {
				s.submit(t, fmt.Sprintf("repair-%d", n))
			}
			// Inject a migration/programming omission; normal admission never does this.
			if _, err := s.raw.ExecContext(ctx(), `DELETE FROM durable_jobs`); err != nil {
				t.Fatal(err)
			}
			reconcile := &probeReconciler{s: s}
			completed := make(chan struct{}, 6)
			h := durable.HandlerFunc(func(c context.Context, w *durable.Work) error {
				r := w.Within(c, []durable.Participant{s.p}, func(tx *durable.Tx) error {
					if err := s.lock(tx, w.Claim().SourceRef()); err != nil {
						return err
					}
					if err := tx.Guard(w.Claim()); err != nil {
						return err
					}
					if err := s.update(tx, w.Claim().SourceRef(), 0, 1); err != nil {
						return err
					}
					_, err := tx.Finish(w.Claim(), durable.Done())
					return err
				})
				if r.Outcome != durable.Committed {
					return r.Err
				}
				completed <- struct{}{}
				return nil
			})
			opt := runnerOptions()
			opt.ReconcileLimit = 2
			opt.ReconcileEvery = 20 * time.Millisecond
			opt.Drain = time.Second
			r, err := durable.NewRunner(s.e, []durable.Binding{{Kind: kind, Handler: h, Reconciler: reconcile}}, opt)
			if err != nil {
				t.Fatal(err)
			}
			life, stop := context.WithCancel(ctx())
			defer stop()
			run := make(chan error, 1)
			go func() { run <- r.Run(life) }()
			deadline := time.After(2 * time.Second)
			for n := 0; n < 6; n++ {
				select {
				case <-completed:
				case <-deadline:
					t.Fatal("bounded source scan failed to repair")
				}
			}
			stop()
			if err = <-run; err != nil {
				t.Fatal(err)
			}
			if reconcile.largest.Load() > 2 || reconcile.pages.Load() < 3 {
				t.Fatal("reconcile ignored fixed page budget")
			}
			for n := 0; n < 6; n++ {
				key := fmt.Sprintf("repair-%d", n)
				j := s.job(t, key)
				rev, result := s.value(t, key)
				if j.State != "done" || j.WorkRevision != 1 || rev != 1 || result != 1 {
					t.Fatal("repair changed original identity/work", key, j, rev, result)
				}
			}
		})
	}
}
func TestPostgresSkipLockedAndNotifications(t *testing.T) {
	s := newSuite(t, "postgres")
	s.submit(t, "a-locked")
	s.submit(t, "b-free")
	locked, err := s.raw.BeginTx(ctx(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer locked.Rollback()
	if _, err = locked.ExecContext(ctx(), `SELECT 1 FROM durable_jobs WHERE responsibility_key='a-locked' FOR UPDATE`); err != nil {
		t.Fatal(err)
	}
	bounded, cancel := context.WithTimeout(ctx(), 200*time.Millisecond)
	defer cancel()
	claims, result := s.e.Claim(bounded, scope, kind, durable.NewID("boot"), 2, time.Second)
	mustCommit(t, result)
	if len(claims) != 1 || claims[0].SourceRef() != "b-free" {
		t.Fatal("worker blocked/reclaimed locked row")
	}
	locked.Rollback()
	u, _ := url.Parse(s.appURL)
	values := u.Query()
	values.Set("application_name", "durable-listener-test")
	u.RawQuery = values.Encode()
	listener, err := pg.Open(ctx(), u.String(), []durable.Scope{scope}, 2, true)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	select {
	case <-listener.Wake():
	case <-time.After(time.Second):
		t.Fatal("LISTEN establishment did not trigger scan")
	}
	sender, err := pg.Open(ctx(), s.appURL, []durable.Scope{scope}, 2, true)
	if err != nil {
		t.Fatal(err)
	}
	defer sender.Close()
	e, _ := durable.New(sender, durable.Options{Kinds: []string{kind}})
	mustCommit(t, e.Within(ctx(), scope, nil, func(tx *durable.Tx) error {
		return tx.Hint(durable.JobKey{Kind: kind, Responsibility: "a-locked"}, time.Now())
	}))
	select {
	case <-listener.Wake():
	case <-time.After(time.Second):
		t.Fatal("committed remote hint did not wake listener")
	}
	// Kill just the receiver LISTEN session, not the application pool or server.
	var killed int
	if err = s.raw.QueryRowContext(ctx(), `WITH stopped AS (SELECT pg_terminate_backend(pid) AS killed FROM pg_stat_activity WHERE datname=current_database() AND pid<>pg_backend_pid() AND application_name='durable-listener-test' AND query='LISTEN lerna_durable') SELECT count(*) FROM stopped WHERE killed`).Scan(&killed); err != nil {
		t.Fatal(err)
	}
	if killed < 1 {
		t.Fatal("reconnect experiment had no listener")
	}
	select {
	case <-listener.Wake():
	case <-time.After(2 * time.Second):
		t.Fatal("LISTEN reconnect did not trigger scan")
	}
	if _, r := e.Claim(ctx(), scope, "unknown", durable.NewID("boot"), 1, time.Second); !errors.Is(r.Err, durable.ErrPrecondition) {
		t.Fatal("unregistered kind accepted")
	}
}
