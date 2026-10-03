package governance_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/governance"
	"github.com/ruipengliu/lerna/runtime"
)

// 同库消费 seam：独立保存原通知，故障使治理进度及交接一起回滚。
type noticeReceiver struct{ Fail bool }

func (r *noticeReceiver) RecordNoticeTx(ctx context.Context, tx runtime.Tx, in governance.ResultNotice) error {
	if r.Fail {
		return errors.New("trusted receiver temporarily unavailable")
	}
	var existing governance.ResultNotice
	_, e := tx.Get(ctx, "governance/fixture_notices", in.NoticeID, &existing)
	if e == nil {
		if !api.Equal(existing, in) {
			return api.E("idempotency_conflict", "original_notice_changed")
		}
		return nil
	}
	if !api.IsCode(e, "not_found") {
		return e
	}
	return tx.Create(ctx, "governance/fixture_notices", in.NoticeID, in.ConsumerTaskRef.ObjectID, in)
}

func TestDefectNoticePageRollbackReplaysOriginalAndContinuesPastHundredHolders(t *testing.T) {
	receiver := &noticeReceiver{Fail: true}
	f := environment(t, governance.Options{ResultNotices: receiver})
	taskID := api.NewID("task")
	check := makeCheck(t, f, taskID, nil)
	result := f.scope.Ref(api.NewID("result"), 1)
	for i := 0; i < 101; i++ {
		current := check
		if i > 0 {
			current.CheckID = api.NewID("check")
			current.RequirementID = api.NewID("requirement")
		}
		consumer := f.scope.Ref(taskID, 1)
		status, e := f.store.Within(f.ctx, f.scope, []string{governance.Namespace}, func(tx runtime.Tx) error {
			if i > 0 {
				if _, e := f.svc.RegisterCheckTx(f.ctx, tx, current); e != nil {
					return e
				}
			}
			_, e := f.svc.CheckEvidenceTx(f.ctx, tx, governance.EvidenceCompletion{ConsumerTaskRef: consumer, Checks: []governance.CheckReference{{CheckRef: f.scope.Ref(current.CheckID, 1)}}, ResultRef: &result})
			return e
		})
		if e != nil || status != runtime.Committed {
			t.Fatalf("registered holder %d: %v", i, e)
		}
	}
	defect := governance.DefectRegister{DefectID: api.NewID("defect"), RuleRef: check.RuleRef, EvaluatorRef: check.EvaluatorRef, ScopeRef: check.ScopeRef, EvidenceRef: ref(t, f, "defect")}
	_, receipt := command(t, f, "evidence.defect.register", defect.DefectID, defect, nil)
	if receipt.Stage != "applied" {
		t.Fatalf("defect: %+v", receipt)
	}
	work, status, e := f.store.Claim(f.ctx, f.scope, api.NewID("worker"), []string{"governance.defect"}, 1, time.Minute)
	if e != nil || status != runtime.Committed || len(work) != 1 {
		t.Fatalf("claim: %v %s %v", work, status, e)
	}
	handler, ok := f.registry.Job("governance.defect")
	if !ok {
		t.Fatal("defect handler absent")
	}
	if e = handler(f.ctx, f.store, f.scope, work[0]); e == nil {
		t.Fatal("receiver failure was swallowed")
	}
	f.auth.Roles = append(f.auth.Roles, "evidence_consumer")
	page := query[api.Page[governance.ResultNotice]](t, f, "evidence.notice.list", api.ListInput{Limit: 100})
	if len(page.Items) != 0 {
		t.Fatal("receiver rollback left notice or advanced cursor")
	}
	receiver.Fail = false
	if e = handler(f.ctx, f.store, f.scope, work[0]); e != nil {
		t.Fatal(e)
	}
	// 同一原 work 完成第一页，后续领取必须真正 Ready 而不是被 FinishDone 吞掉。
	drain(t, f, "governance.defect")
	all := []governance.ResultNotice{}
	cursor := ""
	for i := 0; i < 3; i++ {
		page = query[api.Page[governance.ResultNotice]](t, f, "evidence.notice.list", api.ListInput{Limit: 100, Cursor: cursor})
		all = append(all, page.Items...)
		cursor = page.NextCursor
		if cursor == "" {
			break
		}
	}
	if len(all) != 101 {
		t.Fatalf("remaining holder page was swallowed: %d", len(all))
	}
	for _, notice := range all {
		var received governance.ResultNotice
		if _, e := f.store.Read(f.ctx, f.scope, "governance/fixture_notices", notice.NoticeID, 0, &received); e != nil || !api.Equal(received, notice) {
			t.Fatalf("mechanical original notice handoff changed: %+v %v", notice, e)
		}
	}
}
