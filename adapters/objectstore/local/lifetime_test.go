//go:build linux && integration

package local

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

func ownedLocalRoot(t *testing.T) (string, func()) {
	confirmed := false
	t.Helper()
	registry := os.Getenv("LERNA_TEST_OWNED_SCOPE_REGISTRY")
	if !filepath.IsAbs(registry) || !filepath.IsAbs(os.Getenv("TMPDIR")) {
		t.Fatal("absolute owned registry/TMPDIR required")
	}
	root, err := os.MkdirTemp("", "lerna-local-lifetime-")
	if err != nil {
		t.Fatal(err)
	}
	dir, err := os.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	info, err := dir.Stat()
	if err != nil {
		t.Fatal(err)
	}
	st := info.Sys().(*syscall.Stat_t)
	parent, err := os.Open(filepath.Dir(root))
	if err != nil {
		t.Fatal(err)
	}
	if err = errors.Join(dir.Sync(), parent.Sync(), dir.Close(), parent.Close()); err != nil {
		t.Fatal(err)
	}
	ledger, err := os.OpenFile(registry, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, err = fmt.Fprintf(ledger, "objects %s %d %d\n", root, st.Dev, st.Ino)
	if err = errors.Join(err, ledger.Sync(), ledger.Close()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if !confirmed {
			t.Log("native holder exit unconfirmed; retain exact root", root)
			return
		}
		info, err := os.Lstat(root)
		if err != nil {
			t.Error(err)
			return
		}
		got := info.Sys().(*syscall.Stat_t)
		if got.Dev != st.Dev || got.Ino != st.Ino {
			t.Error("owned root identity changed")
			return
		}
		if err = os.RemoveAll(root); err != nil {
			t.Error(err)
		}
	})
	return root, func() { confirmed = true }
}
func TestPartialOpenCloseUnknownReturnsRetainableHolder(t *testing.T) {
	root, confirmed := ownedLocalRoot(t)
	normal, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	if err = normal.Close(); err != nil {
		t.Fatal(err)
	}
	injected := errors.New("mechanical close unknown")
	openFailure := errors.New("mechanical directory open failure")
	ops := actualNative()
	ops.openDirectory = func(*os.Root) (*os.File, error) { return nil, openFailure }
	actuallyClosed := false
	ops.closeRoot = func(root *os.Root) error {
		err := root.Close()
		actuallyClosed = err == nil
		return errors.Join(err, injected)
	}
	holder, err := open(root, ops)
	if actuallyClosed {
		confirmed()
	}
	if !errors.Is(err, openFailure) || !errors.Is(err, injected) || !actuallyClosed {
		t.Fatal("mechanical control did not close actual root and retain both causes")
	}
	if holder == nil {
		t.Fatal("partial Open discarded unconfirmed native holder")
	}
	if err = holder.Close(); !errors.Is(err, injected) {
		t.Fatal("later Close washed away original unknown")
	}
	// Independent hook observation confirms the real descriptor closed; this is
	// an injected diagnostic, not evidence of an actual native close failure.
}

func TestChildCloseUnknownStaysStickyButOrdinaryReadFailureDoesNot(t *testing.T) {
	root, confirmed := ownedLocalRoot(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	key := strings.Repeat("a", 64)
	hash := "sha256:b6a98d9ce9a2d9149288fa3df42d377c3e42737afdcdaf714e33c0a100b51060"
	normal, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	if err = normal.Put(ctx, key, key+".1.tmp", hash, 6, []byte("alpha\n")); err != nil {
		t.Fatal(err)
	}
	if err = normal.Close(); err != nil {
		t.Fatal(err)
	}
	ops := actualNative()
	injected := errors.New("mechanical child close unknown")
	actuallyClosed := false
	rootClosed := false
	directoryClosed := false
	ops.closeRoot = func(root *os.Root) error { err := root.Close(); rootClosed = err == nil; return err }
	ops.closeFile = func(file *os.File) error {
		isObject := strings.HasSuffix(file.Name(), key)
		err := file.Close()
		if isObject {
			actuallyClosed = err == nil
			return errors.Join(err, injected)
		}
		directoryClosed = err == nil
		return err
	}
	store, err := open(root, ops)
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.Read(ctx, key, hash, 6)
	if !errors.Is(err, injected) || !actuallyClosed {
		t.Fatal("mechanical read close did not preserve cause")
	}
	for i := 0; i < 2; i++ {
		if err = store.Close(); !errors.Is(err, injected) {
			t.Fatal("later Store.Close washed away child unknown")
		}
	}
	ordinary, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	_, err = ordinary.Read(ctx, strings.Repeat("b", 64), hash, 6)
	if !errors.Is(err, ErrUnavailable) {
		t.Fatal("missing object control did not fail")
	}
	if err = ordinary.Close(); err != nil {
		t.Fatal("confirmed ordinary I/O failure poisoned native closure", err)
	}
	if !rootClosed || !directoryClosed {
		t.Fatal("mechanical injection did not independently confirm real root/directory closure")
	}
	confirmed()
}
func TestCloseDrainTimeoutRetainsActiveInvocationUntilRealReturn(t *testing.T) {
	root, confirmed := ownedLocalRoot(t)
	ops := actualNative()
	entered, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	ops.syncFile = func(file *os.File) error { close(entered); <-release; return file.Sync() }
	store, err := open(root, ops)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	key := strings.Repeat("a", 64)
	done := make(chan error, 1)
	doneEvent := make(chan struct{})
	go func() {
		defer close(doneEvent)
		done <- store.Put(ctx, key, key+".1.tmp", "sha256:b6a98d9ce9a2d9149288fa3df42d377c3e42737afdcdaf714e33c0a100b51060", 6, []byte("alpha\n"))
	}()
	t.Cleanup(func() {
		unblock()
		cleanup, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		select {
		case <-doneEvent:
		case <-cleanup.Done():
			t.Error("retain exact active local invocation")
			return
		}
		if err := store.CloseContext(cleanup); err != nil {
			t.Error(err)
			return
		}
		confirmed()
	})
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	wait, cancelWait := context.WithTimeout(ctx, 20*time.Millisecond)
	_, err = store.Read(wait, key, "sha256:b6a98d9ce9a2d9149288fa3df42d377c3e42737afdcdaf714e33c0a100b51060", 6)
	cancelWait()
	if !errors.Is(err, context.DeadlineExceeded) {
		unblock()
		<-done
		t.Fatal("gate wait ignored finite read context", err)
	}
	drain, cancelDrain := context.WithTimeout(ctx, 20*time.Millisecond)
	err = store.CloseContext(drain)
	cancelDrain()
	if !errors.Is(err, ErrCloseTimeout) {
		unblock()
		<-done
		t.Fatal("active invocation was treated as native closed", err)
	}
	select {
	case <-done:
		unblock()
		t.Fatal("held operation lost ownership before release")
	default:
	}
	unblock()
	select {
	case err = <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("actual owned invocation exit unconfirmed")
	}
	if err = store.Close(); err != nil {
		t.Fatal("pre-drain timeout did not allow confirmed closure after real exit", err)
	}
}
