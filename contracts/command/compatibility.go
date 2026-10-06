package command

import (
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// CheckSavedJobContract 仅核验原保存格式，不从调用者旧副本推断当前解释资格。
func CheckSavedJobContract(j *v1.Job) error {
	if j == nil {
		return Fail("STALE_CLAIM")
	}
	if j.ContractVersion != 1 {
		return Fail("UNSUPPORTED_CONTRACT")
	}
	if unknown(j.ProtoReflect()) {
		return Fail("UNSUPPORTED_FEATURE")
	}
	if j.Goal != nil {
		c := j.Goal.Command
		if c == nil || c.ContractVersion != 1 || c.FingerprintVersion != 1 || c.SchemaId != "lerna.v1.SubmitGoal" {
			return Fail("UNSUPPORTED_CONTRACT")
		}
		if len(c.MustUnderstand) != 0 || len(c.ProtoReflect().GetUnknown()) != 0 {
			return Fail("UNSUPPORTED_FEATURE")
		}
	}
	return nil
}

// CheckSavedHeaders 核验持久交接中的原命令版本，已存在对方回执也不能跳过。
func CheckSavedHeaders(m proto.Message) error {
	if unknown(m.ProtoReflect()) {
		return Fail("UNSUPPORTED_FEATURE")
	}
	var failure error
	var visit func(protoreflect.Message)
	visit = func(record protoreflect.Message) {
		switch c := record.Interface().(type) {
		case *v1.CommandHeader:
			failure = ValidateHeader(c, c)
		case *v1.Job:
			failure = CheckSavedJobContract(c)
		case *v1.ReasonerDriver:
			if c.ContractVersion != 1 {
				failure = Fail("UNSUPPORTED_CONTRACT")
			}
		case *v1.SubmitGoalCommand:
			if c.ContractVersion != 1 || c.FingerprintVersion != 1 || c.SchemaId != "lerna.v1.SubmitGoal" {
				failure = Fail("UNSUPPORTED_CONTRACT")
			}
		}
		if failure != nil {
			return
		}
		record.Range(func(f protoreflect.FieldDescriptor, value protoreflect.Value) bool {
			if f.Message() == nil {
				return true
			}
			if f.IsList() {
				for i := 0; i < value.List().Len() && failure == nil; i++ {
					visit(value.List().Get(i).Message())
				}
			} else {
				visit(value.Message())
			}
			return failure == nil
		})
	}
	visit(m.ProtoReflect())
	return failure
}
