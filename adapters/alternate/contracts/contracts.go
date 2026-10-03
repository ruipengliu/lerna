// Package contracts 冻结独立组件的有界线合同；公开签名仅含 api 类型。
package contracts

import (
	"sort"

	file "github.com/ruipengliu/lerna/adapters/execution"
	"github.com/ruipengliu/lerna/adapters/platform"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/brain"
	"github.com/ruipengliu/lerna/internal/execution"
	"github.com/ruipengliu/lerna/internal/governance"
	"github.com/ruipengliu/lerna/internal/memory"
)

type Manifest struct {
	Format         string                          `json:"format"`
	CoreSource     string                          `json:"core_source"`
	CoreDigest     string                          `json:"core_digest"`
	Methods        map[string][]api.MethodContract `json:"methods"`
	Schemas        map[string]api.Schema           `json:"schemas"`
	ReadCapability api.ComponentRef                `json:"read_capability"`
	RankingProfile api.ComponentRef                `json:"ranking_profile"`
	BrainProfile   api.ComponentRef                `json:"brain_profile"`
	AnswerSchema   api.ComponentRef                `json:"answer_schema"`
}

func Build() (Manifest, error) {
	m := Manifest{Format: "alternate-ts-local/1", CoreSource: string(api.CoreSchemaBytes()), CoreDigest: api.CoreDigest(), Methods: map[string][]api.MethodContract{}, Schemas: map[string]api.Schema{"proposal": brain.ProposalSchema(), "execution_intent": api.SchemaFor[execution.ExecutionIntent](), "query_spec": api.SchemaFor[memory.MemoryQuerySpec](), "file_read_input": api.SchemaFor[file.FileReadArguments](), "file_read_output": file.FileReadCapability().OutputSchema, "command": envelope(true), "query": envelope(false)}, ReadCapability: file.FileReadCapability().Ref, RankingProfile: memory.LiteralProfile()}
	m.Schemas["snapshot"] = api.SchemaFor[api.Snapshot]()
	m.Schemas["goal"] = brain.GoalSchema()
	m.Schemas["use_receipt"] = api.SchemaFor[governance.UseReceipt]()
	m.Schemas["proof_claims"] = api.SchemaFor[platform.ProofClaims]()
	m.BrainProfile = api.ComponentRef{ComponentID: "model_00000000000000000000000000000021", Version: "alternate-answer1", Digest: api.Hash([]byte("alternate-ts-answer1/jcs(snapshot.InputTokens=0,EncodedDigest='')+goal_bytes_base64/upper_bound/v1"))}
	goalDigest, err := api.Digest(m.Schemas["goal"])
	if err != nil {
		return Manifest{}, err
	}
	m.AnswerSchema = api.ComponentRef{ComponentID: "schema_00000000000000000000000000000021", Version: "1", Digest: goalDigest}
	m.Methods["brain"] = []api.MethodContract{
		api.Contract[brain.DecideInput, brain.Output]("brain.decide", "brain", "command", false, true),
		api.Contract[brain.CancelInput, brain.Output]("brain.cancel", "brain", "command", false, false),
		api.Contract[brain.GetInput, brain.View]("brain.get", "brain", "query", false, false),
	}
	m.Methods["memory"] = []api.MethodContract{
		api.Contract[memory.Policy, memory.PolicyOutput]("content.policy.install", "content", "command", false, false),
		api.Contract[memory.ReserveInput, memory.ReserveOutput]("content.upload_reserve", "content", "command", false, false),
		api.Contract[memory.PutInput, memory.PutOutput]("content.put", "content", "command", false, false),
		api.Contract[memory.CloseInput, memory.CloseOutput]("content.close", "content", "command", true, false),
		api.Contract[memory.GetContentInput, memory.GetContentOutput]("content.get", "content", "query", false, false),
		api.Contract[memory.TransferStatusInput, memory.TransferStatus]("content.transfer.read", "content", "query", false, false),
		api.Contract[memory.CreateInput, memory.MemoryOutput]("memory.create", "memory", "command", false, false),
		api.Contract[memory.ReplaceInput, memory.MemoryOutput]("memory.replace", "memory", "command", true, false),
		api.Contract[memory.DeleteInput, memory.MemoryOutput]("memory.delete", "memory", "command", true, false),
		api.Contract[memory.ReadMemoryInput, memory.MemoryRecord]("memory.read", "memory", "query", false, false),
		api.Contract[memory.ReadMemoryInput, memory.MemoryRecord]("memory.inspect", "memory", "query", false, false),
		api.Contract[memory.ReadMemoryInput, memory.MemoryRecord]("memory.cleanup.get", "memory", "query", false, false),
		api.Contract[memory.ListMemoryInput, api.Page[memory.MemoryRecord]]("memory.list", "memory", "query", false, false),
		api.Contract[memory.QueryInput, api.Page[memory.Match]]("memory.query", "memory", "query", false, false),
		api.Contract[struct{}, memory.IndexStatus]("memory.index.inspect", "memory", "query", false, false),
		api.Contract[memory.OpenViewInput, memory.ViewOutput]("memory.view.open", "memory", "command", false, false),
		api.Contract[memory.PullViewInput, memory.ViewPage]("memory.view.pull", "memory", "query", false, false),
		api.Contract[memory.AckViewInput, memory.AckViewOutput]("memory.view.ack", "memory", "command", false, false),
	}
	m.Methods["executor"] = []api.MethodContract{
		api.Contract[execution.InvokeInput, execution.OperationOutput]("execution.invoke", "execution", "command", false, false),
		api.Contract[execution.CancelInput, execution.OperationOutput]("execution.cancel", "execution", "command", false, false),
		api.Contract[execution.ControlInput, execution.ControlView]("execution.control", "execution", "command", false, false),
		api.Contract[execution.ReconcileInput, execution.OperationOutput]("execution.reconcile", "execution", "command", false, false),
		api.Contract[execution.OperationIDInput, execution.OperationView]("execution.get", "execution", "query", false, false),
		api.Contract[api.ListInput, api.Page[execution.OperationOutput]]("execution.list", "execution", "query", false, false),
		api.Contract[execution.ControlGetInput, execution.ControlView]("execution.control.get", "execution", "query", false, false),
		api.Contract[execution.OperationIDInput, api.UsageSnapshot]("execution.usage.get", "execution", "query", false, false),
	}
	for system, methods := range m.Methods {
		sort.Slice(methods, func(i, j int) bool { return methods[i].Name < methods[j].Name })
		for i := range methods {
			if system == "executor" {
				closeExecution(methods[i].InputSchema)
				closeExecution(methods[i].OutputSchema)
			}
			digest, err := api.Digest([]any{methods[i].InputSchema, methods[i].OutputSchema})
			if err != nil {
				return Manifest{}, err
			}
			methods[i].SchemaDigest = digest
		}
	}
	return m, nil
}

