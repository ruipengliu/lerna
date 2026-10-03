package api

import (
	"math/big"
	"regexp"
	"strings"
)

var decimalPattern = regexp.MustCompile(`^(0|[1-9][0-9]*)(\.[0-9]{1,9})?$`)

func decimalInt(s string) (*big.Int, error) {
	if len(s) > 4096 || !decimalPattern.MatchString(s) {
		return nil, E("invalid_request", "invalid_decimal")
	}
	a, b, _ := strings.Cut(s, ".")
	n, ok := new(big.Int).SetString(a+b+strings.Repeat("0", 9-len(b)), 10)
	if !ok {
		return nil, E("invalid_request", "invalid_decimal")
	}
	return n, nil
}
func decimalString(n *big.Int) string {
	s := n.String()
	if len(s) <= 9 {
		s = strings.Repeat("0", 10-len(s)) + s
	}
	a, b := s[:len(s)-9], strings.TrimRight(s[len(s)-9:], "0")
	if b == "" {
		return a
	}
	return a + "." + b
}
func CompareDecimal(a, b string) (int, error) {
	x, e := decimalInt(a)
	if e != nil {
		return 0, e
	}
	y, e := decimalInt(b)
	if e != nil {
		return 0, e
	}
	return x.Cmp(y), nil
}
func AddDecimal(a, b string) (string, error) {
	x, e := decimalInt(a)
	if e != nil {
		return "", e
	}
	y, e := decimalInt(b)
	if e != nil {
		return "", e
	}
	return decimalString(x.Add(x, y)), nil
}
func SubDecimal(a, b string) (string, error) {
	x, e := decimalInt(a)
	if e != nil {
		return "", e
	}
	y, e := decimalInt(b)
	if e != nil {
		return "", e
	}
	if x.Cmp(y) < 0 {
		return "", E("invalid_request", "negative_amount")
	}
	return decimalString(x.Sub(x, y)), nil
}
func ValidateAmounts(a []Amount) error {
	seen := map[string]bool{}
	for _, v := range a {
		if v.Unit == "" || len(v.Unit) > 32 || seen[v.Unit] {
			return E("invalid_request", "invalid_amount_unit")
		}
		seen[v.Unit] = true
		if _, e := decimalInt(v.Value); e != nil {
			return e
		}
	}
	return nil
}
