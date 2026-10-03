package wasi

import (
	"context"
	"sort"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/execution"
	rt "github.com/ruipengliu/lerna/runtime"
)

type cellEncoding struct {
	Arguments         execution.ComputeArguments `json:"arguments"`
	Limits            Limits                     `json:"limits"`
	InstallLock       api.ComponentRef           `json:"install_lock"`
	EnvironmentConfig api.ComponentRef           `json:"environment_config"`
	InputHash         string                     `json:"input_hash"`
}

func (r *Runtime) Prepare(ctx context.Context, sc rt.Scope, auth rt.Auth, invoke execution.InvokeInput, intent execution.ExecutionIntent, raw []byte) (execution.PreparedRequest, error) {
	if sc != r.cfg.Scope || auth.TenantID != sc.TenantID || !api.Equal(intent.InstallLockRef, r.InstallLockRef()) {
		return execution.PreparedRequest{}, api.E("forbidden", "wasi_scope_or_lock_changed")
	}
	var args execution.ComputeArguments
	if err := api.Decode(raw, &args); err != nil {
		return execution.PreparedRequest{}, err
	}
	if err := rt.CheckRef(sc, args.EnvironmentRef); err != nil {
		return execution.PreparedRequest{}, err
	}
	if args.EnvironmentRef.OwnerID != sc.OwnerID || !api.Equal(args.OutputSchemaRef, execution.NamespaceOutputSchemaRef()) {
		return execution.PreparedRequest{}, api.E("unsupported", "compute_output_schema_not_supported")
	}
	var env execution.Environment
	if _, err := r.cfg.Store.Read(ctx, sc, execution.Namespace+".environments", args.EnvironmentRef.ObjectID, 0, &env); err != nil {
		return execution.PreparedRequest{}, err
	}
	if _, err := r.Check(env.ConfigRef, env.InstallLockRef, env.Limits); err != nil {
		return execution.PreparedRequest{}, err
	}
	if env.Generation != args.ExpectedGeneration {
		return execution.PreparedRequest{}, api.E("revision_conflict", "generation_changed")
	}
	if env.NamespaceRevision != args.ExpectedNamespaceRevision {
		return execution.PreparedRequest{}, api.E("revision_conflict", "namespace_changed")
	}
	if env.Phase != "active" || !env.ReadyForCell || !env.ActuallyExited || env.NamespaceRef == nil || len(env.ActiveOperationIDs) > 0 || len(env.StopResiduals) > 0 {
		return execution.PreparedRequest{}, api.E("invalid_state", "environment_busy")
	}
	if auth.SubjectID != env.Principal.SubjectID && !auth.HasRole("admin") && !auth.HasRole("executor") {
		return execution.PreparedRequest{}, api.E("forbidden", "environment_not_disclosed")
	}
	limits, err := parseLimits(env.Limits)
	if err != nil {
		return execution.PreparedRequest{}, err
	}
	cpuBound := false
	for _, amount := range intent.CostBound {
		if amount.Unit == "cpu_seconds" {
			comparison, e := api.CompareDecimal(amount.Value, decimalSeconds(limits.CPUSeconds+1, 0))
			cpuBound = e == nil && comparison >= 0
		}
	}
	if !cpuBound {
		return execution.PreparedRequest{}, api.E("invalid_request", "wasi_cpu_bound_missing")
	}
	readNamespace := func(ref api.ContentRef, purpose string) (execution.PassiveNamespace, error) {
		if ref.ByteLength > limits.NamespaceBytes {
			return execution.PassiveNamespace{}, api.E("overloaded", "namespace_byte_limit")
		}
		b, err := r.cfg.Content.ReadBytes(ctx, sc, auth, ref, purpose, r.cfg.Location)
		if err != nil {
			return execution.PassiveNamespace{}, err
		}
		if uint64(len(b)) != ref.ByteLength || api.Hash(b) != ref.Hash {
			return execution.PassiveNamespace{}, api.E("invalid_request", "content_bytes_changed")
		}
		var n execution.PassiveNamespace
		if err = api.Decode(b, &n); err != nil {
			return n, err
		}
		return n, execution.ValidateNamespace(n)
	}
	ns, err := readNamespace(*env.NamespaceRef, "environment_namespace")
	if err != nil {
		return execution.PreparedRequest{}, err
	}
	input, err := readNamespace(args.InputRef, "environment_input")
	if err != nil {
		return execution.PreparedRequest{}, err
	}
	if _, err = r.readModule(ctx, sc, auth, args.CodeRef); err != nil {
		return execution.PreparedRequest{}, err
	}
	values := map[string]execution.NamespaceBinding{}
	for _, v := range ns.Bindings {
		values[v.Name] = v
	}
	for _, v := range input.Bindings {
		values[v.Name] = v
	}
	ns.Bindings = []execution.NamespaceBinding{}
	for _, v := range values {
		ns.Bindings = append(ns.Bindings, v)
	}
	sort.Slice(ns.Bindings, func(i, j int) bool { return ns.Bindings[i].Name < ns.Bindings[j].Name })
	if err = execution.ValidateNamespace(ns); err != nil {
		return execution.PreparedRequest{}, err
	}
	inputBytes := api.Raw(ns)
	if uint64(len(inputBytes)) > limits.NamespaceBytes {
		return execution.PreparedRequest{}, api.E("overloaded", "namespace_byte_limit")
	}
	sources := append([]api.ContentRef{}, env.ProcessedSources...)
	for _, ref := range []api.ContentRef{args.CodeRef, args.InputRef, *env.NamespaceRef} {
		found := false
		for _, old := range sources {
			found = found || api.Equal(old, ref)
		}
		if !found {
			sources = append(sources, ref)
		}
	}
	if len(sources) > 100 {
		return execution.PreparedRequest{}, api.E("overloaded", "namespace_source_limit")
	}
	for _, ref := range sources {
		found := false
		for _, allowed := range intent.ProcessedSourceRefs {
			found = found || api.Equal(allowed, ref)
		}
		if !found {
			return execution.PreparedRequest{}, api.E("forbidden", "compute_source_not_in_intent")
		}
	}
	encoded, err := api.Canonical(api.Raw(cellEncoding{Arguments: args, Limits: limits, InstallLock: r.InstallLockRef(), EnvironmentConfig: r.EnvironmentConfigRef(), InputHash: api.Hash(inputBytes)}))
	if err != nil {
		return execution.PreparedRequest{}, err
	}
	return execution.PreparedRequest{Encoded: encoded, Digest: api.Hash(encoded), Cell: &execution.CellPreparation{EnvironmentRef: args.EnvironmentRef, InstanceID: env.InstanceID, ExpectedGeneration: env.Generation, ExpectedNamespaceRevision: env.NamespaceRevision, Namespace: ns, Sources: sources, DynamicNamespace: true, NamespaceByteLimit: limits.NamespaceBytes}}, nil
}
func (r *Runtime) readModule(ctx context.Context, sc rt.Scope, auth rt.Auth, ref api.ContentRef) ([]byte, error) {
	if ref.ByteLength < 8 || ref.ByteLength > MaxModuleBytes || ref.TenantID != sc.TenantID {
		return nil, api.E("invalid_request", "invalid_wasi_module_size")
	}
	code, err := r.cfg.Content.ReadBytes(ctx, sc, auth, ref, "environment_code", r.cfg.Location)
	if err != nil {
		return nil, err
	}
	if uint64(len(code)) != ref.ByteLength || api.Hash(code) != ref.Hash || len(code) < 8 || string(code[:8]) != "\x00asm\x01\x00\x00\x00" {
		return nil, api.E("invalid_request", "invalid_wasi_module_bytes")
	}
	return code, nil
}
func (r *Runtime) decodeAttempt(q execution.AttemptRequest) (cellEncoding, string, error) {
	var encoded cellEncoding
	cell := q.Attempt.Prepared.Cell
	if q.Scope != r.cfg.Scope || q.Auth.TenantID != q.Scope.TenantID || !api.ValidID(q.Attempt.AttemptID) || q.Attempt.OperationID != q.Invoke.OperationID || q.Attempt.Prepared.Digest != api.Hash(q.Attempt.Prepared.Encoded) || cell == nil || !cell.DynamicNamespace {
		return encoded, "", api.E("invalid_state", "wasi_attempt_binding_changed")
	}
	if err := api.Decode(q.Attempt.Prepared.Encoded, &encoded); err != nil {
		return encoded, "", err
	}
	if !api.Equal(encoded.InstallLock, r.InstallLockRef()) || !api.Equal(q.Intent.InstallLockRef, r.InstallLockRef()) || !api.Equal(encoded.EnvironmentConfig, r.EnvironmentConfigRef()) || !api.Equal(encoded.Arguments.EnvironmentRef, cell.EnvironmentRef) || encoded.Arguments.ExpectedGeneration != cell.ExpectedGeneration || encoded.Arguments.ExpectedNamespaceRevision != cell.ExpectedNamespaceRevision || encoded.InputHash != api.Hash(api.Raw(cell.Namespace)) || cell.NamespaceByteLimit != encoded.Limits.NamespaceBytes {
		return encoded, "", api.E("invalid_state", "wasi_attempt_binding_changed")
	}
	if _, err := parseLimits([]api.Amount{{Unit: "namespace_bytes", Value: decimalSeconds(encoded.Limits.NamespaceBytes, 0)}, {Unit: "memory_pages", Value: decimalSeconds(uint64(encoded.Limits.MemoryPages), 0)}, {Unit: "cpu_seconds", Value: decimalSeconds(encoded.Limits.CPUSeconds, 0)}, {Unit: "wall_millis", Value: decimalSeconds(encoded.Limits.WallMillis, 0)}, {Unit: "output_bytes", Value: decimalSeconds(encoded.Limits.OutputBytes, 0)}}); err != nil {
		return encoded, "", err
	}
	digest, err := api.Digest([]any{q.Scope, q.Invoke, q.Attempt.AttemptID, q.Attempt.ControlWindowID, q.Attempt.Prepared, q.Intent})
	return encoded, digest, err
}
