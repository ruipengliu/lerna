package artifacts

import (
	"context"
	"lerna/authorization"
	"sort"
)

// Clean is a trusted maintenance port. It is bounded, restartable, and does not
// expose content metadata to an ordinary caller. Call repeatedly to drain work.
func (s *Service) Clean(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, s.config.Timeout)
	defer cancel()
	unlock, err := s.blobs.Lock(ctx)
	if err != nil {
		return err
	}
	defer unlock()
	live := map[string]bool{}
	pending := map[string]string{}
	err = s.update(ctx, func(tx authorization.ContentTransaction, j *journal) error {
		live = map[string]bool{}
		pending = map[string]string{}
		keys := make([]string, 0, len(j.Records))
		for k := range j.Records {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			e := j.Records[k]
			r, err := decode(e)
			if err != nil {
				return err
			}
			if r.State == "available" && tx.Now().Unix() >= r.Spec.RetainUntil {
				r.State = "cleaning"
				r.LifecycleRevision++
				e.Body = nil
				j.Records[k] = encode(r, e)
			}
			if r.State == "cleaning" && len(pending) < s.config.CleanupBatch {
				pending[k] = e.File
			}
			if e.File != "" {
				live[e.File] = true
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	for key, file := range pending {
		if file != "" {
			if err = s.blobs.Remove(ctx, file); err != nil {
				return err
			}
		}
		if err = s.update(ctx, func(tx authorization.ContentTransaction, j *journal) error {
			e := j.Records[key]
			r, err := decode(e)
			if err != nil {
				return err
			}
			if r.State != "cleaning" {
				return Error("VERSION_CONFLICT")
			}
			r.State = "cleaned"
			r.Spec.Kind = ""
			r.Spec.AcquiredAt = 0
			r.Spec.MediaType = ""
			r.Spec.Size = 0
			r.Spec.Sha256 = ""
			for id, op := range j.Operations {
				if op.Key == key && op.Method == "PUT" {
					op.Hash = ""
					j.Operations[id] = op
				}
			}
			e.Body = nil
			e.File = ""
			if e.UseID != "" {
				if err := tx.CompleteUse(e.UseID, "artifacts", s.useConfig()); err != nil {
					return err
				}
			}
			j.Records[key] = encode(r, e)
			return nil
		}); err != nil {
			return err
		}
	}
	files, err := s.blobs.List(ctx, s.config.MaxFiles)
	if err != nil {
		return err
	}
	removed := 0
	for _, f := range files {
		if !live[f.Key] && removed < s.config.CleanupBatch {
			if err = s.blobs.Remove(ctx, f.Key); err != nil {
				return err
			}
			removed++
		}
	}
	return nil
}
