package decisionfixture

// Temporary original-bounds diagnostic overlay. Every dependency call is
// forwarded once with its original context, arguments and returned error.
import (
	"context"
	"errors"
	"fmt"
	adapter "github.com/ruipengliu/lerna/adapters/content/decision"
	engine "github.com/ruipengliu/lerna/components/decision_engine"
	"github.com/ruipengliu/lerna/contract"
	v "github.com/ruipengliu/lerna/contract/v1_1"
	c "github.com/ruipengliu/lerna/contract/v1_2"
	"github.com/ruipengliu/lerna/domain/content"
	compiler "github.com/ruipengliu/lerna/domain/task/context"
	rt "github.com/ruipengliu/lerna/runtime"
	"github.com/ruipengliu/lerna/runtime/workpool"
	"sort"
	"sync"
	"testing"
	"time"
)

type claimDiagnosticEvent struct {
	Kind                    string
	At, Duration, Remaining time.Duration
	Detail, Cause           string
}
type claimDiagnosticAggregate struct {
	Count      int
	Total, Max time.Duration
}
type ContextClaimDiagnostics struct {
	mu         sync.Mutex
	start      time.Time
	ctx        context.Context
	events     []claimDiagnosticEvent
	critical   []claimDiagnosticEvent
	dropped    int
	firstCause error
	first      *claimDiagnosticEvent
	totals     map[string]claimDiagnosticAggregate
	lastGate   map[string]claimDiagnosticEvent
}

func NewContextClaimDiagnostics(t *testing.T, ctx context.Context) *ContextClaimDiagnostics {
	d := &ContextClaimDiagnostics{start: time.Now(), ctx: ctx, events: make([]claimDiagnosticEvent, 0, 448), critical: make([]claimDiagnosticEvent, 0, 64), totals: map[string]claimDiagnosticAggregate{}, lastGate: map[string]claimDiagnosticEvent{}}
	d.Mark("phase.testcase-start")
	t.Cleanup(func() { d.summary(t) })
	return d
}
func claimCause(err error) string {
	if err == nil {
		return "nil"
	}
	out := fmt.Sprintf("%T", err)
	for _, cause := range []struct {
		err  error
		name string
	}{{context.DeadlineExceeded, "context_deadline"}, {context.Canceled, "context_canceled"}, {rt.ErrClaim, "claim"}, {rt.ErrCommitUnknown, "commit_unknown"}, {engine.ErrInputLimit, "input_limit"}, {engine.ErrUnavailable, "dependency_unavailable"}, {engine.ErrForbidden, "forbidden"}, {compiler.ErrOverflow, "context_overflow"}} {
		if errors.Is(err, cause.err) {
			out += ":" + cause.name
		}
	}
	return out
}
func (d *ContextClaimDiagnostics) observe(kind string, start time.Time, err error, detail string) {
	if d == nil {
		return
	}
	now := time.Now()
	remaining := time.Duration(0)
	if deadline, ok := d.ctx.Deadline(); ok {
		remaining = deadline.Sub(now)
	}
	e := claimDiagnosticEvent{Kind: kind, At: now.Sub(d.start), Duration: now.Sub(start), Remaining: remaining, Detail: detail, Cause: claimCause(err)}
	d.mu.Lock()
	defer d.mu.Unlock()
	total := d.totals[kind]
	total.Count++
	total.Total += e.Duration
	if e.Duration > total.Max {
		total.Max = e.Duration
	}
	d.totals[kind] = total
	important := len(kind) >= 6 && kind[:6] == "phase." || kind == "store.Claim" || kind == "store.SaveDecision" || kind == "store.WithinSave"
	if important {
		if len(d.critical) < 64 {
			d.critical = append(d.critical, e)
		} else {
			d.dropped++
		}
	} else {
		if len(d.events) < 448 {
			d.events = append(d.events, e)
		} else {
			d.dropped++
		}
	}
	if err != nil && d.first == nil {
		copy := e
		d.first = &copy
		d.firstCause = err
	}
	if kind == "store.ValidateClaim" || kind == "store.ValidatePoolClaim" {
		d.lastGate[kind] = e
	}
}
func (d *ContextClaimDiagnostics) Mark(kind string) { d.observe(kind, time.Now(), nil, "") }
func (d *ContextClaimDiagnostics) Stage(ctx context.Context, kind string, start time.Time, err error) {
	d.observe(kind, start, err, "")
}
func (d *ContextClaimDiagnostics) summary(t *testing.T) {
	d.mu.Lock()
	defer d.mu.Unlock()
	t.Logf("CLAIM_DIAG bounded_events=%d dropped=%d inclusive_totals_do_not_sum=true", len(d.events)+len(d.critical), d.dropped)
	print := func(label string, e claimDiagnosticEvent) {
		t.Logf("CLAIM_DIAG %s kind=%s at=%s duration=%s caller_remaining=%s cause=%s %s", label, e.Kind, e.At, e.Duration, e.Remaining, e.Cause, e.Detail)
	}
	if d.first != nil {
		print("first_error", *d.first)
	}
	for _, e := range d.critical {
		print("event", e)
	}
	for _, e := range d.events {
		if e.Cause != "nil" {
			print("event", e)
		}
	}
	keys := make([]string, 0, len(d.totals))
	for k := range d.totals {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		a := d.totals[k]
		t.Logf("CLAIM_DIAG aggregate kind=%s count=%d inclusive_total=%s max=%s", k, a.Count, a.Total, a.Max)
	}
	keys = keys[:0]
	for k := range d.lastGate {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		print("last_gate", d.lastGate[k])
	}
}
func (w *ContextWorld) DiagnosticStage(ctx context.Context, kind string, start time.Time, err error) {
	if w.diagnostics != nil {
		w.diagnostics.Stage(ctx, kind, start, err)
	}
}

