package admission_test

import (
	"context"
	"errors"
	"testing"
	"time"

	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"github.com/ruipengliu/lerna/contracts/reasoner"
	"github.com/ruipengliu/lerna/core/durable"
	"github.com/ruipengliu/lerna/core/egress"
	"github.com/ruipengliu/lerna/core/tasks"
	"github.com/ruipengliu/lerna/defaults/scripted"
	"github.com/ruipengliu/lerna/infra/sqlite"
	"google.golang.org/protobuf/proto"
)

type originalClosure struct {
	f            *fixture
	kind         string
	claim        *v1.Job
	actor        *v1.Caller
	completion   *v1.CompletionClosureIntent
	cancellation *v1.CancellationClosureIntent
}

func prepareOriginalClosure(t *testing.T, kind string) *originalClosure {
	t.Helper()
	f := newFixture(t, 100, 80, false)
	a, _ := prepareStart(t, f)
	c := &originalClosure{f: f, kind: kind}
	jobType := "DELIVER_CANCELLATION_CLOSURE"
	if kind == "completion" {
		p := completeProposal(t, f, []*v1.CompletionEvidence{{ConditionId: "created", OperationId: a.OperationId}})
		r, err := f.h.Tasks.BeginCompletion(f.ctx, f.caller, &v1.BeginCompletionCommand{Header: header("delivery-begin"), TaskId: f.task.Name, ProposalRef: p})
		accepted(t, r, err)
		v, err := f.h.Tasks.QueryVerification(f.ctx, f.caller, r.ResultRef)
		if err != nil || len(v.GetClosureIntentRefs()) != 1 {
			t.Fatalf("original completion: %v %v", v, err)
		}
		c.completion, err = f.h.Tasks.QueryCompletionIntent(f.ctx, f.caller, v.ClosureIntentRefs[0])
		if err != nil {
			t.Fatal(err)
		}
		c.actor = &v1.Caller{UserId: "u", IssuerId: "tasks-completion"}
		jobType = "DELIVER_COMPLETION_CLOSURE"
	} else {
		cancelTask(t, f, "delivery-cancel")
		scope, err := f.h.Tasks.QueryCancellation(f.ctx, f.caller, f.task.Name)
		if err != nil || len(scope.GetClosureIntentRefs()) != 1 {
			t.Fatalf("original cancellation: %v %v", scope, err)
		}
		c.cancellation, err = f.h.Tasks.QueryCancellationIntent(f.ctx, f.caller, scope.ClosureIntentRefs[0])
		if err != nil {
			t.Fatal(err)
		}
		c.actor = &v1.Caller{UserId: "u", IssuerId: "tasks-cancellation"}
	}
	r, err := f.h.Durable.ExecuteJob(f.ctx, f.caller, &v1.JobCommand{Identity: header("original-delivery-claim").Identity, ContractVersion: 1, Action: "CLAIM", Module: "tasks", AllowedTypes: []string{jobType}, Limit: 1, LeaseMs: 30000, ProcessInstance: "original-worker"})
	accepted(t, r, err)
	if len(r.Jobs) != 1 {
		t.Fatalf("original closure work: %v", r)
	}
	c.claim = r.Jobs[0]
	return c
}

func (c *originalClosure) process(ctx context.Context, owner *tasks.Service, claim *v1.Job) error {
	if c.kind == "completion" {
		return owner.ProcessCompletionClosureClaim(ctx, claim)
	}
	return owner.ProcessCancellationClosureClaim(ctx, claim)
}

func (c *originalClosure) identity() *v1.CommandIdentity {
	if c.kind == "completion" {
		return c.completion.Command.Header.Identity
	}
	return c.cancellation.Command.Header.Identity
}

func (c *originalClosure) recipientReceipt(t *testing.T) *v1.CommandReceipt {
	t.Helper()
	var r *v1.CommandReceipt
	var err error
	if c.kind == "completion" {
		r, err = c.f.h.Egress.CloseForCompletion(c.f.ctx, c.actor, c.completion.Command)
	} else {
		r, err = c.f.h.Egress.CloseForCancellation(c.f.ctx, c.actor, c.cancellation.Command)
	}
	accepted(t, r, err)
	return r
}

