package content

import (
	"errors"
	"testing"
	"time"

	v "github.com/ruipengliu/lerna/contract/v1_2"
	"github.com/ruipengliu/lerna/runtime"
)

func TestCleanupAcceptedCapRejectsInvalidPersistentTime(t *testing.T) {
	for _, text := range []v.Time{"", "not-a-time", "2026-02-30T12:00:00.000000Z", "0000-01-01T00:00:00.000000Z", "0001-01-01T00:00:00.000000Z", "2026-10-04T12:00:00,123456Z"} {
		cap, err := cleanupAcceptedCap(text)
		if !errors.Is(err, runtime.ErrScope) || !cap.IsZero() {
			t.Fatal("malformed persistent cap became deletion evidence", text, cap, err)
		}
	}
	cap, err := cleanupAcceptedCap("2026-10-04T12:00:00.123456Z")
	want := time.Date(2026, 10, 4, 12, 0, 0, 123456000, time.UTC)
	if err != nil || !cap.Equal(want) {
		t.Fatal("valid original cap changed", cap, err)
	}
}
