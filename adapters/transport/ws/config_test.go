package ws_test

import (
	wsbinding "lerna/adapters/transport/ws"
	"testing"
	"time"
)

func TestFiniteConfiguration(t *testing.T) {
	c := wsbinding.Config{MaxConnections: 4, Window: 4, MaxMessage: 65536, Chunk: 4096, Timeout: time.Second, Lifetime: time.Minute, Poll: 50 * time.Millisecond}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []wsbinding.Config{{}, {MaxConnections: 4, Window: 4, MaxMessage: 65536, Chunk: 0, Timeout: time.Second, Lifetime: time.Minute, Poll: 50 * time.Millisecond}} {
		if bad.Validate() == nil {
			t.Fatal("unbounded configuration accepted")
		}
	}
}