func (c *originalClosure) savedReceipt(t *testing.T) *v1.CommandReceipt {
	t.Helper()
	if c.kind == "completion" {
		intent, err := c.f.h.Tasks.QueryCompletionIntent(c.f.ctx, c.f.caller, c.completion.Ref)
		if err != nil {
			t.Fatal(err)
		}
		return intent.RecipientReceipt
	}
	intent, err := c.f.h.Tasks.QueryCancellationIntent(c.f.ctx, c.f.caller, c.cancellation.Ref)
	if err != nil {
		t.Fatal(err)
	}
	return intent.RecipientReceipt
}

// 接收方 Adapter 只注入查询故障或损坏返回值，原接纳仍由真实出口执行。
type closureReceiptFault struct {
	*egress.Service
	unavailable bool
	mutation    string
}

func (f *closureReceiptFault) QueryReceipt(ctx context.Context, c *v1.Caller, id *v1.CommandIdentity) (*v1.ReceiptQuery, error) {
	if f.unavailable {
		return &v1.ReceiptQuery{State: v1.ReceiptQueryState_RECEIPT_QUERY_STATE_UNAVAILABLE}, nil
	}
	q, err := f.Service.QueryReceipt(ctx, c, id)
	if err != nil || q.Receipt == nil || f.mutation == "" {
		return q, err
	}
	q = proto.Clone(q).(*v1.ReceiptQuery)
	r := q.Receipt
	switch f.mutation {
	case "decision":
		r.Decision = v1.Decision_DECISION_REJECTED
	case "phase":
		r.Phase = v1.ReceiptPhase_RECEIPT_PHASE_SUBMITTED
	case "user":
		r.Identity.UserId = "other-user"
	case "issuer":
		r.Identity.IssuerId = "other-issuer"
	case "target":
		r.Identity.TargetDomainId = "other-domain"
	case "command":
		r.Identity.CommandId = "other-command"
	case "responsible-domain":
		r.ResponsibleDomainId = "other-domain"
	case "fingerprint":
		r.Fingerprint = "other-payload"
	case "result-type":
		r.ResultRef.SchemaId = "lerna.v1.OtherSeal"
	}
	return q, nil
}

// 规则：G3、G4、G11、R7
func TestClosureDeliveryWaitsForOriginalReceiptAndRejectsChangedProof(t *testing.T) {
	for _, kind := range []string{"completion", "cancellation"} {
		t.Run(kind, func(t *testing.T) {
			c := prepareOriginalClosure(t, kind)
			f := c.f
			fault := &closureReceiptFault{Service: f.h.Egress, unavailable: true}
			if kind == "completion" {
				f.h.Tasks.WithCompletionClosures(f.h.Durable, fault)
			} else {
				f.h.Tasks.WithCancellationClosures(f.h.Durable, fault)
			}
			for _, field := range []string{"process", "epoch", "revision"} {
				forged := proto.Clone(c.claim).(*v1.Job)
				switch field {
				case "process":
					forged.ProcessInstance = "other-worker"
				case "epoch":
					forged.ClaimEpoch++
				case "revision":
					forged.Ref.Revision++
				}
				if err := c.process(f.ctx, f.h.Tasks, forged); err == nil || err.Error() != "STALE_CLAIM" {
					t.Fatalf("forged %s claim: %v", field, err)
				}
			}
			if err := c.process(f.ctx, f.h.Tasks, c.claim); err == nil || err.Error() != "DEPENDENCY_UNAVAILABLE" {
				t.Fatalf("receipt unavailability: %v", err)
			}
			q, err := f.h.Egress.QueryReceipt(f.ctx, c.actor, c.identity())
			if err != nil || q.State != v1.ReceiptQueryState_RECEIPT_QUERY_STATE_NOT_FOUND || c.savedReceipt(t) != nil {
				t.Fatalf("unavailable query delivered or acknowledged: %v %v", q, err)
			}
			original := c.recipientReceipt(t)
			fault.unavailable = false
			for _, field := range []string{"decision", "phase", "user", "issuer", "target", "command", "responsible-domain", "fingerprint", "result-type"} {
				fault.mutation = field
				if err = c.process(f.ctx, f.h.Tasks, c.claim); err == nil || err.Error() != "HANDOFF_RECEIPT_INVALID" {
					t.Fatalf("changed %s proof: %v", field, err)
				}
				job, err := f.h.Durable.QueryJob(f.ctx, f.caller, c.claim.Ref.Name)
				if err != nil || !proto.Equal(job, c.claim) || c.savedReceipt(t) != nil {
					t.Fatalf("invalid proof completed source work: %v %v", job, err)
				}
			}
			fault.mutation = ""
			if err = c.process(f.ctx, f.h.Tasks, c.claim); err != nil || !proto.Equal(c.savedReceipt(t), original) {
				t.Fatalf("original receipt not acknowledged: %v", err)
			}
			if f.calls.Load() != 0 {
				t.Fatal("closure delivery caused business I/O")
			}
		})
	}
}

