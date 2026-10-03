package governance_test

import (
	"testing"
	"time"

	"github.com/ruipengliu/lerna/adapters/platform"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/governance"
	"github.com/ruipengliu/lerna/runtime"
)

func TestSignedEvidenceContinuousImportAckGapAndAuthorityEpoch(t *testing.T) {
	authority := environment(t, governance.Options{})
	consumer := environment(t, governance.Options{})
	consumer.scope.TenantID = authority.scope.TenantID
	consumer.auth.TenantID = authority.scope.TenantID
	authority.auth.Roles = append(authority.auth.Roles, "evidence_consumer")
	keys, err := platform.NewDevelopmentKey(authority.scope.TenantID, authority.scope.OwnerID, []string{"evidence_eligibility", "evidence_changes"})
	if err != nil {
		t.Fatal(err)
	}
	proof := localProof{ring: keys}
	authority.svc.Ports.Proof = proof
	consumer.svc.Ports.Proof = proof
	taskID := api.NewID("task")
	check := makeCheck(t, authority, taskID, []api.ObjectRef{})
	checkRef := authority.scope.Ref(check.CheckID, 1)
	consumerRef := consumer.scope.Ref(taskID, 1)
	dependencyDigest, err := api.Digest([]api.ObjectRef{checkRef})
	if err != nil {
		t.Fatal(err)
	}
	request := governance.EligibilityRequest{CheckRef: checkRef, ConsumerTaskRef: consumerRef, RuleRef: check.RuleRef, EvaluatorRef: check.EvaluatorRef, ReportRef: check.ReportRef, ScopeRef: check.ScopeRef, DependencyDigest: dependencyDigest, RequestedMaxAgeSeconds: 60, PrepareDeadline: api.Time(time.Now().Add(time.Minute))}
	requestID := api.NewID("eligibility")
	_, received := command(t, authority, "evidence.eligibility.check", requestID, request, nil)
	if received.Stage != "applied" {
		t.Fatalf("eligibility: %+v", received)
	}
	var receipt governance.EligibilityReceipt
	if err = api.Decode(received.Output, &receipt); err != nil {
		t.Fatal(err)
	}
	importBaseline := func(r governance.EligibilityReceipt) error {
		_, err := consumer.store.Within(consumer.ctx, consumer.scope, []string{governance.Namespace}, func(tx runtime.Tx) error { return consumer.svc.InstallEvidenceBaselineTx(consumer.ctx, tx, r, true) })
		return err
	}
	modified := receipt
	modified.Cursor++
	if err = importBaseline(modified); err == nil {
		t.Fatal("unsigned cursor mutation imported")
	}
	if err = importBaseline(receipt); err != nil {
		t.Fatal(err)
	}
	completion := governance.EvidenceCompletion{ConsumerTaskRef: consumerRef, Checks: []governance.CheckReference{{CheckRef: checkRef}}, MaxStalenessSeconds: 60}
	current := func() error {
		_, err := consumer.store.Within(consumer.ctx, consumer.scope, []string{governance.Namespace}, func(tx runtime.Tx) error {
			_, e := consumer.svc.CheckEvidenceTx(consumer.ctx, tx, completion)
			return e
		})
		return err
	}
	if err = current(); err != nil {
		t.Fatal(err)
	}
	registerUnrelated := func() {
		d := governance.DefectRegister{DefectID: api.NewID("defect"), RuleRef: component("unrelated_rule"), EvaluatorRef: component("unrelated_evaluator"), ScopeRef: ref(t, authority, "unrelated_scope"), EvidenceRef: ref(t, authority, "defect_evidence")}
		_, r := command(t, authority, "evidence.defect.register", d.DefectID, d, nil)
		if r.Stage != "applied" {
			t.Fatalf("defect: %+v", r)
		}
	}
	registerUnrelated()
	page := query[governance.ChangesOutput](t, authority, "evidence.defect.changes", governance.ChangesRequest{HolderRef: receipt.HolderRef, AuthorityEpoch: receipt.AuthorityEpoch, Cursor: receipt.Cursor, Limit: 1})
	if page.FromCursor != 0 || page.NextCursor != 1 || page.Proof == "" {
		t.Fatalf("page: %+v", page)
	}
	importPage := func(p governance.ChangesOutput) error {
		_, err := consumer.store.Within(consumer.ctx, consumer.scope, []string{governance.Namespace}, func(tx runtime.Tx) error { return consumer.svc.ImportEvidenceChangesTx(consumer.ctx, tx, checkRef, p) })
		return err
	}
	modifiedPage := page
	modifiedPage.Changes[0].EvidenceRef = ref(t, authority, "tampered_evidence")
	if err = importPage(modifiedPage); err == nil {
		t.Fatal("unsigned page mutation imported")
	}
	// 替换切片副本，保留原签名页准确字节。
	page = query[governance.ChangesOutput](t, authority, "evidence.defect.changes", governance.ChangesRequest{HolderRef: receipt.HolderRef, AuthorityEpoch: receipt.AuthorityEpoch, Cursor: receipt.Cursor, Limit: 1})
	if err = importPage(page); err != nil {
		t.Fatal(err)
	}
	if err = importPage(page); err != nil {
		t.Fatalf("duplicate delivery: %v", err)
	}
	ack := governance.HolderAck{HolderRef: receipt.HolderRef, AuthorityEpoch: receipt.AuthorityEpoch, ThroughCursor: page.NextCursor, ImportDigest: api.Hash([]byte("wrong"))}
	_, r := command(t, authority, "evidence.holder.ack", receipt.HolderRef.ObjectID, ack, nil)
	if r.Stage != "rejected" || r.Error.Reason != "ack_import_digest_mismatch" {
		t.Fatalf("bad ack: %+v", r)
	}
	ack.ImportDigest = page.Digest
	_, r = command(t, authority, "evidence.holder.ack", receipt.HolderRef.ObjectID, ack, nil)
	if r.Stage != "applied" {
		t.Fatalf("ack: %+v", r)
	}
	registerUnrelated()
	registerUnrelated()
	skipped := query[governance.ChangesOutput](t, authority, "evidence.defect.changes", governance.ChangesRequest{HolderRef: receipt.HolderRef, AuthorityEpoch: receipt.AuthorityEpoch, Cursor: 2, Limit: 1})
	if err = importPage(skipped); err != nil {
		t.Fatal(err)
	}
	if err = current(); err == nil || !api.IsCode(err, "snapshot_required") {
		t.Fatalf("gap admitted completion: %v", err)
	}
	readback := query[governance.EligibilityReceipt](t, authority, "evidence.eligibility.read", governance.IDInput{ID: receipt.ReceiptID})
	if !api.Equal(readback, receipt) {
		t.Fatal("query renewed original receipt")
	}
	rows, err := authority.store.List(authority.ctx, authority.scope, "governance/authority", "", "", 1)
	if err != nil || len(rows) != 1 {
		t.Fatalf("head: %v %+v", err, rows)
	}
	var head governance.AuthorityHead
	if err = rows[0].Decode(&head); err != nil {
		t.Fatal(err)
	}
	_, err = authority.store.Within(authority.ctx, authority.scope, []string{governance.Namespace}, func(tx runtime.Tx) error {
		_, e := authority.svc.AdvanceAuthorityEpochTx(authority.ctx, tx, head.Revision, head.Epoch+1)
		return e
	})
	if err != nil {
		t.Fatal(err)
	}
	_, r = command(t, authority, "evidence.holder.ack", receipt.HolderRef.ObjectID, ack, nil)
	if r.Stage != "rejected" || r.Error.Code != "authority_changed" {
		t.Fatalf("old epoch ack: %+v", r)
	}
	newID := api.NewID("eligibility")
	_, r = command(t, authority, "evidence.eligibility.check", newID, request, nil)
	if r.Stage != "applied" {
		t.Fatalf("fresh epoch baseline: %+v", r)
	}
	var fresh governance.EligibilityReceipt
	if err = api.Decode(r.Output, &fresh); err != nil {
		t.Fatal(err)
	}
	if fresh.AuthorityEpoch != receipt.AuthorityEpoch+1 || fresh.Cursor != 3 || fresh.HolderRef.ObjectID == receipt.HolderRef.ObjectID {
		t.Fatalf("fresh baseline: %+v", fresh)
	}
	if err = importBaseline(fresh); err != nil {
		t.Fatal(err)
	}
	if err = current(); err != nil {
		t.Fatalf("new signed baseline remains blocked: %v", err)
	}
}
