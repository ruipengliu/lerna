package memorycheck

import (
	"context"
	"testing"
)

func TestDeletionSDKStatusAndCleanupAcrossReopen(t *testing.T) {
	result, err := CheckDeletion(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.State != "committed" || result.Report.Local[0].State != "applied" || result.Report.Local[0].Position != 2 || result.Report.Replicas[0].State != "not_covered" || result.Report.DerivedArchives[0].State != "not_covered" {
		t.Fatalf("cleanup scope: %+v", result)
	}
}
