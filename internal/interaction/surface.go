package interaction

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"regexp"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/runtime"
)

const JobApplicationEvent = "interaction.application_event"
const applicationEvents = "interaction.application_events"

type SurfaceInput struct {
	BindingRef  api.ObjectRef   `json:"binding_ref"`
	SnapshotRef api.ContentRef  `json:"snapshot_ref"`
	RequestRefs []api.ObjectRef `json:"request_refs"`
	TaskRef     *api.ObjectRef  `json:"task_ref,omitempty"`
}
type Surface struct {
	SurfaceInput
	SurfaceID string `json:"surface_id"`
	TenantID  string `json:"tenant_id"`
	OwnerID   string `json:"owner_id"`
	Revision  uint64 `json:"revision"`
	State     string `json:"state"`
}
type surfaceRecord struct {
	Surface
	SubjectID string `json:"subject_id"`
}
type SurfaceControlInput struct {
	Reason string `json:"reason"`
}
type OpenPresentationInput struct {
	EndpointID string        `json:"endpoint_id"`
	InstanceID string        `json:"instance_id"`
	SurfaceRef api.ObjectRef `json:"surface_ref"`
}
type BeginPresentationInput struct {
	IntentRevision uint64 `json:"intent_revision"`
}
type ClosePresentationInput struct {
	Reason string `json:"reason"`
}
type Presentation struct {
	PresentationID       string        `json:"presentation_id"`
	Revision             uint64        `json:"revision"`
	EndpointID           string        `json:"endpoint_id"`
	InstanceID           string        `json:"instance_id"`
	SurfaceRef           api.ObjectRef `json:"surface_ref"`
	State                string        `json:"state"`
	IntentRevision       uint64        `json:"intent_revision"`
	Generation           uint64        `json:"generation"`
	CredentialGeneration uint64        `json:"credential_generation"`
	Presented            bool          `json:"presented"`
}
type presentationRecord struct {
	Presentation
	SubjectID string `json:"subject_id"`
}
type RenderReadInput struct {
	Generation     uint64 `json:"generation"`
	IntentRevision uint64 `json:"intent_revision"`
	KnownHash      string `json:"known_hash,omitempty"`
}
type RenderBody struct {
	ContentRef api.ContentRef `json:"content_ref"`
	Base64     string         `json:"base64"`
}
type RenderView struct {
	Presentation Presentation     `json:"presentation"`
	Surface      Surface          `json:"surface"`
	Requests     []RequestView    `json:"requests"`
	RequiredRefs []api.ContentRef `json:"required_refs"`
	Bodies       []RenderBody     `json:"bodies"`
	NotModified  bool             `json:"not_modified"`
}
type RenderAckInput struct {
	Generation     uint64           `json:"generation"`
	IntentRevision uint64           `json:"intent_revision"`
	SurfaceRef     api.ObjectRef    `json:"surface_ref"`
	RenderedRefs   []api.ContentRef `json:"rendered_refs"`
	RenderSuccess  bool             `json:"render_success"`
}
type ApplicationEventInput struct {
	Generation     uint64          `json:"generation"`
	IntentRevision uint64          `json:"intent_revision"`
	SurfaceRef     api.ObjectRef   `json:"surface_ref"`
	Name           string          `json:"name"`
	Payload        json.RawMessage `json:"payload"`
}
type ApplicationEventOutput struct {
	EventRef    api.ObjectRef `json:"event_ref"`
	State       string        `json:"state"`
	QueryMethod string        `json:"query_method"`
}
type ApplicationEvent struct {
	EventID         string        `json:"event_id"`
	Revision        uint64        `json:"revision"`
	PresentationRef api.ObjectRef `json:"presentation_ref"`
	SurfaceRef      api.ObjectRef `json:"surface_ref"`
	Name            string        `json:"name"`
	State           string        `json:"state"`
	Command         api.Command   `json:"command"`
	Receipt         *api.Receipt  `json:"receipt,omitempty"`
}
type applicationEventRecord struct {
	ApplicationEvent
	Auth runtime.Auth `json:"auth"`
}

var eventName = regexp.MustCompile(`^[a-z][a-z0-9_.]{0,63}$`)

