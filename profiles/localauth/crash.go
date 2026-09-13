package localauth

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"google.golang.org/protobuf/proto"
	"lerna/adapters/sqliteauth"
	"lerna/authorization"
	wire "lerna/gen/harness/v1"
)

type probePlan struct {
	Token, Operation string
	Command          []byte
	Now              time.Time
}
type crashStore struct {
	authorization.Store
	operation, phase string
}

func (s crashStore) Commit(ctx context.Context, version uint64, state authorization.State) error {
	_, target := state.Operations[s.operation]
	if target && s.phase == "before" {
		os.Exit(73)
	}
	err := s.Store.Commit(ctx, version, state)
	if target && err == nil && s.phase == "after" {
		os.Exit(73)
	}
	return err
}

// RunProbe is an explicit contractcheck test subprocess, not an authority endpoint.
// The parent creates a disposable 0700 directory and 0600 plan containing test credentials.
func RunProbe(ctx context.Context, root, phase string) error {
	if phase != "before" && phase != "after" {
		return fmt.Errorf("unsupported crash phase")
	}
	data, err := os.ReadFile(filepath.Join(root, "probe.json"))
	if err != nil {
		return err
	}
	var plan probePlan
	if err = json.Unmarshal(data, &plan); err != nil {
		return err
	}
	command := new(wire.AuthorizationCommand)
	if err = proto.Unmarshal(plan.Command, command); err != nil {
		return err
	}
	store, err := sqliteauth.Open(filepath.Join(root, "auth.db"))
	if err != nil {
		return err
	}
	defer store.Close()
	service, err := authorization.New(crashStore{store, plan.Operation, phase}, &clock{plan.Now}, Config())
	if err != nil {
		return err
	}
	_, err = service.Execute(ctx, plan.Token, authorization.Mutation{Namespace: "local", OperationID: plan.Operation, Command: command})
	if err != nil {
		return err
	}
	return fmt.Errorf("crash point not reached")
}
func launch(ctx context.Context, h *harness, executable, id, phase string, command *wire.AuthorizationCommand) error {
	raw, err := proto.Marshal(command)
	if err != nil {
		return err
	}
	plan, err := json.Marshal(probePlan{h.admin, id, raw, h.clock.at})
	if err != nil {
		return err
	}
	if err = os.WriteFile(filepath.Join(h.root, "probe.json"), plan, 0600); err != nil {
		return err
	}
	if err = h.store.Close(); err != nil {
		return err
	}
	h.store = nil
	process := exec.CommandContext(ctx, executable, "auth-crash-probe", h.root, phase)
	output, err := process.CombinedOutput()
	if process.ProcessState == nil || process.ProcessState.ExitCode() != 73 {
		return fmt.Errorf("probe did not stop at commit boundary: %v (%s)", err, output)
	}
	return h.reopen()
}
func crashCheck(ctx context.Context, executable, name string) error {
	h, err := newHarness(ctx, Config())
	if err != nil {
		return err
	}
	defer h.close()
	if err = h.setup(ctx, false); err != nil {
		return err
	}
	client := h.client(h.admin)
	view, err := client.GetPolicy(ctx)
	if err != nil {
		return err
	}
	id, err := client.NewOperation(ctx)
	if err != nil {
		return err
	}
	cmd := resourceCommand("crash-resource")
	cmd.ExpectedRevision = view.Revision
	if name == "before-commit" || name == "lost-commit-reply" {
		phase := "before"
		if name == "lost-commit-reply" {
			phase = "after"
		}
		if err = launch(ctx, h, executable, id, phase, cmd); err != nil {
			return err
		}
		client = h.client(h.admin)
		receipt, err := client.LookupOperation(ctx, id)
		if phase == "before" {
			if err = requireCode(err, authorization.NotFound); err != nil {
				return err
			}
			current, err := client.GetPolicy(ctx)
			if err != nil {
				return err
			}
			if current.Revision != view.Revision {
				return fmt.Errorf("partial commit survived")
			}
		} else {
			if err != nil {
				return err
			}
			if receipt.Revision != view.Revision+1 {
				return fmt.Errorf("wrong committed revision")
			}
		}
		first, err := client.Execute(ctx, id, cmd)
		if err != nil {
			return err
		}
		again, err := client.Execute(ctx, id, cmd)
		if err != nil {
			return err
		}
		if first.Revision != view.Revision+1 || !proto.Equal(first, again) {
			return fmt.Errorf("recovery duplicated mutation")
		}
		return nil
	}
	if _, err = client.Execute(ctx, id, cmd); err != nil {
		return err
	}
	unused, err := client.NewOperation(ctx)
	if err != nil {
		return err
	}
	if _, err = h.apply(ctx, &wire.AuthorizationCommand{Change: &wire.AuthorizationCommand_CloseWindows{CloseWindows: true}}); err != nil {
		return err
	}
	h.clock.at = h.clock.at.Add(3 * time.Minute)
	view, err = client.GetPolicy(ctx)
	if err != nil {
		return err
	}
	cleanupID, err := client.NewOperation(ctx)
	if err != nil {
		return err
	}
	cleanup := &wire.AuthorizationCommand{ExpectedRevision: view.Revision, Change: &wire.AuthorizationCommand_CleanRecords{CleanRecords: true}}
	phase := "before"
	if name == "after-cleanup" {
		phase = "after"
	}
	if err = launch(ctx, h, executable, cleanupID, phase, cleanup); err != nil {
		return err
	}
	client = h.client(h.admin)
	if _, err = client.Execute(ctx, unused, cmd); !authorization.Is(err, authorization.Expired) {
		return fmt.Errorf("closed watermark lost: %v", err)
	}
	_, err = client.LookupOperation(ctx, id)
	if phase == "before" {
		if err != nil {
			return err
		}
		if _, err = client.Execute(ctx, cleanupID, cleanup); err != nil {
			return err
		}
	} else if err = requireCode(err, authorization.Expired); err != nil {
		return err
	}
	if err = h.reopen(); err != nil {
		return err
	}
	client = h.client(h.admin)
	_, err = client.Execute(ctx, id, cmd)
	return requireCode(err, authorization.Expired)
}
