package decisionfixture

import (
	"context"
	"database/sql"
	"errors"
	decision "github.com/ruipengliu/lerna/components/decision_engine"
	v "github.com/ruipengliu/lerna/contract/v1_1"
	"reflect"
	"slices"
	"strings"
	"time"
)

// ControlClaim is a fixture Task owner's immutable cancellation statement.
// Issuing it neither creates a Snapshot nor grants Decision execution access.
type ControlClaim struct {
	Subject         v.SubjectBinding `json:"subject"`
	DecisionRef     v.DecisionRef    `json:"decision_ref"`
	TaskRef         v.TaskObjectRef  `json:"task_ref"`
	InputDigest     v.SchemaDigest   `json:"input_digest"`
	ControlRevision v.Revision       `json:"control_revision"`
	ValidUntil      v.Time           `json:"valid_until"`
}
type controlProof struct {
	Kind   string       `json:"kind"`
	Issuer v.OwnerRef   `json:"issuer_owner"`
	Claim  ControlClaim `json:"claim"`
	Stop   string       `json:"stop"`
}

func validateAccess(a decision.ControlAccess) error {
	if _, err := v.Encode(a.Subject); err != nil {
		return decision.ErrForbidden
	}
	if _, err := v.Encode(a.DecisionOwner); err != nil {
		return decision.ErrForbidden
	}
	if a.Subject.TenantID != a.DecisionOwner.TenantID || a.ValidUntil.IsZero() || len(a.Purposes) > 3 {
		return decision.ErrForbidden
	}
	seen := map[string]bool{}
	for _, p := range a.Purposes {
		if seen[p] || (p != "cancel" && p != "get" && p != "command.get") {
			return decision.ErrForbidden
		}
		seen[p] = true
		if (p == "command.get") != (a.DecisionRef == nil) {
			return decision.ErrForbidden
		}
	}
	if a.DecisionRef != nil {
		if _, err := v.Encode(*a.DecisionRef); err != nil {
			return decision.ErrForbidden
		}
		if (v.OwnerRef{TenantID: a.DecisionRef.TenantID, OwnerID: a.DecisionRef.OwnerID}) != a.DecisionOwner {
			return decision.ErrForbidden
		}
	}
	return nil
}
func accessKeys(a decision.ControlAccess) (string, string, string, error) {
	own, err := key(a.DecisionOwner)
	if err != nil {
		return "", "", "", err
	}
	subject, err := key(a.Subject)
	if err != nil {
		return "", "", "", err
	}
	scope := "owner_commands"
	if a.DecisionRef != nil {
		scope, err = key(*a.DecisionRef)
	}
	return own, subject, scope, err
}

