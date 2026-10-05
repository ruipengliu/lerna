//go:build fault

// Package storagevfs 仅在故障子进程中记录生产 SQLite 的文件操作。
package storagevfs

/*
#include <stdint.h>
#include <stdlib.h>
int register_model(const char *path);
uint64_t model_sequence(void);
void model_mode(int mode,int skip);
uint64_t model_native_attempts(void);
uint64_t model_native_successes(void);
uint64_t model_native_failures(void);
uint64_t model_directory_failures(void);
const char *model_source_id(void);
uint64_t model_ephemeral_opens(void);
uint64_t model_ephemeral_reads(void);
uint64_t model_ephemeral_writes(void);
uint64_t model_ephemeral_closes(void);
uint64_t model_ephemeral_failures(void);
void model_ephemeral_write_error(int enabled);
*/
import "C"

import (
	"fmt"
	"unsafe"
)

// Register 将包装器注册到生产驱动链接的同一个 SQLite 库。
func Register(path string) error {
	p := C.CString(path)
	defer C.free(unsafe.Pointer(p))
	if rc := C.register_model(p); rc != 0 {
		return fmt.Errorf("VFS registration: %d", rc)
	}
	return nil
}

// Sequence 返回独立 ACK 所对应的已完成 I/O 前缀长度。
func Sequence() int { return int(C.model_sequence()) }

// Mode 仅供负对照使用：0 正常；1 丢弃屏障；2 同步报错。
func Mode(mode int) { C.model_mode(C.int(mode), 0) }

// NativeBarriers 返回实际 Unix VFS 的 F_FULLFSYNC 尝试、成功和失败次数。
func NativeBarriers() (uint64, uint64, uint64) {
	return uint64(C.model_native_attempts()), uint64(C.model_native_successes()), uint64(C.model_native_failures())
}

// SourceID 用于排除意外链接第二份 SQLite 的验收错误。
func SourceID() string { return C.GoString(C.model_source_id()) }

// ModeAfter 在指定数目的成功业务同步之后注入失败，覆盖内容与接纳事务。
func ModeAfter(mode, skip int) { C.model_mode(C.int(mode), C.int(skip)) }

// DirectoryFailures 返回实际生产目录同步路径被注入的失败次数。
func DirectoryFailures() uint64 { return uint64(C.model_directory_failures()) }

// EphemeralStats 仅统计保存点临时日志的真实 I/O，不增加持久模型文件或切点。
func EphemeralStats() (opens, reads, writes, closes, failures uint64) {
	return uint64(C.model_ephemeral_opens()), uint64(C.model_ephemeral_reads()), uint64(C.model_ephemeral_writes()), uint64(C.model_ephemeral_closes()), uint64(C.model_ephemeral_failures())
}

// FailEphemeralWrites 注入临时日志写失败，独立于持久文件同步故障计数。
func FailEphemeralWrites(enabled bool) {
	value := C.int(0)
	if enabled {
		value = 1
	}
	C.model_ephemeral_write_error(value)
}
