// Package artifacts governs controlled content independently of its storage adapters.
package artifacts

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"lerna/authorization"
	wire "lerna/gen/harness/v1"
	"lerna/internal/randomid"
	"lerna/internal/taskwire"
	"time"
)

type Authority interface {
	UpdateContent(context.Context, func(authorization.ContentTransaction) error) error
}

// Binding is trusted host context, never accepted from a content request.
type Binding struct{ Token, Namespace, Location, Recipient string }

// Sources resolves exact revisions against current trusted policy. Check must be
// bounded and side-effect free; it may be called again after a CAS conflict.
type Sources interface {
	Check(context.Context, *wire.ContentSource, string, string, string, int64) error
}

// Blobs serializes access across processes sharing a content directory. All other
// methods are called while Lock is held. No public client receives these keys.
type Blobs interface {
	Lock(context.Context) (func(), error)
	Put(context.Context, string, []byte) error
	Read(context.Context, string, uint64, uint32, uint64, string) ([]byte, error)
	Remove(context.Context, string) error
	List(context.Context, int) ([]Blob, error)
}
type Blob struct {
	Key  string
	Size uint64
}
type Config struct {
	Inline, MaxObject, MaxTotal                  uint64
	MaxRecords, MaxChunk, MaxFiles, CleanupBatch int
	Timeout, Retention                           time.Duration
}

func (c Config) valid() bool {
	return c.Inline > 0 && c.Inline <= c.MaxObject && c.MaxObject <= 1<<20 && c.MaxTotal >= c.MaxObject && c.MaxTotal <= 8<<20 && c.MaxRecords > 0 && c.MaxRecords <= 256 && c.MaxChunk > 0 && c.MaxChunk <= 65536 && c.MaxFiles >= c.MaxRecords && c.MaxFiles <= 1024 && c.CleanupBatch > 0 && c.CleanupBatch <= c.MaxFiles && c.Timeout > 0 && c.Timeout <= 10*time.Second && c.Retention >= time.Second && c.Retention <= 24*time.Hour
}

type Service struct {
	authority Authority
	blobs     Blobs
	sources   Sources
	config    Config
}

func New(a Authority, b Blobs, s Sources, c Config) (*Service, error) {
	if a == nil || b == nil || s == nil || !c.valid() {
		return nil, Error("INVALID_ARGUMENT")
	}
	return &Service{a, b, s, c}, nil
}

type Error string

func (e Error) Error() string { return string(e) }
func Code(err error) string {
	var a *authorization.Error
	if errors.As(err, &a) {
		return string(a.Code)
	}
	var e Error
	if errors.As(err, &e) {
		return string(e)
	}
	return "UNAVAILABLE"
}

type entry struct {
	Record json.RawMessage
	Body   []byte
	File   string
	UseID  string
}
type operation struct{ Subject, Key, Hash, Method string }
type journal struct {
	InvalidatedRevisions map[string]bool   `json:",omitempty"`
	Invalidated          map[string]uint64 `json:",omitempty"`
	Config               Config
	Records              map[string]entry
	Operations           map[string]operation
}

