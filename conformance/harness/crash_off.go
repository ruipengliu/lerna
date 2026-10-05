//go:build !fault

package harness

func crashPoint(any) (string, bool) { return "", false }
