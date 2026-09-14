package brain

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"lerna/internal/jsonvalue"
	"lerna/tasks"
)

const DelegationContract = "harness_delegation_v1"

// DelegationOutput is a finite proposal. Operation identities are assigned by
// the host after governed output persistence; model text cannot mint authority.
type DelegationOutput struct {
	Children []tasks.ChildSpec `json:"children"`
}

// NewDelegations uses the existing generation reservation, current context,
// output storage and usage settlement path. The host reads the saved proposal
// and passes it through Core's DelegationPort.Admit before any child submission.
func NewDelegations(m Model, c Context, o Output, a Accounting, config Config) (*AnswerBrain, error) {
	b, e := NewAnswer(m, c, o, a, config)
	if e != nil {
		return nil, e
	}
	b.contract = DelegationContract
	return b, nil
}
func parseDelegation(raw []byte, in Input, limit int) (DelegationOutput, error) {
	var out DelegationOutput
	if len(raw) > limit {
		return out, Error("OUTPUT_INVALID")
	}
	if _, e := jsonvalue.Decode(raw); e != nil {
		return out, Error("OUTPUT_INVALID")
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&out) != nil || d.Decode(new(any)) != io.EOF || len(out.Children) == 0 || len(out.Children) > 16 {
		return out, Error("OUTPUT_INVALID")
	}
	allowed := map[string]bool{}
	for _, b := range in.Blocks {
		if b.Role == "internal-agent" {
			allowed[b.Ref] = true
		}
	}
	for _, c := range out.Children {
		if !allowed[c.Agent] || c.OperationID != "" {
			return out, Error("OUTPUT_INVALID")
		}
	}
	return out, nil
}

// PrepareDelegation is local preparation only. Recover an already admitted
// proposal from Core instead of preparing new identities for the same handoff.
func PrepareDelegation(ctx context.Context, raw []byte, in Input, newOperation func(context.Context) (string, error)) (tasks.DelegationProposal, error) {
	out, e := parseDelegation(raw, in, MaxAnswerBytes)
	if e != nil {
		return tasks.DelegationProposal{}, e
	}
	if newOperation == nil {
		return tasks.DelegationProposal{}, Error("INVALID_ARGUMENT")
	}
	proposal := tasks.DelegationProposal{Children: out.Children}
	proposal.OperationID, e = newOperation(ctx)
	if e != nil {
		return tasks.DelegationProposal{}, e
	}
	for i := range proposal.Children {
		proposal.Children[i].OperationID, e = newOperation(ctx)
		if e != nil {
			return tasks.DelegationProposal{}, e
		}
	}
	return proposal, nil
}
