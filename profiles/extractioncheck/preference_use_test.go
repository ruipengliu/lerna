package extractioncheck

import (
	"context"
	"lerna/adapters/executionlocal"
	"lerna/sdk"
	"testing"
)

func TestExtractedPreferenceControlsIndependentTaskOutput(t *testing.T) {
	checkPreferenceUse(t, laterUseCase{Kind: "preference", Condition: "answer", Claim: "explicit-reply-format", Block: `{"claim":"explicit-reply-format","value":"concise"}`, Answer: "Report ready.", Sources: 1})
}

func TestInapplicableExtractedPreferenceDoesNotControlTaskOutput(t *testing.T) {
	checkPreferenceUse(t, laterUseCase{Kind: "preference", Condition: "report", Claim: "explicit-reply-format", Block: `{"claim":"explicit-reply-format","value":"concise"}`, Answer: "The report is ready. No applicable format preference was supplied.", Sources: 1, Inapplicable: true})
}

func checkPreferenceUse(t *testing.T, use laterUseCase) {
	t.Helper()
	ctx := context.Background()
	h, err := fresh(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer h.destroy()
	store, saver := bindProcessSave(t, h, "")
	h.client = sdk.NewCapabilityClient(executionlocal.Bind(h.exec, "local"), "local")
	r, grant, err := h.request(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = h.client.Invoke(ctx, r, grant); err != nil {
		t.Fatal(err)
	}
	if _, err = h.exec.Run(ctx, r.OperationID); err != nil {
		t.Fatal(err)
	}
	if err = h.exec.Drain(ctx, 16); err != nil {
		t.Fatal(err)
	}
	saved, err := saver.Inspect(ctx, r.OperationID)
	if err != nil || saved.State != "committed" || saved.Receipt == nil {
		t.Fatalf("automatic preference save: %v", err)
	}
	later, read := readSavedInLaterTask(t, h, store, *saved.Receipt, r.Qualification.Ref, use)
	if later.Ref == r.Qualification.Ref || len(read.Records) != 1 || read.Records[0].GetSpec().GetKind() != "preference" || read.Records[0].GetSpec().GetConfidence().GetAssessment() != "explicit" {
		t.Fatal("later task lost explicit preference identity")
	}
}
