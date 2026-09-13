package authorization

import (
	"context"
	"time"
)

type UseInspection struct {
	Active bool
	Target string
}

// InspectUse is a trusted host check, not an authorization or disclosure API.
// Inspecting an unknown identity never registers it or replaces its original use.
func (s *Service) InspectUse(ctx context.Context, namespace, consumer, config, id string) (UseInspection, error) {
	if !validName(namespace) || !validName(consumer) || !validName(id) || !digestValid(config) {
		return UseInspection{}, fail(Invalid)
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	out := UseInspection{}
	err := s.update(ctx, func(st *State, now time.Time) error {
		if st.Namespace != namespace {
			return fail(Denied)
		}
		if err := consumerConfig(st, consumer, config); err != nil {
			return err
		}
		entry, ok := st.Uses[id]
		if !ok || entry.Spec.Consumer != consumer {
			return fail(NotFound)
		}
		out = UseInspection{Active: entry.Notice == nil && !entry.Cleaned, Target: entry.Spec.Target}
		return nil
	})
	if err != nil {
		return UseInspection{}, err
	}
	return out, nil
}
