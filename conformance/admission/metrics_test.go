package admission_test

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/adapters/interaction"
	"github.com/ruipengliu/lerna/conformance/simulator"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"github.com/ruipengliu/lerna/infra/sqlite"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// 规则：G3、G4、G10、R3、V4
func TestAdmissionMetricsCountActualOriginalDecisionsAndReplaySeparately(t *testing.T) {
	f := newFixture(t, 100, 80, false)
	empty, e := f.h.Tasks.QueryAdmissionMetrics(f.ctx, f.caller)
	if e != nil || empty.Source.Availability != "NO_SAMPLES" || empty.DurationSumNs != nil || empty.Samples != 0 {
		t.Fatalf("empty sample scope: %v %v", empty, e)
	}
	p := f.propose(t, nil)
	command := &v1.AdmitCommand{Header: header("measured-admission"), TaskId: f.task.Name, ProposalRef: p, GrantRef: f.grant}
	r, e := f.h.Tasks.Admit(f.ctx, f.caller, command)
	accepted(t, r, e)
	after, e := f.h.Tasks.QueryAdmissionMetrics(f.ctx, f.caller)
	if e != nil || after.Samples != 1 || after.Accepted != 1 || after.Rejected != 0 || after.GetDurationSumNs() == 0 || after.GetDurationMaxNs() == 0 || after.LastSampleAtUnixMs == nil {
		t.Fatalf("actual decision not measured: %v %v", after, e)
	}
	r, e = f.h.Tasks.Admit(f.ctx, f.caller, command)
	accepted(t, r, e)
	rejected, e := f.h.Tasks.Admit(f.ctx, f.caller, &v1.AdmitCommand{Header: header("measured-refusal"), TaskId: f.task.Name, ProposalRef: p, GrantRef: f.grant})
	if e != nil || rejected.GetDecision() != v1.Decision_DECISION_REJECTED {
		t.Fatalf("original refusal: %v %v", rejected, e)
	}
	r, e = f.h.Tasks.Admit(f.ctx, f.caller, &v1.AdmitCommand{})
	if e == nil || r != nil {
		t.Fatalf("invalid header unexpectedly decided: %v %v", r, e)
	}
	final, e := f.h.Tasks.QueryAdmissionMetrics(f.ctx, f.caller)
	if e != nil || final.Samples != 2 || final.Accepted != 1 || final.Rejected != 1 || final.Replays != 1 || final.InvalidHeaders != 1 || final.Entries != 4 || final.ProcessInstance != after.ProcessInstance || final.Source.Scope != "PROCESS_INSTANCE:TARGET_MODEL_CLOSURE" {
		t.Fatalf("count scope: %v %v", final, e)
	}
	var bucketSamples uint64
	for _, bucket := range final.Buckets {
		bucketSamples += bucket.Count
	}
	if bucketSamples != 2 || f.calls.Load() != 0 {
		t.Fatalf("histogram or target mismatch: buckets=%d target=%d", bucketSamples, f.calls.Load())
	}
}

