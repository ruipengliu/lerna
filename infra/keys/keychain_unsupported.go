//go:build !darwin || !cgo

package keys

import "github.com/ruipengliu/lerna/contracts/command"

func nativeRemove(string, string) error { return command.Fail("CREDENTIAL_STORE_UNSUPPORTED") }

func nativeOpen(string) error { return command.Fail("CREDENTIAL_STORE_UNSUPPORTED") }
func nativeRead(string, string) ([]byte, error) {
	return nil, command.Fail("CREDENTIAL_STORE_UNSUPPORTED")
}
func nativeCreate(string, []byte) error      { return command.Fail("CREDENTIAL_STORE_UNSUPPORTED") }
func nativePut(string, string, []byte) error { return command.Fail("CREDENTIAL_STORE_UNSUPPORTED") }
func nativePutForExecutable(string, string, []byte, string) error {
	return command.Fail("CREDENTIAL_STORE_UNSUPPORTED")
}
func nativeLock(string) error           { return command.Fail("CREDENTIAL_STORE_UNSUPPORTED") }
func nativeUnlock(string, []byte) error { return command.Fail("CREDENTIAL_STORE_UNSUPPORTED") }
func nativeDelete(string) error         { return command.Fail("CREDENTIAL_STORE_UNSUPPORTED") }
