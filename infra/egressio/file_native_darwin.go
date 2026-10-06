//go:build darwin

package egressio

/*
#include <sys/types.h>
#include <sys/stat.h>
#include <sys/mount.h>
#include <sys/utsname.h>
#include <sys/sysctl.h>
#include <fcntl.h>
#include <unistd.h>
#include <stdlib.h>
#include <stdio.h>
#include <errno.h>
static int file_openat(int parent,const char *name,int flags,int mode){return openat(parent,name,flags|O_NOFOLLOW|O_CLOEXEC|O_NONBLOCK,mode);}
static int file_renameat(int parent,const char *from,const char *to){return renameat(parent,from,parent,to);}
static int file_unlinkat(int parent,const char *name){return unlinkat(parent,name,0);}
static void file_platform(int fd,char *result,int length){
 struct statfs fs;struct utsname os;char product[64],build[64];size_t pn=sizeof(product),bn=sizeof(build);
 if(fstatfs(fd,&fs)!=0 || uname(&os)!=0 || sysctlbyname("kern.osproductversion",product,&pn,0,0)!=0 || sysctlbyname("kern.osversion",build,&bn,0,0)!=0){result[0]=0;return;}
 snprintf(result,length,"%s/%s/%s/%s/%s",product,build,os.release,os.machine,fs.f_fstypename);
}
*/
import "C"

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"github.com/ruipengliu/lerna/contracts/command"
)

type nativeDirectory struct {
	file           *os.File
	parent         *os.File
	name, identity string
}
type nativeRoot struct {
	ctx                context.Context
	chain              []nativeDirectory
	dirs               map[string]*os.File
	platform, identity string
}

