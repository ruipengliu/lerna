// Package brain separates answer strategy from model generation and Core state.
package brain

import (
	"context"
	"lerna/tasks"
)

// These hard envelope bounds are shared by generation, admission, query and
// recovery. Per-Brain configuration may tighten them, never widen them.
const (
	MaxInputBytes  = 32768
	MaxAnswerBytes = 8192
)

type Capabilities struct {
	Model, Location, Version                string
	Text, Structured, Streaming, HardBounds bool
	ContextTokens, InputUpper               uint64
}
type Input struct {
	Goal, Constraints string
	Blocks            []Block
}
type ReplyTo struct{ InteractionID, QuestionRef string }
type Block struct {
	ReplyTo       []ReplyTo `json:",omitempty"`
	Ref, Text     string
	Subject, Role string
}
type Request struct {
	// Contract selects a host-defined structured proposal schema, never a tool
	// authorization. Empty preserves the answer-only contract.
	Contract            string
	Input               Input
	MaxInput, MaxOutput uint64
}
type Usage struct {
	Known         bool
	Input, Output uint64
}
type Result struct {
	// ProviderContent retains original model text when an adapter translates its wire format.
	// It carries the same disclosure restrictions as Content and is never an answer itself.
	ProviderContent    []byte `json:",omitempty"`
	Content            []byte
	Finish, RequestRef string
	Usage              Usage
}
type Model interface {
	Capabilities() Capabilities
	Generate(context.Context, Request) (Result, error)
}
type Error string

func (e Error) Error() string          { return string(e) }
func (e Error) DecisionReason() string { return string(e) }

type Context interface {
	Assemble(context.Context, tasks.Task, string, int) (Input, error)
	Validate(context.Context, tasks.Task, string) error
}
type Output interface {
	Save(context.Context, tasks.DecisionInput, []byte) (string, error)
}
type Accounting interface {
	BeginRequest(context.Context, tasks.Qualification, uint32) error
	CheckDecision(context.Context, tasks.Qualification) error
	Settle(context.Context, tasks.Qualification, tasks.GenerationUsage) error
}
