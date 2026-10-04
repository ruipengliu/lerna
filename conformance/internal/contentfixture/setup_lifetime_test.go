//go:build integration

package contentfixture

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestRegistryFirstCloseUnknownRetainsExactScope(t *testing.T) {
	for _, kind := range []string{"normal", "ledger", "parent"} {
		t.Run(kind, func(t *testing.T) {
			dir, err := os.MkdirTemp("", "lerna-content-fixture-close-")
			if err != nil {
				t.Fatal(err)
			}
			info, err := os.Lstat(dir)
			if err != nil {
				t.Fatal(err)
			}
			st := info.Sys().(*syscall.Stat_t)
			w := &World{t: t, Directory: dir, device: uint64(st.Dev), inode: st.Ino}
			directory, err := os.Open(dir)
			if err != nil {
				t.Fatal(err)
			}
			closeDir := w.ownSetupFile(directory)
			parent, err := os.Open(filepath.Dir(dir))
			if err != nil {
				t.Fatal(errors.Join(err, closeDir()))
			}
			closeParent := w.ownSetupFile(parent)
			if err = errors.Join(directory.Sync(), parent.Sync(), closeDir(), closeParent()); err != nil {
				t.Fatal(err)
			}
			if err = w.register(fmt.Sprintf("objects %s %d %d", dir, st.Dev, st.Ino)); err != nil {
				t.Fatal(err)
			}
			diagnostic := errors.New("mechanical setup Close diagnostic")
			actualClosed := false
			if kind != "normal" {
				w.setupNativeClose = func(file *os.File) error {
					info, statErr := file.Stat()
					closeErr := file.Close()
					if statErr != nil || closeErr != nil {
						return errors.Join(statErr, closeErr)
					}
					if (kind == "parent" && info.IsDir()) || (kind == "ledger" && !info.IsDir()) {
						actualClosed = true
						return diagnostic
					}
					return nil
				}
			}
			err = w.register("fixture_close_control " + dir)
			if kind == "normal" {
				if err != nil {
					t.Fatal(err)
				}
				if err = w.Cleanup(); err != nil {
					t.Fatal(err)
				}
				if _, err = os.Lstat(dir); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("normal exact root retained", err)
				}
				return
			}
			if !errors.Is(err, diagnostic) || !actualClosed {
				t.Fatal("mechanical first-close control failed", err)
			}
			for i := 0; i < 2; i++ {
				if err = w.Cleanup(); !errors.Is(err, diagnostic) {
					t.Fatal("later cleanup washed away first Close", err)
				}
			}
			got, err := os.Lstat(dir)
			if err != nil {
				t.Fatal("unknown Close deleted scope", err)
			}
			actual := got.Sys().(*syscall.Stat_t)
			if actual.Dev != st.Dev || actual.Ino != st.Ino {
				t.Fatal("exact fixture identity changed")
			}
			// The original injected hook independently observed successful actual Close;
			// all other setup FDs have actual first Close success. This fixture alone may
			// remove that exact root. World keeps its first unknown diagnostic unchanged.
			if err = os.RemoveAll(dir); err != nil {
				t.Fatal(err)
			}
		})
	}
}