// 规则：G3、G4、G10、G11、R3、准入-7、准入-8
func TestAdmissionMetricsIncludeActualModelAndClosureDecisions(t *testing.T) {
	t.Run("model", func(t *testing.T) {
		f := modelFixture(t)
		snapshot, e := f.h.Tasks.RequestProposal(f.ctx, f.caller, &v1.RequestProposalCommand{Header: header("metrics-model-request"), TaskId: f.task.Name})
		if e != nil {
			t.Fatal(e)
		}
		claim, e := f.h.Durable.ExecuteJob(f.ctx, f.caller, &v1.JobCommand{Identity: header("metrics-model-claim").Identity, ContractVersion: 1, Action: "CLAIM", AllowedTypes: []string{"PROPOSE"}, Limit: 1, LeaseMs: 30000, ProcessInstance: "metrics-model-worker"})
		accepted(t, claim, e)
		call, e := f.h.Tasks.PrepareModelCall(f.ctx, f.caller, &v1.PrepareModelCallCommand{Header: header("metrics-model-prepare"), RequestRef: snapshot.RequestRef, Claim: claim.Jobs[0], CapabilityRef: f.capability, Settings: &v1.ModelSettings{Provider: "reference", Model: "model-v1", EncoderVersion: "reference-model-v1", PolicyVersion: "m1-v1", ParametersJson: []byte(`{}`), ToolSchemasJson: []byte(`[]`), MaxOutputTokens: 100}, InputRefs: snapshot.ContentRefs})
		if e != nil {
			t.Fatal(e)
		}
		c := &v1.AdmitModelCallCommand{Header: header("metrics-model-admit"), RequestRef: snapshot.RequestRef, Claim: claim.Jobs[0], DescriptorDigest: call.DescriptorDigest, GrantRef: f.grant}
		r, e := f.h.Tasks.AdmitModelCall(f.ctx, f.caller, c)
		accepted(t, r, e)
		r, e = f.h.Tasks.AdmitModelCall(f.ctx, f.caller, c)
		accepted(t, r, e)
		metrics, e := f.h.Tasks.QueryAdmissionMetrics(f.ctx, f.caller)
		if e != nil || metrics.Samples != 1 || metrics.Accepted != 1 || metrics.Replays != 1 || metrics.GetDurationSumNs() == 0 || f.calls.Load() != 0 {
			t.Fatalf("model admission endpoint: %v %v actual calls=%d", metrics, e, f.calls.Load())
		}
	})
	t.Run("closure-query", func(t *testing.T) {
		target := simulator.New("queryable")
		target.SetBehavior("drop-after-apply")
		f := newFixtureWithTarget(t, 200, 200, false, target)
		capability, grant := configureReconciliation(t, f)
		admission, start := prepareStart(t, f)
		r, e := f.h.Egress.Invoke(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, start)
		accepted(t, r, e)
		target.SetBehavior("")
		r, e = f.h.Ledger.RequestReconciliation(f.ctx, f.caller, reconciliationCommand(f, admission, capability, grant))
		accepted(t, r, e)
		if e = f.h.Ledger.ProcessReconciliations(f.ctx, f.caller); e != nil {
			t.Fatal(e)
		}
		plan, e := f.h.Ledger.QueryReconciliation(f.ctx, f.caller, admission.OperationId)
		if e != nil || plan.State != "COMPLETED" || len(plan.QueryRefs) != 1 {
			t.Fatalf("actual query: %v %v", plan, e)
		}
		query, e := f.h.Ledger.QueryReconciliationQuery(f.ctx, f.caller, plan.QueryRefs[0])
		if e != nil {
			t.Fatal(e)
		}
		r, e = f.h.Tasks.AdmitClosure(f.ctx, &v1.Caller{UserId: "u", IssuerId: query.AdmissionCommand.Header.Identity.IssuerId}, query.AdmissionCommand)
		accepted(t, r, e)
		metrics, e := f.h.Tasks.QueryAdmissionMetrics(f.ctx, f.caller)
		requests, effects := target.Snapshot()
		if e != nil || metrics.Samples != 2 || metrics.Accepted != 2 || metrics.Replays != 1 || len(requests) != 2 || requests[0].Method != "POST" || requests[1].Method != "GET" || len(effects) != 1 {
			t.Fatalf("target and closure endpoints: %v %v requests=%v effects=%v", metrics, e, requests, effects)
		}
	})
}

