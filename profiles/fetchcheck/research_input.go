package fetchcheck

import (
	"context"
	taskcontent "lerna/adapters/tasks/content"
	"lerna/answers"
	"lerna/artifacts"
	"lerna/fetch"
	wire "lerna/gen/harness/v1"
	"lerna/tasks"
)

func (a *fetchActionHost) validateCatalog(ctx context.Context, t tasks.Task, location string) error {
	if a.queries == nil {
		return a.Validate(ctx, t, location)
	}
	return a.validateIdentity(ctx, t, location)
}

func (a *fetchActionHost) validateResearchInputs(ctx context.Context, t tasks.Task) error {
	current, err := a.h.core.Load(ctx, t.Ref)
	if err != nil {
		return err
	}
	if current.Task.Version != t.Version {
		return fetch.Denied
	}
	content, err := taskcontent.New(a.h.content, a.queries, tasks.QualificationOf(current))
	if err != nil {
		return err
	}
	for _, ref := range t.InputRefs {
		parsed, err := answers.ParseReference(ref)
		if err != nil || parsed.Namespace != t.Ref.Namespace {
			return fetch.Denied
		}
		// Research inputs are nonempty JSON. READ checks current process/disclose
		// authority and full stored-content integrity even for a one-byte range.
		// Do not substitute a discovery-only metadata lookup for this probe.
		out, err := content.Call(ctx, artifacts.Binding{Token: a.h.token, Namespace: t.Ref.Namespace, Location: "local", Recipient: "local"}, &wire.ContentRequest{Method: "READ", Ref: parsed, Purpose: a.h.cap.Purpose, Limit: 1})
		if err != nil {
			return err
		}
		record := out.GetRecord()
		spec := record.GetSpec()
		if len(out.GetData()) != 1 || record.GetState() != "available" || record.GetRef() == nil || answers.Reference(record.Ref) != ref || spec.GetResource() != a.h.cap.Resource || spec.GetPurpose() != a.h.cap.Purpose || spec.GetMediaType() != "application/json" || spec.GetSize() > 32768 {
			return fetch.Denied
		}
	}
	return nil
}
