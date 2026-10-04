package development

import (
	"context"
	"errors"

	"github.com/ruipengliu/lerna/adapters/providers"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/memory"
	"github.com/ruipengliu/lerna/runtime"
)

// 政策仍按实际配置固定准确用途；现代宿主保留独立 Knowledge 目录的公开能力。
func configuredContentPolicy(c Config, knowledgeCatalog bool) (memory.Policy, error) {
	purposes := []string{"read", "preview", "content.read", "content.write", "task.goal", "task.context", "task.result", "task.submit", "task.snapshot", "task.dispatch", "task.complete", "task.evidence", "task.input", "task.accept_result", "task.revise", "task.steer", "task.action", "task.attach_evidence", "task.adjust_budget", "task.need_context", "task.delegate", "child.create", "child.new_goal", "child.continue", "billing.adjustment", "brain.input", "brain.output", "result", "memory.save", "memory.read", "memory.query", "memory.extract", "memory.sync", "memory.view", "managed_file_write", "managed_file_read", "execution.intent", "execution.arguments", "execution.output", "execution.control", "execution_intent", "execution_arguments", "execution_result", "execution_usage_proof", "environment_namespace", "environment_input", "environment_compute_spec", "environment_restore", "interaction.input", "interaction.history", "interaction.surface", "schedule.template", "confirmation.preview", "evaluation.manifest"}
	for _, purpose := range append([]string{"interaction.snapshot", "interaction.preview"}, RequiredContentPurposes()...) {
		if !containsString(purposes, purpose) {
			purposes = append(purposes, purpose)
		}
	}
	if len(c.Information) > 0 {
		purposes = append(purposes, providers.InformationPurpose, providers.InformationSearch, providers.InformationBody)
	}
	if knowledgeCatalog {
		purposes = append(purposes, RequiredKnowledgeContentPurposes()...)
	}
	if c.WASI != nil && c.WASI.CPUSecondsBudgetLimit != "" {
		for _, purpose := range RequiredWASIContentPurposes() {
			if !containsString(purposes, purpose) {
				purposes = append(purposes, purpose)
			}
		}
	}
	pv := memory.PolicyValues{Subjects: []string{c.SubjectID, c.OwnerID}, Purposes: purposes, Locations: []string{"cloud", "device"}, RetainUntil: c.PolicyExpiresAt, Continuous: true, IndependentDerived: false}
	if c.RemoteAgent != nil {
		for _, subject := range c.RemoteAgent.SourceSubjectRefs {
			if !containsString(pv.Subjects, subject.ObjectID) {
				pv.Subjects = append(pv.Subjects, subject.ObjectID)
			}
		}
	}
	for _, consumer := range c.ForeignConsumers {
		for _, holder := range consumer.Holders {
			if !containsString(pv.Subjects, holder.SubjectRef.ObjectID) {
				pv.Subjects = append(pv.Subjects, holder.SubjectRef.ObjectID)
			}
		}
		for _, purpose := range consumer.Purposes {
			if !containsString(pv.Purposes, purpose) {
				pv.Purposes = append(pv.Purposes, purpose)
			}
		}
		for _, location := range consumer.Locations {
			if !containsString(pv.Locations, location) {
				pv.Locations = append(pv.Locations, location)
			}
		}
	}
	ref := component("content-policy")
	var err error
	ref.Digest, err = api.Digest(pv)
	if err != nil {
		return memory.Policy{}, err
	}
	return memory.NewPolicy(ref, pv)
}

// 恢复只选择本 owner 已显式登记的准确政策，不安装、扫描或扩大许可。
func (a *App) selectRegisteredContentPolicy(ctx context.Context) error {
	modern := a.ContentPolicy
	var selected memory.Policy
	status, err := a.Store.Within(ctx, a.Scope, []string{"content", "memory", "platform"}, func(tx runtime.Tx) error {
		registered, found, err := a.Memory.RegisteredPolicyTx(ctx, tx, a.Scope, a.ServiceAuth, modern.PolicyRef)
		if err != nil {
			return err
		}
		if found {
			if !api.Equal(registered.Values, modern.Values) {
				return api.E("invalid_state", "policy_changed")
			}
			selected = registered
			return nil
		}
		// 任务已选用 Knowledge 时不能借旧政策补出新增用途。
		if a.Config.Knowledge != nil {
			return api.E("forbidden", "saving_policy_unregistered")
		}
		legacy, err := configuredContentPolicy(a.Config, false)
		if err != nil {
			return err
		}
		registered, found, err = a.Memory.RegisteredPolicyTx(ctx, tx, a.Scope, a.ServiceAuth, legacy.PolicyRef)
		if err != nil {
			return err
		}
		if !found {
			return api.E("forbidden", "saving_policy_unregistered")
		}
		if !api.Equal(registered.Values, legacy.Values) {
			return api.E("invalid_state", "policy_changed")
		}
		selected = registered
		return nil
	})
	if status == runtime.CommitUnknown {
		return errors.Join(runtime.ErrCommitUnknown, err)
	}
	if err != nil {
		return err
	}
	if status != runtime.Committed {
		return api.E("dependency_unavailable", "policy_selection_not_committed")
	}
	a.ContentPolicy = selected
	return nil
}
