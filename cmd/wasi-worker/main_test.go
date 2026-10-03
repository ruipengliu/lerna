package main_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/adapters/wasi"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/execution"
)

func TestRestrictedWorkerActuallyRunsWASIPreview1Module(t *testing.T) {
	worker := filepath.Join(t.TempDir(), "worker")
	build := exec.Command("go", "build", "-trimpath", "-o", worker, ".")
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build actual worker: %s %v", out, err)
	}
	// 独立字面真值由真实 WASM 的 fd_write 输出；没有插入业务结果或替代引擎。
	want := []byte(`{"format":"harness-passive-namespace/1","bindings":[{"name":"answer","kind":"decimal","decimal":"42"}]}`)
	request := wasi.WorkerRequest{Protocol: wasi.WorkerProtocol, Module: writeModule(want), Input: execution.PassiveNamespace{Format: execution.PassiveEnvironmentFormat, Bindings: []execution.NamespaceBinding{}}, MemoryPages: 16, OutputBytes: 65536, WallMillis: 1000}
	raw, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "prlimit", "--as=1073741824:1073741824", "--cpu=2:2", "--", "bwrap", "--unshare-all", "--die-with-parent", "--new-session", "--clearenv", "--ro-bind", worker, "/worker", "--", "/worker")
	cmd.Stdin = bytes.NewReader(raw)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("actual isolated WASI worker must execute: %v bytes=%q", err, out)
	}
	var result wasi.WorkerResult
	if err = api.Decode(out, &result); err != nil || !result.Success || !bytes.Equal(result.Output, want) {
		t.Fatalf("actual WASM did not emit the declared whole namespace: %+v %v", result, err)
	}
}

// 编码固定 Preview1 fd_write 程序，保持测试不依赖 WAT 编译器或运行器内部方法。
func writeModule(text []byte) []byte {
	module := []byte{0, 97, 115, 109, 1, 0, 0, 0}
	section := func(id byte, b []byte) {
		module = append(module, id)
		module = append(module, leb(uint32(len(b)))...)
		module = append(module, b...)
	}
	name := func(s string) []byte { return append(leb(uint32(len(s))), []byte(s)...) }
	section(1, []byte{2, 0x60, 4, 0x7f, 0x7f, 0x7f, 0x7f, 1, 0x7f, 0x60, 0, 0})
	imports := append([]byte{1}, name("wasi_snapshot_preview1")...)
	imports = append(imports, name("fd_write")...)
	section(2, append(imports, 0, 0))
	section(3, []byte{1, 1})
	section(5, []byte{1, 0, 1})
	exports := append([]byte{2}, name("memory")...)
	exports = append(exports, 2, 0)
	exports = append(exports, name("_start")...)
	section(7, append(exports, 0, 1))
	code := []byte{0, 0x41, 0, 0x41, 0x80, 0x08, 0x36, 2, 0, 0x41, 4, 0x41}
	code = append(code, sleb(int32(len(text)))...)
	code = append(code, 0x36, 2, 0, 0x41, 1, 0x41, 0, 0x41, 1, 0x41, 8, 0x10, 0, 0x1a, 0x0b)
	section(10, append(append([]byte{1}, leb(uint32(len(code)))...), code...))
	data := append([]byte{1, 0, 0x41, 0x80, 0x08, 0x0b}, leb(uint32(len(text)))...)
	section(11, append(data, text...))
	return module
}
func leb(n uint32) []byte {
	b := []byte{}
	for {
		x := byte(n & 127)
		n >>= 7
		if n != 0 {
			x |= 128
		}
		b = append(b, x)
		if n == 0 {
			return b
		}
	}
}
func sleb(n int32) []byte {
	b := []byte{}
	for {
		x := byte(n & 127)
		n >>= 7
		last := n == 0 && x&64 == 0 || n == -1 && x&64 != 0
		if !last {
			x |= 128
		}
		b = append(b, x)
		if last {
			return b
		}
	}
}
