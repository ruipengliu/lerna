package decisionfixture

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"slices"
	"strings"
	"time"

	decision "github.com/ruipengliu/lerna/components/decision_engine"
	v "github.com/ruipengliu/lerna/contract/v1_1"
)

type Material struct {
	Ref   v.ContentRef
	Bytes []byte
}
type Bundle struct {
	DecisionRef v.DecisionRef
	Permission  decision.Permission
	Snapshot    decision.Snapshot
	Materials   []Material
	Purposes    []string
	RuleVersion string
}
type Manifest struct {
	Kind        string            `json:"kind"`
	Snapshot    decision.Snapshot `json:"snapshot"`
	RuleVersion string            `json:"rule_version"`
}
type ComponentFixtureLock struct {
	Kind         string         `json:"kind"`
	ComponentRef v.ComponentRef `json:"component_ref"`
	ManifestRef  v.ContentRef   `json:"manifest_ref"`
	RuleVersion  string         `json:"rule_version"`
}
type grant struct {
	Permission  decision.Permission `json:"permission"`
	SnapshotRef v.SnapshotRef       `json:"snapshot_ref"`
	ManifestRef v.ContentRef        `json:"manifest_ref"`
	LockRef     v.InstallLockRef    `json:"lock_ref"`
	ContentRefs []v.ContentRef      `json:"content_refs"`
	Purposes    []string            `json:"purposes"`
}

func jsonBytes(value any) ([]byte, error) {
	body, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	if len(body) > v.MaxBodyBytes {
		return nil, errors.New("fixture body exceeds finite size")
	}
	return body, nil
}
func closedJSON(body []byte, out any) error {
	if _, err := v.ParseJSON(body); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return errors.New("fixture trailing JSON")
	}
	return nil
}
func key(value any) (string, error) { body, err := jsonBytes(value); return string(body), err }
func permissionKey(p decision.Permission) (string, error) {
	body, err := jsonBytes(p)
	return digest(body), err
}
func checkContent(ref v.ContentRef, body []byte) error {
	if _, err := v.Encode(ref); err != nil {
		return err
	}
	if ref.Hash != digest(body) || string(ref.ByteLength) != fmt.Sprint(len(body)) {
		return errors.New("fixture exact content digest or length mismatch")
	}
	if len(body) > v.MaxBodyBytes {
		return errors.New("fixture content over finite bound")
	}
	return nil
}
func validateSnapshot(s decision.Snapshot) error {
	if _, err := v.Encode(s.Ref); err != nil {
		return err
	}
	if _, err := v.Encode(s.TaskRef); err != nil {
		return err
	}
	if _, err := v.Encode(s.ComponentRef); err != nil {
		return err
	}
	if err := v.Validate("Revision", string(s.GoalRevision)); err != nil {
		return err
	}
	if err := v.Validate("Revision", string(s.ControlRevision)); err != nil {
		return err
	}
	if s.Rule == "" || len(s.MaterialRefs) > 64 || len(s.RequirementRefs) > 64 || len(s.CapabilityBindings) > 64 || len(s.AnswerSchemaRefs) > 16 || len(s.UseRefs) == 0 || len(s.UseRefs) > 64 {
		return errors.New("fixture snapshot bounds")
	}
	for _, ref := range append(slices.Clone(s.MaterialRefs), s.AnswerSchemaRefs...) {
		if _, err := v.Encode(ref); err != nil {
			return err
		}
	}
	for _, ref := range s.RequirementRefs {
		if _, err := v.Encode(ref); err != nil {
			return err
		}
	}
	for _, ref := range s.UseRefs {
		if _, err := v.Encode(ref); err != nil {
			return err
		}
	}
	for _, binding := range s.CapabilityBindings {
		if _, err := v.Encode(binding.CapabilityRef); err != nil {
			return err
		}
		if _, err := v.Encode(binding.BindingRef); err != nil {
			return err
		}
		if _, err := v.Encode(binding.ArgumentsRef); err != nil {
			return err
		}
		if binding.Purpose == "" {
			return errors.New("fixture empty capability purpose")
		}
	}
	return nil
}

