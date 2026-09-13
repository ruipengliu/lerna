package searchcheck_test

import (
	"encoding/json"
	"lerna/profiles/searchcheck"
	"strings"
	"testing"
)

func TestFrozenResearchCasesSeparateCandidateInputFromJudging(t *testing.T) {
	sources, err := searchcheck.LoadSources()
	if err != nil {
		t.Fatal(err)
	}
	if sources.SHA256 != "c4e872461ebd41c2ad5e3d64b8075d87bab7d12bc786f74bc9e0ae2a357dfe5c" || len(sources.Cases) != 4 {
		t.Fatal("candidate materials changed")
	}
	for _, c := range sources.Cases {
		input, _ := json.Marshal(c)
		for _, field := range []string{"required_facts", "required_disposition", "forbidden", "supporting_quote"} {
			if strings.Contains(string(input), `"`+field+`"`) {
				t.Fatal("judging criteria entered candidate input")
			}
		}
	}
	if sources.Cases[0].Pages[0].Body != "Northbank civic record, edition 2026-01. The Northbank footbridge opened on 14 May 1998. Its length is 84 metres. These measurements describe the original bridge." {
		t.Fatal("wrong first source")
	}
	expectations, err := searchcheck.LoadExpectations()
	if err != nil {
		t.Fatal(err)
	}
	if expectations.SHA256 != "5a171025c1fc7db9b82b82ae6e6cfbbe93a80bdaf6f2f6a6f6f0c3a2b2def3a9" || len(expectations.Cases) != 4 || len(expectations.Cases[0].RequiredFacts) != 2 {
		t.Fatal("independent denominators changed")
	}
	sources.Cases[0].Pages[0].Body = "changed by caller"
	next, err := searchcheck.LoadSources()
	if err != nil || next.Cases[0].Pages[0].Body == "changed by caller" {
		t.Fatal("candidate mutated the shared frozen corpus")
	}
	if sources.Cases[3].Pages[0].Status != 403 || sources.Cases[3].Pages[0].Body != "" {
		t.Fatal("failure fixture invented acquired page bytes")
	}
}
