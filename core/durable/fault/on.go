//go:build fault

package fault

import (
	"fmt"
	"sort"
	"sync"
)

// Enabled 报告当前构建是否包含故障注入钩子。
const Enabled = true

// Mode 是注入的故障种类。
type Mode int

const (
	// Crash 模拟进程在该点崩溃：Hit panic，携带 Crash 值。
	Crash Mode = iota + 1
	// Lose 模拟回执丢失：Hit 返回 ErrLost，操作本身已经发生。
	Lose
)

// CrashSignal 是 Crash 模式下 panic 的值；harness 据此识别注入的崩溃。
type CrashSignal struct {
	Point string
}

func (c CrashSignal) String() string { return "fault: injected crash at " + c.Point }

type rule struct {
	mode Mode
	// 第几次经过该点时触发（从 1 开始）。
	nth int
}

var (
	mu    sync.Mutex
	rules = map[string]rule{}
	seen  = map[string]int{}
	order []string
)

// Inject 在第 nth 次经过 point 时注入 mode 故障。
func Inject(point string, mode Mode, nth int) {
	if nth < 1 {
		nth = 1
	}
	mu.Lock()
	defer mu.Unlock()
	rules[point] = rule{mode: mode, nth: nth}
}

// Reset 清除全部规则和计数。
func Reset() {
	mu.Lock()
	defer mu.Unlock()
	rules = map[string]rule{}
	seen = map[string]int{}
	order = nil
}

// Seen 按首次经过的顺序返回经过的点和次数。
func Seen() []string {
	mu.Lock()
	defer mu.Unlock()
	return append([]string(nil), order...)
}

// Count 返回经过 point 的次数。
func Count(point string) int {
	mu.Lock()
	defer mu.Unlock()
	return seen[point]
}

// SortedSeen 返回排序后的点名，用于报告。
func SortedSeen() []string {
	s := Seen()
	sort.Strings(s)
	return s
}

// Hit 记录经过 point；命中规则时注入故障。
func Hit(point string) error {
	mu.Lock()
	if _, ok := seen[point]; !ok {
		order = append(order, point)
	}
	seen[point]++
	n := seen[point]
	r, ok := rules[point]
	if ok && r.nth == n {
		delete(rules, point)
	}
	mu.Unlock()
	if !ok || r.nth != n {
		return nil
	}
	switch r.mode {
	case Crash:
		panic(CrashSignal{Point: point})
	case Lose:
		return ErrLost
	default:
		panic(fmt.Sprintf("fault: unknown mode %d", r.mode))
	}
}
