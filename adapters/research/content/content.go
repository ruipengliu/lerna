// Package content retains actual acquisition facts in governed Content.
// Configuration is trusted host policy; response bytes cannot add source rights.
package content

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"google.golang.org/protobuf/proto"
	"lerna/answers"
	"lerna/artifacts"
	"lerna/fetch"
	wire "lerna/gen/harness/v1"
	"time"
)

type Content interface {
	Call(context.Context, artifacts.Binding, *wire.ContentRequest) (*wire.ContentResponse, error)
}
type Clock interface{ Now() (time.Time, error) }
type Config struct {
	Clock             Clock
	Resource, Purpose string
	RetainUntil       int64
	Sources           map[string]*wire.ContentSource
}
type Adapter struct {
	content Content
	binding artifacts.Binding
	config  Config
	bounds  *readBounds
}

var _ fetch.Evidence = (*Adapter)(nil)

const maxEvidence = 2 << 20

func New(c Content, b artifacts.Binding, cfg Config) (*Adapter, error) {
	if c == nil || cfg.Clock == nil || b.Token == "" || b.Namespace == "" || b.Location == "" || b.Recipient == "" || cfg.Resource == "" || cfg.Purpose == "" || cfg.RetainUntil <= 0 || len(cfg.Sources) == 0 || len(cfg.Sources) > 128 {
		return nil, fetch.Invalid
	}
	sources := make(map[string]*wire.ContentSource, len(cfg.Sources))
	for url, source := range cfg.Sources {
		if len(url) == 0 || len(url) > 4096 || source == nil || source.Kind == "" || source.Key == "" || source.Revision == 0 {
			return nil, fetch.Invalid
		}
		sources[url] = proto.Clone(source).(*wire.ContentSource)
	}
	cfg.Sources = sources
	return &Adapter{content: c, binding: b, config: cfg, bounds: &readBounds{}}, nil
}

// WithContent shares only immutable read lengths while binding all observations
// to another governed consumer. Source policy and binding remain unchanged.
func (a *Adapter) WithContent(content Content) (*Adapter, error) {
	if content == nil {
		return nil, fetch.Invalid
	}
	copy := *a
	copy.content = content
	return &copy, nil
}

func (a *Adapter) sources(r fetch.Result) ([]*wire.ContentSource, error) {
	network := (r.Mode == "" || r.Mode == "http") && r.HTTPStatus == 200 && r.Requests >= 1 && r.Requests <= 5 && len(r.Sources) == r.Requests
	replay := r.Mode == "fixed-replay" && r.HTTPStatus == 0 && r.Requests == 0 && len(r.Sources) == 1
	if (!network && !replay) || r.Sources[0] != r.RequestedURL || r.Sources[len(r.Sources)-1] != r.FinalURL || r.FetchedAt.IsZero() || r.MediaType == "" || len(r.MediaType) > 128 || len(r.Body) > 1<<20 || r.SHA256 != fmt.Sprintf("%x", sha256.Sum256(r.Body)) {
		return nil, fetch.Invalid
	}
	sources := make([]*wire.ContentSource, 0, len(r.Sources))
	for _, url := range r.Sources {
		source := a.config.Sources[url]
		if source == nil {
			return nil, fetch.Denied
		}
		duplicate := false
		for _, old := range sources {
			if proto.Equal(old, source) {
				duplicate = true
				break
			}
		}
		if !duplicate {
			sources = append(sources, proto.Clone(source).(*wire.ContentSource))
		}
	}
	return sources, nil
}
func (a *Adapter) Save(ctx context.Context, op string, r fetch.Result) (string, error) {
	sources, err := a.sources(r)
	if err != nil {
		return "", err
	}
	body, err := json.Marshal(r)
	if err != nil || len(body) > maxEvidence {
		return "", fetch.Invalid
	}
	out, err := a.content.Call(ctx, a.binding, &wire.ContentRequest{Method: "PUT", OperationId: op, Data: body, Spec: &wire.ContentSpec{Kind: "evidence", Resource: a.config.Resource, Purpose: a.config.Purpose, MediaType: "application/json", Sources: sources, AcquiredAt: r.FetchedAt.Unix(), Size: uint64(len(body)), Sha256: fmt.Sprintf("%x", sha256.Sum256(body)), RetainUntil: a.config.RetainUntil}})
	if err != nil {
		return "", failure(err)
	}
	if err = a.check(out.GetRecord()); err != nil {
		return "", err
	}
	ref := answers.Reference(out.Record.Ref)
	// The successful governed PUT already supplied the bounded length. Keep
	// that fact only; the next READ still checks current metadata and authority.
	a.bounds.remember(ref, out.Record.Spec.Size)
	return ref, nil
}
func (a *Adapter) valid(r *wire.ContentRecord) bool {
	return r.GetRef().GetNamespace() == a.binding.Namespace && r.GetSpec().GetKind() == "evidence" && r.GetSpec().GetResource() == a.config.Resource && r.GetSpec().GetPurpose() == a.config.Purpose && r.GetSpec().GetMediaType() == "application/json" && r.GetSpec().GetSize() > 0 && r.GetSpec().GetSize() <= maxEvidence && r.GetSpec().GetRetainUntil() <= a.config.RetainUntil
}

