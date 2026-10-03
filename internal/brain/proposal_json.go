package brain

import "encoding/json"

// complete 的空 suggestion 也是闭合协议必填字段；其他种类不得携带此字段。
func (p Proposal) MarshalJSON() ([]byte, error) {
	type fields Proposal
	if p.Kind == "complete" && len(p.CheckSuggestions) == 0 {
		return json.Marshal(struct {
			fields
			CheckSuggestions []CheckSuggestion `json:"check_suggestions"`
		}{fields: fields(p), CheckSuggestions: []CheckSuggestion{}})
	}
	return json.Marshal(fields(p))
}
