// Package researchlineage preserves sources discovered during task execution on
// a derived answer, including evidence that influenced it but was not cited.
package researchlineage

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"lerna/answers"
	"lerna/artifacts"
	"lerna/brain"
	wire "lerna/gen/harness/v1"
	"lerna/tasks"
	"time"

	"google.golang.org/protobuf/proto"
)

type Content interface {
	Call(context.Context, artifacts.Binding, *wire.ContentRequest) (*wire.ContentResponse, error)
}
type Provider struct {
	content Content
	binding artifacts.Binding
	task    [32]byte
	purpose string
	refs    []string
}

var _ answers.LineageProvider = (*Provider)(nil)

func fingerprint(task tasks.Task) [32]byte { raw, _ := json.Marshal(task); return sha256.Sum256(raw) }

// New fixes the exact decision snapshot and host-selected retained references.
// Model citation lists cannot remove a source from this set.
func New(content Content, binding artifacts.Binding, task tasks.Task, purpose string, refs []string) (*Provider, error) {
	if content == nil || binding.Token == "" || binding.Namespace == "" || binding.Location == "" || task.Ref.Namespace != binding.Namespace || task.Ref.TaskID == "" || task.Subject == "" || purpose == "" || len(refs) < 1 || len(refs) > 8 {
		return nil, brain.Error("INVALID_ARGUMENT")
	}
	seen := map[string]bool{}
	for _, ref := range refs {
		parsed, err := answers.ParseReference(ref)
		if err != nil || parsed.Namespace != binding.Namespace || seen[ref] {
			return nil, brain.Error("INVALID_ARGUMENT")
		}
		seen[ref] = true
	}
	return &Provider{content, binding, fingerprint(task), purpose, append([]string(nil), refs...)}, nil
}
func (p *Provider) Sources(ctx context.Context, task tasks.Task, storage string) (answers.Lineage, error) {
	if storage != p.binding.Location || fingerprint(task) != p.task {
		return answers.Lineage{}, brain.Error("INPUT_INVALIDATED")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out := answers.Lineage{}
	versions := map[string]uint64{}
	for _, text := range p.refs {
		ref, err := answers.ParseReference(text)
		if err != nil {
			return answers.Lineage{}, err
		}
		binding := p.binding
		binding.Recipient = storage
		response, err := p.content.Call(ctx, binding, &wire.ContentRequest{Method: "GET", Ref: ref, Purpose: p.purpose})
		if err != nil {
			return answers.Lineage{}, err
		}
		record := response.GetRecord()
		if record.GetState() != "available" || record.GetSpec().GetPurpose() != p.purpose || record.GetSpec().GetRetainUntil() <= 0 || len(record.GetSpec().GetSources()) == 0 {
			return answers.Lineage{}, brain.Error("INPUT_INVALIDATED")
		}
		until := record.Spec.RetainUntil
		if out.RetainUntil == 0 || until < out.RetainUntil {
			out.RetainUntil = until
		}
		for _, source := range record.Spec.Sources {
			if source == nil || source.Kind == "" || source.Key == "" || source.Revision == 0 {
				return answers.Lineage{}, brain.Error("INPUT_INVALIDATED")
			}
			identity, _ := json.Marshal([]string{source.Kind, source.Key})
			key := string(identity)
			if version, ok := versions[key]; ok {
				if version != source.Revision {
					return answers.Lineage{}, brain.Error("INPUT_INVALIDATED")
				}
				continue
			}
			if len(versions) >= 16 {
				return answers.Lineage{}, brain.Error("INPUT_BUDGET_EXCEEDED")
			}
			versions[key] = source.Revision
			out.Sources = append(out.Sources, proto.Clone(source).(*wire.ContentSource))
		}
	}
	// ContentAccess validates these sources again when retaining the answer at
	// storage. Metadata discovery alone does not grant processing or retention.
	return out, nil
}
