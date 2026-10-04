//go:build integration

package decisionfixture

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"go/parser"
	"go/token"
	"os"
	"testing"
)

// Mechanical conversion checks inspect only the added supervisor source.
// They create no database/native holder and are not native fault evidence.
func TestGuard970AddedDriverQualificationAndRelease(t *testing.T) {
	original, err := os.ReadFile("testdata/legacy-970fd90/conformance/internal/decisionfixture/legacy_writer_test.go.txt")
	if err != nil {
		t.Fatal(err)
	}
	guarded, err := guard970AddedDriverQualificationAndRelease(original)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(guarded)
	if len(guarded) != 8572 || hex.EncodeToString(sum[:]) != "aa7097a937c0defb5ee7e356b4e2ee71b7c069d7f96b514a2bdc07c07db05945" {
		t.Fatal("finite original supervisor conversion changed")
	}
	if _, err = parser.ParseFile(token.NewFileSet(), "own-restored-legacy-driver.go", guarded, parser.AllErrors); err != nil {
		t.Fatal(err)
	}
	variants := map[string][]byte{
		"unknown hash":                     append(append([]byte(nil), original...), '\n'),
		"missing expected qualification":   bytes.Replace(original, []byte("expired old Finish unexpectedly committed"), nil, 1),
		"duplicate expected qualification": append(append([]byte(nil), original...), []byte("\n\t\t\tif err = service.RunClaim(ctx, claim.Claim); err == nil {\n\t\t\t\tt.Fatal(\"expired old Finish unexpectedly committed\")\n\t\t\t}")...),
		"already converted":                guarded,
	}
	for name, body := range variants {
		t.Run(name, func(t *testing.T) {
			if _, err := guard970AddedDriverQualificationAndRelease(body); err == nil {
				t.Fatal("unverified or repeated source conversion accepted")
			}
		})
	}
}

func TestHistoricalCompilerJoinedCausesRemainSafelyInspectable(t *testing.T) {
	waitCause := errors.New("mechanical Wait cause with private diagnostic")
	groupCause := errors.New("mechanical group observation cause")
	failure := upgradeCause("frozen build group exit confirmation", errors.Join(waitCause, groupCause))
	if !errors.Is(failure, waitCause) || !errors.Is(failure, groupCause) {
		t.Fatal("historical supervision discarded a joined cause")
	}
	if bytes.Contains([]byte(failure.Error()), []byte("private diagnostic")) {
		t.Fatal("historical stage exposed raw diagnostics")
	}
}
