//go:build fault

package egressio

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"
	"sync/atomic"
)

// NativeFileEvent 仅在故障构建导出，内容来自生产原生操作的完成点。
type NativeFileEvent = nativeFileEvent

// NativeFileFault 限定当前调用链，错误只注入原生原语，不替换业务成功结果。
type NativeFileFault struct {
	FailBefore  string
	Error       error
	CrashAfter  string
	OmitBarrier string
	MaxWrite    int
	Boundary    func(string)
	Recorder    *NativeFileRecorder
	fired       atomic.Bool
}
type nativeFileFaultKey struct{}

func WithNativeFileFault(ctx context.Context, p *NativeFileFault) context.Context {
	return context.WithValue(ctx, nativeFileFaultKey{}, p)
}

// NativeFileRecorder 的写入失败使生产者立即失败；不允许丢轨迹后继续验收。
type NativeFileRecorder struct {
	mu       sync.Mutex
	writer   io.Writer
	sequence int
	marker   func(int)
	events   []NativeFileEvent
}

func NewNativeFileRecorder(w io.Writer, marker func(int)) *NativeFileRecorder {
	return &NativeFileRecorder{writer: w, marker: marker}
}
func (r *NativeFileRecorder) Sequence() int { r.mu.Lock(); defer r.mu.Unlock(); return r.sequence }
func (r *NativeFileRecorder) Events() []NativeFileEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]NativeFileEvent(nil), r.events...)
}
func (r *NativeFileRecorder) record(e nativeFileEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sequence++
	e.Sequence = r.sequence
	if r.writer != nil {
		if err := json.NewEncoder(r.writer).Encode(e); err != nil {
			_, _ = fmt.Fprintln(os.Stderr, "native file trace failure:", err)
			os.Exit(88)
		}
	}
	r.events = append(r.events, e)
	if r.marker != nil {
		r.marker(e.Sequence)
	}
}
func nativeFileBefore(ctx context.Context, stage string) error {
	p, _ := ctx.Value(nativeFileFaultKey{}).(*NativeFileFault)
	if p == nil {
		return nil
	}
	if p.Boundary != nil {
		p.Boundary(stage)
	}
	if p.FailBefore == stage && p.fired.CompareAndSwap(false, true) {
		if p.Recorder != nil {
			p.Recorder.record(nativeFileEvent{Kind: "error", Stage: stage, Error: fmt.Sprint(p.Error)})
		}
		return p.Error
	}
	return nil
}
func nativeFileObserve(ctx context.Context, e nativeFileEvent) {
	p, _ := ctx.Value(nativeFileFaultKey{}).(*NativeFileFault)
	if p == nil {
		return
	}
	if p.Recorder != nil {
		p.Recorder.record(e)
	}
	if p.CrashAfter == e.Stage && p.fired.CompareAndSwap(false, true) {
		os.Exit(86)
	}
}
func nativeFileWriteLimit(ctx context.Context, _ string, n int) int {
	p, _ := ctx.Value(nativeFileFaultKey{}).(*NativeFileFault)
	if p != nil && p.MaxWrite > 0 {
		return min(n, p.MaxWrite)
	}
	return n
}
func nativeFileOmitSync(ctx context.Context, stage string) bool {
	p, _ := ctx.Value(nativeFileFaultKey{}).(*NativeFileFault)
	return p != nil && p.OmitBarrier == stage
}
