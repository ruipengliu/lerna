package memory

import (
	"encoding/json"
	"lerna/brain"
	"lerna/contextassembly"
	wire "lerna/gen/harness/v1"
	"lerna/internal/jsonvalue"
)

// ProjectionRule is trusted host configuration for a registered schema. This
// reference policy supports explicit subject preferences with enumerated values;
// other schema families can implement Projector without changing the assembler.
type ProjectionRule struct {
	SchemaID, SchemaVersion, Kind, Condition, Claim, Field string
	Values                                                 []string
}
type projectionKey struct{ schema, version, kind string }
type RegisteredProjector struct {
	version string
	rules   map[projectionKey]ProjectionRule
}

func NewProjector(version string, rules []ProjectionRule) (*RegisteredProjector, error) {
	if !label(version) || len(rules) == 0 || len(rules) > 32 {
		return nil, contextassembly.Invalid
	}
	p := &RegisteredProjector{version: version, rules: map[projectionKey]ProjectionRule{}}
	for _, r := range rules {
		if !label(r.SchemaID) || !label(r.SchemaVersion) || !label(r.Kind) || !label(r.Condition) || !label(r.Claim) || !label(r.Field) || len(r.Values) == 0 || len(r.Values) > 64 {
			return nil, contextassembly.Invalid
		}
		key := projectionKey{r.SchemaID, r.SchemaVersion, r.Kind}
		if _, ok := p.rules[key]; ok {
			return nil, contextassembly.Invalid
		}
		seen := map[string]bool{}
		for _, v := range r.Values {
			if !label(v) || seen[v] {
				return nil, contextassembly.Invalid
			}
			seen[v] = true
		}
		r.Values = append([]string(nil), r.Values...)
		p.rules[key] = r
	}
	return p, nil
}
func (p *RegisteredProjector) Project(req contextassembly.Request, record *wire.MemoryRecord) (contextassembly.Source, error) {
	if p == nil || req.PolicyVersion != p.version || record == nil || record.Spec == nil || record.Spec.Content == nil {
		return contextassembly.Source{}, contextassembly.Invalidated
	}
	spec := record.Spec
	body := spec.Content
	rule, ok := p.rules[projectionKey{body.SchemaId, body.SchemaVersion, spec.Kind}]
	if !ok || len(body.Json) > 32768 {
		return contextassembly.Source{}, contextassembly.Invalidated
	}
	decoded, e := jsonvalue.Decode(body.Json)
	if e != nil {
		return contextassembly.Source{}, contextassembly.Invalidated
	}
	object, ok := decoded.(map[string]any)
	if !ok {
		return contextassembly.Source{}, contextassembly.Invalidated
	}
	value, ok := object[rule.Field].(string)
	if !ok {
		return contextassembly.Source{}, contextassembly.Invalidated
	}
	allowed := false
	for _, v := range rule.Values {
		if value == v {
			allowed = true
			break
		}
	}
	if !allowed {
		return contextassembly.Source{}, contextassembly.Invalidated
	}
	// Emit only configured data, never arbitrary fields, roles or policy text.
	data, _ := json.Marshal(struct {
		Claim string `json:"claim"`
		Value string `json:"value"`
	}{rule.Claim, value})
	return contextassembly.Source{Block: brain.Block{Text: string(data)}, Applicable: spec.About == req.Subject && spec.Conditions == rule.Condition, Claim: rule.Claim, Value: value}, nil
}

var _ Projector = (*RegisteredProjector)(nil)
