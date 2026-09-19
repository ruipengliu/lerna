package answer

import (
	"context"
	"fmt"
	"google.golang.org/protobuf/proto"
	contentpolicy "lerna/adapters/content/policy"
	"lerna/answers"
	"lerna/brain"
	wire "lerna/gen/harness/v1"
	"lerna/tasks"
	"testing"
	"time"
)

func TestPublishesControlledAnswer(t *testing.T) {
	ctx := context.Background()
	l := tasks.GenerationLimits{Requests: 1, InputTokens: 600, OutputTokens: 100}
	h, e := fresh(ctx, l)
	if e != nil {
		t.Fatal(e)
	}
	defer h.destroy()
	s, e := h.submit(ctx, l)
	if e != nil {
		t.Fatal(e)
	}
	m := &controlledModel{}
	out, e := h.run(ctx, s, m)
	if e != nil {
		t.Fatal(e)
	}
	if out.Task.State != "COMPLETED" || out.Task.ModelUsedTokens != 70 || out.Task.ModelReservedTokens != 0 || m.calls != 1 {
		t.Fatalf("result %+v calls %d", out.Task, m.calls)
	}
	ref, e := answers.ParseReference(out.Task.Result)
	if e != nil {
		t.Fatal(e)
	}
	meta, e := h.content.Call(ctx, h.binding(), &wire.ContentRequest{Method: "GET", Ref: ref, Purpose: "task"})
	if e != nil {
		t.Fatal(e)
	}
	got, e := h.content.Call(ctx, h.binding(), &wire.ContentRequest{Method: "READ", Ref: ref, Purpose: "task", Limit: uint32(meta.Record.Spec.Size)})
	if e != nil || len(got.GetData()) == 0 {
		t.Fatalf("query: %v", e)
	}
}

func TestAnswerContracts(t *testing.T) {
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			if e := check(context.Background(), name); e != nil {
				t.Fatal(e)
			}
		})
	}
}

func TestAnswerEdges(t *testing.T) {
	for _, name := range edgeNames {
		t.Run(name, func(t *testing.T) {
			if e := edge(context.Background(), name); e != nil {
				t.Fatal(e)
			}
		})
	}
}

func TestRejectsConfigurationBeyondReadEnvelope(t *testing.T) {
	h, e := fresh(context.Background(), tasks.GenerationLimits{Requests: 1, InputTokens: 600, OutputTokens: 100})
	if e != nil {
		t.Fatal(e)
	}
	defer h.destroy()
	for _, c := range []brain.Config{{MaxInputBytes: 32769, MaxOutputBytes: 8192, SettlementTimeout: time.Second}, {MaxInputBytes: 32768, MaxOutputBytes: 8193, SettlementTimeout: time.Second}} {
		if _, e = brain.NewAnswer(&controlledModel{}, h.access, h.access, h.generation, c); e == nil {
			t.Fatal("accepted configuration producing unreadable answers")
		}
	}
}

type invalidPublicationContext struct{ brain.Context }

func (invalidPublicationContext) Validate(context.Context, tasks.Task, string) error {
	return brain.Error("CONTEXT_INVALIDATED")
}
func TestSavedAnswerCannotPublishWithInvalidContext(t *testing.T) {
	ctx := context.Background()
	limits := tasks.GenerationLimits{Requests: 1, InputTokens: 600, OutputTokens: 100}
	h, e := fresh(ctx, limits)
	if e != nil {
		t.Fatal(e)
	}
	defer h.destroy()
	initial, e := h.submit(ctx, limits)
	if e != nil {
		t.Fatal(e)
	}
	h.port, e = answers.BindContextPort(h.generation, h.access, h.location, invalidPublicationContext{h.access})
	if e != nil {
		t.Fatal(e)
	}
	model := &controlledModel{}
	_, _ = h.run(ctx, initial, model)
	current, e := h.generation.Current(ctx, initial.Task.Ref)
	if e != nil {
		t.Fatal(e)
	}
	if model.calls != 1 || current.Task.State == "COMPLETED" || len(current.Generations) != 1 || current.Generations[0].Publication != nil {
		t.Fatalf("invalid context publication state=%s calls=%d", current.Task.State, model.calls)
	}
	output, e := h.content.Call(ctx, h.binding(), &wire.ContentRequest{Method: "LOOKUP", OperationId: current.Generations[0].OutputOperation, Purpose: "task"})
	if e != nil || output.GetRecord().GetState() != "available" {
		t.Fatalf("test did not reach saved output %v", e)
	}
}

type additionalLineage struct {
	source *wire.ContentSource
	until  int64
}

func (l additionalLineage) Sources(context.Context, tasks.Task, string) (answers.Lineage, error) {
	return answers.Lineage{Sources: []*wire.ContentSource{l.source}, RetainUntil: l.until}, nil
}
func TestAnswerStoresAndEnforcesAdditionalSourceLineage(t *testing.T) {
	for _, allowed := range []bool{true, false} {
		t.Run(fmt.Sprint(allowed), func(t *testing.T) {
			ctx := context.Background()
			l := tasks.GenerationLimits{Requests: 1, InputTokens: 600, OutputTokens: 100}
			h, e := fresh(ctx, l)
			if e != nil {
				t.Fatal(e)
			}
			defer h.destroy()
			source := &wire.ContentSource{Kind: "memory", Key: "public-preference", Revision: 1}
			until := time.Now().Add(2 * time.Minute).Unix()
			rules := []contentpolicy.Rule{h.rule(true)}
			if allowed {
				rules = append(rules, contentpolicy.Rule{Kind: source.Kind, Key: source.Key, Revision: 1, Actions: []string{"store", "process", "retain", "discover", "disclose"}, Purposes: []string{"task"}, Locations: []string{"local", h.location}, RetainUntil: until})
			}
			if e = h.policy.Replace(rules); e != nil {
				t.Fatal(e)
			}
			h.access, e = h.access.WithLineage(additionalLineage{source, until})
			if e != nil {
				t.Fatal(e)
			}
			h.port = answers.BindPort(h.generation, h.access, h.location)
			s, e := h.submit(ctx, l)
			if e != nil {
				t.Fatal(e)
			}
			out, runErr := h.run(ctx, s, &controlledModel{})
			current, e := h.generation.Current(ctx, s.Task.Ref)
			if e != nil {
				t.Fatal(e)
			}
			if !allowed {
				if current.Task.State == "COMPLETED" {
					t.Fatal("published unauthorized derived output")
				}
				return
			}
			if runErr != nil || out.Task.State != "COMPLETED" {
				t.Fatalf("publish %s %v", out.Task.State, runErr)
			}
			ref, e := answers.ParseReference(out.Task.Result)
			if e != nil {
				t.Fatal(e)
			}
			meta, e := h.content.Call(ctx, h.binding(), &wire.ContentRequest{Method: "GET", Ref: ref, Purpose: "task"})
			if e != nil {
				t.Fatal(e)
			}
			found := false
			for _, s := range meta.Record.Spec.Sources {
				if proto.Equal(s, source) {
					found = true
				}
			}
			if !found || meta.Record.Spec.RetainUntil > until {
				t.Fatalf("lost lineage %+v", meta.Record.Spec)
			}
			if e = h.policy.Replace([]contentpolicy.Rule{h.rule(true)}); e != nil {
				t.Fatal(e)
			}
			if _, e = h.content.Call(ctx, h.binding(), &wire.ContentRequest{Method: "READ", Ref: ref, Purpose: "task", Limit: 1}); e == nil {
				t.Fatal("read bypassed revoked derived source")
			}
		})
	}
}
