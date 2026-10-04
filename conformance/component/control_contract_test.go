package component_test

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	v "github.com/ruipengliu/lerna/contract/v1_1"
)

// The same exact bytes are consumed by both typed CLI roundtrips. This focused
// public codec tracer verifies the pre-Input cancellation observation contract.
func TestCancelledBeforeInputHasExactZeroObservations(t *testing.T) {
	body, err := os.ReadFile("../fixtures/1.1.0/fixtures.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Name   string `json:"name"`
		Schema string `json:"schema"`
		Wire   string `json:"wire"`
		Valid  bool   `json:"valid"`
	}
	if err = json.Unmarshal(body, &fixtures); err != nil {
		t.Fatal(err)
	}
	selected := 0
	for _, fixture := range fixtures {
		if fixture.Name != "cancel before decide no fake snapshot" && fixture.Name != "current stop outside frozen cancelled Decision" && !strings.HasPrefix(fixture.Name, "pre-input cancellation cannot invent ") {
			continue
		}
		selected++
		t.Run(fixture.Name, func(t *testing.T) {
			var decision v.Decision
			if fixture.Schema == "Decision" {
				decision, err = v.Decode[v.Decision]([]byte(fixture.Wire))
			} else {
				var response v.DecisionGetResponse
				response, err = v.Decode[v.DecisionGetResponse]([]byte(fixture.Wire))
				if err == nil {
					found, ok := response.AsFound()
					if !ok {
						t.Fatal("expected independently controlled found Decision")
					}
					decision = found.Decision
				}
			}
			if !fixture.Valid {
				if err == nil {
					t.Fatal("pre-Input close invented an execution observation")
				}
				var failure *v.ContractError
				if errors.As(err, &failure) && failure.Code != "schema_invalid" {
					t.Fatal("pre-Input close returned an unrelated refusal", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			encoded, err := v.Encode(decision)
			if err != nil {
				t.Fatal(err)
			}
			decision, err = v.Decode[v.Decision](encoded)
			if err != nil {
				t.Fatal(err)
			}
			closed, ok := decision.AsCancelled()
			if !ok || closed.Input != nil {
				t.Fatal("pre-Input close invented original Input")
			}
			u := closed.Usage
			if u.InputBytes != "0" || u.OutputBytes != "0" || u.RuleSteps != "0" || u.RuleStarts != "0" || u.ModelRequests != "0" || u.Cost.IntegerValue != "0" || !u.MeasurementsComplete {
				t.Fatal("pre-Input close did not preserve exact zero observations")
			}
		})
	}
	if selected != 5 {
		t.Fatal("missing shared pre-Input cancellation fixture")
	}
}
