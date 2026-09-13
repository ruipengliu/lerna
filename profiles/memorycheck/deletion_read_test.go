package memorycheck

import (
	"context"
	"testing"
)

func TestDeletionAfterRecordLoadBlocksOriginalQuery(t *testing.T) {
	if err := CheckDeletionReadRace(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestDeletionOfCoverageWitnessBlocksOriginalQuery(t *testing.T) {
	if err := CheckDeletionCoverageRace(context.Background()); err != nil {
		t.Fatal(err)
	}
}
