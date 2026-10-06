package sqlite

import (
	"context"

	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

// checkSavedContracts 只核验本物理格式中保存的契约元数据，不作业务裁决或跨域写入。
// 全部核验完成后宿主才可以恢复任一负责方，避免别的域先产生部分推进。
func (s *Store) checkSavedContracts(ctx context.Context, q querier) error {
	for _, entry := range []struct {
		table     string
		prototype proto.Message
	}{
		{"jobs", &v1.Job{}}, {"ledger_jobs", &v1.Job{}}, {"reconciliation_jobs", &v1.Job{}},
		{"content_observations", &v1.ObservationHandoff{}}, {"source_trace_outbox", &v1.TraceSourceRecord{}},
		{"observation_reports", &v1.ObservationReports{}}, {"input_deliveries", &v1.InputDelivery{}},
		{"model_calls", &v1.ModelCall{}}, {"proposal_requests", &v1.ProposalRequest{}}, {"proposal_outcomes", &v1.ProposalOutcome{}},
		{"completion_intents", &v1.CompletionClosureIntent{}},
		{"grant_revocations", &v1.GrantRevocation{}}, {"reconciliation_queries", &v1.ReconciliationQuery{}},
		{"operation_progress_handoffs", &v1.OperationProgressHandoff{}}, {"handoffs", &v1.Handoff{}},
		{"content_registrations", &v1.ContentRegistration{}},
		{"cancellations", &v1.Cancellation{}}, {"cancellation_intents", &v1.CancellationClosureIntent{}},
		{"cancellation_seals", &v1.CancellationSeal{}}, {"cancellation_sealed_operations", &v1.CancellationSeal{}},
		{"task_closings", &v1.TaskClosing{}}, {"task_closure_intents", &v1.TaskClosureIntent{}},
		{"task_closure_seals", &v1.TaskClosureSeal{}}, {"task_sealed_operations", &v1.TaskClosureSeal{}},
		{"execution_followups", &v1.ExecutionFollowup{}}, {"settlement_followups", &v1.SettlementFollowup{}},
		{"execution_followup_jobs", &v1.Job{}},
		{"reasoner_drivers", &v1.ReasonerDriver{}}, {"reasoner_driver_versions", &v1.ReasonerDriver{}},
	} {
		rows, e := q.QueryContext(ctx, "SELECT record FROM "+entry.table+" WHERE user_id=?", s.user)
		if e != nil {
			return e
		}
		for rows.Next() {
			var b []byte
			if e = rows.Scan(&b); e != nil {
				rows.Close()
				return e
			}
			m := entry.prototype.ProtoReflect().New().Interface()
			if e = proto.Unmarshal(b, m); e != nil {
				rows.Close()
				return command.Fail("UNSUPPORTED_DATABASE_FORMAT")
			}
			if e = command.CheckSavedHeaders(m); e != nil {
				rows.Close()
				return e
			}
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return e
		}
	}
	return nil
}
