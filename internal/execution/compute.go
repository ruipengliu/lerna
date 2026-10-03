package execution

import (
	"context"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ruipengliu/lerna/api"
	rt "github.com/ruipengliu/lerna/runtime"
)

// TrustedComputeDriver 执行闭合、有界的内置纯计算指令；从不运行用户字节码/源码。
// 它不提供 WASI 隔离，也不能据此开放 environment.run_cell。
type TrustedComputeDriver struct {
	Content  ContentPort
	Store    rt.Store
	Location string
}
type ComputeArguments struct {
	EnvironmentRef            api.ObjectRef    `json:"environment_ref"`
	ExpectedGeneration        uint64           `json:"expected_generation"`
	ExpectedNamespaceRevision uint64           `json:"expected_namespace_revision"`
	CodeRef                   api.ContentRef   `json:"code_ref"`
	InputRef                  api.ContentRef   `json:"input_ref"`
	OutputSchemaRef           api.ComponentRef `json:"output_schema_ref"`
}
type ComputeProgram struct {
	Opcode     string   `json:"opcode"`
	TargetName string   `json:"target_name"`
	InputNames []string `json:"input_names"`
}
type CellResult struct {
	OperationRef      api.ObjectRef  `json:"operation_ref"`
	NamespaceRef      api.ContentRef `json:"namespace_ref"`
	NamespaceRevision uint64         `json:"namespace_revision"`
	Generation        uint64         `json:"generation"`
}

func TrustedComputeCapability() Capability {
	input := api.SchemaFor[ComputeArguments]()
	output := api.SchemaFor[CellResult]()
	digest, _ := api.Digest([]any{"environment.trusted_compute", "1", input, output, PassiveEnvironmentFormat})
	return Capability{Ref: api.ComponentRef{ComponentID: BuiltinComponentID("environment.trusted_compute"), Version: "1", Digest: digest}, EffectClass: "no_idempotency_guarantee", MaxAttempts: 1, InputSchema: input, OutputSchema: output}
}
func NamespaceOutputSchemaRef() api.ComponentRef {
	digest, _ := api.Digest(api.SchemaFor[CellResult]())
	return api.ComponentRef{ComponentID: BuiltinComponentID("environment.namespace.result"), Version: "1", Digest: digest}
}
func (d *TrustedComputeDriver) Capability() Capability { return TrustedComputeCapability() }

