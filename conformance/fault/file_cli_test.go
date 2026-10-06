//go:build fault && darwin

package fault_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"

	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

func buildFileCLI(t *testing.T) string {
	t.Helper()
	binary := filepath.Join(t.TempDir(), "lerna")
	build := exec.Command("go", "build", "-o", binary, "./cmd/lerna")
	build.Dir = filepath.Join("..", "..")
	if output, e := build.CombinedOutput(); e != nil {
		t.Fatalf("build actual CLI: %v %s", e, output)
	}
	return binary
}
func fileCLICommand(t *testing.T, binary string, n *nativeScenario, start *v1.StartExecutionCommand, roots ...string) ([]byte, error) {
	t.Helper()
	body, e := protojson.Marshal(start)
	if e != nil {
		t.Fatal(e)
	}
	path := filepath.Join(t.TempDir(), "start.json")
	if e = os.WriteFile(path, body, 0600); e != nil {
		t.Fatal(e)
	}
	args := []string{"--db", n.path, "--user", "u", "--domain", "d", "--issuer", "egress"}
	for _, root := range roots {
		args = append(args, "--file-root", root)
	}
	args = append(args, "execute", path)
	return exec.Command(binary, args...).CombinedOutput()
}

// nativeManifest 是独立目标 oracle；正文、inode、大小与写入时间不从账本效果推导。
func nativeManifest(t *testing.T, root string) map[string]string {
	t.Helper()
	files := map[string]string{}
	for _, dir := range []string{"objects", "commits", "locks"} {
		entries, e := os.ReadDir(filepath.Join(root, dir))
		if e != nil {
			t.Fatal(e)
		}
		for _, entry := range entries {
			path := filepath.Join(root, dir, entry.Name())
			body, e := os.ReadFile(path)
			if e != nil {
				t.Fatal(e)
			}
			info, e := os.Stat(path)
			if e != nil {
				t.Fatal(e)
			}
			stat := info.Sys().(*syscall.Stat_t)
			files[dir+"/"+entry.Name()] = fmt.Sprintf("%d/%d/%d/%d/%s", stat.Dev, stat.Ino, info.Size(), info.ModTime().UnixNano(), body)
		}
	}
	return files
}

// 规则：G1、G3、G4、G5、G10、G11
func TestNativeFileCLIUsesConfiguredRootAndOriginalExit(t *testing.T) {
	binary := buildFileCLI(t)
	n := newNativeScenario(t)
	a, start := n.prepare(t, "cli-create", "CREATE", "", []byte("created through CLI"))
	output, e := fileCLICommand(t, binary, n, start, "documents="+n.root)
	if e != nil {
		t.Fatalf("configured CLI create: %v %s", e, output)
	}
	receipt := new(v1.CommandReceipt)
	if e = protojson.Unmarshal(output, receipt); e != nil || receipt.Decision != v1.Decision_DECISION_ACCEPTED {
		t.Fatalf("CLI receipt: %v %s", e, output)
	}
	first := readNativePointer(t, n.root)
	body, e := os.ReadFile(filepath.Join(n.root, "objects", first.ObjectName))
	if e != nil || string(body) != "created through CLI" || !proto.Equal(first.OperationId, a.OperationId) {
		t.Fatalf("actual publication: %q %v", body, e)
	}
	snapshot := nativeManifest(t, n.root)
	repeated, e := fileCLICommand(t, binary, n, start, "documents="+n.root)
	if e != nil {
		t.Fatalf("CLI replay: %v %s", e, repeated)
	}
	replay := new(v1.CommandReceipt)
	if e = protojson.Unmarshal(repeated, replay); e != nil || !proto.Equal(receipt, replay) || !reflect.DeepEqual(snapshot, nativeManifest(t, n.root)) {
		t.Fatal("CLI replay changed original receipt or actual files")
	}
	op, e := n.h.Ledger.QueryOperation(n.ctx, n.caller, a.OperationId)
	if e != nil || op.Effect.Outcome != "APPLIED" || op.Execution.Send.SendSeq != 1 || len(op.Execution.PreviousSends) != 0 {
		t.Fatalf("CLI original effect: %v %v", op, e)
	}
	use, e := n.h.Grants.QueryCredentialUse(n.ctx, n.caller, start.CredentialRef)
	if e != nil || use == nil {
		t.Fatalf("CLI bypassed credential consumption: %v %v", use, e)
	}
	reservations, e := n.h.Budget.QueryReservations(n.ctx, n.caller, a.TaskId)
	if e != nil || len(reservations) != 1 || reservations[0].ConsumedSends != 1 {
		t.Fatalf("CLI send consumption: %v %v", reservations, e)
	}
	raw := n.observation(t, a)
	if !raw.FileEvidence.DurabilityConfirmed || !raw.FileEvidence.ReadbackVerified || !proto.Equal(raw.SendRef.Name, op.Execution.Send.Ref.Name) {
		t.Fatalf("CLI native evidence attribution: %v", raw)
	}
	replacement := n.additionalTask(t, "cli-replace")
	b, replace := replacement.prepare(t, "cli-replace", "REPLACE", first.Version, []byte("replacement through CLI"))
	output, e = fileCLICommand(t, binary, n, replace, "documents="+n.root)
	if e != nil {
		t.Fatalf("configured CLI replace: %v %s", e, output)
	}
	second := readNativePointer(t, n.root)
	body, e = os.ReadFile(filepath.Join(n.root, "objects", second.ObjectName))
	if e != nil || string(body) != "replacement through CLI" || second.PreviousVersion != first.Version || !proto.Equal(second.OperationId, b.OperationId) {
		t.Fatalf("actual conditional replacement: %q %v", body, e)
	}
	old, e := os.ReadFile(filepath.Join(n.root, "objects", first.ObjectName))
	if e != nil || string(old) != "created through CLI" {
		t.Fatal("replacement erased original historical object")
	}
	snapshot = nativeManifest(t, n.root)
	output, e = fileCLICommand(t, binary, n, replace, "documents="+n.root)
	if e != nil || !reflect.DeepEqual(snapshot, nativeManifest(t, n.root)) {
		t.Fatalf("replacement replay mutated target: %v %s", e, output)
	}
	t.Log("actual CLI CREATE then REPLACE: two immutable objects, one current pointer; each original command replay leaves inode/bytes/mtime unchanged")
}

