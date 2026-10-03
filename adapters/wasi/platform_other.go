//go:build !linux

package wasi

import (
	"context"
	"errors"
	"github.com/ruipengliu/lerna/api"
	"os"
	"os/exec"
)

func errorsJoin(a, b error) error { return errors.Join(a, b) }
func kernelRelease() string       { return "unprobed" }
func acquireRuntimeLock(*os.Root) (*os.File, error) {
	return nil, api.E("unsupported", "wasi_platform_not_probed")
}
func configureProcess(*exec.Cmd)            {}
func (r *Runtime) verifyExecutables() error { return api.E("unsupported", "wasi_platform_not_probed") }
func verifyPrivateRoot(string) error        { return api.E("unsupported", "wasi_platform_not_probed") }
func openPrivateFile(*os.Root, string, int) (*os.File, error) {
	return nil, api.E("unsupported", "wasi_platform_not_probed")
}
func processStart(int) (string, error) { return "", api.E("unsupported", "wasi_platform_not_probed") }
func fenceProcess(context.Context, int, string) error {
	return api.E("unsupported", "wasi_platform_not_probed")
}
