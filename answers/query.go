package answers

import (
	"context"
	"lerna/artifacts"
	"lerna/brain"
	wire "lerna/gen/harness/v1"
	"lerna/tasks"
)

type TaskReader interface {
	Get(context.Context, tasks.Ref) (tasks.Task, error)
}

// View distinguishes the historical Core result from current content access.
// Delivered is always false: this slice has no UI acknowledgement protocol.
type View struct {
	Task         tasks.Task
	Availability string
	Body         []byte
	Delivered    bool
}

func Query(ctx context.Context, reader TaskReader, ref tasks.Ref, content Content, binding artifacts.Binding, purpose string) (View, error) {
	t, e := reader.Get(ctx, ref)
	if e != nil {
		return View{}, e
	}
	v := View{Task: t, Availability: "not_published"}
	if t.State != "COMPLETED" {
		return v, nil
	}
	r, e := ParseReference(t.Result)
	if e != nil {
		return View{}, e
	}
	meta, e := content.Call(ctx, binding, &wire.ContentRequest{Method: "GET", Ref: r, Purpose: purpose})
	if e != nil {
		return View{}, e
	}
	v.Availability = meta.GetRecord().GetState()
	if v.Availability != "available" {
		return v, nil
	}
	n := meta.Record.Spec.Size
	if n == 0 || n > brain.MaxAnswerBytes {
		return View{}, brain.Error("OUTPUT_INVALID")
	}
	body, e := content.Call(ctx, binding, &wire.ContentRequest{Method: "READ", Ref: r, Purpose: purpose, Limit: uint32(n)})
	if e != nil {
		return View{}, e
	}
	v.Body = body.Data
	return v, nil
}
