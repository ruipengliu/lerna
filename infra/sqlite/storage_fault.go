//go:build fault

package sqlite

import "context"

// StorageFaultSQL 仅供存储故障验收设置负对照和触发检查点。
func (s *Store) StorageFaultSQL(statement string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.conn.ExecContext(context.Background(), statement)
	return err
}
