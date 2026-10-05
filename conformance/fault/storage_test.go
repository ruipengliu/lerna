//go:build fault

package fault_test

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"

	"github.com/ruipengliu/lerna/cmd/assembly"
	"github.com/ruipengliu/lerna/conformance/fault/storagevfs"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

type storageACK struct {
	DirectoryFailures                               uint64
	Claim                                           bool
	Submitted                                       bool
	NativeAttempts, NativeSuccesses, NativeFailures uint64
	Cut                                             int
	ID                                              string
	Receipt                                         []byte
	Start                                           bool
	Error                                           bool
}
type storageEvent struct {
	Kind   byte
	File   int
	Offset int64
	Data   []byte
}

// 规则：G3、G11
func TestStoragePowerLoss(t *testing.T) { storagePowerLoss(t, "FULL") }

// 规则：G3
func TestStorageBootstrapPowerLoss(t *testing.T) { storagePowerLoss(t, "BOOTSTRAP") }

func storagePowerLoss(t *testing.T, mode string) {
	events, acks, base := storageRun(t, mode)
	start := acks[0].Cut
	if runtime.GOOS == "darwin" {
		last := acks[len(acks)-1]
		if last.NativeSuccesses == 0 || last.NativeFailures != 0 || last.NativeAttempts != last.NativeSuccesses {
			t.Fatalf("native barrier evidence missing: %+v", last)
		}
		t.Logf("F_FULLFSYNC: %d successful calls", last.NativeSuccesses)
	}
	total := 0
	for cut := start; cut <= len(events); cut++ {
		for _, policy := range []string{"lost", "all", "reverse", "even", "torn"} {
			t.Run(fmt.Sprintf("cut-%03d/%s", cut, policy), func(t *testing.T) {
				total++
				path := storageImage(t, base, events[:cut], policy)
				storageVerify(t, path, acks, cut)
			})
		}
	}
	t.Logf("G3: %d crash images, %d I/O events, %d ACKs; loss/all/reverse/even/2048-byte tear", total, len(events), len(acks)-1)
}

// 规则：G3
func TestStorageNegativeControls(t *testing.T) {
	for _, mode := range []string{"NORMAL", "OFF", "omit-sync"} {
		t.Run(mode, func(t *testing.T) {
			events, acks, base := storageRun(t, mode)
			path := storageImage(t, base, events, "lost")
			h, err := assembly.Open(path, "alice", "local")
			if err != nil {
				t.Fatal(err)
			}
			defer h.Close()
			caller, c := goal()
			c.Identity.CommandId = "A"
			q, err := h.Durable.QueryReceipt(context.Background(), caller, c.Identity)
			if err != nil || q.State != v1.ReceiptQueryState_RECEIPT_QUERY_STATE_NOT_FOUND || len(acks) != 4 {
				t.Fatalf("negative control did not expose acknowledged loss: %v %v ACK=%d", q, err, len(acks))
			}
		})
	}
}

// 规则：G3
func TestStorageSyncErrorRejectsAcknowledgement(t *testing.T) {
	for _, mode := range []string{"sync-error", "native-sync-error"} {
		if mode == "native-sync-error" && runtime.GOOS != "darwin" {
			continue
		}
		for _, skip := range []string{"0", "1"} {
			t.Setenv("LERNA_STORAGE_SKIP", skip)
			_, acks, _ := storageRun(t, mode)
			if len(acks) != 2 || !acks[1].Error || len(acks[1].Receipt) != 0 {
				t.Fatalf("sync error acknowledged responsibility: %+v", acks)
			}
			if mode == "native-sync-error" && acks[1].NativeFailures == 0 {
				t.Fatal("F_FULLFSYNC failure not exercised")
			}
		}
	}
}

