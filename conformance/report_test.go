package conformance_test

import (
	"context"
	"lerna/conformance"
	"testing"
)

func TestRequiredEvidenceCannotPassWithoutExecution(t *testing.T) {
	for _, status := range []conformance.Status{conformance.NotImplemented, conformance.NotRun, conformance.Unsupported, conformance.Failed} {
		t.Run(string(status), func(t *testing.T) {
			r := conformance.Run(context.Background(), conformance.Profile{ID: "test", Version: "1", Cases: []conformance.Case{{ID: "required", Required: true, Evidence: "contract_fixture", Availability: status}}})
			if r.Passed() {
				t.Fatalf("required %s marked passed", status)
			}
			if len(r.Results) != 1 || r.Results[0].Status != status {
				t.Fatalf("lost case status: %+v", r)
			}
		})
	}
}

func TestRunnerRequiresObservedExpectedOutcome(t *testing.T) {
	profile := conformance.Profile{ID: "negative-case", Version: "1", Cases: []conformance.Case{{ID: "invalid", Evidence: "contract_fixture", Required: true, Expected: "INVALID_ARGUMENT", Check: func(context.Context) (string, error) { return "ACCEPTED", nil }}}}
	r := conformance.Run(context.Background(), profile)
	if r.Passed() || r.Results[0].Status != conformance.Failed || r.Results[0].Actual != "ACCEPTED" {
		t.Fatalf("unexpected acceptance hidden: %+v", r)
	}
	profile.Cases[0].Check = func(context.Context) (string, error) { return "INVALID_ARGUMENT", nil }
	r = conformance.Run(context.Background(), profile)
	if !r.Passed() {
		t.Fatalf("expected rejection failed: %+v", r)
	}
	profile.Cases = append(profile.Cases, conformance.Case{ID: "live-run", Evidence: "real_runtime", Availability: conformance.NotImplemented})
	if !conformance.Run(context.Background(), profile).Passed() {
		t.Fatal("explicitly out-of-scope evidence blocked fixture profile")
	}
	profile.Cases[0].Availability = conformance.Passed
	if conformance.Run(context.Background(), profile).Passed() {
		t.Fatal("declared passing status bypassed observation")
	}
}

func TestCancelledAndEmptyProfilesDoNotPass(t *testing.T) {
	if conformance.Run(context.Background(), conformance.Profile{ID: "empty", Version: "1"}).Passed() {
		t.Fatal("empty profile passed")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	p := conformance.Profile{ID: "cancelled", Version: "1", Cases: []conformance.Case{{ID: "one", Required: true, Evidence: "contract_fixture", Check: func(context.Context) (string, error) { t.Fatal("cancelled check executed"); return "", nil }}}}
	r := conformance.Run(ctx, p)
	if r.Passed() || r.Results[0].Status != conformance.NotRun {
		t.Fatalf("cancelled run: %+v", r)
	}
}
