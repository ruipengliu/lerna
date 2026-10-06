//go:build !darwin

package sqlite

// Ensure 其他平台没有 macOS 的 fsync 回退路径；仍不赋予平台准入。
func ensureBarrier() error { return nil }

func qualificationVFSQuery() string { return "" }

// Platform 未经验证的平台不开放掉电保证。
func observedPlatform(string) string { return "unqualified" }