func storageRun(t *testing.T, mode string) ([]storageEvent, []storageACK, []byte) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "live.db")
	var base []byte
	if mode != "BOOTSTRAP" && mode != "directory-sync-error" {
		h, err := assembly.Open(path, "alice", "local")
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("SQLite build: %+v", h.StorageSettings())
		if err := h.Close(); err != nil {
			t.Fatal(err)
		}
		base, err = os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
	}
	child := exec.Command(os.Args[0], "-test.run=^TestStorageChild$")
	child.Env = append(os.Environ(), "LERNA_STORAGE_DB="+path, "LERNA_STORAGE_MODE="+mode)
	out, err := child.CombinedOutput()
	if err != nil {
		t.Fatalf("child: %v %s", err, out)
	}
	var acks []storageACK
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		var a storageACK
		if err := json.Unmarshal(scanner.Bytes(), &a); err != nil {
			t.Fatalf("invalid ACK: %s", scanner.Bytes())
		}
		acks = append(acks, a)
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if len(acks) == 0 || !acks[0].Start {
		t.Fatalf("missing independent ACK oracle: %s", out)
	}
	trace, err := os.ReadFile(path + ".trace")
	if err != nil {
		t.Fatal(err)
	}
	var events []storageEvent
	for len(trace) > 0 {
		if len(trace) < 24 {
			t.Fatal("incomplete trace header")
		}
		n := int(binary.LittleEndian.Uint32(trace[16:20]))
		if n > len(trace)-24 {
			t.Fatal("incomplete trace payload")
		}
		events = append(events, storageEvent{trace[0], int(trace[1]), int64(binary.LittleEndian.Uint64(trace[8:16])), bytes.Clone(trace[24 : 24+n])})
		trace = trace[24+n:]
	}
	return events, acks, base
}

// 规则：G3
func TestStorageChild(t *testing.T) {
	path := os.Getenv("LERNA_STORAGE_DB")
	if path == "" {
		t.Skip("subprocess only")
	}
	if err := storagevfs.Register(path + ".trace"); err != nil {
		t.Fatal(err)
	}
	if os.Getenv("LERNA_STORAGE_MODE") == "directory-sync-error" {
		storagevfs.Mode(4)
	}
	h, err := assembly.Open(path, "alice", "local")
	if os.Getenv("LERNA_STORAGE_MODE") == "directory-sync-error" {
		if err == nil {
			t.Fatal("directory sync error opened write store")
		}
		if err := json.NewEncoder(os.Stdout).Encode(storageACK{Start: true, Error: true, DirectoryFailures: storagevfs.DirectoryFailures()}); err != nil {
			t.Fatal(err)
		}
		os.Exit(0)
	}
	if err != nil {
		t.Fatal(err)
	}
	if err := h.StorageFaultSQL("PRAGMA wal_autocheckpoint=0"); err != nil {
		t.Fatal(err)
	}
	emit := func(a storageACK) {
		a.Cut = storagevfs.Sequence()
		if a.Start && os.Getenv("LERNA_STORAGE_MODE") == "BOOTSTRAP" {
			a.Cut = 0
		}
		a.NativeAttempts, a.NativeSuccesses, a.NativeFailures = storagevfs.NativeBarriers()
		if err := json.NewEncoder(os.Stdout).Encode(a); err != nil {
			t.Fatal(err)
		}
	}
	if h.StorageSettings().SQLiteSourceID != storagevfs.SourceID() {
		t.Fatal("wrapper and production SQLite differ")
	}
	emit(storageACK{Start: true})
	mode := os.Getenv("LERNA_STORAGE_MODE")
	if mode == "BOOTSTRAP" {
		mode = "FULL"
	}
	skip, _ := strconv.Atoi(os.Getenv("LERNA_STORAGE_SKIP"))
	switch mode {
	case "NORMAL", "OFF":
		if err := h.StorageFaultSQL("PRAGMA synchronous=" + mode); err != nil {
			t.Fatal(err)
		}
	case "omit-sync":
		storagevfs.Mode(1)
	case "sync-error":
		storagevfs.ModeAfter(2, skip)
	case "native-sync-error":
		storagevfs.ModeAfter(3, skip)
	}
	for _, id := range []string{"A", "B", "C"} {
		caller, c := goal()
		c.Identity.CommandId = id
		r, err := h.Sessions.SubmitGoal(context.Background(), caller, c)
		if mode == "sync-error" || mode == "native-sync-error" {
			if err == nil || r != nil {
				t.Fatal("sync error leaked receipt")
			}
			emit(storageACK{ID: id, Error: true})
			os.Exit(0)
		}
		if err != nil {
			t.Fatal(err)
		}
		submitted, err := proto.Marshal(r)
		if err != nil {
			t.Fatal(err)
		}
		emit(storageACK{ID: id, Receipt: submitted, Submitted: true})
		claim, err := h.Durable.ExecuteJob(context.Background(), caller, claimCommand("claim-"+id, "storage-worker", 100))
		if err != nil || len(claim.Jobs) != 1 {
			t.Fatalf("claim: %v %v", claim, err)
		}
		claimBytes, err := proto.Marshal(claim)
		if err != nil {
			t.Fatal(err)
		}
		emit(storageACK{ID: "claim-" + id, Receipt: claimBytes, Claim: true})
		if err := h.Sessions.ProcessClaim(context.Background(), claim.Jobs[0]); err != nil {
			t.Fatal(err)
		}
		q, err := h.Durable.QueryReceipt(context.Background(), caller, c.Identity)
		if err != nil {
			t.Fatal(err)
		}
		b, err := proto.Marshal(q.Receipt)
		if err != nil {
			t.Fatal(err)
		}
		emit(storageACK{ID: id, Receipt: b})
		if mode != "FULL" {
			os.Exit(0)
		}
		// A 保留后检查点；B 复用 WAL 后截断；C 在新 WAL 中提交。
		checkpoint := "PRAGMA wal_checkpoint(RESTART)"
		if id == "B" {
			checkpoint = "PRAGMA wal_checkpoint(TRUNCATE)"
		}
		if err := h.StorageFaultSQL(checkpoint); err != nil {
			t.Fatal(err)
		}
	}
	os.Exit(0) // 禁止 Close 把未同步数据变成稳定数据。
}

