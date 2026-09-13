// fetchcheck separates reproducible local integration from explicit public HTTPS.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"lerna/profiles/fetchcheck"
	"os"
	"runtime"
	"runtime/debug"
	"time"
)

type check struct {
	Name   string
	Passed bool
}
type report struct {
	Profile, Implementation, Location, GoVersion string
	Build                                        map[string]string
	ExternalModelRequests                        int
	Passed                                       bool
	Checks                                       []check                  `json:",omitempty"`
	Public                                       *fetchcheck.PublicReport `json:",omitempty"`
}

func main() {
	profile := flag.String("profile", "local-task-v1", "local-task-v1, fixed-replay-v1, or public-https-v1 (explicit network request)")
	flag.Parse()
	if flag.NArg() != 0 || (*profile != "local-task-v1" && *profile != "public-https-v1" && *profile != "fixed-replay-v1") {
		fmt.Fprintln(os.Stderr, "unsupported profile or arguments")
		os.Exit(2)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	out := report{Profile: *profile, Implementation: "httpfetch-v1", Location: "local", GoVersion: runtime.Version(), Build: map[string]string{}, Passed: true}
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, setting := range info.Settings {
			switch setting.Key {
			case "vcs.revision", "vcs.time", "vcs.modified":
				out.Build[setting.Key] = setting.Value
			}
		}
	}
	if *profile == "fixed-replay-v1" {
		out.Implementation = "replayfetch-v1"
		out.Passed = fetchcheck.CheckFixedReplay(ctx) == nil
		out.Checks = []check{{"core-sdk-fixed-data-zero-http-requests", out.Passed}}
	} else if *profile == "public-https-v1" {
		observed := fetchcheck.CheckPublic(ctx)
		out.Public = &observed
		out.Passed = observed.Status == "acquired" && observed.ControlledRoundTrip
	} else {
		for _, c := range []struct {
			name string
			run  func(context.Context) error
		}{
			{"core-sdk-actual-http-and-replay", fetchcheck.CheckTask},
			{"http-denied-original-budget", func(ctx context.Context) error { return fetchcheck.CheckFailure(ctx, 403) }},
			{"http-unavailable-original-budget", func(ctx context.Context) error { return fetchcheck.CheckFailure(ctx, 404) }},
			{"http-expired-original-budget", func(ctx context.Context) error { return fetchcheck.CheckFailure(ctx, 410) }},
		} {
			passed := c.run(ctx) == nil
			out.Checks = append(out.Checks, check{c.name, passed})
			out.Passed = out.Passed && passed
		}
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(out); err != nil {
		os.Exit(1)
	}
	if !out.Passed {
		os.Exit(1)
	}
}
