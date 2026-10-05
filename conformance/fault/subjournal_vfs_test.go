//go:build fault

package fault_test

import (
	"encoding/binary"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/ruipengliu/lerna/cmd/assembly"
	"github.com/ruipengliu/lerna/conformance/fault/storagevfs"
)

// 规则：G3、V4
func TestSubjournalVFSDelegatesLiveSavepointIO(t *testing.T) {
	for _, mode := range []string{"accepted", "rollback", "write-error"} {
		t.Run(mode, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "subjournal.db")
			child := exec.Command(os.Args[0], "-test.run=^TestSubjournalVFSChild$")
			child.Env = append(os.Environ(), "LERNA_SUBJOURNAL_DB="+path, "LERNA_SUBJOURNAL_MODE="+mode)
			if out, err := child.CombinedOutput(); err != nil {
				t.Fatalf("child %v %s", err, out)
			}
			trace, err := os.ReadFile(path + ".trace")
			if err != nil {
				t.Fatal(err)
			}
			baseBytes, err := os.ReadFile(path + ".base-offset")
			if err != nil {
				t.Fatal(err)
			}
			base, err := strconv.Atoi(string(baseBytes))
			if err != nil || base > len(trace) {
				t.Fatal("invalid trace base")
			}
			trace = trace[base:]
			for len(trace) > 0 {
				if len(trace) < 24 {
					t.Fatal("incomplete trace")
				}
				n := int(binary.LittleEndian.Uint32(trace[16:20]))
				if n > len(trace)-24 {
					t.Fatal("incomplete payload")
				}
				if trace[1] == 2 {
					t.Fatal("anonymous temporary journal entered durable file2 trace")
				}
				trace = trace[24+n:]
			}
		})
	}
}

// 规则：G3、V4
func TestSubjournalVFSChild(t *testing.T) {
	path := os.Getenv("LERNA_SUBJOURNAL_DB")
	if path == "" {
		t.Skip("child only")
	}
	if err := storagevfs.Register(path + ".trace"); err != nil {
		t.Fatal(err)
	}
	h, err := assembly.Open(path, "u", "d")
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	sql := func(s string) {
		t.Helper()
		if err := h.StorageFaultSQL(s); err != nil {
			t.Fatal(err)
		}
	}
	// 固定页数触发保存点回滚日志溢出，不依赖任务、来源事件或其他业务写入量。
	sql("CREATE TABLE spill(id INTEGER PRIMARY KEY, marker INTEGER, payload BLOB); WITH RECURSIVE n(x) AS (VALUES(1) UNION ALL SELECT x+1 FROM n WHERE x<64) INSERT INTO spill SELECT x,0,zeroblob(8192) FROM n; CREATE TABLE guard(ok INTEGER CHECK(ok=1));")
	info, err := os.Stat(path + ".trace")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path+".base-offset", []byte(strconv.FormatInt(info.Size(), 10)), 0600); err != nil {
		t.Fatal(err)
	}
	o0, r0, w0, c0, f0 := storagevfs.EphemeralStats()
	sql("BEGIN; UPDATE spill SET marker=1; SAVEPOINT inner_save;")
	mode := os.Getenv("LERNA_SUBJOURNAL_MODE")
	storagevfs.FailEphemeralWrites(mode == "write-error")
	err = h.StorageFaultSQL("UPDATE spill SET marker=2")
	storagevfs.FailEphemeralWrites(false)
	want := "2"
	if mode == "write-error" {
		if err == nil {
			t.Fatal("temporary write error hidden")
		}
		// SQLite 可在 I/O 错误时已自动回滚；随后持久值断言验证原内容。
		_ = h.StorageFaultSQL("ROLLBACK")
		want = "0"
	} else {
		if err != nil {
			t.Fatal(err)
		}
		if mode == "rollback" {
			sql("ROLLBACK TO inner_save")
			want = "1"
		}
		sql("RELEASE inner_save; COMMIT")
	}
	sql("INSERT INTO guard SELECT CASE WHEN count(*)=64 AND min(marker)=" + want + " AND max(marker)=" + want + " THEN 1 ELSE 0 END FROM spill")
	if err = h.Close(); err != nil {
		t.Fatal(err)
	}
	o, r, w, c, f := storagevfs.EphemeralStats()
	if o <= o0 || w <= w0 || c-c0 != o-o0 {
		t.Fatalf("no real spill/close: %d %d %d", o-o0, w-w0, c-c0)
	}
	if mode == "rollback" && r <= r0 {
		t.Fatal("rollback did not read original undo")
	}
	if mode == "write-error" && f <= f0 {
		t.Fatal("write failure injection not reached")
	}
	if mode != "write-error" && f != f0 {
		t.Fatal("unexpected temporary write failure")
	}
}
