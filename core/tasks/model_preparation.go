package tasks

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"github.com/ruipengliu/lerna/contracts/reasoner"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

type ModelContent interface {
	Read(context.Context, *v1.Caller, *v1.Ref) (*v1.Content, error)
	CheckUsable(context.Context, *v1.Caller, *v1.Ref) error
	PrepareDerivation(context.Context, *v1.Caller, *v1.PrepareDerivationCommand) (*v1.CommandReceipt, error)
	QueryDerivation(context.Context, *v1.Caller, *v1.Ref) (*v1.ContentDerivation, error)
	ReadDerivationInput(context.Context, *v1.Caller, *v1.ReadDerivationInputCommand) (*v1.Content, error)
	SealDerivation(context.Context, *v1.Caller, *v1.SealDerivationCommand) (*v1.CommandReceipt, error)
	CommitDerivation(context.Context, *v1.Caller, *v1.CommitDerivationCommand) (*v1.CommandReceipt, error)
	ProcessRegistrations(context.Context, *v1.Caller) error
}

type modelCallStore interface {
	LoadModelCallRef(context.Context, *v1.Ref) (*v1.ModelCall, error)
	SaveModelCall(context.Context, *v1.ModelCall) error
	LoadModelCall(context.Context, *v1.Ref, uint32) (*v1.ModelCall, error)
}

func (s *Service) WithModelContent(c ModelContent) *Service { s.modelContent = c; return s }

func modelReceipt(r *v1.CommandReceipt, e error) error {
	if e != nil {
		return e
	}
	if r == nil {
		return command.Fail("DEPENDENCY_UNAVAILABLE")
	}
	if r.Error != nil {
		return &command.Failure{Detail: r.Error}
	}
	if r.Decision != v1.Decision_DECISION_ACCEPTED {
		return command.Fail("DEPENDENCY_UNAVAILABLE")
	}
	return nil
}
func (s *Service) modelHeader(id, domain string) *v1.CommandHeader {
	return &v1.CommandHeader{Identity: &v1.CommandIdentity{UserId: s.user, IssuerId: "host", TargetDomainId: domain, CommandId: id}, ContractVersion: 1, FingerprintVersion: 1, SchemaId: "lerna.v1.AdmissionCommands"}
}

func (s *Service) currentModelClaim(ctx context.Context, caller *v1.Caller, r *v1.ProposalRequest, j *v1.Job) error {
	if caller.GetIssuerId() != "host" {
		return command.Fail("PERMISSION_DENIED")
	}
	if j == nil || j.Ref == nil || j.Ref.Name == nil || !proto.Equal(j.Ref.Name, r.JobRef.Name) {
		return command.Fail("STALE_CLAIM")
	}
	current, e := s.store.LoadJob(ctx, r.JobRef.Name)
	if e != nil {
		return e
	}
	if e := command.CheckSavedJobContract(current); e != nil {
		return e
	}
	now, e := s.store.ReadAuthorityTime(ctx)
	if e != nil {
		return e
	}
	if current == nil || current.State != "CLAIMED" || current.Module != "tasks" || current.JobType != "PROPOSE" || !proto.Equal(current.SpecificationRef, r.Ref) || !proto.Equal(current.Ref, j.Ref) || current.ProcessInstance != j.ProcessInstance || current.ClaimEpoch != j.ClaimEpoch || current.LeaseUntilUnixMs <= now {
		return command.Fail("STALE_CLAIM")
	}
	return nil
}
func (s *Service) currentModelRequest(ctx context.Context, caller *v1.Caller, r *v1.ProposalRequest) error {
	t, e := s.QueryTask(ctx, caller, r.TaskId)
	if e != nil {
		return e
	}
	p, e := s.store.LoadPlanning(ctx, r.TaskId)
	if e != nil {
		return e
	}
	snap, e := s.store.LoadSnapshot(ctx, r.SnapshotRef)
	if e != nil {
		return e
	}
	now, e := s.store.ReadAuthorityTime(ctx)
	if e != nil {
		return e
	}
	if t == nil || snap == nil || p.Snapshot == nil || !proto.Equal(p.Snapshot.RequestRef, r.Ref) || r.State == "STOPPED" || r.State == "REPORTED" || now >= snap.ExpiresAtUnixMs || t.PlanningGeneration != snap.PlanningGeneration {
		return command.Fail("STALE_PROPOSAL")
	}
	if t.ControlGeneration != snap.ControlGeneration {
		return command.Fail("STALE_GENERATION")
	}
	if t.InputVersion != snap.InputVersion {
		return command.Fail("STALE_INPUT")
	}
	if t.RequirementsVersion != snap.RequirementsVersion {
		return command.Fail("STALE_REQUIREMENT")
	}
	if t.ParentTaskRef != nil {
		return command.Fail("UNSUPPORTED_FEATURE")
	}
	if t.Lifecycle != v1.TaskLifecycle_TASK_LIFECYCLE_OPEN || t.Control != v1.TaskControl_TASK_CONTROL_ACTIVE || p.VerificationFreeze != 0 {
		return command.Fail("TASK_NOT_ACTIVE")
	}
	return nil
}

