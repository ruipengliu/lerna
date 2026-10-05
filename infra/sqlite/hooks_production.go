//go:build !fault

package sqlite

import "context"

// 生产构建仅保留可内联空边界；不编译登记表、故障计划或配置入口。
func persistenceBoundary(context.Context, string, bool) error { return nil }
