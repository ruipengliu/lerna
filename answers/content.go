// Package answers assembles the controlled answer slice from semantic interfaces.
package answers

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"google.golang.org/protobuf/proto"
	"lerna/artifacts"
	"lerna/authorization"
	"lerna/brain"
	wire "lerna/gen/harness/v1"
	"lerna/tasks"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

type Content interface {
	Call(context.Context, artifacts.Binding, *wire.ContentRequest) (*wire.ContentResponse, error)
}
type Clock interface{ Now() (time.Time, error) }
type ContentAccess struct {
	lineage   LineageProvider
	content   Content
	policy    artifacts.Sources
	binding   artifacts.Binding
	goal      *wire.ContentSource
	purpose   string
	clock     Clock
	retention time.Duration
}

func NewContentAccess(content Content, policy artifacts.Sources, binding artifacts.Binding, goal *wire.ContentSource, purpose string, clock Clock, retention time.Duration) (*ContentAccess, error) {
	if content == nil || policy == nil || goal == nil || clock == nil || purpose == "" || retention < time.Second || retention > time.Hour {
		return nil, brain.Error("INVALID_ARGUMENT")
	}
	return &ContentAccess{content: content, policy: policy, binding: binding, goal: proto.Clone(goal).(*wire.ContentSource), purpose: purpose, clock: clock, retention: retention}, nil
}
func Reference(r *wire.ContentRef) string {
	return "content:" + r.Namespace + ":" + r.Key + ":" + strconv.FormatUint(r.Revision, 10)
}
func ParseReference(text string) (*wire.ContentRef, error) {
	parts := strings.Split(text, ":")
	if len(parts) != 4 || parts[0] != "content" || parts[1] == "" || len(parts[1]) > 128 || len(parts[2]) != 64 || parts[3] != "1" {
		return nil, brain.Error("INPUT_INVALIDATED")
	}
	raw, err := hex.DecodeString(parts[2])
	if err != nil || len(raw) != 32 || strings.ToLower(parts[2]) != parts[2] {
		return nil, brain.Error("INPUT_INVALIDATED")
	}
	return &wire.ContentRef{Namespace: parts[1], Key: parts[2], Revision: 1}, nil
}
func (a *ContentAccess) metadata(ctx context.Context, t tasks.Task) ([]*wire.ContentRecord, error) {
	if (t.GoalRef != "" && !slices.Contains(t.InputRefs, t.GoalRef)) || (t.GuidanceRef != "" && !slices.Contains(t.InputRefs, t.GuidanceRef)) || t.Ref.Namespace != a.binding.Namespace || len(t.InputRefs) > 16 {
		return nil, brain.Error("INPUT_INVALIDATED")
	}
	out := []*wire.ContentRecord{}
	for _, ref := range t.InputRefs {
		r, err := ParseReference(ref)
		if err != nil {
			return nil, err
		}
		resp, err := a.content.Call(ctx, a.binding, &wire.ContentRequest{Method: "GET", Ref: r, Purpose: a.purpose})
		if err != nil || resp.GetRecord().GetState() != "available" {
			return nil, brain.Error("INPUT_INVALIDATED")
		}
		out = append(out, resp.Record)
	}
	return out, nil
}
func (a *ContentAccess) Validate(ctx context.Context, t tasks.Task, location string) error {
	now, err := a.clock.Now()
	if err != nil {
		return brain.Error("INPUT_INVALIDATED")
	}
	if t.GoalRef == "" {
		for _, action := range []string{"process", "disclose"} {
			if err = a.policy.Check(ctx, a.goal, action, a.purpose, location, now.Unix()); err != nil {
				return brain.Error("PROCESSING_DENIED")
			}
		}
	}
	meta, err := a.metadata(ctx, t)
	if err != nil {
		return err
	}
	for _, r := range meta {
		for _, source := range r.Spec.Sources {
			for _, action := range []string{"process", "disclose"} {
				if err = a.policy.Check(ctx, source, action, a.purpose, location, r.Spec.RetainUntil); err != nil {
					return brain.Error("PROCESSING_DENIED")
				}
			}
		}
	}
	return nil
}
func (a *ContentAccess) Assemble(ctx context.Context, t tasks.Task, location string, limit int) (brain.Input, error) {
	var input brain.Input
	if err := a.Validate(ctx, t, location); err != nil {
		return input, err
	}
	constraints, _ := json.Marshal(t.Constraints)
	input = brain.Input{Goal: t.Goal, Constraints: string(constraints)}
	meta, err := a.metadata(ctx, t)
	if err != nil {
		return input, err
	}
	for i, r := range meta {
		if r.Spec.MediaType != "text/plain" || r.Spec.Size > uint64(limit) {
			return brain.Input{}, brain.Error("INPUT_BUDGET_EXCEEDED")
		}
		var data []byte
		b := a.binding
		b.Recipient = location
		for offset := uint64(0); offset < r.Spec.Size; {
			n := min(uint64(16384), r.Spec.Size-offset)
			out, err := a.content.Call(ctx, b, &wire.ContentRequest{Method: "READ", Ref: r.Ref, Purpose: a.purpose, Offset: offset, Limit: uint32(n)})
			if err != nil {
				return brain.Input{}, brain.Error("INPUT_INVALIDATED")
			}
			if uint64(len(out.Data)) != n {
				return brain.Input{}, brain.Error("INPUT_INVALIDATED")
			}
			data = append(data, out.Data...)
			offset += n
		}
		if !utf8.Valid(data) {
			return brain.Input{}, brain.Error("INPUT_INVALIDATED")
		}
		role, subject := "evidence", t.Subject
		replies := []brain.ReplyTo{}
		for _, f := range t.InputFacts {
			if f.Reference == t.InputRefs[i] {
				role = f.Kind
				subject = f.Subject
				if f.Kind == "reply" {
					if f.InteractionID == "" || !slices.Contains(t.InputRefs, f.QuestionRef) {
						return brain.Input{}, brain.Error("INPUT_INVALIDATED")
					}
					replies = append(replies, brain.ReplyTo{InteractionID: f.InteractionID, QuestionRef: f.QuestionRef})
				}
			}
		}
		if t.InputRefs[i] == t.GoalRef {
			input.Goal = string(data)
			role = "goal"
		}
		if t.InputRefs[i] == t.GuidanceRef {
			merged, _ := json.Marshal(struct {
				Limits   tasks.Constraints
				Guidance string
			}{t.Constraints, string(data)})
			input.Constraints = string(merged)
			role = "constraint"
		}
		input.Blocks = append(input.Blocks, brain.Block{Ref: t.InputRefs[i], Text: string(data), Subject: subject, Role: role, ReplyTo: replies})
		encoded, _ := json.Marshal(input)
		if len(encoded) > limit {
			return brain.Input{}, brain.Error("INPUT_BUDGET_EXCEEDED")
		}
	}
	return input, nil
}
func (a *ContentAccess) Save(ctx context.Context, in tasks.DecisionInput, data []byte) (string, error) {
	now, err := a.clock.Now()
	if err != nil {
		return "", brain.Error("INPUT_INVALIDATED")
	}
	meta, err := a.metadata(ctx, in.Task)
	if err != nil {
		return "", err
	}
	sources := []*wire.ContentSource{}
	if in.Task.GoalRef == "" {
		sources = append(sources, a.goal)
	}
	until := now.Add(a.retention).Unix()
	for _, r := range meta {
		until = min(until, r.Spec.RetainUntil)
		sources = append(sources, r.Spec.Sources...)
	}
	if a.lineage != nil {
		lineage, e := a.lineage.Sources(ctx, in.Task, a.binding.Location)
		if e != nil {
			return "", e
		}
		if len(lineage.Sources) > 16 || (len(lineage.Sources) > 0 && lineage.RetainUntil <= now.Unix()) {
			return "", brain.Error("INPUT_INVALIDATED")
		}
		for _, source := range lineage.Sources {
			if source == nil || source.Kind == "" || source.Key == "" || source.Revision == 0 {
				return "", brain.Error("INPUT_INVALIDATED")
			}
			sources = append(sources, proto.Clone(source).(*wire.ContentSource))
		}
		if lineage.RetainUntil > 0 {
			until = min(until, lineage.RetainUntil)
		}
	}
	seen := map[string]bool{}
	unique := []*wire.ContentSource{}
	for _, src := range sources {
		identity, _ := json.Marshal([]any{src.Kind, src.Key, src.Revision})
		key := string(identity)
		if !seen[key] {
			unique = append(unique, src)
			seen[key] = true
		}
	}
	if len(unique) > 16 {
		return "", brain.Error("INPUT_BUDGET_EXCEEDED")
	}
	sum := sha256.Sum256(data)
	out, err := a.content.Call(ctx, a.binding, &wire.ContentRequest{Method: "PUT", OperationId: in.Generation.OutputOperation, Data: data, Spec: &wire.ContentSpec{Kind: "artifact", Resource: in.Task.Resource, Purpose: a.purpose, Sources: unique, AcquiredAt: now.Unix(), MediaType: "application/json", Size: uint64(len(data)), Sha256: hex.EncodeToString(sum[:]), RetainUntil: until}})
	if artifacts.Code(err) == "OUTCOME_UNKNOWN" || authorization.Is(err, authorization.OutcomeUnknown) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		bounded, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
		defer cancel()
		out, err = a.content.Call(bounded, a.binding, &wire.ContentRequest{Method: "LOOKUP", OperationId: in.Generation.OutputOperation, Purpose: a.purpose})
	}
	if err != nil {
		return "", err
	}
	if out.GetRecord().GetState() != "available" {
		return "", brain.Error("INPUT_INVALIDATED")
	}
	return Reference(out.Record.Ref), nil
}

// ValidateResult is used immediately before Core's version/control CAS. It
// confirms the approved result is still readable, without disclosing its body.
func (a *ContentAccess) ValidateResult(ctx context.Context, result string) error {
	ref, err := ParseReference(result)
	if err != nil {
		return err
	}
	out, err := a.content.Call(ctx, a.binding, &wire.ContentRequest{Method: "GET", Ref: ref, Purpose: a.purpose})
	if err != nil || out.GetRecord().GetState() != "available" {
		return brain.Error("INPUT_INVALIDATED")
	}
	return nil
}
