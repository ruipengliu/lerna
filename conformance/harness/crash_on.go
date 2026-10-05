//go:build fault

package harness

import "github.com/ruipengliu/lerna/core/durable/fault"

func crashPoint(v any) (string, bool) {
	c, ok := v.(fault.CrashSignal)
	return c.Point, ok
}
