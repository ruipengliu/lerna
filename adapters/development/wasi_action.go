package development

import (
	"context"
	"strconv"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/brain"
	"github.com/ruipengliu/lerna/internal/execution"
	"github.com/ruipengliu/lerna/internal/task"
	"github.com/ruipengliu/lerna/runtime"
)

// WASICellConfig 是受信装配的准确程序及输入，模型只能建议原声明的 CAS。
type WASICellConfig struct {
	EnvironmentID string         `json:"environment_id"`
	CodeRef       api.ContentRef `json:"code_ref"`
	InputRef      api.ContentRef `json:"input_ref"`
}

type wasiCellDeclaration struct {
	Arguments    execution.ComputeArguments `json:"arguments"`
	NamespaceRef api.ContentRef             `json:"namespace_ref"`
	SourceRefs   []api.ContentRef           `json:"source_refs"`
	CostBound    []api.Amount               `json:"cost_bound"`
}

func (a *App) validateWASICellConfiguration(d actionDescriptor) error {
	if a.WASI == nil || a.Config.WASI == nil || a.Config.WASI.CPUSecondsBudgetLimit == "" {
		return api.E("unsupported", "finite_wasi_task_cpu_policy_not_configured")
	}
	if d.Cell == nil || !api.ValidID(d.Cell.EnvironmentID) || api.ValidateRecord("ContentRef", d.Cell.CodeRef) != nil || api.ValidateRecord("ContentRef", d.Cell.InputRef) != nil || d.Cell.CodeRef.MediaType != "application/wasm" || d.Cell.CodeRef.ByteLength == 0 || d.Cell.CodeRef.ByteLength > 1<<20 || d.Cell.InputRef.ByteLength > 65536 {
		return api.E("invalid_request", "exact_wasi_cell_configuration_required")
	}
	if d.Recipient != a.Scope.OwnerID || d.Location != "cloud" || len(d.Resources) != 1 || d.Resources[0] != "environment:"+d.Cell.EnvironmentID || len(d.Actions) != 1 || d.Actions[0] != "environment.run_cell" || !api.Equal(d.InstallLockRef, a.WASI.InstallLockRef) {
		return api.E("forbidden", "wasi_cell_scope_or_install_lock_mismatch")
	}
	return nil
}

func (a *App) freezeWASICell(ctx context.Context, scope runtime.Scope, d actionDescriptor) (wasiCellDeclaration, error) {
	var out wasiCellDeclaration
	if err := a.validateWASICellConfiguration(d); err != nil {
		return out, err
	}
	raw, err := a.query(ctx, "environment.get", d.Cell.EnvironmentID, execution.EnvironmentIDInput{EnvironmentID: d.Cell.EnvironmentID})
	if err != nil {
		return out, err
	}
	var env execution.Environment
	if err = api.Decode(raw, &env); err != nil {
		return out, err
	}
	if env.Phase != "active" || !env.ReadyForCell || !env.ActuallyExited || env.NamespaceRef == nil || len(env.ActiveOperationIDs) != 0 || len(env.StopResiduals) != 0 || env.RuntimeKind != "restricted_wasi_preview1" || !api.Equal(env.ConfigRef, a.WASI.ConfigRef) || !api.Equal(env.InstallLockRef, d.InstallLockRef) || env.Principal.SubjectID != a.ServiceAuth.SubjectID {
		return out, api.E("invalid_state", "original_wasi_environment_not_ready")
	}
	if _, err = a.WASI.Admission.Check(env.ConfigRef, env.InstallLockRef, env.Limits); err != nil {
		return out, err
	}
	out.Arguments = execution.ComputeArguments{EnvironmentRef: scope.Ref(env.EnvironmentID, env.Revision), ExpectedGeneration: env.Generation, ExpectedNamespaceRevision: env.NamespaceRevision, CodeRef: d.Cell.CodeRef, InputRef: d.Cell.InputRef, OutputSchemaRef: execution.NamespaceOutputSchemaRef()}
	out.NamespaceRef = *env.NamespaceRef
	out.SourceRefs = uniqueSources(append(append([]api.ContentRef{}, env.ProcessedSources...), d.Cell.CodeRef, d.Cell.InputRef, *env.NamespaceRef))
	for _, amount := range env.Limits {
		if amount.Unit != "cpu_seconds" {
			continue
		}
		seconds, err := strconv.ParseUint(amount.Value, 10, 64)
		if err != nil || seconds == 0 || seconds > 5 {
			return out, api.E("invalid_request", "finite_wasi_cell_cpu_limit_required")
		}
		// Linux RLIMIT_CPU 的硬限比软限多一秒，原 reservation 必须覆盖它。
		out.CostBound = []api.Amount{{Unit: "cpu_seconds", Value: strconv.FormatUint(seconds+1, 10)}}
	}
	if len(out.CostBound) != 1 || len(out.SourceRefs) > 100 {
		return out, api.E("invalid_request", "finite_wasi_cell_sources_and_cpu_required")
	}
	for _, ref := range out.SourceRefs {
		purpose := "task.context"
		if api.Equal(ref, d.Cell.CodeRef) {
			purpose = "environment_code"
		} else if api.Equal(ref, d.Cell.InputRef) {
			purpose = "environment_input"
		} else if api.Equal(ref, *env.NamespaceRef) {
			purpose = "environment_namespace"
		}
		bytes, err := a.Memory.Read(ctx, scope, a.ServiceAuth, ref, purpose)
		if err != nil {
			return out, err
		}
		if uint64(len(bytes)) != ref.ByteLength || api.Hash(bytes) != ref.Hash {
			return out, api.E("invalid_request", "original_wasi_content_bytes_changed")
		}
	}
	return out, nil
}

