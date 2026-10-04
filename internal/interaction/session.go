package interaction

import (
	"context"
	"errors"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/runtime"
)

func invalid(reason string) error { return api.E("invalid_request", reason) }
func identity(auth runtime.Auth, scope runtime.Scope) error {
	if auth.TenantID != scope.TenantID || !api.ValidID(auth.SubjectID) || auth.CredentialGeneration == 0 {
		return api.E("forbidden", "invalid_interaction_identity")
	}
	return nil
}
func access(auth runtime.Auth, scope runtime.Scope, subject string) error {
	if err := identity(auth, scope); err != nil {
		return err
	}
	if auth.SubjectID != subject && !auth.HasRole("interaction_admin") {
		return api.E("forbidden", "interaction_access_denied")
	}
	return nil
}
func exactScope(scope runtime.Scope, ref api.ObjectRef) error {
	if err := runtime.CheckRef(scope, ref); err != nil {
		return err
	}
	if ref.OwnerID != scope.OwnerID {
		return api.E("forbidden", "reference_owner_mismatch")
	}
	return nil
}
func getSession(ctx context.Context, tx runtime.Tx, auth runtime.Auth, id string) (sessionRecord, error) {
	var r sessionRecord
	_, err := tx.Get(ctx, sessions, id, &r)
	if err == nil {
		err = access(auth, tx.Scope(), r.SubjectID)
	}
	return r, err
}
func getBranch(ctx context.Context, tx runtime.Tx, sessionID, id string) (branchRecord, error) {
	var r branchRecord
	_, err := tx.Get(ctx, branches, id, &r)
	if err == nil && r.Branch.SessionRef.ObjectID != sessionID {
		err = invalid("branch_session_mismatch")
	}
	return r, err
}
func saveSession(ctx context.Context, tx runtime.Tx, r *sessionRecord) error {
	old := r.Session.Revision
	r.Session.Revision++
	if err := tx.Put(ctx, sessions, r.Session.SessionID, old, *r); err != nil {
		return err
	}
	return bumpCollection(ctx, tx, "sessions", r.SubjectID)
}
func saveBranch(ctx context.Context, tx runtime.Tx, r *branchRecord) error {
	old := r.Branch.Revision
	r.Branch.Revision++
	if err := tx.Put(ctx, branches, r.Branch.BranchID, old, *r); err != nil {
		return err
	}
	return bumpCollection(ctx, tx, "branches", r.Branch.SessionRef.ObjectID)
}
func (s *Service) CreateSessionTx(ctx context.Context, tx runtime.Tx, auth runtime.Auth, c api.Command, in CreateSessionInput) (SessionOutput, error) {
	if c.TargetID != tx.Scope().OwnerID || !api.ValidID(in.SessionID) || !api.ValidID(in.DefaultBranchID) {
		return SessionOutput{}, invalid("target_mismatch")
	}
	if err := api.ValidateRecord("ComponentRef", in.ConfigRef); err != nil {
		return SessionOutput{}, err
	}
	var old sessionRecord
	_, err := tx.Get(ctx, sessions, in.SessionID, &old)
	if err == nil {
		return SessionOutput{}, api.E("idempotency_conflict", "original_session_creation_exists")
	}
	if !errors.Is(err, runtime.ErrNotFound) {
		return SessionOutput{}, err
	}
	now, err := tx.Now(ctx)
	if err != nil {
		return SessionOutput{}, err
	}
	record := sessionRecord{Session: api.Session{SessionID: in.SessionID, TenantID: tx.Scope().TenantID, OwnerID: tx.Scope().OwnerID, Revision: 1, State: "open", DefaultBranchID: in.DefaultBranchID, CreatedAt: api.Time(now)}, SubjectID: auth.SubjectID, CreateCommandID: c.CommandID, ConfigRef: in.ConfigRef}
	branch := branchRecord{Branch: api.Branch{BranchID: in.DefaultBranchID, SessionRef: tx.Scope().Ref(in.SessionID, 1), Revision: 1, HistoryCutoff: 0, ConfigRef: in.ConfigRef}}
	if err = tx.Create(ctx, sessions, in.SessionID, auth.SubjectID, record); err != nil {
		return SessionOutput{}, err
	}
	if err = tx.Create(ctx, branches, in.DefaultBranchID, in.SessionID, branch); err != nil {
		return SessionOutput{}, err
	}
	if err = bumpCollection(ctx, tx, "sessions", auth.SubjectID); err != nil {
		return SessionOutput{}, err
	}
	if err = bumpCollection(ctx, tx, "branches", in.SessionID); err != nil {
		return SessionOutput{}, err
	}
	return SessionOutput{tx.Scope().Ref(in.SessionID, 1), tx.Scope().Ref(in.DefaultBranchID, 1)}, nil
}
func (s *Service) ReadSession(ctx context.Context, store runtime.Store, scope runtime.Scope, auth runtime.Auth, id string, in ReadInput) (SessionView, error) {
	view := SessionView{Branches: []api.Branch{}}
	status, err := store.Within(ctx, scope, s.config.Participants, func(tx runtime.Tx) error {
		r, e := getSession(ctx, tx, auth, id)
		if e != nil {
			return e
		}
		if in.Revision != 0 && in.Revision != r.Session.Revision {
			return api.E("revision_conflict", "session_changed")
		}
		view.Session = r.Session
		view.Sequence = r.Sequence
		view.BodyState = "available"
		if r.Session.State == "deleted" {
			view.BodyState = "gone"
		}
		revision, e := markerRevision(ctx, tx, "branches", id)
		if e != nil {
			return e
		}
		view.BranchesCollectionRevision = revision
		rows, e := tx.List(ctx, branches, id, "", 101)
		if e != nil {
			return e
		}
		view.BranchesComplete = len(rows) <= 100
		if !view.BranchesComplete {
			rows = rows[:100]
			now, e := tx.Now(ctx)
			if e != nil {
				return e
			}
			expiry, e := pageExpiry(ctx, now)
			if e != nil {
				return e
			}
			roles, _ := api.Digest(auth.Roles)
			view.BranchesCursor = s.encodeCursor(pageCursor{TenantID: scope.TenantID, OwnerID: scope.OwnerID, DatabaseID: scope.DatabaseID, SubjectID: auth.SubjectID, CredentialGeneration: auth.CredentialGeneration, RolesDigest: roles, Kind: "branches", Parent: id, Revision: revision, Last: rows[99].ID, ExpiresAt: api.Time(expiry)})
		}
		for _, row := range rows {
			var branch branchRecord
			if e = row.Decode(&branch); e != nil {
				return e
			}
			view.Branches = append(view.Branches, branch.Branch)
		}
		return s.checkDisclosureTx(ctx, tx, auth)
	})
	if status == runtime.CommitUnknown {
		return SessionView{}, runtime.ErrCommitUnknown
	}
	if err != nil {
		return SessionView{}, err
	}
	return view, err
}
func (s *Service) CreateBranchTx(ctx context.Context, tx runtime.Tx, auth runtime.Auth, c api.Command, in CreateBranchInput) (SessionOutput, error) {
	r, err := getSession(ctx, tx, auth, c.TargetID)
	if err != nil {
		return SessionOutput{}, err
	}
	if r.Session.State != "open" {
		return SessionOutput{}, api.E("invalid_state", "session_not_open")
	}
	if err = exactScope(tx.Scope(), in.SourceBranchRef); err != nil {
		return SessionOutput{}, err
	}
	source, err := getBranch(ctx, tx, c.TargetID, in.SourceBranchRef.ObjectID)
	if err != nil {
		return SessionOutput{}, err
	}
	if in.ExpectedSourceRevision != source.Branch.Revision || in.SourceBranchRef.Revision != source.Branch.Revision {
		return SessionOutput{}, api.E("revision_conflict", "branch_changed")
	}
	if !api.ValidID(in.BranchID) {
		return SessionOutput{}, invalid("invalid_branch_id")
	}
	if err = api.ValidateRecord("ComponentRef", in.ConfigRef); err != nil {
		return SessionOutput{}, err
	}
	cutoff := source.Branch.HistoryCutoff
	head := in.SourceHead
	if head == "" {
		head = source.Branch.HeadMessageID
	}
	if head != "" {
		var message api.Message
		if _, err = tx.Get(ctx, messages, head, &message); err != nil {
			return SessionOutput{}, err
		}
		if message.SessionRef.ObjectID != c.TargetID || message.Seq > source.Branch.HistoryCutoff {
			return SessionOutput{}, invalid("source_head_mismatch")
		}
		if head != source.Branch.HeadMessageID {
			if err = checkAncestor(ctx, tx, c.TargetID, source.Branch.HeadMessageID, head); err != nil {
				return SessionOutput{}, err
			}
		}
		cutoff = message.Seq
	}
	rows, err := tx.List(ctx, branches, c.TargetID, "", int(s.config.MaxBranches))
	if err != nil {
		return SessionOutput{}, err
	}
	if uint64(len(rows)) >= s.config.MaxBranches {
		return SessionOutput{}, api.E("overloaded", "branch_limit")
	}
	b := branchRecord{Branch: api.Branch{BranchID: in.BranchID, SessionRef: tx.Scope().Ref(c.TargetID, r.Session.Revision), Revision: 1, HeadMessageID: head, SourceBranchRef: &in.SourceBranchRef, HistoryCutoff: cutoff, ConfigRef: in.ConfigRef}}
	if err = tx.Create(ctx, branches, in.BranchID, c.TargetID, b); err != nil {
		return SessionOutput{}, err
	}
	if err = bumpCollection(ctx, tx, "branches", c.TargetID); err != nil {
		return SessionOutput{}, err
	}
	if err = saveSession(ctx, tx, &r); err != nil {
		return SessionOutput{}, err
	}
	return SessionOutput{tx.Scope().Ref(c.TargetID, r.Session.Revision), tx.Scope().Ref(in.BranchID, 1)}, nil
}