func storageImage(t *testing.T, base []byte, events []storageEvent, policy string) string {
	t.Helper()
	stable := map[int][]byte{}
	if base != nil {
		stable[0] = bytes.Clone(base)
	}
	pending := map[int][]storageEvent{}
	apply := func(e storageEvent) {
		switch e.Kind {
		case 'O':
			if _, ok := stable[e.File]; !ok {
				stable[e.File] = []byte{}
			}
		case 'D':
			delete(stable, e.File)
		case 'T':
			b := stable[e.File]
			if e.Offset < int64(len(b)) {
				b = b[:e.Offset]
			} else {
				b = append(b, make([]byte, int(e.Offset)-len(b))...)
			}
			stable[e.File] = b
		case 'W':
			b := stable[e.File]
			end := int(e.Offset) + len(e.Data)
			if end > len(b) {
				b = append(b, make([]byte, end-len(b))...)
			}
			copy(b[int(e.Offset):], e.Data)
			stable[e.File] = b
		}
	}
	for _, e := range events {
		if e.Kind == 'N' {
			// Unix VFS 首次同步新日志时还同步父目录，稳定该目录先前的名称变化。
			for file, list := range pending {
				var rest []storageEvent
				for _, entry := range list {
					if entry.Kind == 'O' || entry.Kind == 'D' {
						apply(entry)
					} else {
						rest = append(rest, entry)
					}
				}
				pending[file] = rest
			}
		} else if e.Kind == 'S' {
			for _, p := range pending[e.File] {
				apply(p)
			}
			delete(pending, e.File)
		} else if e.Kind == 'D' && e.Offset != 0 {
			apply(e)
			delete(pending, e.File)
		} else {
			pending[e.File] = append(pending[e.File], e)
		}
	}
	for _, list := range pending {
		switch policy {
		case "lost":
		case "all":
			for _, e := range list {
				apply(e)
			}
		case "reverse":
			for i := len(list) - 1; i >= 0; i-- {
				if list[i].Kind == 'W' {
					apply(list[i])
				}
			}
		case "even":
			for i, e := range list {
				if i%2 == 0 && e.Kind == 'W' {
					apply(e)
				}
			}
		case "torn":
			for _, e := range list {
				if e.Kind == 'W' {
					if len(e.Data) > 2048 {
						e.Data = e.Data[:2048]
					} else if len(e.Data) > 1 {
						e.Data = e.Data[:len(e.Data)/2]
					}
					apply(e)
				}
			}
		default:
			t.Fatalf("unknown policy %s", policy)
		}
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "recovered.db")
	for id, b := range stable {
		suffix := []string{"", "-wal", "-journal"}[id]
		if err := os.WriteFile(path+suffix, b, 0600); err != nil {
			t.Fatal(err)
		}
	}
	return path
}

