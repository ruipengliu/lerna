package development

import (
	"context"
	"encoding/base64"
	"os"
	"testing"

	target "github.com/ruipengliu/lerna/adapters/execution"
	"github.com/ruipengliu/lerna/adapters/executor"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/brain"
	"github.com/ruipengliu/lerna/internal/task"
)

const originalRemoteReport = "# Device report\n\nThe original cloud Task saved this exact report on its paired device.\n"

// 原公开 Task 选择准确设备 write/read 两叶；没有预置 Operation、Use 或 lease。
// 保存条件必须核对设备原写入与独立读回，不能借云端同路径文件宣布成功。
func TestConfiguredRemoteExecutorTaskSavesReportReadsBackAndReopensOriginal(t *testing.T) {
	for _, driver := range []string{"sqlite", "postgres"} {
		t.Run(driver, func(t *testing.T) {
			if driver == "postgres" && os.Getenv("HARNESS_TEST_POSTGRES_DSN") == "" {
				t.Skip("actual PostgreSQL DSN required")
			}
			runRemoteExecutorTask(t, driver, true, true)
		})
	}
}

func remoteReportModelReply(t *testing.T, a *App, snapshot api.Snapshot, materials []struct {
	Ref  api.ContentRef `json:"ref"`
	Body string         `json:"body_utf8"`
}, goal brain.GoalSpec, readBinding, writeBinding api.ObjectRef, post int32) []byte {
	t.Helper()
	generated := brain.Generated{Contents: []brain.GeneratedContent{{LocalID: "reason", MediaType: "text/plain", Body: "Preserve the original device report and let Task independently verify its conditions.", DisclosedSources: []api.ContentRef{}}}}
	if snapshot.Purpose == "interpret_requirements" {
		generated.Draft = brain.Draft{Kind: "refine_requirements", ReasonLocalID: "reason", Requirements: []brain.DraftRequirement{
			{CandidateKey: "artifact_exact", Kind: "quality", StatementLocalID: "artifact_statement", ParametersLocalID: "parameters", RuleRef: a.ArtifactRule, Required: true},
			{CandidateKey: "file_saved_readback", Kind: "effect", StatementLocalID: "saved_statement", ParametersLocalID: "parameters", RuleRef: a.SavedRule, Required: true},
		}}
		generated.Contents = append(generated.Contents,
			brain.GeneratedContent{LocalID: "artifact_statement", MediaType: "text/plain", Body: "The artifact must equal the exact original report.", DisclosedSources: []api.ContentRef{}},
			brain.GeneratedContent{LocalID: "saved_statement", MediaType: "text/plain", Body: "The original device must durably save the report and independently read back the same bytes.", DisclosedSources: []api.ContentRef{}},
			brain.GeneratedContent{LocalID: "parameters", MediaType: "application/json", Body: string(api.Raw(brain.RuleParameters{Kind: "report", SavePath: goal.SavePath, ExpectedHash: api.Hash([]byte(originalRemoteReport)), ExpectedLength: uint64(len(originalRemoteReport))})), DisclosedSources: []api.ContentRef{}},
		)
	} else {
		cap, binding := target.FileReadCapability().Ref, readBinding
		if post == 2 {
			cap, binding = target.FileWriteCapability().Ref, writeBinding
		}
		declared := false
		for n, ref := range snapshot.CapabilityRefs {
			declared = declared || api.Equal(ref, cap) && n < len(snapshot.BindingRefs) && api.Equal(snapshot.BindingRefs[n], binding)
		}
		if post <= 3 && !declared {
			t.Error("original device capability/binding not declared in the real Snapshot")
			return remoteReportKnownFailure("The original device capability and binding are unavailable.")
		}
		switch post {
		case 2:
			generated.Draft = brain.Draft{Kind: "act", ReasonLocalID: "reason", Actions: []brain.DraftAction{{LocalKey: "save_report", CapabilityRef: cap, BindingRef: binding, ArgumentsLocalID: "args", DisclosedLocalIDs: []string{"artifact"}}}}
			generated.Contents = append(generated.Contents,
				brain.GeneratedContent{LocalID: "artifact", MediaType: "text/markdown", Body: originalRemoteReport, DisclosedSources: []api.ContentRef{}},
				brain.GeneratedContent{LocalID: "args", MediaType: "application/json", Body: string(api.Raw(struct {
					Path            string `json:"path"`
					ExpectedVersion string `json:"expected_version"`
				}{goal.SavePath, "absent"})), ContentLocalID: "artifact", DisclosedSources: []api.ContentRef{}},
			)
		case 3:
			var source api.ContentRef
			for _, material := range materials {
				var written target.FileWriteResult
				if material.Ref.OwnerID == writeBinding.OwnerID && api.Decode([]byte(material.Body), &written) == nil && written.Path == goal.SavePath && written.DirectorySynced && written.Version == api.Hash([]byte(originalRemoteReport)) {
					source = material.Ref
				}
			}
			if source.ContentID == "" {
				t.Error("real model input lacks the original durable device write result")
				return remoteReportKnownFailure("The original durable device write result is unavailable.")
			}
			generated.Draft = brain.Draft{Kind: "act", ReasonLocalID: "reason", Actions: []brain.DraftAction{{LocalKey: "verify_file", CapabilityRef: cap, BindingRef: binding, ArgumentsLocalID: "args"}}}
			generated.Contents = append(generated.Contents, brain.GeneratedContent{LocalID: "args", MediaType: "application/json", Body: string(api.Raw(target.FileReadArguments{Path: goal.SavePath})), DisclosedSources: []api.ContentRef{source}})
		case 4:
			var source api.ContentRef
			for _, material := range materials {
				var read target.FileReadResult
				if material.Ref.OwnerID == readBinding.OwnerID && api.Decode([]byte(material.Body), &read) == nil && read.Path == goal.SavePath && read.DataBase64 == base64.StdEncoding.EncodeToString([]byte(originalRemoteReport)) && read.Version == api.Hash([]byte(originalRemoteReport)) {
					source = material.Ref
				}
			}
			if source.ContentID == "" {
				t.Error("real model input lacks the original independent device readback")
				return remoteReportKnownFailure("The original independent device readback is unavailable.")
			}
			generated.Draft = brain.Draft{Kind: "complete", ReasonLocalID: "reason", ArtifactLocalIDs: []string{"artifact"}}
			generated.Contents = append(generated.Contents, brain.GeneratedContent{LocalID: "artifact", MediaType: "text/markdown", Body: originalRemoteReport, DisclosedSources: []api.ContentRef{source}})
		default:
			t.Errorf("unexpected additional model physical request: %d", post)
			return remoteReportKnownFailure("The original bounded report procedure has no further action.")
		}
	}
	return remoteReportProviderReply(generated)
}

