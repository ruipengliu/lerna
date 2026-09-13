package fetchcontent

import "sync"

// Only immutable read lengths are retained, never evidence bytes or authority.
// Every reuse performs READ with current authorization and integrity checks.
type readBounds struct {
	mu    sync.RWMutex
	sizes map[string]uint64
}

func (b *readBounds) get(ref string) (uint64, bool) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	size, ok := b.sizes[ref]
	return size, ok
}

func (b *readBounds) remember(ref string, size uint64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	// The evidence context accepts at most eight references. Other callers
	// remain supported using the uncached path once this bound is reached.
	if len(b.sizes) >= 8 {
		return
	}
	if b.sizes == nil {
		b.sizes = map[string]uint64{}
	}
	b.sizes[ref] = size
}
