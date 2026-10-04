package development

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	target "github.com/ruipengliu/lerna/adapters/execution"
	"github.com/ruipengliu/lerna/adapters/executor"
	"github.com/ruipengliu/lerna/adapters/platform"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/brain"
	domain "github.com/ruipengliu/lerna/internal/execution"
	"github.com/ruipengliu/lerna/internal/memory"
	"github.com/ruipengliu/lerna/internal/task"
)

// 此矩阵经实际 Task/两次 TLS 设备操作进入公开 EvidencePort.Check。
// 不注入 Task/Operation/Attempt/ConditionResult，不替换 Evidence 实现。
func TestSavedRuleRejectsActualCounterexamplesAndCurrentAuthorityLoss(t *testing.T) {
	for _, scenario := range []string{"scope_artifact_source", "credential_revoked", "read_before_write", "different_device", "wrong_native_bytes"} {
		t.Run(scenario, func(t *testing.T) {
			for _, driver := range []string{"sqlite", "postgres"} {
				t.Run(driver, func(t *testing.T) {
					if driver == "postgres" && os.Getenv("HARNESS_TEST_POSTGRES_DSN") == "" {
						t.Skip("actual PostgreSQL DSN required")
					}
					probe := &remoteSavedRuleProbe{scenario: scenario}
					runRemoteSavedRuleFixture(t, driver, true, true, probe)
					if !probe.finished.Load() {
						t.Fatal("public Evidence.Check was never reached before completion")
					}
				})
			}
		})
	}
}

type savedRuleModelMaterials = []struct {
	Ref  api.ContentRef `json:"ref"`
	Body string         `json:"body_utf8"`
}

type remoteSavedRuleProbe struct {
	scenario string
	second   *executor.Host
	finished atomic.Bool
	mu       sync.Mutex
	err      error
}

func (p *remoteSavedRuleProbe) finish(err error) {
	p.mu.Lock()
	p.err = err
	p.mu.Unlock()
	p.finished.Store(true)
}
func (p *remoteSavedRuleProbe) result() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.err
}