func normalizedModelSettings(m *v1.ModelSettings) (*v1.ModelSettings, error) {
	if m == nil {
		return nil, command.Fail("INVALID_MODEL_SETTINGS")
	}
	m = proto.Clone(m).(*v1.ModelSettings)
	if m.Provider != "reference" || m.Model == "" || m.EncoderVersion != "reference-model-v1" || m.PolicyVersion != "m1-v1" || m.MaxOutputTokens == 0 || m.MaxOutputTokens > 65536 {
		return nil, command.Fail("PREPARATION_UNRECOVERABLE")
	}
	if m.ViewPolicy != "" && (m.ViewPolicy != reasoner.ViewPolicy || m.MaxInputBytes == 0 || m.MaxInputBytes > 262144 || m.MaxInputTokens == 0 || m.MaxInputTokens > 65536) {
		return nil, command.Fail("INVALID_MODEL_SETTINGS")
	}
	parameters, err := decodeModelJSON(m.ParametersJson)
	if err != nil {
		return nil, command.Fail("INVALID_MODEL_SETTINGS")
	}
	if _, ok := parameters.(map[string]any); !ok {
		return nil, command.Fail("INVALID_MODEL_SETTINGS")
	}
	schemas, err := decodeModelJSON(m.ToolSchemasJson)
	if err != nil {
		return nil, command.Fail("INVALID_MODEL_SETTINGS")
	}
	if _, ok := schemas.([]any); !ok {
		return nil, command.Fail("INVALID_MODEL_SETTINGS")
	}
	m.ParametersJson, _ = json.Marshal(parameters)
	m.ToolSchemasJson, _ = json.Marshal(schemas)

	return m, nil
}
func sameRefs(a, b []*v1.Ref) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !proto.Equal(a[i], b[i]) {
			return false
		}
	}
	return true
}

