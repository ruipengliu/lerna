package artifacts

import (
	"encoding/json"
	"google.golang.org/protobuf/proto"
	"lerna/authorization"
	wire "lerna/gen/harness/v1"
)

// Successful source-authority checks are required conditions of this retained
// use, alongside direct content actions. Source callbacks must not probe optional
// permissions through this view and then ignore a successful result.
type useTransaction struct {
	authorization.ContentTransaction
	actions []*wire.AuthorizationAction
}

func (t *useTransaction) Authorize(token string, action *wire.AuthorizationAction) (authorization.Identity, error) {
	id, err := t.ContentTransaction.Authorize(token, action)
	if err != nil {
		return id, err
	}
	for _, old := range t.actions {
		if proto.Equal(old, action) {
			return id, nil
		}
	}
	if len(t.actions) >= 16 {
		return authorization.Identity{}, Error("CAPACITY_EXCEEDED")
	}
	t.actions = append(t.actions, proto.Clone(action).(*wire.AuthorizationAction))
	return id, nil
}

func (s *Service) useConfig() string {
	raw, _ := json.Marshal(struct {
		Version int
		Config  Config
	}{1, s.config})
	return digest(raw)
}

func (s *Service) invalidateUses(tx authorization.ContentTransaction, j *journal) error {
	for key, e := range j.Records {
		r, err := decode(e)
		if err != nil {
			return err
		}
		if r.State != "available" {
			continue
		}
		active := false
		if e.UseID != "" {
			state, err := tx.UseStatus(e.UseID, "artifacts", s.useConfig())
			if err != nil {
				return err
			}
			active = state == "active"
		}
		// Legacy objects with no original use proof remain unavailable. Only
		// explicit trusted Clean removes their payload; never infer a new grant.
		// A separately pinned migration must run before this retirement.
		if !active {
			r.State = "cleaning"
			r.LifecycleRevision++
			e.Body = nil
			j.Records[key] = encode(r, e)
		}
	}
	return nil
}
