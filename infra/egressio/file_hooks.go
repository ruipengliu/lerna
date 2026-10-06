//go:build !fault

package egressio

import "context"

func nativeFileBefore(context.Context, string) error              { return nil }
func nativeFileObserve(context.Context, nativeFileEvent)          {}
func nativeFileWriteLimit(_ context.Context, _ string, n int) int { return n }
func nativeFileOmitSync(context.Context, string) bool             { return false }
