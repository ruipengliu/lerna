package catalogcheck

import (
	"context"
	"encoding/json"
	"fmt"
	"lerna/adapters/simworkflow"
	"lerna/adapters/sqlitecatalog"
	"lerna/authorization"
	"lerna/catalog"
	"lerna/execution"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

var crashNames = []string{"import-interrupted", "rebuild-interrupted", "before-admission", "after-admission", "after-effect"}

type checkpoint struct {
	Request  execution.Request
	Material string
}

// This context exits only at an application-owned cancellation checkpoint in
// the actual import/rebuild loop, while its real SQLite transaction is open.
// It does not replace the store, codec or transaction implementation.
type crashContext struct {
	context.Context
	function     string
	after, count int
}

func (c *crashContext) Err() error {
	pc, _, _, _ := runtime.Caller(1)
	f := runtime.FuncForPC(pc)
	if f != nil && strings.HasSuffix(f.Name(), c.function) {
		c.count++
		if c.count == c.after {
			os.Exit(73)
		}
	}
	return c.Context.Err()
}
func RunProbe(ctx context.Context, root, point string) error {
	defs, e := simworkflow.Definitions()
	if e != nil {
		return e
	}
	entries := []catalog.Entry{}
	for _, d := range defs {
		entries = append(entries, d.Entries()...)
	}
	check, e := simworkflow.ImplementationCheck()
	if e != nil {
		return e
	}
	st, e := sqlitecatalog.Open(filepath.Join(root, "catalog.db"), check)
	if e != nil {
		return e
	}
	defer st.Close()
	if point == "import-interrupted" {
		_, e = st.Replace(&crashContext{Context: ctx, function: "(*Store).Replace", after: 1018}, 1, entries)
		return fmt.Errorf("crash point not reached: %v", e)
	}
	if point == "rebuild-interrupted" {
		_, e = st.Rebuild(&crashContext{Context: ctx, function: "(*Store).Rebuild", after: 10}, 64)
		return fmt.Errorf("crash point not reached: %v", e)
	}
	token, e := os.ReadFile(filepath.Join(root, defs[0].Kind, "session"))
	if e != nil {
		return e
	}
	h, e := open(ctx, filepath.Join(root, defs[0].Kind), string(token), defs[0])
	if e != nil {
		return e
	}
	defer h.close()
	raw, e := os.ReadFile(filepath.Join(root, "checkpoint.json"))
	if e != nil {
		return e
	}
	var cp checkpoint
	if e = json.Unmarshal(raw, &cp); e != nil {
		return e
	}
	gate := catalog.Admission{Store: st, Executor: h.exec, Ref: entries[0].Ref}
	if point == "before-admission" {
		return st.WithVersion(ctx, entries[0].Ref, func(catalog.Entry) error { os.Exit(73); return nil })
	}
	if _, e = gate.Invoke(ctx, cp.Request, cp.Material); e != nil {
		return e
	}
	if point == "after-admission" {
		os.Exit(73)
	}
	if point == "after-effect" {
		if _, e = h.exec.Run(ctx, cp.Request.OperationID); e != nil {
			return e
		}
		os.Exit(73)
	}
	return fmt.Errorf("unknown crash point")
}
func recovery(ctx context.Context, executable, point string) error {
	f, e := newFixture(ctx)
	if e != nil {
		return e
	}
	defer f.close()
	if point == "rebuild-interrupted" {
		if e = f.store.InvalidateIndex(ctx); e != nil {
			return e
		}
	}
	var cp checkpoint
	if point == "before-admission" || point == "after-admission" || point == "after-effect" {
		if _, e = f.manager.Resolve(ctx, f.entries[0].Ref.Namespace); e != nil {
			return e
		}
		h := f.manager.current
		cp.Request, cp.Material, e = h.request(ctx, []byte(`{"record":"crash","ordered_units":3,"approved_units":10,"vendor_verified":true}`), 1)
		if e != nil {
			return e
		}
		raw, _ := json.Marshal(cp)
		if e = os.WriteFile(filepath.Join(f.root, "checkpoint.json"), raw, 0600); e != nil {
			return e
		}
	}
	cmd := exec.CommandContext(ctx, executable, "catalog-probe", f.root, point)
	if strings.HasSuffix(executable, ".test") {
		cmd = exec.CommandContext(ctx, executable, "-test.run=TestCatalogChild")
		cmd.Env = append(os.Environ(), "CATALOG_PROBE_ROOT="+f.root, "CATALOG_PROBE_POINT="+point)
	}
	out, e := cmd.CombinedOutput()
	exit, ok := e.(*exec.ExitError)
	if !ok || exit.ExitCode() != 73 {
		return fmt.Errorf("child did not interrupt actual transaction: %v %s", e, out)
	}
	snapshot, e := f.store.Snapshot(ctx, "", 2048)
	if e != nil || snapshot.Revision != 1 || len(snapshot.Rows) != 1008 {
		return fmt.Errorf("partial catalog: %v", e)
	}
	if point == "import-interrupted" {
		_, e = f.client.Describe(ctx, f.entries[500].Ref)
		return e
	}
	if point == "rebuild-interrupted" {
		p, e := f.client.Search(ctx, query())
		if e != nil {
			return e
		}
		if len(p.Limitations) == 0 {
			return fmt.Errorf("partial index claimed current")
		}
		for n := 0; n < 18; n++ {
			done, e := f.store.Rebuild(ctx, 64)
			if e != nil {
				return e
			}
			if done {
				p, e = f.client.Search(ctx, query())
				return must(e == nil && p.IndexRevision == 1 && len(p.Limitations) == 0, "rebuild did not recover")
			}
		}
		return fmt.Errorf("unbounded rebuild")
	}
	h := f.manager.current
	record, e := h.exec.GetInvocation(ctx, cp.Request.OperationID)
	if point == "before-admission" {
		_, targetErr := h.target.Snapshot(ctx, "crash")
		return must(authorization.Is(e, authorization.Denied) && targetErr != nil, "unadmitted action survived")
	}
	if e != nil || record.Request.DescriptorSHA256 != f.entries[0].Ref.Digest {
		return fmt.Errorf("admission not durable: %v", e)
	}
	for range 2 {
		if _, e = h.exec.Run(ctx, cp.Request.OperationID); e != nil {
			return e
		}
	}
	if e = h.exec.Drain(ctx, 16); e != nil {
		return e
	}
	snap, e := h.target.Snapshot(ctx, "crash")
	return must(e == nil && snap.State == "draft" && snap.Version == 2 && snap.Ledger == 1000, "repeated/lost effect")
}
