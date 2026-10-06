//go:build fault

package fault_test

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/ruipengliu/lerna/cmd/assembly"
	"github.com/ruipengliu/lerna/conformance/fault/storagevfs"
	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"github.com/ruipengliu/lerna/core/ledger"
	"google.golang.org/protobuf/proto"
)

type spillResult struct {
	Opens, Reads, Writes, Closes, Failures uint64
	Receipt                                *v1.CommandReceipt
	SourceBefore, SourceAfter              int
	BaseCut, AckCut                        int
	Failed                                 bool
}

// 规则：G1、G3、G4、R7、开始-5
func TestStartSavepointSpillPreservesAtomicResponsibilities(t *testing.T) {
	for _, mode := range []string{"accepted", "rejected", "write-error"} {
		t.Run(mode, func(t *testing.T) {
			var calls atomic.Int64
			target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { calls.Add(1); w.WriteHeader(200) }))
			defer target.Close()
			path := filepath.Join(t.TempDir(), "spill.db")
			h, err := assembly.Open(path, "u", "d")
			if err != nil {
				t.Fatal(err)
			}
			a, c := prepareStartFault(t, h, target.URL)
			writeStart(t, path, c)
			if err = h.Close(); err != nil {
				t.Fatal(err)
			}
			child := exec.Command(os.Args[0], "-test.run=^TestStartSavepointSpillChild$")
			child.Env = append(os.Environ(), "LERNA_SPILL_DB="+path, "LERNA_SPILL_MODE="+mode)
			out, err := child.CombinedOutput()
			if err != nil {
				t.Fatalf("spill child %v %s", err, out)
			}
			b, err := os.ReadFile(path + ".result")
			if err != nil {
				t.Fatal(err)
			}
			var result spillResult
			if err = json.Unmarshal(b, &result); err != nil {
				t.Fatal(err)
			}
			if result.Opens == 0 || result.Writes == 0 || result.Closes != result.Opens {
				t.Fatalf("savepoint did not spill/close: %+v", result)
			}
			if mode == "rejected" && result.Reads == 0 {
				t.Fatalf("rollback did not read real undo: %+v", result)
			}
			if mode == "write-error" && (result.Failures == 0 || !result.Failed || result.Receipt != nil) {
				t.Fatalf("temporary write failure hidden: %+v", result)
			}
			if mode != "write-error" && (result.Failures != 0 || result.Failed) {
				t.Fatalf("unexpected temp failure: %+v", result)
			}
			trace, err := os.ReadFile(path + ".trace")
			if err != nil {
				t.Fatal(err)
			}
			for len(trace) > 0 {
				if len(trace) < 24 {
					t.Fatal("incomplete durable trace")
				}
				n := int(binary.LittleEndian.Uint32(trace[16:20]))
				if n > len(trace)-24 {
					t.Fatal("incomplete durable payload")
				}
				// 此路径只使用 WAL，匿名子日志不能伪装成主回滚日志的文件 2。
				if trace[1] == 2 {
					t.Fatalf("ephemeral I/O leaked into durable journal: kind=%c", trace[0])
				}
				trace = trace[24+n:]
			}
			h, err = assembly.Open(path, "u", "d")
			if err != nil {
				t.Fatal(err)
			}
			defer h.Close()
			ctx := context.Background()
			actor := &v1.Caller{UserId: "u", IssuerId: "egress"}
			q, err := h.Durable.QueryReceipt(ctx, actor, c.Header.Identity)
			if err != nil {
				t.Fatal(err)
			}
			use, err := h.Grants.QueryCredentialUse(ctx, actor, c.CredentialRef)
			if err != nil {
				t.Fatal(err)
			}
			reservations, err := h.Budget.QueryReservations(ctx, actor, a.TaskId)
			if err != nil {
				t.Fatal(err)
			}
			source, err := h.Trace.QuerySources(ctx, actor)
			if err != nil {
				t.Fatal(err)
			}
			if len(source) != result.SourceAfter || calls.Load() != 0 {
				t.Fatalf("lost source responsibility or I/O escaped: sources=%d %+v calls=%d", len(source), result, calls.Load())
			}
			if mode == "accepted" {
				if !proto.Equal(q.Receipt, result.Receipt) || q.Receipt.GetDecision() != v1.Decision_DECISION_ACCEPTED || use == nil || reservations[0].ConsumedSends != 1 || result.SourceAfter <= result.SourceBefore {
					t.Fatalf("P4 not atomic: %v %v %+v", q, use, result)
				}
				repeated, e := h.Tasks.StartExecution(ctx, actor, c)
				if e != nil || !proto.Equal(repeated, result.Receipt) {
					t.Fatalf("original replay %v %v", repeated, e)
				}
			} else {
				if use != nil || reservations[0].ConsumedSends != 0 || result.SourceBefore != result.SourceAfter {
					t.Fatalf("partial P4 rollback: %v %v %+v", use, reservations, result)
				}
				if mode == "rejected" {
					if !proto.Equal(q.Receipt, result.Receipt) || q.Receipt.GetError().GetCode() != "TEST_LATE_START_REJECTED" {
						t.Fatalf("rejection decision missing: %v", q)
					}
				} else if q.State != v1.ReceiptQueryState_RECEIPT_QUERY_STATE_NOT_FOUND {
					t.Fatalf("I/O failure fixed business decision: %v", q)
				}
			}
		})
	}
}