// 反例均在首次业务接纳前配置；不重写任何既有 owner/Grant/Bundle/期限。
func (p *remoteSavedRuleProbe) configure(t *testing.T, ctx context.Context, cfg *Config, dc executor.Config, readBinding *api.ObjectRef, deviceRoot string) error {
	if p.scenario == "read_before_write" {
		// 原生介质前态已有准确字节；实际 Read 必须先于后续 Write。
		return os.WriteFile(filepath.Join(deviceRoot, "files", "remote-report.md"), []byte(originalRemoteReport), 0600)
	}
	if p.scenario != "different_device" {
		return nil
	}
	root := t.TempDir()
	if keep := os.Getenv("HARNESS_TEST_REMOTE_FIXTURE_ROOT"); keep != "" {
		var err error
		root, err = os.MkdirTemp(keep, "saved-other-device-")
		if err != nil {
			return err
		}
	}
	ca, cert, key := remoteTestTLS(t, root)
	owner, instance := api.NewID("owner"), api.NewID("instance")
	binding := api.ObjectRef{TenantID: cfg.TenantID, OwnerID: owner, ObjectID: api.NewID("binding"), Revision: 1}
	deviceBinding := executor.Binding{CapabilityRef: target.FileReadCapability().Ref, BindingRef: binding, InstallLockRef: component("builtin-install-lock"), Resources: []string{"managed-files"}, Actions: []string{"file.read"}}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	address := listener.Addr().String()
	if err = listener.Close(); err != nil {
		return err
	}
	dc.OwnerID, dc.InstanceID, dc.DatabaseID = owner, instance, ""
	dc.DatabasePath, dc.DataRoot = filepath.Join(root, "device.sqlite"), root
	dc.SigningKeyFile, dc.PeerTokenFile = filepath.Join(root, "device-key.pem"), filepath.Join(root, "peer-token")
	dc.Bindings, dc.GRPCAddr = []executor.Binding{deviceBinding}, address
	dc.TLSCertificateFile, dc.TLSKeyFile = cert, key
	if err = privateFile(dc.PeerTokenFile, []byte("synthetic-second-device-contract-peer-token")); err != nil {
		return err
	}
	other, err := executor.Open(ctx, dc, true)
	if err != nil {
		return err
	}
	p.second = other
	dc.DatabaseID = other.Scope.DatabaseID
	if err = privateFile(filepath.Join(root, "config.json"), append(api.Raw(dc), '\n')); err != nil {
		return errors.Join(err, other.Close())
	}
	if err = os.WriteFile(filepath.Join(root, "files", "remote-report.md"), []byte(originalRemoteReport), 0600); err != nil {
		return errors.Join(err, other.Close())
	}
	deviceCtx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- other.Run(deviceCtx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Error("second actual device exit", err)
			}
		case <-time.After(10 * time.Second):
			t.Error("second actual device did not join")
		}
		if err := other.Close(); err != nil {
			t.Error(err)
		}
	})
	roots := x509.NewCertPool()
	caBytes, err := os.ReadFile(ca)
	if err != nil || !roots.AppendCertsFromPEM(caBytes) {
		return fmt.Errorf("second device TLS CA unavailable: %w", err)
	}
	for {
		conn, err := tls.DialWithDialer(&net.Dialer{Timeout: time.Second}, "tcp", address, &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: roots})
		if err == nil {
			if err = conn.Close(); err != nil {
				return err
			}
			break
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("second actual device TLS readiness: %w", ctx.Err())
		case <-time.After(10 * time.Millisecond):
		}
	}
	var public struct {
		X   string `json:"x"`
		Y   string `json:"y"`
		KTY string `json:"kty"`
		CRV string `json:"crv"`
	}
	if err = api.Decode(platform.PublicJWK(other.Keys.Keys["device-es256"].Public), &public); err != nil {
		return err
	}
	cfg.RemoteExecutors = append(cfg.RemoteExecutors, RemoteExecutorConfig{OwnerID: owner, DatabaseID: dc.DatabaseID, InstanceID: instance, Endpoint: "grpcs://" + address, TLSCAFile: ca, PeerTokenFile: dc.PeerTokenFile, PublicKeyID: "device-es256", PublicX: public.X, PublicY: public.Y, Bindings: dc.Bindings})
	// 新配置的 Read 许可只授予 B；原 Write 仍由 A 的独立一次许可消费。
	cfg.ActionBindings[0].BindingRef, cfg.ActionBindings[0].Grant.Recipients = binding, []string{owner}
	*readBinding = binding
	return nil
}

