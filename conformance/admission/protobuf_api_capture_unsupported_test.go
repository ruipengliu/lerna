//go:build !darwin || !cgo

package admission_test

import (
	"testing"

	"github.com/ruipengliu/lerna/conformance/protobuf"
)

func captureNativeAPI(t *testing.T, samples *[]protobuf.Sample) {
	t.Helper()
	t.Log("native API capture unavailable: requires Darwin and cgo; no substitute objects added")
}
func captureDefaultModelAPIDriver(t *testing.T, samples *[]protobuf.Sample) {
	t.Helper()
	t.Log("default MODEL/API capture unavailable: requires Darwin and cgo; no substitute objects added")
}