type lateStartRejection struct{ *ledger.Service }

func (l lateStartRejection) CheckRecoveryAllowed(ctx context.Context) error {
	if err := l.Service.CheckRecoveryAllowed(ctx); err != nil {
		return err
	}
	return command.Fail("TEST_LATE_START_REJECTED")
}

// 规则：G3、G4、R7、开始-5
func TestStartSavepointSpillChild(t *testing.T) {
	path := os.Getenv("LERNA_SPILL_DB")
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
	ctx := context.Background()
	actor := &v1.Caller{UserId: "u", IssuerId: "egress"}
	before, err := h.Trace.QuerySources(ctx, actor)
	if err != nil {
		t.Fatal(err)
	}
	mode := os.Getenv("LERNA_SPILL_MODE")
	if mode == "rejected" {
		h.Tasks.WithStart(h.Grants, h.Budget, lateStartRejection{h.Ledger})
	}
	storagevfs.FailEphemeralWrites(mode == "write-error")
	baseCut := 0
	if mode == "storage" {
		// 正常提交后完整检查点并截断 WAL；基线仅含本次 P4 之前的已确认事实。
		if err = h.StorageFaultSQL("PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
			t.Fatal(err)
		}
		base, e := os.ReadFile(path)
		if e != nil {
			t.Fatal(e)
		}
		if e = os.WriteFile(path+".base", base, 0600); e != nil {
			t.Fatal(e)
		}
		baseCut = storagevfs.Sequence()
	}
	opens, reads, writes, closes, failures := storagevfs.EphemeralStats()
	receipt, startErr := h.Tasks.StartExecution(ctx, actor, readStart(t, path))
	ackCut := storagevfs.Sequence()
	storagevfs.FailEphemeralWrites(false)
	after, err := h.Trace.QuerySources(ctx, actor)
	if err != nil {
		t.Fatal(err)
	}
	if mode != "storage" {
		if err = h.Close(); err != nil {
			t.Fatal(err)
		}
	}
	o, r, w, c, f := storagevfs.EphemeralStats()
	result := spillResult{Opens: o - opens, Reads: r - reads, Writes: w - writes, Closes: c - closes, Failures: f - failures, Receipt: receipt, SourceBefore: len(before), SourceAfter: len(after), Failed: startErr != nil, BaseCut: baseCut, AckCut: ackCut}
	b, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path+".result", b, 0600); err != nil {
		t.Fatal(err)
	}
	if mode == "storage" {
		os.Exit(0)
	}
}