// PrepareModelCall 从原快照和实际捕获的读取构造固定字节，不接受推理自报的摘要。
func (s *Service) PrepareModelCall(ctx context.Context, caller *v1.Caller, c *v1.PrepareModelCallCommand) (*v1.ModelCall, error) {
	if e := command.ValidateHeader(c.GetHeader(), c); e != nil {
		return nil, e
	}
	if caller.GetIssuerId() != "host" {
		return nil, command.Fail("PERMISSION_DENIED")
	}
	settings, e := normalizedModelSettings(c.Settings)
	if e != nil {
		return nil, e
	}
	receipt, e := s.traceModelDecisions(c.RequestRef).Execute(ctx, caller, c.Header, command.SemanticFingerprint("model-prepare", c.RequestRef, c.Position, settings, c.InputRefs, c.CapabilityRef), "tasks.model_prepare", func(tx context.Context) (*v1.Ref, error) {
		r, e := s.QueryProposalRequest(tx, caller, c.RequestRef)
		if e != nil {
			return nil, e
		}
		if r == nil {
			return nil, command.Fail("NOT_FOUND")
		}
		old, e := s.store.LoadModelCall(tx, r.Ref, c.Position)
		if e != nil {
			return nil, e
		}
		if old != nil {
			if e = checkSavedModelCall(old); e != nil {
				return nil, e
			}
			if !proto.Equal(old.Settings, settings) || !sameRefs(old.InputRefs, c.InputRefs) || !proto.Equal(old.CapabilityRef, c.CapabilityRef) {
				return nil, command.Fail("MODEL_POSITION_CONFLICT")
			}
			return old.Ref, nil
		}
		if e = s.currentModelClaim(tx, caller, r, c.Claim); e != nil {
			return nil, e
		}
		if e = s.currentModelRequest(tx, caller, r); e != nil {
			return nil, e
		}
		if c.Position >= r.MaxCallPositions {
			return nil, command.Fail("MODEL_CALL_LIMIT")
		}
		snap, e := s.store.LoadSnapshot(tx, r.SnapshotRef)
		if e != nil {
			return nil, e
		}
		cap, e := s.QueryCurrentCapability(tx, caller, c.CapabilityRef)
		if e != nil {
			return nil, e
		}
		if cap == nil || !proto.Equal(cap.Ref, c.CapabilityRef) || cap.Action != "MODEL_INFER" || cap.AdapterRef.GetName().GetLocalId() != "model-reference-v1" || cap.MaxSends != 1 {
			return nil, command.Fail("CAPABILITY_INVALID")
		}
		known := false
		for _, ref := range snap.CapabilityRefs {
			if proto.Equal(ref, cap.Ref) {
				known = true
			}
		}
		if !known {
			return nil, command.Fail("CAPABILITY_INVALID")
		}
		for _, required := range snap.ContentRefs {
			found := false
			for _, input := range c.InputRefs {
				if proto.Equal(required, input) {
					found = true
				}
			}
			if !found {
				return nil, command.Fail("MISSING_REQUIRED_INPUT")
			}
		}
		for _, input := range c.InputRefs {
			if e = s.content.CheckUsable(tx, caller, input); e != nil {
				return nil, e
			}
		}
		call := &v1.ModelCall{Ref: command.NewRef(s.user, s.domain, "model-call", "lerna.v1.ModelCall"), RequestRef: r.Ref, Position: c.Position, Settings: settings, InputRefs: c.InputRefs, CapabilityRef: c.CapabilityRef, State: "PREPARING"}
		return call.Ref, s.saveModelCall(tx, call)
	})
	if e = modelReceipt(receipt, e); e != nil {
		return nil, e
	}
	call, e := s.store.LoadModelCall(ctx, c.RequestRef, c.Position)
	if e != nil {
		return nil, e
	}
	if call.State != "PREPARING" {
		if e = s.modelContent.CheckUsable(ctx, caller, call.InputRef); e != nil {
			return nil, modelPreparationError(e)
		}
		return call, nil
	}
	r, e := s.QueryProposalRequest(ctx, caller, c.RequestRef)
	if e != nil {
		return nil, e
	}
	snap, e := s.store.LoadSnapshot(ctx, r.SnapshotRef)
	if e != nil {
		return nil, e
	}
	prefix := "model-input:" + call.Ref.Name.LocalId
	ch := func(suffix string) *v1.CommandHeader { return s.modelHeader(prefix+suffix, s.domain+"/content") }
	prepared, e := s.modelContent.PrepareDerivation(ctx, caller, &v1.PrepareDerivationCommand{Header: ch(":prepare"), TaskId: r.TaskId, GeneratorVersion: settings.EncoderVersion, OutputKind: "CONTEXT", MediaType: "application/json"})
	if e = modelReceipt(prepared, e); e != nil {
		return nil, e
	}
	d, e := s.modelContent.QueryDerivation(ctx, caller, prepared.ResultRef)
	if e != nil {
		return nil, e
	}
	inputs := make([]modelInput, 0, len(call.InputRefs))
	for i, input := range call.InputRefs {
		var body *v1.Content
		if d.State == "PREPARED" || d.State == "COMPUTING" {
			body, e = s.modelContent.ReadDerivationInput(ctx, caller, &v1.ReadDerivationInputCommand{Header: ch(fmt.Sprintf(":read:%d", i)), DerivationRef: d.Ref, HostInstanceId: d.HostInstanceId, Generation: d.Generation, InputRef: input})
		} else {
			body, e = s.modelContent.Read(ctx, caller, input)
		}
		if e != nil {
			return nil, modelPreparationError(e)
		}
		inputs = append(inputs, modelInput{Ref: input, Body: command.ContentBytes(body)})
	}
	body, e := s.encodeModelInput(ctx, caller, snap, settings, call.CapabilityRef, inputs)
	if e != nil {
		return nil, e
	}
	sealed, e := s.modelContent.SealDerivation(ctx, caller, &v1.SealDerivationCommand{Header: ch(":seal"), DerivationRef: d.Ref, HostInstanceId: d.HostInstanceId, Generation: d.Generation, InputRefs: call.InputRefs})
	if e = modelReceipt(sealed, e); e != nil {
		return nil, e
	}
	committed, e := s.modelContent.CommitDerivation(ctx, caller, &v1.CommitDerivationCommand{Header: ch(":commit"), DerivationRef: d.Ref, HostInstanceId: d.HostInstanceId, Generation: d.Generation, Body: body})
	if e = modelReceipt(committed, e); e != nil {
		return nil, e
	}
	if e = s.modelContent.ProcessRegistrations(ctx, caller); e != nil {
		return nil, e
	}
	h := sha256.Sum256(body)
	completed, e := s.traceModelDecisions(c.RequestRef).Execute(ctx, caller, s.modelHeader(fmt.Sprintf("%s:fixed:%s:%d", prefix, c.Claim.GetProcessInstance(), c.Claim.GetClaimEpoch()), s.domain), command.SemanticFingerprint("model-fixed", call.Ref, d.Ref, committed.ResultRef, hex.EncodeToString(h[:])), "tasks.model_seal", func(tx context.Context) (*v1.Ref, error) {
		current, e := s.store.LoadModelCall(tx, c.RequestRef, c.Position)
		if e != nil {
			return nil, e
		}
		if e = s.currentModelClaim(tx, caller, r, c.Claim); e != nil {
			return nil, e
		}
		if e = s.currentModelRequest(tx, caller, r); e != nil {
			return nil, e
		}
		current.DerivationRef = d.Ref
		current.InputRef = committed.ResultRef
		current.DescriptorDigest = hex.EncodeToString(h[:])
		current.State = "SEALED"
		return current.Ref, s.saveModelCall(tx, current)
	})
	if e = modelReceipt(completed, e); e != nil {
		return nil, e
	}
	return s.store.LoadModelCall(ctx, c.RequestRef, c.Position)
}

