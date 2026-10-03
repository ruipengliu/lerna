// Package contracts exposes metadata from the sole machine contract source.
package contracts

import (
	_ "embed"
	"encoding/json"
	"sync"
)

//go:embed schemas/methods.json
var methodsJSON []byte

type MethodPolicy struct {
	Kind             string
	Input            string
	Output           string
	Target           string
	Stages           []string
	ExpectedRevision bool `json:"expected_revision"`
}

// Method returns a copy of the frozen command or query definition.
func Method(name string) (MethodPolicy, bool) {
	p, ok := registry()[name]
	p.Stages = append([]string(nil), p.Stages...)
	return p, ok
}

var registry = sync.OnceValue(func() map[string]MethodPolicy {
	var r struct{ Methods map[string]MethodPolicy }
	if err := json.Unmarshal(methodsJSON, &r); err != nil {
		panic(err)
	}
	return r.Methods
})

// CommandPolicy reads frozen metadata; method payload validation is a separate port.
func CommandPolicy(method string) (MethodPolicy, bool) {
	p, ok := Method(method)
	return p, ok && p.Kind == "command"
}
