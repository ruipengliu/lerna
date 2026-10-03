package api_test

import (
	"github.com/ruipengliu/lerna/api"
	"testing"
)

func TestCanonicalCrossLanguageVectors(t *testing.T) {
	for _, v := range []struct{ raw, want string }{
		{`{"b":2,"a":1}`, `{"a":1,"b":2}`},
		{`{"s":"<>&\u2028","n":-0,"e":0.00000001}`, "{\"e\":1e-8,\"n\":0,\"s\":\"<>&\u2028\"}"},
		{`{"😀":1,"\ufffd":2}`, `{"😀":1,"�":2}`},
		{`[0.000001,4.50,1e3]`, `[0.000001,4.5,1000]`},
	} {
		got, e := api.Canonical([]byte(v.raw))
		if e != nil || string(got) != v.want {
			t.Fatalf("%s -> %s (%v), want %s", v.raw, got, e, v.want)
		}
	}
}
func TestRejectUntrustedJSON(t *testing.T) {
	for _, s := range []string{`{"a":1,"a":2}`, `{"a":1,"\u0061":2}`, `"\ud800"`, `"\udc00"`, `9007199254740992`, `NaN`, `{} {}`, "\"\xff\""} {
		if _, e := api.ParseJSON([]byte(s)); e == nil {
			t.Fatalf("accepted %q", s)
		}
	}
}
func TestMoneyUsesExactNineDecimalPlaces(t *testing.T) {
	v, e := api.AddDecimal("0.1", "0.2")
	if e != nil || v != "0.3" {
		t.Fatalf("%s %v", v, e)
	}
	v, e = api.SubDecimal("10", "8")
	if e != nil || v != "2" {
		t.Fatalf("%s %v", v, e)
	}
	if _, e = api.SubDecimal("1", "2"); e == nil {
		t.Fatal("negative accepted")
	}
}
func TestClosedRecordContract(t *testing.T) {
	v, e := api.NewValidator(api.Ref("Amount"))
	if e != nil {
		t.Fatal(e)
	}
	if e = v.Validate([]byte(`{"unit":"USD","value":"0.1"}`)); e != nil {
		t.Fatal(e)
	}
	for _, raw := range []string{`{"unit":"USD","value":0.1}`, `{"unit":"USD","value":"1","extra":true}`, `{"unit":"USD","value":"01"}`} {
		if e = v.Validate([]byte(raw)); e == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
}