// SeedControlAccess is trusted fixture administration. Updating an exact scope
// changes current access only; it cannot rewrite an issued immutable proof.
func (s *Store) SeedControlAccess(ctx context.Context, a decision.ControlAccess) error {
	body, err := jsonBytes(a)
	if err != nil {
		return err
	}
	if err = closedJSON(body, &a); err != nil {
		return err
	}
	if err = validateAccess(a); err != nil {
		return err
	}
	if a.DecisionOwner.TenantID != s.owner.TenantID {
		return decision.ErrForbidden
	}
	own, subject, scope, err := accessKeys(a)
	if err != nil {
		return err
	}
	return s.within(ctx, func(ctx context.Context, tx *sql.Tx, _ time.Time) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO `+s.table("fixture_control_access")+`(owner_key,subject_key,decision_key,valid_until,body)VALUES($1,$2,$3,$4,$5)ON CONFLICT(owner_key,subject_key,decision_key)DO UPDATE SET valid_until=excluded.valid_until,body=excluded.body`, own, subject, scope, a.ValidUntil, body)
		return err
	})
}
func (s *Store) AuthorizeControl(ctx context.Context, principal v.SubjectBinding, ref v.DecisionRef, purpose string) (decision.ControlAccess, error) {
	var result decision.ControlAccess
	if _, err := v.Encode(ref); err != nil {
		return result, decision.ErrForbidden
	}
	probe := decision.ControlAccess{Subject: principal, DecisionOwner: v.OwnerRef{TenantID: ref.TenantID, OwnerID: ref.OwnerID}, DecisionRef: &ref, Purposes: []string{purpose}, ValidUntil: time.Now().UTC()}
	if purpose == "command.get" {
		probe.DecisionRef = nil
	}
	if err := validateAccess(probe); err != nil {
		return result, err
	}
	own, subject, scope, err := accessKeys(probe)
	if err != nil {
		return result, err
	}
	err = s.within(ctx, func(ctx context.Context, tx *sql.Tx, now time.Time) error {
		var body []byte
		if err := tx.QueryRowContext(ctx, `SELECT body FROM `+s.table("fixture_control_access")+` WHERE owner_key=$1 AND subject_key=$2 AND decision_key=$3`, own, subject, scope).Scan(&body); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return decision.ErrForbidden
			}
			return err
		}
		if err := closedJSON(body, &result); err != nil {
			return decision.ErrUnavailable
		}
		if err := validateAccess(result); err != nil {
			return decision.ErrUnavailable
		}
		ro, rs, rc, err := accessKeys(result)
		if err != nil {
			return err
		}
		if ro != own || rs != subject || rc != scope || !slices.Contains(result.Purposes, purpose) || !now.Before(result.ValidUntil) {
			return decision.ErrForbidden
		}
		return nil
	})
	return result, err
}
func (s *Store) validateClaim(c ControlClaim) error {
	if _, err := v.Encode(c.Subject); err != nil {
		return decision.ErrForbidden
	}
	if _, err := v.Encode(c.DecisionRef); err != nil {
		return decision.ErrForbidden
	}
	if _, err := v.Encode(c.TaskRef); err != nil {
		return decision.ErrForbidden
	}
	for _, err := range []error{v.Validate("SchemaDigest", string(c.InputDigest)), v.Validate("Revision", string(c.ControlRevision)), v.Validate("Time", string(c.ValidUntil))} {
		if err != nil {
			return decision.ErrForbidden
		}
	}
	if c.TaskRef.TenantID != s.owner.TenantID || c.TaskRef.OwnerID != s.owner.OwnerID || c.DecisionRef.TenantID != s.owner.TenantID || c.Subject.TenantID != s.owner.TenantID {
		return decision.ErrForbidden
	}
	return nil
}
func (s *Store) IssueControl(ctx context.Context, c ControlClaim) (v.ControlBasis, error) {
	var result v.ControlBasis
	body, err := jsonBytes(controlProof{Kind: "fixture_control/1", Issuer: s.owner, Claim: c, Stop: "cancel"})
	if err != nil {
		return result, err
	}
	var proof controlProof
	if err = closedJSON(body, &proof); err != nil {
		return result, err
	}
	c = proof.Claim
	if err = s.validateClaim(c); err != nil {
		return result, err
	}
	expires, err := time.Parse("2006-01-02T15:04:05.000000Z", string(c.ValidUntil))
	if err != nil {
		return result, err
	}
	ref := s.Ref(v.ID("control-"+strings.TrimPrefix(digest(body), "sha256:")), "application/vnd.lerna.fixture-control+json", body)
	identity, err := key(ref)
	if err != nil {
		return result, err
	}
	err = s.within(ctx, func(ctx context.Context, tx *sql.Tx, now time.Time) error {
		if !now.Before(expires) {
			return decision.ErrForbidden
		}
		return s.immutable(ctx, tx, "fixture_objects", "object_key", identity, "control_proof", body)
	})
	if err != nil {
		return result, err
	}
	return v.ControlBasis{IssuerOwner: s.owner, ControlRevision: c.ControlRevision, ValidUntil: c.ValidUntil, ProofRef: ref}, nil
}
func (s *Store) VerifyControl(ctx context.Context, principal v.SubjectBinding, payload v.DecisionCancelPayload, input *v.DecisionDecidePayload) (*v.Revision, error) {
	body, err := v.Encode(payload)
	if err != nil {
		return nil, decision.ErrForbidden
	}
	payload, err = v.Decode[v.DecisionCancelPayload](body)
	if err != nil {
		return nil, decision.ErrForbidden
	}
	encodedSubject, err := v.Encode(principal)
	if err != nil {
		return nil, decision.ErrForbidden
	}
	principal, err = v.Decode[v.SubjectBinding](encodedSubject)
	if err != nil {
		return nil, decision.ErrForbidden
	}
	if input != nil {
		body, err = v.Encode(*input)
		if err != nil {
			return nil, decision.ErrForbidden
		}
		detached, err := v.Decode[v.DecisionDecidePayload](body)
		if err != nil {
			return nil, err
		}
		input = &detached
	}
	basis := payload.ControlBasis
	if basis.IssuerOwner != s.owner || basis.ProofRef.Owner != s.owner || basis.ProofRef.Version != "1" || basis.ProofRef.MediaType != "application/vnd.lerna.fixture-control+json" {
		return nil, decision.ErrForbidden
	}
	var floor *v.Revision
	err = s.within(ctx, func(ctx context.Context, tx *sql.Tx, now time.Time) error {
		raw, err := s.object(ctx, tx, basis.ProofRef, "control_proof")
		if err != nil {
			return err
		}
		if err = checkContent(basis.ProofRef, raw); err != nil {
			return decision.ErrForbidden
		}
		var proof controlProof
		if err = closedJSON(raw, &proof); err != nil {
			return decision.ErrUnavailable
		}
		c := proof.Claim
		if proof.Kind != "fixture_control/1" || proof.Stop != "cancel" || proof.Issuer != s.owner || s.validateClaim(c) != nil || !reflect.DeepEqual(c.Subject, principal) || c.DecisionRef != payload.DecisionRef || c.TaskRef != payload.TaskRef || c.InputDigest != payload.DecisionInputDigest || c.ControlRevision != basis.ControlRevision || c.ValidUntil != basis.ValidUntil {
			return decision.ErrForbidden
		}
		until, err := time.Parse("2006-01-02T15:04:05.000000Z", string(c.ValidUntil))
		if err != nil || !now.Before(until) {
			return decision.ErrForbidden
		}
		if input == nil {
			return nil
		}
		if input.TaskRef != payload.TaskRef {
			return decision.ErrForbidden
		}
		identity, err := key(payload.DecisionRef)
		if err != nil {
			return err
		}
		var granted []byte
		if err = tx.QueryRowContext(ctx, `SELECT body FROM `+s.table("fixture_decisions")+` WHERE decision_key=$1`, identity).Scan(&granted); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return decision.ErrUnavailable
			}
			return err
		}
		var g grant
		if err = closedJSON(granted, &g); err != nil {
			return decision.ErrUnavailable
		}
		if g.SnapshotRef != input.SnapshotRef || g.Permission.TaskRef != input.TaskRef || g.Permission.ComponentRef != input.ComponentRef || !reflect.DeepEqual(g.Permission.UseRefs, input.UseRefs) || g.ManifestRef.Owner != s.owner {
			return decision.ErrForbidden
		}
		raw, err = s.object(ctx, tx, input.SnapshotRef, "snapshot")
		if err != nil {
			return err
		}
		var snapshot decision.Snapshot
		if err = closedJSON(raw, &snapshot); err != nil {
			return decision.ErrUnavailable
		}
		if snapshot.Ref != input.SnapshotRef || snapshot.TaskRef != input.TaskRef || snapshot.ComponentRef != input.ComponentRef || !reflect.DeepEqual(snapshot.UseRefs, input.UseRefs) || validateSnapshot(snapshot) != nil {
			return decision.ErrUnavailable
		}
		manifestBytes, err := s.object(ctx, tx, g.ManifestRef, "material")
		if err != nil {
			return err
		}
		if err = checkContent(g.ManifestRef, manifestBytes); err != nil {
			return decision.ErrUnavailable
		}
		var manifest Manifest
		if err = closedJSON(manifestBytes, &manifest); err != nil {
			return decision.ErrUnavailable
		}
		if manifest.Kind != "durable_fixture_manifest" || !reflect.DeepEqual(manifest.Snapshot, snapshot) {
			return decision.ErrUnavailable
		}
		observed := snapshot.ControlRevision
		floor = &observed
		return nil
	})
	return floor, err
}

var _ decision.ControlAuthority = (*Store)(nil)