// 规则：G1、G2、G3、G11、R3、V4
func TestLedgerMetricsReadOriginalUnknownAndAllReconciliationStates(t *testing.T) {
	target := simulator.New("queryable")
	target.SetBehavior("drop-after-apply")
	f := newFixtureWithTarget(t, 200, 200, false, target)
	capability, grant := configureReconciliation(t, f)
	a, start := prepareStart(t, f)
	r, e := f.h.Egress.Invoke(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, start)
	accepted(t, r, e)
	initial, e := f.h.Ledger.QueryMetrics(f.ctx, f.caller)
	if e != nil || initial.Unknown.Total != 1 || initial.Unknown.Queryable != 1 || initial.Unknown.Unqueryable != 0 || initial.Unknown.AgeSamples != 1 || initial.Unknown.OldestAgeMs == nil || initial.Reconciliation.UnknownUncovered != 1 || initial.Source.OwnerDomainId != "d/ledger" || initial.Source.Scope != "ALL_OPERATIONS_INCLUDING_CLOSED_TASKS" {
		t.Fatalf("original unknown snapshot: %v %v", initial, e)
	}
	request := reconciliationCommand(f, a, capability, grant)
	r, e = f.h.Ledger.RequestReconciliation(f.ctx, f.caller, request)
	accepted(t, r, e)
	ready, e := f.h.Ledger.QueryMetrics(f.ctx, f.caller)
	if e != nil || ready.Reconciliation.Total != 1 || ready.Reconciliation.Active != 1 || ready.Reconciliation.Ready != 1 || ready.Reconciliation.Unfinished != 1 || ready.Reconciliation.UnknownCovered != 1 || ready.Reconciliation.UnknownUncovered != 0 {
		t.Fatalf("complete unfinished denominator: %v %v", ready, e)
	}
	plan, e := f.h.Ledger.QueryReconciliation(f.ctx, f.caller, a.OperationId)
	if e != nil {
		t.Fatal(e)
	}
	r, e = f.h.Ledger.ControlReconciliation(f.ctx, f.caller, &v1.ControlReconciliationCommand{Header: ledgerHeader("metrics-pause"), OperationId: a.OperationId, ExpectedRevision: plan.Ref.Revision, Action: "PAUSE"})
	accepted(t, r, e)
	paused, e := f.h.Ledger.QueryMetrics(f.ctx, f.caller)
	if e != nil || paused.Reconciliation.Paused != 1 || paused.Reconciliation.Active != 0 || paused.Reconciliation.Unfinished != 1 || paused.Reconciliation.Completed != 0 {
		t.Fatalf("paused denominator: %v %v", paused, e)
	}
	again, e := f.h.Ledger.QueryMetrics(f.ctx, f.caller)
	if e != nil || again.Source.GetSourceRevision() != paused.Source.GetSourceRevision() {
		t.Fatalf("metric read advanced owner version: %v %v", again, e)
	}
	plan, e = f.h.Ledger.QueryReconciliation(f.ctx, f.caller, a.OperationId)
	if e != nil {
		t.Fatal(e)
	}
	r, e = f.h.Ledger.ControlReconciliation(f.ctx, f.caller, &v1.ControlReconciliationCommand{Header: ledgerHeader("metrics-resume"), OperationId: a.OperationId, ExpectedRevision: plan.Ref.Revision, Action: "RESUME"})
	accepted(t, r, e)
	target.SetBehavior("")
	if e = f.h.Ledger.ProcessReconciliations(f.ctx, f.caller); e != nil {
		t.Fatal(e)
	}
	completed, e := f.h.Ledger.QueryMetrics(f.ctx, f.caller)
	requests, effects := target.Snapshot()
	if e != nil || completed.Unknown.Total != 0 || completed.Reconciliation.Total != 1 || completed.Reconciliation.Completed != 1 || completed.Reconciliation.Unfinished != 0 || completed.Source.GetSourceRevision() <= ready.Source.GetSourceRevision() || len(requests) != 2 || len(effects) != 1 {
		t.Fatalf("actual completion snapshot: %v %v requests=%v effects=%v", completed, e, requests, effects)
	}
}

