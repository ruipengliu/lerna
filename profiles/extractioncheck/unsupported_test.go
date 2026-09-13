package extractioncheck

import (
	"context"
	"encoding/json"
	"lerna/adapters/executionlocal"
	"lerna/answers"
	"lerna/artifacts"
	wire "lerna/gen/harness/v1"
	"lerna/memory"
	"lerna/sdk"
	"testing"
)

func TestUnsupportedExtractionIsQueryableWithoutSavingCandidate(t *testing.T) {
	checkUnsupported(t, false)
}
func TestUnsupportedExtractionWithholdsReasonAfterDisclosureRevocation(t *testing.T) {
	checkUnsupported(t, true)
}
func checkUnsupported(t *testing.T, denyDisclosure bool) {
	t.Helper()
	ctx := context.Background()
	h, err := fresh(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer h.destroy()
	installObservedChoices(t, h)
	store, saver := bindProcessSave(t, h, "")
	h.client = sdk.NewCapabilityClient(executionlocal.Bind(h.exec, "local"), "local")
	r, grant, err := h.request(ctx, []byte(`{"sources":[{"kind":"choice","key":"one","revision":1},{"kind":"choice","key":"two","revision":1}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = h.client.Invoke(ctx, r, grant); err != nil {
		t.Fatal(err)
	}
	if denyDisclosure {
		if err = revokeSaveAction(ctx, h, "memory.candidate.disclose"); err != nil {
			t.Fatal(err)
		}
	}
	out, err := h.exec.Run(ctx, r.OperationID)
	if err != nil {
		t.Fatal(err)
	}
	if out.Effect != "NOT_OCCURRED" || out.Result != "FAILURE" || (out.Reference == "") != denyDisclosure {
		t.Fatalf("unsupported extraction stayed ambiguous: %s/%s %v", out.Effect, out.Result, err)
	}
	if err = h.exec.Drain(ctx, 16); err != nil {
		t.Fatal(err)
	}
	queried, err := h.client.GetInvocation(ctx, r.OperationID)
	if err != nil || queried.Effect != "NOT_OCCURRED" || queried.Reference != out.Reference {
		t.Fatalf("SDK unsupported query: %v", err)
	}
	state, err := h.candidates.Inspect(ctx, "local", r.OperationID)
	if err != nil || state.State != "unsupported" || state.Reason != "insufficient_evidence" || state.Committed {
		t.Fatalf("unsupported fact: %+v %v", state, err)
	}
	if _, err = saver.Inspect(ctx, r.OperationID); err != memory.Missing {
		t.Fatalf("unsupported extraction created a save intent: %v", err)
	}
	changes, err := store.ReadChanges(ctx, "local", "personal", 0, 16)
	if err != nil || len(changes) != 0 {
		t.Fatalf("unsupported extraction saved Memory: %v", err)
	}
	if denyDisclosure {
		return
	}
	ref, err := answers.ParseReference(out.Reference)
	if err != nil {
		t.Fatal(err)
	}
	b := artifacts.Binding{Token: h.token, Namespace: "local", Location: "local", Recipient: "local"}
	meta, err := h.content.Call(ctx, b, &wire.ContentRequest{Method: "GET", Ref: ref, Purpose: "task"})
	if err != nil {
		t.Fatal(err)
	}
	body, err := h.content.Call(ctx, b, &wire.ContentRequest{Method: "READ", Ref: ref, Purpose: "task", Limit: uint32(meta.Record.Spec.Size)})
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		Evidence struct {
			State, Reason string
			Committed     bool
		}
	}
	if err = json.Unmarshal(body.Data, &result); err != nil || result.Evidence.State != "unsupported" || result.Evidence.Reason != "insufficient_evidence" || result.Evidence.Committed {
		t.Fatalf("unsupported reason not queryable: %v", err)
	}
	task, err := h.core.Get(ctx, h.token, r.Qualification.Ref)
	if err != nil || task.State == "COMPLETED" {
		t.Fatalf("unsupported task claimed success: %v", err)
	}
	replay, err := h.exec.Run(ctx, r.OperationID)
	if err != nil || replay.Revision != out.Revision || replay.Reference != out.Reference {
		t.Fatalf("unsupported retry changed original outcome: %v", err)
	}
}
