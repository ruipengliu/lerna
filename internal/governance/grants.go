package governance

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/runtime"
)

func (s *Service) registerGrants(r *runtime.Registry) error {
	registrations := []func() error{
		func() error {
			return registerCommand[LeaseUseRequest, UseReceipt](s, r, "grant.lease.use", false, false, s.useLease)
		},
		func() error {
			return registerCommand[LeaseReport, GrantLease](s, r, "grant.lease.report", false, true, s.reportLease)
		},
		func() error {
			return registerCommand[GrantIssue, ConfirmedOutput](s, r, "grant.issue", false, true, s.issueGrant)
		},
		func() error {
			return registerCommand[GrantRevoke, ConfirmedOutput](s, r, "grant.revoke", true, true, s.revokeGrant)
		},
		func() error {
			return registerCommand[ConfirmationDecision, StateOutput](s, r, "confirmation.decide", false, false, s.decideConfirmation)
		},
		func() error { return s.registerConfirmationRead(r) },
		func() error { return registerQuery[IDInput, GrantRecord](r, "grant.read", s.readGrant) },
		func() error {
			return registerCommand[UseRequest, UseReceipt](s, r, "grant.use", false, false, func(ctx context.Context, tx runtime.Tx, a runtime.Auth, c api.Command, in UseRequest) (runtime.Outcome, error) {
				out, err := s.UseTx(ctx, tx, a, in)
				return runtime.Applied(out), err
			})
		},
		func() error { return registerQuery[UseRequest, UseReceipt](r, "grant.check", s.checkGrantQuery) },
		func() error {
			return registerQuery[IDInput, UseReceipt](r, "grant.use.get", queryByID[UseReceipt]("uses", func(a runtime.Auth, u UseReceipt) error {
				if a.SubjectID != u.SubjectRef.ObjectID && !a.HasRole("grant_authority") {
					return api.E("forbidden", "use_redacted")
				}
				return nil
			}))
		},
		func() error {
			return registerCommand[SettleRequest, UseSettlement](s, r, "grant.use.settle", false, true, s.requestSettlement)
		},
		func() error {
			return registerQuery[IDInput, UseSettlement](r, "grant.settlement.read", s.readSettlement)
		},
		func() error {
			return registerCommand[AcceptanceCreate, ConfirmedOutput](s, r, "policy.acceptance.create", false, true, s.createAcceptance)
		},
		func() error {
			return registerCommand[RefInput, StateOutput](s, r, "policy.acceptance.revoke", true, false, s.revokeAcceptance)
		},
		func() error {
			return registerQuery[IDInput, PolicyAcceptance](r, "policy.acceptance.read", queryByID[PolicyAcceptance]("acceptances", func(a runtime.Auth, p PolicyAcceptance) error {
				if p.SubjectRef.ObjectID != a.SubjectID && !a.HasRole("grant_authority") {
					return api.E("forbidden", "acceptance_redacted")
				}
				return nil
			}))
		},
		func() error {
			return registerCommand[LeaseAllocate, GrantLease](s, r, "grant.lease.allocate", false, false, s.allocateLease)
		},
		func() error {
			return registerCommand[RefInput, StateOutput](s, r, "grant.lease.close", true, false, s.closeLease)
		},
		func() error {
			return registerQuery[IDInput, GrantLease](r, "grant.lease.read", queryByID[GrantLease]("leases", func(a runtime.Auth, l GrantLease) error {
				if a.SubjectID != l.Scope.SubjectRef.ObjectID && !a.HasRole("grant_authority") {
					return api.E("forbidden", "lease_redacted")
				}
				return nil
			}))
		},
	}
	for _, fn := range registrations {
		if err := fn(); err != nil {
			return err
		}
	}
	return nil
}

func validateGrant(scope runtime.Scope, g api.Grant) error {
	if !api.ValidID(g.GrantID) || g.OwnerID != scope.OwnerID || g.Revision != 1 || g.State != "active" || g.SubjectRef.TenantID != scope.TenantID || g.SubjectRef.Revision == 0 || !api.ValidID(g.SubjectRef.ObjectID) {
		return api.E("invalid_request", "grant_scope_invalid")
	}
	if g.Mode != "once" && g.Mode != "continuous" {
		return api.E("invalid_request", "grant_mode_invalid")
	}
	for _, xs := range [][]string{g.Resources, g.Actions, g.Purposes, g.Recipients, g.Locations} {
		if !subset(xs, xs) {
			return api.E("invalid_request", "grant_scope_invalid")
		}
	}
	start, err := api.ParseTime(g.NotBefore)
	if err != nil {
		return api.E("invalid_request", "invalid_expiry")
	}
	end, err := api.ParseTime(g.ExpiresAt)
	if err != nil || !start.Before(end) {
		return api.E("invalid_request", "invalid_expiry")
	}
	return api.ValidateAmounts(g.Limits)
}

// ProvisionGrantTx 仅供受信宿主导入准确预置开发许可，公开方法不调用它。
// 宿主身份仍须 grant_authority；该事实不冒充本人 Confirmation。
func (s *Service) ProvisionGrantTx(ctx context.Context, tx runtime.Tx, auth runtime.Auth, g api.Grant) error {
	if err := requireRole(auth, "grant_authority"); err != nil {
		return err
	}
	if err := validateGrant(tx.Scope(), g); err != nil {
		return err
	}
	if err := tx.Create(ctx, ns("grants"), g.GrantID, g.SubjectRef.ObjectID, g); err != nil {
		return err
	}
	return tx.Create(ctx, ns("grant_usage"), g.GrantID, g.SubjectRef.ObjectID, GrantUsage{GrantID: g.GrantID, Revision: 1, Spent: []api.Amount{}, Reserved: []api.Amount{}})
}

