package artifacts

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"reflect"

	"lerna/authorization"
)

// LegacyUse is trusted, independently pinned upgrade configuration, never a
// request or a manifest derived from the database being upgraded. Its digest
// covers the exact original encoded ContentRecord, including source policy,
// body digest and retention. It permits registration of a current retained use,
// not fabrication of historical authorization. Hosts must first verify the
// original execution/operation association before invoking this local port.
type LegacyUse struct {
	Namespace    string `json:"namespace"`
	Key          string `json:"key"`
	Revision     uint64 `json:"revision"`
	Subject      string `json:"subject"`
	OperationID  string `json:"operation_id"`
	RecordSHA256 string `json:"record_sha256"`
}

// MigrateLegacyUse must run before ordinary access or cleanup retires the old
// object. It never changes the original content/PUT identity or its lifetime.
// No SDK method exposes this trusted upgrade port.
func (s *Service) MigrateLegacyUse(ctx context.Context, b Binding, in LegacyUse) error {
	hash, err := hex.DecodeString(in.RecordSHA256)
	if err != nil || len(hash) != 32 || len(in.RecordSHA256) != 64 || in.Namespace == "" || in.Namespace != b.Namespace || in.Key == "" || len(in.Key) > 256 || in.Revision == 0 || in.Subject == "" || in.OperationID == "" {
		return Error("INVALID_ARGUMENT")
	}
	ctx, cancel := context.WithTimeout(ctx, s.config.Timeout)
	defer cancel()
	unlock, err := s.blobs.Lock(ctx)
	if err != nil {
		return err
	}
	defer unlock()
	var original entry
	check := func(tx authorization.ContentTransaction, apply bool) error {
		var j journal
		if json.Unmarshal(tx.Data(), &j) != nil || j.Config != s.config || j.Records == nil || j.Operations == nil || len(j.Invalidated)+len(j.InvalidatedRevisions) > maxInvalidatedSources {
			return Error("UNAVAILABLE")
		}
		e, ok := j.Records[in.Key]
		if !ok || digest(e.Record) != in.RecordSHA256 {
			return Error("PERMISSION_DENIED")
		}
		r, err := decode(e)
		if err != nil {
			return err
		}
		if r.Ref.Namespace != in.Namespace || r.Ref.Key != in.Key || r.Ref.Revision != in.Revision || !available(r, tx.Now()) || invalidated(&j, in.Namespace, r.Spec.Sources) {
			return Error("PERMISSION_DENIED")
		}
		op, ok := j.Operations[in.OperationID]
		if !ok || op.Method != "PUT" || op.Subject != in.Subject || op.Key != in.Key || op.Hash == "" {
			return Error("PERMISSION_DENIED")
		}
		id, err := tx.Identity(b.Token)
		if err != nil {
			return err
		}
		if id.Subject != in.Subject || id.Namespace != in.Namespace {
			return Error("PERMISSION_DENIED")
		}
		if err = tx.Operation(in.OperationID, in.Subject, false); err != nil {
			return err
		}
		if r.Spec.AcquiredAt > tx.Now().Unix() || r.Spec.RetainUntil > tx.Now().Add(s.config.Retention).Unix() {
			return Error("PERMISSION_DENIED")
		}
		tracked := &useTransaction{ContentTransaction: tx}
		for _, action := range []string{"store", "process", "retain", "discover"} {
			if _, err = s.authorize(ctx, tracked, b, r.Spec, action, r.Spec.Purpose); err != nil {
				return err
			}
		}
		useID := "artifact." + in.Key
		if e.UseID != "" {
			if e.UseID != useID {
				return Error("PERMISSION_DENIED")
			}
			state, err := tx.UseStatus(useID, "artifacts", s.useConfig())
			if err != nil {
				return err
			}
			if state != "active" {
				return Error("PERMISSION_DENIED")
			}
		}
		if !apply {
			original = e
			return nil
		}
		if !reflect.DeepEqual(original, e) {
			return Error("VERSION_CONFLICT")
		}
		if e.UseID != "" {
			return nil
		}
		if err = tx.RegisterUse(b.Token, authorization.UseSpec{Namespace: in.Namespace, ID: useID, Consumer: "artifacts", ConfigSHA256: s.useConfig(), Until: r.Spec.RetainUntil, Actions: tracked.actions}); err != nil {
			return err
		}
		e.UseID = useID
		j.Records[in.Key] = e
		raw, err := json.Marshal(j)
		if err != nil {
			return err
		}
		tx.SetData(raw)
		return nil
	}
	if err = s.authority.UpdateContent(ctx, func(tx authorization.ContentTransaction) error { return check(tx, false) }); err != nil {
		return err
	}
	r, err := decode(original)
	if err != nil {
		return err
	}
	if original.File != "" {
		if _, err = s.blobs.Read(ctx, original.File, 0, 0, r.Spec.Size, r.Spec.Sha256); err != nil {
			return err
		}
	} else if uint64(len(original.Body)) != r.Spec.Size || digest(original.Body) != r.Spec.Sha256 {
		return Error("CONTENT_CORRUPT")
	}
	return s.authority.UpdateContent(ctx, func(tx authorization.ContentTransaction) error { return check(tx, true) })
}