func bindingKey(ref api.ObjectRef) string {
	return ref.TenantID + ":" + ref.OwnerID + ":" + ref.ObjectID + fmt.Sprint(":", ref.Revision)
}
func validateBindings(bindings []EventBinding) (map[string]EventBinding, error) {
	if len(bindings) > 64 {
		return nil, fmt.Errorf("unbounded application bindings")
	}
	out := map[string]EventBinding{}
	for _, b := range bindings {
		if err := api.ValidateRecord("ObjectRef", b.BindingRef); err != nil {
			return nil, err
		}
		key := bindingKey(b.BindingRef)
		if _, ok := out[key]; ok || len(b.Events) > 64 {
			return nil, fmt.Errorf("duplicate or unbounded application binding")
		}
		seen := map[string]bool{}
		for _, rule := range b.Events {
			if !eventName.MatchString(rule.Name) || seen[rule.Name] || !api.ValidID(rule.OwnerID) || !api.ValidID(rule.TargetID) || !eventName.MatchString(rule.Method) || rule.AcceptForSeconds == 0 || rule.AcceptForSeconds > 3600 || rule.ExpectedRevision != nil && (*rule.ExpectedRevision == 0 || *rule.ExpectedRevision > api.MaxSafeInteger) {
				return nil, fmt.Errorf("invalid fixed application event")
			}
			seen[rule.Name] = true
			var schema api.Schema
			if err := api.Decode(rule.Schema, &schema); err != nil {
				return nil, err
			}
			if err := restrictedFormSchema(schema); err != nil {
				return nil, err
			}
			if _, err := api.NewValidator(schema); err != nil {
				return nil, err
			}
		}
		var clone EventBinding
		if err := json.Unmarshal(api.Raw(b), &clone); err != nil {
			return nil, err
		}
		out[key] = clone
	}
	return out, nil
}

