package brain

import (
	"context"
	"fmt"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/runtime"
	"reflect"
	"time"
)

type Service struct {
	config            Config
	profiles          map[string]Profile
	proposalValidator *api.Validator
}

func New(c Config) (*Service, error) {
	if c.Content == nil || c.Engine == nil || c.Gate == nil || len(c.Profiles) == 0 || len(c.Profiles) > 100 {
		return nil, api.E("invalid_request", "invalid_brain_configuration")
	}
	s := &Service{config: c, profiles: map[string]Profile{}}
	for _, p := range c.Profiles {
		if e := api.ValidateRecord("ComponentRef", p.Ref); e != nil {
			return nil, e
		}
		if p.ContextLimit == 0 || p.MaxInputBytes == 0 || p.MaxInputBytes > api.MaxJSONBytes || p.MaxOutputTokens == 0 || p.RequestTimeout <= 0 || p.RequestTimeout > 2*time.Minute {
			return nil, api.E("invalid_request", "invalid_model_limits")
		}
		s.profiles[key(p.Ref)] = p
	}
	var e error
	s.proposalValidator, e = api.NewValidator(ProposalSchema())
	return s, e
}
func key(ref api.ComponentRef) string { return ref.ComponentID + "/" + ref.Version + "/" + ref.Digest }
func (s *Service) participants() []string {
	return append([]string{Namespace}, s.config.Participants...)
}
func (s *Service) Register(r *runtime.Registry) error {
	methods := []runtime.Method{
		{Contract: api.Contract[DecideInput, Output]("brain.decide", Namespace, "command", false, true), Participants: s.participants(), Apply: func(ctx context.Context, tx runtime.Tx, a runtime.Auth, c api.Command) (runtime.Outcome, error) {
			var in DecideInput
			if e := api.Decode(c.Payload, &in); e != nil {
				return runtime.Outcome{}, e
			}
			out, e := s.decide(ctx, tx, a, c, in)
			return runtime.Accepted(out), e
		}},
		{Contract: api.Contract[CancelInput, Output]("brain.cancel", Namespace, "command", false, false), Participants: s.participants(), Apply: func(ctx context.Context, tx runtime.Tx, a runtime.Auth, c api.Command) (runtime.Outcome, error) {
			var in CancelInput
			if e := api.Decode(c.Payload, &in); e != nil {
				return runtime.Outcome{}, e
			}
			if c.TargetID != in.DecisionID {
				return runtime.Outcome{}, api.E("invalid_request", "target_mismatch")
			}
			var d decision
			rev, e := getDecisionTx(ctx, tx, in.DecisionID, &d)
			if e != nil {
				return runtime.Outcome{}, e
			}
			if !allowed(a, d) || !api.Equal(in.TaskRef, d.Input.TaskRef) {
				return runtime.Outcome{}, api.E("forbidden", "decision_target_mismatch")
			}
			d.CancelRequested = true
			if !d.Record.SendStarted {
				d.Record.Status = "cancelled"
				d.Record.UsageFinal = true
				d.Phase = "cancelled"
			}
			d.Revision++
			d.Record.Revision++
			if e = putDecision(ctx, tx, in.DecisionID, rev, d); e != nil {
				return runtime.Outcome{}, e
			}
			now, e := tx.Now(ctx)
			if e == nil {
				_, e = tx.Raise(ctx, JobAdvance, "decision/"+in.DecisionID, tx.Scope().Ref(in.DecisionID, d.Revision), now)
			}
			return runtime.Applied(Output{tx.Scope().Ref(in.DecisionID, d.Record.Revision), d.Record.Status}), e
		}},
		{Contract: api.Contract[GetInput, View]("brain.get", Namespace, "query", false, false), Query: func(ctx context.Context, store runtime.Store, scope runtime.Scope, a runtime.Auth, q api.Query) (any, error) {
			var in GetInput
			if e := api.Decode(q.Payload, &in); e != nil {
				return nil, e
			}
			if q.TargetID != in.DecisionID {
				return nil, api.E("invalid_request", "target_mismatch")
			}
			return s.Get(ctx, store, scope, a, in.DecisionID)
		}},
	}
	for _, m := range methods {
		if e := r.Register(m); e != nil {
			return e
		}
	}
	return r.RegisterJob(JobAdvance, s.advance)
}
func allowed(a runtime.Auth, d decision) bool {
	return a.TenantID == d.Principal.TenantID && (a.SubjectID == d.Principal.SubjectID || a.HasRole("service"))
}
func (s *Service) decide(ctx context.Context, tx runtime.Tx, a runtime.Auth, c api.Command, in DecideInput) (Output, error) {
	if !a.HasRole("service") || c.TargetID != in.DecisionID {
		return Output{}, api.E("forbidden", "trusted_orchestrator_required")
	}
	if _, ok := s.profiles[key(in.ModelProfileRef)]; !ok {
		return Output{}, api.E("unsupported", "profile_not_supported")
	}
	if in.TaskRef.TenantID != tx.Scope().TenantID || in.SnapshotRef.TenantID != tx.Scope().TenantID || in.SnapshotRevision == 0 {
		return Output{}, api.E("forbidden", "snapshot_scope_mismatch")
	}
	if e := api.ValidateAmounts(in.Limits); e != nil {
		return Output{}, e
	}
	digest, e := api.Digest(in)
	if e != nil {
		return Output{}, e
	}
	if e = s.config.Gate.CheckTx(ctx, tx, a, in, nil); e != nil {
		return Output{}, e
	}
	var old decision
	_, e = tx.Get(ctx, records, in.DecisionID, &old)
	if e == nil {
		if old.InputDigest != digest || old.CommandID != c.CommandID {
			return Output{}, api.E("idempotency_conflict", "decision_input_changed")
		}
		return Output{tx.Scope().Ref(in.DecisionID, old.Record.Revision), old.Record.Status}, nil
	}
	if !api.IsCode(e, "not_found") {
		return Output{}, e
	}
	now, e := tx.Now(ctx)
	if e != nil {
		return Output{}, e
	}
	deadline, e := api.ParseTime(in.Deadline)
	if e != nil || !now.Before(deadline) {
		return Output{}, api.E("expired", "decision_expired")
	}
	d := decision{Revision: 1, Record: api.DecisionRecord{DecisionID: in.DecisionID, OwnerID: tx.Scope().OwnerID, Revision: 1, SnapshotRevision: in.SnapshotRevision, Status: "accepted", Usage: []api.Amount{}, UsageFinal: !s.config.Engine.Physical()}, Input: in, Principal: a, CommandID: c.CommandID, InputDigest: digest, Phase: "accepted", CallID: api.NewID("call"), Publications: []pendingContent{}, ProposalContentID: api.NewID("content")}
	if e = tx.Create(ctx, records, in.DecisionID, in.TaskRef.ObjectID, d); e != nil {
		return Output{}, e
	}
	_, e = tx.Raise(ctx, JobAdvance, "decision/"+in.DecisionID, tx.Scope().Ref(in.DecisionID, 1), now)
	return Output{tx.Scope().Ref(in.DecisionID, 1), "accepted"}, e
}
func (s *Service) Get(ctx context.Context, store runtime.Store, scope runtime.Scope, a runtime.Auth, id string) (View, error) {
	var d decision
	if _, e := store.Read(ctx, scope, records, id, 0, &d); e != nil {
		return View{}, e
	}
	if !allowed(a, d) {
		return View{}, api.E("forbidden", "decision_redacted")
	}
	if e := hydrateStoredDecision(ctx, store, scope, &d); e != nil {
		return View{}, e
	}
	return View{d.Record, d.Input.TaskRef, d.Input.SnapshotRef, d.Phase, d.CallID, d.CancelRequested}, nil
}
func (s *Service) Usage(ctx context.Context, store runtime.Store, scope runtime.Scope, ref api.ObjectRef) (api.UsageSnapshot, error) {
	var d decision
	if ref.TenantID != scope.TenantID || ref.OwnerID != scope.OwnerID {
		return api.UsageSnapshot{}, api.E("forbidden", "usage_owner_mismatch")
	}
	if _, e := store.Read(ctx, scope, records, ref.ObjectID, 0, &d); e != nil {
		return api.UsageSnapshot{}, e
	}
	if e := hydrateStoredDecision(ctx, store, scope, &d); e != nil {
		return api.UsageSnapshot{}, e
	}
	u := api.UsageSnapshot{SourceRef: scope.Ref(ref.ObjectID, d.Record.Revision), UsageRevision: d.Record.Revision, Cumulative: append([]api.Amount{}, d.Record.Usage...), SpendingClosed: d.Record.Status == "completed" || d.Record.Status == "cancelled" || d.Record.Status == "failed", UsageFinal: d.Record.UsageFinal, ProofRefs: []api.ContentRef{}}
	if len(u.Cumulative) == 0 {
		for _, unit := range d.Input.Limits {
			u.Cumulative = append(u.Cumulative, api.Amount{Unit: unit.Unit, Value: "0"})
		}
	}
	proofBody := api.Raw(struct {
		Snapshot         api.UsageSnapshot `json:"snapshot"`
		CallID           string            `json:"call_id"`
		PhysicalRequests uint64            `json:"physical_requests"`
		SendStarted      bool              `json:"send_started"`
		Phase            string            `json:"phase"`
	}{u, d.CallID, d.Record.PhysicalRequestCount, d.Record.SendStarted, d.Phase})
	proofID := "content_" + api.Hash([]byte(d.Input.DecisionID + "/usage/" + fmt.Sprint(d.Record.Revision)))[7:39]
	proof, err := s.config.Content.Publish(ctx, scope, d.Principal, Publication{ContentID: proofID, MediaType: "application/vnd.harness.usage-proof+json", ProcessedSources: []api.ContentRef{d.Input.SnapshotRef}, DisclosedSources: []api.ContentRef{}}, proofBody)
	if err != nil {
		return api.UsageSnapshot{}, err
	}
	u.ProofRefs = []api.ContentRef{proof}
	u.UsageDigest, _ = api.Digest(u)
	return u, nil
}
func (s *Service) finish(ctx context.Context, store runtime.Store, scope runtime.Scope, w runtime.Work, disp runtime.Disposition, fn func(runtime.Tx) error) error {
	return runtime.Finish(ctx, store, scope, s.participants(), w, disp, fn)
}
func (s *Service) change(ctx context.Context, tx runtime.Tx, id string, fn func(*decision) error) error {
	var d decision
	rev, e := getDecisionTx(ctx, tx, id, &d)
	if e != nil {
		return e
	}
	if e = fn(&d); e != nil {
		return e
	}
	d.Revision++
	d.Record.Revision++
	return putDecision(ctx, tx, id, rev, d)
}

