//go:build linux

package local

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	d "github.com/ruipengliu/lerna/domain/content"
	"io"
	"os"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Stable lock names are permanent metadata: unlinking one could make two
// inode locks protect the same object. All protocol users retain it forever.
func (s *Store) lockKey(ctx context.Context, key string, operation int) (func() error, error) {
	file, err := s.root.OpenFile(key+".lock", os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0600)
	if err != nil {
		return nil, errors.Join(ErrUnavailable, err)
	}
	closeFile := s.track(file)
	s.mu.Lock()
	s.keyLocks[file] = true
	s.mu.Unlock()
	info, statErr := file.Stat()
	pathInfo, pathErr := s.root.Lstat(key + ".lock")
	if statErr != nil || pathErr != nil || !info.Mode().IsRegular() || !os.SameFile(info, pathInfo) {
		return nil, errors.Join(ErrUnavailable, statErr, pathErr, closeFile())
	}
	if err = errors.Join(file.Sync(), s.directory.Sync()); err != nil {
		return nil, errors.Join(ErrUnavailable, err, closeFile())
	}
	for {
		if err = ctx.Err(); err != nil {
			return nil, errors.Join(err, closeFile())
		}
		err = syscall.Flock(int(file.Fd()), operation|syscall.LOCK_NB)
		if err == nil {
			break
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EAGAIN) && !errors.Is(err, syscall.EINTR) {
			return nil, errors.Join(ErrUnavailable, err, closeFile())
		}
		timer := time.NewTimer(time.Millisecond)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return nil, errors.Join(ctx.Err(), closeFile())
		case <-timer.C:
		}
	}
	var once sync.Once
	var closeErr error
	return func() error {
		once.Do(func() {
			s.mu.Lock()
			unknown := s.childCloseErr
			s.mu.Unlock()
			if unknown != nil {
				closeErr = errors.Join(ErrUnavailable, unknown)
				return
			}
			// Closing this actual descriptor releases flock only after all body
			// descriptors have positively closed. There is no separate unlock.
			closeErr = closeFile()
		})
		return closeErr
	}, nil
}

func (s *Store) requireOpenKey(key string) error {
	for _, name := range []string{key + ".sealed", key + ".sealed.pending"} {
		_, err := s.root.Lstat(name)
		if err == nil {
			return d.ErrBodySealed
		}
		if !errors.Is(err, os.ErrNotExist) {
			return errors.Join(ErrUnavailable, err)
		}
	}
	return nil
}

func sealBytes(identity d.ErasureIdentity) ([]byte, error) {
	_, key, err := d.VersionIdentity(identity.Ref)
	if err != nil {
		return nil, err
	}
	if key != identity.ObjectKey || identity.HolderID == "" || len(identity.HolderID) > 128 || identity.SealID == "" || len(identity.SealID) > 128 {
		return nil, ErrUnavailable
	}
	return json.Marshal(struct {
		Protocol string            `json:"protocol"`
		Identity d.ErasureIdentity `json:"identity"`
	}{"lerna-content-seal-1", identity})
}

func (s *Store) readSeal(identity d.ErasureIdentity, expected []byte) error {
	actual, err := s.read(identity.ObjectKey + ".sealed")
	if err != nil {
		return err
	}
	if !bytes.Equal(actual, expected) {
		return ErrIntegrity
	}
	return nil
}

