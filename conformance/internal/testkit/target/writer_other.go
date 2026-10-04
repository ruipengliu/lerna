//go:build !linux

package target

import (
	"context"
	"errors"
)

var ErrWriterActive = errors.New("test target writer already active")

func acquireWriter(context.Context, string) (func() error, error) {
	return nil, errors.New("durable test target requires Linux file locking")
}
