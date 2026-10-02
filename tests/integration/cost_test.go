package integration_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/internal/durable"
)

// Instrument actual transaction calls, not a sum of domain operation counts.
type measuredBackend struct {
	durable.Backend
	mu                                sync.Mutex
	durations                         []time.Duration
	began, commitCalls, rollbackCalls atomic.Int64
}

var evidenceOnce sync.Once

type measuredSession struct {
	durable.Session
	owner *measuredBackend
	start time.Time
}

func (b *measuredBackend) Begin(c context.Context, s durable.Scope) (durable.Session, error) {
	start := time.Now()
	x, err := b.Backend.Begin(c, s)
	if err != nil {
		return nil, err
	}
	b.began.Add(1)
	return &measuredSession{x, b, start}, nil
}
func (x *measuredSession) record() {
	x.owner.mu.Lock()
	x.owner.durations = append(x.owner.durations, time.Since(x.start))
	x.owner.mu.Unlock()
}
func (x *measuredSession) Commit(c context.Context) error {
	x.owner.commitCalls.Add(1)
	err := x.Session.Commit(c)
	x.record()
	return err
}
func (x *measuredSession) Rollback(c context.Context) error {
	x.owner.rollbackCalls.Add(1)
	err := x.Session.Rollback(c)
	x.record()
	return err
}

type costEvidence struct {
	Began           int64           `json:"began_transactions"`
	CommitCalls     int64           `json:"commit_calls"`
	RollbackCalls   int64           `json:"rollback_calls"`
	Runtime         string          `json:"runtime"`
	Database        string          `json:"database"`
	Driver          string          `json:"driver"`
	Scenario        string          `json:"scenario"`
	Jobs            int             `json:"jobs"`
	DoneHistory     int             `json:"done_history"`
	ElapsedMS       int64           `json:"elapsed_ms"`
	Transactions    durable.Metrics `json:"transactions"`
	DataSQL         int64           `json:"data_sql"`
	TxP95MS         float64         `json:"tx_p95_ms"`
	TxP99MS         float64         `json:"tx_p99_ms"`
	PoolWaits       int64           `json:"pool_waits"`
	PoolWaitMS      float64         `json:"pool_wait_ms"`
	PeakOpen        int64           `json:"peak_open"`
	PeakOldestAgeMS int64           `json:"peak_oldest_age_ms"`
	FinalOpen       int64           `json:"final_open"`
	DrainPerSecond  float64         `json:"drain_per_second"`
	WALObservation  string          `json:"wal_observation"`
	WALDeltaBytes   int64           `json:"wal_delta_bytes"`
}

