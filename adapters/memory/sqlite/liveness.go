package sqlite

import (
	"context"
	"lerna/memory"
	"strings"
)

func (s *Store) ValidateVersions(ctx context.Context, refs []memory.VersionRef) error {
	if len(refs) > 34 {
		return memory.Invalid
	}
	if len(refs) == 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()
	var query strings.Builder
	query.WriteString("SELECT 1")
	args := make([]any, 0, 4*len(refs))
	for _, ref := range refs {
		if !validRef(ref.Ref) || ref.Revision == 0 || ref.Revision > 1<<32 {
			return memory.Invalid
		}
		query.WriteString(" AND EXISTS(SELECT 1 FROM memory_revisions WHERE namespace=? AND collection=? AND record_key=? AND revision=?)")
		args = append(args, ref.Ref.Namespace, ref.Ref.Collection, ref.Ref.Key, ref.Revision)
	}
	var available bool
	if err := s.db.QueryRowContext(ctx, query.String(), args...).Scan(&available); err != nil {
		return memory.Unavailable
	}
	if !available {
		return memory.Missing
	}
	return nil
}
