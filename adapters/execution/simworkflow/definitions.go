// Package simworkflow provides frozen business types and durable lifecycle APIs.
// Definitions describe types, never deployment instances. Six operations share
// each type's ledger and records, with explicit state and accounting effects.
package simworkflow

import (
	"bytes"
	_ "embed"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"lerna/catalog"
	"lerna/execution"
	"lerna/schema"
	"strings"
)

//go:embed definitions.csv
var manifest []byte

type Definition struct{ Category, Kind, Title, Quantity, Limit, Verified, Unit, Mode string }

var Actions = [...]string{"draft", "submit", "authorize", "reject", "withdraw", "archive"}

func Definitions() ([]Definition, error) {
	rows, e := csv.NewReader(bytes.NewReader(manifest)).ReadAll()
	if e != nil {
		return nil, e
	}
	if len(rows) != 169 {
		return nil, fmt.Errorf("manifest size")
	}
	out := make([]Definition, 0, 168)
	seen := map[string]bool{}
	for _, r := range rows[1:] {
		if len(r) != 8 || seen[r[1]] || (r[7] != "reserve" && r[7] != "credit") {
			return nil, fmt.Errorf("invalid business definition")
		}
		seen[r[1]] = true
		out = append(out, Definition{r[0], r[1], r[2], r[3], r[4], r[5], r[6], r[7]})
	}
	return out, nil
}
func (d Definition) Entries() []catalog.Entry {
	out := make([]catalog.Entry, 0, 6)
	for _, a := range Actions {
		name := d.Kind + "." + a
		props := map[string]any{"record": map[string]any{"type": "string", "minLength": 1, "maxLength": 64, "pattern": "^[a-zA-Z0-9_-]+$"}}
		required := []string{"record"}
		if a == "draft" {
			for _, field := range []string{d.Quantity, d.Limit} {
				props[field] = map[string]any{"type": "integer", "minimum": 1, "maximum": 1000}
				required = append(required, field)
			}
			props[d.Verified] = map[string]any{"type": "boolean"}
			required = append(required, d.Verified)
		}
		pre := map[string]string{"draft": "record absent; positive " + d.Quantity + " and " + d.Limit, "submit": "draft; " + d.Verified + " true; " + d.Quantity + " <= " + d.Limit, "authorize": "submitted; sufficient " + d.Unit + " ledger capacity", "reject": "submitted", "withdraw": "authorized; reversal fits ledger bounds", "archive": "rejected or withdrawn"}[a]
		effect := map[string]string{"draft": "create draft retaining " + d.Quantity + ", " + d.Limit + " and " + d.Verified, "submit": "mark submitted after business validation", "authorize": d.Mode + " " + d.Quantity + " " + d.Unit + "; mark authorized", "reject": "mark rejected without accounting effect", "withdraw": "reverse authorized " + d.Quantity + " " + d.Unit + "; mark withdrawn", "archive": "mark archived retaining audit record"}[a]
		state := map[string]string{"draft": "draft", "submit": "submitted", "authorize": "authorized", "reject": "rejected", "withdraw": "withdrawn", "archive": "archived"}[a]
		cap := execution.Capability{Name: name, Version: "1", Implementation: "sqlite-business", ImplementationVersion: "1", Resource: "root", Purpose: "task", Location: "local", Exclusive: true, Synchronous: true}
		cap.Input = resource(name+":input", map[string]any{"type": "object", "required": required, "additionalProperties": false, "properties": props})
		cap.Output = resource(name+":output", map[string]any{"type": "object", "required": []string{"state", "ledger", "version"}, "additionalProperties": false, "properties": map[string]any{"state": map[string]any{"type": "string", "enum": []string{state}}, "ledger": map[string]any{"type": "integer", "minimum": 0, "maximum": 2000}, "version": map[string]any{"type": "integer", "minimum": 2}}})
		out = append(out, catalog.Entry{Source: catalog.Source{Kind: "catalog", Key: "business-manifest", Revision: 1}, Ref: catalog.Ref{Namespace: d.Kind, Name: name, Version: "1", Implementation: cap.Implementation, ImplementationVersion: "1", Digest: cap.Digest()}, Title: a + " " + d.Title, Category: d.Category, Aliases: []string{strings.ReplaceAll(d.Kind, "_", " ") + " " + a}, Purpose: "task", Location: "local", Resource: "root", DiscoveryResource: "root", ResourceType: d.Kind, Preconditions: pre, Effects: effect, Unsupported: "No external vendor, GUI, distributed cancellation or business compensation; archive is terminal", Guarantees: "Durable operation fingerprint deduplication; exact record version; original-operation inspection; reports via execution outbox; cancellation before start only", Available: true, Capability: cap})
	}
	return out
}
func resource(id string, body map[string]any) schema.Resource {
	id = "urn:business:" + id
	body["$schema"] = "https://json-schema.org/draft/2020-12/schema"
	body["$id"] = id
	raw, _ := json.Marshal(body)
	return schema.Resource{ID: id, Type: id, Version: "1", Document: raw}
}

// ImplementationCheck freezes the host's callable descriptor set once at
// assembly. It does not select candidates or carry expected business outcomes.
func ImplementationCheck() (func(catalog.Entry) error, error) {
	defs, e := Definitions()
	if e != nil {
		return nil, e
	}
	known := map[catalog.Ref]bool{}
	for _, d := range defs {
		for _, entry := range d.Entries() {
			known[entry.Ref] = true
		}
	}
	return func(entry catalog.Entry) error {
		if !known[entry.Ref] || entry.Ref.Digest != entry.Capability.Digest() {
			return fmt.Errorf("unsupported exact business implementation")
		}
		return nil
	}, nil
}
