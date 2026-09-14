package execution

import (
	"bytes"
	"context"
	"encoding/gob"
	"lerna/authorization"
	wire "lerna/gen/harness/v1"
	"lerna/schema"
	"lerna/tasks"
	"reflect"
	"sort"
)

type Service struct {
	offline    *offlineBinding
	authority  Authority
	core       Core
	content    Content
	driver     Driver
	binding    Binding
	cap        Capability
	config     Config
	schemas    *schema.Registry
	identity   func(context.Context) (string, error)
	slots      chan struct{}
	resource   *controlBinding
	startGuard StartGuard
}

// BindCore replaces only the trusted consumer binding for a recovered host
// incarnation. Durable invocation payloads, grants and driver state are unchanged.
func (s *Service) BindCore(core Core) (*Service, error) {
	if core == nil {
		return nil, failure(authorization.Invalid)
	}
	copy := *s
	copy.core = core
	return &copy, nil
}

type journal struct {
	Shared          *resourceJournal
	RecoveryAfter   string
	Cancels         map[string]CancelRecord
	ReservedCancels map[string]string
	Format          int
	Config          Config
	Descriptor      string
	Records         map[string]Record
	Active          string
	Commits         map[string]Receipt
}

func New(a Authority, core Core, content Content, driver Driver, b Binding, cap Capability, c Config, identity func(context.Context) (string, error)) (*Service, error) {
	if a == nil || core == nil || content == nil || driver == nil || identity == nil || !c.valid() || b.Token == "" || b.Subject == "" || b.Namespace == "" || b.Audience == "" || b.Worker == "" || b.Presenter == "" || b.CertificateSHA256 == "" || cap.Resource == "" || cap.Purpose == "" || cap.Location == "" || cap.Name == "" || cap.Version == "" || cap.Implementation == "" || cap.ImplementationVersion == "" || !cap.Exclusive || len(cap.Input.Document) > 16384 || len(cap.Output.Document) > 16384 {
		return nil, failure(authorization.Invalid)
	}
	if cap.Location != "local" {
		return nil, failure(authorization.Unsupported)
	}
	registry, e := schema.New([]schema.Resource{cap.Input, cap.Output})
	if e != nil {
		return nil, failure(authorization.Invalid)
	}
	if cap.Async != nil {
		policy := *cap.Async
		cap.Async = &policy
	}
	cap.Input.Document = append([]byte(nil), cap.Input.Document...)
	cap.Output.Document = append([]byte(nil), cap.Output.Document...)
	svc := &Service{authority: a, core: core, content: content, driver: driver, binding: b, cap: cap, config: c, schemas: registry, identity: identity, slots: make(chan struct{}, 1)}
	if !svc.asyncPolicyValid() {
		return nil, failure(authorization.Unsupported)
	}
	return svc, nil
}
func (s *Service) transaction(ctx context.Context, fn func(*journal, authorization.ExecutionTransaction) error) error {
	bounded, cancel := context.WithTimeout(ctx, s.config.IOTimeout)
	defer cancel()
	return s.authority.UpdateExecution(bounded, func(tx authorization.ExecutionTransaction) error {
		if tx.Namespace() != s.binding.Namespace {
			return failure(authorization.Denied)
		}
		store, e := decodeStorage(tx.ExecutionData())
		if e != nil {
			return e
		}
		if len(store.Partitions) >= 32 {
			if _, ok := store.Partitions[s.cap.Digest()]; !ok {
				return failure(authorization.Unavailable)
			}
		}
		j := journal{Format: 1, Config: s.config, Descriptor: s.cap.Digest(), Records: map[string]Record{}, Commits: map[string]Receipt{}}
		if decoded, ok := store.decoded[s.cap.Digest()]; ok {
			j = decoded
		}
		if j.Cancels == nil {
			j.Cancels = map[string]CancelRecord{}
		}
		if j.ReservedCancels == nil {
			j.ReservedCancels = map[string]string{}
		}
		if j.Format != 1 || j.Config != s.config || j.Descriptor != s.cap.Digest() {
			return failure(authorization.Unsupported)
		}
		j.Shared = &store.Resources
		if e := s.prepareResource(&j, tx); e != nil {
			return e
		}
		if e := fn(&j, tx); e != nil {
			return e
		}
		s.indexResource(&j)
		j.Shared = nil
		var b bytes.Buffer
		if e := gob.NewEncoder(&b).Encode(j); e != nil {
			return failure(authorization.Invalid)
		}
		if b.Len() > 4<<20 {
			return failure(authorization.Unavailable)
		}
		store.Partitions[s.cap.Digest()] = b.Bytes()
		encoded, e := encodeStorage(store)
		if e != nil {
			return e
		}
		tx.SetExecutionData(encoded)
		return nil
	})
}
func (s *Service) action(kind string) *wire.AuthorizationAction {
	return &wire.AuthorizationAction{Resource: s.cap.Resource, Action: kind, Purpose: s.cap.Purpose, Location: s.cap.Location}
}
func (s *Service) authorize(tx authorization.ExecutionTransaction, kind string) error {
	id, e := tx.AuthorizeAction(s.binding.Token, s.action(kind))
	if e != nil {
		return e
	}
	if id.Subject != s.binding.Subject || id.Namespace != s.binding.Namespace {
		return failure(authorization.Denied)
	}
	return nil
}
func (s *Service) identityBinding() Binding { b := s.binding; b.Token = ""; return b }
func (s *Service) owned(r Record) bool      { return r.Binding == s.identityBinding() }
func (s *Service) valid(r Request) bool {
	return len(r.OperationID) > 0 && len(r.OperationID) <= 512 && r.Qualification.Ref.Namespace == s.binding.Namespace && r.Qualification.Version > 0 && len(r.InputRef) > 0 && len(r.InputRef) <= 128 && r.Capability == s.cap.Name && r.Version == s.cap.Version && r.Implementation == s.cap.Implementation && r.ImplementationVersion == s.cap.ImplementationVersion && r.DescriptorSHA256 == s.cap.Digest() && r.ResourceVersion > 0
}
func (s *Service) validate(data []byte, r schema.Resource, max int) error {
	if len(data) == 0 || len(data) > max {
		return failure(authorization.Invalid)
	}
	if e := s.schemas.Validate(&wire.DynamicPayload{TypeName: r.Type, SchemaId: r.ID, SchemaVersion: r.Version, SchemaDigest: schema.Digest(r.Document), Json: data}); e != nil {
		return failure(authorization.Invalid)
	}
	return nil
}
func (s *Service) admission(j *journal, tx authorization.ExecutionTransaction, in Request) (*Receipt, error) {
	if e := s.authorize(tx, "capability.invoke"); e != nil {
		return nil, e
	}
	if e := tx.ExecutionOperation(in.OperationID, s.binding.Subject, false); e != nil {
		return nil, e
	}
	if _, ok := j.Shared.CancelOwners[in.OperationID]; ok {
		return nil, failure(authorization.IdentityConflict)
	}
	if _, ok := j.Shared.Operations[in.OperationID]; ok {
		return nil, failure(authorization.IdentityConflict)
	}
	if metadata, ok := j.Shared.Invocations[in.OperationID]; ok && metadata.Descriptor != s.cap.Digest() {
		return nil, failure(authorization.IdentityConflict)
	}
	if _, ok := j.Cancels[in.OperationID]; ok {
		return nil, failure(authorization.IdentityConflict)
	}
	if _, ok := j.ReservedCancels[in.OperationID]; ok {
		return nil, failure(authorization.IdentityConflict)
	}
	if old, ok := j.Records[in.OperationID]; ok {
		if !s.owned(old) {
			return nil, failure(authorization.Denied)
		}
		if !reflect.DeepEqual(old.Request, in) {
			return nil, failure(authorization.IdentityConflict)
		}
		if e := s.authorize(tx, "capability.read"); e != nil {
			return nil, e
		}
		r := old.Receipt
		return &r, nil
	}
	if e := s.guardResource(j, in); e != nil {
		return nil, e
	}
	if len(j.Records) >= s.config.MaxOperations {
		return nil, failure(authorization.Unavailable)
	}
	if e := s.core.GuardExecution(tx, in.Qualification, in.OperationID, false); e != nil {
		return nil, e
	}
	if e := s.guardActionRequest(tx, in); e != nil {
		return nil, e
	}
	return nil, nil
}