func (p *remoteSavedRuleProbe) modelReply(ctx context.Context, t *testing.T, a *App, device *executor.Host, snapshot api.Snapshot, materials savedRuleModelMaterials, goal brain.GoalSpec, readBinding, writeBinding api.ObjectRef, post int32) ([]byte, error) {
	if snapshot.Purpose == "interpret_requirements" {
		return remoteReportModelReply(t, a, snapshot, materials, goal, readBinding, writeBinding, post), nil
	}
	if post == 4 {
		err := p.check(ctx, a, device, snapshot, post)
		p.finish(err)
		// 本夹具测公开检查边界，不提交 complete 或声明 Task 成功。
		return remoteReportKnownFailure("The actual SavedRule counterexample has been observed; no completion is proposed."), nil
	}
	if post != 2 && post != 3 {
		return nil, fmt.Errorf("unexpected additional physical model call: %d", post)
	}
	read := post == 3
	if p.scenario == "read_before_write" {
		read = post == 2
	}
	capability, binding, localKey := target.FileWriteCapability().Ref, writeBinding, "save_report"
	if read {
		capability, binding, localKey = target.FileReadCapability().Ref, readBinding, "verify_file"
	}
	declared := false
	for n, cap := range snapshot.CapabilityRefs {
		declared = declared || cap == capability && n < len(snapshot.BindingRefs) && snapshot.BindingRefs[n] == binding
	}
	if !declared {
		return nil, fmt.Errorf("counterexample original capability/binding is absent from actual Snapshot")
	}
	g := brain.Generated{Draft: brain.Draft{Kind: "act", ReasonLocalID: "reason", Actions: []brain.DraftAction{{LocalKey: localKey, CapabilityRef: capability, BindingRef: binding, ArgumentsLocalID: "args"}}}, Contents: []brain.GeneratedContent{{LocalID: "reason", MediaType: "text/plain", Body: "Propose a bounded real device action; Task owns its admission and verdict.", DisclosedSources: []api.ContentRef{}}}}
	if read {
		sources := []api.ContentRef{}
		if p.scenario != "read_before_write" && p.scenario != "different_device" {
			for _, material := range materials {
				var written target.FileWriteResult
				if material.Ref.OwnerID == writeBinding.OwnerID && api.Decode([]byte(material.Body), &written) == nil && written.Path == goal.SavePath && written.DirectorySynced {
					sources = append(sources, material.Ref)
				}
			}
			if len(sources) != 1 {
				return nil, fmt.Errorf("actual original write receipt was not supplied to the independent Read")
			}
		}
		g.Contents = append(g.Contents, brain.GeneratedContent{LocalID: "args", MediaType: "application/json", Body: string(api.Raw(target.FileReadArguments{Path: goal.SavePath})), DisclosedSources: sources})
	} else {
		body, version := originalRemoteReport, "absent"
		if p.scenario == "wrong_native_bytes" {
			body = "# Device report\n\nThese are different device bytes.\n"
		}
		if p.scenario == "read_before_write" {
			version = api.Hash([]byte(originalRemoteReport))
		}
		g.Draft.Actions[0].DisclosedLocalIDs = []string{"artifact"}
		g.Contents = append(g.Contents,
			brain.GeneratedContent{LocalID: "artifact", MediaType: "text/markdown", Body: body, DisclosedSources: []api.ContentRef{}},
			brain.GeneratedContent{LocalID: "args", MediaType: "application/json", Body: string(api.Raw(struct {
				Path            string `json:"path"`
				ExpectedVersion string `json:"expected_version"`
			}{goal.SavePath, version})), ContentLocalID: "artifact", DisclosedSources: []api.ContentRef{}},
		)
	}
	return remoteReportProviderReply(g), nil
}

