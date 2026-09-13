package main_test

import (
	"encoding/json"
	"lerna/conformance"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestCLIReportsSuccessAndUnknownProfileFailure(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "contractcheck")
	if out, err := exec.Command("go", "build", "-o", binary, ".").CombinedOutput(); err != nil {
		t.Fatalf("build CLI: %v\n%s", err, out)
	}
	for _, tc := range []struct {
		profile string
		pass    bool
	}{{"sdk-contract-v1", true}, {"local-auth-v1", true}, {"durable-tasks-v1", true}, {"bounded-worker-v1", true}, {"task-control-v1", true}, {"restricted-grants-v1", true}, {"controlled-content-v1", true}, {"long-term-memory-v1", true}, {"memory-deletion-v1", true}, {"personalized-context-v1", true}, {"unknown-profile", false}} {
		t.Run(tc.profile, func(t *testing.T) {
			out, err := exec.Command(binary, "-profile", tc.profile).Output()
			if (err == nil) != tc.pass {
				t.Fatalf("exit mismatch: %v\n%s", err, out)
			}
			var report conformance.Report
			if err := json.Unmarshal(out, &report); err != nil {
				t.Fatal(err)
			}
			if tc.profile == "personalized-context-v1" && len(report.Results) != 52 {
				t.Fatalf("incomplete personalized profile: %d cases", len(report.Results))
			}
			if report.Passed() != tc.pass || report.Profile != tc.profile {
				t.Fatalf("wrong report: %+v", report)
			}
		})
	}
}
