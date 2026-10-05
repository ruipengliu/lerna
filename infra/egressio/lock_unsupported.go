//go:build !darwin && !linux

package egressio

import (
	"context"
	"fmt"
)

type FileLock struct{}

func NewFileLock(string) (*FileLock, error) {
	return nil, fmt.Errorf("egress locking unsupported on this platform")
}
func (*FileLock) Enter(context.Context) (func(), error) {
	return nil, fmt.Errorf("egress locking unsupported on this platform")
}