func (s *Store) installSeal(ctx context.Context, identity d.ErasureIdentity, expected []byte) error {
	key := identity.ObjectKey
	if _, err := s.root.Lstat(key + ".sealed"); err == nil {
		if err = s.readSeal(identity, expected); err != nil {
			return err
		}
		// A previous successful installation may have lost its directory Sync
		// response. Establish it now, never infer it just from existence.
		return errors.Join(s.directory.Sync(), ctx.Err())
	} else if !errors.Is(err, os.ErrNotExist) {
		return errors.Join(ErrUnavailable, err)
	}
	file, err := s.root.OpenFile(key+".sealed.pending", os.O_CREATE|os.O_EXCL|os.O_WRONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0600)
	if errors.Is(err, os.ErrExist) {
		// A partial marker stays closed. Only the original complete bytes can
		// be adopted; an ambiguous partial identity retains unknown ownership.
		actual, readErr := s.read(key + ".sealed.pending")
		if readErr != nil || !bytes.Equal(actual, expected) {
			return errors.Join(ErrUnavailable, readErr, ErrIntegrity)
		}
		file, err = s.root.OpenFile(key+".sealed.pending", os.O_WRONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
		if err != nil {
			return errors.Join(ErrUnavailable, err)
		}
	} else if err != nil {
		return errors.Join(ErrUnavailable, err)
	}
	closeFile := s.track(file)
	info, statErr := file.Stat()
	if statErr != nil || !info.Mode().IsRegular() {
		return errors.Join(ErrUnavailable, statErr, closeFile())
	}
	if _, err = file.WriteAt(expected, 0); err == nil {
		err = file.Sync()
	}
	err = errors.Join(err, closeFile())
	if err != nil {
		return errors.Join(ErrUnavailable, err)
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = s.root.Link(key+".sealed.pending", key+".sealed"); err != nil && !errors.Is(err, os.ErrExist) {
		return errors.Join(ErrUnavailable, err)
	}
	if err = s.readSeal(identity, expected); err != nil {
		return err
	}
	if err = s.directory.Sync(); err != nil {
		return errors.Join(ErrUnavailable, err)
	}
	if err = s.root.Remove(key + ".sealed.pending"); err != nil && !errors.Is(err, os.ErrNotExist) {
		return errors.Join(ErrUnavailable, err)
	}
	return errors.Join(s.directory.Sync(), ctx.Err())
}

func (s *Store) FenceAndErase(ctx context.Context, identity d.ErasureIdentity, attempts []string) (observed d.ErasureObservation, returnErr error) {
	observed.Identity = identity
	expected, err := sealBytes(identity)
	if err != nil {
		return observed, err
	}
	if len(attempts) > 64 {
		return observed, ErrUnavailable
	}
	seen := map[string]bool{}
	for _, attempt := range attempts {
		if !temporaryPattern.MatchString(attempt) || !strings.HasPrefix(attempt, identity.ObjectKey+".") || seen[attempt] {
			return observed, ErrUnavailable
		}
		seen[attempt] = true
	}
	if err = s.acquire(ctx); err != nil {
		return observed, err
	}
	defer s.release()
	finishLock, err := s.lockKey(ctx, identity.ObjectKey, syscall.LOCK_EX)
	if err != nil {
		return observed, err
	}
	defer func() {
		if err := finishLock(); err != nil {
			observed.Erased = false
			returnErr = errors.Join(returnErr, err)
		}
	}()
	if err = s.installSeal(ctx, identity, expected); err != nil {
		return observed, err
	}
	observed.Fenced = true
	for _, name := range append([]string{identity.ObjectKey}, attempts...) {
		if err = ctx.Err(); err != nil {
			return observed, err
		}
		if err = s.root.Remove(name); err != nil && !errors.Is(err, os.ErrNotExist) {
			return observed, errors.Join(ErrUnavailable, err)
		}
	}
	if err = s.directory.Sync(); err != nil {
		return observed, errors.Join(ErrUnavailable, err)
	}
	// Reopen actual directory/body facts under the same effect lock. Presence
	// of an unregistered temporary name is residual, never deletion authority.
	return s.observeLocked(ctx, identity, expected, "", 64)
}

func (s *Store) ObserveErasure(ctx context.Context, identity d.ErasureIdentity, cursor string, limit int) (observed d.ErasureObservation, returnErr error) {
	observed.Identity = identity
	expected, err := sealBytes(identity)
	if err != nil {
		return observed, err
	}
	if limit < 1 || limit > 64 || cursor != "" && cursor != identity.ObjectKey && (!temporaryPattern.MatchString(cursor) || !strings.HasPrefix(cursor, identity.ObjectKey+".")) {
		return observed, ErrUnavailable
	}
	if err = s.acquire(ctx); err != nil {
		return observed, err
	}
	defer s.release()
	finishLock, err := s.lockKey(ctx, identity.ObjectKey, syscall.LOCK_SH)
	if err != nil {
		return observed, err
	}
	defer func() {
		if err := finishLock(); err != nil {
			observed.Erased = false
			returnErr = errors.Join(returnErr, err)
		}
	}()
	return s.observeLocked(ctx, identity, expected, cursor, limit)
}

func (s *Store) observeLocked(ctx context.Context, identity d.ErasureIdentity, expected []byte, cursor string, limit int) (d.ErasureObservation, error) {
	observed := d.ErasureObservation{Identity: identity, Residual: []string{}}
	if err := s.readSeal(identity, expected); err != nil {
		return observed, err
	}
	// Sync the independently reopened marker itself. Existence is insufficient
	// to confirm a durable fence after an unknown previous file/dir Sync.
	marker, err := s.root.OpenFile(identity.ObjectKey+".sealed", os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return observed, errors.Join(ErrUnavailable, err)
	}
	closeMarker := s.track(marker)
	if err = errors.Join(marker.Sync(), closeMarker(), s.directory.Sync()); err != nil {
		return observed, errors.Join(ErrUnavailable, err)
	}
	observed.Fenced = true
	directory, err := s.root.Open(".")
	if err != nil {
		return observed, errors.Join(ErrUnavailable, err)
	}
	closeDirectory := s.track(directory)
	residual := false
	for {
		if err = ctx.Err(); err != nil {
			return observed, errors.Join(err, closeDirectory())
		}
		entries, readErr := directory.ReadDir(64)
		for _, entry := range entries {
			name := entry.Name()
			if name != identity.ObjectKey && (!temporaryPattern.MatchString(name) || !strings.HasPrefix(name, identity.ObjectKey+".")) {
				continue
			}
			residual = true
			if name <= cursor {
				continue
			}
			observed.Residual = append(observed.Residual, name)
			sort.Strings(observed.Residual)
			if len(observed.Residual) > limit+1 {
				observed.Residual = observed.Residual[:limit+1]
			}
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return observed, errors.Join(ErrUnavailable, readErr, closeDirectory())
		}
	}
	if err = closeDirectory(); err != nil {
		return observed, errors.Join(ErrUnavailable, err)
	}
	if len(observed.Residual) > limit {
		observed.Residual = observed.Residual[:limit]
		observed.NextCursor = observed.Residual[len(observed.Residual)-1]
	}
	observed.Erased = !residual
	return observed, ctx.Err()
}