type closureSourceFault struct {
	*sqlite.Store
	fail bool
	kind string
}

var errClosureSourceUnavailable = errors.New("closure source unavailable")

func (s *closureSourceFault) SaveTraceSource(ctx context.Context, producer string, event *v1.TraceEvent) error {
	if s.fail && producer == "tasks" && event.EventType == s.kind {
		return errClosureSourceUnavailable
	}
	return s.Store.SaveTraceSource(ctx, producer, event)
}

func closureOwnerWithStore(t *testing.T, f *fixture, store tasks.Store, work *durable.Service) *tasks.Service {
	t.Helper()
	owner, err := tasks.New(store, "u", "d")
	if err != nil {
		t.Fatal(err)
	}
	owner.WithDecisions(work).WithCancellationJobs(work)
	owner.WithConfirmationRequests(f.h.Sessions, f.h.Content).WithModelContent(f.h.Content).WithReasonerQuestions(f.h.Sessions).WithConditionConfirmations(f.h.Sessions)
	owner.WithStart(f.h.Grants, f.h.Budget, f.h.Ledger).WithCompletion(f.h.Ledger, f.h.Budget)
	owner.WithModelExecution(f.h.Ledger, f.h.LedgerWork, f.h.Grants, f.h.Egress)
	owner.WithReasonerDriver(work, func(*v1.ModelSettings) reasoner.Reasoner { return &scripted.Reasoner{} })
	owner.WithCompletionClosures(work, f.h.Egress).WithCancellationClosures(work, f.h.Egress)
	owner.WithTaskClosures(work, f.h.Egress).WithTaskClosingFacts(f.h.Ledger, f.h.Budget)
	owner.WithClosureSource(f.h.Ledger).WithOperationProgress(f.h.Ledger)
	owner.WithAdmission(f.h.Grants, f.h.Budget, f.h.Content, f.h.Sessions, work, f.h.Ledger).WithHandoffs(work, f.h.Ledger)
	if err = owner.ValidateDependencies(); err != nil {
		t.Fatal(err)
	}
	return owner
}

