//go:build integration

package recovery_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/adapters/postgres"
	"github.com/ruipengliu/lerna/adapters/sqlite"
	process "github.com/ruipengliu/lerna/conformance/internal/testkit/process"
	"github.com/ruipengliu/lerna/contract"
	"github.com/ruipengliu/lerna/host/durablework"
	demo "github.com/ruipengliu/lerna/internal/durableworkdemo"
	"github.com/ruipengliu/lerna/runtime"
)

const processDeadline = 12 * time.Second

// This trusted configuration travels on a dedicated parent-owned pipe. The
// child inherits the explicit test DSN environment; credentials are not framed
// or printed. Only the parent creates/registers/drops the PostgreSQL scope.
type processConfig struct {
	Backend, Schema, Path, Scenario, Gate string
	fixture                               *ownedFixture
	Generation                            int
	Now                                   time.Time
	Raw                                   json.RawMessage `json:",omitempty"`
}
type processFrame struct {
	Scenario, Stage string
	Generation      int
	Now             time.Time
	Outcome         *contract.TransportOutcome `json:",omitempty"`
	Work            *durablework.Work          `json:",omitempty"`
	Projection      *durablework.Projection    `json:",omitempty"`
	Settings        string                     `json:",omitempty"`
	Observation     *demo.Observation          `json:",omitempty"`
}

// The demo owns business frames and stages; shared process.Child owns all
// physical pipe allocation, cancellation, single Wait and exit confirmation.
type hostProcess struct {
	physical *process.Child
	ctx      context.Context
	cancel   context.CancelFunc
	waited   bool
	cfg      processConfig
}

func startHostProcess(t *testing.T, cfg processConfig) *hostProcess {
	t.Helper()
	if cfg.fixture == nil {
		t.Fatal("parent process requires owned fixture")
	}
	if err := cfg.fixture.prepareChild(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), processDeadline)
	physical, err := process.New(ctx, "TestDurableWorkHostProcess", "LERNA_DURABLE_WORK_PROCESS=1")
	child := &hostProcess{physical: physical, ctx: ctx, cancel: cancel, cfg: cfg}
	if physical != nil {
		cfg.fixture.child = child
	} // Before Start/firstready, including partial allocation.
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), 2*time.Second)
		defer stop()
		if e := child.stop(cleanup); e != nil {
			t.Error(e)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = physical.Start(); err != nil {
		t.Fatal(err)
	}
	if err = physical.Send(ctx, cfg); err != nil {
		t.Fatal(err)
	}
	ready := child.event(t, "configured")
	t.Logf("process configured backend=%s scenario=%s generation=%d clock=%s settings=%s", cfg.Backend, cfg.Scenario, ready.Generation, ready.Now.Format(time.RFC3339Nano), ready.Settings)
	return child
}
func (p *hostProcess) event(t *testing.T, stage string) processFrame {
	t.Helper()
	var frame processFrame
	if err := p.physical.Event(p.ctx, &frame); err != nil {
		t.Fatalf("scenario=%s expected=%s pipe=%v", p.cfg.Scenario, stage, err)
	}
	if frame.Scenario != p.cfg.Scenario || frame.Stage != stage || frame.Generation != p.cfg.Generation || !frame.Now.Equal(p.cfg.Now) {
		t.Fatalf("wrong process stage: %+v expected=%s generation=%d", frame, stage, p.cfg.Generation)
	}
	t.Logf("process synchronized scenario=%s stage=%s generation=%d", frame.Scenario, stage, frame.Generation)
	return frame
}
func (p *hostProcess) send(t *testing.T, stage string, work *durablework.Work, projection *durablework.Projection) {
	t.Helper()
	if err := p.physical.Send(p.ctx, processFrame{Scenario: p.cfg.Scenario, Stage: stage, Generation: p.cfg.Generation, Now: p.cfg.Now, Work: work, Projection: projection}); err != nil {
		t.Fatal(err)
	}
}
func (p *hostProcess) wait(t *testing.T, killed bool) {
	t.Helper()
	if p.waited {
		t.Fatal("child Wait observation repeated")
	}
	if killed {
		t.Fatal("SIGKILL must use actual shared KillWait")
	}
	confirmed, err := p.physical.Wait(p.ctx)
	p.waited = confirmed
	if !confirmed || err != nil {
		t.Fatal("normal child exit not confirmed:", err)
	}
}
func (p *hostProcess) kill(t *testing.T) {
	t.Helper()
	if err := p.physical.KillWait(p.ctx); err != nil {
		t.Fatal(err)
	}
	p.waited = true
	var reply processFrame
	if err := p.physical.Reply(p.ctx, &reply); err != io.EOF {
		t.Fatalf("killed child sent business reply or malformed frame: %v", err)
	}
}

