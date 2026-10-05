package content

// These tokens recognize only the current Go BodySeal byte encoding. They
// neither serialize JSON nor change any stored value. The query supplies this
// expression as a parameter, avoiding an additional SQL-string escape layer.
const canonicalSealStringUnit = `([^"\\\x01-\x1f<>&\u2028\u2029]|\\(["\\bfnrt]|u00(0[0-7bef]|1[0-9a-f])|u(0026|003c|003e|2028|2029)))`

// Dates are parsed separately by PostgreSQL under the original finite query
// deadline. Fractions and offsets here preserve Go RFC3339Nano Marshal form;
// the independent +1us selection hint never changes a Go execution deadline.
const canonicalSealTime = `"[0-9]{4}-(0[1-9]|1[0-2])-(0[1-9]|[12][0-9]|3[01])T([01][0-9]|2[0-3]):[0-5][0-9]:[0-5][0-9]([.][0-9]{0,8}[1-9])?(Z|[+-](00:([0-5][1-9]|[1-5]0)|(0[1-9]|1[0-9]|2[0-3]):[0-5][0-9]))"`

func canonicalBodySealEncoding() string {
	text := `"` + canonicalSealStringUnit + `*"`
	nonempty := `"` + canonicalSealStringUnit + `+"`
	owner := `\{"tenant_id":` + text + `,"owner_id":` + text + `\}`
	ref := `\{"owner":` + owner + `,"content_id":` + text + `,"version":` + text + `,"hash":` + text + `,"media_type":` + text + `,"byte_length":` + text + `\}`
	delegate := `\{"tenant_id":` + text + `,"subject_id":` + text + `\}`
	subject := `\{"tenant_id":` + text + `,"subject_id":` + text + `,"delegation_chain":\[(` + delegate + `(,` + delegate + `){0,15})?\]\}`
	return `\A\{("policy_change_key":` + nonempty + `,)?"primary_holder_binding":` + text + `,"primary_holder_id":` + text + `,"id":` + text + `,"content_ref":` + ref + `,"subject":` + subject + `,"purpose":` + text + `,"started_at":` + canonicalSealTime + `,"deadline":` + canonicalSealTime + `\}\Z`
}
