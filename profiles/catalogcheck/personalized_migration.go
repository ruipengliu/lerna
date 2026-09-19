package catalogcheck

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"

	"lerna/adapters/context/checkpointfile"
	"lerna/contextassembly"
	"lerna/tasks"
)

func migrateActionCheckpoint(ctx context.Context, root string, store checkpointSnapshots, cp actionContextCheckpoint, run tasks.RunSnapshot) (actionContextCheckpoint, error) {
	if cp.Format != 1 {
		return cp, nil
	}
	if err := verifyOriginalActions(run, cp); err != nil {
		return cp, err
	}
	// The current comparison and every history entry must agree with the
	// original store, or have durable proof of retirement and comparison erasure.
	if _, err := verifyCheckpointHistory(ctx, store, cp, run, true); err != nil {
		return cp, err
	}
	err := verifyActionCheckpoint(ctx, store, cp)
	if err == contextassembly.Invalidated {
		err = store.VerifyCheckpoint(ctx, cp.Binding.Key)
		if err == contextassembly.Invalidated {
			err = nil
		}
	}
	if err != nil {
		return cp, err
	}
	next := cp
	next.Format = 2
	next.SnapshotSHA, next.ContextDigests, next.fileImage = nil, nil, nil
	next.BoundContexts = nil
	if len(cp.ContextDigests) == 0 {
		next.BoundContexts = []uint32{uint32(cp.Binding.Key.Decision)}
	} else {
		for n := range cp.ContextDigests {
			next.BoundContexts = append(next.BoundContexts, n)
		}
		sort.Slice(next.BoundContexts, func(i, j int) bool { return next.BoundContexts[i] < next.BoundContexts[j] })
	}
	raw, err := json.Marshal(next)
	if err != nil {
		return cp, err
	}
	files, err := checkpointfile.Open(root)
	if err != nil {
		return cp, err
	}
	defer files.Close()
	if err = files.Replace(ctx, "action-context.json", cp.fileImage, raw); err != nil {
		return cp, err
	}
	return next, nil
}

func cleanActionCheckpoint(ctx context.Context, root string, store checkpointSnapshots, core *tasks.Service, ref tasks.Ref) error {
	cp, err := readActionContext(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if cp.Baseline.Task.Ref != ref || cp.Binding.Key.Namespace != ref.Namespace || cp.Binding.Key.TaskID != ref.TaskID {
		return fmt.Errorf("checkpoint belongs to another task")
	}
	if cp.Format == 2 {
		return nil
	}
	run, err := core.Load(ctx, ref)
	if err != nil {
		return err
	}
	_, err = migrateActionCheckpoint(ctx, root, store, cp, run)
	return err
}

func actionCheckpointClean(ctx context.Context, root string, ref tasks.Ref) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	cp, err := readActionContext(root)
	if errors.Is(err, os.ErrNotExist) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	if cp.Baseline.Task.Ref != ref || cp.Binding.Key.Namespace != ref.Namespace || cp.Binding.Key.TaskID != ref.TaskID {
		return false, fmt.Errorf("checkpoint belongs to another task")
	}
	return cp.Format == 2 && cp.SnapshotSHA == nil && len(cp.ContextDigests) == 0, nil
}