func processFixture(t *testing.T, backend, scenario string) processConfig {
	t.Helper()
	cfg := processConfig{Backend: backend, Scenario: scenario, Generation: 1, Now: time.Date(2026, 10, 3, 1, 0, 0, 0, time.UTC)}
	f := newOwnedFixture(t, backend, true)
	cfg.fixture = f
	cfg.Schema = f.pg.Schema
	cfg.Path = f.sq.Path
	if err := f.CloseWriter(); err != nil {
		t.Fatal(err)
	}
	return cfg
}
func openProcessStore(ctx context.Context, cfg processConfig) (workStore, error) {
	switch cfg.Backend {
	case "postgres":
		if !strings.HasPrefix(cfg.Schema, "lerna_test_") || len(cfg.Schema) != len("lerna_test_")+24 {
			return nil, errors.New("unregistered process schema shape")
		}
		store, err := postgres.Open(ctx, postgres.Config{DSN: os.Getenv("LERNA_TEST_POSTGRES_DSN"), Schema: cfg.Schema, TransactionTimeout: 3 * time.Second, StatementTimeout: 2 * time.Second, LockTimeout: time.Second})
		if store == nil {
			return nil, err
		}
		return store, err
	case "sqlite":
		store, err := sqlite.Open(ctx, sqlite.Config{Path: cfg.Path, TransactionTimeout: 3 * time.Second, BusyTimeout: 100 * time.Millisecond})
		if store == nil {
			return nil, err
		}
		return store, err
	}
	return nil, errors.New("unsupported process backend")
}
func processObserver(t *testing.T, cfg processConfig) (workStore, *durablework.Host, *durablework.Worker) {
	t.Helper()
	if cfg.fixture == nil {
		t.Fatal("parent observer requires owned fixture")
	}
	store := cfg.fixture.Replace(t)
	clock := &controlledClock{admissionStore: store, instant: cfg.Now}
	h := hostFor(store, owner, principal)
	h.Clock = clock
	return store, h, processWorker(t, store, clock)
}

// gateBeforeCommit wraps the actual callback and all actual SQL writes. It
// announces only after that callback succeeds, before returning to Commit.
// This intentional fault pause is confined to conformance, never production.
type gateBeforeCommit struct {
	runtime.TxRunner
	pause func(context.Context) error
}

// This conformance-only storage boundary delegates the entire real transaction,
// including Commit. Only a nil confirmation is withheld from its consumer.
// It does not execute or prove SQLite's native sqlite3 Commit error branch.
type storagePortConfirmationLoss struct{ runtime.TxRunner }

func (g storagePortConfirmationLoss) Within(ctx context.Context, owner contract.OwnerRef, fn func(context.Context, runtime.Tx) error) error {
	if err := g.TxRunner.Within(ctx, owner, fn); err != nil {
		return err
	}
	return fmt.Errorf("conformance storage-port confirmation withheld after real commit: %w", runtime.ErrCommitUnknown)
}

func (g gateBeforeCommit) Within(ctx context.Context, owner contract.OwnerRef, fn func(context.Context, runtime.Tx) error) error {
	return g.TxRunner.Within(ctx, owner, func(ctx context.Context, tx runtime.Tx) error {
		if err := fn(ctx, tx); err != nil {
			return err
		}
		return g.pause(ctx)
	})
}