func (a *App) prepareWASICell(ctx context.Context, scope runtime.Scope, snap api.Snapshot, d actionDescriptor, candidate brain.ActionCandidate, raw []byte) error {
	if err := a.validateWASICellConfiguration(d); err != nil {
		return err
	}
	for _, ref := range d.PreparedCell.SourceRefs {
		found := false
		for _, source := range candidate.ProcessedSourceRefs {
			found = found || api.Equal(ref, source)
		}
		if !found {
			return api.E("forbidden", "original_wasi_source_not_declared")
		}
	}
	intent := execution.ExecutionIntent{InstallLockRef: d.InstallLockRef, TaskRef: snap.TaskRef, CapabilityRef: candidate.CapabilityRef, ArgumentsRef: candidate.ArgumentsRef, CostBound: d.PreparedCell.CostBound, ProcessedSourceRefs: candidate.ProcessedSourceRefs, DisclosedSourceRefs: candidate.DisclosedSourceRefs}
	_, err := a.WASI.Driver.Prepare(ctx, scope, a.ServiceAuth, execution.InvokeInput{TaskRef: snap.TaskRef, CapabilityRef: candidate.CapabilityRef}, intent, raw)
	return err
}

// WASM 的原 bytes 留在完整 ProcessedSources；模型材料只含准确普通声明。
// 旧已运行 Cell 的原参数提供二进制身份，不依赖新的宿主配置来猜来源。
func (a *App) wasiContext(ctx context.Context, scope runtime.Scope, facts task.ContextFacts, registered actionSnapshot, processed []api.ContentRef) ([]api.ContentRef, []api.ContentRef, error) {
	binary := []api.ContentRef{}
	for _, d := range registered.Entries {
		if d.PreparedCell != nil {
			processed = append(processed, d.PreparedCell.SourceRefs...)
			binary = append(binary, d.PreparedCell.Arguments.CodeRef)
		}
	}
	for _, op := range facts.Operations {
		if !api.Equal(op.Intent.CapabilityRef, execution.WASIRunCellCapability().Ref) {
			continue
		}
		bytes, err := a.Memory.Read(ctx, scope, a.ServiceAuth, op.Intent.ArgumentsRef, "execution.arguments")
		if err != nil {
			return nil, nil, err
		}
		var args execution.ComputeArguments
		if err = api.Decode(bytes, &args); err != nil {
			return nil, nil, err
		}
		binary = append(binary, args.CodeRef)
		if !op.Fact.Closed || op.Fact.MayApplyLater || op.Fact.Effect != "applied" {
			continue
		}
		operation, err := (executionBridge{a}).Read(ctx, scope, op.Fact.Ref)
		if err != nil {
			return nil, nil, err
		}
		later, known := operation.MayApplyLater.(bool)
		if operation.ExecutionState != "closed" || operation.Effect != "applied" || !known || later || operation.ResultRef == nil {
			return nil, nil, api.E("dependency_unavailable", "original_wasi_result_not_closed")
		}
		resultBytes, err := a.Memory.Read(ctx, scope, a.ServiceAuth, *operation.ResultRef, "execution_result")
		if err != nil {
			return nil, nil, err
		}
		var result execution.CellResult
		if err = api.Decode(resultBytes, &result); err != nil {
			return nil, nil, err
		}
		if runtime.CheckRef(scope, result.OperationRef) != nil || result.OperationRef.ObjectID != op.Intent.OperationID || result.Generation != args.ExpectedGeneration || result.NamespaceRevision != args.ExpectedNamespaceRevision+1 || api.ValidateRecord("ContentRef", result.NamespaceRef) != nil || result.NamespaceRef.TenantID != scope.TenantID || result.NamespaceRef.OwnerID != scope.OwnerID || result.NamespaceRef.MediaType != "application/json" || result.NamespaceRef.ByteLength > 65536 {
			return nil, nil, api.E("forbidden", "original_wasi_namespace_result_mismatch")
		}
		namespaceBytes, err := a.Memory.Read(ctx, scope, a.ServiceAuth, result.NamespaceRef, "environment_namespace")
		if err != nil {
			return nil, nil, err
		}
		var namespace execution.PassiveNamespace
		if uint64(len(namespaceBytes)) != result.NamespaceRef.ByteLength || api.Hash(namespaceBytes) != result.NamespaceRef.Hash {
			return nil, nil, api.E("forbidden", "original_wasi_namespace_bytes_changed")
		}
		if err = api.Decode(namespaceBytes, &namespace); err != nil {
			return nil, nil, err
		}
		if err = execution.ValidateNamespace(namespace); err != nil {
			return nil, nil, err
		}
		// 只装载原 CellResult 指定的完整已提交输出，不读取新的 Environment 头。
		processed = append(processed, result.NamespaceRef)
	}
	processed = uniqueSources(processed)
	materials := []api.ContentRef{}
	for _, ref := range processed {
		isBinary := false
		for _, code := range binary {
			isBinary = isBinary || api.Equal(code, ref)
		}
		if !isBinary {
			materials = append(materials, ref)
		}
	}
	return processed, materials, nil
}
