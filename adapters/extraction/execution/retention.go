package execution

import "lerna/memory"

// WithRetentionDeadline returns an immutable host configuration for new
// candidates. The absolute Unix deadline can only shorten source retention or
// an earlier host deadline. It is not accepted from source/model output and
// grants no permission. Already committed operations keep their durable facts;
// changing configuration does not retroactively retire their bodies.
// A host restoring this driver must restore the same deadline, not restart a
// relative TTL on each retry. Expired configurations refuse new extraction.
func WithRetentionDeadline(driver *Driver, until int64) (*Driver, error) {
	if driver == nil || until <= 0 {
		return nil, memory.Invalid
	}
	copy := *driver
	if copy.retainUntil == 0 || until < copy.retainUntil {
		copy.retainUntil = until
	}
	return &copy, nil
}