func (s *Service) guardActionRequest(tx authorization.RuntimeTransaction, in Request) error {
	if gate, ok := s.core.(interface {
		GuardActionRequest(authorization.RuntimeTransaction, tasks.ActionBinding) error
	}); ok {
		return gate.GuardActionRequest(tx, tasks.ActionBinding{Qualification: in.Qualification, OperationID: in.OperationID, Descriptor: in.DescriptorSHA256, InputRef: in.InputRef, ResourceVersion: in.ResourceVersion, ControlVersion: in.ControlVersion})
	}
	return nil
}
func (s *Service) Invoke(ctx context.Context, in Request, material string) (Receipt, error) {
	if !s.valid(in) || len(material) > 32768 {
		return Receipt{}, failure(authorization.Invalid)
	}
	var replay *Receipt
	e := s.transaction(ctx, func(j *journal, tx authorization.ExecutionTransaction) error {
		var e error
		replay, e = s.admission(j, tx, in)
		return e
	})
	if e != nil {
		return Receipt{}, e
	}
	if replay != nil {
		return *replay, nil
	}
	bounded, cancel := context.WithTimeout(ctx, s.config.IOTimeout)
	defer cancel()
	data, e := s.readContent(bounded, in)
	if e != nil {
		return Receipt{}, e
	}
	if e = s.validate(data, s.cap.Input, s.config.MaxInput); e != nil {
		return Receipt{}, e
	}
	// A process may stop after allocating a permit but before recording the
	// Invocation. Recover that exact allocation; ValidateUse below rechecks the
	// original grant and current policy before admitting any effect.
	permit, e := s.authority.LookupUse(bounded, s.binding.Presentation(in))
	if authorization.Is(e, authorization.NotFound) {
		permit, e = s.authority.ReserveUse(bounded, material, s.binding.Presentation(in), s.action("resource.change"), 1)
	}
	if e != nil {
		return Receipt{}, e
	}
	output, e := s.identity(bounded)
	if e != nil {
		return Receipt{}, e
	}
	taskCancel := ""
	if s.cap.Async != nil {
		taskCancel, e = s.identity(bounded)
		if e != nil {
			return Receipt{}, e
		}
	}
	var out Receipt
	e = s.transaction(bounded, func(j *journal, tx authorization.ExecutionTransaction) error {
		old, e := s.admission(j, tx, in)
		if e != nil {
			return e
		}
		if old != nil {
			out = *old
			return nil
		}
		if e = tx.ValidateUse(permit, s.binding.Presentation(in), s.action("resource.change")); e != nil {
			return e
		}
		if e = tx.ExecutionOperation(in.OperationID, s.binding.Subject, true); e != nil {
			return e
		}
		if taskCancel != "" {
			if e = tx.ExecutionControlOperation(taskCancel, s.binding.Subject); e != nil {
				return e
			}
			j.ReservedCancels[taskCancel] = in.OperationID
		}
		out = Receipt{in.OperationID, 1}
		j.Records[in.OperationID] = Record{TaskCancelOperation: taskCancel, Request: in, Binding: s.identityBinding(), Permit: permit, Receipt: out, OutputOperation: output, Revision: 1, Phase: "NOT_STARTED", Result: "UNKNOWN", Effect: "UNKNOWN"}
		j.Commits[in.OperationID+"/admit"] = out
		return nil
	})
	return out, e
}
func (s *Service) GetInvocation(ctx context.Context, op string) (Record, error) {
	var out Record
	e := s.transaction(ctx, func(j *journal, tx authorization.ExecutionTransaction) error {
		if e := s.authorize(tx, "capability.read"); e != nil {
			return e
		}
		r, ok := j.Records[op]
		if !ok || !s.owned(r) {
			return failure(authorization.Denied)
		}
		if e := tx.ExecutionOperation(op, s.binding.Subject, false); e != nil {
			return e
		}
		out = r
		return nil
	})
	return out, e
}
func (s *Service) LookupCommit(ctx context.Context, op, stage string) (Receipt, error) {
	if stage != "admit" && stage != "start" {
		return Receipt{}, failure(authorization.Invalid)
	}
	var out Receipt
	e := s.transaction(ctx, func(j *journal, tx authorization.ExecutionTransaction) error {
		if e := s.authorize(tx, "capability.read"); e != nil {
			return e
		}
		r, ok := j.Records[op]
		if !ok || !s.owned(r) {
			return failure(authorization.Denied)
		}
		v, ok := j.Commits[op+"/"+stage]
		if !ok {
			return failure(authorization.NotFound)
		}
		out = v
		return nil
	})
	return out, e
}

// ListRecoverable is bounded and stable over operation identities, not leases.
func (s *Service) ListRecoverable(ctx context.Context, after string, limit int) ([]Record, error) {
	if limit < 1 || limit > 16 || len(after) > 512 {
		return nil, failure(authorization.Invalid)
	}
	out := []Record{}
	e := s.transaction(ctx, func(j *journal, tx authorization.ExecutionTransaction) error {
		out = nil
		if e := s.authorize(tx, "capability.read"); e != nil {
			return e
		}
		keys := []string{}
		for k, r := range j.Records {
			if k > after && s.owned(r) && (r.Effect == "UNKNOWN" || r.Applied < r.Revision) {
				keys = append(keys, k)
			}
		}
		sort.Strings(keys)
		for _, k := range keys[:min(limit, len(keys))] {
			out = append(out, j.Records[k])
		}
		return nil
	})
	return out, e
}
