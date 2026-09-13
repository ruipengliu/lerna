package fetchcheck

import (
	"encoding/json"
	"lerna/brain"
	"lerna/profiles/searchcheck"
	"reflect"
	"testing"
)

// This is a real loopback HTTP/Core/SDK publication test with a local protocol
// model. It is deliberately not the independent semantic quality evaluation.
func TestReferenceResearchMaterialsRunThroughOriginalTask(t *testing.T) {
	materials, err := searchcheck.LoadSources()
	if err != nil {
		t.Fatal(err)
	}
	for _, material := range materials.Cases {
		t.Run(material.ID, func(t *testing.T) {
			record := runReferenceResearchTask(t, material)
			actual := record.Answer
			if record.Usage.ModelRequests != 3 || record.Usage.SearchRequests != 1 || record.Usage.PageRequests != uint64(len(material.Pages)) || record.Usage.NetworkCharged != uint64(len(material.Pages)+1) || record.Usage.ActionQueries == 0 || record.Usage.QueryLimit != 128 || record.Usage.ActionQueries > uint64(record.Usage.QueryLimit) {
				t.Fatal("profile report lost original task usage")
			}
			var generated brain.EvidenceAnswer
			if json.Unmarshal(record.ModelOutput, &generated) != nil || !reflect.DeepEqual(generated, actual) {
				t.Fatal("archived model output differs from the published answer")
			}
			var pageBlocks []brain.Block
			discoveries := 0
			for _, block := range record.Input.Blocks {
				switch block.Role {
				case "external-evidence", "external-evidence-gap":
					pageBlocks = append(pageBlocks, block)
				case "search-candidates":
					discoveries++
				default:
					t.Fatalf("unexpected evidence role: %s", block.Role)
				}
			}
			wantDiscoveries := 0
			if material.ID == "fetch_failed" {
				wantDiscoveries = 1 // Discovery grounds the scope of the failed page request.
			}
			if len(pageBlocks) != len(material.Pages) || discoveries != wantDiscoveries {
				t.Fatal("archive omitted the evidence available to the answer model")
			}
			if err := brain.ValidateEvidenceAnswer(record.ModelOutput, record.Input, brain.MaxAnswerBytes); err != nil {
				t.Fatalf("saved evidence cannot validate the original model output: %v", err)
			}
			if material.ID == "fetch_failed" {
				var failure struct {
					Status   string
					Requests uint32
				}
				if pageBlocks[0].Role != "external-evidence-gap" || json.Unmarshal([]byte(pageBlocks[0].Text), &failure) != nil || failure.Status != "denied" || failure.Requests != 1 {
					t.Fatal("archive lost the actual denial fact")
				}
			}
			raw, err := json.Marshal(record)
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("mode=loopback-http model=local-protocol-fixture sources_sha256=%s record=%s", materials.SHA256, raw)
			if actual.Status != material.ID {
				t.Fatalf("published status = %s", actual.Status)
			}
			if material.ID == "insufficient" && (len(actual.Claims) != 0 || len(actual.Gaps) != 1) {
				t.Fatal("missing cost turned into an asserted answer")
			}
			if material.ID == "fetch_failed" && len(actual.Claims) != 0 {
				t.Fatal("denied page turned into an acquired claim")
			}
		})
	}
}