// 规则：G1、G2、G3、G11、R3、V4
func TestTraceMetricsSeparateOriginalSourceAcceptanceIndexAndAcknowledgement(t *testing.T) {
	f := newFixture(t, 100, 80, false)
	sources, e := f.h.Trace.QuerySources(f.ctx, f.caller)
	if e != nil || len(sources) == 0 {
		t.Fatalf("actual sources: %v %v", sources, e)
	}
	initial, e := f.h.Trace.QueryMetrics(f.ctx, f.caller)
	if e != nil || len(initial.Streams) != 6 || initial.Produced != uint64(len(sources)) || initial.Accepted != 0 || initial.PendingAcknowledgements != uint64(len(sources)) || initial.ReceiverBacklog != uint64(len(sources)) || initial.CoverageComplete || initial.Diagnostics.Source.Availability != "DISABLED" || initial.Diagnostics.Dropped != nil || initial.Diagnostics.StalenessMs != nil {
		t.Fatalf("source is not accepted evidence: %v %v", initial, e)
	}
	c := sources[0].Command
	r, e := f.h.Trace.Accept(f.ctx, &v1.Caller{UserId: "u", IssuerId: c.Header.Identity.IssuerId}, c)
	accepted(t, r, e)
	acceptedOnly, e := f.h.Trace.QueryMetrics(f.ctx, f.caller)
	if e != nil || acceptedOnly.Accepted != 1 || acceptedOnly.Indexed != 0 || acceptedOnly.PendingAcknowledgements != uint64(len(sources)) || acceptedOnly.IndexBacklog != 1 || acceptedOnly.CoverageComplete {
		t.Fatalf("receiver acceptance is not source ACK or index: %v %v", acceptedOnly, e)
	}
	if e = f.h.Trace.Collect(f.ctx, f.caller); e != nil {
		t.Fatal(e)
	}
	collected, e := f.h.Trace.QueryMetrics(f.ctx, f.caller)
	if e != nil || collected.Accepted != uint64(len(sources)) || collected.PendingAcknowledgements != 0 || collected.IndexBacklog != uint64(len(sources)) || collected.CoverageComplete {
		t.Fatalf("source ACK is not index: %v %v", collected, e)
	}
	if e = f.h.Trace.Index(f.ctx, f.caller); e != nil {
		t.Fatal(e)
	}
	indexed, e := f.h.Trace.QueryMetrics(f.ctx, f.caller)
	if e != nil || !indexed.CoverageComplete || indexed.Indexed != uint64(len(sources)) || indexed.IndexBacklog != 0 || indexed.ReceiverBacklog != 0 || f.calls.Load() != 0 {
		t.Fatalf("actual indexed coverage: %v %v calls=%d", indexed, e, f.calls.Load())
	}
}

// 规则：G1、G2、G3、G11、R3、R6、V4
func TestLocalMetricCLIReadsOwnersWithoutBusinessAdvance(t *testing.T) {
	f := newFixture(t, 100, 80, false)
	p := f.propose(t, nil)
	r, e := f.h.Tasks.Admit(f.ctx, f.caller, &v1.AdmitCommand{Header: header("metric-cli-original"), TaskId: f.task.Name, ProposalRef: p, GrantRef: f.grant})
	accepted(t, r, e)
	before, e := f.h.Trace.QuerySources(f.ctx, f.caller)
	if e != nil {
		t.Fatal(e)
	}
	var out bytes.Buffer
	cli := interaction.CLI{Metrics: f.h, Caller: f.caller, Domain: "d"}
	if e = cli.Run(f.ctx, []string{"metrics"}, &out); e != nil {
		t.Fatal(e)
	}
	metrics := new(v1.LocalMetrics)
	if e = protojson.Unmarshal(out.Bytes(), metrics); e != nil || metrics.CrossDomainAtomic || metrics.Admission.Samples != 1 || metrics.Ledger == nil || metrics.Trace.Produced != uint64(len(before)) || metrics.Trace.CoverageComplete || len(metrics.UnavailableSources) != 0 {
		t.Fatalf("thin metric CLI: %v %v output=%s", metrics, e, out.String())
	}
	after, e := f.h.Trace.QuerySources(f.ctx, f.caller)
	if e != nil || len(after) != len(before) {
		t.Fatalf("source inventory changed: %v %v", after, e)
	}
	for i := range before {
		if !proto.Equal(before[i], after[i]) {
			t.Fatal("metric query advanced source responsibility")
		}
	}
	q, e := f.h.Durable.QueryReceipt(f.ctx, f.caller, r.Identity)
	if e != nil || !proto.Equal(q.Receipt, r) || f.calls.Load() != 0 {
		t.Fatalf("metric query changed decision or target: %v %v target=%d", q, e, f.calls.Load())
	}
}