// Classify retention only after matching the governed object's metadata.
func (a *Adapter) check(r *wire.ContentRecord) error {
	// Expired Content can already be cleaning/cleaned; its scope and retention
	// metadata remain governed even after the body fields have been erased.
	if r.GetRef().GetNamespace() != a.binding.Namespace || r.GetSpec().GetResource() != a.config.Resource || r.GetSpec().GetPurpose() != a.config.Purpose || r.GetSpec().GetRetainUntil() <= 0 || r.GetSpec().GetRetainUntil() > a.config.RetainUntil {
		return fetch.Unavailable
	}
	now, err := a.config.Clock.Now()
	if err != nil || now.IsZero() {
		return fetch.Unavailable
	}
	if now.Unix() >= r.Spec.RetainUntil {
		return fetch.Expired
	}
	if !a.valid(r) || r.GetState() != "available" {
		return fetch.Unavailable
	}
	return nil
}
func (a *Adapter) Lookup(ctx context.Context, op string) (string, error) {
	out, err := a.content.Call(ctx, a.binding, &wire.ContentRequest{Method: "LOOKUP", OperationId: op, Purpose: a.config.Purpose})
	if err != nil {
		return "", failure(err)
	}
	if err = a.check(out.GetRecord()); err != nil {
		return "", err
	}
	return answers.Reference(out.Record.Ref), nil
}
func (a *Adapter) Read(ctx context.Context, ref string) (fetch.Result, error) {
	id, err := answers.ParseReference(ref)
	if err != nil || id.Namespace != a.binding.Namespace {
		return fetch.Result{}, fetch.Invalid
	}
	size, known := a.bounds.get(ref)
	var record *wire.ContentRecord
	if !known {
		out, err := a.content.Call(ctx, a.binding, &wire.ContentRequest{Method: "GET", Ref: id, Purpose: a.config.Purpose})
		if err != nil {
			return fetch.Result{}, failure(err)
		}
		if err = a.check(out.GetRecord()); err != nil {
			return fetch.Result{}, err
		}
		record = out.Record
		size = record.Spec.Size
	}
	body := make([]byte, 0, size)
	for offset := uint64(0); offset < size; {
		n := min(uint64(16384), size-offset)
		chunk, e := a.content.Call(ctx, a.binding, &wire.ContentRequest{Method: "READ", Ref: id, Purpose: a.config.Purpose, Offset: offset, Limit: uint32(n)})
		if e != nil {
			// READ rejects unavailable content before returning metadata. Recover
			// the governed expiry classification, never bytes, via current GET.
			// This additional failed-path observation is charged by the caller.
			if artifacts.Code(e) == "CONTENT_UNAVAILABLE" {
				meta, metaErr := a.content.Call(ctx, a.binding, &wire.ContentRequest{Method: "GET", Ref: id, Purpose: a.config.Purpose})
				if metaErr != nil {
					return fetch.Result{}, failure(metaErr)
				}
				if checkErr := a.check(meta.GetRecord()); checkErr != nil {
					return fetch.Result{}, checkErr
				}
			}
			return fetch.Result{}, failure(e)
		}
		if uint64(len(chunk.GetData())) != n {
			return fetch.Result{}, fetch.Unavailable
		}
		if e = a.check(chunk.GetRecord()); e != nil {
			return fetch.Result{}, e
		}
		current := chunk.Record
		if !proto.Equal(current.Ref, id) || current.Spec.Size != size || (record != nil && !proto.Equal(current.Spec, record.Spec)) {
			return fetch.Result{}, fetch.Unavailable
		}
		record = current
		body = append(body, chunk.Data...)
		offset += n
	}
	var result fetch.Result
	if fmt.Sprintf("%x", sha256.Sum256(body)) != record.Spec.Sha256 || json.Unmarshal(body, &result) != nil {
		return fetch.Result{}, fetch.Unavailable
	}
	sources, e := a.sources(result)
	if e != nil {
		return fetch.Result{}, e
	}
	if result.FetchedAt.Unix() != record.Spec.AcquiredAt || !sameSources(sources, record.Spec.Sources) {
		return fetch.Result{}, fetch.Unavailable
	}
	a.bounds.remember(ref, size)
	return result, nil
}
func sameSources(a, b []*wire.ContentSource) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !proto.Equal(a[i], b[i]) {
			return false
		}
	}
	return true
}
func failure(err error) error {
	switch artifacts.Code(err) {
	case "PERMISSION_DENIED":
		return fetch.Denied
	case "INVALID_ARGUMENT":
		return fetch.Invalid
	default:
		return fetch.Unavailable
	}
}
