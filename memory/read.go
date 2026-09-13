package memory

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"google.golang.org/protobuf/proto"
	wire "lerna/gen/harness/v1"
	"strings"
)

type ReadIntent struct{ ID, Method, Collection, Purpose, SemanticSHA256 string }

// ReadPermits binds and revalidates a real grant to the whole read intent.
// Reserve must durably allocate at most once; Validate never grants a new use.
// A returned permit identifies the trusted issuer allocation, not request text.
type ReadPermits interface {
	Reserve(context.Context, Binding, ReadIntent, string) (string, error)
	Validate(context.Context, Binding, ReadIntent, string, string) error
}
type Reader struct {
	service *Service
	store   QueryStore
	permits ReadPermits
}

const Irrecoverable Error = "READ_IRRECOVERABLE"

func NewReader(service *Service, permits ReadPermits) (*Reader, error) {
	if service == nil || permits == nil {
		return nil, Invalid
	}
	store, ok := service.store.(QueryStore)
	if !ok {
		return nil, Invalid
	}
	return &Reader{service, store, permits}, nil
}
func intent(b Binding, id, method, collection, purpose string, request proto.Message) (ReadIntent, error) {
	encoded, e := canonical(request)
	if e != nil {
		return ReadIntent{}, e
	}
	binding, _ := json.Marshal(struct{ Namespace, Subject, Location, Recipient string }{b.Namespace, b.Subject, b.Location, b.Recipient})
	hash := sha256.Sum256(append(append([]byte(method+":"), binding...), encoded...))
	return ReadIntent{id, method, collection, purpose, hex.EncodeToString(hash[:])}, nil
}
func (r *Reader) Query(ctx context.Context, b Binding, request *wire.MemoryQuery, material string) (*wire.MemoryReadResult, error) {
	ctx, cancel := context.WithTimeout(ctx, r.service.config.Timeout)
	defer cancel()
	if !r.service.binding(b) {
		return nil, Denied
	}
	if request == nil || !known(request.ProtoReflect()) || !text(request.ReadId, 256) || !text(request.Collection, 256) || !text(request.Purpose, 256) || len(request.Text) > 1024 || len(request.About) > 512 || request.MaxResults < 1 || request.MaxResults > 32 || request.MaxBytes < 1 || request.MaxBytes > 65536 {
		return nil, Invalid
	}
	if request.Kind != "" {
		switch request.Kind {
		case "fact", "preference", "inference", "experience":
		default:
			return nil, Invalid
		}
	}
	q := proto.Clone(request).(*wire.MemoryQuery)
	in, e := intent(b, q.ReadId, "query", q.Collection, q.Purpose, q)
	if e != nil {
		return nil, e
	}
	permit, e := r.permits.Reserve(ctx, b, in, material)
	if e != nil {
		return nil, e
	}
	original, e := r.store.LookupRead(ctx, b.Namespace, in.ID)
	if e == nil {
		return r.release(ctx, b, in, material, permit, original)
	}
	if e != Missing {
		return nil, e
	}
	rows, e := r.store.Scan(ctx, b.Namespace, q.Collection)
	if e != nil {
		return nil, e
	}
	if len(rows) > 512 {
		return nil, Capacity
	}
	chosen := ReadBinding{Namespace: b.Namespace, ID: in.ID, Subject: b.Subject, SemanticSHA256: in.SemanticSHA256, PermitID: permit, Coverage: "complete"}
	// max_bytes bounds the encoded records; fixed coverage metadata is separate.
	body := &wire.MemoryReadResult{}
	partial, budget := false, false
	for _, row := range rows {
		if ctx.Err() != nil {
			return nil, Unavailable
		}
		record, e := decode(row)
		if e != nil {
			return nil, e
		}
		if record.Ref.Namespace != b.Namespace || record.Ref.Collection != q.Collection {
			return nil, Unavailable
		}
		if record.Spec.Purpose != q.Purpose {
			continue
		}
		// Discover is checked independently: unavailable private sources must not
		// leak through a "partial coverage" flag to an unauthorized recipient.
		if e = r.service.check(ctx, b, record.Ref, record.Spec, "discover"); e != nil {
			if e == Denied {
				continue
			}
			return nil, e
		}
		if e = r.service.check(ctx, b, record.Ref, record.Spec, "read"); e != nil {
			if e == Denied {
				continue
			}
			if e == Unavailable {
				partial = true
				if chosen.PartialWitness == nil {
					chosen.PartialWitness = &VersionRef{Ref: row.Ref, Revision: row.Revision}
				}
				continue
			}
			return nil, e
		}
		now, e := r.service.clock.Now()
		if e != nil {
			return nil, Unavailable
		}
		if record.Spec.ValidFrom != nil && now.Unix() < *record.Spec.ValidFrom || record.Spec.ValidUntil != nil && now.Unix() >= *record.Spec.ValidUntil {
			continue
		}
		if r.service.validator.Validate(record.Spec.Content) != nil {
			partial = true
			if chosen.PartialWitness == nil {
				chosen.PartialWitness = &VersionRef{Ref: row.Ref, Revision: row.Revision}
			}
			continue
		}
		if q.Kind != "" && record.Spec.Kind != q.Kind || q.About != "" && record.Spec.About != q.About || q.Text != "" && !strings.Contains(strings.ToLower(string(record.Spec.Content.Json)), strings.ToLower(q.Text)) {
			continue
		}
		body.Records = append(body.Records, record)
		if len(body.Records) > int(q.MaxResults) || proto.Size(body) > int(q.MaxBytes) {
			body.Records = body.Records[:len(body.Records)-1]
			budget = true
			if chosen.BudgetWitness == nil {
				chosen.BudgetWitness = &VersionRef{Ref: row.Ref, Revision: row.Revision}
			}
			continue
		}
		chosen.Results = append(chosen.Results, VersionRef{Ref: row.Ref, Revision: row.Revision})
	}
	if partial {
		chosen.Coverage = "partial_unavailable"
	}
	if budget {
		chosen.Coverage = "budget_exhausted"
		if partial {
			chosen.Coverage = "partial_and_budget_exhausted"
		}
	}
	if e = r.permits.Validate(ctx, b, in, material, permit); e != nil {
		return nil, e
	}
	bound, e := r.store.BindRead(ctx, chosen)
	if e != nil {
		return nil, e
	}
	return r.release(ctx, b, in, material, permit, bound)
}
func (r *Reader) Get(ctx context.Context, b Binding, request *wire.MemoryGet, material string) (*wire.MemoryReadResult, error) {
	ctx, cancel := context.WithTimeout(ctx, r.service.config.Timeout)
	defer cancel()
	if !r.service.binding(b) {
		return nil, Denied
	}
	if request == nil || !known(request.ProtoReflect()) || !validReference(request.Ref) || !text(request.ReadId, 256) || !text(request.Purpose, 256) || request.Revision == 0 || request.Revision > 1<<32 {
		return nil, Invalid
	}
	q := proto.Clone(request).(*wire.MemoryGet)
	if q.Ref.Namespace != b.Namespace {
		return nil, Denied
	}
	in, e := intent(b, q.ReadId, "get", q.Ref.Collection, q.Purpose, q)
	if e != nil {
		return nil, e
	}
	permit, e := r.permits.Reserve(ctx, b, in, material)
	if e != nil {
		return nil, e
	}
	original, e := r.store.LookupRead(ctx, b.Namespace, in.ID)
	if e == nil {
		return r.release(ctx, b, in, material, permit, original)
	}
	if e != Missing {
		return nil, e
	}
	row, e := r.store.Read(ctx, refOf(q.Ref), q.Revision)
	if e != nil {
		if e == Missing {
			return nil, Denied
		}
		return nil, e
	}
	record, e := decode(row)
	if e != nil {
		return nil, e
	}
	if record.Spec.Purpose != q.Purpose {
		return nil, Denied
	}
	if e = r.service.check(ctx, b, record.Ref, record.Spec, "read"); e != nil {
		return nil, e
	}
	if r.service.validator.Validate(record.Spec.Content) != nil {
		return nil, Unavailable
	}
	if e = r.permits.Validate(ctx, b, in, material, permit); e != nil {
		return nil, e
	}
	bound, e := r.store.BindRead(ctx, ReadBinding{Namespace: b.Namespace, ID: in.ID, Subject: b.Subject, SemanticSHA256: in.SemanticSHA256, PermitID: permit, Coverage: "complete", Results: []VersionRef{{Ref: row.Ref, Revision: row.Revision}}})
	if e != nil {
		return nil, e
	}
	return r.release(ctx, b, in, material, permit, bound)
}
func (r *Reader) release(ctx context.Context, b Binding, in ReadIntent, material, permit string, bound ReadBinding) (*wire.MemoryReadResult, error) {
	if bound.Namespace != b.Namespace || bound.ID != in.ID || bound.Subject != b.Subject || bound.SemanticSHA256 != in.SemanticSHA256 || bound.PermitID != permit {
		return nil, IdentityConflict
	}
	if len(bound.Results) > 32 {
		return nil, Irrecoverable
	}
	if e := r.permits.Validate(ctx, b, in, material, permit); e != nil {
		return nil, e
	}
	witnesses, e := bound.CoverageReferences()
	if e != nil {
		return nil, Irrecoverable
	}
	var coverageRecords []*wire.MemoryRecord
	for _, ref := range witnesses {
		if ref.Ref.Namespace != b.Namespace || ref.Ref.Collection != in.Collection {
			return nil, Irrecoverable
		}
		row, e := r.store.Read(ctx, ref.Ref, ref.Revision)
		if e != nil {
			return nil, Irrecoverable
		}
		record, e := decode(row)
		if e != nil {
			return nil, Irrecoverable
		}
		coverageRecords = append(coverageRecords, record)
	}
	out := &wire.MemoryReadResult{Coverage: bound.Coverage}
	for _, ref := range bound.Results {
		if ref.Ref.Namespace != b.Namespace || ref.Ref.Collection != in.Collection {
			return nil, Irrecoverable
		}
		row, e := r.store.Read(ctx, ref.Ref, ref.Revision)
		if e != nil {
			return nil, Irrecoverable
		}
		record, e := decode(row)
		if e != nil {
			return nil, Irrecoverable
		}
		if record.Spec.Purpose != in.Purpose {
			return nil, Denied
		}
		if e = r.service.check(ctx, b, record.Ref, record.Spec, "read"); e != nil {
			return nil, e
		}
		if r.service.validator.Validate(record.Spec.Content) != nil {
			return nil, Irrecoverable
		}
		out.Records = append(out.Records, record)
	}
	// Coverage is a disclosure too, even when no record body was selected.
	for i, record := range coverageRecords {
		if record.Spec.Purpose != in.Purpose {
			return nil, Denied
		}
		if e := r.service.check(ctx, b, record.Ref, record.Spec, "discover"); e != nil {
			return nil, e
		}
		e := r.service.check(ctx, b, record.Ref, record.Spec, "read")
		partial := bound.PartialWitness != nil && i == 0
		if e != nil && !(partial && e == Unavailable) {
			return nil, e
		}
	}
	// Recheck all selected source restrictions after reconstruction; a later
	// record lookup cannot invalidate an earlier check unnoticed within the call.
	for _, record := range out.Records {
		if e := r.service.check(ctx, b, record.Ref, record.Spec, "read"); e != nil {
			return nil, e
		}
	}
	if e := r.permits.Validate(ctx, b, in, material, permit); e != nil {
		return nil, e
	}
	refs := append(append([]VersionRef(nil), bound.Results...), witnesses...)
	if e := r.store.ValidateVersions(ctx, refs); e != nil {
		if e == Missing {
			return nil, Irrecoverable
		}
		return nil, e
	}
	return out, nil
}

// DescribeQuery provides the exact intent for a grant issuer or SDK. This is
// descriptive metadata only; it neither authorizes a query nor allocates a use.
func DescribeQuery(b Binding, q *wire.MemoryQuery) (ReadIntent, error) {
	if q == nil {
		return ReadIntent{}, Invalid
	}
	return intent(b, q.ReadId, "query", q.Collection, q.Purpose, q)
}
func DescribeGet(b Binding, q *wire.MemoryGet) (ReadIntent, error) {
	if q == nil || q.Ref == nil {
		return ReadIntent{}, Invalid
	}
	return intent(b, q.ReadId, "get", q.Ref.Collection, q.Purpose, q)
}
