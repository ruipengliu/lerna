package development

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/brain"
	"github.com/ruipengliu/lerna/internal/execution"
	"github.com/ruipengliu/lerna/runtime"
)

type preferenceClaimEvidence struct {
	Job       api.Job `json:"job"`
	ClaimedAt string  `json:"claimed_at"`
}

// 只观察真实 Claim 返回，不改 Job、租期或 handler；有界保留最后64次领取。
type preferenceClaimStore struct {
	runtime.Store
	t      *testing.T
	claims []preferenceClaimEvidence
}

func (s *preferenceClaimStore) Claim(ctx context.Context, scope runtime.Scope, holder string, kinds []string, limit int, lease time.Duration) ([]runtime.Work, runtime.CommitStatus, error) {
	works, status, err := s.Store.Claim(ctx, scope, holder, kinds, limit, lease)
	for _, work := range works {
		if len(s.claims) == 64 {
			s.claims = s.claims[1:]
		}
		at := api.Time(time.Now())
		s.claims = append(s.claims, preferenceClaimEvidence{Job: work.Job, ClaimedAt: at})
		s.t.Logf("original preference claim: at=%s kind=%s job=%s source=%s epoch=%d", at, work.Job.Kind, work.Job.JobID, work.Job.SourceRef.ObjectID, work.Claim.LeaseEpoch)
	}
	return works, status, err
}

// 仅取原公开事实作阶段诊断，另用有界ctx，不能将fixture超时写成业务失败。
func preferenceStageEvidence(t *testing.T, a *App, driver, root, phase string, original api.Command, cause error, claims ...preferenceClaimEvidence) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	metadata := map[string]any{"driver": driver, "scope": a.Scope, "fixture_root": root, "phase": phase, "original_command": original, "at": api.Time(time.Now())}
	metadata["claim_history"] = claims
	if cause != nil {
		metadata["runner_error"] = cause.Error()
	}
	facts, err := a.Task.ContextFacts(ctx, a.Store, a.Scope, a.UserAuth, original.TargetID)
	if err != nil {
		metadata["facts_error"] = err.Error()
	} else {
		metadata["facts"] = facts
		operations := []execution.OperationView{}
		decisions := []brain.View{}
		for _, op := range facts.Operations {
			raw, err := a.query(ctx, "execution.get", op.Intent.OperationID, execution.OperationIDInput{OperationID: op.Intent.OperationID})
			var view execution.OperationView
			if err == nil {
				err = api.Decode(raw, &view)
			}
			if err != nil {
				metadata["operation_error"] = err.Error()
				break
			}
			operations = append(operations, view)
		}
		refs := append([]api.ObjectRef{}, facts.FactRefs...)
		for i := len(claims) - 1; i >= 0; i-- {
			if claims[i].Job.Kind == brain.JobAdvance || claims[i].Job.Kind == "task.dispatch_decision" {
				refs = append(refs, claims[i].Job.SourceRef)
			}
		}
		seenDecisions := map[string]bool{}
		for _, ref := range refs {
			if len(decisions) >= 20 {
				break
			}
			if len(ref.ObjectID) < 9 || ref.ObjectID[:9] != "decision_" {
				continue
			}
			if seenDecisions[ref.ObjectID] {
				continue
			}
			seenDecisions[ref.ObjectID] = true
			raw, err := a.query(ctx, "brain.get", ref.ObjectID, brain.GetInput{DecisionID: ref.ObjectID})
			var view brain.View
			if err == nil {
				err = api.Decode(raw, &view)
			}
			if err != nil {
				metadata["decision_error"] = err.Error()
				break
			}
			decisions = append(decisions, view)
		}
		metadata["operations"], metadata["decisions"] = operations, decisions
	}
	fileName := "context14-preference-" + driver + "-" + original.TargetID + "-" + phase + ".json"
	if destination := os.Getenv("HARNESS_CONTEXT14_EVIDENCE_DIR"); destination != "" {
		encoded, err := json.MarshalIndent(metadata, "", "  ")
		if err != nil {
			t.Error(err)
			return
		}
		file, err := os.OpenFile(filepath.Join(destination, fileName), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			t.Error(err)
			return
		}
		_, writeErr := file.Write(append(encoded, '\n'))
		closeErr := file.Close()
		if writeErr != nil {
			t.Error(writeErr)
		}
		if closeErr != nil {
			t.Error(closeErr)
		}
	}
	t.Logf("public preference stage %s: task=%s scope=%s/%s evidence=%s", phase, original.TargetID, a.Scope.TenantID, a.Scope.OwnerID, fileName)
}
