// searchcheck runs preregistered public fictional materials through the actual
// task runtime. Protocol fixture success is distinct from semantic acceptance.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"lerna/profiles/fetchcheck"
	"lerna/profiles/searchcheck"
	"os"
	"runtime"
	"runtime/debug"
	"time"
)

type caseReport struct {
	CaseID          string
	RuntimeVerified bool
	Record          *fetchcheck.ResearchRecord `json:",omitempty"`
	Error           string                     `json:",omitempty"`
}
type report struct {
	SearchFormat                                                   string
	Profile, Mode, Model, SourceSHA256, SemanticQuality, GoVersion string
	Build                                                          map[string]string
	ExternalModelRequests                                          int
	QueryLimit                                                     uint32
	RuntimeVerified                                                bool
	Cases                                                          []caseReport
	Limitations                                                    []string
}

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }
func run(args []string, output, diagnostics io.Writer) int {
	flags := flag.NewFlagSet("searchcheck", flag.ContinueOnError)
	flags.SetOutput(diagnostics)
	profile := flags.String("profile", "frozen-loopback-v1", "frozen-loopback-v1, frozen-replay-v1, reference-loopback-v2 or reference-replay-v2 (protocol models only)")
	searchFormat := flags.String("search-format", "json", "json or duckduckgo-html; synthetic reference-v2 materials only")
	queries := flags.Uint("queries", 0, "v2 observation allowance, 1..128 (default 128); frozen-v1 always uses 64")
	selected := flags.String("case", "all", "all, answerable, insufficient, conflicting, or fetch_failed")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	reference := *profile == "reference-loopback-v2" || *profile == "reference-replay-v2"
	if flags.NArg() != 0 || (!reference && *profile != "frozen-loopback-v1" && *profile != "frozen-replay-v1") {
		fmt.Fprintln(diagnostics, "unsupported profile or arguments")
		return 2
	}
	if (*searchFormat != "json" && *searchFormat != "duckduckgo-html") || (!reference && *searchFormat != "json") {
		fmt.Fprintln(diagnostics, "search format requires reference-v2; expected json or duckduckgo-html")
		return 2
	}
	queryLimit := uint32(64)
	if !reference && *queries != 0 || *queries > 128 {
		fmt.Fprintln(diagnostics, "query override requires reference-v2 and must be at most 128")
		return 2
	}
	if reference {
		queryLimit = uint32(*queries)
		if queryLimit == 0 {
			queryLimit = 128
		}
	}
	sources, err := searchcheck.LoadSources()
	if err != nil {
		fmt.Fprintln(diagnostics, err)
		return 1
	}
	var cases []string
	for _, c := range sources.Cases {
		if *selected == "all" || *selected == c.ID {
			cases = append(cases, c.ID)
		}
	}
	if len(cases) == 0 {
		fmt.Fprintln(diagnostics, "unknown frozen case")
		return 2
	}
	result := report{Profile: *profile, Mode: "loopback-http", Model: "local-protocol-fixture", SourceSHA256: sources.SHA256, SemanticQuality: "not_evaluated", GoVersion: runtime.Version(), Build: map[string]string{}, RuntimeVerified: true, Limitations: []string{
		"Protocol models use placeholder and preselected responses; runtime success does not establish semantic quality.",
		"No public network or real model run is represented by this profile.",
		"Only these public fictional inputs are exported; this is not a generic Content archive or authorization grant.",
		"Temporary task stores are removed after each case; retained snapshots are offline evidence only.",
	}}
	check := fetchcheck.CheckFrozenResearch
	result.QueryLimit = queryLimit
	result.SearchFormat = *searchFormat
	if *profile == "frozen-replay-v1" {
		result.Mode = "fixed-replay"
		check = fetchcheck.CheckFrozenResearchReplay
	}
	if reference {
		replay := *profile == "reference-replay-v2"
		if replay {
			result.Mode = "fixed-replay"
		}
		check = func(ctx context.Context, id string) (fetchcheck.ResearchRecord, error) {
			return fetchcheck.CheckReferenceResearch(ctx, id, fetchcheck.ResearchConfig{Replay: replay, MaxQueries: queryLimit, SearchFormat: *searchFormat})
		}
		result.Limitations = append(result.Limitations, "The configured v2 observation budget differs from frozen-v1; this is not a pass of the frozen-v1 plan.")
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, s := range info.Settings {
			switch s.Key {
			case "vcs.revision", "vcs.time", "vcs.modified":
				result.Build[s.Key] = s.Value
			}
		}
	}
	for _, id := range cases {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		record, err := check(ctx, id)
		cancel()
		entry := caseReport{CaseID: id, RuntimeVerified: err == nil}
		if err != nil {
			entry.Error = err.Error()
			result.RuntimeVerified = false
		} else {
			entry.Record = &record
		}
		result.Cases = append(result.Cases, entry)
	}
	encoder := json.NewEncoder(output)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(result); err != nil {
		fmt.Fprintln(diagnostics, err)
		return 1
	}
	if !result.RuntimeVerified {
		return 1
	}
	return 0
}
