package extractioncheck

import (
	"context"
	"lerna/execution"
	"lerna/memory"
	"testing"
)

func TestOrdinaryExtractionRejectsAmbiguousSourceInput(t *testing.T) {
	for name, input := range map[string]string{
		"root-alias":       `{"Sources":[{"kind":"note","key":"one","revision":1}]}`,
		"source-alias":     `{"sources":[{"Kind":"note","key":"one","revision":1}]}`,
		"duplicate-field":  `{"sources":[{"kind":"note","key":"other","key":"one","revision":1}]}`,
		"duplicate-root":   `{"sources":[],"sources":[{"kind":"note","key":"one","revision":1}]}`,
		"duplicate-source": `{"sources":[{"kind":"note","key":"one","revision":1},{"kind":"note","key":"one","revision":1}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			h, err := fresh(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer h.destroy()
			request, _, err := h.request(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if err = h.target.Start(ctx, execution.Call{Request: request, Input: []byte(input)}); err != memory.Invalid {
				t.Fatalf("ambiguous source input accepted or reached effects: %v", err)
			}
			if _, err = h.candidates.Inspect(ctx, "local", request.OperationID); err != memory.Missing {
				t.Fatalf("invalid input retained candidate: %v", err)
			}
		})
	}
}