// 受限表单仅有有界字段与本地约束，禁止脚本、远端/递归 $ref 和开放对象。
func restrictedFormSchema(schema api.Schema) error {
	nodes := 0
	var inspect func(api.Schema, int) error
	inspect = func(s api.Schema, depth int) error {
		nodes++
		if depth > 8 || nodes > 256 {
			return fmt.Errorf("form schema budget exceeded")
		}
		allowed := map[string]bool{"oneOf": true, "type": true, "properties": true, "required": true, "additionalProperties": true, "enum": true, "const": true, "minLength": true, "maxLength": true, "minimum": true, "maximum": true, "items": true, "minItems": true, "maxItems": true, "description": true, "title": true}
		for k := range s {
			if !allowed[k] {
				return fmt.Errorf("unsupported form schema keyword %s", k)
			}
		}
		if options, ok := s["oneOf"]; ok {
			branches, ok := options.([]any)
			if !ok || len(branches) < 2 || len(branches) > 8 || len(s) != 1 {
				return fmt.Errorf("invalid bounded form variants")
			}
			for _, branch := range branches {
				child, ok := branch.(map[string]any)
				if !ok {
					return fmt.Errorf("invalid form variant")
				}
				if err := inspect(child, depth+1); err != nil {
					return err
				}
			}
			return nil
		}
		if value, ok := s["const"]; ok {
			switch v := value.(type) {
			case string:
				if len(v) > 65536 {
					return fmt.Errorf("unbounded form const")
				}
			case float64:
				if v < -float64(api.MaxSafeInteger) || v > float64(api.MaxSafeInteger) {
					return fmt.Errorf("unbounded form const")
				}
			case bool:
			default:
				return fmt.Errorf("unsupported form const")
			}
			if len(s) == 1 {
				return nil
			}
		}
		kind, _ := s["type"].(string)
		switch kind {
		case "object":
			if s["additionalProperties"] != false {
				return fmt.Errorf("form object must be closed")
			}
			p, ok := s["properties"].(map[string]any)
			if !ok || len(p) > 64 {
				return fmt.Errorf("unbounded form fields")
			}
			for _, v := range p {
				child, ok := v.(map[string]any)
				if !ok {
					return fmt.Errorf("invalid form field")
				}
				if err := inspect(child, depth+1); err != nil {
					return err
				}
			}
		case "array":
			max, ok := schemaNumber(s["maxItems"])
			if !ok || max < 1 || max > 100 {
				return fmt.Errorf("unbounded form array")
			}
			child, ok := s["items"].(map[string]any)
			if !ok {
				return fmt.Errorf("invalid form array")
			}
			return inspect(child, depth+1)
		case "string":
			max, ok := schemaNumber(s["maxLength"])
			if !ok || max < 1 || max > 65536 {
				return fmt.Errorf("unbounded form string")
			}
		case "integer", "number":
			min, ok := schemaNumber(s["minimum"])
			max, okMax := schemaNumber(s["maximum"])
			if !ok || !okMax || min > max || min < -float64(api.MaxSafeInteger) || max > float64(api.MaxSafeInteger) {
				return fmt.Errorf("unbounded form number")
			}
		case "boolean":
		default:
			return fmt.Errorf("unsupported form field type")
		}
		if values, ok := s["enum"].([]any); ok && len(values) > 100 {
			return fmt.Errorf("unbounded form enum")
		}
		return nil
	}
	encoded := api.Raw(schema)
	var normalized api.Schema
	if err := json.Unmarshal(encoded, &normalized); err != nil {
		return err
	}
	return inspect(normalized, 0)
}
func schemaNumber(v any) (float64, bool) { n, ok := v.(float64); return n, ok }
func (s *Service) checkSurfaceInput(ctx context.Context, tx runtime.Tx, a runtime.Auth, in SurfaceInput) error {
	if _, ok := s.bindings[bindingKey(in.BindingRef)]; !ok || in.BindingRef.TenantID != tx.Scope().TenantID || in.BindingRef.OwnerID != tx.Scope().OwnerID {
		return api.E("forbidden", "application_binding_unregistered")
	}
	if len(in.RequestRefs) > 20 {
		return invalid("request_limit")
	}
	if in.TaskRef != nil {
		if err := runtime.CheckRef(tx.Scope(), *in.TaskRef); err != nil {
			return err
		}
	}
	if err := s.content(ctx, tx, a, in.SnapshotRef, "interaction.snapshot"); err != nil {
		return err
	}
	for _, ref := range in.RequestRefs {
		if s.ports.Requests == nil {
			return api.E("unsupported", "request_views_unconfigured")
		}
		if _, err := s.ports.Requests.CheckTx(ctx, tx, a, ref); err != nil {
			return err
		}
	}
	return nil
}
func (s *Service) CreateSurfaceTx(ctx context.Context, tx runtime.Tx, a runtime.Auth, c api.Command, in SurfaceInput) (Surface, error) {
	if err := s.checkSurfaceInput(ctx, tx, a, in); err != nil {
		return Surface{}, err
	}
	r := surfaceRecord{Surface: Surface{SurfaceInput: in, SurfaceID: c.TargetID, TenantID: tx.Scope().TenantID, OwnerID: tx.Scope().OwnerID, Revision: 1, State: "open"}, SubjectID: a.SubjectID}
	if err := tx.Create(ctx, surfaces, c.TargetID, a.SubjectID, r); err != nil {
		return Surface{}, err
	}
	return r.Surface, nil
}
func (s *Service) UpdateSurfaceTx(ctx context.Context, tx runtime.Tx, a runtime.Auth, c api.Command, in SurfaceInput) (Surface, error) {
	r, err := getSurface(ctx, tx, a, c.TargetID)
	if err != nil {
		return Surface{}, err
	}
	if c.ExpectedRevision == nil || *c.ExpectedRevision != r.Revision {
		return Surface{}, api.E("revision_conflict", "surface_changed")
	}
	if r.State != "open" {
		return Surface{}, api.E("invalid_state", "surface_closed")
	}
	if err = s.checkSurfaceInput(ctx, tx, a, in); err != nil {
		return Surface{}, err
	}
	r.SurfaceInput = in
	old := r.Revision
	r.Revision++
	if err = tx.Put(ctx, surfaces, c.TargetID, old, r); err != nil {
		return Surface{}, err
	}
	return r.Surface, nil
}
func getSurface(ctx context.Context, tx runtime.Tx, a runtime.Auth, id string) (surfaceRecord, error) {
	var r surfaceRecord
	_, err := tx.Get(ctx, surfaces, id, &r)
	if err == nil {
		err = access(a, r.SubjectID)
	}
	return r, err
}
func (s *Service) CloseSurfaceTx(ctx context.Context, tx runtime.Tx, a runtime.Auth, c api.Command, in SurfaceControlInput) (Surface, error) {
	r, err := getSurface(ctx, tx, a, c.TargetID)
	if err != nil {
		return Surface{}, err
	}
	if c.ExpectedRevision == nil || *c.ExpectedRevision != r.Revision {
		return Surface{}, api.E("revision_conflict", "surface_changed")
	}
	old := r.Revision
	r.Revision++
	r.State = "closed"
	if err = tx.Put(ctx, surfaces, c.TargetID, old, r); err != nil {
		return Surface{}, err
	}
	return r.Surface, nil
}
func (s *Service) ReadSurface(ctx context.Context, store runtime.Store, scope runtime.Scope, a runtime.Auth, id string) (Surface, error) {
	var out Surface
	status, err := store.Within(ctx, scope, s.config.Participants, func(tx runtime.Tx) error {
		r, e := getSurface(ctx, tx, a, id)
		if e != nil {
			return e
		}
		if e = s.content(ctx, tx, a, r.SnapshotRef, "interaction.snapshot"); e != nil {
			return e
		}
		out = r.Surface
		return nil
	})
	if status == runtime.CommitUnknown {
		return Surface{}, runtime.ErrCommitUnknown
	}
	return out, err
}
func (s *Service) OpenPresentationTx(ctx context.Context, tx runtime.Tx, a runtime.Auth, c api.Command, in OpenPresentationInput) (Presentation, error) {
	if !api.ValidID(in.EndpointID) || !api.ValidID(in.InstanceID) {
		return Presentation{}, invalid("invalid_presentation_endpoint")
	}
	if err := exactScope(tx.Scope(), in.SurfaceRef); err != nil {
		return Presentation{}, err
	}
	surface, err := getSurface(ctx, tx, a, in.SurfaceRef.ObjectID)
	if err != nil {
		return Presentation{}, err
	}
	if surface.Revision != in.SurfaceRef.Revision || surface.State != "open" {
		return Presentation{}, api.E("revision_conflict", "surface_changed")
	}
	if err = s.content(ctx, tx, a, surface.SnapshotRef, "interaction.snapshot"); err != nil {
		return Presentation{}, err
	}
	r := presentationRecord{Presentation: Presentation{PresentationID: c.TargetID, Revision: 1, EndpointID: in.EndpointID, InstanceID: in.InstanceID, SurfaceRef: in.SurfaceRef, State: "open", IntentRevision: 1, CredentialGeneration: a.CredentialGeneration}, SubjectID: a.SubjectID}
	if err = tx.Create(ctx, presentations, c.TargetID, in.SurfaceRef.ObjectID, r); err != nil {
		return Presentation{}, err
	}
	return r.Presentation, nil
}
func getPresentation(ctx context.Context, tx runtime.Tx, a runtime.Auth, id string) (presentationRecord, error) {
	var r presentationRecord
	_, err := tx.Get(ctx, presentations, id, &r)
	if err == nil {
		err = access(a, r.SubjectID)
	}
	return r, err
}
func savePresentation(ctx context.Context, tx runtime.Tx, r *presentationRecord) error {
	old := r.Revision
	r.Revision++
	return tx.Put(ctx, presentations, r.PresentationID, old, *r)
}
func (s *Service) BeginPresentationTx(ctx context.Context, tx runtime.Tx, a runtime.Auth, c api.Command, in BeginPresentationInput) (Presentation, error) {
	r, err := getPresentation(ctx, tx, a, c.TargetID)
	if err != nil {
		return Presentation{}, err
	}
	if c.ExpectedRevision == nil || *c.ExpectedRevision != r.Revision || in.IntentRevision != r.IntentRevision {
		return Presentation{}, api.E("revision_conflict", "presentation_changed")
	}
	if r.State != "open" {
		return Presentation{}, api.E("invalid_state", "presentation_closed")
	}
	surface, err := getSurface(ctx, tx, a, r.SurfaceRef.ObjectID)
	if err != nil {
		return Presentation{}, err
	}
	if surface.State != "open" {
		return Presentation{}, api.E("invalid_state", "surface_closed")
	}
	if err = s.content(ctx, tx, a, surface.SnapshotRef, "interaction.snapshot"); err != nil {
		return Presentation{}, err
	}
	r.SurfaceRef = tx.Scope().Ref(surface.SurfaceID, surface.Revision)
	r.Generation++
	r.CredentialGeneration = a.CredentialGeneration
	r.Presented = false
	if err = savePresentation(ctx, tx, &r); err != nil {
		return Presentation{}, err
	}
	return r.Presentation, nil
}
func (s *Service) SwitchPresentationTx(ctx context.Context, tx runtime.Tx, a runtime.Auth, c api.Command, in OpenPresentationInput) (Presentation, error) {
	r, err := getPresentation(ctx, tx, a, c.TargetID)
	if err != nil {
		return Presentation{}, err
	}
	if c.ExpectedRevision == nil || *c.ExpectedRevision != r.Revision {
		return Presentation{}, api.E("revision_conflict", "presentation_changed")
	}
	if err = exactScope(tx.Scope(), in.SurfaceRef); err != nil {
		return Presentation{}, err
	}
	surface, err := getSurface(ctx, tx, a, in.SurfaceRef.ObjectID)
	if err != nil {
		return Presentation{}, err
	}
	if surface.State != "open" || surface.Revision != in.SurfaceRef.Revision {
		return Presentation{}, api.E("revision_conflict", "surface_changed")
	}
	if !api.ValidID(in.EndpointID) || !api.ValidID(in.InstanceID) {
		return Presentation{}, invalid("invalid_presentation_endpoint")
	}
	r.EndpointID = in.EndpointID
	r.InstanceID = in.InstanceID
	r.SurfaceRef = in.SurfaceRef
	r.State = "open"
	r.IntentRevision++
	r.Generation++
	r.CredentialGeneration = a.CredentialGeneration
	r.Presented = false
	if err = savePresentation(ctx, tx, &r); err != nil {
		return Presentation{}, err
	}
	return r.Presentation, nil
}
func (s *Service) ClosePresentationTx(ctx context.Context, tx runtime.Tx, a runtime.Auth, c api.Command, in ClosePresentationInput) (Presentation, error) {
	r, err := getPresentation(ctx, tx, a, c.TargetID)
	if err != nil {
		return Presentation{}, err
	}
	if c.ExpectedRevision == nil || *c.ExpectedRevision != r.Revision {
		return Presentation{}, api.E("revision_conflict", "presentation_changed")
	}
	r.State = "closed"
	r.IntentRevision++
	r.Generation++
	r.Presented = false
	if err = savePresentation(ctx, tx, &r); err != nil {
		return Presentation{}, err
	}
	return r.Presentation, nil
}
func (s *Service) renderGate(ctx context.Context, tx runtime.Tx, a runtime.Auth, id string, generation, intent uint64) (RenderView, error) {
	p, err := getPresentation(ctx, tx, a, id)
	if err != nil {
		return RenderView{}, err
	}
	if p.State != "open" || p.Generation == 0 || p.Generation != generation || p.IntentRevision != intent {
		return RenderView{}, api.E("invalid_state", "render_generation_stale")
	}
	if a.SubjectID != p.SubjectID || p.CredentialGeneration != a.CredentialGeneration {
		return RenderView{}, api.E("forbidden", "render_identity_changed")
	}
	surface, err := getSurface(ctx, tx, a, p.SurfaceRef.ObjectID)
	if err != nil {
		return RenderView{}, err
	}
	if surface.State != "open" || surface.Revision != p.SurfaceRef.Revision {
		return RenderView{}, api.E("invalid_state", "render_surface_changed")
	}
	if _, ok := s.bindings[bindingKey(surface.BindingRef)]; !ok {
		return RenderView{}, api.E("forbidden", "application_binding_unregistered")
	}
	view := RenderView{Presentation: p.Presentation, Surface: surface.Surface, Requests: []RequestView{}, RequiredRefs: []api.ContentRef{surface.SnapshotRef}, Bodies: []RenderBody{}}
	for _, ref := range surface.RequestRefs {
		if s.ports.Requests == nil {
			return view, api.E("unsupported", "request_views_unconfigured")
		}
		request, err := s.ports.Requests.CheckTx(ctx, tx, a, ref)
		if err != nil {
			return view, err
		}
		if err = restrictedSchemaRaw(request.AnswerSchema); err != nil {
			return view, api.E("unsupported", "renderer_schema_unsupported")
		}
		view.Requests = append(view.Requests, request)
		view.RequiredRefs = appendUniqueContent(view.RequiredRefs, request.Request.QuestionRef)
		for _, preview := range request.Request.PreviewRefs {
			view.RequiredRefs = appendUniqueContent(view.RequiredRefs, preview)
		}
	}
	if len(view.RequiredRefs) > 100 {
		return view, invalid("render_content_limit")
	}
	for i, ref := range view.RequiredRefs {
		purpose := "interaction.preview"
		if i == 0 {
			purpose = "interaction.snapshot"
		}
		if err = s.content(ctx, tx, a, ref, purpose); err != nil {
			return view, err
		}
	}
	return view, nil
}
func restrictedSchemaRaw(raw json.RawMessage) error {
	var schema api.Schema
	if err := api.Decode(raw, &schema); err != nil {
		return err
	}
	return restrictedFormSchema(schema)
}
func appendUniqueContent(refs []api.ContentRef, ref api.ContentRef) []api.ContentRef {
	for _, r := range refs {
		if r == ref {
			return refs
		}
	}
	return append(refs, ref)
}
func (s *Service) ReadPresentation(ctx context.Context, store runtime.Store, scope runtime.Scope, a runtime.Auth, id string, in RenderReadInput) (RenderView, error) {
	var view RenderView
	status, err := store.Within(ctx, scope, s.config.Participants, func(tx runtime.Tx) error {
		var e error
		view, e = s.renderGate(ctx, tx, a, id, in.Generation, in.IntentRevision)
		return e
	})
	if status == runtime.CommitUnknown {
		return RenderView{}, runtime.ErrCommitUnknown
	}
	if err != nil {
		return RenderView{}, err
	}
	view.NotModified = in.KnownHash == view.Surface.SnapshotRef.Hash
	var bytes uint64
	for i, ref := range view.RequiredRefs {
		if i == 0 && view.NotModified {
			continue
		}
		bytes += ref.ByteLength
		if bytes > 128<<10 {
			return RenderView{}, api.E("overloaded", "render_body_budget")
		}
		purpose := "interaction.preview"
		if i == 0 {
			purpose = "interaction.snapshot"
		}
		body, e := s.ports.Content.Read(ctx, scope, a, ref, purpose)
		if e != nil {
			return RenderView{}, e
		}
		if uint64(len(body)) != ref.ByteLength || api.Hash(body) != ref.Hash {
			return RenderView{}, api.E("invalid_request", "render_body_mismatch")
		}
		view.Bodies = append(view.Bodies, RenderBody{ContentRef: ref, Base64: base64.StdEncoding.EncodeToString(body)})
	}
	status, err = store.Within(ctx, scope, s.config.Participants, func(tx runtime.Tx) error {
		current, e := s.renderGate(ctx, tx, a, id, in.Generation, in.IntentRevision)
		if e != nil {
			return e
		}
		if !api.Equal(current.RequiredRefs, view.RequiredRefs) || !api.Equal(current.Surface, view.Surface) {
			return api.E("invalid_state", "render_surface_changed")
		}
		return nil
	})
	if status == runtime.CommitUnknown {
		return RenderView{}, runtime.ErrCommitUnknown
	}
	if err != nil {
		return RenderView{}, err
	}
	return view, nil
}
func (s *Service) RenderedTx(ctx context.Context, tx runtime.Tx, a runtime.Auth, c api.Command, in RenderAckInput) (Presentation, error) {
	view, err := s.renderGate(ctx, tx, a, c.TargetID, in.Generation, in.IntentRevision)
	if err != nil {
		return Presentation{}, err
	}
	if in.SurfaceRef != view.Presentation.SurfaceRef {
		return Presentation{}, api.E("revision_conflict", "render_surface_changed")
	}
	if in.RenderSuccess && !api.Equal(in.RenderedRefs, view.RequiredRefs) {
		return Presentation{}, invalid("rendered_refs_incomplete")
	}
	r, err := getPresentation(ctx, tx, a, c.TargetID)
	if err != nil {
		return Presentation{}, err
	}
	r.Presented = in.RenderSuccess
	if err = savePresentation(ctx, tx, &r); err != nil {
		return Presentation{}, err
	}
	return r.Presentation, nil
}