// GrantExistsTx 供受信初始化沿本库原 ID 核对；撤回或已消费仍然存在。
// 不开放权限重置，宿主必须显式声明治理 participant 与准确 scope。
func (s *Service) GrantExistsTx(ctx context.Context, tx runtime.Tx, auth runtime.Auth, id string) (bool, error) {
	if err := requireRole(auth, "grant_authority"); err != nil {
		return false, err
	}
	if auth.TenantID != tx.Scope().TenantID || !api.ValidID(id) {
		return false, api.E("forbidden", "grant_management_scope_mismatch")
	}
	var grant api.Grant
	_, err := tx.Get(ctx, ns("grants"), id, &grant)
	if errMissing(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if grant.OwnerID != tx.Scope().OwnerID || grant.GrantID != id {
		return false, api.E("forbidden", "grant_management_scope_mismatch")
	}
	return true, nil
}

func (s *Service) beginConfirmed(ctx context.Context, tx runtime.Tx, auth runtime.Auth, c api.Command, previews []api.ContentRef, expires string) (*api.Confirmation, runtime.Outcome, error) {
	now, err := tx.Now(ctx)
	if err != nil {
		return nil, runtime.Outcome{}, err
	}
	if err = before(now, expires); err != nil {
		return nil, runtime.Outcome{}, err
	}
	if len(previews) == 0 {
		return nil, runtime.Outcome{}, api.E("invalid_request", "preview_required")
	}
	for _, p := range previews {
		if p.TenantID != auth.TenantID || p.Version == 0 || !api.ValidID(p.ContentID) {
			return nil, runtime.Outcome{}, api.E("forbidden", "preview_scope_mismatch")
		}
	}
	intent, err := api.Digest(c)
	if err != nil {
		return nil, runtime.Outcome{}, err
	}
	var pending PendingConfirmation
	_, err = tx.Get(ctx, ns("pending_confirmation"), c.CommandID, &pending)
	if errMissing(err) {
		id := digestID("confirmation", []string{c.CommandID, intent})
		confirm := api.Confirmation{RequestID: id, OwnerID: tx.Scope().OwnerID, Revision: 1, OriginalCommandID: c.CommandID, IntentHash: intent, PreviewRefs: previews, ExpiresAt: expires, State: "pending", Challenge: api.NewID("challenge"), TrustedUserSessionRef: auth.Ref(tx.Scope().OwnerID)}
		record := ConfirmationRecord{Confirmation: confirm, SubjectID: auth.SubjectID, CredentialGeneration: auth.CredentialGeneration, PendingCommandID: c.CommandID}
		if err = tx.Create(ctx, ns("confirmations"), id, auth.SubjectID, record); err != nil {
			return nil, runtime.Outcome{}, err
		}
		pending = PendingConfirmation{Command: c, SubjectID: auth.SubjectID, CredentialGeneration: auth.CredentialGeneration, Roles: auth.Roles, ConfirmationID: id}
		if err = tx.Create(ctx, ns("pending_confirmation"), c.CommandID, id, pending); err != nil {
			return nil, runtime.Outcome{}, err
		}
		if _, err = tx.Raise(ctx, "governance.confirmation", c.CommandID, tx.Scope().Ref(c.CommandID, 1), now); err != nil {
			return nil, runtime.Outcome{}, err
		}
		ref := tx.Scope().Ref(id, 1)
		return nil, runtime.Accepted(ConfirmedOutput{State: "pending_confirmation", ConfirmationRef: &ref}), nil
	}
	if err != nil {
		return nil, runtime.Outcome{}, err
	}
	var record ConfirmationRecord
	if _, err = tx.Get(ctx, ns("confirmations"), pending.ConfirmationID, &record); err != nil {
		return nil, runtime.Outcome{}, err
	}
	confirm := &record.Confirmation
	if record.SubjectID != auth.SubjectID || record.CredentialGeneration != auth.CredentialGeneration || confirm.IntentHash != intent || !api.Equal(confirm.PreviewRefs, previews) {
		return nil, runtime.Outcome{}, api.E("forbidden", "confirmation_stale")
	}
	if err = before(now, confirm.ExpiresAt); err != nil {
		return nil, runtime.Outcome{}, api.E("invalid_state", "confirmation_expired")
	}
	switch confirm.State {
	case "pending":
		ref := tx.Scope().Ref(confirm.RequestID, confirm.Revision)
		return nil, runtime.Accepted(ConfirmedOutput{State: "pending_confirmation", ConfirmationRef: &ref}), nil
	case "denied":
		return nil, runtime.Outcome{}, api.E("forbidden", "confirmation_denied")
	case "approved":
		return confirm, runtime.Outcome{}, nil
	case "consumed":
		return nil, runtime.Outcome{}, api.E("invalid_state", "confirmation_consumed")
	default:
		return nil, runtime.Outcome{}, api.E("invalid_state", "confirmation_expired")
	}
}

func (s *Service) consumeConfirmation(ctx context.Context, tx runtime.Tx, auth runtime.Auth, confirm *api.Confirmation, commandID string) error {
	var current ConfirmationRecord
	rev, err := tx.Get(ctx, ns("confirmations"), confirm.RequestID, &current)
	if err != nil {
		return err
	}
	if current.Confirmation.Revision != confirm.Revision || current.Confirmation.State != "approved" || current.Confirmation.OriginalCommandID != commandID {
		return api.E("forbidden", "confirmation_stale")
	}
	now, err := tx.Now(ctx)
	if err != nil {
		return err
	}
	if err = before(now, current.Confirmation.ExpiresAt); err != nil {
		return api.E("forbidden", "confirmation_expired")
	}
	if s.Ports.PreviewGate == nil {
		return api.E("unsupported", "confirmation_preview_gate_unavailable")
	}
	if err = s.Ports.PreviewGate.CheckTx(ctx, tx, auth, current.Confirmation.PreviewRefs); err != nil {
		if confirmationPreviewRejected(err) {
			return api.E("forbidden", "confirmation_stale")
		}
		return err
	}
	current.Confirmation.Revision = rev + 1
	current.Confirmation.State = "consumed"
	current.Confirmation.ConsumedBy = commandID
	current.Confirmation.ConsumedAt = api.Time(now)
	return tx.Put(ctx, ns("confirmations"), confirm.RequestID, rev, current)
}

// 当前预览的确定拒绝才令本人确认失效；暂时依赖或持久化异常不证明披露被撤回。
func confirmationPreviewRejected(err error) bool {
	return api.IsCode(err, "forbidden") || api.IsCode(err, "expired") || api.IsCode(err, "revision_conflict") || api.IsCode(err, "not_found") || api.IsCode(err, "gone") || api.IsCode(err, "invalid_state")
}

func (s *Service) issueGrant(ctx context.Context, tx runtime.Tx, auth runtime.Auth, c api.Command, in GrantIssue) (runtime.Outcome, error) {
	if err := requireRole(auth, "grant_authority"); err != nil {
		return runtime.Outcome{}, err
	}
	if err := validateGrant(tx.Scope(), in.Grant); err != nil {
		return runtime.Outcome{}, err
	}
	if in.Grant.GrantID != c.TargetID {
		return runtime.Outcome{}, api.E("invalid_request", "target_mismatch")
	}
	if len(in.ParentGrantRefs) > 32 {
		return runtime.Outcome{}, api.E("invalid_request", "parent_grant_limit")
	}
	confirm, pending, err := s.beginConfirmed(ctx, tx, auth, c, in.PreviewRefs, in.ConfirmationExpiresAt)
	if err != nil || confirm == nil {
		return pending, err
	}
	parentRefs, err := s.resolveGrantParents(ctx, tx, in.ParentGrantRefs)
	if err != nil {
		return runtime.Outcome{}, err
	}
	for _, pr := range parentRefs {
		if pr.ObjectID == in.Grant.GrantID {
			return runtime.Outcome{}, api.E("invalid_request", "grant_dependency_cycle")
		}
		if err = ownerRef(tx.Scope(), pr); err != nil {
			return runtime.Outcome{}, err
		}
		var parent api.Grant
		if _, err = tx.Get(ctx, ns("grants"), pr.ObjectID, &parent); err != nil {
			return runtime.Outcome{}, err
		}
		now, e := tx.Now(ctx)
		if e != nil {
			return runtime.Outcome{}, e
		}
		if parent.Revision != pr.Revision || parent.State != "active" || before(now, parent.ExpiresAt) != nil || !subset(in.Grant.Resources, parent.Resources) || !subset(in.Grant.Actions, parent.Actions) || !subset(in.Grant.Purposes, parent.Purposes) || !subset(in.Grant.Recipients, parent.Recipients) || !subset(in.Grant.Locations, parent.Locations) || !bounded(in.Grant.Limits, parent.Limits) {
			return runtime.Outcome{}, api.E("forbidden", "scope_exceeded")
		}
		if minTime(parent.ExpiresAt, in.Grant.ExpiresAt) != in.Grant.ExpiresAt {
			return runtime.Outcome{}, api.E("forbidden", "scope_exceeded")
		}
		start, e := api.ParseTime(parent.NotBefore)
		if e != nil {
			return runtime.Outcome{}, e
		}
		childStart, e := api.ParseTime(in.Grant.NotBefore)
		if e != nil {
			return runtime.Outcome{}, e
		}
		if childStart.Before(start) || !api.Equal(parent.SubjectRef, in.Grant.SubjectRef) || parent.Mode == "once" && in.Grant.Mode != "once" {
			return runtime.Outcome{}, api.E("forbidden", "scope_exceeded")
		}
	}
	if err = s.consumeConfirmation(ctx, tx, auth, confirm, c.CommandID); err != nil {
		return runtime.Outcome{}, err
	}
	if err = s.ProvisionGrantTx(ctx, tx, auth, in.Grant); err != nil {
		return runtime.Outcome{}, err
	}
	if err = tx.Create(ctx, ns("grant_parents"), in.Grant.GrantID, "", GrantParents{GrantID: in.Grant.GrantID, Refs: in.ParentGrantRefs}); err != nil {
		return runtime.Outcome{}, err
	}
	ref := tx.Scope().Ref(in.Grant.GrantID, 1)
	return runtime.Applied(ConfirmedOutput{Ref: &ref, State: "active"}), nil
}
func (s *Service) revokeGrant(ctx context.Context, tx runtime.Tx, auth runtime.Auth, c api.Command, in GrantRevoke) (runtime.Outcome, error) {
	if err := requireRole(auth, "grant_authority"); err != nil {
		return runtime.Outcome{}, err
	}
	if err := ownerRef(tx.Scope(), in.GrantRef); err != nil {
		return runtime.Outcome{}, err
	}
	if c.TargetID != in.GrantRef.ObjectID {
		return runtime.Outcome{}, api.E("invalid_request", "target_mismatch")
	}
	// 不可变出生版本仅用于路由；当前预览可能先锁 Memory 变更头及其来源许可。
	// 必须先取得这些上游门禁，再锁本次撤回的当前 Grant。初始 pending 同样核 CAS。
	var birth api.Grant
	if err := tx.GetVersion(ctx, ns("grants"), in.GrantRef.ObjectID, 1, &birth); err != nil {
		return runtime.Outcome{}, err
	}
	if birth.GrantID != in.GrantRef.ObjectID || birth.OwnerID != tx.Scope().OwnerID {
		return runtime.Outcome{}, api.E("forbidden", "grant_management_scope_mismatch")
	}
	if s.Ports.PreviewGate == nil {
		return runtime.Outcome{}, api.E("unsupported", "confirmation_preview_gate_unavailable")
	}
	if err := s.Ports.PreviewGate.CheckTx(ctx, tx, auth, in.PreviewRefs); err != nil {
		return runtime.Outcome{}, err
	}
	var g api.Grant
	rev, err := tx.Get(ctx, ns("grants"), in.GrantRef.ObjectID, &g)
	if err != nil {
		return runtime.Outcome{}, err
	}
	if err = requireCAS(c, rev); err != nil {
		return runtime.Outcome{}, err
	}
	if g.Revision != in.GrantRef.Revision {
		return runtime.Outcome{}, api.E("revision_conflict", "revision_changed")
	}
	confirm, pending, err := s.beginConfirmed(ctx, tx, auth, c, in.PreviewRefs, in.ConfirmationExpiresAt)
	if err != nil || confirm == nil {
		return pending, err
	}
	if err = s.consumeConfirmation(ctx, tx, auth, confirm, c.CommandID); err != nil {
		return runtime.Outcome{}, err
	}
	g.Revision = rev + 1
	g.State = "revoked"
	if err = tx.Put(ctx, ns("grants"), g.GrantID, rev, g); err != nil {
		return runtime.Outcome{}, err
	}
	ref := tx.Scope().Ref(g.GrantID, g.Revision)
	return runtime.Applied(ConfirmedOutput{Ref: &ref, State: "revoked"}), nil
}
func (s *Service) decideConfirmation(ctx context.Context, tx runtime.Tx, auth runtime.Auth, c api.Command, in ConfirmationDecision) (runtime.Outcome, error) {
	if err := requireRole(auth, "trusted_renderer"); err != nil {
		return runtime.Outcome{}, err
	}
	var record ConfirmationRecord
	rev, err := tx.Get(ctx, ns("confirmations"), in.RequestID, &record)
	if err != nil {
		return runtime.Outcome{}, err
	}
	confirm := &record.Confirmation
	if record.SubjectID != auth.SubjectID || record.CredentialGeneration != auth.CredentialGeneration || confirm.Challenge != in.Challenge {
		return runtime.Outcome{}, api.E("forbidden", "confirmation_session_mismatch")
	}
	if rev != in.RequestRevision || !api.Equal(in.PreviewRefs, confirm.PreviewRefs) {
		return runtime.Outcome{}, api.E("revision_conflict", "confirmation_stale")
	}
	if in.Decision != "approved" && in.Decision != "denied" {
		return runtime.Outcome{}, api.E("invalid_request", "invalid_confirmation_decision")
	}
	if confirm.State != "pending" {
		return runtime.Outcome{}, api.E("invalid_state", "confirmation_already_decided")
	}
	now, err := tx.Now(ctx)
	if err != nil {
		return runtime.Outcome{}, err
	}
	if err = before(now, confirm.ExpiresAt); err != nil {
		return runtime.Outcome{}, api.E("invalid_state", "confirmation_expired")
	}
	confirm.Revision = rev + 1
	confirm.State = in.Decision
	confirm.DecidedBy = auth.SubjectID
	confirm.DecidedAt = api.Time(now)
	if err = tx.Put(ctx, ns("confirmations"), in.RequestID, rev, record); err != nil {
		return runtime.Outcome{}, err
	}
	if _, err = tx.Raise(ctx, "governance.confirmation", record.PendingCommandID, tx.Scope().Ref(record.PendingCommandID, 1), now); err != nil {
		return runtime.Outcome{}, err
	}
	return runtime.Applied(StateOutput{Ref: tx.Scope().Ref(confirm.RequestID, confirm.Revision), State: confirm.State}), nil
}
func (s *Service) readConfirmation(ctx context.Context, store runtime.Store, scope runtime.Scope, auth runtime.Auth, q api.Query, in IDInput) (ConfirmationView, error) {
	var record ConfirmationRecord
	_, err := store.Read(ctx, scope, ns("confirmations"), in.ID, 0, &record)
	if err != nil {
		return ConfirmationView{}, err
	}
	if record.SubjectID != auth.SubjectID || record.CredentialGeneration != auth.CredentialGeneration {
		return ConfirmationView{}, api.E("forbidden", "confirmation_redacted")
	}
	c := record.Confirmation
	var pending PendingConfirmation
	if _, err = store.Read(ctx, scope, ns("pending_confirmation"), c.OriginalCommandID, 0, &pending); err != nil {
		return ConfirmationView{}, err
	}
	digest, err := api.Digest(pending.Command)
	if err != nil {
		return ConfirmationView{}, err
	}
	if digest != c.IntentHash || pending.SubjectID != auth.SubjectID || pending.CredentialGeneration != auth.CredentialGeneration {
		return ConfirmationView{}, api.E("forbidden", "original_confirmation_input_unavailable")
	}
	return ConfirmationView{RequestID: c.RequestID, Revision: c.Revision, OriginalCommandID: c.OriginalCommandID, IntentHash: c.IntentHash, PreviewRefs: c.PreviewRefs, ExpiresAt: c.ExpiresAt, State: c.State, Challenge: c.Challenge, TrustedUserSessionRef: c.TrustedUserSessionRef, ConsumedBy: c.ConsumedBy, ConsumedAt: c.ConsumedAt, DecidedBy: c.DecidedBy, DecidedAt: c.DecidedAt, OriginalCommand: pending.Command}, nil
}

func (s *Service) registerConfirmationRead(r *runtime.Registry) error {
	contract := api.Contract[IDInput, ConfirmationView]("confirmation.read", Namespace, "query", false, false)
	branches := []any{}
	for _, entry := range []struct {
		name   string
		schema api.Schema
	}{{"grant.issue", api.SchemaFor[GrantIssue]()}, {"grant.revoke", api.SchemaFor[GrantRevoke]()}, {"policy.acceptance.create", api.SchemaFor[AcceptanceCreate]()}, {"release.approval.create", api.SchemaFor[ApprovalCreate]()}} {
		commandSchema := api.SchemaFor[api.Command]()
		properties := commandSchema["properties"].(map[string]any)
		properties["method"] = api.Schema{"const": entry.name}
		properties["payload"] = entry.schema
		branches = append(branches, commandSchema)
	}
	contract.OutputSchema["properties"].(map[string]any)["original_command"] = api.Schema{"oneOf": branches}
	return r.Register(runtime.Method{Contract: contract, Query: func(ctx context.Context, store runtime.Store, scope runtime.Scope, auth runtime.Auth, q api.Query) (any, error) {
		var in IDInput
		if err := api.Decode(q.Payload, &in); err != nil {
			return nil, err
		}
		return s.readConfirmation(ctx, store, scope, auth, q, in)
	}})
}
func (s *Service) continueConfirmation(ctx context.Context, store runtime.Store, scope runtime.Scope, work runtime.Work) error {
	var pending PendingConfirmation
	if _, err := store.Read(ctx, scope, ns("pending_confirmation"), work.Job.ResponsibilityKey, 0, &pending); err != nil {
		return err
	}
	m, ok := s.registry.Method(pending.Command.Method)
	if !ok {
		return api.E("unsupported", "method_not_supported")
	}
	return finish(ctx, store, scope, s.participants(), work, runtime.Done(), func(tx runtime.Tx) error {
		auth := runtime.Auth{TenantID: scope.TenantID, SubjectID: pending.SubjectID, CredentialGeneration: pending.CredentialGeneration, Roles: pending.Roles}
		old, err := tx.LoadCommand(ctx, pending.Command.CommandID)
		if err != nil {
			return err
		}
		if old.Receipt.Stage != "accepted" {
			return nil
		}
		var outcome runtime.Outcome
		var applyErr error
		applyErr = tx.Savepoint(ctx, func(inner runtime.Tx) error {
			outcome, applyErr = m.Apply(ctx, inner, auth, pending.Command)
			return applyErr
		})
		if applyErr != nil {
			var e *api.Error
			if !errors.As(applyErr, &e) || e.Code == "dependency_unavailable" || e.Code == "overloaded" || e.Code == "effect_unknown" || e.Code == "accounting_unknown" {
				return applyErr
			}
			return runtime.Decide(ctx, tx, pending.Command.CommandID, nil, e)
		}
		if outcome.Stage == "accepted" {
			var rec ConfirmationRecord
			if _, err = tx.Get(ctx, ns("confirmations"), pending.ConfirmationID, &rec); err != nil {
				return err
			}
			at, err := api.ParseTime(rec.Confirmation.ExpiresAt)
			if err != nil {
				return err
			}
			return tx.Hint(ctx, work.Job.JobID, at)
		}
		return runtime.Decide(ctx, tx, pending.Command.CommandID, outcome.Output, nil)
	})
}

func (s *Service) readGrant(ctx context.Context, store runtime.Store, scope runtime.Scope, auth runtime.Auth, q api.Query, in IDInput) (GrantRecord, error) {
	var g api.Grant
	if _, err := store.Read(ctx, scope, ns("grants"), in.ID, 0, &g); err != nil {
		return GrantRecord{}, err
	}
	if g.SubjectRef.ObjectID != auth.SubjectID && !auth.HasRole("grant_authority") {
		return GrantRecord{}, api.E("forbidden", "grant_redacted")
	}
	var usage GrantUsage
	if _, err := store.Read(ctx, scope, ns("grant_usage"), in.ID, 0, &usage); err != nil {
		return GrantRecord{}, err
	}
	return GrantRecord{Grant: g, OnceConsumed: usage.OnceConsumed, Spent: usage.Spent, Reserved: usage.Reserved}, nil
}

func validateUse(scope runtime.Scope, auth runtime.Auth, in UseRequest) error {
	if !api.ValidID(in.UseID) || in.IntentHash == "" || in.SubjectRef.TenantID != scope.TenantID || in.SubjectRef.ObjectID != auth.SubjectID || in.SubjectRef.Revision != auth.CredentialGeneration || in.SubjectRef.OwnerID != scope.OwnerID {
		return api.E("forbidden", "use_subject_mismatch")
	}
	if err := runtime.CheckRef(scope, in.TargetRef); err != nil {
		return err
	}
	if in.TargetKind != "operation" && in.TargetKind != "decision" && in.TargetKind != "content_use" {
		return api.E("invalid_request", "invalid_use_target")
	}
	if len(in.GrantRefs) == 0 || len(in.GrantRefs) > 32 || !subset(in.Resources, in.Resources) || !subset(in.Actions, in.Actions) || !subset(in.Purposes, in.Purposes) || in.Recipient == "" || in.Location == "" {
		return api.E("invalid_request", "use_scope_invalid")
	}
	return api.ValidateAmounts(in.RequestedUnits)
}

// 原父边永久保留。使用时解析完整交集，并按稳定 grant ID 锁当前许可和账本。
// 这样父撤回立即阻断子许可，once 与金额预留也无法经兄弟子许可复制。
func (s *Service) resolveGrantParents(ctx context.Context, tx runtime.Tx, roots []api.ObjectRef) ([]api.ObjectRef, error) {
	seen := map[string]api.ObjectRef{}
	active := map[string]bool{}
	var visit func(api.ObjectRef, int) error
	visit = func(ref api.ObjectRef, depth int) error {
		if depth > 8 {
			return api.E("invalid_request", "grant_dependency_depth")
		}
		if err := ownerRef(tx.Scope(), ref); err != nil {
			return err
		}
		if active[ref.ObjectID] {
			return api.E("invalid_request", "grant_dependency_cycle")
		}
		if old, ok := seen[ref.ObjectID]; ok {
			if !api.Equal(old, ref) {
				return api.E("invalid_request", "grant_revision_conflict")
			}
			return nil
		}
		if len(seen) >= 128 {
			return api.E("invalid_request", "grant_dependency_limit")
		}
		seen[ref.ObjectID] = ref
		active[ref.ObjectID] = true
		var parents GrantParents
		err := tx.GetVersion(ctx, ns("grant_parents"), ref.ObjectID, 1, &parents)
		if err != nil && !errMissing(err) {
			return err
		}
		if err == nil {
			for _, parent := range parents.Refs {
				if e := visit(parent, depth+1); e != nil {
					return e
				}
			}
		}
		active[ref.ObjectID] = false
		return nil
	}
	rootSeen := map[string]bool{}
	for _, root := range roots {
		if rootSeen[root.ObjectID] {
			return nil, api.E("invalid_request", "duplicate_grant")
		}
		rootSeen[root.ObjectID] = true
		if err := visit(root, 1); err != nil {
			return nil, err
		}
	}
	refs := make([]api.ObjectRef, 0, len(seen))
	for _, ref := range seen {
		refs = append(refs, ref)
	}
	sort.Slice(refs, func(i, j int) bool { return refs[i].ObjectID < refs[j].ObjectID })
	return refs, nil
}
func (s *Service) checkUse(ctx context.Context, tx runtime.Tx, auth runtime.Auth, in UseRequest, consume bool) (UseReceipt, error) {
	if err := validateUse(tx.Scope(), auth, in); err != nil {
		return UseReceipt{}, err
	}
	now, err := tx.Now(ctx)
	if err != nil {
		return UseReceipt{}, err
	}
	digest, err := api.Digest(in)
	if err != nil {
		return UseReceipt{}, err
	}
	out := UseReceipt{UseID: in.UseID, SubjectRef: in.SubjectRef, TargetRef: in.TargetRef, TargetKind: in.TargetKind, IntentHash: in.IntentHash, RequestDigest: digest, GrantRefs: in.GrantRefs, Decision: "allowed", Reserved: in.RequestedUnits, CostBound: in.RequestedUnits, Recipient: in.Recipient, Location: in.Location, Purposes: in.Purposes, IssuedAt: api.Time(now), StartBefore: in.StartBefore}
	if before(now, in.StartBefore) != nil {
		out.Decision = "denied"
		out.Reason = "window_expired"
	}
	refs, err := s.resolveGrantParents(ctx, tx, in.GrantRefs)
	if err != nil {
		return UseReceipt{}, err
	}
	out.GrantRefs = refs
	seen := map[string]bool{}
	type update struct {
		usage    GrantUsage
		revision uint64
	}
	updates := []update{}
	for _, gr := range refs {
		if seen[gr.ObjectID] {
			return UseReceipt{}, api.E("invalid_request", "duplicate_grant")
		}
		seen[gr.ObjectID] = true
		if err = ownerRef(tx.Scope(), gr); err != nil {
			return UseReceipt{}, err
		}
		var grant api.Grant
		_, err = tx.Get(ctx, ns("grants"), gr.ObjectID, &grant)
		if err != nil {
			return UseReceipt{}, err
		}
		var usage GrantUsage
		rev, err := tx.Get(ctx, ns("grant_usage"), gr.ObjectID, &usage)
		if err != nil {
			return UseReceipt{}, err
		}
		out.StartBefore = minTime(out.StartBefore, grant.ExpiresAt)
		start, e := api.ParseTime(grant.NotBefore)
		if e != nil {
			return UseReceipt{}, e
		}
		if grant.Revision != gr.Revision || grant.State != "active" || before(now, grant.ExpiresAt) != nil || now.Before(start) || grant.SubjectRef.ObjectID != auth.SubjectID || grant.SubjectRef.Revision != auth.CredentialGeneration || !subset(in.Resources, grant.Resources) || !subset(in.Actions, grant.Actions) || !subset(in.Purposes, grant.Purposes) || !contains(grant.Recipients, in.Recipient) || !contains(grant.Locations, in.Location) {
			out.Decision = "denied"
			out.Reason = "scope_exceeded"
		}
		if grant.Mode == "once" && usage.OnceConsumed {
			out.Decision = "denied"
			out.Reason = "once_consumed"
		}
		total, e := amountsAdd(usage.Spent, usage.Reserved)
		if e != nil {
			return UseReceipt{}, e
		}
		total, e = amountsAdd(total, in.RequestedUnits)
		if e != nil {
			return UseReceipt{}, e
		}
		if !bounded(total, grant.Limits) {
			out.Decision = "denied"
			out.Reason = "scope_exceeded"
		}
		updates = append(updates, update{usage, rev})
	}
	if out.Decision == "allowed" && consume {
		for _, u := range updates {
			var g api.Grant
			if _, err = tx.Get(ctx, ns("grants"), u.usage.GrantID, &g); err != nil {
				return UseReceipt{}, err
			}
			u.usage.Revision = u.revision + 1
			if g.Mode == "once" {
				u.usage.OnceConsumed = true
			}
			u.usage.Reserved, err = amountsAdd(u.usage.Reserved, in.RequestedUnits)
			if err != nil {
				return UseReceipt{}, err
			}
			if err = tx.Put(ctx, ns("grant_usage"), u.usage.GrantID, u.revision, u.usage); err != nil {
				return UseReceipt{}, err
			}
		}
	} else if out.Decision == "denied" {
		out.Reserved = []api.Amount{}
	}
	return out, nil
}
func (s *Service) UseTx(ctx context.Context, tx runtime.Tx, auth runtime.Auth, in UseRequest) (UseReceipt, error) {
	var old UseReceipt
	_, err := tx.Get(ctx, ns("uses"), in.UseID, &old)
	if err == nil {
		digest, e := api.Digest(in)
		if e != nil {
			return UseReceipt{}, e
		}
		if old.RequestDigest != digest || old.SubjectRef.ObjectID != auth.SubjectID {
			return UseReceipt{}, api.E("idempotency_conflict", "use_intent_mismatch")
		}
		return old, nil
	}
	if !errMissing(err) {
		return UseReceipt{}, err
	}
	out, err := s.checkUse(ctx, tx, auth, in, true)
	if err != nil {
		return out, err
	}
	if s.Ports.Proof != nil && out.Decision == "allowed" {
		proofDigest, digestErr := UseReceiptDigest(out)
		if digestErr != nil {
			return out, digestErr
		}
		out.Proof, err = s.Ports.Proof.SignLocal(ProofStatement{TenantID: tx.Scope().TenantID, IssuerID: tx.Scope().OwnerID, AudienceID: in.TargetRef.OwnerID, Purpose: "grant_use", ObjectRef: tx.Scope().Ref(in.UseID, 1), Digest: proofDigest, IssuedAt: out.IssuedAt, StartBefore: out.StartBefore})
		if err != nil {
			return out, err
		}
	}
	if err = tx.Create(ctx, ns("uses"), in.UseID, in.TargetRef.ObjectID, out); err != nil {
		return out, err
	}
	settlement := UseSettlement{UseID: in.UseID, Revision: 1, CumulativeUsage: []api.Amount{}, SourceRefs: []api.ObjectRef{}, EvidenceRefs: []api.ContentRef{}, RemainingReserved: out.Reserved}
	if out.Decision == "denied" {
		settlement.SpendingClosed = true
		settlement.UsageFinal = true
	}
	if err = tx.Create(ctx, ns("settlements"), in.UseID, in.TargetRef.ObjectID, settlement); err != nil {
		return out, err
	}
	return out, nil
}

// UseReceiptDigest 包含完整父许可链、当前决定和原请求摘要，排除签名承载字段。
func UseReceiptDigest(out UseReceipt) (string, error) {
	out.Proof = ""
	out.ProofRef = nil
	return api.Digest(out)
}
func (s *Service) checkGrantQuery(ctx context.Context, store runtime.Store, scope runtime.Scope, auth runtime.Auth, q api.Query, in UseRequest) (UseReceipt, error) {
	var out UseReceipt
	status, err := store.Within(ctx, scope, s.participants(), func(tx runtime.Tx) error { var err error; out, err = s.checkUse(ctx, tx, auth, in, false); return err })
	if status == runtime.CommitUnknown {
		return UseReceipt{}, runtime.ErrCommitUnknown
	}
	return out, err
}

// CheckUseHeadsTx 只核原 Use 和当前完整 Grant 链，供宿主另核有限数据保留期。
// 它不授予新出口，不消费 once，不延长原 StartBefore；开始仍须 CheckUseTx。
func (s *Service) CheckUseHeadsTx(ctx context.Context, tx runtime.Tx, auth runtime.Auth, ref, target api.ObjectRef, intentHash string) (UseReceipt, error) {
	if auth.TenantID != tx.Scope().TenantID {
		return UseReceipt{}, api.E("forbidden", "tenant_mismatch")
	}
	if err := ownerRef(tx.Scope(), ref); err != nil {
		return UseReceipt{}, err
	}
	if err := runtime.CheckRef(tx.Scope(), target); err != nil {
		return UseReceipt{}, err
	}
	var use UseReceipt
	if _, err := tx.Get(ctx, ns("uses"), ref.ObjectID, &use); err != nil {
		return UseReceipt{}, err
	}
	if ref.Revision != 1 || use.Decision != "allowed" || use.SubjectRef.TenantID != auth.TenantID || use.SubjectRef.OwnerID != tx.Scope().OwnerID || use.SubjectRef.ObjectID != auth.SubjectID || use.SubjectRef.Revision != auth.CredentialGeneration || !api.Equal(use.TargetRef, target) || use.IntentHash != intentHash {
		return UseReceipt{}, api.E("forbidden", "use_binding_mismatch")
	}
	now, err := tx.Now(ctx)
	if err != nil {
		return UseReceipt{}, err
	}
	for _, ref := range use.GrantRefs {
		if err = ownerRef(tx.Scope(), ref); err != nil {
			return UseReceipt{}, err
		}
		var grant api.Grant
		if _, err = tx.Get(ctx, ns("grants"), ref.ObjectID, &grant); err != nil {
			return UseReceipt{}, err
		}
		start, e := api.ParseTime(grant.NotBefore)
		if e != nil || now.Before(start) || grant.State != "active" || grant.Revision != ref.Revision || before(now, grant.ExpiresAt) != nil {
			return UseReceipt{}, api.E("forbidden", "authorization_changed")
		}
	}
	return use, nil
}
func (s *Service) CheckUseTx(ctx context.Context, tx runtime.Tx, auth runtime.Auth, ref, target api.ObjectRef, intentHash string, now time.Time) error {
	use, err := s.CheckUseHeadsTx(ctx, tx, auth, ref, target, intentHash)
	if err != nil {
		return err
	}
	return before(now, use.StartBefore)
}

func (s *Service) requestSettlement(ctx context.Context, tx runtime.Tx, auth runtime.Auth, c api.Command, in SettleRequest) (runtime.Outcome, error) {
	var use UseReceipt
	if _, err := tx.Get(ctx, ns("uses"), in.UseID, &use); err != nil {
		return runtime.Outcome{}, err
	}
	if !auth.HasRole("usage_reporter") {
		return runtime.Outcome{}, api.E("forbidden", "trusted_usage_reporter_required")
	}
	if s.Ports.UsageVerifier == nil {
		return runtime.Outcome{}, api.E("unsupported", "settlement_verifier_unavailable")
	}
	if in.Usage.SourceRef.TenantID != tx.Scope().TenantID || in.Usage.SourceRef.OwnerID != use.TargetRef.OwnerID || in.Usage.SourceRef.ObjectID != use.TargetRef.ObjectID {
		return runtime.Outcome{}, api.E("forbidden", "settlement_unverified")
	}
	if err := api.ValidateAmounts(in.Usage.Cumulative); err != nil {
		return runtime.Outcome{}, err
	}
	id := c.CommandID
	pending := SettlePending{ID: id, CommandID: c.CommandID, Request: in}
	if err := tx.Create(ctx, ns("settle_pending"), id, in.UseID, pending); err != nil {
		return runtime.Outcome{}, err
	}
	now, err := tx.Now(ctx)
	if err != nil {
		return runtime.Outcome{}, err
	}
	if _, err = tx.Raise(ctx, "governance.settle", id, tx.Scope().Ref(id, 1), now); err != nil {
		return runtime.Outcome{}, err
	}
	var out UseSettlement
	if _, err = tx.Get(ctx, ns("settlements"), in.UseID, &out); err != nil {
		return runtime.Outcome{}, err
	}
	return runtime.Accepted(out), nil
}
func (s *Service) ApplySettlementTx(ctx context.Context, tx runtime.Tx, in SettleRequest) (UseSettlement, error) {
	var use UseReceipt
	if _, err := tx.Get(ctx, ns("uses"), in.UseID, &use); err != nil {
		return UseSettlement{}, err
	}
	var leaseLink LeaseUseLink
	if _, e := tx.Get(ctx, ns("lease_use_links"), in.UseID, &leaseLink); e == nil {
		return s.applyLeaseUseSettlement(ctx, tx, use, in.Usage)
	} else if !errMissing(e) {
		return UseSettlement{}, e
	}
	var old UseSettlement
	rev, err := tx.Get(ctx, ns("settlements"), in.UseID, &old)
	if err != nil {
		return old, err
	}
	u := in.Usage
	if u.SourceRef.TenantID != tx.Scope().TenantID || u.SourceRef.OwnerID != use.TargetRef.OwnerID || u.SourceRef.ObjectID != use.TargetRef.ObjectID || u.UsageRevision == 0 || u.UsageDigest == "" {
		return old, api.E("forbidden", "settlement_unverified")
	}
	if u.UsageRevision < old.SourceRevision {
		return old, nil
	}
	if u.UsageRevision == old.SourceRevision {
		if old.SourceDigest != u.UsageDigest {
			return old, api.E("idempotency_conflict", "usage_revision_changed")
		}
		return old, nil
	}
	if !amountsMonotone(old.CumulativeUsage, u.Cumulative) || old.SpendingClosed && !u.SpendingClosed || old.UsageFinal && !u.UsageFinal {
		return old, api.E("invalid_state", "usage_regressed")
	}
	newRemaining, err := amountsRemaining(use.Reserved, u.Cumulative)
	if err != nil {
		return old, err
	}
	if u.SpendingClosed && u.UsageFinal {
		newRemaining = []api.Amount{}
	}
	for _, gr := range use.GrantRefs {
		var grant api.Grant
		if _, err = tx.Get(ctx, ns("grants"), gr.ObjectID, &grant); err != nil {
			return old, err
		}
		var gu GrantUsage
		grev, err := tx.Get(ctx, ns("grant_usage"), gr.ObjectID, &gu)
		if err != nil {
			return old, err
		}
		delta, err := amountsSubtract(u.Cumulative, old.CumulativeUsage)
		if err != nil {
			return old, err
		}
		gu.Spent, err = amountsAdd(gu.Spent, delta)
		if err != nil {
			return old, err
		}
		release, err := amountsSubtract(old.RemainingReserved, newRemaining)
		if err != nil {
			return old, err
		}
		gu.Reserved, err = amountsSubtract(gu.Reserved, release)
		if err != nil {
			return old, err
		}
		gu.Revision = grev + 1
		if err = tx.Put(ctx, ns("grant_usage"), gr.ObjectID, grev, gu); err != nil {
			return old, err
		}
	}
	old.Revision = rev + 1
	old.CumulativeUsage = u.Cumulative
	old.SpendingClosed = u.SpendingClosed
	old.UsageFinal = u.UsageFinal
	old.SourceRevision = u.UsageRevision
	old.SourceDigest = u.UsageDigest
	old.SourceRefs = []api.ObjectRef{u.SourceRef}
	old.EvidenceRefs = u.ProofRefs
	old.RemainingReserved = newRemaining
	err = tx.Put(ctx, ns("settlements"), in.UseID, rev, old)
	return old, err
}
func (s *Service) continueSettlement(ctx context.Context, store runtime.Store, scope runtime.Scope, work runtime.Work) error {
	var pending SettlePending
	if _, err := store.Read(ctx, scope, ns("settle_pending"), work.Job.ResponsibilityKey, 0, &pending); err != nil {
		return err
	}
	var use UseReceipt
	if _, err := store.Read(ctx, scope, ns("uses"), pending.Request.UseID, 0, &use); err != nil {
		return err
	}
	if err := s.Ports.UsageVerifier.Verify(ctx, scope, use.TargetRef, pending.Request.Usage); err != nil {
		if !usageVerificationRejected(err) {
			// 原来源尚不可核验，保留同一 accepted 命令和待结算责任。
			return err
		}
		return finish(ctx, store, scope, s.participants(), work, runtime.Done(), func(tx runtime.Tx) error {
			if _, err := tx.LoadCommand(ctx, pending.CommandID); err != nil {
				return err
			}
			return runtime.Decide(ctx, tx, pending.CommandID, nil, api.E("forbidden", "settlement_unverified"))
		})
	}
	return finish(ctx, store, scope, s.participants(), work, runtime.Done(), func(tx runtime.Tx) error {
		if _, err := tx.LoadCommand(ctx, pending.CommandID); err != nil {
			return err
		}
		out, err := s.ApplySettlementTx(ctx, tx, pending.Request)
		if err != nil {
			return err
		}
		return runtime.Decide(ctx, tx, pending.CommandID, out, nil)
	})
}

// 只有核验器明确确认原冻结证明非法，才裁决终态拒绝。
// 依赖、效果、context 和持久化异常不证明用量为假。
func usageVerificationRejected(err error) bool {
	return api.IsCode(err, "forbidden") || api.IsCode(err, "invalid_request")
}
func (s *Service) readSettlement(ctx context.Context, store runtime.Store, scope runtime.Scope, auth runtime.Auth, q api.Query, in IDInput) (UseSettlement, error) {
	var use UseReceipt
	if _, err := store.Read(ctx, scope, ns("uses"), in.ID, 0, &use); err != nil {
		return UseSettlement{}, err
	}
	if use.SubjectRef.ObjectID != auth.SubjectID && !auth.HasRole("grant_authority") {
		return UseSettlement{}, api.E("forbidden", "settlement_redacted")
	}
	var out UseSettlement
	_, err := store.Read(ctx, scope, ns("settlements"), in.ID, 0, &out)
	return out, err
}

func (s *Service) createAcceptance(ctx context.Context, tx runtime.Tx, auth runtime.Auth, c api.Command, in AcceptanceCreate) (runtime.Outcome, error) {
	if err := requireRole(auth, "grant_authority"); err != nil {
		return runtime.Outcome{}, err
	}
	if !in.Scope.NonHardLimitAcknowledged || in.Scope.ReservationMethod == "" || len(in.Scope.Units) == 0 {
		return runtime.Outcome{}, api.E("invalid_request", "estimate_risk_acceptance_required")
	}
	if in.AcceptanceID != c.TargetID || in.ScopeRef.TenantID != tx.Scope().TenantID || in.ExplanationRef.TenantID != tx.Scope().TenantID {
		return runtime.Outcome{}, api.E("forbidden", "acceptance_scope_mismatch")
	}
	scopeSeen, explanationSeen := false, false
	for _, preview := range in.PreviewRefs {
		scopeSeen = scopeSeen || api.Equal(preview, in.ScopeRef)
		explanationSeen = explanationSeen || api.Equal(preview, in.ExplanationRef)
	}
	if !scopeSeen || !explanationSeen {
		return runtime.Outcome{}, api.E("invalid_request", "acceptance_preview_incomplete")
	}
	if len(in.Scope.TaskRefs) == 0 || len(in.Scope.TaskRefs) > 128 || len(in.Scope.CapabilityRefs) == 0 || len(in.Scope.CapabilityRefs) > 32 || !subset(in.Scope.Units, in.Scope.Units) {
		return runtime.Outcome{}, api.E("invalid_request", "acceptance_scope_invalid")
	}
	for _, task := range in.Scope.TaskRefs {
		if err := ownerRef(tx.Scope(), task); err != nil {
			return runtime.Outcome{}, api.E("unsupported", "estimate_requires_same_owner_transaction")
		}
	}
	if err := api.ValidateAmounts(in.Scope.Budget); err != nil {
		return runtime.Outcome{}, err
	}
	confirm, pending, err := s.beginConfirmed(ctx, tx, auth, c, in.PreviewRefs, in.ExpiresAt)
	if err != nil || confirm == nil {
		return pending, err
	}
	if err = s.consumeConfirmation(ctx, tx, auth, confirm, c.CommandID); err != nil {
		return runtime.Outcome{}, err
	}
	out := PolicyAcceptance{AcceptanceID: in.AcceptanceID, Revision: 1, SubjectRef: auth.Ref(tx.Scope().OwnerID), PolicyRef: in.PolicyRef, ScopeRef: in.ScopeRef, Scope: in.Scope, ExplanationRef: in.ExplanationRef, ConfirmationRef: tx.Scope().Ref(confirm.RequestID, confirm.Revision+1), ExpiresAt: in.ExpiresAt, State: "active"}
	if err = tx.Create(ctx, ns("acceptances"), in.AcceptanceID, auth.SubjectID, out); err != nil {
		return runtime.Outcome{}, err
	}
	ref := tx.Scope().Ref(in.AcceptanceID, 1)
	return runtime.Applied(ConfirmedOutput{Ref: &ref, State: "active"}), nil
}
func (s *Service) revokeAcceptance(ctx context.Context, tx runtime.Tx, auth runtime.Auth, c api.Command, in RefInput) (runtime.Outcome, error) {
	if err := ownerRef(tx.Scope(), in.Ref); err != nil {
		return runtime.Outcome{}, err
	}
	var out PolicyAcceptance
	rev, err := tx.Get(ctx, ns("acceptances"), in.Ref.ObjectID, &out)
	if err != nil {
		return runtime.Outcome{}, err
	}
	if out.SubjectRef.ObjectID != auth.SubjectID {
		return runtime.Outcome{}, api.E("forbidden", "acceptance_owner_required")
	}
	if err = requireCAS(c, rev); err != nil {
		return runtime.Outcome{}, err
	}
	out.Revision = rev + 1
	out.State = "revoked"
	if err = tx.Put(ctx, ns("acceptances"), in.Ref.ObjectID, rev, out); err != nil {
		return runtime.Outcome{}, err
	}
	return runtime.Applied(StateOutput{Ref: tx.Scope().Ref(in.Ref.ObjectID, out.Revision), State: out.State}), nil
}
func (s *Service) CheckAcceptanceTx(ctx context.Context, tx runtime.Tx, in AcceptanceCheck) error {
	if in.Mode == "strict" {
		return nil
	}
	if in.Mode != "estimate" {
		return api.E("invalid_request", "unsupported_cost_mode")
	}
	if err := ownerRef(tx.Scope(), in.TaskRef); err != nil {
		return api.E("unsupported", "estimate_requires_same_owner_transaction")
	}
	if err := api.ValidateAmounts(in.Requested); err != nil {
		return err
	}
	if err := ownerRef(tx.Scope(), in.AcceptanceRef); err != nil {
		return api.E("unsupported", "estimate_requires_same_owner_transaction")
	}
	var p PolicyAcceptance
	if _, err := tx.Get(ctx, ns("acceptances"), in.AcceptanceRef.ObjectID, &p); err != nil {
		return err
	}
	now, err := tx.Now(ctx)
	if err != nil {
		return err
	}
	if p.Revision != in.AcceptanceRef.Revision || p.State != "active" || !api.Equal(p.SubjectRef, in.SubjectRef) || !api.Equal(p.PolicyRef, in.PolicyRef) || before(now, p.ExpiresAt) != nil {
		return api.E("forbidden", "policy_acceptance_invalid")
	}
	allowedTask := false
	for _, tr := range p.Scope.TaskRefs {
		if tr.TenantID == in.TaskRef.TenantID && tr.OwnerID == in.TaskRef.OwnerID && tr.ObjectID == in.TaskRef.ObjectID {
			allowedTask = true
		}
	}
	allowedCapability := false
	for _, cr := range p.Scope.CapabilityRefs {
		if api.Equal(cr, in.CapabilityRef) {
			allowedCapability = true
		}
	}
	if !allowedTask || !allowedCapability || !bounded(in.Requested, p.Scope.Budget) {
		return api.E("forbidden", "scope_exceeded")
	}
	for _, a := range in.Requested {
		if !contains(p.Scope.Units, a.Unit) {
			return api.E("forbidden", "scope_exceeded")
		}
	}
	return nil
}

// AllocateLeaseTx 供宿主在已显式声明的同库准入事务中预留原有限 lease。
// 签名计算纯本地；调用者不能另做 UseTx 来重复消费 once 或重复预留。
func (s *Service) AllocateLeaseTx(ctx context.Context, tx runtime.Tx, auth runtime.Auth, in LeaseAllocate) (GrantLease, error) {
	if auth.TenantID != tx.Scope().TenantID {
		return GrantLease{}, api.E("forbidden", "tenant_mismatch")
	}
	validator, err := api.NewValidator(api.SchemaFor[LeaseAllocate]())
	if err != nil {
		return GrantLease{}, err
	}
	if err = validator.Validate(api.Raw(in)); err != nil {
		return GrantLease{}, err
	}
	if err := requireRole(auth, "grant_authority"); err != nil {
		return GrantLease{}, err
	}
	if in.CostMode != "strict" {
		return GrantLease{}, api.E("unsupported", "offline_estimate_not_supported")
	}
	if !api.ValidID(in.EndpointID) || !api.ValidID(in.InstanceID) || !api.ValidID(in.LeaseID) {
		return GrantLease{}, api.E("invalid_request", "lease_endpoint_invalid")
	}
	request := in.Scope
	request.UseID = in.LeaseID
	request.RequestedUnits = in.Limits
	request.StartBefore = in.ExpiresAt
	use, err := s.UseTx(ctx, tx, auth, request)
	if err != nil {
		return GrantLease{}, err
	}
	if use.Decision != "allowed" {
		return GrantLease{}, api.E("forbidden", use.Reason)
	}
	request.GrantRefs = use.GrantRefs
	out := GrantLease{LeaseID: in.LeaseID, Revision: 1, EndpointID: in.EndpointID, InstanceID: in.InstanceID, GrantRefs: request.GrantRefs, Scope: request, Limits: in.Limits, ExpiresAt: use.StartBefore, State: "open", Cumulative: []api.Amount{}, Reserved: in.Limits}
	out.Mode = "continuous"
	out.IssuedAt = use.IssuedAt
	for _, gr := range request.GrantRefs {
		var grant api.Grant
		if _, err = tx.Get(ctx, ns("grants"), gr.ObjectID, &grant); err != nil {
			return GrantLease{}, err
		}
		if grant.Mode == "once" {
			out.Mode = "once"
		}
	}
	out.AllocationDigest, err = api.Digest(out)
	if err != nil {
		return GrantLease{}, err
	}
	if s.Ports.Proof != nil {
		out.Proof, err = s.Ports.Proof.SignLocal(ProofStatement{TenantID: tx.Scope().TenantID, IssuerID: tx.Scope().OwnerID, AudienceID: in.EndpointID, Purpose: "grant_lease", ObjectRef: tx.Scope().Ref(in.LeaseID, 1), Digest: out.AllocationDigest, IssuedAt: out.IssuedAt, StartBefore: out.ExpiresAt})
		if err != nil {
			return GrantLease{}, err
		}
	}
	if err = tx.Create(ctx, ns("leases"), in.LeaseID, in.EndpointID, out); err != nil {
		return GrantLease{}, err
	}
	return out, nil
}
func (s *Service) allocateLease(ctx context.Context, tx runtime.Tx, auth runtime.Auth, c api.Command, in LeaseAllocate) (runtime.Outcome, error) {
	if in.LeaseID != c.TargetID {
		return runtime.Outcome{}, api.E("invalid_request", "lease_endpoint_invalid")
	}
	out, err := s.AllocateLeaseTx(ctx, tx, auth, in)
	return runtime.Applied(out), err
}
func (s *Service) closeLease(ctx context.Context, tx runtime.Tx, auth runtime.Auth, c api.Command, in RefInput) (runtime.Outcome, error) {
	if err := requireRole(auth, "grant_authority"); err != nil {
		return runtime.Outcome{}, err
	}
	if err := ownerRef(tx.Scope(), in.Ref); err != nil {
		return runtime.Outcome{}, err
	}
	var out GrantLease
	rev, err := tx.Get(ctx, ns("leases"), in.Ref.ObjectID, &out)
	if err != nil {
		return runtime.Outcome{}, err
	}
	if err = requireCAS(c, rev); err != nil {
		return runtime.Outcome{}, err
	}
	out.Revision = rev + 1
	out.State = "closed"
	if err = tx.Put(ctx, ns("leases"), out.LeaseID, rev, out); err != nil {
		return runtime.Outcome{}, err
	}
	return runtime.Applied(StateOutput{Ref: tx.Scope().Ref(out.LeaseID, out.Revision), State: out.State}), nil
}