func sameFrozenDecision(current, original decision) bool {
	// Encoding的私有base64容器可超过公开256KiB；比较完整冻结类型和原字节，
	// 不借公开JSON上界判断相等，也不只核Digest放过receiver/来源等字段变化。
	return current.CommandID == original.CommandID && current.InputDigest == original.InputDigest && api.Equal(current.Input, original.Input) && api.Equal(current.Principal, original.Principal) && (original.Encoding == nil || reflect.DeepEqual(current.Encoding, original.Encoding))
}
func (s *Service) wait(ctx context.Context, store runtime.Store, scope runtime.Scope, w runtime.Work) error {
	return s.finish(ctx, store, scope, w, runtime.Waiting(time.Now().Add(time.Second)), nil)
}
func (s *Service) advance(ctx context.Context, store runtime.Store, scope runtime.Scope, w runtime.Work) error {
	id := w.Job.SourceRef.ObjectID
	var d decision
	if _, e := store.Read(ctx, scope, records, id, 0, &d); e != nil {
		return e
	}
	if e := hydrateStoredDecision(ctx, store, scope, &d); e != nil {
		return e
	}
	if !d.CancelRequested && (d.Phase == "accepted" || d.Phase == "encoded" || d.Phase == "publishing") {
		if preparer, ok := s.config.Gate.(GatePreparer); ok {
			if e := store.CheckClaim(ctx, scope, w.Claim); e != nil {
				return e
			}
			prepared, e := preparer.PrepareGate(ctx, scope, d.Principal, d.Input, d.Encoding)
			if e != nil {
				return s.closeAfterGateError(ctx, store, scope, w, d, gateFailure(e))
			}
			if prepared == nil {
				return api.E("dependency_unavailable", "current_gate_context_unavailable")
			}
			ctx = prepared
			if e = store.CheckClaim(ctx, scope, w.Claim); e != nil {
				return e
			}
		}
	}
	p := s.profiles[key(d.Input.ModelProfileRef)]
	switch d.Phase {
	case "completed", "failed":
		if d.Record.SendStarted && !d.Record.UsageFinal {
			return s.reconcileClosed(ctx, store, scope, w, d, p)
		}
		return s.finish(ctx, store, scope, w, runtime.Done(), nil)
	case "cancelled":
		return s.reconcileClosed(ctx, store, scope, w, d, p)
	case "accepted":
		if e := s.currentGate(ctx, store, scope, w, d); e != nil {
			return s.closeAfterGateError(ctx, store, scope, w, d, e)
		}
		raw, e := s.config.Content.Read(ctx, scope, d.Principal, d.Input.SnapshotRef, "brain.input")
		if e != nil {
			return s.wait(ctx, store, scope, w)
		}
		if api.Hash(raw) != d.Input.SnapshotRef.Hash || uint64(len(raw)) != d.Input.SnapshotRef.ByteLength {
			return api.E("invalid_request", "snapshot_digest_mismatch")
		}
		var snap api.Snapshot
		if e = api.Decode(raw, &snap); e != nil {
			return e
		}
		if !api.Equal(snap.TaskRef, d.Input.TaskRef) || snap.Revision != d.Input.SnapshotRevision || !api.Equal(snap.ModelProfileRef, d.Input.ModelProfileRef) {
			return api.E("revision_conflict", "snapshot_mismatch")
		}
		goal, e := s.config.Content.Read(ctx, scope, d.Principal, snap.GoalRef, "brain.input")
		if e != nil {
			return s.wait(ctx, store, scope, w)
		}
		if api.Hash(goal) != snap.GoalRef.Hash {
			return api.E("invalid_request", "goal_digest_mismatch")
		}
		encoding, e := s.config.Engine.Encode(ctx, snap, goal, p)
		if e != nil {
			return e
		}
		if e = validateEncoding(snap, p, encoding); e != nil {
			return e
		}
		err := s.finish(ctx, store, scope, w, runtime.Ready(time.Now()), func(tx runtime.Tx) error {
			// 当前 Task/预算门禁先于 Decision 行锁；只使用事务外已读的原冻结输入。
			if e := s.config.Gate.CheckTx(ctx, tx, d.Principal, d.Input, &encoding); e != nil {
				return gateFailure(e)
			}
			return s.change(ctx, tx, id, func(next *decision) error {
				if !sameFrozenDecision(*next, d) || next.Phase != "accepted" || next.CancelRequested {
					return api.E("revision_conflict", "decision_changed")
				}
				next.Snapshot = &snap
				next.Encoding = &encoding
				next.Phase = "encoded"
				return nil
			})
		})
		return s.closeAfterGateError(ctx, store, scope, w, d, err)
	case "encoded":
		if d.Encoding == nil || d.Snapshot == nil {
			return fmt.Errorf("brain sealed encoding missing")
		}
		if d.CancelRequested {
			return s.finishCancelled(ctx, store, scope, w, d)
		}
		status, e := store.Within(ctx, scope, s.participants(), func(tx runtime.Tx) error {
			if e := s.config.Gate.CheckTx(ctx, tx, d.Principal, d.Input, d.Encoding); e != nil {
				return gateFailure(e)
			}
			if e := tx.Guard(ctx, w.Claim); e != nil {
				return e
			}
			return s.change(ctx, tx, id, func(n *decision) error {
				if !sameFrozenDecision(*n, d) || n.Phase != "encoded" {
					return api.E("revision_conflict", "decision_changed")
				}
				if n.CancelRequested {
					return gateFailure(api.E("invalid_state", "decision_cancelled"))
				}
				now, e := tx.Now(ctx)
				if e != nil {
					return e
				}
				deadline, e := api.ParseTime(n.Input.Deadline)
				if e != nil || !now.Before(deadline) {
					return gateFailure(api.E("expired", "decision_expired"))
				}
				if s.config.Engine.Physical() {
					n.Record.SendStarted = true
					n.Record.PhysicalRequestCount = 1
					n.Phase = "send_started"
				}
				return nil
			})
		})
		if status == runtime.CommitUnknown {
			return runtime.ErrCommitUnknown
		}
		if e != nil {
			return s.closeAfterGateError(ctx, store, scope, w, d, e)
		}
		callCtx, cancel := context.WithTimeout(ctx, p.RequestTimeout)
		generated, e := s.config.Engine.Request(callCtx, d.CallID, *d.Encoding)
		cancel()
		if e != nil && s.config.Engine.Physical() {
			return s.finish(ctx, store, scope, w, runtime.Waiting(time.Now().Add(time.Second)), func(tx runtime.Tx) error {
				return s.change(ctx, tx, id, func(n *decision) error {
					n.Phase = "provider_result_unknown"
					n.Record.Status = n.Phase
					n.Record.UsageFinal = false
					return nil
				})
			})
		}
		if e != nil {
			return e
		}
		return s.saveGenerated(ctx, store, scope, w, d, generated)
	case "send_started", "provider_result_unknown":
		if d.Encoding == nil {
			return fmt.Errorf("missing original model encoding")
		}
		out, e := s.config.Engine.Lookup(ctx, d.CallID, *d.Encoding)
		if e != nil {
			return s.finish(ctx, store, scope, w, runtime.Waiting(time.Now().Add(time.Second)), func(tx runtime.Tx) error {
				return s.change(ctx, tx, id, func(n *decision) error {
					n.Phase = "provider_result_unknown"
					n.Record.Status = n.Phase
					n.Record.UsageFinal = false
					return nil
				})
			})
		}
		return s.saveGenerated(ctx, store, scope, w, d, out)
	case "publishing":
		return s.publish(ctx, store, scope, w, d)
	default:
		return fmt.Errorf("unrecognized brain phase %q", d.Phase)
	}
}
func validateEncoding(snap api.Snapshot, p Profile, e Encoding) error {
	if len(e.Body) == 0 || uint64(len(e.Body)) > p.MaxInputBytes || api.Hash(e.Body) != e.Digest || e.Receiver == "" || e.Location == "" || e.CountMode != "exact" && e.CountMode != "upper_bound" {
		return api.E("invalid_request", "invalid_model_encoding")
	}
	if e.InputTokens > p.MaxInputTokens || snap.ReservedOutputTokens > p.MaxOutputTokens || e.InputTokens+snap.ReservedOutputTokens+p.SafetyMargin > p.ContextLimit {
		return api.E("invalid_request", "input_over_limit")
	}
	for _, ref := range snap.ProcessedSources {
		found := false
		for _, actual := range e.ProcessedSources {
			if api.Equal(ref, actual) {
				found = true
				break
			}
		}
		if !found {
			return api.E("forbidden", "processed_source_missing")
		}
	}
	return nil
}
func (s *Service) saveGenerated(ctx context.Context, store runtime.Store, scope runtime.Scope, w runtime.Work, d decision, out Generated) error {
	if e := validateGenerated(out); e != nil {
		disposition := runtime.Done()
		if s.config.Engine.Physical() && !out.UsageFinal {
			disposition = runtime.Waiting(time.Now().Add(time.Second))
		}
		return s.finish(ctx, store, scope, w, disposition, func(tx runtime.Tx) error {
			if _, e := tx.LoadCommand(ctx, d.CommandID); e != nil {
				return e
			}
			return s.change(ctx, tx, d.Input.DecisionID, func(n *decision) error {
				if !sameFrozenDecision(*n, d) {
					return api.E("idempotency_conflict", "original_decision_changed")
				}
				n.Phase = "failed"
				n.Record.Status = "failed"
				n.Generated = &out
				n.Record.Usage = out.Usage
				n.Record.UsageFinal = out.UsageFinal
				return runtime.Decide(ctx, tx, n.CommandID, nil, api.E("invalid_request", "model_output_invalid"))
			})
		})
	}
	return s.finish(ctx, store, scope, w, runtime.Ready(time.Now()), func(tx runtime.Tx) error {
		return s.change(ctx, tx, d.Input.DecisionID, func(n *decision) error {
			n.Generated = &out
			n.Record.Usage = out.Usage
			n.Record.UsageFinal = out.UsageFinal
			if n.CancelRequested {
				n.Phase = "cancelled"
				n.Record.Status = "cancelled"
				return nil
			}
			n.Phase = "publishing"
			n.Publications = []pendingContent{}
			for _, c := range out.Contents {
				n.Publications = append(n.Publications, pendingContent{GeneratedContent: c, ContentID: api.NewID("content")})
			}
			return nil
		})
	})
}
func (s *Service) publish(ctx context.Context, store runtime.Store, scope runtime.Scope, w runtime.Work, d decision) error {
	if d.CancelRequested {
		return s.finishCancelled(ctx, store, scope, w, d)
	}
	if e := s.currentGate(ctx, store, scope, w, d); e != nil {
		return s.closeAfterGateError(ctx, store, scope, w, d, e)
	}
	for i, c := range d.Publications {
		if c.Ref != nil {
			continue
		}
		body, e := publicationBytes(d, c)
		if e != nil {
			return e
		}
		sources := append([]api.ContentRef{}, d.Encoding.ProcessedSources...)
		if c.ContentLocalID != "" {
			for _, dependency := range d.Publications {
				if dependency.LocalID == c.ContentLocalID && dependency.Ref != nil {
					sources = append(sources, *dependency.Ref)
				}
			}
		}
		ref, e := s.config.Content.Publish(ctx, scope, d.Principal, Publication{ContentID: c.ContentID, MediaType: c.MediaType, ProcessedSources: sources, DisclosedSources: c.DisclosedSources}, body)
		if e != nil {
			return s.wait(ctx, store, scope, w)
		}
		return s.finish(ctx, store, scope, w, runtime.Ready(time.Now()), func(tx runtime.Tx) error {
			return s.change(ctx, tx, d.Input.DecisionID, func(n *decision) error {
				if n.Publications[i].ContentID != c.ContentID {
					return api.E("idempotency_conflict", "publication_changed")
				}
				n.Publications[i].Ref = &ref
				return nil
			})
		})
	}
	proposal, e := materialize(d)
	if e != nil {
		return e
	}
	if e = s.proposalValidator.Validate(api.Raw(proposal)); e != nil {
		return e
	}
	ref, e := s.config.Content.Publish(ctx, scope, d.Principal, Publication{ContentID: d.ProposalContentID, MediaType: "application/vnd.harness.proposal+json", ProcessedSources: append(append([]api.ContentRef{}, d.Encoding.ProcessedSources...), publicationRefs(d)...), DisclosedSources: []api.ContentRef{}}, api.Raw(proposal))
	if e != nil {
		return s.wait(ctx, store, scope, w)
	}
	disposition := runtime.Done()
	if d.Record.SendStarted && !d.Record.UsageFinal {
		disposition = runtime.Waiting(time.Now().Add(time.Second))
	}
	err := s.finish(ctx, store, scope, w, disposition, func(tx runtime.Tx) error {
		if _, e := tx.LoadCommand(ctx, d.CommandID); e != nil {
			return e
		}
		if e := s.config.Gate.CheckTx(ctx, tx, d.Principal, d.Input, d.Encoding); e != nil {
			return gateFailure(e)
		}
		return s.change(ctx, tx, d.Input.DecisionID, func(n *decision) error {
			if !sameFrozenDecision(*n, d) || n.Phase != "publishing" {
				return api.E("revision_conflict", "decision_changed")
			}
			if n.CancelRequested {
				return gateFailure(api.E("invalid_state", "decision_cancelled"))
			}
			n.Record.ProposalRef = &ref
			n.Record.Status = "completed"
			n.Phase = "completed"
			return runtime.Decide(ctx, tx, n.CommandID, Output{scope.Ref(n.Input.DecisionID, n.Record.Revision+1), "completed"}, nil)
		})
	})
	return s.closeAfterGateError(ctx, store, scope, w, d, err)
}
