package execution

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/ruipengliu/lerna/api"
	domain "github.com/ruipengliu/lerna/internal/execution"
	rt "github.com/ruipengliu/lerna/runtime"
)

// SimulatedPhones 每部手机拥有独立耐久状态。它只证明模拟目标合同，不声称 Android/iOS 支持。
type SimulatedPhones struct {
	root      *os.Root
	ownerLock *os.File
	mu        sync.Mutex
	phones    map[string]*simulatedPhone
	Fault     func(string, string) error
}
type PhoneState struct {
	Screen  string `json:"screen"`
	Note    string `json:"note"`
	WiFi    bool   `json:"wifi"`
	Version uint64 `json:"version"`
}
type PhoneActionArguments struct {
	ResourceID    string `json:"resource_id"`
	InstanceID    string `json:"instance_id"`
	ControlEpoch  uint64 `json:"control_epoch"`
	ObservationID string `json:"observation_id"`
	TargetVersion string `json:"target_version"`
	ActionBefore  string `json:"action_before"`
	Action        string `json:"action"`
	Value         string `json:"value"`
}
type PhoneActionResult struct {
	ResourceID        string `json:"resource_id"`
	OriginalAttemptID string `json:"original_attempt_id"`
	TargetVersion     string `json:"target_version"`
	Applied           bool   `json:"applied"`
	Atomic            bool   `json:"atomic"`
}
type phoneAttempt struct {
	AttemptID   string            `json:"attempt_id"`
	OperationID string            `json:"operation_id"`
	InputDigest string            `json:"input_digest"`
	Result      PhoneActionResult `json:"result"`
}
type simulatedPhone struct {
	ResourceID   string         `json:"resource_id"`
	TenantID     string         `json:"tenant_id,omitempty"`
	InstanceID   string         `json:"instance_id,omitempty"`
	ControlEpoch uint64         `json:"control_epoch"`
	Automatic    bool           `json:"automatic"`
	State        PhoneState     `json:"state"`
	Attempts     []phoneAttempt `json:"attempts"`
}