type claimDiagnosticSave struct {
	Status   string
	Prepared bool
	Sequence int64
	Digest   string
	Cause    string
}
type claimDiagnosticTx struct{ Saves []claimDiagnosticSave }
type claimDiagnosticStore struct {
	engine.Store
	d      *ContextClaimDiagnostics
	mu     sync.Mutex
	active map[rt.Tx]*claimDiagnosticTx
}

func (s *claimDiagnosticStore) Within(ctx context.Context, owner contract.OwnerRef, fn func(context.Context, rt.Tx) error) error {
	start := time.Now()
	var token rt.Tx
	var callbackErr error
	var state *claimDiagnosticTx
	err := s.Store.Within(ctx, owner, func(actual context.Context, tx rt.Tx) error {
		token = tx
		state = &claimDiagnosticTx{}
		s.mu.Lock()
		s.active[token] = state
		s.mu.Unlock()
		callbackErr = fn(actual, tx)
		return callbackErr
	})
	s.mu.Lock()
	if token != nil {
		delete(s.active, token)
	}
	s.mu.Unlock()
	s.d.observe("store.Within", start, err, "")
	if state != nil && len(state.Saves) > 0 {
		// A nil final Within is the original PG adapter's positive Commit result.
		// A successful callback or Save alone is never called committed.
		for _, save := range state.Saves {
			s.d.observe("store.WithinSave", start, err, fmt.Sprintf("callback_cause=%s final_cause=%s committed=%t save_cause=%s status=%s prepared=%t sequence=%d input_digest=%s", claimCause(callbackErr), claimCause(err), err == nil && save.Cause == "nil", save.Cause, save.Status, save.Prepared, save.Sequence, save.Digest))
		}
	}
	return err
}
func (s *claimDiagnosticStore) SaveDecision(ctx context.Context, tx rt.Tx, r engine.Record) error {
	start := time.Now()
	err := s.Store.SaveDecision(ctx, tx, r)
	saved := claimDiagnosticSave{Status: r.Status, Prepared: r.Prepared != nil || r.PreparedV2 != nil, Sequence: r.StartSequence, Digest: r.InputDigest, Cause: claimCause(err)}
	s.mu.Lock()
	if state := s.active[tx]; state != nil {
		if len(state.Saves) < 4 {
			state.Saves = append(state.Saves, saved)
		}
	}
	s.mu.Unlock()
	s.d.observe("store.SaveDecision", start, err, fmt.Sprintf("status=%s prepared=%t sequence=%d input_digest=%s callback_only=true", saved.Status, saved.Prepared, saved.Sequence, saved.Digest))
	return err
}
func claimDetail(claim rt.Claim, now time.Time) string {
	return fmt.Sprintf("job=%s epoch=%d revision=%d phase=%s lease_until=%s supplied_now=%s lease_delta=%s", claim.JobID, claim.Epoch, claim.ClaimedRevision, claim.Phase, claim.LeaseUntil.UTC().Format(time.RFC3339Nano), now.UTC().Format(time.RFC3339Nano), claim.LeaseUntil.Sub(now))
}
func (s *claimDiagnosticStore) Claim(ctx context.Context, tx rt.Tx, j rt.Job, worker string, now, until time.Time) (*rt.Claim, error) {
	start := time.Now()
	claim, err := s.Store.Claim(ctx, tx, j, worker, now, until)
	detail := "claim=nil"
	if claim != nil {
		detail = claimDetail(*claim, now)
	}
	s.d.observe("store.Claim", start, err, detail)
	return claim, err
}
func (s *claimDiagnosticStore) ValidateClaim(ctx context.Context, tx rt.Tx, claim rt.Claim, now time.Time) error {
	start := time.Now()
	err := s.Store.ValidateClaim(ctx, tx, claim, now)
	s.d.observe("store.ValidateClaim", start, err, claimDetail(claim, now))
	return err
}
func (s *claimDiagnosticStore) ValidatePoolClaim(ctx context.Context, tx rt.Tx, state workpool.State, claim rt.Claim, now time.Time) error {
	start := time.Now()
	err := s.Store.ValidatePoolClaim(ctx, tx, state, claim, now)
	s.d.observe("store.ValidatePoolClaim", start, err, claimDetail(claim, now))
	return err
}
func (s *claimDiagnosticStore) Now(ctx context.Context, tx rt.Tx) (time.Time, error) {
	start := time.Now()
	now, err := s.Store.Now(ctx, tx)
	s.d.observe("store.Now", start, err, "")
	return now, err
}

