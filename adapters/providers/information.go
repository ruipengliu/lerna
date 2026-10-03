package providers

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"io"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/execution"
	"github.com/ruipengliu/lerna/runtime"
)

const informationNamespace = "providers.information_attempts"
const informationProtocol = "harness.information/1"

// HTTPInformation 的唯一物理出口是 Start。恢复只读取原 Attempt 和 Content。
type HTTPInformation struct {
	cfg          InformationConfig
	origin       *url.URL
	allowed      []netip.Prefix
	search, body *informationDriver
	parts        []string
	slots        chan struct{}
	mu           sync.Mutex
	closed       bool
	active       map[string]*informationActive
}
type informationActive struct {
	cancel context.CancelFunc
	done   chan struct{}
}
type informationDriver struct {
	source     *HTTPInformation
	action     string
	capability execution.Capability
}
type informationWire struct {
	SourceRef api.ComponentRef `json:"source_ref"`
	Action    string           `json:"action"`
	Method    string           `json:"method"`
	URL       string           `json:"url"`
	Body      []byte           `json:"body"`
	Limit     uint64           `json:"limit"`
}
type informationRecord struct {
	Revision     uint64                 `json:"revision"`
	InputDigest  string                 `json:"input_digest"`
	PrincipalRef api.ObjectRef          `json:"principal_ref"`
	ArgumentsRef api.ContentRef         `json:"arguments_ref"`
	OperationID  string                 `json:"operation_id"`
	SearchLimit  uint64                 `json:"search_limit"`
	Phase        string                 `json:"phase"`
	Outlet       HTTPOutRequest         `json:"outlet"`
	Observation  InformationObservation `json:"observation"`
	Publication  *ReceivedPublication   `json:"publication,omitempty"`
}