func NewSimulatedPhones(root string, ids []string) (*SimulatedPhones, error) {
	r, err := os.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	s := &SimulatedPhones{root: r, phones: map[string]*simulatedPhone{}}
	lock, err := r.OpenFile("owner.lock", os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		r.Close()
		return nil, err
	}
	if _, err = fileIdentity(lock); err == nil {
		err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	}
	if err != nil {
		lock.Close()
		r.Close()
		return nil, api.E("invalid_state", "device_host_already_active")
	}
	s.ownerLock = lock
	for _, id := range ids {
		if !api.ValidID(id) {
			s.Close()
			return nil, api.E("invalid_request", "invalid_resource_id")
		}
		if len(s.phones) >= 100 {
			s.Close()
			return nil, api.E("overloaded", "device_limit")
		}
		p := &simulatedPhone{ResourceID: id, State: PhoneState{Screen: "home", Version: 1}, Attempts: []phoneAttempt{}}
		f, err := r.OpenFile(id+".json", os.O_RDONLY|syscall.O_NOFOLLOW, 0)
		if err == nil {
			if _, err = fileIdentity(f); err == nil {
				var b []byte
				b, err = io.ReadAll(io.LimitReader(f, 8<<20))
				if err == nil {
					err = api.Decode(b, p)
				}
			}
			f.Close()
		}
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			s.Close()
			return nil, err
		}
		s.phones[id] = p
		if err = s.save(p); err != nil {
			s.Close()
			return nil, err
		}
	}
	return s, nil
}
func (s *SimulatedPhones) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var err error
	if s.ownerLock != nil {
		err = s.ownerLock.Close()
		s.ownerLock = nil
	}
	return errors.Join(err, s.root.Close())
}
func (s *SimulatedPhones) save(p *simulatedPhone) error {
	b, err := json.Marshal(p)
	if err != nil {
		return err
	}
	name := p.ResourceID + ".json"
	f, err := s.root.OpenFile(name+".next", os.O_CREATE|os.O_TRUNC|os.O_WRONLY|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return err
	}
	if _, err = fileIdentity(f); err == nil {
		_, err = f.Write(b)
	}
	if err == nil {
		err = f.Sync()
	}
	e := f.Close()
	if err == nil {
		err = e
	}
	if err != nil {
		return err
	}
	if err = s.root.Rename(name+".next", name); err != nil {
		return err
	}
	dir, err := s.root.Open(".")
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
func (s *SimulatedPhones) phone(sc rt.Scope, id string) (*simulatedPhone, error) {
	p := s.phones[id]
	if p == nil {
		return nil, api.E("unsupported", "device_not_installed")
	}
	if p.TenantID != "" && p.TenantID != sc.TenantID {
		return nil, api.E("forbidden", "device_tenant_mismatch")
	}
	return p, nil
}
func PhoneCapability() domain.Capability {
	input := api.SchemaFor[PhoneActionArguments]()
	input["properties"].(map[string]any)["action"] = api.Enum("set_note", "open_notes", "press_home", "set_wifi")
	output := api.SchemaFor[PhoneActionResult]()
	digest, _ := api.Digest([]any{"simulated_phone.action", "1", input, output, "atomic"})
	return domain.Capability{Ref: api.ComponentRef{ComponentID: domain.BuiltinComponentID("simulated_phone.action"), Version: "1", Digest: digest}, EffectClass: "no_idempotency_guarantee", MaxAttempts: 1, InputSchema: input, OutputSchema: output}
}
func (s *SimulatedPhones) Capability() domain.Capability { return PhoneCapability() }
func (s *SimulatedPhones) Fence(ctx context.Context, sc rt.Scope, lease domain.ResourceLease) (domain.StopFact, error) {
	if err := ctx.Err(); err != nil {
		return domain.StopFact{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	p, err := s.phone(sc, lease.ResourceID)
	if err != nil {
		return domain.StopFact{}, err
	}
	if lease.ControlEpoch < p.ControlEpoch {
		return domain.StopFact{}, api.E("revision_conflict", "resource_epoch_changed")
	}
	if lease.ControlEpoch == p.ControlEpoch && p.InstanceID != "" && p.InstanceID != lease.InstanceID {
		return domain.StopFact{}, api.E("idempotency_conflict", "device_instance_changed")
	}
	p.TenantID = sc.TenantID
	p.ControlEpoch = lease.ControlEpoch
	p.InstanceID = lease.InstanceID
	p.Automatic = lease.State == "held" && !lease.TakeoverRequested
	if err = s.save(p); err != nil {
		return domain.StopFact{}, err
	}
	return domain.StopFact{ActuallyStopped: true, MayApplyLater: false}, nil
}
func (s *SimulatedPhones) Observe(ctx context.Context, sc rt.Scope, lease domain.ResourceLease, id string) (domain.Observation, error) {
	if err := ctx.Err(); err != nil {
		return domain.Observation{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	p, err := s.phone(sc, lease.ResourceID)
	if err != nil {
		return domain.Observation{}, err
	}
	if p.ControlEpoch != lease.ControlEpoch || p.InstanceID != lease.InstanceID {
		return domain.Observation{}, api.E("revision_conflict", "resource_epoch_changed")
	}
	now := time.Now().UTC()
	return domain.Observation{ObservationID: id, ResourceRef: sc.Ref(p.ResourceID, lease.Revision), InstanceID: p.InstanceID, ControlEpoch: p.ControlEpoch, ContentRefs: []api.ContentRef{}, TargetVersion: strconv.FormatUint(p.State.Version, 10), ObservedAt: api.Time(now), ActionBefore: api.Time(now.Add(30 * time.Second)), Data: api.Raw(p.State)}, nil
}
func (s *SimulatedPhones) Prepare(ctx context.Context, sc rt.Scope, a rt.Auth, invoke domain.InvokeInput, i domain.ExecutionIntent, b []byte) (domain.PreparedRequest, error) {
	var p PhoneActionArguments
	if err := api.Decode(b, &p); err != nil {
		return domain.PreparedRequest{}, err
	}
	if !api.ValidID(p.ResourceID) || !api.ValidID(p.InstanceID) || !api.ValidID(p.ObservationID) || p.ControlEpoch == 0 || len(p.Value) > 4096 {
		return domain.PreparedRequest{}, api.E("invalid_request", "invalid_device_arguments")
	}
	switch p.Action {
	case "set_note", "open_notes", "press_home", "set_wifi":
	default:
		return domain.PreparedRequest{}, api.E("unsupported", "device_action_not_supported")
	}
	if _, err := api.ParseTime(p.ActionBefore); err != nil {
		return domain.PreparedRequest{}, api.E("invalid_request", "invalid_observation_window")
	}
	raw := api.Raw(p)
	return domain.PreparedRequest{Encoded: raw, Digest: api.Hash(raw), ResourceID: p.ResourceID, ResourceEpoch: p.ControlEpoch, ObservationID: p.ObservationID, ObservationBefore: p.ActionBefore}, nil
}
func (s *SimulatedPhones) Start(ctx context.Context, q domain.AttemptRequest, barrier func(context.Context) error) (domain.Fact, error) {
	if err := ctx.Err(); err != nil {
		return domain.Fact{}, err
	}
	var a PhoneActionArguments
	if err := api.Decode(q.Attempt.Prepared.Encoded, &a); err != nil {
		return domain.Fact{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	p, err := s.phone(q.Scope, a.ResourceID)
	if err != nil {
		return domain.Fact{}, err
	}
	for _, old := range p.Attempts {
		if old.AttemptID == q.Attempt.AttemptID {
			if old.OperationID != q.Invoke.OperationID || old.InputDigest != q.Attempt.Prepared.Digest {
				return domain.Fact{}, api.E("idempotency_conflict", "device_attempt_changed")
			}
			return phoneFact(old.Result, q.Attempt.FactRevision), nil
		}
	}
	if !p.Automatic || p.ControlEpoch != a.ControlEpoch || p.InstanceID != a.InstanceID {
		return domain.Fact{}, api.E("revision_conflict", "resource_epoch_changed")
	}
	if strconv.FormatUint(p.State.Version, 10) != a.TargetVersion {
		return domain.Fact{}, api.E("revision_conflict", "observation_stale")
	}
	until, err := api.ParseTime(a.ActionBefore)
	if err != nil || !time.Now().UTC().Before(until) {
		return domain.Fact{}, api.E("expired", "observation_expired")
	}
	if len(p.Attempts) >= 10000 {
		return domain.Fact{}, api.E("overloaded", "device_retention_capacity")
	}
	if err = barrier(ctx); err != nil {
		return domain.Fact{}, err
	}
	next := *p
	next.Attempts = append([]phoneAttempt{}, p.Attempts...)
	switch a.Action {
	case "set_note":
		next.State.Screen = "notes"
		next.State.Note = a.Value
	case "open_notes":
		next.State.Screen = "notes"
	case "press_home":
		next.State.Screen = "home"
	case "set_wifi":
		if a.Value != "true" && a.Value != "false" {
			return domain.Fact{}, api.E("invalid_request", "wifi_requires_boolean")
		}
		next.State.WiFi = a.Value == "true"
	}
	next.State.Version++
	result := PhoneActionResult{ResourceID: p.ResourceID, OriginalAttemptID: q.Attempt.AttemptID, TargetVersion: strconv.FormatUint(next.State.Version, 10), Applied: true, Atomic: true}
	next.Attempts = append(next.Attempts, phoneAttempt{AttemptID: q.Attempt.AttemptID, OperationID: q.Invoke.OperationID, InputDigest: q.Attempt.Prepared.Digest, Result: result})
	if err = s.save(&next); err != nil {
		return domain.Fact{}, err
	}
	*p = next
	if s.Fault != nil {
		if err = s.Fault(p.ResourceID, "applied"); err != nil {
			return domain.Fact{}, err
		}
	}
	return phoneFact(result, q.Attempt.FactRevision), nil
}
func phoneFact(r PhoneActionResult, prior uint64) domain.Fact {
	n := uint64(1)
	if prior > 0 {
		n = prior + 1
	}
	return domain.Fact{Revision: n, Effect: "applied", MayApplyLater: false, Output: api.Raw(r), MediaType: "application/json", Evidence: []api.ContentRef{}, Usage: []api.Amount{}, UsageFinal: true}
}
func (s *SimulatedPhones) Reconcile(ctx context.Context, q domain.AttemptRequest) (domain.Fact, error) {
	var args PhoneActionArguments
	if err := api.Decode(q.Attempt.Prepared.Encoded, &args); err != nil {
		return domain.Fact{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	p, err := s.phone(q.Scope, args.ResourceID)
	if err != nil {
		return domain.Fact{}, err
	}
	for _, a := range p.Attempts {
		if a.AttemptID == q.Attempt.AttemptID {
			if a.OperationID != q.Invoke.OperationID {
				return domain.Fact{}, api.E("idempotency_conflict", "device_attempt_changed")
			}
			return phoneFact(a.Result, q.Attempt.FactRevision), nil
		}
	}
	return domain.Fact{Revision: q.Attempt.FactRevision + 1, Effect: "not_applied", MayApplyLater: false, Usage: []api.Amount{}, UsageFinal: true, Evidence: []api.ContentRef{}}, nil
}
func (s *SimulatedPhones) Stop(ctx context.Context, q domain.AttemptRequest) (domain.StopFact, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return domain.StopFact{ActuallyStopped: true, MayApplyLater: false}, nil
}

// HumanChange 是独立目标主体，不走 Agent 入口，用于观察竞争验收。
func (s *SimulatedPhones) HumanChange(ctx context.Context, resourceID, screen string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.phones[resourceID]
	if p == nil {
		return fmt.Errorf("unknown device")
	}
	p.State.Screen = screen
	p.State.Version++
	return s.save(p)
}

var _ domain.Driver = (*SimulatedPhones)(nil)
var _ domain.ResourceDriver = (*SimulatedPhones)(nil)

func (s *SimulatedPhones) ObservationSchema() api.Schema { return api.SchemaFor[PhoneState]() }
