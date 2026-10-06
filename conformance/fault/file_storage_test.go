//go:build fault && darwin

package fault_test

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"testing"

	"github.com/ruipengliu/lerna/cmd/assembly"
	"github.com/ruipengliu/lerna/conformance/fault/storagevfs"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"github.com/ruipengliu/lerna/infra/egressio"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// fileImage 独立保存 inode 正文和目录名称，目录同步不能隐式稳定正文。
type fileImage struct {
	data  map[string][]byte
	names map[string]map[string]string
}

func replayNative(events []egressio.NativeFileEvent, bytePolicy, namePolicy string) fileImage {
	image := fileImage{data: map[string][]byte{}, names: map[string]map[string]string{}}
	pendingBytes := map[string][]egressio.NativeFileEvent{}
	pendingNames := map[string][]egressio.NativeFileEvent{}
	write := func(e egressio.NativeFileEvent) {
		data := image.data[e.Identity]
		end := int(e.Offset) + len(e.Data)
		if len(data) < end {
			data = append(data, make([]byte, end-len(data))...)
		}
		copy(data[int(e.Offset):], e.Data)
		image.data[e.Identity] = data
	}
	name := func(e egressio.NativeFileEvent) {
		dir := image.names[e.Directory]
		if dir == nil {
			dir = map[string]string{}
			image.names[e.Directory] = dir
		}
		switch e.Kind {
		case "create":
			dir[e.Name] = e.Identity
		case "unlink":
			delete(dir, e.Name)
		case "rename":
			// rename 的删除和替换是一个原子名称变化；不生成半次 rename。
			id, ok := dir[e.Name]
			if ok {
				delete(dir, e.Name)
				dir[e.NewName] = id
			}
		}
	}
	for _, e := range events {
		switch e.Kind {
		case "create", "rename", "unlink":
			pendingNames[e.Directory] = append(pendingNames[e.Directory], e)
		case "write":
			pendingBytes[e.Identity] = append(pendingBytes[e.Identity], e)
		case "sync":
			for _, w := range pendingBytes[e.Identity] {
				write(w)
			}
			delete(pendingBytes, e.Identity)
		case "dirsync":
			for _, n := range pendingNames[e.Directory] {
				name(n)
			}
			delete(pendingNames, e.Directory)
		}
	}
	for _, list := range pendingBytes {
		switch bytePolicy {
		case "all":
			for _, e := range list {
				write(e)
			}
		case "reverse":
			for i := len(list) - 1; i >= 0; i-- {
				write(list[i])
			}
		case "even":
			for i, e := range list {
				if i%2 == 0 {
					write(e)
				}
			}
		case "torn":
			for _, e := range list {
				size := len(e.Data) / 2
				if size > 2048 {
					size = 2048
				}
				e.Data = e.Data[:size]
				write(e)
			}
		}
	}
	for _, list := range pendingNames {
		// 以目录事件前缀选择未同步名称；保留每个 rename 的原子性与前置 create 关系。
		count := 0
		switch namePolicy {
		case "all":
			count = len(list)
		case "prefix":
			count = len(list) / 2
		}
		for _, e := range list[:count] {
			name(e)
		}
	}
	return image
}
func (image fileImage) commit() (*v1.FileCommit, error) {
	id, ok := image.names["commits"]["report"]
	if !ok {
		return nil, os.ErrNotExist
	}
	c := new(v1.FileCommit)
	if e := protojson.Unmarshal(image.data[id], c); e != nil {
		return nil, e
	}
	object, ok := image.names["objects"][c.ObjectName]
	if !ok {
		return nil, fmt.Errorf("object name lost")
	}
	body := image.data[object]
	if uint64(len(body)) != c.ByteSize || fmt.Sprintf("%x", sha256.Sum256(body)) != c.Digest {
		return nil, fmt.Errorf("object bytes lost")
	}
	return c, nil
}

type fileClock struct{ Native, SQLite int }
type nativeStorageACK struct {
	Receipt, Observation []byte
	SQLite, Native       int
}
type nativeStorageRun struct {
	events    []egressio.NativeFileEvent
	sql       []storageEvent
	clocks    []fileClock
	ack       nativeStorageACK
	base      []byte
	root      string
	admission *v1.Admission
}

