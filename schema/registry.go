// Package schema validates registered dynamic payloads without network access.
package schema

import (
	"crypto/sha256"
	"fmt"
	"net/url"

	jsonschema "github.com/santhosh-tekuri/jsonschema/v6"
	wire "lerna/gen/harness/v1"
	"lerna/internal/jsonvalue"
)

const Dialect = "https://json-schema.org/draft/2020-12/schema"

type Resource struct {
	Type, ID, Version string
	Document          []byte
}

type binding struct {
	id, version, digest string
	compiled            *jsonschema.Schema
}

// Registry is immutable after construction and safe for concurrent validation.
// A registry selects one exact schema version for each type for this profile.
type Registry struct{ types map[string]binding }

func Digest(document []byte) string { return fmt.Sprintf("sha256:%x", sha256.Sum256(document)) }

type offlineLoader struct{}

func (offlineLoader) Load(url string) (any, error) {
	return nil, fmt.Errorf("unregistered schema resource: %s", url)
}

func New(resources []Resource) (*Registry, error) {
	c := jsonschema.NewCompiler()
	c.DefaultDraft(jsonschema.Draft2020)
	c.AssertFormat()
	c.AssertVocabs()
	c.UseLoader(offlineLoader{})
	r := &Registry{types: make(map[string]binding)}
	ids := make(map[string]bool)
	for _, resource := range resources {
		if resource.Type == "" || resource.ID == "" || resource.Version == "" {
			return nil, fmt.Errorf("incomplete schema identity")
		}
		if _, exists := r.types[resource.Type]; exists || ids[resource.ID] {
			return nil, fmt.Errorf("duplicate schema identity")
		}
		uri, err := url.Parse(resource.ID)
		if err != nil || !uri.IsAbs() || uri.Fragment != "" {
			return nil, fmt.Errorf("schema ID must be an absolute URI without fragment")
		}
		document, err := jsonvalue.Decode(resource.Document)
		if err != nil {
			return nil, err
		}
		obj, ok := document.(map[string]any)
		if !ok || obj["$schema"] != Dialect || obj["$id"] != resource.ID {
			return nil, fmt.Errorf("schema dialect or identity mismatch")
		}
		if err := c.AddResource(resource.ID, document); err != nil {
			return nil, err
		}
		ids[resource.ID] = true
		r.types[resource.Type] = binding{id: resource.ID, version: resource.Version, digest: Digest(resource.Document)}
	}
	for name, b := range r.types {
		var err error
		b.compiled, err = c.Compile(b.id)
		if err != nil {
			return nil, err
		}
		r.types[name] = b
	}
	return r, nil
}

func (r *Registry) Validate(payload *wire.DynamicPayload) error {
	if payload == nil {
		return fmt.Errorf("missing dynamic payload")
	}
	b, ok := r.types[payload.TypeName]
	if !ok {
		return fmt.Errorf("unknown required type: %s", payload.TypeName)
	}
	if payload.SchemaId != b.id || payload.SchemaVersion != b.version || payload.SchemaDigest != b.digest {
		return fmt.Errorf("schema binding mismatch")
	}
	value, err := jsonvalue.Decode(payload.Json)
	if err != nil {
		return err
	}
	return b.compiled.Validate(value)
}
