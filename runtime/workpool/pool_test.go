package workpool_test

import (
	"reflect"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/contract"
	"github.com/ruipengliu/lerna/runtime/workpool"
)

func TestWaitingTenantKeepsPlaceWhileNewArrivalJoinsTail(t *testing.T) {
	now := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	members := []contract.OwnerRef{{TenantID: "tenant-a", OwnerID: "owner-a"}, {TenantID: "tenant-b", OwnerID: "owner-b"}, {TenantID: "tenant-c", OwnerID: "owner-c"}}
	cursor := workpool.Cursor{Order: []contract.ID{"tenant-b", "tenant-a"}, Waiting: map[contract.ID]time.Time{"tenant-b": now.Add(-time.Minute), "tenant-a": now.Add(-time.Second)}, LastAllocated: map[contract.ID]int64{}, After: "position", Through: "boundary"}
	ready := map[contract.ID]time.Time{"tenant-a": now, "tenant-b": now, "tenant-c": now.Add(-time.Hour)}
	got := workpool.RefreshCursor(cursor, members, ready, now)
	if !reflect.DeepEqual(got.Order, []contract.ID{"tenant-b", "tenant-a", "tenant-c"}) || got.After != "position" || got.Through != "boundary" {
		t.Fatalf("waiting order changed: %+v", got)
	}
	if err := workpool.RotateCursor(&got, true); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Order, []contract.ID{"tenant-a", "tenant-c", "tenant-b"}) || got.Sequence != 1 || got.LastAllocated["tenant-b"] != 1 || got.After != "" || got.Through != "" {
		t.Fatalf("successful allocation did not rotate fairly: %+v", got)
	}
}