type modelInput struct {
	Ref  *v1.Ref `json:"ref"`
	Body []byte  `json:"body"`
}

func (s *Service) encodeModelInput(ctx context.Context, caller *v1.Caller, snap *v1.ContextSnapshot, settings *v1.ModelSettings, capability *v1.Ref, inputs []modelInput) ([]byte, error) {
	facts := []json.RawMessage{}
	add := func(m proto.Message) error {
		b, e := protojson.Marshal(m)
		if e == nil {
			facts = append(facts, b)
		}
		return e
	}
	if e := add(snap); e != nil {
		return nil, e
	}
	if snap.RequirementsRef != nil {
		r, e := s.store.LoadRequirements(ctx, snap.RequirementsRef)
		if e != nil {
			return nil, e
		}
		if r == nil {
			return nil, command.Fail("PREPARATION_UNRECOVERABLE")
		}
		if e = add(r); e != nil {
			return nil, e
		}
	}
	for _, ref := range snap.CapabilityRefs {
		cap, e := s.QueryCapability(ctx, caller, ref)
		if e != nil {
			return nil, e
		}
		if cap == nil {
			return nil, command.Fail("PREPARATION_UNRECOVERABLE")
		}
		if e = add(cap); e != nil {
			return nil, e
		}
	}
	for _, ref := range snap.ProgressRefs {
		a, e := s.store.LoadAdmission(ctx, ref)
		if e != nil {
			return nil, e
		}
		if a == nil {
			return nil, command.Fail("PREPARATION_UNRECOVERABLE")
		}
		if e = add(a); e != nil {
			return nil, e
		}
	}
	value := struct {
		Settings   *v1.ModelSettings `json:"settings"`
		Capability *v1.Ref           `json:"capability"`
		Facts      []json.RawMessage `json:"facts"`
		Inputs     []modelInput      `json:"inputs"`
	}{settings, capability, facts, inputs}
	if settings.ViewPolicy == reasoner.ViewPolicy {
		fixed, e := json.Marshal(struct {
			Settings   *v1.ModelSettings `json:"settings"`
			Capability *v1.Ref           `json:"capability"`
			Facts      []json.RawMessage `json:"facts"`
		}{settings, capability, facts})
		if e != nil {
			return nil, e
		}
		materials := make([]reasoner.ViewMaterial, 0, len(inputs))
		for _, input := range inputs {
			required := false
			for _, ref := range snap.ContentRefs {
				if proto.Equal(ref, input.Ref) {
					required = true
					break
				}
			}
			materials = append(materials, reasoner.ViewMaterial{Ref: input.Ref, Body: input.Body, Required: required})
		}
		body, e := reasoner.BuildView(fixed, materials, settings.MaxInputBytes, settings.MaxInputTokens)
		if e != nil {
			return nil, e
		}
		canonical, e := decodeModelJSON(body)
		if e != nil {
			return nil, e
		}
		return json.Marshal(canonical)
	}
	b, e := json.Marshal(value)
	if e != nil {
		return nil, e
	}
	// 对对象键排序，规范化 Protobuf JSON 中允许变化的空白。
	canonical, e := decodeModelJSON(b)
	if e != nil {
		return nil, e
	}
	return json.Marshal(canonical)
}

