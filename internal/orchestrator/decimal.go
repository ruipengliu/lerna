package orchestrator

import (
	"math/big"
	"regexp"
	"strings"
)

var decimalPattern = regexp.MustCompile(`^(0|[1-9][0-9]*)(\.[0-9]+)?$`)

// Bound decimal parsing independently of payload bytes; never round money.
func decimal(s string) (*big.Rat, error) {
	if len(s) > 1<<20 || !decimalPattern.MatchString(s) {
		return nil, failure("invalid_argument", "amount must be a bounded exact nonnegative decimal")
	}
	v, ok := new(big.Rat).SetString(s)
	if !ok {
		return nil, failure("invalid_argument", "invalid amount")
	}
	return v, nil
}
func compare(a, b string) (int, error) {
	x, e := decimal(a)
	if e != nil {
		return 0, e
	}
	y, e := decimal(b)
	if e != nil {
		return 0, e
	}
	return x.Cmp(y), nil
}
func arithmetic(a, b string, subtract bool) (string, error) {
	x, e := decimal(a)
	if e != nil {
		return "", e
	}
	y, e := decimal(b)
	if e != nil {
		return "", e
	}
	if subtract {
		x.Sub(x, y)
	} else {
		x.Add(x, y)
	}
	if x.Sign() < 0 {
		return "", failure("internal_error", "negative ledger amount")
	}
	precision := 0
	for _, s := range []string{a, b} {
		if i := strings.IndexByte(s, '.'); i >= 0 && len(s)-i-1 > precision {
			precision = len(s) - i - 1
		}
	}
	out := x.FloatString(precision)
	if strings.Contains(out, ".") {
		out = strings.TrimRight(strings.TrimRight(out, "0"), ".")
	}
	return out, nil
}
func add(a, b string) (string, error) { return arithmetic(a, b, false) }
func sub(a, b string) (string, error) { return arithmetic(a, b, true) }
