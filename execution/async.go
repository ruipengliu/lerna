package execution

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"lerna/authorization"
	"lerna/tasks"
	"time"
)

// AsyncPolicy is frozen with the capability. Handles are non-secret target
// locators in this reference profile; credentials must use a controlled broker.
type AsyncPolicy struct {
	Target, Source                      string
	Correlation, Cancel                 bool
	PollInterval, RecoveryWindow, Lease time.Duration
	MaxEvidence                         int
}
type Association struct{ Target, OperationID, Fingerprint, Handle string }
type Fact struct {
	CompletedAt int64
	Source      string
	Version     uint64
	Association Association
	Observation Observation
}
type AsyncDriver interface {
	Driver
	StartAsync(context.Context, Call) (Fact, error)
	InspectAsync(context.Context, Call, Association) (Fact, error)
}
type Evidence struct {
	Source                           string
	Version                          uint64
	Digest, Phase, Effect, Reference string
}
type AsyncState struct {
	Association                  Association
	Evidence                     []Evidence
	Conflict                     bool
	NextCheck, Until, LeaseUntil int64
	Generation                   uint64
	CompletedAt                  int64
}

func (s *Service) asyncPolicyValid() bool {
	p := s.cap.Async
	if p == nil {
		return s.cap.Synchronous
	}
	_, ok := s.driver.(AsyncDriver)
	if p.Cancel {
		if _, supports := s.driver.(CancelDriver); !supports {
			return false
		}
	}
	return ok && !s.cap.Synchronous && p.Target != "" && p.Source != "" && len(p.Target) <= 128 && len(p.Source) <= 128 && p.PollInterval >= time.Millisecond && p.PollInterval <= time.Minute && p.RecoveryWindow >= p.PollInterval && p.RecoveryWindow <= time.Hour && p.Lease >= s.config.IOTimeout && p.Lease <= time.Minute && p.MaxEvidence >= 2 && p.MaxEvidence <= 32
}
func (s *Service) asyncFact(ctx context.Context, original Record, f Fact) (Record, error) {
	p := s.cap.Async
	a := f.Association
	v := f.Observation
	if f.Source != p.Source || f.Version == 0 || a.Target != p.Target || a.OperationID != original.Request.OperationID || a.Fingerprint != original.Request.Fingerprint() || len(a.Handle) > 256 || len(v.Output) > s.config.MaxOutput || len(v.Evidence) > s.config.MaxOutput {
		return Record{}, failure(authorization.Denied)
	}
	if v.Phase != "IN_PROGRESS" && !validObservation(v) {
		return Record{}, failure(authorization.Invalid)
	}
	if v.Phase == "IN_PROGRESS" && (v.Effect != "UNKNOWN" || v.Result != "UNKNOWN") {
		return Record{}, failure(authorization.Invalid)
	}
	raw, _ := json.Marshal(f)
	sum := sha256.Sum256(raw)
	digest := hex.EncodeToString(sum[:])
	var replay Record
	skip := false
	e := s.transaction(ctx, func(j *journal, tx authorization.ExecutionTransaction) error {
		if e := s.authorize(tx, "capability.reconcile"); e != nil {
			return e
		}
		if f.CompletedAt < 0 || f.CompletedAt > tx.Now().UnixNano() {
			return failure(authorization.Invalid)
		}
		r, ok := j.Records[original.Request.OperationID]
		if !ok || !s.owned(r) || !r.Started || r.Async == nil {
			return failure(authorization.Denied)
		}
		skip = false
		for _, old := range r.Async.Evidence {
			if old.Source == f.Source && old.Version == f.Version && old.Digest == digest {
				skip = true
			}
		}
		if v.Phase == "UNKNOWN" && len(v.Evidence) == 0 && len(r.Reports) > 0 && r.Phase == "UNKNOWN" {
			skip = true
		}
		if skip {
			replay = r
			return nil
		}
		if len(r.Async.Evidence) >= p.MaxEvidence || len(r.Reports) >= s.config.MaxReports || pending(j) >= s.config.MaxOutbox {
			return failure(authorization.Unavailable)
		}
		return nil
	})
	if e != nil {
		return Record{}, e
	}
	if skip {
		return replay, nil
	}
	ref := ""
	if len(v.Evidence) > 0 {
		op, e := s.identity(ctx)
		if e != nil {
			return Record{}, e
		}
		output := v.Output
		if len(output) == 0 {
			output = []byte(`null`)
		}
		ref, e = s.saveContent(ctx, original.Request, op, output, v.Evidence)
		if e != nil {
			v.Result = "FAILURE"
		}
	}
	if v.Effect == "CONFIRMED" && (s.validate(v.Output, s.cap.Output, s.config.MaxOutput) != nil || ref == "") {
		v.Result = "FAILURE"
	}
	var out Record
	e = s.transaction(ctx, func(j *journal, tx authorization.ExecutionTransaction) error {
		if e := s.authorize(tx, "capability.reconcile"); e != nil {
			return e
		}
		r, ok := j.Records[original.Request.OperationID]
		if !ok || !s.owned(r) || !r.Started {
			return failure(authorization.Denied)
		}
		if r.Async == nil {
			return failure(authorization.Invalid)
		}
		for _, old := range r.Async.Evidence {
			if old.Source == f.Source && old.Version == f.Version && old.Digest == digest {
				out = r
				return nil
			}
		}
		if len(r.Async.Evidence) >= p.MaxEvidence || len(r.Reports) >= s.config.MaxReports || pending(j) >= s.config.MaxOutbox {
			return failure(authorization.Unavailable)
		}
		if r.Async.Association.Handle != "" && a.Handle != "" && r.Async.Association != a {
			return failure(authorization.IdentityConflict)
		}
		if a.Handle != "" {
			r.Async.Association = a
		}
		conflicting := false
		stale := false
		for _, old := range r.Async.Evidence {
			if v.Phase == "UNKNOWN" && len(v.Evidence) == 0 {
				continue
			}
			if old.Source == f.Source && old.Version > f.Version {
				stale = true
			}
			if old.Source == f.Source && old.Version == f.Version && old.Digest != digest {
				conflicting = true
			}
			if old.Effect != "UNKNOWN" && v.Effect != "UNKNOWN" && old.Effect != v.Effect {
				conflicting = true
			}
		}
		if v.Phase != "UNKNOWN" || len(v.Evidence) > 0 {
			r.Async.Evidence = append(r.Async.Evidence, Evidence{f.Source, f.Version, digest, v.Phase, v.Effect, ref})
		}
		r.Async.Conflict = r.Async.Conflict || conflicting
		r.ConflictingEvidence = r.Async.Conflict
		if r.Async.Conflict {
			r.Phase = "UNKNOWN"
			r.Result = "UNKNOWN"
			r.Effect = "UNKNOWN"
			r.Reference = ""
			if j.Active == "" {
				j.Active = r.Request.OperationID
			}
		} else if !stale && (r.Effect == "UNKNOWN" || v.Effect != "UNKNOWN") {
			r.Phase = v.Phase
			r.Result = v.Result
			r.Effect = v.Effect
			r.Reference = ref
			r.Async.CompletedAt = f.CompletedAt
		}
		if r.Effect != "UNKNOWN" && !r.Async.Conflict && j.Active == r.Request.OperationID {
			j.Active = ""
		}
		if r.CancelID != "" && !r.Async.Conflict && r.Effect != "UNKNOWN" {
			c := j.Cancels[r.CancelID]
			if r.Effect == "CONFIRMED" {
				c.Progress = "NOT_PREVENTED"
			} else {
				c.Progress = "STOPPED"
			}
			j.Cancels[r.CancelID] = c
		}
		r.Revision++
		r.Reports = append(r.Reports, tasks.ExecutionReport{OperationID: r.Request.OperationID, Qualification: r.Request.Qualification, Revision: r.Revision, Phase: r.Phase, Result: r.Result, Effect: r.Effect, Reference: r.Reference, Conflict: r.Async.Conflict, CompletedAt: r.Async.CompletedAt})
		j.Records[r.Request.OperationID] = r
		out = r
		return nil
	})
	return out, e
}

