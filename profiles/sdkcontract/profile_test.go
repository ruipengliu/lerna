package sdkcontract_test

import (
	"context"
	"lerna/conformance"
	"lerna/profiles/sdkcontract"
	"testing"
)

func TestSDKContractProfileProducesScopedEvidence(t *testing.T) {
	p, err := sdkcontract.Profile()
	if err != nil {
		t.Fatal(err)
	}
	r := conformance.Run(context.Background(), p)
	if !r.Passed() {
		t.Fatalf("contract profile failed: %+v", r.Results)
	}
	found := false
	for _, item := range r.Results {
		if item.Evidence == "real_runtime" {
			found = true
			if item.Status == conformance.Passed || item.Required {
				t.Fatal("fixture claims real runtime evidence")
			}
		}
	}
	if !found {
		t.Fatal("missing real runtime limitation")
	}
}
