package execution

import (
	"encoding/json"
	"lerna/extraction"
	wire "lerna/gen/harness/v1"
	"lerna/internal/jsonvalue"
	"lerna/memory"
	"strconv"
)

// Source identities have one spelling and one revision per source scope.
// Reject ambiguous metadata before any source reader receives it.
func sourceInput(input []byte) ([]*wire.ContentSource, error) {
	if len(input) == 0 || len(input) > 8192 {
		return nil, memory.Invalid
	}
	value, err := jsonvalue.Decode(input)
	object, ok := value.(map[string]any)
	if err != nil || !ok || len(object) != 1 {
		return nil, memory.Invalid
	}
	values, ok := object["sources"].([]any)
	if !ok || len(values) == 0 || len(values) > 16 {
		return nil, memory.Invalid
	}
	refs := make([]*wire.ContentSource, 0, len(values))
	seen := map[extraction.SourceScope]bool{}
	for _, value := range values {
		object, ok := value.(map[string]any)
		if !ok || len(object) != 3 {
			return nil, memory.Invalid
		}
		kind, kindOK := object["kind"].(string)
		key, keyOK := object["key"].(string)
		number, numberOK := object["revision"].(json.Number)
		if !kindOK || !keyOK || !numberOK || kind == "" || key == "" {
			return nil, memory.Invalid
		}
		revision, err := strconv.ParseUint(string(number), 10, 64)
		if err != nil || revision == 0 {
			return nil, memory.Invalid
		}
		scope := extraction.SourceScope{Kind: kind, Key: key}
		if seen[scope] {
			return nil, memory.Invalid
		}
		seen[scope] = true
		refs = append(refs, &wire.ContentSource{Kind: kind, Key: key, Revision: revision})
	}
	return refs, nil
}