type claimDiagnosticSource struct {
	engine.Source
	d *ContextClaimDiagnostics
}

func (s claimDiagnosticSource) ReadSnapshot(ctx context.Context, ref v.SnapshotRef, p engine.Permission, remaining int64) (engine.Snapshot, error) {
	start := time.Now()
	out, err := s.Source.ReadSnapshot(ctx, ref, p, remaining)
	s.d.observe("source.Snapshot", start, err, fmt.Sprintf("bytes=%d materials=%d", len(out.Raw), len(out.MaterialRefs)))
	return out, err
}
func (s claimDiagnosticSource) ReadFixtureLock(ctx context.Context, ref v.InstallLockRef, p engine.Permission, remaining int64) (engine.FixtureLock, error) {
	start := time.Now()
	out, err := s.Source.ReadFixtureLock(ctx, ref, p, remaining)
	s.d.observe("source.Lock", start, err, fmt.Sprintf("lock_bytes=%d manifest_bytes=%d", len(out.Raw), len(out.ManifestRaw)))
	return out, err
}
func (s claimDiagnosticSource) ReadMaterial(ctx context.Context, ref v.ContentRef, purpose string, p engine.Permission, remaining int64) ([]byte, error) {
	start := time.Now()
	out, err := s.Source.ReadMaterial(ctx, ref, purpose, p, remaining)
	s.d.observe("source.Material", start, err, fmt.Sprintf("bytes=%d", len(out)))
	return out, err
}

type claimDiagnosticPublisher struct {
	engine.Publisher
	d *ContextClaimDiagnostics
}

