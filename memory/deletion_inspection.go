package memory

import (
	"context"
	"google.golang.org/protobuf/proto"
	wire "lerna/gen/harness/v1"
)

// DeletionReporting is trusted host configuration, not a request-supplied
// source of completion claims. Its implementation verifies authoritative events.
type DeletionReporting interface {
	Status(context.Context, SourceEvent) (DeletionStatus, error)
}
type DeletionStatusRequest struct {
	OperationID string
	Ref         *wire.MemoryRef
	Purpose     string
}
type DeletionInspection struct {
	State  string
	Report *DeletionStatus
}

func (s *Service) WithDeletionReporter(reporter DeletionReporting) (*Service, error) {
	if reporter == nil {
		return nil, Invalid
	}
	out := *s
	out.deletionReporter = reporter
	return &out, nil
}

// DeletionStatus requires current permission for the claimed record and the
// original operation. It neither allocates identities nor reads erased bodies.
func (s *Service) DeletionStatus(ctx context.Context, b Binding, in DeletionStatusRequest) (DeletionInspection, error) {
	ctx, cancel := context.WithTimeout(ctx, s.config.Timeout)
	defer cancel()
	if !s.binding(b) || !text(in.OperationID, 256) || !validReference(in.Ref) || !text(in.Purpose, 256) {
		return DeletionInspection{}, Denied
	}
	in.Ref = proto.Clone(in.Ref).(*wire.MemoryRef)
	authority, ok := s.authority.(DeletionAuthority)
	if !ok {
		return DeletionInspection{}, Unavailable
	}
	if err := authority.CheckDeletion(ctx, b, in.Ref, in.Purpose); err != nil {
		return DeletionInspection{}, err
	}
	admission, err := s.authority.Inspect(ctx, b, in.OperationID)
	if err != nil {
		return DeletionInspection{}, err
	}
	receipt, err := s.store.LookupOperation(ctx, b.Namespace, in.OperationID)
	out := DeletionInspection{}
	if err == Missing {
		if admission.Reserved {
			out.State = "unknown"
		} else {
			switch admission.State {
			case "not_admitted":
				out.State = "not_admitted"
			case "expired":
				out.State = "admission_expired"
			default:
				return DeletionInspection{}, Unavailable
			}
		}
	} else if err != nil {
		return DeletionInspection{}, Unavailable
	} else {
		if receipt.Subject != b.Subject || receipt.Ref != refOf(in.Ref) {
			return DeletionInspection{}, Denied
		}
		if !admission.Reserved || s.deletionReporter == nil {
			return DeletionInspection{}, Unavailable
		}
		event := SourceEvent{Ref: receipt.Ref, Kind: SourceDeleted, Revision: receipt.Revision, Position: receipt.Position}
		report, e := s.deletionReporter.Status(ctx, event)
		if e != nil {
			return DeletionInspection{}, e
		}
		if report.Event != event || report.Authority != "committed" {
			return DeletionInspection{}, Unavailable
		}
		out = DeletionInspection{State: "committed", Report: &report}
	}
	// Permission may have changed while reading independent cleanup progress.
	if err = authority.CheckDeletion(ctx, b, in.Ref, in.Purpose); err != nil {
		return DeletionInspection{}, err
	}
	if _, err = s.authority.Inspect(ctx, b, in.OperationID); err != nil {
		return DeletionInspection{}, err
	}
	return out, nil
}
