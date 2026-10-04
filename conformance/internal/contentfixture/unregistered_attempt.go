package contentfixture

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	v "github.com/ruipengliu/lerna/contract/v1_2"
	d "github.com/ruipengliu/lerna/domain/content"
)

// OwnedUnregisteredAttempt belongs only to this fixture's actual setup effect.
// It is deliberately never admitted as a product publication responsibility.
type OwnedUnregisteredAttempt struct {
	world         *World
	Name          string
	Device, Inode uint64
}

func (w *World) CreateUnregisteredAttempt(ref v.ContentRef, body []byte) OwnedUnregisteredAttempt {
	w.t.Helper()
	if w.closing {
		w.t.Fatal("world closing")
	}
	_, key, err := d.VersionIdentity(ref)
	if err != nil {
		w.t.Fatal(err)
	}
	name := key + ".999.tmp"
	// Register the original owner's exact planned setup path before its effect.
	if err = w.register("fixture_unregistered_attempt_duty " + w.Directory + "/" + name); err != nil {
		w.t.Fatal(err)
	}
	file, err := os.OpenFile(filepath.Join(w.Directory, name), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		w.t.Fatal(err)
	}
	closeFile := w.ownSetupFile(file)
	info, statErr := file.Stat()
	if statErr != nil {
		w.t.Fatal(errors.Join(statErr, closeFile()))
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.Mode().IsRegular() {
		w.t.Fatal(errors.Join(errors.New("original temporary inode unavailable"), closeFile()))
	}
	owned := OwnedUnregisteredAttempt{world: w, Name: name, Device: uint64(stat.Dev), Inode: stat.Ino}
	if err = w.register(fmt.Sprintf("fixture_unregistered_attempt_inode %s/%s %d %d", w.Directory, name, owned.Device, owned.Inode)); err != nil {
		w.t.Fatal(errors.Join(err, closeFile()))
	}
	n, writeErr := file.Write(body)
	if writeErr == nil && n != len(body) {
		writeErr = errors.New("exact setup temporary short write")
	}
	if err = errors.Join(writeErr, file.Sync(), closeFile()); err != nil {
		w.t.Fatal(err)
	}
	w.syncExactFaultDirectory()
	return owned
}

// Remove is an explicit owner recovery operation on this newly created setup
// inode. Product cleanup cannot call it or infer responsibility from the name.
func (a OwnedUnregisteredAttempt) Remove() {
	w := a.world
	w.t.Helper()
	if w.closing {
		w.t.Fatal("world closing")
	}
	info, err := os.Lstat(filepath.Join(w.Directory, a.Name))
	if err != nil {
		w.t.Fatal(err)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.Mode().IsRegular() || uint64(stat.Dev) != a.Device || stat.Ino != a.Inode {
		w.t.Fatal("original owner temporary inode changed")
	}
	if err = w.register(fmt.Sprintf("fixture_unregistered_attempt_owner_removal %s/%s %d %d", w.Directory, a.Name, a.Device, a.Inode)); err != nil {
		w.t.Fatal(err)
	}
	if err = os.Remove(filepath.Join(w.Directory, a.Name)); err != nil {
		w.t.Fatal(err)
	}
	w.syncExactFaultDirectory()
}
