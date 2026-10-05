//go:build darwin

package sqlite

/*
int register_strict_barrier(void);
void barrier_platform(const char *path,char *result,int length);
#include <stdlib.h>
*/
import "C"

import (
	"fmt"
	"unsafe"
)

// 导入时先注册，故障包装器随后可以在同一 VFS 外侧观测和注入。
var registration = C.register_strict_barrier()

// Ensure 注册失败时禁止打开关键事实的写连接。
func ensureBarrier() error {
	if registration != 0 {
		return fmt.Errorf("strict SQLite F_FULLFSYNC unavailable: %d", registration)
	}
	return nil
}

// Platform 从文件所在挂载点与内核读取实际平台。
func observedPlatform(path string) string {
	p := C.CString(path)
	defer C.free(unsafe.Pointer(p))
	var result [256]C.char
	C.barrier_platform(p, &result[0], C.int(len(result)))
	return C.GoString(&result[0])
}