func NewHTTPInformation(c InformationConfig) (*HTTPInformation, error) {
	if c.Store == nil || c.Content == nil || c.Egress == nil || c.Scope.DatabaseID != c.Store.ID() || !api.ValidID(c.Scope.TenantID) || !api.ValidID(c.Scope.OwnerID) {
		return nil, api.E("invalid_request", "information_ports_or_scope_invalid")
	}
	s := c.Source
	if !api.ValidID(s.SourceRef.ComponentID) || s.SourceRef.Version == "" || !s.PublicUnbilled || s.Receiver == "" || len(s.Receiver) > 256 || s.Location == "" || len(s.Location) > 64 {
		return nil, api.E("invalid_request", "information_source_contract_invalid")
	}
	if s.MaxQueryBytes < 1 || s.MaxQueryBytes > 4096 || s.MaxItems < 1 || s.MaxItems > 20 || s.MaxResponseBytes < 1 || s.MaxResponseBytes > 1<<20 || s.TimeoutMillis < 1 || s.TimeoutMillis > 30000 || s.MaxConcurrent < 1 || s.MaxConcurrent > 32 {
		return nil, api.E("invalid_request", "information_limits_invalid")
	}
	u, err := url.Parse(s.Origin)
	if err != nil || u.User != nil || u.Host == "" || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") || len(s.Origin) > 4096 {
		return nil, api.E("invalid_request", "information_origin_invalid")
	}
	u.Path = ""
	if u.Scheme != "https" {
		ip := net.ParseIP(u.Hostname())
		if !c.AllowHTTPForLoopback || u.Scheme != "http" || ip == nil || !ip.IsLoopback() {
			return nil, api.E("forbidden", "information_origin_requires_https")
		}
	}
	if len(s.FetchPathPrefixes) > 16 || len(s.AllowedCIDRs) < 1 || len(s.AllowedCIDRs) > 16 || (s.SearchPath == "" && len(s.FetchPathPrefixes) == 0) {
		return nil, api.E("invalid_request", "information_source_routes_invalid")
	}
	for _, path := range append(append([]string{}, s.FetchPathPrefixes...), s.SearchPath) {
		if path != "" && (len(path) > 1024 || !strings.HasPrefix(path, "/") || strings.ContainsAny(path, "?#\\") || strings.Contains(path, "%") || strings.Contains(path, "..")) {
			return nil, api.E("invalid_request", "information_source_routes_invalid")
		}
	}
	allowed := []netip.Prefix{}
	for _, cidr := range s.AllowedCIDRs {
		p, err := netip.ParsePrefix(cidr)
		if err != nil {
			return nil, api.E("invalid_request", "information_cidr_invalid")
		}
		allowed = append(allowed, p.Masked())
	}
	if len(c.Credential) > 8192 || strings.ContainsAny(c.Credential, "\r\n") || len(s.CredentialID) > 256 || (s.CredentialRequired && s.CredentialID == "") {
		return nil, api.E("invalid_request", "information_credential_invalid")
	}
	if len(c.TLSRootPEM) > 64<<10 {
		return nil, api.E("invalid_request", "information_tls_roots_too_large")
	}
	if len(c.TLSRootPEM) > 0 {
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(c.TLSRootPEM) {
			return nil, api.E("invalid_request", "information_tls_roots_invalid")
		}
	}
	originalDigest := s.SourceRef.Digest
	s.SourceRef.Digest = ""
	digest, err := api.Digest(struct {
		Descriptor                   InformationSourceDescriptor
		CredentialDigest, RootDigest string
		Loopback                     bool
	}{s, api.Hash([]byte(c.Credential)), api.Hash(c.TLSRootPEM), c.AllowHTTPForLoopback})
	if err != nil {
		return nil, err
	}
	if originalDigest != "" && originalDigest != digest {
		return nil, api.E("revision_conflict", "information_source_digest_changed")
	}
	s.SourceRef.Digest = digest
	if err = api.ValidateRecord("ComponentRef", s.SourceRef); err != nil {
		return nil, err
	}
	s.FetchPathPrefixes = append([]string{}, s.FetchPathPrefixes...)
	s.AllowedCIDRs = append([]string{}, s.AllowedCIDRs...)
	c.Source = s
	c.TLSRootPEM = append([]byte{}, c.TLSRootPEM...)
	parts := []string{"providers"}
	for _, p := range c.Participants {
		if p != "providers" {
			parts = append(parts, p)
		}
	}
	sort.Strings(parts)
	h := &HTTPInformation{cfg: c, origin: u, allowed: allowed, parts: parts, slots: make(chan struct{}, s.MaxConcurrent), active: map[string]*informationActive{}}
	if s.SearchPath != "" {
		h.search = h.driver(InformationSearch, api.SchemaFor[SearchArguments]())
	}
	if len(s.FetchPathPrefixes) > 0 {
		h.body = h.driver(InformationBody, api.SchemaFor[BodyArguments]())
	}
	return h, nil
}
func (h *HTTPInformation) driver(action string, input api.Schema) *informationDriver {
	digest, _ := api.Digest(struct {
		Source        api.ComponentRef
		Action        string
		Input, Output api.Schema
	}{h.cfg.Source.SourceRef, action, input, api.SchemaFor[InformationObservation]()})
	ref := api.ComponentRef{ComponentID: "capability_" + strings.TrimPrefix(digest, "sha256:")[:32], Version: "1", Digest: digest}
	return &informationDriver{source: h, action: action, capability: execution.Capability{Ref: ref, EffectClass: "read_only", MaxAttempts: 1, InputSchema: input, OutputSchema: api.SchemaFor[InformationObservation]()}}
}
func (h *HTTPInformation) Descriptor() InformationSourceDescriptor {
	s := h.cfg.Source
	s.AllowedCIDRs = append([]string{}, s.AllowedCIDRs...)
	s.FetchPathPrefixes = append([]string{}, s.FetchPathPrefixes...)
	return s
}
func (h *HTTPInformation) SearchDriver() execution.Driver {
	if h.search == nil {
		return nil
	}
	return h.search
}
func (h *HTTPInformation) BodyDriver() execution.Driver {
	if h.body == nil {
		return nil
	}
	return h.body
}
func (h *HTTPInformation) Drivers() []execution.Driver {
	out := []execution.Driver{}
	if h.search != nil {
		out = append(out, h.search)
	}
	if h.body != nil {
		out = append(out, h.body)
	}
	return out
}
func (d *informationDriver) Capability() execution.Capability {
	c := d.capability
	_ = api.Decode(api.Raw(c.InputSchema), &c.InputSchema)
	_ = api.Decode(api.Raw(c.OutputSchema), &c.OutputSchema)
	return c
}
func (h *HTTPInformation) Close() error {
	h.mu.Lock()
	h.closed = true
	active := make([]*informationActive, 0, len(h.active))
	for _, a := range h.active {
		a.cancel()
		active = append(active, a)
	}
	h.mu.Unlock()
	for _, a := range active {
		<-a.done
	}
	return nil
}
func (h *HTTPInformation) within(ctx context.Context, fn func(runtime.Tx) error) error {
	status, err := h.cfg.Store.Within(ctx, h.cfg.Scope, h.parts, fn)
	if status == runtime.CommitUnknown {
		return runtime.ErrCommitUnknown
	}
	return err
}
func (h *HTTPInformation) currentTime(ctx context.Context) (time.Time, error) {
	var now time.Time
	err := h.within(ctx, func(tx runtime.Tx) error { var err error; now, err = tx.Now(ctx); return err })
	return now, err
}
func (h *HTTPInformation) scopeAuth(scope runtime.Scope, a runtime.Auth) error {
	if scope != h.cfg.Scope || a.TenantID != scope.TenantID || !api.ValidID(a.SubjectID) || a.CredentialGeneration == 0 {
		return api.E("forbidden", "information_scope_mismatch")
	}
	return api.ValidateRecord("ObjectRef", a.Ref(scope.OwnerID))
}
func containsRef(refs []api.ContentRef, ref api.ContentRef) bool {
	for _, r := range refs {
		if r == ref {
			return true
		}
	}
	return false
}
func (h *HTTPInformation) refs(intent execution.ExecutionIntent) error {
	if len(intent.ProcessedSourceRefs) > 100 || len(intent.DisclosedSourceRefs) > 100 {
		return api.E("invalid_request", "information_sources_too_many")
	}
	if len(api.Raw(struct{ Processed, Disclosed []api.ContentRef }{intent.ProcessedSourceRefs, intent.DisclosedSourceRefs})) > 32<<10 {
		return api.E("invalid_request", "information_source_metadata_bytes_limit")
	}
	for _, refs := range [][]api.ContentRef{intent.ProcessedSourceRefs, intent.DisclosedSourceRefs} {
		for _, ref := range refs {
			if ref.TenantID != h.cfg.Scope.TenantID || ref.OwnerID != h.cfg.Scope.OwnerID {
				return api.E("forbidden", "information_source_owner_unavailable")
			}
			if err := api.ValidateRecord("ContentRef", ref); err != nil {
				return err
			}
		}
	}
	for _, ref := range intent.DisclosedSourceRefs {
		if !containsRef(intent.ProcessedSourceRefs, ref) {
			return api.E("forbidden", "information_disclosed_source_undeclared")
		}
	}
	if !containsRef(intent.ProcessedSourceRefs, intent.ArgumentsRef) || !containsRef(intent.DisclosedSourceRefs, intent.ArgumentsRef) {
		return api.E("forbidden", "information_arguments_disclosure_undeclared")
	}
	return nil
}
func (h *HTTPInformation) bodyURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || !utf8.ValidString(raw) || len(raw) > 4096 || u.User != nil || u.Fragment != "" || u.RawQuery != "" || u.Scheme != h.origin.Scheme || u.Host != h.origin.Host || strings.ContainsAny(u.Path, "\\\x00") || u.RawPath != "" {
		return nil, api.E("forbidden", "information_url_outside_source")
	}
	for _, part := range strings.Split(u.Path, "/") {
		if part == ".." || part == "." {
			return nil, api.E("forbidden", "information_url_outside_source")
		}
	}
	for _, prefix := range h.cfg.Source.FetchPathPrefixes {
		if strings.HasPrefix(u.Path, prefix) {
			return u, nil
		}
	}
	return nil, api.E("forbidden", "information_url_outside_source")
}
func (d *informationDriver) Prepare(ctx context.Context, s runtime.Scope, a runtime.Auth, invoke execution.InvokeInput, intent execution.ExecutionIntent, args []byte) (execution.PreparedRequest, error) {
	h := d.source
	if err := h.scopeAuth(s, a); err != nil {
		return execution.PreparedRequest{}, err
	}
	if h.cfg.Source.CredentialRequired && h.cfg.Credential == "" {
		return execution.PreparedRequest{}, api.E("unsupported", "information_credential_unavailable")
	}
	if invoke.CapabilityRef != d.capability.Ref || intent.CapabilityRef != d.capability.Ref || invoke.OperationID != intent.OperationID || invoke.TaskRef != intent.TaskRef || intent.ArgumentsRef.Hash != api.Hash(args) || intent.ArgumentsRef.ByteLength != uint64(len(args)) || len(args) > 16384 {
		return execution.PreparedRequest{}, api.E("invalid_request", "information_intent_changed")
	}
	if err := h.refs(intent); err != nil {
		return execution.PreparedRequest{}, err
	}
	wire := informationWire{SourceRef: h.cfg.Source.SourceRef, Action: d.action, Body: []byte{}}
	if d.action == InformationBody {
		var in BodyArguments
		if err := api.Decode(args, &in); err != nil {
			return execution.PreparedRequest{}, err
		}
		u, err := h.bodyURL(in.URL)
		if err != nil {
			return execution.PreparedRequest{}, err
		}
		wire.URL = u.String()
		wire.Method = "GET"
	} else {
		var in SearchArguments
		if err := api.Decode(args, &in); err != nil {
			return execution.PreparedRequest{}, err
		}
		if in.Limit < 1 || in.Limit > h.cfg.Source.MaxItems || in.QueryRef.ByteLength > h.cfg.Source.MaxQueryBytes || (in.Cursor != nil && (len(*in.Cursor) > 1024 || !utf8.ValidString(*in.Cursor))) {
			return execution.PreparedRequest{}, api.E("invalid_request", "information_search_limits")
		}
		if !containsRef(intent.ProcessedSourceRefs, in.QueryRef) || !containsRef(intent.DisclosedSourceRefs, in.QueryRef) {
			return execution.PreparedRequest{}, api.E("forbidden", "information_query_disclosure_undeclared")
		}
		query, err := h.cfg.Content.ReadBytes(ctx, s, a, in.QueryRef, InformationSearch, h.cfg.Source.Location)
		if err != nil {
			return execution.PreparedRequest{}, err
		}
		if uint64(len(query)) != in.QueryRef.ByteLength || api.Hash(query) != in.QueryRef.Hash || !utf8.Valid(query) || len(query) == 0 {
			return execution.PreparedRequest{}, api.E("invalid_request", "information_query_bytes_invalid")
		}
		wire.Method = "POST"
		u := *h.origin
		u.Path = h.cfg.Source.SearchPath
		wire.URL = u.String()
		wire.Limit = in.Limit
		wire.Body = api.Raw(SearchRequest{Protocol: informationProtocol, RequestID: intent.OperationID, Query: string(query), Limit: in.Limit, Cursor: in.Cursor})
	}
	encoded := api.Raw(wire)
	digest, err := api.Digest(encoded)
	return execution.PreparedRequest{Encoded: encoded, Digest: digest, TargetRequestKey: intent.OperationID}, err
}
func (d *informationDriver) request(r execution.AttemptRequest) (informationWire, string, error) {
	h := d.source
	if err := h.scopeAuth(r.Scope, r.Auth); err != nil {
		return informationWire{}, "", err
	}
	if !api.ValidID(r.Attempt.AttemptID) || r.Attempt.OperationID != r.Intent.OperationID || r.Attempt.AttemptNo != 1 || r.Intent.CapabilityRef != d.capability.Ref || r.Invoke.CapabilityRef != d.capability.Ref {
		return informationWire{}, "", api.E("invalid_request", "information_attempt_binding_changed")
	}
	if err := h.refs(r.Intent); err != nil {
		return informationWire{}, "", err
	}
	var wire informationWire
	if err := api.Decode(r.Attempt.Prepared.Encoded, &wire); err != nil {
		return wire, "", err
	}
	digest, err := api.Digest(r.Attempt.Prepared.Encoded)
	if err != nil {
		return wire, "", err
	}
	if digest != r.Attempt.Prepared.Digest || wire.SourceRef != h.cfg.Source.SourceRef || wire.Action != d.action || r.Attempt.Prepared.TargetRequestKey != r.Intent.OperationID {
		return wire, "", api.E("idempotency_conflict", "information_frozen_request_changed")
	}
	if d.action == InformationBody {
		if _, err = h.bodyURL(wire.URL); err != nil {
			return wire, "", err
		}
		if wire.Method != "GET" || len(wire.Body) != 0 {
			return wire, "", api.E("invalid_request", "information_wire_changed")
		}
	} else {
		u := *h.origin
		u.Path = h.cfg.Source.SearchPath
		var q SearchRequest
		if err = api.Decode(wire.Body, &q); err != nil {
			return wire, "", err
		}
		if wire.Method != "POST" || wire.URL != u.String() || q.Protocol != informationProtocol || q.RequestID != r.Intent.OperationID || q.Limit != wire.Limit || q.Limit < 1 || q.Limit > h.cfg.Source.MaxItems || len(q.Query) > int(h.cfg.Source.MaxQueryBytes) {
			return wire, "", api.E("invalid_request", "information_wire_changed")
		}
	}
	input, err := api.Digest(struct {
		Invoke   execution.InvokeInput
		Intent   execution.ExecutionIntent
		Prepared execution.PreparedRequest
		Auth     runtime.Auth
	}{r.Invoke, r.Intent, r.Attempt.Prepared, r.Auth})
	return wire, input, err
}
func (h *HTTPInformation) resolve(ctx context.Context) ([]string, error) {
	addresses, err := net.DefaultResolver.LookupNetIP(ctx, "ip", h.origin.Hostname())
	if err != nil {
		return nil, err
	}
	if len(addresses) == 0 || len(addresses) > 16 {
		return nil, api.E("forbidden", "information_dns_addresses_invalid")
	}
	out := []string{}
	seen := map[string]bool{}
	originIP, originLiteral := netip.ParseAddr(h.origin.Hostname())
	for _, address := range addresses {
		ip := address.Unmap()
		localAllowed := h.cfg.AllowHTTPForLoopback && originLiteral == nil && originIP.IsLoopback() && ip.IsLoopback()
		if (!ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLoopback()) && !localAllowed {
			return nil, api.E("forbidden", "information_network_address_denied")
		}
		allowed := false
		for _, cidr := range h.allowed {
			if cidr.Contains(ip) {
				allowed = true
				break
			}
		}
		if !allowed {
			return nil, api.E("forbidden", "information_network_address_denied")
		}
		if !seen[ip.String()] {
			out = append(out, ip.String())
			seen[ip.String()] = true
		}
	}
	sort.Strings(out)
	return out, nil
}
func (d *informationDriver) Start(ctx context.Context, r execution.AttemptRequest, barrier func(context.Context) error) (execution.Fact, error) {
	h := d.source
	wire, input, err := d.request(r)
	if err != nil {
		return execution.Fact{}, err
	}
	if barrier == nil {
		return execution.Fact{}, api.E("invalid_request", "information_start_barrier_required")
	}
	select {
	case h.slots <- struct{}{}:
	default:
		return execution.Fact{}, api.E("overloaded", "information_concurrency_limit")
	}
	defer func() { <-h.slots }()
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return execution.Fact{}, api.E("dependency_unavailable", "information_driver_closed")
	}
	h.mu.Unlock()
	var record informationRecord
	created := false
	err = h.within(ctx, func(tx runtime.Tx) error {
		_, err := tx.Get(ctx, informationNamespace, r.Attempt.AttemptID, &record)
		if err == nil {
			if record.InputDigest != input {
				return api.E("idempotency_conflict", "information_attempt_changed")
			}
			return nil
		}
		if !api.IsCode(err, "not_found") {
			return err
		}
		record = informationRecord{Revision: 1, InputDigest: input, PrincipalRef: r.Auth.Ref(r.Scope.OwnerID), ArgumentsRef: r.Intent.ArgumentsRef, OperationID: r.Intent.OperationID, SearchLimit: wire.Limit, Phase: "reserved"}
		created = true
		if err = tx.Create(ctx, informationNamespace, r.Attempt.AttemptID, r.Intent.OperationID, record); err != nil {
			return err
		}
		return tx.Bind(ctx, informationNamespace, r.Intent.OperationID, r.Attempt.AttemptID, input)
	})
	if err != nil {
		return execution.Fact{}, err
	}
	if !created {
		return d.Reconcile(ctx, r)
	}
	callCtx, cancel := context.WithTimeout(ctx, time.Duration(h.cfg.Source.TimeoutMillis)*time.Millisecond)
	active := &informationActive{cancel: cancel, done: make(chan struct{})}
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		cancel()
		return execution.Fact{}, api.E("dependency_unavailable", "information_driver_closed")
	}
	h.active[r.Attempt.AttemptID] = active
	h.mu.Unlock()
	defer func() {
		cancel()
		h.mu.Lock()
		delete(h.active, r.Attempt.AttemptID)
		close(active.done)
		h.mu.Unlock()
	}()
	ips, err := h.resolve(callCtx)
	if err != nil {
		return execution.Fact{}, err
	}
	outlet := HTTPOutRequest{SourceRef: wire.SourceRef, Action: d.action, URL: wire.URL, Method: wire.Method, Receiver: h.cfg.Source.Receiver, Location: h.cfg.Source.Location, ActualIPs: ips, ProcessedSources: append([]api.ContentRef{}, r.Intent.ProcessedSourceRefs...), DisclosedSources: append([]api.ContentRef{}, r.Intent.DisclosedSourceRefs...)}
	outlet.RequestDigest, err = api.Digest(struct {
		Outlet    HTTPOutRequest
		Wire      informationWire
		AttemptID string
		Auth      runtime.Auth
	}{outlet, wire, r.Attempt.AttemptID, r.Auth})
	if err != nil {
		return execution.Fact{}, err
	}
	permit, err := h.cfg.Egress.Check(callCtx, r, outlet)
	if err != nil {
		return execution.Fact{}, err
	}
	if permit.RequestDigest != outlet.RequestDigest || permit.PolicyRevision == 0 {
		return execution.Fact{}, api.E("forbidden", "information_egress_permit_changed")
	}
	startBefore, err := api.ParseTime(permit.StartBefore)
	if err != nil {
		return execution.Fact{}, err
	}
	for _, deadline := range []string{r.Intent.Deadline, r.Intent.TaskDeadline, r.Invoke.Deadline} {
		t, e := api.ParseTime(deadline)
		if e != nil {
			return execution.Fact{}, e
		}
		if t.Before(startBefore) {
			startBefore = t
		}
	}
	now, err := h.currentTime(callCtx)
	if err != nil {
		return execution.Fact{}, err
	}
	if !now.Before(startBefore) {
		return execution.Fact{}, api.E("expired", "information_original_start_expired")
	}
	if err = barrier(callCtx); err != nil {
		return execution.Fact{}, err
	}
	err = h.within(callCtx, func(tx runtime.Tx) error {
		var current informationRecord
		rev, err := tx.Get(callCtx, informationNamespace, r.Attempt.AttemptID, &current)
		if err != nil {
			return err
		}
		if current.InputDigest != input || current.Phase != "reserved" {
			return api.E("idempotency_conflict", "information_send_marker_changed")
		}
		now, err := tx.Now(callCtx)
		if err != nil {
			return err
		}
		if !now.Before(startBefore) {
			return api.E("expired", "information_original_start_expired")
		}
		current.Phase = "send_started"
		current.Revision = rev + 1
		current.Outlet = outlet
		current.Observation = InformationObservation{SourceRef: wire.SourceRef, AttemptID: r.Attempt.AttemptID, Action: d.action, URL: wire.URL, MediaType: "application/octet-stream", Cache: "unspecified", Items: []SearchItem{}, Coverage: "unknown", Gaps: []string{}}
		record = current
		return tx.Put(callCtx, informationNamespace, r.Attempt.AttemptID, rev, current)
	})
	if err != nil {
		return execution.Fact{}, err
	}
	deadlineCtx, deadlineCancel := context.WithTimeout(callCtx, startBefore.Sub(now))
	defer deadlineCancel()
	response, body, readErr := h.receive(deadlineCtx, r.Attempt.AttemptID, wire, ips)
	if response == nil {
		return unknownInformationFact(), nil
	}
	obtained, err := h.currentTime(callCtx)
	if err != nil {
		return execution.Fact{}, err
	}
	observation := record.Observation
	observation.ObtainedAt = api.Time(obtained)
	observation.HTTPStatus = uint64(response.StatusCode)
	media, _, mediaErr := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if mediaErr != nil || len(media) > 128 {
		media = "application/octet-stream"
		observation.Gaps = append(observation.Gaps, "media_type_unspecified")
	}
	observation.MediaType = media
	if value := response.Header.Get("Last-Modified"); len(value) <= 128 {
		if modified, e := http.ParseTime(value); e == nil {
			observation.ObservedAt = api.Time(modified)
		}
	}
	if response.Header.Get("Age") != "" || response.Header.Get("X-Cache") != "" {
		observation.Cache = "source_reports_cache"
	}
	if readErr != nil {
		observation.PartialRead = true
		observation.Gaps = append(observation.Gaps, "response_read_failed")
	}
	if uint64(len(body)) > h.cfg.Source.MaxResponseBytes {
		observation.Truncated = true
		observation.Gaps = append(observation.Gaps, "response_bytes_limit")
		body = body[:h.cfg.Source.MaxResponseBytes]
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		observation.Gaps = append(observation.Gaps, "http_status_failure")
	}
	if response.StatusCode >= 300 && response.StatusCode < 400 {
		observation.Gaps = append(observation.Gaps, "redirect_not_followed")
	}
	if d.action == InformationBody {
		observation.Coverage = "single_resource"
		observation.Exhausted = !observation.Truncated && !observation.PartialRead
	} else if !observation.Truncated && !observation.PartialRead && response.StatusCode >= 200 && response.StatusCode < 300 {
		search, e := h.searchResponse(body, wire.Limit, r.Intent.OperationID)
		if e != nil {
			observation.Gaps = append(observation.Gaps, "search_response_invalid")
		} else {
			observation.Items = search.Items
			observation.Cursor = search.Cursor
			observation.Exhausted = search.Exhausted
			observation.Coverage = search.Coverage
		}
	}
	ref := api.ContentRef{TenantID: r.Scope.TenantID, OwnerID: r.Scope.OwnerID, ContentID: api.NewID("content"), Version: 1, Hash: api.Hash(body), MediaType: media, ByteLength: uint64(len(body))}
	publication := ReceivedPublication{ContentRef: ref, ObtainedAt: observation.ObtainedAt, ProcessedSources: append([]api.ContentRef{}, r.Intent.ProcessedSourceRefs...), DisclosedSources: append([]api.ContentRef{}, r.Intent.DisclosedSourceRefs...)}
	if len(api.Raw(observation)) > 128<<10 {
		observation.Items = []SearchItem{}
		observation.Cursor = nil
		observation.Exhausted = false
		observation.Gaps = append(observation.Gaps, "search_metadata_bytes_limit")
	}
	err = h.update(callCtx, r.Attempt.AttemptID, input, func(record *informationRecord) error {
		record.Phase = "received"
		record.Observation = observation
		// 命中正文留在 Content；SQL 原调用账只保存来源元数据与引用。
		record.Observation.Items = []SearchItem{}
		record.Publication = &publication
		return nil
	})
	if err != nil {
		return execution.Fact{}, err
	}
	ref, err = h.cfg.Content.PublishReceived(callCtx, r.Scope, r.Auth, publication, body)
	if err != nil {
		return execution.Fact{}, err
	}
	if ref != publication.ContentRef {
		return execution.Fact{}, api.E("idempotency_conflict", "information_publication_changed")
	}
	err = h.update(callCtx, r.Attempt.AttemptID, input, func(record *informationRecord) error {
		record.Phase = "published"
		record.Observation.BodyRef = &ref
		return nil
	})
	if err != nil {
		return execution.Fact{}, err
	}
	return d.Reconcile(ctx, r)
}
func (h *HTTPInformation) update(ctx context.Context, id, input string, apply func(*informationRecord) error) error {
	return h.within(ctx, func(tx runtime.Tx) error {
		var record informationRecord
		rev, err := tx.Get(ctx, informationNamespace, id, &record)
		if err != nil {
			return err
		}
		if record.InputDigest != input {
			return api.E("idempotency_conflict", "information_attempt_changed")
		}
		if err = apply(&record); err != nil {
			return err
		}
		record.Revision = rev + 1
		return tx.Put(ctx, informationNamespace, id, rev, record)
	})
}
func (h *HTTPInformation) receive(ctx context.Context, id string, wire informationWire, ips []string) (*http.Response, []byte, error) {
	u, _ := url.Parse(wire.URL)
	port := u.Port()
	if port == "" {
		if u.Scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	transport := &http.Transport{Proxy: nil, DisableKeepAlives: true, DisableCompression: true, ForceAttemptHTTP2: false, MaxResponseHeaderBytes: 16384, TLSNextProto: map[string]func(string, *tls.Conn) http.RoundTripper{}, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, ServerName: u.Hostname()}, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		if address != net.JoinHostPort(u.Hostname(), port) {
			return nil, api.E("forbidden", "information_dial_target_changed")
		}
		return (&net.Dialer{}).DialContext(ctx, "tcp", net.JoinHostPort(ips[0], port))
	}}
	if len(h.cfg.TLSRootPEM) > 0 {
		pool := x509.NewCertPool()
		pool.AppendCertsFromPEM(h.cfg.TLSRootPEM)
		transport.TLSClientConfig.RootCAs = pool
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	request, err := http.NewRequestWithContext(ctx, wire.Method, wire.URL, bytes.NewReader(wire.Body))
	if err != nil {
		return nil, nil, err
	}
	request.GetBody = nil
	request.Header.Set("Accept-Encoding", "identity")
	request.Header.Set("X-Harness-Attempt-ID", id)
	if wire.Method == "POST" {
		request.Header.Set("Content-Type", "application/json")
	}
	if h.cfg.Source.CredentialRequired {
		request.Header.Set("Authorization", "Bearer "+h.cfg.Credential)
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, nil, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, int64(h.cfg.Source.MaxResponseBytes)+1))
	return response, body, err
}
func (h *HTTPInformation) searchResponse(body []byte, limit uint64, id string) (SearchResponse, error) {
	var out SearchResponse
	if len(body) > api.MaxJSONBytes {
		return out, api.E("invalid_request", "search_response_metadata_limit")
	}
	if err := api.Decode(body, &out); err != nil {
		return out, err
	}
	if out.Protocol != informationProtocol || out.RequestID != id || out.Items == nil || uint64(len(out.Items)) > limit || len(out.Coverage) > 128 || out.Coverage == "" || (out.Cursor != nil && (len(*out.Cursor) > 1024 || out.Exhausted)) {
		return out, api.E("invalid_request", "search_response_invalid")
	}
	for _, item := range out.Items {
		u, err := url.Parse(item.URL)
		if err != nil || len(item.URL) > 4096 || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || u.Fragment != "" || u.Host == "" || len(item.Title) > 1024 || len(item.Snippet) > 2048 || !utf8.ValidString(item.Title) || !utf8.ValidString(item.Snippet) {
			return out, api.E("invalid_request", "search_item_invalid")
		}
		if item.ObservedAt != "" {
			if _, err = api.ParseTime(item.ObservedAt); err != nil {
				return out, err
			}
		}
	}
	return out, nil
}
func unknownInformationFact() execution.Fact {
	return execution.Fact{Revision: 1, Effect: "unknown", MayApplyLater: "unknown", Evidence: []api.ContentRef{}, Usage: []api.Amount{{Unit: "USD", Value: "0"}}, UsageFinal: true}
}
func informationFact(o InformationObservation, revision uint64) execution.Fact {
	evidence := []api.ContentRef{}
	if o.BodyRef != nil {
		evidence = append(evidence, *o.BodyRef)
	}
	return execution.Fact{Revision: revision, Effect: "applied", MayApplyLater: false, Output: api.Raw(o), MediaType: "application/json", Evidence: evidence, Usage: []api.Amount{{Unit: "USD", Value: "0"}}, UsageFinal: true}
}
func (d *informationDriver) Reconcile(ctx context.Context, r execution.AttemptRequest) (execution.Fact, error) {
	h := d.source
	_, input, err := d.request(r)
	if err != nil {
		return execution.Fact{}, err
	}
	var record informationRecord
	_, err = h.cfg.Store.Read(ctx, r.Scope, informationNamespace, r.Attempt.AttemptID, 0, &record)
	if err != nil {
		return execution.Fact{}, err
	}
	if record.InputDigest != input {
		return execution.Fact{}, api.E("idempotency_conflict", "information_attempt_changed")
	}
	if record.Phase == "reserved" || record.Phase == "send_started" {
		return unknownInformationFact(), nil
	}
	if record.Publication == nil {
		return execution.Fact{}, api.E("invalid_state", "information_original_publication_missing")
	}
	if record.Phase == "received" {
		ref, e := h.cfg.Content.RecoverReceived(ctx, r.Scope, r.Auth, *record.Publication)
		if e != nil {
			if api.IsCode(e, "not_found") || api.IsCode(e, "dependency_unavailable") {
				o := record.Observation
				o.Items = []SearchItem{}
				o.Cursor = nil
				o.Exhausted = false
				o.Gaps = append(o.Gaps, "original_bytes_unavailable")
				return informationFact(o, record.Revision), nil
			}
			return execution.Fact{}, e
		}
		if ref != record.Publication.ContentRef {
			return execution.Fact{}, api.E("idempotency_conflict", "information_original_content_changed")
		}
		err = h.update(ctx, r.Attempt.AttemptID, input, func(record *informationRecord) error {
			record.Phase = "published"
			record.Observation.BodyRef = &ref
			return nil
		})
		if err != nil {
			return execution.Fact{}, err
		}
		record.Phase = "published"
		record.Revision++
		record.Observation.BodyRef = &ref
	}
	if record.Phase != "published" || record.Observation.BodyRef == nil {
		return execution.Fact{}, api.E("invalid_state", "information_original_phase_invalid")
	}
	b, err := h.cfg.Content.ReadBytes(ctx, r.Scope, r.Auth, *record.Observation.BodyRef, InformationPurpose, h.cfg.Source.Location)
	if err != nil {
		return execution.Fact{}, err
	}
	if uint64(len(b)) != record.Observation.BodyRef.ByteLength || api.Hash(b) != record.Observation.BodyRef.Hash {
		return execution.Fact{}, api.E("invalid_request", "information_original_bytes_changed")
	}
	if err = h.expandSearch(&record, b); err != nil {
		return execution.Fact{}, err
	}
	return informationFact(record.Observation, record.Revision), nil
}