func (s claimDiagnosticPublisher) PlanPublication(ctx context.Context, key string, b []byte, refs []v.ContentRef, p engine.Permission) (v.ContentRef, error) {
	start := time.Now()
	out, err := s.Publisher.PlanPublication(ctx, key, b, refs, p)
	s.d.observe("publisher.Plan", start, err, fmt.Sprintf("bytes=%d sources=%d", len(b), len(refs)))
	return out, err
}
func (s claimDiagnosticPublisher) Publish(ctx context.Context, key string, b []byte, refs []v.ContentRef, p engine.Permission) (v.ContentRef, error) {
	start := time.Now()
	out, err := s.Publisher.Publish(ctx, key, b, refs, p)
	s.d.observe("publisher.Publish", start, err, fmt.Sprintf("bytes=%d sources=%d", len(b), len(refs)))
	return out, err
}
func (s claimDiagnosticPublisher) ReadPublished(ctx context.Context, ref v.ContentRef, p engine.Permission) ([]byte, error) {
	start := time.Now()
	out, err := s.Publisher.ReadPublished(ctx, ref, p)
	s.d.observe("publisher.ReadPublished", start, err, fmt.Sprintf("bytes=%d", len(out)))
	return out, err
}

type claimDiagnosticContent struct {
	adapter.Content
	d *ContextClaimDiagnostics
}

func (s claimDiagnosticContent) ReadForProcessing(ctx context.Context, subject *c.SubjectBinding, ref c.ContentRef, purpose string, remaining int64) (content.ProcessingRead, error) {
	start := time.Now()
	out, err := s.Content.ReadForProcessing(ctx, subject, ref, purpose, remaining)
	s.d.observe("content.ReadForProcessing", start, err, fmt.Sprintf("bytes=%d sources=%d", len(out.Bytes), len(out.Sources)))
	return out, err
}
func (s claimDiagnosticContent) Put(ctx context.Context, raw []byte, subject *c.SubjectBinding) (c.TransportOutcome, error) {
	start := time.Now()
	out, err := s.Content.Put(ctx, raw, subject)
	s.d.observe("content.Put", start, err, fmt.Sprintf("request_bytes=%d", len(raw)))
	return out, err
}
func (s claimDiagnosticContent) Step(ctx context.Context) (bool, error) {
	start := time.Now()
	out, err := s.Content.Step(ctx)
	s.d.observe("content.Step", start, err, fmt.Sprintf("processed=%t", out))
	return out, err
}
func (s claimDiagnosticContent) GetCommand(ctx context.Context, raw []byte, subject *c.SubjectBinding) (c.CommandGetResponse, error) {
	start := time.Now()
	out, err := s.Content.GetCommand(ctx, raw, subject)
	s.d.observe("content.GetCommand", start, err, "")
	return out, err
}

type claimDiagnosticAccess struct {
	adapter.Access
	d *ContextClaimDiagnostics
}

func (s claimDiagnosticAccess) Current(ctx context.Context, p engine.Permission, purpose string) (adapter.Binding, error) {
	start := time.Now()
	out, err := s.Access.Current(ctx, p, purpose)
	s.d.observe("access.Current", start, err, "")
	return out, err
}
func (s claimDiagnosticAccess) Binding(ctx context.Context, ref v.SnapshotRef, p engine.Permission, purpose string) (adapter.Binding, error) {
	start := time.Now()
	out, err := s.Access.Binding(ctx, ref, p, purpose)
	s.d.observe("access.Binding", start, err, "")
	return out, err
}
func (s claimDiagnosticAccess) StagePublication(ctx context.Context, in compiler.Input, request c.ContentPutRequest) ([]byte, error) {
	start := time.Now()
	out, err := s.Access.StagePublication(ctx, in, request)
	s.d.observe("access.StagePublication", start, err, "")
	return out, err
}
func (s claimDiagnosticAccess) PrepareContent(ctx context.Context, in compiler.Input, ref c.ContentRef) error {
	start := time.Now()
	err := s.Access.PrepareContent(ctx, in, ref)
	s.d.observe("access.PrepareContent", start, err, "")
	return err
}
