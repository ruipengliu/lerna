package filekeys

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"lerna/credentials"
)

func validOperation(op string) bool {
	if len(op) == 0 || len(op) > 64 {
		return false
	}
	for _, c := range op {
		if c < 33 || c > 126 {
			return false
		}
	}
	return true
}

// Prepare persists new material without changing the current encryption key.
// Operation mappings are retained (at most 32); retries never mint a new key.
func (s *Source) Prepare(ctx context.Context, operation string) (string, error) {
	if !validOperation(operation) {
		return "", credentials.Invalid
	}
	var id string
	e := s.transaction(ctx, false, func(r *ring) error {
		if existing, ok := r.Prepared[operation]; ok {
			if _, available := r.Keys[existing]; !available {
				return credentials.KeyUnavailable
			}
			id = existing
			return nil
		}
		if len(r.Prepared) >= 32 || len(r.Keys) >= maxKeys {
			return credentials.Exhausted
		}
		material := make([]byte, 32)
		if _, e := rand.Read(material); e != nil {
			clear(material)
			return credentials.KeyUnavailable
		}
		sum := sha256.Sum256(material)
		id = hex.EncodeToString(sum[:])
		if _, exists := r.Keys[id]; exists {
			clear(material)
			return credentials.KeyUnavailable
		}
		if r.Prepared == nil {
			r.Prepared = map[string]string{}
		}
		r.Prepared[operation] = id
		r.Keys[id] = key{Material: material}
		return nil
	})
	if e != nil {
		return "", e
	}
	return id, nil
}

// Activate accepts only the material prepared for this operation. A replay of
// an older completed activation cannot roll the active version backwards.
func (s *Source) Activate(ctx context.Context, operation, version string) error {
	if !validOperation(operation) {
		return credentials.Invalid
	}
	return s.transaction(ctx, false, func(r *ring) error {
		if r.Prepared[operation] != version || version == "" {
			return credentials.Denied
		}
		if _, ok := r.Keys[version]; !ok {
			return credentials.KeyUnavailable
		}
		if r.Activated[operation] {
			if r.Active != version {
				return credentials.Conflict
			}
			return nil
		}
		if r.Activated == nil {
			r.Activated = map[string]bool{}
		}
		r.Active = version
		r.Activated[operation] = true
		return nil
	})
}

// Retire is a privileged material operation. The lifecycle manager must first
// fence writes and prove there are no remaining records or allowed backups.
func (s *Source) Retire(ctx context.Context, version string) error {
	if len(version) != 64 {
		return credentials.Invalid
	}
	if _, e := hex.DecodeString(version); e != nil {
		return credentials.Invalid
	}
	return s.transaction(ctx, false, func(r *ring) error {
		if r.Active == version {
			return credentials.Denied
		}
		if k, ok := r.Keys[version]; ok {
			clear(k.Material)
			delete(r.Keys, version)
		}
		return nil
	})
}

// Active returns metadata without reserving or disclosing encryption material.
func (s *Source) Active(ctx context.Context) (string, error) {
	var id string
	e := s.transaction(ctx, false, func(r *ring) error { id = r.Active; return nil })
	if e != nil {
		return "", e
	}
	return id, nil
}
