package artifacts

import (
	"context"
	"google.golang.org/protobuf/proto"
	"lerna/authorization"
	wire "lerna/gen/harness/v1"
)

func (s *Service) get(ctx context.Context, b Binding, in *wire.ContentRequest) (*wire.ContentResponse, error) {
	out := new(wire.ContentResponse)
	var body entry
	check := func(tx authorization.ContentTransaction, j *journal) error {
		if _, err := tx.Identity(b.Token); err != nil {
			return err
		}
		key := in.GetRef().GetKey()
		var op operation
		if in.Method == "LOOKUP" {
			var ok bool
			op, ok = j.Operations[in.OperationId]
			if !ok {
				return Error("PERMISSION_DENIED")
			}
			key = op.Key
		} else if in.Ref.Namespace != b.Namespace {
			return Error("PERMISSION_DENIED")
		}
		e, ok := j.Records[key]
		if !ok {
			return Error("PERMISSION_DENIED")
		}
		r, err := decode(e)
		if err != nil {
			return err
		}
		id, err := s.authorize(ctx, tx, b, r.Spec, "discover", in.Purpose)
		if err != nil {
			return err
		}
		if in.Method == "LOOKUP" && id.Subject != op.Subject {
			return Error("PERMISSION_DENIED")
		}
		if in.Method == "READ" {
			for _, action := range []string{"process", "disclose"} {
				if _, err = s.authorize(ctx, tx, b, r.Spec, action, in.Purpose); err != nil {
					return err
				}
			}
			if !available(r, tx.Now()) {
				return Error("CONTENT_UNAVAILABLE")
			}
			if in.Offset >= r.Spec.Size || uint64(in.Limit) > r.Spec.Size-in.Offset {
				return Error("INVALID_ARGUMENT")
			}
		}
		out.Record = view(r, tx.Now())
		body = e
		out.OperationId = in.OperationId
		return nil
	}
	if err := s.update(ctx, check); err != nil {
		return nil, err
	}

	if out.Record.State == "available" {
		var data []byte
		var probeErr error
		offset, limit := in.Offset, in.Limit
		if in.Method != "READ" {
			offset = 0
			limit = 0
		}
		if body.File != "" {
			data, probeErr = s.blobs.Read(ctx, body.File, offset, limit, out.Record.Spec.Size, out.Record.Spec.Sha256)
		} else {
			if uint64(len(body.Body)) != out.Record.Spec.Size || digest(body.Body) != out.Record.Spec.Sha256 {
				probeErr = Error("CONTENT_CORRUPT")
			} else {
				data = append([]byte(nil), body.Body[offset:offset+uint64(limit)]...)
			}
		}
		if err := s.update(ctx, check); err != nil {
			return nil, err
		}
		if probeErr != nil {
			if in.Method == "READ" {
				return nil, probeErr
			}
			if out.Record.State == "available" {
				switch Code(probeErr) {
				case "CONTENT_MISSING":
					out.Record.State = "missing"
				case "CONTENT_CORRUPT":
					out.Record.State = "corrupt"
				default:
					out.Record.State = "unavailable"
				}
			}
		} else if in.Method == "READ" {
			out.Data = data
		}
	}

	return out, nil
}
func (s *Service) remove(ctx context.Context, b Binding, in *wire.ContentRequest) (*wire.ContentResponse, error) {
	payload, _ := proto.MarshalOptions{Deterministic: true}.Marshal(&wire.ContentRequest{Method: in.Method, Ref: in.Ref, ExpectedRevision: in.ExpectedRevision, Purpose: in.Purpose})
	hash := digest(payload)
	out := new(wire.ContentResponse)
	err := s.update(ctx, func(tx authorization.ContentTransaction, j *journal) error {
		if _, err := tx.Identity(b.Token); err != nil {
			return err
		}
		if in.Ref.Namespace != b.Namespace {
			return Error("PERMISSION_DENIED")
		}
		e, ok := j.Records[in.Ref.Key]
		if !ok {
			return Error("PERMISSION_DENIED")
		}
		r, err := decode(e)
		if err != nil {
			return err
		}
		id, err := s.authorize(ctx, tx, b, r.Spec, "delete", in.Purpose)
		if err != nil {
			return err
		}
		if _, err = s.authorize(ctx, tx, b, r.Spec, "discover", in.Purpose); err != nil {
			return err
		}
		if old, ok := j.Operations[in.OperationId]; ok {
			if old.Subject != id.Subject {
				return Error("PERMISSION_DENIED")
			}
			if old.Hash != hash || old.Method != "DELETE" {
				return Error("IDENTITY_CONFLICT")
			}
		} else {
			if r.LifecycleRevision != in.ExpectedRevision || r.State != "available" {
				return Error("VERSION_CONFLICT")
			}
			if len(j.Operations) >= 2*s.config.MaxRecords {
				return Error("CAPACITY_EXCEEDED")
			}
			if err = tx.Operation(in.OperationId, id.Subject, true); err != nil {
				return err
			}
			r.State = "cleaning"
			r.LifecycleRevision++
			e.Body = nil
			j.Records[in.Ref.Key] = encode(r, e)
			j.Operations[in.OperationId] = operation{id.Subject, in.Ref.Key, hash, "DELETE"}
		}
		out.Record = view(r, tx.Now())
		out.OperationId = in.OperationId
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