// 规则：G3、G4、G10、R3、V4
func TestAdmissionMetricIncludesActualStorageQueueBeforeDurableDecision(t *testing.T) {
	f := newFixture(t, 100, 80, false)
	p := f.propose(t, nil)
	writer, e := sqlite.Open(f.path, "u", "d")
	if e != nil {
		t.Fatal(e)
	}
	defer writer.Close()
	entered, release := make(chan struct{}), make(chan struct{})
	locked := make(chan error, 1)
	// 真实独立连接持有写调度位，不制造任何业务记录。
	go func() {
		locked <- writer.Transaction(context.Background(), "tasks.planning", func(context.Context) error {
			close(entered)
			<-release
			return nil
		})
	}()
	select {
	case <-entered:
	case e := <-locked:
		t.Fatalf("writer lock: %v", e)
	case <-time.After(5 * time.Second):
		t.Fatal("writer lock timeout")
	}
	done := make(chan error, 1)
	go func() {
		r, e := f.h.Tasks.Admit(f.ctx, f.caller, &v1.AdmitCommand{Header: header("metrics-queued-admission"), TaskId: f.task.Name, ProposalRef: p, GrantRef: f.grant})
		if e == nil && r.GetDecision() != v1.Decision_DECISION_ACCEPTED {
			e = fmt.Errorf("queued admission rejected: %v", r)
		}
		done <- e
	}()
	deadline := time.Now().Add(5 * time.Second)
	for {
		metrics, e := f.h.Tasks.QueryAdmissionMetrics(f.ctx, f.caller)
		if e != nil {
			close(release)
			t.Fatal(e)
		}
		if metrics.Entries == 1 && metrics.Samples == 0 {
			break
		}
		if time.Now().After(deadline) {
			close(release)
			t.Fatal("queued command entry was not observed")
		}
		time.Sleep(time.Millisecond)
	}
	heldAt := time.Now()
	time.Sleep(20 * time.Millisecond)
	held := time.Since(heldAt)
	close(release)
	if e = <-locked; e != nil {
		t.Fatal(e)
	}
	if e = <-done; e != nil {
		t.Fatal(e)
	}
	metrics, e := f.h.Tasks.QueryAdmissionMetrics(f.ctx, f.caller)
	if e != nil || metrics.Samples != 1 || metrics.GetDurationSumNs() < uint64(held) || f.calls.Load() != 0 {
		t.Fatalf("storage queue excluded from original monotonic duration: %v %v held=%s target=%d", metrics, e, held, f.calls.Load())
	}
}

