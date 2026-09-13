package memorywire

import (
	"errors"
	"google.golang.org/protobuf/proto"
	wire "lerna/gen/harness/v1"
	"lerna/internal/taskwire"
	"lerna/memory"
)

const MaxRequest = 65536
const MaxResponse = 131072

func Request(in *wire.MemoryRequest) bool {
	if in == nil || !taskwire.Known(in.ProtoReflect()) || proto.Size(in) > MaxRequest || len(in.MessageId) == 0 || len(in.MessageId) > 128 || len(in.Namespace) == 0 || len(in.Namespace) > 256 || len(in.GrantMaterial) > 32768 {
		return false
	}
	if in.Method != "DELETION_STATUS" && in.DeletionQuery != nil {
		return false
	}
	switch in.Method {
	case "PUT", "CORRECT":
		return in.Delete == nil && in.Write != nil && in.Query == nil && in.Get == nil && in.OperationId == "" && in.GrantMaterial == ""
	case "DELETION_STATUS":
		q := in.DeletionQuery
		return q != nil && q.Ref != nil && q.Ref.Namespace == in.Namespace && len(q.Ref.Collection) > 0 && len(q.Ref.Collection) <= 256 && len(q.Ref.Key) > 0 && len(q.Ref.Key) <= 256 && len(q.OperationId) > 0 && len(q.OperationId) <= 256 && len(q.Purpose) > 0 && len(q.Purpose) <= 256 && in.Delete == nil && in.Write == nil && in.Query == nil && in.Get == nil && in.OperationId == "" && in.GrantMaterial == ""
	case "DELETE":
		return in.Delete != nil && in.Write == nil && in.Query == nil && in.Get == nil && in.OperationId == "" && in.GrantMaterial == ""
	case "QUERY":
		return in.Delete == nil && in.Write == nil && in.Query != nil && in.Get == nil && in.OperationId == "" && in.GrantMaterial != ""
	case "GET":
		return in.Delete == nil && in.Write == nil && in.Query == nil && in.Get != nil && in.OperationId == "" && in.GrantMaterial != ""
	case "LOOKUP":
		return in.Delete == nil && in.Write == nil && in.Query == nil && in.Get == nil && len(in.OperationId) > 0 && len(in.OperationId) <= 256 && in.GrantMaterial == ""
	}
	return false
}
func Receipt(r memory.Receipt) *wire.MemoryReceipt {
	return &wire.MemoryReceipt{OperationId: r.OperationID, Subject: r.Subject, SemanticSha256: r.SemanticSHA256, Ref: &wire.MemoryRef{Namespace: r.Ref.Namespace, Collection: r.Ref.Collection, Key: r.Ref.Key}, Revision: r.Revision, ChangePosition: r.Position}
}
func ErrorCode(e error) string {
	var domain memory.Error
	if !errors.As(e, &domain) {
		return "UNAVAILABLE"
	}
	switch domain {
	case memory.Invalid, memory.Denied, memory.Conflict, memory.IdentityConflict, memory.Unavailable, memory.Missing, memory.Capacity, memory.Irrecoverable, memory.AdmissionExpired, memory.ReplayUnavailable:
		return string(domain)
	}
	return "UNAVAILABLE"
}
func Failure(code string) error {
	switch memory.Error(code) {
	case memory.Invalid, memory.Denied, memory.Conflict, memory.IdentityConflict, memory.Unavailable, memory.Missing, memory.Capacity, memory.Irrecoverable, memory.AdmissionExpired, memory.ReplayUnavailable:
		return memory.Error(code)
	}
	return memory.Invalid
}
