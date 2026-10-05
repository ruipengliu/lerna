//go:build fault

package sqlite

import (
	"context"
	"errors"
)

type unavailableContentKey struct{}

// WithContentReadUnavailable 在实际正文读取边界注入依赖不可用，不伪造内容治理决定。
func WithContentReadUnavailable(ctx context.Context) context.Context {
	return context.WithValue(ctx, unavailableContentKey{}, true)
}
func contentReadBoundary(ctx context.Context) error {
	if ctx.Value(unavailableContentKey{}) == true {
		return storageError(errors.New("injected content read unavailable"), false)
	}
	return nil
}
