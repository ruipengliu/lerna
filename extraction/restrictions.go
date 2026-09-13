package extraction

import (
	"lerna/memory"
	"sort"
)

// Restrictions are trusted current source-policy output. A proposal cannot
// declare these values or use a claimed transformation to broaden them.
// Recipients covers every disclosure outlet, including logs and tool inputs.
type Restrictions struct {
	Storage, Processing, Purposes, Recipients []string
	RetainUntil                               int64
}

// IntersectRestrictions requires a nonempty common scope in every dimension
// and the earliest retention deadline. A stricter candidate policy may be
// included as another operand. This does not grant permission or persist a
// policy; the effect path must re-resolve current source policy before use.
func IntersectRestrictions(now int64, sources []Restrictions) (Restrictions, error) {
	if now <= 0 || len(sources) == 0 || len(sources) > 17 {
		return Restrictions{}, memory.Invalid
	}
	var sets [4]map[string]bool
	until := int64(0)
	for i, s := range sources {
		if s.RetainUntil <= now {
			return Restrictions{}, memory.Denied
		}
		if until == 0 || s.RetainUntil < until {
			until = s.RetainUntil
		}
		for dimension, values := range [][]string{s.Storage, s.Processing, s.Purposes, s.Recipients} {
			if len(values) == 0 {
				return Restrictions{}, memory.Denied
			}
			if len(values) > 16 {
				return Restrictions{}, memory.Invalid
			}
			current := map[string]bool{}
			for _, v := range values {
				if len(v) == 0 || len(v) > 256 {
					return Restrictions{}, memory.Invalid
				}
				current[v] = true
			}
			if i == 0 {
				sets[dimension] = current
			} else {
				for v := range sets[dimension] {
					if !current[v] {
						delete(sets[dimension], v)
					}
				}
			}
			if len(sets[dimension]) == 0 {
				return Restrictions{}, memory.Denied
			}
		}
	}
	values := func(set map[string]bool) []string {
		out := make([]string, 0, len(set))
		for v := range set {
			out = append(out, v)
		}
		sort.Strings(out)
		return out
	}
	return Restrictions{Storage: values(sets[0]), Processing: values(sets[1]), Purposes: values(sets[2]), Recipients: values(sets[3]), RetainUntil: until}, nil
}
