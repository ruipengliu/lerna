package integration_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/internal/durable"
	pg "github.com/ruipengliu/lerna/internal/storage/postgres"
	filedb "github.com/ruipengliu/lerna/internal/storage/sqlite"
)

// Child helpers run the same framework on independent process connections.
func TestDurableChild(t *testing.T) {
	mode := os.Getenv("LERNA_DURABLE_CHILD")
	if mode == "" {
		return
	}
	s := &suite{driver: os.Getenv("LERNA_CHILD_DRIVER"), appURL: os.Getenv("LERNA_CHILD_URL"), path: os.Getenv("LERNA_CHILD_PATH")}
	var err error
	if s.driver == "postgres" {
		s.store, err = pg.Open(ctx(), s.appURL, []durable.Scope{scope}, 2, false)
	} else {
		s.store, err = filedb.Open(ctx(), s.path, []durable.Scope{scope})
	}
	if err != nil {
		t.Fatal(err)
	}
	defer s.store.Close()
	s.e, err = durable.New(s.store, durable.Options{Kinds: []string{kind}})
	if err != nil {
		t.Fatal(err)
	}
	s.p, _ = s.e.Register("probe")
	if mode == "before" || mode == "after" {
		var data durable.Intent
		if err := json.Unmarshal([]byte(os.Getenv("LERNA_CHILD_INTENT")), &data); err != nil {
			t.Fatal(err)
		}
		fixed, err := durable.FixIntent(data)
		if err != nil {
			t.Fatal(err)
		}
		a := s.admission("process-admit", false, false)
		if mode == "before" {
			apply := a.Apply
			a.Apply = func(tx *durable.Tx, i durable.FixedIntent) (durable.Decision, error) {
				decision, err := apply(tx, i)
				if err != nil {
					return decision, err
				}
				fmt.Println("ready")
				_, _ = io.Copy(io.Discard, os.Stdin)
				return decision, nil
			}
		}
		_, result := s.e.Admit(ctx(), scope, "service", fixed, a)
		mustCommit(t, result)
		fmt.Println("committed")
		_, _ = io.Copy(io.Discard, os.Stdin)
		return
	}
	if mode == "claim" || mode == "hold" {
		lease := 250 * time.Millisecond
		if mode == "hold" {
			lease = 3 * time.Second
		}
		c := s.claim(t, durable.NewID("boot"), lease)
		fmt.Printf("claimed %s %d %s\n", c.HolderID(), c.Until().UnixMilli(), c.SourceRef())
		_, _ = io.Copy(io.Discard, os.Stdin)
		return
	}
	if mode == "finish" {
		life, cancel := context.WithTimeout(ctx(), 4*time.Second)
		defer cancel()
		h := durable.HandlerFunc(func(c context.Context, w *durable.Work) error {
			result := w.Within(c, []durable.Participant{s.p}, func(tx *durable.Tx) error {
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
			if result.Outcome != durable.Committed {
				return result.Err
			}
			fmt.Printf("done %s %s\n", w.Claim().HolderID(), w.Claim().SourceRef())
			cancel()
			return nil
		})
		r, err := durable.NewRunner(s.e, []durable.Binding{{Kind: kind, Handler: h}}, runnerOptions())
		if err != nil {
			t.Fatal(err)
		}
		if err = r.Run(life); err != nil {
			t.Fatal(err)
		}
		return
	}
	t.Fatal("unknown child mode")
}

func TestConcurrentPostgresWorkerProcesses(t *testing.T) {
	s := newSuite(t, "postgres")
	s.submit(t, "process-a")
	s.submit(t, "process-b")
	w1 := startChild(t, s, "hold", durable.FixedIntent{})
	one := strings.Fields(w1.line(t, "claimed "))
	w2 := startChild(t, s, "finish", durable.FixedIntent{})
	two := strings.Fields(w2.line(t, "done "))
	if err := w1.cmd.Process.Signal(syscall.Signal(0)); err != nil {
		t.Fatal("first worker not alive during competition")
	}
	if len(one) != 4 || len(two) != 3 || one[1] == two[1] || one[3] == two[2] {
		t.Fatal("workers did not hold distinct claims")
	}
	if s.job(t, one[3]).State != "leased" || s.job(t, two[2]).State != "done" {
		t.Fatal("second process stole live lease")
	}
	w2.stdin.Close()
	if err := w2.cmd.Wait(); err != nil {
		t.Fatal("second worker failed", w2.stderr.String())
	}
	w2.cancel()
	w1.kill(t)
	// Reclaim the crashed worker's remaining original responsibility.
	w3 := startChild(t, s, "finish", durable.FixedIntent{})
	w3.line(t, "done ")
	w3.stdin.Close()
	if err := w3.cmd.Wait(); err != nil {
		t.Fatal("takeover failed", w3.stderr.String())
	}
	w3.cancel()
	for _, key := range []string{"process-a", "process-b"} {
		if _, v := s.value(t, key); v != 1 || s.job(t, key).State != "done" {
			t.Fatal("process competition lost/duplicated result", key)
		}
	}
}

type child struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	lines  *bufio.Scanner
	stderr bytes.Buffer
	cancel context.CancelFunc
}

func startChild(t *testing.T, s *suite, mode string, fixed durable.FixedIntent) *child {
	t.Helper()
	life, cancel := context.WithTimeout(ctx(), 8*time.Second)
	c := &child{cmd: exec.CommandContext(life, os.Args[0], "-test.run=^TestDurableChild$", "-test.timeout=7s"), cancel: cancel}
	data, _ := json.Marshal(fixed.Value())
	c.cmd.Env = append(os.Environ(), "LERNA_DURABLE_CHILD="+mode, "LERNA_CHILD_DRIVER="+s.driver, "LERNA_CHILD_URL="+s.appURL, "LERNA_CHILD_PATH="+s.path, "LERNA_CHILD_INTENT="+string(data))
	out, err := c.cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	c.stdin, err = c.cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	c.lines = bufio.NewScanner(out)
	c.cmd.Stderr = &c.stderr
	if err = c.cmd.Start(); err != nil {
		cancel()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		c.stdin.Close()
		c.cancel()
		if c.cmd.ProcessState == nil {
			_ = c.cmd.Process.Kill()
			_ = c.cmd.Wait()
		}
	})
	return c
}
func (c *child) line(t *testing.T, prefix string) string {
	t.Helper()
	if !c.lines.Scan() {
		t.Fatalf("child did not reach %s: %s", prefix, c.stderr.String())
	}
	line := c.lines.Text()
	if !strings.HasPrefix(line, prefix) {
		t.Fatalf("child boundary: %s", line)
	}
	return line
}
func (c *child) kill(t *testing.T) {
	t.Helper()
	if err := c.cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = c.cmd.Wait()
	c.stdin.Close()
	c.cancel()
}
func TestDurableProcessCrashes(t *testing.T) {
	for _, driver := range []string{"sqlite", "postgres"} {
		for _, point := range []string{"before", "after"} {
			t.Run(driver+"/"+point, func(t *testing.T) {
				s := newSuite(t, driver)
				fixed := intent(t, "task.submit", time.Now().Add(time.Hour), `{}`)
				// SQLite has one owning host; its restart never overlaps another owner.
				if driver == "sqlite" {
					s.store.Close()
					s.store = nil
				}
				c := startChild(t, s, point, fixed)
				prefix := "ready"
				if point == "after" {
					prefix = "committed"
				}
				c.line(t, prefix)
				c.kill(t)
				s.reopen(t)
				revision, _ := s.value(t, "process-admit")
				if point == "before" && revision != 0 || point == "after" && revision != 1 {
					t.Fatal("crash persistence boundary mismatch")
				}
				_, result := s.e.Admit(ctx(), scope, "service", fixed, s.admission("process-admit", false, false))
				mustCommit(t, result)
				if revision, _ = s.value(t, "process-admit"); revision != 1 || s.job(t, "process-admit").WorkRevision != 1 {
					t.Fatal("crash recovery duplicated responsibility")
				}
			})
		}
	}
}
func TestTwoPostgresWorkerProcesses(t *testing.T) {
	s := newSuite(t, "postgres")
	s.submit(t, "worker-process")
	w1 := startChild(t, s, "claim", durable.FixedIntent{})
	line := w1.line(t, "claimed ")
	w1.kill(t)
	if s.job(t, "worker-process").State != "leased" {
		t.Fatal("worker crash deleted durable claim")
	}
	w2 := startChild(t, s, "finish", durable.FixedIntent{})
	done := w2.line(t, "done ")
	w2.stdin.Close()
	if err := w2.cmd.Wait(); err != nil {
		t.Fatal("replacement process failed", w2.stderr.String())
	}
	w2.cancel()
	oldHolder := strings.Fields(line)[1]
	newHolder := strings.Fields(done)[1]
	if oldHolder == newHolder || s.job(t, "worker-process").LeaseEpoch != 2 || s.job(t, "worker-process").State != "done" {
		t.Fatal("worker restart reused identity or failed takeover")
	}
	if _, value := s.value(t, "worker-process"); value != 1 {
		t.Fatal("process takeover duplicated result")
	}
}
