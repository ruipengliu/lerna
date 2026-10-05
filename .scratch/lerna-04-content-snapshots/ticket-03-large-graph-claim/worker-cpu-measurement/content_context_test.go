//go:build integration

package component_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	fixture "github.com/ruipengliu/lerna/conformance/internal/decisionfixture"
	v "github.com/ruipengliu/lerna/contract/v1_1"
	c "github.com/ruipengliu/lerna/contract/v1_2"
	compiler "github.com/ruipengliu/lerna/domain/task/context"
	work "github.com/ruipengliu/lerna/runtime"
	"reflect"
	"runtime/pprof"
	"strconv"
	"strings"
	"testing"
	"time"
)

func publicContextBytes(t *testing.T, ctx context.Context, w *fixture.ContextWorld, ref c.ContentRef) []byte {
	t.Helper()
	raw, err := c.Encode(c.ContentGetRequest{ContractVersion: c.Version, Profile: "content", CommandID: "context-read", Target: c.ContentTarget{TenantID: ref.Owner.TenantID, OwnerID: ref.Owner.OwnerID, Kind: "content", ID: ref.ContentID}, Method: "content.get", AcceptBefore: c.Time(w.Input.Request.Payload.Deadline), Payload: c.ContentGetPayload{ContentRef: ref, Purpose: c.Purpose(w.Input.Purpose)}})
	if err != nil {
		t.Fatal(err)
	}
	response, err := w.Content.Get(ctx, raw, &w.Subject)
	if err != nil {
		t.Fatal(err)
	}
	found, ok := response.AsPublished()
	if !ok {
		t.Fatal("exact Content public bytes unavailable", response)
	}
	body, err := base64.StdEncoding.DecodeString(found.BytesBase64)
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func TestContentContextCompleteMandatoryBodyReachesRealRuleDecision(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	w := fixture.NewContextWorld(t, ctx)
	bundle, err := w.Compile(ctx)
	if err != nil {
		t.Fatal(err)
	}
	mandatory := publicContextBytes(t, ctx, w, bundle.Mandatory.Ref)
	m, err := compiler.DecodeMandatory(mandatory)
	if err != nil {
		t.Fatal(err)
	}
	if m.Input.Goal != w.Input.Goal || len(m.Input.Conditions) != 2 || len(m.Input.Unresolved) != 1 || m.Input.Unresolved[0].Effect != "unknown" || !m.Input.Unresolved[0].MayApplyLater || !reflect.DeepEqual(m.Processed, bundle.Processed) {
		t.Fatal("mandatory information/actual compiler processing lost")
	}
	for _, ref := range []c.ContentRef{bundle.Shell.Ref, bundle.Manifest.Ref, bundle.Lock.Ref} {
		if len(publicContextBytes(t, ctx, w, ref)) == 0 {
			t.Fatal("empty structural bytes")
		}
	}
	w.Reopen(ctx)
	fixed, err := w.Dispatcher.Observe(ctx, "context-input")
	if err != nil || fixed.Bundle == nil || fixed.Bundle.Mandatory.Ref != bundle.Mandatory.Ref || fixed.Bundle.Manifest.Ref != bundle.Manifest.Ref {
		t.Fatal("reopen changed fixed bundle", err)
	}
	if _, err = w.Dispatcher.Step(ctx, w.Boundary); err != nil {
		t.Fatal(err)
	}
	stepped, err := w.Decision.Step(ctx)
	if err != nil || stepped.Processed != 1 {
		t.Fatal("real rule failed", stepped, err)
	}
	request := v.DecisionGetRequest{ContractVersion: v.Version, Profile: "decision_engine", CommandID: "context-get", Target: w.Input.Request.Target, Method: "decision_engine.get", AcceptBefore: w.Input.Request.AcceptBefore, Payload: v.DecisionGetPayload{DecisionRef: w.Input.Request.Target}}
	raw, err := v.Encode(request)
	if err != nil {
		t.Fatal(err)
	}
	response, err := w.Decision.Get(ctx, raw, &w.Principal)
	if err != nil {
		t.Fatal(err)
	}
	found, ok := response.AsFound()
	if !ok {
		t.Fatal("rule not found")
	}
	completed, ok := found.Decision.AsCompleted()
	if !ok {
		t.Fatal("rule did not complete", found.Decision)
	}
	candidate, ok := completed.Proposal.Advance.AsCandidateResult()
	if !ok || len(candidate.ArtifactRefs) != 1 {
		t.Fatal("candidate result missing")
	}
	artifact := candidate.ArtifactRefs[0]
	exact := c.ContentRef{Owner: c.OwnerRef{TenantID: c.ID(artifact.Owner.TenantID), OwnerID: c.ID(artifact.Owner.OwnerID)}, ContentID: c.ID(artifact.ContentID), Version: string(artifact.Version), Hash: artifact.Hash, MediaType: artifact.MediaType, ByteLength: c.Revision(artifact.ByteLength)}
	output := publicContextBytes(t, ctx, w, exact)
	if !bytes.Equal(output, append([]byte("fixture result: "), mandatory...)) {
		t.Fatal("artifact did not echo entire mandatory canonical body")
	}
	proposalBytes, err := v.Encode(completed.Proposal)
	if err != nil {
		t.Fatal(err)
	}
	if string(completed.Usage.InputBytes) != strconv.FormatInt(bundle.InputBytes, 10) || int64(len(output)+len(proposalBytes)) != bundle.OutputBytes || string(completed.Usage.OutputBytes) != strconv.FormatInt(bundle.OutputBytes, 10) {
		t.Fatal("actual complete I/O differs from preflight", completed.Usage, bundle.InputBytes, bundle.OutputBytes)
	}
	if len(completed.Proposal.ProcessedSourceRefs) != len(bundle.Materials)+1 {
		t.Fatal("worker processed differs from its actual manifest/material set")
	}
	if bundle.ExtraReadBytes <= 0 {
		t.Fatal("compiler processing I/O unreported")
	}
}

func assertContextOverflowNoDecision(t *testing.T, ctx context.Context, w *fixture.ContextWorld, err error) {
	t.Helper()
	if !errors.Is(err, compiler.ErrOverflow) {
		t.Fatal("compile must return context_overflow", err)
	}
	observation, e := w.Dispatcher.Observe(ctx, "context-input")
	if e != nil || observation.Status != "context_overflow" || observation.Bundle != nil || observation.Attempts != 0 {
		t.Fatal("overflow retained a successful Snapshot or send qualification", e, observation.Status)
	}
	assertContextNoDispatchFacts(t, ctx, w)
}

func assertContextNoDispatchFacts(t *testing.T, ctx context.Context, w *fixture.ContextWorld) {
	t.Helper()
	observation, e := w.Dispatcher.Observe(ctx, "context-input")
	if e != nil || observation.BindingPresent || observation.DispatchPresent || observation.Bundle != nil || observation.Receipt != nil || observation.Attempts != 0 || observation.InputRevision != w.Input.Revision {
		t.Fatal("precise original binding/dispatch absence not established", e)
	}
	if sent, e := w.Dispatcher.Step(ctx, w.Boundary); e != nil || sent {
		t.Fatal("overflow dispatch responsibility not closed", sent, e)
	}
	raw, e := v.Encode(v.DecisionGetRequest{ContractVersion: v.Version, Profile: "decision_engine", CommandID: "overflow-decision-query", Target: w.Input.Request.Target, Method: "decision_engine.get", AcceptBefore: w.Input.Request.AcceptBefore, Payload: v.DecisionGetPayload{DecisionRef: w.Input.Request.Target}})
	if e != nil {
		t.Fatal(e)
	}
	result, e := w.Decision.Get(ctx, raw, &w.Principal)
	if e != nil {
		t.Fatal(e)
	}
	// Frozen1.1 has no Decision not_found variant. This is only the
	// authorized supplementary result observation; absence is independently
	// established by the original Command.get not_found below.
	if _, ok := result.AsResultUnavailable(); !ok {
		t.Fatal("overflow unexpectedly has a Component Decision result", result)
	}
	raw, e = v.Encode(v.CommandGetRequest{ContractVersion: v.Version, Profile: "command", CommandID: "overflow-command-query", Target: v.CommandTarget{TenantID: w.Input.Request.Target.TenantID, OwnerID: w.Input.Request.Target.OwnerID, Kind: "command", ID: w.Input.Request.CommandID}, Method: "command.get", AcceptBefore: w.Input.Request.AcceptBefore, Payload: v.CommandGetPayload{CommandRef: v.CommandRef{Owner: v.OwnerRef{TenantID: w.Input.Request.Target.TenantID, OwnerID: w.Input.Request.Target.OwnerID}, CommandID: w.Input.Request.CommandID}}})
	if e != nil {
		t.Fatal(e)
	}
	command, e := w.Decision.GetCommand(ctx, raw, &w.Principal)
	if e != nil {
		t.Fatal(e)
	}
	if _, ok := command.AsNotFound(); !ok {
		t.Fatal("overflow has a Component receipt", command)
	}
}

func TestContentContextEchoReserveOverflowHasNoDecision(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	w := fixture.NewContextWorld(t, ctx)
	w.Input.Revision = "2"
	w.Input.Goal = strings.Repeat("x", 10000)
	if err := w.Dispatcher.InstallInput(ctx, "context-input", w.Input, "1"); err != nil {
		t.Fatal(err)
	}
	// Independent lower bound: echo alone exceeds 8192 (10000 required UTF8
	// goal bytes plus fixed prefix), while the entire input remains below 65536.
	_, err := w.Compile(ctx)
	assertContextOverflowNoDecision(t, ctx, w, err)
}

func completeContextDecision(t *testing.T, ctx context.Context, w *fixture.ContextWorld, b compiler.Bundle) v.DecisionCompleted {
	t.Helper()
	dispatchStart := time.Now()
	w.DiagnosticStage(ctx, "phase.dispatch-start", dispatchStart, nil)
	sent, dispatchErr := w.Dispatcher.Step(ctx, w.Boundary)
	w.DiagnosticStage(ctx, "phase.dispatch-end", dispatchStart, dispatchErr)
	if dispatchErr != nil || !sent {
		t.Fatal("normal real Component send failed", sent, dispatchErr)
	}
	stepStart := time.Now()
	w.DiagnosticStage(ctx, "phase.decision-step-start", stepStart, nil)
	var step work.StepResult
	var err error
	profileCase := ""
	switch t.Name() {
	case "TestContentContextReachableClosure64And65IncludesIntermediateVersions/64":
		profileCase = "closure64"
	case "TestContentContextStaticModeMaximumReachable62Materials":
		profileCase = "static62"
	}
	if profileCase == "" {
		step, err = w.Decision.Step(ctx)
	} else {
		pprof.Do(ctx, pprof.Labels("case", profileCase, "phase", "decision_step"), func(labelled context.Context) {
			step, err = w.Decision.Step(labelled)
		})
	}
	w.DiagnosticStage(ctx, "phase.decision-step-end", stepStart, err)
	if err != nil || step.Processed != 1 {
		t.Fatal("normal real rule step failed", step, err)
	}
	raw, err := v.Encode(v.DecisionGetRequest{ContractVersion: v.Version, Profile: "decision_engine", CommandID: "normal-context-result", Target: w.Input.Request.Target, Method: "decision_engine.get", AcceptBefore: w.Input.Request.AcceptBefore, Payload: v.DecisionGetPayload{DecisionRef: w.Input.Request.Target}})
	if err != nil {
		t.Fatal(err)
	}
	result, err := w.Decision.Get(ctx, raw, &w.Principal)
	if err != nil {
		t.Fatal(err)
	}
	found, ok := result.AsFound()
	if !ok {
		t.Fatal("normal Component result unavailable", result)
	}
	done, ok := found.Decision.AsCompleted()
	if !ok {
		t.Fatal("normal rule did not complete", found.Decision)
	}
	candidate, ok := done.Proposal.Advance.AsCandidateResult()
	if !ok || len(candidate.ArtifactRefs) != 1 {
		t.Fatal("normal candidate missing")
	}
	ref := candidate.ArtifactRefs[0]
	exact := c.ContentRef{Owner: c.OwnerRef{TenantID: c.ID(ref.Owner.TenantID), OwnerID: c.ID(ref.Owner.OwnerID)}, ContentID: c.ID(ref.ContentID), Version: string(ref.Version), Hash: ref.Hash, MediaType: ref.MediaType, ByteLength: c.Revision(ref.ByteLength)}
	if !bytes.Equal(publicContextBytes(t, ctx, w, exact), append([]byte("fixture result: "), publicContextBytes(t, ctx, w, b.Mandatory.Ref)...)) {
		t.Fatal("normal independent Content artifact mismatch")
	}
	return done
}
func TestContentContextEntryObservationCoversNormalAndOverflowReopen(t *testing.T) {
	for _, overflow := range []bool{false, true} {
		t.Run(strconv.FormatBool(overflow), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			w := fixture.NewContextWorld(t, ctx)
			if overflow {
				w.Input.Revision = "2"
				w.Input.Goal = strings.Repeat("x", 10000)
				if err := w.Dispatcher.InstallInput(ctx, "context-input", w.Input, "1"); err != nil {
					t.Fatal(err)
				}
			}
			before, active, err := w.Boundary.Observe()
			if err != nil || active != 0 || len(before) != 0 {
				t.Fatal("entry observation did not start before compilation")
			}
			bundle, err := w.Compile(ctx)
			if overflow {
				assertContextOverflowNoDecision(t, ctx, w, err)
			} else {
				if err != nil {
					t.Fatal(err)
				}
				state, e := w.Dispatcher.Observe(ctx, "context-input")
				if e != nil || !state.BindingPresent || !state.DispatchPresent {
					t.Fatal("trusted observation failed to report precise live binding and dispatch", e)
				}
				completeContextDecision(t, ctx, w, bundle)
			}
			w.Reopen(ctx)
			if overflow {
				assertContextOverflowNoDecision(t, ctx, w, err)
			} else {
				state, e := w.Dispatcher.Observe(ctx, "context-input")
				if e != nil || !state.BindingPresent || !state.DispatchPresent || state.Receipt == nil {
					t.Fatal("normal original durable receipt/binding absent after reopen", e)
				}
			}
			entries, active, e := w.Boundary.Observe()
			if e != nil || active != 0 {
				t.Fatal("finite send observation remained unclosed", e)
			}
			if overflow {
				state, e := w.Dispatcher.Observe(ctx, "context-input")
				if e != nil || state.BindingPresent || state.DispatchPresent || state.Receipt != nil || state.InputRevision != w.Input.Revision || len(entries) != 0 {
					t.Fatal("overflow reached real Decide or retained durable dispatch facts", e, len(entries))
				}
			} else {
				raw, e := v.Encode(w.Input.Request)
				if e != nil {
					t.Fatal(e)
				}
				if len(entries) != 1 || !entries[0].SubjectPresent || !bytes.Equal(entries[0].Raw, raw) || !reflect.DeepEqual(entries[0].Subject, w.Principal) {
					t.Fatal("normal observer did not fully forward exact original bytes/subject")
				}
			}
		})
	}
}