func decode(e entry) (*wire.ContentRecord, error) {
	r := new(wire.ContentRecord)
	if err := protojson.Unmarshal(e.Record, r); err != nil {
		return nil, Error("UNAVAILABLE")
	}
	return r, nil
}
func encode(r *wire.ContentRecord, e entry) entry { e.Record, _ = protojson.Marshal(r); return e }
func digest(b []byte) string                      { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func (s *Service) update(ctx context.Context, fn func(authorization.ContentTransaction, *journal) error) error {
	return s.authority.UpdateContent(ctx, func(tx authorization.ContentTransaction) error {
		j := journal{Config: s.config, Records: map[string]entry{}, Operations: map[string]operation{}}
		if len(tx.Data()) > 0 {
			if json.Unmarshal(tx.Data(), &j) != nil || j.Config != s.config || j.Records == nil || j.Operations == nil {
				return Error("UNAVAILABLE")
			}
		}
		if j.Invalidated == nil {
			j.Invalidated = map[string]uint64{}
		}
		if j.InvalidatedRevisions == nil {
			j.InvalidatedRevisions = map[string]bool{}
		}
		if len(j.Invalidated)+len(j.InvalidatedRevisions) > maxInvalidatedSources {
			return Error("UNAVAILABLE")
		}
		if err := s.invalidateUses(tx, &j); err != nil {
			return err
		}
		if err := fn(tx, &j); err != nil {
			return err
		}
		// Enforce the persisted source watermark even if a stale source adapter
		// would otherwise permit a new PUT. The whole journal write rolls back.
		if len(j.Invalidated)+len(j.InvalidatedRevisions) > 0 {
			for _, e := range j.Records {
				r, err := decode(e)
				if err != nil {
					return err
				}
				if r.State == "available" && invalidated(&j, r.Ref.Namespace, r.Spec.Sources) {
					return Error("PERMISSION_DENIED")
				}
			}
		}

		b, err := json.Marshal(j)
		if err != nil {
			return Error("UNAVAILABLE")
		}
		tx.SetData(b)
		return nil
	})
}
func (s *Service) authorize(ctx context.Context, tx authorization.ContentTransaction, b Binding, spec *wire.ContentSpec, action, purpose string) (authorization.Identity, error) {
	if purpose != spec.Purpose {
		return authorization.Identity{}, Error("PERMISSION_DENIED")
	}
	if b.Namespace != tx.Namespace() || b.Location == "" || b.Recipient == "" {
		return authorization.Identity{}, Error("PERMISSION_DENIED")
	}
	loc := b.Location
	if action == "disclose" || action == "discover" {
		loc = b.Recipient
	}
	id, err := tx.Authorize(b.Token, &wire.AuthorizationAction{Resource: spec.Resource, Action: "content." + action, Purpose: purpose, Location: loc})
	if err != nil {
		return id, err
	}
	for _, src := range spec.Sources {
		if err := s.checkSource(ctx, tx, b, proto.Clone(src).(*wire.ContentSource), action, purpose, loc, spec.RetainUntil); err != nil {
			return id, Error("PERMISSION_DENIED")
		}
	}
	return id, nil
}
func available(r *wire.ContentRecord, now time.Time) bool {
	return r.State == "available" && now.Unix() < r.Spec.RetainUntil
}
func view(r *wire.ContentRecord, now time.Time) *wire.ContentRecord {
	r = proto.Clone(r).(*wire.ContentRecord)
	if r.State == "available" && now.Unix() >= r.Spec.RetainUntil {
		r.State = "expired"
	}
	return r
}
func (s *Service) Call(ctx context.Context, b Binding, in *wire.ContentRequest) (*wire.ContentResponse, error) {
	if in == nil || proto.Size(in) > int(s.config.MaxObject)+16384 || !taskwire.Known(in.ProtoReflect()) {
		return nil, Error("INVALID_ARGUMENT")
	}
	in = proto.Clone(in).(*wire.ContentRequest)
	if err := validate(in, s.config); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, s.config.Timeout)
	defer cancel()
	unlock, err := s.blobs.Lock(ctx)
	if err != nil {
		return nil, err
	}
	defer unlock()
	switch in.Method {
	case "PUT":
		return s.put(ctx, b, in)
	case "GET", "READ", "LOOKUP":
		return s.get(ctx, b, in)
	case "DELETE":
		return s.remove(ctx, b, in)
	}
	return nil, Error("UNSUPPORTED")
}
func (s *Service) put(ctx context.Context, b Binding, in *wire.ContentRequest) (*wire.ContentResponse, error) {
	hashData, _ := proto.MarshalOptions{Deterministic: true}.Marshal(&wire.ContentRequest{Method: "PUT", Spec: in.Spec, Data: in.Data})
	hash := digest(hashData)
	key, err := randomid.New()
	if err != nil {
		return nil, Error("UNAVAILABLE")
	}
	out := new(wire.ContentResponse)
	replay := false
	check := func(tx authorization.ContentTransaction, j *journal) error {
		replay = false
		id, err := s.authorize(ctx, tx, b, in.Spec, "store", in.Spec.Purpose)
		if err != nil {
			return err
		}
		if old, ok := j.Operations[in.OperationId]; ok {
			if old.Subject != id.Subject {
				return Error("PERMISSION_DENIED")
			}
			if old.Method == "PUT" && old.Hash == "" {
				return Error("CONTENT_UNAVAILABLE")
			}
			if old.Hash != hash || old.Method != "PUT" {
				return Error("IDENTITY_CONFLICT")
			}
			r, err := decode(j.Records[old.Key])
			if err != nil {
				return err
			}
			if _, err = s.authorize(ctx, tx, b, r.Spec, "discover", in.Spec.Purpose); err != nil {
				return err
			}
			out.Record = view(r, tx.Now())
			out.OperationId = in.OperationId
			replay = true
			return nil
		}
		if err := tx.Operation(in.OperationId, id.Subject, false); err != nil {
			return err
		}
		for _, action := range []string{"process", "retain", "discover"} {
			if _, err := s.authorize(ctx, tx, b, in.Spec, action, in.Spec.Purpose); err != nil {
				return err
			}
		}
		if in.Spec.AcquiredAt > tx.Now().Unix() || in.Spec.RetainUntil <= tx.Now().Unix() || in.Spec.RetainUntil > tx.Now().Add(s.config.Retention).Unix() {
			return Error("INVALID_ARGUMENT")
		}
		if len(j.Records) >= s.config.MaxRecords || len(j.Operations) >= 2*s.config.MaxRecords {
			return Error("CAPACITY_EXCEEDED")
		}
		var total uint64
		for _, e := range j.Records {
			r, err := decode(e)
			if err != nil {
				return err
			}
			if r.State != "cleaned" {
				total += r.Spec.Size
			}
		}
		if total+in.Spec.Size > s.config.MaxTotal {
			return Error("CAPACITY_EXCEEDED")
		}
		return nil
	}
	if err = s.update(ctx, check); err != nil {
		return nil, err
	}
	if replay {
		return s.get(ctx, b, &wire.ContentRequest{Method: "LOOKUP", OperationId: in.OperationId, Purpose: in.Spec.Purpose})
	}
	file := ""
	if in.Spec.Size > s.config.Inline {
		files, err := s.blobs.List(ctx, s.config.MaxFiles)
		if err != nil {
			return nil, err
		}
		var total uint64
		for _, f := range files {
			total += f.Size
		}
		if len(files) >= s.config.MaxFiles || total+in.Spec.Size > s.config.MaxTotal {
			return nil, Error("CAPACITY_EXCEEDED")
		}
		file = key
		if err = s.blobs.Put(ctx, file, in.Data); err != nil {
			return nil, err
		}
		if _, err = s.blobs.Read(ctx, file, 0, 1, in.Spec.Size, in.Spec.Sha256); err != nil {
			return nil, err
		}
	}
	err = s.update(ctx, func(tx authorization.ContentTransaction, j *journal) error {
		tracked := &useTransaction{ContentTransaction: tx}
		if err := check(tracked, j); err != nil {
			return err
		}
		if replay {
			return nil
		}
		id, err := s.authorize(ctx, tracked, b, in.Spec, "store", in.Spec.Purpose)
		if err != nil {
			return err
		}
		if err = tx.Operation(in.OperationId, id.Subject, true); err != nil {
			return err
		}
		r := &wire.ContentRecord{Ref: &wire.ContentRef{Namespace: b.Namespace, Key: key, Revision: 1}, Spec: in.Spec, LifecycleRevision: 1, State: "available"}
		useID := "artifact." + key
		if err = tx.RegisterUse(b.Token, authorization.UseSpec{Namespace: b.Namespace, ID: useID, Consumer: "artifacts", ConfigSHA256: s.useConfig(), Until: in.Spec.RetainUntil, Actions: tracked.actions}); err != nil {
			return err
		}
		e := entry{File: file, UseID: useID}
		if file == "" {
			e.Body = append([]byte(nil), in.Data...)
		}
		j.Records[key] = encode(r, e)
		j.Operations[in.OperationId] = operation{id.Subject, key, hash, "PUT"}
		out.Record = r
		out.OperationId = in.OperationId
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
