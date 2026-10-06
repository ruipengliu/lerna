//go:build fault

package fault_test

import "testing"

// 规则：G3、G11、V4
func TestQualificationKeepsOriginalStorageVFSChronology(t *testing.T) {
	for _, mode := range []string{"FULL", "BOOTSTRAP"} {
		t.Run(mode, func(t *testing.T) {
			events, acks, base := storageRun(t, mode)
			if len(events) == 0 || len(acks) < 2 || mode == "BOOTSTRAP" && acks[0].Cut != 0 {
				t.Fatalf("original source chronology missing: events=%d ACKs=%v", len(events), acks)
			}
			for _, ack := range acks {
				if ack.Cut > len(events) {
					t.Fatal("ACK is not an original I/O prefix")
				}
			}
			// 只运行最终完整字节图的负漏接检查；所有中间前缀仍由最终重矩阵验收。
			path := storageImage(t, base, events, "all")
			storageVerify(t, path, acks, len(events))
			t.Logf("original %s: events=%d first=%d final=%d prefixes=%d all-five-images=%d (full matrix NOT RUN)", mode, len(events), acks[0].Cut, acks[len(acks)-1].Cut, len(events)-acks[0].Cut+1, (len(events)-acks[0].Cut+1)*5)
		})
	}
}
