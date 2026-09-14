package tasks

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"lerna/authorization"
	"reflect"
	"time"
)

// HandoffConfig is trusted host assembly. Isolated must verify that every old
// execution endpoint can no longer start new actions, including offline grants.
// Policy checks current authority, residency and recoverability at each boundary.
type HandoffConfig struct {
	PrivateKey ed25519.PrivateKey
	Peers      map[string]ed25519.PublicKey
	Policy     func(context.Context, string, RunSnapshot, string) error
	Isolated   func(context.Context, RunSnapshot) error
	IOTimeout  time.Duration
}
type HandoffEnvelope struct{ Body, Signature []byte }
type handoffMessage struct {
	Peer       authorization.GrantPresentation
	Kind       string
	Request    HandoffRequest
	Source     string
	Epoch      uint64
	Digest     string
	Checkpoint []byte
}
type handoffCheckpoint struct {
	Run           RunSnapshot
	Commits       map[string]commit
	Operations    map[string]operation
	Controls      map[string]controlRecord
	Inputs        map[string]inputRecord
	ControlLimits ControlLimits
	Admission     authorization.RuntimeAdmission
}

const handoffMaxBytes = 2 << 20

func (p *HandoffPort) configured() bool {
	return len(p.config.PrivateKey) == ed25519.PrivateKeySize && p.config.Policy != nil && p.config.Isolated != nil && p.config.IOTimeout >= time.Millisecond && p.config.IOTimeout <= 5*time.Second
}
func (p *HandoffPort) check(ctx context.Context, phase string, r RunSnapshot, target string) error {
	if !p.configured() {
		return failure(authorization.Invalid)
	}
	bounded, cancel := context.WithTimeout(ctx, p.config.IOTimeout)
	defer cancel()
	return p.config.Policy(bounded, phase, r, target)
}
func (p *HandoffPort) sign(m handoffMessage) (HandoffEnvelope, error) {
	b, e := json.Marshal(m)
	if e != nil || len(b) > 3<<20 {
		return HandoffEnvelope{}, failure(authorization.Unavailable)
	}
	return HandoffEnvelope{Body: b, Signature: ed25519.Sign(p.config.PrivateKey, b)}, nil
}
func (p *HandoffPort) verify(in HandoffEnvelope, kind string) (handoffMessage, error) {
	var m handoffMessage
	if !p.configured() || len(in.Body) == 0 || len(in.Body) > 3<<20 || len(in.Signature) != ed25519.SignatureSize || json.Unmarshal(in.Body, &m) != nil || m.Kind != kind {
		return m, failure(authorization.Invalid)
	}
	peer := m.Source
	if kind == "prepared" || kind == "validate-admission" {
		peer = m.Request.Target
	}
	key := p.config.Peers[peer]
	if len(key) != ed25519.PublicKeySize || !ed25519.Verify(key, in.Body, in.Signature) || m.Request.Ref.Namespace != p.service.config.Namespace || !name(m.Request.Ref.TaskID) || !name(m.Request.Target) || m.Epoch == 0 || m.Epoch == ^uint64(0) || len(m.Digest) != 64 {
		return m, failure(authorization.Denied)
	}
	return m, nil
}
func handoffMessageOf(h HandoffState, kind string) handoffMessage {
	return handoffMessage{Kind: kind, Request: h.Request, Source: h.Source, Epoch: h.Epoch, Digest: h.Digest}
}
func matchesHandoff(h HandoffState, m handoffMessage) bool {
	return h.Request == m.Request && h.Source == m.Source && h.Epoch == m.Epoch && h.Digest == m.Digest
}
func (p *HandoffPort) Status(ctx context.Context, ref Ref, op string) (HandoffState, error) {
	var out HandoffState
	var visible RunSnapshot
	e := p.service.transaction(ctx, func(j *journal, tx authorization.RuntimeTransaction) error {
		if ref.Namespace != p.service.config.Namespace {
			return failure(authorization.Denied)
		}
		h, ok := j.Handoffs[ref.TaskID]
		if !ok || h.Request.OperationID != op {
			h, ok = j.HandoffHistory[op]
		}
		if !ok || h.Request.OperationID != op || h.Request.Ref != ref {
			return failure(authorization.NotFound)
		}
		r, ok := j.Runs[ref.TaskID]
		if !ok {
			var c handoffCheckpoint
			if json.Unmarshal(h.Checkpoint, &c) != nil {
				return failure(authorization.Invalid)
			}
			r = c.Run
		}
		if e := p.authorize(tx, r); e != nil {
			return e
		}
		out = h
		visible = r
		return nil
	})
	if e == nil {
		e = p.check(ctx, "status", visible, out.Request.Target)
	}
	if e == nil && len(out.Checkpoint) != 0 {
		var checkpoint handoffCheckpoint
		if json.Unmarshal(out.Checkpoint, &checkpoint) != nil {
			return HandoffState{}, failure(authorization.Invalid)
		}
		visible = checkpoint.Run
	}
	if e == nil {
		e = p.checkReports(ctx, "status", visible, out.Reports, out.Request.Target)
	}
	if e != nil {
		return HandoffState{}, e
	}
	return out, nil
}
func (p *HandoffPort) Export(ctx context.Context, ref Ref, op string) (HandoffEnvelope, error) {
	var h HandoffState
	var c handoffCheckpoint
	e := p.service.transaction(ctx, func(j *journal, tx authorization.RuntimeTransaction) error {
		var ok bool
		h, ok = j.Handoffs[ref.TaskID]
		if !ok || h.Request.OperationID != op || ref.Namespace != p.service.config.Namespace || h.Phase != "PREPARING" {
			return failure(authorization.Conflict)
		}
		r := j.Runs[ref.TaskID]
		if e := p.authorize(tx, r); e != nil {
			return e
		}
		if h.Digest != "" {
			if json.Unmarshal(h.Checkpoint, &c) != nil {
				return failure(authorization.Invalid)
			}
			return nil
		}
		c = handoffCheckpoint{Run: r, Commits: j.Commits[ref.TaskID], Operations: map[string]operation{}, Controls: map[string]controlRecord{}, Inputs: map[string]inputRecord{}, ControlLimits: j.ControlLimits}
		ids := []string{op}
		for id, v := range j.Operations {
			if v.Ref == ref {
				c.Operations[id] = v
				ids = append(ids, id)
			}
		}
		for id, v := range j.Controls {
			if v.Request.Ref == ref {
				c.Controls[id] = v
				ids = append(ids, id)
			}
		}
		for id, v := range j.InputChanges {
			_, r, _ := v.Change.meta()
			if r == ref {
				c.Inputs[id] = v
				ids = append(ids, id)
			}
		}
		if r.Delegations != nil {
			ids = append(ids, r.Delegations.Proposal.OperationID)
			for _, v := range r.Delegations.Children {
				ids = append(ids, v.Spec.OperationID)
			}
		}
		a, ok := tx.(authorization.RuntimeHandoffTransaction)
		if !ok {
			return failure(authorization.Unsupported)
		}
		var e error
		c.Admission, e = a.ExportRuntimeAdmission(ids)
		return e
	})
	if e != nil {
		return HandoffEnvelope{}, e
	}
	if e = p.check(ctx, "export", c.Run, h.Request.Target); e != nil {
		return HandoffEnvelope{}, e
	}
	b := h.Checkpoint
	if len(b) == 0 {
		b, e = json.Marshal(c)
	}
	if e != nil || len(b) > handoffMaxBytes {
		return HandoffEnvelope{}, failure(authorization.Unavailable)
	}
	h.Digest = fmt.Sprintf("%x", sha256.Sum256(b))
	h.Checkpoint = b
	e = p.service.transaction(ctx, func(j *journal, tx authorization.RuntimeTransaction) error {
		old := j.Handoffs[ref.TaskID]
		if old.Phase != "PREPARING" || old.Request != h.Request || !reflect.DeepEqual(j.Runs[ref.TaskID], c.Run) {
			return failure(authorization.Conflict)
		}
		if e := p.authorize(tx, c.Run); e != nil {
			return e
		}
		if old.Digest != "" && old.Digest != h.Digest {
			return failure(authorization.IdentityConflict)
		}
		h.Reports = old.Reports
		j.Handoffs[ref.TaskID] = h
		return nil
	})
	if e != nil {
		return HandoffEnvelope{}, e
	}
	m := handoffMessageOf(h, "prepare")
	m.Checkpoint = b
	return p.sign(m)
}
func (p *HandoffPort) Prepare(ctx context.Context, in HandoffEnvelope) (HandoffEnvelope, error) {
	m, e := p.verify(in, "prepare")
	if e != nil {
		return HandoffEnvelope{}, e
	}
	if m.Request.Target != p.service.config.Owner || m.Source == m.Request.Target || len(m.Checkpoint) > handoffMaxBytes || fmt.Sprintf("%x", sha256.Sum256(m.Checkpoint)) != m.Digest {
		return HandoffEnvelope{}, failure(authorization.Denied)
	}
	var c handoffCheckpoint
	if json.Unmarshal(m.Checkpoint, &c) != nil || c.Run.Task.Ref != m.Request.Ref || c.Run.Task.Owner != m.Source || c.Run.Task.OwnerEpoch != m.Epoch || c.Run.Task.Version != m.Request.ExpectedVersion || c.Run.Task.Resource != p.service.config.Resource {
		return HandoffEnvelope{}, failure(authorization.Invalid)
	}
	if e = p.check(ctx, "prepare", c.Run, m.Source); e != nil {
		return HandoffEnvelope{}, e
	}
	e = p.service.transaction(ctx, func(j *journal, tx authorization.RuntimeTransaction) error {
		if e := p.authorize(tx, c.Run); e != nil {
			return e
		}
		if h, ok := j.Handoffs[m.Request.Ref.TaskID]; ok {
			if matchesHandoff(h, m) {
				if h.Phase == "ABORTED" {
					return failure(authorization.Conflict)
				}
				return nil
			}
			if h.Phase != "ABORTED" {
				return failure(authorization.IdentityConflict)
			}
			if len(j.HandoffHistory) >= 32 {
				return failure(authorization.Unavailable)
			}
			j.HandoffHistory[h.Request.OperationID] = h
		}
		if _, ok := j.Runs[m.Request.Ref.TaskID]; ok {
			return failure(authorization.IdentityConflict)
		}
		if len(j.Handoffs) >= p.service.config.MaxTasks {
			return failure(authorization.Unavailable)
		}
		j.Handoffs[m.Request.Ref.TaskID] = HandoffState{Request: m.Request, Source: m.Source, Epoch: m.Epoch, Phase: "PREPARED", Digest: m.Digest, Checkpoint: m.Checkpoint}
		return nil
	})
	if e != nil {
		return HandoffEnvelope{}, e
	}
	m.Kind = "prepared"
	m.Checkpoint = nil
	return p.sign(m)
}
func (p *HandoffPort) Seal(ctx context.Context, ack HandoffEnvelope) (HandoffEnvelope, error) {
	m, e := p.verify(ack, "prepared")
	if e != nil {
		return HandoffEnvelope{}, e
	}
	h, e := p.Status(ctx, m.Request.Ref, m.Request.OperationID)
	if e != nil {
		return HandoffEnvelope{}, e
	}
	if !matchesHandoff(h, m) || h.Source != p.service.config.Owner {
		return HandoffEnvelope{}, failure(authorization.IdentityConflict)
	}
	var c handoffCheckpoint
	if json.Unmarshal(h.Checkpoint, &c) != nil {
		return HandoffEnvelope{}, failure(authorization.Invalid)
	}
	if e = p.check(ctx, "seal", c.Run, h.Request.Target); e != nil {
		return HandoffEnvelope{}, e
	}
	if h.Phase == "SEALED" {
		return h.Activation, nil
	}
	if h.Phase != "PREPARING" {
		return HandoffEnvelope{}, failure(authorization.Conflict)
	}
	bounded, cancel := context.WithTimeout(ctx, p.config.IOTimeout)
	defer cancel()
	if e = p.config.Isolated(bounded, c.Run); e != nil {
		return HandoffEnvelope{}, e
	}
	activation, e := p.sign(handoffMessageOf(h, "activate"))
	if e != nil {
		return HandoffEnvelope{}, e
	}
	e = p.service.transaction(ctx, func(j *journal, tx authorization.RuntimeTransaction) error {
		old := j.Handoffs[m.Request.Ref.TaskID]
		if !matchesHandoff(old, m) || old.Phase != "PREPARING" || !reflect.DeepEqual(j.Runs[m.Request.Ref.TaskID], c.Run) {
			return failure(authorization.Conflict)
		}
		if e := p.authorize(tx, c.Run); e != nil {
			return e
		}
		old.Phase = "SEALED"
		old.Activation = activation
		j.Handoffs[m.Request.Ref.TaskID] = old
		return nil
	})
	if e != nil {
		return HandoffEnvelope{}, e
	}
	return activation, nil
}
func (p *HandoffPort) Activate(ctx context.Context, in HandoffEnvelope) (HandoffState, error) {
	var out HandoffState
	m, e := p.verify(in, "activate")
	if e != nil {
		return out, e
	}
	h, e := p.Status(ctx, m.Request.Ref, m.Request.OperationID)
	if e != nil {
		return out, e
	}
	if !matchesHandoff(h, m) || m.Request.Target != p.service.config.Owner {
		return out, failure(authorization.IdentityConflict)
	}
	var c handoffCheckpoint
	if json.Unmarshal(h.Checkpoint, &c) != nil {
		return out, failure(authorization.Invalid)
	}
	if e = p.check(ctx, "activate", c.Run, m.Source); e != nil {
		return out, e
	}
	e = p.service.transaction(ctx, func(j *journal, tx authorization.RuntimeTransaction) error {
		old := j.Handoffs[m.Request.Ref.TaskID]
		if !matchesHandoff(old, m) {
			return failure(authorization.IdentityConflict)
		}
		if e := p.authorize(tx, c.Run); e != nil {
			return e
		}
		if old.Phase == "ACTIVE" {
			out = old
			return nil
		}
		if old.Phase != "PREPARED" {
			return failure(authorization.Conflict)
		}
		if len(j.Runs) >= p.service.config.MaxTasks {
			return failure(authorization.Unavailable)
		}
		a, ok := tx.(authorization.RuntimeHandoffTransaction)
		if !ok {
			return failure(authorization.Unsupported)
		}
		if e := a.ImportRuntimeAdmission(c.Admission); e != nil {
			return e
		}
		if j.ControlLimits != (ControlLimits{}) && c.ControlLimits != (ControlLimits{}) && j.ControlLimits != c.ControlLimits {
			return failure(authorization.IdentityConflict)
		}
		if j.ControlLimits == (ControlLimits{}) {
			j.ControlLimits = c.ControlLimits
		}
		for id, v := range c.Operations {
			if _, ok := j.Operations[id]; ok {
				return failure(authorization.IdentityConflict)
			}
			j.Operations[id] = v
		}
		for id, v := range c.Controls {
			if _, ok := j.Controls[id]; ok {
				return failure(authorization.IdentityConflict)
			}
			j.Controls[id] = v
		}
		for id, v := range c.Inputs {
			if _, ok := j.InputChanges[id]; ok {
				return failure(authorization.IdentityConflict)
			}
			j.InputChanges[id] = v
		}
		r := c.Run
		if r.ExecutionOrigins == nil {
			r.ExecutionOrigins = map[string]Qualification{}
		}
		for _, w := range r.Work {
			if w.ExecutionOperation != "" {
				if _, ok := r.ExecutionOrigins[w.ExecutionOperation]; !ok {
					q := QualificationOf(c.Run)
					q.WorkID = w.ID
					q.Generation = w.Generation
					q.Version = w.DecisionVersion
					if report, ok := r.ExecutionReports[w.ExecutionOperation]; ok {
						q = report.Qualification
					}
					r.ExecutionOrigins[w.ExecutionOperation] = q
				}
			}
		}
		r.Task.Owner = m.Request.Target
		r.Task.OwnerEpoch = m.Epoch + 1
		r.Task.Version++
		for i := range r.Work {
			r.Work[i].LeaseUntil = 0
			r.Work[i].Generation++
		}
		j.Runs[m.Request.Ref.TaskID] = r
		j.Commits[m.Request.Ref.TaskID] = c.Commits
		old.Phase = "ACTIVE"
		old.Activation = in
		j.Handoffs[m.Request.Ref.TaskID] = old
		out = old
		return nil
	})
	return out, e
}
