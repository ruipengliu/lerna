package execution

import (
	"context"
	"lerna/authorization"
)

// MatchPeer checks a host-created service binding against transport-authenticated
// identity. The caller cannot choose the service's local token or worker.
func (s *Service) MatchPeer(p authorization.GrantPresentation) error {
	b := s.binding
	if p.Namespace != b.Namespace || p.Subject != b.Subject || p.Audience != b.Audience || p.Presenter != b.Presenter || p.CertificateSHA256 != b.CertificateSHA256 {
		return failure(authorization.Denied)
	}
	return nil
}

// ReadRemoteInvocation rechecks the original grant before remote disclosure.
// Local recovery can still inspect denied/expired work through GetInvocation.
func (s *Service) ReadRemoteInvocation(ctx context.Context, op string) (Record, error) {
	var out Record
	e := s.transaction(ctx, func(j *journal, tx authorization.ExecutionTransaction) error {
		if e := s.authorize(tx, "capability.read"); e != nil {
			return e
		}
		r, ok := j.Records[op]
		if !ok || !s.owned(r) {
			return failure(authorization.Denied)
		}
		if e := tx.ExecutionOperation(op, s.binding.Subject, false); e != nil {
			return e
		}
		if e := tx.ValidateUse(r.Permit, s.binding.Presentation(r.Request), s.action("resource.change")); e != nil {
			return e
		}
		out = r
		return nil
	})
	return out, e
}
