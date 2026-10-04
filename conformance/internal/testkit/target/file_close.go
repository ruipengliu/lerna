package target

import "sync"

// firstFileClose is the physical FD release boundary used by writer acquisition.
// Once attempted, an unknown first result cannot authorize repeated FD close.
func firstFileClose(closeFile func() error) func() error {
	var once sync.Once
	var err error
	return func() error { once.Do(func() { err = closeFile() }); return err }
}