func nativeStorageProducer(t *testing.T, omit string, combined ...bool) nativeStorageRun {
	t.Helper()
	n := newNativeScenario(t)
	a, start := n.prepare(t, "storage", "CREATE", "", bytes.Repeat([]byte("native payload;"), 128))
	blob, e := proto.Marshal(start)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(n.path+".start", blob, 0600); e != nil {
		t.Fatal(e)
	}
	if e = n.h.Close(); e != nil {
		t.Fatal(e)
	}
	base, e := os.ReadFile(n.path)
	if e != nil {
		t.Fatal(e)
	}
	child := exec.Command(os.Args[0], "-test.run=^TestNativeFileStorageChild$")
	child.Env = append(os.Environ(), "LERNA_FILE_STORAGE_DB="+n.path, "LERNA_FILE_STORAGE_ROOT="+n.root, "LERNA_FILE_OMIT="+omit)
	useSQL := len(combined) > 0 && combined[0]
	if useSQL {
		child.Env = append(child.Env, "LERNA_FILE_COMBINED=1")
	}
	out, e := child.CombinedOutput()
	if e != nil {
		t.Fatalf("producer: %v %s", e, out)
	}
	run := nativeStorageRun{base: base, root: n.root, admission: a}
	if e = json.Unmarshal(bytes.TrimSpace(out), &run.ack); e != nil {
		t.Fatalf("independent ACK: %v %s", e, out)
	}
	readLines := func(path string, consume func([]byte) error) {
		f, e := os.Open(path)
		if e != nil {
			t.Fatal(e)
		}
		defer f.Close()
		scan := bufio.NewScanner(f)
		scan.Buffer(make([]byte, 4096), 4<<20)
		for scan.Scan() {
			if e = consume(scan.Bytes()); e != nil {
				t.Fatal(e)
			}
		}
		if e = scan.Err(); e != nil {
			t.Fatal(e)
		}
	}
	readLines(n.path+".native", func(b []byte) error {
		var event egressio.NativeFileEvent
		if e := json.Unmarshal(b, &event); e != nil {
			return e
		}
		run.events = append(run.events, event)
		return nil
	})
	readLines(n.path+".clock", func(b []byte) error {
		var clock fileClock
		if e := json.Unmarshal(b, &clock); e != nil {
			return e
		}
		run.clocks = append(run.clocks, clock)
		return nil
	})
	var trace []byte
	if useSQL {
		trace, e = os.ReadFile(n.path + ".sqltrace")
		if e != nil {
			t.Fatal(e)
		}
	}
	for len(trace) > 0 {
		if len(trace) < 24 {
			t.Fatal("truncated SQL trace")
		}
		size := int(binary.LittleEndian.Uint32(trace[16:20]))
		if size > len(trace)-24 {
			t.Fatal("truncated SQL payload")
		}
		run.sql = append(run.sql, storageEvent{Kind: trace[0], File: int(trace[1]), Offset: int64(binary.LittleEndian.Uint64(trace[8:16])), Data: bytes.Clone(trace[24 : 24+size])})
		trace = trace[24+size:]
	}
	if len(run.events) != len(run.clocks) || run.ack.Native != len(run.events) || run.ack.SQLite != len(run.sql) {
		t.Fatal("trace/clock/ACK mismatch")
	}
	for i, event := range run.events {
		if event.Sequence != i+1 || run.clocks[i].Native != event.Sequence || run.clocks[i].SQLite < 0 || run.clocks[i].SQLite > len(run.sql) || (i > 0 && run.clocks[i].SQLite < run.clocks[i-1].SQLite) {
			t.Fatal("non-monotonic native/SQLite chronology")
		}
	}
	receipt := new(v1.CommandReceipt)
	if proto.Unmarshal(run.ack.Receipt, receipt) != nil || receipt.Decision != v1.Decision_DECISION_ACCEPTED {
		t.Fatal("producer missing accepted receipt")
	}
	raw := new(v1.RawObservation)
	if proto.Unmarshal(run.ack.Observation, raw) != nil || !raw.GetFileEvidence().GetDurabilityConfirmed() || !raw.GetFileEvidence().GetReadbackVerified() {
		t.Fatal("producer missing durable evidence")
	}
	return run
}