func (s *suite) walMark(t *testing.T) int64 {
	t.Helper()
	if s.driver == "postgres" {
		var n int64
		if err := s.raw.QueryRowContext(ctx(), `SELECT pg_wal_lsn_diff(pg_current_wal_insert_lsn(),'0/0')::bigint`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	info, err := os.Stat(s.path + "-wal")
	if os.IsNotExist(err) {
		return 0
	}
	if err != nil {
		t.Fatal(err)
	}
	return info.Size()
}
func TestFW09MeasuredCost(t *testing.T) {
	for _, driver := range []string{"sqlite", "postgres"} {
		for _, scenario := range []string{"normal_no_notify", "commit_ack_lost", "expired_takeover", "done_history"} {
			t.Run(driver+"/"+scenario, func(t *testing.T) {
				s := newSuite(t, driver)
				history := 0
				if scenario == "done_history" {
					history = 256
					for n := 0; n < history; n++ {
						key := durable.JobKey{Kind: kind, Responsibility: fmt.Sprintf("history-%04d", n)}
						mustCommit(t, s.e.Within(ctx(), scope, nil, func(tx *durable.Tx) error { _, err := tx.Raise(key, key.Responsibility, time.Now()); return err }))
					}
					for n := 0; n < history; n++ {
						c := s.claim(t, durable.NewID("boot"), time.Second)
						mustCommit(t, s.e.Within(ctx(), scope, nil, func(tx *durable.Tx) error { _, err := tx.Finish(c, durable.Done()); return err }))
					}
				}
				b := &measuredBackend{Backend: quietBackend{s.store}}
				var backend durable.Backend = b
				if scenario == "commit_ack_lost" {
					f := &faulty{Backend: b}
					f.remaining.Store(32)
					backend = f
				}
				s.e, _ = durable.New(backend, durable.Options{Kinds: []string{kind}})
				s.p, _ = s.e.Register("probe")
				before, err := s.store.Stats(ctx(), scope)
				if err != nil {
					t.Fatal(err)
				}
				wal := s.walMark(t)
				start := time.Now()
				for n := 0; n < 32; n++ {
					i := intent(t, "task.submit", time.Now().Add(time.Hour), `{}`)
					source := fmt.Sprintf("load-%04d", n)
					_, r := s.e.Admit(ctx(), scope, "service", i, s.admission(source, false, false))
					if scenario == "commit_ack_lost" {
						if r.Outcome != durable.CommitUnknown {
							t.Fatal("fault did not lose confirmed acknowledgement", r)
						}
					} else {
						mustCommit(t, r)
					}
				}
				if scenario == "expired_takeover" {
					claims, r := s.e.Claim(ctx(), scope, kind, durable.NewID("boot"), 32, 100*time.Millisecond)
					mustCommit(t, r)
					if len(claims) != 32 {
						t.Fatal("takeover fixture incomplete")
					}
					time.Sleep(130 * time.Millisecond)
				}
				initial, err := s.store.Stats(ctx(), scope)
				if err != nil {
					t.Fatal(err)
				}
				peak := initial.OpenJobs
				oldestAge := max(int64(0), time.Now().UnixMilli()-initial.OldestDue)
				life, stop := context.WithCancel(ctx())
				defer stop()
				completed := make(chan struct{}, 32)
				failures := make(chan error, 32)
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
						failures <- r.Err
						return r.Err
					}
					completed <- struct{}{}
					return nil
				})
				opt := runnerOptions()
				opt.Capacity = 2
				opt.Batch = 2
				opt.Scan = 10 * time.Millisecond
				opt.Lease = time.Second
				opt.Renew = 200 * time.Millisecond
				opt.Drain = time.Second
				r, err := durable.NewRunner(s.e, []durable.Binding{{Kind: kind, Handler: h}}, opt)
				if err != nil {
					t.Fatal(err)
				}
				run := make(chan error, 1)
				drainStart := time.Now()
				go func() { run <- r.Run(life) }()
				deadline := time.After(6 * time.Second)
				samples := time.NewTicker(10 * time.Millisecond)
				defer samples.Stop()
				for n := 0; n < 32; n++ {
				waitResult:
					for {
						select {
						case <-completed:
							break waitResult
						case <-samples.C:
							stats, err := s.store.Stats(ctx(), scope)
							if err != nil {
								t.Fatal(err)
							}
							peak = max(peak, stats.OpenJobs)
							if stats.OpenJobs > 0 {
								oldestAge = max(oldestAge, time.Now().UnixMilli()-stats.OldestDue)
							}
						case err := <-failures:
							t.Fatal("load handler failed", err)
						case <-deadline:
							t.Fatal("fixed workload did not drain")
						}
					}
				}
				drained := time.Since(drainStart)
				stop()
				if err = <-run; err != nil {
					t.Fatal(err)
				}
				after, err := s.store.Stats(ctx(), scope)
				if err != nil {
					t.Fatal(err)
				}
				if after.OpenJobs != 0 {
					t.Fatal("workload left open work")
				}
				var total int64
				if err = s.raw.QueryRowContext(ctx(), `SELECT sum(result) FROM durable_probe`).Scan(&total); err != nil || total != 32 {
					t.Fatal("load duplicated/lost results", total, err)
				}
				b.mu.Lock()
				times := append([]time.Duration(nil), b.durations...)
				b.mu.Unlock()
				sort.Slice(times, func(i, j int) bool { return times[i] < times[j] })
				percentile := func(percent int) float64 {
					if len(times) == 0 {
						return 0
					}
					idx := (len(times)*percent+99)/100 - 1
					return float64(times[idx]) / float64(time.Millisecond)
				}
				evidence := costEvidence{Driver: driver, Scenario: scenario, Jobs: 32, DoneHistory: history, ElapsedMS: time.Since(start).Milliseconds(), Transactions: s.e.Metrics(), DataSQL: after.SQL - before.SQL, TxP95MS: percentile(95), TxP99MS: percentile(99), PoolWaits: after.Connections.WaitCount - before.Connections.WaitCount, PoolWaitMS: float64(after.Connections.WaitDuration-before.Connections.WaitDuration) / float64(time.Millisecond), PeakOpen: peak, PeakOldestAgeMS: oldestAge, FinalOpen: after.OpenJobs, DrainPerSecond: 32 / drained.Seconds(), WALDeltaBytes: s.walMark(t) - wal}
				if driver == "postgres" {
					evidence.WALObservation = "cluster insert LSN delta; includes cluster background writes"
				} else {
					evidence.WALObservation = "WAL file size delta; allocation/checkpoint proxy, not total bytes written"
				}
				evidence.Runtime = runtime.Version() + " " + runtime.GOOS + "/" + runtime.GOARCH + " race"
				evidence.Began = b.began.Load()
				evidence.CommitCalls = b.commitCalls.Load()
				evidence.RollbackCalls = b.rollbackCalls.Load()
				versionQuery := "SELECT sqlite_version()"
				if driver == "postgres" {
					versionQuery = "SHOW server_version"
				}
				if err = s.raw.QueryRowContext(ctx(), versionQuery).Scan(&evidence.Database); err != nil {
					t.Fatal(err)
				}
				data, _ := json.Marshal(evidence)
				t.Logf("COST %s", data)
				if path := os.Getenv("LERNA_DURABLE_EVIDENCE_FILE"); path != "" {
					evidenceOnce.Do(func() {
						if err := os.WriteFile(path, nil, 0o600); err != nil {
							t.Fatal("evidence initialization failed")
						}
					})
					f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
					if err != nil {
						t.Fatal(err)
					}
					_, err = f.Write(append(data, '\n'))
					closeErr := f.Close()
					if err != nil || closeErr != nil {
						t.Fatal("evidence write failed", err, closeErr)
					}
				}
			})
		}
	}
}
