//go:build fault

package fault_test

import (
	"context"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/ruipengliu/lerna/cmd/assembly"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

type startSpillCapture struct {
	base      []byte
	events    []storageEvent
	result    spillResult
	command   *v1.StartExecutionCommand
	admission *v1.Admission
	calls     *atomic.Int64
}

func captureStartSpill(t *testing.T) startSpillCapture {
	t.Helper()
	calls := new(atomic.Int64)
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { calls.Add(1); w.WriteHeader(200) }))
	t.Cleanup(target.Close)
	path := filepath.Join(t.TempDir(), "p4.db")
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
	child.Env = append(os.Environ(), "LERNA_SPILL_DB="+path, "LERNA_SPILL_MODE=storage")
	out, err := child.CombinedOutput()
	if err != nil {
		t.Fatalf("P4 producer %v %s", err, out)
	}
	b, err := os.ReadFile(path + ".result")
	if err != nil {
		t.Fatal(err)
	}
	var result spillResult
	if err = json.Unmarshal(b, &result); err != nil {
		t.Fatal(err)
	}
	if result.Failed || result.Receipt.GetDecision() != v1.Decision_DECISION_ACCEPTED || result.Opens == 0 || result.Writes == 0 || result.Failures != 0 || calls.Load() != 0 {
		t.Fatalf("invalid P4 ACK oracle: %+v calls=%d", result, calls.Load())
	}
	base, err := os.ReadFile(path + ".base")
	if err != nil {
		t.Fatal(err)
	}
	trace, err := os.ReadFile(path + ".trace")
	if err != nil {
		t.Fatal(err)
	}
	var events []storageEvent
	for len(trace) > 0 {
		if len(trace) < 24 {
			t.Fatal("incomplete trace")
		}
		n := int(binary.LittleEndian.Uint32(trace[16:20]))
		if n > len(trace)-24 {
			t.Fatal("incomplete payload")
		}
		events = append(events, storageEvent{Kind: trace[0], File: int(trace[1]), Offset: int64(binary.LittleEndian.Uint64(trace[8:16])), Data: append([]byte{}, trace[24:24+n]...)})
		trace = trace[24+n:]
	}
	if result.BaseCut <= 0 || result.AckCut <= result.BaseCut || result.AckCut != len(events) {
		t.Fatalf("unexpected transaction scope %+v events=%d", result, len(events))
	}
	events = events[result.BaseCut:]
	for _, event := range events {
		if event.File == 2 {
			t.Fatal("anonymous subjournal became durable file 2")
		}
	}
	t.Logf("P4: %d durable I/O events, %d images at 5 policies; real subjournal opens=%d writes=%d; fully checkpointed pre-P4 baseline", len(events), (len(events)+1)*5, result.Opens, result.Writes)
	return startSpillCapture{base, events, result, c, a, calls}
}

// 规则：G3、R7、开始-5
func TestStartSavepointStorageProducer(t *testing.T) { captureStartSpill(t) }

// 规则：G1、G3、G4、R7、开始-5
func TestStorageStartSavepointPowerLoss(t *testing.T) {
	captured := captureStartSpill(t)
	for cut := 0; cut <= len(captured.events); cut++ {
		for _, policy := range []string{"lost", "all", "reverse", "even", "torn"} {
			t.Run(fmt.Sprintf("cut-%03d/%s", cut, policy), func(t *testing.T) {
				path := storageImage(t, captured.base, captured.events[:cut], policy)
				verifyStartSpillImage(t, path, captured, cut)
			})
		}
	}
}

func verifyStartSpillImage(t *testing.T, path string, captured startSpillCapture, cut int) {
	t.Helper()
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("PRAGMA fullfsync=ON; PRAGMA synchronous=FULL"); err != nil {
		t.Fatal(err)
	}
	var integrity string
	if err = db.QueryRow("PRAGMA integrity_check").Scan(&integrity); err != nil || integrity != "ok" {
		t.Fatalf("P4 image corrupt %s %v", integrity, err)
	}
	rows, err := db.Query("PRAGMA foreign_key_check")
	if err != nil {
		t.Fatal(err)
	}
	if rows.Next() {
		t.Fatal("P4 foreign-key violation")
	}
	rows.Close()
	db.Close()
	h, err := assembly.Open(path, "u", "d")
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	ctx := context.Background()
	actor := &v1.Caller{UserId: "u", IssuerId: "egress"}
	q, err := h.Durable.QueryReceipt(ctx, actor, captured.command.Header.Identity)
	if err != nil {
		t.Fatal(err)
	}
	use, err := h.Grants.QueryCredentialUse(ctx, actor, captured.command.CredentialRef)
	if err != nil {
		t.Fatal(err)
	}
	reservations, err := h.Budget.QueryReservations(ctx, actor, captured.admission.TaskId)
	if err != nil {
		t.Fatal(err)
	}
	sources, err := h.Trace.QuerySources(ctx, actor)
	if err != nil {
		t.Fatal(err)
	}
	decided := q.State == v1.ReceiptQueryState_RECEIPT_QUERY_STATE_DECIDED
	if cut == len(captured.events) && !decided {
		t.Fatal("acknowledged P4 lost")
	}
	if decided {
		if !proto.Equal(q.Receipt, captured.result.Receipt) || use == nil || reservations[0].ConsumedSends != 1 || len(sources) != captured.result.SourceAfter {
			t.Fatalf("partial committed P4 %v %v %v sources=%d", q, use, reservations, len(sources))
		}
	} else if q.State != v1.ReceiptQueryState_RECEIPT_QUERY_STATE_NOT_FOUND || use != nil || reservations[0].ConsumedSends != 0 || len(sources) != captured.result.SourceBefore {
		t.Fatalf("partial absent P4 %v %v %v sources=%d", q, use, reservations, len(sources))
	}
	// 未确认时允许原领取或凭据已过期；原身份重新裁决后仍不能重复消费。
	first, err := h.Tasks.StartExecution(ctx, actor, captured.command)
	if err != nil {
		t.Fatal(err)
	}
	repeated, err := h.Tasks.StartExecution(ctx, actor, captured.command)
	if err != nil || !proto.Equal(first, repeated) {
		t.Fatalf("original P4 replay changed %v %v", repeated, err)
	}
	use, err = h.Grants.QueryCredentialUse(ctx, actor, captured.command.CredentialRef)
	if err != nil {
		t.Fatal(err)
	}
	reservations, err = h.Budget.QueryReservations(ctx, actor, captured.admission.TaskId)
	if err != nil {
		t.Fatal(err)
	}
	want := uint32(0)
	if first.Decision == v1.Decision_DECISION_ACCEPTED {
		want = 1
	}
	if reservations[0].ConsumedSends != want || (use != nil) != (want == 1) || captured.calls.Load() != 0 {
		t.Fatalf("retry consumed twice or escaped P5 %v %v calls=%d", use, reservations, captured.calls.Load())
	}
}
