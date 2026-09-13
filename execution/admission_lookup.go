package execution

import (
	"context"
	"lerna/authorization"
	"reflect"
)

// LookupAdmission checks an exact original request using the same identity and
// authority checks as Invoke for missing records. Existing receipts require
// current read authority and exact identity, independently of invoke authority.
// A false result permits attempting admission, not execution: Invoke must still validate current authority and reserve its permit.
// Unlike GetInvocation, missing and denied are not conflated for this request.
func (s *Service) LookupAdmission(ctx context.Context, in Request) (Receipt, bool, error) {
	if !s.valid(in) {
		return Receipt{}, false, failure(authorization.Invalid)
	}
	var receipt *Receipt
	err := s.transaction(ctx, func(j *journal, tx authorization.ExecutionTransaction) error {
		if prior, ok := j.Records[in.OperationID]; ok {
			if err := s.authorize(tx, "capability.read"); err != nil {
				return err
			}
			if !s.owned(prior) {
				return failure(authorization.Denied)
			}
			if err := tx.ExecutionOperation(in.OperationID, s.binding.Subject, false); err != nil {
				return err
			}
			if !reflect.DeepEqual(prior.Request, in) {
				return failure(authorization.IdentityConflict)
			}
			original := prior.Receipt
			receipt = &original
			return nil
		}
		var err error
		receipt, err = s.admission(j, tx, in)
		return err
	})
	if err != nil {
		return Receipt{}, false, err
	}
	if receipt == nil {
		return Receipt{}, false, nil
	}
	return *receipt, true, nil
}
