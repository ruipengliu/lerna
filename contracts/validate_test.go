package contracts

import (
	"testing"
)

func TestFrozenSchemaKeepsIntegerAndUnionConstraints(t *testing.T) {
	for _, tc := range []struct {
		def, raw string
		valid    bool
	}{{"Revision", "9007199254740991", true}, {"Revision", "9007199254740992", false}, {"Revision", "1.5", false}, {"Revision", "1.0", true}, {"Id", `"model_call_0123456789abcdef0123456789abcdef"`, true}, {"TaskPauseInput", `{}`, false}, {"TaskPauseInput", `{"reason":"x"}`, true}, {"Amount", `{"unit":"usd","amount":"1.00000000000000000001"}`, true}, {"Amount", `{"unit":"usd","amount":1}`, false}} {
		t.Run(tc.def+tc.raw, func(t *testing.T) {
			if e := Validate(tc.def, []byte(tc.raw)); (e == nil) != tc.valid {
				t.Fatal(e)
			}
		})
	}
}