// 规则：G3、G5、G11
func TestNativeFileStorageChild(t *testing.T) {
	path := os.Getenv("LERNA_FILE_STORAGE_DB")
	if path == "" {
		t.Skip("subprocess only")
	}
	if os.Getenv("LERNA_FILE_COMBINED") == "1" {
		if e := storagevfs.Register(path + ".sqltrace"); e != nil {
			t.Fatal(e)
		}
	}
	h, e := assembly.OpenWithFiles(path, "u", "d", map[string]string{"documents": os.Getenv("LERNA_FILE_STORAGE_ROOT")})
	if e != nil {
		t.Fatal(e)
	}
	if os.Getenv("LERNA_FILE_COMBINED") == "1" && h.StorageSettings().SQLiteSourceID != storagevfs.SourceID() {
		t.Fatal("SQLite VFS source ID mismatch")
	}
	if e = h.StorageFaultSQL("PRAGMA wal_autocheckpoint=0"); e != nil {
		t.Fatal(e)
	}
	trace, e := os.Create(path + ".native")
	if e != nil {
		t.Fatal(e)
	}
	clock, e := os.Create(path + ".clock")
	if e != nil {
		t.Fatal(e)
	}
	recorder := egressio.NewNativeFileRecorder(trace, func(seq int) {
		if e := json.NewEncoder(clock).Encode(fileClock{Native: seq, SQLite: storagevfs.Sequence()}); e != nil {
			panic(e)
		}
	})
	blob, e := os.ReadFile(path + ".start")
	if e != nil {
		t.Fatal(e)
	}
	start := new(v1.StartExecutionCommand)
	if e = proto.Unmarshal(blob, start); e != nil {
		t.Fatal(e)
	}
	ctx := egressio.WithNativeFileFault(context.Background(), &egressio.NativeFileFault{Recorder: recorder, OmitBarrier: os.Getenv("LERNA_FILE_OMIT"), MaxWrite: 997})
	receipt, e := h.Egress.Invoke(ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, start)
	requireAccepted(t, receipt, e)
	execution, e := h.Ledger.QueryExecution(context.Background(), &v1.Caller{UserId: "u", IssuerId: "host"}, start.Binding.OperationId)
	if e != nil {
		t.Fatal(e)
	}
	raw, e := h.Ledger.QueryObservation(context.Background(), &v1.Caller{UserId: "u", IssuerId: "host"}, execution.Send.ObservationRef)
	if e != nil {
		t.Fatal(e)
	}
	rb, e := proto.Marshal(receipt)
	if e != nil {
		t.Fatal(e)
	}
	ob, e := proto.Marshal(raw)
	if e != nil {
		t.Fatal(e)
	}
	if e = json.NewEncoder(os.Stdout).Encode(nativeStorageACK{Receipt: rb, Observation: ob, SQLite: storagevfs.Sequence(), Native: recorder.Sequence()}); e != nil {
		t.Fatal(e)
	}
	os.Exit(0) // 生产轨迹不允许 Close 引入额外屏障。
}

// 规则：G3、G5、G11
func TestNativeFileStorageProtocolPrefixes(t *testing.T) {
	run := nativeStorageProducer(t, "")
	count := 0
	for cut := 0; cut <= len(run.events); cut++ {
		for _, bytesPolicy := range []string{"lost", "all", "reverse", "even", "torn"} {
			for _, namesPolicy := range []string{"lost", "all", "prefix"} {
				image := replayNative(run.events[:cut], bytesPolicy, namesPolicy)
				_, e := image.commit()
				count++
				if _, visible := image.names["commits"]["report"]; visible && e != nil {
					t.Fatalf("visible pointer references lost object at %d/%s/%s: %v", cut, bytesPolicy, namesPolicy, e)
				}
				if cut >= run.ack.Native && e != nil {
					t.Fatalf("ACK publication lost at %d/%s/%s: %v", cut, bytesPolicy, namesPolicy, e)
				}
			}
		}
	}
	// 实际四道同步必须独立出现，且读回在最后一道之后。
	stages := map[string]int{}
	renames := 0
	for _, e := range run.events {
		if e.Kind == "sync" || e.Kind == "dirsync" {
			stages[e.Stage]++
		}
		if e.Kind == "rename" {
			renames++
		}
	}
	for _, stage := range []string{"object.sync", "objects.sync", "pointer.sync", "commits.sync"} {
		if stages[stage] != 1 {
			t.Fatalf("missing native barrier %s", stage)
		}
	}
	if renames != 1 {
		t.Fatalf("actual publications=%d", renames)
	}
	t.Logf("native protocol: %d finite crash images, %d actual completed native events", count, len(run.events))
}

