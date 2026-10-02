package orchestrator

import (
	"strings"
	"testing"
)

func TestExactDecimalDoesNotRoundTrustedDebt(t *testing.T) {
	a := "0." + strings.Repeat("0", 79) + "1"
	sum, e := add(a, a)
	if e != nil || sum != "0."+strings.Repeat("0", 79)+"2" {
		t.Fatal(sum, e)
	}
	d, e := sub("9007199254740993.000000000000000001", "9007199254740992")
	if e != nil || d != "1.000000000000000001" {
		t.Fatal(d, e)
	}
	if cmp, e := compare(d, "1"); e != nil || cmp != 1 {
		t.Fatal(cmp, e)
	}
	for _, invalid := range []string{"-1", "01", "1e3", "NaN", ".1", "1."} {
		if _, e := decimal(invalid); e == nil {
			t.Fatal("accepted", invalid)
		}
	}
}