// Seed is the trusted fixture dispatcher. Every byte and permission is saved
// in this owner's short transaction before a Decision is submitted.
func (s *Store) Seed(ctx context.Context, b Bundle) (v.ContentRef, error) {
	var manifestRef v.ContentRef
	if err := validateSnapshot(b.Snapshot); err != nil {
		return manifestRef, err
	}
	if string(b.Snapshot.ComponentRef.ArtifactDigest) != digest([]byte(b.RuleVersion)) || string(b.Snapshot.ComponentRef.ConfigDigest) != digest([]byte(b.Snapshot.Rule)) {
		return manifestRef, errors.New("fixture component artifact or configuration digest mismatch")
	}
	if _, err := v.Encode(b.DecisionRef); err != nil {
		return manifestRef, err
	}
	if _, err := v.Encode(b.Permission.Subject); err != nil {
		return manifestRef, err
	}
	if b.Permission.TaskRef != b.Snapshot.TaskRef || b.Permission.ComponentRef != b.Snapshot.ComponentRef || !reflect.DeepEqual(b.Permission.UseRefs, b.Snapshot.UseRefs) || b.DecisionRef.TenantID != s.owner.TenantID || b.DecisionRef.TenantID != b.Permission.Subject.TenantID || b.Permission.DecisionOwner != (v.OwnerRef{TenantID: b.DecisionRef.TenantID, OwnerID: b.DecisionRef.OwnerID}) || b.Snapshot.Ref.OwnerID != s.owner.OwnerID || b.Snapshot.Ref.TenantID != s.owner.TenantID || b.Snapshot.TaskRef.OwnerID != s.owner.OwnerID || b.Snapshot.TaskRef.TenantID != s.owner.TenantID || b.RuleVersion == "" || len(b.Purposes) == 0 || len(b.Purposes) > 16 || len(b.Materials) > 96 {
		return manifestRef, decision.ErrForbidden
	}
	lockRef := b.Snapshot.ComponentRef.InstallLockRef
	if lockRef.OwnerID != s.owner.OwnerID || lockRef.TenantID != s.owner.TenantID {
		return manifestRef, decision.ErrForbidden
	}
	for _, ref := range b.Permission.UseRefs {
		if ref.TenantID != s.owner.TenantID || ref.OwnerID != s.owner.OwnerID {
			return manifestRef, decision.ErrForbidden
		}
	}
	manifest, err := jsonBytes(Manifest{Kind: "durable_fixture_manifest", Snapshot: b.Snapshot, RuleVersion: b.RuleVersion})
	if err != nil {
		return manifestRef, err
	}
	manifestRef = s.Ref(v.ID("manifest-"+strings.TrimPrefix(digest(manifest), "sha256:")[:40]), "application/json", manifest)
	lock, err := jsonBytes(ComponentFixtureLock{Kind: "component_fixture_lock", ComponentRef: b.Snapshot.ComponentRef, ManifestRef: manifestRef, RuleVersion: b.RuleVersion})
	if err != nil {
		return manifestRef, err
	}
	snapshot, err := jsonBytes(b.Snapshot)
	if err != nil {
		return manifestRef, err
	}
	contents := []v.ContentRef{manifestRef}
	for _, material := range b.Materials {
		if material.Ref.Owner != s.owner || material.Ref.Version != "1" {
			return manifestRef, decision.ErrForbidden
		}
		if err = checkContent(material.Ref, material.Bytes); err != nil {
			return manifestRef, err
		}
		contents = append(contents, material.Ref)
	}
	for _, ref := range append(slices.Clone(b.Snapshot.MaterialRefs), b.Snapshot.AnswerSchemaRefs...) {
		if !slices.Contains(contents, ref) {
			return manifestRef, decision.ErrUnavailable
		}
	}
	for _, binding := range b.Snapshot.CapabilityBindings {
		if !slices.Contains(contents, binding.ArgumentsRef) {
			return manifestRef, decision.ErrUnavailable
		}
	}
	g := grant{Permission: b.Permission, SnapshotRef: b.Snapshot.Ref, ManifestRef: manifestRef, LockRef: lockRef, ContentRefs: contents, Purposes: slices.Clone(b.Purposes)}
	permissionID, err := permissionKey(g.Permission)
	if err != nil {
		return manifestRef, err
	}
	grantBody, err := jsonBytes(g)
	if err != nil {
		return manifestRef, err
	}
	decisionKey, err := key(b.DecisionRef)
	if err != nil {
		return manifestRef, err
	}
	ownerKey, err := key(b.Permission.DecisionOwner)
	if err != nil {
		return manifestRef, err
	}
	subjectKey, err := key(b.Permission.Subject)
	if err != nil {
		return manifestRef, err
	}
	err = s.within(ctx, func(ctx context.Context, tx *sql.Tx, now time.Time) error {
		if !now.Before(b.Permission.ValidUntil) {
			return decision.ErrForbidden
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO `+s.table("fixture_permissions")+`(permission_key,owner_key,subject_key,read_commands,valid_until,body) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT DO NOTHING`, permissionID, ownerKey, subjectKey, slices.Contains(b.Purposes, "command.get"), b.Permission.ValidUntil, grantBody); err != nil {
			return err
		}
		if err := s.immutable(ctx, tx, "fixture_permissions", "permission_key", permissionID, "", grantBody); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO `+s.table("fixture_decisions")+`(decision_key,permission_key,body) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, decisionKey, permissionID, grantBody); err != nil {
			return err
		}
		var previous []byte
		if err := tx.QueryRowContext(ctx, `SELECT body FROM `+s.table("fixture_decisions")+` WHERE decision_key=$1`, decisionKey).Scan(&previous); err != nil {
			return err
		}
		if !bytes.Equal(previous, grantBody) {
			return decision.ErrPublicationConflict
		}
		for _, object := range []struct {
			ref  any
			kind string
			body []byte
		}{{b.Snapshot.Ref, "snapshot", snapshot}, {lockRef, "lock", lock}, {manifestRef, "material", manifest}} {
			objectKey, err := key(object.ref)
			if err != nil {
				return err
			}
			if err = s.immutable(ctx, tx, "fixture_objects", "object_key", objectKey, object.kind, object.body); err != nil {
				return err
			}
		}
		for _, material := range b.Materials {
			objectKey, err := key(material.Ref)
			if err != nil {
				return err
			}
			if err = s.immutable(ctx, tx, "fixture_objects", "object_key", objectKey, "material", material.Bytes); err != nil {
				return err
			}
		}
		return nil
	})
	return manifestRef, err
}
func (s *Store) immutable(ctx context.Context, tx *sql.Tx, table, column, identity, kind string, body []byte) error {
	if kind != "" {
		query := `INSERT INTO ` + s.table(table) + `(` + column + `,kind,body) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`
		if _, err := tx.ExecContext(ctx, query, identity, kind, body); err != nil {
			return err
		}
	}
	var prior []byte
	if err := tx.QueryRowContext(ctx, `SELECT body FROM `+s.table(table)+` WHERE `+column+`=$1`, identity).Scan(&prior); err != nil {
		return err
	}
	if !bytes.Equal(prior, body) {
		return decision.ErrPublicationConflict
	}
	return nil
}
func (s *Store) Authorize(ctx context.Context, subject v.SubjectBinding, ref v.DecisionRef, purpose string, input *v.DecisionDecidePayload) (decision.Permission, error) {
	var out decision.Permission
	if _, err := v.Encode(subject); err != nil {
		return out, decision.ErrForbidden
	}
	if _, err := v.Encode(ref); err != nil {
		return out, decision.ErrForbidden
	}
	identity, err := key(ref)
	if err != nil {
		return out, err
	}
	err = s.within(ctx, func(ctx context.Context, tx *sql.Tx, now time.Time) error {
		var body []byte
		query := `SELECT body FROM ` + s.table("fixture_decisions") + ` WHERE decision_key=$1`
		args := []any{identity}
		if purpose == "command.get" {
			ownerKey, err := key(v.OwnerRef{TenantID: ref.TenantID, OwnerID: ref.OwnerID})
			if err != nil {
				return decision.ErrForbidden
			}
			subjectKey, err := key(subject)
			if err != nil {
				return decision.ErrForbidden
			}
			query = `SELECT body FROM ` + s.table("fixture_permissions") + ` WHERE owner_key=$1 AND subject_key=$2 AND read_commands AND valid_until>clock_timestamp() ORDER BY permission_key LIMIT 1`
			args = []any{ownerKey, subjectKey}
		}
		if err := tx.QueryRowContext(ctx, query, args...).Scan(&body); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return decision.ErrForbidden
			}
			return err
		}
		var g grant
		if err := closedJSON(body, &g); err != nil {
			return decision.ErrUnavailable
		}
		if g.ManifestRef.Owner != s.owner || g.SnapshotRef.TenantID != s.owner.TenantID || g.SnapshotRef.OwnerID != s.owner.OwnerID || !reflect.DeepEqual(g.Permission.Subject, subject) || !slices.Contains(g.Purposes, purpose) || !now.Before(g.Permission.ValidUntil) {
			return decision.ErrForbidden
		}
		if input != nil && (input.TaskRef != g.Permission.TaskRef || input.SnapshotRef != g.SnapshotRef || input.ComponentRef != g.Permission.ComponentRef || !reflect.DeepEqual(input.UseRefs, g.Permission.UseRefs)) {
			return decision.ErrForbidden
		}
		out = g.Permission
		return nil
	})
	return out, err
}
func (s *Store) current(ctx context.Context, tx *sql.Tx, p decision.Permission, purpose string) (grant, error) {
	var g grant
	identity, err := permissionKey(p)
	if err != nil {
		return g, decision.ErrForbidden
	}
	var body []byte
	if err = tx.QueryRowContext(ctx, `SELECT body FROM `+s.table("fixture_permissions")+` WHERE permission_key=$1`, identity).Scan(&body); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return g, decision.ErrForbidden
		}
		return g, err
	}
	if err = closedJSON(body, &g); err != nil {
		return g, decision.ErrUnavailable
	}
	var now time.Time
	if err = tx.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return g, err
	}
	if g.ManifestRef.Owner != s.owner || g.SnapshotRef.TenantID != s.owner.TenantID || g.SnapshotRef.OwnerID != s.owner.OwnerID || !reflect.DeepEqual(g.Permission, p) || !slices.Contains(g.Purposes, purpose) || !now.Before(g.Permission.ValidUntil) {
		return g, decision.ErrForbidden
	}
	return g, nil
}
func (s *Store) object(ctx context.Context, tx *sql.Tx, ref any, kind string) ([]byte, error) {
	identity, err := key(ref)
	if err != nil {
		return nil, err
	}
	var body []byte
	if err = tx.QueryRowContext(ctx, `SELECT body FROM `+s.table("fixture_objects")+` WHERE object_key=$1 AND kind=$2`, identity, kind).Scan(&body); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, decision.ErrUnavailable
		}
		return nil, err
	}
	return body, nil
}
func (s *Store) ReadSnapshot(ctx context.Context, ref v.SnapshotRef, p decision.Permission) (decision.Snapshot, error) {
	var out decision.Snapshot
	err := s.within(ctx, func(ctx context.Context, tx *sql.Tx, _ time.Time) error {
		g, err := s.current(ctx, tx, p, "material")
		if err != nil {
			return err
		}
		if g.SnapshotRef != ref {
			return decision.ErrForbidden
		}
		body, err := s.object(ctx, tx, ref, "snapshot")
		if err != nil {
			return err
		}
		if err = closedJSON(body, &out); err != nil {
			return decision.ErrUnavailable
		}
		out.Raw = slices.Clone(body)
		return validateSnapshot(out)
	})
	return out, err
}
func (s *Store) ReadMaterial(ctx context.Context, ref v.ContentRef, purpose string, p decision.Permission) ([]byte, error) {
	var out []byte
	err := s.within(ctx, func(ctx context.Context, tx *sql.Tx, _ time.Time) error {
		g, err := s.current(ctx, tx, p, purpose)
		if err != nil {
			return err
		}
		if !slices.Contains(g.ContentRefs, ref) {
			return decision.ErrForbidden
		}
		out, err = s.object(ctx, tx, ref, "material")
		if err != nil {
			return err
		}
		return checkContent(ref, out)
	})
	return out, err
}
func (s *Store) ReadFixtureLock(ctx context.Context, ref v.InstallLockRef, p decision.Permission) ([]byte, error) {
	var out []byte
	err := s.within(ctx, func(ctx context.Context, tx *sql.Tx, _ time.Time) error {
		g, err := s.current(ctx, tx, p, "fixture.lock")
		if err != nil {
			return err
		}
		if g.LockRef != ref {
			return decision.ErrForbidden
		}
		out, err = s.object(ctx, tx, ref, "lock")
		if err != nil {
			return err
		}
		var lock ComponentFixtureLock
		if err = closedJSON(out, &lock); err != nil {
			return decision.ErrUnavailable
		}
		if lock.ComponentRef != p.ComponentRef || lock.ManifestRef != g.ManifestRef || lock.Kind != "component_fixture_lock" {
			return decision.ErrUnavailable
		}
		manifest, err := s.object(ctx, tx, lock.ManifestRef, "material")
		if err != nil {
			return err
		}
		if err = checkContent(lock.ManifestRef, manifest); err != nil {
			return err
		}
		var fixed Manifest
		if err = closedJSON(manifest, &fixed); err != nil {
			return decision.ErrUnavailable
		}
		if fixed.Kind != "durable_fixture_manifest" || fixed.RuleVersion != lock.RuleVersion || fixed.Snapshot.ComponentRef != lock.ComponentRef || string(lock.ComponentRef.ArtifactDigest) != digest([]byte(lock.RuleVersion)) || string(lock.ComponentRef.ConfigDigest) != digest([]byte(fixed.Snapshot.Rule)) {
			return decision.ErrUnavailable
		}
		return validateSnapshot(fixed.Snapshot)
	})
	return out, err
}
