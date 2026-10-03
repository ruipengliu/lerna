package interaction_test

import (
	"os"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/interaction"
)

func TestPinnedCalendarSkipsGapChoosesEarlierFoldAndAbsentMonthDay(t *testing.T) {
	zone, e := os.ReadFile("/usr/share/zoneinfo/America/New_York")
	if e != nil {
		t.Fatal(e)
	}
	utc, e := os.ReadFile("/usr/share/zoneinfo/Etc/UTC")
	if e != nil {
		t.Fatal(e)
	}
	calendar, e := interaction.NewTZDB("2026b", map[string][]byte{"America/New_York": zone, "Etc/UTC": utc})
	if e != nil {
		t.Fatal(e)
	}
	s, e := interaction.New(interaction.Config{}, interaction.Ports{Calendar: calendar})
	if e != nil {
		t.Fatal(e)
	}
	cases := []struct {
		rule              interaction.ScheduleSpec
		zone, after, want string
	}{
		{interaction.ScheduleSpec{Type: "daily", LocalTime: "02:30:00"}, "America/New_York", "2026-03-08T00:00:00Z", "2026-03-09T06:30:00Z"},
		{interaction.ScheduleSpec{Type: "daily", LocalTime: "01:30:00"}, "America/New_York", "2026-11-01T00:00:00Z", "2026-11-01T05:30:00Z"},
		{interaction.ScheduleSpec{Type: "daily", LocalTime: "01:30:00"}, "America/New_York", "2026-11-01T05:45:00Z", "2026-11-02T06:30:00Z"},
		{interaction.ScheduleSpec{Type: "monthly", LocalTime: "12:00:00", Monthdays: []uint64{31}}, "Etc/UTC", "2026-04-01T00:00:00Z", "2026-05-31T12:00:00Z"},
		{interaction.ScheduleSpec{Type: "weekly", LocalTime: "08:00:00", Weekdays: []uint64{1, 5}}, "Etc/UTC", "2026-10-02T08:00:00.000000001Z", "2026-10-05T08:00:00Z"},
		{interaction.ScheduleSpec{Type: "interval", AnchorAt: "2026-10-01T00:00:00Z", EverySeconds: 60}, "Etc/UTC", "2026-10-01T00:01:00.001Z", "2026-10-01T00:02:00Z"},
	}
	for _, tc := range cases {
		after, _ := api.ParseTime(tc.after)
		next, e := s.NextDue(tc.rule, tc.zone, "2026b", after)
		if e != nil || next == nil || api.Time(*next) != tc.want {
			t.Fatalf("%+v after=%s got=%v err=%v want=%s", tc.rule, tc.after, next, e, tc.want)
		}
	}
	if _, e = s.NextDue(cases[0].rule, cases[0].zone, "2025a", time.Now()); !api.IsCode(e, "dependency_unavailable") {
		t.Fatalf("pinned timezone fell back: %v", e)
	}
	if _, e = s.NextDue(interaction.ScheduleSpec{Type: "weekly", LocalTime: "09:00:00", Weekdays: []uint64{5, 1}}, "Etc/UTC", "2026b", time.Now()); !api.IsCode(e, "invalid_request") {
		t.Fatalf("unsorted calendar accepted: %v", e)
	}
}
