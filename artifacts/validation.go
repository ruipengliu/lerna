package artifacts

import (
	"encoding/hex"
	wire "lerna/gen/harness/v1"
	"strings"
)

func validKey(k string) bool {
	b, e := hex.DecodeString(k)
	return e == nil && len(b) == 32 && strings.ToLower(k) == k
}
func text(v string) bool { return len(v) > 0 && len(v) <= 128 && !strings.ContainsAny(v, "\x00\n\r") }
func validate(in *wire.ContentRequest, c Config) error {
	invalid := Error("INVALID_ARGUMENT")
	if len(in.OperationId) > 512 || len(in.Purpose) > 128 {
		return invalid
	}
	switch in.Method {
	case "PUT":
		p := in.Spec
		if p == nil || in.Ref != nil || in.Offset != 0 || in.Limit != 0 || in.ExpectedRevision != 0 || in.Purpose != "" || in.OperationId == "" {
			return invalid
		}
		if (p.Kind != "evidence" && p.Kind != "artifact") || !text(p.Resource) || !text(p.Purpose) || !text(p.MediaType) || p.AcquiredAt <= 0 || p.Size == 0 || p.Size > c.MaxObject || p.Size != uint64(len(in.Data)) || p.Sha256 != digest(in.Data) || len(p.Sources) == 0 || len(p.Sources) > 16 {
			return invalid
		}
		for _, src := range p.Sources {
			if src == nil || !text(src.Kind) || !text(src.Key) || src.Revision == 0 {
				return invalid
			}
		}
	case "GET", "READ", "DELETE":
		if in.Ref == nil || !validKey(in.Ref.Key) || in.Ref.Revision != 1 || !text(in.Ref.Namespace) || !text(in.Purpose) || in.Spec != nil || len(in.Data) != 0 {
			return invalid
		}
		if in.Method == "READ" {
			if in.Limit == 0 || in.Limit > uint32(c.MaxChunk) || in.Offset > c.MaxObject || in.OperationId != "" || in.ExpectedRevision != 0 {
				return invalid
			}
		} else if in.Limit != 0 || in.Offset != 0 {
			return invalid
		}
		if in.Method == "DELETE" {
			if in.OperationId == "" || in.ExpectedRevision == 0 {
				return invalid
			}
		} else if in.ExpectedRevision != 0 || in.OperationId != "" {
			return invalid
		}
	case "LOOKUP":
		if in.OperationId == "" || !text(in.Purpose) || in.Ref != nil || in.Spec != nil || len(in.Data) != 0 || in.Offset != 0 || in.Limit != 0 || in.ExpectedRevision != 0 {
			return invalid
		}
	default:
		return Error("UNSUPPORTED")
	}
	return nil
}
