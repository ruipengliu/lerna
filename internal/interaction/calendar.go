package interaction

import (
	"bytes"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/ruipengliu/lerna/api"
)

// ScheduleSpec 是有限规则集；跨类型字段也被拒绝，不接纳 cron/RRULE。
type ScheduleSpec struct {
	Type         string   `json:"type"`
	At           string   `json:"at,omitempty"`
	AnchorAt     string   `json:"anchor_at,omitempty"`
	EverySeconds uint64   `json:"every_seconds,omitempty"`
	LocalTime    string   `json:"local_time,omitempty"`
	Weekdays     []uint64 `json:"weekdays,omitempty"`
	Monthdays    []uint64 `json:"monthdays,omitempty"`
}
type TZDB struct {
	version string
	zones   map[string]*time.Location
}

var zoneName = regexp.MustCompile(`^[A-Za-z0-9_+/-]{1,128}$`)
var clockTime = regexp.MustCompile(`^(?:[01][0-9]|2[0-3]):[0-5][0-9]:[0-5][0-9]$`)

// NewTZDB 只接受受信宿主按锁定版本取得的 TZif；构造时解析，Tx 内 Load 不进行 I/O。
func NewTZDB(version string, data map[string][]byte) (*TZDB, error) {
	if version == "" || len(version) > 32 || len(data) == 0 || len(data) > 128 {
		return nil, fmt.Errorf("invalid pinned tzdb")
	}
	db := &TZDB{version: version, zones: map[string]*time.Location{}}
	for name, b := range data {
		if !validZoneName(name) || len(b) > 1<<20 {
			return nil, fmt.Errorf("invalid tzdb zone")
		}
		loc, err := time.LoadLocationFromTZData(name, append([]byte(nil), b...))
		if err != nil {
			return nil, err
		}
		db.zones[name] = loc
	}
	return db, nil
}
func validZoneName(name string) bool {
	return zoneName.MatchString(name) && !strings.HasPrefix(name, "/") && !strings.Contains(name, "..")
}

// OpenTZDB 显式比对本地发布版本后只载入有限 zone 集；版本不存在时不回退。
func OpenTZDB(root, version string, names []string) (*TZDB, error) {
	b, err := os.ReadFile(filepath.Join(root, "tzdata.zi"))
	if err != nil {
		return nil, api.E("dependency_unavailable", "tzdb_unavailable")
	}
	line, _, _ := bytes.Cut(b, []byte("\n"))
	if string(line) != "# version "+version {
		return nil, api.E("dependency_unavailable", "tzdb_unavailable")
	}
	if len(names) == 0 || len(names) > 128 {
		return nil, invalid("invalid_calendar_value")
	}
	data := map[string][]byte{}
	for _, name := range names {
		if !validZoneName(name) {
			return nil, invalid("invalid_calendar_value")
		}
		data[name], err = os.ReadFile(filepath.Join(root, name))
		if err != nil {
			return nil, api.E("dependency_unavailable", "tzdb_unavailable")
		}
	}
	return NewTZDB(version, data)
}
func (db *TZDB) Load(name, version string) (*time.Location, error) {
	loc := db.zones[name]
	if version != db.version || loc == nil {
		return nil, api.E("dependency_unavailable", "tzdb_unavailable")
	}
	return loc, nil
}
func orderedDays(days []uint64, max uint64) bool {
	if len(days) == 0 || len(days) > int(max) {
		return false
	}
	var old uint64
	for _, d := range days {
		if d <= old || d > max {
			return false
		}
		old = d
	}
	return true
}
func validateSpec(spec ScheduleSpec) error {
	calendar := spec.LocalTime != "" || len(spec.Weekdays) > 0 || len(spec.Monthdays) > 0
	switch spec.Type {
	case "once_at":
		if spec.At == "" || spec.AnchorAt != "" || spec.EverySeconds != 0 || calendar {
			return invalid("unsupported_spec")
		}
		t, e := api.ParseTime(spec.At)
		if e != nil || t.Nanosecond() != 0 {
			return invalid("invalid_calendar_value")
		}
	case "interval":
		if spec.At != "" || spec.AnchorAt == "" || spec.EverySeconds == 0 || spec.EverySeconds > api.MaxSafeInteger || calendar {
			return invalid("unsupported_spec")
		}
		t, e := api.ParseTime(spec.AnchorAt)
		if e != nil || t.Nanosecond() != 0 {
			return invalid("invalid_calendar_value")
		}
	case "daily", "weekly", "monthly":
		if spec.At != "" || spec.AnchorAt != "" || spec.EverySeconds != 0 || !clockTime.MatchString(spec.LocalTime) {
			return invalid("invalid_calendar_value")
		}
		switch spec.Type {
		case "daily":
			if len(spec.Weekdays) > 0 || len(spec.Monthdays) > 0 {
				return invalid("unsupported_spec")
			}
		case "weekly":
			if !orderedDays(spec.Weekdays, 7) || len(spec.Monthdays) > 0 {
				return invalid("invalid_calendar_value")
			}
		case "monthly":
			if !orderedDays(spec.Monthdays, 31) || len(spec.Weekdays) > 0 {
				return invalid("invalid_calendar_value")
			}
		}
	default:
		return invalid("unsupported_spec")
	}
	return nil
}
func (s *Service) NextDue(spec ScheduleSpec, zone, version string, after time.Time) (*time.Time, error) {
	if err := validateSpec(spec); err != nil {
		return nil, err
	}
	if s.ports.Calendar == nil {
		return nil, api.E("dependency_unavailable", "tzdb_unavailable")
	}
	loc, err := s.ports.Calendar.Load(zone, version)
	if err != nil {
		return nil, err
	}
	return nextPlanned(spec, loc, after)
}

