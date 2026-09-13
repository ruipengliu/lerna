package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestFrozenProfileCommandReportsRuntimeAndQualitySeparately(t *testing.T) {
	var output, errors bytes.Buffer
	if code := run([]string{"-case", "insufficient"}, &output, &errors); code != 0 {
		t.Fatalf("exit=%d: %s", code, errors.String())
	}
	var report struct {
		Profile, SemanticQuality string
		RuntimeVerified          bool
		ExternalModelRequests    int
		Cases                    []struct {
			CaseID string
			Record struct{ Answer struct{ Status string } }
		}
	}
	if err := json.Unmarshal(output.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.Profile != "frozen-loopback-v1" || report.SemanticQuality != "not_evaluated" || !report.RuntimeVerified || report.ExternalModelRequests != 0 || len(report.Cases) != 1 || report.Cases[0].CaseID != "insufficient" || report.Cases[0].Record.Answer.Status != "insufficient" {
		t.Fatal("command misreported runtime or semantic quality")
	}
}

func TestResearchCommandRejectsUnconfiguredModes(t *testing.T) {
	for _, args := range [][]string{{"-profile", "public"}, {"-case", "unknown"}, {"unexpected"}, {"-search-format", "duckduckgo-html"}, {"-profile", "reference-loopback-v2", "-search-format", "unknown"}} {
		var output, errors bytes.Buffer
		if code := run(args, &output, &errors); code != 2 || output.Len() != 0 {
			t.Fatalf("invalid configuration produced execution output: %d", code)
		}
	}
}

func TestFrozenReplayCommandReportsZeroNetwork(t *testing.T) {
	var output, diagnostics bytes.Buffer
	if code := run([]string{"-profile", "frozen-replay-v1", "-case", "fetch_failed"}, &output, &diagnostics); code != 0 {
		t.Fatalf("exit=%d: %s", code, diagnostics.String())
	}
	var got struct {
		Mode, SemanticQuality string
		RuntimeVerified       bool
		Cases                 []struct {
			Record struct {
				Usage  struct{ SearchRequests, PageRequests, NetworkCharged uint64 }
				Answer struct{ Status string }
			}
		}
	}
	if json.Unmarshal(output.Bytes(), &got) != nil || got.Mode != "fixed-replay" || !got.RuntimeVerified || got.SemanticQuality != "not_evaluated" || len(got.Cases) != 1 {
		t.Fatal("replay mode was lost")
	}
	usage := got.Cases[0].Record.Usage
	if usage.SearchRequests != 0 || usage.PageRequests != 0 || usage.NetworkCharged != 2 || got.Cases[0].Record.Answer.Status != "fetch_failed" {
		t.Fatal("fixed denial masqueraded as HTTP or refunded its reservation")
	}
}

func TestReferenceBudgetIsExplicitAndFrozenBudgetCannotBeOverridden(t *testing.T) {
	var output, diagnostics bytes.Buffer
	if code := run([]string{"-profile", "reference-replay-v2", "-case", "conflicting", "-queries", "128"}, &output, &diagnostics); code != 0 {
		t.Fatalf("configured reference failed: exit=%d %s %s", code, diagnostics.String(), output.String())
	}
	var got struct {
		Profile, SemanticQuality string
		QueryLimit               uint32
		Cases                    []struct {
			Record struct {
				Usage  struct{ ActionQueries uint32 }
				Answer struct{ Status string }
			}
		}
	}
	if err := json.Unmarshal(output.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Profile != "reference-replay-v2" || got.QueryLimit != 128 || got.SemanticQuality != "not_evaluated" || len(got.Cases) != 1 || got.Cases[0].Record.Answer.Status != "conflicting" || got.Cases[0].Record.Usage.ActionQueries <= 64 || got.Cases[0].Record.Usage.ActionQueries > 128 {
		t.Fatal("configuration or actual query accounting was lost")
	}
	for _, args := range [][]string{
		{"-profile", "frozen-replay-v1", "-queries", "128"},
		{"-profile", "reference-replay-v2", "-queries", "129"},
	} {
		output.Reset()
		diagnostics.Reset()
		if code := run(args, &output, &diagnostics); code != 2 || output.Len() != 0 {
			t.Fatal("invalid budget dispatched work")
		}
	}
}

func TestFrozenFailureAndConfiguredBudgetExhaustionRemainVisible(t *testing.T) {
	for _, args := range [][]string{
		{"-profile", "frozen-replay-v1", "-case", "conflicting"},
		{"-profile", "reference-replay-v2", "-case", "conflicting", "-queries", "1"},
	} {
		var output, diagnostics bytes.Buffer
		if code := run(args, &output, &diagnostics); code != 1 {
			t.Fatalf("budget exhaustion was relabelled as success: exit=%d %s", code, output.String())
		}
		var got struct {
			RuntimeVerified bool
			QueryLimit      uint32
			Cases           []struct {
				RuntimeVerified bool
				Error           string
				Record          json.RawMessage
			}
		}
		if err := json.Unmarshal(output.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		expected := uint32(64)
		if len(args) > 4 {
			expected = 1
		}
		if got.QueryLimit != expected || got.RuntimeVerified || len(got.Cases) != 1 || got.Cases[0].RuntimeVerified || got.Cases[0].Error == "" || len(got.Cases[0].Record) != 0 {
			t.Fatal("failed run hid its original budget or manufactured a result")
		}
		if expected == 64 && !strings.Contains(got.Cases[0].Error, "queries=64/64") {
			t.Fatal("frozen baseline failed for a different reason than query exhaustion")
		}
	}
}

func TestDuckDuckGoReferenceCommandReportsSyntheticFormat(t *testing.T) {
	var output, diagnostics bytes.Buffer
	if code := run([]string{"-profile", "reference-loopback-v2", "-search-format", "duckduckgo-html", "-case", "answerable"}, &output, &diagnostics); code != 0 {
		t.Fatalf("exit=%d: %s", code, diagnostics.String())
	}
	var got struct{ SearchFormat, Mode, SemanticQuality string }
	if err := json.Unmarshal(output.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.SearchFormat != "duckduckgo-html" || got.Mode != "loopback-http" || got.SemanticQuality != "not_evaluated" {
		t.Fatalf("misreported provider: %+v", got)
	}
}
