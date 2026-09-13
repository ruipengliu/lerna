package memorycheck

import (
	"context"
	"encoding/json"
	"fmt"
	"lerna/authorization"
	wire "lerna/gen/harness/v1"
	"lerna/memory"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type crashStore struct {
	memory.QueryStore
	mode string
}

func (s crashStore) Commit(ctx context.Context, in memory.Change) (memory.Receipt, error) {
	if s.mode == "before-commit" {
		os.Exit(73)
	}
	result, e := s.QueryStore.Commit(ctx, in)
	if e == nil && s.mode == "after-commit" {
		os.Exit(73)
	}
	return result, e
}
func (s crashStore) BindRead(ctx context.Context, in memory.ReadBinding) (memory.ReadBinding, error) {
	if s.mode == "before-binding" {
		os.Exit(73)
	}
	result, e := s.QueryStore.BindRead(ctx, in)
	if e == nil && s.mode == "after-binding" {
		os.Exit(73)
	}
	return result, e
}

type crashPermits struct {
	memory.ReadPermits
	mode string
}

func (p crashPermits) Reserve(ctx context.Context, b memory.Binding, i memory.ReadIntent, m string) (string, error) {
	result, e := p.ReadPermits.Reserve(ctx, b, i, m)
	if e == nil && p.mode == "after-reserve" {
		os.Exit(73)
	}
	return result, e
}
func validMode(mode string) bool {
	switch mode {
	case "before-commit", "after-commit", "after-reserve", "before-binding", "after-binding":
		return true
	}
	return false
}
func RunProbe(ctx context.Context, path, mode string) error {
	if !validMode(mode) {
		return fmt.Errorf("invalid memory probe mode")
	}
	st, e := os.Lstat(path)
	if e != nil || !st.Mode().IsRegular() || st.Mode().Perm() != 0600 || st.Size() > 65536 {
		return fmt.Errorf("invalid memory probe configuration")
	}
	raw, e := os.ReadFile(path)
	if e != nil {
		return e
	}
	var cfg probeConfig
	if json.Unmarshal(raw, &cfg) != nil || filepath.Clean(cfg.Root) != filepath.Dir(filepath.Clean(path)) {
		return fmt.Errorf("invalid memory probe root")
	}
	f, e := openFixture(ctx, cfg)
	if e != nil {
		return e
	}
	defer f.close()
	client, e := f.bind(crashStore{f.store, mode}, crashPermits{f.permits, mode})
	if e != nil {
		return e
	}
	var request *wire.MemoryRequest
	if mode == "before-commit" || mode == "after-commit" {
		request = f.write(cfg.WriteID, 0, "concise")
	} else {
		request = &wire.MemoryRequest{Method: "QUERY", Query: f.query(), GrantMaterial: cfg.Grant}
	}
	if _, e = client.Exchange(ctx, request); e != nil {
		return e
	}
	return fmt.Errorf("memory probe did not exit at the requested boundary")
}
func ProcessCheck(ctx context.Context, mode string) error {
	if !validMode(mode) {
		return fmt.Errorf("invalid memory process case")
	}
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	f, e := newFixture(ctx)
	if e != nil {
		return e
	}
	cfg := f.config
	defer os.RemoveAll(cfg.Root)
	writing := mode == "before-commit" || mode == "after-commit"
	if !writing {
		if e = f.put(ctx); e != nil {
			f.close()
			return e
		}
	}
	f.close()
	path := filepath.Join(cfg.Root, "probe.json")
	executable, e := os.Executable()
	if e != nil {
		return e
	}
	child, cancelChild := context.WithTimeout(ctx, 10*time.Second)
	defer cancelChild()
	var command *exec.Cmd
	if strings.HasSuffix(executable, ".test") {
		command = exec.CommandContext(child, executable, "-test.run=^TestProcessProbe$")
		command.Env = append(os.Environ(), "HARNESS_MEMORY_PROBE="+path, "HARNESS_MEMORY_MODE="+mode)
	} else {
		command = exec.CommandContext(child, executable, "memory-crash-probe", path, mode)
	}
	e = command.Run()
	exit, ok := e.(*exec.ExitError)
	if !ok || exit.ExitCode() != 73 {
		return fmt.Errorf("child did not confirm requested memory boundary")
	}
	f, e = openFixture(ctx, cfg)
	if e != nil {
		return e
	}
	defer f.close()
	if writing {
		state, e := f.client.Exchange(ctx, &wire.MemoryRequest{Method: "LOOKUP", OperationId: cfg.WriteID})
		if e != nil {
			return e
		}
		expected := "unknown"
		if mode == "after-commit" {
			expected = "committed"
		}
		if state.GetOperation().GetState() != expected {
			return fmt.Errorf("recovered commit state does not match interruption boundary")
		}
		if e = f.put(ctx); e != nil {
			return e
		}
		changes, e := f.store.ReadChanges(ctx, "local", "personal", 0, 10)
		if e != nil || len(changes) != 1 || changes[0].Revision != 1 {
			return fmt.Errorf("resuming original operation duplicated a revision")
		}
	} else {
		if e = f.correct(ctx); e != nil {
			return e
		}
	}
	result, e := f.read(ctx)
	if e != nil {
		return e
	}
	expected := uint64(1)
	if !writing && mode != "after-binding" {
		expected = 2
	}
	if len(result.Records) != 1 || result.Records[0].Revision != expected {
		return fmt.Errorf("recovered read did not retain its disclosed selection")
	}
	again, e := f.read(ctx)
	if e != nil || len(again.Records) != 1 || again.Records[0].Revision != expected {
		return fmt.Errorf("read replay changed after recovery")
	}
	intent, e := memory.DescribeQuery(f.binding, f.query())
	if e != nil {
		return e
	}
	p := peer()
	use, e := f.grants.LookupUse(ctx, authorization.GrantPresentation{Namespace: "local", Subject: "alice", Audience: p.Audience, Presenter: p.Presenter, CertificateSHA256: p.CertificateSHA256, OperationID: cfg.ReadID, SemanticSHA256: intent.SemanticSHA256})
	if e != nil {
		return e
	}
	grant, e := f.grants.Get(ctx, cfg.Token, use.GrantID)
	if e != nil {
		return e
	}
	return require(grant.Allocated == 1, "recovery consumed another single-use allocation")
}