func envelope(command bool) api.Schema {
	s := api.SchemaFor[api.Query]()
	if command {
		s = api.SchemaFor[api.Command]()
	}
	p := s["properties"].(map[string]any)
	p["protocol"] = api.Schema{"const": api.Protocol}
	p["profile"] = api.Schema{"const": api.Profile}
	for _, name := range []string{"logical_service_id", "target_id", "command_id", "query_id"} {
		if _, ok := p[name]; ok {
			p[name] = api.Ref("Id")
		}
	}
	p["method"] = api.String()
	if command {
		p["expires_at"] = api.Ref("Time")
		p["expected_revision"] = api.Ref("Revision")
	}
	return s
}

func closeExecution(s api.Schema) {
	if p, ok := s["properties"].(map[string]any); ok {
		for name, value := range p {
			child, ok := value.(map[string]any)
			if !ok {
				continue
			}
			switch name {
			case "may_apply_later":
				p[name] = api.Schema{"oneOf": []any{api.Schema{"type": "boolean"}, api.Schema{"const": "unknown"}}}
			case "effect":
				p[name] = api.Enum("not_started", "not_applied", "applied", "unknown")
			case "execution_state":
				p[name] = api.Enum("accepted", "started", "closed")
			default:
				closeExecution(child)
			}
		}
	}
	if items, ok := s["items"].(map[string]any); ok {
		closeExecution(items)
	}
}
