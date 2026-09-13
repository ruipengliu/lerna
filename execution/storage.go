package execution

import (
	"bytes"
	"encoding/gob"
	"lerna/authorization"
)

type executionStorage struct {
	Format     int
	Partitions map[string][]byte
	Resources  resourceJournal
	decoded    map[string]journal
}

func decodeStorage(data []byte) (executionStorage, error) {
	out := executionStorage{Format: 2, Partitions: map[string][]byte{}}
	if len(data) > 0 {
		var header struct{ Format int }
		if e := gob.NewDecoder(bytes.NewReader(data)).Decode(&header); e != nil {
			return out, failure(authorization.Unavailable)
		}
		if header.Format == 1 {
			var legacy journal
			if e := gob.NewDecoder(bytes.NewReader(data)).Decode(&legacy); e != nil || legacy.Descriptor == "" {
				return out, failure(authorization.Unavailable)
			}
			out.Partitions[legacy.Descriptor] = data
		} else if header.Format == 2 {
			if e := gob.NewDecoder(bytes.NewReader(data)).Decode(&out); e != nil {
				return out, failure(authorization.Unavailable)
			}
		} else {
			return out, failure(authorization.Unsupported)
		}
	}
	out.decoded = map[string]journal{}
	r := &out.Resources
	if r.Scopes == nil {
		r.Scopes = map[string]controlState{}
	}
	if r.Aliases == nil {
		r.Aliases = map[string]string{}
	}
	if r.Participants == nil {
		r.Participants = map[string]string{}
	}
	if r.Operations == nil {
		r.Operations = map[string]controlOperation{}
	}
	if r.Invocations == nil {
		r.Invocations = map[string]resourceInvocation{}
	}
	if r.Cursors == nil {
		r.Cursors = map[string]string{}
	}
	r.CancelOwners = map[string]string{}
	r.Invocations = map[string]resourceInvocation{}
	if out.Partitions == nil || len(out.Partitions) > 32 {
		return out, failure(authorization.Unavailable)
	}
	for descriptor, data := range out.Partitions {
		var j journal
		if e := gob.NewDecoder(bytes.NewReader(data)).Decode(&j); e != nil || j.Format != 1 || j.Descriptor != descriptor {
			return out, failure(authorization.Unavailable)
		}
		out.decoded[descriptor] = j
		for op, record := range j.Records {
			if _, ok := r.Invocations[op]; ok {
				return out, failure(authorization.IdentityConflict)
			}
			r.indexInvocation(descriptor, op, record)
		}
		for op := range j.ReservedCancels {
			if owner, ok := r.CancelOwners[op]; ok && owner != descriptor {
				return out, failure(authorization.IdentityConflict)
			}
			r.CancelOwners[op] = descriptor
		}
		for op := range j.Cancels {
			if owner, ok := r.CancelOwners[op]; ok && owner != descriptor {
				return out, failure(authorization.IdentityConflict)
			}
			r.CancelOwners[op] = descriptor
		}
	}
	return out, nil
}
func encodeStorage(st executionStorage) ([]byte, error) {
	var b bytes.Buffer
	if e := gob.NewEncoder(&b).Encode(st); e != nil {
		return nil, e
	}
	if b.Len() > 4<<20 {
		return nil, failure(authorization.Unavailable)
	}
	return b.Bytes(), nil
}