var lastStorageTime = time.Date(2261, 12, 31, 23, 59, 59, 0, time.UTC)

func nextPlanned(spec ScheduleSpec, loc *time.Location, after time.Time) (*time.Time, error) {
	switch spec.Type {
	case "once_at":
		at, _ := api.ParseTime(spec.At)
		if !at.After(after) || at.After(lastStorageTime) {
			return nil, nil
		}
		return &at, nil
	case "interval":
		anchor, _ := api.ParseTime(spec.AnchorAt)
		if anchor.After(after) {
			if anchor.After(lastStorageTime) {
				return nil, nil
			}
			return &anchor, nil
		}
		step := new(big.Int).SetUint64(spec.EverySeconds)
		delta := new(big.Int).Sub(big.NewInt(after.Unix()), big.NewInt(anchor.Unix()))
		n := new(big.Int).Add(new(big.Int).Quo(delta, step), big.NewInt(1))
		sec := new(big.Int).Add(big.NewInt(anchor.Unix()), new(big.Int).Mul(n, step))
		if !sec.IsInt64() {
			return nil, nil
		}
		at := time.Unix(sec.Int64(), 0).UTC()
		if at.After(lastStorageTime) {
			return nil, nil
		}
		return &at, nil
	}
	h, _ := strconv.Atoi(spec.LocalTime[:2])
	m, _ := strconv.Atoi(spec.LocalTime[3:5])
	second, _ := strconv.Atoi(spec.LocalTime[6:])
	year, month, day := after.In(loc).Date()
	start := time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
	for n := 0; n < 370; n++ {
		date := start.AddDate(0, 0, n)
		if date.Year() > 2261 {
			return nil, nil
		}
		match := true
		if spec.Type == "weekly" {
			iso := uint64(date.Weekday())
			if iso == 0 {
				iso = 7
			}
			match = containsDay(spec.Weekdays, iso)
		}
		if spec.Type == "monthly" {
			match = containsDay(spec.Monthdays, uint64(date.Day()))
		}
		if !match {
			continue
		}
		wall := time.Date(date.Year(), date.Month(), date.Day(), h, m, second, 0, time.UTC)
		at, ok := resolveWall(wall, loc)
		if ok && at.After(after) && !at.After(lastStorageTime) {
			return &at, nil
		}
	}
	return nil, api.E("dependency_unavailable", "calendar_search_budget")
}
func containsDay(days []uint64, d uint64) bool {
	for _, v := range days {
		if v == d {
			return true
		}
	}
	return false
}

// 枚举 ZoneBounds 的准确偏移，再核当地字段。空缺无候选；重复只取较早 UTC。
func resolveWall(wall time.Time, loc *time.Location) (time.Time, bool) {
	offsets := map[int]bool{}
	end := wall.Add(48 * time.Hour)
	cursor := wall.Add(-48 * time.Hour)
	for n := 0; n < 128 && cursor.Before(end); n++ {
		local := cursor.In(loc)
		_, off := local.Zone()
		offsets[off] = true
		_, bound := local.ZoneBounds()
		if bound.IsZero() || !bound.After(cursor) {
			break
		}
		cursor = bound
	}
	var chosen time.Time
	for offset := range offsets {
		candidate := wall.Add(-time.Duration(offset) * time.Second)
		local := candidate.In(loc)
		if local.Year() == wall.Year() && local.Month() == wall.Month() && local.Day() == wall.Day() && local.Hour() == wall.Hour() && local.Minute() == wall.Minute() && local.Second() == wall.Second() {
			if chosen.IsZero() || candidate.Before(chosen) {
				chosen = candidate
			}
		}
	}
	return chosen, !chosen.IsZero()
}
