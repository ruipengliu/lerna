package development

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func configuredWASITestEvidenceRoot() (string, error) {
	root := os.Getenv("HARNESS_TEST_WASI_EVIDENCE_ROOT")
	if root != "" && !filepath.IsAbs(root) {
		return "", errors.New("HARNESS_TEST_WASI_EVIDENCE_ROOT must name an existing absolute directory")
	}
	return root, nil
}

// 默认沿标准测试临时目录清理；只有显式证据目录才保留失败原现场。
// 不创建用户指定的base，也不读取或初始化任何旧Scope。
func configuredWASITestDataRoot(t *testing.T) string {
	t.Helper()
	base, err := configuredWASITestEvidenceRoot()
	if err != nil {
		t.Fatal(err)
	}
	if base == "" {
		return t.TempDir()
	}
	root, err := os.MkdirTemp(base, "wasi15-private-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("original private fixture retained for terminal/fee observation: %s; config=%s; original local contract HTTP has stopped, do not issue new requests", root, filepath.Join(root, "config.json"))
			return
		}
		if err := os.RemoveAll(root); err != nil {
			t.Errorf("remove successful private WASI fixture: %v", err)
		}
	})
	return root
}
