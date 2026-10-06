package sqlite

import (
	"context"

	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
)

func (s *Store) SaveFileResources(ctx context.Context, r *v1.FileResources) error {
	return s.saveRecord(ctx, "content", "INSERT INTO file_resources VALUES(?,?,?,?) ON CONFLICT(user_id,id) DO UPDATE SET record=excluded.record", r, r.Ref.Name.UserId, r.Ref.Name.LocalId, r.SendRef.Name.LocalId)
}
func (s *Store) LoadFileResources(ctx context.Context, r *v1.Ref) (*v1.FileResources, error) {
	v := new(v1.FileResources)
	ok, e := s.load(ctx, v, "SELECT record FROM file_resources WHERE user_id=? AND id=?", r.Name.UserId, r.Name.LocalId)
	if !ok {
		return nil, e
	}
	return v, e
}
func (s *Store) FileResourcesForSend(ctx context.Context, r *v1.Ref) (*v1.FileResources, error) {
	v := new(v1.FileResources)
	ok, e := s.load(ctx, v, "SELECT record FROM file_resources WHERE user_id=? AND send_id=?", r.Name.UserId, r.Name.LocalId)
	if !ok {
		return nil, e
	}
	return v, e
}

func (s *Store) SaveManagedFileRoot(ctx context.Context, r *v1.ManagedFileRoot) error {
	return s.saveRecord(ctx, "content", "INSERT INTO managed_file_roots VALUES(?,?,?)", r, r.Ref.Name.UserId, r.RootId)
}
func (s *Store) LoadManagedFileRoot(ctx context.Context, user, id string) (*v1.ManagedFileRoot, error) {
	v := new(v1.ManagedFileRoot)
	ok, e := s.load(ctx, v, "SELECT record FROM managed_file_roots WHERE user_id=? AND root_id=?", user, id)
	if !ok {
		return nil, e
	}
	return v, e
}
