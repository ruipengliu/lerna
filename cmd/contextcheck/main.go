// Command contextcheck runs explicitly authorized paired personalization checks.
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
	"lerna/profiles/answer"
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
	Input         brain.Input
	Output        string
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
	item := call{Input: in.Input, Output: string(r.Content), Usage: r.Usage, Finish: r.Finish}
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
	limit := flag.Int("max-requests", 0, "explicit total attempted requests: 4 for all, 2 for answers")
	suite := flag.String("suite", "all", "all or answers; only selected pairs use requests")
	env := flag.String("env-file", "", "optional local ARK_API_KEY file")
	flag.Parse()
	expected := 4
	if *suite == "answers" {
		expected = 2
	}
	if !*real || (*suite != "all" && *suite != "answers") || *limit != expected || flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "require -real -suite all -max-requests 4, or -suite answers -max-requests 2")
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

	type caseReport struct {
		Name   string
		Passed bool
		Error  string `json:",omitempty"`
		Calls  []call
		Answer *answer.PersonalizedReport `json:",omitempty"`
		Action *catalogcheck.ActionReport `json:",omitempty"`
	}
	report := struct {
		Build                                conformance.Build
		ExecutedAt                           time.Time
		Status, Endpoint, Suite              string
		Capabilities                         brain.Capabilities
		RequestLimit, ActualRequests         int
		Cases                                []caseReport
		AnswerPairChanged, ActionPairChanged bool
		Limitations                          []string
	}{Build: conformance.BuildInfo(), ExecutedAt: time.Now().UTC(), Status: "failed", Endpoint: arkmodel.Endpoint, Capabilities: model.Capabilities(), RequestLimit: expected, Suite: *suite,
		Limitations: []string{"Selected public synthetic paired cases, one provider request per case, no hidden retry; not the 90%/95% quality benchmark.", "Answer output ceiling 512 tokens, action output ceiling 1024. Failed or uncertain calls also consume this command's attempt limit.", "Answer contrast checks named facts and greater detail by word count; human inspection is required for semantic quality. Each task uses independent state, so model randomness is not controlled.", "Reports include public synthetic model inputs and outputs; private fixture SQLite state is removed after each case. The limit applies per command invocation; reruns require a newly authorized budget."}}
	names := []string{"concise", "detailed"}
	if *suite == "all" {
		names = append(names, "personalized-item", "personalized-alternative")
	}
	for _, name := range names {
		m := &observed{Model: model, Limit: 1}
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		cr := caseReport{Name: name}
		if strings.HasPrefix(name, "personalized-") {
			r, err := catalogcheck.RunActionCase(ctx, name, m)
			cr.Action = &r
			if err != nil {
				cr.Error = err.Error()
			}
			want := int64(997)
			if name == "personalized-alternative" {
				want = 995
			}
			cr.Passed = err == nil && r.State == "COMPLETED" && r.FinalState == "authorized" && r.Ledger == want && r.OtherState == "submitted" && r.DirectorySize == 1008 && r.MemoryReadAllocated == 1 && r.GovernedArtifacts == 4 && r.RevokedArtifacts == 4
		} else {
			r, err := answer.RunPersonalizedAnswer(ctx, name, true, m)
			cr.Answer = &r
			if err != nil {
				cr.Error = err.Error()
			}
			cr.Passed = err == nil && r.State == "COMPLETED" && r.MemorySources == 1 && r.ReadAllocated == 1 && strings.Contains(r.Answer, "Memory") && strings.Contains(r.Answer, "Brain") && strings.Contains(r.Answer, "Execution")
		}
		cancel()
		m.mu.Lock()
		cr.Calls = append([]call(nil), m.Calls...)
		m.mu.Unlock()
		report.ActualRequests += len(cr.Calls)
		report.Cases = append(report.Cases, cr)
	}
	report.AnswerPairChanged = report.Cases[0].Passed && report.Cases[1].Passed && len(strings.Fields(report.Cases[1].Answer.Answer)) > len(strings.Fields(report.Cases[0].Answer.Answer))
	if *suite == "all" {
		report.ActionPairChanged = report.Cases[2].Passed && report.Cases[3].Passed && report.Cases[2].Action.SelectedRecord != report.Cases[3].Action.SelectedRecord
	}
	if report.AnswerPairChanged && (*suite == "answers" || report.ActionPairChanged) && report.ActualRequests == expected {
		report.Status = "passed"
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if enc.Encode(report) != nil {
		os.Exit(2)
	}
	if report.Status != "passed" {
		os.Exit(1)
	}
}
