// Package wasi 实现经过本机探针的受限 WASI 进程；它不拥有业务权限或命名空间 head。
package wasi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"runtime/debug"
	"time"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/execution"
	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
	"github.com/tetratelabs/wazero/sys"
)

const RuntimeVersion = "wazero/v1.10.1/interpreter"
const WorkerProtocol = "harness-restricted-wasi/1"
const MaxModuleBytes = 512 << 10

type WorkerRequest struct {
	Protocol    string                     `json:"protocol"`
	Module      []byte                     `json:"module"`
	Input       execution.PassiveNamespace `json:"input"`
	MemoryPages uint32                     `json:"memory_pages"`
	OutputBytes uint64                     `json:"output_bytes"`
	WallMillis  uint64                     `json:"wall_millis"`
}

type WorkerResult struct {
	Protocol string `json:"protocol"`
	Success  bool   `json:"success"`
	Reason   string `json:"reason"`
	Output   []byte `json:"output"`
}

type WorkerProbe struct {
	Protocol       string `json:"protocol"`
	RuntimeVersion string `json:"runtime_version"`
	ModuleVersion  string `json:"module_version"`
	ModuleSum      string `json:"module_sum"`
	GoVersion      string `json:"go_version"`
	Answer         uint64 `json:"answer"`
}

// ProbeWorker 只执行内置固定探针，不接收用户代码或业务输入。
func ProbeWorker(out io.Writer) error {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return api.E("unsupported", "worker_build_identity_missing")
	}
	probe := WorkerProbe{Protocol: WorkerProtocol, RuntimeVersion: RuntimeVersion, GoVersion: info.GoVersion}
	for _, dep := range info.Deps {
		if dep.Path == "github.com/tetratelabs/wazero" && dep.Replace == nil {
			probe.ModuleVersion, probe.ModuleSum = dep.Version, dep.Sum
		}
	}
	if probe.ModuleVersion != "v1.10.1" || probe.ModuleSum != "h1:2DugeJf6VVk58KTPszlNfeeN8AhhpwcZqkJj2wwFuH8=" {
		return api.E("unsupported", "worker_runtime_lock_changed")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	runtime := wazero.NewRuntimeWithConfig(ctx, wazero.NewRuntimeConfigInterpreter().WithMemoryLimitPages(1).WithCloseOnContextDone(true))
	defer runtime.Close(context.Background())
	module, err := runtime.InstantiateWithConfig(ctx, []byte{0, 97, 115, 109, 1, 0, 0, 0, 1, 5, 1, 96, 0, 1, 127, 3, 2, 1, 0, 7, 9, 1, 5, 112, 114, 111, 98, 101, 0, 0, 10, 6, 1, 4, 0, 65, 42, 11}, wazero.NewModuleConfig().WithStartFunctions())
	if err != nil {
		return err
	}
	values, err := module.ExportedFunction("probe").Call(ctx)
	if err != nil || len(values) != 1 || values[0] != 42 {
		return api.E("unsupported", "worker_probe_failed")
	}
	probe.Answer = values[0]
	return json.NewEncoder(out).Encode(probe)
}

func RunWorker(in io.Reader, out io.Writer) error {
	raw, err := io.ReadAll(io.LimitReader(in, (1<<20)+1))
	if err != nil {
		return err
	}
	if _, err = api.ParseJSONLimit(raw, 1<<20); err != nil {
		return err
	}
	var request WorkerRequest
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&request); err != nil {
		return api.E("invalid_request", "invalid_worker_request")
	}
	if request.Protocol != WorkerProtocol || len(request.Module) == 0 || len(request.Module) > MaxModuleBytes || request.MemoryPages < 1 || request.MemoryPages > 256 || request.OutputBytes < 1 || request.OutputBytes > 65536 || request.WallMillis < 1 || request.WallMillis > 10000 {
		return api.E("invalid_request", "invalid_worker_limits")
	}
	if err = execution.ValidateNamespace(request.Input); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(request.WallMillis)*time.Millisecond)
	defer cancel()
	runtime := wazero.NewRuntimeWithConfig(ctx, wazero.NewRuntimeConfigInterpreter().WithMemoryLimitPages(request.MemoryPages).WithMemoryCapacityFromMax(false).WithCloseOnContextDone(true))
	defer runtime.Close(context.Background())
	if _, err = wasi_snapshot_preview1.Instantiate(ctx, runtime); err != nil {
		return err
	}
	stdout := limitedBuffer{limit: int(request.OutputBytes)}
	stderr := limitedBuffer{limit: 4096}
	// 默认没有 FS、env、网络、凭据、宿主函数、真实时钟或进程能力。
	// fd0 仅是准确被动数据，fd1/2 的写入是有界 provisional 输出。
	config := wazero.NewModuleConfig().WithName("cell").WithStdin(bytes.NewReader(api.Raw(request.Input))).WithStdout(&stdout).WithStderr(&stderr)
	_, runErr := runtime.InstantiateWithConfig(ctx, request.Module, config)
	var exit *sys.ExitError
	if errors.As(runErr, &exit) && exit.ExitCode() == 0 {
		runErr = nil
	}
	result := WorkerResult{Protocol: WorkerProtocol, Output: []byte{}}
	switch {
	case ctx.Err() != nil:
		result.Reason = "cell_wall_limit"
	case stdout.exceeded || stderr.exceeded:
		result.Reason = "cell_output_limit"
	case runErr != nil:
		result.Reason = "cell_runtime_failure"
	default:
		var namespace execution.PassiveNamespace
		if err = api.Decode(stdout.Bytes(), &namespace); err != nil || execution.ValidateNamespace(namespace) != nil {
			result.Reason = "cell_namespace_invalid"
		} else {
			result.Success = true
			result.Output = append([]byte{}, stdout.Bytes()...)
		}
	}
	return json.NewEncoder(out).Encode(result)
}

type limitedBuffer struct {
	bytes.Buffer
	limit    int
	exceeded bool
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if len(p) > b.limit-b.Len() {
		b.exceeded = true
		return 0, io.ErrShortWrite
	}
	return b.Buffer.Write(p)
}
