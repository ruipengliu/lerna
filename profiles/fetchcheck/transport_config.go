package fetchcheck

import (
	"bytes"
	"context"
	"encoding/json"
	"golang.org/x/sys/unix"
	"io"
	"lerna/fetch"
	"os"
	"path/filepath"
)

// A run directory keeps its original transport scope. This manifest grants no
// permission; the adapter still checks current URL, network and source access.
// Missing, partial or changed manifests fail closed rather than being reseeded
// from the configuration supplied by a recovering host.
func bindTransportConfig(ctx context.Context, root string, initial bool, urls []string, network networkConfig, pageBytes uint32) error {
	if pageBytes == 1024 {
		pageBytes = 0
	}
	return bindRunConfiguration(ctx, root, "transport-config.json", initial, struct {
		Version      int
		URLs         []string
		Network      networkConfig
		PageMaxBytes uint32 `json:",omitempty"`
	}{1, urls, network, pageBytes})
}

// Names are a fixed host-owned inventory, never supplied by a task or peer.
func bindRunConfiguration(ctx context.Context, root, name string, initial bool, value any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	raw, err := json.Marshal(value)
	const maxManifest = 1 << 20
	if err != nil || len(raw) > maxManifest {
		return fetch.Invalid
	}
	path := filepath.Join(root, name)
	if initial {
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return fetch.Invalid
		}
		defer f.Close()
		if n, err := f.Write(raw); err != nil || n != len(raw) {
			return fetch.Unavailable
		}
		if err := f.Sync(); err != nil {
			return fetch.Unavailable
		}
		dir, err := os.Open(root)
		if err != nil {
			return fetch.Unavailable
		}
		defer dir.Close()
		if err := dir.Sync(); err != nil {
			return fetch.Unavailable
		}
		return nil
	}
	saved, err := readRunConfiguration(ctx, root, name)
	if err != nil {
		return err
	}
	if !bytes.Equal(raw, saved) {
		return fetch.Invalid
	}
	return nil
}

func readRunConfiguration(ctx context.Context, root, name string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	const maxManifest = 1 << 20
	f, err := os.OpenFile(filepath.Join(root, name), os.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, fetch.Invalid
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || info.Size() > maxManifest {
		return nil, fetch.Invalid
	}
	saved, err := io.ReadAll(io.LimitReader(f, maxManifest+1))
	if err != nil {
		return nil, fetch.Unavailable
	}
	if len(saved) > maxManifest {
		return nil, fetch.Invalid
	}
	return saved, nil
}
