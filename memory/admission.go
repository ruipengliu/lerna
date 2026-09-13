package memory

import "context"

const AdmissionExpired Error = "ADMISSION_EXPIRED"

// Reserved refers to authorization-side identity allocation, never a proven
// business commit. A reserved operation expiring can still have an unknown
// in-flight commit and must not be reported as conclusively absent.
type AdmissionState struct {
	State    string
	Reserved bool
}
type OperationState struct {
	State               string
	Receipt             *Receipt
	ContentAvailability string
}

func (s *Service) InspectOperation(ctx context.Context, b Binding, id string) (OperationState, error) {
	ctx, cancel := context.WithTimeout(ctx, s.config.Timeout)
	defer cancel()
	if !s.binding(b) || !text(id, 256) {
		return OperationState{}, Denied
	}

	receipt, e := s.store.LookupOperation(ctx, b.Namespace, id)
	if e == nil {
		if receipt.Subject != b.Subject {
			return OperationState{}, Denied
		}
		admission, err := s.authority.Inspect(ctx, b, id)
		if err != nil {
			return OperationState{}, err
		}
		if !admission.Reserved {
			return OperationState{}, Unavailable
		}
		availability := "unavailable"
		row, err := s.store.Read(ctx, receipt.Ref, receipt.Revision)
		if err == nil {
			record, err := decode(row)
			if err == nil && s.check(ctx, b, record.Ref, record.Spec, "read") == nil && s.validator.Validate(record.Spec.Content) == nil {
				availability = "available"
			}
		}
		// The admission authority rechecks current operation permission independently
		// of the original body and its retention/source availability.
		if _, err = s.authority.Inspect(ctx, b, id); err != nil {
			return OperationState{}, err
		}
		return OperationState{State: "committed", Receipt: &receipt, ContentAvailability: availability}, nil
	}
	if e != Missing {
		return OperationState{}, Unavailable
	}
	admission, e := s.authority.Inspect(ctx, b, id)
	if e != nil {
		return OperationState{}, e
	}
	if admission.Reserved {
		return OperationState{State: "unknown"}, nil
	}
	switch admission.State {
	case "not_admitted":
		return OperationState{State: "not_admitted"}, nil
	case "expired":
		return OperationState{State: "admission_expired"}, nil
	}
	return OperationState{}, Unavailable
}
