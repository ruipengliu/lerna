package answer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"lerna/adapters/context/checkpointfile"
	sqlitecontext "lerna/adapters/context/sqlite"
	"lerna/contextassembly"
)

// Checkpoint maintenance does not renew a worker or construct a Memory reader.
// The host has loaded the original task from Core before entering this path.
func maintainAnswerCheckpoint(ctx context.Context, root string, cp *answerContextCheckpoint) error {
	if cp.Format != 1 {
		return nil
	}
	store, err := sqlitecontext.Open(filepath.Join(root, "context.db"))
	if err != nil {
		return err
	}
	defer store.Close()
	_, err = migrateAnswerCheckpoint(ctx, root, store, contextKey(cp), cp)
	return err
}

// The caller has already fixed this host's task identity. A file belonging to
// another task is never upgraded against this host's snapshot store.
func migrateAnswerCheckpoint(ctx context.Context, root string, store contextassembly.CheckpointStore, key contextassembly.Key, cp *answerContextCheckpoint) (*answerContextCheckpoint, error) {
	if contextKey(cp) != key {
		return nil, fmt.Errorf("checkpoint belongs to another task")
	}
	if cp.Format != 1 {
		return cp, nil
	}
	err := verifyAnswerCheckpoint(ctx, store, cp)
	if err == contextassembly.Invalidated {
		err = store.VerifyCheckpoint(ctx, key)
		if err == contextassembly.Invalidated {
			err = nil
		}
	}
	if err != nil {
		return nil, err
	}
	next := *cp
	next.Format = 2
	next.SnapshotSHA256 = nil
	next.fileImage = nil
	data, err := json.Marshal(&next)
	if err != nil {
		return nil, err
	}
	files, err := checkpointfile.Open(root)
	if err != nil {
		return nil, err
	}
	defer files.Close()
	if err = files.Replace(ctx, "personalized-checkpoint.json", cp.fileImage, data); err != nil {
		return nil, err
	}
	return &next, nil
}

func cleanAnswerCheckpoint(ctx context.Context, root string, store contextassembly.CheckpointStore, key contextassembly.Key) error {
	cp, err := readAnswerCheckpoint(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	_, err = migrateAnswerCheckpoint(ctx, root, store, key, cp)
	return err
}

func answerCheckpointClean(ctx context.Context, root string, key contextassembly.Key) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	cp, err := readAnswerCheckpoint(root)
	if errors.Is(err, os.ErrNotExist) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	if contextKey(cp) != key {
		return false, fmt.Errorf("checkpoint belongs to another task")
	}
	return cp.Format == 2 && cp.SnapshotSHA256 == nil, nil
}