func fileOpenAt(parent *os.File, name string, flags int, mode uint32) (*os.File, error) {
	p := C.CString(name)
	defer C.free(unsafe.Pointer(p))
	fd, e := C.file_openat(C.int(parent.Fd()), p, C.int(flags), C.int(mode))
	if fd < 0 {
		return nil, e
	}
	return os.NewFile(uintptr(fd), name), nil
}
func fileIdentity(f *os.File) (string, error) {
	i, e := f.Stat()
	if e != nil {
		return "", e
	}
	s, ok := i.Sys().(*syscall.Stat_t)
	if !ok {
		return "", command.Fail("FILE_IDENTITY_UNAVAILABLE")
	}
	return fmt.Sprintf("%d:%d:%d:%d:%d", s.Dev, s.Ino, s.Gen, s.Birthtimespec.Sec, s.Birthtimespec.Nsec), nil
}
func fileRegular(f *os.File) error {
	i, e := f.Stat()
	if e != nil {
		return e
	}
	s, ok := i.Sys().(*syscall.Stat_t)
	if !ok || !i.Mode().IsRegular() || s.Nlink != 1 || s.Uid != uint32(os.Geteuid()) || i.Mode().Perm() != 0600 {
		return command.Fail("FILE_TYPE_UNSUPPORTED")
	}
	return nil
}
func openNativeRoot(ctx context.Context, path string) (r *nativeRoot, err error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || path == "/" {
		return nil, command.Fail("FILE_ROOT_INVALID")
	}
	r = &nativeRoot{ctx: ctx, dirs: map[string]*os.File{}}
	defer func() {
		if err != nil {
			r.close()
		}
	}()
	base, e := os.Open("/")
	if e != nil {
		return r, e
	}
	id, e := fileIdentity(base)
	if e != nil {
		base.Close()
		return r, e
	}
	r.chain = append(r.chain, nativeDirectory{file: base, identity: id})
	for _, part := range strings.Split(strings.TrimPrefix(path, "/"), "/") {
		f, e := fileOpenAt(base, part, syscall.O_RDONLY|syscall.O_DIRECTORY, 0)
		if e != nil {
			return r, e
		}
		id, e := fileIdentity(f)
		if e != nil {
			f.Close()
			return r, e
		}
		r.chain = append(r.chain, nativeDirectory{file: f, parent: base, name: part, identity: id})
		base = f
	}
	rootInfo, e := base.Stat()
	if e != nil {
		return r, e
	}
	if st, ok := rootInfo.Sys().(*syscall.Stat_t); !ok || st.Uid != uint32(os.Geteuid()) || rootInfo.Mode().Perm() != 0700 {
		return r, command.Fail("FILE_ROOT_NOT_ISOLATED")
	}
	r.identity = r.chain[len(r.chain)-1].identity
	var platform [256]C.char
	C.file_platform(C.int(base.Fd()), &platform[0], C.int(len(platform)))
	r.platform = C.GoString(&platform[0])
	if r.platform != "27.0.1/26A434/27.0.0/arm64/apfs" {
		return r, command.Fail("FILE_PLATFORM_UNQUALIFIED")
	}
	r.dirs[""] = base
	for _, name := range []string{"objects", "commits", "locks"} {
		f, e := fileOpenAt(base, name, syscall.O_RDONLY|syscall.O_DIRECTORY, 0)
		if e != nil {
			return r, e
		}
		id, e := fileIdentity(f)
		if e != nil {
			f.Close()
			return r, e
		}
		if strings.SplitN(id, ":", 2)[0] != strings.SplitN(r.identity, ":", 2)[0] {
			f.Close()
			return r, command.Fail("FILE_CROSS_DEVICE")
		}
		info, e := f.Stat()
		if e != nil {
			f.Close()
			return r, e
		}
		if st, ok := info.Sys().(*syscall.Stat_t); !ok || st.Uid != uint32(os.Geteuid()) || info.Mode().Perm() != 0700 {
			f.Close()
			return r, command.Fail("FILE_ROOT_NOT_ISOLATED")
		}
		r.chain = append(r.chain, nativeDirectory{file: f, parent: base, name: name, identity: id})
		r.dirs[name] = f
	}
	// 命名空间的三个目录同样绑定；重建任意一个都不能继承旧资源身份。
	for _, name := range []string{"objects", "commits", "locks"} {
		id, e := fileIdentity(r.dirs[name])
		if e != nil {
			return r, e
		}
		r.identity += "|" + name + "=" + id
	}
	return r, nil
}
func (r *nativeRoot) close() {
	for i := len(r.chain) - 1; i >= 0; i-- {
		_ = r.chain[i].file.Close()
	}
}
func (r *nativeRoot) validate() error {
	if e := r.ctx.Err(); e != nil {
		return e
	}
	for _, d := range r.chain {
		if d.parent == nil {
			continue
		}
		f, e := fileOpenAt(d.parent, d.name, syscall.O_RDONLY|syscall.O_DIRECTORY, 0)
		if e != nil {
			return command.Fail("FILE_ROOT_CHANGED")
		}
		id, e := fileIdentity(f)
		f.Close()
		if e != nil || id != d.identity {
			return command.Fail("FILE_ROOT_CHANGED")
		}
	}
	return nil
}
func (r *nativeRoot) create(dir, name, stage string) (*os.File, error) {
	if e := nativeFileBefore(r.ctx, stage); e != nil {
		return nil, e
	}
	if e := r.validate(); e != nil {
		return nil, e
	}
	f, e := fileOpenAt(r.dirs[dir], name, syscall.O_RDWR|syscall.O_CREAT|syscall.O_EXCL, 0600)
	if e != nil {
		nativeFileObserve(r.ctx, nativeFileEvent{Kind: "error", Stage: stage, Directory: dir, Name: name, Error: e.Error()})
		return nil, e
	}
	id, e := fileIdentity(f)
	if e != nil {
		f.Close()
		return nil, e
	}
	nativeFileObserve(r.ctx, nativeFileEvent{Kind: "create", Stage: stage, Directory: dir, Name: name, Identity: id})
	if e = fileRegular(f); e != nil {
		f.Close()
		return nil, e
	}
	return f, nil
}
func (r *nativeRoot) read(dir, name, stage string) ([]byte, error) {
	if e := nativeFileBefore(r.ctx, stage); e != nil {
		return nil, e
	}
	if e := r.validate(); e != nil {
		return nil, e
	}
	f, e := fileOpenAt(r.dirs[dir], name, syscall.O_RDONLY, 0)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	if e = fileRegular(f); e != nil {
		return nil, e
	}
	id, e := fileIdentity(f)
	if e != nil {
		return nil, e
	}
	body, e := io.ReadAll(io.LimitReader(f, (1<<20)+4097))
	nativeFileObserve(r.ctx, nativeFileEvent{Kind: "read", Stage: stage, Directory: dir, Name: name, Identity: id, Length: len(body)})
	if len(body) > (1<<20)+4096 {
		return nil, command.Fail("FILE_TOO_LARGE")
	}
	return body, e
}
func (r *nativeRoot) write(f *os.File, dir, name, stage string, body []byte) error {
	id, e := fileIdentity(f)
	if e != nil {
		return e
	}
	for offset := 0; offset < len(body); {
		if e = nativeFileBefore(r.ctx, stage); e != nil {
			return e
		}
		if e = r.validate(); e != nil {
			return e
		}
		requested := len(body) - offset
		count := nativeFileWriteLimit(r.ctx, stage, requested)
		n, e := f.Write(body[offset : offset+count])
		event := nativeFileEvent{Kind: "write", Stage: stage, Directory: dir, Name: name, Identity: id, Offset: int64(offset), Data: append([]byte(nil), body[offset:offset+n]...), Requested: requested, Length: n}
		if e != nil {
			event.Error = e.Error()
		}
		nativeFileObserve(r.ctx, event)
		offset += n
		if e != nil {
			return e
		}
		if n == 0 {
			return io.ErrShortWrite
		}
	}
	return nil
}
func (r *nativeRoot) sync(f *os.File, dir, name, stage string, directory bool) error {
	if e := nativeFileBefore(r.ctx, stage); e != nil {
		return e
	}
	if e := r.validate(); e != nil {
		return e
	}
	if nativeFileOmitSync(r.ctx, stage) {
		return nil
	}
	if e := syscall.Fsync(int(f.Fd())); e != nil {
		return e
	}
	if e := nativeFileBefore(r.ctx, stage+".fullfsync"); e != nil {
		return e
	}
	if e := r.validate(); e != nil {
		return e
	}
	_, _, errno := syscall.Syscall(syscall.SYS_FCNTL, f.Fd(), syscall.F_FULLFSYNC, 0)
	if errno != 0 {
		return errno
	}
	id, e := fileIdentity(f)
	if e != nil {
		return e
	}
	kind := "sync"
	if directory {
		kind = "dirsync"
	}
	nativeFileObserve(r.ctx, nativeFileEvent{Kind: kind, Stage: stage, Directory: dir, Name: name, Identity: id})
	return nil
}
func (r *nativeRoot) rename(from, to string, check func(context.Context) error) error {
	if e := r.validate(); e != nil {
		return e
	}
	if e := nativeFileBefore(r.ctx, "publish.rename"); e != nil {
		return e
	}
	if e := check(r.ctx); e != nil {
		return e
	}
	if e := r.validate(); e != nil {
		return e
	}
	a, b := C.CString(from), C.CString(to)
	defer C.free(unsafe.Pointer(a))
	defer C.free(unsafe.Pointer(b))
	rc, e := C.file_renameat(C.int(r.dirs["commits"].Fd()), a, b)
	if rc != 0 {
		nativeFileObserve(r.ctx, nativeFileEvent{Kind: "error", Stage: "publish.rename", Directory: "commits", Name: from, NewName: to, Error: e.Error()})
		return e
	}
	nativeFileObserve(r.ctx, nativeFileEvent{Kind: "rename", Stage: "publish.rename", Directory: "commits", Name: from, NewName: to})
	return nil
}
func (r *nativeRoot) lock(target string) (func(), error) {
	f, e := r.create("locks", target, "lock.create")
	if errors.Is(e, syscall.EEXIST) {
		f, e = fileOpenAt(r.dirs["locks"], target, syscall.O_RDWR, 0)
	}
	if e != nil {
		return nil, e
	}
	if e = fileRegular(f); e != nil {
		f.Close()
		return nil, e
	}
	// 锁 inode 不随提交替换。等待只观察上下文，不因工作租约过期破坏内核锁。
	for {
		e = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if e == nil {
			break
		}
		if !errors.Is(e, syscall.EWOULDBLOCK) && !errors.Is(e, syscall.EAGAIN) {
			f.Close()
			return nil, e
		}
		select {
		case <-r.ctx.Done():
			f.Close()
			return nil, r.ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
	return func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN); _ = f.Close() }, nil
}

