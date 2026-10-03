package development

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/ruipengliu/lerna/adapters/providers"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/execution"
	"github.com/ruipengliu/lerna/internal/governance"
	"github.com/ruipengliu/lerna/internal/memory"
	"github.com/ruipengliu/lerna/runtime"
)

// InformationSourceConfig 声明一个受信开发源及固定数据保留上限。
// 凭据只从原环境引用读取；许可证仍由准确 ActionBindingConfig.Grant 声明。
type InformationSourceConfig struct {
	Source               providers.InformationSourceDescriptor `json:"source"`
	CredentialEnv        string                                `json:"credential_env,omitempty"`
	TLSRootFile          string                                `json:"tls_root_file,omitempty"`
	AllowHTTPForLoopback bool                                  `json:"allow_http_for_loopback"`
	RetainUntil          string                                `json:"retain_until"`
}

type configuredInformation struct {
	Config InformationSourceConfig
	Source *providers.HTTPInformation
}

const informationPolicyVersion = "information-received/1"

type informationPermitRecord struct {
	Scope         runtime.Scope               `json:"scope"`
	Auth          runtime.Auth                `json:"auth"`
	OperationID   string                      `json:"operation_id"`
	AdmissionHash string                      `json:"admission_hash"`
	AttemptID     string                      `json:"attempt_id"`
	Outlet        providers.HTTPOutRequest    `json:"outlet"`
	UseRefs       []api.ObjectRef             `json:"use_refs"`
	Permit        providers.InformationPermit `json:"permit"`
}

type informationPolicyBinding struct {
	Policy      memory.Policy                 `json:"policy"`
	Publication providers.ReceivedPublication `json:"publication"`
	Permit      informationPermitRecord       `json:"permit"`
}

