package task_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/governance"
	"github.com/ruipengliu/lerna/internal/task"
	"github.com/ruipengliu/lerna/runtime"
)

func fixtureRule() api.RuleDefinition {
	ref := api.ComponentRef{ComponentID: api.NewID("rule"), Version: "1.0.0", Digest: api.Hash([]byte("registered exact rule"))}
	age := uint64(600)
	return api.RuleDefinition{RuleRef: ref, Kind: "effect", ParametersSchemaRef: ref, Predicate: "current_state", AllowedBasis: []string{"verified"}, RequiredEvidenceSchemaRef: ref, ScopeSchemaRef: ref, MaxObservationAgeSeconds: &age, RiskClass: "ordinary", ApplicabilityPolicyRef: ref}
}
func candidate(h *harness, t api.Task, r api.RuleDefinition, key string) api.RequirementCandidate {
	return api.RequirementCandidate{CandidateKey: key, Kind: r.Kind, StatementRef: h.content("exact task criterion"), SourceRefs: []api.SourceEvidence{{ContentRef: t.GoalRef, SourceKind: "user_input", Locator: "text:0:10"}}, Origin: "explicit_user", RuleRef: r.RuleRef, Required: true, OpenQuestions: []string{}}
}
func TestSemanticUpsertKeepsUnmentionedHardRequirementsAndOriginalIdentity(t *testing.T) {
	rule := fixtureRule()
	h := newHarness(t, task.Ports{}, rule)
	current := h.submit(t)
	c := candidate(h, current, rule, "first")
	first, err := h.service.AdoptRequirements(context.Background(), h.store, h.scope, h.trusted(), current.TaskID, "command", h.scope.Ref(api.NewID("command"), 1), api.RequirementDelta{BaseGoalRevision: 1, Candidates: []api.RequirementCandidate{c}, ReasonRef: current.GoalRef}, task.ValidationReport{Valid: true, ReportRef: h.content("independent validation fixture"), SemanticKeys: []string{"effect:required:source:exact-rule-parameters"}})
	if err != nil {
		t.Fatal(err)
	}
	current, err = h.service.Read(context.Background(), h.store, h.scope, h.auth, current.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	c.CandidateKey = "different-key"
	c.StatementRef = h.content("equivalent wording proven by registered rule")
	second, err := h.service.AdoptRequirements(context.Background(), h.store, h.scope, h.trusted(), current.TaskID, "command", h.scope.Ref(api.NewID("command"), 1), api.RequirementDelta{BaseGoalRevision: current.GoalRevision, Candidates: []api.RequirementCandidate{c}, ReasonRef: current.GoalRef}, task.ValidationReport{Valid: true, ReportRef: h.content("equivalence proof fixture"), SemanticKeys: []string{"effect:required:source:exact-rule-parameters"}})
	if err != nil {
		t.Fatal(err)
	}
	if second.Outcome != "unchanged" || second.Mappings[0].RequirementID != first.Mappings[0].RequirementID || second.Mappings[0].Revision != 1 {
		t.Fatalf("semantic equality changed identity: %+v", second)
	}
	after, err := h.service.Read(context.Background(), h.store, h.scope, h.auth, current.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	if after.GoalRevision != 2 || len(after.Requirements) != 1 || after.Requirements[0].AdoptionID != first.AdoptionID {
		t.Fatalf("unchanged definition mutated: %+v", after)
	}
	c.CandidateKey = "additional"
	c.StatementRef = h.content("different exact criterion")
	_, err = h.service.AdoptRequirements(context.Background(), h.store, h.scope, h.trusted(), current.TaskID, "command", h.scope.Ref(api.NewID("command"), 1), api.RequirementDelta{BaseGoalRevision: after.GoalRevision, Candidates: []api.RequirementCandidate{c}, ReasonRef: after.GoalRef}, task.ValidationReport{Valid: true, ReportRef: h.content("additional proof"), SemanticKeys: []string{"effect:another-exact-parameter"}})
	if err != nil {
		t.Fatal(err)
	}
	after, err = h.service.Read(context.Background(), h.store, h.scope, h.auth, current.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	if after.GoalRevision != 3 || len(after.Requirements) != 2 || after.Requirements[0].RequirementID != first.Mappings[0].RequirementID {
		t.Fatalf("incremental candidate removed hard requirement: %+v", after)
	}
}
func TestEmptyRequirementSetCannotCompleteAndCancelDoesNotInventResult(t *testing.T) {
	h := newHarness(t, task.Ports{})
	current := h.submit(t)
	_, err := h.service.Complete(context.Background(), h.store, h.scope, h.trusted(), task.CompleteInput{TaskID: current.TaskID, ExpectedGoalRevision: current.GoalRevision, ArtifactRefs: []api.ContentRef{h.content("artifact")}})
	if !api.IsCode(err, "invalid_state") {
		t.Fatalf("empty condition result: %v", err)
	}
	receipt, err := h.dispatch.Command(context.Background(), h.auth, api.Raw(h.command("task.cancel", current.TaskID, &current.Revision, task.ControlInput{TaskID: current.TaskID, Reason: "stop"})))
	if err != nil || receipt.Stage != "applied" {
		t.Fatalf("cancel %+v %v", receipt, err)
	}
	_, err = h.query("task.result", current.TaskID, task.ResultInput{})
	if !api.IsCode(err, "invalid_state") {
		t.Fatalf("cancel fabricated Result: %v", err)
	}
}
func TestBillingTracksCumulativeDeltasAndLateCorrectionAfterCancellation(t *testing.T) {
	h := newHarness(t, task.Ports{})
	current := h.submit(t)
	prepared := h.prepared(current, "10")
	if _, err := h.service.PrepareDecision(context.Background(), h.store, h.scope, h.trusted(), prepared); err != nil {
		t.Fatal(err)
	}
	source := h.scope.Ref(prepared.DecisionID, 1)
	usage := api.UsageSnapshot{SourceRef: source, UsageRevision: 1, Cumulative: []api.Amount{{Unit: "USD", Value: "6"}}, SpendingClosed: false, UsageFinal: false, ProofRefs: []api.ContentRef{h.content("bill 6")}}
	apply := func(rev uint64, value string, closed bool) {
		t.Helper()
		usage.UsageRevision = rev
		usage.SourceRef.Revision = rev
		usage.Cumulative[0].Value = value
		usage.SpendingClosed = closed
		usage.UsageFinal = closed
		usage.UsageDigest, _ = task.UsageDigest(usage)
		if _, err := h.service.ReconcileUsage(context.Background(), h.store, h.scope, h.trusted(), "brain_decision", usage); err != nil {
			t.Fatal(err)
		}
	}
	apply(1, "6", false)
	apply(1, "6", false)
	b, err := h.service.BudgetRead(context.Background(), h.store, h.scope, h.auth, current.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	if b.Budget[0].Spent != "6" || b.Budget[0].Reserved != "4" {
		t.Fatalf("non-final cumulative6: %+v", b)
	}
	current, err = h.service.Read(context.Background(), h.store, h.scope, h.auth, current.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := h.dispatch.Command(context.Background(), h.auth, api.Raw(h.command("task.cancel", current.TaskID, &current.Revision, task.ControlInput{TaskID: current.TaskID, Reason: "cancel while bill open"})))
	if err != nil || receipt.Stage != "applied" {
		t.Fatalf("cancel %+v %v", receipt, err)
	}
	apply(2, "8", true)
	apply(3, "9", true)
	b, err = h.service.BudgetRead(context.Background(), h.store, h.scope, h.auth, current.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	if b.Budget[0].Spent != "9" || b.Budget[0].Reserved != "0" || b.AccountingOpen {
		t.Fatalf("late delta not one: %+v", b)
	}
	after, err := h.service.Read(context.Background(), h.store, h.scope, h.auth, current.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Status != "cancelled" {
		t.Fatal("late bill reopened Task")
	}
	usage.Cumulative[0].Value = "10"
	usage.UsageDigest, _ = task.UsageDigest(usage)
	if _, err = h.service.ReconcileUsage(context.Background(), h.store, h.scope, h.trusted(), "brain_decision", usage); !api.IsCode(err, "idempotency_conflict") {
		t.Fatalf("same revision/different bill accepted: %v", err)
	}
}

// 宿主桥使用真实治理原库。报告/准入已由fixture的准确版本预置；不声称文本质量已取证。
type evidenceBridge struct {
	service *governance.Service
	rules   map[string]api.RuleDefinition
}

func (b *evidenceBridge) Authorize(ctx context.Context, tx runtime.Tx, auth runtime.Auth, purpose string, refs []api.ContentRef, objects []api.ObjectRef) error {
	for _, ref := range refs {
		if ref.TenantID != tx.Scope().TenantID || ref.OwnerID != tx.Scope().OwnerID {
			return api.E("forbidden", "content_scope_mismatch")
		}
	}
	return nil
}
func (b *evidenceBridge) Evidence(ctx context.Context, tx runtime.Tx, t api.Task, refs []api.ObjectRef, implementations []api.ComponentRef) error {
	list := []governance.CheckReference{}
	for _, ref := range refs {
		list = append(list, governance.CheckReference{CheckRef: ref})
	}
	_, err := b.service.CheckEvidenceTx(ctx, tx, governance.EvidenceCompletion{ConsumerTaskRef: tx.Scope().Ref(t.TaskID, t.Revision), Checks: list, MaxStalenessSeconds: 300})
	return err
}
func (b *evidenceBridge) RegisterCoverage(ctx context.Context, tx runtime.Tx, t api.Task, c api.GoalCoverage) error {
	rule := governance.RuleDefinition{ComponentRef: c.RuleRef, Kind: "effect", Predicate: "current_state", AllowedBasis: []string{"verified"}, RiskClass: "ordinary", MaxObservationAgeSeconds: 600}
	if _, err := b.service.RegisterRuleTx(ctx, tx, rule); err != nil {
		return err
	}
	_, err := b.service.RegisterCheckTx(ctx, tx, governance.ConditionCheck{CheckID: c.CoverageID, Revision: 1, AuthorityID: tx.Scope().OwnerID, TaskID: t.TaskID, GoalRevision: c.GoalRevision, RequirementID: api.NewID("requirement"), RequirementRevision: 1, ArtifactRef: c.GoalRef, ScopeRef: c.MappingReportRef, ObservedAt: c.CheckedAt, CheckedAt: c.CheckedAt, RuleRef: c.RuleRef, EvaluatorRef: c.EvaluatorRef, ConfigRef: c.RuleRef, InstallLockRef: c.RuleRef, Verdict: c.Verdict, Basis: "verified", ReportRef: c.MappingReportRef, EvidenceRefs: []api.ContentRef{c.MappingReportRef}, DependentCheckRefs: []api.ObjectRef{}})
	return err
}
func (b *evidenceBridge) RegisterCheck(ctx context.Context, tx runtime.Tx, t api.Task, c api.ConditionResult) error {
	r := b.rules[c.RuleRef.ComponentID]
	rule := governance.RuleDefinition{ComponentRef: c.RuleRef, Kind: r.Kind, Predicate: r.Predicate, AllowedBasis: r.AllowedBasis, AllowUserAcceptance: r.AllowUserAcceptance, RiskClass: r.RiskClass, MaxObservationAgeSeconds: 600}
	if _, err := b.service.RegisterRuleTx(ctx, tx, rule); err != nil {
		return err
	}
	_, err := b.service.RegisterCheckTx(ctx, tx, governance.ConditionCheck{CheckID: c.CheckID, Revision: 1, AuthorityID: tx.Scope().OwnerID, TaskID: t.TaskID, GoalRevision: c.GoalRevision, RequirementID: c.RequirementID, RequirementRevision: c.RequirementRevision, ArtifactRef: c.ArtifactRef, ScopeRef: c.ScopeRef, ObservedAt: c.ObservedAt, CheckedAt: c.CheckedAt, RuleRef: c.RuleRef, EvaluatorRef: c.EvaluatorRef, ConfigRef: c.RuleRef, InstallLockRef: c.RuleRef, Verdict: c.Verdict, Basis: c.Basis, ReportRef: c.EvidenceRefs[0], EvidenceRefs: c.EvidenceRefs, DependentCheckRefs: []api.ObjectRef{}})
	return err
}
func (b *evidenceBridge) BindResult(ctx context.Context, tx runtime.Tx, t api.Task, result api.Result, refs []api.ObjectRef) error {
	list := []governance.CheckReference{}
	for _, ref := range refs {
		list = append(list, governance.CheckReference{CheckRef: ref})
	}
	resultRef := tx.Scope().Ref(result.ResultID, 1)
	_, err := b.service.CheckEvidenceTx(ctx, tx, governance.EvidenceCompletion{ConsumerTaskRef: tx.Scope().Ref(t.TaskID, t.Revision), Checks: list, MaxStalenessSeconds: 300, ResultRef: &resultRef})
	return err
}
func readyTask(t *testing.T, h *harness, rule api.RuleDefinition) api.Task {
	t.Helper()
	current := h.submit(t)
	_, err := h.service.AdoptRequirements(context.Background(), h.store, h.scope, h.trusted(), current.TaskID, "command", h.scope.Ref(api.NewID("command"), 1), api.RequirementDelta{BaseGoalRevision: 1, Candidates: []api.RequirementCandidate{candidate(h, current, rule, "exact")}, ReasonRef: current.GoalRef}, task.ValidationReport{Valid: true, ReportRef: h.content("full original mapping validation"), SemanticKeys: []string{"effect:required:exact-rule"}})
	if err != nil {
		t.Fatal(err)
	}
	current, err = h.service.Read(context.Background(), h.store, h.scope, h.auth, current.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = h.service.StoreCoverage(context.Background(), h.store, h.scope, h.trusted(), api.GoalCoverage{CoverageID: api.NewID("coverage"), Revision: 1, TaskRef: h.scope.Ref(current.TaskID, current.Revision), GoalRevision: current.GoalRevision, GoalRef: current.GoalRef, RequirementsDigest: current.RequirementsDigest, MappingReportRef: h.content("full-goal coverage fixture"), RuleRef: rule.RuleRef, EvaluatorRef: rule.RuleRef, Verdict: "pass", Applicability: "usable", CheckedAt: api.Time(time.Now())})
	if err != nil {
		t.Fatal(err)
	}
	current, err = h.service.Read(context.Background(), h.store, h.scope, h.auth, current.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	return current
}
func TestCurrentFullCoverageAndChecksCreateImmutableResultBeforePublication(t *testing.T) {
	rule := fixtureRule()
	bridge := &evidenceBridge{rules: map[string]api.RuleDefinition{rule.RuleRef.ComponentID: rule}}
	h := newHarness(t, task.Ports{Gate: bridge}, rule)
	bridge.service = governance.New(h.store, governance.Options{})
	current := readyTask(t, h, rule)
	artifact := h.content("independently verified artifact fixture")
	now := api.Time(time.Now())
	check := api.ConditionResult{CheckID: api.NewID("check"), TaskID: current.TaskID, GoalRevision: current.GoalRevision, RequirementID: current.Requirements[0].RequirementID, RequirementRevision: 1, ArtifactRef: artifact, RuleRef: rule.RuleRef, EvaluatorRef: rule.RuleRef, Verdict: "pass", Applicability: "usable", Basis: "verified", EvidenceRefs: []api.ContentRef{h.content("independent readback fixture")}, ScopeRef: h.content("exact path/hash/observation scope fixture"), ObservedAt: now, CheckedAt: now}
	if _, err := h.service.RecordCheck(context.Background(), h.store, h.scope, h.trusted(), check); err != nil {
		t.Fatal(err)
	}
	result, err := h.service.Complete(context.Background(), h.store, h.scope, h.trusted(), task.CompleteInput{TaskID: current.TaskID, ExpectedGoalRevision: current.GoalRevision, ArtifactRefs: []api.ContentRef{artifact}})
	if err != nil {
		t.Fatal(err)
	}
	b, err := h.query("task.result", current.TaskID, task.ResultInput{})
	if err != nil {
		t.Fatal(err)
	}
	var out task.ResultOutput
	if err = api.Decode(b, &out); err != nil {
		t.Fatal(err)
	}
	if out.Publication != "pending" || out.Result.ResultID != result.ResultID || out.Result.CompletionBasis != "verified" {
		t.Fatalf("completion depended on remote copy: %+v", out)
	}
	before := api.Raw(out.Result)
	if err = runtime.Drain(context.Background(), h.store, h.scope, h.dispatch.Registry, 50); err != nil && !strings.Contains(err.Error(), "drain_limit") {
		t.Fatal(err)
	}
	again, err := h.service.Result(context.Background(), h.store, h.scope, h.auth, current.TaskID, task.ResultInput{})
	if err != nil {
		t.Fatal(err)
	}
	if !api.Equal(before, api.Raw(again.Result)) {
		t.Fatal("pending publisher rewrote authoritative Result")
	}
	if err = bridge.service.Register(h.dispatch.Registry); err != nil {
		t.Fatal(err)
	}
	maintainer := h.auth
	maintainer.Roles = []string{"maintainer", "evidence_consumer"}
	defectID := api.NewID("defect")
	r, err := h.dispatch.Command(context.Background(), maintainer, api.Raw(h.command("evidence.defect.register", defectID, nil, governance.DefectRegister{DefectID: defectID, RuleRef: rule.RuleRef, EvaluatorRef: rule.RuleRef, ScopeRef: check.ScopeRef, EvidenceRef: h.content("late real defect registry fixture")})))
	if err != nil || r.Stage != "applied" {
		t.Fatalf("late defect %+v %v", r, err)
	}
	drainKind(t, h, "governance.defect")
	pageBytes, err := h.dispatch.Query(context.Background(), maintainer, api.Raw(api.Query{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: h.scope.OwnerID, QueryID: api.NewID("query"), Method: "evidence.notice.list", TargetID: h.scope.OwnerID, Payload: api.Raw(api.ListInput{Limit: 100})}))
	if err != nil {
		t.Fatal(err)
	}
	var notices api.Page[governance.ResultNotice]
	if err = api.Decode(pageBytes, &notices); err != nil || len(notices.Items) != 1 {
		t.Fatalf("original governance holder notice missing %s %v", pageBytes, err)
	}
	notice := notices.Items[0]
	transfer := task.ResultNotice{NoticeRef: h.scope.Ref(notice.NoticeID, 1), ResultRef: notice.ResultRef, ConsumerTaskRef: notice.ConsumerTaskRef, HolderRef: notice.HolderRef, DefectRef: notice.DefectRef, Reason: notice.Reason, RegisteredAt: notice.RegisteredAt}
	receive := func(n task.ResultNotice) error {
		_, err := h.store.Within(context.Background(), h.scope, []string{"task"}, func(tx runtime.Tx) error {
			return h.service.RecordResultNoticeTx(context.Background(), tx, h.trusted(), n)
		})
		return err
	}
	if err = receive(transfer); err != nil {
		t.Fatal(err)
	}
	if err = receive(transfer); err != nil {
		t.Fatal(err)
	}
	transfer.Reason = "different text at original notice identity"
	if err = receive(transfer); !api.IsCode(err, "idempotency_conflict") {
		t.Fatalf("original notice changed %+v %v", transfer, err)
	}
	view, err := h.service.Result(context.Background(), h.store, h.scope, h.auth, current.TaskID, task.ResultInput{})
	if err != nil || !api.Equal(before, api.Raw(view.Result)) || len(view.NoticeRefs) != 1 || len(view.Notices) != 1 || view.Notices[0] != notice.Reason {
		t.Fatalf("late defect lost or rewrote Result %+v %v", view, err)
	}
}
