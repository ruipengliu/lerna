//go:build !linux

package sqlite

import (
	"context"
	"errors"
)

func acquireWriter(context.Context, string) (func() error, error) {
	return nil, errors.New("SQLite writable Host exclusion currently supports Linux only")
}
