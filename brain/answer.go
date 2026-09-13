package brain

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"lerna/internal/jsonvalue"
	"lerna/tasks"
	"time"
	"unicode/utf8"
)

// Answer is the complete fixed result schema. Ref strings must identify a block
// actually supplied to this decision. It never includes executable tool calls.
type Answer struct {
	Text    string   `json:"answer"`
	Sources []string `json:"sources"`
}
type Config struct {
	MaxInputBytes, MaxOutputBytes int
	SettlementTimeout             time.Duration
}
type AnswerBrain struct {
	model    Model
	context  Context
	output   Output
	account  Accounting
	config   Config
	contract string
}

func NewAnswer(m Model, c Context, o Output, a Accounting, config Config) (*AnswerBrain, error) {
	if m == nil || c == nil || o == nil || a == nil || config.MaxInputBytes < 1 || config.MaxInputBytes > MaxInputBytes || config.MaxOutputBytes < 1 || config.MaxOutputBytes > MaxAnswerBytes || config.SettlementTimeout <= 0 || config.SettlementTimeout > time.Second {
		return nil, Error("INVALID_ARGUMENT")
	}
	return &AnswerBrain{model: m, context: c, output: o, account: a, config: config}, nil
}
func (b *AnswerBrain) Decide(ctx context.Context, in tasks.DecisionInput) (proposal tasks.Proposal, err error) {
	g := in.Generation
	q := g.Qualification
	u := tasks.GenerationUsage{}
	settle := true
	if q.Version != in.Task.Version || q.Generation != in.Work.Generation || g.OutputOperation == "" || g.Settled {
		return proposal, Error("INPUT_INVALIDATED")
	}
	defer func() {
		if !settle {
			return
		}
		bounded, cancel := context.WithTimeout(context.WithoutCancel(ctx), b.config.SettlementTimeout)
		defer cancel()
		if e := b.account.Settle(bounded, q, u); e != nil {
			proposal = tasks.Proposal{}
			err = e
		}
	}()
	cap := b.model.Capabilities()
	if !cap.Text || !cap.Structured || !cap.HardBounds || cap.Location == "" || cap.InputUpper == 0 || g.Limits.InputTokens < cap.InputUpper || g.Limits.InputTokens+g.Limits.OutputTokens > cap.ContextTokens {
		return proposal, Error("CAPABILITY_UNAVAILABLE")
	}
	input, err := b.context.Assemble(ctx, in.Task, cap.Location, b.config.MaxInputBytes)
	if err != nil {
		return proposal, err
	}
	encoded, err := json.Marshal(input)
	if err != nil || len(encoded) > b.config.MaxInputBytes {
		return proposal, Error("INPUT_BUDGET_EXCEEDED")
	}
	for attempt := uint32(0); attempt < g.Limits.Requests; attempt++ {
		if ctx.Err() != nil {
			return proposal, Error("GENERATION_CANCELLED")
		}
		if err = b.account.CheckDecision(ctx, q); err != nil {
			return proposal, err
		}
		if err = b.context.Validate(ctx, in.Task, cap.Location); err != nil {
			return proposal, err
		}
		if err = b.account.BeginRequest(ctx, q, attempt); err != nil {
			// A dispatch reservation may have committed. Do not release it using an
			// unconfirmed zero-request settlement.
			settle = false
			return proposal, err
		}
		u.Requests++
		u.UnknownRequests++
		result, callErr := b.model.Generate(ctx, Request{Contract: b.contract, Input: input, MaxInput: g.Limits.InputTokens, MaxOutput: g.Limits.OutputTokens})
		if result.Usage.Known && result.Usage.Input <= g.Limits.InputTokens && result.Usage.Output <= g.Limits.OutputTokens {
			u.UnknownRequests--
			u.Tokens += result.Usage.Input + result.Usage.Output
		}
		if result.Usage.Known && (result.Usage.Input > g.Limits.InputTokens || result.Usage.Output > g.Limits.OutputTokens) {
			return proposal, Error("MODEL_USAGE_INVALID")
		}
		if callErr != nil {
			return proposal, callErr
		}
		if ctx.Err() != nil {
			return proposal, Error("GENERATION_CANCELLED")
		}
		if result.Finish != "stop" {
			return proposal, Error("OUTPUT_TRUNCATED")
		}
		answer, e := b.parseResult(result.Content, input)
		if e != nil {
			if attempt+1 < g.Limits.Requests {
				continue
			}
			return proposal, e
		}
		if err = b.context.Validate(ctx, in.Task, cap.Location); err != nil {
			return proposal, err
		}
		if err = b.account.CheckDecision(ctx, q); err != nil {
			return proposal, err
		}
		data, e := json.Marshal(answer)
		if e != nil || len(data) > b.config.MaxOutputBytes {
			return proposal, Error("OUTPUT_INVALID")
		}
		ref, err := b.output.Save(ctx, in, data)
		if err != nil {
			return proposal, err
		}
		return tasks.Proposal{Kind: "answer", BaseVersion: in.Task.Version, Complete: true, Result: ref}, nil
	}
	return proposal, Error("GENERATION_BUDGET_EXCEEDED")
}
func parse(data []byte, in Input, limit int) (Answer, error) {
	var out Answer
	if len(data) > limit || !utf8.Valid(data) {
		return out, Error("OUTPUT_INVALID")
	}
	// The shared parser rejects duplicate keys before typed decoding.
	value, err := jsonvalue.Decode(data)
	if err != nil {
		return out, Error("OUTPUT_INVALID")
	}
	object, ok := value.(map[string]any)
	if !ok || len(object) != 2 {
		return out, Error("OUTPUT_INVALID")
	}
	if _, ok = object["answer"]; !ok {
		return out, Error("OUTPUT_INVALID")
	}
	if _, ok = object["sources"]; !ok {
		return out, Error("OUTPUT_INVALID")
	}

	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&out) != nil || decoder.Decode(new(any)) != io.EOF || out.Text == "" || out.Sources == nil || len(out.Sources) > 64 {
		return Answer{}, Error("OUTPUT_INVALID")
	}
	allowed := map[string]bool{}
	for _, block := range in.Blocks {
		allowed[block.Ref] = true
	}
	seen := map[string]bool{}
	for _, ref := range out.Sources {
		if !allowed[ref] || seen[ref] {
			return Answer{}, Error("OUTPUT_INVALID")
		}
		seen[ref] = true
	}
	return out, nil
}

// ValidateAnswer rechecks a complete stored result during publication recovery.
func ValidateAnswer(data []byte, in Input, limit int) error {
	_, err := parse(data, in, limit)
	return err
}

// NewEvidenceAnswer selects the explicit evidence contract while retaining the
// same generation accounting, current-context checks and governed output path.
func NewEvidenceAnswer(m Model, c Context, o Output, a Accounting, config Config) (*AnswerBrain, error) {
	b, err := NewAnswer(m, c, o, a, config)
	if err != nil {
		return nil, err
	}
	b.contract = EvidenceAnswerContract
	return b, nil
}
func (b *AnswerBrain) parseResult(data []byte, in Input) (any, error) {
	if b.contract == EvidenceAnswerContract {
		return parseEvidenceAnswer(data, in, b.config.MaxOutputBytes)
	}
	return parse(data, in, b.config.MaxOutputBytes)
}
