//go:build integration

package component_test

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"testing"
	"time"

	decision "github.com/ruipengliu/lerna/components/decision_engine"
	fixture "github.com/ruipengliu/lerna/conformance/internal/decisionfixture"
	v "github.com/ruipengliu/lerna/contract/v1_1"
)

// Read the real fixture bytes independently of execution to set the exact
// input allowance, including the Snapshot, lock, manifest and material.
func controlInputSize(t *testing.T, ctx context.Context, w *fixture.World, scene fixture.Scenario) int {
	t.Helper()
	source := w.Source()
	permit, err := source.Authorize(ctx, scene.Subject, scene.DecisionRef, "start", &scene.Request.Payload)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := source.ReadSnapshot(ctx, scene.Request.Payload.SnapshotRef, permit, v.MaxBodyBytes)
	if err != nil {
		t.Fatal(err)
	}
	lock, err := source.ReadFixtureLock(ctx, scene.Request.Payload.ComponentRef.InstallLockRef, permit, v.MaxBodyBytes)
	if err != nil {
		t.Fatal(err)
	}
	total := len(snapshot.Raw) + len(lock.Raw) + len(lock.ManifestRaw)
	for _, ref := range snapshot.MaterialRefs {
		body, err := source.ReadMaterial(ctx, ref, "rule.input", permit, v.MaxBodyBytes)
		if err != nil {
			t.Fatal(err)
		}
		total += len(body)
	}
	return total
}

// The adapter has read genuine immutable material successfully. Its returned
// bytes are corrupted at the public Source port, not by modifying a business
// table or pretending a native storage failure occurred.
type corruptedControlMaterial struct{ decision.Source }

func (s corruptedControlMaterial) ReadMaterial(ctx context.Context, ref v.ContentRef, purpose string, permit decision.Permission, cap int64) ([]byte, error) {
	body, err := s.Source.ReadMaterial(ctx, ref, purpose, permit, cap)
	if err != nil {
		return body, err
	}
	body = append([]byte(nil), body...)
	if len(body) == 0 {
		return nil, errors.New("expected actual fixture material")
	}
	body[0] ^= 1
	return body, nil
}

