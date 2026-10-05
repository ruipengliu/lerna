// Package ids 生成核心契约 2.1 规定的局部标识（UUIDv7）。
//
// UUIDv7 只提供唯一性：不得当作访问凭据，其中的时间部分也不得用来判断因果顺序。
package ids

import (
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"sync"
	"time"
)

var (
	mu     sync.Mutex
	lastMs uint64
	seq    uint16
)

// New 返回一个新的 UUIDv7 字符串。同一毫秒内用递增的计数保持单调。
func New() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("ids: crypto/rand unavailable: " + err.Error())
	}
	ms := uint64(time.Now().UnixMilli())
	mu.Lock()
	if ms <= lastMs {
		ms = lastMs
		seq++
	} else {
		lastMs = ms
		seq = binary.BigEndian.Uint16(b[6:8]) & 0x07ff
	}
	s := seq
	mu.Unlock()
	b[0] = byte(ms >> 40)
	b[1] = byte(ms >> 32)
	b[2] = byte(ms >> 24)
	b[3] = byte(ms >> 16)
	b[4] = byte(ms >> 8)
	b[5] = byte(ms)
	b[6] = 0x70 | byte(s>>8)&0x0f
	b[7] = byte(s)
	b[8] = b[8]&0x3f | 0x80
	var out [36]byte
	hex.Encode(out[0:8], b[0:4])
	out[8] = '-'
	hex.Encode(out[9:13], b[4:6])
	out[13] = '-'
	hex.Encode(out[14:18], b[6:8])
	out[18] = '-'
	hex.Encode(out[19:23], b[8:10])
	out[23] = '-'
	hex.Encode(out[24:], b[10:])
	return string(out[:])
}

// Name 返回日志和人可读的全局名字：u/<用户>/<类型>/<负责域>/<UUID>。
func Name(userID, kind, domainID, localID string) string {
	return "u/" + userID + "/" + kind + "/" + domainID + "/" + localID
}