func (s *Service) reconcileAsync(ctx context.Context, op string) (Record, error) {
	select {
	case s.slots <- struct{}{}:
	default:
		return Record{}, failure(authorization.Unavailable)
	}
	release := true
	defer func() {
		if release {
			<-s.slots
		}
	}()
	var r Record
	claimed := false
	e := s.transaction(ctx, func(j *journal, tx authorization.ExecutionTransaction) error {
		claimed = false
		if e := s.authorize(tx, "capability.reconcile"); e != nil {
			return e
		}
		var ok bool
		r, ok = j.Records[op]
		if !ok || !s.owned(r) {
			return failure(authorization.Denied)
		}
		if !r.Started || r.Async == nil || r.Effect != "UNKNOWN" {
			return nil
		}
		now := tx.Now().UnixNano()
		if now < r.Async.NextCheck || now < r.Async.LeaseUntil {
			return failure(authorization.Conflict)
		}
		if now >= r.Async.Until || r.Checks >= uint32(s.config.MaxChecks) {
			return failure(authorization.Unavailable)
		}
		r.Checks++
		r.Async.Generation++
		r.Async.LeaseUntil = tx.Now().Add(s.cap.Async.Lease).UnixNano()
		r.Async.NextCheck = tx.Now().Add(s.cap.Async.PollInterval).UnixNano()
		j.Records[op] = r
		claimed = true
		return nil
	})
	if e != nil {
		return Record{}, e
	}
	if !claimed {
		return r, nil
	}
	bounded, cancel := context.WithTimeout(ctx, s.config.IOTimeout)
	type response struct {
		r Record
		e error
	}
	done := make(chan response, 1)
	release = false
	go func() {
		defer func() { <-s.slots; cancel() }()
		var out Record
		var err error
		defer func() {
			if recover() != nil {
				out = r
				err = failure(authorization.Unavailable)
			}
			done <- response{out, err}
		}()
		if r.Async.Association.Handle == "" && !s.cap.Async.Correlation {
			out = r
			return
		}
		fact, e := s.driver.(AsyncDriver).InspectAsync(bounded, Call{Request: r.Request}, r.Async.Association)
		save, stop := context.WithTimeout(context.WithoutCancel(ctx), s.config.IOTimeout)
		defer stop()
		out = r
		err = e
		if e == nil {
			out, err = s.asyncFact(save, r, fact)
		}
		// Late evidence is retained, but an old claim cannot clear a newer lease.
		_ = s.transaction(save, func(j *journal, tx authorization.ExecutionTransaction) error {
			v := j.Records[op]
			if v.Async.Generation == r.Async.Generation {
				v.Async.LeaseUntil = 0
				j.Records[op] = v
			}
			return nil
		})
	}()
	select {
	case v := <-done:
		return v.r, v.e
	case <-bounded.Done():
		return r, nil
	}
}
