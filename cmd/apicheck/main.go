// Command apicheck runs an explicitly authorized, bounded real-model regression.
// It is excluded from make verify; credentials are parsed as data, never executed.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	arkmodel "lerna/adapters/model/ark"
	"lerna/brain"
	"lerna/conformance"
	"lerna/profiles/catalogcheck"
	"os"
	"strings"
	"sync"
	"time"
)

type observed struct {
	brain.Model
	mu    sync.Mutex
	Limit int
	Calls []call
}
type call struct {
	Usage         brain.Usage
	Finish, Error string
}

func (m *observed) Generate(ctx context.Context, in brain.Request) (brain.Result, error) {
	m.mu.Lock()
	if len(m.Calls) >= m.Limit {
		m.mu.Unlock()
		return brain.Result{}, brain.Error("RUN_REQUEST_LIMIT")
	}
	index := len(m.Calls)
	m.Calls = append(m.Calls, call{})
	m.mu.Unlock()
	r, e := m.Model.Generate(ctx, in)
	item := call{Usage: r.Usage, Finish: r.Finish}
	if e != nil {
		item.Error = e.Error()
	}
	m.mu.Lock()
	m.Calls[index] = item
	m.mu.Unlock()
	return r, e
}
func main() {
	real := flag.Bool("real", false, "authorize real provider calls for this invocation")
	limit := flag.Int("max-requests", 0, "explicit total attempted requests, including failures, 1..12")
	env := flag.String("env-file", "", "optional local ARK_API_KEY file")
	flag.Parse()
	if !*real || *limit < 1 || *limit > 12 || flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "require -real -max-requests 1..12")
		os.Exit(2)
	}
	key := os.Getenv("ARK_API_KEY")
	if *env != "" {
		f, e := os.Open(*env)
		if e != nil {
			fmt.Fprintln(os.Stderr, "credential file unavailable")
			os.Exit(2)
		}
		data, e := io.ReadAll(io.LimitReader(f, 65537))
		f.Close()
		if e != nil || len(data) > 65536 {
			fmt.Fprintln(os.Stderr, "credential file exceeds limit")
			os.Exit(2)
		}
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimPrefix(strings.TrimSpace(line), "export ")
			name, value, ok := strings.Cut(line, "=")
			if ok && strings.TrimSpace(name) == "ARK_API_KEY" {
				key = strings.Trim(strings.TrimSpace(value), "\"'")
			}
		}
	}
	model, e := arkmodel.New(arkmodel.Config{Model: arkmodel.ModelID, APIKey: key, Timeout: 30 * time.Second}, nil)
	if e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(2)
	}
	m := &observed{Model: model, Limit: *limit}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	outcome, err := catalogcheck.RunActionRegression(ctx, m)
	m.mu.Lock()
	calls := append([]call(nil), m.Calls...)
	m.mu.Unlock()
	status, code := "failed", ""
	if err != nil {
		code = err.Error()
	} else if outcome.State == "COMPLETED" && outcome.Ledger == 997 && outcome.FinalState == "authorized" && outcome.DirectorySize == 1008 {
		status = "passed"
	}
	report := struct {
		Build                   conformance.Build
		ExecutedAt              time.Time
		Status, Error, Endpoint string
		Capabilities            brain.Capabilities
		RequestLimit            int
		Calls                   []call
		Outcome                 catalogcheck.ActionReport
		Limitations             []string
	}{conformance.BuildInfo(), time.Now().UTC(), status, code, arkmodel.Endpoint, model.Capabilities(), *limit, calls, outcome, []string{"One public simulated business task with a full 1008-entry catalog; not formal 90%/95% quality acceptance.", "Each call reserves 224K input +1024 output tokens under the unchanged multi-step 262144-token task budget; known usage releases unused reservation. The single-step 65536-token envelope is unsupported by this binding.", "Raw model input/output and SQLite state are retained in the private StateDirectory. Do not publish credentials or copy that directory into git.", "Catalog search is goal/declared-namespace driven; model selects exact descriptors and action order from six authorized candidate schemas."}}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if enc.Encode(report) != nil {
		os.Exit(2)
	}
	if status != "passed" {
		os.Exit(1)
	}
}