func TestDurableWorkHostProcess(t *testing.T) {
	if os.Getenv("LERNA_DURABLE_WORK_PROCESS") != "1" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), processDeadline)
	defer cancel()
	pipes, err := process.OpenInherited()
	if pipes != nil {
		defer func() {
			if e := pipes.Close(); e != nil {
				t.Error(e)
			}
		}()
	}
	if err != nil {
		t.Fatal(err)
	}
	var cfg processConfig
	if err := pipes.Receive(ctx, &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Scenario == "" || cfg.Generation < 1 || cfg.Now.IsZero() {
		t.Fatal("invalid trusted process clock/scenario")
	}
	store, err := openProcessStore(ctx, cfg)
	if store != nil {
		defer func() {
			if err := store.Close(); err != nil {
				t.Error(err)
			}
		}()
	}
	if err != nil {
		t.Fatal(err)
	}
	clock := &controlledClock{admissionStore: store, instant: cfg.Now}
	h := hostFor(store, owner, principal)
	h.Clock = clock
	frame := func(stage string) processFrame {
		return processFrame{Scenario: cfg.Scenario, Stage: stage, Generation: cfg.Generation, Now: cfg.Now}
	}
	emit := func(value processFrame) {
		if err := pipes.Emit(ctx, value); err != nil {
			t.Fatal(err)
		}
	}
	receive := func(stage string) processFrame {
		var value processFrame
		if err := pipes.Receive(ctx, &value); err != nil {
			t.Fatal(err)
		}
		if value.Scenario != cfg.Scenario || value.Stage != stage || value.Generation != cfg.Generation || !value.Now.Equal(cfg.Now) {
			t.Fatalf("unexpected process control stage: %+v", value)
		}
		return value
	}
	ready := frame("configured")
	err = store.Within(ctx, owner, func(ctx context.Context, tx runtime.Tx) error {
		switch actual := store.(type) {
		case *postgres.Store:
			settings, err := actual.Settings(ctx, tx)
			if err != nil {
				return err
			}
			if settings.Isolation != "read committed" || settings.SynchronousCommit != "on" || settings.StatementTimeout != "2s" || settings.LockTimeout != "1s" {
				return errors.New("incorrect PG process durability")
			}
			ready.Settings = fmt.Sprintf("%+v", settings)
		case *sqlite.Store:
			settings, err := actual.Settings(ctx, tx)
			if err != nil {
				return err
			}
			if settings.JournalMode != "wal" || settings.Synchronous != 2 || settings.ForeignKeys != 1 || settings.BusyTimeout != 100 {
				return errors.New("incorrect SQLite process durability")
			}
			ready.Settings = fmt.Sprintf("%+v", settings)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	emit(ready)
	if cfg.Gate == "worker" {
		if len(cfg.Raw) > 0 {
			out, err := h.Record(ctx, cfg.Raw, &principal)
			receipt := assertReceived(t, out, err)
			if _, ok := receipt.AsApplied(); !ok {
				t.Fatal("worker update did not apply")
			}
		}
		worker := processWorker(t, store, clock)
		batch, err := worker.Claim(ctx, fmt.Sprintf("process-worker-%d", cfg.Generation), 1, time.Minute)
		if err != nil || len(batch) != 1 {
			t.Fatalf("process Claim: %+v %v", batch, err)
		}
		work := batch[0]
		startWork(t, worker, work)
		projection := durablework.Project(work) // Claim Tx has already committed.
		value := frame("work_computed")
		value.Work = &work
		value.Projection = &projection
		emit(value)
		message := receive("complete")
		if message.Work == nil || message.Projection == nil {
			t.Fatal("missing buffered completion")
		}
		err = worker.Complete(ctx, message.Work.Claim, *message.Projection)
		if errors.Is(err, runtime.ErrClaim) {
			observed, observeErr := h.Observe(ctx, "input", &principal)
			if observeErr != nil {
				t.Fatal(observeErr)
			}
			rejected := frame("late_completion_rejected")
			rejected.Observation = &observed
			emit(rejected)
			message = receive("complete_current")
			if message.Work == nil || message.Projection == nil {
				t.Fatal("missing current completion")
			}
			err = worker.Complete(ctx, message.Work.Claim, *message.Projection)
		}
		if err != nil {
			t.Fatal(err)
		}
		emit(frame("completed"))
		if err := pipes.Respond(ctx, frame("worker_reply")); err != nil {
			t.Fatal(err)
		}
		return
	}

	if cfg.Gate == "writes_staged_before_commit" {
		h.Runner = gateBeforeCommit{TxRunner: store, pause: func(gatectx context.Context) error {
			if err := pipes.Emit(gatectx, frame(cfg.Gate)); err != nil {
				return err
			}
			var value processFrame
			if err := pipes.Receive(gatectx, &value); err != nil {
				return err
			}
			if value.Scenario != cfg.Scenario || value.Stage != "release" || value.Generation != cfg.Generation || !value.Now.Equal(cfg.Now) {
				return errors.New("wrong precommit release stage")
			}
			return nil
		}}
	}
	out, err := h.Record(ctx, cfg.Raw, &principal)
	if err != nil {
		t.Fatal(err)
	}
	received, ok := out.AsReceived()
	if !ok {
		t.Fatal("process admission did not receive confirmed commit")
	}
	if applied, ok := received.Receipt.AsApplied(); !ok || applied.Revision != "1" {
		t.Fatal("process admission did not create revision1")
	}
	if cfg.Gate == "commit_confirmed_before_host_reply" {
		emit(frame(cfg.Gate))
		receive("release")
	}
	value := frame("host_reply")
	value.Outcome = &out
	if err := pipes.Respond(ctx, value); err != nil {
		t.Fatal(err)
	}
}

func processWorker(t *testing.T, store workStore, clock runtime.Clock) *durablework.Worker {
	t.Helper()
	permissions, err := demo.NewWorkerPermissions([]string{"process-worker-1", "process-worker-2", "process-worker-3", "recovery-observer", "normal-recovery", "after-successor", "new-revision"})
	if err != nil {
		t.Fatal(err)
	}
	worker, err := durablework.NewScheduledWorker(owner, store, store, store, clock, permissions)
	if err != nil {
		t.Fatal(err)
	}
	return worker
}

// Historical child errors remain diagnostics; only unconfirmed physical
// cleanup prevents this concrete fixture from closing/dropping its scope.
func (p *hostProcess) stop(ctx context.Context) error {
	if p.physical == nil {
		p.cancel()
		return nil
	}
	confirmed, err := p.physical.Stop(ctx)
	p.waited = confirmed
	p.cancel()
	return err
}
