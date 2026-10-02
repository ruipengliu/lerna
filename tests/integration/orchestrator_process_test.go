package integration_test

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/durable"
	"github.com/ruipengliu/lerna/internal/host"
	o "github.com/ruipengliu/lerna/internal/orchestrator"
	pg "github.com/ruipengliu/lerna/internal/storage/postgres"
	filedb "github.com/ruipengliu/lerna/internal/storage/sqlite"
	"github.com/ruipengliu/lerna/internal/storage/sqlstore"
	"github.com/ruipengliu/lerna/internal/storage/taskstore"
)

type taskCommitBoundary struct {
	durable.Backend
	armed *atomic.Bool
}
type taskCommitSession struct {
	durable.Session
	armed *atomic.Bool
}

func (b taskCommitBoundary) Begin(ctx context.Context, s durable.Scope) (durable.Session, error) {
	x, e := b.Backend.Begin(ctx, s)
	if e != nil {
		return nil, e
	}
	return taskCommitSession{x, b.armed}, nil
}
func (b taskCommitBoundary) SetCandidateSelector(s durable.Scope, q sqlstore.CandidateSelector) error {
	return b.Backend.(interface {
		SetCandidateSelector(durable.Scope, sqlstore.CandidateSelector) error
	}).SetCandidateSelector(s, q)
}
func (s taskCommitSession) Commit(ctx context.Context) error {
	if s.armed.Swap(false) {
		fmt.Println("prepared")
		_, _ = io.Copy(io.Discard, os.Stdin)
	}
	return s.Session.Commit(ctx)
}

