package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"io"
	"net/url"
	"os"
	"path/filepath"

	"github.com/ruipengliu/lerna/contracts/command"
)

var databaseSuffixes = [...]string{"", "-wal", "-shm", "-journal"}

type qualificationFile struct {
	suffix string
	info   os.FileInfo
	digest string
}
type qualificationReader struct {
	ctx context.Context
	r   io.Reader
}

func (r qualificationReader) Read(b []byte) (int, error) {
	if e := r.ctx.Err(); e != nil {
		return 0, e
	}
	return r.r.Read(b)
}

// qualifyOriginal 不在未知原文件上打开 SQLite，热日志恢复和检查点只作用于私有资格副本。
func qualifyOriginal(ctx context.Context, path, user, domain string) ([]qualificationFile, error) {
	dir, e := os.MkdirTemp("", "lerna-format-qualification-")
	if e != nil {
		return nil, e
	}
	defer os.RemoveAll(dir)
	qualified := filepath.Join(dir, "state.db")
	var files []qualificationFile
	for _, suffix := range databaseSuffixes {
		info, e := os.Lstat(path + suffix)
		if errors.Is(e, os.ErrNotExist) {
			files = append(files, qualificationFile{suffix: suffix})
			continue
		}
		if e != nil {
			return nil, e
		}
		if !info.Mode().IsRegular() {
			return nil, command.Fail("UNSUPPORTED_DATABASE_FORMAT")
		}
		source, e := os.Open(path + suffix)
		if e != nil {
			return nil, e
		}
		target, e := os.OpenFile(qualified+suffix, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if e != nil {
			source.Close()
			return nil, e
		}
		hash := sha256.New()
		_, copyErr := io.Copy(io.MultiWriter(target, hash), qualificationReader{ctx, source})
		e = errors.Join(copyErr, source.Close(), target.Close())
		if e != nil {
			return nil, e
		}
		files = append(files, qualificationFile{suffix: suffix, info: info, digest: hex.EncodeToString(hash.Sum(nil))})
	}
	if files[0].info == nil {
		for _, file := range files[1:] {
			if file.info != nil {
				return nil, command.Fail("UNSUPPORTED_DATABASE_FORMAT")
			}
		}
	}
	db, e := sql.Open("sqlite3", (&url.URL{Scheme: "file", Path: qualified}).String()+"?_busy_timeout=0"+qualificationVFSQuery())
	if e != nil {
		return nil, e
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	conn, e := db.Conn(ctx)
	if e != nil {
		return nil, e
	}
	defer conn.Close()
	probe := &Store{db: db, conn: conn, user: user, domain: domain}
	empty, e := probe.checkFormat(ctx, conn)
	if e != nil {
		return nil, e
	}
	if !empty {
		if e = probe.checkSavedContracts(ctx, conn); e != nil {
			return nil, e
		}
	}
	if e = verifyQualification(ctx, path, files); e != nil {
		return nil, e
	}
	return files, nil
}

func verifyQualification(ctx context.Context, path string, files []qualificationFile) error {
	for _, file := range files {
		info, e := os.Lstat(path + file.suffix)
		if errors.Is(e, os.ErrNotExist) && file.info == nil {
			continue
		}
		if e != nil || file.info == nil || !info.Mode().IsRegular() || !os.SameFile(info, file.info) || info.Size() != file.info.Size() {
			return command.Fail("DEPENDENCY_UNAVAILABLE")
		}
		f, e := os.Open(path + file.suffix)
		if e != nil {
			return e
		}
		hash := sha256.New()
		_, readErr := io.Copy(hash, qualificationReader{ctx, f})
		e = errors.Join(readErr, f.Close())
		if e != nil {
			return e
		}
		if file.digest != hex.EncodeToString(hash.Sum(nil)) {
			return command.Fail("DEPENDENCY_UNAVAILABLE")
		}
	}
	return nil
}