func (h *HTTPInformation) expandSearch(record *informationRecord, b []byte) error {
	o := &record.Observation
	if o.Action != InformationSearch || o.PartialRead || o.Truncated || o.HTTPStatus < 200 || o.HTTPStatus >= 300 || len(o.Gaps) > 0 {
		return nil
	}
	search, err := h.searchResponse(b, record.SearchLimit, record.OperationID)
	if err != nil {
		return err
	}
	// 元数据界限是原取得时的事实，恢复不得扩大已宣布的范围。
	o.Items = search.Items
	if len(api.Raw(*o)) > 128<<10 {
		return api.E("invalid_request", "information_original_metadata_limit_changed")
	}
	return nil
}
func (d *informationDriver) Stop(ctx context.Context, r execution.AttemptRequest) (execution.StopFact, error) {
	if _, _, err := d.request(r); err != nil {
		return execution.StopFact{}, err
	}
	h := d.source
	h.mu.Lock()
	active := h.active[r.Attempt.AttemptID]
	if active != nil {
		active.cancel()
	}
	h.mu.Unlock()
	if active != nil {
		select {
		case <-active.done:
		case <-ctx.Done():
			return execution.StopFact{}, ctx.Err()
		}
	}
	var record informationRecord
	_, err := h.cfg.Store.Read(ctx, r.Scope, informationNamespace, r.Attempt.AttemptID, 0, &record)
	if err != nil {
		return execution.StopFact{}, err
	}
	if record.Phase == "published" || record.Phase == "received" {
		return execution.StopFact{ActuallyStopped: true, MayApplyLater: false}, nil
	}
	// 没有本机实际退出证据时，原远端请求可能仍在处理。
	return execution.StopFact{ActuallyStopped: active != nil, MayApplyLater: "unknown"}, nil
}

var _ execution.Driver = (*informationDriver)(nil)