func TestContentContextCanonicalSetOrderPreservesFullTrustedProjection(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	w := fixture.NewContextWorld(t, ctx)
	w.Input.Revision = "2"
	w.Input.Conditions[0], w.Input.Conditions[1] = w.Input.Conditions[1], w.Input.Conditions[0]
	second := w.Input.Unresolved[0]
	second.ID = "context-unknown-z"
	second.OperationRef.ID = "context-original-operation-z"
	second.Cost = "original-z-cost-unknown"
	w.Input.Unresolved = append([]compiler.Responsibility{second}, w.Input.Unresolved...)
	if err := w.Dispatcher.InstallInput(ctx, "context-input", w.Input, "1"); err != nil {
		t.Fatal(err)
	}
	bundle, err := w.Compile(ctx)
	if err != nil {
		t.Fatal(err)
	}
	body := publicContextBytes(t, ctx, w, bundle.Mandatory.Ref)
	mandatory, err := compiler.DecodeMandatory(body)
	if err != nil {
		t.Fatal(err)
	}
	// Independent expected set order, while the original fixture input remains
	// reversed. All fields of each element are retained, not just its identity.
	want := w.Input
	want.Conditions = []compiler.Condition{w.Input.Conditions[1], w.Input.Conditions[0]}
	want.Unresolved = []compiler.Responsibility{w.Input.Unresolved[1], w.Input.Unresolved[0]}
	if !reflect.DeepEqual(mandatory.Input, want) {
		t.Fatal("canonical sets lost a complete trusted field or failed to sort")
	}
	w.Reopen(ctx)
	state, err := w.Dispatcher.Observe(ctx, "context-input")
	if err != nil || !state.BindingPresent || !state.DispatchPresent || state.InputRevision != "2" {
		t.Fatal("original reversed trusted binding was changed on reopen", err)
	}
	done := completeContextDecision(t, ctx, w, bundle)
	advance, ok := done.Proposal.Advance.AsCandidateResult()
	if !ok || len(advance.Evidence) != 2 || advance.Evidence[0].RequirementRef != w.Input.Conditions[0].Ref || advance.Evidence[1].RequirementRef != w.Input.Conditions[1].Ref {
		t.Fatal("typed Snapshot/Proposal requirements differ from the original full fixture projection")
	}
}