// 规则：G3
func TestNativeFileStorageBarrierNegativeControls(t *testing.T) {
	for _, omit := range []string{"object.sync", "objects.sync", "pointer.sync", "commits.sync"} {
		t.Run(omit, func(t *testing.T) {
			run := nativeStorageProducer(t, omit)
			for _, e := range run.events {
				if e.Stage == omit && (e.Kind == "sync" || e.Kind == "dirsync") {
					t.Fatal("omitted barrier still traced")
				}
			}
			image := replayNative(run.events, "lost", "lost")
			if _, e := image.commit(); e == nil {
				t.Fatalf("missing %s failed to expose acknowledged loss", omit)
			}
		})
	}
}

// combinedFileCuts 合并每个 SQLite 事件与原生完成事件，单调枚举实际总时间线的每个前缀。
func combinedFileCuts(run nativeStorageRun) []fileClock {
	cuts := []fileClock{{}}
	current := fileClock{}
	for _, native := range run.clocks {
		for current.SQLite < native.SQLite {
			current.SQLite++
			cuts = append(cuts, current)
		}
		current.Native = native.Native
		cuts = append(cuts, current)
	}
	for current.SQLite < run.ack.SQLite {
		current.SQLite++
		cuts = append(cuts, current)
	}
	return cuts
}

// 规则：G3、G11
func TestNativeFileStorageCombinedChronology(t *testing.T) {
	var run nativeStorageRun
	t.Run("producer", func(t *testing.T) {
		run = nativeStorageProducer(t, "", true)
		t.Logf("combined actual trace: %d SQLite events, %d native events, %d chronological cuts, %d finite paired images", len(run.sql), len(run.events), len(combinedFileCuts(run)), len(combinedFileCuts(run))*5)
	})
	if run.admission == nil {
		t.Fatal("combined producer unavailable")
	}
	// 每个真实 SQLite 与原生事件完成点形成同一时间线切点，包含零前缀与最终 ACK。
	cuts := combinedFileCuts(run)
	if len(cuts) != len(run.sql)+len(run.events)+1 || cuts[len(cuts)-1] != (fileClock{Native: run.ack.Native, SQLite: run.ack.SQLite}) {
		t.Fatal("incomplete combined event prefixes")
	}
	for i, cut := range cuts {
		for _, policy := range []string{"lost", "all", "reverse", "even", "torn"} {
			t.Run(fmt.Sprintf("cut-%d/%s", i, policy), func(t *testing.T) {
				path := storageImage(t, run.base, run.sql[:cut.SQLite], policy)
				image := replayNative(run.events[:cut.Native], policy, "lost")
				h, e := assembly.OpenWithFiles(path, "u", "d", map[string]string{"documents": run.root})
				if e != nil {
					t.Fatal(e)
				}
				defer h.Close()
				op, e := h.Ledger.QueryOperation(context.Background(), &v1.Caller{UserId: "u", IssuerId: "host"}, run.admission.OperationId)
				if e != nil {
					t.Fatal(e)
				}
				if op == nil {
					t.Fatal("pre-existing admitted responsibility lost")
				}
				if op.Effect.Outcome == "APPLIED" {
					c, e := image.commit()
					if e != nil || !proto.Equal(c.OperationId, op.Ref.Name) {
						t.Fatalf("durable ledger without native publication: %v %v", c, e)
					}
				}
				if cut.SQLite >= run.ack.SQLite {
					if op.Effect.Outcome != "APPLIED" {
						t.Fatalf("acknowledged ledger lost: %v", op.Effect)
					}
					c, e := image.commit()
					if e != nil || !proto.Equal(c.OperationId, op.Ref.Name) {
						t.Fatalf("acknowledged native publication lost: %v", e)
					}
				}
			})
		}
	}
}