// 规则：G1、G2、G3、G11、R3、V4
func TestLedgerMetricsRetainInProgressAndWaitingInUnfinishedDenominator(t *testing.T) {
	target := simulator.New("queryable")
	target.SetBehavior("accept-and-delay")
	target.SetQueryRetryAfter(60_000)
	entered, release := make(chan struct{}), make(chan struct{})
	defer close(release)
	f := newFixtureWithTarget(t, 200, 200, false, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			close(entered)
			<-release
		}
		target.ServeHTTP(w, r)
	}))
	capability, grant := configureReconciliation(t, f)
	a, start := prepareStart(t, f)
	r, e := f.h.Egress.Invoke(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, start)
	accepted(t, r, e)
	r, e = f.h.Ledger.RequestReconciliation(f.ctx, f.caller, reconciliationCommand(f, a, capability, grant))
	accepted(t, r, e)
	ctx, cancel := context.WithTimeout(f.ctx, 5*time.Second)
	defer cancel()
	finished := make(chan error, 1)
	go func() { finished <- f.h.Ledger.ProcessReconciliations(ctx, f.caller) }()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("actual query did not reach independent target")
	}
	inProgress, e := f.h.Ledger.QueryMetrics(f.ctx, f.caller)
	if e != nil || inProgress.Reconciliation.InProgress != 1 || inProgress.Reconciliation.Active != 1 || inProgress.Reconciliation.Unfinished != 1 || inProgress.Reconciliation.Completed != 0 || inProgress.UnknownBusinessOperations != 1 || inProgress.UnknownQueryOperations != 1 {
		t.Fatalf("actual in-flight query omitted from denominator %v %v", inProgress, e)
	}
	release <- struct{}{}
	if e = <-finished; e != nil {
		t.Fatal(e)
	}
	waiting, e := f.h.Ledger.QueryMetrics(f.ctx, f.caller)
	if e != nil || waiting.Reconciliation.Total != 1 || waiting.Reconciliation.Waiting != 1 || waiting.Reconciliation.Active != 0 || waiting.Reconciliation.Completed != 0 || waiting.Reconciliation.Unfinished != 1 || waiting.Unknown.Total != 1 || waiting.UnknownBusinessOperations != 1 || waiting.Reconciliation.UnknownCovered != 1 || waiting.Reconciliation.DurationAvailability != "MISSING_ORIGINAL_UNKNOWN_AND_COMPLETION_ENDPOINTS" {
		t.Fatalf("waiting plan disappeared or became completed latency %v %v", waiting, e)
	}
	requests, effects := target.Snapshot()
	if len(requests) != 2 || requests[0].Method != "POST" || requests[1].Method != "GET" || len(effects) != 0 {
		t.Fatalf("metric observation changed actual target %v %v", requests, effects)
	}
}

// 规则：G3、G11、R3、R7、V4
func TestTraceMetricsExposeActualOutOfOrderAcceptanceAndIndexGaps(t *testing.T) {
	f := newFixture(t, 100, 80, false)
	sources, e := f.h.Trace.QuerySources(f.ctx, f.caller)
	if e != nil {
		t.Fatal(e)
	}
	var later *v1.TraceSourceRecord
	for _, source := range sources {
		if source.Command.Event.SourceSeq > 1 {
			later = source
			break
		}
	}
	if later == nil {
		t.Fatal("public producer has no actual multi-event source")
	}
	r, e := f.h.Trace.Accept(f.ctx, &v1.Caller{UserId: "u", IssuerId: later.Command.Header.Identity.IssuerId}, later.Command)
	accepted(t, r, e)
	if e = f.h.Trace.Index(f.ctx, f.caller); e != nil {
		t.Fatal(e)
	}
	metrics, e := f.h.Trace.QueryMetrics(f.ctx, f.caller)
	if e != nil || metrics.Accepted != 1 || metrics.Indexed != 1 || metrics.CoverageComplete || metrics.PendingAcknowledgements != uint64(len(sources)) {
		t.Fatalf("out-of-order index falsely complete %v %v", metrics, e)
	}
	var stream *v1.TraceStreamMetrics
	for _, item := range metrics.Streams {
		if item.Progress.SourceStreamId == later.Command.Event.SourceStreamId {
			stream = item
		}
	}
	if stream == nil || stream.Progress.AcceptedContiguous != 0 || stream.Progress.IndexedContiguous != 0 || len(stream.Progress.Gaps) == 0 || stream.Progress.Gaps[0].First != 1 || stream.Progress.Gaps[0].Last != later.Command.Event.SourceSeq-1 || stream.Accepted != 1 || stream.Indexed != 1 || stream.Acknowledged != 0 || stream.Progress.Backlog == 0 {
		t.Fatalf("actual gap/source ACK lost %v", stream)
	}
	if e = f.h.Trace.Collect(f.ctx, f.caller); e != nil {
		t.Fatal(e)
	}
	if e = f.h.Trace.Index(f.ctx, f.caller); e != nil {
		t.Fatal(e)
	}
	complete, e := f.h.Trace.QueryMetrics(f.ctx, f.caller)
	if e != nil || !complete.CoverageComplete || complete.Accepted != uint64(len(sources)) || complete.Indexed != uint64(len(sources)) || complete.PendingAcknowledgements != 0 || f.calls.Load() != 0 {
		t.Fatalf("actual source recovery metrics %v %v", complete, e)
	}
}