func (a *App) configureInformation() ([]execution.Driver, error) {
	if len(a.Config.Information) > 8 {
		return nil, api.E("invalid_request", "information_source_limit")
	}
	drivers := []execution.Driver{}
	for _, original := range a.Config.Information {
		var c InformationSourceConfig
		if err := api.Decode(api.Raw(original), &c); err != nil {
			return nil, err
		}
		if _, err := api.ParseTime(c.RetainUntil); err != nil {
			return nil, err
		}
		if c.Source.Location != "cloud" || c.Source.CredentialRequired && c.CredentialEnv == "" {
			return nil, api.E("unsupported", "information_source_configuration_incomplete")
		}
		if c.CredentialEnv != "" && !regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,127}$`).MatchString(c.CredentialEnv) {
			return nil, api.E("invalid_request", "information_credential_reference_invalid")
		}
		var roots []byte
		var err error
		if c.TLSRootFile != "" {
			if !filepath.IsAbs(c.TLSRootFile) {
				return nil, api.E("invalid_request", "information_tls_file_must_be_absolute")
			}
			file, openErr := os.Open(c.TLSRootFile)
			if openErr != nil {
				return nil, openErr
			}
			roots, err = io.ReadAll(io.LimitReader(file, (64<<10)+1))
			err = errors.Join(err, file.Close())
			if err != nil {
				return nil, err
			}
			if len(roots) > 64<<10 {
				return nil, api.E("invalid_request", "information_tls_roots_too_large")
			}
		}
		source, err := providers.NewHTTPInformation(providers.InformationConfig{Store: a.Store, Scope: a.Scope, Source: c.Source, Content: informationContent{a}, Egress: informationEgress{a}, Participants: []string{"platform", "task", "governance", "content", "memory"}, Credential: os.Getenv(c.CredentialEnv), TLSRootPEM: roots, AllowHTTPForLoopback: a.Config.Development && c.AllowHTTPForLoopback})
		if err != nil {
			return nil, err
		}
		c.Source = source.Descriptor()
		for _, existing := range a.information {
			if api.Equal(existing.Config.Source.SourceRef, c.Source.SourceRef) {
				return nil, errors.Join(api.E("invalid_request", "duplicate_information_source"), source.Close())
			}
		}
		a.Information = append(a.Information, source)
		a.information = append(a.information, configuredInformation{c, source})
		drivers = append(drivers, source.Drivers()...)
	}
	return drivers, nil
}

// InformationInstallLock 固定准确源、凭据摘要、TLS 根和驱动合同；此函数不出站。
func InformationInstallLock(source *providers.HTTPInformation) (api.ComponentRef, error) {
	if source == nil {
		return api.ComponentRef{}, api.E("invalid_request", "information_source_missing")
	}
	refs := []api.ComponentRef{}
	for _, d := range source.Drivers() {
		refs = append(refs, d.Capability().Ref)
	}
	digest, err := api.Digest(struct {
		Descriptor providers.InformationSourceDescriptor `json:"descriptor"`
		Drivers    []api.ComponentRef                    `json:"drivers"`
	}{source.Descriptor(), refs})
	if err != nil {
		return api.ComponentRef{}, err
	}
	return api.ComponentRef{ComponentID: stableID("component", "information-lock/"+source.Descriptor().SourceRef.ComponentID), Version: "1", Digest: digest}, nil
}

func (a *App) configuredInformation(ref api.ComponentRef) (configuredInformation, error) {
	for _, c := range a.information {
		if api.Equal(c.Config.Source.SourceRef, ref) {
			if c.Config.Source.CredentialRequired && os.Getenv(c.Config.CredentialEnv) == "" {
				return configuredInformation{}, api.E("unsupported", "information_credential_unavailable")
			}
			return c, nil
		}
	}
	return configuredInformation{}, api.E("unsupported", "original_information_source_not_configured")
}

func (a *App) informationHeadsTx(ctx context.Context, tx runtime.Tx, record informationPermitRecord) (governance.UseReceipt, error) {
	if !api.Equal(record.Scope, tx.Scope()) || len(record.UseRefs) != 1 {
		return governance.UseReceipt{}, api.E("forbidden", "information_original_scope_mismatch")
	}
	if err := currentCredentialTx(ctx, tx, record.Auth); err != nil {
		return governance.UseReceipt{}, err
	}
	return a.Governance.CheckUseHeadsTx(ctx, tx, record.Auth, record.UseRefs[0], tx.Scope().Ref(record.OperationID, 1), record.AdmissionHash)
}

type informationEgress struct{ a *App }

func (g informationEgress) Check(ctx context.Context, r execution.AttemptRequest, out providers.HTTPOutRequest) (providers.InformationPermit, error) {
	c, err := g.a.configuredInformation(out.SourceRef)
	if err != nil {
		return providers.InformationPermit{}, err
	}
	if !api.Equal(r.Scope, g.a.Scope) || r.Auth.TenantID != r.Scope.TenantID || len(r.Invoke.UseRefs) != 1 || out.Receiver != c.Config.Source.Receiver || out.Location != c.Config.Source.Location || !api.Equal(out.ProcessedSources, r.Intent.ProcessedSourceRefs) || !api.Equal(out.DisclosedSources, r.Intent.DisclosedSourceRefs) || out.RequestDigest == "" {
		return providers.InformationPermit{}, api.E("forbidden", "information_outlet_binding_mismatch")
	}
	// 数据许可沿原完整 Grant 链的准确 head；查询在 Tx 外且后续强核原 revisions。
	raw, err := g.a.queryAs(ctx, r.Auth, "grant.use.get", r.Invoke.UseRefs[0].ObjectID, governance.IDInput{ID: r.Invoke.UseRefs[0].ObjectID})
	if err != nil {
		return providers.InformationPermit{}, err
	}
	var expectedUse governance.UseReceipt
	if err = api.Decode(raw, &expectedUse); err != nil || len(expectedUse.GrantRefs) > 64 {
		if err == nil {
			err = api.E("dependency_unavailable", "information_grant_chain_limit")
		}
		return providers.InformationPermit{}, err
	}
	retain, err := api.ParseTime(c.Config.RetainUntil)
	if err != nil {
		return providers.InformationPermit{}, err
	}
	for _, ref := range expectedUse.GrantRefs {
		raw, err = g.a.queryAs(ctx, r.Auth, "grant.read", ref.ObjectID, governance.IDInput{ID: ref.ObjectID})
		var grant governance.GrantRecord
		if err != nil {
			return providers.InformationPermit{}, err
		}
		if err = api.Decode(raw, &grant); err != nil {
			return providers.InformationPermit{}, err
		}
		if grant.Grant.Revision != ref.Revision || grant.Grant.State != "active" {
			return providers.InformationPermit{}, api.E("forbidden", "information_original_grant_changed")
		}
		until, err := api.ParseTime(grant.Grant.ExpiresAt)
		if err != nil {
			return providers.InformationPermit{}, err
		}
		if until.Before(retain) {
			retain = until
		}
	}
	record := informationPermitRecord{Scope: r.Scope, Auth: r.Auth, OperationID: r.Intent.OperationID, AttemptID: r.Attempt.AttemptID, Outlet: out, UseRefs: r.Invoke.UseRefs}
	status, err := g.a.Store.Within(ctx, r.Scope, []string{"task", "governance", "content", "memory", "platform"}, func(tx runtime.Tx) error {
		original, err := g.a.Task.OperationIntentTx(ctx, tx, r.Intent.OperationID)
		if err != nil {
			return err
		}
		record.AdmissionHash = original.IntentHash
		if err = currentCredentialTx(ctx, tx, r.Auth); err != nil {
			return err
		}
		// 先取得 Memory head 和全部输入来源门禁，再取得 Source Grant head。
		revision := uint64(1)
		for _, ref := range uniqueSources(append(append([]api.ContentRef{}, r.Intent.ProcessedSourceRefs...), r.Intent.DisclosedSourceRefs...)) {
			v, err := g.a.Memory.CheckContentTx(ctx, tx, r.Auth, ref, providers.InformationPurpose, out.Location, true)
			if err != nil {
				return err
			}
			until, err := api.ParseTime(v.RetentionUntil)
			if err != nil {
				return err
			}
			if until.Before(retain) {
				retain = until
			}
			if v.ControlRevision > revision {
				revision = v.ControlRevision
			}
		}
		admission, err := g.a.readActionAdmissionTx(ctx, tx, original)
		if err != nil {
			return err
		}
		if admission.Descriptor.Kind != out.Action || admission.Descriptor.Source == nil || !api.Equal(admission.Descriptor.Source.SourceRef, out.SourceRef) || admission.Descriptor.Recipient != out.Receiver || admission.Descriptor.Location != out.Location {
			return api.E("forbidden", "information_original_action_mismatch")
		}
		originalRetain, err := api.ParseTime(admission.Descriptor.SourceRetainUntil)
		if err != nil {
			return err
		}
		if originalRetain.Before(retain) {
			retain = originalRetain
		}
		use, err := g.a.informationHeadsTx(ctx, tx, record)
		if err != nil {
			return err
		}
		if !api.Equal(use, expectedUse) || use.Recipient != out.Receiver || use.Location != out.Location || !containsString(use.Purposes, original.AdmissionPurpose) {
			return api.E("forbidden", "information_original_use_changed")
		}
		permit, err := (executionAuthority{g.a}).VerifyStart(ctx, tx, execution.StartRequest{ControlWindow: r.Attempt.ControlWindow, Invoke: r.Invoke, Intent: r.Intent, AttemptID: r.Attempt.AttemptID, Auth: r.Auth}, r.Attempt.PreparedAuthority)
		if err != nil {
			return err
		}
		now, err := tx.Now(ctx)
		if err != nil {
			return err
		}
		if !now.Before(retain) {
			return api.E("gone", "information_data_permission_expired")
		}
		record.Permit = providers.InformationPermit{StartBefore: permit.StartBefore, PolicyRevision: revision, RequestDigest: out.RequestDigest, RetainUntil: api.Time(retain)}
		var fixed informationPermitRecord
		if _, err = tx.Get(ctx, "platform.information_permits", record.AttemptID, &fixed); err == nil {
			if !api.Equal(record, fixed) {
				return api.E("idempotency_conflict", "information_original_permit_changed")
			}
			record = fixed
			return nil
		} else if !api.IsCode(err, "not_found") {
			return err
		}
		return tx.Create(ctx, "platform.information_permits", record.AttemptID, record.OperationID, record)
	})
	if status == runtime.CommitUnknown {
		return providers.InformationPermit{}, runtime.ErrCommitUnknown
	}
	return record.Permit, err
}

type informationContent struct{ a *App }

// 信息结果中的命中和引用仍派生自原响应正文，不能把 metadata 出版成脱离许可的副本。
func (a *App) informationOutputSources(ctx context.Context, s runtime.Scope, auth runtime.Auth, sources []api.ContentRef, body []byte) ([]api.ContentRef, error) {
	var observation providers.InformationObservation
	if api.Decode(body, &observation) != nil || observation.BodyRef == nil {
		return sources, nil
	}
	for _, configured := range a.information {
		if !api.Equal(configured.Source.Descriptor().SourceRef, observation.SourceRef) {
			continue
		}
		original, _, err := configured.Source.ReadEvidence(ctx, s, auth, providers.InformationEvidenceRef{SourceRef: observation.SourceRef, AttemptID: observation.AttemptID})
		if err != nil {
			return nil, err
		}
		if !api.Equal(original, observation) {
			return nil, api.E("forbidden", "information_output_origin_changed")
		}
		return uniqueSources(append(append([]api.ContentRef{}, sources...), *original.BodyRef)), nil
	}
	return nil, api.E("unsupported", "original_information_source_not_configured")
}

func (c informationContent) ReadBytes(ctx context.Context, s runtime.Scope, auth runtime.Auth, ref api.ContentRef, purpose, location string) ([]byte, error) {
	return c.a.Memory.ReadBytes(ctx, s, auth, ref, purpose, location)
}

func (c informationContent) publicationPlan(ctx context.Context, s runtime.Scope, auth runtime.Auth, p providers.ReceivedPublication, create bool) (publicationPlan, memory.Policy, error) {
	var record informationPermitRecord
	if _, err := c.a.Store.Read(ctx, s, "platform.information_permits", p.AttemptID, 1, &record); err != nil {
		return publicationPlan{}, memory.Policy{}, err
	}
	if !api.Equal(record.Auth, auth) || !api.Equal(record.Scope, s) || !api.Equal(record.Outlet.SourceRef, p.SourceRef) || !api.Equal(record.UseRefs, p.UseRefs) || record.Permit.RetainUntil != p.RetainUntil || !api.Equal(record.Outlet.ProcessedSources, p.ProcessedSources) || !api.Equal(record.Outlet.DisclosedSources, p.DisclosedSources) || p.ContentRef.TenantID != s.TenantID || p.ContentRef.OwnerID != s.OwnerID {
		return publicationPlan{}, memory.Policy{}, api.E("forbidden", "information_publication_binding_mismatch")
	}
	obtained, err := api.ParseTime(p.ObtainedAt)
	if err != nil {
		return publicationPlan{}, memory.Policy{}, err
	}
	retain, err := api.ParseTime(p.RetainUntil)
	if err != nil || !obtained.Before(retain) {
		return publicationPlan{}, memory.Policy{}, api.E("gone", "information_original_data_expired")
	}
	values := c.a.ContentPolicy.Values
	policyEnd, err := api.ParseTime(values.RetainUntil)
	if err != nil {
		return publicationPlan{}, memory.Policy{}, err
	}
	if policyEnd.Before(retain) {
		retain = policyEnd
	}
	values.RetainUntil = api.Time(retain)
	policy := memory.Policy{PolicyRef: api.ComponentRef{ComponentID: stableID("component", "information-policy/"+p.ContentRef.ContentID), Version: informationPolicyVersion}, Values: values, Revision: 1, State: "active"}
	policy.PolicyRef.Digest, err = api.Digest(values)
	if err != nil {
		return publicationPlan{}, memory.Policy{}, err
	}
	var plan publicationPlan
	status, err := c.a.Store.Within(ctx, s, []string{"platform", "governance", "content", "memory"}, func(tx runtime.Tx) error {
		for _, source := range uniqueSources(append(append([]api.ContentRef{}, p.ProcessedSources...), p.DisclosedSources...)) {
			v, err := c.a.Memory.CheckContentTx(ctx, tx, auth, source, providers.InformationPurpose, record.Outlet.Location, true)
			if err != nil {
				return err
			}
			until, err := api.ParseTime(v.RetentionUntil)
			if err != nil {
				return err
			}
			if until.Before(retain) {
				retain = until
			}
		}
		if _, err := c.a.informationHeadsTx(ctx, tx, record); err != nil {
			return err
		}
		now, err := tx.Now(ctx)
		if err != nil {
			return err
		}
		if !now.Before(retain) {
			return api.E("gone", "information_original_data_expired")
		}
		values.RetainUntil = api.Time(retain)
		policy.Values = values
		policy.PolicyRef.Digest, err = api.Digest(values)
		if err != nil {
			return err
		}
		_, err = tx.Get(ctx, "platform.publications", p.ContentRef.ContentID, &plan)
		if err == nil {
			if !api.Equal(plan.Ref, p.ContentRef) || !api.Equal(plan.Processed, p.ProcessedSources) || !api.Equal(plan.Disclosed, p.DisclosedSources) || plan.SubjectID != auth.SubjectID || plan.PolicyRef == nil {
				return api.E("idempotency_conflict", "information_original_publication_changed")
			}
			var original informationPolicyBinding
			if err = tx.GetVersion(ctx, "platform.information_policies", plan.PolicyRef.ComponentID, 1, &original); err != nil {
				return err
			}
			if !api.Equal(original.Publication, p) || !api.Equal(original.Permit, record) {
				return api.E("idempotency_conflict", "information_original_publication_changed")
			}
			policy = original.Policy
			return nil
		}
		if !api.IsCode(err, "not_found") {
			return err
		}
		if !create {
			return api.E("not_found", "original_information_publication_missing")
		}
		deadline := obtained.Add(30 * time.Minute)
		if retain.Before(deadline) {
			deadline = retain
		}
		plan = publicationPlan{Ref: p.ContentRef, TransferID: api.NewID("transfer"), ReserveID: api.NewID("command"), PutID: api.NewID("command"), Processed: p.ProcessedSources, Disclosed: p.DisclosedSources, Retention: api.Time(retain), Deadline: api.Time(deadline), SubjectID: auth.SubjectID, PolicyRef: &policy.PolicyRef}
		if err = tx.Create(ctx, "platform.information_policies", policy.PolicyRef.ComponentID, p.ContentRef.ContentID, informationPolicyBinding{policy, p, record}); err != nil {
			return err
		}
		return tx.Create(ctx, "platform.publications", p.ContentRef.ContentID, auth.SubjectID, plan)
	})
	if status == runtime.CommitUnknown {
		return plan, policy, runtime.ErrCommitUnknown
	}
	return plan, policy, err
}

func (c informationContent) PublishReceived(ctx context.Context, s runtime.Scope, auth runtime.Auth, p providers.ReceivedPublication, body []byte) (api.ContentRef, error) {
	if api.Hash(body) != p.ContentRef.Hash || uint64(len(body)) != p.ContentRef.ByteLength {
		return api.ContentRef{}, api.E("invalid_request", "information_original_bytes_changed")
	}
	plan, policy, err := c.publicationPlan(ctx, s, auth, p, true)
	if err != nil {
		return api.ContentRef{}, err
	}
	if err = c.a.Memory.InstallPolicy(ctx, s, auth, policy); err != nil {
		return api.ContentRef{}, err
	}
	ref, err := c.a.Memory.Upload(ctx, s, auth, memory.PublicationRequest{ContentRef: plan.Ref, TransferID: plan.TransferID, ReserveCommandID: plan.ReserveID, PutCommandID: plan.PutID, PolicyRef: policy.PolicyRef, ProcessedSources: plan.Processed, DisclosedSources: plan.Disclosed, RetentionUntil: plan.Retention, TransferDeadline: plan.Deadline}, body)
	if err != nil {
		return ref, err
	}
	_, err = c.a.Memory.ReadBytes(ctx, s, auth, ref, providers.InformationPurpose, "cloud")
	return ref, err
}

func (c informationContent) RecoverReceived(ctx context.Context, s runtime.Scope, auth runtime.Auth, p providers.ReceivedPublication) (api.ContentRef, error) {
	plan, policy, err := c.publicationPlan(ctx, s, auth, p, false)
	if err != nil {
		return api.ContentRef{}, err
	}
	if err = c.a.Memory.InstallPolicy(ctx, s, auth, policy); err != nil {
		return api.ContentRef{}, err
	}
	ref, err := c.a.Memory.RecoverUpload(ctx, s, auth, memory.PublicationRequest{ContentRef: plan.Ref, TransferID: plan.TransferID, ReserveCommandID: plan.ReserveID, PutCommandID: plan.PutID, PolicyRef: policy.PolicyRef, ProcessedSources: plan.Processed, DisclosedSources: plan.Disclosed, RetentionUntil: plan.Retention, TransferDeadline: plan.Deadline})
	if err != nil {
		return ref, err
	}
	_, err = c.a.Memory.ReadBytes(ctx, s, auth, ref, providers.InformationPurpose, "cloud")
	return ref, err
}

// checkInformationPolicyTx 收紧直接及全部派生 Content 的当前原许可；不授予新HTTP。
func (a *App) checkInformationPolicyTx(ctx context.Context, tx runtime.Tx, policyRef api.ComponentRef) error {
	if policyRef.Version != informationPolicyVersion {
		return nil
	}
	var binding informationPolicyBinding
	err := tx.GetVersion(ctx, "platform.information_policies", policyRef.ComponentID, 1, &binding)
	if api.IsCode(err, "not_found") {
		return api.E("forbidden", "information_policy_authority_missing")
	}
	if err != nil {
		return err
	}
	if !api.Equal(binding.Policy.PolicyRef, policyRef) {
		return api.E("forbidden", "information_policy_binding_mismatch")
	}
	now, err := tx.Now(ctx)
	if err != nil {
		return err
	}
	until, err := api.ParseTime(binding.Policy.Values.RetainUntil)
	if err != nil || !now.Before(until) {
		return api.E("gone", "information_original_data_expired")
	}
	current, err := a.configuredInformation(binding.Publication.SourceRef)
	if err != nil {
		return err
	}
	currentUntil, err := api.ParseTime(current.Config.RetainUntil)
	if err != nil {
		return err
	}
	if !now.Before(currentUntil) {
		return api.E("gone", "information_current_data_permission_expired")
	}
	_, err = a.informationHeadsTx(ctx, tx, binding.Permit)
	return err
}
