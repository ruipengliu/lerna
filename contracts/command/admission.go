package command

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// ValidateHeader 拒绝未知的授权语义，包括嵌套消息。
func ValidateHeader(h *v1.CommandHeader, body proto.Message) error {
	if h == nil || h.ContractVersion != 1 || h.FingerprintVersion != 1 || h.SchemaId != "lerna.v1.AdmissionCommands" {
		return Fail("UNSUPPORTED_CONTRACT")
	}
	if len(h.MustUnderstand) > 0 || unknown(body.ProtoReflect()) {
		return Fail("UNSUPPORTED_FEATURE")
	}
	return nil
}
func unknown(m protoreflect.Message) bool {
	if len(m.GetUnknown()) > 0 {
		return true
	}
	bad := false
	m.Range(func(f protoreflect.FieldDescriptor, v protoreflect.Value) bool {
		if f.Message() != nil {
			if f.IsList() {
				l := v.List()
				for i := 0; i < l.Len(); i++ {
					bad = bad || unknown(l.Get(i).Message())
				}
			} else {
				bad = unknown(v.Message())
			}
		}
		return !bad
	})
	return bad
}

// SemanticFingerprint 的调用者按版本显式列举全部业务字段。
func SemanticFingerprint(kind string, fields ...any) string {
	b, _ := json.Marshal(struct {
		Version uint32
		Kind    string
		Fields  []any
	}{1, kind, fields})
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
