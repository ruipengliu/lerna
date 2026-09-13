// Package answer exercises the bounded model and controlled publication path.
package answer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"lerna/adapters/contentpolicy"
	"lerna/adapters/filecontent"
	"lerna/adapters/sqliteauth"
	"lerna/adapters/tasklocal"
	"lerna/answers"
	"lerna/artifacts"
	"lerna/authorization"
	"lerna/brain"
	wire "lerna/gen/harness/v1"
	"lerna/sdk"
	"lerna/tasks"
	"os"
	"path/filepath"
	"time"
)

const publicFact = "Public test fact: the reference project has three systems: Memory, Brain, Execution."

type wallClock struct{}

func (wallClock) Now() (time.Time, error) { return time.Now(), nil }

type harness struct {
	root, token string
	db          *sqliteauth.Store
	blobs       *filecontent.Store
	auth        *authorization.Service
	core        *tasks.Service
	content     *artifacts.Service
	policy      *contentpolicy.Policy
	access      *answers.ContentAccess
	client      *sdk.TaskClient
	port        *answers.Port
	generation  *tasks.GenerationPort
	source      *wire.ContentSource
	location    string
}

// Publication includes current context and lineage checks before the Core write.
// Bound that composed operation separately from individual one-second storage calls.
func limits() tasks.RunLimits {
	return tasks.RunLimits{Lease: 10 * time.Second, RenewEvery: time.Second, DecisionTimeout: 45 * time.Second, IOTimeout: 5 * time.Second, MaxAttempts: 3, MaxConcurrent: 1}
}
func open(ctx context.Context, root, token, location string, l tasks.GenerationLimits) (h *harness, err error) {
	h = &harness{root: root, token: token, location: location, source: &wire.ContentSource{Kind: "input", Key: "public-test", Revision: 1}}
	h.db, err = sqliteauth.Open(filepath.Join(root, "state.db"))
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			h.close()
		}
	}()
	h.auth, err = authorization.New(h.db, wallClock{}, authorization.Config{CredentialTTL: 24 * time.Hour, GrantTTL: time.Hour, WindowTTL: time.Minute, ReceiptRetention: time.Hour, MaxRules: 32, MaxResources: 128, MaxDepth: 16, MaxWork: 4096, EvaluationTimeout: time.Second})
	if err != nil {
		return nil, err
	}
	h.core, err = tasks.New(h.auth, tasks.Config{Namespace: "local", Resource: "root", Owner: "local-owner", MaxTasks: 100, MaxPage: 10})
	if err != nil {
		return nil, err
	}
	if token == "" {
		h.token, err = h.auth.Bootstrap(ctx, "local", "operator")
		if err != nil {
			return nil, err
		}
		scope := &wire.AuthorizationScope{Resources: &wire.ResourceSelector{Selection: &wire.ResourceSelector_Exact{Exact: "root"}}, Actions: []string{"task.submit", "task.read", "task.execute", "task.reconcile", "task.pause", "task.cancel", "task.resume", "content.store", "content.process", "content.retain", "content.discover", "content.disclose", "content.delete"}, Purposes: []string{"task"}, Locations: []string{"local", location}, ExpiresUnix: time.Now().Add(time.Hour).Unix()}
		for i, c := range []*wire.AuthorizationCommand{{Change: &wire.AuthorizationCommand_ReplacePolicy{ReplacePolicy: &wire.ReplacePolicy{Rules: []*wire.PolicyRule{{Id: "answer", Scope: scope}}}}}, {Change: &wire.AuthorizationCommand_IssueGrant{IssueGrant: &wire.LocalGrant{Id: "answer", Subject: "operator", Mode: "continuous", Scope: scope}}}} {
			op, e := h.auth.NewOperation(ctx, h.token)
			if e != nil {
				return nil, e
			}
			c.ExpectedRevision = uint64(i)
			_, err = h.auth.Execute(ctx, h.token, authorization.Mutation{Namespace: "local", OperationID: op, Command: c})
			if err != nil {
				return nil, err
			}
		}
	}
	h.client = sdk.NewTaskClient(tasklocal.Bind(h.core, h.token), "local")
	h.blobs, err = filecontent.Open(filepath.Join(root, "content"))
	if err != nil {
		return nil, err
	}
	h.policy, err = contentpolicy.New([]contentpolicy.Rule{h.rule(true)})
	if err != nil {
		return nil, err
	}
	h.content, err = artifacts.New(h.auth, h.blobs, h.policy, artifacts.Config{Inline: 64, MaxObject: 1 << 20, MaxTotal: 2 << 20, MaxRecords: 32, MaxChunk: 65536, MaxFiles: 64, CleanupBatch: 16, Timeout: time.Second, Retention: time.Hour})
	if err != nil {
		return nil, err
	}
	h.access, err = answers.NewContentAccess(h.content, h.policy, h.binding(), h.source, "task", wallClock{}, 5*time.Minute)
	if err != nil {
		return nil, err
	}
	work, e := h.core.BindWorker(tasks.WorkerBinding{Token: h.token, Subject: "operator", WorkerID: "answer-worker"}, limits())
	if e != nil {
		return nil, e
	}
	h.generation, err = work.Generations(l)
	if err != nil {
		return nil, err
	}
	h.generation = h.generation.WithOutputIdentities(func(ctx context.Context) (string, error) { return h.auth.NewOperation(ctx, h.token) })
	h.port = answers.BindPort(h.generation, h.access, location)
	return h, nil
}
func (h *harness) rule(remote bool) contentpolicy.Rule {
	locations := []string{"local"}
	if remote {
		locations = append(locations, h.location)
	}
	return contentpolicy.Rule{Kind: "input", Key: "public-test", Revision: 1, Actions: []string{"store", "process", "retain", "discover", "disclose", "delete"}, Purposes: []string{"task"}, Locations: locations, RetainUntil: time.Now().Add(time.Hour).Unix()}
}
func (h *harness) binding() artifacts.Binding {
	return artifacts.Binding{Token: h.token, Namespace: "local", Location: "local", Recipient: "local"}
}
func (h *harness) close() {
	if h.blobs != nil {
		h.blobs.Close()
	}
	if h.db != nil {
		h.db.Close()
	}
}
func fresh(ctx context.Context, l tasks.GenerationLimits) (*harness, error) {
	root, e := os.MkdirTemp("", "answer-profile-")
	if e != nil {
		return nil, e
	}
	h, e := open(ctx, root, "", "test-model-location", l)
	if e != nil {
		os.RemoveAll(root)
	}
	return h, e
}
func (h *harness) destroy() { h.close(); os.RemoveAll(h.root) }
func (h *harness) submit(ctx context.Context, l tasks.GenerationLimits) (tasks.RunSnapshot, error) {
	return h.submitGoal(ctx, l, "List the three systems exactly as named in the supplied public fact, and cite its reference.")
}
func (h *harness) submitGoal(ctx context.Context, l tasks.GenerationLimits, goal string) (tasks.RunSnapshot, error) {
	body := []byte(publicFact)
	sum := sha256.Sum256(body)
	op, e := h.auth.NewOperation(ctx, h.token)
	if e != nil {
		return tasks.RunSnapshot{}, e
	}
	now := time.Now()
	out, e := h.content.Call(ctx, h.binding(), &wire.ContentRequest{Method: "PUT", OperationId: op, Data: body, Spec: &wire.ContentSpec{Kind: "evidence", Resource: "root", Purpose: "task", Sources: []*wire.ContentSource{h.source}, AcquiredAt: now.Unix(), MediaType: "text/plain", Size: uint64(len(body)), Sha256: hex.EncodeToString(sum[:]), RetainUntil: now.Add(10 * time.Minute).Unix()}})
	if e != nil {
		return tasks.RunSnapshot{}, e
	}
	op, e = h.auth.NewOperation(ctx, h.token)
	if e != nil {
		return tasks.RunSnapshot{}, e
	}
	t, e := h.client.Submit(ctx, tasks.Submission{Namespace: "local", OperationID: op, Goal: goal, InputRefs: []string{answers.Reference(out.Record.Ref)}, Constraints: tasks.Constraints{MaxSteps: 3, DeadlineUnix: now.Add(5 * time.Minute).Unix(), ModelRequests: l.Requests, ModelTokens: uint64(l.Requests) * (l.InputTokens + l.OutputTokens)}})
	if e != nil {
		return tasks.RunSnapshot{}, e
	}
	return h.core.Load(ctx, t.Ref)
}
func (h *harness) run(ctx context.Context, s tasks.RunSnapshot, m brain.Model) (tasks.RunSnapshot, error) {
	b, e := brain.NewAnswer(m, h.access, h.access, h.generation, brain.Config{MaxInputBytes: brain.MaxInputBytes, MaxOutputBytes: brain.MaxAnswerBytes, SettlementTimeout: time.Second})
	if e != nil {
		return tasks.RunSnapshot{}, e
	}
	r, e := tasks.NewRunner(h.port, b, limits(), wallClock{})
	if e != nil {
		return tasks.RunSnapshot{}, e
	}
	return r.Run(ctx, s)
}

type controlledModel struct {
	calls int
	fn    func(context.Context, brain.Request) (brain.Result, error)
}

func (m *controlledModel) Capabilities() brain.Capabilities {
	return brain.Capabilities{Model: "controlled-text", Version: "1", Location: "test-model-location", Text: true, Structured: true, HardBounds: true, ContextTokens: 1024, InputUpper: 600}
}
func (m *controlledModel) Generate(ctx context.Context, r brain.Request) (brain.Result, error) {
	m.calls++
	if m.fn != nil {
		return m.fn(ctx, r)
	}
	b, _ := json.Marshal(brain.Answer{Text: "Memory, Brain, Execution", Sources: []string{r.Input.Blocks[0].Ref}})
	return brain.Result{Content: b, Finish: "stop", Usage: brain.Usage{Known: true, Input: 50, Output: 20}}, nil
}
func demand(ok bool) error {
	if !ok {
		return fmt.Errorf("answer contract failed")
	}
	return nil
}
