// Package inputs prepares governed source metadata for Core tasks.
// It reads no source body and derives no authority from the event payload.
package inputs

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"google.golang.org/protobuf/proto"
	"lerna/answers"
	"lerna/artifacts"
	"lerna/extraction"
	wire "lerna/gen/harness/v1"
	"lerna/memory"
	"lerna/tasks"
)

type Content interface {
	Call(context.Context, artifacts.Binding, *wire.ContentRequest) (*wire.ContentResponse, error)
}
type Preparer struct {
	content  Content
	clock    memory.Clock
	binding  memory.Binding
	resource string
	allocate func(context.Context) (string, error)
}

func New(content Content, clock memory.Clock, binding memory.Binding, resource string, allocate func(context.Context) (string, error)) (*Preparer, error) {
	if content == nil || clock == nil || allocate == nil || binding.Token == "" || binding.Namespace == "" || binding.Subject == "" || binding.Location == "" || binding.Recipient != binding.Location || resource == "" || len(resource) > 256 {
		return nil, memory.Invalid
	}
	return &Preparer{content, clock, binding, resource, allocate}, nil
}

// Prepare must be called by the authorized Dispatcher. Content independently
// checks current metadata storage policy for the exact event revision. The
// original task operation and Content PUT operation are distinct identities.
// A competing dispatcher may leave an unused expiring metadata input; it does
// not create a source read, extraction effect, or another accepted task round.
func (p *Preparer) Prepare(ctx context.Context, r extraction.TriggerRecord, event *wire.ContentSource, operation string) (tasks.Submission, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if !extraction.ValidTriggerSpec(r.Spec) || r.State != "active" || r.Namespace != p.binding.Namespace || r.Subject != p.binding.Subject || r.Location != p.binding.Location || r.Purpose == "" || operation == "" || len(operation) > 256 || event == nil || event.Revision == 0 || event.Revision > 1<<53 || len(event.ProtoReflect().GetUnknown()) != 0 {
		return tasks.Submission{}, memory.Invalid
	}
	allowed := false
	for _, scope := range r.Spec.Sources {
		if scope.Kind == event.Kind && scope.Key == event.Key {
			allowed = true
			break
		}
	}
	if !allowed {
		return tasks.Submission{}, memory.Denied
	}
	event = proto.Clone(event).(*wire.ContentSource)
	now, err := p.clock.Now()
	if err != nil {
		return tasks.Submission{}, memory.Unavailable
	}
	deadline := min(r.Spec.ExpiresUnix, now.Add(5*time.Minute).Unix())
	if deadline <= now.Unix() {
		return tasks.Submission{}, memory.Denied
	}
	body, err := json.Marshal(map[string]any{"sources": []*wire.ContentSource{event}})
	if err != nil {
		return tasks.Submission{}, memory.Invalid
	}
	contentOp, err := p.allocate(ctx)
	if err != nil {
		return tasks.Submission{}, err
	}
	if contentOp == operation {
		return tasks.Submission{}, memory.IdentityConflict
	}
	digest := sha256.Sum256(body)
	spec := &wire.ContentSpec{Kind: "evidence", Resource: p.resource, Purpose: r.Purpose, Sources: []*wire.ContentSource{event}, AcquiredAt: now.Unix(), MediaType: "application/json", Size: uint64(len(body)), Sha256: hex.EncodeToString(digest[:]), RetainUntil: deadline}
	out, err := p.content.Call(ctx, artifacts.Binding{Token: p.binding.Token, Namespace: p.binding.Namespace, Location: p.binding.Location, Recipient: p.binding.Recipient}, &wire.ContentRequest{Method: "PUT", OperationId: contentOp, Data: body, Spec: spec})
	if err != nil {
		return tasks.Submission{}, err
	}
	if out == nil || out.Record == nil || out.Record.Ref == nil || out.Record.Ref.Namespace != p.binding.Namespace || out.Record.Ref.Key == "" || out.Record.Ref.Revision == 0 || !proto.Equal(out.Record.Spec, spec) {
		return tasks.Submission{}, memory.Unavailable
	}
	return tasks.Submission{Namespace: r.Namespace, OperationID: operation, Goal: "Extract memory from the registered source revision", InputRefs: []string{answers.Reference(out.Record.Ref)}, Constraints: tasks.Constraints{MaxSteps: uint32(r.Spec.MaxSteps), DeadlineUnix: deadline}}, nil
}

var _ extraction.TriggerInputs = (*Preparer)(nil)
