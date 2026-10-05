package conformance_test

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func symbols(t *testing.T, tags string) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "lernad")
	args := []string{"build", "-o", bin}
	if tags != "" {
		args = append(args, "-tags", tags)
	}
	args = append(args, "github.com/ruipengliu/lerna/cmd/lernad")
	if out, err := exec.Command("go", args...).CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	out, err := exec.Command("go", "tool", "nm", bin).CombinedOutput()
	if err != nil {
		t.Fatalf("go tool nm: %v\n%s", err, out)
	}
	return string(out)
}

// 生产构建不包含故障注入钩子；只有 fault 构建标签才编译进来。
//
// 规则：G3
func TestProductionBuildHasNoFaultHooks(t *testing.T) {
	if testing.Short() {
		t.Skip("builds binaries")
	}
	const marker = "lerna/core/durable/fault.rules"
	if strings.Contains(symbols(t, ""), marker) {
		t.Fatal("production lernad contains fault injection hooks")
	}
	if !strings.Contains(symbols(t, "fault"), marker) {
		t.Fatal("the fault build must contain the hooks; the check above would be vacuous otherwise")
	}
}