func (r *nativeRoot) remove(dir, name, identity string, check func(context.Context) error) error {
	if e := r.validate(); e != nil {
		return e
	}
	if e := nativeFileBefore(r.ctx, "cleanup.unlink"); e != nil {
		return e
	}
	if e := check(r.ctx); e != nil {
		return e
	}
	if e := r.validate(); e != nil {
		return e
	}
	f, e := fileOpenAt(r.dirs[dir], name, syscall.O_RDONLY, 0)
	if errors.Is(e, syscall.ENOENT) {
		return nil
	}
	if e != nil {
		return e
	}
	defer f.Close()
	if e = fileRegular(f); e != nil {
		return e
	}
	id, e := fileIdentity(f)
	if e != nil {
		return e
	}
	if identity == "" || id != identity {
		return command.Fail("FILE_RESOURCE_IDENTITY_MISMATCH")
	}
	p := C.CString(name)
	defer C.free(unsafe.Pointer(p))
	rc, e := C.file_unlinkat(C.int(r.dirs[dir].Fd()), p)
	if rc != 0 {
		return e
	}
	nativeFileObserve(r.ctx, nativeFileEvent{Kind: "unlink", Stage: "cleanup.unlink", Directory: dir, Name: name, Identity: id})
	return nil
}
func (r *nativeRoot) absent(dir, name string) error {
	if e := r.validate(); e != nil {
		return e
	}
	f, e := fileOpenAt(r.dirs[dir], name, syscall.O_RDONLY, 0)
	if errors.Is(e, syscall.ENOENT) {
		return nil
	}
	if e != nil {
		return e
	}
	f.Close()
	return command.Fail("FILE_CLEANUP_UNVERIFIED")
}