// 诊断失败也返回准确、可计费的供应商回复；首轮旧 nil 回复的未知责任仍保留，
// 新 tracer 不以无效供应商正文遮住真实驱动/证据门禁的拒绝。
func remoteReportKnownFailure(reason string) []byte {
	return remoteReportProviderReply(brain.Generated{
		Draft: brain.Draft{Kind: "fail", ReasonLocalID: "reason"},
		Contents: []brain.GeneratedContent{{
			LocalID: "reason", MediaType: "text/plain", Body: reason, DisclosedSources: []api.ContentRef{},
		}},
	})
}

func remoteReportProviderReply(generated brain.Generated) []byte {
	return api.Raw(map[string]any{"id": "remote-report-original-reply", "choices": []any{map[string]any{"index": 0, "finish_reason": "stop", "message": map[string]any{"role": "assistant", "content": string(api.Raw(map[string]any{"draft": generated.Draft, "contents": generated.Contents}))}}}, "usage": map[string]any{"prompt_tokens": 100, "completion_tokens": 20, "total_tokens": 120, "prompt_tokens_details": map[string]any{"cached_tokens": 40}}})
}

func verifyRemoteReportTarget(t *testing.T, ctx context.Context, a *App, device *executor.Host, client *executor.Client, facts task.ContextFacts, goal brain.GoalSpec, read target.FileReadResult) {
	t.Helper()
	actual, err := device.Files.Read(ctx, goal.SavePath)
	if err != nil || string(actual.Data) != originalRemoteReport || actual.Version != read.Version || actual.SourceChanged {
		t.Fatalf("independent original device target bytes changed: %v %+v", err, actual)
	}
	cloud, err := a.Files.Read(ctx, goal.SavePath)
	if err != nil || cloud.Version != "absent" || len(cloud.Data) != 0 {
		t.Fatalf("cloud incorrectly owns the original device target: %v %+v", err, cloud)
	}
	for _, fact := range facts.Operations {
		view, err := client.Get(ctx, fact.Intent.OperationID)
		if err != nil || !view.ActuallyStopped || len(view.Attempts.Items) != 1 || view.Operation.ResultRef == nil {
			t.Fatalf("original remote action was repeated or remains open: %v %+v", err, view)
		}
		if !api.Equal(fact.Intent.CapabilityRef, target.FileWriteCapability().Ref) {
			continue
		}
		raw, err := a.ReadContent(ctx, a.Scope, a.UserAuth, *view.Operation.ResultRef, "execution_result")
		var written target.FileWriteResult
		if err != nil || api.Decode(raw, &written) != nil || written.Path != goal.SavePath || written.Version != actual.Version || !written.DirectorySynced || written.JournalID != view.Attempts.Items[0].AttemptID {
			t.Fatalf("original device write receipt lacks durable exact bytes: %v %+v", err, written)
		}
		journal, err := device.Files.Recover(ctx, view.Attempts.Items[0].AttemptID)
		if err != nil || journal.Effect != "applied" || journal.MayApplyLater || !journal.DirectorySynced || journal.JournalID != written.JournalID {
			t.Fatalf("original device target journal did not survive reopening: %v %+v", err, journal)
		}
		t.Logf("remote_write_original_refs %s", api.Raw(struct {
			TaskRef     api.ObjectRef  `json:"task_ref"`
			CommandID   string         `json:"command_id"`
			OperationID string         `json:"operation_id"`
			AttemptID   string         `json:"attempt_id"`
			ResultRef   api.ContentRef `json:"result_ref"`
			Version     string         `json:"version"`
		}{fact.Intent.TaskRef, fact.Intent.CommandID, fact.Intent.OperationID, written.JournalID, *view.Operation.ResultRef, written.Version}))
	}
}
