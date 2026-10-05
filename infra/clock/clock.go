// Package clock 提供平台时钟（受信实现）。
package clock

import "time"

// System 是系统时钟。SQLite 本地档没有数据库端的权威时钟，以单进程的系统时钟为准。
type System struct{}

// Now 返回当前时间。
func (System) Now() time.Time { return time.Now() }