func TestOrchestratorProcessChild(t *testing.T) {
	mode := os.Getenv("LERNA_TASK_CHILD")
	if mode == "" {
		return
	}
	s := &suite{driver: os.Getenv("LERNA_CHILD_DRIVER"), appURL: os.Getenv("LERNA_CHILD_URL"), path: os.Getenv("LERNA_CHILD_PATH")}
	var e error
	if s.driver == "postgres" {
		s.store, e = pg.Open(ctx(), s.appURL, []durable.Scope{taskScope}, 2, false)
	} else {
		s.store, e = filedb.Open(ctx(), s.path, []durable.Scope{taskScope})
	}
	if e != nil {
		t.Fatal("child store open failed")
	}
	defer s.store.Close()
	f := newTaskFixture()
	original := s.store
	var service api.Orchestrator
	if mode == "before" {
		armed := &atomic.Bool{}
		f.beforeSubmit = func() { armed.Store(true) }
		q := filedb.TaskQueries()
		if s.driver == "postgres" {
			q = pg.TaskQueries()
		}
		hostService, e := newProcessTaskService(taskCommitBoundary{s.store, armed}, q, f)
		if e != nil {
			t.Fatal(e)
		}
		service = hostService
	} else {
		service = newTaskService(t, s, f)
	}
	s.store = original
	if mode == "before" || mode == "after" {
		var command api.Command
		if e = json.Unmarshal([]byte(os.Getenv("LERNA_TASK_COMMAND")), &command); e != nil {
			t.Fatal(e)
		}
		r, e := service.Execute(ctx(), taskCaller, command)
		if e != nil || r.Stage != "applied" {
			t.Fatal("child admission", e, r.Stage)
		}
		fmt.Println("committed")
		_, _ = io.Copy(io.Discard, os.Stdin)
		return
	}
	p := newTaskProbe(t, s, f)
	lease := 100 * time.Millisecond
	if mode == "hold" {
		lease = time.Second
	}
	claim := p.claim(t, "decide", lease)
	if mode == "hold" {
		fmt.Printf("claimed %s\n", claim.SourceRef())
		_, _ = io.Copy(io.Discard, os.Stdin)
		return
	}
	if e = p.c.Handle(ctx(), probeUnit{p, claim}); e != nil {
		t.Fatal(e)
	}
	fmt.Printf("done %s\n", claim.SourceRef())
}
func startTaskChild(t *testing.T, s *suite, mode string, command api.Command) *child {
	t.Helper()
	life, cancel := context.WithTimeout(ctx(), 15*time.Second)
	c := &child{cmd: exec.CommandContext(life, os.Args[0], "-test.run=^TestOrchestratorProcessChild$", "-test.timeout=14s"), cancel: cancel}
	b, _ := json.Marshal(command)
	c.cmd.Env = append(os.Environ(), "LERNA_TASK_CHILD="+mode, "LERNA_CHILD_DRIVER="+s.driver, "LERNA_CHILD_URL="+s.appURL, "LERNA_CHILD_PATH="+s.path, "LERNA_TASK_COMMAND="+string(b))
	out, e := c.cmd.StdoutPipe()
	if e != nil {
		t.Fatal(e)
	}
	c.stdin, e = c.cmd.StdinPipe()
	if e != nil {
		t.Fatal(e)
	}
	c.lines = bufio.NewScanner(out)
	c.cmd.Stderr = &c.stderr
	if e = c.cmd.Start(); e != nil {
		cancel()
		t.Fatal(e)
	}
	t.Cleanup(func() {
		_ = c.stdin.Close()
		cancel()
		if c.cmd.ProcessState == nil {
			_ = c.cmd.Process.Kill()
			_ = c.cmd.Wait()
		}
	})
	return c
}
func TestOrchestratorProcessAdmissionRecovery(t *testing.T) {
	for _, driver := range []string{"sqlite", "postgres"} {
		for _, mode := range []string{"before", "after"} {
			t.Run(driver+"/"+mode, func(t *testing.T) {
				s := newSuite(t, driver)
				f := newTaskFixture()
				command := taskSubmitCommand(f)
				if driver == "sqlite" {
					_ = s.store.Close()
				}
				c := startTaskChild(t, s, mode, command)
				marker := "committed"
				if mode == "before" {
					marker = "prepared"
				}
				taskChildLine(t, c, marker)
				c.kill(t)
				if driver == "sqlite" {
					s.reopen(t)
				}
				service := newTaskService(t, s, f)
				if mode == "before" {
					if _, e := service.CommandStatus(ctx(), taskCaller, command.CommandID); e != durable.ErrNotFound {
						t.Fatal("uncommitted receipt survived", e)
					}
				}
				r := executeTask(t, service, command)
				if r.Stage != "applied" {
					t.Fatal(r)
				}
				var task api.Task
				_ = json.Unmarshal(r.Output, &task)
				var tasks, jobs int
				_ = s.raw.QueryRowContext(ctx(), "SELECT count(*) FROM orchestrator_tasks").Scan(&tasks)
				_ = s.raw.QueryRowContext(ctx(), "SELECT count(*) FROM durable_jobs WHERE kind='decide'").Scan(&jobs)
				if tasks != 1 || jobs != 1 {
					t.Fatal("original responsibility", tasks, jobs)
				}
				runTaskService(t, service)
				awaitTask(t, service, f, task.TaskID, func(v api.Task) bool { return v.Status == "succeeded" })
			})
		}
	}
}
func TestOrchestratorPostgresWorkerProcesses(t *testing.T) {
	s := newSuite(t, "postgres")
	f := newTaskFixture()
	service := newTaskService(t, s, f)
	a := submitTask(t, service, f, "10")
	b := submitTask(t, service, f, "10")
	first := startTaskChild(t, s, "hold", api.Command{})
	one := strings.TrimPrefix(taskChildLine(t, first, "claimed "), "claimed ")
	second := startTaskChild(t, s, "finish", api.Command{})
	two := strings.TrimPrefix(taskChildLine(t, second, "done "), "done ")
	if one == two || one != a.TaskID && one != b.TaskID || two != a.TaskID && two != b.TaskID {
		t.Fatal("workers selected wrong original tasks", one, two)
	}
	if e := first.cmd.Process.Signal(syscall.Signal(0)); e != nil {
		t.Fatal("first holder exited before competition")
	}
	_ = second.stdin.Close()
	if e := second.cmd.Wait(); e != nil {
		t.Fatal("second holder failed")
	}
	second.cancel()
	first.kill(t)
	p := newTaskProbe(t, s, f)
	p.handle(t, "decide")
	if len(readDomainRecords(t, s, "snapshot_preparation")) != 2 {
		t.Fatal("takeover lost or duplicated original snapshots")
	}
}

func newProcessTaskService(backend durable.Backend, q taskstore.Queries, f *taskFixture) (*host.TaskService, error) {
	s, e := host.NewTaskService(backend, q, f.ports(), host.TaskServiceOptions{Scope: taskScope, ServiceID: o.ID("service", "tasks")})
	if e != nil {
		return nil, e
	}
	return s, s.Recover(ctx())
}

func taskChildLine(t *testing.T, c *child, prefix string) string {
	t.Helper()
	if !c.lines.Scan() {
		t.Fatal("child exited", c.stderr.String())
	}
	line := c.lines.Text()
	if !strings.HasPrefix(line, prefix) {
		messages := []string{line}
		for c.lines.Scan() {
			messages = append(messages, c.lines.Text())
			if len(messages) > 10 {
				break
			}
		}
		t.Fatal("child boundary", strings.Join(messages, "\n"))
	}
	return line
}