func (p *remoteSavedRuleProbe) check(ctx context.Context, a *App, device *executor.Host, snapshot api.Snapshot, posts int32) error {
	current, err := a.Task.Read(ctx, a.Store, a.Scope, a.UserAuth, snapshot.TaskRef.ObjectID)
	if err != nil {
		return err
	}
	if current.Status != "active" || current.Control != "running" || current.ResultRef != nil || current.GoalRevision != snapshot.GoalRevision || current.ControlRevision != snapshot.ControlRevision || current.GoalRef != snapshot.GoalRef || posts != 4 {
		return fmt.Errorf("negative fixture missed the original active check boundary")
	}
	facts, err := a.Task.ContextFacts(ctx, a.Store, a.Scope, a.UserAuth, current.TaskID)
	if err != nil || len(facts.Operations) != 2 {
		return fmt.Errorf("real original paired operations are absent: %w", err)
	}
	var write, read *task.ContextOperation
	for n := range facts.Operations {
		op := &facts.Operations[n]
		if !op.Fact.Closed || op.Fact.MayApplyLater || op.Fact.Effect == "unknown" || op.Intent.TaskRef.ObjectID != current.TaskID || op.Intent.GoalRevision != current.GoalRevision || op.Intent.ControlRevision != current.ControlRevision {
			return fmt.Errorf("counterexample does not retain original closed Task generation")
		}
		if op.Intent.CapabilityRef == target.FileWriteCapability().Ref {
			write = op
		}
		if op.Intent.CapabilityRef == target.FileReadCapability().Ref {
			read = op
		}
	}
	if write == nil || read == nil || write.Intent.OperationID == read.Intent.OperationID {
		return fmt.Errorf("two independent actual write/read operations are required")
	}
	views := make([]struct {
		Attempt domain.AttemptView
		Result  api.ContentRef
	}, 0, 2)
	for _, op := range []*task.ContextOperation{write, read} {
		route, err := a.remoteExecutors.client(ctx, op.Intent.ExecutorID)
		if err != nil {
			return err
		}
		view, err := route.Client.Get(ctx, op.Intent.OperationID)
		if err != nil {
			return err
		}
		if len(view.Attempts.Items) != 1 || !view.Attempts.Exhausted || view.Attempts.Partial || len(view.Attempts.Gaps) != 0 || view.Operation.ResultRef == nil || view.Attempts.Items[0].ResultRef == nil || *view.Operation.ResultRef != *view.Attempts.Items[0].ResultRef || view.Operation.TaskRef != op.Intent.TaskRef || !view.ActuallyStopped || !view.Operation.UsageFinal || view.Attempts.Items[0].StartedAt == "" {
			return fmt.Errorf("actual original device effect/Result/Attempt did not close")
		}
		views = append(views, struct {
			Attempt domain.AttemptView
			Result  api.ContentRef
		}{view.Attempts.Items[0], *view.Operation.ResultRef})
	}
	readBytes, err := a.ReadContent(ctx, a.Scope, a.ServiceAuth, views[1].Result, "task.context")
	if err != nil {
		return err
	}
	var actualRead target.FileReadResult
	if err = api.Decode(readBytes, &actualRead); err != nil {
		return err
	}
	actualBytes, err := base64.StdEncoding.Strict().DecodeString(actualRead.DataBase64)
	if err != nil {
		return err
	}
	writeTarget, err := device.Files.Read(ctx, "remote-report.md")
	if err != nil {
		return err
	}
	if p.scenario == "wrong_native_bytes" {
		const wrong = "# Device report\n\nThese are different device bytes.\n"
		if string(actualBytes) != wrong || string(writeTarget.Data) != wrong || actualRead.Version != api.Hash([]byte(wrong)) || actualRead.Version == api.Hash([]byte(originalRemoteReport)) {
			return fmt.Errorf("actual mismatching target bytes were not observed independently")
		}
	} else if string(actualBytes) != originalRemoteReport || string(writeTarget.Data) != originalRemoteReport {
		return fmt.Errorf("real target bytes are not the exact independent original report")
	}
	if p.scenario == "different_device" {
		if p.second == nil || write.Intent.ExecutorID != device.Scope.OwnerID || read.Intent.ExecutorID != p.second.Scope.OwnerID || read.Intent.ExecutorID == write.Intent.ExecutorID {
			return fmt.Errorf("wrong-device fixture did not use two actual paired owners")
		}
		otherTarget, err := p.second.Files.Read(ctx, "remote-report.md")
		if err != nil || string(otherTarget.Data) != originalRemoteReport {
			return fmt.Errorf("second device real bytes unavailable: %w", err)
		}
	}
	if p.scenario == "read_before_write" {
		readStarted, err := api.ParseTime(views[1].Attempt.StartedAt)
		if err != nil {
			return err
		}
		writeStarted, err := api.ParseTime(views[0].Attempt.StartedAt)
		if err != nil || !readStarted.Before(writeStarted) {
			return fmt.Errorf("actual Read did not precede the actual Write: %w", err)
		}
	}
	var requirement *api.Requirement
	for n := range current.Requirements {
		if current.Requirements[n].RuleRef == a.SavedRule {
			requirement = &current.Requirements[n]
		}
	}
	if requirement == nil || requirement.RuleParametersRef == nil {
		return fmt.Errorf("original frozen Saved requirement is missing")
	}
	parameters, err := a.ReadContent(ctx, a.Scope, a.ServiceAuth, *requirement.RuleParametersRef, "task.context")
	if err != nil {
		return err
	}
	var expected brain.RuleParameters
	if err = api.Decode(parameters, &expected); err != nil || expected.SavePath != "remote-report.md" || expected.ExpectedHash != api.Hash([]byte(originalRemoteReport)) || expected.ExpectedLength != uint64(len(originalRemoteReport)) {
		return fmt.Errorf("original requirement was replaced by the wrong device result: %w", err)
	}
	artifact, err := a.Publish(ctx, a.Scope, a.ServiceAuth, api.NewID("content"), "text/markdown", []byte(originalRemoteReport), []api.ContentRef{current.GoalRef, views[1].Result}, []api.ContentRef{})
	if err != nil {
		return err
	}
	request := func(ref api.ContentRef) task.CheckRequest {
		return task.CheckRequest{CheckID: api.NewID("check"), Revision: 1, State: "pending", CreatorID: a.ServiceAuth.SubjectID, Input: task.AttachInput{TaskID: current.TaskID, GoalRevision: current.GoalRevision, RequirementRef: api.RequirementRef{RequirementID: requirement.RequirementID, Revision: requirement.Revision}, ArtifactRef: ref, EvidenceRefs: []api.ContentRef{views[0].Result, views[1].Result}}}
	}
	var publicEvidence task.EvidencePort = evidenceBridge{a}
	first, err := publicEvidence.Check(ctx, a.Scope, current, request(artifact))
	if err != nil {
		return fmt.Errorf("exact original public check was unavailable: %w", err)
	}
	if p.scenario == "different_device" || p.scenario == "read_before_write" || p.scenario == "wrong_native_bytes" {
		if first.Verdict != "fail" || first.Basis != "verified" || first.Applicability != "usable" || first.TaskID != current.TaskID || first.ArtifactRef != artifact {
			return fmt.Errorf("real counterexample was accepted: %s/%s/%s", first.Verdict, first.Basis, first.Applicability)
		}
		return nil
	}
	if first.Verdict != "pass" || first.Basis != "verified" || first.Applicability != "usable" {
		return fmt.Errorf("legal original public baseline failed: %s/%s/%s", first.Verdict, first.Basis, first.Applicability)
	}
	if p.scenario == "credential_revoked" {
		if err = a.Identity.Revoke(ctx, a.ServiceAuth); err != nil {
			return err
		}
		_, err = publicEvidence.Check(ctx, a.Scope, current, request(artifact))
		return savedRuleRefusal(err, "forbidden", "credential_revoked")
	}
	wrongScope := a.Scope
	wrongScope.OwnerID = api.NewID("owner")
	_, err = publicEvidence.Check(ctx, wrongScope, current, request(artifact))
	if err = savedRuleRefusal(err, "forbidden", "original_condition_check_scope_required"); err != nil {
		return err
	}
	mismatch, err := a.Publish(ctx, a.Scope, a.ServiceAuth, api.NewID("content"), "text/markdown", []byte("A different claimed artifact.\n"), []api.ContentRef{current.GoalRef, views[1].Result}, []api.ContentRef{})
	if err != nil {
		return err
	}
	result, err := publicEvidence.Check(ctx, a.Scope, current, request(mismatch))
	if err != nil || result.Verdict != "fail" || result.ArtifactRef != mismatch {
		return fmt.Errorf("public check accepted a mismatching artifact: verdict=%s: %w", result.Verdict, err)
	}
	// 原参数 Content 初始 control revision=1；准确公开 close 命令沿该 CAS。
	one := uint64(1)
	command := api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: a.Scope.OwnerID, CommandID: api.NewID("command"), TargetID: requirement.RuleParametersRef.ContentID, Method: "content.close", ExpectedRevision: &one, ExpiresAt: api.Time(time.Now().Add(time.Minute)), Payload: api.Raw(memory.CloseInput{ContentRef: *requirement.RuleParametersRef, Reason: "withdraw the original SavedRule parameter source before a new current check"})}
	receipt, err := a.Dispatcher.Command(ctx, a.UserAuth, api.Raw(command))
	if err != nil || receipt.Error != nil || receipt.Stage != "applied" {
		return fmt.Errorf("actual parameter source close was not applied: %v: %w", receipt.Error, err)
	}
	if _, err = a.ReadContent(ctx, a.Scope, a.ServiceAuth, *requirement.RuleParametersRef, "task.context"); err == nil {
		return fmt.Errorf("ordinary original parameter source remained readable after actual close")
	}
	_, err = publicEvidence.Check(ctx, a.Scope, current, request(artifact))
	return savedRuleRefusal(err, "forbidden", "source_closed")
}

func savedRuleRefusal(err error, code, reason string) error {
	var refusal *api.Error
	if !errors.As(err, &refusal) || refusal.Code != code || refusal.Reason != reason {
		return fmt.Errorf("expected public refusal %s/%s, observed %v", code, reason, err)
	}
	return nil
}
