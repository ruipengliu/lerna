package artifacts

import (
	"context"
	"encoding/json"
	"lerna/authorization"
	wire "lerna/gen/harness/v1"
)

const maxInvalidatedSources = 512

// SourceInvalidation is trusted host input derived from an authoritative source
// change. It is not a peer-facing deletion API and grants no disclosure rights.
type SourceInvalidation struct {
	Namespace, Kind, Key string
	ThroughRevision      uint64
}

// SourceRevisionInvalidation identifies one exact source revision.
type SourceRevisionInvalidation struct {
	Namespace, Kind, Key string
	Revision             uint64
}
type RevisionInvalidationStore interface {
	InvalidateRevision(context.Context, SourceRevisionInvalidation) (InvalidationStatus, error)
}

func sourceRevisionKey(namespace, kind, key string, revision uint64) string {
	raw, _ := json.Marshal([]any{namespace, kind, key, revision})
	return digest(raw)
}

type InvalidationStatus struct{ Cleaning, Cleaned int }

func sourceKey(namespace, kind, key string) string {
	raw, _ := json.Marshal([]string{namespace, kind, key})
	return digest(raw)
}
func invalidated(j *journal, namespace string, sources []*wire.ContentSource) bool {
	for _, source := range sources {
		if source != nil && source.Revision > 0 && (source.Revision <= j.Invalidated[sourceKey(namespace, source.Kind, source.Key)] || j.InvalidatedRevisions[sourceRevisionKey(namespace, source.Kind, source.Key, source.Revision)]) {
			return true
		}
	}
	return false
}

// InvalidateSource atomically marks dependent records unusable and persists the
// highest invalidated revision. Clean removes bodies separately; repetition
// reports the existing state without creating another lifecycle transition.
func (s *Service) InvalidateSource(ctx context.Context, in SourceInvalidation) (InvalidationStatus, error) {
	return s.invalidate(ctx, in, false)
}
func (s *Service) InvalidateRevision(ctx context.Context, in SourceRevisionInvalidation) (InvalidationStatus, error) {
	return s.invalidate(ctx, SourceInvalidation{Namespace: in.Namespace, Kind: in.Kind, Key: in.Key, ThroughRevision: in.Revision}, true)
}
func (s *Service) invalidate(ctx context.Context, in SourceInvalidation, exact bool) (InvalidationStatus, error) {
	if !text(in.Namespace) || !text(in.Kind) || !text(in.Key) || in.ThroughRevision == 0 {
		return InvalidationStatus{}, Error("INVALID_ARGUMENT")
	}
	ctx, cancel := context.WithTimeout(ctx, s.config.Timeout)
	defer cancel()
	result := InvalidationStatus{}
	err := s.update(ctx, func(tx authorization.ContentTransaction, j *journal) error {
		result = InvalidationStatus{}
		if tx.Namespace() != in.Namespace {
			return Error("PERMISSION_DENIED")
		}
		through := in.ThroughRevision
		if exact {
			key := sourceRevisionKey(in.Namespace, in.Kind, in.Key, through)
			if !j.InvalidatedRevisions[key] && len(j.Invalidated)+len(j.InvalidatedRevisions) >= maxInvalidatedSources {
				return Error("CAPACITY_EXCEEDED")
			}
			j.InvalidatedRevisions[key] = true
		} else {
			key := sourceKey(in.Namespace, in.Kind, in.Key)
			if _, ok := j.Invalidated[key]; !ok && len(j.Invalidated)+len(j.InvalidatedRevisions) >= maxInvalidatedSources {
				return Error("CAPACITY_EXCEEDED")
			}
			if through > j.Invalidated[key] {
				j.Invalidated[key] = through
			}
			through = j.Invalidated[key]
		}
		for id, e := range j.Records {
			r, err := decode(e)
			if err != nil {
				return err
			}
			if r.Ref.Namespace != in.Namespace {
				continue
			}
			matched := false
			for _, source := range r.Spec.Sources {
				if source.Kind == in.Kind && source.Key == in.Key && source.Revision <= through && (!exact || source.Revision == through) {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
			if r.State == "available" {
				r.State = "cleaning"
				r.LifecycleRevision++
				e.Body = nil
				j.Records[id] = encode(r, e)
			}
			switch r.State {
			case "cleaning":
				result.Cleaning++
			case "cleaned":
				result.Cleaned++
			}
		}
		return nil
	})
	if err != nil {
		return InvalidationStatus{}, err
	}
	return result, nil
}
