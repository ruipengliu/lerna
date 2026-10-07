package rules

import "github.com/ruipengliu/lerna/core/ledger"

// queryResponse 保存两个原参考查询协议的字段，各解析器分别限定其字段与绑定。
type queryResponse struct {
	Account            string `json:"account"`
	Origin             string `json:"origin"`
	QueryStatus        string `json:"query_status"`
	Protocol           string `json:"protocol"`
	QueryExternalKey   string `json:"query_external_key"`
	QueryAttemptID     string `json:"query_attempt_id"`
	QueryOperationID   string `json:"query_operation_id"`
	ReadTerminal       *bool  `json:"read_terminal"`
	SubjectExternalKey string `json:"subject_external_key"`
	SubjectAttemptID   string `json:"subject_attempt_id"`
	SubjectOperationID string `json:"subject_operation_id"`
	SubjectScope       string `json:"subject_scope"`
	Applied            *bool  `json:"applied"`
	Terminal           *bool  `json:"terminal"`
	NegativeProof      bool   `json:"negative_proof"`
	RetryAfterMs       int64  `json:"retry_after_ms"`
}

// queryEvidence 只归一化已核验的原主体证据，不替代查询自身的终局或核心历史裁决。
func queryEvidence(response *queryResponse, conflict bool, rule string) *ledger.QueryEvidence {
	fact := &ledger.QueryEvidence{Rule: rule, Outcome: "UNKNOWN", LateEffect: "MAY_OCCUR", Reason: "QUERY_RESULT_UNKNOWN", Conflict: conflict}
	if response != nil {
		fact.RetryAfterMs = &response.RetryAfterMs
	}
	if response != nil && *response.ReadTerminal && (response.QueryStatus == "" || response.QueryStatus == "AVAILABLE") {
		fact.Reason = "SUBJECT_NOT_TERMINAL"
		if *response.Applied {
			fact.Outcome = "APPLIED"
		}
		if *response.Terminal && (*response.Applied || response.NegativeProof) {
			fact.LateEffect, fact.Reason = "RULED_OUT", "TERMINAL_SUBJECT_PROOF"
			if !*response.Applied {
				fact.Outcome = "NOT_APPLIED"
			}
		}
	}
	if response != nil && *response.ReadTerminal && response.QueryStatus == "RETENTION_EXPIRED" {
		fact.Reason = "QUERY_RETENTION_EXPIRED"
	}
	if response != nil && *response.ReadTerminal && response.QueryStatus == "TEMPORARILY_UNAVAILABLE" {
		fact.Reason = "QUERY_TEMPORARILY_UNAVAILABLE"
	}
	if conflict {
		fact.Reason = "EVIDENCE_CONFLICT"
	}
	return fact
}
