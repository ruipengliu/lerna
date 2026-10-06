//go:build darwin && cgo

package keys

/*
#cgo LDFLAGS: -framework Security -framework CoreFoundation
#cgo CFLAGS: -Wno-deprecated-declarations
#include <stdlib.h>
#include "keychain_darwin.h"
*/
import "C"

import (
	"sync"
	"unsafe"

	"github.com/ruipengliu/lerna/contracts/command"
)

var nativeUI struct {
	sync.Once
	status C.int
}

func initializeNative() error {
	nativeUI.Do(func() { nativeUI.status = C.lerna_keychain_disable_ui() })
	if nativeUI.status != 0 {
		return command.Fail("CREDENTIAL_STORE_UNAVAILABLE")
	}
	return nil
}

func nativeStatus(status C.int) error {
	if status != 0 {
		return command.Fail("CREDENTIAL_UNAVAILABLE")
	}
	return nil
}
func nativeOpen(path string) error {
	if e := initializeNative(); e != nil {
		return e
	}
	p := C.CString(path)
	defer C.free(unsafe.Pointer(p))
	return nativeStatus(C.lerna_keychain_open(p))
}
func nativeRead(path, account string) ([]byte, error) {
	if e := initializeNative(); e != nil {
		return nil, e
	}
	p, a := C.CString(path), C.CString(account)
	defer C.free(unsafe.Pointer(p))
	defer C.free(unsafe.Pointer(a))
	var data *C.uchar
	var size C.size_t
	if e := nativeStatus(C.lerna_keychain_read(p, a, &data, &size)); e != nil {
		return nil, e
	}
	defer C.lerna_keychain_free(data, size)
	return C.GoBytes(unsafe.Pointer(data), C.int(size)), nil
}
func nativeCreate(path string, password []byte) error {
	if e := initializeNative(); e != nil {
		return e
	}
	p := C.CString(path)
	defer C.free(unsafe.Pointer(p))
	return nativeStatus(C.lerna_keychain_create(p, (*C.uchar)(unsafe.Pointer(&password[0])), C.size_t(len(password))))
}
func nativePut(path, account string, secret []byte) error {
	p, a := C.CString(path), C.CString(account)
	defer C.free(unsafe.Pointer(p))
	defer C.free(unsafe.Pointer(a))
	return nativeStatus(C.lerna_keychain_put(p, a, (*C.uchar)(unsafe.Pointer(&secret[0])), C.size_t(len(secret))))
}
func nativePutForExecutable(path, account string, secret []byte, executable string) error {
	p, a, x := C.CString(path), C.CString(account), C.CString(executable)
	defer C.free(unsafe.Pointer(p))
	defer C.free(unsafe.Pointer(a))
	defer C.free(unsafe.Pointer(x))
	return nativeStatus(C.lerna_keychain_put_executable(p, a, (*C.uchar)(unsafe.Pointer(&secret[0])), C.size_t(len(secret)), x))
}
func nativeLock(path string) error {
	p := C.CString(path)
	defer C.free(unsafe.Pointer(p))
	return nativeStatus(C.lerna_keychain_lock(p))
}
func nativeUnlock(path string, password []byte) error {
	p := C.CString(path)
	defer C.free(unsafe.Pointer(p))
	return nativeStatus(C.lerna_keychain_unlock(p, (*C.uchar)(unsafe.Pointer(&password[0])), C.size_t(len(password))))
}
func nativeDelete(path string) error {
	p := C.CString(path)
	defer C.free(unsafe.Pointer(p))
	return nativeStatus(C.lerna_keychain_delete(p))
}
func nativeRemove(path, account string) error {
	p, a := C.CString(path), C.CString(account)
	defer C.free(unsafe.Pointer(p))
	defer C.free(unsafe.Pointer(a))
	return nativeStatus(C.lerna_keychain_remove(p, a))
}
