// Package searchprivacy rechecks the controlled query input at the actual
// search processing/disclosure location. A readable local input is not an
// authorization to send that input to an external search service.
package searchprivacy

import (
	"bytes"
	"context"
	"lerna/adapters/executioncontent"
	"lerna/artifacts"
	"lerna/execution"
	"lerna/fetch"
	"time"
)

type InputReader interface {
	Read(context.Context, string, string, execution.Capability) ([]byte, error)
}
type Guard struct {
	reader     *executioncontent.Adapter
	content    executioncontent.Content
	queries    ExecutionQueries
	token      string
	capability execution.Capability
	recipient  string
}

func New(content executioncontent.Content, binding artifacts.Binding, clock executioncontent.Clock, capability execution.Capability, recipient string) (*Guard, error) {
	if content == nil || clock == nil || binding.Token == "" || binding.Namespace == "" || capability.Name == "" || capability.Resource == "" || capability.Purpose == "" || recipient == "" || len(recipient) > 256 {
		return nil, fetch.Invalid
	}
	// Execution has already checked local access. This separate read verifies
	// both processing and disclosure at the external service's actual location.
	binding.Location = recipient
	binding.Recipient = recipient
	reader := executioncontent.New(content, binding, clock)
	return &Guard{reader: reader, content: content, token: binding.Token, capability: capability, recipient: recipient}, nil
}

func (g *Guard) Check(ctx context.Context, call execution.Call) error {
	if call.Request.InputRef == "" || call.Request.DescriptorSHA256 != g.capability.Digest() {
		return fetch.Denied
	}
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	target := g.capability
	target.Location = g.recipient
	reader, err := g.forRequest(call.Request)
	if err != nil {
		return err
	}
	data, err := reader.Read(ctx, g.token, call.Request.InputRef, target)
	if err != nil || len(data) == 0 || !bytes.Equal(data, call.Input) {
		return fetch.Denied
	}
	return nil
}
