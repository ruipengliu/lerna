// Package rules implements bounded local rules without network access.
package rules

import (
	"context"
	"google.golang.org/protobuf/proto"
	"lerna/extraction"
	wire "lerna/gen/harness/v1"
	"lerna/memory"
)

type Rules struct{}

var _ extraction.Extractor = Rules{}

func (Rules) Extract(ctx context.Context, in extraction.Input) (extraction.Result, error) {
	if err := ctx.Err(); err != nil {
		return extraction.Result{}, err
	}
	if in.About == "" || len(in.About) > 512 || len(in.Materials) == 0 || len(in.Materials) > 16 {
		return extraction.Result{}, memory.Invalid
	}
	for _, m := range in.Materials {
		if m.Source == nil || m.Source.Ref == nil || m.Source.Ref.Kind == "" || m.Source.Ref.Key == "" || m.Source.Ref.Revision == 0 || m.Source.Method == "" || m.Source.GetFragment() == "" || proto.Size(m.Source) > 2048 || len(m.Text) > 4096 || m.Speaker == "" || len(m.Speaker) > 512 {
			return extraction.Result{}, memory.Invalid
		}
	}
	if in.Materials[0].Choice != nil {
		return infer(in), nil
	}
	// A complete supported statement is required. Substring matching would turn
	// negation, quotations or mixed source instructions into false preferences.
	if len(in.Materials) != 1 {
		return extraction.Result{Reason: "unsupported"}, nil
	}
	m := in.Materials[0]
	if m.Speaker != in.About {
		return extraction.Result{Reason: "unsupported"}, nil
	}
	attribute, value := "", ""
	switch m.Text {
	case "回答时，我偏好简洁的说明。":
		attribute, value = "format", "concise"
	case "回答时，我偏好详细的说明。":
		attribute, value = "format", "detailed"
	case "回答时，我偏好中文。":
		attribute, value = "language", "zh"
	case "回答时，我偏好英文。":
		attribute, value = "language", "en"
	default:
		return extraction.Result{Reason: "unsupported"}, nil
	}
	return extraction.Result{Candidates: []extraction.Candidate{{
		About: in.About, Kind: "preference", Attribute: attribute, Value: value, Conditions: "answer",
		Sources:    []*wire.MemorySource{proto.Clone(m.Source).(*wire.MemorySource)},
		Confidence: &wire.MemoryConfidence{Assessment: "explicit", Basis: "本人明确陈述", Method: "local-rules-v1"},
	}}}, nil
}

func infer(in extraction.Input) extraction.Result {
	if len(in.Materials) != 3 {
		return extraction.Result{Reason: "insufficient_evidence"}
	}
	sources := make([]*wire.MemorySource, 0, 3)
	events := map[string]bool{}
	refs := map[string]bool{}
	format := in.Materials[0].Choice.Format
	for _, m := range in.Materials {
		c := m.Choice
		if c == nil || m.Text != "" || m.Speaker != in.About || c.Task != "report" || (c.Format != "concise" && c.Format != "detailed") || c.Event == "" || len(c.Event) > 256 {
			return extraction.Result{Reason: "unsupported"}
		}
		if c.Format != format {
			return extraction.Result{Reason: "inconsistent_evidence"}
		}
		// A new revision or fragment of the same source is not an independent event.
		ref := m.Source.Ref.Kind + "\x00" + m.Source.Ref.Key
		if events[c.Event] || refs[ref] {
			return extraction.Result{Reason: "insufficient_evidence"}
		}
		events[c.Event] = true
		refs[ref] = true
		sources = append(sources, proto.Clone(m.Source).(*wire.MemorySource))
	}
	return extraction.Result{Candidates: []extraction.Candidate{{About: in.About, Kind: "inference", Attribute: "format", Value: format, Conditions: "report", Sources: sources, Confidence: &wire.MemoryConfidence{Assessment: "tentative", Basis: "三个独立且一致的报告格式选择", Method: "local-rules-v1"}}}}
}