func compileOverflowObservedThroughReopen(t *testing.T, ctx context.Context, w *fixture.ContextWorld) {
	t.Helper()
	entries, active, err := w.Boundary.Observe()
	if err != nil || active != 0 || len(entries) != 0 {
		t.Fatal("overflow entry window did not start before compilation")
	}
	_, err = w.Compile(ctx)
	assertContextOverflowNoDecision(t, ctx, w, err)
	w.Reopen(ctx)
	assertContextOverflowNoDecision(t, ctx, w, err)
	state, e := w.Dispatcher.Observe(ctx, "context-input")
	entries, active, boundaryErr := w.Boundary.Observe()
	if e != nil || boundaryErr != nil || state.BindingPresent || state.DispatchPresent || state.Receipt != nil || state.InputRevision != w.Input.Revision || len(entries) != 0 || active != 0 {
		t.Fatal("original overflow entry/fixture responsibility survived or was unknown", e, boundaryErr, len(entries), active)
	}
}

func TestContentContextOriginalInputLimitPreventsDispatch(t *testing.T) {
	for _, overflow := range []bool{false, true} {
		t.Run(strconv.FormatBool(overflow), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			w := fixture.NewContextWorldWithInput(t, ctx, func(in *compiler.Input) {
				if overflow {
					// Fix the original one-byte limit before trusted installation.
					in.Request.Payload.Limits.MaxInputBytes = "1"
				}
			})
			if overflow {
				compileOverflowObservedThroughReopen(t, ctx, w)
			} else {
				bundle, err := w.Compile(ctx)
				if err != nil {
					t.Fatal(err)
				}
				completeContextDecision(t, ctx, w, bundle)
			}
		})
	}
}