// 规则：G1、G4、G5、G8
func TestNativeFileCLIRejectsInvalidRootConfiguration(t *testing.T) {
	binary := buildFileCLI(t)
	for _, roots := range [][]string{{"documents=relative/path"}, {"documents="}, {".internal=/absolute/root"}, {"documents=/a/../b"}, {"documents=/a", "documents=/b"}} {
		t.Run(strings.Join(roots, ","), func(t *testing.T) {
			n := newNativeScenario(t)
			a, start := n.prepare(t, "invalid-root", "CREATE", "", []byte("must not publish"))
			output, e := fileCLICommand(t, binary, n, start, roots...)
			if e == nil || !strings.Contains(string(output), "INVALID_FILE_ROOT_CONFIG") {
				t.Fatalf("invalid root configuration accepted: %v %s", e, output)
			}
			x, e := n.h.Ledger.QueryExecution(n.ctx, n.caller, a.OperationId)
			if e != nil || x.Send.Phase != "REGISTERED" || len(nativeManifest(t, n.root)) != 0 {
				t.Fatalf("bad configuration entered target: %v %v", x, e)
			}
			use, e := n.h.Grants.QueryCredentialUse(n.ctx, n.caller, start.CredentialRef)
			if e != nil || use != nil {
				t.Fatalf("bad configuration consumed authority: %v %v", use, e)
			}
		})
	}
}

// 规则：G1、G3、G4、G5、G8、G11
func TestNativeFileCLIRefusesReboundOriginalRoot(t *testing.T) {
	binary := buildFileCLI(t)
	n := newNativeScenario(t)
	_, start := n.prepare(t, "cli-original-root", "CREATE", "", []byte("bound original root"))
	output, e := fileCLICommand(t, binary, n, start, "documents="+n.root)
	if e != nil {
		t.Fatalf("initial CLI create: %v %s", e, output)
	}
	original := readNativePointer(t, n.root)
	before := nativeManifest(t, n.root)
	binding, e := n.h.Content.QueryManagedFileRoot(n.ctx, n.caller, "documents")
	if e != nil || binding == nil {
		t.Fatalf("missing governed root: %v %v", binding, e)
	}
	successor := n.additionalTask(t, "cli-rebind")
	a, replace := successor.prepare(t, "cli-rebind", "REPLACE", original.Version, []byte("must not publish into replacement root"))
	other := nativeRootDirectory(t)
	output, e = fileCLICommand(t, binary, n, replace, "documents="+other)
	if e != nil {
		t.Fatalf("rebind command did not retain observation: %v %s", e, output)
	}
	raw := n.observation(t, a)
	if raw.FileEvidence.ErrorCode != "FILE_ROOT_CHANGED" || raw.FileEvidence.Published || raw.FileEvidence.DurabilityConfirmed || len(nativeManifest(t, other)) != 0 || !reflect.DeepEqual(before, nativeManifest(t, n.root)) {
		t.Fatalf("rebound root touched target: %v", raw)
	}
	after, e := n.h.Content.QueryManagedFileRoot(n.ctx, n.caller, "documents")
	if e != nil || !proto.Equal(binding, after) {
		t.Fatalf("root alias rebound: %v %v", after, e)
	}
	op, e := n.h.Ledger.QueryOperation(n.ctx, n.caller, a.OperationId)
	if e != nil || op.Effect.Outcome != "NOT_APPLIED" {
		t.Fatalf("rebind failed to preserve negative effect evidence: %v %v", op, e)
	}
	t.Log("new CLI mapping refused after original persisted native root binding; both target namespaces unchanged")
}
