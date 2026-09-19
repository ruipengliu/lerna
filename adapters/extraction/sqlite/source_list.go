package sqlite

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"lerna/extraction"
	"lerna/memory"
	"sort"
	"strconv"
)

func (s *Store) ListSourceFences(ctx context.Context, namespace, after string, limit int) ([]extraction.FencedSource, error) {
	if !name(namespace) || limit < 1 || limit > 16 {
		return nil, memory.Invalid
	}
	if after != "" {
		raw, err := hex.DecodeString(after)
		if err != nil || len(raw) != 32 || hex.EncodeToString(raw) != after {
			return nil, memory.Invalid
		}
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	rows, err := s.db.QueryContext(ctx, `SELECT kind,source_key,revision FROM candidate_source_fences WHERE namespace=?`, namespace)
	if err != nil {
		return nil, memory.Unavailable
	}
	defer rows.Close()
	var out []extraction.FencedSource
	seen := map[string]bool{}
	count := 0
	for rows.Next() {
		count++
		if count > maxRecords {
			return nil, memory.Unavailable
		}
		in := extraction.SourceInvalidation{Namespace: namespace}
		var revision string
		if err = rows.Scan(&in.Kind, &in.Key, &revision); err != nil {
			return nil, memory.Unavailable
		}
		in.ThroughRevision, err = strconv.ParseUint(revision, 10, 64)
		if err != nil || in.ThroughRevision == 0 || !name(in.Kind) || !name(in.Key) {
			return nil, memory.Unavailable
		}
		raw, _ := json.Marshal([]string{namespace, in.Kind, in.Key})
		hash := sha256.Sum256(raw)
		cursor := hex.EncodeToString(hash[:])
		if seen[cursor] {
			return nil, memory.Unavailable
		}
		seen[cursor] = true
		if cursor > after {
			out = append(out, extraction.FencedSource{Cursor: cursor, Invalidation: in})
		}
	}
	if err = rows.Err(); err != nil {
		return nil, memory.Unavailable
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Cursor < out[j].Cursor })
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

var _ extraction.SourceFenceDiscovery = (*Store)(nil)
