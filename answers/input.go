package answers

import (
	"context"
	"lerna/artifacts"
	"lerna/authorization"
	"lerna/brain"
	wire "lerna/gen/harness/v1"
	"lerna/tasks"
	"slices"
	"unicode/utf8"
)

// InputValidator binds the local content authority to formal task input. Bodies
// are validated here but only immutable references enter the task journal.
type InputValidator struct {
	content Content
	binding artifacts.Binding
	purpose string
}

func NewInputValidator(content Content, binding artifacts.Binding, purpose string) (*InputValidator, error) {
	if content == nil || binding.Namespace == "" || binding.Location == "" || purpose == "" {
		return nil, &authorization.Error{Code: authorization.Invalid}
	}
	return &InputValidator{content, binding, purpose}, nil
}
func (v *InputValidator) ValidateInput(ctx context.Context, token string, t tasks.Task, refs []string, answer *tasks.AnswerConstraint) error {
	if len(refs) > 16 || t.Ref.Namespace != v.binding.Namespace {
		return &authorization.Error{Code: authorization.Invalid}
	}
	b := v.binding
	b.Token = token
	b.Recipient = b.Location
	for _, text := range refs {
		ref, e := ParseReference(text)
		if e != nil {
			return &authorization.Error{Code: authorization.Invalid}
		}
		meta, e := v.content.Call(ctx, b, &wire.ContentRequest{Method: "GET", Ref: ref, Purpose: v.purpose})
		if e != nil {
			return &authorization.Error{Code: authorization.Denied}
		}
		record := meta.GetRecord()
		if record.GetState() != "available" {
			return &authorization.Error{Code: authorization.Conflict}
		}
		spec := record.Spec
		limit := uint64(brain.MaxInputBytes)
		if answer != nil {
			limit = uint64(answer.MaxBytes)
		}
		if spec.Resource != t.Resource || spec.Purpose != v.purpose || spec.MediaType != "text/plain" || spec.Size == 0 || spec.Size > limit {
			return &authorization.Error{Code: authorization.Invalid}
		}
		var data []byte
		for offset := uint64(0); offset < spec.Size; {
			n := min(uint64(16384), spec.Size-offset)
			out, e := v.content.Call(ctx, b, &wire.ContentRequest{Method: "READ", Ref: ref, Purpose: v.purpose, Offset: offset, Limit: uint32(n)})
			if e != nil {
				return &authorization.Error{Code: authorization.Denied}
			}
			if uint64(len(out.Data)) != n {
				return &authorization.Error{Code: authorization.Invalid}
			}
			data = append(data, out.Data...)
			offset += n
		}
		if !utf8.Valid(data) || (answer != nil && answer.Kind == "choice" && !slices.Contains(answer.Choices, string(data))) {
			return &authorization.Error{Code: authorization.Invalid}
		}
	}
	return nil
}