func storageVerify(t *testing.T, path string, acks []storageACK, cut int) {
	t.Helper()
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("PRAGMA fullfsync=ON; PRAGMA synchronous=FULL"); err != nil {
		db.Close()
		t.Fatal(err)
	}
	var integrity string
	err = db.QueryRow("PRAGMA integrity_check").Scan(&integrity)
	if err != nil || integrity != "ok" {
		db.Close()
		t.Fatalf("crash image corrupt: %s %v", integrity, err)
	}
	rows, err := db.Query("PRAGMA foreign_key_check")
	if err != nil {
		t.Fatal(err)
	}
	if rows.Next() {
		t.Fatal("foreign-key violation")
	}
	rows.Close()
	db.Close()
	h, err := assembly.Open(path, "alice", "local")
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	for _, a := range acks {
		if a.Start || a.Cut > cut {
			continue
		}
		caller, c := goal()
		c.Identity.CommandId = a.ID
		q, err := h.Durable.QueryReceipt(context.Background(), caller, c.Identity)
		expected := new(v1.CommandReceipt)
		if err := proto.Unmarshal(a.Receipt, expected); err != nil {
			t.Fatal(err)
		}
		same := proto.Equal(q.GetReceipt(), expected)
		if a.Submitted && q.GetReceipt() != nil {
			actual := q.Receipt
			same = proto.Equal(actual.Identity, expected.Identity) && proto.Equal(actual.InputRef, expected.InputRef) && proto.Equal(actual.JobRef, expected.JobRef) && actual.Fingerprint == expected.Fingerprint && actual.ResponsibleDomainId == expected.ResponsibleDomainId && actual.DurabilityProfile == expected.DurabilityProfile
		}
		if err != nil || !same {
			t.Fatalf("acknowledged %s lost/changed at cut %d: %v %v", a.ID, cut, q, err)
		}
		if !a.Claim {
			assertOneTask(t, h, caller, c)
		}
	}
	// 未收到确认的命令允许缺失或已提交；原身份重试后仍只有一个任务。
	for _, id := range []string{"A", "B", "C"} {
		caller, c := goal()
		c.Identity.CommandId = id
		q, err := h.Durable.QueryReceipt(context.Background(), caller, c.Identity)
		if err != nil {
			t.Fatal(err)
		}
		if q.State == v1.ReceiptQueryState_RECEIPT_QUERY_STATE_NOT_FOUND {
			for _, a := range acks {
				if a.ID != id || a.Submitted {
					continue
				}
				r := new(v1.CommandReceipt)
				if err := proto.Unmarshal(a.Receipt, r); err != nil {
					t.Fatal(err)
				}
				task, _ := h.Tasks.QueryTask(context.Background(), caller, r.TaskRef.Name)
				session, _ := h.Sessions.QuerySession(context.Background(), caller, r.SessionRef.Name)
				if task != nil || session != nil {
					t.Fatalf("unacknowledged %s partially committed", id)
				}
			}
		}
		if _, err := h.Sessions.SubmitGoal(context.Background(), caller, c); err != nil {
			t.Fatal(err)
		}
		if err := h.Sessions.ProcessPending(context.Background(), caller); err != nil {
			t.Fatal(err)
		}
		assertOneTask(t, h, caller, c)
	}
}

// 规则：G3
func TestStorageDirectorySyncError(t *testing.T) {
	_, acks, _ := storageRun(t, "directory-sync-error")
	if len(acks) != 1 || !acks[0].Error || acks[0].DirectoryFailures == 0 {
		t.Fatalf("directory sync failure not rejected: %+v", acks)
	}
}