var bindingName = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_]{0,63}$`)

func validateNamespace(n PassiveNamespace) error {
	if n.Format != PassiveEnvironmentFormat || len(n.Bindings) > 100 {
		return api.E("invalid_request", "invalid_passive_namespace")
	}
	seen := map[string]bool{}
	for _, b := range n.Bindings {
		if !bindingName.MatchString(b.Name) || seen[b.Name] {
			return api.E("invalid_request", "invalid_namespace_binding")
		}
		seen[b.Name] = true
		switch b.Kind {
		case "string":
			if b.Decimal != "" || b.Bool != nil || len(b.String) > 4096 {
				return api.E("invalid_request", "invalid_namespace_string")
			}
		case "decimal":
			if b.String != "" || b.Bool != nil {
				return api.E("invalid_request", "invalid_namespace_decimal")
			}
			if _, err := api.CompareDecimal(b.Decimal, "0"); err != nil {
				return err
			}
		case "bool":
			if b.Bool == nil || b.String != "" || b.Decimal != "" {
				return api.E("invalid_request", "invalid_namespace_bool")
			}
		default:
			return api.E("invalid_request", "invalid_namespace_kind")
		}
	}
	return nil
}
func (d *TrustedComputeDriver) Prepare(ctx context.Context, sc rt.Scope, a rt.Auth, invoke InvokeInput, intent ExecutionIntent, raw []byte) (PreparedRequest, error) {
	if d.Content == nil || d.Store == nil {
		return PreparedRequest{}, api.E("dependency_unavailable", "passive_compute_not_configured")
	}
	var args ComputeArguments
	if err := api.Decode(raw, &args); err != nil {
		return PreparedRequest{}, err
	}
	if err := rt.CheckRef(sc, args.EnvironmentRef); err != nil {
		return PreparedRequest{}, err
	}
	if args.EnvironmentRef.OwnerID != sc.OwnerID || !api.Equal(args.OutputSchemaRef, NamespaceOutputSchemaRef()) {
		return PreparedRequest{}, api.E("unsupported", "compute_output_schema_not_supported")
	}
	var env Environment
	if _, err := d.Store.Read(ctx, sc, Namespace+".environments", args.EnvironmentRef.ObjectID, 0, &env); err != nil {
		return PreparedRequest{}, err
	}
	if env.Generation != args.ExpectedGeneration {
		return PreparedRequest{}, api.E("revision_conflict", "generation_changed")
	}
	if env.NamespaceRevision != args.ExpectedNamespaceRevision {
		return PreparedRequest{}, api.E("revision_conflict", "namespace_changed")
	}
	if env.Phase != "active" || !env.ReadyForCell || env.NamespaceRef == nil || len(env.ActiveOperationIDs) > 0 || len(env.StopResiduals) > 0 {
		return PreparedRequest{}, api.E("invalid_state", "environment_busy")
	}
	if a.SubjectID != env.Principal.SubjectID && !a.HasRole("admin") && !a.HasRole("executor") {
		return PreparedRequest{}, api.E("forbidden", "environment_not_disclosed")
	}
	rawNamespace, err := d.Content.ReadBytes(ctx, sc, a, *env.NamespaceRef, "environment_namespace", d.Location)
	if err != nil {
		return PreparedRequest{}, err
	}
	var ns PassiveNamespace
	if err = api.Decode(rawNamespace, &ns); err != nil {
		return PreparedRequest{}, err
	}
	if err = validateNamespace(ns); err != nil {
		return PreparedRequest{}, err
	}
	inputRaw, err := d.Content.ReadBytes(ctx, sc, a, args.InputRef, "environment_input", d.Location)
	if err != nil {
		return PreparedRequest{}, err
	}
	var input PassiveNamespace
	if err = api.Decode(inputRaw, &input); err != nil {
		return PreparedRequest{}, err
	}
	if err = validateNamespace(input); err != nil {
		return PreparedRequest{}, err
	}
	code, err := d.Content.ReadBytes(ctx, sc, a, args.CodeRef, "environment_compute_spec", d.Location)
	if err != nil {
		return PreparedRequest{}, err
	}
	var program ComputeProgram
	if err = api.Decode(code, &program); err != nil {
		return PreparedRequest{}, err
	}
	if !bindingName.MatchString(program.TargetName) || len(program.InputNames) > 100 {
		return PreparedRequest{}, api.E("invalid_request", "invalid_compute_program")
	}
	values := map[string]NamespaceBinding{}
	for _, v := range ns.Bindings {
		values[v.Name] = v
	}
	for _, v := range input.Bindings {
		values[v.Name] = v
	}
	operands := []NamespaceBinding{}
	for _, name := range program.InputNames {
		value, ok := values[name]
		if !ok {
			return PreparedRequest{}, api.E("invalid_request", "compute_input_missing")
		}
		operands = append(operands, value)
	}
	result := NamespaceBinding{Name: program.TargetName}
	switch program.Opcode {
	case "add_decimal":
		result.Kind = "decimal"
		result.Decimal = "0"
		for _, operand := range operands {
			if operand.Kind != "decimal" {
				return PreparedRequest{}, api.E("invalid_request", "compute_type_mismatch")
			}
			result.Decimal, err = api.AddDecimal(result.Decimal, operand.Decimal)
			if err != nil {
				return PreparedRequest{}, err
			}
		}
	case "concat":
		result.Kind = "string"
		var b strings.Builder
		for _, operand := range operands {
			if operand.Kind != "string" {
				return PreparedRequest{}, api.E("invalid_request", "compute_type_mismatch")
			}
			if b.Len()+len(operand.String) > 4096 {
				return PreparedRequest{}, api.E("overloaded", "compute_string_limit")
			}
			b.WriteString(operand.String)
		}
		result.String = b.String()
	case "not":
		if len(operands) != 1 || operands[0].Kind != "bool" || operands[0].Bool == nil {
			return PreparedRequest{}, api.E("invalid_request", "compute_type_mismatch")
		}
		result.Kind = "bool"
		v := !*operands[0].Bool
		result.Bool = &v
	case "copy":
		if len(operands) != 1 {
			return PreparedRequest{}, api.E("invalid_request", "compute_arity_mismatch")
		}
		result = operands[0]
		result.Name = program.TargetName
	default:
		return PreparedRequest{}, api.E("unsupported", "compute_opcode_not_supported")
	}
	values[program.TargetName] = result
	ns.Bindings = []NamespaceBinding{}
	for _, v := range values {
		ns.Bindings = append(ns.Bindings, v)
	}
	sort.Slice(ns.Bindings, func(i, j int) bool { return ns.Bindings[i].Name < ns.Bindings[j].Name })
	if err = validateNamespace(ns); err != nil {
		return PreparedRequest{}, err
	}
	limit, err := strconv.ParseUint(env.Limits[0].Value, 10, 64)
	if err != nil || uint64(len(api.Raw(ns))) > limit {
		return PreparedRequest{}, api.E("overloaded", "namespace_byte_limit")
	}
	sources := append([]api.ContentRef{}, env.ProcessedSources...)
	sources = appendUniqueSources(sources, args.CodeRef, args.InputRef, *env.NamespaceRef)
	for _, ref := range sources {
		found := false
		for _, allowed := range intent.ProcessedSourceRefs {
			found = found || api.Equal(ref, allowed)
		}
		if !found {
			return PreparedRequest{}, api.E("forbidden", "compute_source_not_in_intent")
		}
	}
	encoded := api.Raw(args)
	return PreparedRequest{Encoded: encoded, Digest: api.Hash(encoded), Cell: &CellPreparation{EnvironmentRef: args.EnvironmentRef, ExpectedGeneration: env.Generation, ExpectedNamespaceRevision: env.NamespaceRevision, Namespace: ns, Sources: sources}}, nil
}
func appendUniqueSources(sources []api.ContentRef, refs ...api.ContentRef) []api.ContentRef {
	for _, r := range refs {
		found := false
		for _, old := range sources {
			found = found || api.Equal(old, r)
		}
		if !found {
			sources = append(sources, r)
		}
	}
	return sources
}
func (d *TrustedComputeDriver) Start(ctx context.Context, q AttemptRequest, barrier func(context.Context) error) (Fact, error) {
	if q.Attempt.Prepared.Cell == nil {
		return Fact{}, api.E("invalid_state", "cell_preparation_missing")
	}
	if err := barrier(ctx); err != nil {
		return Fact{}, err
	}
	return Fact{Revision: 1, Effect: "applied", MayApplyLater: false, Evidence: []api.ContentRef{}, Usage: []api.Amount{}, UsageFinal: true}, nil
}
func (d *TrustedComputeDriver) Reconcile(ctx context.Context, q AttemptRequest) (Fact, error) {
	var op operationRecord
	if _, err := d.Store.Read(ctx, q.Scope, Namespace+".operations", q.Invoke.OperationID, 0, &op); err != nil {
		return Fact{}, err
	}
	if op.Operation.Effect == "applied" && q.Attempt.ResultRef != nil {
		raw, err := d.Content.ReadBytes(ctx, q.Scope, q.Auth, *q.Attempt.ResultRef, "execution_result", d.Location)
		if err != nil {
			return Fact{}, err
		}
		return Fact{Revision: q.Attempt.FactRevision + 1, Output: raw, Effect: "applied", MayApplyLater: false, Evidence: []api.ContentRef{}, Usage: []api.Amount{}, UsageFinal: true}, nil
	}
	return Fact{Revision: q.Attempt.FactRevision + 1, Effect: "not_applied", MayApplyLater: false, Evidence: []api.ContentRef{}, Usage: []api.Amount{}, UsageFinal: true}, nil
}
func (d *TrustedComputeDriver) Stop(ctx context.Context, q AttemptRequest) (StopFact, error) {
	return StopFact{ActuallyStopped: true, MayApplyLater: false}, nil
}

func (s *Service) cellBarrier(ctx context.Context, tx rt.Tx, op operationRecord, a Attempt, now time.Time) error {
	cell := a.Prepared.Cell
	if cell == nil {
		return nil
	}
	var env Environment
	rev, err := tx.Get(ctx, Namespace+".environments", cell.EnvironmentRef.ObjectID, &env)
	if err != nil {
		return err
	}
	if env.Generation != cell.ExpectedGeneration {
		return api.E("revision_conflict", "generation_changed")
	}
	if env.NamespaceRevision != cell.ExpectedNamespaceRevision {
		return api.E("revision_conflict", "namespace_changed")
	}
	if env.Phase != "active" || !env.ReadyForCell || !env.ActuallyExited || len(env.ActiveOperationIDs) > 0 || len(env.StopResiduals) > 0 {
		return api.E("invalid_state", "environment_busy")
	}
	if err = minDeadline(now, env.ExpiresAt); err != nil {
		return err
	}
	env.ReadyForCell = false
	env.ActuallyExited = false
	env.ActiveOperationIDs = append(env.ActiveOperationIDs, op.Operation.OperationID)
	env.Revision = rev + 1
	return tx.Put(ctx, Namespace+".environments", env.EnvironmentID, rev, env)
}
func (s *Service) commitCell(ctx context.Context, tx rt.Tx, op operationRecord, a Attempt, namespaceRef *api.ContentRef) (bool, error) {
	cell := a.Prepared.Cell
	if cell == nil {
		return true, nil
	}
	var env Environment
	rev, err := tx.Get(ctx, Namespace+".environments", cell.EnvironmentRef.ObjectID, &env)
	if err != nil {
		return false, err
	}
	valid := env.Generation == cell.ExpectedGeneration && env.NamespaceRevision == cell.ExpectedNamespaceRevision && env.Phase == "active"
	if valid && namespaceRef != nil {
		env.NamespaceRef = namespaceRef
		env.NamespaceRevision++
		env.ProcessedSources = cell.Sources
	}
	remaining := []string{}
	for _, id := range env.ActiveOperationIDs {
		if id != op.Operation.OperationID {
			remaining = append(remaining, id)
		}
	}
	env.ActiveOperationIDs = remaining
	env.ActuallyExited = len(remaining) == 0
	env.ReadyForCell = env.Phase == "active" && env.ActuallyExited && len(env.StopResiduals) == 0
	env.Revision = rev + 1
	return valid, tx.Put(ctx, Namespace+".environments", env.EnvironmentID, rev, env)
}
