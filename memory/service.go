package memory

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	wire "lerna/gen/harness/v1"
	"lerna/internal/jsonvalue"
	"time"
)

// Binding is trusted authenticated host context, never copied from a request.
type Binding struct{ Token, Namespace, Subject, Location, Recipient string }

// Authority checks current policy and every source; policy references in a
// request do not grant rights. Check must be bounded and safe to repeat.
type Checker interface {
	Check(context.Context, Binding, *wire.MemoryRef, *wire.MemorySpec, string) error
}
type Authority interface {
	Checker
	Admit(context.Context, Binding, string, string, *wire.MemoryRef, *wire.MemorySpec, string) (int64, error)
	Inspect(context.Context, Binding, string) (AdmissionState, error)
}
type Validator interface {
	Validate(*wire.DynamicPayload) error
}
type Clock interface{ Now() (time.Time, error) }
type Config struct {
	Location string
	Timeout  time.Duration
}
type Service struct {
	deletionReporter DeletionReporting
	store            Store
	authority        Authority
	validator        Validator
	clock            Clock
	config           Config
}

const Denied Error = "PERMISSION_DENIED"

func New(store Store, authority Authority, validator Validator, clock Clock, config Config) (*Service, error) {
	if store == nil || authority == nil || validator == nil || clock == nil || !text(config.Location, 256) || config.Timeout <= 0 || config.Timeout > 10*time.Second {
		return nil, Invalid
	}
	return &Service{store: store, authority: authority, validator: validator, clock: clock, config: config}, nil
}
func text(s string, max int) bool { return len(s) > 0 && len(s) <= max }
func known(m protoreflect.Message) bool {
	if len(m.GetUnknown()) != 0 {
		return false
	}
	ok := true
	m.Range(func(f protoreflect.FieldDescriptor, v protoreflect.Value) bool {
		if f.Kind() == protoreflect.MessageKind {
			if f.IsList() {
				l := v.List()
				for i := 0; i < l.Len(); i++ {
					if !known(l.Get(i).Message()) {
						ok = false
						break
					}
				}
			} else if !f.IsMap() {
				ok = known(v.Message())
			}
		}
		return ok
	})
	return ok
}
func validSpec(s *wire.MemorySpec) bool {
	if s == nil || s.Content == nil || s.Confidence == nil || !known(s.ProtoReflect()) || proto.Size(s) > 12000 {
		return false
	}
	switch s.Kind {
	case "fact", "preference", "inference", "experience":
	default:
		return false
	}
	if !text(s.About, 512) || !text(s.Conditions, 1024) || !text(s.PolicyRef, 256) || !text(s.Purpose, 256) || s.RecordedAt <= 0 || s.RetainUntil <= s.RecordedAt || len(s.Sources) < 1 || len(s.Sources) > 16 {
		return false
	}
	if !text(s.Confidence.Assessment, 128) || !text(s.Confidence.Basis, 1024) || !text(s.Confidence.Method, 128) {
		return false
	}
	if s.OccurredAt != nil && *s.OccurredAt <= 0 || s.ValidFrom != nil && *s.ValidFrom <= 0 || s.ValidUntil != nil && *s.ValidUntil <= 0 {
		return false
	}
	if s.ValidFrom != nil && s.ValidUntil != nil && *s.ValidFrom >= *s.ValidUntil {
		return false
	}
	for _, source := range s.Sources {
		if source == nil || source.Ref == nil || !text(source.Ref.Kind, 128) || !text(source.Ref.Key, 256) || source.Ref.Revision == 0 || !text(source.Method, 128) || source.Fragment != nil && !text(*source.Fragment, 512) {
			return false
		}
	}
	return true
}
func (s *Service) binding(b Binding) bool {
	return text(b.Token, 16384) && text(b.Subject, 256) && text(b.Namespace, 256) && b.Location == s.config.Location && text(b.Recipient, 256)
}
func refOf(r *wire.MemoryRef) Ref { return Ref{r.GetNamespace(), r.GetCollection(), r.GetKey()} }
func validReference(r *wire.MemoryRef) bool {
	return r != nil && known(r.ProtoReflect()) && text(r.Namespace, 256) && text(r.Collection, 256) && text(r.Key, 256)
}
func (s *Service) check(ctx context.Context, b Binding, r *wire.MemoryRef, spec *wire.MemorySpec, action string) error {
	if !s.binding(b) || r.Namespace != b.Namespace {
		return Denied
	}
	now, e := s.clock.Now()
	if e != nil {
		return Unavailable
	}
	if spec.RecordedAt > now.Unix() || spec.RetainUntil <= now.Unix() || spec.RetainUntil-now.Unix() > 31536000 {
		return Denied
	}
	if e = s.authority.Check(ctx, b, proto.Clone(r).(*wire.MemoryRef), proto.Clone(spec).(*wire.MemorySpec), action); e != nil {
		return e
	}
	return nil
}
func canonical(m proto.Message) ([]byte, error) {
	raw, e := protojson.Marshal(m)
	if e != nil {
		return nil, Invalid
	}
	value, e := jsonvalue.Decode(raw)
	if e != nil {
		return nil, Invalid
	}
	return json.Marshal(value)
}
func (s *Service) Put(ctx context.Context, b Binding, in *wire.MemoryWrite) (Receipt, error) {
	if in == nil || in.ExpectedRevision != 0 {
		return Receipt{}, Invalid
	}
	return s.write(ctx, b, in, "put")
}
func (s *Service) Correct(ctx context.Context, b Binding, in *wire.MemoryWrite) (Receipt, error) {
	if in == nil || in.ExpectedRevision == 0 {
		return Receipt{}, Invalid
	}
	return s.write(ctx, b, in, "correct")
}
func (s *Service) write(ctx context.Context, b Binding, request *wire.MemoryWrite, method string) (Receipt, error) {
	ctx, cancel := context.WithTimeout(ctx, s.config.Timeout)
	defer cancel()
	if !s.binding(b) {
		return Receipt{}, Denied
	}
	if request == nil || !known(request.ProtoReflect()) || !validReference(request.Ref) || !text(request.OperationId, 256) || request.ExpectedRevision >= 1<<32 || !validSpec(request.Spec) {
		return Receipt{}, Invalid
	}
	in := proto.Clone(request).(*wire.MemoryWrite)
	if e := s.validator.Validate(in.Spec.Content); e != nil {
		return Receipt{}, Invalid
	}
	value, e := jsonvalue.Decode(in.Spec.Content.Json)
	if e != nil {
		return Receipt{}, Invalid
	}
	in.Spec.Content.Json, e = json.Marshal(value)
	if e != nil {
		return Receipt{}, Invalid
	}
	if e = s.check(ctx, b, in.Ref, in.Spec, method); e != nil {
		return Receipt{}, e
	}
	// A deleted operation retains acceptance metadata, never the erased payload
	// comparison basis. Reject its resubmission before loading the old revision.
	original, lookupErr := s.store.LookupOperation(ctx, b.Namespace, in.OperationId)
	if lookupErr != nil && lookupErr != Missing {
		return Receipt{}, lookupErr
	}
	if lookupErr == nil && original.Subject == b.Subject && original.Ref == refOf(in.Ref) && original.SemanticSHA256 == "" {
		admission, err := s.authority.Inspect(ctx, b, in.OperationId)
		if err != nil {
			return Receipt{}, err
		}
		if !admission.Reserved {
			return Receipt{}, Unavailable
		}
		return Receipt{}, ReplayUnavailable
	}
	// Existing content restrictions must also authorize a correction; changing
	// the incoming policy reference cannot erase the old source's constraints.
	var previous *wire.MemoryRecord
	if in.ExpectedRevision > 0 {
		row, e := s.store.Read(ctx, refOf(in.Ref), in.ExpectedRevision)
		if e != nil {
			return Receipt{}, e
		}
		previous, e = decode(row)
		if e != nil {
			return Receipt{}, e
		}
		if e = s.check(ctx, b, previous.Ref, previous.Spec, "correct"); e != nil {
			return Receipt{}, e
		}
	}
	semantic, e := canonical(in)
	if e != nil {
		return Receipt{}, e
	}
	hash := sha256.Sum256(append([]byte(method+":"), semantic...))
	record := &wire.MemoryRecord{Ref: in.Ref, Revision: in.ExpectedRevision + 1, PreviousRevision: in.ExpectedRevision, OperationId: in.OperationId, Spec: in.Spec}
	document, e := canonical(record)
	if e != nil {
		return Receipt{}, e
	}
	if previous != nil {
		if e = s.check(ctx, b, previous.Ref, previous.Spec, "correct"); e != nil {
			return Receipt{}, e
		}
	}
	if e = s.check(ctx, b, in.Ref, in.Spec, method); e != nil {
		return Receipt{}, e
	}
	// Recover original durable acceptance before consulting a new admission
	// window. An expired window never destroys a committed operation receipt.
	if lookupErr == Missing {
		expires, e := s.authority.Admit(ctx, b, in.OperationId, hex.EncodeToString(hash[:]), in.Ref, in.Spec, method)
		if e != nil {
			return Receipt{}, e
		}
		now, e := s.clock.Now()
		if e != nil {
			return Receipt{}, Unavailable
		}
		remaining := time.Unix(0, expires).Sub(now)
		if remaining <= 0 {
			return Receipt{}, AdmissionExpired
		}
		bounded, cancelAdmission := context.WithTimeout(ctx, remaining)
		defer cancelAdmission()
		ctx = bounded
	}
	result, e := s.store.Commit(ctx, Change{OperationID: in.OperationId, Subject: b.Subject, SemanticSHA256: hex.EncodeToString(hash[:]), Expected: in.ExpectedRevision, Record: Revision{Ref: refOf(in.Ref), Revision: record.Revision, Document: document}})
	if e != nil {
		return Receipt{}, e
	}
	if e = s.check(ctx, b, in.Ref, in.Spec, "discover"); e != nil {
		return Receipt{}, e
	}
	return result, nil
}
func decode(row Revision) (*wire.MemoryRecord, error) {
	r := new(wire.MemoryRecord)
	if len(row.Document) > 16384 || protojson.Unmarshal(row.Document, r) != nil || !validReference(r.Ref) || refOf(r.Ref) != row.Ref || r.Revision != row.Revision || r.Revision == 0 || r.PreviousRevision != r.Revision-1 || !text(r.OperationId, 256) || !validSpec(r.Spec) {
		return nil, Unavailable
	}
	return r, nil
}
func (s *Service) LookupOperation(ctx context.Context, b Binding, operation string) (Receipt, error) {
	ctx, cancel := context.WithTimeout(ctx, s.config.Timeout)
	defer cancel()
	if !s.binding(b) || !text(operation, 256) {
		return Receipt{}, Denied
	}
	receipt, e := s.store.LookupOperation(ctx, b.Namespace, operation)
	if e != nil {
		return Receipt{}, Denied
	}
	if receipt.Subject != b.Subject {
		return Receipt{}, Denied
	}
	row, e := s.store.Read(ctx, receipt.Ref, receipt.Revision)
	if e != nil {
		return Receipt{}, Denied
	}
	record, e := decode(row)
	if e != nil {
		return Receipt{}, Unavailable
	}
	if e = s.check(ctx, b, record.Ref, record.Spec, "discover"); e != nil {
		return Receipt{}, e
	}
	return receipt, nil
}