func TestContentContextOriginalOutputLimitPreventsDispatch(t *testing.T) {
	for _, overflow := range []bool{false, true} {
		t.Run(strconv.FormatBool(overflow), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			w := fixture.NewContextWorldWithInput(t, ctx, func(in *compiler.Input) {
				if overflow {
					// Fix the original one-byte limit before trusted installation.
					in.Request.Payload.Limits.MaxOutputBytes = "1"
				}
			})
			if overflow {
				compileOverflowObservedThroughReopen(t, ctx, w)
			} else {
				bundle, err := w.Compile(ctx)
				if err != nil {
					t.Fatal(err)
				}
				completeContextDecision(t, ctx, w, bundle)
			}
		})
	}
}

func TestContentContextAllNecessaryConditions64And65(t *testing.T) {
	for _, count := range []int{64, 65} {
		t.Run(strconv.Itoa(count), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			// Fix the independent finite quantity-isolation tuple before Install.
			w := fixture.NewContextWorldWithInput(t, ctx, func(in *compiler.Input) {
				in.Policy.Capacity = "524288"
				in.Policy.OutputReserve = "262144"
				in.Policy.ReadBudget = "262160" // original MaxInput plus omitted B16
				in.Request.Payload.Limits.MaxInputBytes = "262144"
				in.Request.Payload.Limits.MaxOutputBytes = "262144"
			})
			w.Input.Revision = "2"
			w.Input.Conditions = make([]compiler.Condition, count)
			for i := range w.Input.Conditions {
				w.Input.Conditions[i] = compiler.Condition{Ref: v.RequirementRef{TenantID: w.Input.Owner.TenantID, OwnerID: w.Input.Owner.OwnerID, Kind: "requirement", ID: v.ID("context-necessary-" + strconv.Itoa(100+i)), Revision: "1"}, Text: "完整保留每个必要条件", Necessary: true, State: "unknown", Gaps: []string{"未核验"}}
			}
			if err := w.Dispatcher.InstallInput(ctx, "context-input", w.Input, "1"); err != nil {
				t.Fatal(err)
			}
			if count == 65 {
				compileOverflowObservedThroughReopen(t, ctx, w)
				return
			}
			bundle, err := w.Compile(ctx)
			if err != nil {
				t.Fatal(err)
			}
			mandatory, err := compiler.DecodeMandatory(publicContextBytes(t, ctx, w, bundle.Mandatory.Ref))
			if err != nil || !reflect.DeepEqual(mandatory.Input.Conditions, w.Input.Conditions) {
				t.Fatal("necessary condition information was omitted", err)
			}
			done := completeContextDecision(t, ctx, w, bundle)
			candidate, ok := done.Proposal.Advance.AsCandidateResult()
			if !ok || len(candidate.Evidence) != 64 {
				t.Fatal("real worker failed to retain all64 condition projections")
			}
		})
	}
}
