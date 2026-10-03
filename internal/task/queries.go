package task

import (
	"context"
	"encoding/base64"
	"encoding/json"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/runtime"
)

type cursor struct {
	Owner    string `json:"owner"`
	Tenant   string `json:"tenant"`
	Subject  string `json:"subject"`
	Kind     string `json:"kind"`
	Revision uint64 `json:"revision"`
	Last     string `json:"last"`
	Filter   string `json:"filter"`
}

func (s *Service) List(ctx context.Context, store runtime.Store, scope runtime.Scope, auth runtime.Auth, in TaskListInput) (api.Page[api.Task], error) {
	if in.Limit < 1 || in.Limit > 100 || !statusAllowed(in.Status) {
		return api.Page[api.Task]{}, invalid("invalid_list")
	}
	var c collection
	_, e := store.Read(ctx, scope, collections, auth.SubjectID, 0, &c)
	if confirmedNotFound(e) {
		return api.Page[api.Task]{Items: []api.Task{}, CollectionRevision: 1, Exhausted: true, Gaps: []string{}}, nil
	}
	if e != nil {
		return api.Page[api.Task]{}, e
	}
	last, e := parseCursor(scope, auth, in.Cursor, "task", c.Revision, in.Status)
	if e != nil {
		return api.Page[api.Task]{}, e
	}
	rows, e := store.List(ctx, scope, tasks, auth.SubjectID, last, int(in.Limit)+1)
	if e != nil {
		return api.Page[api.Task]{}, e
	}
	page := api.Page[api.Task]{Items: []api.Task{}, CollectionRevision: c.Revision, Exhausted: len(rows) <= int(in.Limit), Gaps: []string{}}
	if len(rows) > int(in.Limit) {
		rows = rows[:in.Limit]
	}
	last = ""
	for _, r := range rows {
		last = r.ID
		var t taskState
		if e = r.Decode(&t); e != nil {
			return page, e
		}
		if in.Status == "" || in.Status == t.Task.Status {
			page.Items = append(page.Items, t.Task)
		}
	}
	if !page.Exhausted {
		page.NextCursor = makeCursor(cursor{Owner: scope.OwnerID, Tenant: scope.TenantID, Subject: auth.SubjectID, Kind: "task", Revision: c.Revision, Last: last, Filter: in.Status})
	}
	return page, nil
}
func parseCursor(scope runtime.Scope, auth runtime.Auth, encoded, kind string, revision uint64, filter string) (string, error) {
	if encoded == "" {
		return "", nil
	}
	if len(encoded) > 2048 {
		return "", invalid("invalid_cursor")
	}
	raw, e := base64.RawURLEncoding.DecodeString(encoded)
	if e != nil {
		return "", invalid("invalid_cursor")
	}
	var c cursor
	if e = api.Decode(raw, &c); e != nil {
		return "", invalid("invalid_cursor")
	}
	if c.Owner != scope.OwnerID || c.Tenant != scope.TenantID || c.Subject != auth.SubjectID || c.Kind != kind || c.Filter != filter {
		return "", api.E("forbidden", "cursor_scope_mismatch")
	}
	if c.Revision != revision {
		return "", api.E("cursor_expired", "collection_changed")
	}
	return c.Last, nil
}
func makeCursor(c cursor) string { return base64.RawURLEncoding.EncodeToString(api.Raw(c)) }
func (s *Service) ChildList(ctx context.Context, store runtime.Store, scope runtime.Scope, auth runtime.Auth, in api.ListInput) (api.Page[ChildHandle], error) {
	if in.Limit < 1 || in.Limit > 100 {
		return api.Page[ChildHandle]{}, invalid("invalid_list")
	}
	rows, e := store.List(ctx, scope, children, auth.SubjectID, "", int(s.config.MaxTasksPerSubject)+1)
	if e != nil {
		return api.Page[ChildHandle]{}, e
	}
	if uint64(len(rows)) > s.config.MaxTasksPerSubject {
		return api.Page[ChildHandle]{}, api.E("dependency_unavailable", "child_collection_capacity")
	}
	revision := uint64(1)
	for _, r := range rows {
		revision += r.Revision
	}
	last, e := parseCursor(scope, auth, in.Cursor, "child", revision, "")
	if e != nil {
		return api.Page[ChildHandle]{}, e
	}
	page := api.Page[ChildHandle]{Items: []ChildHandle{}, CollectionRevision: revision, Gaps: []string{}}
	remaining := []runtime.Record{}
	for _, r := range rows {
		if r.ID > last {
			remaining = append(remaining, r)
		}
	}
	page.Exhausted = len(remaining) <= int(in.Limit)
	if len(remaining) > int(in.Limit) {
		remaining = remaining[:in.Limit]
	}
	for _, r := range remaining {
		var h ChildHandle
		if e = r.Decode(&h); e != nil {
			return page, e
		}
		page.Items = append(page.Items, h)
		last = r.ID
	}
	if !page.Exhausted {
		page.NextCursor = makeCursor(cursor{Owner: scope.OwnerID, Tenant: scope.TenantID, Subject: auth.SubjectID, Kind: "child", Revision: revision, Last: last})
	}
	return page, nil
}
func decodeQueryInput[T any](q api.Query) (T, error) {
	var in T
	e := json.Unmarshal(q.Payload, &in)
	return in, e
}

type BudgetReadInput struct {
	TaskID        string         `json:"task_id,omitempty"`
	AllocationRef *api.ObjectRef `json:"allocation_ref,omitempty"`
	Incoming      bool           `json:"incoming,omitempty"`
}
type BudgetReadResponse struct {
	Task               *BudgetReadOutput   `json:"task,omitempty"`
	Allocation         *Allocation         `json:"allocation,omitempty"`
	IncomingAllocation *IncomingAllocation `json:"incoming_allocation,omitempty"`
}

func (s *Service) budgetQuery(ctx context.Context, store runtime.Store, scope runtime.Scope, auth runtime.Auth, q api.Query, in BudgetReadInput) (BudgetReadResponse, error) {
	if in.AllocationRef != nil {
		if q.TargetID != in.AllocationRef.ObjectID {
			return BudgetReadResponse{}, invalid("target_mismatch")
		}
		if in.Incoming {
			a, e := s.IncomingRead(ctx, store, scope, auth, *in.AllocationRef)
			return BudgetReadResponse{IncomingAllocation: &a}, e
		}
		if in.AllocationRef.OwnerID != scope.OwnerID || in.AllocationRef.TenantID != scope.TenantID {
			return BudgetReadResponse{}, api.E("forbidden", "allocation_scope_mismatch")
		}
		a, e := s.AllocationRead(ctx, store, scope, auth, in.AllocationRef.ObjectID)
		return BudgetReadResponse{Allocation: &a}, e
	}
	if in.TaskID != q.TargetID {
		return BudgetReadResponse{}, invalid("task_id_required")
	}
	t, e := s.BudgetRead(ctx, store, scope, auth, in.TaskID)
	return BudgetReadResponse{Task: &t}, e
}