func TestDurableControlResourceLimitsRetainFailureAndAllowExactCounterpart(t *testing.T) {
	for _, name := range []string{"zero_input", "zero_output", "zero_steps", "zero_cost", "short_input", "short_output", "bad_source", "cost_unit", "exact_allowance", "maximum_decimal"} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			w := fixture.NewWorld(t, ctx)
			scene := w.Scenario()
			// A completed, independently readable Proposal supplies the whole
			// output size. The counterpart has an equal-length Decision ID, so
			// its changed identity does not change its serialized byte allowance.
			scene.Request.Payload.Limits.MaxInputBytes = v.Revision(strconv.Itoa(controlInputSize(t, ctx, w, scene)))
			scene.Request.Payload.Limits.MaxRuleSteps = "1"
			scene.Request.Payload.Limits.MaxActions = "0" // candidate_result contains no actions.
			scene.Request.Payload.Limits.MaxCost.IntegerValue = "1"
			s := w.Service()
			original := acceptAccounting(t, ctx, s, scene)
			if step, err := s.Step(ctx); err != nil || step.Processed != 1 {
				t.Fatal("exact normal input/steps/cost counterpart", err)
			}
			view, err := s.Get(ctx, scene.GetJSON, &scene.Subject)
			if err != nil {
				t.Fatal(err)
			}
			found, ok := view.AsFound()
			if !ok {
				t.Fatal("normal result unavailable")
			}
			completed, ok := found.Decision.AsCompleted()
			if !ok {
				t.Fatal("zero actions prevented a candidate without actions")
			}
			proposalBytes, err := w.ReadArtifact(ctx, completed.ProposalRef)
			if err != nil {
				t.Fatal(err)
			}
			candidate, ok := completed.Proposal.Advance.AsCandidateResult()
			if !ok || len(candidate.ArtifactRefs) != 1 {
				t.Fatal("normal candidate artifact unavailable")
			}
			artifactBytes, err := w.ReadArtifact(ctx, candidate.ArtifactRefs[0])
			if err != nil || string(artifactBytes) != "fixture result: alpha\n" {
				t.Fatal("independent normal artifact", err)
			}
			wholeOutput := len(proposalBytes) + len(artifactBytes)
			bounded := w.AdditionalScenario(ctx, scene.DecisionRef.OwnerID, "bounded-decisionx", time.Now().Add(15*time.Second))
			if len(bounded.DecisionRef.ID) != len(scene.DecisionRef.ID) {
				t.Fatal("counterpart identity changes byte boundary")
			}
			bounded.Request.Payload.Limits = scene.Request.Payload.Limits
			bounded.Request.Payload.Limits.MaxOutputBytes = v.Revision(strconv.Itoa(wholeOutput))
			failure := v.DecisionFailure("")
			var source decision.Source = w.Source()
			switch name {
			case "zero_input":
				bounded.Request.Payload.Limits.MaxInputBytes = "0"
				failure = "input_over_limit"
			case "zero_output":
				bounded.Request.Payload.Limits.MaxOutputBytes = "0"
				failure = "output_over_limit"
			case "zero_steps":
				bounded.Request.Payload.Limits.MaxRuleSteps = "0"
				failure = "rule_limit_exceeded"
			case "zero_cost":
				bounded.Request.Payload.Limits.MaxCost.IntegerValue = "0"
				failure = "budget_exhausted"
			case "short_input":
				bounded.Request.Payload.Limits.MaxInputBytes = v.Revision(strconv.Itoa(controlInputSize(t, ctx, w, bounded) - 1))
				failure = "input_over_limit"
			case "short_output":
				bounded.Request.Payload.Limits.MaxOutputBytes = v.Revision(strconv.Itoa(wholeOutput - 1))
				failure = "output_over_limit"
			case "bad_source":
				source = corruptedControlMaterial{Source: w.Source()}
				failure = "snapshot_unavailable"
			case "cost_unit":
				bounded.Request.Payload.Limits.MaxCost.Unit = "fixture.other"
				failure = "budget_exhausted"
			case "maximum_decimal":
				bounded.Request.Payload.Limits = v.DecisionLimits{MaxInputBytes: "1048576", MaxOutputBytes: "1048576", MaxRuleSteps: "1024", MaxActions: "4", MaxCost: v.Amount{Unit: "fixture", IntegerValue: "9223372036854775807"}}
			}
			cfg := decision.Config{Owner: v.OwnerRef{TenantID: scene.DecisionRef.TenantID, OwnerID: scene.DecisionRef.OwnerID}, Store: w.Store(), Authority: w.Source(), ControlAuthority: w.Source(), Source: source, Publisher: w.Source(), Component: scene.Request.Payload.ComponentRef, Worker: "resource-boundary", Lease: 5 * time.Second, PoolControl: true}
			s, err = decision.New(cfg)
			if err != nil {
				t.Fatal(err)
			}
			if name == "maximum_decimal" {
				for _, field := range []string{"input", "output", "steps", "actions", "negative", "leading_zero"} {
					invalid := bounded.Request
					switch field {
					case "input":
						invalid.Payload.Limits.MaxInputBytes = "1048577"
					case "output":
						invalid.Payload.Limits.MaxOutputBytes = "1048577"
					case "steps":
						invalid.Payload.Limits.MaxRuleSteps = "1025"
					case "actions":
						invalid.Payload.Limits.MaxActions = "5"
					case "negative":
						invalid.Payload.Limits.MaxCost.IntegerValue = "-1"
					case "leading_zero":
						invalid.Payload.Limits.MaxInputBytes = "01"
					}
					raw, err := json.Marshal(invalid)
					if err != nil {
						t.Fatal(err)
					}
					_, err = s.Decide(ctx, raw, &bounded.Subject)
					var refusal *v.ContractError
					if !errors.As(err, &refusal) || refusal.Code != "schema_invalid" {
						t.Fatal("invalid resource decimal bypassed admission", field, err)
					}
				}
				view, err := s.Get(ctx, bounded.GetJSON, &bounded.Subject)
				if err != nil {
					t.Fatal(err)
				}
				if _, ok := view.AsResultUnavailable(); !ok {
					t.Fatal("malformed resource admission created work")
				}
			}
			boundedReceipt := acceptAccounting(t, ctx, s, bounded)
			if _, err = s.Step(ctx); err != nil {
				t.Fatal(err)
			}
			view, err = s.Get(ctx, bounded.GetJSON, &bounded.Subject)
			if err != nil {
				t.Fatal(err)
			}
			found, ok = view.AsFound()
			if !ok {
				t.Fatal("bounded result unavailable")
			}
			if failure != "" {
				failed, ok := found.Decision.AsFailed()
				if !ok || failed.Failure != failure || failed.Input.Limits != bounded.Request.Payload.Limits {
					t.Fatalf("original limit failure %s was not retained", failure)
				}
			} else {
				allowed, ok := found.Decision.AsCompleted()
				if !ok || allowed.Usage.InputBytes != scene.Request.Payload.Limits.MaxInputBytes || allowed.Usage.OutputBytes != v.Revision(strconv.Itoa(wholeOutput)) || allowed.Usage.RuleStarts != "1" || allowed.Usage.RuleSteps != "1" || allowed.Usage.Cost.IntegerValue != "1" {
					t.Fatal("exact or maximum decimal allowance did not finish exactly one fixture rule")
				}
			}
			before, err := v.Encode(found.Decision)
			if err != nil {
				t.Fatal(err)
			}
			w.Reopen(ctx)
			s = w.Service()
			view, err = s.Get(ctx, bounded.GetJSON, &bounded.Subject)
			if err != nil {
				t.Fatal(err)
			}
			after, ok := view.AsFound()
			if !ok {
				t.Fatal("reopened limit result unavailable")
			}
			encoded, err := v.Encode(after.Decision)
			if err != nil || string(encoded) != string(before) {
				t.Fatal("reopen reset the original limit result", err)
			}
			if step, err := s.Step(ctx); err != nil || step.Processed != 0 {
				t.Fatal("limit terminal acquired fresh work", err)
			}
			for _, pair := range []struct {
				scene   fixture.Scenario
				receipt v.CommandReceipt
			}{{scene, original}, {bounded, boundedReceipt}} {
				query, err := s.GetCommand(ctx, pair.scene.CommandGetJSON, &pair.scene.Subject)
				if err != nil {
					t.Fatal(err)
				}
				command, ok := query.AsFound()
				if !ok {
					t.Fatal("fixed accepted receipt unavailable")
				}
				want, err := v.Encode(pair.receipt)
				if err != nil {
					t.Fatal(err)
				}
				got, err := v.Encode(command.Receipt)
				if err != nil || string(want) != string(got) {
					t.Fatal("resource failure changed original command receipt", err)
				}
			}
		})
	}
}
