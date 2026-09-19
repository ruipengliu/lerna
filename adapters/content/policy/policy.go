// Package policy provides a trusted, locally configured source-policy
// resolver. Revisions and rules come from the host, never from content payloads.
package policy

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"lerna/artifacts"
	wire "lerna/gen/harness/v1"
	"sync"
)

type Rule struct {
	Kind, Key                    string
	Revision                     uint64
	Actions, Purposes, Locations []string
	RetainUntil                  int64
}
type Policy struct {
	mu       sync.RWMutex
	rules    []Rule
	revision uint64
	digest   string
}

func New(rules []Rule) (*Policy, error) {
	p := new(Policy)
	if err := p.Replace(rules); err != nil {
		return nil, err
	}
	return p, nil
}

// Replace is a privileged configuration port. It cannot be reached through the SDK.
func (p *Policy) Replace(rules []Rule) error {
	if len(rules) > 256 {
		return artifacts.Error("INVALID_ARGUMENT")
	}
	copyRules := make([]Rule, len(rules))
	seen := map[string]bool{}
	for i, r := range rules {
		key := r.Kind + "\x00" + r.Key
		if r.Kind == "" || r.Key == "" || len(r.Kind) > 128 || len(r.Key) > 128 || r.Revision == 0 || r.RetainUntil <= 0 || seen[key] {
			return artifacts.Error("INVALID_ARGUMENT")
		}
		seen[key] = true
		for _, set := range [][]string{r.Actions, r.Purposes, r.Locations} {
			if len(set) == 0 || len(set) > 32 {
				return artifacts.Error("INVALID_ARGUMENT")
			}
			for _, v := range set {
				if v == "" || len(v) > 128 {
					return artifacts.Error("INVALID_ARGUMENT")
				}
			}
		}
		r.Actions = append([]string(nil), r.Actions...)
		r.Purposes = append([]string(nil), r.Purposes...)
		r.Locations = append([]string(nil), r.Locations...)
		copyRules[i] = r
	}
	p.mu.Lock()
	p.rules = copyRules
	p.revision++
	raw, _ := json.Marshal(copyRules)
	p.digest = fmt.Sprintf("%x", sha256.Sum256(raw))
	p.mu.Unlock()
	return nil
}
func contains(set []string, v string) bool {
	for _, x := range set {
		if x == v {
			return true
		}
	}
	return false
}
func (p *Policy) Check(ctx context.Context, s *wire.ContentSource, action, purpose, location string, until int64) error {
	if ctx.Err() != nil {
		return artifacts.Error("UNAVAILABLE")
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	for _, r := range p.rules {
		if s.Kind == r.Kind && s.Key == r.Key && s.Revision == r.Revision && until <= r.RetainUntil && contains(r.Actions, action) && contains(r.Purposes, purpose) && contains(r.Locations, location) {
			return nil
		}
	}
	return artifacts.Error("PERMISSION_DENIED")
}

// Revision binds data-policy views, including time-based expiry, without
// exposing source rules. A process restart with changed rules also differs.
func (p *Policy) Revision(ctx context.Context, at int64) (string, error) {
	if ctx.Err() != nil {
		return "", artifacts.Error("UNAVAILABLE")
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	expired := 0
	for _, r := range p.rules {
		if at > r.RetainUntil {
			expired++
		}
	}
	return fmt.Sprintf("%s:%d:%d", p.digest, p.revision, expired), nil
}
