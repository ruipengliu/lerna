package execution

import "lerna/authorization"

type offlineBinding struct {
	replica *authorization.OfflineReplica
	read    authorization.OfflineRead
}

// BindOffline adds remote authorization to existing local policy and guards.
// Hosts must restore this trusted binding before exposing a recovered service.
func (s *Service) BindOffline(replica *authorization.OfflineReplica, read authorization.OfflineRead) (*Service, error) {
	if s == nil || replica == nil {
		return nil, failure(authorization.Invalid)
	}
	out := *s
	out.offline = &offlineBinding{replica, read}
	return &out, nil
}
