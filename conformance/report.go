// Package conformance runs named contract profiles and reports evidence honestly.
package conformance

import (
	"context"
	"fmt"
	"runtime"
	"runtime/debug"
)

type Status string

const (
	NotImplemented Status = "not_implemented"
	NotRun         Status = "not_run"
	Passed         Status = "passed"
	Failed         Status = "failed"
	Unsupported    Status = "unsupported"
)

type Case struct {
	ID, Evidence, Input, Expected string
	Required                      bool
	// Availability is empty for executable cases. Nonempty values are evidence
	// declarations only and can never manufacture a passed result.
	Availability Status
	Check        func(context.Context) (string, error)
}

type Profile struct {
	ID, Version   string
	Configuration map[string]string
	Method        string
	Limitations   []string
	Cases         []Case
}

type Result struct {
	ID       string `json:"id"`
	Evidence string `json:"evidence"`
	Required bool   `json:"required"`
	Input    string `json:"input"`
	Expected string `json:"expected"`
	Actual   string `json:"actual"`
	Status   Status `json:"status"`
	Error    string `json:"error,omitempty"`
}

type Build struct {
	Go           string            `json:"go"`
	Platform     string            `json:"platform"`
	Module       string            `json:"module"`
	Revision     string            `json:"revision"`
	Dirty        string            `json:"dirty"`
	Dependencies map[string]string `json:"dependencies"`
}

type Report struct {
	Status         Status            `json:"status"`
	FormatVersion  int               `json:"format_version"`
	Profile        string            `json:"profile"`
	ProfileVersion string            `json:"profile_version"`
	Configuration  map[string]string `json:"configuration"`
	Method         string            `json:"method"`
	Build          Build             `json:"build"`
	Limitations    []string          `json:"limitations"`
	Results        []Result          `json:"results"`
	Errors         []string          `json:"errors,omitempty"`
}

func (r Report) Passed() bool {
	if len(r.Errors) != 0 || len(r.Results) == 0 {
		return false
	}
	required := false
	for _, item := range r.Results {
		required = required || item.Required
		if item.Status == Failed || (item.Required && item.Status != Passed) {
			return false
		}
	}
	return required
}

func Run(ctx context.Context, profile Profile) Report {
	r := Report{FormatVersion: 1, Profile: profile.ID, ProfileVersion: profile.Version, Configuration: profile.Configuration, Method: profile.Method, Build: BuildInfo(), Limitations: profile.Limitations, Results: []Result{}}
	if profile.ID == "" || profile.Version == "" {
		r.Errors = append(r.Errors, "profile identity required")
	}
	seen := make(map[string]bool)
	for _, c := range profile.Cases {
		result := Result{ID: c.ID, Evidence: c.Evidence, Required: c.Required, Input: c.Input, Expected: c.Expected}
		if c.ID == "" || seen[c.ID] || c.Evidence == "" {
			result.Status, result.Error = Failed, "case requires unique ID and evidence kind"
		} else {
			result.Status, result.Actual, result.Error = execute(ctx, c)
		}
		seen[c.ID] = true
		r.Results = append(r.Results, result)
	}
	r.Status = Failed
	if r.Passed() {
		r.Status = Passed
	}
	return r
}

func execute(ctx context.Context, c Case) (Status, string, string) {
	if c.Availability != "" {
		switch c.Availability {
		case NotImplemented, NotRun, Unsupported, Failed:
			return c.Availability, "", ""
		default:
			return Failed, "", "invalid declared availability"
		}
	}
	if err := ctx.Err(); err != nil {
		return NotRun, "", err.Error()
	}
	if c.Check == nil {
		return NotImplemented, "", "no check registered"
	}
	actual, err := c.Check(ctx)
	if err != nil {
		return Failed, actual, err.Error()
	}
	if actual != c.Expected {
		return Failed, actual, fmt.Sprintf("expected %q, observed %q", c.Expected, actual)
	}
	return Passed, actual, ""
}

// BuildInfo captures the executable revision, toolchain and dependency evidence.
func BuildInfo() Build {
	b := Build{Go: runtime.Version(), Platform: runtime.GOOS + "/" + runtime.GOARCH, Revision: "unavailable", Dirty: "unavailable", Dependencies: make(map[string]string)}
	if info, ok := debug.ReadBuildInfo(); ok {
		b.Module = info.Main.Path + "@" + info.Main.Version
		for _, dep := range info.Deps {
			version := dep.Version + " " + dep.Sum
			if dep.Replace != nil {
				version += " replaced by " + dep.Replace.Path + "@" + dep.Replace.Version + " " + dep.Replace.Sum
			}
			b.Dependencies[dep.Path] = version
		}
		for _, setting := range info.Settings {
			if setting.Key == "vcs.revision" {
				b.Revision = setting.Value
			}
			if setting.Key == "vcs.modified" {
				b.Dirty = setting.Value
			}
		}
	}
	return b
}