func checkAncestor(ctx context.Context, tx runtime.Tx, sessionID, head, wanted string) error {
	seen := map[string]bool{}
	for n := 0; n < 100 && head != ""; n++ {
		if head == wanted {
			return nil
		}
		if seen[head] {
			return api.E("invalid_state", "history_cycle")
		}
		seen[head] = true
		var message api.Message
		if err := tx.GetVersion(ctx, messages, head, 1, &message); err != nil {
			return err
		}
		if message.SessionRef.ObjectID != sessionID {
			return invalid("source_head_mismatch")
		}
		head = message.ParentMessageID
	}
	return invalid("source_head_mismatch")
}
func (s *Service) ControlSessionTx(ctx context.Context, tx runtime.Tx, auth runtime.Auth, c api.Command, in SessionControlInput) (SessionOutput, error) {
	r, err := getSession(ctx, tx, auth, c.TargetID)
	if err != nil {
		return SessionOutput{}, err
	}
	if c.ExpectedRevision == nil || *c.ExpectedRevision != r.Session.Revision {
		return SessionOutput{}, api.E("revision_conflict", "revision_changed")
	}
	if r.Session.State == "deleted" && c.Method != "session.delete" {
		return SessionOutput{}, api.E("gone", "session_deleted")
	}
	state := r.Session.State
	switch c.Method {
	case "session.archive":
		r.Session.State = "archived"
	case "session.delete":
		r.Session.State = "deleted"
	case "session.reopen":
		r.Session.State = "open"
	default:
		return SessionOutput{}, invalid("unknown_control")
	}
	if r.Session.State != state {
		if err = saveSession(ctx, tx, &r); err != nil {
			return SessionOutput{}, err
		}
	}
	var b branchRecord
	if _, err = tx.Get(ctx, branches, r.Session.DefaultBranchID, &b); err != nil {
		return SessionOutput{}, err
	}
	return SessionOutput{tx.Scope().Ref(c.TargetID, r.Session.Revision), tx.Scope().Ref(b.Branch.BranchID, b.Branch.Revision)}, nil
}
func (s *Service) SelectBranchTx(ctx context.Context, tx runtime.Tx, auth runtime.Auth, c api.Command, in SelectBranchInput) (SessionOutput, error) {
	r, err := getSession(ctx, tx, auth, c.TargetID)
	if err != nil {
		return SessionOutput{}, err
	}
	if c.ExpectedRevision == nil || *c.ExpectedRevision != r.Session.Revision {
		return SessionOutput{}, api.E("revision_conflict", "revision_changed")
	}
	if r.Session.State == "deleted" {
		return SessionOutput{}, api.E("gone", "session_deleted")
	}
	b, err := getBranch(ctx, tx, c.TargetID, in.BranchID)
	if err != nil {
		return SessionOutput{}, err
	}
	r.Session.DefaultBranchID = in.BranchID
	if err = saveSession(ctx, tx, &r); err != nil {
		return SessionOutput{}, err
	}
	return SessionOutput{tx.Scope().Ref(c.TargetID, r.Session.Revision), tx.Scope().Ref(in.BranchID, b.Branch.Revision)}, nil
}