// decodeModelJSON 保留数字的十进制精度，并拒绝对象重复字段。
func decodeModelJSON(body []byte) (any, error) {
	d := json.NewDecoder(bytes.NewReader(body))
	d.UseNumber()
	var value func() (any, error)
	value = func() (any, error) {
		token, e := d.Token()
		if e != nil {
			return nil, e
		}
		delimiter, ok := token.(json.Delim)
		if !ok {
			return token, nil
		}
		switch delimiter {
		case '{':
			object := map[string]any{}
			for d.More() {
				k, e := d.Token()
				if e != nil {
					return nil, e
				}
				key, ok := k.(string)
				if !ok {
					return nil, command.Fail("INVALID_MODEL_SETTINGS")
				}
				if _, exists := object[key]; exists {
					return nil, command.Fail("INVALID_MODEL_SETTINGS")
				}
				v, e := value()
				if e != nil {
					return nil, e
				}
				object[key] = v
			}
			end, e := d.Token()
			if e != nil || end != json.Delim('}') {
				return nil, command.Fail("INVALID_MODEL_SETTINGS")
			}
			return object, nil
		case '[':
			array := []any{}
			for d.More() {
				v, e := value()
				if e != nil {
					return nil, e
				}
				array = append(array, v)
			}
			end, e := d.Token()
			if e != nil || end != json.Delim(']') {
				return nil, command.Fail("INVALID_MODEL_SETTINGS")
			}
			return array, nil
		default:
			return nil, command.Fail("INVALID_MODEL_SETTINGS")
		}
	}
	v, e := value()
	if e != nil {
		return nil, e
	}
	if _, e = d.Token(); e != io.EOF {
		return nil, command.Fail("INVALID_MODEL_SETTINGS")
	}
	return v, nil
}

// modelPreparationError 只有内容永久不可恢复才终止准备；存储回执不确定仍按原命令恢复。
func modelPreparationError(err error) error {
	var failure *command.Failure
	if err != nil && !errors.As(err, &failure) {
		return err
	}
	if failure != nil && (failure.Detail.Category == v1.ErrorCategory_ERROR_CATEGORY_TRANSIENT || failure.Detail.Category == v1.ErrorCategory_ERROR_CATEGORY_INDETERMINATE) {
		return err
	}
	return command.Fail("PREPARATION_UNRECOVERABLE")
}
