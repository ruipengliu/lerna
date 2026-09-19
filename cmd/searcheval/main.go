// searcheval executes the fixed eight-case prospective search evaluation.
// It accepts only public reference materials; credentials come from the host.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	arkmodel "lerna/adapters/model/ark"
	"lerna/brain"
	"lerna/profiles/fetchcheck"
	"os"
	"path/filepath"
	"time"
)

type attempt struct {
	Input  brain.Request
	Result brain.Result
	Error  string `json:",omitempty"`
}
type model struct {
	inner       brain.Model
	attempts    []attempt
	path        string
	outputLimit uint64
}

func (m *model) Capabilities() brain.Capabilities { return m.inner.Capabilities() }
func save(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	_, err = f.Write(append(data, '\n'))
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return syncDirectory(filepath.Dir(path))
}
func syncDirectory(path string) error {
	d, err := os.Open(path)
	if err != nil {
		return err
	}
	err = d.Sync()
	closeErr := d.Close()
	if err != nil {
		return err
	}
	return closeErr
}
func (m *model) Generate(ctx context.Context, in brain.Request) (brain.Result, error) {
	limit := m.outputLimit
	if limit == 0 {
		limit = 512
	}
	if len(m.attempts) != 0 || in.MaxOutput > limit {
		return brain.Result{}, fmt.Errorf("evaluation request limit")
	}
	// Durable reservation precedes the API call. An interrupted batch is never retried.
	if err := save(m.path+".reserved.json", map[string]any{"Input": in, "InputUpper": in.MaxInput, "OutputUpper": in.MaxOutput, "ReservedCNY": 1}); err != nil {
		return brain.Result{}, err
	}
	m.attempts = append(m.attempts, attempt{Input: in})
	out, err := m.inner.Generate(ctx, in)
	m.attempts[0].Result = out
	if err != nil {
		m.attempts[0].Error = err.Error()
	}
	if e := save(m.path+".model.json", m.attempts); e != nil {
		return brain.Result{}, e
	}
	return out, err
}
func run() error {
	public := flag.String("public-config", "", "trusted JSON RuntimeConfig for one governed public task")
	output := flag.String("out", "", "new private output directory; never reuse")
	compact := flag.Bool("compact-evidence", false, "v6 evaluation with provider citation selection and source scope")
	execute := flag.Bool("execute", false, "execute eight cases, at most eight external requests")
	flag.Parse()
	if !*execute || *output == "" || flag.NArg() != 0 {
		return fmt.Errorf("requires -execute -out NEW_DIRECTORY")
	}
	inner, err := arkmodel.New(arkmodel.Config{CompactEvidence: *compact, Model: arkmodel.ModelID, APIKey: os.Getenv("ARK_API_KEY"), Timeout: 20 * time.Second}, nil)
	if err != nil {
		return err
	}
	if err = os.Mkdir(*output, 0700); err != nil {
		return err
	}
	if err = syncDirectory(filepath.Dir(*output)); err != nil {
		return err
	}
	if *public != "" {
		return runPublic(*output, *public, inner)
	}
	plan := "search-small-prospective-v2"
	if *compact {
		plan = "search-small-prospective-v6"
	}
	if err = save(filepath.Join(*output, "batch.json"), map[string]any{"Plan": plan, "Started": time.Now().UTC(), "Model": inner.Capabilities(), "MaxExternalRequests": 8, "ReservedCNY": 8}); err != nil {
		return err
	}
	for _, replay := range []bool{false, true} {
		mode := "loopback"
		if replay {
			mode = "replay"
		}
		for _, id := range []string{"answerable", "insufficient", "conflicting", "fetch_failed"} {
			path := filepath.Join(*output, mode+"-"+id)
			m := &model{inner: inner, path: path}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			started := time.Now().UTC()
			record, e := fetchcheck.CheckReferenceResearch(ctx, id, fetchcheck.ResearchConfig{Replay: replay, SearchMaxBytes: 4096, MaxQueries: 128, AnswerModel: m, DiscloseTo: []string{"ark-cn-beijing"}, ModelTokens: 256 * 1024, SearchFormat: "json"})
			cancel()
			result := struct {
				Case, Mode       string
				Started          time.Time
				ElapsedSeconds   float64
				Record           fetchcheck.ResearchRecord
				Error            string
				ExternalRequests int
			}{Case: id, Mode: mode, Started: started, ElapsedSeconds: time.Since(started).Seconds(), Record: record, ExternalRequests: len(m.attempts)}
			if e != nil {
				result.Error = e.Error()
			}
			if err = save(path+".json", result); err != nil {
				return err
			}
			fmt.Printf("%s/%s requests=%d success=%t\n", mode, id, len(m.attempts), e == nil)
		}
	}
	return nil
}
func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
