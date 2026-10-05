//go:build !fault

package fault

// Enabled 报告当前构建是否包含故障注入钩子。
const Enabled = false

// Hit 在生产构建中什么也不做。
func Hit(string) error { return nil }
