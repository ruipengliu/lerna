// Package output preserves acquired source restrictions on the outer
// Execution artifact. It delegates ordinary input reading to executioncontent.
package output

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"google.golang.org/protobuf/proto"
	executioncontent "lerna/adapters/execution/content"
	"lerna/answers"
	"lerna/artifacts"
	"lerna/execution"
	"lerna/fetch"
	wire "lerna/gen/harness/v1"
	"lerna/internal/jsonvalue"
)

type Adapter struct {
	content executioncontent.Content
	binding artifacts.Binding
	input   *executioncontent.Adapter
	clock   executioncontent.Clock
}

var _ execution.Content = (*Adapter)(nil)

func New(c executioncontent.Content, b artifacts.Binding, clock executioncontent.Clock) (*Adapter, error) {
	if c == nil || clock == nil || b.Namespace == "" || b.Location == "" || b.Recipient == "" {
		return nil, artifacts.Error("INVALID_ARGUMENT")
	}
	return &Adapter{content: c, binding: b, input: executioncontent.New(c, b, clock), clock: clock}, nil
}

// WithContent binds observations to a new governed consumer while preserving
// input length facts. The binding and current-read checks remain unchanged.
func (a *Adapter) WithContent(content executioncontent.Content) (*Adapter, error) {
	if content == nil {
		return nil, artifacts.Error("INVALID_ARGUMENT")
	}
	return &Adapter{content: content, binding: a.binding, clock: a.clock, input: a.input.WithContent(content)}, nil
}

// RememberInput retains only a task write's input length, never its authority.
func (a *Adapter) RememberInput(record *wire.ContentRecord) {
	a.input.RememberInput(record)
}

func (a *Adapter) Read(ctx context.Context, token, ref string, c execution.Capability) ([]byte, error) {
	return a.input.Read(ctx, token, ref, c)
}
func (a *Adapter) metadata(ctx context.Context, b artifacts.Binding, ref string, c execution.Capability) (*wire.ContentRecord, error) {
	id, err := answers.ParseReference(ref)
	if err != nil || id.Namespace != b.Namespace {
		return nil, artifacts.Error("PERMISSION_DENIED")
	}
	out, err := a.content.Call(ctx, b, &wire.ContentRequest{Method: "GET", Ref: id, Purpose: c.Purpose})
	if err != nil {
		return nil, err
	}
	r := out.GetRecord()
	if r.GetState() != "available" || r.GetSpec().GetResource() != c.Resource || r.GetSpec().GetPurpose() != c.Purpose || r.GetSpec().GetMediaType() != "application/json" {
		return nil, artifacts.Error("PERMISSION_DENIED")
	}
	return r, nil
}
func (a *Adapter) Save(ctx context.Context, token, op, inputRef string, c execution.Capability, output, evidence []byte) (string, error) {
	if len(output)+len(evidence) > 32768 {
		return "", artifacts.Error("INVALID_ARGUMENT")
	}
	value, err := jsonvalue.Decode(output)
	object, ok := value.(map[string]any)
	if err != nil || !ok || len(object) != 2 {
		return "", artifacts.Error("INVALID_ARGUMENT")
	}
	status, ok := object["status"].(string)
	if !ok || (status != "acquired" && !fetch.IsFailureStatus(status)) {
		return "", artifacts.Error("INVALID_ARGUMENT")
	}
	ref, ok := object["reference"].(string)
	if !ok || (status != "acquired" && ref != "") {
		return "", artifacts.Error("INVALID_ARGUMENT")
	}
	b := a.binding
	b.Token = token
	b.Recipient = c.Location
	input, err := a.metadata(ctx, b, inputRef, c)
	if err != nil {
		return "", err
	}
	// A failed acquisition has no acquired body. Its finite facts inherit the
	// controlled input restrictions without inventing a source or reference.
	acquired := input
	if status == "acquired" {
		acquired, err = a.metadata(ctx, b, ref, c)
		if err != nil {
			return "", err
		}
		if acquired.Spec.Kind != "evidence" {
			return "", artifacts.Error("INVALID_ARGUMENT")
		}
	}
	sources := []*wire.ContentSource{}
	for _, record := range []*wire.ContentRecord{input, acquired} {
		for _, source := range record.Spec.Sources {
			found := false
			for _, old := range sources {
				if old.Kind == source.Kind && old.Key == source.Key {
					if old.Revision != source.Revision {
						return "", artifacts.Error("PERMISSION_DENIED")
					}
					found = true
					break
				}
			}
			if !found {
				sources = append(sources, proto.Clone(source).(*wire.ContentSource))
			}
		}
	}
	if len(sources) == 0 || len(sources) > 16 {
		return "", artifacts.Error("INVALID_ARGUMENT")
	}
	body, err := json.Marshal(struct {
		Output   json.RawMessage `json:"output"`
		Evidence json.RawMessage `json:"evidence"`
	}{output, evidence})
	if err != nil {
		return "", artifacts.Error("INVALID_ARGUMENT")
	}
	// Use immutable source acquisition facts, not a fresh wall-clock timestamp,
	// so replay of the original outer save cannot change its semantic payload.
	spec := &wire.ContentSpec{Kind: "artifact", Resource: c.Resource, Purpose: c.Purpose, MediaType: "application/json", Sources: sources, AcquiredAt: max(input.Spec.AcquiredAt, acquired.Spec.AcquiredAt), RetainUntil: min(input.Spec.RetainUntil, acquired.Spec.RetainUntil), Size: uint64(len(body)), Sha256: fmt.Sprintf("%x", sha256.Sum256(body))}
	out, err := a.content.Call(ctx, b, &wire.ContentRequest{Method: "PUT", OperationId: op, Spec: spec, Data: body})
	if err != nil {
		return "", err
	}
	if out.GetRecord().GetState() != "available" {
		return "", artifacts.Error("CONTENT_UNAVAILABLE")
	}
	return answers.Reference(out.Record.Ref), nil
}