// 规则：G3、G4、G11、R7
func TestClosureSourceFailureRollsBackReceiptEventAndOriginalJob(t *testing.T) {
	for _, kind := range []string{"completion", "cancellation"} {
		t.Run(kind, func(t *testing.T) {
			c := prepareOriginalClosure(t, kind)
			f := c.f
			original := c.recipientReceipt(t)
			beforeTask, err := f.h.Tasks.QueryTask(f.ctx, f.caller, f.task.Name)
			if err != nil {
				t.Fatal(err)
			}
			beforeSources, err := f.h.Trace.QuerySources(f.ctx, f.caller)
			if err != nil {
				t.Fatal(err)
			}
			store, err := sqlite.Open(f.path, "u", "d")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { store.Close() })
			eventType := "COMPLETION_HANDOFF_CHANGED"
			if kind == "cancellation" {
				eventType = "CANCELLATION_CLOSURE_ACKNOWLEDGED"
			}
			fault := &closureSourceFault{Store: store, fail: true, kind: eventType}
			// 源 Owner 与原 Work 共用这个真实存储实例，保持原事务上下文。
			work, err := durable.New(store, "u", "d")
			if err != nil {
				t.Fatal(err)
			}
			owner := closureOwnerWithStore(t, f, fault, work)
			if err = c.process(f.ctx, owner, c.claim); !errors.Is(err, errClosureSourceUnavailable) {
				t.Fatalf("source failure not propagated: %v", err)
			}
			job, err := f.h.Durable.QueryJob(f.ctx, f.caller, c.claim.Ref.Name)
			if err != nil || !proto.Equal(job, c.claim) || c.savedReceipt(t) != nil {
				t.Fatalf("partial source acknowledgment: %v %v", job, err)
			}
			afterTask, err := f.h.Tasks.QueryTask(f.ctx, f.caller, f.task.Name)
			if err != nil || !proto.Equal(beforeTask, afterTask) {
				t.Fatalf("source failure changed task waiting: %v %v", afterTask, err)
			}
			afterSources, err := f.h.Trace.QuerySources(f.ctx, f.caller)
			if err != nil || len(afterSources) != len(beforeSources) {
				t.Fatalf("partial source event: %v %v", afterSources, err)
			}
			for i, source := range beforeSources {
				if !proto.Equal(source, afterSources[i]) {
					t.Fatal("source failure changed original source")
				}
			}
			fault.fail = false
			if err = c.process(f.ctx, owner, c.claim); err != nil || !proto.Equal(c.savedReceipt(t), original) {
				t.Fatalf("original source retry: %v", err)
			}
			job, err = f.h.Durable.QueryJob(f.ctx, f.caller, c.claim.Ref.Name)
			if err != nil || job.State != "COMPLETED" || !proto.Equal(job.Responsibility, c.claim.Responsibility) || job.ClaimEpoch != c.claim.ClaimEpoch {
				t.Fatalf("original work completion: %v %v", job, err)
			}
			afterSources, err = f.h.Trace.QuerySources(f.ctx, f.caller)
			if err != nil {
				t.Fatal(err)
			}
			acknowledgments := 0
			for _, source := range afterSources {
				event := source.Command.Event
				if event.EventType == eventType && proto.Equal(event.OriginCommand, c.identity()) {
					for _, ref := range event.RelatedRefs {
						if proto.Equal(ref, original.DecisionRef) {
							acknowledgments++
						}
					}
				}
			}
			if acknowledgments != 1 {
				t.Fatalf("original ACK source count: %d", acknowledgments)
			}
			result, err := f.h.Tasks.QueryResult(f.ctx, f.caller, f.task.Name)
			if err != nil || result != nil || f.calls.Load() != 0 {
				t.Fatalf("source ACK fixed Result or sent business I/O: %v %v", result, err)
			}
		})
	}
}

// 规则：G3、G11、R7
func TestClosureRecoveryCancellationPreservesUnexpiredClaim(t *testing.T) {
	for _, kind := range []string{"completion", "cancellation"} {
		t.Run(kind, func(t *testing.T) {
			c := prepareOriginalClosure(t, kind)
			f := c.f
			ctx, cancel := context.WithTimeout(f.ctx, 40*time.Millisecond)
			defer cancel()
			var err error
			if kind == "completion" {
				err = f.h.Tasks.RecoverCompletions(ctx, f.caller)
			} else {
				err = f.h.Tasks.RecoverCancellations(ctx, f.caller)
			}
			// 期限可在等待或原存储读取期间到期；后者保留存储的暂时不可用错误。
			if !errors.Is(ctx.Err(), context.DeadlineExceeded) || err == nil || (!errors.Is(err, context.DeadlineExceeded) && err.Error() != "DEPENDENCY_UNAVAILABLE") {
				t.Fatalf("bounded recovery: %v", err)
			}
			job, err := f.h.Durable.QueryJob(f.ctx, f.caller, c.claim.Ref.Name)
			if err != nil || !proto.Equal(job, c.claim) || c.savedReceipt(t) != nil {
				t.Fatalf("cancelled recovery advanced original claim: %v %v", job, err)
			}
			if err = c.process(f.ctx, f.h.Tasks, c.claim); err != nil || c.savedReceipt(t) == nil || f.calls.Load() != 0 {
				t.Fatalf("original worker cannot continue: %v", err)
			}
		})
	}
}

var _ tasks.Store = (*closureSourceFault)(nil)
var _ tasks.CompletionCloser = (*closureReceiptFault)(nil)
var _ tasks.CancellationCloser = (*closureReceiptFault)(nil)
